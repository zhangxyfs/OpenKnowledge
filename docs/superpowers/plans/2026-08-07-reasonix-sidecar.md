# Reasonix sidecar 集成实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 OpenKnowledge 接入 Reasonix agent——以 Extension Protocol sidecar 实现逐 prompt 检索注入与强制检查，插件包登记进 Reasonix 信任门，GUI 引导页提供 enforce 三档开关。

**Architecture:** ok.exe 新增 `extension-serve` 子命令（Reasonix 宿主 exec 拉起，NDJSON/JSON-RPC over stdio），拦截 `input.receive`（注入 + enforce）与 `tool.after`（touched 追踪）；安装面走 agentx 注册表新适配器（写 `<reasonixHome>/plugins/openknowledge/` + `plugin-packages.json`）；检索/enforce 核心从 hook 包抽出共用。

**Tech Stack:** Go 1.25（CGO_ENABLED=0）；vendored Reasonix Go SDK（仅标准库）；TOML 全局配置；原生 JS SPA（web/ → dist/web 同步拷贝）。

**Spec:** `docs/superpowers/specs/2026-08-07-reasonix-sidecar-design.md`（已批准，目标 v2.5.0）

## Global Constraints

- **fail-open 铁律**：sidecar 拦截器任何内部错误/panic 一律 `Continue`，绝不阻断用户输入；错误只写 `~/.openknowledge/ok.log`
- **写入收敛铁律**：Reasonix 侧只写两个文件（插件 manifest、`plugin-packages.json`），写前 `.bak-openknowledge` 备份，State 用 temp+rename 原子写
- 注入内容包 `<ok-context>` 标签、前缀进用户输入文本（合规 Reasonix 缓存指引：动态内容在上下文尾部）
- vendored SDK 与上游 `D:\develop\DeepSeek-Reasonix\sdk\go` 保持逐字节一致（除包头 SYNC 注释），禁止就地改逻辑
- Reasonix 工具名事实：`write_file`/`edit_file`/`multi_edit`/`notebook_edit`，参数键 `path`
- 全局配置文件：`~/.openknowledge/config.toml`；新节 `[reasonix] enforce_mode`，合法值 `soft|hard|mixed`，缺省/非法一律按 `mixed`
- 版本号取 `openknowledge/internal/version.Version`（ldflags 注入，默认 "dev"）
- **工作区注意**：master 上有 v2.4.0（zcode）未提交 WIP。执行前与用户确认其已提交或接受共存；每个任务只 `git add` 本任务文件，绝不 `git add -A`
- 测试夹具惯例：`t.Setenv("OK_HOME", t.TempDir())` + `registry.Save`；包级 TestMain 必须隔离 agent home 环境变量（含新增 `OK_REASONIX_HOME`），绝不可触碰真实配置
- 全程中文注释/提交信息，风格对齐现有代码

---

### Task 1: hook 核心抽取（InjectForPrompt / TrackTouched / CheckStop）

纯重构：三个 Handler 从"逻辑全包"变为"stdin 解析 → 调核心 → 协议输出"的薄壳，行为零变化。sidecar（Task 3-6）将直接调用这些核心。

**Files:**
- Create: `internal/hook/core.go`
- Modify: `internal/hook/hook.go`（HandlePrompt 154-265、HandlePostTool 270-295、HandleStop 314-364 瘦身）
- Modify: `internal/hook/hook_test.go`（TestMain 增加 OK_REASONIX_HOME 隔离）
- Test: `internal/hook/core_test.go`（新建）

**Interfaces:**
- Consumes: 现有 `project.FromCwd`、`state.Load/Save`、`index.Open/Sync`、`retrieve.Terms`、`store.TruncateToBudget`、`enforce.EvalChangelog`、`registry.HooksDisabled`、`wikiNudge`（hook.go 内）、`relativize`（hook.go 内）
- Produces（Task 3-6 依赖，签名不得改）:
  ```go
  // core.go
  func InjectForPrompt(pc *project.Context, sessionID, cwd, promptText string) string
  func TrackTouched(pc *project.Context, sessionID, toolName, filePath string)
  func CheckStop(pc *project.Context, sessionID string) (reason string, isBlock bool)
  ```

- [ ] **Step 1: 跑基线测试**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/hook/ ./internal/agentx/ ./cmd/ok/`
Expected: 全部 PASS（记录基线；若有 v2.4.0 WIP 导致的失败，先与用户确认，不要顺手修）

- [ ] **Step 2: 写 core.go（完整代码如下）**

```go
package hook

// core.go 是 hook 三条链路的核心逻辑，与传输协议（kimi/zcode 的 stdin/stdout、
// reasonix 的 sidecar JSON-RPC）解耦：Handler 负责解析事件与格式化输出，
// 核心函数负责注入组装、文件追踪与强制检查。fail-open：内部错误仅记 ok.log。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/enforce"
	"openknowledge/internal/index"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/state"
	"openknowledge/internal/store"
)

// InjectForPrompt 组装 prompt 注入文本：会话首次基础注入（mandatory 全文 + 索引）
// + 每次检索注入 + wiki 落后提醒；外层截断到注入预算。
// 由 HandlePrompt（kimi/zcode/pi）与 rxext sidecar（reasonix）共用。
func InjectForPrompt(pc *project.Context, sessionID, cwd, promptText string) string {
	if registry.HooksDisabled() {
		return ""
	}
	var client embed.Client
	if key := pc.Config.Embedding.ResolvedAPIKey(); key != "" && pc.Config.Embedding.BaseURL != "" {
		client = &embed.OpenAIClient{
			BaseURL: pc.Config.Embedding.BaseURL,
			APIKey:  key,
			Model:   pc.Config.Embedding.Model,
			Timeout: time.Duration(pc.Config.Embedding.TimeoutSec) * time.Second,
		}
	}
	db, err := index.Open(pc.Store.KbPath())
	if err != nil {
		logErr("prompt open index: %v", err)
		return ""
	}
	defer db.Close()
	if err := db.Sync(pc.Store.KnowledgeDir(), client); err != nil {
		var corrupt *index.CorruptEntriesError
		switch {
		case errors.As(err, &corrupt):
			logErr("prompt sync index: %v", err)
		case client == nil:
			logErr("prompt sync index: %v", err)
			return ""
		default:
			logErr("prompt sync index with embedding: %v", err)
			if err2 := db.Sync(pc.Store.KnowledgeDir(), nil); err2 != nil {
				logErr("prompt sync index: %v", err2)
				if !errors.As(err2, &corrupt) {
					return ""
				}
			}
		}
	}
	st := state.Load(pc.Store.StateDir(), sessionID)
	var b strings.Builder
	if !st.BaseInjected {
		_ = state.Clean(pc.Store.StateDir(), 7*24*time.Hour)
		base := b.Len()
		mandatory, err := db.Mandatory()
		if err != nil {
			logErr("prompt mandatory: %v", err)
		}
		for _, h := range mandatory {
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", h.Title, h.Body)
		}
		if idx, err := os.ReadFile(pc.Store.IndexPath()); err == nil {
			b.Write(idx)
		}
		if b.Len() > base {
			st.BaseInjected = true
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("prompt save state: %v", err)
			}
		}
	}
	var queryVec []float32
	if client != nil {
		if vec, err := client.Embed(context.Background(), promptText); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			queryVec = vec
		}
	}
	hits, err := db.Query(retrieve.Terms(promptText), queryVec, pc.Config.Retrieve)
	if err != nil {
		logErr("prompt query: %v", err)
	}
	if len(hits) > 0 {
		b.WriteString("## 相关知识（需要全文时读取对应文件）\n\n")
		for _, h := range hits {
			p := filepath.ToSlash(filepath.Join(pc.Store.KnowledgeDir(), h.Filename))
			if h.Summary != "" {
				fmt.Fprintf(&b, "- **%s** (%s) — %s（%s）\n", h.Title, h.Type, h.Summary, p)
			} else {
				fmt.Fprintf(&b, "- **%s** (%s)（%s）\n", h.Title, h.Type, p)
			}
		}
		b.WriteString("\n")
	}
	out := store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens)
	if nudge := wikiNudge(pc, st, cwd); nudge != "" {
		out += nudge
	}
	return out
}

// TrackTouched 记录工具触碰的文件（相对项目根、小写、"/" 分隔）。
// 静默分支均记 ok.log（无 post-tool 日志 = 宿主未派发或进程被超时杀死）。
func TrackTouched(pc *project.Context, sessionID, toolName, filePath string) {
	if registry.HooksDisabled() {
		return
	}
	rel := relativize(pc, filePath)
	if rel == "" {
		logErr("post-tool skip: tool=%s path=%q 不在项目 %s 的路径内", toolName, filePath, pc.Project.Name)
		return
	}
	st := state.Load(pc.Store.StateDir(), sessionID)
	st.AddTouched(rel)
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("post-tool save state: %v", err)
	}
}

