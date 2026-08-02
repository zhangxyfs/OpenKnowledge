# 多 Agent 支持（kimiCode + pi）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 抽象出 `internal/agentx` Agent 适配器注册表，在零行为变化迁移 kimi 支持的基础上新增 pi 支持（TS 扩展注入 hook），CLI 加 `--agent`，GUI 引导页加 agents 下拉并联动。

**Architecture:** 每个 agent 一个适配器实现统一 `Agent` 接口并注册；pi 侧通过 `//go:embed` 的 TS 扩展模板写入 `~/.pi/agent/extensions/openknowledge.ts`，扩展把 pi 事件翻译成 kimi 形态的 stdin JSON 调 `ok.exe hook *`，`internal/hook` 三个 handler 完全复用。

**Tech Stack:** Go（现有 CLI/daemon/GUI），pi TypeScript 扩展 API（`@earendil-works/pi-coding-agent` 0.83.0）。

**Spec:** `docs/superpowers/specs/2026-08-01-multi-agent-support-design.md`

## Global Constraints

- 两条铁律：fail-open（hook 相关失败只记 `ok.log`，exit 0）；写入收敛（仅 setup/uninstall 路径写 agent 配置文件）。
- kimi 侧行为逐字不变：标记块 `# >>> openknowledge hooks >>>`、备份 `.bak-openknowledge`、`UpsertHooksBlock`/`EnsureHooksBlock` 逻辑原样迁移。
- 技能继续装共享目录 `~/.agents/skills`（`OK_SKILLS_HOME` 优先），`SkillsDir()` 接口预留。
- **测试隔离**：任何触发 hook 安装/检测/自愈的测试必须 `t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())`（本机真实 `~/.pi/agent` 存在，误触会改写用户真实文件），同理沿用 `KIMI_CODE_HOME` / `OK_SKILLS_HOME` / `OK_HOME` 隔离惯例。
- pi 的 Stop 阻断语义：`ok hook stop` 以 **exit 2 + stderr 文本** 表达阻断（不是 stdout），pi 扩展据此 `sendMessage` 注入。（对 spec 3.4 表格的修正：读 stderr。）
- 全部测试命令在项目根 `D:/develop/OpenKnowledge` 执行；Go 测试：`go test ./...`。

---

### Task 1: agentx 包骨架 + kimi 适配器迁移（零行为变化）

**Files:**
- Create: `internal/agentx/agentx.go`
- Create: `internal/agentx/kimi.go`
- Create: `internal/agentx/kimi_test.go`（自 `internal/setupx/setupx_test.go` 迁入 hook 相关测试）
- Modify: `internal/setupx/setupx.go`（删除迁出的 hook 逻辑与 `KimiHome`/`SkillsHome`，新增 `SkillNames()`）
- Modify: `internal/setupx/uninstall.go`（hook 移除改为遍历注册表）
- Modify: `internal/setupx/setupx_test.go`（删除迁出的 hook 测试）
- Modify: `internal/cli/setup.go:56-69`（writeHooks 改走注册表）
- Modify: `internal/hook/hook.go:96-109`（selfHealHooks 遍历注册表）
- Modify: `internal/gui/api.go:264-303, 777-805`（改用 agentx）
- Modify: `internal/gui/api_test.go`（import 与引用更新）
- Modify: `internal/setupx/uninstall_test.go`（引用更新，编译器驱动）

**Interfaces:**
- Consumes: 无（首个任务）。
- Produces（后续任务依赖）:
  - `agentx.Agent` 接口：`ID() string`、`DisplayName() string`、`Detect() bool`、`HooksInstalled() bool`、`InstallHooks(exe string) error`、`RemoveHooks() (bool, error)`、`EnsureHooks(exe string) error`、`HooksTarget() string`、`SkillsDir() string`
  - `agentx.Register(a Agent)`、`agentx.All() []Agent`、`agentx.Find(id string) (Agent, bool)`、`agentx.Detected() []Agent`
  - `agentx.SkillsHome() string`、`agentx.KimiHome() string`、`agentx.MarkerBegin/MarkerEnd`、`agentx.HooksBlockFor/UpsertHooksBlock/EnsureHooksBlock/StripLegacyOKHooks`
  - `setupx.SkillNames() []string`
  - `cli.writeHooks(targets []agentx.Agent, exe string, stdout, stderr io.Writer) int`（targets 为 nil 时取 `agentx.Detected()`）

- [ ] **Step 1: 创建 `internal/agentx/agentx.go`（接口 + 注册表 + 共享路径）**

