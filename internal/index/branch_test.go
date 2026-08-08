package index

import (
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