// CheckStop 评估 auto 自省提醒与 enforce 规则并维护回合计数。
// 返回 (reason, isBlock)：reason 空 = 放行；isBlock false = auto 自省提醒（软），
// true = enforce 规则命中（硬）。auto 提醒先于 enforce 评估（与既有 Stop 行为一致）。
func CheckStop(pc *project.Context, sessionID string) (string, bool) {
	if registry.HooksDisabled() {
		return "", false
	}
	if len(pc.Config.Enforce) == 0 && pc.Config.Capture.Mode != "auto" {
		return "", false
	}
	st := state.Load(pc.Store.StateDir(), sessionID)
	st.StopCount++
	interval := pc.Config.Capture.TurnInterval
	if interval <= 0 {
		interval = 1
	}
	if pc.Config.Capture.Mode == "auto" && len(st.Touched) > 0 &&
		st.StopCount-st.LastExtractReminder >= interval {
		st.LastExtractReminder = st.StopCount
		if err := st.Save(pc.Store.StateDir()); err != nil {
			logErr("stop save state: %v", err)
		}
		return "本会话修改过文件。请回顾是否有值得记录的经验（非显而易见的坑或解法），有则立即运行 ok propose 记录草稿条目；没有则继续。", false
	}
	for _, rule := range pc.Config.Enforce {
		if rule.Type != "changelog_required" {
			continue
		}
		if block, reason := enforce.EvalChangelog(rule, st); block {
			st.MarkBlocked(rule.Type)
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("stop save state: %v", err)
			}
			return reason, true
		}
	}
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("stop save state: %v", err)
	}
	return "", false
}
```

- [ ] **Step 3: hook.go 三个 Handler 变薄壳（完整替换）**

HandlePrompt 替换为：

```go
// HandlePrompt 解析 hook 事件并输出注入文本；核心逻辑见 InjectForPrompt。
// format 为 claude 时输出包成 Claude 协议 JSON（hookSpecificOutput），否则纯文本。
func HandlePrompt(r io.Reader, w io.Writer, format string) int {
	if registry.HooksDisabled() {
		return 0
	}
	selfHealHooks()
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("prompt parse: %v", err)
		return 0
	}
	promptText := ev.PromptText()
	if strings.TrimSpace(promptText) == "" {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	out := InjectForPrompt(pc, ev.SessionID, ev.Cwd, promptText)
	if strings.TrimSpace(out) != "" {
		if format == FormatClaude {
			writeClaudeContext(w, out)
		} else {
			fmt.Fprintln(w, out)
		}
	}
	return 0
}
```

HandlePostTool 替换为：

```go
// HandlePostTool 解析 hook 事件并记录触碰文件；核心逻辑见 TrackTouched。
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
	TrackTouched(pc, ev.SessionID, ev.ToolName, ev.FilePath())
	return 0
}
```

HandleStop 替换为（isBlock 在本 Handler 不区分——两种结果都走 stopBlock，行为与现状一致；isBlock 供 sidecar 三档分流用）：

```go
// HandleStop 解析 hook 事件，按 CheckStop 评估结果阻断：纯文本格式 stderr + exit 2
// （kimi/pi）；claude 格式 stdout decision:block JSON + exit 0。
func HandleStop(r io.Reader, stderr, stdout io.Writer, format string) int {
	if registry.HooksDisabled() {
		return 0
	}
	ev, err := ParseEvent(r)
	if err != nil {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	reason, _ := CheckStop(pc, ev.SessionID)
	if reason != "" {
		return stopBlock(stderr, stdout, format, reason)
	}
	return 0
}
```

清理 hook.go 中因抽取而不再使用的 import（`context`、`errors`、`embed`、`index`、`retrieve`、`store`、`enforce` 等——以 `go build` 报错为准逐个移除）。

- [ ] **Step 4: hook_test.go TestMain 补 OK_REASONIX_HOME 隔离**

在现有 `os.Setenv("OK_ZCODE_HOME", zcodeDir)` 之后追加：

```go
	reasonixDir, err := os.MkdirTemp("", "hook-test-reasonix-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("OK_REASONIX_HOME", reasonixDir)
```

- [ ] **Step 5: 验证重构零行为变化**

Run: `cd D:/develop/OpenKnowledge && gofmt -l internal/hook/ && go vet ./internal/hook/ && go test ./internal/hook/ ./cmd/ok/ -count=1`
Expected: gofmt 无输出；vet 无输出；测试与 Step 1 基线一致全 PASS

- [ ] **Step 6: core_test.go 直接覆盖核心（防回归）**

```go
package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/project"
	"openknowledge/internal/state"
)

func TestInjectForPromptBaseAndRetrieve(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	// 一条 mandatory 条目 + 一条普通条目（正文含独特词）
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\ntags: [\"mandatory\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: experience\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s1", projDir, "RetrievalQuirk 是什么")
	if !strings.Contains(out, "架构规约") {
		t.Errorf("首次注入应含 mandatory 全文，got: %q", out)
	}
	if !strings.Contains(out, "检索经验") {
		t.Errorf("注入应含检索命中条目，got: %q", out)
	}
	// 第二次：mandatory 不再重复，检索仍在
	out2 := InjectForPrompt(pc, "s1", projDir, "RetrievalQuirk 再问")
	if strings.Contains(out2, "永远先跑 gofmt") {
		t.Errorf("第二次注入不应重复 mandatory 全文，got: %q", out2)
	}
	if !strings.Contains(out2, "检索经验") {
		t.Errorf("第二次注入应仍含检索命中，got: %q", out2)
	}
}

func TestTrackTouchedAndCheckStopRemind(t *testing.T) {
	projDir, _ := setupProject(t)
	// 项目配置：auto 自省模式
	writeProjectConfig(t, projDir, "capture:\n  mode: auto\n  turn_interval: 1\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	TrackTouched(pc, "s2", "write_file", filepath.Join(projDir, "a.go"))
	st := state.Load(pc.Store.StateDir(), "s2")
	if len(st.Touched) != 1 || st.Touched[0] != "a.go" {
		t.Fatalf("touched 记录错误: %+v", st.Touched)
	}
	reason, isBlock := CheckStop(pc, "s2")
	if reason == "" || isBlock {
		t.Fatalf("auto 自省应返回软提醒，got (%q, %v)", reason, isBlock)
	}
	if !strings.Contains(reason, "ok propose") {
		t.Errorf("自省提醒文案应引导 ok propose，got: %q", reason)
	}
}
```

注：`writeEntry`/`writeProjectConfig` 若 hook_test.go 已有等价 helper 则复用（同名冲突时改用现有 helper 并调整调用）；没有则在 core_test.go 新增这两个 helper：

```go
func writeEntry(t *testing.T, kbRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "knowledge", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeProjectConfig(t *testing.T, projDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projDir, "ok.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

（项目配置文件名以 `project.FromCwd` 实际读取的为准——先看 `internal/project` 源码确认是 ok.yaml 还是别的名字，写错会导致配置不生效、测试假绿。）

- [ ] **Step 7: 跑测试**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/hook/ -count=1 -v -run "TestInjectForPrompt|TestTrackTouchedAndCheckStopRemind"`
Expected: PASS；随后 `go test ./... -count=1` 全仓绿

- [ ] **Step 8: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/hook/core.go internal/hook/hook.go internal/hook/core_test.go internal/hook/hook_test.go
git commit -m "refactor(hook): 抽取注入/追踪/强制检查核心供 sidecar 复用（行为不变）"
```

---

### Task 2: vendor Reasonix Go SDK

把 Reasonix 官方 SDK 原样纳入 ok 仓库，作为 sidecar 的传输/握手实现。零逻辑修改，便于后续对照上游同步。

**Files:**
- Create: `internal/rxext/sdk/sdk.go`、`internal/rxext/sdk/wire.go`、`internal/rxext/sdk/types_ext.go`、`internal/rxext/sdk/types_generated.go`（从 `D:\develop\DeepSeek-Reasonix\sdk\go\` 拷贝）
- Create: `internal/rxext/sdk/doc.go`（SYNC 说明）

**Interfaces:**
- Consumes: 仅标准库
- Produces（Task 3 依赖）:
  ```go
  // 包名保持 extension（上游原名，减少同步 diff）；import 路径 openknowledge/internal/rxext/sdk
  type Handler interface { Initialize(context.Context, InitializeParams) (*InitializeResult, error) }
  type InterceptorFunc func(ctx context.Context, event string, payload json.RawMessage) (*InterceptResult, error)
  type Options struct { Name, Version string; Interceptors map[string]InterceptorFunc /* 其余字段用零值 */ }
  func Serve(ctx context.Context, h Handler, opts Options) error
  func Continue() *InterceptResult
  func Block(reason string) *InterceptResult
  func Replace(payload any) (*InterceptResult, error)
  // InitializeParams.Session = SessionContext{SessionID, WorkspaceRoot string; Generation uint64}
  // InitializeResult{Subscriptions []string}
  ```

- [ ] **Step 1: 拷贝四个源文件**

Run:
```bash
mkdir -p D:/develop/OpenKnowledge/internal/rxext/sdk
cp D:/develop/DeepSeek-Reasonix/sdk/go/sdk.go D:/develop/DeepSeek-Reasonix/sdk/go/wire.go D:/develop/DeepSeek-Reasonix/sdk/go/types_ext.go D:/develop/DeepSeek-Reasonix/sdk/go/types_generated.go D:/develop/OpenKnowledge/internal/rxext/sdk/
```
Expected: 四个文件就位；**不拷** `*_test.go`、`examples/`、`go.mod`、`README.md`

- [ ] **Step 2: 写 doc.go（SYNC 说明）**

```go
// Package sdk 是 Reasonix Extension Protocol Go SDK 的 vendor 快照。
//
// 上游：D:\develop\DeepSeek-Reasonix\sdk\go（module github.com/esengine/DeepSeek-Reasonix/sdk/go）
// 同步方式：上游 sdk.go / wire.go / types_ext.go / types_generated.go 四个文件整文件覆盖
// 本目录同名文件（包名 extension 保持上游原名），然后跑 go build ./... 与
// go test ./internal/rxext/... 验证。禁止在本目录就地修改逻辑——改动请回上游。
//
// 快照日期：2026-08-07（上游 schema hash sha256:22338e66…，协议 major v1）
package extension
```

- [ ] **Step 3: 编译验证**

Run: `cd D:/develop/OpenKnowledge && go build ./internal/rxext/sdk/ && go vet ./internal/rxext/sdk/`
Expected: 无输出（编译通过；若有 import 路径引用上游 module 的行——预期没有，SDK 单包自洽——则报错并检查拷贝完整性）

- [ ] **Step 4: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/rxext/sdk/
git commit -m "feat(rxext): vendor Reasonix Extension Protocol Go SDK 快照（2026-08-07）"
```

---

### Task 3: sidecar 骨架 + `ok extension-serve` 子命令

最小可运行 sidecar：initialize 握手记录会话上下文，两个拦截器先返回 Continue（桩），CLI 注册子命令。后续任务逐个点亮拦截器。

**Files:**
- Create: `internal/rxext/serve.go`
- Create: `internal/rxext/serve_test.go`
- Modify: `cmd/ok/main.go`（注册 `extension-serve`）

**Interfaces:**
- Consumes: Task 2 的 SDK API；`openknowledge/internal/version.Version`
- Produces（Task 4-6 依赖）:
  ```go
  func Serve(ctx context.Context) error          // ok extension-serve 入口
  type handler struct{ sessionID, cwd string }   // initialize 填充
  func (h *handler) onInput(ctx context.Context, event string, payload json.RawMessage) (*extension.InterceptResult, error)
  func (h *handler) onToolAfter(ctx context.Context, event string, payload json.RawMessage) (*extension.InterceptResult, error)
  ```

- [ ] **Step 1: 写 serve_test.go 骨架测试（先红）**

```go
package rxext

import (
	"context"
	"testing"

	extension "openknowledge/internal/rxext/sdk"
)

func TestInitializeRecordsSession(t *testing.T) {
	h := &handler{}
	res, err := h.Initialize(context.Background(), extension.InitializeParams{
		Session: extension.SessionContext{SessionID: "sess-1", WorkspaceRoot: `D:\work\demo`, Generation: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.sessionID != "sess-1" || h.cwd != `D:\work\demo` {
		t.Errorf("会话上下文未记录: %+v", h)
	}
	if len(res.Subscriptions) != 2 {
		t.Errorf("应订阅 input.receive 与 tool.after，got: %v", res.Subscriptions)
	}
}

func TestStubInterceptorsContinue(t *testing.T) {
	h := &handler{sessionID: "s", cwd: "."}
	for _, fn := range []extension.InterceptorFunc{h.onInput, h.onToolAfter} {
		res, err := fn(context.Background(), "", []byte(`{}`))
		if err != nil || res == nil {
			t.Fatalf("拦截器错误: %v", err)
		}
		if res.Decision != extension.DecisionContinue {
			t.Errorf("桩拦截器应 Continue，got: %v", res.Decision)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ -run TestInitialize -count=1`
Expected: 编译失败（handler 未定义）

- [ ] **Step 3: 写 serve.go（骨架实现）**

```go
// Package rxext 实现 ok 的 Reasonix Extension Protocol sidecar：
// Reasonix 宿主以 exec form 拉起 `ok extension-serve`，经 NDJSON/JSON-RPC
// 下发 input.receive / tool.after 拦截。fail-open：任何内部错误一律 Continue。
package rxext

import (
	"context"
	"encoding/json"
	"fmt"

	"openknowledge/internal/version"
	extension "openknowledge/internal/rxext/sdk"
)

// Serve 是 `ok extension-serve` 的入口：跑 SDK 的 stdio 服务循环直到宿主关闭。
func Serve(ctx context.Context) error {
	h := &handler{}
	return extension.Serve(ctx, h, extension.Options{
		Name:    "openknowledge",
		Version: version.Version,
		Interceptors: map[string]extension.InterceptorFunc{
			"input.receive": h.onInput,
			"tool.after":    h.onToolAfter,
		},
	})
}

// handler 持有 initialize 握手确定的单会话上下文（一个 Reasonix 会话一个 sidecar 进程）。
type handler struct {
	sessionID string
	cwd       string
}

func (h *handler) Initialize(_ context.Context, p extension.InitializeParams) (*extension.InitializeResult, error) {
	h.sessionID = p.Session.SessionID
	h.cwd = p.Session.WorkspaceRoot
	return &extension.InitializeResult{
		Subscriptions: []string{"input.receive", "tool.after"},
	}, nil
}

// onInput input.receive 拦截器（Task 4/5 点亮注入与 enforce）。
func (h *handler) onInput(_ context.Context, _ string, _ json.RawMessage) (*extension.InterceptResult, error) {
	return extension.Continue(), nil
}

// onToolAfter tool.after 拦截器（Task 6 点亮 touched 追踪）。
func (h *handler) onToolAfter(_ context.Context, _ string, _ json.RawMessage) (*extension.InterceptResult, error) {
	return extension.Continue(), nil
}

// continueOnPanic 把 panic 折叠为 Continue（fail-open 铁律）。供拦截器 defer 使用：
//
//	func (h *handler) onInput(...) (res *extension.InterceptResult, err error) {
//		defer continueOnPanic(&res, &err)
//		...
//	}
func continueOnPanic(res **extension.InterceptResult, err *error) {
	if r := recover(); r != nil {
		*res = extension.Continue()
		*err = fmt.Errorf("rxext panic: %v", r)
	}
}
```

- [ ] **Step 4: 跑测试确认绿**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ -count=1`
Expected: PASS（2 个测试）

- [ ] **Step 5: main.go 注册子命令**

在 `cmd/ok/main.go` 的 switch 中（`case "hook":` 附近）加：

```go
	case "extension-serve":
		os.Exit(runExtensionServe())
```

并在 runHook 之后新增：

```go
// runExtensionServe Reasonix sidecar 入口：宿主经 stdio 驱动，退出码恒 0（fail-open）。
func runExtensionServe() int {
	if err := rxext.Serve(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "extension-serve:", err)
	}
	return 0
}
```

import 块加 `"context"`（若无）与 `"openknowledge/internal/rxext"`。用法行（106 行附近）的命令列表不加 `extension-serve`（同 `hook` 一样属内部子命令——确认 `hook` 也不在用法列表则保持一致）。

- [ ] **Step 6: 手动冒烟**

Run: `cd D:/develop/OpenKnowledge && go build -o dist/ok.exe ./cmd/ok && echo {"jsonrpc":"2.0","id":1,"method":"extension/shutdown","params":{"timeoutMillis":1000}} | ./dist/ok.exe extension-serve; echo exit=$?`
Expected: 进程正常退出 exit=0（initialize 前的 shutdown 属非法时序，SDK 可能报错——只要进程不挂住、退出码 0 即可）

- [ ] **Step 7: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/rxext/serve.go internal/rxext/serve_test.go cmd/ok/main.go
git commit -m "feat(rxext): extension-serve sidecar 骨架（initialize + 桩拦截器）"
```

---

### Task 4: input.receive 检索注入

点亮注入：复用 Task 1 的 `InjectForPrompt`，产出 `Replace` 把 `<ok-context>` 前缀进用户输入。

**Files:**
- Modify: `internal/rxext/serve.go`（onInput 实现 + selfHeal）
- Test: `internal/rxext/serve_test.go`

**Interfaces:**
- Consumes: `hook.InjectForPrompt(pc, sessionID, cwd, promptText) string`（Task 1）；`project.FromCwd`；`agentx.Detected()/EnsureHooks`（自愈）
- Produces: onInput 的 Replace 载荷形状 `{"text": "<ok-context>\n…\n</ok-context>\n\n<原文>"}`（Task 5 在此基础上加 enforce 分支，注入组装函数签名 `buildInputReplacement(prefix string, parts []string) (*extension.InterceptResult, error)`，见 Step 3）

- [ ] **Step 1: 写失败测试**

```go
func TestOnInputInjectsKnowledge(t *testing.T) {
	projDir, kbRoot := setupProject(t) // 复用 hook 包同款夹具（见 Step 4 说明）
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\ntags: [\"mandatory\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	h := &handler{sessionID: "s1", cwd: projDir}
	res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"项目规约是什么"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != extension.DecisionReplace {
		t.Fatalf("应 Replace，got: %v", res.Decision)
	}
	var rep struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(res.Replacement, &rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Text, "<ok-context>") || !strings.Contains(rep.Text, "架构规约") {
		t.Errorf("注入文本缺 ok-context 或知识内容: %q", rep.Text)
	}
	if !strings.HasSuffix(rep.Text, "项目规约是什么") {
		t.Errorf("原输入必须完整保留在尾部: %q", rep.Text)
	}
}

func TestOnInputFailOpen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	h := &handler{sessionID: "s", cwd: filepath.Join(home, "不存在的项目")}
	res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"hi"}`))
	if err == nil && res.Decision != extension.DecisionContinue {
		t.Errorf("项目解析失败应 Continue（fail-open），got: %v", res.Decision)
	}
	// 坏 payload 也不得 panic
	res2, _ := h.onInput(context.Background(), "input.receive", []byte(`不是JSON`))
	if res2 == nil || res2.Decision != extension.DecisionContinue {
		t.Errorf("坏 payload 应 Continue")
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ -run TestOnInput -count=1`
Expected: FAIL（onInput 仍是桩，DecisionContinue ≠ DecisionReplace）

- [ ] **Step 3: 实现 onInput（serve.go 完整替换该函数 + 新增辅助）**

```go
// onInput input.receive 拦截器：检索注入（replace 前缀 <ok-context>）。
// enforce 分支由 Task 5 加入。fail-open：panic/错误一律 Continue。
func (h *handler) onInput(_ context.Context, _ string, payload json.RawMessage) (res *extension.InterceptResult, err error) {
	defer continueOnPanic(&res, &err)
	res = extension.Continue()
	selfHealHooks()
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		return res, nil
	}
	pc, err := project.FromCwd(h.cwd)
	if err != nil {
		return res, nil
	}
	prefix := hook.InjectForPrompt(pc, h.sessionID, h.cwd, in.Text)
	if strings.TrimSpace(prefix) == "" {
		return res, nil
	}
	return buildInputReplacement(in.Text, []string{prefix})
}

// buildInputReplacement 把若干注入片段合并为一个 <ok-context> 块前缀进原输入。
func buildInputReplacement(original string, parts []string) (*extension.InterceptResult, error) {
	var b strings.Builder
	b.WriteString("<ok-context>\n")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		b.WriteString(strings.TrimRight(p, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString("</ok-context>\n\n")
	b.WriteString(original)
	return extension.Replace(map[string]string{"text": b.String()})
}

// selfHealHooks 逐 agent 自检 hooks/插件集成（如 ok.exe 迁移后重写登记）。fail-open。
func selfHealHooks() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	for _, a := range agentx.Detected() {
		_ = a.EnsureHooks(exe)
	}
}
```

import 追加：`"os"`、`"path/filepath"`、`"strings"`、`"openknowledge/internal/agentx"`、`"openknowledge/internal/hook"`、`"openknowledge/internal/project"`。

注意 `hook` 包 import 会引入其 init/依赖链——`go build ./...` 若报 import cycle 立即停手检查（hook 不 import rxext，不应成环）。

- [ ] **Step 4: 测试夹具对齐**

`setupProject`/`writeEntry` helper 在 rxext 包需要自己的一份（跨包不可复用 hook 的私有 helper）。在 serve_test.go 顶部加（与 hook/core_test.go 同款逻辑，另需 rxext 的 TestMain 隔离 `OK_REASONIX_HOME`/`KIMI_CODE_HOME`/`PI_CODING_AGENT_DIR`/`OK_ZCODE_HOME` 四个 agent home——selfHealHooks 会遍历 detected agents）：

```go
func TestMain(m *testing.M) {
	for i, env := range []string{"OK_REASONIX_HOME", "KIMI_CODE_HOME", "PI_CODING_AGENT_DIR", "OK_ZCODE_HOME"} {
		dir, err := os.MkdirTemp("", "rxext-test-"+string(rune('a'+i)))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Setenv(env, dir)
	}
	os.Exit(m.Run())
}

func setupProject(t *testing.T) (projDir, kbRoot string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	projDir = filepath.Join(home, "work", "demo")
	kbRoot = filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(filepath.Join(kbRoot, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(kbRoot, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "demo", Paths: []string{projDir}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	return projDir, kbRoot
}

func writeEntry(t *testing.T, kbRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "knowledge", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 5: 跑测试确认绿 + 回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ ./internal/hook/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/rxext/serve.go internal/rxext/serve_test.go
git commit -m "feat(rxext): input.receive 检索注入（<ok-context> 前缀，fail-open）"
```

---

### Task 5: input.receive enforce 三档（soft/hard/mixed）

加入强制检查：复用 `CheckStop`，按全局配置 `[reasonix] enforce_mode` 三档分流。本任务配置读取先用 `setupx.ReasonixEnforceMode()`——该函数在 Task 8 才实现，因此本任务**先写本地私有实现** `enforceMode()`（读同一配置键），Task 8 收编到 setupx 后替换调用。

**Files:**
- Modify: `internal/rxext/serve.go`（onInput 加 enforce 分支 + enforceMode 私有函数）
- Test: `internal/rxext/serve_test.go`

**Interfaces:**
- Consumes: `hook.CheckStop(pc, sessionID) (reason string, isBlock bool)`（Task 1）；`config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))` 读 `cfg.Reasonix.EnforceMode`（**Task 8 才加该字段**——本任务 Step 1 先加 config 字段，属配置面最小改动）
- Produces: 无新签名（onInput 行为变化）

- [ ] **Step 1: config 加 `[reasonix]` 节（最小改动，先行）**

`internal/config/config.go`：`Hooks` 结构附近加——

```go
// Reasonix 控制 reasonix sidecar 的强制检查表达方式。
type Reasonix struct {
	EnforceMode string `toml:"enforce_mode"` // soft|hard|mixed；缺省/非法按 mixed
}
```

`Config` 结构加字段 `Reasonix Reasonix \`toml:"reasonix"\``（与 `Hooks Hooks \`toml:"hooks"\`` 同段）。Default 不加显式值（空串即 mixed 语义）。

- [ ] **Step 2: 写失败测试（表驱动覆盖三档）**

```go
func TestOnInputEnforceModes(t *testing.T) {
	cases := []struct {
		mode        string
		wantDecision extension.InterceptDecision
	}{
		{"mixed", extension.DecisionBlock},   // changelog 硬阻断
		{"hard", extension.DecisionBlock},
		{"soft", extension.DecisionReplace},  // 软提醒前缀
		{"非法值", extension.DecisionBlock},   // 非法按 mixed
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			projDir, _ := setupProject(t)
			writeProjectConfig(t, projDir, "enforce:\n  - type: changelog_required\n")
			t.Setenv("OK_RX_ENFORCE_TEST_MODE", tc.mode) // 见 Step 4：测试注入口
			h := &handler{sessionID: "s1", cwd: projDir}
			// 先制造 touched（不经 tool.after，直接写 state）
			trackTouchedForTest(t, pc(t, projDir), "s1", filepath.Join(projDir, "main.go"))
			res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"继续"}`))
			if err != nil {
				t.Fatal(err)
			}
			if res.Decision != tc.wantDecision {
				t.Errorf("mode=%s 应 %v，got %v", tc.mode, tc.wantDecision, res.Decision)
			}
			if tc.wantDecision == extension.DecisionReplace {
				var rep struct{ Text string `json:"text"` }
				_ = json.Unmarshal(res.Replacement, &rep)
				if !strings.Contains(rep.Text, "CHANGELOG") && !strings.Contains(rep.Text, "changelog") {
					t.Errorf("软提醒应含 changelog 提示: %q", rep.Text)
				}
			}
		})
	}
}
```

（helper `trackTouchedForTest`/`pc`/`writeProjectConfig` 命名冲突时复用已有；enforce 规则的具体字段名与 EvalChangelog 的判定条件——如是否要求 touched 含代码文件、CHANGEOG 文件名——以 `internal/enforce/enforce.go` 与 `internal/config` 的 EnforceRule 定义为准，测试数据按真实判定条件构造。）

- [ ] **Step 3: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ -run TestOnInputEnforceModes -count=1`
Expected: FAIL（onInput 无 enforce 分支，全部 Continue/Replace）

