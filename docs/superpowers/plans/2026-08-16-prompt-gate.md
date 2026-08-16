# 泛化 prompt 门控（v2.16.0）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对"继续"/"好的"类泛化 prompt 跳过检索注入与 embed 调用（省 token 与网络往返），并在 GUI 引导页提供门控卡片 + 短语管理弹窗。

**Architecture:** 门控判定为 `internal/retrieve` 包的纯函数（内置短语表 + 用户 extra 追加层，归一化后精确匹配 / Terms 为空两分支）；在 `InjectForPrompt` 的 EmbedQuery 之前短路检索段，mandatory/INDEX/wiki nudge 不受影响。配置走 `[retrieve.gate]` 子表（`config.SetGate` 小节重写，SetCapture 同款算法）；GUI API 镜像 capture 的 get/set 对；前端镜像"经验沉淀卡片"与 changelog-modal 模式。

**Tech Stack:** Go（标准库 only）、TOML 配置、原生 JS/HTML/CSS（web/ 源码目录，dist/web/ 是构建产物不要改）。

**Spec:** `docs/superpowers/specs/2026-08-16-retrieval-evolution.md` 第 6 节（特性④）。

## Global Constraints

- **宁窄勿宽**：只拦高置信泛化 prompt；误拦代价 > 误放代价；**不设长度阈值**。
- **fail-open**：门控相关任何异常仅记 `ok.log`（`logErr`），不阻断注入。
- 配置三态沿用 `auto_born` 先例：文件层 TOML 缺键 = 继承（LoadMerged decode-over 天然成立）；API 层用 `*bool` / `*[]string`（null = 不变）。
- 内置短语表**不物化**进 config.toml——编译进二进制，用户维护的只是 `extra_phrases` 追加层，两层取并集。
- 前端源码改 `web/`（git 跟踪）；`dist/web/` 是 `scripts/build.py` 的拷贝产物，**不要改**。
- 测试风格：标准库 `testing` + `t.TempDir()`，非表驱动、不用 testify；每场景一个 `TestXxx`。
- 本 plan 不含版本号 bump 与 `scripts/sync-version.sh`——发布时例行处理。

---

### Task 1: `retrieve.Gated` 纯函数 + 内置短语表

**Files:**
- Create: `internal/retrieve/gate.go`
- Test: `internal/retrieve/gate_test.go`

**Interfaces:**
- Consumes: 同包既有 `retrieve.Terms(s string) []string`（`internal/retrieve/retrieve.go`）
- Produces（后续任务依赖这三个导出符号，签名不得改）:
  - `func BuiltinPhrases() []string` — 内置表副本（GUI GET 视图用）
  - `func Normalize(s string) string` — 归一化（门控判定与 extra 去重共用）
  - `func Gated(prompt string, extra []string) bool` — 门控判定

- [ ] **Step 1: 写失败测试**

```go
package retrieve

import "testing"

func TestGatedBuiltin(t *testing.T) {
	// 精确命中内置表（含归一化：大小写/标点/空白不敏感）
	for _, p := range []string{"继续", "好的", "好的。", "  OK  ", "go, on!", "Thanks", "嗯"} {
		if !Gated(p, nil) {
			t.Errorf("%q 应被门控", p)
		}
	}
}

func TestGatedEmptyTerms(t *testing.T) {
	// 纯标点/emoji/空白/单字符拉丁：Terms 为空，检索必无结果，门控省 embed 调用
	for _, p := range []string{"", "   ", "！！！", "👍", "a b"} {
		if !Gated(p, nil) {
			t.Errorf("%q 应被门控（Terms 为空）", p)
		}
	}
}

func TestGatedNormalPrompts(t *testing.T) {
	for _, p := range []string{"继续推进索引重构", "构建", "ok 的检索怎么配", "git 提交规范", "好的方案有哪些"} {
		if Gated(p, nil) {
			t.Errorf("%q 不应被门控", p)
		}
	}
}

func TestGatedExtra(t *testing.T) {
	if !Gated("走起", []string{"走起"}) {
		t.Error("extra 短语应生效")
	}
	if !Gated("走 起。", []string{"走  起"}) {
		t.Error("extra 判定应走归一化（空白折叠/标点忽略）")
	}
	if Gated("走起吧", []string{"走起"}) {
		t.Error("门控是精确匹配，不做子串")
	}
}

func TestBuiltinPhrasesCopy(t *testing.T) {
	a := BuiltinPhrases()
	if len(a) != 21 {
		t.Fatalf("内置短语表应为 21 条，got %d", len(a))
	}
	a[0] = "篡改"
	if BuiltinPhrases()[0] == "篡改" {
		t.Error("BuiltinPhrases 必须返回副本")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/retrieve/ -run TestGated -v`
Expected: FAIL（`undefined: Gated`）

- [ ] **Step 3: 实现 gate.go**

