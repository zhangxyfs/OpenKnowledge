# claude 生态适配器实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 agentx 第六个适配器 `claude`，合并写 `~/.claude/settings.json` 的 hooks，让 Claude Code / CodePilot 等 SDK 兼容宿主获得完整三链路（注入/追踪/自省）。

**Architecture:** 仿 `zcode.go` 的 JSON 合并写模式，目标文件换成 Claude Code 原生 hooks 结构；输出协议复用现成的 `claude` JSON 协议（`ok hook <event> claude`，`internal/hook/hook.go` 零改动）。注册表驱动 CLI/GUI/自愈自动生效。

**Tech Stack:** Go 1.25、encoding/json、`internal/agentx` 注册表。规格：`docs/superpowers/specs/2026-08-12-claude-adapter-design.md`。

## Global Constraints

- **测试隔离铁律**（v2.5.0 踩过坑）：任何遍历注册表/`Detected()` 的测试必须设 `OK_CLAUDE_HOME` + `OK_CODEPILOT_HOME` 到不存在目录，否则真实写入用户配置
- **合并写纪律**：strip 只删 ok 条目、第三方 hooks 原样保留；写前 `.bak-openknowledge` 备份；解析失败不覆盖损坏文件
- **fail-open**：hook 链路所有内部错误仅记 ok.log；未注册项目目录静默 `return 0`（`hook.go:159-162` 已有，勿动）
- **识别哲学**：ok 条目识别不看 exe basename（改名/迁移/测试二进制不影响）
- 适配器自包含一个文件——与 kimi/zcode 的 strip/has 逻辑同构重复是**既有模式**，不做抽象重构
- 版本无需 bump（2.11.0 为在研版本，iss/README 已是 2.11.0）；changelog 追加到 `docs/changelogs/2.11.0.md`
- 提交信息风格：conventional commits 中文描述（参照 git log）

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/agentx/claude.go`（新建） | claude 适配器全部逻辑（~230 行） |
| `internal/agentx/claude_test.go`（新建） | 适配器单测 |
| 13 个既有 `*_test.go`（修改） | 补 `OK_CLAUDE_HOME`/`OK_CODEPILOT_HOME` 隔离 |
| `docs/changelogs/2.11.0.md`（修改） | 新功能条目 |
| `README.md` / `README_EN.md`（修改） | agent 列表两处 |
| `site/changelog.html`（修改） | v2.11.0 区块加条目 |

---

### Task 1: claude.go 骨架 + 安装链路

**Files:**
- Create: `internal/agentx/claude.go`
- Test: `internal/agentx/claude_test.go`

**Interfaces:**
- Produces（后续任务与注册表消费）:
  - `ClaudeHome() string` — `OK_CLAUDE_HOME` > `~/.claude`
  - `claudeAgent` 实现 `agentx.Agent` 九方法，`init()` 中 `Register`
  - 命令串格式：`"<正斜杠exe>" hook <prompt|post-tool|stop> claude`（`strconv.Quote` 包裹）
- Consumes: `HookTimeoutSec()`（`internal/agentx/kimi.go:53`，读全局配置回退 10）

- [ ] **Step 1: 写失败测试**

`internal/agentx/claude_test.go`：

```go
package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateClaude 隔离 claude/codepilot 配置根与 ok 全局配置（HookTimeoutSec 读它）。
func isolateClaude(t *testing.T) string {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	home := filepath.Join(t.TempDir(), "claude")
	t.Setenv("OK_CLAUDE_HOME", home)
	return home
}

func claudeTestExe() string { return `D:\develop\OpenKnowledge\dist\ok.exe` }

func TestClaudeHomeEnvOverride(t *testing.T) {
	home := isolateClaude(t)
	if ClaudeHome() != home {
		t.Fatalf("ClaudeHome() = %q, want %q", ClaudeHome(), home)
	}
}