- [ ] **Step 4: 实现 enforce 分支（onInput 完整替换 + enforceMode）**

onInput 在 `pc, err := project.FromCwd(h.cwd)` 成功后、注入组装**之前**插入 enforce 评估（block 优先于注入——设计 §4.1：阻断时不注入）：

```go
	reason, isBlock := hook.CheckStop(pc, h.sessionID)
	mode := enforceMode()
	if reason != "" {
		switch {
		case isBlock && mode != "soft":
			return extension.Block(reason), nil
		case !isBlock && mode == "hard":
			return extension.Block(reason), nil
		}
	}
	prefix := hook.InjectForPrompt(pc, h.sessionID, h.cwd, in.Text)
	var parts []string
	if reason != "" { // 软路径：提醒与注入合并为一个 ok-context 块
		parts = append(parts, reason)
	}
	if strings.TrimSpace(prefix) != "" {
		parts = append(parts, prefix)
	}
	if len(parts) == 0 {
		return res, nil
	}
	return buildInputReplacement(in.Text, parts)
```

新增私有函数（Task 8 收编到 setupx 后删除本地实现、改调 `setupx.ReasonixEnforceMode()`）：

```go
// enforceMode 读全局配置 [reasonix] enforce_mode：soft|hard|mixed，缺省/非法按 mixed。
// OK_RX_ENFORCE_TEST_MODE 是测试注入口（生产不设置）。
func enforceMode() string {
	if m := os.Getenv("OK_RX_ENFORCE_TEST_MODE"); m != "" {
		return normalizeEnforceMode(m)
	}
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return "mixed"
	}
	return normalizeEnforceMode(cfg.Reasonix.EnforceMode)
}

func normalizeEnforceMode(m string) string {
	switch m {
	case "soft", "hard":
		return m
	default:
		return "mixed"
	}
}
```