```go
package retrieve

import (
	"strings"
	"unicode"
)

// builtinPhrases 内置泛化确认短语表（宁窄勿宽，只收高置信无信息量的确认/推进词，
// 多语言常用确认词）。编译进二进制随版本演进；用户追加层见 [retrieve.gate]
// extra_phrases，两层取并集。表内条目本身即归一化形（小写、无标点）。
var builtinPhrases = []string{
	"继续", "继续吧", "好的", "好", "嗯", "对", "是的", "行", "可以", "收到", "谢谢",
	"ok", "okay", "yes", "no", "thanks", "continue", "go", "go on", "next", "done",
}

// BuiltinPhrases 返回内置短语表副本（GUI 展示用，调用方不得依赖可写性）。
func BuiltinPhrases() []string {
	return append([]string(nil), builtinPhrases...)
}

// Normalize 归一化短语：小写、字母/数字之外的字符视为分隔、折叠连续空白、去首尾
// 空白。门控判定与 extra_phrases 去重共用同一归一化形（"go,  On!" ≡ "go on"）。
func Normalize(s string) string {
	var b strings.Builder
	lastSpace := true // 起始视为空白，吃掉前导分隔
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Gated 判定 prompt 是否为无信息量的泛化输入（应跳过检索注入与 embed 调用）：
//  1. 归一化后精确命中内置/extra 短语表；
//  2. Terms 提取为空（纯标点/emoji/空白/单字符——此时 queryAll 本就返回空，
//     门控只是省掉 embed 网络往返）。
// 不设长度阈值：两字的"构建"是合法查询，长度启发式误杀率高。
func Gated(prompt string, extra []string) bool {
	n := Normalize(prompt)
	if n == "" || len(Terms(prompt)) == 0 {
		return true
	}
	for _, p := range builtinPhrases {
		if n == p {
			return true
		}
	}
	for _, p := range extra {
		if n == Normalize(p) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/retrieve/ -v`
Expected: PASS（含既有 TestTerms）

- [ ] **Step 5: Commit**

```bash
git add internal/retrieve/gate.go internal/retrieve/gate_test.go
git commit -m "feat(retrieve): 泛化 prompt 门控纯函数 Gated + 内置短语表"
```

---

### Task 2: config 增 `Retrieve.Gate` 子结构 + 默认值

**Files:**
- Modify: `internal/config/config.go`（Retrieve 结构体 :100-117、Default :166-176）
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `type RetrieveGate struct { Enabled bool; ExtraPhrases []string }`（toml 键 `enabled` / `extra_phrases`）
  - `Retrieve.Gate RetrieveGate`（toml 键 `gate`，即 `[retrieve.gate]` 子表）
  - 默认 `Gate.Enabled = true`

- [ ] **Step 1: 写失败测试**（追加到 `internal/config/config_test.go`，模板照抄 `TestWikiConfigDefaultAndOverride` :173-196）

```go
func TestGateConfigDefaultAndOverride(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	// 全缺省：门控默认启用、无 extra
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Retrieve.Gate.Enabled || len(cfg.Retrieve.Gate.ExtraPhrases) != 0 {
		t.Fatalf("unexpected defaults %+v", cfg.Retrieve.Gate)
	}
	// 全局关、项目缺键 → 继承全局 false；extra 来自全局
	if err := os.WriteFile(global, []byte("[retrieve.gate]\nenabled = false\nextra_phrases = [\"走起\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Gate.Enabled || len(cfg.Retrieve.Gate.ExtraPhrases) != 1 || cfg.Retrieve.Gate.ExtraPhrases[0] != "走起" {
		t.Fatalf("global override failed %+v", cfg.Retrieve.Gate)
	}
	// 项目显式开 → 覆盖全局
	if err := os.WriteFile(project, []byte("[retrieve.gate]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Retrieve.Gate.Enabled {
		t.Fatalf("project override failed %+v", cfg.Retrieve.Gate)
	}
	// 项目清掉 → 重新继承全局 false
	if err := os.WriteFile(project, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Gate.Enabled {
		t.Fatalf("project 缺键应继承全局 %+v", cfg.Retrieve.Gate)
	}
}
```

注意：`LoadMerged` 第一参数是项目路径、第二是全局路径（照抄 `internal/gui/api.go:898` 的调用顺序 `config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))`）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestGateConfig -v`
Expected: FAIL（`cfg.Retrieve.Gate undefined`）

- [ ] **Step 3: 实现**

`internal/config/config.go` Retrieve 结构体尾部（`MinScore` 字段之后、`}` 之前）加：

```go
	// Gate 是泛化 prompt 门控（[retrieve.gate] 子表）：命中内置/extra 短语的
	// prompt 跳过检索注入与 embed 调用。Enabled 默认 true（见 Default）；
	// ExtraPhrases 是内置短语表之外的追加层，两层取并集生效。
	Gate RetrieveGate `toml:"gate"`