```go
// Package agentx 抽象 AI 编码 agent 的 hook 集成：每个 agent 一个适配器，
// 注册表统一驱动 CLI / GUI / hook 自愈；新增 agent = 实现 Agent 并 Register。
package agentx

import (
	"os"
	"path/filepath"
)

// Agent 一个 AI 编码 agent 的集成适配器。
type Agent interface {
	ID() string                   // 稳定标识："kimi" / "pi"，CLI/GUI/API 统一使用
	DisplayName() string          // 展示名："Kimi Code" / "Pi"
	Detect() bool                 // 本机是否已安装该 agent
	HooksInstalled() bool         // hooks 集成是否已安装且为当前版本
	InstallHooks(exe string) error
	RemoveHooks() (bool, error)   // 返回是否真的移除了内容
	EnsureHooks(exe string) error // hook 入口自愈；错误由调用方 fail-open 处理
	HooksTarget() string          // hook 写入目标的展示路径
	SkillsDir() string            // 技能目录（当前均返回共享 SkillsHome）
}

var agents []Agent

// Register 登记 agent（在各适配器文件的 init 中调用）。
func Register(a Agent) { agents = append(agents, a) }

// All 返回全部已注册 agent（注册顺序）。
func All() []Agent { return append([]Agent(nil), agents...) }

// Find 按 id 查找 agent。
func Find(id string) (Agent, bool) {
	for _, a := range agents {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}

// Detected 返回本机已安装的 agent。
func Detected() []Agent {
	var out []Agent
	for _, a := range agents {
		if a.Detect() {
			out = append(out, a)
		}
	}
	return out
}

// SkillsHome 返回共享技能安装目录（OK_SKILLS_HOME 优先）。
func SkillsHome() string {
	if h := os.Getenv("OK_SKILLS_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents", "skills")
}
```

- [ ] **Step 2: 创建 `internal/agentx/kimi.go`（从 setupx.go 迁入 hook 逻辑 + 适配器）**

以下内容**逐字迁自** `internal/setupx/setupx.go:22-173`（常量、`KimiHome`、`HooksBlockFor`、`okHookCommand`、`StripLegacyOKHooks`、`UpsertHooksBlock`、`EnsureHooksBlock`），仅包名改为 `agentx`，并新增适配器：

```go
package agentx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const MarkerBegin = "# >>> openknowledge hooks >>>"
const MarkerEnd = "# <<< openknowledge hooks <<<"

// KimiHome 返回 kimi-code 配置目录（KIMI_CODE_HOME 优先）。
func KimiHome() string {
	if h := os.Getenv("KIMI_CODE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code")
}

func kimiConfigPath() string { return filepath.Join(KimiHome(), "config.toml") }

// HooksBlockFor 生成指向 exe 的 hooks 配置块。
func HooksBlockFor(exe string) string {
	exe = filepath.ToSlash(exe)
	return fmt.Sprintf(`[[hooks]]
event = "UserPromptSubmit"
command = "%s hook prompt"
timeout = 10

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "%s hook post-tool"
timeout = 5

[[hooks]]
event = "Stop"
command = "%s hook stop"
timeout = 5
`, exe, exe, exe)
}

// okHookCommand 匹配指向 ok hook 的 command 行（如 "ok hook prompt"、"D:/x/ok.exe hook stop"）。
var okHookCommand = regexp.MustCompile(`(?i)^\s*command\s*=\s*"[^"]*\bok(?:\.exe)?\s+hook\s`)

// StripLegacyOKHooks ……（逐字迁自 setupx.go:67-114，含原注释）
// UpsertHooksBlock ……（逐字迁自 setupx.go:116-158，含原注释）
// EnsureHooksBlock ……（逐字迁自 setupx.go:160-173，含原注释）

// kimiAgent kimiCode 适配器。
type kimiAgent struct{}

func init() { Register(kimiAgent{}) }

func (kimiAgent) ID() string          { return "kimi" }
func (kimiAgent) DisplayName() string { return "Kimi Code" }
func (kimiAgent) SkillsDir() string   { return SkillsHome() }
func (kimiAgent) HooksTarget() string { return kimiConfigPath() }

func (kimiAgent) Detect() bool {
	info, err := os.Stat(KimiHome())
	return err == nil && info.IsDir()
}

func (kimiAgent) HooksInstalled() bool {
	data, err := os.ReadFile(kimiConfigPath())
	return err == nil && strings.Contains(string(data), MarkerBegin)
}

func (kimiAgent) InstallHooks(exe string) error {
	cfgPath := kimiConfigPath()
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = os.WriteFile(cfgPath+".bak-openknowledge", data, 0o644)
	}
	return UpsertHooksBlock(cfgPath, HooksBlockFor(exe))
}

func (kimiAgent) RemoveHooks() (bool, error) {
	cfgPath := kimiConfigPath()
	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	content := string(data)
	orig := content
	i := strings.Index(content, MarkerBegin)
	j := strings.Index(content, MarkerEnd)
	if i >= 0 && j > i {
		tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
		head := strings.TrimRight(content[:i], "\n")
		content = head + "\n" + tail
	}
	content = StripLegacyOKHooks(content)
	if content == orig {
		return false, nil
	}
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("移除 hooks 配置: %w", err)
	}
	return true, nil
}

func (kimiAgent) EnsureHooks(exe string) error { return EnsureHooksBlock(kimiConfigPath(), exe) }
```

> 注：Step 2 代码块中以省略形式标注的三个函数（`StripLegacyOKHooks`、`UpsertHooksBlock`、`EnsureHooksBlock`）必须**连注释逐字搬运**，不得重写。

- [ ] **Step 3: 创建 `internal/agentx/kimi_test.go`**

