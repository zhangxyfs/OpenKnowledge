# wiki 基线读时继承（inherited 态）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新切分支无本分支游标时，CheckStatus 读时回退继承可达游标（第六态 `inherited`），替代 `no_cursor` 误报并获得正常 stale 追踪；展示层按原型变体 B（状态卡）落地。

**Architecture:** 检测层 `internal/wiki.CheckStatus` 增加只读回退查找（基准游标 merge-base 短路 → 其余游标取 rev-list 最近者），零落盘零迁移；hook 注入 `wikiContextLine` 新增 inherited 两档文案、`wikiNudge` 对 inherited+Stale 去重；CLI/GUI 透传新字段，GUI 分支上下文升级为状态卡（fail-open 回退现状一行文字）。

**Tech Stack:** Go（stdlib + 内部 procx/fsx/state/index 包）、外部 git 命令、静态 Web GUI（原生 JS/CSS）。

**Spec:** `docs/superpowers/specs/2026-08-19-wiki-baseline-inheritance-design.md`；原型：`web/prototype-wiki-inherited.html`（变体 B 已选定）。

## Global Constraints

- `CheckStatus` 只读铁律：绝不写盘；继承零落盘，`wiki.json` 零变更、零迁移
- fail-open：git 不可用/命令失败/状态损坏 → 不回退、不提醒，行为同现状
- 写入收敛：游标写入仍只有 `ok wiki mark` 一个入口
- git 命令一律走既有封装：`exec.Command` + `procx.HideWindow`（Windows 弹窗抑制）
- 回归红线：基准分支与 `ok`/`diverged`/`gone`/`legacy_orphan`/`no_cursor`（真无基线）路径行为逐字节不变
- 测试助手现状：`internal/wiki/status_test.go` 有 `initRepo(t, n)`/`run(t, dir, args...)`/`headOf(t, dir)`；`internal/hook/hook_test.go` 有 `setupProject(t)`/`initGitRepo(t, dir, n)`/`runGit`/`gitHead`/`writeEntry`

---

### Task 1: wiki 包——Status.InheritedFrom 与 CheckStatus 回退

**Files:**
- Modify: `internal/wiki/wiki.go`（Status 结构体 :102-112；CheckStatus 的 `!ok` 分支 :180-190；文件末尾新增 inheritCursor）
- Test: `internal/wiki/status_test.go`（改写 TestCheckStatusNoCursor :132-147；新增 3 个测试）

**Interfaces:**
- Produces: `Status.InheritedFrom string`（json `inherited_from,omitempty`，空串=非继承态）；`BranchState` 新增取值 `"inherited"`；`inheritCursor(srcDir string, s *State) (from, lastCommit string, behind int, ok bool)`（包内私有，Task 2/3/4 只消费 Status 字段）

- [ ] **Step 1: 改写 TestCheckStatusNoCursor 为新语义（先红）**

现状该测试构造的「master 有游标 + 切 dev」在新设计下应判 `inherited` 而非 `no_cursor`。改写为「唯一游标不可达」场景：

```go
func TestCheckStatusNoCursor(t *testing.T) {
	// 唯一游标（dev 上的 B）对 master HEAD 不可达 → 真·无基线 no_cursor
	dir := initRepo(t, 1)
	run(t, dir, "checkout", "-q", "-b", "dev")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "b")
	devTip := headOf(t, dir)
	run(t, dir, "checkout", "-q", "master")
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"dev": {LastCommit: devTip}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "no_cursor" || !st.HasWiki || st.Branch != "master" {
		t.Fatalf("应为 no_cursor: %+v", st)
	}
	if st.InheritedFrom != "" {
		t.Errorf("no_cursor 不得带继承来源: %+v", st)
	}
}
```

- [ ] **Step 2: 新增 inherited 三个测试（先红）**

