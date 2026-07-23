package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/retrieve"
)

// ftsText 将原文切分为空格分隔的词元文本（复用 retrieve.Terms），
// 供 FTS 表入库；MATCH 查询使用同样的切分保证词元一致。
func ftsText(s string) string { return strings.Join(retrieve.Terms(s), " ") }

// readEntry 读取并解析单个条目文件；仅当 diff 判定条目变化
// （或未变化条目需要补向量）时才被调用。
func readEntry(path string) (*entry.Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	e, err := entry.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	e.Path = path
	return e, nil
}

// Sync 将 dir（knowledge 目录）下的 Markdown 条目增量同步进索引库：
// 先用 os.ReadDir 枚举文件名+mtime（不读文件内容），与 entries 表按
// filename+mtime 对比；仅新增/变化的条目才 read+parse 并 upsert
// （entries 存原文，entries_fts 存 ftsText 切分文本），client!=nil 时
// 为变化条目重算向量、并为缺向量的未变化条目补齐向量（只读这些文件）；
// 库中多余的 filename 删除。变化条目解析失败即中止并回滚（事务性，
// CLI 路径需要暴露损坏文件）。diff 非空或 INDEX.md 缺失时重建
// <dir>/../INDEX.md，无变化的纯热路径不做任何写盘。
func (db *DB) Sync(dir string, client embed.Client) error {
	// Windows 上 DirEntry.Info 复用 readdir 数据，无额外系统调用
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type diskFile struct {
		name  string
		path  string
		mtime int64
	}
	var disk []diskFile
	for _, de := range dirents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			return err
		}
		disk = append(disk, diskFile{de.Name(), filepath.Join(dir, de.Name()), info.ModTime().Unix()})
	}

	existing := map[string]int64{}
	rows, err := db.sql.Query(`SELECT filename, mtime FROM entries`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var mtime int64
		if err := rows.Scan(&name, &mtime); err != nil {
			_ = rows.Close()
			return err
		}
		existing[name] = mtime
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	hasVector := map[string]bool{}
	vrows, err := db.sql.Query(`SELECT filename FROM vectors`)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var name string
		if err := vrows.Scan(&name); err != nil {
			_ = vrows.Close()
			return err
		}
		hasVector[name] = true
	}
	if err := vrows.Err(); err != nil {
		_ = vrows.Close()
		return err
	}
	_ = vrows.Close()

	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	alive := map[string]bool{}
	changed := false
	for _, f := range disk {
		name := f.name
		alive[name] = true
		mtime := f.mtime
		if old, ok := existing[name]; ok && old == mtime {
			// 未变化条目不读不解析、不重算向量；仅在缺向量（曾无 key 同步）且有 key 时补齐
			if client != nil && !hasVector[name] {
				e, err := readEntry(f.path)
				if err != nil {
					return rollback(err)
				}
				vec, err := client.Embed(context.Background(), e.EmbedText())
				if err != nil {
					return rollback(err)
				}
				if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
					name, len(vec), encodeVector(vec)); err != nil {
					return rollback(err)
				}
			}
			continue
		}
		changed = true
		e, err := readEntry(f.path)
		if err != nil {
			return rollback(err)
		}
		tags := strings.Join(e.Tags, ", ")
		mandatory := 0
		if e.Mandatory {
			mandatory = 1
		}
		if _, err := tx.Exec(`INSERT INTO entries(filename,title,type,tags,summary,body,mandatory,mtime)
			VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT(filename) DO UPDATE SET
			title=excluded.title, type=excluded.type, tags=excluded.tags,
			summary=excluded.summary, body=excluded.body,
			mandatory=excluded.mandatory, mtime=excluded.mtime`,
			name, e.Title, e.Type, tags, e.Summary, e.Body, mandatory, mtime); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`DELETE FROM entries_fts WHERE filename=?`, name); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`INSERT INTO entries_fts(title,tags,summary,body,filename) VALUES(?,?,?,?,?)`,
			ftsText(e.Title), ftsText(strings.Join(e.Tags, " ")), ftsText(e.Summary), ftsText(e.Body), name); err != nil {
			return rollback(err)
		}
		if client != nil {
			vec, err := client.Embed(context.Background(), e.EmbedText())
			if err != nil {
				return rollback(err)
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
				name, len(vec), encodeVector(vec)); err != nil {
				return rollback(err)
			}
		}
	}
	for name := range existing {
		if !alive[name] {
			changed = true
			for _, q := range []string{
				`DELETE FROM entries WHERE filename=?`,
				`DELETE FROM entries_fts WHERE filename=?`,
				`DELETE FROM vectors WHERE filename=?`,
			} {
				if _, err := tx.Exec(q, name); err != nil {
					return rollback(err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// diff 为空时跳过重写（hook 热路径零写盘）；INDEX.md 缺失时总是重建
	if !changed {
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "INDEX.md")); err == nil {
			return nil
		}
	}
	return db.rebuildIndex(dir)
}

// rebuildIndex 从 entries 表重写 <dir>/../INDEX.md（标题+类型+tags+摘要的固定行格式）。
func (db *DB) rebuildIndex(dir string) error {
	rows, err := db.sql.Query(`SELECT title, type, tags, summary FROM entries ORDER BY filename`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("# 知识索引\n\n")
	for rows.Next() {
		var title, typ, tags, summary string
		if err := rows.Scan(&title, &typ, &tags, &summary); err != nil {
			return err
		}
		fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, typ, tags, summary)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(filepath.Dir(dir), "INDEX.md"), []byte(b.String()), 0o644)
}
