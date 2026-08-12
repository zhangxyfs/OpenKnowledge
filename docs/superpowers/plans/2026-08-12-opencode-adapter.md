# opencode 适配器实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 agentx 注册表新增第五个适配器 opencode——pi 式 go:embed TS 插件形态，实现 prompt 注入 / post-tool 追踪 / stop 沉淀闭环（nudge），技能分发零新机制。

**Architecture:** opencode 无 hooks 配置字段，hooks 形态是"全局插件目录的单文件 TS 插件返回 hooks 对象"。适配器写 `~/.config/opencode/plugins/openknowledge.ts`（头标记 + 模板 sha256 指纹管幂等/自愈，pi 同款机制）；插件三钩子（`chat.message` / `tool.execute.after` / `event: session.idle`）全部经 `Bun.spawn` 调 `ok hook <event>` 子进程（纯文本协议），Go 侧零改动。设计全文见 `docs/superpowers/specs/2026-08-12-opencode-adapter-design.md`（§11 已含全部实现事实核实结论）。

**Tech Stack:** Go 1.25.0（go.mod，无新依赖）；opencode 插件 = Bun 运行时单文件 TS（免 package.json/tsconfig）。

## Global Constraints

- fail-open 铁律：插件与 Go 侧任何单点失败不得阻断宿主会话，也不得影响其余 agent（收集式错误聚合）。
- 测试隔离铁律：任何遍历注册表的测试必须设 `OK_OPENCODE_HOME` 指向不存在路径（知识库 v2.5.0 pitfall：真实写入过用户配置）。
- 写入插件的 exe 路径一律 `filepath.ToSlash`（正斜杠，Windows 下 spawn 可用）。
- 提交信息：Conventional Commits + 中文摘要（参照 `git log` 现有风格）。
- 面向用户文案一律中文；`README_EN.md` / `site/assets/site.js` 维护对应英文。
- **不修改**（已验证数据驱动自发现/零改动）：`internal/agentx/agentx.go` 注册表机制、`internal/hook/`、`internal/daemon/`、`web/app.js`、`internal/gui/api.go`、`internal/setupx/setupx.go` 分发逻辑、`installer/`。
- 版本号提升与官网 changelog 条目属发布动作，不在本计划范围。

---

### Task 1: opencode 配置根解析

**Files:**
- Create: `internal/agentx/opencode.go`
- Test: `internal/agentx/opencode_test.go`

**Interfaces:**
- Consumes: 无（仅标准库）。
- Produces: `OpencodeHome() string`（四级配置根解析）、`opencodePluginPath() string`——Task 2 的适配器方法依赖这两个函数。

- [ ] **Step 1: 写失败测试**

创建 `internal/agentx/opencode_test.go`：

```go
package agentx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpencodeHomePrecedence(t *testing.T) {
	// OK_OPENCODE_HOME（ok 自留测试口）最优先
	t.Setenv("OK_OPENCODE_HOME", `D:\t\ok-port`)
	t.Setenv("OPENCODE_CONFIG_DIR", `D:\t\official`)
	t.Setenv("XDG_CONFIG_HOME", `D:\t\xdg`)
	if got := OpencodeHome(); got != `D:\t\ok-port` {
		t.Fatalf("OK_OPENCODE_HOME 应最优先, got %q", got)
	}

	// OPENCODE_CONFIG_DIR（opencode 官方覆盖）次之
	t.Setenv("OK_OPENCODE_HOME", "")
	if got := OpencodeHome(); got != `D:\t\official` {
		t.Fatalf("OPENCODE_CONFIG_DIR 次之, got %q", got)
	}

	// XDG_CONFIG_HOME/opencode 再次
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	if got, want := OpencodeHome(), filepath.Join(`D:\t\xdg`, "opencode"); got != want {
		t.Fatalf("XDG_CONFIG_HOME/opencode, got %q want %q", got, want)
	}

	// 默认 ~/.config/opencode
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	if got, want := OpencodeHome(), filepath.Join(home, ".config", "opencode"); got != want {
		t.Fatalf("默认 ~/.config/opencode, got %q want %q", got, want)
	}
}

func TestOpencodePluginPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_OPENCODE_HOME", home)
	want := filepath.Join(home, "plugins", "openknowledge.ts")
	if got := opencodePluginPath(); got != want {
		t.Fatalf("opencodePluginPath = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run TestOpencode -v`
Expected: FAIL（编译错误 `undefined: OpencodeHome` / `undefined: opencodePluginPath`）

- [ ] **Step 3: 实现**

创建 `internal/agentx/opencode.go`：

