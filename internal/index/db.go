// Package index 提供基于 SQLite+FTS5 的知识条目索引，
// 使检索在大规模条目（万级）下无需逐文件扫描 Markdown。
package index

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// schema 建表语句。entries 存原文（供注入与 INDEX.md）；
// entries_fts 是独立内容的 FTS5 表（不用 external-content + 触发器），
// 存 ftsText 切分后的文本，由同步代码显式维护（delete+insert），
// 行与 entries 以 filename 关联（UNINDEXED 列）。
const schema = `
CREATE TABLE IF NOT EXISTS entries(
  filename TEXT PRIMARY KEY,
  title TEXT NOT NULL, type TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  mandatory INTEGER NOT NULL DEFAULT 0,
  draft INTEGER NOT NULL DEFAULT 0,
  mtime INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS vectors(
  filename TEXT PRIMARY KEY,
  dim INTEGER NOT NULL, blob BLOB NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
  title, tags, summary, body,
  filename UNINDEXED
);
`

// DB 是知识索引库句柄。
type DB struct {
	sql *sql.DB
}

// Open 打开（必要时创建）索引库并建表；若同目录存在旧版 vectors.json
// 且 vectors 表为空，则导入其向量并将该文件改名为 vectors.json.bak。
func Open(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := sqldb.Exec(schema); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	db := &DB{sql: sqldb}
	if err := db.migrateDraftColumn(); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	if err := db.migrateVectorsJSON(path); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return db, nil
}

// migrateDraftColumn 为 v2.0 及更早的库补 entries.draft 列
// （CREATE TABLE IF NOT EXISTS 不会改动已存在的表）。
func (db *DB) migrateDraftColumn() error {
	rows, err := db.sql.Query(`PRAGMA table_info(entries)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.RawBytes
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "draft" {
			has = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if has {
		return nil
	}
	_, err = db.sql.Exec(`ALTER TABLE entries ADD COLUMN draft INTEGER NOT NULL DEFAULT 0`)
	return err
}

// Close 关闭索引库。
func (db *DB) Close() error { return db.sql.Close() }

// Count 返回已索引的条目数。
func (db *DB) Count() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&n)
	return n, err
}

// legacyVectors 是旧版 vectors.json 的格式（v1.2 embed.VectorSet），仅用于迁移导入。
type legacyVectors struct {
	Vectors map[string]struct {
		Vector []float32 `json:"vector"`
	} `json:"vectors"`
}

// migrateVectorsJSON 导入旧版 vectors.json。
func (db *DB) migrateVectorsJSON(dbPath string) error {
	vj := filepath.Join(filepath.Dir(dbPath), "vectors.json")
	if _, err := os.Stat(vj); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM vectors`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	data, err := os.ReadFile(vj)
	if err != nil {
		return err
	}
	var vs legacyVectors
	if err := json.Unmarshal(data, &vs); err != nil {
		return err
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	for name, ev := range vs.Vectors {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
			name, len(ev.Vector), encodeVector(ev.Vector)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.Rename(vj, vj+".bak")
}

// encodeVector 将 float32 向量编码为小端字节 blob（位级精确往返）。
func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// decodeVector 是 encodeVector 的逆操作。
func decodeVector(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
