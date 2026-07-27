package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

const e1 = `---
title: Git 提交规范
type: note
tags: [git]
summary: 提交信息格式
---

使用 Conventional Commits。
`

const e2 = `---
title: 变更日志强制规则
type: rule
mandatory: true
summary: 改代码必须写日志
---

改完代码先写日志。
`

const e3 = `---
title: 构建命令速查
type: reference
tags: [build]
summary: 常用构建命令
---

go build ./... 即可。
`

func writeEntryFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeEmbedder 返回确定性向量，避免真实网络。
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// 含 "git" 的文本给 [1,0]，其余给 [0,1]
	if strings.Contains(strings.ToLower(text), "git") {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func setupDB(t *testing.T) (*DB, string) {
	t.Helper()
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	writeEntryFile(t, kdir, "rule.md", e2)
	writeEntryFile(t, kdir, "build.md", e3)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, kdir
}

func TestSyncAndCount(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	n, err := db.Count()
	if err != nil || n != 3 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	// INDEX.md 已重建
	data, err := os.ReadFile(filepath.Join(filepath.Dir(kdir), "INDEX.md"))
	if err != nil || !strings.Contains(string(data), "Git 提交规范") {
		t.Fatalf("INDEX.md not rebuilt: %v %q", err, data)
	}
	// 幂等：mtime 未变时再次 Sync 不报错且数量不变
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	// 删除一个文件后同步应删除对应行
	if err := os.Remove(filepath.Join(kdir, "build.md")); err != nil {
		t.Fatal(err)
	}
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Count(); n != 2 {
		t.Fatalf("after delete count=%d", n)
	}
}

func TestQueryKeywordAndHybrid(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 3}
	// 关键词命中 git 条目
	hits, err := db.Query(retrieve.Terms("git 提交规范"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("keyword query wrong: %+v", hits)
	}
	if hits[0].Body == "" {
		t.Fatal("hit should carry body for injection")
	}
	// mandatory 条目不出现在检索结果
	for _, h := range hits {
		if h.Title == "变更日志强制规则" {
			t.Fatal("mandatory entry must be excluded from query")
		}
	}
	// 语义通道：queryVec=[1,0] 与 git 条目向量同向 → 即使关键词不命中也能召回
	hits, err = db.Query(retrieve.Terms("zzz"), []float32{1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("semantic recall wrong: %+v", hits)
	}
}

func TestMandatory(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Mandatory()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "变更日志强制规则" || rows[0].Body == "" {
		t.Fatalf("mandatory wrong: %+v", rows)
	}
}

func TestVectorsJSONMigration(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	// 造一个旧版 vectors.json（格式与 embed.VectorSet 相同）
	vj := `{"vectors":{"git.md":{"mod_time":1,"vector":[1,0]}}}`
	if err := os.WriteFile(filepath.Join(root, "vectors.json"), []byte(vj), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Stat(filepath.Join(root, "vectors.json.bak")); err != nil {
		t.Fatalf("vectors.json should be renamed to .bak: %v", err)
	}
}

func TestManyEntriesQuery(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	for i := 0; i < 2000; i++ {
		body := "---\ntitle: 噪音条目" + strings.Repeat("x", 1) + string(rune('a'+i%26)) +
			"\ntype: note\nsummary: 噪音\n---\n\n噪音正文\n"
		writeEntryFile(t, kdir, "noise"+strings.Repeat("a", i%7+1)+string(rune('a'+i%26))+".md", body)
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := db.Query(retrieve.Terms("git 提交"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("2k entries query wrong: %+v", hits)
	}
}

// 两个连接并发写同一库：无 busy_timeout 时第二个立即 SQLITE_BUSY；
// 有 busy_timeout 时等待第一个提交后成功。
func TestOpenBusyTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	db1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	tx, err := db1.sql.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO entries(filename,title,type) VALUES('a.md','A','note')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		done <- tx.Commit()
	}()
	start := time.Now()
	_, werr := db2.sql.Exec(`INSERT INTO entries(filename,title,type) VALUES('b.md','B','note')`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if werr != nil {
		t.Fatalf("concurrent write should wait for busy_timeout, got %v", werr)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("write did not wait — busy_timeout likely not applied")
	}
}