```

`Retrieve` 结构体之前加新类型：

```go
// RetrieveGate 控制泛化 prompt 门控：内置短语表编译进二进制（随版本演进，
// 不物化进 config.toml），用户在 GUI 维护的只是 extra_phrases 追加层。
type RetrieveGate struct {
	Enabled      bool     `toml:"enabled"`       // 默认 true（见 Default）
	ExtraPhrases []string `toml:"extra_phrases"` // 内置表之外的追加层
}
```

`Default()` 的 Retrieve 字面量改为：

```go
		Retrieve:   Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 2, MinScore: 0.5, MinGap: 0.25, Gate: RetrieveGate{Enabled: true}},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS（含既有测试全部绿）

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): [retrieve.gate] 子表配置（enabled + extra_phrases，默认启用）"
```

---

### Task 3: `config.SetGate` 小节重写

**Files:**
- Create: `internal/config/gate.go`
- Test: `internal/config/gate_test.go`

**Interfaces:**
- Consumes: `internal/fsx.WriteFile`（原子写）；算法照抄 `internal/config/capture.go:15-53` 的 `SetCapture`
- Produces: `func SetGate(path string, enabled bool, extra []string) error` — GUI apiGateSet（Task 5）依赖此签名

- [ ] **Step 1: 写失败测试**

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGateAppendAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	// 既有 [retrieve] 顶级小节 + 注释 + [[enforce]]——追加子表不得动它们
	original := "# 头部注释\n\n[retrieve]\ntop_n = 3\n\n[[enforce]]\ntype = \"changelog_required\"\nmessage = \"x\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetGate(path, true, []string{"走起", "go ahead"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"# 头部注释", "[retrieve]\ntop_n = 3", "[[enforce]]", "[retrieve.gate]\nenabled = true\nextra_phrases = [\"走起\", \"go ahead\"]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q: %q", want, got)
		}
	}
	// 替换而非叠加；边界判定不得吞掉后面的 [[enforce]]
	if err := SetGate(path, false, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "[retrieve.gate]") != 1 {
		t.Fatalf("duplicate gate block: %q", got)
	}
	if !strings.Contains(got, "enabled = false") || !strings.Contains(got, "extra_phrases = []") || strings.Contains(got, "走起") {
		t.Fatalf("replace failed: %q", got)
	}
	if !strings.Contains(got, "[[enforce]]") || !strings.Contains(got, "[retrieve]\ntop_n = 3") {
		t.Fatalf("unrelated content lost: %q", got)
	}
	// 合并读取应生效
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Gate.Enabled || len(cfg.Retrieve.Gate.ExtraPhrases) != 0 {
		t.Fatalf("merged config wrong: %+v", cfg.Retrieve.Gate)
	}
}

func TestSetGateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetGate(path, true, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got := string(data); !strings.Contains(got, "[retrieve.gate]") || !strings.Contains(got, `extra_phrases = ["x"]`) {
		t.Fatalf("unexpected: %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestSetGate -v`
Expected: FAIL（`undefined: SetGate`）

- [ ] **Step 3: 实现**（与 `SetCapture` 逐行同构，仅匹配串换 `[retrieve.gate]`、block 内容不同、文件不存在分支不带 header——对齐 `setProvenanceAutoBorn` 先例）

```go
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"openknowledge/internal/fsx"
)

// SetGate 重写 config.toml 的 [retrieve.gate] 子表：已存在则整段替换（到下一个
// [section] 或文件尾），不存在则在文件尾追加；其余内容（含注释）原样保留。
// 算法与 SetCapture 同款；子表头的匹配串是 "[retrieve.gate]"，边界判定
// （下一个 [ 开头行）无需改动——[retrieve.gate] 之后的任何小节头（含
// [retrieve.xxx] / [[enforce]]）都以 [ 开头，都会被正确识别为边界。
func SetGate(path string, enabled bool, extra []string) error {
	var sb strings.Builder
	sb.WriteString("[retrieve.gate]\nenabled = ")
	sb.WriteString(strconv.FormatBool(enabled))
	sb.WriteString("\nextra_phrases = [")
	for i, p := range extra {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strconv.Quote(p))
	}
	sb.WriteString("]\n")
	block := sb.String()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsx.WriteFile(path, []byte(block), 0o644)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == "[retrieve.gate]" {
				start = i
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	var out []string
	if start >= 0 {
		out = append(out, lines[:start]...)
		out = append(out, strings.TrimSuffix(block, "\n"))
		out = append(out, lines[end:]...)
	} else {
		out = append(out, lines...)
		// 与上文保持空行分隔
		if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) != "" {
			out = append(out, "")
		}
		out = append(out, strings.TrimSuffix(block, "\n"))
	}
	return fsx.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/gate.go internal/config/gate_test.go
git commit -m "feat(config): SetGate 重写 [retrieve.gate] 子表（整段替换/追加）"
```

---

### Task 4: hook 接线——InjectForPrompt 前置门控

**Files:**
- Modify: `internal/hook/core.go:119-142`（EmbedQuery + QueryExBranch 段）
- Test: `internal/hook/core_test.go`

**Interfaces:**
- Consumes: `retrieve.Gated`（Task 1）、`pc.Config.Retrieve.Gate`（Task 2）
- Produces: 无新导出符号；行为契约 = 门控命中时输出不含"相关知识"段、不调用 EmbedQuery，mandatory/INDEX/wiki nudge 不受影响

- [ ] **Step 1: 写失败测试**（追加到 `internal/hook/core_test.go`；夹具复用 `setupProject` / `writeEntry`，写法对照同文件 `TestInjectForPromptBaseAndRetrieve` :23-47 与写 config 的 `TestInjectSemanticDegradeHintOnce` :72-74）

