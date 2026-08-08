# wiki 分支感知（一期）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** wiki 游标升级为按分支记录 + 分支状态三态检测（分叉/无基线/失效）+ 注入分支上下文行 + `ok wiki base` 子命令，旧 wiki.json 惰性迁移。

**Architecture:** `state/wiki.json` 从单游标扩展为 `{base_branch, cursors: map[branch]…}`；`wiki.CheckStatus` 扩展为分支状态机；hook 注入在非基准分支时附一行上下文；全部 git 判定 fail-open。

**Tech Stack:** Go 1.25（CGO_ENABLED=0）；外部 git 命令经 `internal/procx` 静默封装；无新依赖。

**Spec:** `docs/superpowers/specs/2026-08-08-wiki-branch-awareness-design.md`（已批准，目标 v2.6.0；差异条目属二期，不在本计划）

## Global Constraints

- **fail-open 铁律**：git 不可用/命令失败/状态损坏 → 不提醒、不影响注入主流程；退出码不变
- **写入收敛**：检测只读 git 不改条目；CheckStatus **不写盘**（迁移落盘只发生在 mark/base 写入路径）
- **基准分支零回归**：当前分支 == 基准分支（或单分支项目）时，注入文本、`ok wiki status` 输出、nudge 文案与现状逐字节一致
- Status JSON 只加键不改键（`has_wiki/last_commit/behind/stale/threshold` 语义不变）
- 本机 `core.autocrlf=true`：gofmt 验证用去 CR 比对（`gofmt -d` 输出去 \r 后应为空），不信 `gofmt -l` 字面
- 测试夹具惯例：`t.Setenv("OK_HOME", t.TempDir())` + registry.Save；git 仓库测试用 `git -c commit.gpgsign=false`（防继承全局签名配置）；hook/rxext 包 TestMain 已隔离四个 agent home，不得触碰真实配置
- 注释/提交信息中文；每个任务只 `git add` 本任务文件，绝不 `git add -A`
- 当前分支探测统一为 `wiki.CurrentBranch`：`git symbolic-ref --short -q HEAD`，detach 记 `DETACHED@<short>`，非 git/错误返回 `""`

---

### Task 1: wiki 包数据模型与旧格式迁移（State/BranchCursor/LoadState/SaveState）

纯数据层：新格式读写 + 旧格式识别（git 可达性判定属 Task 2，本任务的 Legacy 只挂不修）。

**Files:**
- Modify: `internal/wiki/wiki.go`（Cursor→State 模型替换；`CursorPath` 保留）
- Test: `internal/wiki/wiki_test.go`（新建；若已存在则追加——先 `ls internal/wiki/` 确认）

**Interfaces:**
- Consumes: 仅 stdlib（`encoding/json`、`os`、`path/filepath`、`time`）
- Produces（Task 2-4 依赖，签名不得改）:
  ```go
  type BranchCursor struct { LastCommit string; GeneratedAt time.Time; EntryCount int }
  type State struct {            // wiki.json 新格式
      BaseBranch string                  // json:"base_branch,omitempty"
      Cursors    map[string]BranchCursor // json:"cursors,omitempty"
      Legacy     *BranchCursor           // json:"-"（旧格式惰性迁移暂存，不落盘）
  }
  func LoadState(stateDir string) *State                 // 不存在/损坏 nil；旧格式 → State{Legacy}
  func SaveState(stateDir string, s *State) error        // 只写新格式；MkdirAll 0755 + 0644
  ```
- 移除（调用点全在本仓，Task 4 迁完）：`type Cursor`、`LoadCursor`、`SaveCursor`——**本任务先保留旧三件套**，Task 2 改用新 API 后由 Task 4 最终删除（防中间态编译断）

- [ ] **Step 1: 写失败测试**

`internal/wiki/wiki_test.go`：

