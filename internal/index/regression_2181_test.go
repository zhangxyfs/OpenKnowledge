package index

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 归档的 mandatory 条目不参与强制注入（v2.18.1 回归：Mandatory 的 SQL 曾漏
// archived 过滤，归档的过时规则仍每会话全文注入）。
func TestMandatoryExcludesArchived(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "live.md", "---\ntitle: 现行规则\ntype: rule\nmandatory: true\nsummary: s\n---\n正文 live\n")
	writeEntryFile(t, kdir, "old.md", "---\ntitle: 过时规则\ntype: rule\nmandatory: true\narchived: true\nsummary: s\n---\n正文 old\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Mandatory()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "现行规则" {
		t.Fatalf("Mandatory 应排除归档条目: %+v", rows)
	}
}

// wiki 标签按整 tag 精确匹配（v2.18.1 回归：LIKE '%wiki%' 子串会把
// sewiki/nowiki 误判为 wiki 条目）；WikiCount 与 WikiEntries 同口径（含归档过滤）。
func TestWikiTagExactMatch(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "real.md", "---\ntitle: 真 wiki\ntype: reference\ntags: [wiki, 架构]\nsummary: s\n---\n正文\n")
	writeEntryFile(t, kdir, "fake1.md", "---\ntitle: 假 wiki 一\ntype: note\ntags: [sewiki]\nsummary: s\n---\n正文 xxonlyone\n")
	writeEntryFile(t, kdir, "fake2.md", "---\ntitle: 假 wiki 二\ntype: note\ntags: [nowiki, x]\nsummary: s\n---\n正文\n")
	writeEntryFile(t, kdir, "arch.md", "---\ntitle: 归档 wiki\ntype: reference\ntags: [wiki]\narchived: true\nsummary: s\n---\n正文\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}

	entries, err := db.WikiEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Title != "真 wiki" {
		t.Fatalf("WikiEntries 应只有精确 wiki 且未归档条目: %+v", entries)
	}
	if n, _ := db.WikiCount(); n != 1 {
		t.Fatalf("WikiCount = %d, want 1（须与 WikiEntries 同口径，归档不计）", n)
	}
	// HasWikiMatch：只在假 wiki 条目里出现的关键词不应命中
	ok, err := db.HasWikiMatch([]string{"xxonlyone"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("HasWikiMatch 把 sewiki 条目误判为 wiki 覆盖")
	}
	// 主列表应保留 sewiki 条目（它不是 wiki 条目，不该被挪进 Wiki 目录节）
	data, err := os.ReadFile(filepath.Join(root, "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	i := strings.Index(s, "## Wiki 目录")
	if i < 0 {
		t.Fatalf("INDEX.md missing wiki section:\n%s", s)
	}
	if !strings.Contains(s[:i], "假 wiki 一") {
		t.Fatalf("sewiki 条目应留在主列表:\n%s", s[:i])
	}
	if strings.Contains(s[i:], "假 wiki") || strings.Contains(s[i:], "归档 wiki") {
		t.Fatalf("Wiki 目录节只应含真 wiki:\n%s", s[i:])
	}
}

// 未变化且缺向量的损坏条目：跳过并告警，不中止整轮 Sync（v2.18.1 回归：
// 补齐路径曾 rollback 整个事务——模型切换/新配 embedding 后叠加同秒写坏的
// 文件时 ok index 持续失败）。
func TestSyncSkipsCorruptUnchangedEntry(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "good.md", e3)
	writeEntryFile(t, kdir, "bad.md", e1)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// 首轮 nil client：条目与 mtime 入库、无向量
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}

	// 同 mtime 把 bad.md 写坏（秒级粒度下同秒写坏的等价模拟）
	victim := filepath.Join(kdir, "bad.md")
	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("---\ntitle: [unclosed\n---\n\n已损坏\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(victim, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}

	// 第二轮带 embedder：两条都缺向量、都未变化 → 走补齐路径；bad 损坏应跳过
	err = db.Sync(kdir, fakeEmbedder{})
	var corrupt *CorruptEntriesError
	if !errors.As(err, &corrupt) {
		t.Fatalf("expected *CorruptEntriesError, got %v", err)
	}
	if len(corrupt.Files) != 1 || corrupt.Files[0] != "bad.md" {
		t.Fatalf("CorruptEntriesError.Files = %v", corrupt.Files)
	}
	// good.md 的向量已补齐——整轮同步未被坏文件中止
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM vectors WHERE filename='good.md'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("good.md 向量未补齐，同步疑似被损坏条目中止")
	}
}