从 `internal/setupx/setupx_test.go` 把 hook 相关测试（`TestUpsertHooksBlock*`、`TestStripLegacyOKHooks*`、`TestEnsureHooksBlock*` 等全部引用 `UpsertHooksBlock/HooksBlockFor/MarkerBegin` 的用例）**逐字迁移**，包名改 `package agentx`。再追加适配器测试：

```go
func TestKimiAgentInstallDetectRemove(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	a, ok := Find("kimi")
	if !ok {
		t.Fatal("kimi agent not registered")
	}
	if !a.Detect() {
		t.Fatal("Detect should be true when KIMI_CODE_HOME dir exists")
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false before install")
	}
	if err := a.InstallHooks(`D:\x\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("HooksInstalled should be true after install")
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false after remove")
	}
}

func TestKimiAgentDetectFalse(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (kimiAgent{}).Detect() {
		t.Fatal("Detect should be false when dir missing")
	}
}
```

- [ ] **Step 4: 修改 `internal/setupx/setupx.go`**

删除：`MarkerBegin`/`MarkerEnd` 常量、`KimiHome()`、`SkillsHome()`、`HooksBlockFor()`、`okHookCommand`、`StripLegacyOKHooks()`、`UpsertHooksBlock()`、`EnsureHooksBlock()`，以及随之不再使用的 import（`errors`、`regexp` 等，编译器驱动）。`InstallSkills` 中的 `SkillsHome()` 改为 `agentx.SkillsHome()`，import 增加 `"openknowledge/internal/agentx"`。文件顶部新增：

```go
// SkillNames 返回登记的技能名（供状态检测遍历）。
func SkillNames() []string {
	names := make([]string, 0, len(skillTemplates))
	for name := range skillTemplates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

（import 增加 `"sort"`。）

- [ ] **Step 5: 修改 `internal/setupx/uninstall.go`（hook 移除走注册表）**

把 "1. 移除 kimi config.toml …" 整段（`uninstall.go:28-50`）替换为：

```go
	// 1. 移除所有已注册 agent 的 hooks 集成
	hooksRemoved := false
	for _, a := range agentx.All() {
		removed, err := a.RemoveHooks()
		if err != nil {
			return r, fmt.Errorf("移除 %s hooks: %w", a.ID(), err)
		}
		hooksRemoved = hooksRemoved || removed
	}
	r.HooksRemoved = hooksRemoved
```

技能目录路径 `SkillsHome()` → `agentx.SkillsHome()`；import 增加 agentx、删除不再使用的 `strings`。`UninstallResult` 结构不变。

- [ ] **Step 6: 修改 `internal/cli/setup.go`（writeHooks 走注册表）**

`writeHooks` 整体替换：

```go
// writeHooks 对目标 agent 幂等写入 hooks 集成（targets 为 nil 时取全部已检测
// agent），供 setup 与 init 共用。
func writeHooks(targets []agentx.Agent, exe string, stdout, stderr io.Writer) int {
	if targets == nil {
		targets = agentx.Detected()
	}
	if len(targets) == 0 {
		fmt.Fprintln(stdout, "未检测到支持的 agent（kimi / pi），跳过 hooks 写入")
		return 0
	}
	for _, a := range targets {
		if err := a.InstallHooks(exe); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "hooks 配置已写入 %s\n", a.HooksTarget())
	}
	return 0
}
```

`Setup` 中调用改 `writeHooks(nil, exe, stdout, stderr)`；`setupx.InstallSkills`/`SkillsHome` 用法中 `SkillsHome()` → `agentx.SkillsHome()`；import 增加 agentx、删除 `os`（若不再使用）与 `path/filepath`（若不再使用，编译器驱动）。`internal/cli/cli.go:98` 的调用同步改 `writeHooks(nil, exe, stdout, stderr)`。

- [ ] **Step 7: 修改 `internal/hook/hook.go`（selfHealHooks 遍历注册表）**

```go
// selfHealHooks 逐 agent 自检 hooks 集成（如 kimi 清掉标记块时自动修复）。fail-open。
func selfHealHooks() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	for _, a := range agentx.Detected() {
		if err := a.EnsureHooks(exe); err != nil {
			logErr("self-heal hooks (%s): %v", a.ID(), err)
		}
	}
}
```

import：`"openknowledge/internal/setupx"` → `"openknowledge/internal/agentx"`（hook.go 中 setupx 仅 selfHeal 使用，确认后替换）。

- [ ] **Step 8: 修改 `internal/gui/api.go`**

`apiStatus` 中（api.go:274-284）替换为：

```go
	hooksInstalled := false
	if a, ok := agentx.Find("kimi"); ok {
		hooksInstalled = a.HooksInstalled()
	}
	skillsInstalled := true
	for _, name := range setupx.SkillNames() {
		if _, err := os.Stat(filepath.Join(agentx.SkillsHome(), name, "SKILL.md")); err != nil {
			skillsInstalled = false
			break
		}
	}
```

`apiSetupHooks`（api.go:777-792）替换为：

```go
func (h *Handler) apiSetupHooks(w http.ResponseWriter, _ *http.Request) {
	exe, err := exePath()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	a, ok := agentx.Find("kimi")
	if !ok {
		writeErr(w, http.StatusInternalServerError, "kimi agent 未注册")
		return
	}
	if err := a.InstallHooks(exe); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": a.HooksTarget()})
}
```

`apiSetupSkills` 中 `setupx.SkillsHome()` → `agentx.SkillsHome()`；import 增加 agentx。

- [ ] **Step 9: 修测试引用并跑全量测试**

- `internal/setupx/setupx_test.go`：删除已迁出的 hook 测试（保留技能/embedding/开关测试）。
- `internal/gui/api_test.go`：`setupx.KimiHome()` → `agentx.KimiHome()`、`setupx.MarkerBegin` → `agentx.MarkerBegin`、`setupx.SkillsHome()` → `agentx.SkillsHome()`。
- `internal/setupx/uninstall_test.go`：同上，编译器驱动。
- 其余引用 `setupx.SkillsHome/KimiHome/MarkerBegin` 的文件：`Grep` 全库逐一更新。

Run: `go build ./... && go test ./...`
Expected: 全部 PASS（行为与迁移前一致）。

- [ ] **Step 10: Commit**

```bash
git add internal/agentx internal/setupx internal/cli internal/hook internal/gui
git commit -m "refactor: 抽象 agentx 适配器注册表，kimi 支持零行为变化迁移"
```

---

### Task 2: pi 适配器与 TS 扩展模板

**Files:**
- Create: `internal/agentx/pi.go`
- Create: `internal/agentx/pi_extension.ts`
- Create: `internal/agentx/pi_test.go`

**Interfaces:**
- Consumes: Task 1 的 `Agent`/`Register`/`SkillsHome`。
- Produces:
  - `agentx.PiHome() string`（`PI_CODING_AGENT_DIR` 优先，否则 `~/.pi/agent`）
  - `agentx.Find("pi")` 可用；`pi` 适配器完整实现 `Agent` 接口
  - pi 事件 → ok 命令映射：`before_agent_start`→`hook prompt`、`tool_result`(write/edit)→`hook post-tool`、`agent_settled`→`hook stop`

- [ ] **Step 1: 写失败测试 `internal/agentx/pi_test.go`**

```go
package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiAgentInstallDetectRemove(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	a, ok := Find("pi")
	if !ok {
		t.Fatal("pi agent not registered")
	}
	if !a.Detect() {
		t.Fatal("Detect should be true when PI_CODING_AGENT_DIR exists")
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false before install")
	}
	if err := a.InstallHooks(`D:\x\ok.exe`); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(PiHome(), "extensions", "openknowledge.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("extension not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "// fingerprint: ") || !strings.Contains(content, "D:/x/ok.exe") {
		t.Fatalf("bad extension content: %.200s", content)
	}
	if strings.Contains(content, "{{EXE}}") {
		t.Fatal("unrendered placeholder remains")
	}
	if !a.HooksInstalled() {
		t.Fatal("HooksInstalled should be true after install")
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("extension file should be removed")
	}
}

func TestPiAgentForeignFilePreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	extDir := filepath.Join(dir, "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(extDir, "openknowledge.ts")
	if err := os.WriteFile(path, []byte("// user hand-written extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := piAgent{}
	if a.HooksInstalled() {
		t.Fatal("foreign file should not count as installed")
	}
	if err := a.InstallHooks(`D:\x\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak-openknowledge"); err != nil {
		t.Fatal("foreign file should be backed up before overwrite")
	}
	// 新文件是本工具生成后可删除；但手工文件恢复后不删
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if err := os.WriteFile(path, []byte("// user hand-written extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err = a.RemoveHooks()
	if err != nil || removed {
		t.Fatalf("foreign file must not be removed: %v, %v", removed, err)
	}
}

func TestPiAgentEnsureHooksStaleRewrite(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	a := piAgent{}
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(a.HooksTarget())
	if !strings.Contains(string(data), "D:/new/ok.exe") {
		t.Fatal("EnsureHooks should rewrite stale extension with new exe path")
	}
	// 文件不存在时 EnsureHooks 为 no-op（pi 不会触发 hook）
	if err := os.Remove(a.HooksTarget()); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.HooksTarget()); !os.IsNotExist(err) {
		t.Fatal("EnsureHooks must not recreate a deleted extension")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run TestPi -v`
Expected: FAIL（`Find("pi")` not registered）

- [ ] **Step 3: 创建 `internal/agentx/pi_extension.ts`**

```ts
import { execFile } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const OK = "{{EXE}}";

function runOk(
  args: string[],
  payload: unknown,
  timeoutMs: number,
): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve) => {
    try {
      const child = execFile(OK, args, { timeout: timeoutMs, windowsHide: true }, (error, stdout, stderr) => {
        const code = error && typeof error.code === "number" ? (error.code as number) : 0;
        resolve({ code, stdout: stdout ?? "", stderr: stderr ?? "" });
      });
      child.stdin?.end(JSON.stringify(payload));
    } catch {
      resolve({ code: 0, stdout: "", stderr: "" });
    }
  });
}