```go
package wiki

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &State{
		BaseBranch: "master",
		Cursors: map[string]BranchCursor{
			"master": {LastCommit: "abc123", GeneratedAt: time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC), EntryCount: 15},
			"dev":    {LastCommit: "def456", GeneratedAt: time.Date(2026, 8, 8, 2, 0, 0, 0, time.UTC), EntryCount: 3},
		},
	}
	if err := SaveState(dir, s); err != nil {
		t.Fatal(err)
	}
	got := LoadState(dir)
	if got == nil || got.BaseBranch != "master" || len(got.Cursors) != 2 {
		t.Fatalf("回环失败: %+v", got)
	}
	if got.Cursors["dev"].LastCommit != "def456" || got.Cursors["master"].EntryCount != 15 {
		t.Errorf("游标内容错位: %+v", got.Cursors)
	}
	if got.Legacy != nil {
		t.Errorf("新格式不得带 Legacy: %+v", got.Legacy)
	}
	// 落盘文本不得含旧顶层字段
	data, _ := os.ReadFile(filepath.Join(dir, "wiki.json"))
	var raw map[string]any
	if err := jsonUnmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["last_commit"]; ok {
		t.Errorf("新格式不应含顶层 last_commit: %s", data)
	}
}

func TestLoadStateLegacyDetected(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"last_commit":"d9f495c","generated_at":"2026-08-08T09:36:47+08:00","entry_count":15}`
	if err := os.WriteFile(filepath.Join(dir, "wiki.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadState(dir)
	if got == nil || got.Legacy == nil {
		t.Fatalf("旧格式应识别为 Legacy: %+v", got)
	}
	if got.Legacy.LastCommit != "d9f495c" || got.Legacy.EntryCount != 15 {
		t.Errorf("Legacy 内容错误: %+v", got.Legacy)
	}
	if got.Cursors != nil || got.BaseBranch != "" {
		t.Errorf("Legacy 未判归属前 Cursors/BaseBranch 应为空: %+v", got)
	}
}

func TestLoadStateMissingAndCorrupt(t *testing.T) {
	if LoadState(t.TempDir()) != nil {
		t.Error("不存在应返回 nil")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wiki.json"), []byte("{坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadState(dir) != nil {
		t.Error("损坏应返回 nil")
	}
}
```

注：`jsonUnmarshal` 占位——直接用 `encoding/json` 的 `json.Unmarshal`（import 后替换，无此 helper）。

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ -count=1`
Expected: 编译失败（State/BranchCursor/LoadState/SaveState 未定义）

- [ ] **Step 3: 实现（wiki.go 追加，旧三件套暂留）**

```go
// BranchCursor 单分支游标（字段语义同旧 Cursor）。
type BranchCursor struct {
	LastCommit  string    `json:"last_commit"`
	GeneratedAt time.Time `json:"generated_at"`
	EntryCount  int       `json:"entry_count,omitempty"`
}

// State 是 wiki.json 的新格式：基准分支 + 按分支游标表。
// Legacy 承载旧格式（顶层 last_commit）的惰性迁移：LoadState 识别后挂在此处、
// 不落盘；归属判定（git 可达性）由 CheckStatus 完成（见 status.go）。
type State struct {
	BaseBranch string                  `json:"base_branch,omitempty"`
	Cursors    map[string]BranchCursor `json:"cursors,omitempty"`
	Legacy     *BranchCursor           `json:"-"`
}

// LoadState 读 wiki.json：不存在/损坏返回 nil；旧格式（顶层 last_commit 且
// 无 cursors）升级为 State{Legacy}，归属待 CheckStatus 判定。
func LoadState(stateDir string) *State {
	data, err := os.ReadFile(CursorPath(stateDir))
	if err != nil {
		return nil
	}
	var disk struct {
		BaseBranch  string                  `json:"base_branch"`
		Cursors     map[string]BranchCursor `json:"cursors"`
		LastCommit  string                  `json:"last_commit"`
		GeneratedAt time.Time               `json:"generated_at"`
		EntryCount  int                     `json:"entry_count"`
	}
	if json.Unmarshal(data, &disk) != nil {
		return nil
	}
	s := &State{BaseBranch: disk.BaseBranch, Cursors: disk.Cursors}
	if disk.Cursors == nil && disk.LastCommit != "" {
		s.Legacy = &BranchCursor{
			LastCommit:  disk.LastCommit,
			GeneratedAt: disk.GeneratedAt,
			EntryCount:  disk.EntryCount,
		}
	}
	return s
}

// SaveState 以新格式写 wiki.json（Legacy 不落盘）。
func SaveState(stateDir string, s *State) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(CursorPath(stateDir), data, 0o644)
}
```

（旧 `Cursor`/`LoadCursor`/`SaveCursor` 保持原样不动。）

- [ ] **Step 4: 跑测试确认绿**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ ./internal/hook/ ./cmd/ok/ -count=1`
Expected: PASS（既有测试不受新代码影响——旧 API 仍在）

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/wiki/wiki.go internal/wiki/wiki_test.go
git commit -m "feat(wiki): State 数据模型（base_branch+按分支游标）与旧格式惰性识别"
```

---

### Task 2: 分支探测与 CheckStatus 状态机

**Files:**
- Create: `internal/wiki/status.go`
- Modify: `internal/wiki/wiki.go`（CheckStatus 搬走或改写；旧 Cursor 三件套此时可删——确认无引用后删，引用点 cli.go:604 由 Task 4 迁移……**修正**：Task 4 才迁 cli.go，本任务删旧三件套会导致 cli.go 编译断。因此本任务**保留**旧三件套，cli.go 的 `wiki.Cursor` 引用到 Task 4 才换；本任务只新增）
- Test: `internal/wiki/status_test.go`（新建）

**Interfaces:**
- Consumes: Task 1 的 `State`/`LoadState`；现有 `countCommits`、`procx.HideWindow`
- Produces（Task 3/4 依赖，签名不得改）:
  ```go
  func CurrentBranch(srcDir string) string // 分支名 / "DETACHED@<short>" / ""（非 git）
  // Status 新增字段（既有五字段语义不变）：
  //   Branch      string `json:"branch,omitempty"`       // 当前分支
  //   BaseBranch  string `json:"base_branch,omitempty"`
  //   BranchState string `json:"branch_state,omitempty"` // "ok"|"no_cursor"|"diverged"|"gone"|"legacy_orphan"
  //   MergeBase   string `json:"merge_base,omitempty"`   // diverged 时的分叉点
  // CheckStatus(stateDir, srcDir string, threshold int) *Status —— 签名不变，语义扩展
  ```

状态机（srcDir 非 git → `Branch=""`、`BranchState=""`，行为同现状）：

| 条件 | BranchState | Behind | 其余 |
|---|---|---|---|
| 无游标文件（State nil 或 Cursors/Legacy 均空） | ""（现状路径） | 全历史计数（同现状） | HasWiki=false |
| Legacy 且可达 HEAD | "ok" 并按现状算 behind | rev-list legacy..HEAD | 视同归入当前分支（**不写盘**） |
| Legacy 且 commit 不存在 | 按现状路径试算（rev-list 失败 Behind=-1） | -1 或现值 | 同现状（git 不可判时不新增状态） |
| Legacy 且存在但不可达 | "legacy_orphan" | -1 | HasWiki=true |
| 当前分支有游标，commit 不存在 | "gone" | -1 | HasWiki=true, LastCommit=游标值 |
| 当前分支有游标，可达 | "ok" | rev-list 正常算 | 同现状 |
| 当前分支有游标，不可达 | "diverged" | -1 | MergeBase=merge-base 结果 |
| 当前分支无游标、其他分支有 | "no_cursor" | -1 | HasWiki=true, LastCommit=基准分支游标值 |

- [ ] **Step 1: 写失败测试（status_test.go，含 git 夹具）**

```go
package wiki

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"openknowledge/internal/procx"
)

// initRepo 建临时 git 仓库并做 n 个提交；返回目录。
func initRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-b", "master")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "c0")
	for i := 1; i < n; i++ {
		run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "c")
	}
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	procx.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	h, err := HeadCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCurrentBranch(t *testing.T) {
	dir := initRepo(t, 1)
	if b := CurrentBranch(dir); b != "master" {
		t.Errorf("应为 master，got %q", b)
	}
	if b := CurrentBranch(t.TempDir()); b != "" {
		t.Errorf("非 git 应为空，got %q", b)
	}
}

func TestCheckStatusSameBranchUnchanged(t *testing.T) {
	dir := initRepo(t, 3)
	sd := t.TempDir()
	// 游标 = 第 1 个提交之后 → 落后 2
	run(t, dir, "checkout", "-q", "-b", "work", "HEAD~2")
	c0 := headOf(t, dir)
	run(t, dir, "checkout", "-q", "master")
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"master": {LastCommit: c0}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.Branch != "master" || st.BaseBranch != "master" || st.BranchState != "ok" {
		t.Fatalf("基准分支应 ok: %+v", st)
	}
	if !st.HasWiki || st.Behind != 2 || !st.Stale {
		t.Errorf("behind/stale 语义漂移: %+v", st)
	}
}

func TestCheckStatusDiverged(t *testing.T) {
	dir := initRepo(t, 2)
	base := headOf(t, dir)
	run(t, dir, "checkout", "-q", "-b", "dev")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "dev1")
	// master 上也前进一格制造分叉
	run(t, dir, "checkout", "-q", "master")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "m1")
	run(t, dir, "checkout", "-q", "dev")
	sd := t.TempDir()
	// dev 的游标指向 master 独有提交（不可达 dev HEAD）
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"dev": {LastCommit: headOfAt(t, dir, "master")}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "diverged" {
		t.Fatalf("应为 diverged: %+v", st)
	}
	if st.MergeBase != base {
		t.Errorf("分叉点应为 %s，got %q", base, st.MergeBase)
	}
	if st.Behind != -1 {
		t.Errorf("分叉时 Behind 应为 -1，got %d", st.Behind)
	}
}

func TestCheckStatusGone(t *testing.T) {
	dir := initRepo(t, 1)
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"master": {LastCommit: "0123456789abcdef0123456789abcdef01234567"}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "gone" || !st.HasWiki {
		t.Fatalf("应为 gone: %+v", st)
	}
}

func TestCheckStatusNoCursor(t *testing.T) {
	dir := initRepo(t, 1)
	master := headOf(t, dir)
	run(t, dir, "checkout", "-q", "-b", "dev")
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"master": {LastCommit: master}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "no_cursor" || !st.HasWiki || st.Branch != "dev" {
		t.Fatalf("应为 no_cursor: %+v", st)
	}
	if st.LastCommit != master {
		t.Errorf("no_cursor 应展示基准分支游标: %+v", st)
	}
}

func TestCheckStatusLegacyReachable(t *testing.T) {
	dir := initRepo(t, 3)
	sd := t.TempDir()
	legacy := `{"last_commit":"` + headOfAt(t, dir, "HEAD~2") + `","generated_at":"2026-08-08T09:00:00+08:00","entry_count":5}`
	if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "ok" || st.Behind != 2 {
		t.Fatalf("可达 legacy 应视同归入当前分支: %+v", st)
	}
	// 不写盘：文件内容仍是旧格式
	data, _ := os.ReadFile(filepath.Join(sd, "wiki.json"))
	if string(data) != legacy {
		t.Error("CheckStatus 不得写盘")
	}
}

func TestCheckStatusLegacyOrphan(t *testing.T) {
	dir := initRepo(t, 1)
	sd := t.TempDir()
	// 存在但不可达的 commit：另建仓库取一个 commit hash
	other := initRepo(t, 1)
	orphan := headOf(t, other)
	legacy := `{"last_commit":"` + orphan + `","generated_at":"2026-08-08T09:00:00+08:00","entry_count":5}`
	if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "legacy_orphan" || !st.HasWiki || st.Behind != -1 {
		t.Fatalf("应为 legacy_orphan: %+v", st)
	}
}

func headOfAt(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", rev)
	procx.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSpace(out))
}
```

（`bytes` import 补全；helper 名与既有 wiki_test.go 冲突时改 `mustHead`。）

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ -count=1`
Expected: 编译失败（CurrentBranch / 新字段未定义）

- [ ] **Step 3: 实现 status.go（完整代码）**

```go
package wiki

import (
	"os/exec"
	"strings"

	"openknowledge/internal/procx"
)

// CurrentBranch 返回 srcDir 当前分支名；detach 为 "DETACHED@<short>"；
// 非 git 仓库或命令失败返回 ""（fail-open）。
func CurrentBranch(srcDir string) string {
	if out, err := gitOut(srcDir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && out != "" {
		return out
	}
	head, err := gitOut(srcDir, "rev-parse", "--short", "HEAD")
	if err != nil || head == "" {
		return ""
	}
	return "DETACHED@" + head
}

func gitOut(srcDir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", srcDir}, args...)...)
	procx.HideWindow(cmd)
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// commitExists 报告 commit 在仓库中是否存在。
func commitExists(srcDir, commit string) bool {
	_, err := gitOut(srcDir, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	return err == nil
}

// isAncestor 报告 a 是否是 b 的祖先（a 可达 b）。
func isAncestor(srcDir, a, b string) bool {
	cmd := exec.Command("git", "-C", srcDir, "merge-base", "--is-ancestor", a, b)
	procx.HideWindow(cmd)
	return cmd.Run() == nil
}

// mergeBase 返回两引用的分叉点；无共同祖先返回空串。
func mergeBase(srcDir, a, b string) string {
	out, err := gitOut(srcDir, "merge-base", a, b)
	if err != nil {
		return ""
	}
	return out
}
```

CheckStatus 改写（wiki.go 中原实现替换为新状态机；Status 结构加四字段）：

```go
// Status 是 ok wiki status 的结果。Behind=-1 表示 git 不可用或状态无法计算。
// BranchState 分支状态："ok"（正常）、"no_cursor"（本分支无基线）、"diverged"
// （游标与本分支分叉）、"gone"（游标 commit 被改写）、"legacy_orphan"（旧格式
// 游标归属不可判）；非 git 项目与旧行为路径为空串。
type Status struct {
	HasWiki     bool   `json:"has_wiki"`
	LastCommit  string `json:"last_commit,omitempty"`
	Behind      int    `json:"behind"`
	Stale       bool   `json:"stale"`
	Threshold   int    `json:"threshold"`
	Branch      string `json:"branch,omitempty"`
	BaseBranch  string `json:"base_branch,omitempty"`
	BranchState string `json:"branch_state,omitempty"`
	MergeBase   string `json:"merge_base,omitempty"`
}

// CheckStatus 计算 wiki 状态（只读 git 与游标文件，绝不写盘；迁移落盘只发生
// 在 mark/base 写入路径）。srcDir 为项目源码目录。非 git 项目行为同旧版。
func CheckStatus(stateDir, srcDir string, threshold int) *Status {
	st := &Status{Behind: -1, Threshold: threshold}
	s := LoadState(stateDir)
	branch := CurrentBranch(srcDir)
	st.Branch = branch
	if s == nil {
		// 无游标文件：现状路径（非 git 时 Behind=-1）
		if n, err := countCommits(srcDir, "HEAD"); err == nil {
			st.Behind = n
			st.Stale = threshold > 0 && n >= threshold
		}
		return st
	}
	st.BaseBranch = s.BaseBranch
	// 旧格式惰性迁移判定（不写盘）
	if s.Legacy != nil {
		lc := s.Legacy.LastCommit
		switch {
		case branch == "" || !commitExists(srcDir, lc):
			// git 不可判：保持旧行为（直接用 legacy 算，失败则 Behind=-1）
			st.HasWiki = true
			st.LastCommit = lc
			if n, err := countCommits(srcDir, lc+"..HEAD"); err == nil {
				st.Behind = n
				st.Stale = threshold > 0 && n >= threshold
			}
			return st
		case isAncestor(srcDir, lc, "HEAD"):
			// 可达 → 视同归入当前分支（内存升级，不落盘）
			st.HasWiki = true
			st.LastCommit = lc
			st.BranchState = "ok"
			if s.BaseBranch == "" {
				st.BaseBranch = branch
			}
			if n, err := countCommits(srcDir, lc+"..HEAD"); err == nil {
				st.Behind = n
				st.Stale = threshold > 0 && n >= threshold
			}
			return st
		default:
			// 存在但不可达：报疑，不归入任何分支
			st.HasWiki = true
			st.BranchState = "legacy_orphan"
			return st
		}
	}
	st.HasWiki = len(s.Cursors) > 0
	cur, ok := s.Cursors[branch]
	if !ok {
		// 本分支无基线：展示基准分支游标供提示使用
		if bc, ok2 := s.Cursors[s.BaseBranch]; ok2 {
			st.LastCommit = bc.LastCommit
		}
		if st.HasWiki {
			st.BranchState = "no_cursor"
		}
		return st
	}
	st.LastCommit = cur.LastCommit
	if cur.LastCommit == "" {
		return st // 非 git mark 的时间戳游标：无 behind 可算，同旧行为
	}
	switch {
	case !commitExists(srcDir, cur.LastCommit):
		st.BranchState = "gone"
	case isAncestor(srcDir, cur.LastCommit, "HEAD"):
		st.BranchState = "ok"
		if n, err := countCommits(srcDir, cur.LastCommit+"..HEAD"); err == nil {
			st.Behind = n
			st.Stale = threshold > 0 && n >= threshold
		}
	default:
		st.BranchState = "diverged"
		st.MergeBase = mergeBase(srcDir, cur.LastCommit, "HEAD")
	}
	return st
}
```

注意：`st.HasWiki` 在 `legacy_orphan`/`gone`/`diverged`/`no_cursor` 各分支都必须为 true（条目存在）——检查上表逐行对齐。

- [ ] **Step 4: 跑测试确认绿 + 回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ ./internal/hook/ ./cmd/ok/ -count=1`
Expected: PASS（既有 wiki 相关测试——cmd/ok/wiki_test.go、hook nudge 测试——全部不动且全绿，即零回归证据）

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/wiki/status.go internal/wiki/status_test.go internal/wiki/wiki.go
git commit -m "feat(wiki): 分支状态机——CurrentBranch + CheckStatus 三态（分叉/无基线/失效）+ 旧游标可达性判归属"
```

---

### Task 3: 注入分支上下文行 + nudge 文案扩展

**Files:**
- Modify: `internal/hook/hook.go`（wikiNudge 扩展 + 新 `wikiContextLine`）
- Modify: `internal/hook/core.go`（InjectForPrompt 组装上下文行；CheckStatus 只调一次）
- Test: `internal/hook/hook_test.go`（追加）

**Interfaces:**
- Consumes: Task 2 的 `wiki.Status`（Branch/BaseBranch/BranchState/MergeBase）、`state.Session.WikiNudged`
- Produces:
  ```go
  func wikiContextLine(s *wiki.Status) string           // standing 行（分叉/无基线）；否则 ""
  // wikiNudge(pc, st, s *wiki.Status) string           // 签名变化：传入已算的 Status（内部函数，可改）
  ```
  注入形态（Task 4 不改、验收依赖）：
  - 分叉：`[OpenKnowledge] wiki 基于 master@abc1234；当前分支 dev（分叉点 def5678），结构描述可能与当前分支不符。`
  - 无基线：`[OpenKnowledge] wiki 基于 master@abc1234；当前分支 dev 尚无基线，结构描述以 master 为准。`
  - 失效（nudge，每会话一次）：`[OpenKnowledge] wiki 游标失效（分支可能被改写），建议在生成 wiki 的分支上重新运行 openknowledge-wiki 技能。`
  - legacy_orphan（nudge，每会话一次）：`[OpenKnowledge] wiki 游标与当前分支分叉、无法确认归属；请在生成 wiki 的分支上运行 openknowledge-wiki 技能。`

- [ ] **Step 1: 写失败测试**

```go
func TestPromptWikiBranchContextOnDiverged(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2)
	run2(t, projDir, "checkout", "-q", "-b", "dev") // 切到 dev：master 游标对 dev 为 no_cursor
	head := gitHead(t, projDir)
	// 游标记在 master（dev 上无基线）
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: head}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	writeEntry(t, kbRoot, "条目.md", "---\ntitle: 测试条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文含线索词 BranchCue。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"BranchCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "[OpenKnowledge] wiki 基于 master@") || !strings.Contains(got, "当前分支 dev") {
		t.Errorf("分叉/无基线分支应附上下文行: %q", got)
	}
	if !strings.Contains(got, "测试条目") {
		t.Errorf("注入主流程不应受影响: %q", got)
	}
}

func TestPromptWikiNoContextOnBaseBranch(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2)
	head := gitHead(t, projDir)
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: head}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	writeEntry(t, kbRoot, "条目.md", "---\ntitle: 测试条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文含线索词 BranchCue。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"BranchCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "当前分支") {
		t.Errorf("基准分支不得出现分支上下文行（零回归）: %q", out.String())
	}
}

func TestPromptWikiGoneNudge(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1)
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[wiki]\nstale_commits = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: "0123456789abcdef0123456789abcdef01234567"}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"hello"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "游标失效") {
		t.Errorf("失效态应提示: %q", out.String())
	}
}
```

（`run2`/`gitHead` 为占位名——hook_test.go 已有 git 操作 helper（`initGitRepo` 内的 `run` 是闭包不可复用），新增文件级 helper 时复用现有命名风格；writeEntry 用现有 helper。）

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/hook/ -run TestPromptWiki -count=1`
Expected: 新测试 FAIL（无上下文行/无失效提示），既有 TestPromptWikiNudge* 不受影响