```go
func TestCheckStatusInherited(t *testing.T) {
	// master 有游标，切 dev 后再提交 1 个 → 继承 master 基线，behind=1，超阈值 Stale
	dir := initRepo(t, 2)
	master := headOf(t, dir)
	run(t, dir, "checkout", "-q", "-b", "dev")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "d1")
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"master": {LastCommit: master}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "inherited" || st.InheritedFrom != "master" || st.LastCommit != master {
		t.Fatalf("应为 inherited(master): %+v", st)
	}
	if st.Behind != 1 || !st.Stale {
		t.Errorf("behind 应照算且走正常门控: %+v", st)
	}
	if !st.HasWiki || st.Branch != "dev" || st.BaseBranch != "master" {
		t.Errorf("基础字段应透传: %+v", st)
	}
}

func TestCheckStatusInheritedPrefersBase(t *testing.T) {
	// 基准游标可达即短路：即使其他游标距离更近也用基准
	dir := initRepo(t, 3)
	mid := func() string { // HEAD~1
		full, err := ResolveRevision(dir, "HEAD~1")
		if err != nil {
			t.Fatal(err)
		}
		return full
	}()
	tip := headOf(t, dir)
	run(t, dir, "checkout", "-q", "-b", "dev")
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{
		"master": {LastCommit: mid}, // 距离 1
		"other":  {LastCommit: tip}, // 距离 0，更近
	}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "inherited" || st.InheritedFrom != "master" || st.LastCommit != mid {
		t.Fatalf("基准可达应短路优先: %+v", st)
	}
}

func TestCheckStatusInheritedFallbackNearest(t *testing.T) {
	// 基准无游标，其余两条可达 → 取 rev-list 距离最近者
	dir := initRepo(t, 3)
	tip := headOf(t, dir)
	full, err := ResolveRevision(dir, "HEAD~2")
	if err != nil {
		t.Fatal(err)
	}
	run(t, dir, "checkout", "-q", "-b", "dev")
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{
		"far":  {LastCommit: full}, // 距离 2
		"near": {LastCommit: tip},  // 距离 0
	}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "inherited" || st.InheritedFrom != "near" || st.Behind != 0 {
		t.Fatalf("应取最近者 near: %+v", st)
	}
}
```

- [ ] **Step 3: 运行确认 4 个测试全红**

Run: `go test ./internal/wiki/ -run 'TestCheckStatusNoCursor|TestCheckStatusInherited' -v`
Expected: FAIL（`BranchState` 得到 `no_cursor`/字段不存在编译错误）

- [ ] **Step 4: 实现 Status.InheritedFrom 与 inheritCursor**

`internal/wiki/wiki.go` Status 结构体加字段（注释同步更新 BranchState 取值列表）：

```go
// BranchState 分支状态："ok"（正常）、"inherited"（读时继承可达游标）、"no_cursor"
// （本分支无基线且无游标可继承）、"diverged"（游标与本分支分叉）、"gone"（游标
// commit 被改写）、"legacy_orphan"（旧格式游标归属不可判）；非 git 项目与旧行为路径为空串。
type Status struct {
	// ...既有字段不动...
	InheritedFrom string `json:"inherited_from,omitempty"` // 继承来源分支（inherited 态非空）
}
```

CheckStatus 的 `!ok` 分支改为：

```go
	st.HasWiki = len(s.Cursors) > 0
	cur, ok := s.Cursors[branch]
	if !ok {
		// 本分支无游标：读时回退继承可达游标（不落盘）。基准分支优先
		// （一次 merge-base 短路），否则取可达且 rev-list 距离最近者。
		if branch != "" {
			if from, lc, behind, ok2 := inheritCursor(srcDir, s); ok2 {
				st.BranchState = "inherited"
				st.InheritedFrom = from
				st.LastCommit = lc
				st.Behind = behind
				st.Stale = threshold > 0 && behind >= threshold
				return st
			}
		}
		// 真·无基线：展示基准分支游标供提示使用
		if bc, ok2 := s.Cursors[s.BaseBranch]; ok2 {
			st.LastCommit = bc.LastCommit
		}
		if st.HasWiki {
			st.BranchState = "no_cursor"
		}
		return st
	}
```

文件末尾新增：

