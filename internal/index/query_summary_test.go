package index

import (
	"path/filepath"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// 检索命中必须携带 summary（注入摘要行依赖），FTS 与向量通道都要有。
func TestQueryReturnsSummary(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", "---\ntitle: Git 提交规范\ntype: note\ntags: [git]\nsummary: 提交信息格式\ndraft: false\nmandatory: false\n---\n使用 Conventional Commits。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := db.Query(retrieve.Terms("git 提交"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Summary != "提交信息格式" {
		t.Fatalf("Summary = %q, want 提交信息格式", hits[0].Summary)
	}
}