- [ ] **Step 3: 实现**

hook.go——wikiNudge 改为接收已算好的 Status，并扩展文案：

```go
// wikiNudge 返回 wiki 提示（每会话最多一次，预算外放行）；不适用返回空串。
// fail-open：git 不可用/非 git 项目时 Status 不带分支状态，自然无提示。
func wikiNudge(pc *project.Context, st *state.Session, s *wiki.Status) string {
	if st.WikiNudged {
		return ""
	}
	var msg string
	switch {
	case s.BranchState == "gone":
		msg = "[OpenKnowledge] wiki 游标失效（分支可能被改写），建议在生成 wiki 的分支上重新运行 openknowledge-wiki 技能。"
	case s.BranchState == "legacy_orphan":
		msg = "[OpenKnowledge] wiki 游标与当前分支分叉、无法确认归属；请在生成 wiki 的分支上运行 openknowledge-wiki 技能。"
	case !s.HasWiki && s.Stale:
		msg = "[OpenKnowledge] 本项目还没有 wiki，建议用 openknowledge-wiki 技能生成项目 wiki（含架构、模块与演进历史）。"
	case s.HasWiki && s.Stale:
		msg = fmt.Sprintf("[OpenKnowledge] wiki 已落后 %d 个 commit，建议用 openknowledge-wiki 技能增量更新。", s.Behind)
	default:
		return ""
	}
	st.WikiNudged = true
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("prompt save state: %v", err)
	}
	return "\n" + msg + "\n"
}

// wikiContextLine 返回 standing 分支上下文行：当前分支有 wiki 内容注入、
// 但 wiki 基准不在本分支时提示出处；基准分支/无 wiki/非 git 返回空串。
func wikiContextLine(s *wiki.Status) string {
	if !s.HasWiki || s.Branch == "" || s.BaseBranch == "" || s.Branch == s.BaseBranch {
		return ""
	}
	short := s.LastCommit
	if len(short) > 7 {
		short = short[:7]
	}
	switch s.BranchState {
	case "diverged":
		mb := s.MergeBase
		if len(mb) > 7 {
			mb = mb[:7]
		}
		return fmt.Sprintf("[OpenKnowledge] wiki 基于 %s@%s；当前分支 %s（分叉点 %s），结构描述可能与当前分支不符。\n",
			s.BaseBranch, short, s.Branch, mb)
	case "no_cursor":
		return fmt.Sprintf("[OpenKnowledge] wiki 基于 %s@%s；当前分支 %s 尚无基线，结构描述以 %s 为准。\n",
			s.BaseBranch, short, s.Branch, s.BaseBranch)
	}
	return ""
}
```

