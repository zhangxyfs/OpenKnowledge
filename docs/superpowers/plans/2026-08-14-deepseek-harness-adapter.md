# DeepSeek Harness 适配器实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 agentx 第十适配器 `dsh`，以本地绝对路径挂载的原生 JS 插件为 DeepSeek Harness 提供 prompt 注入 / post-tool 追踪 / stop 沉淀闭环。

**Architecture:** `internal/agentx/deepharness.go`（Go 适配器，注册表驱动）+ `internal/agentx/dsh_plugin.js`（go:embed 薄桥插件，事件订阅→`ok.exe hook <event>`→DSH 注入通道）。挂载点 = 家目录级 `$DSH_HOME/cordis.patch.yml` 标记块。技能分发复用共享 `SkillsHome()`，零改动。

**Tech Stack:** Go（agentx 适配器，复用 `UpsertHooksBlock`/`MarkerBegin`/`MarkerEnd`）、纯 ESM JS 插件（`node:child_process` execFile 直 exec）。

**Spec:** `docs/superpowers/specs/2026-08-14-deepseek-harness-adapter-design.md`

## Global Constraints

- 适配器 ID 固定 `"dsh"`，DisplayName 固定 `"DeepSeek Harness"`。
- home 解析序固定：`OK_DSH_HOME` > `DSH_HOME` > `~/.dsh`。
- 插件为纯 ESM JS，**禁止** import 任何 `@deepseek-ai/*` 包与 `bun` 模块；只用 `node:child_process` + `node:crypto`。
- 插件全程 fail-open：任何异常/超时/非零退出（stop 的 exit 2 除外）静默吞掉，不得拖垮 DSH 宿主。
- patch 文件编辑一律文本标记块（`MarkerBegin`/`MarkerEnd`，`#` 注释在 YAML 合法），**不引入 YAML 库**。
- 输出协议 = 纯文本 format（args 末尾不带 `claude`）：注入写 stdout；阻断 = stderr + exit 2。
- stdin 一律 Claude 风格 snake_case JSON。
- 测试隔离口一律 `t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))`（遍历注册表测试）或真实 TempDir（适配器自身测试）。
- git 提交需经用户逐次确认后再执行（会话规则）。

---

### Task 1: Go 适配器骨架（home 解析 / Detect / SkillsDir / 注册）

**Files:**
- Create: `internal/agentx/deepharness.go`
- Create: `internal/agentx/deepharness_test.go`

**Interfaces:**
- Consumes: `agentx.go` 的 `Register(Agent)`、`SkillsHome()`、`Agent` 接口 9 方法
- Produces: `DSHHome() string`、`dshPluginPath() string`、`dshPatchPath() string`、`dshAgent{}`（后续 Task 补全其余方法）

- [ ] **Step 1: 写失败测试**

`internal/agentx/deepharness_test.go`:

```go
package agentx

import (
	"os"
	"path/filepath"
	"testing"
)

// setupDSH 隔离 DSH 家目录与 OK_HOME（HookTimeoutSec 读全局配置），返回 DSHHome。
func setupDSH(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_DSH_HOME", home)
	t.Setenv("OK_HOME", t.TempDir())
	return home
}

func TestDSHHome(t *testing.T) {
	t.Setenv("OK_DSH_HOME", "")
	t.Setenv("DSH_HOME", `D:\dsh-official`)
	if got := DSHHome(); got != `D:\dsh-official` {
		t.Fatalf("DSH_HOME 优先于默认目录: %q", got)
	}
	t.Setenv("OK_DSH_HOME", `D:\dsh-ok`)
	t.Setenv("DSH_HOME", `D:\dsh-official`)
	if got := DSHHome(); got != `D:\dsh-ok` {
		t.Fatalf("OK_DSH_HOME 应最优先: %q", got)
	}
}

func TestDSHDetect(t *testing.T) {
	setupDSH(t)
	if !(dshAgent{}).Detect() {
		t.Fatal("DSHHome 存在应检测为真")
	}
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (dshAgent{}).Detect() {
		t.Fatal("DSHHome 不存在应检测为假")
	}
}

func TestDSHSkillsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	setupDSH(t)
	if got := (dshAgent{}).SkillsDir(); got != dir {
		t.Fatalf("SkillsDir 应为共享 SkillsHome: %q", got)
	}
}

func TestDSHRegistered(t *testing.T) {
	if Find("dsh") == nil {
		t.Fatal("dsh 适配器应已注册")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run TestDSH -v`
