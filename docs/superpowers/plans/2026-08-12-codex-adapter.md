# Codex 适配器实施计划（agentx 第七席）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 codex 适配器——合并写 `~/.codex/hooks.json` 三事件 + apply_patch 补丁头解析，知识注入/写盘追踪/Stop 阻断与 Claude 完全同档。

**Architecture:** 单文件适配器 `internal/agentx/codex.go` 实现 `Agent` 九方法接口并 `init()` 注册（镜像 `claude.go`），hook 输出协议复用 `"claude"` 零改动；唯一新逻辑在 hook 包输入侧——`Event.PatchPaths()` 解析 Codex `apply_patch` 的 `tool_input.command` 补丁头，`HandlePostTool` 改为多路径记录。

**Tech Stack:** Go（stdlib only：encoding/json、os、path/filepath、strconv、strings）、go test、Inno Setup（版本事实源）、bash（sync-version.sh）。

**Spec:** `docs/superpowers/specs/2026-08-12-codex-adapter-design.md`（已获用户确认）

## Global Constraints

- 三事件固定：`UserPromptSubmit(matcher "*")→prompt`、`PostToolUse(matcher "apply_patch")→post-tool`、`Stop(matcher "*")→stop`；不接 SessionStart、不追 Bash。
- hook 命令为 shell 字符串：`strconv.Quote(filepath.ToSlash(exe)) + " hook <okHook> claude"`；`timeout` 秒级 = `HookTimeoutSec()`（`internal/agentx/kimi.go:53`）。
- ok 条目识别：命令串 TrimSpace 后以 ` hook <prompt|post-tool|stop> claude` 结尾，不看 exe basename。
- 合并写铁律：写前 `.bak-openknowledge` 备份；JSON 解析失败报错**不覆盖**；第三方条目原样保留；map 合并写 key 重排可接受；全部路径 fail-open。
- `CodexHome()` 优先级：`OK_CODEX_HOME`（测试隔离口）> `CODEX_HOME`（官方）> `~/.codex`（`os.UserHomeDir()` 跟随重定向）。
- 自愈语义：曾装过且过期才重写，从未安装不复活（用户显式移除不复活）。
- 不做 GUI 信任提示卡、不动 hook 输出层 / `cmd/ok/main.go` / daemon / setupx / web。
- 版本：minor bump → `2.12.0`，`installer/openknowledge.iss` 单一事实源；bump 后必须跑 `bash scripts/sync-version.sh`（README + site 徽标漂移是已沉淀坑）。
- 测试铁律：凡遍历注册表的测试必须补 `OK_CODEX_HOME` 隔离，否则真实写入用户 `~/.codex/hooks.json`（已沉淀坑）。
- Go 版本：仓库 go.mod 的 go 指令 ≥ 1.21，`strings.CutPrefix`（1.20+）可用。
- 测试命令统一在仓库根 `D:\develop\OpenKnowledge` 下执行。

---

### Task 1: `Event.PatchPaths()` 补丁头解析

**Files:**
- Modify: `internal/hook/hook.go`（在 `FilePath()` 之后插入新方法，约 :78 后）
- Test: `internal/hook/hook_test.go`

**Interfaces:**
- Consumes: 现有 `Event.ToolInput json.RawMessage`（`hook.go:24`）
- Produces: `func (e *Event) PatchPaths() []string` —— 从 `tool_input.command` 提取 `*** Add File: ` / `*** Update File: ` / `*** Delete File: ` / `*** Move to: ` 头标记后的相对路径；command 兼容字符串与数组两种 JSON 形态；非补丁输入返回 nil。Task 2 的 `HandlePostTool` 依赖此签名。

- [ ] **Step 1: 写失败测试**

在 `internal/hook/hook_test.go` 追加（import 需补 `encoding/json`、`reflect`）：

```go
func TestEventPatchPaths(t *testing.T) {
	patchJSON := `"*** Begin Patch\n*** Add File: internal/foo.go\n*** Update File: docs/bar.md\n@@\n+added line\n*** Delete File: old.txt\n*** Update File: moved.go\n*** Move to: newdir/moved.go\n*** End Patch\n"`
	want := []string{"internal/foo.go", "docs/bar.md", "old.txt", "moved.go", "newdir/moved.go"}
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"字符串 command", `{"command":` + patchJSON + `}`, want},
		{"数组 command", `{"command":["apply_patch",` + patchJSON + `]}`, want},
		{"非补丁输入", `{"command":"ls -la"}`, nil},
		{"无 command 字段", `{"path":"D:/x/y.go"}`, nil},
		{"空 tool_input", ``, nil},
	}
	for _, c := range cases {
		e := &Event{ToolInput: json.RawMessage(c.input)}
		if got := e.PatchPaths(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: PatchPaths() = %v, want %v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hook/ -run TestEventPatchPaths -v`
Expected: 编译失败 `e.PatchPaths undefined`

- [ ] **Step 3: 实现 PatchPaths**

在 `internal/hook/hook.go` 的 `FilePath()` 方法（:66-78）之后插入：

```go
// PatchPaths 从 Codex apply_patch 的 tool_input.command 提取补丁触碰的相对路径：
// 按行扫描 *** Add File: / *** Update File: / *** Delete File: / *** Move to:
// 头标记（move 语义下 Update 与 Move to 两个路径都算触碰）。command 兼容字符串
// 与数组两种 JSON 形态；非补丁输入返回 nil。路径相对会话 cwd。
func (e *Event) PatchPaths() []string {
	if len(e.ToolInput) == 0 {
		return nil
	}
	var ti struct {
		Command json.RawMessage `json:"command"`
	}
	if err := json.Unmarshal(e.ToolInput, &ti); err != nil || len(ti.Command) == 0 {
		return nil
	}
	var text string
	if err := json.Unmarshal(ti.Command, &text); err != nil {
		var parts []string
		if err := json.Unmarshal(ti.Command, &parts); err != nil {
			return nil
		}
		text = strings.Join(parts, "\n")
	}
	var paths []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if p, ok := strings.CutPrefix(line, prefix); ok {
				if p = strings.TrimSpace(p); p != "" {
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/hook/ -run TestEventPatchPaths -v`
Expected: PASS（5 个子断言全过）

- [ ] **Step 5: 提交**

```bash
git add internal/hook/hook.go internal/hook/hook_test.go
git commit -m "feat(hook): Event.PatchPaths 解析 Codex apply_patch 补丁头——Add/Update/Delete/Move to 四标记、字符串与数组 command 双形态"
```

---

### Task 2: `HandlePostTool` 多路径记录

**Files:**
- Modify: `internal/hook/hook.go:175-191`（`HandlePostTool`）
- Test: `internal/hook/hook_test.go`