core.go——InjectForPrompt 尾部（`out := store.TruncateToBudget(...)` 之后）改为统一算一次 Status、先放行上下文行再追加 nudge：

```go
	out := store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens)
	threshold := pc.Config.Wiki.StaleCommits
	ws := wiki.CheckStatus(pc.Store.StateDir(), cwd, threshold)
	if line := wikiContextLine(ws); line != "" && strings.TrimSpace(out) != "" {
		out = line + "\n" + out
	}
	if nudge := wikiNudge(pc, st, ws); nudge != "" {
		out += nudge
	}
	return out
```

注意两处现状保持：(a) wikiNudge 原实现里 `threshold <= 0` 提前返回——新实现中 gone/legacy_orphan 提示**不受 threshold 影响**（threshold 只控落后提醒），`!HasWiki` 与落后提示仍要求 `threshold > 0`（把 `threshold<=0 → 只看 gone/orphan` 的语义落到 wikiNudge 开头：`if threshold <= 0 && s.BranchState != "gone" && s.BranchState != "legacy_orphan" { return "" }`）；(b) core.go import 加 `"openknowledge/internal/wiki"`（若尚无）。

- [ ] **Step 4: 跑测试确认绿 + 全量回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/hook/ ./internal/rxext/ -count=1 && go test ./... -count=1`
Expected: 全 PASS（既有 nudge 测试逐字未动且绿 = 基准分支零回归证据之一）

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/hook/hook.go internal/hook/core.go internal/hook/hook_test.go
git commit -m "feat(hook): 非基准分支注入附 wiki 上下文行 + 游标失效/归属存疑提示"
```

