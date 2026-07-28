package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wiki 标签条目进入 INDEX.md 的 "Wiki 目录" 节（按 title 排序）；
// 草稿与非 wiki 条目不进入该节。
func TestRebuildIndexWikiSection(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "b.md", "---\ntitle: B 模块\ntype: reference\ntags: [wiki, 模块]\nsummary: B 的描述\ndraft: false\nmandatory: false\n---\n正文\n")
	writeEntryFile(t, kdir, "a.md", "---\ntitle: A 架构\ntype: reference\ntags: [wiki, 架构]\nsummary: A 的描述\ndraft: false\nmandatory: false\n---\n正文\n")
	writeEntryFile(t, kdir, "draft.md", "---\ntitle: 草稿wiki\ntype: reference\ntags: [wiki]\nsummary: 不应出现\ndraft: true\nmandatory: false\n---\n正文\n")
	writeEntryFile(t, kdir, "plain.md", "---\ntitle: 普通条目\ntype: note\ntags: [其他]\nsummary: 不进目录\ndraft: false\nmandatory: false\n---\n正文\n")

	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}

	// WikiEntries：按 title 排序、排除 draft 与非 wiki
	entries, err := db.WikiEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Title != "A 架构" || entries[1].Title != "B 模块" {
		t.Fatalf("WikiEntries: %+v", entries)
	}
	if n, _ := db.WikiCount(); n != 2 {
		t.Fatalf("WikiCount = %d, want 2", n)
	}

	// INDEX.md 含目录节且链接指向文件名；草稿 wiki 不出现在目录节中
	data, err := os.ReadFile(filepath.Join(root, "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	i := strings.Index(s, "## Wiki 目录")
	if i < 0 {
		t.Fatalf("INDEX.md missing wiki section:\n%s", s)
	}
	section := s[i:]
	if !strings.Contains(section, "- [A 架构](a.md) — A 的描述") ||
		!strings.Contains(section, "- [B 模块](b.md) — B 的描述") ||
		strings.Contains(section, "草稿wiki") ||
		strings.Contains(section, "普通条目") {
		t.Fatalf("wiki section wrong:\n%s", section)
	}
}

// 无 wiki 条目时 INDEX.md 不生成 "Wiki 目录" 节（输出与现状逐字节一致）。
func TestRebuildIndexNoWikiSection(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "x.md", "---\ntitle: X\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "INDEX.md"))
	if strings.Contains(string(data), "Wiki 目录") {
		t.Fatalf("no wiki entries should mean no section:\n%s", data)
	}
}