**Interfaces:**
- Consumes: Task 1 的 `func (e *Event) PatchPaths() []string`；现有 `func (e *Event) FilePath() string`、`func TrackTouched(pc *project.Context, sessionID, toolName, filePath string)`（`core.go:156`，签名不变）；测试复用 `setupProject(t) (projDir, kbRoot string)`（`hook_test.go:68-86`）与 `state.Load(filepath.Join(kbRoot, "state"), sessionID)`。
- Produces: `HandlePostTool` 行为契约——`FilePath()` 与 `PatchPaths()` 合并去重，相对路径先 `filepath.Join(ev.Cwd, p)` 再 `TrackTouched`。Task 9 探针依赖此行为。

- [ ] **Step 1: 写失败测试**

在 `internal/hook/hook_test.go` 追加：

```go
func TestHandlePostToolApplyPatch(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"sp1","cwd":%q,"tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Add File: internal/a.go\n*** Update File: README.md\n*** End Patch\n"}}`, projDir)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("post-tool exit %d", code)
	}
	st := state.Load(filepath.Join(kbRoot, "state"), "sp1")
	// 补丁头路径相对 cwd：join 后 relativize 得项目相对路径（NormalizePath 规范化，
	// 大小写行为随平台，故 EqualFold 断言）
	for _, want := range []string{"internal/a.go", "readme.md"} {
		found := false
		for _, p := range st.Touched {
			if strings.EqualFold(p, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("touched 缺 %q: %+v", want, st.Touched)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hook/ -run TestHandlePostToolApplyPatch -v`
Expected: FAIL——`touched 缺 "internal/a.go"`（现行实现 `FilePath()` 从补丁输入取不到路径，`TrackTouched` 全部 skip）

- [ ] **Step 3: 改写 HandlePostTool**

将 `internal/hook/hook.go:175-191` 的 `HandlePostTool` 整体替换为：

```go
// HandlePostTool 解析 hook 事件并记录触碰文件；核心逻辑见 TrackTouched。
// 路径来源：FilePath()（path/file_path——kimi/claude/zcode 写工具）+
// PatchPaths()（Codex apply_patch 补丁头）。补丁头路径相对会话 cwd，
// 先 join 再 relativize，否则项目前缀匹配不上全部 skip。
func HandlePostTool(r io.Reader) int {
	if registry.HooksDisabled() {
		return 0
	}
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("post-tool parse: %v", err)
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		logErr("post-tool project (cwd=%q): %v", ev.Cwd, err)
		return 0
	}
	paths := make([]string, 0, 4)
	if fp := ev.FilePath(); fp != "" {
		paths = append(paths, fp)
	}
	paths = append(paths, ev.PatchPaths()...)
	seen := map[string]bool{}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(ev.Cwd, p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		TrackTouched(pc, ev.SessionID, ev.ToolName, p)
	}
	return 0
}
```

- [ ] **Step 4: 跑测试确认通过（新测试 + 既有 post-tool 测试不回归）**

Run: `go test ./internal/hook/ -run 'TestHandlePostToolApplyPatch|TestPostTool|TestStop|TestEnforce' -v`
Expected: 全 PASS（既有测试的绝对路径 `path` 输入经 `filepath.IsAbs` 直通，行为不变）

Run: `go test ./internal/hook/`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/hook/hook.go internal/hook/hook_test.go
git commit -m "feat(hook): HandlePostTool 多路径记录——FilePath 与 PatchPaths 合并去重，补丁相对路径先 join cwd 再 relativize"
```

---

### Task 3: codex 适配器骨架（CodexHome / Detect / 注册 / 识别函数）

**Files:**
- Create: `internal/agentx/codex.go`
- Test: `internal/agentx/codex_test.go`

**Interfaces:**
- Consumes: `Register(a Agent)` / `Find(id string) Agent`（`internal/agentx/agentx.go:26-50`）；`SkillsHome()`（`agentx.go:52-59`）；`HookTimeoutSec()`（`kimi.go:53`）。
- Produces: `func CodexHome() string`；`func codexHooksPath() string`；`var codexHookEvents`（三事件表）；`func codexCommand(exe, okHook string) string`；`func isOKCodexHook(h map[string]any) bool`；`type codexAgent struct{}`（已注册，Task 4 填充合并写方法体）。Task 4 与 Task 5 依赖这些名称。

- [ ] **Step 1: 写失败测试**

创建 `internal/agentx/codex_test.go`：

```go
package agentx

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateCodex 隔离 codex 配置根与 ok 全局配置（HookTimeoutSec 读它）；
// CODEX_HOME 指向不存在目录防真实环境变量泄漏（OK_CODEX_HOME 优先于它）。
func isolateCodex(t *testing.T) string {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex-official"))
	home := filepath.Join(t.TempDir(), "codex")
	t.Setenv("OK_CODEX_HOME", home)
	return home
}

func codexTestExe() string { return `D:\develop\OpenKnowledge\dist\ok.exe` }

func TestCodexHomeEnvOverride(t *testing.T) {
	home := isolateCodex(t)
	if CodexHome() != home {
		t.Fatalf("CodexHome() = %q, want %q（OK_CODEX_HOME 应最优先）", CodexHome(), home)
	}
}

func TestCodexHomeOfficialEnv(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("OK_CODEX_HOME", "") // 置空落到下一级
	official := filepath.Join(t.TempDir(), "codex-official")
	t.Setenv("CODEX_HOME", official)
	if CodexHome() != official {
		t.Fatalf("CodexHome() = %q, want %q（CODEX_HOME 次之）", CodexHome(), official)
	}
}

func TestIsOKCodexHook(t *testing.T) {
	cases := []struct {
		name string
		hook map[string]any
		want bool
	}{
		{"prompt", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude`}, true},
		{"post-tool", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook post-tool claude`}, true},
		{"stop", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook stop claude`}, true},
		{"尾部空白容忍", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude  `}, true},
		{"非 command 类型", map[string]any{"type": "process", "command": `"D:/x/ok.exe" hook prompt claude`}, false},
		{"非 ok 命令", map[string]any{"type": "command", "command": "echo hi"}, false},
		{"相邻词误匹配", map[string]any{"type": "command", "command": "myhook prompt claude"}, false},
		{"缺 claude 协议段", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt`}, false},
	}
	for _, c := range cases {
		if got := isOKCodexHook(c.hook); got != c.want {
			t.Errorf("%s: isOKCodexHook() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCodexDetect(t *testing.T) {
	home := isolateCodex(t)
	a := codexAgent{}
	if a.Detect() {
		t.Error("目录不存在时不应 Detect")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.Detect() {
		t.Error("~/.codex 存在应 Detect")
	}
}

func TestCodexRegistered(t *testing.T) {
	isolateCodex(t)
	if Find("codex") == nil {
		t.Fatal("codexAgent 未注册（init/Register 缺失）")
	}
	if Find("codex").ID() != "codex" {
		t.Errorf("ID() = %q, want codex", Find("codex").ID())
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run 'TestCodex' -v`
Expected: 编译失败（`CodexHome`、`codexAgent`、`isOKCodexHook` 未定义）

- [ ] **Step 3: 创建 codex.go 骨架**

创建 `internal/agentx/codex.go`：

```go
package agentx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CodexHome 返回 Codex 配置根目录：OK_CODEX_HOME（ok 自留测试隔离口，
// OK_CLAUDE_HOME 同款命名）> CODEX_HOME（Codex CLI 官方重定位环境变量）>
// ~/.codex。os.UserHomeDir() 跟随环境重定向，与各适配器 XxxHome 一致——
// hook 子进程在 shadow HOME 下自愈最坏只写 shadow 副本，真实配置无风险。
func CodexHome() string {
	if h := os.Getenv("OK_CODEX_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func codexHooksPath() string { return filepath.Join(CodexHome(), "hooks.json") }

// codexHookEvents 是 ok 接入的 Codex hook 事件（对应 ok 的三条 hook 链路）。
// 命令形态与 claude 适配器相同：shell 字符串（正斜杠 exe + 双引号），输出协议
// Claude JSON（args 末尾 "claude"）——Codex hook 契约逐字兼容 Claude Code
// （hookSpecificOutput.additionalContext 注入、Stop decision:block 阻断），
// hook.go 输出层零改动。PostToolUse 只追 apply_patch（Codex 专用写盘工具，
// 无 Write/Edit），不追 Bash——与 claude 不追 Bash 对齐。
var codexHookEvents = []struct {
	event   string // Codex 事件名（逐字沿用 Claude Code 命名）
	matcher string // 组级 matcher
	okHook  string // ok hook 子命令
}{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "apply_patch", "post-tool"},
	{"Stop", "*", "stop"},
}

// codexCommand 生成 hook 命令串（claudeCommand 同款形态）。
func codexCommand(exe, okHook string) string {
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// isOKCodexHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串以
// " hook <prompt|post-tool|stop> claude" 结尾。不看 exe basename——改名/迁移/
// 测试二进制都不影响识别。
func isOKCodexHook(h map[string]any) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range codexHookEvents {
		if strings.HasSuffix(cmd, " hook "+e.okHook+" claude") {
			return true
		}
	}
	return false
}

// codexAgent Codex 适配器：hook 集成 = 合并写用户层 ~/.codex/hooks.json
// （官方建议每层一种机制，不动 config.toml）；技能目录共享 SkillsHome——
// Codex 原生扫描 USER 作用域 ~/.agents/skills（opencode 同款零适配）。
type codexAgent struct{}

func init() { Register(codexAgent{}) }

func (codexAgent) ID() string          { return "codex" }
func (codexAgent) DisplayName() string { return "Codex" }
func (codexAgent) HooksTarget() string { return codexHooksPath() }
func (codexAgent) SkillsDir() string   { return SkillsHome() }

func (codexAgent) Detect() bool {
	info, err := os.Stat(CodexHome())
	return err == nil && info.IsDir()
}

// 以下四个方法为 Task 3 行走骨架（walking skeleton），Task 4 填充真实合并写实现。
func (codexAgent) InstallHooks(exe string) error     { return nil }
func (codexAgent) RemoveHooks() (bool, error)        { return false, nil }
func (codexAgent) EnsureHooks(exe string) error      { return nil }
func (codexAgent) HooksInstalled() bool              { return false }
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentx/ -run 'TestCodex' -v`
Expected: 5 个测试全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentx/codex.go internal/agentx/codex_test.go
git commit -m "feat(agentx): codex 适配器骨架——CodexHome 三级优先级（OK_CODEX_HOME>CODEX_HOME>~/.codex）、Detect、注册与 ok 条目后缀识别"
```

---

### Task 4: codex 合并写机制 + 完整适配器测试

**Files:**
- Modify: `internal/agentx/codex.go`（追加 JSON 机制函数，替换四个骨架方法体）
- Test: `internal/agentx/codex_test.go`

**Interfaces:**
- Consumes: Task 3 全部符号 + `currentExe(t *testing.T) string`（`zcode_test.go:21`，同包可直接用）。
- Produces: 完整 `codexAgent`——`InstallHooks(exe string) error` / `RemoveHooks() (bool, error)` / `EnsureHooks(exe string) error` / `HooksInstalled() bool`；helper：`loadCodexHooks()` / `writeCodexHooks(cfg)` / `codexEventsOf(cfg)` / `codexEventsEdit(cfg)` / `stripOKCodexHooks(events) bool` / `hasOKCodexHook(events) bool` / `codexHooksCurrent(events, exe) bool` / `codexOKGroup(exe, matcher, okHook) map[string]any`。注册表消费方（CLI/GUI/自愈/setupx）经 `Agent` 接口使用，零接线。

- [ ] **Step 1: 写失败测试**

在 `internal/agentx/codex_test.go` 追加（import 补 `encoding/json`、`strings`）：

```go
func TestCodexInstallHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := `{"note":"keep","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	if err := os.WriteFile(hp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	data, err := os.ReadFile(hp)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("写回后 JSON 非法: %v", err)
	}
	if cfg["note"] != "keep" {
		t.Error("既有未知字段 note 被丢")
	}
	events, _ := cfg["hooks"].(map[string]any)
	pre, _ := events["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Error("第三方 PreToolUse 组被删")
	}
	wantCmd := map[string]string{
		"UserPromptSubmit": `"D:/develop/OpenKnowledge/dist/ok.exe" hook prompt claude`,
		"PostToolUse":      `"D:/develop/OpenKnowledge/dist/ok.exe" hook post-tool claude`,
		"Stop":             `"D:/develop/OpenKnowledge/dist/ok.exe" hook stop claude`,
	}
	wantMatcher := map[string]string{"UserPromptSubmit": "*", "PostToolUse": "apply_patch", "Stop": "*"}
	wantTimeout := float64(HookTimeoutSec())
	for ev, cmd := range wantCmd {
		groups, _ := events[ev].([]any)
		found := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if m, _ := gm["matcher"].(string); m != wantMatcher[ev] {
				continue
			}
			for _, h := range gm["hooks"].([]any) {
				hm, _ := h.(map[string]any)
				if c, _ := hm["command"].(string); c == cmd {
					if to, _ := hm["timeout"].(float64); to != wantTimeout {
						t.Errorf("%s: timeout = %v, want %v", ev, to, wantTimeout)
					}
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s: 未找到期望的 ok hook 组", ev)
		}
	}
	if _, err := os.Stat(hp + ".bak-openknowledge"); err != nil {
		t.Error("未生成 .bak-openknowledge 备份")
	}
}

func TestCodexInstallIdempotent(t *testing.T) {
	home := isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "hooks.json"))
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	for _, ev := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		groups, _ := events[ev].([]any)
		if len(groups) != 1 {
			t.Fatalf("重复安装产生堆积: %s 组数 = %d, want 1", ev, len(groups))
		}
	}
}

func TestCodexCorruptHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(hp, []byte("{broken"), 0o644)
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err == nil {
		t.Fatal("损坏文件应报错")
	}
	data, _ := os.ReadFile(hp)
	if string(data) != "{broken" {
		t.Error("损坏文件被覆盖")
	}
}

func TestCodexRemoveHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	_ = os.MkdirAll(home, 0o755)
	preset := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	_ = os.WriteFile(hp, []byte(preset), 0o644)
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = (%v, %v), want (true, nil)", removed, err)
	}
	data, _ := os.ReadFile(hp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	if hasOKCodexHook(events) {
		t.Error("ok hooks 未移除干净")
	}
	if pre, _ := events["PreToolUse"].([]any); len(pre) != 1 {
		t.Error("第三方 PreToolUse 被误删")
	}
	removed, err = a.RemoveHooks()
	if err != nil || removed {
		t.Fatalf("二次 RemoveHooks = (%v, %v), want (false, nil)", removed, err)
	}
}

func TestCodexEnsureHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	a := codexAgent{}
	// 从未安装（文件不存在）→ no-op，不创建文件
	if err := a.EnsureHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hp); !os.IsNotExist(err) {
		t.Error("从未安装时 EnsureHooks 不应创建文件")
	}
	// 安装后把 exe 改旧 → EnsureHooks 重写为新 exe。
	// 注意：HooksInstalled 以 os.Executable() 为判定基准（见 TestCodexHooksInstalled），
	// 故此处用 currentExe(t) 作为"新 exe"（zcode_test.go 同款模式）。
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Error("自愈后 HooksInstalled 应为 true")
	}
	data, _ := os.ReadFile(hp)
	if strings.Contains(string(data), `D:\old`) || strings.Contains(string(data), `D:/old`) {
		t.Error("旧 exe 路径残留")
	}
	// 用户显式移除 → 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(hp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if hasOKCodexHook(codexEventsOf(cfg)) {
		t.Error("用户显式移除的集成被复活")
	}
}

