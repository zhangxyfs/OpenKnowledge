# 条目级分支溯源（provenance）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 每条知识自动携带出生分支（`born:<分支>`，只展示不过滤）+ 合并谱系落盘 + GUI 分支上下文/双徽标/过滤器/开关，存量经 `ok backfill-born` 显式回填。

**Architecture:** born 走 tags 约定（零 DB 迁移），写入侧两个口子（add/propose 落笔）+ 一个回填命令；`wiki.json` 加 `merges` 数组；GUI 新增 `/api/project/branch-info` 单职责端点 + capture API 扩展 provenance 开关。

**Tech Stack:** Go 1.25（无新依赖）；原生 JS SPA。

**Spec:** `docs/superpowers/specs/2026-08-09-entry-branch-provenance-design.md`（已批准，目标 v2.8.0）

## Global Constraints

- 维度正交：`branch:<名>`=scope（过滤），`born:<名>`=provenance（只展示）——born **不得**参与任何检索/注入过滤
- **approve 不改写 born**（出生以创建时刻为准）；用户显式传入的 born 标签优先，自动记录不覆盖
- **零 DB 迁移**：tags 约定 + JSON 零值兼容（旧 wiki.json 无 merges 字段、旧 config 无 [provenance] 节均正常）
- fail-open：分支探测失败 = 不写 born（条日照常创建）；merges 读写失败不影响 status 主输出
- 写入收敛：born 只写在 add/propose/backfill 三个口子；merges 只追加不改历史
- 默认开启：`[provenance] auto_born` 键缺失 = true（config.Default() 给 true，LoadMerged 覆盖链不重置——实现前先验证该覆盖链行为）
- 注释/提交信息中文；本机 autocrlf：gofmt 用去 CR 比对；测试夹具惯例（OK_HOME 隔离 + git `-c commit.gpgsign=false`）；只 git add 本任务文件

---

### Task 1: `[provenance]` 配置 + add/propose 自动记录 born

**Files:**
- Modify: `internal/config/config.go`（Provenance 节）
- Modify: `internal/cli/cli.go`（Add 139-144 区域、Propose 423 区域、bornTag/hasBorn 辅助）
- Test: `internal/cli/cli_test.go`（追加）

**Interfaces:**
- Consumes: `wiki.CurrentBranch(srcDir string) string`（既有）；`pc.Config`、`pc.Store`
- Produces（后续任务依赖，签名不得改）:
  ```go
  // config.go
  type Provenance struct { AutoBorn bool `toml:"auto_born"` } // Config 加字段 Provenance Provenance `toml:"provenance"`
  // cli.go（包内私有）
  func bornTag(pc *project.Context) string  // auto_born 关/非 git/失败 → ""
  func hasBorn(tags []string) bool
  ```

- [ ] **Step 1: 先验证 LoadMerged 覆盖链**

读 `internal/config` 的 LoadMerged 实现，确认：`Default()` 置 `AutoBorn: true` 后，配置文件**缺省** `[provenance]` 节时 merged 结果保持 true（TOML 解码缺键不重置已有值）；文件显式写 `auto_born = false` 时为 false。把结论（含行号）写进报告——若覆盖链会重置，改用 `*bool` 或反向键 `disable_auto_born` 并在报告中说明。

- [ ] **Step 2: 写失败测试**

```go
func TestAddAutoBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t) // 以 cli_test 现有夹具为准
	initGitForTest(t, projDir, 1)      // 当前分支 master
	var out, errb bytes.Buffer
	if code := Add([]string{"--title", "测试条目", "--type", "note"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(kbRoot, "knowledge", "测试条目.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "born:master") {
		t.Errorf("应自动带 born:master: %s", data)
	}
}

func TestAddBornDisabled(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[provenance]\nauto_born = false\n"), 0o644)
	var out, errb bytes.Buffer
	if code := Add([]string{"--title", "测试条目", "--type", "note"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "测试条目.md"))
	if strings.Contains(string(data), "born:") {
		t.Errorf("关闭后不得标 born: %s", data)
	}
}

func TestAddBornNotOverrideExplicit(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	var out, errb bytes.Buffer
	if code := Add([]string{"--title", "测试条目", "--type", "note", "--tags", "born:hotfix"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "测试条目.md"))
	if !strings.Contains(string(data), "born:hotfix") || strings.Contains(string(data), "born:master") {
		t.Errorf("显式 born 不得被覆盖/叠加: %s", data)
	}
}

func TestProposeAutoBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	var out, errb bytes.Buffer
	if code := Propose([]string{"--title", "草稿条目", "--type", "pitfall", "--body", "正文"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(kbRoot, "knowledge", "草稿条目.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "born:master") {
		t.Errorf("propose 应自动带 born: %s", data)
	}
}

func TestApproveKeepsBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	var out, errb bytes.Buffer
	Propose([]string{"--title", "草稿条目", "--type", "note", "--body", "x"}, &out, &errb)
	out.Reset()
	if code := Approve([]string{"草稿条目"}, &out, &errb); code != 0 {
		t.Fatalf("approve exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "草稿条目.md"))
	if !strings.Contains(string(data), "born:master") {
		t.Errorf("approve 不得丢 born: %s", data)
	}
	if strings.Contains(string(data), "draft: true") {
		t.Errorf("approve 后不应仍为草稿")
	}
}
```

