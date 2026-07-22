package retrieve

import (
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
)

func TestTerms(t *testing.T) {
	got := Terms("Git 提交规范")
	want := map[string]bool{"git": true, "提交": true, "交规": true, "规范": true}
	if len(got) != len(want) {
		t.Fatalf("terms %v", got)
	}
	for _, term := range got {
		if !want[term] {
			t.Fatalf("unexpected term %q in %v", term, got)
		}
	}
}

func TestKeywordScore(t *testing.T) {
	e := &entry.Entry{Title: "Git 提交规范", Tags: []string{"git"}, Summary: "提交信息怎么写"}
	if s := KeywordScore("git 提交 规范", e); s <= 0 {
		t.Fatalf("expected positive, got %v", s)
	}
	if s := KeywordScore("zzz qqq", e); s != 0 {
		t.Fatalf("expected 0, got %v", s)
	}
}

func TestRankHybridAndTopN(t *testing.T) {
	entries := []*entry.Entry{
		{Title: "不相关", Type: "note", Path: "a.md", Summary: "无"},
		{Title: "Git 提交规范", Type: "rule", Tags: []string{"git"}, Path: "b.md", Summary: "提交信息"},
		{Title: "构建命令", Type: "note", Path: "c.md", Summary: "构建"},
		{Title: "强制规则", Type: "rule", Mandatory: true, Path: "d.md", Summary: "git"},
	}
	vs := &embed.VectorSet{Vectors: map[string]*embed.EntryVector{
		"c.md": {ModTime: 1, Vector: []float32{1, 0}},
	}}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 2}
	got := Rank(entries, "git 提交", []float32{1, 0}, vs, cfg)
	if len(got) != 2 {
		t.Fatalf("expected top 2, got %d (%+v)", len(got), got)
	}
	for _, s := range got {
		if s.Entry.Mandatory {
			t.Fatal("mandatory entry must be excluded")
		}
	}
	// 纯关键词退化：queryVec=nil 时仍能按关键词命中
	got = Rank(entries, "git 提交", nil, vs, cfg)
	if len(got) == 0 || got[0].Entry.Title != "Git 提交规范" {
		t.Fatalf("degraded ranking wrong %+v", got)
	}
}