---

### Task 4: CLI——status 输出扩展、mark 按分支记游标、`wiki base` 子命令

**Files:**
- Modify: `internal/cli/cli.go`（WikiCmd 565-620 区域）
- Test: `internal/cli/cli_test.go`（追加）

**Interfaces:**
- Consumes: Task 1/2 全部（`LoadState`/`SaveState`/`CurrentBranch`/`Status` 新字段）
- Produces:
  - `ok wiki status` JSON 新增键（有值才出）：`branch`、`base_branch`、`branch_state`、`merge_base`
  - `ok wiki mark [commit]`：游标记入**当前分支**；`base_branch` 为空时设为当前分支；旧格式 Legacy 在此时收敛归入（mark 即用户显式表态）
  - `ok wiki base`：打印当前基准分支；`ok wiki base <名>`：设置并落盘
  - 旧 `wiki.Cursor`/`LoadCursor`/`SaveCursor` 本任务删除（最后引用点迁移完毕）

- [ ] **Step 1: 写失败测试**

```go
func TestWikiBaseSetAndShow(t *testing.T) {
	// 夹具与 cli_test 现有 wiki 测试同款（OK_HOME 隔离 + 注册项目 + 临时 git 仓库 chdir）
	projDir, _ := setupOKProject(t) // 占位：以 cli_test.go 现有 wiki 测试的夹具函数为准
	initGitForTest(t, projDir, 1)   // 占位：同上，复用现有 git 夹具
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"base"}, &out, &errb); code != 0 {
		t.Fatalf("base 查询 exit %d", code)
	}
	out.Reset()
	if code := WikiCmd([]string{"base", "dev"}, &out, &errb); code != 0 {
		t.Fatalf("base 设置 exit %d", code)
	}
	out.Reset()
	if code := WikiCmd([]string{"base"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Errorf("base 设置后查询应显示 dev: %q", out.String())
	}
}

func TestWikiMarkRecordsCurrentBranch(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	run2(t, projDir, "checkout", "-q", "-b", "dev")
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 {
		t.Fatalf("mark exit %d", code)
	}
	s := wiki.LoadState(filepath.Join(kbRoot, "state"))
	if s == nil || s.Cursors["dev"].LastCommit == "" {
		t.Fatalf("mark 应记入当前分支 dev: %+v", s)
	}
	if s.BaseBranch != "dev" {
		t.Errorf("空基准应设为当前分支: %+v", s)
	}
	// 旧字段不存在于落盘
	data, _ := os.ReadFile(wiki.CursorPath(filepath.Join(kbRoot, "state")))
	if strings.Contains(string(data), `"last_commit":`) && !strings.Contains(string(data), `"cursors"`) {
		t.Errorf("落盘应为新格式: %s", data)
	}
}

func TestWikiStatusBranchFields(t *testing.T) {
	projDir, _ := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	var st map[string]any
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st["branch"] != "master" || st["base_branch"] != "master" || st["branch_state"] != "ok" {
		t.Errorf("status 分支字段缺失或错误: %v", st)
	}
	if st["has_wiki"] != true {
		t.Errorf("has_wiki 语义漂移: %v", st)
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ -run TestWiki -count=1`
Expected: 编译失败或断言失败（新行为未实现）