- [ ] **Step 3: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ -run "TestAdd.*Born|TestProposeAutoBorn|TestApproveKeepsBorn" -count=1`
Expected: FAIL（无 born 写入 / Provenance 未定义）

- [ ] **Step 4: 实现**

config.go：

```go
// Provenance 控制分支溯源（born 标签的自动记录）。
type Provenance struct {
	AutoBorn bool `toml:"auto_born"` // 默认 true（见 Default）
}
```

`Config` 加 `Provenance Provenance \`toml:"provenance"\``；`Default()` 加 `Provenance: Provenance{AutoBorn: true}`。

cli.go 辅助（放 Add 附近）：

```go
// bornTag 返回当前分支的 born 标签（born:<分支>）；auto_born 关闭、
// 非 git 仓库或探测失败返回 ""（fail-open，不阻断建条目）。
func bornTag(pc *project.Context) string {
	if !pc.Config.Provenance.AutoBorn {
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	b := wiki.CurrentBranch(cwd)
	if b == "" {
		return ""
	}
	return "born:" + b
}

// hasBorn 报告 tags 中已存在 born 标签（用户显式传入时自动记录不覆盖）。
func hasBorn(tags []string) bool {
	for _, t := range tags {
		if strings.HasPrefix(t, "born:") {
			return true
		}
	}
	return false
}
```

Add 的 tags 解析块（139-144 行）之后插入：

```go
	if !hasBorn(e.Tags) {
		if bt := bornTag(pc); bt != "" {
			e.Tags = append(e.Tags, bt)
		}
	}
```

Propose 的 `e := &entry.Entry{...}`（423 行附近）构造后同样插入该三行（先确认 Propose 是否已有 pc——若无按 Add 的 resolveFromCwd 同款取法补上）。cli.go import 加 `"openknowledge/internal/wiki"`。

- [ ] **Step 5: 跑测试确认绿 + 全量回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ ./internal/config/ -count=1 && go test ./... -count=1`
Expected: 全 PASS

- [ ] **Step 6: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/config/config.go internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): born 自动记录——add/propose 落笔探测当前分支写入 born 标签（可配置关闭）"
```

---

### Task 2: `ok backfill-born` 回填命令

**Files:**
- Modify: `internal/cli/cli.go`（BackfillBorn）
- Modify: `cmd/ok/main.go`（路由注册）
- Test: `internal/cli/cli_test.go`（追加）

**Interfaces:**
- Consumes: `entry.Parse`（frontmatter 解析，以 internal/entry 实际 API 为准）、`wiki.CurrentBranch`、`resolveFromCwd`
- Produces: `ok backfill-born`——预览"将按当前分支 X 回填 N 条"，stdin 确认后写入；只补无 born 的条目

- [ ] **Step 1: 写失败测试**

