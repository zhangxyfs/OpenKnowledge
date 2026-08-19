# 跨轮注入冷却（dedup_turns）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 同一 session 内已注入的检索条目冷却 N 轮（默认 3）不重复注入，冷却排除在 top_n 截断前生效，采纳归因窗口覆盖冷却期，GUI 引导页可配。

**Architecture:** 冷却时钟与台账存于 session 状态（`state.Session` 新增 `PromptTurns` 计数器与 `InjectedLog` 台账）；hook 层每轮把冷却集合作为排除参数下推给检索查询（`QueryExBranch` 新增 `exclude` 参数，在 top_n 截断前生效）；配置走 `[retrieve] dedup_turns` 单键 upsert，GUI 引导页数值输入 + 保存按钮两段式。

**Tech Stack:** Go（modernc.org/sqlite、BurntSushi/toml）、原生 JS SPA（web/）、标准库 httptest 测试。

**Spec:** `docs/superpowers/specs/2026-08-19-injection-cooldown.md`（设计裁决与外部印证的全部依据以 spec 为准，本计划是实现路径）。

## Global Constraints

- **不动分通道准入语义**（既有坑不再踩）：本特性只在检索之后做排除，不改任何通道准入/阈值逻辑。
- **fail-open**：状态损坏/锁超时/配置非法一律退回旧行为（每轮都注入），仅记 ok.log，永不阻断注入。
- **冷却语义公式**（全计划统一）：条目注入于轮次 T，则当 `PromptTurns - T <= dedup_turns` 时冷却中；`dedup_turns = N` 即注入后接下来 N 个 prompt 轮跳过、第 N+1 个后续轮恢复（例：N=2 时第 1 轮注入、第 2~3 轮跳过、第 4 轮恢复）。`dedup_turns <= 0` 关闭（旧行为）。
- **冷却时钟自走**：`PromptTurns` 每 prompt 轮 +1，门控命中轮也计（不绑任何外部计数器）。
- **冷却轮次不记 `EventInjected`**：未注入不记，反馈降权统计不被冷却污染。
- **配置写路径必须单键 upsert**（`[retrieve]` 是多人共用笔触小节，禁止整段替换），全局配置写走锁内原子写。
- **GUI 引导页交互两段式**：输入 + 保存按钮，不做 change 即存；校验 0~99，非法 400。
- **commit 风格**：中文 conventional commits（`feat(scope): 描述`），每个 Task 一次提交。
- **changelog 不硬折行**。
- **不 bump 版本号**（发布流程另走 `scripts/sync-version.sh`，不在本计划内）。
- 测试命令均为仓库根目录下 Git Bash 执行。

---

### Task 1: internal/state —— 注入台账与冷却判定

**Files:**
- Modify: `internal/state/state.go:13-35`（Session 结构体）与 `:100` 附近（方法区）
- Test: `internal/state/state_test.go`

**Interfaces:**
- Consumes: 无（纯新增）。
- Produces（后续 Task 依赖的确切签名）:
  - `Session.PromptTurns int`（json `prompt_turns`）
  - `Session.InjectedLog map[string]int`（json `injected_log,omitempty`）
  - `func (s *Session) Cooling(name string, dedupTurns int) bool`
  - `func (s *Session) CoolingSet(dedupTurns int) map[string]bool`
  - `func (s *Session) MarkRetrievalInjected(names []string)`
  - `func (s *Session) AdoptableName(base string, dedupTurns int) string`（返回匹配的库内原名，无匹配返回 ""）

- [ ] **Step 1: 写失败测试**

在 `internal/state/state_test.go` 追加：

```go
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
//（无新字段）按零值自愈，冷却判定安全。
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
```

（若 `state_test.go` 未 import `os`/`path/filepath`，测试文件头部补上。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/state/ -run "TestCooling|TestAdoptableName|TestSessionCooldown" -v`
Expected: 编译失败（`Session.Cooling` 未定义等）。

- [ ] **Step 3: 实现**

`internal/state/state.go` Session 结构体追加两个字段（放在 `AdoptedKnowledge` 之后）：

```go
	// PromptTurns 本会话 prompt 轮次计数：每轮 InjectForPrompt +1，是注入冷却的
	// 时钟；门控命中轮也计（语义"每 prompt 一轮"，时钟自走无停摆模式）。
	PromptTurns int `json:"prompt_turns"`
	// InjectedLog 检索注入台账：条目 basename（原始大小写，与索引库 filename
	// 同源，精确匹配无需大小写折叠）→ 最后注入的 prompt 轮次。冷却判定见
	// Cooling/CoolingSet；采纳归因窗口见 AdoptableName。
	InjectedLog map[string]int `json:"injected_log,omitempty"`
```

结构体之后追加方法（`strings` 包已 import）：

```go
// Cooling 报告条目是否在冷却期：距上次注入 ≤ dedupTurns 轮。dedupTurns<=0
//（关闭）恒 false；nil 台账安全。
func (s *Session) Cooling(name string, dedupTurns int) bool {
	if dedupTurns <= 0 {
		return false
	}
	last, ok := s.InjectedLog[name]
	return ok && s.PromptTurns-last <= dedupTurns
}