```go
// inheritCursor 找可继承游标：commit 是 HEAD 祖先的分支游标。基准分支优先
// （短路，不与其他游标比距离）；基准无游标或不可达时，取其余可达游标中
// rev-list 距离最近者。只读（git 调用上限 = 1 + 游标表大小，常态 1-2 条）。
func inheritCursor(srcDir string, s *State) (from, lastCommit string, behind int, ok bool) {
	try := func(c BranchCursor) (int, bool) {
		if c.LastCommit == "" || !isAncestor(srcDir, c.LastCommit, "HEAD") {
			return 0, false
		}
		n, err := countCommits(srcDir, c.LastCommit+"..HEAD")
		if err != nil {
			return 0, false
		}
		return n, true
	}
	if s.BaseBranch != "" {
		if bc, exists := s.Cursors[s.BaseBranch]; exists {
			if n, good := try(bc); good {
				return s.BaseBranch, bc.LastCommit, n, true
			}
		}
	}
	best := -1
	for name, c := range s.Cursors {
		if name == s.BaseBranch {
			continue
		}
		if n, good := try(c); good && (best < 0 || n < best) {
			best, from, lastCommit, ok = n, name, c.LastCommit, true
		}
	}
	return from, lastCommit, best, ok
}
```

- [ ] **Step 5: 运行 wiki 包全部测试**

Run: `go test ./internal/wiki/ -v`
Expected: PASS（含既有测试；注意 `TestCheckStatusNoCursor` 已改写）

- [ ] **Step 6: Commit**

```bash
git add internal/wiki/wiki.go internal/wiki/status_test.go
git commit -m "feat(wiki): CheckStatus 读时继承可达游标，新增 inherited 态"
```

---

### Task 2: hook 注入——inherited 上下文行与 nudge 去重

**Files:**
- Modify: `internal/hook/hook.go`（wikiContextLine :388-409；wikiNudge :346-371）
- Test: `internal/hook/hook_test.go`（新增 2 个测试，仿 TestPromptWikiBranchContextOnDiverged :709-732）

**Interfaces:**
- Consumes: `wiki.Status{BranchState:"inherited", InheritedFrom, LastCommit, Behind, Stale}`（Task 1）
- Produces: 注入文案两档（见下），`wikiNudge` 对 inherited+Stale 返回空

- [ ] **Step 1: 写失败测试**

```go
func TestPromptWikiInheritedContextLine(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2)
	head := gitHead(t, projDir)
	runGit(t, projDir, "checkout", "-q", "-b", "dev") // dev 无本分支游标，master 游标可达 → inherited
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: head}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	writeEntry(t, kbRoot, "条目.md", "---\ntitle: 测试条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文含线索词 InhCue。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"InhCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "当前分支 dev 继承该基线") {
		t.Errorf("inherited 未超阈值应为纯上下文行: %q", got)
	}
	if strings.Contains(got, "尚无基线") {
		t.Errorf("可继承场景不得再报尚无基线: %q", got)
	}
}

func TestPromptWikiInheritedStaleMergedLine(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1)
	head := gitHead(t, projDir) // 切分支前的 master tip 作为游标
	runGit(t, projDir, "checkout", "-q", "-b", "dev")
	runGit(t, projDir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "d1")
	runGit(t, projDir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "d2")
	// 阈值 1：inherited + Stale（behind=2）
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[wiki]\nstale_commits = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: head}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	writeEntry(t, kbRoot, "条目.md", "---\ntitle: 测试条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文含线索词 StaleInhCue。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"StaleInhCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "当前分支 dev 继承，落后 2 commit") || !strings.Contains(got, "建议更新") {
		t.Errorf("超阈值应为合并行（带落后数）: %q", got)
	}
	if strings.Contains(got, "wiki 已落后") {
		t.Errorf("wikiNudge 不得重复提醒（去重）: %q", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/hook/ -run 'TestPromptWikiInherited' -v`
Expected: FAIL（输出含「尚无基线」，无「继承该基线」）

- [ ] **Step 3: 实现 wikiContextLine inherited 分支与 wikiNudge 去重**

`internal/hook/hook.go` wikiContextLine 的 switch 加分支（`short` 截断逻辑复用）：

```go
	case "inherited":
		if s.Stale && s.Behind > 0 {
			return fmt.Sprintf("[OpenKnowledge] wiki 基于 %s@%s（当前分支 %s 继承，落后 %d commit），结构描述以 %s 为准，建议更新。\n",
				s.InheritedFrom, short, s.Branch, s.Behind, s.InheritedFrom)
		}
		return fmt.Sprintf("[OpenKnowledge] wiki 基于 %s@%s；当前分支 %s 继承该基线，结构描述以 %s 为准。\n",
			s.InheritedFrom, short, s.Branch, s.InheritedFrom)
```