Expected: 编译失败 `undefined: dshAgent` / `undefined: DSHHome`

- [ ] **Step 3: 写适配器骨架**

`internal/agentx/deepharness.go`（本 Task 只含骨架；InstallHooks 等在 Task 2/3 补全——先写完整文件骨架，方法体在后续 Task 替换）:

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

//go:embed dsh_plugin.js
var dshPluginTemplate string

// dshPluginMarker 本工具生成的插件文件头标记（RemoveHooks 据此识别归属）。
const dshPluginMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"

// DSHHome 返回 DeepSeek Harness 家目录。解析序：OK_DSH_HOME（ok 自留测试隔离口，
// OK_ZCODE_HOME 同款）> DSH_HOME（官方重定位变量，packages/util/home-paths 的
// resolveDshHome）> ~/.dsh。
func DSHHome() string {
	if h := os.Getenv("OK_DSH_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("DSH_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dsh")
}

// dshPluginPath 插件写入目标：<home>/plugins/openknowledge/index.js。
// DSH 无插件目录自动扫描，位置为 ok 自选，经 cordis.patch.yml 绝对路径挂载。
func dshPluginPath() string { return filepath.Join(DSHHome(), "plugins", "openknowledge", "index.js") }

// dshPatchPath 家目录级 patch 文件：<home>/cordis.patch.yml（所有 profile 共享，
// DSH 文档明示的家目录级 patch 层）。
func dshPatchPath() string { return filepath.Join(DSHHome(), "cordis.patch.yml") }

// dshTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func dshTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(dshPluginTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderDSHPlugin 渲染插件：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderDSHPlugin(exe string) string {
	body := strings.ReplaceAll(dshPluginTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return dshPluginMarker + "\n// fingerprint: " + dshTemplateFingerprint() + "\n" + body
}

// dshPatchBlock 家目录 patch 行：绝对路径挂载本地插件（cordis patch 的 name 字段
// 接受绝对路径；YAML 单引号字符串 + 正斜杠，规避 Windows 反斜杠转义）。
func dshPatchBlock() string {
	return "- insert:\n    - id: ok-hooks\n      name: '" + filepath.ToSlash(dshPluginPath()) + "'\n"
}

// dshAgent DeepSeek Harness 适配器：hook 集成 = 本地 JS 插件 + 家目录 patch 行挂载；
// 技能共享 SkillsHome（DSH 原生扫描 ~/.agents/skills）。
type dshAgent struct{}

func init() { Register(dshAgent{}) }

func (dshAgent) ID() string          { return "dsh" }
func (dshAgent) DisplayName() string { return "DeepSeek Harness" }
func (dshAgent) SkillsDir() string   { return SkillsHome() }
func (dshAgent) HooksTarget() string { return dshPluginPath() }

func (dshAgent) Detect() bool {
	info, err := os.Stat(DSHHome())
	return err == nil && info.IsDir()
}

func (dshAgent) HooksInstalled() bool { return false }             // Task 3 实现
func (dshAgent) InstallHooks(exe string) error { _ = exe; return nil } // Task 2/3 实现
func (dshAgent) RemoveHooks() (bool, error)  { return false, nil }     // Task 3 实现
func (dshAgent) EnsureHooks(exe string) error { _ = exe; return nil }  // Task 3 实现
```

- [ ] **Step 4: 建空插件模板占位（编译需要 embed 目标存在）**

`internal/agentx/dsh_plugin.js`，先写一行占位（Task 2 替换为完整内容）:

```js
export const name = "openknowledge";
export function apply(ctx) {}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/agentx/ -run TestDSH -v`
Expected: 4 个测试 PASS

- [ ] **Step 6: Commit（先经用户确认）**

```bash
git add internal/agentx/deepharness.go internal/agentx/deepharness_test.go internal/agentx/dsh_plugin.js
git commit -m "feat(agentx): add dsh adapter skeleton (home resolution, detect, registry)"
```

---

### Task 2: JS 插件模板完整实现 + 插件文件安装

**Files:**
- Modify: `internal/agentx/dsh_plugin.js`（替换占位全文）
- Modify: `internal/agentx/deepharness.go`（实现 `InstallHooks` 插件文件部分 + `HooksInstalled` 插件文件部分）
- Test: `internal/agentx/deepharness_test.go`

**Interfaces:**
- Consumes: Task 1 的 `renderDSHPlugin`、`dshPluginMarker`、`dshPluginPath`
- Produces: `InstallHooks` 写插件文件行为（Task 3 在此基础上加 patch 行）

**已核实的事实（写入插件注释，勿再改）:**
- DSH 写盘工具名为 `write`/`edit`，参数键 `file_path`（`packages/fs/tool-fs/src/write.ts:51`、`edit.ts:84`），与 Claude 方言一致。
- `UserMessage` = 纯数据 `{id, role:'user', content, source:{kind:'plugin',plugin}}`，id 为 uuid（`packages/llm/llm/src/message.ts:178-199`），插件可用 `node:crypto` 的 `randomUUID()` 自造，无需依赖 dsh-llm。
- `agent.steer(message UserMessage)`（`runtime-types.ts:133`）；阻断式 stop 的官方桥译法就是 `agent.steer(...)`（`packages/hooks/hooks-claude-code/src/index.ts:270-277`）。
- waterfall 监听器 `next()` 必须恰好调用一次；官方桥模式 = 先跑 hook 再 `next()`，把注入折叠到下游 enter 决定上。

- [ ] **Step 1: 写失败测试（插件文件安装部分）**

向 `deepharness_test.go` 追加:

```go
func TestDSHInstallWritesPlugin(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	if err := (dshAgent{}).InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dshPluginPath())
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, dshPluginMarker) {
		t.Fatal("插件应含头标记")
	}
	if !strings.Contains(content, "// fingerprint: "+dshTemplateFingerprint()) {
		t.Fatal("插件应含当前指纹")
	}
	if !strings.Contains(content, filepath.ToSlash(exe)) {
		t.Fatal("插件应烘焙 exe 正斜杠路径")
	}
	for _, want := range []string{`ctx.on("agent/pre-step"`, `ctx.on("tools/post-execute"`, `ctx.on("agent/turn-stopping"`, `["hook", "prompt"]`, `["hook", "post-tool"]`, `["hook", "stop"]`} {
		if !strings.Contains(content, want) {
			t.Fatalf("插件缺少 %q", want)
		}
	}
}