// CoolingSet 返回当前冷却中的条目 basename 集合（检索排除用）；无冷却或关闭
// 返回 nil。
func (s *Session) CoolingSet(dedupTurns int) map[string]bool {
	if dedupTurns <= 0 || len(s.InjectedLog) == 0 {
		return nil
	}
	out := map[string]bool{}
	for name := range s.InjectedLog {
		if s.Cooling(name, dedupTurns) {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MarkRetrievalInjected 把本轮检索注入的条目记入台账（轮次=当前 PromptTurns）；
// 重新注入刷新轮次。
func (s *Session) MarkRetrievalInjected(names []string) {
	if len(names) == 0 {
		return
	}
	if s.InjectedLog == nil {
		s.InjectedLog = map[string]int{}
	}
	for _, n := range names {
		s.InjectedLog[n] = s.PromptTurns
	}
}

// AdoptableName 报告 base 是否在采纳归因窗口内并返回库内原名：本轮注入过，
// 或冷却窗口内注入过（模型可能按历史轮指针读取冷却中的条目，仍应归因）。
// 大小写不敏感——TrackTouched 的 basename 来自 OS 工具调用路径，大小写可能与
// 库内不一致；返回原名保证 entry_events 与 entries.filename 大小写对齐。
// dedupTurns<=0 时只认本轮注入（旧行为）。
func (s *Session) AdoptableName(base string, dedupTurns int) string {
	for _, n := range s.InjectedKnowledge {
		if strings.EqualFold(n, base) {
			return n
		}
	}
	for n := range s.InjectedLog {
		if strings.EqualFold(n, base) && s.Cooling(n, dedupTurns) {
			return n
		}
	}
	return ""
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/state/ -v`
Expected: 全部 PASS（含既有用例）。

- [ ] **Step 5: Commit**

```bash
git add internal/state/state.go internal/state/state_test.go
git commit -m "feat(state): 会话状态新增 prompt 轮次计数器与检索注入台账（冷却判定与归因窗口）"
```

---

### Task 2: internal/config —— DedupTurns 配置项与单键 upsert 写路径

**Files:**
- Modify: `internal/config/config.go:177-211`（Retrieve 结构体）、`:266-271`（Default）
- Create: `internal/config/toml_upsert.go`
- Modify: `internal/config/inject.go`（重构为复用 helper，行为不变）
- Create: `internal/config/retrieve.go`
- Test: `internal/config/retrieve_test.go`（新建）；`internal/config/inject_test.go`（既有，守卫重构）

**Interfaces:**
- Consumes: 无。
- Produces:
  - `Retrieve.DedupTurns int`（toml `dedup_turns`，默认 3）
  - `func SetRetrieveDedupTurns(path string, n int) error`（GUI Task 6 依赖）
  - `func upsertTomlKey(path, section, key, keyLine string) error`（包内 helper）

- [ ] **Step 1: 写失败测试**

新建 `internal/config/retrieve_test.go`：

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetRetrieveDedupTurnsUpsert [retrieve] 小节内单键 upsert：其余键、注释与
// [retrieve.gate] 子表原样保留；键行落在 [retrieve] 顶层段内（子表之前）；
// 重复设置幂等。
func TestSetRetrieveDedupTurnsUpsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "# 顶部注释\n[retrieve]\nalpha = 1.5 # 行内注释\nfusion = \"rrf\"\n\n[retrieve.gate]\nenabled = false\nextra_phrases = [\"走起\"]\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRetrieveDedupTurns(path, 5); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"# 顶部注释", "alpha = 1.5 # 行内注释", "fusion = \"rrf\"", "dedup_turns = 5", "[retrieve.gate]\nenabled = false", `extra_phrases = ["走起"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("upsert 后丢失 %q:\n%s", want, got)
		}
	}
	// 键行必须在 [retrieve] 顶层段内（[retrieve.gate] 子表之前）
	if strings.Index(got, "dedup_turns = 5") > strings.Index(got, "[retrieve.gate]") {
		t.Fatalf("键行落在子表内:\n%s", got)
	}
	// 回读生效且子表不受影响
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 5 || cfg.Retrieve.Alpha != 1.5 || cfg.Retrieve.Gate.Enabled {
		t.Fatalf("回读值不对: %+v", cfg.Retrieve)
	}
	// 重复设置：替换而非追加
	if err := SetRetrieveDedupTurns(path, 0); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "dedup_turns") != 1 || !strings.Contains(got, "dedup_turns = 0") {
		t.Fatalf("重复设置应幂等替换:\n%s", got)
	}
}

// TestSetRetrieveDedupTurnsNoSection 无 [retrieve] 小节时文件尾追加整块
//（[retrieve.gate] 子表已存在也不受影响）。
func TestSetRetrieveDedupTurnsNoSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[retrieve.gate]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRetrieveDedupTurns(path, 7); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 7 || cfg.Retrieve.Gate.Enabled {
		t.Fatalf("回读值不对: %+v", cfg.Retrieve)
	}
}

// TestSetRetrieveDedupTurnsNewFile 文件不存在时直接创建。
func TestSetRetrieveDedupTurnsNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetRetrieveDedupTurns(path, 9); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 9 {
		t.Fatalf("got %v, want 9", cfg.Retrieve.DedupTurns)
	}
}