```go
// TestInjectGateSkipsRetrieval 门控命中时跳过检索注入段（连查询词本身被登记为
// 泛化短语也不例外）；关闭门控后同 prompt 恢复注入——证明跳过的是检索段而非"没命中"。
func TestInjectGateSkipsRetrieval(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	// 把查询词登记为 extra 泛化短语 → 门控必命中
	cfg := "[retrieve.gate]\nenabled = true\nextra_phrases = [\"RetrievalQuirk 是什么\"]\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s-gate", projDir, "RetrievalQuirk 是什么")
	// 注：不断言标题"检索经验"缺席——首轮基础注入的 INDEX 主列表合法地列出全部
	// 条目标题（门控按设计不影响 INDEX）；检索段的判据是"相关知识"小节与条目路径。
	if strings.Contains(out, "相关知识") || strings.Contains(out, "检索.md") {
		t.Errorf("门控命中不应注入检索段，got: %q", out)
	}
	if !strings.Contains(out, "永远先跑 gofmt") {
		t.Errorf("门控不应影响 mandatory 基础注入，got: %q", out)
	}
	// 对照：关闭门控后同 prompt 应命中检索
	cfg2 := "[retrieve.gate]\nenabled = false\nextra_phrases = [\"RetrievalQuirk 是什么\"]\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg2), 0o644); err != nil {
		t.Fatal(err)
	}
	pc2, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out2 := InjectForPrompt(pc2, "s-gate-off", projDir, "RetrievalQuirk 是什么")
	// 对照组同样以"相关知识"小节为判据（标题在 INDEX 中恒在，不足以证明检索恢复）
	if !strings.Contains(out2, "相关知识") || !strings.Contains(out2, "检索.md") {
		t.Errorf("关闭门控后应恢复检索注入，got: %q", out2)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hook/ -run TestInjectGateSkipsRetrieval -v`
Expected: FAIL（门控未实现，第一个断言即失败——`out` 含"相关知识"检索段）

- [ ] **Step 3: 实现**（`internal/hook/core.go:119-142`，把 embed + 查询段包进 else 分支；`hits` 提升到外层声明，内层用短变量名接住再赋值避免 shadow）

改前（:119-142 现状）：

```go
	var queryVec []float32
	var embedWarn string
	if client != nil {
		if vec, err := client.EmbedQuery(context.Background(), promptText); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			queryVec, embedWarn = embedx.QueryVec(db, client, vec)
			if embedWarn != "" {
				logErr("prompt embed identity: %s", embedWarn)
			}
		}
	}
	// top_n 截断在分支过滤之后（QueryExBranch 内部保证），其他分支的差异条目
	// 不再白白挤占名额；无 branch 标签的条目与未知分支场景不受影响。
	hits, info, err := db.QueryExBranch(retrieve.Terms(promptText), queryVec, pc.Config.Retrieve, ws.Branch)
	if err != nil {
		logErr("prompt query: %v", err)
	}
	// 语义通道未准入任何条目（无显著头部）：记 ok.log，GUI 日志页可按"语义"过滤
	// 查看；低对比度自定义模型可调低 retrieve.min_gap 放宽。
	if info.SemanticRejected {
		logErr("prompt semantic: 语义通道未准入任何条目（样本 %d，max=%.3f median=%.3f relGap=%.3f）；低对比度模型可调低 retrieve.min_gap 放宽",
			info.Coses, info.MaxCos, info.MedianCos, info.RelGap)
	}
```

改后：

```go
	var queryVec []float32
	var embedWarn string
	var hits []index.Hit
	if pc.Config.Retrieve.Gate.Enabled && retrieve.Gated(promptText, pc.Config.Retrieve.Gate.ExtraPhrases) {
		// 泛化门控：无信息量 prompt（"继续"/"好的"类）跳过检索注入段——连 embed
		// 调用都省（每轮一次网络往返）；mandatory/INDEX/wiki nudge 不受影响。
		// GUI 日志页可按"门控"过滤。
		logErr("prompt gate: 门控命中，泛化 prompt 跳过检索与 embed")
	} else {
		if client != nil {
			if vec, err := client.EmbedQuery(context.Background(), promptText); err != nil {
				logErr("prompt embed: %v", err)
			} else {
				queryVec, embedWarn = embedx.QueryVec(db, client, vec)
				if embedWarn != "" {
					logErr("prompt embed identity: %s", embedWarn)
				}
			}
		}
		// top_n 截断在分支过滤之后（QueryExBranch 内部保证），其他分支的差异条目
		// 不再白白挤占名额；无 branch 标签的条目与未知分支场景不受影响。
		h, info, err := db.QueryExBranch(retrieve.Terms(promptText), queryVec, pc.Config.Retrieve, ws.Branch)
		if err != nil {
			logErr("prompt query: %v", err)
		}
		// 语义通道未准入任何条目（无显著头部）：记 ok.log，GUI 日志页可按"语义"过滤
		// 查看；低对比度自定义模型可调低 retrieve.min_gap 放宽。
		if info.SemanticRejected {
			logErr("prompt semantic: 语义通道未准入任何条目（样本 %d，max=%.3f median=%.3f relGap=%.3f）；低对比度模型可调低 retrieve.min_gap 放宽",
				info.Coses, info.MaxCos, info.MedianCos, info.RelGap)
		}
		hits = h
	}
```