function sessionId(ctx: { sessionManager: { getSessionId(): string } }): string {
  try {
    return ctx.sessionManager.getSessionId();
  } catch {
    return "";
  }
}

export default function (pi: ExtensionAPI) {
  // ≈ kimi UserPromptSubmit：检索注入（ok 把结果写到 stdout）
  pi.on("before_agent_start", async (event, ctx) => {
    const r = await runOk(
      ["hook", "prompt"],
      { prompt: event.prompt, cwd: ctx.cwd, session_id: sessionId(ctx) },
      10000,
    );
    const out = r.stdout.trim();
    if (out) {
      return { message: { customType: "openknowledge", content: out, display: false } };
    }
  });

  // ≈ kimi PostToolUse（matcher Write|Edit）：记录触碰文件
  pi.on("tool_result", async (event, ctx) => {
    if (event.toolName !== "write" && event.toolName !== "edit") return;
    const input = (event as { input?: { path?: string } }).input;
    if (!input || !input.path) return;
    await runOk(
      ["hook", "post-tool"],
      { tool_input: { path: input.path }, cwd: ctx.cwd, session_id: sessionId(ctx) },
      5000,
    );
  });

  // ≈ kimi Stop：auto 自省 / enforce。ok 以 exit 2 + stderr 文本表达"阻断"，
  // pi 无法阻断已结束的回合，改为把提示注入会话驱动 agent 当场完成自省。
  pi.on("agent_settled", async (_event, ctx) => {
    const r = await runOk(
      ["hook", "stop"],
      { cwd: ctx.cwd, session_id: sessionId(ctx) },
      5000,
    );
    const reason = r.stderr.trim();
    if (r.code === 2 && reason) {
      pi.sendMessage(
        { customType: "openknowledge", content: reason, display: true },
        { triggerTurn: true },
      );
    }
  });
}
```

（类型签名依据：`types.ts:699-709` BeforeAgentStartEvent、`:779-785`/`:914-973` ToolResultEvent、`:722-725` AgentSettledEvent、`:1297-1300` sendMessage、`:317`+`session-manager.ts:194` getSessionId。）

- [ ] **Step 4: 创建 `internal/agentx/pi.go`**

```go
package agentx

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed pi_extension.ts
var piExtensionTemplate string