```go
package agentx

import (
	"os"
	"path/filepath"
)

// OpencodeHome 返回 opencode 全局配置目录。解析序：OK_OPENCODE_HOME（ok 自留
// 测试隔离口，OK_ZCODE_HOME 同款）> OPENCODE_CONFIG_DIR（opencode 官方覆盖，
// 见 opencode packages/core/src/global.ts 的 Flag.OPENCODE_CONFIG_DIR）>
// XDG_CONFIG_HOME/opencode > ~/.config/opencode（xdg-basedir 拼法，win32 无特判）。
func OpencodeHome() string {
	if h := os.Getenv("OK_OPENCODE_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("OPENCODE_CONFIG_DIR"); h != "" {
		return h
	}
	if h := os.Getenv("XDG_CONFIG_HOME"); h != "" {
		return filepath.Join(h, "opencode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "opencode")
}

// opencodePluginPath 插件写入目标：<配置根>/plugins/openknowledge.ts。
// opencode 对每个配置目录 glob {plugin,plugins}/*.{ts,js} 并直接 import 单文件
// （packages/opencode/src/config/plugin.ts:21-29），免 package.json/tsconfig。
func opencodePluginPath() string { return filepath.Join(OpencodeHome(), "plugins", "openknowledge.ts") }
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentx/ -run TestOpencode -v`
Expected: PASS（2 个测试）

- [ ] **Step 5: 提交**

```bash
git add internal/agentx/opencode.go internal/agentx/opencode_test.go
git commit -m "feat(agentx): opencode 配置根解析——OK_OPENCODE_HOME 测试口 + OPENCODE_CONFIG_DIR/XDG 优先级"
```

---

### Task 2: opencode 适配器（Agent 全方法 + TS 插件模板）

**Files:**
- Create: `internal/agentx/opencode_plugin.ts`
- Modify: `internal/agentx/opencode.go`（追加 embed、适配器类型与全方法）
- Modify: `internal/agentx/agentx.go:20`（SkillsDir 注释更正）
- Test: `internal/agentx/opencode_test.go`（追加行为测试）

**Interfaces:**
- Consumes: Task 1 的 `OpencodeHome()` / `opencodePluginPath()`；`agentx.go` 的 `Register` / `SkillsHome()`；`zcode_test.go:21-31` 已定义的 `currentExe(t)`（同包直接复用，**不要重复定义**）。
- Produces: `opencodeAgent`（注册进注册表，id `"opencode"`）；`opencodePluginMarker`、`opencodeTemplateFingerprint()`、`renderOpencodePlugin(exe)`（仅包内测试用）。注册后 Task 3 的计数断言与隔离才成立。

模板设计依据（全部已在 spec §11 核实）：opencode 写盘工具为 `write`/`edit`（参数 `filePath`）与 `apply_patch`（参数 `patchText`，gpt 系新模型与 write/edit 互斥，patch 内文件标记为 `*** Add/Update/Delete File:` 行）；`session.idle` 的 properties 为 `{ sessionID }`；nudge 用 `client.session.promptAsync`（立即返回）；**Bun.spawn 无内建 timeout，须手动 setTimeout + kill**；防循环插件侧不计数（pi 同款，靠 ok 侧 `CheckStop` 幂等语义）。

- [ ] **Step 1: 写 TS 插件模板**

创建 `internal/agentx/opencode_plugin.ts`（注意：Go 侧渲染时会在文件最前面拼头标记行与指纹行，模板本体从 import 开始）：

