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

// 崩溃残留的锁（mtime 早于抢占阈值）不得卡住后续 Update：应被抢占后正常执行，
// 否则一次 hook 进程被杀留下的锁文件会让该会话此后每次 Update 都白等 5s 进
// fail-open 无锁直写。
func TestUpdatePreemptsStaleLock(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, fileName("s1")+".lock")
	if err := os.WriteFile(lp, []byte("dead-holder"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lp, past, past); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Update(dir, "s1", func(s *Session) { s.AddTouched("a.go") })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stale lock not preempted")
	}
	if s := Load(dir, "s1"); len(s.Touched) != 1 {
		t.Fatalf("update lost: %+v", s)
	}
}

// TestCoolingLifecycle 冷却语义：dedupTurns=N 时注入后接下来 N 轮冷却、第 N+1 个
// 后续轮恢复；dedupTurns<=0 恒不冷却（关闭）。重新注入刷新台账轮次。
func TestCoolingLifecycle(t *testing.T) {
	s := &Session{SessionID: "s1"}
	s.PromptTurns = 1
	s.MarkRetrievalInjected([]string{"a.md"})
	// 注入后第 1、2 个后续轮冷却中（dedupTurns=2）
	s.PromptTurns = 2
	if !s.Cooling("a.md", 2) {
		t.Fatal("第 2 轮应冷却中")
	}
	s.PromptTurns = 3
	if !s.Cooling("a.md", 2) {
		t.Fatal("第 3 轮应冷却中")
	}
	// 第 3 个后续轮恢复
	s.PromptTurns = 4
	if s.Cooling("a.md", 2) {
		t.Fatal("第 4 轮应恢复")
	}
	// 恢复后重新注入 → 台账刷新，下一轮又冷却
	s.MarkRetrievalInjected([]string{"a.md"})
	if !s.Cooling("a.md", 2) {
		t.Fatal("重新注入后应立即进入冷却")
	}
	// 关闭：恒不冷却
	if s.Cooling("a.md", 0) || s.Cooling("a.md", -1) {
		t.Fatal("dedupTurns<=0 应恒不冷却")
	}
	// 从未注入的条目不冷却；nil 台账安全
	if s.Cooling("never.md", 2) {
		t.Fatal("未注入条目不冷却")
	}
	empty := &Session{SessionID: "s2"}
	if empty.Cooling("a.md", 2) || empty.CoolingSet(2) != nil {
		t.Fatal("空台账应安全返回")
	}
}

// TestCoolingSet 冷却集合只含冷却中的条目，供检索排除下推。
func TestCoolingSet(t *testing.T) {
	s := &Session{SessionID: "s1", PromptTurns: 5}
	s.InjectedLog = map[string]int{"cool.md": 4, "old.md": 1}
	set := s.CoolingSet(2)
	if !set["cool.md"] || set["old.md"] || len(set) != 1 {
		t.Fatalf("cooling set 错误: %+v", set)
	}
	if got := s.CoolingSet(0); got != nil {
		t.Fatalf("dedupTurns=0 应返回 nil: %+v", got)
	}
}

// TestAdoptableNameWindow 归因窗口 = 本轮注入 ∪ 冷却窗口内；返回库内原名（大小写
// 不敏感匹配）；窗口外与关闭时（dedupTurns=0）仅认本轮注入。
func TestAdoptableNameWindow(t *testing.T) {
	s := &Session{SessionID: "s1", PromptTurns: 5}
	s.InjectedKnowledge = []string{"cur.md"}
	s.InjectedLog = map[string]int{"cur.md": 5, "cool.md": 4, "old.md": 1}
	if got := s.AdoptableName("CUR.MD", 2); got != "cur.md" {
		t.Fatalf("本轮注入应归因且返回原名, got %q", got)
	}
	if got := s.AdoptableName("Cool.MD", 2); got != "cool.md" {
		t.Fatalf("冷却窗口内应归因且返回原名, got %q", got)
	}
	if got := s.AdoptableName("old.md", 2); got != "" {
		t.Fatalf("窗口外不应归因, got %q", got)
	}
	if got := s.AdoptableName("cool.md", 0); got != "" {
		t.Fatalf("dedupTurns=0 时冷却条目不归因, got %q", got)
	}
	if got := s.AdoptableName("never.md", 2); got != "" {
		t.Fatalf("未注入条目不归因, got %q", got)
	}
}

// TestSessionCooldownRoundTrip 台账与轮次随状态 JSON 落盘/读回；旧版状态文件
// （无新字段）按零值自愈，冷却判定安全。
func TestSessionCooldownRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Session{SessionID: "rt", PromptTurns: 3}
	s.MarkRetrievalInjected([]string{"a.md"})
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	back := Load(dir, "rt")
	if back.PromptTurns != 3 || !back.Cooling("a.md", 2) {
		t.Fatalf("roundtrip 后台账丢失: %+v", back)
	}
	// 旧版文件（无 prompt_turns/injected_log 字段）：零值自愈不 panic
	if err := os.WriteFile(filepath.Join(dir, fileName("legacy")),
		[]byte(`{"session_id":"legacy","base_injected":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := Load(dir, "legacy")
	if legacy.PromptTurns != 0 || legacy.Cooling("a.md", 3) || legacy.CoolingSet(3) != nil {
		t.Fatalf("旧版状态应零值自愈: %+v", legacy)
	}
}

// 损坏的状态文件按空状态加载（自愈），不得返回半解析结果——半截 Session 被回写
// 会把损坏"洗白"成错误状态（如 BaseInjected=false 触发 mandatory 重注入）。
func TestLoadCorruptFileSelfHeals(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, fileName("s1"))
	if err := os.WriteFile(p, []byte(`{"session_id":"s1","base_injected":tr`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load(dir, "s1")
	if s.BaseInjected || len(s.Touched) != 0 || len(s.BlockedRules) != 0 {
		t.Fatalf("corrupt state must load as empty: %+v", s)
	}
}