- [ ] **Step 3: 实现 WikiCmd 改造**

`status` 分支改为透传新字段：

```go
	case "status":
		st := wiki.CheckStatus(pc.Store.StateDir(), cwd, pc.Config.Wiki.StaleCommits)
		out := map[string]any{
			"project":   pc.Project.Name,
			"has_wiki":  st.HasWiki,
			"behind":    st.Behind,
			"stale":     st.Stale,
			"threshold": st.Threshold,
		}
		if st.LastCommit != "" {
			out["last_commit"] = st.LastCommit
		}
		if st.Branch != "" {
			out["branch"] = st.Branch
		}
		if st.BaseBranch != "" {
			out["base_branch"] = st.BaseBranch
		}
		if st.BranchState != "" {
			out["branch_state"] = st.BranchState
		}
		if st.MergeBase != "" {
			out["merge_base"] = st.MergeBase
		}
		_ = json.NewEncoder(stdout).Encode(out)
		return 0
```

`mark` 分支（`wiki.Cursor` 引用点替换为新 API；Legacy 收敛归入当前分支）：

```go
	case "mark":
		commit := fs.Arg(1)
		if commit == "" {
			commit, _ = wiki.HeadCommit(cwd) // 非 git 项目留空，只写时间戳
		}
		count := 0
		if db, err := index.Open(pc.Store.KbPath()); err == nil {
			if err := db.Sync(pc.Store.KnowledgeDir(), nil); err == nil {
				count, _ = db.WikiCount()
			}
			db.Close()
		}
		branch := wiki.CurrentBranch(cwd) // 非 git 为 ""：游标挂在 "" 键下（与旧单游标等价）
		s := wiki.LoadState(pc.Store.StateDir())
		if s == nil {
			s = &wiki.State{}
		}
		if s.Cursors == nil {
			s.Cursors = map[string]wiki.BranchCursor{}
		}
		s.Cursors[branch] = wiki.BranchCursor{LastCommit: commit, GeneratedAt: time.Now(), EntryCount: count}
		if s.BaseBranch == "" {
			s.BaseBranch = branch
		}
		if err := wiki.SaveState(pc.Store.StateDir(), s); err != nil {
			fmt.Fprintln(stderr, "写游标失败:", err)
			return 1
		}
		// 后续 short/输出逻辑保持现状
```