// piExtensionMarker 本工具生成的扩展文件头标记（RemoveHooks 据此识别归属）。
const piExtensionMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"

// PiHome 返回 pi 配置根目录（PI_CODING_AGENT_DIR 优先）。
func PiHome() string {
	if h := os.Getenv("PI_CODING_AGENT_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}

// piAgent Pi 适配器：hook 集成 = 写 TS 扩展到 ~/.pi/agent/extensions/。
type piAgent struct{}

func init() { Register(piAgent{}) }

func (piAgent) ID() string          { return "pi" }
func (piAgent) DisplayName() string { return "Pi" }
func (piAgent) SkillsDir() string   { return SkillsHome() }
func (piAgent) HooksTarget() string { return piExtensionPath() }

func piExtensionPath() string { return filepath.Join(PiHome(), "extensions", "openknowledge.ts") }

func (piAgent) Detect() bool {
	info, err := os.Stat(PiHome())
	return err == nil && info.IsDir()
}

// piTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func piTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(piExtensionTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderPiExtension 渲染扩展：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderPiExtension(exe string) string {
	body := strings.ReplaceAll(piExtensionTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return piExtensionMarker + "\n// fingerprint: " + piTemplateFingerprint() + "\n" + body
}

func (piAgent) HooksInstalled() bool {
	data, err := os.ReadFile(piExtensionPath())
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, piExtensionMarker) &&
		strings.Contains(content, "// fingerprint: "+piTemplateFingerprint())
}

func (piAgent) InstallHooks(exe string) error {
	path := piExtensionPath()
	if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), piExtensionMarker) {
		_ = os.WriteFile(path+".bak-openknowledge", data, 0o644)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(renderPiExtension(exe)), 0o644)
}