wikiNudge 的 stale 分支去重（在置 msg 之前短路，不置 WikiNudged——提醒已并入上下文行，上下文行是 standing 的，每轮都在，无需消耗每会话一次预算）：

```go
	case s.HasWiki && s.Stale:
		if s.BranchState == "inherited" {
			return "" // 提醒已并入 wikiContextLine 的 inherited 行，避免同语义双行
		}
		msg = fmt.Sprintf("[OpenKnowledge] wiki 已落后 %d 个 commit，建议用 openknowledge-wiki 技能增量更新。", s.Behind)
```

- [ ] **Step 4: 运行 hook 包全部测试**

Run: `go test ./internal/hook/ -v`
Expected: PASS（`TestPromptWikiBranchContextOnDiverged` 断言的子串 `wiki 基于 master@` + `当前分支 dev` 在 inherited 行中仍成立，无需改动）

- [ ] **Step 5: Commit**

```bash
git add internal/hook/hook.go internal/hook/hook_test.go
git commit -m "feat(hook): inherited 态上下文行两档文案，stale 提醒并入去重"
```

---

### Task 3: CLI——ok wiki status 透传 inherited_from

**Files:**
- Modify: `internal/cli/cli.go`（status 分支 :855-896）
- Test: `internal/cli/cli_test.go`（仿 :709-752 的 WikiCmd status 测试）

**Interfaces:**
- Consumes: `wiki.Status.InheritedFrom`（Task 1）
- Produces: status JSON 新增 `inherited_from` 字段（omitempty 语义：仅在非空时输出）

- [ ] **Step 1: 写失败测试**

```go
// ok wiki status：本分支无游标但基准游标可达 → branch_state=inherited + inherited_from。
func TestWikiStatusInheritedFrom(t *testing.T) {
	repo, stateDir := setupWikiProject(t)
	masterTip, err := wiki.HeadCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "checkout", "-q", "-b", "dev")
	runGit(t, repo, "commit", "--allow-empty", "-m", "d1")
	if err := wiki.SaveState(stateDir, &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{
		"master": {LastCommit: masterTip},
	}}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatalf("status exit %d err=%q", code, errb.String())
	}
	var st map[string]any
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st["branch_state"] != "inherited" || st["inherited_from"] != "master" {
		t.Fatalf("应透传 inherited/inherited_from: %v", st)
	}
	if st["behind"].(float64) != 1 {
		t.Errorf("behind 应为 1: %v", st)
	}
}
```

（`setupWikiProject`/`runGit` 为 `cli_test.go` 既有助手，见 :682-685 同款用法。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/cli/ -run TestWikiStatusInheritedFrom -v`
Expected: FAIL（JSON 无 inherited_from 键）

- [ ] **Step 3: 实现**

`internal/cli/cli.go` status 分支在 `out["branch_state"]` 之后加：

```go
		if st.InheritedFrom != "" {
			out["inherited_from"] = st.InheritedFrom
		}
```

说明：status 输出是 JSON（供技能/GUI 消费），spec §5 的「键值块 + 建议文案」由消费侧呈现，CLI 层只做字段透传（最小改动）。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): wiki status 透传 inherited_from"
```

---

### Task 4: GUI 后端——branch-info 端点透传分支状态

**Files:**
- Modify: `internal/gui/api.go`（apiProjectBranchInfo :1326-1351）
- Test: `internal/gui/api_test.go`（扩展 TestBranchInfoAPI :1012-1080）

**Interfaces:**
- Consumes: `wiki.CheckStatus`、`config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))`（`internal/gui/llm.go:345` 同款加载）
- Produces: branch-info JSON 新增字段：`branch_state`、`inherited_from`、`behind`、`stale`、`last_commit`（短哈希由前端截）、`generated_at`、`entry_count`；全部 fail-open（CheckStatus 不返回错误，git 失败时字段为空/零值，前端据 branch_state 缺失回退）

- [ ] **Step 1: 写失败测试**