新增 `base` 分支（switch 里）：

```go
	case "base":
		s := wiki.LoadState(pc.Store.StateDir())
		name := fs.Arg(1)
		if name == "" {
			base := ""
			if s != nil {
				base = s.BaseBranch
			}
			if base == "" {
				fmt.Fprintln(stdout, "(未设置基准分支)")
			} else {
				fmt.Fprintln(stdout, base)
			}
			return 0
		}
		if s == nil {
			s = &wiki.State{}
		}
		s.BaseBranch = name
		if err := wiki.SaveState(pc.Store.StateDir(), s); err != nil {
			fmt.Fprintln(stderr, "写基准分支失败:", err)
			return 1
		}
		fmt.Fprintf(stdout, "基准分支已设为 %s\n", name)
		return 0
```

WikiCmd 注释行（`// WikiCmd 处理 ok wiki status|mark。`）改为 `// WikiCmd 处理 ok wiki status|mark|base。`。删除 wiki.go 旧三件套（`Cursor`/`LoadCursor`/`SaveCursor`），全仓 grep 确认零引用。

- [ ] **Step 4: 跑测试确认绿 + 全量回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ ./internal/wiki/ -count=1 && go test ./... -count=1`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/cli/cli.go internal/cli/cli_test.go internal/wiki/wiki.go
git commit -m "feat(cli): wiki status 分支字段 + mark 按分支记游标 + wiki base 子命令；退役旧单游标 API"
```

