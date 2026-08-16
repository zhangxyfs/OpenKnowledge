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

func TestDedupSummary(t *testing.T) {
	cases := []struct{ title, summary, want string }{
		{"Git 提交规范", "Git 提交规范", ""},                       // 完全复读
		{"Git 提交规范", "Git 提交规范。", ""},                      // 末尾标点归一后复读
		{"Bun.spawn 无内建 timeout", "Bun.spawn 无内建 timeout：opencode 插件须手动 kill 防挂死", ""}, // 标题为摘要前缀
		{"索引膨胀治理方案", "索引膨胀治理方案分三级", ""},           // 共有前缀 8/10 ≥80%
		{"Git 提交规范", "提交信息格式", "提交信息格式"},              // 摘要补充新信息，保留
		{"短", "短甲长得多得多的补充说明", "短甲长得多得多的补充说明"}, // 共有前缀<80%，保留
		{"任意标题", "", ""},                                        // 空摘要原样
	}
	for _, c := range cases {
		if got := dedupSummary(c.title, c.summary); got != c.want {
			t.Errorf("dedupSummary(%q,%q)=%q, want %q", c.title, c.summary, got, c.want)
		}
	}
}