在 TestBranchInfoAPI 基础上加子场景：demo 项目为 git 仓库（api_test 现有 git 夹具模式，参考 :1014-1056），SaveState 写 master 游标、checkout 出 dev，断言响应含 `"branch_state":"inherited"`、`"inherited_from":"master"`、`"behind"` 为数字。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gui/ -run TestBranchInfoAPI -v`
Expected: FAIL（无 branch_state 键）

- [ ] **Step 3: 实现**

apiProjectBranchInfo 中找到项目路径的分支（`out["current_branch"]` 赋值处）之后加：

```go
				path := p.Paths[0]
				out["current_branch"] = wiki.CurrentBranch(path)
				cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
				if err != nil {
					cfg = config.Default() // fail-open：阈值回退默认
				}
				ws := wiki.CheckStatus(st.StateDir(), path, cfg.Wiki.StaleCommits)
				if ws.BranchState != "" {
					out["branch_state"] = ws.BranchState
				}
				if ws.InheritedFrom != "" {
					out["inherited_from"] = ws.InheritedFrom
				}
				out["behind"] = ws.Behind
				out["stale"] = ws.Stale
				if ws.LastCommit != "" {
					out["last_commit"] = ws.LastCommit
				}
```

注意先确认 `config.Default` 是否存在（`internal/config`）；不存在则用 `config.LoadMerged("", ...)` 或字面默认 20。`generated_at`/`entry_count` 从 `wiki.LoadState` 的对应游标取（继承来源分支的游标）。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/gui/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gui/api.go internal/gui/api_test.go
git commit -m "feat(gui): branch-info 透传 branch_state/inherited_from/behind"
```

---

### Task 5: GUI 前端——分支上下文状态卡（变体 B）

**Files:**
- Modify: `web/index.html`（:52 merge-lineage 后加卡片挂载点）
- Modify: `web/style.css`（:223-227 徽标区后追加样式）
- Modify: `web/app.js`（renderBranchInfo :320-347）

**Interfaces:**
- Consumes: Task 4 的 branch-info 新字段
- Produces: 状态卡 DOM（`#wiki-card`）与徽标 `badge-inherit`；无 `branch_state` 时零渲染变化

- [ ] **Step 1: index.html 挂载点**

`web/index.html` 第 52 行 merge-lineage 后加：

```html
      <div id="wiki-card" class="wiki-card hidden"></div>
```

- [ ] **Step 2: style.css 样式（色板取自原型，与既有徽标一致）**

```css
.badge-inherit { background: #e6f4ea; color: #1e7e34; border-radius: 4px; padding: 1px 6px; font-size: 12px; white-space: nowrap; }
.wiki-card { border: 1px solid #e3e5ea; border-radius: 8px; background: #fff; padding: 12px 16px; margin: 6px 0; max-width: 560px; font-size: 13px; }
.wiki-card .row { display: flex; justify-content: space-between; padding: 3px 0; }
.wiki-card .row .k { color: #8a8f9c; }
.wiki-card .act { margin-top: 8px; display: flex; gap: 8px; }
```

- [ ] **Step 3: app.js 渲染逻辑**

renderBranchInfo 的 `.then` 内、现有基准/当前分支渲染之后加（fail-open：无 branch_state 或 inherited 之外的状态 → 卡片隐藏，现状不变）：

```javascript
      var card = $("wiki-card");
      if (card) {
        if (info.branch_state === "inherited") {
          var short = (info.last_commit || "").slice(0, 7);
          var rows = [
            ["基准分支", info.base_branch || "—"],
            ["当前分支", info.current_branch || "—"],
            ["基线", "继承自 " + (info.inherited_from || "—") + "@" + short],
            ["落后", info.behind + " commit（阈值 " + (info.threshold || 20) + "）" + (info.stale ? "，建议更新" : "")],
          ];
          card.innerHTML = rows.map(function (r) {
            return '<div class="row"><span class="k"></span><span class="v"></span></div>';
          }).join("");
          // 逐格填文本（textContent 防注入），再追加动作行
          var rowEls = card.querySelectorAll(".row");
          rows.forEach(function (r, i) {
            rowEls[i].querySelector(".k").textContent = r[0];
            rowEls[i].querySelector(".v").textContent = r[1];
          });
          var act = document.createElement("div");
          act.className = "act";
          act.innerHTML = '<button type="button" class="btn btn-primary">在本分支更新 wiki</button>' +
            '<button type="button" class="btn">查看分支差异</button>';
          var btns = act.querySelectorAll("button");
          btns[0].onclick = function () { copyText("在 agent 会话运行 /openknowledge-wiki 更新本分支 wiki"); };
          btns[1].onclick = function () { copyText("ok wiki diff"); };
          card.appendChild(act);
          card.classList.remove("hidden");
          // 工具栏摘要：超阈值时在继承徽标上带落后数
          if (info.stale) { c.textContent = cur + " "; var tag = document.createElement("span"); tag.className = "badge-inherit"; tag.textContent = "wiki 落后 " + info.behind; c.appendChild(tag); }
        } else {
          card.classList.add("hidden");
        }
      }
```