func (piAgent) RemoveHooks() (bool, error) {
	path := piExtensionPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(data), piExtensionMarker) {
		return false, nil // 非本工具生成，不删
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("删除 pi 扩展: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：文件存在且为本工具生成、但内容过期（模板升级或 exe 迁移）
// 时重写；文件不存在时为 no-op（pi 无扩展即不会触发 hook，无需修复）。
func (piAgent) EnsureHooks(exe string) error {
	path := piExtensionPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if !strings.Contains(string(data), piExtensionMarker) {
		return nil
	}
	rendered := renderPiExtension(exe)
	if string(data) == rendered {
		return nil
	}
	return os.WriteFile(path, []byte(rendered), 0o644)
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/agentx/ -v`
Expected: 全部 PASS（含 Task 1 迁入的 kimi 测试）

- [ ] **Step 6: 扩展模板冒烟检查**

Run: `go test ./internal/agentx/ -run TestPiAgentInstallDetectRemove -v` 已覆盖 `{{EXE}}` 无残留；另手动执行 `node --check internal/agentx/pi_extension.ts`（模板含 `{{EXE}}` 占位符，语法检查仅需确认 import/函数结构可被解析；若 node 报占位符行错，忽略该行——占位符在字符串字面量内，不影响解析）。
Expected: node --check 无语法错误。

- [ ] **Step 7: Commit**

```bash
git add internal/agentx/pi.go internal/agentx/pi_extension.ts internal/agentx/pi_test.go
git commit -m "feat(agentx): pi 适配器——TS 扩展注入三事件 hook"
```

---

### Task 3: CLI `--agent` 参数

**Files:**
- Modify: `internal/cli/setup.go`
- Modify: `internal/cli/setup_test.go`

**Interfaces:**
- Consumes: `agentx.Find/Detected`、`cli.writeHooks(targets, ...)`（Task 1）。
- Produces: `ok setup [--agent <id>]`；`ok uninstall` 行为不变（Task 1 已走注册表）。

- [ ] **Step 1: 写失败测试（追加到 `internal/cli/setup_test.go`）**

```go
func TestSetupUnknownAgent(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	var out, errBuf bytes.Buffer
	code := Setup([]string{"--agent", "nope"}, strings.NewReader(""), &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "未知 agent") {
		t.Fatalf("stderr should mention unknown agent: %q", errBuf.String())
	}
}

func TestSetupAgentKimiOnly(t *testing.T) {
	kimiHome := t.TempDir()
	piHome := t.TempDir()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", kimiHome)
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	var out, errBuf bytes.Buffer
	code := Setup([]string{"--agent", "kimi"}, strings.NewReader("\n\n\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(kimiHome, "config.toml")); err != nil {
		t.Fatal("kimi hooks should be written")
	}
	if _, err := os.Stat(filepath.Join(piHome, "extensions", "openknowledge.ts")); !os.IsNotExist(err) {
		t.Fatal("pi extension should NOT be written with --agent kimi")
	}
}

func TestSetupAllDetectedAgents(t *testing.T) {
	kimiHome := t.TempDir()
	piHome := t.TempDir()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", kimiHome)
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	var out, errBuf bytes.Buffer
	code := Setup(nil, strings.NewReader("\n\n\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(kimiHome, "config.toml")); err != nil {
		t.Fatal("kimi hooks should be written")
	}
	if _, err := os.Stat(filepath.Join(piHome, "extensions", "openknowledge.ts")); err != nil {
		t.Fatal("pi extension should be written")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestSetup -v`
Expected: `TestSetupUnknownAgent` 与 `TestSetupAgentKimiOnly` FAIL（`--agent` flag 未定义）；`TestSetupAllDetectedAgents` 此时应 PASS（Task 2 后 Detected 已含 pi）。

- [ ] **Step 3: 修改 `internal/cli/setup.go`**

`Setup` 函数 flags 区追加并调整流程：

```go
	agentID := fs.String("agent", "", "只安装指定 agent 的 hooks（kimi|pi）；缺省为全部已检测 agent")
```

`fs.Parse` 之后、`resolveExe` 之前插入：

```go
	var targets []agentx.Agent
	if *agentID != "" {
		a, ok := agentx.Find(*agentID)
		if !ok {
			fmt.Fprintf(stderr, "未知 agent %q（可用：%s）\n", *agentID, agentIDs())
			return 1
		}
		if !a.Detect() {
			fmt.Fprintf(stderr, "提示：未检测到 %s，仍将写入其配置\n", a.DisplayName())
		}
		targets = []agentx.Agent{a}
	}
```

`writeHooks(nil, ...)` 调用改 `writeHooks(targets, ...)`。文件末尾追加：

```go
// agentIDs 返回已注册 agent 的 id 列表（用于报错提示）。
func agentIDs() string {
	ids := make([]string, 0, len(agentx.All()))
	for _, a := range agentx.All() {
		ids = append(ids, a.ID())
	}
	return strings.Join(ids, " / ")
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/cli/setup.go internal/cli/setup_test.go
git commit -m "feat(cli): ok setup 支持 --agent 指定目标 agent"
```

---

### Task 4: GUI API 多 agent

**Files:**
- Modify: `internal/gui/api.go`（apiStatus、apiSetupHooks）
- Modify: `internal/gui/api_test.go`（newEnv 隔离 pi、status 断言、新增用例）

**Interfaces:**
- Consumes: `agentx.All/Find/Detected`、`setupx.SkillNames()`（Task 1-2）。
- Produces（前端 Task 5 依赖）:
  - `GET /api/status` 响应含 `agents: [{id, name, detected, hooksInstalled}]`（**移除**顶层 `hooksInstalled` 字段），`skillsInstalled` 保留
  - `POST /api/setup/hooks` 接受可选 `{"agent": "<id>"}`，响应 `{"ok": true, "installed": [{"agent", "path"}]}`；未知 id → 400

- [ ] **Step 1: 改测试（先失败）——`internal/gui/api_test.go`**

`newEnv`（api_test.go:25-43）增加一行隔离：

```go
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
```

`TestStatusEmptyRegistry`（api.go:90 附近）：把对 `hooksInstalled` 的断言改为：

```go
	var res struct {
		Agents []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Detected       bool   `json:"detected"`
			HooksInstalled bool   `json:"hooksInstalled"`
		} `json:"agents"`
		SkillsInstalled bool `json:"skillsInstalled"`
	}
	// json.Unmarshal(data, &res) 后：
	if len(res.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d: %s", len(res.Agents), data)
	}
```

`TestSetupHooksAndSkills`（api_test.go:402-430）：引用更新为 agentx，并追加 pi 指定安装断言：

```go
	code, data = do(t, "POST", srv.URL+"/api/setup/hooks", testToken, map[string]any{"agent": "pi"})
	if code != 200 {
		t.Fatalf("setup/hooks pi: status = %d, body %s", code, data)
	}
	ext, err := os.ReadFile(filepath.Join(agentx.PiHome(), "extensions", "openknowledge.ts"))
	if err != nil {
		t.Fatalf("pi extension not written: %v", err)
	}
	if !strings.Contains(string(ext), "hook prompt") {
		t.Fatalf("unexpected pi extension content: %.200s", ext)
	}
	code, _ = do(t, "POST", srv.URL+"/api/setup/hooks", testToken, map[string]any{"agent": "nope"})
	if code != 400 {
		t.Fatalf("unknown agent should be 400, got %d", code)
	}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run 'TestStatusEmptyRegistry|TestSetupHooksAndSkills' -v`
Expected: FAIL（`agents` 字段不存在 / pi 分支未实现）。

- [ ] **Step 3: 修改 `internal/gui/api.go`**

`apiStatus`：把 Task 1 的临时 `hooksInstalled` 计算替换为：

```go
	agents := make([]map[string]any, 0, len(agentx.All()))
	for _, a := range agentx.All() {
		agents = append(agents, map[string]any{
			"id":             a.ID(),
			"name":           a.DisplayName(),
			"detected":       a.Detect(),
			"hooksInstalled": a.HooksInstalled(),
		})
	}
```

响应 map 中 `"hooksInstalled": hooksInstalled` 改为 `"agents": agents`（其余字段不变）。

`apiSetupHooks` 整体替换为：

```go
func (h *Handler) apiSetupHooks(w http.ResponseWriter, r *http.Request) {
	exe, err := exePath()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Agent string `json:"agent"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	var targets []agentx.Agent
	if req.Agent != "" {
		a, ok := agentx.Find(req.Agent)
		if !ok {
			writeErr(w, http.StatusBadRequest, "未知 agent: "+req.Agent)
			return
		}
		targets = []agentx.Agent{a}
	} else {
		targets = agentx.Detected()
	}
	installed := make([]map[string]string, 0, len(targets))
	for _, a := range targets {
		if err := a.InstallHooks(exe); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		installed = append(installed, map[string]string{"agent": a.ID(), "path": a.HooksTarget()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "installed": installed})
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api.go internal/gui/api_test.go
git commit -m "feat(gui): /api/status 返回 agents 数组，setup/hooks 支持指定 agent"
```

---

### Task 5: 前端 agents 下拉联动

**Files:**
- Modify: `web/index.html`（`#page-guide` 顶部加下拉，hooks/技能卡文案）
- Modify: `web/app.js`（state.agent、renderGuide、事件绑定）
- Modify: `web/style.css`（agent-bar 样式）

**Interfaces:**
- Consumes: Task 4 的 `agents` 数组与 `POST /api/setup/hooks {agent}`。
- Produces: 无（纯前端）。

- [ ] **Step 1: 修改 `web/index.html`**

`#page-guide` 内 `<div class="cards">` 之前插入：

```html
      <div class="agent-bar">
        <label for="agent-select">Agent：</label>
        <select id="agent-select"></select>
      </div>
```

hooks 卡描述（index.html:74）改为：

```html
          <p class="card-desc muted">把 3 条 hook 写入 <span id="hooks-agent-name">agent</span> 配置，知识注入与强制检查的入口。</p>
```

技能卡描述（index.html:79）改为：

```html
          <p class="card-desc muted">6 个技能：init / on / off / propose / capture / wiki，装入共享目录，Kimi Code 与 Pi 均可读。</p>
```

- [ ] **Step 2: 修改 `web/style.css`（追加）**

```css
.agent-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.agent-bar select { padding: 4px 8px; font-size: 14px; }
```

- [ ] **Step 3: 修改 `web/app.js`**

在 `state` 对象初始化处（`var state = {...}` 定义后一行）追加：

```js
  state.agent = localStorage.getItem("ok.agent") || "";
```

`renderGuide`（app.js:373-386）整体替换为：

```js
  function currentAgent() {
    var agents = (state.status && state.status.agents) || [];
    for (var i = 0; i < agents.length; i++) {
      if (agents[i].id === state.agent) return agents[i];
    }
    return null;
  }

  function renderAgentSelect(agents) {
    var sel = $("agent-select");
    sel.innerHTML = "";
    agents.forEach(function (a) {
      var opt = document.createElement("option");
      opt.value = a.id;
      opt.textContent = a.name + (a.detected ? "" : "（未安装）");
      opt.disabled = !a.detected;
      sel.appendChild(opt);
    });
    var ids = agents.map(function (a) { return a.id; });
    if (ids.indexOf(state.agent) < 0) {
      var first = agents.filter(function (a) { return a.detected; })[0] || agents[0];
      state.agent = first ? first.id : "";
    }
    sel.value = state.agent;
  }

  function renderGuide(s) {
    var agents = s.agents || [];
    renderAgentSelect(agents);
    var cur = currentAgent();
    setBadge("badge-hooks", !!(cur && cur.hooksInstalled), "已安装", "未配置");
    $("hooks-agent-name").textContent = cur ? cur.name : "agent";
    $("btn-hooks").textContent = cur ? ("写入 " + cur.name + " hooks 配置") : "写入 hooks 配置";
    setBadge("badge-skills", s.skillsInstalled, "已安装", "未配置");
    setBadge("badge-embedding", s.embeddingConfigured, "已配置", "未配置");
    setBadge("badge-toggle", !s.disabled, "已开启", "已关闭");
    $("btn-toggle").textContent = s.disabled ? "开启" : "关闭";
    if (s.embedding) {
      if (s.embedding.base_url) $("emb-base-url").value = s.embedding.base_url;
      if (s.embedding.model) $("emb-model").value = s.embedding.model;
      $("emb-api-key").placeholder = s.embedding.has_key ? "已保存（留空保持不变）" : "api_key";
    }
    refreshCapture();
  }
```

事件绑定区（`$("btn-hooks")` 监听附近）替换/新增：

```js
  $("agent-select").addEventListener("change", function () {
    state.agent = this.value;
    localStorage.setItem("ok.agent", state.agent);
    if (state.status) renderGuide(state.status);
  });

  $("btn-hooks").addEventListener("click", function () {
    api("/api/setup/hooks", { method: "POST", body: { agent: state.agent } })
      .then(function () { refreshStatus(); })
      .catch(function (err) { showError(err.message); });
  });
```

（确认 `refreshStatus` 内部以 `state.status = s; renderGuide(s)` 形式调用——现有行为不变，若实现不同则按其现状对齐。）

- [ ] **Step 4: 构建并手动验证**

Run: `go build -o dist/ok.exe ./cmd/ok` 后启动 daemon/GUI：
- 引导页出现 Agent 下拉，含 Kimi Code / Pi 两项；
- 切换下拉，hooks 卡徽标与按钮文案联动；
- 点"写入 Pi hooks 配置"后 `~/.pi/agent/extensions/openknowledge.ts` 生成且徽标变"已安装"；
- 刷新页面下拉选择保持（localStorage）。

- [ ] **Step 5: Commit**

```bash
git add web/index.html web/app.js web/style.css
git commit -m "feat(web): 引导页 agents 下拉，hooks 卡随选中 agent 联动"
```

---

### Task 6: 文档与全量验证

**Files:**
- Modify: `docs/ARCHITECTURE.md`（5.11、6.4、9.1、新增 9.x 多 agent 小节、18.3/18.4）
- Modify: `README.md`（提及支持 pi）

- [ ] **Step 1: 更新 `docs/ARCHITECTURE.md`**

- 5.9 hook 节：补充"hook 入口 selfHeal 遍历 `agentx.Detected()`"。
- 5.11 gui 节 API 表：`/api/status` 响应字段更新（`agents` 数组）、`/api/setup/hooks` 增加 `agent` 参数说明。
- 6.4 首次引导：`ok setup [--agent]` 行为说明（缺省全部已检测 agent）。
- 9.1 之后新增 `9.x 多 agent 抽象（agentx）`小节：`Agent` 接口契约、注册表、kimi= TOML 标记块 / pi=TS 扩展两种注入形态、pi 事件映射表（含"exit 2+stderr → sendMessage"的语义差异说明）。
- 18.3 环境变量：新增 `PI_CODING_AGENT_DIR`；18.4 hooks 配置补 pi 扩展文件路径与指纹机制。

- [ ] **Step 2: 更新 `README.md`**：特性列表加"支持 Kimi Code 与 Pi 双 agent（可扩展适配器架构）"。

- [ ] **Step 3: 全量验证**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部 PASS。

- [ ] **Step 4: Commit**

```bash
git add docs/ARCHITECTURE.md README.md
git commit -m "docs: 多 agent 架构（agentx + pi 扩展）写入 ARCHITECTURE 与 README"
```

---

## Self-Review 记录

- **Spec 覆盖**：接口抽象(T1) ✓、pi 三事件 hook(T2) ✓、CLI --agent(T3) ✓、status/setup API(T4) ✓、GUI 下拉联动(T5) ✓、skillsInstalled 漏检 wiki 修复(T1 Step8 `SkillNames`) ✓、卸载遍历注册表(T1 Step5) ✓、selfHeal 遍历(T1 Step7) ✓、文档(T6) ✓。
- **对 spec 的一处修正**：pi Stop 阻断读 **stderr**（`HandleStop` 的 `fmt.Fprintln(stderr, ...)` + exit 2），不是 spec 3.4 表格写的 stdout；已反映在本计划 Global Constraints 与 Task 2 模板中。
- **类型一致性**：`Agent` 接口方法集在 T1 定义、T2 实现、T3/T4 消费一致；`writeHooks(targets []agentx.Agent, ...)` 签名在 T1 Step6 定义、T3 Step3 复用一致。
