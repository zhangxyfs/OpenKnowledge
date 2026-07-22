package store

import (
	"os"
	"strings"
	"testing"

	"openknowledge/internal/entry"
)

func TestIndexContent(t *testing.T) {
	entries := []*entry.Entry{
		{Title: "规则A", Type: "rule", Tags: []string{"a", "b"}, Summary: "摘要A"},
	}
	got := IndexContent(entries)
	if !strings.Contains(got, "**规则A** (rule) [a, b] — 摘要A") {
		t.Fatalf("unexpected index %q", got)
	}
}

func TestTruncateToBudget(t *testing.T) {
	s := strings.Repeat("好", 100)
	got := TruncateToBudget(s, 10) // 预算 20 runes
	if !strings.HasSuffix(got, "…(已截断)") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	if got := TruncateToBudget("short", 100); got != "short" {
		t.Fatalf("short text should pass through, got %q", got)
	}
}

func TestRebuildIndex(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if err := s.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	entries := []*entry.Entry{{Title: "A", Type: "note", Summary: "s"}}
	if err := s.RebuildIndex(entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.IndexPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "**A**") {
		t.Fatalf("unexpected %q", data)
	}
}