`copyText` 若 app.js 已有剪贴板助手则复用（先 Grep `clipboard` 确认），没有就写最小实现（`navigator.clipboard.writeText` + 既有 toast/提示机制，无则 `title` 提示即可）。** inherited 时当前分支不再加 `branch-warn`**（原 :332 的条件需排除 inherited：`info.branch_state !== "inherited"` 才加 warn；无 branch_state 的旧后端行为不变）。

- [ ] **Step 4: 手动验证**

Run: `go build ./... && ./ok.exe gui`（或项目既有 GUI 启动方式），在 master 游标存在时切到 dev 分支打开管理页
Expected: inherited 场景出状态卡；基准分支与旧数据（无 branch_state）零变化

- [ ] **Step 5: Commit**

```bash
git add web/index.html web/style.css web/app.js
git commit -m "feat(gui): 分支上下文状态卡（inherited 态，变体 B）"
```

---

### Task 6: 文档与收尾回归

**Files:**
- Modify: `web/help.md`（:88 切分支 FAQ）
- Create: `docs/changelogs/<下一版本号>.md`（版本号以 `git tag` 最新为准递增 patch/minor；仓库有 changelog_required 强制链路）
- Modify: `docs/superpowers/specs/2026-08-19-wiki-baseline-inheritance-design.md`（状态行改「已实现」）

- [ ] **Step 1: help.md 更新**

:88 的 FAQ 行改为（口径：可继承时提示继承而非尚无基线）：

```markdown
- **切了 git 分支**：wiki 是分支感知的（2.6+）——新分支自动继承可达基线并提示"wiki 基于 <基准分支>；当前分支 X 继承该基线"，落后超阈值照常提醒；长期并行分支用 `/openknowledge-wiki` 在该分支生成差异条目（2.7+），互不影响
```

- [ ] **Step 2: changelog 条目**

按 `docs/changelogs/` 既有格式写新条目（参考 `docs/changelogs/2026-08-16-rrf-fusion.md` 的结构），正文不硬折行（项目排版约定）。

- [ ] **Step 3: 全量回归**

Run: `go test ./...`
Expected: 全绿

Run: `go vet ./...`
Expected: 无输出

- [ ] **Step 4: 验收对照 spec §10 逐条人工核对**

1. 切新分支首问：注入「继承该基线」✓/✗
2. 超阈值：上下文行带落后数且无第二行重复提醒 ✓/✗
3. 游离历史切出：仍「尚无基线」✓/✗（TestCheckStatusNoCursor 覆盖）
4. 基准分支注入逐字节不变 ✓/✗（TestPromptWikiNoContextOnBaseBranch 覆盖）
5. wiki.json 零写入 ✓/✗（git status 确认注入后 state 目录无变更）
6. GUI 状态卡与 fail-open 回退 ✓/✗
7. 全量测试绿 ✓/✗

- [ ] **Step 5: Commit**

```bash
git add web/help.md docs/changelogs/ docs/superpowers/specs/2026-08-19-wiki-baseline-inheritance-design.md
git commit -m "docs: wiki 基线继承 help/changelog/spec 状态收尾"
```

---

## Self-Review 记录

- **Spec 覆盖**：§3 检测层→Task 1；§4 注入→Task 2；§5 CLI/GUI→Task 3/4/5；§6 边界→Task 1 不落盘设计 + Step 验收 5；§7 铁律→Global Constraints；§8 测试→各 Task 测试步骤；§10 验收→Task 6 Step 4
- **已知留白**（实现时现场核对，均给出定位）：Task 4 的 `config.Default` 是否存在（internal/config）；Task 5 的 `copyText`/剪贴板助手（Grep app.js `clipboard`）
- **类型一致性**：`InheritedFrom`/`inherited_from`/`BranchState=="inherited"` 在 Task 1-5 间拼写一致
