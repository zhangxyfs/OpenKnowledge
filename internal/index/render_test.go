package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syncAndReadIndex 同步并返回 INDEX.md 内容。
func syncAndReadIndex(t *testing.T, db *DB, kdir string, opts ...SyncOptions) string {
	t.Helper()
	if err := db.Sync(kdir, fakeEmbedder{}, opts...); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(kdir), "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestArchivedColumnStored(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "old.md", `---
title: 老条目
type: note
archived: true
summary: s
---

正文。
`)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	var archived int
	if err := db.sql.QueryRow(`SELECT archived FROM entries WHERE filename='old.md'`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatalf("archived=%d, want 1", archived)
	}
	_ = strings.TrimSpace("") // 占位防未用导入，后续任务测试会用 strings
}