func TestCodexHooksInstalled(t *testing.T) {
	isolateCodex(t)
	a := codexAgent{}
	if a.HooksInstalled() {
		t.Error("未安装时不应为 true")
	}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	// HooksInstalled 用 os.Executable() 比对——测试二进制路径与安装路径不同，应为 false；
	// 用安装时的同一路径判定逻辑直接测 codexHooksCurrent。
	data, _ := os.ReadFile(codexHooksPath())
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if !codexHooksCurrent(codexEventsOf(cfg), codexTestExe()) {
		t.Error("安装后 codexHooksCurrent(安装 exe) 应为 true")
	}
	if codexHooksCurrent(codexEventsOf(cfg), `D:\other\ok.exe`) {
		t.Error("换 exe 后 codexHooksCurrent 应为 false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run 'TestCodexInstall|TestCodexCorrupt|TestCodexRemove|TestCodexEnsure|TestCodexHooksInstalled' -v`
Expected: FAIL——安装/自愈断言不成立（骨架方法什么都不做）

- [ ] **Step 3: 实现合并写机制**

在 `internal/agentx/codex.go` 中**删除** Task 3 的四个骨架方法体，追加以下完整实现（import 补 `encoding/json`、`fmt`）：

```go
// codexOKGroup 生成一个事件的 ok hook 组：type=command shell 串，
// timeout 秒级 = 全局 HookTimeoutSec()。
func codexOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": codexCommand(exe, okHook),
		"timeout": HookTimeoutSec(),
	}
	return map[string]any{"matcher": matcher, "hooks": []any{hook}}
}

// loadCodexHooks 读 hooks.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadCodexHooks() (map[string]any, error) {
	data, err := os.ReadFile(codexHooksPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("codex hooks.json 解析失败: %w", err)
	}
	return cfg, nil
}

// codexEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func codexEventsOf(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	return events
}

// codexEventsEdit 取 hooks 事件表供写入：缺失时创建。
func codexEventsEdit(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		cfg["hooks"] = events
	}
	return events
}

// stripOKCodexHooks 移除事件表里所有 ok 自有 hook（组内 hooks 被删空时整组移除，
// 事件数组空了删事件键），返回是否有改动。第三方条目原样保留。
func stripOKCodexHooks(events map[string]any) bool {
	changed := false
	for name, v := range events {
		groups, _ := v.([]any)
		if groups == nil {
			continue
		}
		kept := groups[:0]
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			if gm == nil || hooks == nil {
				kept = append(kept, g)
				continue
			}
			var keptHooks []any
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKCodexHook(hm) {
					changed = true
					continue
				}
				keptHooks = append(keptHooks, h)
			}
			if len(keptHooks) == 0 {
				changed = true // 整组都是 ok 的，连组移除
				continue
			}
			gm["hooks"] = keptHooks
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(events, name)
		} else {
			events[name] = kept
		}
	}
	return changed
}

// hasOKCodexHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKCodexHook(events map[string]any) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKCodexHook(hm) {
					return true
				}
			}
		}
	}
	return false
}

// codexHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态
// （command=exe、matcher 与 timeout 正确）。
func codexHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		found := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if gm == nil {
				continue
			}
			matcher, _ := gm["matcher"].(string)
			if matcher != e.matcher {
				continue
			}
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				hm, _ := h.(map[string]any)
				if hm == nil || !isOKCodexHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeout"].(float64)
				if cmd == codexCommand(exe, e.okHook) && timeout == wantTimeout {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// writeCodexHooks 备份后写回 hooks.json（MarshalIndent，未知字段保留）。
func writeCodexHooks(cfg map[string]any) error {
	path := codexHooksPath()
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak-openknowledge", data, 0o644)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (codexAgent) InstallHooks(exe string) error {
	cfg, err := loadCodexHooks()
	if err != nil {
		return err
	}
	events := codexEventsEdit(cfg)
	stripOKCodexHooks(events)
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, codexOKGroup(exe, e.matcher, e.okHook))
	}
	return writeCodexHooks(cfg)
}

func (codexAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(codexHooksPath()); os.IsNotExist(err) {
		return false, nil
	}
	cfg, err := loadCodexHooks()
	if err != nil {
		return false, err
	}
	events := codexEventsOf(cfg)
	if events == nil || !stripOKCodexHooks(events) {
		return false, nil
	}
	if err := writeCodexHooks(cfg); err != nil {
		return false, fmt.Errorf("移除 codex hooks: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：hooks.json 存在、曾安装过 ok hooks 且内容过期（exe 迁移、
// 超时变更）时重写；从未安装（无任何 ok 条目）则 no-op——用户显式移除的集成
// 不复活。
func (codexAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(codexHooksPath()); err != nil {
		return nil
	}
	cfg, err := loadCodexHooks()
	if err != nil {
		return err
	}
	events := codexEventsOf(cfg)
	if events == nil || !hasOKCodexHook(events) || codexHooksCurrent(events, exe) {
		return nil
	}
	events = codexEventsEdit(cfg)
	stripOKCodexHooks(events)
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, codexOKGroup(exe, e.matcher, e.okHook))
	}
	return writeCodexHooks(cfg)
}

func (codexAgent) HooksInstalled() bool {
	cfg, err := loadCodexHooks()
	if err != nil {
		return false
	}
	events := codexEventsOf(cfg)
	if events == nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return codexHooksCurrent(events, exe)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentx/ -run 'TestCodex' -v`
Expected: Task 3 + Task 4 共 11 个 codex 测试全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentx/codex.go internal/agentx/codex_test.go
git commit -m "feat(agentx): codex 合并写机制——hooks.json 三事件安装/移除/自愈/判定，备份+第三方保留+损坏不覆盖（claude 同款纪律）"
```

---

### Task 5: 12 个测试文件补 OK_CODEX_HOME 隔离

**Files:**
- Modify: `cmd/ok/daemon_test.go:52`、`cmd/ok/integration_test.go:42`
- Modify: `internal/agentx/opencode_test.go:53-54`
- Modify: `internal/cli/setup_test.go`（:21-22、:47-48、:68-69、:96-97、:118-119、:143-144、:167-168 共 7 处）
- Modify: `internal/cli/cli_test.go`（:41-42、:135-136、:167-168、:211-212、:286-287、:334-335、:405-406 共 7 处）
- Modify: `internal/cli/propose_test.go:26-27`
- Modify: `internal/daemon/server_test.go:29-30`
- Modify: `internal/gui/api_test.go:35-36`
- Modify: `internal/hook/hook_test.go`（TestMain，os.Setenv 块）
- Modify: `internal/rxext/serve_test.go:23`
- Modify: `internal/setupx/setupx_test.go:15-16`、`:37-38`；`internal/setupx/uninstall_test.go:27-28`

**Interfaces:**
- Consumes: Task 3 的 `CodexHome()` 优先级（`OK_CODEX_HOME` 最优先，置它即屏蔽真实 `CODEX_HOME` 与 `~/.codex`）。
- Produces: 全仓注册表遍历测试对 codex 免疫——不补则 Task 4 落地后跑全量测试会真实写入用户 `~/.codex/hooks.json`。

- [ ] **Step 1: `t.Setenv` 形态的 9 个文件批量补行**

规则：在每个出现 `t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))` 的行**之后**追加一行：

```go
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
```

涉及文件与处数：`internal/agentx/opencode_test.go`（1 处）、`internal/cli/setup_test.go`（7 处）、`internal/cli/cli_test.go`（7 处）、`internal/cli/propose_test.go`（1 处）、`internal/daemon/server_test.go`（1 处）、`internal/gui/api_test.go`（1 处）、`internal/setupx/setupx_test.go`（2 处）、`internal/setupx/uninstall_test.go`（1 处）。

- [ ] **Step 2: `cmd/ok` 两个 env 列表文件**

`cmd/ok/daemon_test.go:52` 与 `cmd/ok/integration_test.go:42` 的 `cmd.Env = append(os.Environ(), ...)` 列表中，在 `"OK_CODEPILOT_HOME="+filepath.Join(home, "codepilot-nonexistent")` 之后插入：

```go
"OK_CODEX_HOME="+filepath.Join(home, "codex-nonexistent"),
```

（保持与相邻元素相同的逗号分隔形态；行尾注释里的 agent 名单补 codex。）

- [ ] **Step 3: `internal/hook/hook_test.go` TestMain**

在 codepilotDir 块（:57-62）之后、`os.Exit(m.Run())` 之前插入：

```go
	codexDir, err := os.MkdirTemp("", "hook-test-codex-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("OK_CODEX_HOME", codexDir)
```

- [ ] **Step 4: `internal/rxext/serve_test.go` env 名单**

:23 的切片 `[]string{"OK_REASONIX_HOME", "KIMI_CODE_HOME", "PI_CODING_AGENT_DIR", "OK_ZCODE_HOME", "OK_OPENCODE_HOME", "OK_CLAUDE_HOME", "OK_CODEPILOT_HOME"}` 末尾追加 `"OK_CODEX_HOME"`。

- [ ] **Step 5: 跑全量测试**

Run: `go test ./...`
Expected: 全部 PASS，且真实 `~/.codex/` 未被创建/修改（可 `ls ~/.codex 2>/dev/null` 或资源管理器确认——若本机装有 Codex 且已有该目录，确认 `hooks.json` 内容未被测试 exe 改写）

- [ ] **Step 6: 提交**

```bash
git add cmd/ok/daemon_test.go cmd/ok/integration_test.go internal/agentx/opencode_test.go internal/cli/setup_test.go internal/cli/cli_test.go internal/cli/propose_test.go internal/daemon/server_test.go internal/gui/api_test.go internal/hook/hook_test.go internal/rxext/serve_test.go internal/setupx/setupx_test.go internal/setupx/uninstall_test.go
git commit -m "test(agentx): 全仓注册表遍历测试补 OK_CODEX_HOME 隔离——12 文件防真实写入 ~/.codex（eb98f8c 同款清单）"
```

---

### Task 6: 文档（ARCHITECTURE + README 中英 + changelog 2.12.0）

**Files:**
- Modify: `docs/ARCHITECTURE.md`（§9.2 四处）
- Modify: `README.md:43`、`:95-96`
- Modify: `README_EN.md:44`、`:95-96`
- Create: `docs/changelogs/2.12.0.md`

**Interfaces:**
- Consumes: Task 3-4 落地的事实（事件表、CodexHome 优先级、PatchPaths、信任门）。
- Produces: 与代码一致的公开文档；2.12.0 changelog 供 Task 8 版本 bump 引用。

- [ ] **Step 1: ARCHITECTURE.md §9.2**

① 接口注释 SkillsDir 行（约 :515）：
- 旧：`    SkillsDir() string            // 技能目录（kimi/pi/reasonix/opencode 共享 SkillsHome；zcode 为 ~/.zcode/skills；claude 为 ~/.claude/skills）`
- 新：`    SkillsDir() string            // 技能目录（kimi/pi/reasonix/opencode/codex 共享 SkillsHome；zcode 为 ~/.zcode/skills；claude 为 ~/.claude/skills）`

② 注册表段落（约 :520）：
- 旧：`（`setupx.SkillDirs()`，kimi/pi/reasonix/opencode 共享 `SkillsHome()`（`OK_SKILLS_HOME` 优先，默认 `~/.agents/skills`），`
- 新：`（`setupx.SkillDirs()`，kimi/pi/reasonix/opencode/codex 共享 `SkillsHome()`（`OK_SKILLS_HOME` 优先，默认 `~/.agents/skills`），`

③ `六种注入形态：` → `七种注入形态：`

④ 注入形态表末尾（claude 行之后）追加：

```markdown
| codex | 合并写 JSON 配置（hooks.json 三事件组，`type:"command"` shell 串） | `~/.codex/hooks.json`（`OK_CODEX_HOME` 优先，ok 自留测试口；`CODEX_HOME` 次之） | 三事件的 ok hook 均为当前 exe + `claude` 参数 + 当前 timeout（秒） |
```

⑤ claude 适配器段落之后插入新段落：

```markdown
codex 适配器（`codex.go`）：Codex 的 hook 契约逐字兼容 Claude Code（官方文档核实）——stdin JSON 同字段、注入走 `hookSpecificOutput.additionalContext`、Stop 阻断 `decision:block`，故 hook 命令继续以 `claude` 为输出协议参数，`hook.go` 输出层零改动；唯一新逻辑在输入侧——Codex 写盘走 `apply_patch`（`tool_input.command` 载补丁文本，无 Write/Edit），`Event.PatchPaths()` 解析 `*** Add File:` / `*** Update File:` / `*** Delete File:` / `*** Move to:` 头标记，`HandlePostTool` 合并 `FilePath()` 与 `PatchPaths()` 多路径记录（补丁路径相对会话 cwd，先 join 再 relativize），auto 自省与 enforce 规则与 Claude 同档。配置目标为用户层独立 `~/.codex/hooks.json`（官方建议每层一种机制，不动 config.toml；合并写纪律、后缀识别、备份与自愈语义均同 claude）。Codex 有**信任门**：非受管 hooks 按内容哈希记账，安装后首次运行提示用户审查信任（`/hooks` 管理），exe 迁移自愈重写后哈希变化会再询问一次。PostToolUse matcher 只追 `apply_patch` 不追 `Bash`（与 claude 不追 Bash 对齐）。技能零适配：Codex 原生扫描 USER 作用域 `~/.agents/skills`（共享 `SkillsHome()`）。`CodexHome()` 优先级：`OK_CODEX_HOME` > `CODEX_HOME`（官方重定位）> `~/.codex`。
```

- [ ] **Step 2: README.md**

① :43 多 Agent 行：
- 旧：`Kimi Code、Pi、ZCode、Reasonix、opencode、Claude Code/CodePilot 共用同一套知识库（可扩展适配器架构）——kimi 走 TOML hooks 标记块，pi 走 TypeScript 扩展，zcode 走 Claude JSON 协议，reasonix 走 Extension Protocol sidecar，opencode 走 TypeScript 插件 hooks，claude 走 ~/.claude/settings.json 合并写（Claude Code / CodePilot 等兼容宿主共用）`
- 新：`Kimi Code、Pi、ZCode、Reasonix、opencode、Claude Code/CodePilot、Codex 共用同一套知识库（可扩展适配器架构）——kimi 走 TOML hooks 标记块，pi 走 TypeScript 扩展，zcode 走 Claude JSON 协议，reasonix 走 Extension Protocol sidecar，opencode 走 TypeScript 插件 hooks，claude 走 ~/.claude/settings.json 合并写（Claude Code / CodePilot 等兼容宿主共用），codex 走 ~/.codex/hooks.json 合并写（hook 契约兼容 Claude，技能零适配共享 ~/.agents/skills）`

② :95 setup 描述：
- 旧尾：`……claude 是 `~/.claude/settings.json` 的 hooks 合并写；`ok setup --agent <id>` 可只装指定 agent`
- 新尾：`……claude 是 `~/.claude/settings.json` 的 hooks 合并写，codex 是 `~/.codex/hooks.json` 的 hooks 合并写；`ok setup --agent <id>` 可只装指定 agent`

③ :96 技能目录：
- 旧：`（kimi/pi/opencode 共享 `~/.agents/skills/`，zcode 为 `~/.zcode/skills`，claude 为 `~/.claude/skills`）`
- 新：`（kimi/pi/opencode/codex 共享 `~/.agents/skills/`，zcode 为 `~/.zcode/skills`，claude 为 `~/.claude/skills`）`

- [ ] **Step 3: README_EN.md**

① :44 多 Agent 行：
- 旧：`Kimi Code, Pi, ZCode, Reasonix, opencode and Claude Code/CodePilot share the same knowledge base (extensible adapter architecture) — kimi via TOML hook marker blocks, pi via a TypeScript extension, zcode via the Claude JSON protocol, reasonix via an Extension Protocol sidecar, opencode via a TypeScript plugin, claude via a merged hooks write to ~/.claude/settings.json (shared by Claude Code, CodePilot and other compatible hosts)`
- 新：`Kimi Code, Pi, ZCode, Reasonix, opencode, Claude Code/CodePilot and Codex share the same knowledge base (extensible adapter architecture) — kimi via TOML hook marker blocks, pi via a TypeScript extension, zcode via the Claude JSON protocol, reasonix via an Extension Protocol sidecar, opencode via a TypeScript plugin, claude via a merged hooks write to ~/.claude/settings.json (shared by Claude Code, CodePilot and other compatible hosts), codex via a merged hooks write to ~/.codex/hooks.json (Claude-compatible hook contract; zero skill adaptation via the shared ~/.agents/skills)`

② :95 setup 描述：
- 旧尾：`……; claude gets a merged hooks write to `~/.claude/settings.json`. Use `ok setup --agent <id>` to target one agent only`
- 新尾：`……; claude gets a merged hooks write to `~/.claude/settings.json`; codex gets a merged hooks write to `~/.codex/hooks.json`. Use `ok setup --agent <id>` to target one agent only`

③ :96 技能目录：
- 旧：`(kimi/pi/opencode share `~/.agents/skills/`; zcode uses `~/.zcode/skills`; claude uses `~/.claude/skills`)`
- 新：`(kimi/pi/opencode/codex share `~/.agents/skills/`; zcode uses `~/.zcode/skills`; claude uses `~/.claude/skills`)`

- [ ] **Step 4: 创建 docs/changelogs/2.12.0.md**

```markdown
# 2.12.0

## 新功能
- 新增 codex 适配器（第七个 AI 助手集成）：合并写 `~/.codex/hooks.json`
  （备份 + 幂等 + 第三方条目保留），hook 契约逐字兼容 Claude Code——注入
  `hookSpecificOutput.additionalContext`、Stop `decision:block`，输出协议零改动
  复用；写盘追踪解析 apply_patch 补丁头（`*** Add File:` / `*** Update File:` /
  `*** Delete File:` / `*** Move to:`），auto 自省与 enforce 规则与 Claude 同档；
  技能零适配——Codex 原生扫描共享 `~/.agents/skills`。注意 Codex 信任门：安装后
  首次运行会提示审查信任 hooks（`/hooks` 管理），exe 迁移自愈重写后会再次询问
```

- [ ] **Step 5: 提交**

```bash
git add docs/ARCHITECTURE.md README.md README_EN.md docs/changelogs/2.12.0.md
git commit -m "docs: codex 适配器——ARCHITECTURE §9.2 第七种注入形态 + 中英 README 多 Agent 行与 setup 描述 + changelog 2.12.0"
```

---

### Task 7: 官网（index.html + docs.html + changelog.html + site.js）

**Files:**
- Modify: `site/index.html:155`
- Modify: `site/docs.html:151`
- Modify: `site/changelog.html`（导航组 + 版本区块 + latest 徽标迁移）
- Modify: `site/assets/site.js`（:35 `'i.f4p'`、:86 `'d.s1l1'`、新增 `'c.2120.n1'`）

**Interfaces:**
- Consumes: Task 6 的 changelog 文案。
- Produces: 官网与 2.12.0 一致；`下载最新版 vX` 按钮文案由 Task 8 的 sync-version.sh 统一刷新（本任务不动）。

- [ ] **Step 1: site/index.html 功能卡**

- 旧：`<p data-i18n="i.f4p">Kimi Code、Pi、ZCode、Reasonix、opencode、Claude Code/CodePilot 共用同一套知识库，可扩展适配器架构。</p>`
- 新：`<p data-i18n="i.f4p">Kimi Code、Pi、ZCode、Reasonix、opencode、Claude Code/CodePilot、Codex 共用同一套知识库，可扩展适配器架构。</p>`

- [ ] **Step 2: site/assets/site.js 英文字典 i.f4p**

- 旧：`'i.f4p': 'Kimi Code, Pi, ZCode, Reasonix, opencode and Claude Code/CodePilot share the same knowledge base through an extensible adapter architecture.',`
- 新：`'i.f4p': 'Kimi Code, Pi, ZCode, Reasonix, opencode, Claude Code/CodePilot and Codex share the same knowledge base through an extensible adapter architecture.',`

- [ ] **Step 3: site/docs.html setup 枚举（:151，data-i18n="d.s1l1"）**

- 旧尾：`……claude 合并写 <code>~/.claude/settings.json</code> 的 hooks（Claude Code / CodePilot 等兼容宿主共用）</li>`
- 新尾：`……claude 合并写 <code>~/.claude/settings.json</code> 的 hooks（Claude Code / CodePilot 等兼容宿主共用），codex 合并写 <code>~/.codex/hooks.json</code>（hook 契约兼容 Claude；技能零适配共享技能目录；安装后首次运行需在 Codex 中确认信任 hooks）</li>`

- [ ] **Step 4: site/assets/site.js 英文字典 d.s1l1**

- 旧尾：`……, claude gets a merged hooks write to <code>~/.claude/settings.json</code> (shared by Claude Code, CodePilot and other compatible hosts)',`
- 新尾：`……, claude gets a merged hooks write to <code>~/.claude/settings.json</code> (shared by Claude Code, CodePilot and other compatible hosts), codex gets a merged hooks write to <code>~/.codex/hooks.json</code> (Claude-compatible hook contract; zero skill adaptation via the shared skills directory; approve the hook trust prompt on first Codex run after install)',`

- [ ] **Step 5: site/changelog.html 导航组**

在 `<div class="group-title">2.11.x</div>` 所在 group **之前**插入新组，并把 latest-dot 从 v2.11.1 移走：

```html
    <div class="group">
      <div class="group-title">2.12.x</div>
      <a href="#v2-12-0"><span><span class="latest-dot"></span>v2.12.0</span><span class="d">08-12</span></a>
    </div>
```

同时把 v2.11.1 锚点里的 `<span class="latest-dot"></span>` 删掉：
- 旧：`<a href="#v2-11-1"><span><span class="latest-dot"></span>v2.11.1</span><span class="d">08-12</span></a>`
- 新：`<a href="#v2-11-1"><span>v2.11.1</span><span class="d">08-12</span></a>`

- [ ] **Step 6: site/changelog.html 版本区块**

在 `<section class="ver" id="v2-11-1">` **之前**插入：

```html
    <section class="ver" id="v2-12-0">
      <div class="ver-head"><h2>v2.12.0</h2><span class="badge-latest" data-i18n="c.latest">最新</span><time>2026-08-12</time></div>
      <div class="chg">
        <span class="tag tag-new" data-i18n="t.new">新功能</span>
        <ul>
          <li data-i18n="c.2120.n1">新增 codex 适配器（第七个 AI 助手集成）：合并写 <code>~/.codex/hooks.json</code>（备份 + 幂等 + 第三方条目保留），hook 契约逐字兼容 Claude Code（注入 <code>hookSpecificOutput.additionalContext</code>、Stop <code>decision:block</code>），输出协议零改动复用；写盘追踪解析 apply_patch 补丁头（<code>*** Add File:</code> 等四标记），auto 自省与 enforce 规则与 Claude 同档；技能零适配——Codex 原生扫描共享 <code>~/.agents/skills</code>；注意 Codex 信任门：安装后首次运行会提示信任 hooks（<code>/hooks</code> 管理）</li>
        </ul>
      </div>
    </section>
```

并把 v2-11-1 区块头的 latest 徽标删掉：
- 旧：`<div class="ver-head"><h2>v2.11.1</h2><span class="badge-latest" data-i18n="c.latest">最新</span><time>2026-08-12</time></div>`
- 新：`<div class="ver-head"><h2>v2.11.1</h2><time>2026-08-12</time></div>`

- [ ] **Step 7: site/assets/site.js 英文字典 c.2120.n1**

在 `'c.2111.i1'` 行之后插入：

```js
    'c.2120.n1': 'New codex adapter (seventh AI assistant integration): a merged hooks write to <code>~/.codex/hooks.json</code> (backup + idempotent + third-party entries preserved); the hook contract is byte-compatible with Claude Code (injection via <code>hookSpecificOutput.additionalContext</code>, Stop via <code>decision:block</code>), so the output protocol is reused unchanged; write-tracking parses apply_patch patch headers (<code>*** Add File:</code> and friends), keeping auto-capture and enforce rules at full Claude parity; zero skill adaptation — Codex natively scans the shared <code>~/.agents/skills</code>; note the Codex trust gate: first run after install prompts to trust the hooks (managed via <code>/hooks</code>)',
```

- [ ] **Step 8: 浏览器肉眼核对（可选但推荐）**

打开 `site/changelog.html` 确认 v2.12.0 置顶且"最新"徽标唯一；切英文确认 `c.2120.n1` 生效。

- [ ] **Step 9: 提交**

```bash
git add site/index.html site/docs.html site/changelog.html site/assets/site.js
git commit -m "docs(site): 官网补第七个 agent——index 功能卡、docs setup 枚举、changelog 2.12.0 区块与英文字典"
```

---

### Task 8: 版本 bump 2.12.0 + 徽标同步 + 全量验证

**Files:**
- Modify: `installer/openknowledge.iss:5`
- Modify（脚本自动）: `README.md`、`README_EN.md`、`site/index.html`、`site/changelog.html`、`site/assets/site.js` 的版本引用

**Interfaces:**
- Consumes: Task 6-7 的 2.12.0 文案；`scripts/sync-version.sh`（从 iss 提取 AppVersion 重写 README/site 徽标与直链，幂等）。
- Produces: 仓内版本引用全部一致 = 2.12.0。

- [ ] **Step 1: bump iss 单一事实源**

`installer/openknowledge.iss:5`：
- 旧：`#define AppVersion "2.11.1"`
- 新：`#define AppVersion "2.12.0"`

- [ ] **Step 2: 跑同步脚本**

Run: `bash scripts/sync-version.sh`
Expected 输出形态：
```
README.md: version 徽标 2.11.1 → 2.12.0
README_EN.md: version 徽标 2.11.1 → 2.12.0
site/index.html: 版本引用已更新 → 2.12.0
site/changelog.html: 版本引用已更新 → 2.12.0
site/assets/site.js: 版本引用已更新 → 2.12.0
```

- [ ] **Step 3: 验证无残留旧版本引用**

Run: `grep -rn "2\.11\.1" README.md README_EN.md site/index.html site/changelog.html site/assets/site.js installer/openknowledge.iss | grep -v "v2-11-1\|v2.11.1</h2>\|#v2-11-1\|>v2.11.1<\|c.2111\|2.11.x"`
Expected: 无输出（保留项只允许是 changelog 历史区块与导航对 2.11.1 的合法引用）

- [ ] **Step 4: 全量构建与测试**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 构建无错、vet 无警、测试全 PASS

- [ ] **Step 5: 提交**

```bash
git add installer/openknowledge.iss README.md README_EN.md site/index.html site/changelog.html site/assets/site.js
git commit -m "chore(release): 版本 bump 2.12.0（codex 适配器归入）+ sync-version 徽标同步"
```

---

### Task 9: 真机探针（手动，用户配合）

**Files:**
- 无代码改动；探针结论回写 `internal/agentx/codex.go` 注释（若有偏差）

**Interfaces:**
- Consumes: Task 8 构建的 `dist/ok.exe`（`go build -o dist/ok.exe ./cmd/ok`）。
- Produces: 三项探针结论（spec §2 待验证项）：① UserPromptSubmit/Stop 组级 matcher 形态；② `apply_patch` 精确 tool 名；③ Windows shell 执行形态。

- [ ] **Step 1: 前置检查**

Run: `codex --version`
Expected: 有版本输出；若无则先安装 Codex CLI 再继续。

- [ ] **Step 2: 备份现有 hooks.json（若存在）**

```bash
[ -f ~/.codex/hooks.json ] && cp ~/.codex/hooks.json ~/.codex/hooks.json.pre-ok-probe
```

- [ ] **Step 3: 安装 codex hooks**

Run: `./dist/ok.exe setup --agent codex`（在已 `ok init` 的项目目录外也可；安装是用户级）
Expected: 输出含 codex 安装成功；`cat ~/.codex/hooks.json` 可见三事件组（command 指向 dist/ok.exe、PostToolUse matcher `apply_patch`、timeout 数字）

- [ ] **Step 4: 信任门 + matcher 形态验证**

在**已注册进知识库的项目目录**里启动 `codex`。Expected: 首次运行提示审查/信任新 hooks（确认信任）。发一条消息，然后：

Run: `tail -5 ~/.openknowledge/ok.log`
Expected: 无 `prompt parse` 错误；且该消息上下文中出现 `[OpenKnowledge]` 注入（Codex 回复或 transcript 可见）。
若 hook **完全未触发**（ok.log 无任何新行、无注入）→ matcher `"*"` 形态不被 Codex 接受：改 `codexHookEvents` 中 UserPromptSubmit/Stop 的组级 matcher 为省略形态（组内仅 `hooks` 键），重装重试，并把结论回写 `codex.go` 注释与 spec §2。

- [ ] **Step 5: apply_patch 写盘追踪验证**

让 Codex 修改该项目一个代码文件（它会走 apply_patch）。
Run: `cat ~/.openknowledge/projects/<项目名>/state/*.json | grep -i touched`（或经 GUI 管理页查看会话状态）
Expected: touched 含被改文件的项目相对路径。若无 → 核实 Codex hooks 上报的精确 tool 名（可能非 `apply_patch`），按需修正 matcher。

- [ ] **Step 6: Stop 阻断验证（enforce 项目）**

在配置了 `changelog_required` enforce 规则的项目里，让 Codex 改 `.go` 文件后结束回合。
Expected: Codex 被阻断并收到"请补变更日志"类提示（`decision:block` 语义：reason 作为新提示继续跑）。

- [ ] **Step 7: 清理与结论沉淀**

探针完成：`./dist/ok.exe` 的集成可保留（正式安装将由 2.12.0 安装包接管，exe 路径变化时自愈会重写）；如有探针期备份且正式安装已验证，删 `~/.codex/hooks.json.pre-ok-probe`。三项探针结论中任何与 spec 不符的发现 → 改代码/注释 + 走 openknowledge-propose 沉淀新坑。

---

## Self-Review 记录

**Spec 覆盖**：spec §1 集成形态→Task 3；§2 配置目标与格式→Task 3-4（探针项→Task 9）；§3 CodexHome→Task 3；§4 补丁解析→Task 1-2；§5 接口九方法→Task 3-4；§6 合并写纪律→Task 4；§7 信任门→Task 6-7 文档 + Task 9 验证；§8 测试→Task 1-5、9；§9 版本与文档→Task 6-8；范围之外各项→Global Constraints 显式约束。✅

**Placeholder 扫描**：所有代码步骤含完整代码；Task 9 为手动验证任务（步骤含命令与预期），非占位。✅

**类型一致性**：`PatchPaths() []string`（Task 1 产、Task 2 用）；`codexHookEvents`/`codexCommand`/`isOKCodexHook`（Task 3 产、Task 4 用）；`codexEventsOf`/`hasOKCodexHook`/`codexHooksCurrent`/`codexHooksPath`（Task 4 产，其测试用）；`currentExe(t)` 复用 `zcode_test.go:21` 同包既有符号。✅
