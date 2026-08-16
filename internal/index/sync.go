package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/fsx"
	"openknowledge/internal/retrieve"
)

// ftsText 将原文切分为空格分隔的词元文本（复用 retrieve.Terms），
// 供 FTS 表入库；MATCH 查询使用同样的切分保证词元一致。
func ftsText(s string) string { return strings.Join(retrieve.Terms(s), " ") }

// CorruptEntriesError 表示同步已完成，但有文件因解析失败被跳过。
type CorruptEntriesError struct{ Files []string }

func (e *CorruptEntriesError) Error() string {
	return fmt.Sprintf("跳过 %d 个损坏条目: %s", len(e.Files), strings.Join(e.Files, ", "))
}

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
// 收集变化条目与缺向量的未变化条目（只读这些文件）的 EmbedText，
// 提交前按 32 条一批调 EmbedDocuments 批量算向量写入 vectors 表；
// 提交后若 client 身份非空且确有向量写入，则刷新 meta 表的
// embedding_model/embedding_dim。client 身份与 meta 记录不符时
// 跳过全部向量写与 meta 更新（INDEX/FTS 照常），杜绝新旧模型向量
// 混合——需调用方显式 ClearVectors 后再同步以全量重建。
// 库中多余的 filename 删除。变化条目解析失败时跳过该文件（已索引旧行
// 保留，无旧行则缺席），其余条目照常提交——一个 YAML 笔误不能压制全部
// 注入；提交成功后若有跳过，返回 *CorruptEntriesError 警告（调用方用
// errors.As 区分）。SQL 失败、目录不可读、INDEX.md 写入失败等致命错误
// 仍中止并回滚。diff 非空或 INDEX.md 缺失时重建 <dir>/../INDEX.md，
// 无变化的纯热路径不做任何写盘。
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

	// 模型身份闸：client 身份与索引 meta 不符时跳过全部向量写（INDEX/FTS 照常），
	// 杜绝新旧模型向量混合；由 ok index 显式 ClearVectors 后全量重建。
	// meta 空但有向量 = ≤2.13 历史库（向量身份不明），同样阻断待重建。
	embedBlocked := client == nil
	if client != nil && client.ModelIdentity() != "" {
		m, _, err := db.EmbeddingMeta()
		if err == nil {
			switch {
			case m != "" && m != client.ModelIdentity():
				embedBlocked = true // 身份不符
			case m == "":
				if hv, herr := db.HasVectors(); herr == nil && hv {
					embedBlocked = true // 历史向量无身份记录（≤2.13 库），阻断待 ok index 重建
				}
			}
		}
	}
	type pendingEmbed struct{ name, text string }
	var pending []pendingEmbed

	alive := map[string]bool{}
	changed := false
	var skipped []string
	for _, f := range disk {
		name := f.name
		alive[name] = true
		mtime := f.mtime
		if old, ok := existing[name]; ok && old == mtime {
			// 未变化条目不读不解析；仅在缺向量且可算向量时收集补齐
			if !embedBlocked && !hasVector[name] {
				e, err := readEntry(f.path)
				if err != nil {
					return rollback(err)
				}
				pending = append(pending, pendingEmbed{name, e.EmbedText()})
			}
			continue
		}
		changed = true
		e, err := readEntry(f.path)
		if err != nil {
			// 损坏条目跳过：已索引旧行保留（新文件则缺席），其余条目照常提交；
			// mtime 未入库，下次同步会重试并在修复后自动追上
			skipped = append(skipped, name)
			continue
		}
		tags := strings.Join(e.Tags, ", ")
		mandatory := 0
		if e.Mandatory {
			mandatory = 1
		}
		draft := 0
		if e.Draft {
			draft = 1
		}
		if _, err := tx.Exec(`INSERT INTO entries(filename,title,type,tags,summary,body,mandatory,draft,mtime)
			VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(filename) DO UPDATE SET
			title=excluded.title, type=excluded.type, tags=excluded.tags,
			summary=excluded.summary, body=excluded.body,
			mandatory=excluded.mandatory, draft=excluded.draft, mtime=excluded.mtime`,
			name, e.Title, e.Type, tags, e.Summary, e.Body, mandatory, draft, mtime); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`DELETE FROM entries_fts WHERE filename=?`, name); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`INSERT INTO entries_fts(title,tags,summary,body,filename) VALUES(?,?,?,?,?)`,
			ftsText(e.Title), ftsText(strings.Join(e.Tags, " ")), ftsText(e.Summary), ftsText(e.Body), name); err != nil {
			return rollback(err)
		}
		if !embedBlocked {
			pending = append(pending, pendingEmbed{name, e.EmbedText()})
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
	const embedBatchSize = 32
	vecDim := 0
	for i := 0; i < len(pending); i += embedBatchSize {
		j := i + embedBatchSize
		if j > len(pending) {
			j = len(pending)
		}
		texts := make([]string, 0, j-i)
		for _, p := range pending[i:j] {
			texts = append(texts, p.text)
		}
		vecs, err := client.EmbedDocuments(context.Background(), texts)
		if err != nil {
			return rollback(err)
		}
		for k, vec := range vecs {
			vecDim = len(vec)
			if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
				pending[i+k].name, len(vec), encodeVector(vec)); err != nil {
				return rollback(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if !embedBlocked && client != nil && vecDim > 0 && client.ModelIdentity() != "" {
		if err := db.SetMeta("embedding_model", client.ModelIdentity()); err != nil {
			return err
		}
		if err := db.SetMeta("embedding_dim", strconv.Itoa(vecDim)); err != nil {
			return err
		}
	}
	// 顺带 prune 60 天前的条目事件（统计性数据，失败不阻断 Sync）
	_ = db.PruneEvents(time.Now().Unix() - 60*86400)
	// diff 为空时跳过重写（hook 热路径除上方事件 prune 外零写盘）；INDEX.md 缺失时总是重建
	if !changed {
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "INDEX.md")); err == nil {
			return nil
		}
	}
	if err := db.rebuildIndex(dir); err != nil {
		return err
	}
	if len(skipped) > 0 {
		return &CorruptEntriesError{Files: skipped}
	}
	return nil
}

// rebuildIndex 从 entries 表重写 <dir>/../INDEX.md（标题+类型+tags+摘要的固定行格式）；
// 草稿行标题前加【草稿】前缀。
func (db *DB) rebuildIndex(dir string) error {
	rows, err := db.sql.Query(`SELECT title, type, tags, summary, draft FROM entries ORDER BY filename`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("# 知识索引\n\n")
	for rows.Next() {
		var title, typ, tags, summary string
		var draft int
		if err := rows.Scan(&title, &typ, &tags, &summary, &draft); err != nil {
			return err
		}
		// 已转正的 wiki 条目只进 Wiki 目录节（带链接），主列表不重复
		if draft == 0 && strings.Contains(tags, "wiki") {
			continue
		}
		// 带 branch: 标签的条目（无论类型）不进全分支共享的主列表：
		// branch 标签语义=分支专属——wiki 差异条目已在下方差异节，
		// 非 wiki 分支条目仍可按分支检索命中，只是不进共享目录
		if BranchOf(splitTags(tags)) != "" {
			continue
		}
		if draft != 0 {
			title = "【草稿】" + title
		}
		fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, typ, tags, summary)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if wikiEntries, err := db.WikiEntries(); err == nil && len(wikiEntries) > 0 {
		writeWikiLine := func(b *strings.Builder, we WikiEntry) {
			if we.Summary != "" {
				fmt.Fprintf(b, "- [%s](%s) — %s\n", we.Title, we.Filename, we.Summary)
			} else {
				fmt.Fprintf(b, "- [%s](%s)\n", we.Title, we.Filename)
			}
		}
		b.WriteString("\n## Wiki 目录\n\n")
		branches := map[string][]WikiEntry{}
		for _, we := range wikiEntries {
			if we.Branch == "" {
				writeWikiLine(&b, we)
			} else {
				branches[we.Branch] = append(branches[we.Branch], we)
			}
		}
		names := make([]string, 0, len(branches))
		for n := range branches {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "\n## 分支差异（%s）\n\n", n)
			for _, we := range branches[n] {
				writeWikiLine(&b, we)
			}
		}
	}
	return fsx.WriteFile(filepath.Join(filepath.Dir(dir), "INDEX.md"), []byte(b.String()), 0o644)
}