func TestDSHInstallBacksUpForeignPlugin(t *testing.T) {
	setupDSH(t)
	if err := os.MkdirAll(filepath.Dir(dshPluginPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// someone else's plugin\n"
	if err := os.WriteFile(dshPluginPath(), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(dshPluginPath() + ".bak-openknowledge")
	if err != nil || string(bak) != foreign {
		t.Fatalf("应备份既有非自家插件: %v %q", err, bak)
	}
}
```

（文件头 import 需补 `"strings"`。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run 'TestDSHInstall' -v`
Expected: FAIL（InstallHooks 是 no-op，读不到插件文件）

- [ ] **Step 3: 写完整插件模板**

`internal/agentx/dsh_plugin.js` 全文替换为:

```js
import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";

const OK = "{{EXE}}";

// 与 pi/opencode 一致的超时预算
const PROMPT_TIMEOUT_MS = 10000;
const HOOK_TIMEOUT_MS = 5000;

// runOk 调 `ok hook <event>` 子进程：stdin 喂 Claude 风格 snake_case JSON，读
// stdout/stderr/exit code。execFile 直 exec（无 shell 层，天然免疫 Windows pwsh
// 引号问题，且不经 DSH 的 workspace-write 沙箱执行器），内建 timeout（超时自动
// kill）与 windowsHide。全程 fail-open——启动失败、超时、读写异常一律解析为
// 空结果，绝不拖累 DSH 会话。
function runOk(args, payload, timeoutMs) {
  return new Promise((resolve) => {
    try {
      const child = execFile(OK, args, { timeout: timeoutMs, windowsHide: true }, (error, stdout, stderr) => {
        // error.code 为数字即进程退出码（exit 2 = ok 的阻断语义）；超时/启动失败
        // 时 code 非数字，归为空结果
        const code = error && typeof error.code === "number" ? error.code : 0;
        resolve({ code, stdout: stdout ?? "", stderr: stderr ?? "" });
      });
      child.stdin?.end(JSON.stringify(payload));
    } catch {
      resolve({ code: 0, stdout: "", stderr: "" });
    }
  });
}

// userMessage 构造 DSH 的 UserMessage：纯数据 + uuid + role 'user' + plugin 来源
// 标记，等价 dsh-llm 的 createUserMessage（packages/llm/llm/src/message.ts:192），
// 避免本地绝对路径插件依赖 DSH 内部包（node 解析不到 @deepseek-ai/*）。
function userMessage(text) {
  return {
    id: randomUUID(),
    role: "user",
    content: [{ type: "text", text }],
    source: { kind: "plugin", plugin: "openknowledge" },
  };
}

// sessionFields 从 agent 提取 ok 侧需要的会话字段（与官方桥 base() 同款取法）。
function sessionFields(agent) {
  return {
    session_id: agent?.session?.header?.id ?? "",
    cwd: agent?.session?.header?.cwd ?? process.cwd(),
  };
}

export const name = "openknowledge";

export function apply(ctx) {
  // ≈ UserPromptSubmit：检索注入。waterfall 语义：先跑 ok（fail-open）再 delegate
  // next()，仅当下游为 enter 时把注入消息附加到尾部——与官方桥同构
  // （packages/hooks/hooks-claude-code/src/index.ts:219-235）。插件侧不去重，
  // 是否注入由 ok 的 InjectForPrompt 决定（与其他 agent 一致）。
  ctx.on("agent/pre-step", async ({ agent, messages }, next) => {
    let extra = null;
    try {
      if (messages && messages.length > 0) {
        const promptText = messages
          .flatMap((m) => m.content ?? [])
          .filter((b) => b && b.type === "text" && typeof b.text === "string")
          .map((b) => b.text)
          .join("\n")
          .trim();
        if (promptText) {
          const r = await runOk(
            ["hook", "prompt"],
            { hook_event_name: "UserPromptSubmit", ...sessionFields(agent), prompt: promptText },
            PROMPT_TIMEOUT_MS,
          );
          const out = (r.stdout || "").trim();
          if (out) extra = userMessage(out);
        }
      }
    } catch { /* fail-open */ }
    const downstream = await next();
    if (!extra || downstream.kind !== "enter") return downstream;
    return { kind: "enter", messages: [...downstream.messages, extra] };
  });

  // ≈ PostToolUse(matcher write|edit)：记录触碰文件。DSH 写盘工具为 write/edit，
  // 参数键 file_path（packages/fs/tool-fs/src/write.ts:51），与 Claude 方言一致，
  // tool_input 原样透传。输出忽略，任何失败静默，结果一律 accept 放行。
  ctx.on("tools/post-execute", async (exec, _result, next) => {
    try {
      if (exec && (exec.name === "write" || exec.name === "edit")) {
        await runOk(
          ["hook", "post-tool"],
          {
            hook_event_name: "PostToolUse",
            ...sessionFields(exec.agent),
            tool_name: exec.name,
            tool_input: exec.arguments ?? {},
          },
          HOOK_TIMEOUT_MS,
        );
      }
    } catch { /* fail-open */ }
    return next();
  });

  // ≈ Stop：auto 自省 / enforce。ok 以 exit 2 + stderr 表达"阻断"；
  // agent/turn-stopping 边界上 agent.steer() 强制再来一步（官方桥同款，
  // hooks-claude-code/src/index.ts:270-277）。防重依赖 ok 侧 CheckStop 幂等
  // 语义，插件侧与 pi/opencode 一致不计数。
  ctx.on("agent/turn-stopping", async ({ agent }) => {
    try {
      const r = await runOk(
        ["hook", "stop"],
        { hook_event_name: "Stop", ...sessionFields(agent) },
        HOOK_TIMEOUT_MS,
      );
      const reason = (r.stderr || "").trim();
      if (r.code === 2 && reason) agent.steer(userMessage(reason));
    } catch { /* fail-open */ }
  });
}
```

- [ ] **Step 4: 实现 InstallHooks 插件文件部分 + HooksInstalled 插件文件部分**

`deepharness.go` 中替换两个方法:

```go
func (dshAgent) HooksInstalled() bool {
	data, err := os.ReadFile(dshPluginPath())
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, dshPluginMarker) &&
		strings.Contains(content, "// fingerprint: "+dshTemplateFingerprint())
	// Task 3 追加 patch 行判定
}

func (dshAgent) InstallHooks(exe string) error {
	// 插件文件（自有新文件整写；既有文件非自家则先备份）
	path := dshPluginPath()
	if data, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(data), dshPluginMarker) {
			if err := os.WriteFile(path+".bak-openknowledge", data, 0o644); err != nil {
				return fmt.Errorf("备份既有插件失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取既有插件失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(renderDSHPlugin(exe)), 0o644)
	// Task 3 追加 patch 行 upsert
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/agentx/ -run TestDSH -v`
Expected: 全部 PASS

- [ ] **Step 6: Commit（先经用户确认）**

```bash
git add internal/agentx/dsh_plugin.js internal/agentx/deepharness.go internal/agentx/deepharness_test.go
git commit -m "feat(agentx): dsh plugin template with prompt/post-tool/stop bridges"
```

---

### Task 3: patch 行管理（cordis.patch.yml 标记块 upsert / remove / ensure）

**Files:**
- Modify: `internal/agentx/deepharness.go`
- Test: `internal/agentx/deepharness_test.go`

**Interfaces:**
- Consumes: `kimi.go` 的 `UpsertHooksBlock(configPath, block string) error`、`MarkerBegin`、`MarkerEnd`（`#` 注释对 YAML 合法；`StripLegacyOKHooks` 只认 TOML `[[hooks]]` 表，对 patch 文件是安全 no-op）
- Produces: 完整的 `InstallHooks`/`RemoveHooks`/`HooksInstalled`/`EnsureHooks`；`removeDSHMarkerBlock(content string) (string, bool)`

- [ ] **Step 1: 写失败测试**

向 `deepharness_test.go` 追加:

```go
func readDSHPatch(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(dshPatchPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDSHInstallWritesPatchLine(t *testing.T) {
	setupDSH(t)
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if !strings.Contains(patch, MarkerBegin) || !strings.Contains(patch, MarkerEnd) {
		t.Fatalf("patch 应含标记块: %q", patch)
	}
	if !strings.Contains(patch, "id: ok-hooks") {
		t.Fatalf("patch 应含 ok-hooks 行: %q", patch)
	}
	if !strings.Contains(patch, "name: '"+filepath.ToSlash(dshPluginPath())+"'") {
		t.Fatalf("patch 应含插件绝对路径（正斜杠单引号）: %q", patch)
	}
	if !(dshAgent{}).HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为真")
	}
}

func TestDSHInstallIdempotent(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	a := dshAgent{}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if n := strings.Count(patch, "id: ok-hooks"); n != 1 {
		t.Fatalf("重复安装后应有 1 条 ok-hooks 行, got %d", n)
	}
}

func TestDSHInstallPreservesForeignPatch(t *testing.T) {
	setupDSH(t)
	pre := "# 用户自己的 patch\n- insert:\n    - id: my-plugin\n      name: 'D:/x/my.js'\n"
	if err := os.WriteFile(dshPatchPath(), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if !strings.Contains(patch, "id: my-plugin") || !strings.Contains(patch, "# 用户自己的 patch") {
		t.Fatalf("第三方行与注释应保留: %q", patch)
	}
	if !strings.Contains(patch, "id: ok-hooks") {
		t.Fatalf("ok 行应追加: %q", patch)
	}
	if _, err := os.Stat(dshPatchPath() + ".bak-openknowledge"); err != nil {
		t.Fatal("应生成 .bak-openknowledge 备份")
	}
}

func TestDSHRemoveHooks(t *testing.T) {
	setupDSH(t)
	a := dshAgent{}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("无配置 RemoveHooks = %v, %v", removed, err)
	}
	pre := "- insert:\n    - id: my-plugin\n      name: 'D:/x/my.js'\n"
	if err := os.WriteFile(dshPatchPath(), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("插件文件应被删除")
	}
	patch := readDSHPatch(t)
	if strings.Contains(patch, "ok-hooks") {
		t.Fatalf("patch 不应残留 ok 行: %q", patch)
	}
	if !strings.Contains(patch, "id: my-plugin") {
		t.Fatalf("第三方行应保留: %q", patch)
	}
	if a.HooksInstalled() {
		t.Fatal("移除后 HooksInstalled 应为假")
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("重复 RemoveHooks = %v, %v", removed, err)
	}
}

func TestDSHRemoveKeepsForeignPlugin(t *testing.T) {
	setupDSH(t)
	if err := os.MkdirAll(filepath.Dir(dshPluginPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// someone else's plugin\n"
	if err := os.WriteFile(dshPluginPath(), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := (dshAgent{}).RemoveHooks()
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dshPluginPath()); string(data) != foreign {
		t.Fatal("非自家插件不应被删")
	}
	_ = removed
}

func TestDSHEnsureHooks(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	a := dshAgent{}

	// 从未安装 → no-op（不创建任何文件）
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("从未安装时 EnsureHooks 不应创建插件")
	}
	if _, err := os.Stat(dshPatchPath()); !os.IsNotExist(err) {
		t.Fatal("从未安装时 EnsureHooks 不应创建 patch")
	}

	// 过期（旧 exe 路径）→ 重写为当前
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Fatal("旧 exe 路径应判为未安装/过期")
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("自愈后 HooksInstalled 应为真")
	}

	// 当前 → no-op
	beforePlugin, _ := os.ReadFile(dshPluginPath())
	beforePatch, _ := os.ReadFile(dshPatchPath())
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	afterPlugin, _ := os.ReadFile(dshPluginPath())
	afterPatch, _ := os.ReadFile(dshPatchPath())
	if string(beforePlugin) != string(afterPlugin) || string(beforePatch) != string(afterPatch) {
		t.Fatal("内容当前时 EnsureHooks 应 no-op")
	}

	// 显式移除 → 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("显式移除后 EnsureHooks 不应复活插件")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run 'TestDSHInstallWritesPatchLine|TestDSHInstallIdempotent|TestDSHInstallPreservesForeignPatch|TestDSHRemoveHooks|TestDSHRemoveKeepsForeignPlugin|TestDSHEnsureHooks' -v`
Expected: FAIL（patch 文件不存在 / RemoveHooks no-op / EnsureHooks no-op）

- [ ] **Step 3: 实现 patch 管理**

`deepharness.go` 中：

新增 helper:

```go
// removeDSHMarkerBlock 从 patch 内容移除 ok 标记块，返回 (新内容, 是否移除)。
func removeDSHMarkerBlock(content string) (string, bool) {
	i := strings.Index(content, MarkerBegin)
	j := strings.Index(content, MarkerEnd)
	if i < 0 || j <= i {
		return content, false
	}
	tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
	head := strings.TrimRight(content[:i], "\n")
	out := head
	if strings.TrimSpace(tail) != "" {
		if out != "" {
			out += "\n"
		}
		out += "\n" + tail
	}
	out = strings.TrimLeft(out, "\n")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, true
}
```

`InstallHooks` 末尾（`return os.WriteFile(path, ...)` 一行）改为:

```go
	if err := os.WriteFile(path, []byte(renderDSHPlugin(exe)), 0o644); err != nil {
		return err
	}
	// patch 行（标记块幂等 upsert；# 标记在 YAML 是合法注释；UpsertHooksBlock
	// 的 StripLegacyOKHooks 只认 TOML [[hooks]] 表，对 YAML 是安全 no-op）
	patch := dshPatchPath()
	if data, err := os.ReadFile(patch); err == nil {
		_ = os.WriteFile(patch+".bak-openknowledge", data, 0o644)
	}
	return UpsertHooksBlock(patch, dshPatchBlock())
```

`HooksInstalled` 改为（**勘误**：brief 原版缺 exe 时效判定，与 `TestDSHEnsureHooks` 自相矛盾；实现者按 zcode/claude/codex 惯例补 `os.Executable()`+`EvalSymlinks` 基准的全文比对，评审已核验批准——以下为落地版）:

```go
func (dshAgent) HooksInstalled() bool {
	data, err := os.ReadFile(dshPluginPath())
	if err != nil {
		return false
	}
	content := string(data)
	if !strings.Contains(content, dshPluginMarker) ||
		!strings.Contains(content, "// fingerprint: "+dshTemplateFingerprint()) {
		return false
	}
	// 旧 exe 路径视为过期（与 zcodeAgent 同款，以解析后的当前可执行文件为基准）
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if content != renderDSHPlugin(exe) {
		return false
	}
	patch, err := os.ReadFile(dshPatchPath())
	return err == nil && strings.Contains(string(patch), "id: ok-hooks")
}
```

`RemoveHooks` / `EnsureHooks` 替换为:

```go
func (dshAgent) RemoveHooks() (bool, error) {
	removed := false
	path := dshPluginPath()
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return false, err
	case strings.Contains(string(data), dshPluginMarker):
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("删除 dsh 插件: %w", err)
		}
		removed = true
	}
	patch := dshPatchPath()
	data, err = os.ReadFile(patch)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return removed, err
	default:
		if out, ok := removeDSHMarkerBlock(string(data)); ok {
			if err := os.WriteFile(patch, []byte(out), 0o644); err != nil {
				return removed, fmt.Errorf("移除 patch 行: %w", err)
			}
			removed = true
		}
	}
	return removed, nil
}

// EnsureHooks 自愈：仅在曾安装（patch 标记块存在或插件文件为自家）且内容过期
// 时整体重写；从未安装 / 经 RemoveHooks 显式移除（两者均不在）不复活。
func (dshAgent) EnsureHooks(exe string) error {
	pluginData, pluginErr := os.ReadFile(dshPluginPath())
	patchData, patchErr := os.ReadFile(dshPatchPath())
	ours := (pluginErr == nil && strings.Contains(string(pluginData), dshPluginMarker)) ||
		(patchErr == nil && strings.Contains(string(patchData), MarkerBegin))
	if !ours {
		return nil
	}
	if pluginErr == nil && string(pluginData) == renderDSHPlugin(exe) &&
		patchErr == nil && strings.Contains(string(patchData), "id: ok-hooks") {
		return nil
	}
	return dshAgent{}.InstallHooks(exe)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentx/ -v`
Expected: 全部 PASS（含既有 9 个适配器的测试不受影响）

- [ ] **Step 5: Commit（先经用户确认）**

```bash
git add internal/agentx/deepharness.go internal/agentx/deepharness_test.go
git commit -m "feat(agentx): dsh cordis.patch.yml marker-block management with self-heal"
```

---

### Task 4: 注册表遍历测试同步更新

**Files:**
- Modify: `internal/gui/api_test.go:155` 与 `:40`（`newEnv`）
- Modify: `internal/setupx/setupx_test.go`（所有含 `OK_REASONIX_HOME` 的 Setenv 清单）
- Modify: `internal/setupx/uninstall_test.go:32`
- Modify: `internal/cli/setup_test.go`、`internal/cli/cli_test.go`、`internal/cli/propose_test.go`、`internal/daemon/server_test.go`（各自的隔离清单）

**Interfaces:**
- Consumes: Task 1 注册的 `dsh` 适配器（遍历注册表的测试会把真实 `~/.dsh` 当作已检测，必须隔离）

- [ ] **Step 1: 写失败测试（agent 计数）**

`internal/gui/api_test.go:155-157` 改为:

```go
	if len(res.Agents) != 10 {
		t.Fatalf("expected 10 agents, got %d: %s", len(res.Agents), data)
	}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestStatus -v`（计数断言所在测试名以实际为准，先 `grep -n "expected 9 agents" internal/gui/api_test.go` 定位所属函数再跑）
Expected: FAIL —— 若本机存在 `~/.dsh` 则 Detected 为真可能连带其他断言失败；计数本身在适配器注册后应为 10

- [ ] **Step 3: 全量补隔离 Setenv**

对上述 7 个文件中每一处出现 `t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))` 的位置，在其下一行追加:

```go
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
```

定位命令: `grep -rn "nonexistent-reasonix" internal/ cmd/`

注意 `internal/gui/api_test.go` 的 `newEnv` 与 `internal/setupx/setupx_test.go` 各有一处以上，逐处补齐。

- [ ] **Step 4: 跑全仓测试确认通过**

Run: `go test ./...`
Expected: 全绿

- [ ] **Step 5: Commit（先经用户确认）**

```bash
git add internal/gui/api_test.go internal/setupx/ internal/cli/ internal/daemon/server_test.go
git commit -m "test: isolate OK_DSH_HOME in registry-traversal tests; agent count 9->10"
```

---

### Task 5: 文档更新

**Files:**
- Modify: `docs/ARCHITECTURE.md` §9.2（适配器对比表加行 + dsh 段落）、§18.3（环境变量表加 `OK_DSH_HOME`）
- Modify: `README.md`、`README_EN.md`（多 agent 支持列表加 dsh）
- Modify: `web/help.md`（agent 列表如需）

- [ ] **Step 1: ARCHITECTURE.md §9.2 表格加行**

先读现状定位: `grep -n "opencode" docs/ARCHITECTURE.md`，在九适配器对比表（约 541-551 行）追加一行:

```
| dsh | 本地 JS 插件（cordis.patch.yml 绝对路径挂载） | `$DSH_HOME/plugins/openknowledge/index.js` + `$DSH_HOME/cordis.patch.yml` | `OK_DSH_HOME` > `DSH_HOME` |
```

（列结构以表格实际表头为准对齐。）

- [ ] **Step 2: ARCHITECTURE.md §9.2 追加 dsh 设计段落**

在 opencode 适配器段落后追加一段，内容要点：本地绝对路径挂载的薄桥 JS 插件（`agent/pre-step` → prompt 注入、`tools/post-execute` → 追踪、`agent/turn-stopping` → `steer()` 续跑）；复用 `UpsertHooksBlock` 标记块管理 patch 行；技能共享 `SkillsHome`；execFile 直 exec 不经沙箱执行器。3-6 行，风格对齐既有段落。

- [ ] **Step 3: ARCHITECTURE.md §18.3 环境变量表加行**

定位: `grep -n "OK_REASONIX_HOME" docs/ARCHITECTURE.md`，在同表追加 `OK_DSH_HOME` 行（说明：dsh 家目录测试隔离口）。

- [ ] **Step 4: README 双语更新**

定位: `grep -n -i "opencode\|qoder" README.md README_EN.md`，在多 agent 列表/setup 说明处加 dsh（DeepSeek Harness），中英文对应。

- [ ] **Step 5: web/help.md**

定位: `grep -n "agent" web/help.md`，在 agent 列表处加 dsh（若列表存在）。

- [ ] **Step 6: 校验构建**

Run: `go build ./... && go vet ./internal/agentx/`
Expected: 无输出（通过）

- [ ] **Step 7: Commit（先经用户确认）**

```bash
git add docs/ARCHITECTURE.md README.md README_EN.md web/help.md
git commit -m "docs: dsh adapter in architecture, readme and help"
```

---

### Task 6: 实机验证（对真实 DSH 仓库）与 spec 风险项回写

**Files:**
- Modify: `docs/superpowers/specs/2026-08-14-deepseek-harness-adapter-design.md` §9（回写实测结论）

**Interfaces:**
- Consumes: Task 1-5 全部产物；本机 `D:\develop\deepseek-harness1`（DSH 源码，pnpm monorepo）

- [ ] **Step 1: 构建并沙箱安装**

```bash
go build -o dist/ok.exe ./cmd/ok
export OK_DSH_HOME=/d/temp/dsh-sandbox   # 沙箱家目录，不污染真实 ~/.dsh
mkdir -p "$OK_DSH_HOME"
./dist/ok.exe setup --agent dsh --yes    # flag 以 setup.go 实际为准
cat "$OK_DSH_HOME/cordis.patch.yml"      # 人工核对标记块与插件路径
```

Expected: patch 文件含 `id: ok-hooks` 行；`$OK_DSH_HOME/plugins/openknowledge/index.js` 存在且含烘焙路径。

- [ ] **Step 2: 验证 DSH 加载该插件**

```bash
cd /d/develop/deepseek-harness1
DSH_HOME=/d/temp/dsh-sandbox pnpm dsh --dump-config 2>&1 | grep -A3 ok-hooks
```

Expected: dump 中出现 ok-hooks 行 = 行语法被接受。若报插件加载错误（如纯 `.js` 单文件不可加载），按 spec §9.4 预案补最小 `package.json`（`{"type":"module"}`）到插件目录并重验——相应改动回写 Task 2 的 `InstallHooks`（多写一个文件）与本计划。

- [ ] **Step 3: 跑真实会话验证三事件**

```bash
DSH_HOME=/d/temp/dsh-sandbox pnpm dsh web
```

在 Web UI 里：发一条 prompt（应见知识注入）、让 agent 写一个文件（ok 侧 `ok.log`/触文件记录应有 post-tool 痕迹）、结束回合（auto 模式应触发自省提醒）。每步失败先查 DSH 控制台插件异常与 `ok.log`。

- [ ] **Step 4: 回写 spec §9**

把 §9 五项风险逐条标注实测结论（已验证/降级方案），用 Edit 更新 spec。

- [ ] **Step 5: Commit（先经用户确认）**

```bash
git add docs/superpowers/specs/2026-08-14-deepseek-harness-adapter-design.md
git commit -m "docs: record dsh live-verification results in spec"
```

---

## Self-Review 记录

- **Spec 覆盖**:§3 两文件 → Task 1/2；§4 方法语义 → Task 1-3；§5 插件 → Task 2；§6 回调链路 → 零改动（插件走 `ok hook` 既有链路，Task 6 实机验证）；§7 技能 → Task 1 `SkillsDir` + Task 6 顺带验证；§8 测试 → Task 1-4；§9 风险项 → Task 6（其中沙箱项已由设计消解：execFile 直 exec 不经 `ctx.shell` 沙箱执行器；`steer()` 签名/工具名/UserMessage 结构已在本计划 Task 2 核实并写入注释）；§10 文案 → Task 5；§11 不做项 → 无对应任务，符合。
- **占位符扫描**:Task 6 的 Step 2 含条件预案（加载失败时补 package.json），为外部依赖实测分支，非占位符；其余步骤代码完整。
- **类型一致性**:`DSHHome`/`dshPluginPath`/`dshPatchPath`/`renderDSHPlugin`/`dshTemplateFingerprint`/`dshPatchBlock`/`removeDSHMarkerBlock`/`dshAgent`/`dshPluginMarker`/`setupDSH`/`readDSHPatch` 在 Task 1-4 间引用一致。