```ts
import { spawn } from "bun";
import path from "node:path";

const OK = "{{EXE}}";

// 与 pi 扩展一致的超时预算
const PROMPT_TIMEOUT_MS = 10000;
const HOOK_TIMEOUT_MS = 5000;

type OkResult = { code: number; stdout: string; stderr: string };

// runOk 调 `ok hook <event>` 子进程：stdin 喂 Claude 风格 snake_case JSON，
// 读 stdout/stderr/exit code。全程 fail-open——启动失败、超时、读写异常
// 一律解析为空结果，绝不拖累 opencode 会话。Bun.spawn 无内建 timeout，
// 超时手动 kill。
function runOk(args: string[], payload: unknown, timeoutMs: number): Promise<OkResult> {
  return new Promise((resolve) => {
    let proc;
    try {
      proc = spawn([OK, ...args], { stdin: "pipe", stdout: "pipe", stderr: "pipe" });
    } catch {
      resolve({ code: 0, stdout: "", stderr: "" });
      return;
    }
    let settled = false;
    const finish = (r: OkResult) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(r);
    };
    const timer = setTimeout(() => {
      try { proc.kill(); } catch { /* ignore */ }
      finish({ code: 0, stdout: "", stderr: "" });
    }, timeoutMs);
    (async () => {
      try {
        proc.stdin.write(JSON.stringify(payload));
        proc.stdin.end();
        const [code, stdout, stderr] = await Promise.all([
          proc.exited,
          new Response(proc.stdout).text(),
          new Response(proc.stderr).text(),
        ]);
        finish({ code, stdout, stderr });
      } catch {
        finish({ code: 0, stdout: "", stderr: "" });
      }
    })();
  });
}

let partSeq = 0;

// 旧式命名导出：opencode 把模块每个导出值当作插件函数（getLegacyPlugins）。
export const OpenKnowledgePlugin = async ({ directory, client }: any) => {
  return {
    // ≈ UserPromptSubmit：检索注入。ok 把注入文本写 stdout；
    // 以 synthetic text part 注入（不在 UI 当用户输入渲染，但进会话历史
    // 并参与本次 LLM 请求——output.parts 按引用传入且 hook 后继续使用）。
    "chat.message": async (input: any, output: any) => {
      try {
        const texts: string[] = [];
        for (const p of output.parts) {
          if (p && p.type === "text" && typeof p.text === "string" && p.text.trim()) {
            texts.push(p.text);
          }
        }
        const promptText = texts.join("\n").trim();
        if (!promptText) return;
        const r = await runOk(
          ["hook", "prompt"],
          {
            hook_event_name: "UserPromptSubmit",
            session_id: input.sessionID,
            cwd: directory,
            prompt: promptText,
          },
          PROMPT_TIMEOUT_MS,
        );
        const out = (r.stdout || "").trim();
        if (!out) return;
        output.parts.push({
          id: `part_ok_${Date.now()}_${partSeq++}`,
          messageID: output.message.id,
          sessionID: input.sessionID,
          type: "text",
          text: out,
          synthetic: true,
        });
      } catch { /* fail-open */ }
    },

    // ≈ PostToolUse(matcher Write|Edit)：记录触碰文件。opencode 的 gpt 系新模型
    // 用 apply_patch 替代 write/edit（registry 互斥），patchText 里的
    // *** Add/Update/Delete File: 行逐路径上报；相对路径按 directory 绝对化
    // （ok 侧 TrackTouched 只认项目根内的绝对路径）。
    "tool.execute.after": async (input: any) => {
      try {
        const paths: string[] = [];
        if (input.tool === "write" || input.tool === "edit") {
          const p = input.args?.filePath;
          if (typeof p === "string" && p) paths.push(p);
        } else if (input.tool === "apply_patch") {
          const text = input.args?.patchText;
          if (typeof text === "string") {
            for (const m of text.matchAll(/^\*\*\* (?:Add|Update|Delete) File:\s*(.+)$/gm)) {
              if (m[1]) paths.push(m[1].trim());
            }
          }
        }
        for (const p of paths) {
          await runOk(
            ["hook", "post-tool"],
            {
              hook_event_name: "PostToolUse",
              session_id: input.sessionID,
              cwd: directory,
              tool_name: input.tool,
              tool_input: { path: path.isAbsolute(p) ? p : path.join(directory, p) },
            },
            HOOK_TIMEOUT_MS,
          );
        }
      } catch { /* fail-open */ }
    },

    // ≈ Stop：auto 自省 / enforce。ok 以 exit 2 + stderr 表达"阻断"；opencode 的
    // session.idle 无法阻断已结束的回合，改为把 reason 作为用户消息补发回会话，
    // 驱动 agent 当场完成自省（与 pi 的 sendMessage(triggerTurn) 同构）。
    // 防重依赖 ok 侧 CheckStop 的 LastExtractReminder / MarkBlocked 语义，
    // 插件侧与 pi 一致不计数。promptAsync 立即返回，不等回合结束。
    event: async ({ event }: any) => {
      try {
        if (!event || event.type !== "session.idle") return;
        const sessionID = event.properties?.sessionID;
        if (!sessionID) return;
        const r = await runOk(
          ["hook", "stop"],
          { hook_event_name: "Stop", session_id: sessionID, cwd: directory },
          HOOK_TIMEOUT_MS,
        );
        const reason = (r.stderr || "").trim();
        if (r.code !== 2 || !reason) return;
        await client.session.promptAsync({
          path: { id: sessionID },
          body: { parts: [{ type: "text", text: reason }] },
        });
      } catch { /* fail-open */ }
    },
  };
};
```

- [ ] **Step 2: 追加失败测试**

向 `internal/agentx/opencode_test.go` 追加（`currentExe` 复用 `zcode_test.go` 的同名 helper）：