注意：`core.go` 已 import `openknowledge/internal/index`（`index.Open`）与 `openknowledge/internal/retrieve`（`retrieve.Terms`），无需改 import。后续代码（`if len(hits) > 0`、`embedWarn` 语义退化提示）零改动——门控时 `hits` 为 nil 天然跳过检索段，`embedWarn` 为空不触发提示。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/hook/ -v`
Expected: PASS（含既有 core_test / hook_test 全部绿）

- [ ] **Step 5: Commit**

```bash
git add internal/hook/core.go internal/hook/core_test.go
git commit -m "feat(hook): InjectForPrompt 前置泛化门控，命中即跳过 embed 与检索段"
```

---

### Task 5: GUI API——apiGateGet / apiGateSet

**Files:**
- Modify: `internal/gui/api.go`（路由注册 :73-74 旁；capture handler 组 :891-1003 旁追加新 handler；import 块）
- Test: `internal/gui/api_test.go`

**Interfaces:**
- Consumes: `retrieve.BuiltinPhrases` / `retrieve.Normalize`（Task 1）、`config.SetGate`（Task 3）、`cfg.Retrieve.Gate`（Task 2）、既有 `resolveProject` / `decodeJSON` / `writeJSON` / `writeErr`
- Produces（前端 Task 6 依赖此契约）:
  - `GET /api/gate?project=X` → `{"enabled": bool, "builtin": ["继续",...], "extra": ["..."]}`
  - `POST /api/gate` body `{"project": "X", "enabled": <bool|null>, "extra": <[]string|null>}`（null = 该字段不变）→ 成功返回同 GET 的最新视图；校验失败 400 `{"error": "..."}`

- [ ] **Step 1: 写失败测试**（追加到 `internal/gui/api_test.go`；基建 `newEnv` / `mkProject` / `do` / `testToken` 全现成，写法照抄 `TestCaptureRoundTrip` :929 与 `TestCaptureAutoBorn` :1088）

```go
func TestGateRoundTrip(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()
	cfgPath := filepath.Join(okHome, "projects", "demo", "config.toml")

	// 默认 GET：enabled=true、内置 21 条、extra 空
	code, data := do(t, "GET", srv.URL+"/api/gate?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("gate get: status = %d, body %s", code, data)
	}
	var view struct {
		Enabled bool     `json:"enabled"`
		Builtin []string `json:"builtin"`
		Extra   []string `json:"extra"`
	}
	if err := json.Unmarshal([]byte(data), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || len(view.Builtin) != 21 || len(view.Extra) != 0 {
		t.Fatalf("unexpected default view %+v", view)
	}

	// POST enabled=false → 项目 config.toml 落盘 [retrieve.gate]；GET 反映
	code, _ = do(t, "POST", srv.URL+"/api/gate", testToken,
		map[string]any{"project": "demo", "enabled": false})
	if code != 200 {
		t.Fatalf("gate set enabled: status = %d", code)
	}
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "[retrieve.gate]") || !strings.Contains(string(cfgData), "enabled = false") {
		t.Fatalf("config 应含 [retrieve.gate] enabled = false: %q", cfgData)
	}

	// POST extra 全量替换：去重（归一化形）、与内置重复丢弃、折叠空白
	code, data = do(t, "POST", srv.URL+"/api/gate", testToken,
		map[string]any{"project": "demo", "extra": []string{"走起", "走起 ", "好的", "Go  On"}})
	if code != 200 {
		t.Fatalf("gate set extra: status = %d, body %s", code, data)
	}
	if err := json.Unmarshal([]byte(data), &view); err != nil {
		t.Fatal(err)
	}
	// "走起 " 重复丢弃；"好的"/"go on" 与内置重复丢弃 → 只剩 ["走起"]
	if len(view.Extra) != 1 || view.Extra[0] != "走起" {
		t.Fatalf("extra 清洗错误: %+v", view.Extra)
	}
	// enabled 为 null = 不变（仍是 false）
	if view.Enabled {
		t.Fatalf("enabled null 应保持不变: %+v", view)
	}

	// 校验：单条 >64 字符 → 400
	code, _ = do(t, "POST", srv.URL+"/api/gate", testToken,
		map[string]any{"project": "demo", "extra": []string{strings.Repeat("长", 65)}})
	if code != 400 {
		t.Fatalf("超长短语应 400, got %d", code)
	}
	// 校验：空短语 → 400
	code, _ = do(t, "POST", srv.URL+"/api/gate", testToken,
		map[string]any{"project": "demo", "extra": []string{"   "}})
	if code != 400 {
		t.Fatalf("空短语应 400, got %d", code)
	}
	// 校验：>200 条 → 400
	big := make([]string, 201)
	for i := range big {
		big[i] = fmt.Sprintf("短语%d", i)
	}
	code, _ = do(t, "POST", srv.URL+"/api/gate", testToken,
		map[string]any{"project": "demo", "extra": big})
	if code != 400 {
		t.Fatalf("超量短语应 400, got %d", code)
	}

	// 再设 enabled=true → 整段替换而非重复追加
	code, _ = do(t, "POST", srv.URL+"/api/gate", testToken,
		map[string]any{"project": "demo", "enabled": true})
	if code != 200 {
		t.Fatalf("gate re-enable: status = %d", code)
	}
	cfgData, _ = os.ReadFile(cfgPath)
	if strings.Count(string(cfgData), "[retrieve.gate]") != 1 || !strings.Contains(string(cfgData), "enabled = true") {
		t.Fatalf("[retrieve.gate] 应唯一且 enabled = true: %q", cfgData)
	}
	if !strings.Contains(string(cfgData), `extra_phrases = ["走起"]`) {
		t.Fatalf("extra 应保留: %q", cfgData)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestGateRoundTrip -v`
Expected: FAIL（404，路由未注册）

- [ ] **Step 3: 实现**

`internal/gui/api.go` 路由注册处（`api("POST /api/capture", h.apiCaptureSet)` 之后）加两行：

```go
	api("GET /api/gate", h.apiGateGet)
	api("POST /api/gate", h.apiGateSet)
```

import 块加 `"openknowledge/internal/retrieve"`。

`apiCaptureSet` 之后追加：

```go
// apiGateGet 返回项目合并配置中的门控开关、内置短语表与 extra 追加层。
func (h *Handler) apiGateGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	extra := cfg.Retrieve.Gate.ExtraPhrases
	if extra == nil {
		extra = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": cfg.Retrieve.Gate.Enabled,
		"builtin": retrieve.BuiltinPhrases(),
		"extra":   extra,
	})
}