// TestDedupTurnsDefault 默认 3；配置文件缺省时 Load 回填默认值。
func TestDedupTurnsDefault(t *testing.T) {
	if got := Default().Retrieve.DedupTurns; got != 3 {
		t.Fatalf("默认应为 3, got %v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run "DedupTurns" -v`
Expected: 编译失败（`SetRetrieveDedupTurns` / `Retrieve.DedupTurns` 未定义）。

- [ ] **Step 3: 实现**

3a. `internal/config/config.go` Retrieve 结构体，`Feedback` 字段后追加：

```go
	// DedupTurns 是跨轮注入冷却轮数（默认 3）：同 session 内已注入的检索条目
	// 冷却 N 个 prompt 轮不再注入（门控轮也计），0=关闭（旧行为，每轮都注入）。
	// <0 按 0（使用处归一，fail-open 方向）。GUI 引导页可配（0~99）。
	DedupTurns int `toml:"dedup_turns"`
```

3b. `Default()` 的 Retrieve 字面量加 `DedupTurns: 3`（放在 `TopN: 2,` 后）。

3c. 新建 `internal/config/toml_upsert.go`（从 inject.go 抽取的通用 helper）：

```go
package config

import (
	"errors"
	"os"
	"strings"

	"openknowledge/internal/fsx"
)

// upsertTomlKey 在 config.toml 的指定小节内 upsert 单个键：小节已存在则只
// 替换/追加该键行（其余键、注释与子表原样保留），小节不存在则文件尾追加整块。
// 多人共用笔触的小节（[inject]/[retrieve]）不能整段覆盖，GUI 配置写路径统一
// 走这里。落盘经 fsx.WriteFile 原子写。
func upsertTomlKey(path, section, key, keyLine string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsx.WriteFile(path, []byte("["+section+"]\n"+keyLine+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == "["+section+"]" {
				start = i
			}
			continue
		}
		// 任何后续小节头（含 [retrieve.gate] 这类子表与 [[enforce]] 数组表）
		// 都是本小节边界
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	if start < 0 {
		if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+section+"]", keyLine)
		return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
	// 小节内 upsert：命中键行替换，未命中插到小节末尾
	hit := false
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, key) && strings.Contains(t, "=") {
			lines[i] = keyLine
			hit = true
			break
		}
	}
	if !hit {
		tail := append([]string{keyLine}, lines[end:]...)
		lines = append(lines[:end], tail...)
	}
	return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
```

3d. `internal/config/inject.go` 整体改为（行为不变，委托 helper）：

```go
package config

import "strconv"

// SetInjectMandatoryMax 在 config.toml 的 [inject] 小节内 upsert 单个键
// mandatory_max_tokens：小节已存在则只替换/追加该键行（max_tokens、
// reinject_turns 与注释原样保留），小节不存在则文件尾追加 [inject] 块。
// 与 SetGate 的整段替换不同——[inject] 是多人共用笔触的小节，不能整段覆盖。
func SetInjectMandatoryMax(path string, n int) error {
	return upsertTomlKey(path, "inject", "mandatory_max_tokens", "mandatory_max_tokens = "+strconv.Itoa(n))
}
```

3e. 新建 `internal/config/retrieve.go`：

```go
package config

import "strconv"

// SetRetrieveDedupTurns 在 config.toml 的 [retrieve] 小节内 upsert 单个键
// dedup_turns（alpha/fusion 等其余键与 [retrieve.gate] 子表原样保留）。
// 供 GUI 引导页冷却轮数配置写盘。
func SetRetrieveDedupTurns(path string, n int) error {
	return upsertTomlKey(path, "retrieve", "dedup_turns", "dedup_turns = "+strconv.Itoa(n))
}
```

- [ ] **Step 4: 跑测试确认通过（含重构守卫）**

Run: `go test ./internal/config/ -v`
Expected: 新用例与既有 `TestSetInjectMandatoryMax*` 全部 PASS（inject 重构零行为变化）。

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): 新增 retrieve.dedup_turns 配置（默认 3）与 TOML 单键 upsert 通用写路径"
```

---

### Task 3: internal/index —— 排除集合下推到 top_n 截断之前

**Files:**
- Modify: `internal/index/query.go:66-83`（QueryInfo）、`:129-145`（QueryEx/QueryExBranch）、`:154`（queryAll）、`:305-317`（结果收尾）
- Modify: `internal/hook/core.go:178`（本任务仅机械适配签名，传 nil）
- Modify: `internal/index/query_admission_test.go:56,67`（调用点补 nil）
- Test: `internal/index/query_dedup_test.go`（新建）

**Interfaces:**
- Consumes: 无（exclude 由 Task 4 才从 state 计算；本任务 hook 传 nil）。
- Produces:
  - `func (db *DB) QueryExBranch(terms []string, queryVec []float32, cfg config.Retrieve, branch string, exclude map[string]bool) ([]Hit, QueryInfo, error)`
  - `QueryInfo.CooledSkipped []string`——被排除但本轮本可准入的条目 basename（按名次排序）；exclude 为空时恒 nil。

- [ ] **Step 1: 写失败测试**

新建 `internal/index/query_dedup_test.go`：

```go
package index

import (
	"path/filepath"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// 冷却排除必须发生在 top_n 截断之前：排除第 1 名后第 3 名应补位进入 top_n=2，
// 且被排除条目记入 QueryInfo.CooledSkipped。
func TestQueryExBranchExcludeBeforeTopN(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "a.md",
		"---\ntitle: 冷却甲\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 紫晶冷却 紫晶冷却 相关内容。\n")
	writeEntryFile(t, kdir, "b.md",
		"---\ntitle: 冷却乙\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 紫晶冷却 相关内容。\n")
	writeEntryFile(t, kdir, "c.md",
		"---\ntitle: 冷却丙\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 相关内容。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 2}
	// 基线：3 条命中截到 2 条（纯关键词通道，BM25 词频排序 a>b>c）
	hits0, _, err := db.QueryExBranch(retrieve.Terms("紫晶冷却"), nil, cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits0) != 2 {
		t.Fatalf("基线应命中 2 条: %+v", hits0)
	}
	excluded := hits0[0].Filename
	// 排除第 1 名：第 3 名（c.md）补位，而不是只剩 1 条
	hits, info, err := db.QueryExBranch(retrieve.Terms("紫晶冷却"), nil, cfg, "", map[string]bool{excluded: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("排除后第 3 名应补位进 top_n=2: %+v", hits)
	}
	for _, h := range hits {
		if h.Filename == excluded {
			t.Fatalf("冷却条目应被排除: %+v", hits)
		}
	}
	if hits[0].Filename != hits0[1].Filename || hits[1].Filename != "c.md" {
		t.Fatalf("补位顺序应为原第 2、3 名: %+v", hits)
	}
	if len(info.CooledSkipped) != 1 || info.CooledSkipped[0] != excluded {
		t.Fatalf("CooledSkipped 应记录被排除条目: %+v", info.CooledSkipped)
	}
}

// exclude 传 nil / 空集合行为一致，且不产生 CooledSkipped（ok search / GUI 检索
// 的行为不变）。
func TestQueryExBranchNilExcludeUnchanged(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "a.md",
		"---\ntitle: 冷却甲\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 相关内容。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 2}
	for _, exclude := range []map[string]bool{nil, {}} {
		hits, info, err := db.QueryExBranch(retrieve.Terms("紫晶冷却"), nil, cfg, "", exclude)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].Filename != "a.md" {
			t.Fatalf("无排除时应照常命中: %+v", hits)
		}
		if len(info.CooledSkipped) != 0 {
			t.Fatalf("无排除时 CooledSkipped 应为空: %+v", info.CooledSkipped)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run "Exclude" -v`
Expected: 编译失败（`QueryExBranch` 参数数量不对 / `QueryInfo.CooledSkipped` 未定义）。

- [ ] **Step 3: 实现**

3a. `internal/index/query.go` QueryInfo 结构体追加字段（`FeedbackDemoted` 之后）：

```go
	// CooledSkipped：因跨轮冷却被 exclude 排除、但本轮本可准入的条目
	//（basename，按名次排序）；exclude 为空时恒 nil。供 hook 层记 ok.log
	//（GUI 日志页按"冷却"过滤）。
	CooledSkipped []string
```

3b. `QueryEx` 改传 nil：

```go
func (db *DB) QueryEx(terms []string, queryVec []float32, cfg config.Retrieve) ([]Hit, QueryInfo, error) {
	hits, info, err := db.queryAll(terms, queryVec, cfg, nil)
	if err != nil {
		return nil, QueryInfo{}, err
	}
	return truncateHits(hits, cfg.TopN), info, nil
}
```

3c. `QueryExBranch` 改签名并下传：

```go
// QueryExBranch 同 QueryEx，但 top_n 截断前先按 branch 过滤差异条目、再按 exclude
// 排除冷却条目（basename 精确匹配，与 session 台账/索引库 filename 同源，无需
// 大小写折叠）：截断先于过滤/排除时，其他分支或冷却中的条目白白挤占名额，注入
// 条数无谓少于 top_n 且无补位。exclude 传 nil 表示无排除（ok search / GUI 检索
// 的行为）。被排除但本可准入的条目记入 QueryInfo.CooledSkipped。
func (db *DB) QueryExBranch(terms []string, queryVec []float32, cfg config.Retrieve, branch string, exclude map[string]bool) ([]Hit, QueryInfo, error) {
	hits, info, err := db.queryAll(terms, queryVec, cfg, exclude)
	if err != nil {
		return nil, QueryInfo{}, err
	}
	return truncateHits(FilterHitsByBranch(hits, branch), cfg.TopN), info, nil
}
```

3d. `queryAll` 改签名并在收尾处应用排除：

```go
func (db *DB) queryAll(terms []string, queryVec []float32, cfg config.Retrieve, exclude map[string]bool) ([]Hit, QueryInfo, error) {
```

`return out, info, nil`（`:317`）之前插入：

```go
	// 冷却排除在排序后、返回前（QueryExBranch 内 top_n 截断随之位于排除之后）；
	// 被排除但本可准入（Score>0 进入 out）的条目记入 CooledSkipped 供观测。
	if len(exclude) > 0 {
		kept := make([]Hit, 0, len(out))
		for _, h := range out {
			if exclude[h.Filename] {
				info.CooledSkipped = append(info.CooledSkipped, h.Filename)
				continue
			}
			kept = append(kept, h)
		}
		out = kept
	}
```

3e. 机械适配既有调用点：

- `internal/hook/core.go:178`：`db.QueryExBranch(retrieve.Terms(promptText), queryVec, pc.Config.Retrieve, ws.Branch, nil)`（本任务先传 nil，Task 4 替换为冷却集合）。
- `internal/index/query_admission_test.go:56`：`db.QueryExBranch(retrieve.Terms("构建"), nil, cfg, "main", nil)`
- `internal/index/query_admission_test.go:67`：`db.QueryExBranch(retrieve.Terms("构建"), nil, cfg, "", nil)`

- [ ] **Step 4: 跑测试确认通过（全仓编译绿）**

Run: `go build ./... && go test ./internal/index/ ./internal/hook/ -v`
Expected: 全部 PASS（index 新旧用例 + hook 既有用例；hook 行为尚未变化）。

- [ ] **Step 5: Commit**

```bash
git add internal/index/ internal/hook/core.go
git commit -m "feat(index): 检索查询支持排除集合并在 top_n 截断前生效，QueryInfo 暴露冷却跳过清单"
```

---

### Task 4: internal/hook —— 冷却主链路接线

**Files:**
- Modify: `internal/hook/core.go:61-70`（采纳入账 Update：加冷却时钟与台账快照）、`:178`（检索调用：传冷却集合）、`:192-195` 附近（加冷却日志）、`:216-220`（注入挂账 Update：写台账）
- Modify: `internal/hook/core_test.go:23-47`（TestInjectForPromptBaseAndRetrieve 钉 dedup_turns=0）、`internal/hook/hook_test.go:140`（TestFirstPromptInjectsBaseOnce 钉 dedup_turns=0）
- Test: `internal/hook/core_test.go`（新增用例）

**Interfaces:**
- Consumes: Task 1 的 `Session.PromptTurns / CoolingSet / MarkRetrievalInjected`；Task 2 的 `Retrieve.DedupTurns`；Task 3 的 `QueryExBranch(..., exclude)` 与 `QueryInfo.CooledSkipped`。
- Produces: 无新接口（行为变化）。

注意：本任务完成后默认行为改变（Default DedupTurns=3），两个既有测试因"同 session 第二轮检索仍命中"的旧假设会红，必须在本任务内一并钉配置修复（它们是冷却的正交测试，钉 0 保持原意图）。

- [ ] **Step 1: 先修既有测试（钉 dedup_turns=0 保持原意图）**

1a. `internal/hook/core_test.go` `TestInjectForPromptBaseAndRetrieve`：writeEntry 两行之后、`project.FromCwd` 之前插入：

```go
	// 本测试意图是"基础注入后检索仍生效"，与跨轮冷却正交：钉 dedup_turns=0 关闭冷却
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
```

1b. `internal/hook/hook_test.go` `TestFirstPromptInjectsBaseOnce`：INDEX.md 写入之后、`mkPrompt` 定义之前插入：

```go
	// 本测试意图是"第二轮检索仍生效"，与跨轮冷却正交：钉 dedup_turns=0 关闭冷却
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
```

- [ ] **Step 2: 写失败测试**

`internal/hook/core_test.go` 追加（helper 复用 hook_test.go 的 `setupProject`/`writeEntry`）：

```go
// TestInjectCooldownSkipAndRecover 冷却主语义：dedup_turns=2 时第 1 轮注入、
// 第 2~3 轮跳过、第 4 轮恢复。
func TestInjectCooldownSkipAndRecover(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out1 := InjectForPrompt(pc, "s-cool", projDir, "RetrievalQuirk 是什么")
	if !strings.Contains(out1, "相关知识") || !strings.Contains(out1, "检索.md") {
		t.Fatalf("第 1 轮应注入检索命中, got: %q", out1)
	}
	for i, q := range []string{"RetrievalQuirk 再问", "RetrievalQuirk 三问"} {
		out := InjectForPrompt(pc, "s-cool", projDir, q)
		if strings.Contains(out, "相关知识") || strings.Contains(out, "检索.md") {
			t.Fatalf("第 %d 轮冷却期不应注入, got: %q", i+2, out)
		}
	}
	out4 := InjectForPrompt(pc, "s-cool", projDir, "RetrievalQuirk 四问")
	if !strings.Contains(out4, "检索.md") {
		t.Fatalf("第 4 轮冷却结束应恢复注入, got: %q", out4)
	}
}

// TestInjectCooldownYieldsSlot 冷却条目不占 top_n 名额：top_n=1 时第 1 轮注入
// 第 1 名，第 2 轮第 1 名冷却 → 第 2 名补位注入。
func TestInjectCooldownYieldsSlot(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "甲.md", "---\ntitle: 冷却甲\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n紫晶冷却 紫晶冷却 紫晶冷却 词。\n")
	writeEntry(t, kbRoot, "乙.md", "---\ntitle: 冷却乙\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n紫晶冷却 词。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ntop_n = 1\ndedup_turns = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out1 := InjectForPrompt(pc, "s-slot", projDir, "紫晶冷却 是什么")
	first, second := "甲.md", "乙.md"
	if !strings.Contains(out1, first) {
		first, second = second, first
	}
	if !strings.Contains(out1, first) {
		t.Fatalf("第 1 轮应注入其一, got: %q", out1)
	}
	out2 := InjectForPrompt(pc, "s-slot", projDir, "紫晶冷却 再问")
	if !strings.Contains(out2, second) || strings.Contains(out2, first) {
		t.Fatalf("第 2 轮应由另一条目补位, got: %q", out2)
	}
}

// TestInjectCooldownDisabled dedup_turns=0 关闭冷却：每轮都注入（旧行为回归保护）。
func TestInjectCooldownDisabled(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range []string{"RetrievalQuirk 一问", "RetrievalQuirk 再问", "RetrievalQuirk 三问"} {
		out := InjectForPrompt(pc, "s-off", projDir, q)
		if !strings.Contains(out, "检索.md") {
			t.Fatalf("第 %d 轮关闭冷却应照常注入, got: %q", i+1, out)
		}
	}
}

// TestCooldownGatedTurnTicks 门控命中轮也计冷却轮次：dedup_turns=1 时，第 1 轮
// 注入 → 第 2 轮门控轮（跳检索但时钟走）→ 第 3 轮轮距 2>1 恢复注入。
func TestCooldownGatedTurnTicks(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	cfg := "[retrieve]\ndedup_turns = 1\n\n[retrieve.gate]\nenabled = true\nextra_phrases = [\"泛泛而谈\"]\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if out := InjectForPrompt(pc, "s-gate-tick", projDir, "RetrievalQuirk 一问"); !strings.Contains(out, "检索.md") {
		t.Fatalf("第 1 轮应注入, got: %q", out)
	}
	if out := InjectForPrompt(pc, "s-gate-tick", projDir, "泛泛而谈"); strings.Contains(out, "相关知识") {
		t.Fatalf("第 2 轮门控命中不应注入, got: %q", out)
	}
	// 门控轮若不计冷却轮次，此处轮距为 1（仍冷却）；计则为 2（恢复）
	if out := InjectForPrompt(pc, "s-gate-tick", projDir, "RetrievalQuirk 三问"); !strings.Contains(out, "检索.md") {
		t.Fatalf("门控轮应计冷却轮次，第 3 轮应恢复注入, got: %q", out)
	}
}

// TestCooldownNoInjectedEvent 冷却跳过的轮次不记 injected 事件（反馈降权统计
// 不被冷却污染）。
func TestCooldownNoInjectedEvent(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	InjectForPrompt(pc, "s-evt", projDir, "RetrievalQuirk 一问")
	InjectForPrompt(pc, "s-evt", projDir, "RetrievalQuirk 再问") // 冷却中
	InjectForPrompt(pc, "s-evt", projDir, "RetrievalQuirk 三问") // 冷却中
	db, err := index.Open(filepath.Join(kbRoot, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	stats, err := db.FeedbackStats(30)
	if err != nil {
		t.Fatal(err)
	}
	if got := stats["检索.md"].Injections; got != 1 {
		t.Fatalf("冷却轮不应记 injected 事件（应只有首轮 1 次）, got %d", got)
	}
}

// TestInjectCooldownCorruptStateFailOpen 状态文件损坏：注入照常（fail-open），
// 台账按空状态自愈。
func TestInjectCooldownCorruptStateFailOpen(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(kbRoot, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "session-s-bad.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s-bad", projDir, "RetrievalQuirk 是什么")
	if !strings.Contains(out, "检索.md") {
		t.Fatalf("状态损坏应照常注入, got: %q", out)
	}
}
```

（`core_test.go` 若未 import `openknowledge/internal/index`，头部补上。）

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/hook/ -run "Cooldown" -v`
Expected: FAIL —— `TestInjectCooldownSkipAndRecover` 第 2 轮仍注入（冷却未接线）等。
（`TestInjectCooldownDisabled` 此刻应已 PASS——它钉了 dedup_turns=0 的旧行为。）

- [ ] **Step 4: 实现（四处改动）**

4a. `internal/hook/core.go` 采纳入账 Update（`:64-70`）改为：

```go
	var adopted []string
	var coolingSet map[string]bool
	dedupTurns := pc.Config.Retrieve.DedupTurns
	if dedupTurns < 0 {
		dedupTurns = 0
	}
	if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
		adopted = st.AdoptedKnowledge
		st.AdoptedKnowledge = nil
		// 冷却时钟：每 prompt 轮 +1（门控命中轮也计——语义"每 prompt 一轮"，
		// 时钟自走无停摆）。先推进时钟再取冷却集合，本轮与上次注入的轮距才正确。
		st.PromptTurns++
		coolingSet = st.CoolingSet(dedupTurns)
	}); err != nil {
		logErr("prompt adopt load: %v", err)
	}
```

4b. 检索调用（`:178`）把 Task 3 的 nil 换成冷却集合：

```go
		h, info, err := db.QueryExBranch(retrieve.Terms(promptText), queryVec, pc.Config.Retrieve, ws.Branch, coolingSet)
```

4c. 冷却日志：在反馈降权日志块（`:192-195` `info.FeedbackDemoted` 判断）之后插入：

```go
		// 冷却跳过（本可准入但在冷却窗口内被排除）：记 ok.log，GUI 日志页可按"冷却"过滤
		if len(info.CooledSkipped) > 0 {
			logErr("prompt dedup: 冷却跳过（%s）", strings.Join(info.CooledSkipped, "、"))
		}
```

4d. 注入挂账 Update（`:216-220`）写入台账：

```go
		if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
			st.InjectedKnowledge = names
			st.MarkRetrievalInjected(names)
		}); err != nil {
			logErr("prompt inject state: %v", err)
		}
```

- [ ] **Step 5: 跑测试确认通过（全包绿）**

Run: `go test ./internal/hook/ ./internal/state/ ./internal/index/ -v`
Expected: 全部 PASS（含 Step 1 钉配置后的两个既有用例）。

- [ ] **Step 6: Commit**

```bash
git add internal/hook/
git commit -m "feat(hook): 检索注入跨轮冷却——同 session 条目冷却 dedup_turns 轮不重复注入，排除不占 top_n 名额"
```

---

### Task 5: internal/hook —— TrackTouched 归因窗口扩展

**Files:**
- Modify: `internal/hook/core.go:292-308`（TrackTouched 知识库分支）
- Test: `internal/hook/core_test.go`

**Interfaces:**
- Consumes: Task 1 的 `Session.AdoptableName(base, dedupTurns) string`。
- Produces: 无新接口（行为变化：冷却中条目的读取也记采纳）。

- [ ] **Step 1: 写失败测试**

`internal/hook/core_test.go` 追加：

```go
// TestAdoptionDuringCooldown 冷却中的条目被模型读取（按历史轮指针）仍记采纳——
// 归因窗口 = 本轮注入 ∪ 冷却窗口内。
func TestAdoptionDuringCooldown(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(kbRoot, "state")
	if out := InjectForPrompt(pc, "s-cool-adopt", projDir, "RetrievalQuirk 一问"); !strings.Contains(out, "检索.md") {
		t.Fatalf("第 1 轮应注入, got: %q", out)
	}
	// 第 2 轮：条目冷却中（不再注入）
	if out := InjectForPrompt(pc, "s-cool-adopt", projDir, "RetrievalQuirk 再问"); strings.Contains(out, "检索.md") {
		t.Fatalf("第 2 轮应冷却中, got: %q", out)
	}
	// 模型按第 1 轮历史里的指针读取冷却中的条目 → 仍应挂账
	TrackTouched(pc, "s-cool-adopt", "read_file", filepath.Join(kbRoot, "knowledge", "检索.md"))
	st := state.Load(stateDir, "s-cool-adopt")
	if len(st.AdoptedKnowledge) != 1 || st.AdoptedKnowledge[0] != "检索.md" {
		t.Fatalf("冷却中条目的读取应记采纳: %+v", st.AdoptedKnowledge)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hook/ -run "TestAdoptionDuringCooldown" -v`
Expected: FAIL —— 第 2 轮 `InjectedKnowledge` 已被覆盖为空，`检索.md` 不在归因窗口。

- [ ] **Step 3: 实现**

`internal/hook/core.go` TrackTouched 知识库分支（`:296-307`）改为：

```go
		if base, ok := knowledgeBase(pc, filePath); ok {
			dedupTurns := pc.Config.Retrieve.DedupTurns
			if dedupTurns < 0 {
				dedupTurns = 0
			}
			// 归因窗口 = 本轮注入 ∪ 冷却窗口内（冷却中的条目模型仍可能按历史
			// 轮指针读取）；mandatory 粘性指针重读不经检索、不在台账，天然不计入。
			if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
				if name := st.AdoptableName(base, dedupTurns); name != "" {
					st.AddAdopted(name)
				}
			}); err != nil {
				logErr("post-tool adopt state: %v", err)
			}
			return
		}
```

（删除旧的 `for _, name := range st.InjectedKnowledge` 循环；注意保留上方 `relativize` 与 `return` 的控制流不变。）

- [ ] **Step 4: 跑测试确认通过（含既有采纳闭环回归）**

Run: `go test ./internal/hook/ -run "Adoption|Adopt" -v && go test ./internal/hook/`
Expected: 新用例与既有 `TestAdoptionLoop` 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/hook/core.go internal/hook/core_test.go
git commit -m "feat(hook): 采纳归因窗口扩至冷却期条目——冷却中被读取仍记采纳"
```

---

### Task 6: GUI —— /api/retrieve 端点与引导页冷却卡片

**Files:**
- Modify: `internal/gui/api.go:79-80` 附近（路由注册）、`:1257` 之后（新 handler）
- Modify: `web/index.html:146` 之后（泛化门控卡与规则配置卡之间插新卡）
- Modify: `web/app.js:238`、`:861`（刷新挂载点）、`:1052` 之后（卡片逻辑，模型配置卡注释之前）
- Test: `internal/gui/api_test.go`

**Interfaces:**
- Consumes: Task 2 的 `config.SetRetrieveDedupTurns`。
- Produces: `GET /api/retrieve?project=X` → `{"dedup_turns": int}`；`POST /api/retrieve` body `{"project": string, "dedup_turns": int}`（0~99，非法 400）。

- [ ] **Step 1: 写失败测试**

`internal/gui/api_test.go` 追加（helper 复用既有 `newEnv`/`mkProject`/`do`/`testToken`）：

```go
// /api/retrieve：dedup_turns 的 GET 默认值 / POST 落盘 / 非法值 400 / 重复设置幂等 /
// [retrieve] 其余键保留。
func TestRetrieveDedupTurnsRoundTrip(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()
	cfgPath := filepath.Join(okHome, "projects", "demo", "config.toml")

	code, data := do(t, "GET", srv.URL+"/api/retrieve?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("retrieve get: status = %d, body %s", code, data)
	}
	var view struct {
		DedupTurns int `json:"dedup_turns"`
	}
	if err := json.Unmarshal([]byte(data), &view); err != nil {
		t.Fatal(err)
	}
	if view.DedupTurns != 3 {
		t.Fatalf("默认应为 3, got %d", view.DedupTurns)
	}

	// 预置 [retrieve] 既有键，验证单键 upsert 不整段覆盖
	if err := os.WriteFile(cfgPath, []byte("[retrieve]\nalpha = 1.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _ = do(t, "POST", srv.URL+"/api/retrieve", testToken,
		map[string]any{"project": "demo", "dedup_turns": 5})
	if code != 200 {
		t.Fatalf("retrieve set: status = %d", code)
	}
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "dedup_turns = 5") || !strings.Contains(string(cfgData), "alpha = 1.5") {
		t.Fatalf("config 应含 dedup_turns = 5 且保留 alpha: %q", cfgData)
	}
	code, data = do(t, "GET", srv.URL+"/api/retrieve?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("retrieve re-get: status = %d", code)
	}
	if err := json.Unmarshal([]byte(data), &view); err != nil {
		t.Fatal(err)
	}
	if view.DedupTurns != 5 {
		t.Fatalf("GET 应反映 5, got %d", view.DedupTurns)
	}

	// 非法值：负数 / 超上限 → 400
	for _, bad := range []int{-1, 100} {
		code, _ = do(t, "POST", srv.URL+"/api/retrieve", testToken,
			map[string]any{"project": "demo", "dedup_turns": bad})
		if code != 400 {
			t.Fatalf("dedup_turns=%d 应 400, got %d", bad, code)
		}
	}
	// 0 = 关闭，是合法值；重复设置幂等：键行唯一
	code, _ = do(t, "POST", srv.URL+"/api/retrieve", testToken,
		map[string]any{"project": "demo", "dedup_turns": 0})
	if code != 200 {
		t.Fatalf("dedup_turns=0 应合法: status = %d", code)
	}
	cfgData, _ = os.ReadFile(cfgPath)
	if strings.Count(string(cfgData), "dedup_turns") != 1 {
		t.Fatalf("重复设置应幂等替换: %q", cfgData)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run "TestRetrieveDedupTurns" -v`
Expected: FAIL —— `/api/retrieve` 404。

- [ ] **Step 3: 实现**

3a. `internal/gui/api.go` 路由注册（`:80` `api("POST /api/inject", h.apiInjectSet)` 之后）：

```go
	api("GET /api/retrieve", h.apiRetrieveGet)
	api("POST /api/retrieve", h.apiRetrieveSet)
```

3b. handler（`apiInjectSet` 函数结束之后追加）：

```go
// apiRetrieveGet 返回项目合并配置中的跨轮注入冷却轮数
//（[retrieve] dedup_turns，默认 3；<0 归一为 0）。
func (h *Handler) apiRetrieveGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	n := cfg.Retrieve.DedupTurns
	if n < 0 {
		n = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{"dedup_turns": n})
}

// apiRetrieveSet 设置跨轮注入冷却轮数（0~99，0=关闭，非法 400）；落盘走
// config.SetRetrieveDedupTurns（[retrieve] 小节内单键 upsert，其余键保留）。
func (h *Handler) apiRetrieveSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project    string `json:"project"`
		DedupTurns int    `json:"dedup_turns"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	if req.DedupTurns < 0 || req.DedupTurns > 99 {
		writeErr(w, http.StatusBadRequest, "dedup_turns 必须在 0~99 之间")
		return
	}
	if err := config.SetRetrieveDedupTurns(st.ConfigPath(), req.DedupTurns); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
```

3c. `web/index.html`：泛化门控卡结束（`:146` 的 `</div>`）与规则配置卡（`:148`）之间插入：

```html
        <div class="card">
          <div class="card-head"><h3>跨轮注入冷却</h3></div>
          <p class="card-desc muted">同一会话内已注入的检索条目冷却 N 轮不再重复注入，节省注入预算；0 = 关闭（默认 3）。</p>
          <div class="card-actions">
            <span class="form-inline">
              <label for="retrieve-dedup-turns">冷却轮数</label>
              <input id="retrieve-dedup-turns" type="number" min="0" max="99" value="3">
              <button id="btn-dedup-save" type="button" class="btn">保存</button>
            </span>
          </div>
        </div>
```

3d. `web/app.js` 规则配置卡逻辑之后（`// ---------- 引导页：模型配置卡` 注释之前）插入：

```js
  // ---------- 引导页：跨轮注入冷却卡（dedup_turns） ----------

  function refreshDedup() {
    var project = captureProject();
    if (!project) return;
    api("/api/retrieve?project=" + encodeURIComponent(project)).then(function (cfg) {
      $("retrieve-dedup-turns").value = cfg.dedup_turns;
    }).catch(function (err) { showError(err.message); });
  }

  $("btn-dedup-save").addEventListener("click", function () {
    var project = captureProject();
    if (!project) {
      showError("尚无已注册项目，请先 ok init");
      return;
    }
    var n = parseInt($("retrieve-dedup-turns").value, 10);
    if (isNaN(n) || n < 0 || n > 99) {
      showError("冷却轮数必须在 0~99 之间");
      refreshDedup();
      return;
    }
    api("/api/retrieve", {
      method: "POST",
      body: { project: project, dedup_turns: n }
    }).then(refreshDedup)
      .catch(function (err) { showError(err.message); refreshDedup(); });
  });
```

3e. 挂载刷新点：`web/app.js:238` 与 `:861` 两处的 `refreshInject();` 之后各加一行 `refreshDedup();`。

- [ ] **Step 4: 跑测试确认通过 + 全仓编译**

Run: `go test ./internal/gui/ -v && go build ./...`
Expected: 全部 PASS，编译绿。

- [ ] **Step 5: 手动验证（可选但建议）**

```bash
go run ./cmd/ok gui
```

打开引导页：确认"跨轮注入冷却"卡片显示当前值（默认 3）；改为 5 保存 → 刷新后仍为 5；输入 -1/100 → 报错且不生效；项目 config.toml 里 `[retrieve]` 其余键保留。
注意既有坑：**本地调试裸 go build 不同步 dist/web**——若 GUI 实际读的是 dist/web，先把 `web/` 的两个改动文件同步过去再验证。

- [ ] **Step 6: Commit**

```bash
git add internal/gui/ web/index.html web/app.js
git commit -m "feat(gui): 引导页新增跨轮注入冷却轮数配置（/api/retrieve，两段式保存）"
```

---

### Task 7: 文档收尾 —— changelog 与 spec 措辞对齐

**Files:**
- Create: `docs/changelogs/2026-08-19-injection-cooldown.md`
- Modify: `docs/superpowers/specs/2026-08-19-injection-cooldown.md`（§6 测试用例 1 措辞具体化）

**Interfaces:**
- Consumes: 无。
- Produces: 无。

- [ ] **Step 1: 写 changelog**

新建 `docs/changelogs/2026-08-19-injection-cooldown.md`（不硬折行）：

```markdown
# 跨轮注入冷却（dedup_turns）

日期：2026-08-19

同一 session 内已注入的检索条目冷却 N 轮（默认 3）不再重复注入，避免连续同主题对话时同一条目每轮重复消耗注入预算。冷却排除在 top_n 截断之前生效——冷却条目不占名额，排名靠后的新条目得以补位；冷却中条目若被模型按历史轮指针读取，仍正常计入采纳归因。冷却轮次不记 injected 事件，反馈降权统计不受影响。借鉴 OpenViking RecallLedger，轮次语义按 prompt 轮计（门控命中轮也计），时钟自走无停摆。

配置：`[retrieve] dedup_turns = 3`（0 = 关闭，恢复每轮都注入的旧行为）；GUI 引导页新增"跨轮注入冷却"卡片可直接调整（0~99）。
```

- [ ] **Step 2: spec 测试用例措辞对齐**

`docs/superpowers/specs/2026-08-19-injection-cooldown.md` §6 测试用例 1 改为（消除"第 2~N 轮"歧义，与实现公式 `PromptTurns - T <= dedup_turns` 一致）：

```markdown
1. 同 session 同查询连续多轮：注入后接下来 N 个 prompt 轮该条目缺席（"相关知识"段
   不出现或其路径不出现），第 N+1 个后续轮恢复注入（以 dedup_turns=2 为例：
   第 1 轮注入、第 2~3 轮缺席、第 4 轮恢复）。
```

- [ ] **Step 3: 全量回归 + 提交**

Run: `go build ./... && go test ./...`
Expected: 全绿。

```bash
git add docs/changelogs/2026-08-19-injection-cooldown.md docs/superpowers/specs/2026-08-19-injection-cooldown.md
git commit -m "docs: 跨轮注入冷却 changelog 与 spec 测试措辞对齐"
```

---

## Self-Review 记录

- **Spec 覆盖**：§3.1 冷却语义/时钟自走 → Task 1+4；§3.1 不记 EventInjected → Task 4（TestCooldownNoInjectedEvent）；§3.2 排除不占名额 → Task 3；§3.3 归因窗口 → Task 5；§3.4 fail-open → Task 1（零值自愈）+Task 4（损坏状态用例）；§3.5 观测（ok.log 冷却行 + QueryInfo.CooledSkipped）→ Task 3+4；§3.6 配置与 GUI 引导页 → Task 2+6；§4 用户故事 1-14 → 各任务测试对应；§6 测试用例 1-10 全覆盖（用例 7"新 session 无冷却"由各用例的独立 sessionID 天然覆盖）。
- **默认行为变化点**：Default DedupTurns=3 使两个既有测试（TestInjectForPromptBaseAndRetrieve、TestFirstPromptInjectsBaseOnce）的"第二轮仍命中"假设失效 → Task 4 Step 1 钉 dedup_turns=0 保原意图。其余多轮测试（TestAdoptionLoop、TestInjectSemanticDegradeHintOnce、TestReinjectTurnsPeriodic 等）不依赖"同条目第二轮命中"，不受影响。
- **类型一致性**：`QueryExBranch` 新签名（branch 后加 exclude）在 Task 3 定义、Task 4 消费；`AdoptableName` 返回 string 在 Task 1 定义、Task 5 消费；`SetRetrieveDedupTurns` 在 Task 2 定义、Task 6 消费。