import 追加：`"openknowledge/internal/config"`、`"openknowledge/internal/registry"`。

- [ ] **Step 5: 跑测试确认绿 + 回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ ./internal/config/ -count=1 && go test ./... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/rxext/serve.go internal/rxext/serve_test.go internal/config/config.go
git commit -m "feat(rxext): input.receive enforce 三档（soft/hard/mixed，block 优先于注入）"
```

---

### Task 6: tool.after touched 追踪

**Files:**
- Modify: `internal/rxext/serve.go`（onToolAfter 实现）
- Test: `internal/rxext/serve_test.go`

**Interfaces:**
- Consumes: `hook.TrackTouched(pc, sessionID, toolName, filePath)`（Task 1）
- Produces: 无新签名

- [ ] **Step 1: 写失败测试**

```go
func TestOnToolAfterTracksTouched(t *testing.T) {
	projDir, _ := setupProject(t)
	h := &handler{sessionID: "s9", cwd: projDir}
	payload := fmt.Sprintf(`{"name":"write_file","arguments":%q,"result":"ok","isError":false}`,
		`{"path":"`+strings.ReplaceAll(filepath.Join(projDir, "b.go"), `\`, `\\`)+`"}`)
	res, err := h.onToolAfter(context.Background(), "tool.after", []byte(payload))
	if err != nil || res.Decision != extension.DecisionContinue {
		t.Fatalf("tool.after 应恒 Continue: %v %v", res, err)
	}
	st := state.Load(filepath.Join(t.TempDir(), "x"), "s9") // 仅占位——实际读法见下
	_ = st
}
```

注意：state 读取路径需经 `project.FromCwd(projDir)` 取 `pc.Store.StateDir()`（与 Task 4 夹具一致），上面占位行替换为：

```go
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	st := state.Load(pc.Store.StateDir(), "s9")
	if len(st.Touched) != 1 || st.Touched[0] != "b.go" {
		t.Fatalf("touched 记录错误: %+v", st.Touched)
	}
	// isError 与非写工具不记录
	h2 := &handler{sessionID: "s10", cwd: projDir}
	_, _ = h2.onToolAfter(context.Background(), "tool.after", []byte(`{"name":"write_file","arguments":"{\"path\":\"x.go\"}","isError":true}`))
	_, _ = h2.onToolAfter(context.Background(), "tool.after", []byte(`{"name":"bash","arguments":"{\"command\":\"ls\"}","isError":false}`))
	st2 := state.Load(pc.Store.StateDir(), "s10")
	if len(st2.Touched) != 0 {
		t.Errorf("isError/非写工具不应记录: %+v", st2.Touched)
	}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ -run TestOnToolAfter -count=1`
Expected: FAIL（桩不记录 touched）

- [ ] **Step 3: 实现 onToolAfter（完整替换）**

```go
// onToolAfter tool.after 拦截器：写文件工具成功执行后记录 touched。恒 Continue。
func (h *handler) onToolAfter(_ context.Context, _ string, payload json.RawMessage) (res *extension.InterceptResult, err error) {
	defer continueOnPanic(&res, &err)
	res = extension.Continue()
	var p struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		IsError   bool   `json:"isError"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.IsError {
		return res, nil
	}
	switch p.Name {
	case "write_file", "edit_file", "multi_edit", "notebook_edit":
	default:
		return res, nil
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(p.Arguments), &args); err != nil || args.Path == "" {
		return res, nil
	}
	pc, err := project.FromCwd(h.cwd)
	if err != nil {
		return res, nil
	}
	hook.TrackTouched(pc, h.sessionID, p.Name, args.Path)
	return res, nil
}
```

import 追加：`"openknowledge/internal/state"`（仅测试用则加在测试文件）。

- [ ] **Step 4: 跑测试确认绿**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/rxext/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/rxext/serve.go internal/rxext/serve_test.go
git commit -m "feat(rxext): tool.after touched 追踪（写工具成功执行才记录）"
```

---

### Task 7: agentx Reasonix 适配器

把 Reasonix 接入注册表：安装 = 写插件 manifest + 登记 plugin-packages.json；CLI/GUI/自愈/卸载自动获得支持（均遍历注册表）。

**Files:**
- Create: `internal/agentx/reasonix.go`
- Test: `internal/agentx/reasonix_test.go`

**Interfaces:**
- Consumes: `HookTimeoutSec()`（kimi.go）、`SkillsHome()`、`version.Version`、`Register`（agentx.go）
- Produces: `ReasonixHome() string`（导出，与 ZcodeHome 同级惯例）；`reasonixAgent` 实现 Agent 接口全部方法

- [ ] **Step 1: 写失败测试（覆盖装/删/自愈/损坏保护/外插件保留）**

```go
package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupReasonixHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_REASONIX_HOME", home)
	return home
}

const testExe = `D:\tools\ok.exe`

func TestReasonixInstallAndInstalled(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if !a.Detect() {
		t.Fatal("home 存在应 Detect")
	}
	if a.HooksInstalled() {
		t.Fatal("未安装前 HooksInstalled 应为 false")
	}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	// manifest 关键字段
	data, err := os.ReadFile(filepath.Join(home, "plugins", "openknowledge", "reasonix-plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mf map[string]any
	if err := json.Unmarshal(data, &mf); err != nil {
		t.Fatal(err)
	}
	rt, _ := mf["runtime"].(map[string]any)
	if rt["command"] != testExe {
		t.Errorf("runtime.command 错误: %v", rt["command"])
	}
	if rt["required"] != false {
		t.Errorf("required 必须为 false（宿主降级语义）: %v", rt["required"])
	}
	// state 登记
	st, err := os.ReadFile(filepath.Join(home, "plugin-packages.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(st), `"openknowledge"`) {
		t.Errorf("plugin-packages.json 未登记: %s", st)
	}
	if !a.HooksInstalled() {
		t.Error("安装后 HooksInstalled 应为 true")
	}
	// 幂等重装
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
}

func TestReasonixRemove(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if removed, _ := a.RemoveHooks(); removed {
		t.Error("未安装时 RemoveHooks 应返回 false")
	}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks: %v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(home, "plugins", "openknowledge")); !os.IsNotExist(err) {
		t.Error("插件目录应删除")
	}
	st, _ := os.ReadFile(filepath.Join(home, "plugin-packages.json"))
	if strings.Contains(string(st), `"openknowledge"`) {
		t.Error("state 条目应移除")
	}
	if a.HooksInstalled() {
		t.Error("移除后 HooksInstalled 应为 false")
	}
}

func TestReasonixEnsureRewritesStaleAndKeepsRemoved(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	// 从未安装：Ensure 不复活
	if err := a.EnsureHooks(testExe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "plugin-packages.json")); !os.IsNotExist(err) {
		t.Error("从未安装时 Ensure 不得创建 state")
	}
	// 安装后 exe 迁移：Ensure 重写
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	newExe := `D:\moved\ok.exe`
	if err := a.EnsureHooks(newExe); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "plugins", "openknowledge", "reasonix-plugin.json"))
	if !strings.Contains(string(data), `D:\moved\ok.exe`) {
		t.Error("exe 迁移后 manifest 应重写")
	}
	// 用户手动删 state 条目：Ensure 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(newExe); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(home, "plugin-packages.json"))
	if strings.Contains(string(st), `"openknowledge"`) {
		t.Error("用户显式移除后 Ensure 不得复活")
	}
}

func TestReasonixCorruptStateNotOverwritten(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(home, "plugin-packages.json")
	if err := os.WriteFile(sp, []byte("{损坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(testExe); err == nil {
		t.Fatal("损坏 state 应报错")
	}
	data, _ := os.ReadFile(sp)
	if string(data) != "{损坏" {
		t.Error("损坏文件不得被覆盖")
	}
}

func TestReasonixPreservesForeignPlugins(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	// 注入一个第三方插件条目
	sp := filepath.Join(home, "plugin-packages.json")
	data, _ := os.ReadFile(sp)
	var st map[string]any
	_ = json.Unmarshal(data, &st)
	plugins, _ := st["plugins"].([]any)
	st["plugins"] = append(plugins, map[string]any{"name": "other-plugin", "root": `D:\x`, "enabled": true})
	out, _ := json.Marshal(st)
	if err := os.WriteFile(sp, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(sp)
	if !strings.Contains(string(after), "other-plugin") {
		t.Error("第三方插件条目必须保留")
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/agentx/ -run TestReasonix -count=1`
Expected: 编译失败（reasonixAgent 未定义）

- [ ] **Step 3: 写 reasonix.go（完整实现）**

```go
package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"openknowledge/internal/version"
)

// ReasonixHome 返回 Reasonix 配置根目录：OK_REASONIX_HOME（测试口）>
// REASONIX_HOME > Windows %APPDATA%\reasonix（回退 ~/AppData/Roaming/reasonix）/
// 其他 ~/.reasonix——与 Reasonix 源码 internal/config/paths.go reasonixHomeDir 对应。
func ReasonixHome() string {
	if h := os.Getenv("OK_REASONIX_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("REASONIX_HOME"); h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil && dir != "" {
			return filepath.Join(dir, "reasonix")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Roaming", "reasonix")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".reasonix")
}

func reasonixPluginDir() string { return filepath.Join(ReasonixHome(), "plugins", "openknowledge") }
func reasonixStatePath() string { return filepath.Join(ReasonixHome(), "plugin-packages.json") }
func reasonixManifestPath() string {
	return filepath.Join(reasonixPluginDir(), "reasonix-plugin.json")
}

// reasonixAgent Reasonix 适配器：集成 = 安装 Extension Protocol 插件包
// （plugins/openknowledge/ + plugin-packages.json 信任门登记），
// 技能目录用共享 SkillsHome（Reasonix 全局扫描 ~/.agents/skills）。
type reasonixAgent struct{}

func init() { Register(reasonixAgent{}) }

func (reasonixAgent) ID() string          { return "reasonix" }
func (reasonixAgent) DisplayName() string { return "Reasonix" }
func (reasonixAgent) HooksTarget() string { return reasonixPluginDir() }
func (reasonixAgent) SkillsDir() string   { return SkillsHome() }

func (reasonixAgent) Detect() bool {
	info, err := os.Stat(ReasonixHome())
	return err == nil && info.IsDir()
}

// reasonixManifest 生成 manifest v1：runtime.command 直指 ok.exe（协议允许
// 插件根外绝对路径，exec form）；required=false——sidecar 崩溃宿主降级不阻断。
func reasonixManifest(exe string) map[string]any {
	return map[string]any{
		"apiVersion":  "reasonix.io/plugin/v1",
		"name":        "openknowledge",
		"version":     version.Version,
		"description": "OpenKnowledge 知识库 sidecar：逐 prompt 检索注入与经验沉淀",
		"contributes": map[string]any{},
		"runtime": map[string]any{
			"command":       exe,
			"args":          []any{"extension-serve"},
			"required":      false,
			"priority":      0,
			"timeoutMillis": HookTimeoutSec() * 1000,
			"intercepts":    []any{"input.receive", "tool.after"},
			"capabilities":  []any{"interceptors"},
		},
	}
}

// loadReasonixState 读 plugin-packages.json；不存在返回空 State，解析失败报错（不覆盖）。
func loadReasonixState() (map[string]any, error) {
	data, err := os.ReadFile(reasonixStatePath())
	if os.IsNotExist(err) {
		return map[string]any{"version": float64(1), "plugins": []any{}}, nil
	}
	if err != nil {
		return nil, err
	}
	st := map[string]any{}
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("reasonix plugin-packages.json 解析失败: %w", err)
	}
	return st, nil
}

// writeReasonixState 备份后原子写（temp+rename）。
func writeReasonixState(st map[string]any) error {
	path := reasonixStatePath()
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak-openknowledge", data, 0o644)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp-openknowledge"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// reasonixStatePlugins 取 plugins 数组（只读视图）。
func reasonixStatePlugins(st map[string]any) []any {
	plugins, _ := st["plugins"].([]any)
	return plugins
}

// findOKReasonixEntry 返回 ok 条目下标（无则 -1）：按 name=="openknowledge" 识别。
func findOKReasonixEntry(plugins []any) int {
	for i, p := range plugins {
		if pm, _ := p.(map[string]any); pm != nil && pm["name"] == "openknowledge" {
			return i
		}
	}
	return -1
}

// upsertOKReasonixEntry 追加或更新 ok 条目。
func upsertOKReasonixEntry(st map[string]any) {
	plugins := reasonixStatePlugins(st)
	entry := map[string]any{
		"name":         "openknowledge",
		"root":         reasonixPluginDir(),
		"version":      version.Version,
		"description":  "OpenKnowledge 知识库 sidecar",
		"manifestKind": "reasonix.io/plugin/v1",
		"enabled":      true,
	}
	if i := findOKReasonixEntry(plugins); i >= 0 {
		plugins[i] = entry
	} else {
		plugins = append(plugins, entry)
	}
	st["plugins"] = plugins
	if _, ok := st["version"]; !ok {
		st["version"] = float64(1)
	}
}

// removeOKReasonixEntry 移除 ok 条目，返回是否有改动。
func removeOKReasonixEntry(st map[string]any) bool {
	plugins := reasonixStatePlugins(st)
	i := findOKReasonixEntry(plugins)
	if i < 0 {
		return false
	}
	st["plugins"] = append(plugins[:i], plugins[i+1:]...)
	return true
}

// writeReasonixManifest 写插件 manifest（目录随建）。
func writeReasonixManifest(exe string) error {
	data, err := json.MarshalIndent(reasonixManifest(exe), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(reasonixPluginDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(reasonixManifestPath(), append(data, '\n'), 0o644)
}

// reasonixCurrent 报告插件登记与 manifest 是否均为当前期望形态
// （条目 enabled、root 正确；manifest command=exe、args/timeoutMillis 正确）。
func reasonixCurrent(st map[string]any, exe string) bool {
	i := findOKReasonixEntry(reasonixStatePlugins(st))
	if i < 0 {
		return false
	}
	entry, _ := reasonixStatePlugins(st)[i].(map[string]any)
	if enabled, _ := entry["enabled"].(bool); !enabled {
		return false
	}
	if root, _ := entry["root"].(string); root != reasonixPluginDir() {
		return false
	}
	data, err := os.ReadFile(reasonixManifestPath())
	if err != nil {
		return false
	}
	mf := map[string]any{}
	if err := json.Unmarshal(data, &mf); err != nil {
		return false
	}
	rt, _ := mf["runtime"].(map[string]any)
	if rt == nil {
		return false
	}
	cmd, _ := rt["command"].(string)
	timeout, _ := rt["timeoutMillis"].(float64)
	args, _ := rt["args"].([]any)
	return cmd == exe && timeout == float64(HookTimeoutSec()*1000) &&
		len(args) == 1 && args[0] == "extension-serve"
}

func (reasonixAgent) InstallHooks(exe string) error {
	st, err := loadReasonixState()
	if err != nil {
		return err
	}
	if err := writeReasonixManifest(exe); err != nil {
		return err
	}
	upsertOKReasonixEntry(st)
	return writeReasonixState(st)
}

func (reasonixAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(reasonixStatePath()); os.IsNotExist(err) {
		return false, nil
	}
	st, err := loadReasonixState()
	if err != nil {
		return false, err
	}
	changed := removeOKReasonixEntry(st)
	// 插件目录仅当内含 ok manifest 才删（防误删同名外来目录）
	if data, err := os.ReadFile(reasonixManifestPath()); err == nil {
		mf := map[string]any{}
		if json.Unmarshal(data, &mf) == nil && mf["name"] == "openknowledge" {
			if err := os.RemoveAll(reasonixPluginDir()); err == nil {
				changed = true
			}
		}
	}
	if !changed {
		return false, nil
	}
	if err := writeReasonixState(st); err != nil {
		return false, fmt.Errorf("移除 reasonix 插件登记: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：state 存在、曾登记过 ok 插件且内容过期时重写；
// 从未登记（无 ok 条目）则 no-op——用户显式移除的集成不复活。
func (reasonixAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(reasonixStatePath()); err != nil {
		return nil
	}
	st, err := loadReasonixState()
	if err != nil {
		return err
	}
	if findOKReasonixEntry(reasonixStatePlugins(st)) < 0 || reasonixCurrent(st, exe) {
		return nil
	}
	if err := writeReasonixManifest(exe); err != nil {
		return err
	}
	upsertOKReasonixEntry(st)
	return writeReasonixState(st)
}

func (reasonixAgent) HooksInstalled() bool {
	st, err := loadReasonixState()
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return reasonixCurrent(st, exe)
}
```

- [ ] **Step 4: 跑测试确认绿**

Run: `cd D:/develop/OpenKnowledge && gofmt -l internal/agentx/ && go test ./internal/agentx/ -count=1`
Expected: gofmt 无输出；全部 PASS（含既有 kimi/pi/zcode 测试）

- [ ] **Step 5: 验证注册表联动**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/setupx/ ./internal/gui/ -count=1`
Expected: PASS（setupx/uninstall/GUI 遍历注册表，新适配器自动纳入；若 gui 测试断言 agent 数量为固定值而失败，按测试语义更新预期——这是预期内联动）

- [ ] **Step 6: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/agentx/reasonix.go internal/agentx/reasonix_test.go internal/gui/api_test.go internal/setupx/
git commit -m "feat(agentx): Reasonix 适配器——插件包安装 + plugin-packages.json 信任门登记"
```

（`git add` 的 gui/setupx 仅当 Step 5 确有预期更新才包含。）

---

### Task 8: enforce_mode 配置收编 + GUI 三档开关

把 Task 5 的本地 `enforceMode()` 收编为 `setupx.ReasonixEnforceMode()`，GUI status 暴露当前值，新增保存端点，引导页条件渲染三档 radio。

**Files:**
- Modify: `internal/setupx/setupx.go`（`ReasonixEnforceMode`/`SaveReasonixEnforceMode`）
- Modify: `internal/rxext/serve.go`（enforceMode 改调 setupx，删本地 normalize 外的实现）
- Modify: `internal/gui/api.go`（status 加 `rxEnforceMode`；新端点）
- Modify: `web/index.html`（三档卡）、`web/app.js`（条件渲染 + 保存）
- Modify: `dist/web/index.html`、`dist/web/app.js`（与 web/ 同步拷贝）
- Test: `internal/gui/api_test.go`、`internal/setupx/setupx_test.go`

**Interfaces:**
- Consumes: Task 5 的 `cfg.Reasonix.EnforceMode`；`SaveHooksTimeout` 的 toml 写盘模式（setupx.go:110-126）
- Produces:
  ```go
  func ReasonixEnforceMode() string            // soft|hard|mixed（规范化后）
  func SaveReasonixEnforceMode(mode string) error // 非法值报错
  ```
  HTTP：`POST /api/reasonix/enforce-mode`，body `{"mode":"soft|hard|mixed"}`；status 响应新键 `rxEnforceMode`

- [ ] **Step 1: setupx 两个函数（仿 SaveHooksTimeout 写盘模式）**

```go
// ReasonixEnforceMode 返回 reasonix sidecar 的强制检查表达方式：
// 全局配置 [reasonix] enforce_mode（soft|hard|mixed），缺省/非法按 mixed。
func ReasonixEnforceMode() string {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return "mixed"
	}
	switch cfg.Reasonix.EnforceMode {
	case "soft", "hard":
		return cfg.Reasonix.EnforceMode
	default:
		return "mixed"
	}
}

// SaveReasonixEnforceMode 校验并写入全局配置 [reasonix] enforce_mode。
func SaveReasonixEnforceMode(mode string) error {
	switch mode {
	case "soft", "hard", "mixed":
	default:
		return fmt.Errorf("enforce_mode 必须是 soft|hard|mixed: %q", mode)
	}
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	cfg.Reasonix.EnforceMode = mode
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("全局配置编码失败: %w", err)
	}
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(globalPath, []byte(buf.String()), 0o600); err != nil {
		return fmt.Errorf("全局配置写入失败: %w", err)
	}
	return nil
}
```

rxext/serve.go 的 `enforceMode()` 改为：

```go
// enforceMode 读全局三档配置（OK_RX_ENFORCE_TEST_MODE 是测试注入口）。
func enforceMode() string {
	if m := os.Getenv("OK_RX_ENFORCE_TEST_MODE"); m != "" {
		return normalizeEnforceMode(m)
	}
	return setupx.ReasonixEnforceMode()
}
```

（`normalizeEnforceMode` 保留；删除对 config/registry 的直接依赖，import 换 `"openknowledge/internal/setupx"`——若 setupx import rxext 会成环，检查：setupx 不 import rxext，安全。）

- [ ] **Step 2: setupx 测试**

```go
func TestReasonixEnforceMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	if got := ReasonixEnforceMode(); got != "mixed" {
		t.Errorf("缺省应为 mixed，got %q", got)
	}
	if err := SaveReasonixEnforceMode("soft"); err != nil {
		t.Fatal(err)
	}
	if got := ReasonixEnforceMode(); got != "soft" {
		t.Errorf("保存后应为 soft，got %q", got)
	}
	if err := SaveReasonixEnforceMode("歪值"); err == nil {
		t.Error("非法值应报错")
	}
}
```

Run: `cd D:/develop/OpenKnowledge && go test ./internal/setupx/ ./internal/rxext/ -count=1`
Expected: PASS（rxext 既有三档测试经 OK_RX_ENFORCE_TEST_MODE 不受影响）

- [ ] **Step 3: GUI API——status 加键 + 新端点**

`api.go` status 组装处（302-318 行附近）`"hooksTimeout": hooksTimeout,` 之后加：

```go
		"rxEnforceMode":      setupx.ReasonixEnforceMode(),
```

mux 注册处（46-60 行附近）加：

```go
	mux.HandleFunc("POST /api/reasonix/enforce-mode", h.apiReasonixEnforceMode)
```

新 handler（放 apiSetupSkills 之后）：

```go
// apiReasonixEnforceMode 保存 reasonix sidecar 的强制检查表达方式（soft|hard|mixed）。
// sidecar 每条输入实时读配置，即时生效，无需重装插件。
func (h *Handler) apiReasonixEnforceMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := setupx.SaveReasonixEnforceMode(req.Mode); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": setupx.ReasonixEnforceMode()})
}
```

- [ ] **Step 4: api_test 覆盖**

```go
func TestReasonixEnforceModeAPI(t *testing.T) {
	h, _ := newTestHandler(t) // 复用 api_test.go 现有测试 handler 构造（名字以其为准）
	// status 默认 mixed
	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req) // 若测试走直接调 handler 函数则按其惯例
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["rxEnforceMode"] != "mixed" {
		t.Errorf("status 默认应为 mixed，got %v", body["rxEnforceMode"])
	}
	// POST 保存 hard
	post := httptest.NewRequest("POST", "/api/reasonix/enforce-mode", strings.NewReader(`{"mode":"hard"}`))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, post)
	if rec2.Code != 200 {
		t.Fatalf("保存失败: %d %s", rec2.Code, rec2.Body)
	}
	// 非法值 400
	bad := httptest.NewRequest("POST", "/api/reasonix/enforce-mode", strings.NewReader(`{"mode":"歪"}`))
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, bad)
	if rec3.Code != 400 {
		t.Errorf("非法值应 400，got %d", rec3.Code)
	}
}
```

（测试 handler 构造与鉴权令牌头以 api_test.go 现有惯例为准——先读其 TestMain/helper 再定稿。）

Run: `cd D:/develop/OpenKnowledge && go test ./internal/gui/ -count=1`
Expected: PASS

- [ ] **Step 5: 前端——index.html 三档卡**

`web/index.html` 引导页 hook 超时卡（84 行附近）之后加：

```html
      <div class="card" id="rx-enforce-card" hidden>
        <h3>强制检查方式</h3>
        <p class="card-desc muted">仅对 Reasonix 生效（sidecar 每条输入实时读取，即时生效）。auto 自省 = 经验沉淀提醒；changelog = 改了代码未更新日志的检查。</p>
        <div class="rx-enforce-options">
          <label><input type="radio" name="rx-enforce" value="mixed"> 软+硬（默认）：自省提醒、changelog 阻断输入</label>
          <label><input type="radio" name="rx-enforce" value="soft"> 全软提示：都以前缀提醒，不打断输入</label>
          <label><input type="radio" name="rx-enforce" value="hard"> 全硬阻断：都阻断输入</label>
        </div>
      </div>
```

- [ ] **Step 6: 前端——app.js 条件渲染与保存**

在 `renderAgentSelect`（448 行附近）末尾或状态渲染处（467 行附近 `renderAgentSelect(agents);` 之后）加可见性联动与初始值回填：

```js
  function renderRxEnforce() {
    var card = document.getElementById("rx-enforce-card");
    if (!card) return;
    var isRx = state.agent === "reasonix";
    card.hidden = !isRx;
    if (isRx) {
      var mode = (state.status && state.status.rxEnforceMode) || "mixed";
      var radios = document.getElementsByName("rx-enforce");
      for (var i = 0; i < radios.length; i++) {
        radios[i].checked = radios[i].value === mode;
      }
    }
  }
  renderRxEnforce();
```

radio 变更即保存（文件内合适的初始化位置，如超时卡保存按钮绑定 565 行附近）：

```js
  document.getElementsByName("rx-enforce").forEach(function (r) {
    r.addEventListener("change", function () {
      api("/api/reasonix/enforce-mode", { method: "POST", body: { mode: r.value } })
        .then(function () { toast("强制检查方式已保存：" + r.value); })
        .catch(function (e) { toast("保存失败：" + e.message); });
    });
  });
```

（`toast`/`api` helper 名以 app.js 现有实现为准——先读现有超时卡保存代码再落笔；若 agent 下拉 change 事件有独立 handler，也在其中调 `renderRxEnforce()`。）

同步 dist：`cp web/index.html web/app.js dist/web/`（webDir 安装态读 dist/web——以 daemon/run.go:70 的 webDir 实参确认为准）。

- [ ] **Step 7: 手动验证 GUI**

Run: `cd D:/develop/OpenKnowledge && go build -o dist/ok.exe ./cmd/ok && ./dist/ok.exe gui`
Expected: 引导页 agent 下拉选 Reasonix → 出现三档卡且默认选中"软+硬"；切到"全软提示"→ `~/.openknowledge/config.toml` 出现 `enforce_mode = "soft"`；切其他 agent → 卡隐藏

- [ ] **Step 8: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/setupx/setupx.go internal/setupx/setupx_test.go internal/rxext/serve.go internal/gui/api.go internal/gui/api_test.go web/index.html web/app.js dist/web/
git commit -m "feat(gui): Reasonix enforce 三档开关（引导页条件渲染，即时生效）"
```

---

### Task 9: 文档与真机验收

**Files:**
- Modify: `docs/ARCHITECTURE.md`（agentx 章节补 reasonix）
- Create: `docs/changelogs/2.5.0.md`

- [ ] **Step 1: ARCHITECTURE.md 更新**

在 agentx 段落补一段（风格对齐现有 kimi/pi/zcode 描述）：

```markdown
- **reasonix**：不写 settings.json hook（其 UserPromptSubmit 不注入 stdout），改为安装
  Extension Protocol 插件包——`plugins/openknowledge/reasonix-plugin.json`（runtime.command
  直指 ok.exe，`args=["extension-serve"]`，`required=false`）+ `plugin-packages.json` 信任门
  登记（备份 + temp+rename 原子写）。sidecar 拦截 input.receive（检索注入 + enforce 三档）
  与 tool.after（touched 追踪）；注入/检查核心与 hook 子命令共用 internal/hook/core.go。
  技能目录共享 SkillsHome。SDK 为 internal/rxext/sdk vendor 快照。
```

- [ ] **Step 2: changelog 2.5.0.md**

照 `docs/changelogs/2.4.0.md` 的既有格式新建 2.5.0.md：

```markdown
# v2.5.0

## 新 Agent：Reasonix

- 接入 Reasonix（Extension Protocol sidecar）：逐 prompt 检索注入、写文件追踪、
  强制检查（auto 自省 / changelog_required）全链路支持
- 安装：`ok setup --agent reasonix` 或 GUI 引导页一键安装（插件包 + 信任门登记，
  支持自愈与卸载清理）
- GUI 引导页新增"强制检查方式"三档开关（仅 Reasonix）：软+硬（默认）/ 全软提示 /
  全硬阻断，即时生效
```

（条目以实际交付为准增删。）

- [ ] **Step 3: 全量验证**

Run: `cd D:/develop/OpenKnowledge && gofmt -l . && go vet ./... && go test ./... -count=1 && go build -o dist/ok.exe ./cmd/ok`
Expected: gofmt/vet 无输出；全部测试 PASS；构建成功

- [ ] **Step 4: 真机验收（手动，对照设计 §9）**

1. `dist/ok.exe setup --agent reasonix` → 检查 `%APPDATA%\reasonix\plugins\openknowledge\reasonix-plugin.json` 与 `plugin-packages.json` 登记
2. 启动 Reasonix 新会话 → 首条输入见到 `<ok-context>` 注入（mandatory + 索引 + 检索）
3. 配置 `changelog_required` 后改代码不更 CHANGELOG → 下一条输入被 block（mixed）；GUI 切"全软提示"→ 变为前缀提醒，即时生效
4. `ok setup --agent reasonix --remove`（或卸载命令）→ 两文件清理干净，Reasonix 无报错
5. Reasonix 内 `/reload` → sidecar 正常重生，注入仍工作

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add docs/ARCHITECTURE.md docs/changelogs/2.5.0.md
git commit -m "docs: Reasonix sidecar 架构说明与 v2.5.0 changelog"
```

---

## Self-Review 记录

- **Spec 覆盖**：spec §4.1 sidecar→Task 3-6；§4.2 重构→Task 1；§4.3 适配器→Task 7；§4.4 GUI 三档→Task 8（config 字段提前到 Task 5 Step 1 落地）；§4.5 CLI 路由→Task 3 Step 5；§5 fail-open→各拦截器 continueOnPanic + Global Constraints；§6 缓存→Global Constraints；§7 测试→各任务测试 + Task 9；§9 验收→Task 9 Step 4。spec §2 vendor→Task 2。
- **已知留白**（实现时以源码为准，均有明确指引）：项目配置文件名（Task 1 Step 6）、enforce 规则判定细节（Task 5 Step 2）、api_test helper 名（Task 8 Step 4）、app.js 的 api/toast helper 名（Task 8 Step 6）。
- **类型一致性**：`InjectForPrompt/TrackTouched/CheckStop`（Task 1 定义，Task 4-6 消费）、`buildInputReplacement`（Task 4 定义，Task 5 复用）、`continueOnPanic`（Task 3 定义，Task 4/6 使用）、`ReasonixEnforceMode/SaveReasonixEnforceMode`（Task 8 定义，rxext 消费）、`normalizeEnforceMode`（Task 5 定义，Task 8 保留）——已逐一对齐。