// apiGateSet 设置门控开关与 extra 短语（全量替换，幂等）：enabled / extra 任一为
// null 表示该字段不变。extra 校验：逐条 trim+折叠空白，按归一化形去重（与内置
// 重复的直接丢弃），单条 ≤64 字符、总数 ≤200 条；非法即 400。
// 落盘走 config.SetGate（[retrieve.gate] 整段替换）。
func (h *Handler) apiGateSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string    `json:"project"`
		Enabled *bool     `json:"enabled"`
		Extra   *[]string `json:"extra"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	enabled := cfg.Retrieve.Gate.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	extra := cfg.Retrieve.Gate.ExtraPhrases
	if req.Extra != nil {
		cleaned, err := cleanGatePhrases(*req.Extra)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		extra = cleaned
	}
	if req.Enabled != nil || req.Extra != nil {
		if err := config.SetGate(st.ConfigPath(), enabled, extra); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if extra == nil {
		extra = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled,
		"builtin": retrieve.BuiltinPhrases(),
		"extra":   extra,
	})
}

// cleanGatePhrases 校验并清洗 extra 短语：trim+折叠连续空白、按归一化形去重、
// 与内置重复的丢弃；单条 ≤64 字符、总数 ≤200 条（防止 config 被刷爆）。
func cleanGatePhrases(in []string) ([]string, error) {
	if len(in) > 200 {
		return nil, fmt.Errorf("短语总数 %d 超过上限 200", len(in))
	}
	builtin := map[string]bool{}
	for _, p := range retrieve.BuiltinPhrases() {
		builtin[p] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		folded := strings.Join(strings.Fields(p), " ")
		if folded == "" {
			return nil, fmt.Errorf("短语不能为空")
		}
		if utf8.RuneCountInString(folded) > 64 {
			return nil, fmt.Errorf("短语 %q 超过 64 字符", folded)
		}
		n := retrieve.Normalize(folded)
		if builtin[n] || seen[n] {
			continue // 与内置重复 / 列表内重复：直接丢弃
		}
		seen[n] = true
		out = append(out, folded)
	}
	return out, nil
}
```

检查 `api.go` 的 import 是否已有 `strings` / `fmt` / `unicode/utf8`（`setProvenanceAutoBorn` 用了 `strconv`；`fmt`/`strings` 基本必有，`unicode/utf8` 需补——长度校验按 rune 计）以及 `"openknowledge/internal/retrieve"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -v`
Expected: PASS（含既有 API 测试全部绿）

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api.go internal/gui/api_test.go
git commit -m "feat(gui): /api/gate get/set——门控开关 + extra 短语全量替换（校验/去重）"
```

---

### Task 6: 前端——引导页门控卡片 + 短语管理弹窗

**Files:**
- Modify: `web/index.html`（capture 卡片 :137-151 之后插卡片；changelog-modal :268-276 之后插 gate-modal）
- Modify: `web/app.js`（capture 逻辑 :710-782 旁加 gate 逻辑；`renderGuide()` 末尾 :676 与 `project-select` change :209 挂 refreshGate）
- Modify: `web/style.css`（modal 样式组 :339-388 旁加短语列表样式）

**Interfaces:**
- Consumes: Task 5 的 API 契约；既有 `$()` / `api()` / `showError()` / `captureProject()` / `state` / `.hidden` 显隐模式
- Produces: 无（纯前端）；行为契约见 Step 4 手动验证清单

无前端自动化测试基建，本任务为人工验证（Step 4）。

- [ ] **Step 1: `web/index.html` 加卡片**（紧跟 capture 卡片 `</div>` 之后，镜像其结构 + hooks 卡的 badge 写法）

```html
      <div class="card card-wide">
        <div class="card-head"><h3>泛化门控</h3><span id="badge-gate" class="badge badge-on">启用</span></div>
        <p id="gate-status" class="card-desc muted">门控：…</p>
        <div class="card-actions">
          <label><input id="gate-enabled" type="checkbox"> 启用泛化 prompt 门控（"继续"/"好的"类跳过检索注入）</label>
          <button id="btn-gate-phrases" type="button" class="btn">管理短语表</button>
        </div>
      </div>
