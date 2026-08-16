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

// MergedChecked 持久化回环：置位后 Save/Load 仍为真（merged 检测每会话熔断
// 依赖此字段跨进程存活——hook 每次 prompt 都是新进程）。
func TestSessionMergedCheckedRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir, "s1")
	if s.MergedChecked {
		t.Fatal("新会话 MergedChecked 应为 false")
	}
	s.MergedChecked = true
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	s2 := Load(dir, "s1")
	if !s2.MergedChecked {
		t.Fatalf("MergedChecked 未持久化: %+v", s2)
	}
	// 与 WikiNudged 相互独立：置 MergedChecked 不得连带 WikiNudged
	if s2.WikiNudged {
		t.Fatalf("MergedChecked 不得误置 WikiNudged: %+v", s2)
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

// Update 基于锁内最新快照重放修改：两次 Update 修改不同字段时后者不得覆盖前者
// （并发 hook 各自 Load→Save 互相覆盖是旧路径丢 Touched/防重标记的根源）。
func TestUpdateMergesConcurrentFields(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, "s1", func(s *Session) { s.AddTouched("a.go") }); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, "s1", func(s *Session) { s.MarkBlocked("changelog_required") }); err != nil {
		t.Fatal(err)
	}
	s := Load(dir, "s1")
	if len(s.Touched) != 1 || !s.HasBlocked("changelog_required") {
		t.Fatalf("second Update must not clobber first Update's fields: %+v", s)
	}
	// Update 后锁文件必须释放，不残留 state 目录
	if _, err := os.Stat(filepath.Join(dir, fileName("s1")+".lock")); !os.IsNotExist(err) {
		t.Fatal("lock file should be released after Update")
	}
}

func TestSessionAdoptedKnowledge(t *testing.T) {
	dir := t.TempDir()
	// 去重追加
	if err := Update(dir, "s1", func(s *Session) {
		s.AddAdopted("a.md")
		s.AddAdopted("a.md")
		s.AddAdopted("b.md")
		s.InjectedKnowledge = []string{"a.md", "b.md"}
	}); err != nil {
		t.Fatal(err)
	}
	st := Load(dir, "s1")
	if len(st.AdoptedKnowledge) != 2 || st.AdoptedKnowledge[0] != "a.md" || st.AdoptedKnowledge[1] != "b.md" {
		t.Fatalf("AddAdopted 去重失败: %v", st.AdoptedKnowledge)
	}
	if len(st.InjectedKnowledge) != 2 {
		t.Fatalf("InjectedKnowledge 落盘失败: %v", st.InjectedKnowledge)
	}
	// 入账后清空挂账、保留注入清单（模拟 InjectForPrompt 开头的入账动作）
	if err := Update(dir, "s1", func(s *Session) {
		s.AdoptedKnowledge = nil
	}); err != nil {
		t.Fatal(err)
	}
	st = Load(dir, "s1")
	if len(st.AdoptedKnowledge) != 0 || len(st.InjectedKnowledge) != 2 {
		t.Fatalf("清挂账/留注入失败: %+v", st)
	}
	// 并发场合并不覆盖（既有 TestUpdateMergesConcurrentFields 同款断言路径）：
	// 两次 Update 分别改两字段互不丢
	if err := Update(dir, "s1", func(s *Session) { s.AddAdopted("c.md") }); err != nil {
		t.Fatal(err)
	}
	st = Load(dir, "s1")
	if len(st.InjectedKnowledge) != 2 || len(st.AdoptedKnowledge) != 1 {
		t.Fatalf("Update 合并失败: %+v", st)
	}
}
