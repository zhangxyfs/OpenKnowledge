package index

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchOf(t *testing.T) {
	if got := BranchOf([]string{"wiki", "branch:dev"}); got != "dev" {
		t.Errorf("got %q", got)
	}
	if got := BranchOf([]string{"wiki", "agentx"}); got != "" {
		t.Errorf("无分支标签应为空，got %q", got)
	}
	if got := BranchOf(nil); got != "" {
		t.Errorf("nil 应为空，got %q", got)
	}
}

func TestFilterHitsByBranch(t *testing.T) {
	hits := []Hit{
		{Filename: "a.md", Title: "共享", Tags: []string{"wiki"}},
		{Filename: "b.md", Title: "dev 差异", Tags: []string{"wiki", "branch:dev"}},
		{Filename: "c.md", Title: "master 差异", Tags: []string{"wiki", "branch:master"}},
	}
	got := FilterHitsByBranch(hits, "dev")
	if len(got) != 2 || got[0].Title != "共享" || got[1].Title != "dev 差异" {
		t.Fatalf("dev 视角应留共享+dev: %+v", got)
	}
	// 分支未知：不过滤
	if got := FilterHitsByBranch(hits, ""); len(got) != 3 {
		t.Fatalf("空分支不过滤: %+v", got)
	}
}

func TestTrimIndexBranchSections(t *testing.T) {
	idx := "# 知识索引\n\n- **x** (note) [] — s\n\n## Wiki 目录\n\n- [架构总览](a.md) — s\n\n## 分支差异（dev）\n\n- [架构总览（dev 分支差异）](b.md) — s\n\n## 分支差异（hotfix）\n\n- [x（hotfix 分支差异）](c.md) — s\n"
	got := TrimIndexBranchSections(idx, "dev")
	if !strings.Contains(got, "## 分支差异（dev）") || !strings.Contains(got, "架构总览（dev 分支差异）") {
		t.Errorf("当前分支小节应保留: %q", got)
	}
	if strings.Contains(got, "hotfix") {
		t.Errorf("其他分支小节应裁掉: %q", got)
	}
	if !strings.Contains(got, "## Wiki 目录") || !strings.Contains(got, "[架构总览](a.md)") {
		t.Errorf("主目录不受影响: %q", got)
	}
	// 空分支不裁剪
	if got := TrimIndexBranchSections(idx, ""); got != idx {
		t.Errorf("空分支应原样返回")
	}
	// 无差异小节：逐字节不变
	plain := "# 知识索引\n\n## Wiki 目录\n\n- [a](a.md)\n"
	if got := TrimIndexBranchSections(plain, "dev"); got != plain {
		t.Errorf("无差异小节应逐字节不变（零回归）")
	}
}

// 全程未裁任何节（只有当前分支小节）时也必须逐字节返回原文，
// 且重复调用结果稳定（幂等）——否则 hook 每次注入会让 INDEX 多一个尾部换行。
func TestTrimIndexBranchSectionsIdempotent(t *testing.T) {
	only := "# 知识索引\n\n## Wiki 目录\n\n- [a](a.md) — s\n\n## 分支差异（dev）\n\n- [b](b.md) — s\n"
	got := TrimIndexBranchSections(only, "dev")
	if got != only {
		t.Fatalf("未裁任何节应逐字节不变:\n%q\nwant:\n%q", got, only)
	}
	if twice := TrimIndexBranchSections(got, "dev"); twice != got {
		t.Fatalf("幂等：两次调用结果应一致:\n%q\nvs:\n%q", twice, got)
	}
	// 裁掉其他分支小节后再次调用同样稳定
	mixed := "# 知识索引\n\n## 分支差异（dev）\n\n- [b](b.md) — s\n\n## 分支差异（hotfix）\n\n- [c](c.md) — s\n"
	once := TrimIndexBranchSections(mixed, "dev")
	if strings.Contains(once, "hotfix") {
		t.Fatalf("hotfix 节应被裁: %q", once)
	}
	if twice := TrimIndexBranchSections(once, "dev"); twice != once {
		t.Fatalf("裁剪后再次调用应稳定:\n%q\nvs:\n%q", twice, once)
	}
	// 原文无末尾换行的形态也要保持
	noNL := "# 知识索引\n\n## 分支差异（dev）\n\n- [b](b.md) — s"
	if got := TrimIndexBranchSections(noNL, "dev"); got != noNL {
		t.Fatalf("无末换行原文应逐字节不变:\n%q\nwant:\n%q", got, noNL)
	}
}

// HasBranchWiki 空串防御：空分支（非 git/未知）不得把无分支 wiki 条目误判为差异条目。
func TestHasBranchWikiEmptyBranch(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "w.md", "---\ntitle: 架构总览\ntype: reference\ntags: [wiki]\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	if has, err := db.HasBranchWiki(""); err != nil || has {
		t.Fatalf("空分支必须返回 (false, nil)，got (%v, %v)", has, err)
	}
	// 有 dev 差异条目时：dev 命中、空串仍不命中
	writeEntryFile(t, kdir, "d.md", "---\ntitle: 架构总览（dev 分支差异）\ntype: reference\ntags: [wiki, branch:dev]\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n")
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	if has, err := db.HasBranchWiki("dev"); err != nil || !has {
		t.Fatalf("dev 应命中，got (%v, %v)", has, err)
	}
	if has, err := db.HasBranchWiki(""); err != nil || has {
		t.Fatalf("空分支仍须 (false, nil)，got (%v, %v)", has, err)
	}
}
