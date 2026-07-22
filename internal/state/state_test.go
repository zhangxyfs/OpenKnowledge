package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir, "abc/123") // 含非法字符 → 文件名被净化
	s.AddTouched("a.go")
	s.AddTouched("a.go")
	s.MarkBlocked("changelog_required")
	if len(s.Touched) != 1 {
		t.Fatal("dedupe failed")
	}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	s2 := Load(dir, "abc/123")
	if !s2.HasBlocked("changelog_required") || len(s2.Touched) != 1 {
		t.Fatalf("unexpected %+v", s2)
	}
}

func TestClean(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "session-old.json")
	if err := os.WriteFile(old, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "session-new.json")
	if err := os.WriteFile(fresh, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Clean(dir, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old state should be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh state should remain")
	}
}
