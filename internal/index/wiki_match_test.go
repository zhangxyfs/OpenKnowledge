package index

import (
	"path/filepath"
	"testing"

	"openknowledge/internal/retrieve"
)

// HasWikiMatch 只在"非草稿且 tags 含 wiki 的条目"命中检索词时返回 true。
func TestHasWikiMatch(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "arch.md", "---\ntitle: 架构总览\ntype: reference\ntags: [wiki, 架构]\nsummary: 架构\ndraft: false\nmandatory: false\n---\n守护进程与托盘架构。\n")
	writeEntryFile(t, kdir, "git.md", "---\ntitle: Git 规范\ntype: note\ntags: [git]\nsummary: 规范\ndraft: false\nmandatory: false\n---\n使用 Conventional Commits。\n")
	writeEntryFile(t, kdir, "draft-wiki.md", "---\ntitle: 草稿维基\ntype: reference\ntags: [wiki]\nsummary: 草稿\ndraft: true\nmandatory: false\n---\n甲子园草稿内容。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}

	// wiki 条目覆盖
	if ok, err := db.HasWikiMatch(retrieve.Terms("守护进程")); err != nil || !ok {
		t.Fatalf("守护进程 should be wiki-covered: ok=%v err=%v", ok, err)
	}
	// 仅非 wiki 条目命中 → false
	if ok, err := db.HasWikiMatch(retrieve.Terms("Commits")); err != nil || ok {
		t.Fatalf("Commits should not be wiki-covered: ok=%v err=%v", ok, err)
	}
	// draft 的 wiki 条目不计入
	if ok, err := db.HasWikiMatch(retrieve.Terms("甲子园")); err != nil || ok {
		t.Fatalf("甲子园 draft wiki should not count: ok=%v err=%v", ok, err)
	}
	// 空 terms → true（不提示）
	if ok, err := db.HasWikiMatch(nil); err != nil || !ok {
		t.Fatalf("nil terms should be true: ok=%v err=%v", ok, err)
	}
}