```

- [ ] **Step 2: `web/index.html` 加弹窗**（紧跟 changelog-modal 之后，复用 `.modal.hidden` + `.modal-box` 模式；`changelog-box` 是加宽变体直接复用）

```html
  <div id="gate-modal" class="modal hidden">
    <div class="modal-box changelog-box">
      <h3>门控短语表</h3>
      <p class="muted">内置短语随版本演进、只读；自定义短语与内置取并集生效，与内置重复的会被自动忽略（单条 ≤64 字符，总数 ≤200 条）。</p>
      <div class="form-inline">
        <input id="gate-phrase-input" type="text" placeholder="新增自定义短语" maxlength="64">
        <button id="btn-gate-add" type="button" class="btn">添加</button>
      </div>
      <div id="gate-phrase-list" class="gate-phrase-list"></div>
      <div class="modal-actions">
        <button id="gate-close" type="button" class="btn btn-primary">完成</button>
      </div>
    </div>
  </div>
```

- [ ] **Step 3: `web/app.js` + `web/style.css`**

`web/app.js`（capture 逻辑块之后追加；`$` / `api` / `showError` / `captureProject` / `state` 均既有）：

```js
// ---- 泛化门控卡片 ----
function refreshGate() {
    var project = captureProject();
    var statusEl = $("gate-status");
    if (!project) {
      statusEl.textContent = "门控：尚无已注册项目（先 ok init）";
      $("badge-gate").className = "badge badge-off";
      $("badge-gate").textContent = "无项目";
      return;
    }
    api("/api/gate?project=" + encodeURIComponent(project)).then(function (g) {
      state.gate = g;
      $("gate-enabled").checked = !!g.enabled;
      $("badge-gate").className = "badge " + (g.enabled ? "badge-on" : "badge-off");
      $("badge-gate").textContent = g.enabled ? "启用" : "停用";
      statusEl.textContent = "门控：" + (g.enabled ? "启用" : "停用") +
        "（内置 " + g.builtin.length + " 条 + 自定义 " + (g.extra || []).length +
        " 条，项目 " + project + "）";
    }).catch(function (err) { showError(err.message); });
}

$("gate-enabled").addEventListener("change", function () {
    var project = captureProject();
    if (!project) {
      showError("尚无已注册项目，请先 ok init");
      this.checked = !this.checked;
      return;
    }
    api("/api/gate", {
      method: "POST",
      body: { project: project, enabled: this.checked }
    }).then(refreshGate)
      .catch(function (err) { showError(err.message); refreshGate(); });
});

// ---- 短语管理弹窗 ----
function renderGatePhrases() {
    var g = state.gate || { builtin: [], extra: [] };
    var list = $("gate-phrase-list");
    list.innerHTML = "";
    g.builtin.forEach(function (p) {
      list.appendChild(gatePhraseRow(p, "内置", null));
    });
    (g.extra || []).forEach(function (p, i) {
      list.appendChild(gatePhraseRow(p, "自定义", i));
    });
}

// onEdit==null → 内置只读行；否则为 extra 下标（编辑/删除）
function gatePhraseRow(phrase, source, extraIdx) {
    var row = document.createElement("div");
    row.className = "gate-phrase-row";
    var text = document.createElement("span");
    text.className = "grow";
    text.textContent = phrase;
    var src = document.createElement("span");
    src.className = "muted";
    src.textContent = source;
    row.appendChild(text);
    row.appendChild(src);
    if (extraIdx === null) return row;
    var edit = document.createElement("button");
    edit.type = "button";
    edit.className = "btn";
    edit.textContent = "编辑";
    edit.addEventListener("click", function () {
      var input = document.createElement("input");
      input.type = "text";
      input.maxLength = 64;
      input.value = phrase;
      row.replaceChild(input, text);
      input.focus();
      var done = false;
      function finish(save) {
        if (done) return;
        done = true;
        if (save) saveGateExtra(function (xs) { xs[extraIdx] = input.value; });
        else renderGatePhrases();
      }
      input.addEventListener("keydown", function (ev) {
        if (ev.key === "Enter") finish(true);
        if (ev.key === "Escape") finish(false);
      });
      input.addEventListener("blur", function () { finish(false); });
    });
    var del = document.createElement("button");
    del.type = "button";
    del.className = "btn btn-danger";
    del.textContent = "删除";
    del.addEventListener("click", function () {
      saveGateExtra(function (xs) { xs.splice(extraIdx, 1); });
    });
    row.appendChild(edit);
    row.appendChild(del);
    return row;
}

// 全量替换语义：本地改完整个 extra 列表后一次性 POST（幂等）
function saveGateExtra(mutate) {
    var project = captureProject();
    if (!project) { showError("尚无已注册项目，请先 ok init"); return; }
    var xs = ((state.gate && state.gate.extra) || []).slice();
    mutate(xs);
    api("/api/gate", {
      method: "POST",
      body: { project: project, extra: xs }
    }).then(function (g) {
      state.gate = g;
      renderGatePhrases();
      refreshGate();
    }).catch(function (err) { showError(err.message); renderGatePhrases(); });
}