```go
import (
	// 追加到既有 import 块
	"strings"
)

// setupOpencode 隔离 opencode 配置根与 OK_HOME，返回 OpencodeHome。
func setupOpencode(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_OPENCODE_HOME", home)
	t.Setenv("OK_HOME", t.TempDir())
	return home
}

func readOpencodePlugin(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(opencodePluginPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOpencodeRegistered(t *testing.T) {
	a, ok := Find("opencode")
	if !ok {
		t.Fatal("opencode 应已注册")
	}
	if a.ID() != "opencode" || a.DisplayName() != "opencode" {
		t.Fatalf("ID/DisplayName = %q/%q", a.ID(), a.DisplayName())
	}
}

func TestOpencodeDetect(t *testing.T) {
	setupOpencode(t)
	if !(opencodeAgent{}).Detect() {
		t.Fatal("配置根存在应检测为真")
	}
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (opencodeAgent{}).Detect() {
		t.Fatal("配置根不存在应检测为假")
	}
}

func TestOpencodeSkillsDir(t *testing.T) {
	skills := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", skills)
	if got := (opencodeAgent{}).SkillsDir(); got != skills {
		t.Fatalf("SkillsDir 应共享 SkillsHome, got %q", got)
	}
}

func TestOpencodeInstallHooks(t *testing.T) {
	setupOpencode(t)
	exe := currentExe(t)
	a := opencodeAgent{}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	content := readOpencodePlugin(t)
	for _, want := range []string{
		opencodePluginMarker,
		"// fingerprint: " + opencodeTemplateFingerprint(),
		"const OK = \"" + filepath.ToSlash(exe) + "\"",
		`"chat.message"`,
		`"tool.execute.after"`,
		`"session.idle"`,
		"promptAsync",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("插件文件应包含 %q", want)
		}
	}
	if a.HooksTarget() != opencodePluginPath() {
		t.Fatalf("HooksTarget = %q", a.HooksTarget())
	}
	if !a.HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为真")
	}
}

func TestOpencodeInstallIdempotent(t *testing.T) {
	setupOpencode(t)
	exe := currentExe(t)
	a := opencodeAgent{}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	first := readOpencodePlugin(t)
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got := readOpencodePlugin(t); got != first {
		t.Fatal("重复安装内容应一致")
	}
}

func TestOpencodeInstallBacksUpForeign(t *testing.T) {
	setupOpencode(t)
	path := opencodePluginPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// user plugin\nexport const X = async () => ({})\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (opencodeAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(path + ".bak-openknowledge")
	if err != nil || string(bak) != foreign {
		t.Fatalf("应备份外部插件文件, err=%v", err)
	}
	if !strings.Contains(readOpencodePlugin(t), opencodePluginMarker) {
		t.Fatal("安装后应为 ok 插件内容")
	}
}

func TestOpencodeRemoveHooks(t *testing.T) {
	setupOpencode(t)
	a := opencodeAgent{}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("无文件 RemoveHooks = %v, %v", removed, err)
	}
	// 外部文件不删不动
	path := opencodePluginPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// user plugin\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("外部文件 RemoveHooks = %v, %v", removed, err)
	}
	if got, _ := os.ReadFile(path); string(got) != foreign {
		t.Fatal("外部文件不应被删除/改动")
	}
	// 本工具生成文件删除
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if removed, err := a.RemoveHooks(); err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("移除后插件文件应不存在")
	}
	if a.HooksInstalled() {
		t.Fatal("移除后 HooksInstalled 应为假")
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("重复 RemoveHooks = %v, %v", removed, err)
	}
}

func TestOpencodeEnsureHooks(t *testing.T) {
	setupOpencode(t)
	exe := currentExe(t)
	a := opencodeAgent{}
	path := opencodePluginPath()

	// 无文件 → no-op（不创建，显式移除不复活）
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("无文件时 EnsureHooks 不应创建")
	}

	// 外部文件 → 不动
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// user plugin\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != foreign {
		t.Fatal("外部文件 EnsureHooks 不应改动")
	}

	// 过期（旧 exe 路径渲染）→ 重写为当前
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got := readOpencodePlugin(t); !strings.Contains(got, "const OK = \""+filepath.ToSlash(exe)+"\"") {
		t.Fatal("自愈后应为当前 exe 渲染内容")
	}

	// 当前 → no-op（文件不再变化）
	before := readOpencodePlugin(t)
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got := readOpencodePlugin(t); got != before {
		t.Fatal("内容当前时 EnsureHooks 应 no-op")
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/agentx/ -run TestOpencode -v`
Expected: FAIL（编译错误 `undefined: opencodeAgent` / `opencodePluginMarker` 等）

- [ ] **Step 4: 实现 Go 适配器**

把 `internal/agentx/opencode.go` 的 import 块与文件内容补全为（Task 1 的两个函数保持不变，追加其余部分）：

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

//go:embed opencode_plugin.ts
var opencodePluginTemplate string

// opencodePluginMarker 本工具生成的插件文件头标记（RemoveHooks 据此识别归属）。
const opencodePluginMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"

// （Task 1 已有）OpencodeHome / opencodePluginPath 保持原样，不重复列出——
// 实施时保留 Task 1 代码，在其后追加以下内容。

// opencodeTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func opencodeTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(opencodePluginTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderOpencodePlugin 渲染插件：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderOpencodePlugin(exe string) string {
	body := strings.ReplaceAll(opencodePluginTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return opencodePluginMarker + "\n// fingerprint: " + opencodeTemplateFingerprint() + "\n" + body
}

// opencodeAgent opencode 适配器：hook 集成 = 写 TS 插件到 <配置根>/plugins/
// （opencode 无 hooks 配置字段，hooks 形态为插件返回 hooks 对象）；
// 技能共享 SkillsHome（opencode 原生扫描 ~/.agents/skills）。
type opencodeAgent struct{}

func init() { Register(opencodeAgent{}) }