```go
func TestBackfillBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	// 两条无 born 条目 + 一条已有 born
	writeEntryFile(t, kbRoot, "老条目1.md", "---\ntitle: 老条目1\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	writeEntryFile(t, kbRoot, "老条目2.md", "---\ntitle: 老条目2\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	writeEntryFile(t, kbRoot, "已标.md", "---\ntitle: 已标\ntype: note\ntags: [\"born:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	var out, errb bytes.Buffer
	in := strings.NewReader("y\n")
	if code := BackfillBorn(nil, in, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "master") || !strings.Contains(out.String(), "2") {
		t.Errorf("预览应含分支与数量: %q", out.String())
	}
	d1, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "老条目1.md"))
	if !strings.Contains(string(d1), "born:master") {
		t.Errorf("老条目1 应被回填: %s", d1)
	}
	d3, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "已标.md"))
	if !strings.Contains(string(d3), "born:dev") || strings.Contains(string(d3), "born:master") {
		t.Errorf("已有 born 不得覆盖: %s", d3)
	}
}

func TestBackfillBornAbort(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	writeEntryFile(t, kbRoot, "老条目.md", "---\ntitle: 老条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	var out, errb bytes.Buffer
	in := strings.NewReader("n\n")
	if code := BackfillBorn(nil, in, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "老条目.md"))
	if strings.Contains(string(data), "born:") {
		t.Errorf("取消后不得写入: %s", data)
	}
}

func TestBackfillBornNonGit(t *testing.T) {
	setupOKProject(t) // 不 init git
	var out, errb bytes.Buffer
	if code := BackfillBorn(nil, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("非 git 应返回 1，got %d", code)
	}
}
```