$("btn-gate-add").addEventListener("click", function () {
    var input = $("gate-phrase-input");
    var v = input.value.trim();
    if (!v) return;
    saveGateExtra(function (xs) { xs.push(v); });
    input.value = "";
});
$("gate-phrase-input").addEventListener("keydown", function (ev) {
    if (ev.key === "Enter") $("btn-gate-add").click();
});
$("btn-gate-phrases").addEventListener("click", function () {
    renderGatePhrases();
    $("gate-modal").classList.remove("hidden");
});
$("gate-close").addEventListener("click", function () {
    $("gate-modal").classList.add("hidden");
});
```

挂刷新点两处（与 `refreshCapture()` 同位置）：
- `renderGuide()` 末尾 `refreshCapture();` 之后加一行 `refreshGate();`
- `project-select` change 监听器里 `refreshCapture();` 之后加一行 `refreshGate();`

`web/style.css`（modal 样式组旁追加）：

```css
/* 门控短语管理弹窗 */
.gate-phrase-list { max-height: 320px; overflow-y: auto; margin: 12px 0; border-top: 1px solid #e5e7eb; }
.gate-phrase-row { display: flex; align-items: center; gap: 8px; padding: 6px 0; border-bottom: 1px solid #e5e7eb; }
.gate-phrase-row .grow { flex: 1; word-break: break-all; }
.gate-phrase-row input[type="text"] { flex: 1; }
```

- [ ] **Step 4: 手动验证**

```bash
go build -o dist/ok.exe ./cmd/ok
```

在项目根运行 GUI（`./dist/ok.exe gui` 或既有启动方式，`findWebDir` 会吃到 `<cwd>/web` 的源码改动），逐项确认：

- 引导页出现"泛化门控"卡片，状态行显示 `门控：启用（内置 21 条 + 自定义 0 条，项目 X）`
- 取消勾选 checkbox → 立即保存，badge 变"停用"；项目 `config.toml` 出现 `[retrieve.gate] enabled = false`
- "管理短语表"弹窗：内置 21 行只读（无编辑/删除按钮）；添加"走起"→ 出现在自定义区；再次添加"走起 "（带空格）→ 服务端去重不新增；添加"好的"→ 与内置重复被丢弃；编辑一行 Enter 保存 / Esc 取消；删除生效
- 无项目环境下卡片提示"尚无已注册项目（先 ok init）"，checkbox 变更被回退
- 门控开启时发 prompt "好的" → `ok.log` 有一行 `prompt gate: 泛化 prompt，跳过检索与 embed`；正常提问不受影响

- [ ] **Step 5: Commit**

```bash
git add web/index.html web/app.js web/style.css
git commit -m "feat(gui): 引导页泛化门控卡片 + 短语管理弹窗"
```

---

### Task 7: changelog

**Files:**
- Create: `docs/changelogs/2026-08-16-prompt-gate.md`

**Interfaces:**
- Consumes: Task 1-6 全部
- Produces: 发布素材（版本 bump / `scripts/sync-version.sh` / `dist/changelogs` 双写在发版时例行处理，不在本 plan）

- [ ] **Step 1: 写 changelog**（格式对照 `docs/changelogs/2026-08-15-retrieve-channel-gating.md`：问题 → 修复 → 验证 → 测试）

```markdown
# 2026-08-16 泛化 prompt 门控：[retrieve.gate] + 引导页卡片与短语管理

- **问题**："继续"/"好的"/emoji 这类无信息量 prompt 也跑完整检索 + embedding，
  每轮白烧 token 与一次网络往返（embedding timeout 5s 的场景更明显）。
- **修复**：
  - `retrieve.Gated` 纯函数：归一化（小写/标点忽略/空白折叠）后精确命中短语表，
    或 `Terms` 提取为空（纯标点/emoji/单字符）→ 判定泛化；宁窄勿宽，不设长度阈值；
  - 分层短语表：内置 21 条中英常用确认词编译进二进制（随版本演进，不物化进
    config.toml），用户在 GUI 维护 `extra_phrases` 追加层，两层取并集；
  - `InjectForPrompt` 在 EmbedQuery 之前短路：门控命中连 embed 调用都省；
    mandatory/INDEX/wiki nudge 等其余注入逻辑不受影响；命中记 ok.log
    `prompt gate` 一行（GUI 日志可按"门控"过滤）；
  - 配置 `[retrieve.gate]`（enabled 默认 true / extra_phrases），`config.SetGate`
    小节重写（SetCapture 同款整段替换算法）；
  - GUI：引导页"泛化门控"卡片（启用 checkbox change 即存、内置/自定义条数状态行）
    + 短语管理弹窗（内置只读、自定义增删改、全量替换语义、服务端校验去重，
    单条 ≤64 字符 / 总数 ≤200）。
- **验证**：门控开启时 "好的" 零检索零 embed（ok.log 有门控行）；extra 登记查询词后
  同 prompt 跳过检索段、关 enabled 即恢复；GUI 弹窗增删改与去重生效。
- **测试**：`TestGated*`（内置/空 Terms/正常 prompt/extra 四分支）、
  `TestGateConfigDefaultAndOverride`（默认→全局→项目→缺键继承四态）、
  `TestSetGateAppendAndReplace`（追加/替换/边界/缺失文件）、
  `TestInjectGateSkipsRetrieval`（门控跳检索段 + mandatory 不受影响 + 关闭对照）、
  `TestGateRoundTrip`（API 默认视图/落盘/清洗去重/400 校验/null=不变/替换不追加）。
```

- [ ] **Step 2: 全仓回归**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add docs/changelogs/2026-08-16-prompt-gate.md
git commit -m "docs: 泛化门控 changelog"
```