func (opencodeAgent) ID() string          { return "opencode" }
func (opencodeAgent) DisplayName() string { return "opencode" }
func (opencodeAgent) SkillsDir() string   { return SkillsHome() }
func (opencodeAgent) HooksTarget() string { return opencodePluginPath() }

func (opencodeAgent) Detect() bool {
	info, err := os.Stat(OpencodeHome())
	return err == nil && info.IsDir()
}

func (opencodeAgent) HooksInstalled() bool {
	data, err := os.ReadFile(opencodePluginPath())
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, opencodePluginMarker) &&
		strings.Contains(content, "// fingerprint: "+opencodeTemplateFingerprint())
}

func (opencodeAgent) InstallHooks(exe string) error {
	path := opencodePluginPath()
	if data, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(data), opencodePluginMarker) {
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
	return os.WriteFile(path, []byte(renderOpencodePlugin(exe)), 0o644)
}

func (opencodeAgent) RemoveHooks() (bool, error) {
	path := opencodePluginPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(data), opencodePluginMarker) {
		return false, nil // 非本工具生成，不删
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("删除 opencode 插件: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：文件存在且为本工具生成、但内容与当前渲染结果不同（模板升级
// 或 exe 迁移）时重写；文件不存在为 no-op（opencode 无插件即不会触发 hook；
// 用户显式移除不复活）。
func (opencodeAgent) EnsureHooks(exe string) error {
	path := opencodePluginPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if !strings.Contains(string(data), opencodePluginMarker) {
		return nil
	}
	rendered := renderOpencodePlugin(exe)
	if string(data) == rendered {
		return nil
	}
	return os.WriteFile(path, []byte(rendered), 0o644)
}
```

同时更正 `internal/agentx/agentx.go:20` 的过时注释：

old:
```go
	SkillsDir() string            // 技能目录（当前均返回共享 SkillsHome）
```
new:
```go
	SkillsDir() string            // 技能目录（kimi/pi/reasonix/opencode 共享 SkillsHome；zcode 独立 ~/.zcode/skills）
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/agentx/ -v`
Expected: PASS（含 Task 1+2 全部 opencode 测试与既有 kimi/pi/zcode/reasonix 测试）

- [ ] **Step 6: 提交**

```bash
git add internal/agentx/opencode.go internal/agentx/opencode_plugin.ts internal/agentx/opencode_test.go internal/agentx/agentx.go
git commit -m "feat(agentx): opencode 适配器——TS 插件三钩子（chat.message 注入 / apply_patch 感知追踪 / idle nudge 闭环）"
```

---

### Task 3: 全仓测试隔离与 agents 计数

**Files:**
- Modify: `internal/gui/api_test.go:33`（newEnv 加隔离）、`:150`（计数 4→5）
- Modify: `internal/setupx/setupx_test.go:13,32`
- Modify: `internal/setupx/uninstall_test.go:25`
- Modify: `cmd/ok/integration_test.go:42`、`cmd/ok/daemon_test.go:52`

**Interfaces:**
- Consumes: Task 2 已注册的 `opencodeAgent`（注册表第 5 个成员）。
- Produces: 全仓 `go test ./...` 绿。

- [ ] **Step 1: 加隔离与计数**

① `internal/gui/api_test.go` newEnv 中，在

```go
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
```

之后插入：

```go
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
```

② 同文件计数断言：

old:
```go
	if len(res.Agents) != 4 {
		t.Fatalf("expected 4 agents, got %d: %s", len(res.Agents), data)
	}
```
new:
```go
	if len(res.Agents) != 5 {
		t.Fatalf("expected 5 agents, got %d: %s", len(res.Agents), data)
	}
```

③ `internal/setupx/setupx_test.go:13` 与 `:32` 两处，各在 `t.Setenv("OK_ZCODE_HOME", ...)` 行后插入：

```go
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
```

④ `internal/setupx/uninstall_test.go:25` 同样插入。

⑤ `cmd/ok/integration_test.go:42`，在 `"OK_REASONIX_HOME="+filepath.Join(home, "reasonix-nonexistent"),` 之后、`"OPENAI_API_KEY="` 之前插入：

```go
"OK_OPENCODE_HOME="+filepath.Join(home, "opencode-nonexistent"),
```

⑥ `cmd/ok/daemon_test.go:52` 同样插入。

- [ ] **Step 2: 全量测试**

Run: `go test ./...`
Expected: PASS（全部包）

- [ ] **Step 3: 提交**

```bash
git add internal/gui/api_test.go internal/setupx/setupx_test.go internal/setupx/uninstall_test.go cmd/ok/integration_test.go cmd/ok/daemon_test.go
git commit -m "test: 遍历注册表的测试补 OK_OPENCODE_HOME 隔离（gui/setupx/uninstall/E2E），agents 计数 4→5"
```

---

### Task 4: 文案与文档

**Files:**
- Modify: `internal/cli/setup.go:87`（名单动态化）、`:142-147`（guideText 泛化）
- Modify: `internal/cli/cli.go:441`（doctor 名单动态化）
- Modify: `web/index.html:159-161`（引导页"下一步"泛化）
- Modify: `README.md:43,94-95`、`README_EN.md:44,95-96`
- Modify: `docs/ARCHITECTURE.md` §9.2（注入形态表 + 适配器段落 + 接口注释引用）
- Modify: `site/assets/site.js:86`

**Interfaces:**
- Consumes: `setup.go` 既有 `agentIDs()`（`cli.go` 同包可直接调用）。
- Produces: 用户可见文案与文档覆盖 opencode。

- [ ] **Step 1: CLI 两处名单动态化（顺带还"写死 kimi/pi"的旧债）**

`internal/cli/setup.go:87`：

old:
```go
		fmt.Fprintln(stdout, "未检测到支持的 agent（kimi / pi），跳过 hooks 写入")
```
new:
```go
		fmt.Fprintf(stdout, "未检测到支持的 agent（%s），跳过 hooks 写入\n", agentIDs())
```

`internal/cli/cli.go:441`：

old:
```go
		fmt.Fprintln(stdout, "未检测到支持的 agent（kimi / pi / zcode）")
```
new:
```go
		fmt.Fprintf(stdout, "未检测到支持的 agent（%s）\n", agentIDs())
```

- [ ] **Step 2: 引导文案泛化（不写死 kimi）**

`internal/cli/setup.go:142-147`：

old:
```go
const guideText = `
下一步：
  1. 在需要知识库的项目目录运行 ok init（自动取当前目录名，或在 kimi 中说"初始化知识库"）
  2. 用 ok add 添加知识条目
  3. 新开 kimi 会话即可生效；ok off / ok on 可随时全局开关
`
```
new:
```go
const guideText = `
下一步：
  1. 在需要知识库的项目目录运行 ok init（自动取当前目录名，或在 agent 会话中说"初始化知识库"）
  2. 用 ok add 添加知识条目
  3. 新开 agent 会话即可生效；ok off / ok on 可随时全局开关
`
```

`web/index.html:159-161`：

old:
```html
          <li>在需要知识库的项目目录运行 <code>ok init</code>（自动取当前目录名，或在 kimi 中说"初始化知识库"）</li>
          <li>用 <code>ok add</code> 添加知识条目</li>
          <li>新开 kimi 会话即可生效；<code>ok off</code> / <code>ok on</code> 可随时全局开关</li>
```
new:
```html
          <li>在需要知识库的项目目录运行 <code>ok init</code>（自动取当前目录名，或在 agent 会话中说"初始化知识库"）</li>
          <li>用 <code>ok add</code> 添加知识条目</li>
          <li>新开 agent 会话即可生效；<code>ok off</code> / <code>ok on</code> 可随时全局开关</li>
```

- [ ] **Step 3: README 双语**

`README.md:43` 多 Agent 支持行：

old:
```markdown
| **多 Agent 支持** | Kimi Code、Pi、ZCode、Reasonix 共用同一套知识库（可扩展适配器架构）——kimi 走 TOML hooks 标记块，pi 走 TypeScript 扩展，zcode 走 Claude JSON 协议，reasonix 走 Extension Protocol sidecar |
```
new:
```markdown
| **多 Agent 支持** | Kimi Code、Pi、ZCode、Reasonix、opencode 共用同一套知识库（可扩展适配器架构）——kimi 走 TOML hooks 标记块，pi 走 TypeScript 扩展，zcode 走 Claude JSON 协议，reasonix 走 Extension Protocol sidecar，opencode 走 TypeScript 插件 hooks |
```

`README.md:94`（setup 第 1 条）：

old: `pi 是 TypeScript 扩展，zcode 是 ` + `` `config.json` `` + ` 合并写；` + `` `ok setup --agent <id>` `` + ` 可只装指定 agent`

new: `pi 是 TypeScript 扩展，zcode 是 ` + `` `config.json` `` + ` 合并写，opencode 是 ` + `` `~/.config/opencode/plugins/` `` + ` 的 TypeScript 插件；` + `` `ok setup --agent <id>` `` + ` 可只装指定 agent`

`README.md:95`（setup 第 2 条）：

old: `（kimi/pi 共享 ` + `` `~/.agents/skills/` `` + `，zcode 为 ` + `` `~/.zcode/skills` `` + `）`
new: `（kimi/pi/opencode 共享 ` + `` `~/.agents/skills/` `` + `，zcode 为 ` + `` `~/.zcode/skills` `` + `）`

`README_EN.md:44`：

old:
```markdown
| **Multi-agent support** | Kimi Code, Pi, ZCode and Reasonix share the same knowledge base (extensible adapter architecture) — kimi via TOML hook marker blocks, pi via a TypeScript extension, zcode via the Claude JSON protocol, reasonix via an Extension Protocol sidecar |
```
new:
```markdown
| **Multi-agent support** | Kimi Code, Pi, ZCode, Reasonix and opencode share the same knowledge base (extensible adapter architecture) — kimi via TOML hook marker blocks, pi via a TypeScript extension, zcode via the Claude JSON protocol, reasonix via an Extension Protocol sidecar, opencode via a TypeScript plugin |
```

`README_EN.md` setup 第 1 条：

old: `pi gets a TypeScript extension; zcode gets a merged ` + `` `config.json` `` + ` write. Use ` + `` `ok setup --agent <id>` `` + ` to target one agent only`
new: `pi gets a TypeScript extension; zcode gets a merged ` + `` `config.json` `` + ` write; opencode gets a TypeScript plugin in ` + `` `~/.config/opencode/plugins/` `` + `. Use ` + `` `ok setup --agent <id>` `` + ` to target one agent only`

`README_EN.md` setup 第 2 条：

old: `(kimi/pi share ` + `` `~/.agents/skills/` `` + `; zcode uses ` + `` `~/.zcode/skills` `` + `)`
new: `(kimi/pi/opencode share ` + `` `~/.agents/skills/` `` + `; zcode uses ` + `` `~/.zcode/skills` `` + `)`

（README.md:228 附近为"以 Kimi Code 为例……其他 agent 经各自适配器等价触发"的泛称表述，已核对无需改动。）

- [ ] **Step 4: ARCHITECTURE.md §9.2**

① 接口引用中的 SkillsDir 注释：

old: `    SkillsDir() string            // 技能目录（kimi/pi 共享 SkillsHome；zcode 为 ~/.zcode/skills）`
new: `    SkillsDir() string            // 技能目录（kimi/pi/reasonix/opencode 共享 SkillsHome；zcode 为 ~/.zcode/skills）`

② `四种注入形态：` → `五种注入形态：`

③ 注入形态表追加一行（接在 reasonix 行后）：

```markdown
| opencode | TypeScript 插件（三钩子：`chat.message` / `tool.execute.after` / `event: session.idle`） | `~/.config/opencode/plugins/openknowledge.ts`（`OK_OPENCODE_HOME` 优先，ok 自留测试口；`OPENCODE_CONFIG_DIR` / `XDG_CONFIG_HOME` 次之） | 头标记 + `// fingerprint:` 行与当前模板指纹一致 |
```

④ 在 reasonix 适配器段落之后追加：

```markdown
opencode 适配器（`opencode.go` + 内嵌模板 `opencode_plugin.ts`）：opencode 无 hooks 配置字段，其 hooks 形态是"插件文件返回 hooks 对象"——对每个配置目录 glob `{plugin,plugins}/*.{ts,js}` 单文件直接 import（Bun 原生跑 TS，免 package.json）。安装/幂等/自愈机制与 pi 同款（头标记 + 模板 sha256 前 12 位指纹 + 外部文件先备份 `.bak-openknowledge`；曾安装且过期才重写，显式移除不复活）。插件三钩子：`chat.message` ≈ UserPromptSubmit（`ok hook prompt` 纯文本 stdout 以 `synthetic:true` text part push 进 `output.parts` 注入——parts 按引用传入且 hook 后继续使用并持久化）；`tool.execute.after` ≈ PostToolUse（`write`/`edit` 取 `args.filePath`，`apply_patch` 从 `patchText` 解析 `*** Add/Update/Delete File:` 行——gpt 系新模型 apply_patch 与 write/edit 互斥，必须覆盖；相对路径按 directory 绝对化后逐路径调 `ok hook post-tool`）；`event: session.idle` ≈ Stop（exit 2 + stderr 时经 SDK `client.session.promptAsync` 把 reason 作为用户消息补发回该会话，驱动当场自省——idle 无法拒绝停止，与 pi 的 `sendMessage(triggerTurn)` 同构；防重靠 ok 侧 `CheckStop` 的 LastExtractReminder/MarkBlocked 语义，插件侧与 pi 一致不计数）。子进程走 `Bun.spawn` stdin pipe + 手动超时 kill（10s/5s/5s，Bun.spawn 无内建 timeout），全程 fail-open。技能共享 SkillsHome（opencode 原生扫描 `~/.agents/skills`，机制零改动）。
```

- [ ] **Step 5: 官网 site.js**

`site/assets/site.js:86`（已 grep 确认该枚举全站仅此一处）：

old:
```js
    'd.s1l1': '<b>Writes hook configurations</b> — covering every detected AI assistant: Kimi Code gets 3 hook marker blocks in <code>~/.kimi-code/config.toml</code> (backup + idempotent overwrite), Pi gets a TypeScript extension, ZCode gets a merged <code>config.json</code> write, Reasonix gets an Extension Protocol plugin package',
```
new:
```js
    'd.s1l1': '<b>Writes hook configurations</b> — covering every detected AI assistant: Kimi Code gets 3 hook marker blocks in <code>~/.kimi-code/config.toml</code> (backup + idempotent overwrite), Pi gets a TypeScript extension, ZCode gets a merged <code>config.json</code> write, Reasonix gets an Extension Protocol plugin package, opencode gets a TypeScript plugin in <code>~/.config/opencode/plugins/</code>',
```

- [ ] **Step 6: 验证并提交**

Run: `go build ./... && go test ./internal/cli/ ./internal/gui/`
Expected: PASS

```bash
git add internal/cli/setup.go internal/cli/cli.go web/index.html README.md README_EN.md docs/ARCHITECTURE.md site/assets/site.js
git commit -m "docs: opencode 接入文案与架构文档——cli 名单动态化、引导文案泛化、README/§9.2/官网补 opencode"
```

---

### Task 5: 全量验证、手动 E2E 与知识沉淀

**Files:**
- 无代码改动（知识库写操作经 ok CLI / 技能完成）

**Interfaces:**
- Consumes: Task 1-4 的全部产物。
- Produces: 全仓绿 + 安装链路实测结论 + 知识库沉淀（agentx wiki 条目更新 + pitfall 草稿）。

- [ ] **Step 1: 全量验证**

Run: `go vet ./... && go test ./...`
Expected: 无 vet 输出；全部 PASS

- [ ] **Step 2: 模拟安装 E2E（不依赖本机装 opencode）**

Git Bash 执行：

```bash
go build -o /d/tmp/ok-e2e/ok.exe ./cmd/ok
export OK_HOME=/d/tmp/ok-e2e/home OK_OPENCODE_HOME=/d/tmp/ok-e2e/opencode OK_SKILLS_HOME=/d/tmp/ok-e2e/skills
/d/tmp/ok-e2e/ok.exe setup --agent opencode   # embedding 交互直接回车跳过
```

Expected:
- 输出含 `hooks 配置已写入 D:\tmp\ok-e2e\opencode\plugins\openknowledge.ts`
- 输出含 `技能已安装到 D:\tmp\ok-e2e\skills`
- `cat /d/tmp/ok-e2e/opencode/plugins/openknowledge.ts`：首行为头标记，第二行 `// fingerprint:`，`const OK = "D:/tmp/ok-e2e/ok.exe"`（正斜杠）
- 再跑 `/d/tmp/ok-e2e/ok.exe doctor`：输出含 `[opencode] hooks 已安装`
- 重复执行 setup：插件内容不变（幂等）

- [ ] **Step 3: 真实 opencode 验证（可选，需本机已装 opencode）**

1. 用真实 `ok.exe setup --agent opencode` 安装（确认 `~/.config/opencode/plugins/openknowledge.ts` 生成）；
2. 在一个已 `ok init` 的项目目录启动 opencode，提问一个命中知识库关键词的问题——预期：模型上下文中出现注入知识（可先在 `~/.openknowledge/ok.log` 确认无报错）；
3. 让 opencode 改一个文件后结束回合——若 capture 为 auto 模式且达到轮次间隔，预期会话中收到一条以 `[OpenKnowledge]`/自省提醒为内容的补发消息（nudge）。

任一项不符合预期：停下来按 superpowers:systematic-debugging 排查，不要带病提交。

- [ ] **Step 4: 知识沉淀（项目纪律）**

- 结构型：更新知识库《多-Agent支持（agentx）》条目——注入形态补 opencode（TS 插件三钩子）、测试隔离纪律补 `OK_OPENCODE_HOME`（经 openknowledge-wiki 技能/同名 add --force 重写）。
- 经验型：经 openknowledge-propose 技能记 pitfall 草稿，素材两条：① opencode 的 `apply_patch` 与 `write`/`edit` 按模型互斥（gpt 系新模型只有 apply_patch），post-tool 追踪漏掉它就漏掉整个模型系的写盘追踪；② `Bun.spawn` 无内建 `timeout` 选项，须手动 `setTimeout` + `proc.kill()`，否则 hook 子进程挂死会拖住插件。

---

## Self-Review 记录（计划编制者已执行）

- **Spec 覆盖**：§3→Task 1/2；§4→Task 2；§5（TS 三钩子）→Task 2 Step 1（完整模板代码，含 §11 全部核实结论）；§6 零改动→Global Constraints；§7→Task 2（`SkillsDir()` 返回共享 SkillsHome）；§8→Task 2 测试 + Task 3 隔离；§9→Task 4；§10→Global Constraints；§11→已在 spec 内回填核实结论（2026-08-12）。
- **占位扫描**：无 TBD/TODO；所有代码步骤含完整代码；Task 2 Step 4 中"Task 1 代码保留不重复列出"为同文件追加说明，两函数全文在 Task 1 Step 3。
- **类型一致性**：`OpencodeHome` / `opencodePluginPath` / `opencodeAgent` / `opencodePluginMarker` / `opencodeTemplateFingerprint` / `renderOpencodePlugin` 命名跨任务一致；`currentExe` 明确标注复用自 `zcode_test.go`（同包），`setupOpencode` / `readOpencodePlugin` 为新定义无冲突。