（`writeEntryFile` 与既有 helper 同名冲突时复用。）

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ -run TestBackfillBorn -count=1`
Expected: 编译失败（BackfillBorn 未定义）

- [ ] **Step 3: 实现**

```go
// BackfillBorn: ok backfill-born —— 按当前分支给无 born 的存量条目回填 born 标签。
// 预览确认后写入；只补无 born 的条目，不覆盖已有值。非 git 项目报错退出。
func BackfillBorn(args []string, in io.Reader, stdout, stderr io.Writer) int {
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	cwd, _ := os.Getwd()
	branch := wiki.CurrentBranch(cwd)
	if branch == "" {
		fmt.Fprintln(stderr, "当前目录不是 git 仓库，无法确定回填分支")
		return 1
	}
	files, err := filepath.Glob(filepath.Join(pc.Store.KnowledgeDir(), "*.md"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var pending []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		e, err := entry.Parse(string(data))
		if err != nil {
			continue // 损坏条目跳过（与其他路径一致的容忍口径）
		}
		if !hasBorn(e.Tags) {
			pending = append(pending, f)
		}
	}
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "所有条目已有 born 标签，无需回填")
		return 0
	}
	fmt.Fprintf(stdout, "将按当前分支 %s 回填 %d 条无 born 条目，确认？(y/N) ", branch, len(pending))
	var ans string
	if _, err := fmt.Fscanln(in, &ans); err != nil || (ans != "y" && ans != "Y") {
		fmt.Fprintln(stdout, "已取消")
		return 0
	}
	n := 0
	for _, f := range pending {
		data, _ := os.ReadFile(f)
		e, _ := entry.Parse(string(data))
		e.Tags = append(e.Tags, "born:"+branch)
		if err := os.WriteFile(f, e.Serialize(), 0o644); err != nil {
			fmt.Fprintf(stderr, "写回失败 %s: %v\n", f, err)
			continue
		}
		n++
	}
	fmt.Fprintf(stdout, "已回填 %d 条\n", n)
	return afterAdd(pc, stdout, stderr) // 重建索引与 INDEX
}
```

（`entry.Parse` 的签名/错误形态以 internal/entry 现状为准调整；Serialize 若改变 frontmatter 字段序属可接受的规范化。）main.go switch 加：

```go
	case "backfill-born":
		os.Exit(cli.BackfillBorn(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
```

- [ ] **Step 4: 跑测试确认绿 + 全量回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ ./cmd/ok/ -count=1 && go test ./... -count=1`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/cli/cli.go internal/cli/cli_test.go cmd/ok/main.go
git commit -m "feat(cli): ok backfill-born——按当前分支回填存量条目 born（预览确认、只补无 born）"
```

---

### Task 3: 合并谱系（merges 落盘）+ mark 明示基准 + base 无参列候选

**Files:**
- Modify: `internal/wiki/wiki.go`（State.Merges + AppendMerge）
- Modify: `internal/cli/cli.go`（status/mark 谱系记录、mark 输出基准、base 无参候选）
- Test: `internal/wiki/wiki_test.go`、`internal/cli/cli_test.go`（追加）

**Interfaces:**
- Consumes: Task 1/2 无依赖；既有 `MergedIntoBase`、`HeadCommit`
- Produces:
  ```go
  // wiki.go
  type MergeRecord struct { From, To, Commit string; Time time.Time }
  // State 加 Merges []MergeRecord `json:"merges,omitempty"`
  func (s *State) AppendMerge(from, to, commit string, t time.Time) bool // from+commit 判重；返回是否新增
  ```
  CLI 行为：`ok wiki status|mark` 检出 merged 时落盘谱系；mark 输出加"基准分支: X"行；`ok wiki base` 无参输出当前基准 + 候选分支清单

- [ ] **Step 1: 写失败测试（wiki 包 AppendMerge + cli 行为）**

```go
func TestAppendMergeDedup(t *testing.T) {
	s := &State{}
	tm := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	if !s.AppendMerge("dev", "master", "abc123", tm) {
		t.Fatal("首次应新增")
	}
	if s.AppendMerge("dev", "master", "abc123", tm) {
		t.Fatal("from+commit 相同应判重")
	}
	if !s.AppendMerge("dev", "master", "def456", tm) {
		t.Fatal("不同 commit 应新增")
	}
	if len(s.Merges) != 2 {
		t.Fatalf("应为 2 条: %+v", s.Merges)
	}
}
```

cli 侧（git 夹具：master 上合并 dev，dev 游标在册 + dev 差异条目存在）：

```go
func TestWikiStatusRecordsMerge(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 2)
	run2(t, projDir, "checkout", "-q", "-b", "dev")
	run2(t, projDir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "d1")
	run2(t, projDir, "checkout", "-q", "master")
	// dev 游标 + dev 差异条目（HasBranchWiki 为真的前提）
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 { // 在 dev 上记游标
		t.Fatal(code)
	}
	writeEntryFile(t, kbRoot, "差异.md", "---\ntitle: 架构（dev 分支差异）\ntype: reference\ntags: [\"wiki\", \"branch:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	run2(t, projDir, "merge", "-q", "--no-ff", "dev", "-m", "merge dev")
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	s := wiki.LoadState(filepath.Join(kbRoot, "state"))
	found := false
	for _, m := range s.Merges {
		if m.From == "dev" && m.To == "master" {
			found = true
		}
	}
	if !found {
		t.Fatalf("谱系应记录 dev→master: %+v", s.Merges)
	}
	// 再次 status 不重复记录
	n1 := len(s.Merges)
	WikiCmd([]string{"status"}, &out, &errb)
	s2 := wiki.LoadState(filepath.Join(kbRoot, "state"))
	if len(s2.Merges) != n1 {
		t.Errorf("重复执行不得重复记录: %d→%d", n1, len(s2.Merges))
	}
}
```

（`run2` 为占位名，用 cli_test 现有 git helper；mark 在 dev 上执行需先 checkout dev——测试里直接 run2 checkout。）

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ ./internal/cli/ -run "TestAppendMerge|TestWikiStatusRecordsMerge" -count=1`
Expected: FAIL

- [ ] **Step 3: 实现**

wiki.go：

```go
// MergeRecord 是一条合并谱系：from 分支于 time 被并入 to（检出时 HEAD 为 commit）。
type MergeRecord struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	Commit string    `json:"commit"`
	Time   time.Time `json:"time"`
}

// AppendMerge 追加合并谱系（from+commit 判重）；返回是否实际新增。
func (s *State) AppendMerge(from, to, commit string, t time.Time) bool {
	for _, m := range s.Merges {
		if m.From == from && m.Commit == commit {
			return false
		}
	}
	s.Merges = append(s.Merges, MergeRecord{From: from, To: to, Commit: commit, Time: t})
	return true
}
```

State 加字段 `Merges []MergeRecord \`json:"merges,omitempty"\``。

cli.go——status/mark 共用记录逻辑（放 WikiCmd 内或抽小函数）：

```go
// recordMerges 检出已并入分支时落盘谱系（from+commit 判重）。fail-open 仅记日志。
func recordMerges(pc *project.Context, cwd string, merged []string) {
	if len(merged) == 0 {
		return
	}
	s := wiki.LoadState(pc.Store.StateDir())
	if s == nil {
		return
	}
	head, _ := wiki.HeadCommit(cwd)
	changed := false
	for _, b := range merged {
		if s.AppendMerge(b, s.BaseBranch, head, time.Now()) {
			changed = true
		}
	}
	if changed {
		if err := wiki.SaveState(pc.Store.StateDir(), s); err != nil {
			fmt.Fprintln(os.Stderr, "谱系落盘失败:", err)
		}
	}
}
```

status 分支的 merged 检测处（Task 3 已交付的 status merged_branches 段）追加 `recordMerges(pc, cwd, merged)`；mark 分支同样做 merged 检测+记录（mark 前先算 merged——复用 status 段同款 hasDelta 闭包）。mark 输出在"已记录 wiki 游标…"后加一行：

```go
	fmt.Fprintf(stdout, "基准分支: %s\n", s.BaseBranch)
```

base 无参分支改为输出基准 + 候选：

```go
		base := ""
		if s != nil {
			base = s.BaseBranch
		}
		if base == "" {
			fmt.Fprintln(stdout, "(未设置基准分支)")
		} else {
			fmt.Fprintf(stdout, "基准分支: %s\n", base)
		}
		// 候选：本地分支清单（* 标记 origin/HEAD 若可判）
		if out, err := exec.Command("git", "-C", cwd, "branch", "--format", "%(refname:short)").Output(); err == nil {
			fmt.Fprintln(stdout, "候选分支:")
			for _, b := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if b != "" {
					fmt.Fprintf(stdout, "  %s\n", b)
				}
			}
		}
		return 0
```

（git 调用走 procx.HideWindow 惯例——WikiCmd 区域 import 按需补。）

- [ ] **Step 4: 跑测试确认绿 + 全量回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ ./internal/cli/ -count=1 && go test ./... -count=1`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/wiki/wiki.go internal/wiki/wiki_test.go internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(wiki): 合并谱系落盘（status/mark 检出即记录、判重）+ mark 明示基准 + base 无参列候选"
```

---

### Task 4: GUI——branch-info 端点 + 分支上下文/双徽标/过滤器/provenance 开关

**Files:**
- Modify: `internal/gui/api.go`（新端点 + capture API 扩展）
- Modify: `web/index.html`（工具条分支上下文/谱系行/provenance checkbox）
- Modify: `web/app.js`（渲染与联动）
- Modify: `web/style.css`（双徽标样式）
- Test: `internal/gui/api_test.go`（追加）
- 同步： `dist/web/`（cp）

**Interfaces:**
- Consumes: `wiki.LoadState`、`wiki.CurrentBranch`、`config` 的 Provenance（Task 1）
- Produces:
  - `GET /api/project/branch-info?project=X` → `{"base_branch":"master","current_branch":"dev","merges":[{from,to,commit,time}]}`
  - capture GET/POST 扩展键 `auto_born`（沿用该端点现有"空/零=保持不变"语义：GET 恒返回当前值）

- [ ] **Step 1: 写失败测试（api_test）**

```go
func TestBranchInfoAPI(t *testing.T) {
	// 夹具：注册项目 + kbRoot/state/wiki.json 写 {base_branch: master, cursors, merges:[{from:"dev",to:"master",...}]}
	// GET /api/project/branch-info?project=<名>
	var body map[string]any
	// ... 既有测试 handler 惯例请求并解码
	if body["base_branch"] != "master" {
		t.Errorf("base_branch 错误: %v", body)
	}
	merges, _ := body["merges"].([]any)
	if len(merges) != 1 {
		t.Errorf("merges 应透传: %v", body["merges"])
	}
	if _, ok := body["current_branch"]; !ok {
		t.Errorf("应含 current_branch 键")
	}
}

func TestCaptureAutoBorn(t *testing.T) {
	// GET 默认 true（无 [provenance] 节）；POST auto_born=false 后 GET 为 false 且项目 config.toml 落盘
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/gui/ -run "TestBranchInfo|TestCaptureAutoBorn" -count=1`
Expected: FAIL（端点/键不存在）

- [ ] **Step 3: 实现（后端）**

api.go 路由加：

```go
	api("GET /api/project/branch-info", h.apiProjectBranchInfo)
```

handler：

```go
// apiProjectBranchInfo 返回项目的分支上下文：基准分支（wiki.json）、
// 项目目录实际 checkout 分支、合并谱系。
func (h *Handler) apiProjectBranchInfo(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	out := map[string]any{"base_branch": "", "current_branch": "", "merges": []any{}}
	if s := wiki.LoadState(st.StateDir()); s != nil {
		out["base_branch"] = s.BaseBranch
		out["merges"] = s.Merges
	}
	if len(st.Paths) > 0 {
		out["current_branch"] = wiki.CurrentBranch(st.Paths[0])
	}
	writeJSON(w, http.StatusOK, out)
}
```

（`st.StateDir()`/`st.Paths` 的字段名以 resolveProject 返回类型现状为准；merges 为空时给 `[]` 便于前端。）capture GET 加 `"auto_born": cfg.Provenance.AutoBorn`；POST 的 req 加 `AutoBorn *bool \`json:"auto_born"\``，非 nil 时写项目 config 的 `[provenance]`（写盘机制照 apiCaptureSet 写 [capture] 的既有路径扩展——先读该实现再落笔）。

- [ ] **Step 4: 实现（前端）**

index.html 工具条 `branch-filter` label 后加：

```html
        <span id="branch-context" class="muted"></span>
```

工具条下（toolbar 结束处）加：

```html
      <div id="merge-lineage" class="merge-lineage muted hidden"></div>
```

经验沉淀卡的 interval 行附近加：

```html
          <label class="capture-auto-born"><input id="auto-born" type="checkbox"> 自动记录出生分支</label>
```

app.js：

```js
  // bornOf 取条目出生分支（born:<名>，第一个）；无则空串
  function bornOf(e) {
    var tags = e.tags || [];
    for (var i = 0; i < tags.length; i++) {
      if (tags[i].indexOf("born:") === 0) return tags[i].slice(5);
    }
    return "";
  }

  // renderBranchInfo 拉取并渲染分支上下文与合并谱系（随项目联动）
  function renderBranchInfo() {
    var el = $("branch-context");
    if (!el || !state.project) return;
    api("/api/project/branch-info?project=" + encodeURIComponent(state.project)).then(function (info) {
      var base = info.base_branch || "—";
      var cur = info.current_branch || "—";
      el.innerHTML = "";
      var b = document.createElement("span");
      b.textContent = "基准分支: " + base + " · 当前分支: ";
      var c = document.createElement("span");
      c.textContent = cur;
      if (info.base_branch && info.current_branch && info.base_branch !== info.current_branch) {
        c.className = "branch-warn";
      }
      el.appendChild(b); el.appendChild(c);
      var lineage = $("merge-lineage");
      var ms = info.merges || [];
      if (ms.length > 0) {
        var last = ms[ms.length - 1];
        lineage.textContent = "合并谱系: " + last.from + " → " + last.to +
          "（" + String(last.time || "").slice(0, 10) + "，共 " + ms.length + " 条）";
        lineage.classList.remove("hidden");
      } else {
        lineage.classList.add("hidden");
      }
    }).catch(function () {});
  }
```

条目行分支格改为双徽标：

```js
      var born = bornOf(e), scope = entryBranch(e);
      var branchCell = "";
      if (born) branchCell += '<span class="badge badge-born">⎇ ' + esc(born) + "</span> ";
      if (scope) branchCell += '<span class="badge badge-branch">⇢ ' + esc(scope) + "</span>";
```

分支过滤语义（选中 X → born==X 或 scope==X + 无 born 无 scope）：

```js
      if (state.branchFilter) {
        var bo = bornOf(e), sc = entryBranch(e);
        if (bo !== state.branchFilter && sc !== state.branchFilter && (bo !== "" || sc !== "")) return false;
      }
```

renderBranchFilter 聚合改 born ∪ scope。加载完成与项目切换处加 `renderBranchInfo()` 调用。capture 卡渲染处把 `auto_born` 填到 `$("auto-born").checked`，该 checkbox change 时随 capture 保存路径 POST `{project, auto_born}`。

style.css：

```css
.badge-born { background: #eef2f7; color: #475569; border-radius: 4px; padding: 1px 6px; font-size: 12px; white-space: nowrap; }
.branch-warn { color: #b45309; font-weight: 600; }
.merge-lineage { padding: 2px 4px; font-size: 12px; }
```

- [ ] **Step 5: 跑测试 + 同步 dist + 手动冒烟**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/gui/ -count=1 && cp web/index.html web/app.js web/style.css dist/web/ && go build -o dist/ok.exe ./cmd/ok && node --check web/app.js`
Expected: PASS；`./dist/ok.exe gui` 管理页工具条出现"基准分支/当前分支"、born 徽标、谱系行（有 merges 时）

- [ ] **Step 6: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/gui/api.go internal/gui/api_test.go web/index.html web/app.js web/style.css
git commit -m "feat(gui): 分支上下文（基准·当前+警示）+ born/scope 双徽标 + 谱系行 + provenance 开关"
```

---

### Task 5: 文档、版本 2.8.0 与验收

**Files:**
- Modify: `docs/ARCHITECTURE.md`（条目模型/wiki 段补 born/谱系）、`web/help.md`（frontmatter 小节补 born 一行 + CLI 表补 backfill-born）
- Create: `docs/changelogs/2.8.0.md`
- Modify: `installer/openknowledge.iss`（2.7.1→2.8.0）、`cmd/ok/winres.json`（2.7.1.0→2.8.0.0）

- [ ] **Step 1: ARCHITECTURE.md**——条目模型段补：`born:<分支>` 溯源标签（创建时自动记录，可配置关闭；approve 不改写；`ok backfill-born` 回填存量）；wiki.json `merges` 合并谱系数组（status/mark 检出落盘、from+commit 判重）。help.md 的"条目级控制"小节补一行：`- `tags` 含 `born:<分支名>` → 出生分支溯源（2.8+，自动记录，只展示不过滤）`；CLI 速查补 `ok backfill-born`。
- [ ] **Step 2: changelog 2.8.0.md**（格式对齐既有）：

```markdown
# 2.8.0

## 新功能
- 条目级分支溯源（provenance）：每条新知识自动记录出生分支（born 标签），GUI 管理页
  分支列不再空白——每条都有"⎇ 出生分支"徽标（只展示不过滤；`[provenance] auto_born`
  可关；`ok backfill-born` 回填存量）
- 合并谱系落盘：分支并入基准自动记录（wiki.json merges），GUI 管理页显示"dev → master"
- GUI 分支上下文：工具条显示"基准分支 · 当前分支"，不一致时警示；`ok wiki base` 无参列候选，
  `ok wiki mark` 明示基准

## 说明
- born 与 branch 标签正交：branch 管"在哪生效"（注入过滤），born 管"在哪出生"（展示）
- 零迁移：旧知识库开箱即用，born 从新条目自然积累（或 backfill-born 一次补齐）
```

- [ ] **Step 3: 版本 bump + 全量验证**

iss `2.8.0`、winres `2.8.0.0`。
Run: `cd D:/develop/OpenKnowledge && go vet ./... && go test ./... -count=1 && python scripts/build.py --skip-installer`
Expected: vet 无输出；全绿；dist/changelogs/2.8.0.md 存在

- [ ] **Step 4: 真机验收（controller 可做）**

1. `ok add` 新条目 → 文件含 born:master，GUI 行有 ⎇ master 徽标
2. `ok backfill-born`（y 确认）→ 存量补齐；GUI 分支列有内容
3. 造分支合并 → `ok wiki status` → GUI 谱系行出现；重复 status 不重复记录
4. GUI 工具条"基准分支: master · 当前分支: X"；经验沉淀卡 checkbox 开关生效
5. `python scripts/build.py` 打 OpenKnowledgeSetup-2.8.0.exe

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add docs/ARCHITECTURE.md web/help.md docs/changelogs/2.8.0.md installer/openknowledge.iss cmd/ok/winres.json
git commit -m "docs: provenance 架构说明与 v2.8.0 changelog；版本 bump 2.8.0"
```

---

## Self-Review 记录

- **Spec 覆盖**：§3 数据模型→Task 1（born 标签）+Task 3（merges）；§4 时机→Task 1（add/propose）+Task 2（backfill）+Task 3（谱系/status/mark）+Task 1 的 approve 回归；§5 GUI→Task 4（5.1-5.5 全覆盖）；§2 基准锚定→Task 3（mark 明示+base 候选）；§6 铁律→Global Constraints；§7 测试→各任务；§9 验收→Task 5 Step 4。
- **类型一致性**：`bornTag/hasBorn`（T1 定义，T2 backfill 复用 hasBorn）；`MergeRecord/AppendMerge`（T3 定义，T4 端点透传）；`Provenance.AutoBorn`（T1 定义，T4 capture 消费）；`bornOf`（T4 前端内部，与后端 born: 约定对齐）——已逐一对齐。
- **已知留白**（实现时以源码为准，均有指引）：LoadMerged 覆盖链（T1 Step 1 先验证）、entry.Parse 签名、resolveProject 返回类型字段名、apiCaptureSet 写盘机制、cli_test 的 git helper 名（run2 占位）。
- **自审补充**：Task 4 Step 3 的 merges 空值给 `[]`（前端 forEach 友好）；Task 3 recordMerges 的 To 字段取 `s.BaseBranch`（与 MergedIntoBase 语义一致——并入对象是基准）。