---

### Task 5: 文档、版本与真机验收

**Files:**
- Modify: `docs/ARCHITECTURE.md`（wiki 段落补分支感知）
- Create: `docs/changelogs/2.6.0.md`
- Modify: `installer/openknowledge.iss`（AppVersion 2.5.0→2.6.0）、`cmd/ok/winres.json`（2.5.0.0→2.6.0.0）

- [ ] **Step 1: ARCHITECTURE.md**

在 wiki 相关段落补（落点以文件实际小节为准，风格对齐现有描述）：

```markdown
wiki 游标按分支记录（`state/wiki.json`：`base_branch` + `cursors` 表，旧单游标格式读取时
按 merge-base 可达性惰性迁移，不可达报疑不归错）；CheckStatus 三态检测（分叉/无基线/失效），
非基准分支注入附一行 wiki 出处上下文；`ok wiki base` 查看/设置基准分支。分支差异条目属二期。
```

- [ ] **Step 2: changelog 2.6.0.md**（格式对齐 2.5.0.md）

```markdown
# 2.6.0

## 新功能
- wiki 分支感知：游标按分支记录（`state/wiki.json` 新格式，旧格式自动迁移）；
  切到分叉/未建基线的分支时，知识注入附一行 wiki 出处提示（"wiki 基于 master@…；
  当前分支 dev"），wiki 结构描述不再张冠李戴
- 游标被 rebase/squash 改写后显式提示"游标失效"（原静默不提醒）
- `ok wiki base` 查看/设置基准分支；`ok wiki status` 输出 branch/base_branch/branch_state

## 说明
- 基准分支上的行为与旧版完全一致；长期并行分支的"差异条目"在后续版本提供
```

- [ ] **Step 3: 版本 bump + 全量验证**

iss `2.5.0→2.6.0`、winres `2.5.0.0→2.6.0.0`。
Run: `cd D:/develop/OpenKnowledge && go vet ./... && go test ./... -count=1 && python scripts/build.py --skip-installer`
Expected: vet 无输出；24+ 包全绿；dist 构建成功

- [ ] **Step 4: 真机验收（本仓库实测）**

```bash
cd D:/develop/OpenKnowledge
# 1. 现状（master，基准分支）：status 行为不变
./dist/ok.exe wiki status   # 期望 branch=master base_branch=master branch_state=ok（迁移自旧格式）
# 2. 切分叉分支：git checkout -b wiki-branch-test && 提交一个空 commit
./dist/ok.exe wiki status   # 期望 no_cursor（dev 无基线）
# 3. 模拟注入（任何 agent 的 prompt hook 触发）应带"wiki 基于 master@…；当前分支 wiki-branch-test"行
# 4. mark 后 status → ok；wiki base 查询/设置往返
# 5. 切回 master 删除测试分支；git checkout master && git branch -D wiki-branch-test
# 6. 失效态：手改 wiki.json 游标为不存在 hash → status branch_state=gone
```

- [ ] **Step 5: 打包 + Commit**

```bash
cd D:/develop/OpenKnowledge
python scripts/build.py   # 产出 OpenKnowledgeSetup-2.6.0.exe
git add docs/ARCHITECTURE.md docs/changelogs/2.6.0.md installer/openknowledge.iss cmd/ok/winres.json
git commit -m "docs: wiki 分支感知架构说明与 v2.6.0 changelog；版本 bump 2.6.0"
```

---

## Self-Review 记录

- **Spec 覆盖**：§3 数据模型→Task 1；§4 迁移→Task 1（识别）+Task 2（可达性判归属，不落盘）+Task 4（mark/base 落盘）；§5 检测（除"已并入"）→Task 2；§6 注入→Task 3；`ok wiki base`→Task 4；§10 铁律→Global Constraints+各任务；§11 测试→各任务；§13 验收→Task 5 Step 4。二期（§8/差异条目）明确不在本计划。
- **类型一致性**：`BranchCursor`/`State`/`LoadState`/`SaveState`（T1 定义，T2/3/4 消费）；`CurrentBranch`（T2 定义，T4 消费）；`Status.BranchState` 五值（T2 定义，T3 文案分支、T4 输出共用）；`wikiContextLine`/`wikiNudge(pc, st, s)`（T3 定义，core.go 调用）——已逐一对齐。
- **已知留白**（实现时以源码为准，均有指引）：cli_test 的 wiki 夹具函数名（Task 4 Step 1 占位说明）、hook_test 的 git helper 命名（Task 3 Step 1 注）。
- **阈值语义裁决**（spec 未细化处）：gone/legacy_orphan 提示不受 `stale_commits` 阈值门控（属异常告警而非落后提醒），落后/无 wiki 提示仍受门控——Task 3 Step 3 已写明实现口径。