func TestIsOKClaudeHook(t *testing.T) {
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
		if got := isOKClaudeHook(c.hook); got != c.want {
			t.Errorf("%s: isOKClaudeHook() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClaudeInstallHooks(t *testing.T) {
	home := isolateClaude(t)
	// 预置含第三方内容的 settings.json
	sp := filepath.Join(home, "settings.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := `{"theme":"light","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	if err := os.WriteFile(sp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := claudeAgent{}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("写回后 JSON 非法: %v", err)
	}
	if cfg["theme"] != "light" {
		t.Error("既有未知字段 theme 被丢")
	}
	events, _ := cfg["hooks"].(map[string]any)
	// 第三方 PreToolUse 保留
	pre, _ := events["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Error("第三方 PreToolUse 组被删")
	}
	// 三事件各多出一个 ok 组
	wantCmd := map[string]string{
		"UserPromptSubmit": `"D:/develop/OpenKnowledge/dist/ok.exe" hook prompt claude`,
		"PostToolUse":      `"D:/develop/OpenKnowledge/dist/ok.exe" hook post-tool claude`,
		"Stop":             `"D:/develop/OpenKnowledge/dist/ok.exe" hook stop claude`,
	}
	wantMatcher := map[string]string{"UserPromptSubmit": "*", "PostToolUse": "Write|Edit", "Stop": "*"}
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
	// 备份已生成
	if _, err := os.Stat(sp + ".bak-openknowledge"); err != nil {
		t.Error("未生成 .bak-openknowledge 备份")
	}
}

func TestClaudeInstallIdempotent(t *testing.T) {
	home := isolateClaude(t)
	a := claudeAgent{}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "settings.json"))
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	groups, _ := events["UserPromptSubmit"].([]any)
	if len(groups) != 1 {
		t.Fatalf("重复安装产生堆积: UserPromptSubmit 组数 = %d, want 1", len(groups))
	}
}

func TestClaudeCorruptSettings(t *testing.T) {
	home := isolateClaude(t)
	sp := filepath.Join(home, "settings.json")
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(sp, []byte("{broken"), 0o644)
	a := claudeAgent{}
	if err := a.InstallHooks(claudeTestExe()); err == nil {
		t.Fatal("损坏文件应报错")
	}
	data, _ := os.ReadFile(sp)
	if string(data) != "{broken" {
		t.Error("损坏文件被覆盖")
	}
}

func TestClaudeDetect(t *testing.T) {
	home := isolateClaude(t)
	a := claudeAgent{}
	if a.Detect() {
		t.Error("两目录均不存在时不应 Detect")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.Detect() {
		t.Error("~/.claude 存在应 Detect")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/agentx/ -run TestClaude -v`
Expected: 编译失败 `undefined: claudeAgent` / `isOKClaudeHook` / `ClaudeHome`

- [ ] **Step 3: 写实现**

`internal/agentx/claude.go`：

```go
package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ClaudeHome 返回 Claude 生态配置根目录（OK_CLAUDE_HOME 优先——ok 自留测试隔离口，
// OK_ZCODE_HOME 同款），否则 ~/.claude。CodePilot 等 claude-agent-sdk 兼容宿主经
// settingSources:['user'] 同样加载该目录的 settings.json（hooks 字段 shadow 原样继承）。
func ClaudeHome() string {
	if h := os.Getenv("OK_CLAUDE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func claudeSettingsPath() string { return filepath.Join(ClaudeHome(), "settings.json") }

// codepilotHome 仅用于 Detect：只装 CodePilot 的机器可能还没有 ~/.claude。
// OK_CODEPILOT_HOME 为测试隔离口；CLAUDE_GUI_DATA_DIR 是 CodePilot 官方覆盖。
func codepilotHome() string {
	if h := os.Getenv("OK_CODEPILOT_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("CLAUDE_GUI_DATA_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codepilot")
}

// claudeHookEvents 是 ok 接入的 Claude Code hook 事件（对应 ok 的三条 hook 链路）。
// 命令为 shell 字符串：正斜杠 exe + 双引号（cmd.exe 与 bash 均可执行——探针实测
// cmd 接受正斜杠路径）。输出协议 Claude JSON（args 末尾 "claude"）：注入走
// hookSpecificOutput.additionalContext，Stop 阻断走 decision:block（hook.go 现成）。
var claudeHookEvents = []struct {
	event   string // Claude Code 事件名
	matcher string // 组级 matcher（UserPromptSubmit/Stop 用 "*"，实测可用形态）
	okHook  string // ok hook 子命令
}{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "*", "stop"},
}

// claudeCommand 生成 hook 命令串。
func claudeCommand(exe, okHook string) string {
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// isOKClaudeHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串以
// " hook <prompt|post-tool|stop> claude" 结尾。不看 exe basename——改名/迁移/
// 测试二进制都不影响识别。
func isOKClaudeHook(h map[string]any) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range claudeHookEvents {
		if strings.HasSuffix(cmd, " hook "+e.okHook+" claude") {
			return true
		}
	}
	return false
}

// claudeAgent Claude 生态适配器：hook 集成 = 合并写 ~/.claude/settings.json 的
// hooks 字段（Claude Code 与 CodePilot 共享此文件，装一次多宿主生效）；
// 技能目录 ~/.claude/skills（CodePilot 的 skill-discovery 同样扫描）。
type claudeAgent struct{}

func init() { Register(claudeAgent{}) }

func (claudeAgent) ID() string          { return "claude" }
func (claudeAgent) DisplayName() string { return "Claude Code（含 CodePilot 等兼容宿主）" }
func (claudeAgent) HooksTarget() string { return claudeSettingsPath() }
func (claudeAgent) SkillsDir() string   { return filepath.Join(ClaudeHome(), "skills") }

func (claudeAgent) Detect() bool {
	for _, dir := range []string{ClaudeHome(), codepilotHome()} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// claudeOKGroup 生成一个事件的 ok hook 组：type=command shell 串，
// timeout 秒级 = 全局 [hooks] timeout_sec。
func claudeOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": claudeCommand(exe, okHook),
		"timeout": HookTimeoutSec(),
	}
	return map[string]any{"matcher": matcher, "hooks": []any{hook}}
}

// loadClaudeSettings 读 settings.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadClaudeSettings() (map[string]any, error) {
	data, err := os.ReadFile(claudeSettingsPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("claude settings.json 解析失败: %w", err)
	}
	return cfg, nil
}

// claudeEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func claudeEventsOf(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	return events
}

// claudeEventsEdit 取 hooks 事件表供写入：缺失时创建（Claude Code 无 enabled 开关）。
func claudeEventsEdit(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		cfg["hooks"] = events
	}
	return events
}

// stripOKClaudeHooks 移除事件表里所有 ok 自有 hook（组内 hooks 被删空时整组移除，
// 事件数组空了删事件键），返回是否有改动。第三方条目原样保留。
func stripOKClaudeHooks(events map[string]any) bool {
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
				if hm, _ := h.(map[string]any); hm != nil && isOKClaudeHook(hm) {
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

// hasOKClaudeHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKClaudeHook(events map[string]any) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKClaudeHook(hm) {
					return true
				}
			}
		}
	}
	return false
}

// claudeHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态
// （command=exe、matcher 与 timeout 正确）。
func claudeHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range claudeHookEvents {
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
				if hm == nil || !isOKClaudeHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeout"].(float64)
				if cmd == claudeCommand(exe, e.okHook) && timeout == wantTimeout {
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

// writeClaudeSettings 备份后写回 settings.json（MarshalIndent，未知字段保留）。
func writeClaudeSettings(cfg map[string]any) error {
	path := claudeSettingsPath()
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

func (claudeAgent) InstallHooks(exe string) error {
	cfg, err := loadClaudeSettings()
	if err != nil {
		return err
	}
	events := claudeEventsEdit(cfg)
	stripOKClaudeHooks(events)
	for _, e := range claudeHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, claudeOKGroup(exe, e.matcher, e.okHook))
	}
	return writeClaudeSettings(cfg)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/agentx/ -run TestClaude -v`
Expected: 6 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/agentx/claude.go internal/agentx/claude_test.go
git commit -m "feat(agentx): claude 生态适配器——合并写 ~/.claude/settings.json hooks（安装链路）"
```

---

### Task 2: RemoveHooks / EnsureHooks / HooksInstalled

**Files:**
- Modify: `internal/agentx/claude.go`（追加三个方法）
- Test: `internal/agentx/claude_test.go`（追加测试）

**Interfaces:**
- Consumes: Task 1 的 `stripOKClaudeHooks` / `hasOKClaudeHook` / `claudeHooksCurrent` / `loadClaudeSettings` / `writeClaudeSettings`

- [ ] **Step 1: 写失败测试**（追加到 claude_test.go）

```go
func TestClaudeRemoveHooks(t *testing.T) {
	home := isolateClaude(t)
	sp := filepath.Join(home, "settings.json")
	_ = os.MkdirAll(home, 0o755)
	preset := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	_ = os.WriteFile(sp, []byte(preset), 0o644)
	a := claudeAgent{}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = (%v, %v), want (true, nil)", removed, err)
	}
	data, _ := os.ReadFile(sp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	if hasOKClaudeHook(events) {
		t.Error("ok hooks 未移除干净")
	}
	if pre, _ := events["PreToolUse"].([]any); len(pre) != 1 {
		t.Error("第三方 PreToolUse 被误删")
	}
	// 二次移除 = no-op
	removed, err = a.RemoveHooks()
	if err != nil || removed {
		t.Fatalf("二次 RemoveHooks = (%v, %v), want (false, nil)", removed, err)
	}
}

func TestClaudeEnsureHooks(t *testing.T) {
	home := isolateClaude(t)
	sp := filepath.Join(home, "settings.json")
	a := claudeAgent{}
	// 从未安装（文件不存在）→ no-op，不创建文件
	if err := a.EnsureHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sp); !os.IsNotExist(err) {
		t.Error("从未安装时 EnsureHooks 不应创建文件")
	}
	// 安装后把 exe 改旧 → EnsureHooks 重写为新 exe
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Error("自愈后 HooksInstalled 应为 true")
	}
	data, _ := os.ReadFile(sp)
	if strings.Contains(string(data), `D:\old`) || strings.Contains(string(data), `D:/old`) {
		t.Error("旧 exe 路径残留")
	}
	// 用户显式移除 → 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(sp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if hasOKClaudeHook(claudeEventsOf(cfg)) {
		t.Error("用户显式移除的集成被复活")
	}
}

func TestClaudeHooksInstalled(t *testing.T) {
	isolateClaude(t)
	a := claudeAgent{}
	if a.HooksInstalled() {
		t.Error("未安装时不应为 true")
	}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	// HooksInstalled 用 os.Executable() 比对——测试二进制路径与安装路径不同，应为 false；
	// 用安装时的同一路径判定逻辑直接测 claudeHooksCurrent。
	data, _ := os.ReadFile(claudeSettingsPath())
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if !claudeHooksCurrent(claudeEventsOf(cfg), claudeTestExe()) {
		t.Error("安装后 claudeHooksCurrent(安装 exe) 应为 true")
	}
	if claudeHooksCurrent(claudeEventsOf(cfg), `D:\other\ok.exe`) {
		t.Error("换 exe 后 claudeHooksCurrent 应为 false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/agentx/ -run "TestClaudeRemove|TestClaudeEnsure|TestClaudeHooksInstalled" -v`
Expected: 编译失败 `claudeAgent 缺少 RemoveHooks/EnsureHooks/HooksInstalled`（接口不满足）

- [ ] **Step 3: 写实现**（追加到 claude.go 末尾）

```go
func (claudeAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(claudeSettingsPath()); os.IsNotExist(err) {
		return false, nil
	}
	cfg, err := loadClaudeSettings()
	if err != nil {
		return false, err
	}
	events := claudeEventsOf(cfg)
	if events == nil || !stripOKClaudeHooks(events) {
		return false, nil
	}
	if err := writeClaudeSettings(cfg); err != nil {
		return false, fmt.Errorf("移除 claude hooks: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：settings 存在、曾安装过 ok hooks 且内容过期（exe 迁移、超时
// 变更）时重写；从未安装（无任何 ok 条目）则 no-op——用户显式移除的集成不复活。
func (claudeAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(claudeSettingsPath()); err != nil {
		return nil
	}
	cfg, err := loadClaudeSettings()
	if err != nil {
		return err
	}
	events := claudeEventsOf(cfg)
	if events == nil || !hasOKClaudeHook(events) || claudeHooksCurrent(events, exe) {
		return nil
	}
	events = claudeEventsEdit(cfg)
	stripOKClaudeHooks(events)
	for _, e := range claudeHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, claudeOKGroup(exe, e.matcher, e.okHook))
	}
	return writeClaudeSettings(cfg)
}

func (claudeAgent) HooksInstalled() bool {
	cfg, err := loadClaudeSettings()
	if err != nil {
		return false
	}
	events := claudeEventsOf(cfg)
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
	return claudeHooksCurrent(events, exe)
}
```

- [ ] **Step 4: 跑全部 adapter 测试 + vet**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/agentx/ -v -run TestClaude && go vet ./internal/agentx/`
Expected: 全 PASS，vet 无输出

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/agentx/claude.go internal/agentx/claude_test.go
git commit -m "feat(agentx): claude 适配器补全——移除/自愈/安装检测三方法"
```

---

### Task 3: 全注册表测试隔离补齐

**Files:**
- Modify: 所有含 `OK_OPENCODE_HOME` 的 `*_test.go`（执行时以 grep 结果为准，当前已知 13 个：
  `cmd/ok/daemon_test.go`、`cmd/ok/integration_test.go`、`internal/agentx/zcode_test.go`、`internal/agentx/reasonix_test.go`、`internal/cli/setup_test.go`、`internal/cli/propose_test.go`、`internal/cli/cli_test.go`、`internal/daemon/server_test.go`、`internal/gui/api_test.go`、`internal/hook/hook_test.go`、`internal/rxext/serve_test.go`、`internal/setupx/setupx_test.go`、`internal/setupx/uninstall_test.go`）

**Interfaces:**
- Consumes: Task 1 的 `OK_CLAUDE_HOME` / `OK_CODEPILOT_HOME` 环境变量语义

- [ ] **Step 1: 定位全部隔离点**

Run: `cd D:/develop/OpenKnowledge && grep -rn "OK_OPENCODE_HOME" --include="*_test.go" .`
Expected: 列出全部需要补隔离的行（每处形如 `t.Setenv("OK_OPENCODE_HOME", ...)`）

- [ ] **Step 2: 每处追加两行**（紧跟 OK_OPENCODE_HOME 行之后，缩进对齐）

```go
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
```

注意：若某文件 import 块没有 `path/filepath` 需补上（参照该文件已有 Setenv 行是否用 filepath.Join 判断——全部已知文件都用了）。

- [ ] **Step 3: 验证补齐无遗漏**

Run: `cd D:/develop/OpenKnowledge && for f in $(grep -rl "OK_OPENCODE_HOME" --include="*_test.go" .); do grep -L "OK_CLAUDE_HOME" $f; done`
Expected: 无输出（每个含 OK_OPENCODE_HOME 的测试文件都已有 OK_CLAUDE_HOME）

- [ ] **Step 4: 全仓测试**

Run: `cd D:/develop/OpenKnowledge && go test ./... 2>&1 | tail -25`
Expected: 全部 `ok`（含 cmd/ok E2E），无 FAIL；**特别确认没有在真实 `~/.claude/` 留下任何文件**——`git status` 干净、真实 settings.json 无 hooks 字段

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add -A "*_test.go"
git commit -m "test(agentx): 全注册表遍历测试补 OK_CLAUDE_HOME/OK_CODEPILOT_HOME 隔离"
```

---

### Task 4: 真实环境手动验证（需用户配合）

**Files:** 无代码改动

- [ ] **Step 1: 构建并安装**

```bash
cd D:/develop/OpenKnowledge
go build -o dist/ok.exe ./cmd/ok
./dist/ok.exe setup --agent claude
```
Expected: 输出 claude 安装成功；`cat ~/.claude/settings.json` 含三事件 ok 组，`command` 指向 dist/ok.exe，`env`/`theme` 等既有字段原样保留；生成 `.bak-openknowledge`

- [ ] **Step 2: CLI 冒烟（不经 GUI 直接验证协议）**

```bash
cd D:/develop/OpenKnowledge
echo '{"hook_event_name":"UserPromptSubmit","session_id":"smoke","cwd":"D:/develop/OpenKnowledge","prompt":"wiki 机制"}' | ./dist/ok.exe hook prompt claude
```
Expected: 输出一行以 `{` 开头的 JSON，含 `"hookSpecificOutput"` 与 `"additionalContext"`（OpenKnowledge 是已注册项目，应检索到 wiki 相关条目）

- [ ] **Step 3: 未注册项目 fail-open 验证**

```bash
echo '{"hook_event_name":"UserPromptSubmit","session_id":"smoke","cwd":"D:/","prompt":"test"}' | ./dist/ok.exe hook prompt claude; echo "exit=$?"
```
Expected: 无任何输出，`exit=0`

- [ ] **Step 4: CodePilot 实测（用户操作）**

请用户在 CodePilot 中打开 `D:\develop\OpenKnowledge`，发一条与本项目知识相关的问题（如"这个项目的经验沉淀机制是怎样的"）。预期：回答体现出注入的知识库内容（引用 `~/.openknowledge/...` 下的条目）；回合结束时 stop hook 静默通过。

- [ ] **Step 5: 汇报验证结果后提交（如有修正确认无代码变更则跳过）**

```bash
cd D:/develop/OpenKnowledge && git status
```
Expected: 干净（验证不产生代码变更）

---

### Task 5: 文档（CHANGELOG + README×2 + 官网）

**Files:**
- Modify: `docs/changelogs/2.11.0.md`
- Modify: `README.md:43`、`README.md:94-95`
- Modify: `README_EN.md:44`、`README_EN.md:95-96`
- Modify: `site/changelog.html`（v2.11.0 区块，`#v2-11-0` 附近）

- [ ] **Step 1: `docs/changelogs/2.11.0.md` 的「## 新功能」追加**（仿 opencode 条目风格）

```markdown
- 新增 claude 生态适配器（第六个 AI 助手集成）：合并写 `~/.claude/settings.json`
  的 hooks（备份 + 幂等 + 第三方条目保留），Claude Code 与 CodePilot 等
  claude-agent-sdk 兼容宿主共享此文件，装一次多宿主生效；CodePilot 实测
  UserPromptSubmit/Stop 均原生执行（agent 内核 settingSources 含 user 层，
  shadow HOME 只剥 ANTHROPIC_* env 键）；技能目录 `~/.claude/skills`
```

- [ ] **Step 2: README.md 两处**

`:43` 行尾 `opencode 走 TypeScript 插件 hooks` 后追加 `，claude 走 ~/.claude/settings.json 合并写（Claude Code / CodePilot 等兼容宿主共用）`，并把行首名单改为 `Kimi Code、Pi、ZCode、Reasonix、opencode、Claude Code/CodePilot`。

`:94` 的 `opencode 是 \`~/.config/opencode/plugins/\` 的 TypeScript 插件` 后追加 `，claude 是 \`~/.claude/settings.json\` 的 hooks 合并写`；`:95` 的 `zcode 为 \`~/.zcode/skills\`` 后追加 `，claude 为 \`~/.claude/skills\``。

- [ ] **Step 3: README_EN.md 两处**（对应 :44 / :95-96，同义英文）

`:44` 名单改 `Kimi Code, Pi, ZCode, Reasonix, opencode and Claude Code/CodePilot`，行尾追加 `, claude via a merged hooks write to ~/.claude/settings.json (shared by Claude Code, CodePilot and other compatible hosts)`；`:95` 同义追加；`:96` 追加 `; claude uses \`~/.claude/skills\``。

- [ ] **Step 4: site/changelog.html 的 `#v2-11-0` 区块**

仿该区块既有 `<li>` 样式加一条中文版+对应 `data-i18n` 英文条目（参照区块内 opencode 条目的双语写法）。

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add docs/changelogs/2.11.0.md README.md README_EN.md site/changelog.html
git commit -m "docs: claude 适配器——changelog 2.11.0 + 中英 README + 官网更新日志"
```

---

### Task 6: ok propose 沉淀 MCP 设计结论

**Files:** 无代码改动（知识库草稿）

- [ ] **Step 1: 调用 openknowledge-propose 技能**，沉淀一条 reference 草稿，要点如下（技能负责落库格式）：

标题：`MCP server 实现路径（未来 hook-less agent 预案）`
类型：reference；tags 建议 `mcp, agentx, born:master`
正文要点：
- **触发背景**：CodePilot 实测原生支持 Claude Code 用户级 hooks（2026-08-12 探针验证：UserPromptSubmit/Stop 均触发），claude 适配器已覆盖——MCP 方案**不排期**，仅作未来真 hook-less agent 的预案
- **实现路径**：`ok mcp` 子命令走 stdio；官方 SDK `github.com/modelcontextprotocol/go-sdk`（泛型 `mcp.AddTool` + `mcp.StdioTransport`）；handler 直接复用 `internal/retrieve`（混合检索）与 `internal/store`/`entry`，不经 daemon 网络层
- **hook→MCP 降级映射**：InjectForPrompt 自动注入 → agent 主动调 search 工具（需 CLAUDE.md/AGENTS.md 指令引导，弱保证）；TrackTouched 无等效物（MCP 感知不到文件操作）；CheckStop 无 stop 时机（退化为指令软约定）
- **结论**：hook=宿主推送（零心智、强制），MCP=模型拉取（指令引导、弱保证）；检索/写入可迁移，自动注入与回合末自省不可等效

- [ ] **Step 2: 确认草稿落库**

Run: `cd D:/develop/OpenKnowledge && ./dist/ok.exe propose --help 2>&1 | head -5`（或按技能指引查看草稿列表）
Expected: 草稿条目存在，待人批准（draft 二分，不进检索）

---

## Self-Review 记录

- **规格覆盖**：§1→Task 1-2（注册表驱动零接线）；§2→Task 1（格式/识别/命令串）；§3→Task 1-2（九方法、Detect 双目录、SkillsDir）；§4→Task 1-3（隔离口+补齐+单测）；§5→Task 6；§6→Task 5（版本无需 bump 已核实：2.11.0 在研）
- **占位符**：无
- **类型一致**：`claudeHooksCurrent`/`stripOKClaudeHooks` 等签名在 Task 1/2 间一致；测试均经 `isolateClaude` 设置三环境变量
