# wiki 分支差异条目（二期）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 分支差异条目全链路——检索/注入按当前分支过滤、`ok wiki diff` 供素材、技能差异流程、已并入检测、GUI 分支列与过滤器，顺带收口一期遗留（写侧防呆内生、git 调用收敛）。

**Architecture:** 分支维度只落 tags 约定（`branch:<名>`）；index 包产出过滤与原语（Hit.Tags、INDEX 双段、裁剪函数），hook 注入层消费；`ok wiki diff`/merged 检测在 wiki+cli 层；GUI 纯前端过滤（API 零改动）。

**Tech Stack:** Go 1.25（无新依赖）；原生 JS SPA；外部 git 经 procx 静默封装。

**Spec:** `docs/superpowers/specs/2026-08-08-wiki-branch-delta-design.md`（已批准，目标 v2.7.0）

## Global Constraints

- **零回归**：项目无任何 `branch:` 标签条目时，注入文本、INDEX.md 字节、status 输出与一期逐字节一致（现有测试全绿即证据之一）
- **fail-open**：非 git/分支未知（`Status.Branch==""`）时**不过滤不裁剪**（宁多勿漏）；git 失败不产生提示
- **写入收敛**：已并入检测只读；差异条目删除只发生在技能流程经用户确认；本计划不改 `ok add` 的任何行为
- Status JSON 只加键不改键（新增 `merged_branches`，有值才出）
- 过滤语义精确值：条目 tags 含 `branch:X` 且 X ≠ 当前分支 → 注入侧丢弃；X == 当前分支 → 正常命中；无 `branch:` 标签 → 所有分支可见
- INDEX 小节格式逐字约定：主节 `## Wiki 目录`（仅无分支条目），差异节 `## 分支差异（<分支名>）`（半角括号全角（）——**与一期既有文本一致用全角括号**，见 Task 1 Step 3 代码）
- 本机 autocrlf：gofmt 用去 CR 比对；测试夹具惯例（OK_HOME 隔离 + registry.Save + git `-c commit.gpgsign=false`）；注释/提交信息中文；只 git add 本任务文件
- `internal/index` 的 tags 列存储形态：`strings.Join(tags, ", ")`（如 `wiki, branch:dev`）——拆分按 `", "`

---

### Task 1: index 包分支原语（Hit.Tags / WikiEntry.Branch / INDEX 双段 / 过滤与裁剪函数）

**Files:**
- Modify: `internal/index/query.go`（Hit 加 Tags、两条 SELECT 带 e.tags、WikiEntry 加 Branch、WikiEntries 带 tags）
- Modify: `internal/index/sync.go`（rebuildIndex 双段输出）
- Create: `internal/index/branch.go`（BranchOf/FilterHitsByBranch/TrimIndexBranchSections）
- Test: `internal/index/branch_test.go`（新建）+ `internal/index/` 既有测试适配

**Interfaces:**
- Consumes: 现有 entries 表（tags 列为 ", " 拼接）
- Produces（Task 2/3/5 依赖，签名不得改）:
  ```go
  // branch.go
  func BranchOf(tags []string) string                          // 首个 branch:<名> 的值；无则 ""
  func FilterHitsByBranch(hits []Hit, branch string) []Hit     // branch=="" 原样返回
  func TrimIndexBranchSections(idx, branch string) string      // branch=="" 原样返回
  // query.go
  type Hit struct { Filename, Title, Type, Summary, Body string; Tags []string; Score float64 }
  type WikiEntry struct { Title, Filename, Summary string; Branch string }
  func (db *DB) HasBranchWiki(branch string) (bool, error)     // 该分支是否有差异条目（draft=0 且 tags 含 wiki 且 Branch 精确匹配）
  ```

- [ ] **Step 1: 写失败测试（branch_test.go）**

```go
package index

import (
	"strings"
	"testing"
)

func TestBranchOf(t *testing.T) {
	if got := BranchOf([]string{"wiki", "branch:dev"}); got != "dev" {
		t.Errorf("got %q", got)
	}
	if got := BranchOf([]string{"wiki", "agentx"}); got != "" {
		t.Errorf("无分支标签应为空，got %q", got)
	}
	if got := BranchOf(nil); got != "" {
		t.Errorf("nil 应为空，got %q", got)
	}
}

func TestFilterHitsByBranch(t *testing.T) {
	hits := []Hit{
		{Filename: "a.md", Title: "共享", Tags: []string{"wiki"}},
		{Filename: "b.md", Title: "dev 差异", Tags: []string{"wiki", "branch:dev"}},
		{Filename: "c.md", Title: "master 差异", Tags: []string{"wiki", "branch:master"}},
	}
	got := FilterHitsByBranch(hits, "dev")
	if len(got) != 2 || got[0].Title != "共享" || got[1].Title != "dev 差异" {
		t.Fatalf("dev 视角应留共享+dev: %+v", got)
	}
	// 分支未知：不过滤
	if got := FilterHitsByBranch(hits, ""); len(got) != 3 {
		t.Fatalf("空分支不过滤: %+v", got)
	}
}

func TestTrimIndexBranchSections(t *testing.T) {
	idx := "# 知识索引\n\n- **x** (note) [] — s\n\n## Wiki 目录\n\n- [架构总览](a.md) — s\n\n## 分支差异（dev）\n\n- [架构总览（dev 分支差异）](b.md) — s\n\n## 分支差异（hotfix）\n\n- [x（hotfix 分支差异）](c.md) — s\n"
	got := TrimIndexBranchSections(idx, "dev")
	if !strings.Contains(got, "## 分支差异（dev）") || !strings.Contains(got, "架构总览（dev 分支差异）") {
		t.Errorf("当前分支小节应保留: %q", got)
	}
	if strings.Contains(got, "hotfix") {
		t.Errorf("其他分支小节应裁掉: %q", got)
	}
	if !strings.Contains(got, "## Wiki 目录") || !strings.Contains(got, "[架构总览](a.md)") {
		t.Errorf("主目录不受影响: %q", got)
	}
	// 空分支不裁剪
	if got := TrimIndexBranchSections(idx, ""); got != idx {
		t.Errorf("空分支应原样返回")
	}
	// 无差异小节：逐字节不变
	plain := "# 知识索引\n\n## Wiki 目录\n\n- [a](a.md)\n"
	if got := TrimIndexBranchSections(plain, "dev"); got != plain {
		t.Errorf("无差异小节应逐字节不变（零回归）")
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/index/ -run "TestBranchOf|TestFilterHits|TestTrimIndex" -count=1`
Expected: 编译失败（三个函数未定义）

- [ ] **Step 3: 实现 branch.go（完整代码）**

```go
package index

import "strings"

// BranchOf 提取条目的分支标签（branch:<名>，取第一个）；无则空串。
func BranchOf(tags []string) string {
	for _, t := range tags {
		if strings.HasPrefix(t, "branch:") {
			return strings.TrimPrefix(t, "branch:")
		}
	}
	return ""
}

// splitTags 把 entries.tags 列（", " 拼接）拆回切片。
func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ", ")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FilterHitsByBranch 丢弃其他分支的差异条目；branch 为空（非 git/未知）不过滤（宁多勿漏）。
func FilterHitsByBranch(hits []Hit, branch string) []Hit {
	if branch == "" {
		return hits
	}
	out := hits[:0]
	for _, h := range hits {
		if b := BranchOf(h.Tags); b != "" && b != branch {
			continue
		}
		out = append(out, h)
	}
	return out
}

// TrimIndexBranchSections 裁剪 INDEX.md 的"## 分支差异（X）"小节：只保留 branch 的，
// 其余整节移除；branch 为空或无差异小节时逐字节返回原文（零回归）。
// 小节边界：下一个 "## " 级标题或 EOF。
func TrimIndexBranchSections(idx, branch string) string {
	if branch == "" || !strings.Contains(idx, "## 分支差异（") {
		return idx
	}
	lines := strings.Split(idx, "\n")
	var b strings.Builder
	dropping := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "## ") {
			dropping = false
			if strings.HasPrefix(ln, "## 分支差异（") {
				name := strings.TrimPrefix(ln, "## 分支差异（")
				if i := strings.Index(name, "）"); i >= 0 {
					name = name[:i]
				}
				dropping = name != branch
			}
		}
		if !dropping {
			b.WriteString(ln)
			b.WriteString("\n")
		}
	}
	// Split/重组会规整末尾换行：与原文不一致时回退原文比对调用方无感知——此处直接返回重组结果，
	// 保证语义等价（末尾多一个换行属可接受偏差？不允许：见下）
	return strings.TrimSuffix(b.String(), "\n") + trailingNL(idx)
}

// trailingNL 保留原文末尾换行形态。
func trailingNL(s string) string {
	if strings.HasSuffix(s, "\n") {
		return "\n"
	}
	return ""
}
```

注意：`TrimIndexBranchSections` 末尾换行处理若与你的实现不同，以"`plain` 用例逐字节不变"为验收锚点自校准。

- [ ] **Step 4: query.go 扩展（Hit.Tags、WikiEntry.Branch、HasBranchWiki）**

- `Hit` 结构加 `Tags []string`（注释：供注入层按分支过滤）
- FTS 通道 SELECT 加 `e.tags`，Scan 后 `h.Tags = splitTags(tagsStr)`；向量通道同样处理（两处 SELECT 都在 WHERE mandatory=0 AND draft=0）
- `WikiEntry` 加 `Branch string`；`WikiEntries()` SQL 改 `SELECT title, filename, summary, tags FROM entries WHERE draft = 0 AND tags LIKE '%wiki%' ORDER BY title`，Scan 后 `e.Branch = BranchOf(splitTags(tagsStr))`
- 新增：

```go
// HasBranchWiki 报告指定分支是否存在已转正的差异条目（wiki 标签且 branch 精确匹配）。
func (db *DB) HasBranchWiki(branch string) (bool, error) {
	entries, err := db.WikiEntries()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Branch == branch {
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 5: sync.go rebuildIndex 双段**

Wiki 目录段替换为：

```go
	if wikiEntries, err := db.WikiEntries(); err == nil && len(wikiEntries) > 0 {
		writeWikiLine := func(b *strings.Builder, we WikiEntry) {
			if we.Summary != "" {
				fmt.Fprintf(b, "- [%s](%s) — %s\n", we.Title, we.Filename, we.Summary)
			} else {
				fmt.Fprintf(b, "- [%s](%s)\n", we.Title, we.Filename)
			}
		}
		b.WriteString("\n## Wiki 目录\n\n")
		branches := map[string][]WikiEntry{}
		for _, we := range wikiEntries {
			if we.Branch == "" {
				writeWikiLine(&b, we)
			} else {
				branches[we.Branch] = append(branches[we.Branch], we)
			}
		}
		names := make([]string, 0, len(branches))
		for n := range branches {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "\n## 分支差异（%s）\n\n", n)
			for _, we := range branches[n] {
				writeWikiLine(&b, we)
			}
		}
	}
```

（sync.go import 加 sort；无分支条目时输出与现状逐字节一致——主节行序不变。）

- [ ] **Step 6: 跑测试确认绿 + 回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/index/ ./internal/hook/ -count=1 && go test ./... -count=1`
Expected: 全 PASS（既有测试零改动全绿 = 零回归证据；若有断言 INDEX 逐字内容的既有测试失败，检查是否确因双段新行为——无分支条目的项目输出必须逐字节不变，失败即实现 bug）

- [ ] **Step 7: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/index/query.go internal/index/sync.go internal/index/branch.go internal/index/branch_test.go
git commit -m "feat(index): 分支原语——Hit.Tags/WikiEntry.Branch、INDEX 双段、过滤与裁剪函数"
```

---

### Task 2: hook 注入按分支过滤 + INDEX 裁剪

**Files:**
- Modify: `internal/hook/core.go`（InjectForPrompt：ws 前移、INDEX 裁剪、hits 过滤）
- Test: `internal/hook/hook_test.go`（追加）

**Interfaces:**
- Consumes: Task 1 的 `FilterHitsByBranch`/`TrimIndexBranchSections`；既有 `wiki.CheckStatus`（Status.Branch）
- Produces: 无新签名（行为变化）

- [ ] **Step 1: 写失败测试**

```go
func TestPromptFiltersOtherBranchEntries(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1) // 当前分支 master
	writeEntry(t, kbRoot, "共享.md", "---\ntitle: 共享规约\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n共享正文含线索词 FilterCue。\n")
	writeEntry(t, kbRoot, "dev差异.md", "---\ntitle: dev 差异经验\ntype: note\ntags: [\"branch:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\ndev 正文也含线索词 FilterCue。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"FilterCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "共享规约") {
		t.Errorf("共享条目应命中: %q", got)
	}
	if strings.Contains(got, "dev 差异经验") {
		t.Errorf("master 会话不得命中 branch:dev 条目: %q", got)
	}
}

func TestPromptIndexTrimmedByBranch(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1)
	// 一条 dev 差异 wiki 条目（触发 INDEX 双段）
	writeEntry(t, kbRoot, "差异.md", "---\ntitle: 架构总览（dev 分支差异）\ntype: reference\ntags: [\"wiki\", \"branch:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	writeEntry(t, kbRoot, "主条目.md", "---\ntitle: 架构总览\ntype: reference\ntags: [\"wiki\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s2","cwd":%q,"prompt":"hello"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "架构总览") {
		t.Errorf("主目录应注入: %q", got)
	}
	if strings.Contains(got, "分支差异（dev）") || strings.Contains(got, "（dev 分支差异）") {
		t.Errorf("master 会话的 INDEX 注入不得含 dev 差异小节: %q", got)
	}
}
```

（writeEntry/helper 用 hook_test.go 现有者；initGitRepo 已钉 `-b master`。）

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/hook/ -run "TestPromptFilters|TestPromptIndexTrimmed" -count=1`
Expected: FAIL（dev 差异条目仍命中/INDEX 未裁剪）

- [ ] **Step 3: 实现（core.go InjectForPrompt 改造）`

把 `ws := wiki.CheckStatus(...)` 从尾部前移到 `st := state.Load(...)` 之后：

```go
	st := state.Load(pc.Store.StateDir(), sessionID)
	ws := wiki.CheckStatus(pc.Store.StateDir(), cwd, pc.Config.Wiki.StaleCommits)
```

INDEX 读取处改为裁剪后写入：

```go
		if idx, err := os.ReadFile(pc.Store.IndexPath()); err == nil {
			b.Write(index.TrimIndexBranchSections(string(idx), ws.Branch))
		}
```

hits 过滤（`hits, err := db.Query(...)` 之后）：

```go
	hits = index.FilterHitsByBranch(hits, ws.Branch)
```

尾部删除原 `ws := wiki.CheckStatus(...)` 重复行（ws 复用前移的）；`wikiContextLine(ws)`/`wikiNudge(pc, st, ws)` 调用不变。core.go import 确认 `"openknowledge/internal/index"` 与 `"openknowledge/internal/wiki"` 均在。

- [ ] **Step 4: 跑测试确认绿 + 全量回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/hook/ ./internal/rxext/ -count=1 && go test ./... -count=1`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/hook/core.go internal/hook/hook_test.go
git commit -m "feat(hook): 注入按当前分支过滤差异条目 + INDEX 差异小节裁剪（分支未知不过滤）"
```

---

### Task 3: `ok wiki diff` + 已并入检测 + CheckStatus 性能收敛

**Files:**
- Create: `internal/wiki/diff.go`（DiffSummary + MergedIntoBase）
- Modify: `internal/wiki/wiki.go`（CheckStatus 收敛 git 调用）
- Modify: `internal/cli/cli.go`（WikiCmd 加 `diff` 分支；status 输出 `merged_branches`）
- Test: `internal/wiki/diff_test.go`（新建）、`internal/cli/cli_test.go`（追加）

**Interfaces:**
- Consumes: Task 1 的 `HasBranchWiki`；既有 git 原语（status.go 的 gitOut/isAncestor 等私有函数）
- Produces:
  ```go
  // diff.go
  func DiffSummary(srcDir, base string) (string, error)        // 结构变化摘要；无分叉/非 git 返回说明性空摘要
  func MergedIntoBase(s *State, srcDir string, hasDelta func(string) bool) []string
  ```
  CLI：`ok wiki diff`（WikiCmd 新分支）；status JSON 新键 `merged_branches`（仅非空且当前在基准分支时输出）

**性能收敛口径（修正终审建议）**：`rev-list X..HEAD` 对"存在但非祖先"的 X **也会成功**——不能用它隐含可达性。正确收敛：用 `merge-base` 结果判别——`mb=="" 且 commitExists 失败` → gone；`mb==lc` → ok（再跑 rev-list 算 behind）；`mb!="" && mb!=lc` → diverged。ok 路径 git 调用从 4 次降为 3 次（symbolic-ref + merge-base + rev-list）。五态语义不变（Task 2 全部状态测试必须不改且全绿）。

- [ ] **Step 1: 写失败测试（diff_test.go）**

```go
package wiki

import (
	"strings"
	"testing"
)

func TestDiffSummary(t *testing.T) {
	dir := initRepo(t, 2) // Task 2 的夹具
	run(t, dir, "checkout", "-q", "-b", "dev")
	// dev 上：新增目录 internal/foo 两个文件 + 改根目录 README
	mkdirWrite(t, dir, "internal/foo/a.go", "package foo\n")
	mkdirWrite(t, dir, "internal/foo/b.go", "package foo\n")
	writeFile(t, dir, "README.md", "v2\n")
	run(t, dir, "add", ".")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "dev work")
	out, err := DiffSummary(dir, "master")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"master", "dev", "internal/foo", "README.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("摘要缺 %q:\n%s", want, out)
		}
	}
}

func TestDiffSummaryNoBase(t *testing.T) {
	dir := initRepo(t, 1)
	out, err := DiffSummary(dir, "不存在的分支")
	if err != nil {
		t.Fatal(err) // fail-open：错误也要返回说明性文本而非 err？见实现注
	}
	_ = out // 具体行为以实现注为准
}

func TestMergedIntoBase(t *testing.T) {
	dir := initRepo(t, 2)
	run(t, dir, "checkout", "-q", "-b", "dev")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "d1")
	run(t, dir, "checkout", "-q", "master")
	run(t, dir, "merge", "-q", "--no-ff", "dev", "-m", "merge dev")
	s := &State{BaseBranch: "master", Cursors: map[string]BranchCursor{
		"master": {LastCommit: headOf(t, dir)},
		"dev":    {LastCommit: headOfAt(t, dir, "dev")},
	}}
	hasDelta := func(b string) bool { return b == "dev" }
	got := MergedIntoBase(s, dir, hasDelta)
	if len(got) != 1 || got[0] != "dev" {
		t.Fatalf("应检出 dev 已并入: %v", got)
	}
	// 无差异条目则不报
	if got := MergedIntoBase(s, dir, func(string) bool { return false }); len(got) != 0 {
		t.Fatalf("无差异条目不报: %v", got)
	}
}
```

（`mkdirWrite`/`writeFile` 占位——用既有夹具风格补文件级 helper。`TestDiffSummaryNoBase` 的行为在实现时定死并写成断言：推荐——base 不存在/无共同祖先时返回 `("", nil)` 表示无摘要，cli 层打印"无法计算分叉点"。）

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ -run "TestDiffSummary|TestMergedIntoBase" -count=1`
Expected: 编译失败

- [ ] **Step 3: 实现 diff.go**

```go
package wiki

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DiffSummary 输出当前分支相对基准分叉点的结构变化摘要（供 openknowledge-wiki 技能消化）。
// base 为空/分叉点不可算/非 git 时返回 ("", nil)——fail-open，由调用方打印说明。
func DiffSummary(srcDir, base string) (string, error) {
	branch := CurrentBranch(srcDir)
	if branch == "" || base == "" {
		return "", nil
	}
	mb := mergeBase(srcDir, base, "HEAD")
	if mb == "" {
		return "", nil
	}
	ns, err := gitOut(srcDir, "diff", "--name-status", mb+"..HEAD")
	if err != nil {
		return "", err
	}
	num, _ := gitOut(srcDir, "diff", "--numstat", mb+"..HEAD") // 失败不致命（Top-N 留空）

	var b strings.Builder
	fmt.Fprintf(&b, "基准分支: %s（分叉点 %s）\n当前分支: %s\n\n", base, short(mb), branch)

	// 目录与文件聚类
	type dirStat struct{ add, del int }
	dirs := map[string]*dirStat{}
	exts := map[string][2]int{} // ext -> [新增, 删除]
	for _, ln := range strings.Split(ns, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		status, path := f[0], f[len(f)-1]
		top := strings.SplitN(path, "/", 2)[0]
		d := dirs[top]
		if d == nil {
			d = &dirStat{}
			dirs[top] = d
		}
		ext := ""
		if i := strings.LastIndex(path, "."); i >= 0 {
			ext = path[i:]
		}
		e := exts[ext]
		switch status[0] {
		case 'A':
			d.add++
			exts[ext] = [2]int{e[0] + 1, e[1]}
		case 'D':
			d.del++
			exts[ext] = [2]int{e[0], e[1] + 1}
		}
	}
	b.WriteString("目录变化:\n")
	for _, n := range sortedKeysDS(dirs) {
		d := dirs[n]
		fmt.Fprintf(&b, "  %s（+%d/-%d 文件）\n", n, d.add, d.del)
	}
	b.WriteString("文件增删:\n")
	for _, e := range sortedKeysExt(exts) {
		c := exts[e]
		fmt.Fprintf(&b, "  %s +%d/-%d\n", extOrNone(e), c[0], c[1])
	}
	// Top-10 变更文件
	type nf struct {
		path      string
		add, del  int
	}
	var tops []nf
	for _, ln := range strings.Split(num, "\n") {
		f := strings.Fields(ln)
		if len(f) != 3 {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		tops = append(tops, nf{f[2], a, d})
	}
	sort.Slice(tops, func(i, j int) bool { return tops[i].add+tops[i].del > tops[j].add+tops[j].del })
	if len(tops) > 10 {
		tops = tops[:10]
	}
	if len(tops) > 0 {
		b.WriteString("变更最多:\n")
		for _, t := range tops {
			fmt.Fprintf(&b, "  %s（+%d/-%d）\n", t.path, t.add, t.del)
		}
	}
	return b.String(), nil
}

// MergedIntoBase 返回已并入基准的分支清单：cursors 中每条非基准分支，
// 分支引用仍存在、tip 已是 HEAD 祖先、且 hasDelta 报告有差异条目时计入。
// 仅在当前处于基准分支时由调用方触发；分支已删除的静默跳过。
func MergedIntoBase(s *State, srcDir string, hasDelta func(string) bool) []string {
	if s == nil {
		return nil
	}
	var out []string
	for name := range s.Cursors {
		if name == "" || name == s.BaseBranch {
			continue
		}
		if _, err := gitOut(srcDir, "rev-parse", "--verify", "--quiet", name); err != nil {
			continue // 分支已删
		}
		if !isAncestor(srcDir, name, "HEAD") {
			continue
		}
		if hasDelta != nil && !hasDelta(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
```

helper（short/sortedKeysDS/sortedKeysExt/extOrNone）同文件补齐（short 截 7 位；两个 sortedKeys 取 map 键排序；extOrNone 空扩展名显示 "(无扩展名)"）。

- [ ] **Step 4: CheckStatus 性能收敛（wiki.go）**

游标分支的三段 switch 改为：

```go
	lc := cur.LastCommit
	if lc == "" {
		return st // 非 git mark 的时间戳游标，同旧行为
	}
	mb := mergeBase(srcDir, lc, "HEAD")
	if mb == "" {
		if !commitExists(srcDir, lc) {
			st.BranchState = "gone"
		} else {
			st.BranchState = "diverged" // 无共同祖先
		}
		return st
	}
	if mb == lc {
		st.BranchState = "ok"
		if n, err := countCommits(srcDir, lc+"..HEAD"); err == nil {
			st.Behind = n
			st.Stale = threshold > 0 && n >= threshold
		}
		return st
	}
	st.BranchState = "diverged"
	st.MergeBase = mb
	return st
```

（legacy 分支里的 isAncestor 调用同样可用 mergeBase 等价替换——`mb==lc` 即祖先；保持行为等价即可。）

- [ ] **Step 5: cli.go——`diff` 分支 + status 的 merged_branches**

WikiCmd switch 加：

```go
	case "diff":
		s := wiki.LoadState(pc.Store.StateDir())
		base := ""
		if s != nil {
			base = s.BaseBranch
		}
		out, err := wiki.DiffSummary(cwd, base)
		if err != nil {
			fmt.Fprintln(stderr, "diff 计算失败:", err)
			return 1
		}
		if out == "" {
			fmt.Fprintln(stdout, "无法计算分叉点（非 git / 未设基准分支 / 无共同祖先）")
			return 0
		}
		fmt.Fprint(stdout, out)
		return 0
```

status 分支在 JSON 输出前补 merged 检测：

```go
		if st.Branch != "" && st.Branch == st.BaseBranch && st.BaseBranch != "" {
			if s := wiki.LoadState(pc.Store.StateDir()); s != nil {
				if db, err := index.Open(pc.Store.KbPath()); err == nil {
					merged := wiki.MergedIntoBase(s, cwd, func(b string) bool {
						ok, _ := db.HasBranchWiki(b)
						return ok
					})
					db.Close()
					if len(merged) > 0 {
						out["merged_branches"] = merged
					}
				}
			}
		}
```

WikiCmd 注释行改 `status|mark|base|diff`。

- [ ] **Step 6: 跑测试确认绿 + 全量回归**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/wiki/ ./internal/cli/ -count=1 && go test ./... -count=1`
Expected: 全 PASS（Task 2 的五态测试不改且绿 = 收敛语义不变证据）

- [ ] **Step 7: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/wiki/diff.go internal/wiki/diff_test.go internal/wiki/wiki.go internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(wiki): ok wiki diff 供差异素材 + 已并入检测（merged_branches）+ CheckStatus git 调用收敛"
```

---

### Task 4: 技能流程改造（openknowledge-wiki SKILL.md）

**Files:**
- Modify: `internal/setupx/skills/openknowledge-wiki/SKILL.md`
- Test: `internal/setupx/setupx_test.go`（若有技能内容断言则适配）

**Interfaces:**
- Consumes: Task 3 的 `ok wiki diff`、`merged_branches`
- Produces: SKILL.md 新第 0 步判读表与差异流程（文本即契约）

- [ ] **Step 1: 改写 SKILL.md（关键段落完整文本）**

第 0 步之后插入分支判读（"## 第 0 步：状态检查"段落改造）：

```markdown
## 第 0 步：状态检查

```bash
"{{EXE}}" wiki status
```

输出 JSON：`has_wiki`、`last_commit`、`behind`（-1 = 非 git 项目）、`stale`、`threshold`，以及分支字段 `branch`（当前分支）、`base_branch`（基准分支）、`branch_state`（ok/no_cursor/diverged/gone/legacy_orphan）、`merged_branches`（已并入基准的分支，可选）。

**先判分支，再判新旧：**

- `merged_branches` 非空 → 告诉用户这些分支已并入基准、差异条目已失效，**经用户确认后**删除对应条目（并可用 `{{EXE}} wiki mark` 刷新游标）。然后继续下面的分支判断。
- `branch` 与 `base_branch` 均非空且**不相等** → 走【分支差异流程】（不得重写全量条目——那会污染基准分支视角）
- 否则（在基准分支上）：`has_wiki:false` → 走【全量流程】；`behind > 0` → 走【增量流程】；`behind = 0` → 告诉用户 wiki 已是最新，结束

## 分支差异流程

为非基准分支生成/更新**差异条目**——只记本分支与基准的结构差异，不重写全量条目：

1. 取素材：`"{{EXE}}" wiki diff`（分叉点以来的目录/文件/Top 变更摘要），必要时再 `git log --oneline <分叉点>..HEAD` 补充
2. 把差异消化成条目：标题 `原条目名（<分支> 分支差异）`（无对应主条目时自定主题名），tags 为 `wiki,branch:<分支名>`，正文写"与基准分支的结构差异是什么/为什么"，300 字内
3. 同名已存在用 `add --force` 覆盖更新；不再适用的差异条目经用户确认后删除
4. `"{{EXE}}" wiki mark` 记本分支游标，汇报变更摘要
```

- [ ] **Step 2: 验证**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/setupx/ -count=1 && go build ./...`
Expected: PASS（若有技能文本断言失败，按新文本更新断言）

- [ ] **Step 3: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/setupx/skills/openknowledge-wiki/SKILL.md internal/setupx/setupx_test.go
git commit -m "feat(setupx): wiki 技能分支判读——非基准分支走差异流程 + 已并入清理引导"
```

---

### Task 5: GUI 管理页分支列 + 过滤器 + sticky 操作列

**Files:**
- Modify: `web/index.html`（分支 th、分支过滤 select、表格外包滚动容器）
- Modify: `web/app.js`（entryBranch/分支过滤/选项聚合/单元格徽标）
- Modify: `web/style.css`（滚动容器 + sticky 操作列 + 徽标样式 + nth-child 修正）
- Modify: `dist/web/`（三文件同步拷贝）

**Interfaces:**
- Consumes: `/api/entries` 既有 tags 字段（API 零改动）
- Produces: 无 API 变化；前端状态 `state.branchFilter`

- [ ] **Step 1: index.html**

工具条"类型"label 之后加：

```html
        <label>分支
          <select id="branch-filter">
            <option value="">全部</option>
          </select>
        </label>
```

表头 `th-time` 之后加 `<th>分支</th>`；`<table class="entries">` 外包一层 `<div class="entries-wrap">…</div>`。

- [ ] **Step 2: app.js**

```js
  // entryBranch 提取条目的分支标签（branch:<名>，第一个）；无则空串
  function entryBranch(e) {
    var tags = e.tags || [];
    for (var i = 0; i < tags.length; i++) {
      if (tags[i].indexOf("branch:") === 0) return tags[i].slice(7);
    }
    return "";
  }

  // renderBranchFilter 按当前项目条目聚合分支选项（项目切换时重聚合，联动）
  function renderBranchFilter() {
    var sel = $("branch-filter");
    if (!sel) return;
    var seen = {};
    (state.entries || []).forEach(function (e) {
      var b = entryBranch(e);
      if (b) seen[b] = true;
    });
    var cur = state.branchFilter || "";
    sel.innerHTML = '<option value="">全部</option>';
    Object.keys(seen).sort().forEach(function (b) {
      var o = document.createElement("option");
      o.value = b;
      o.textContent = b;
      sel.appendChild(o);
    });
    if (cur && seen[cur]) sel.value = cur; else { state.branchFilter = ""; sel.value = ""; }
  }
```

renderEntries 的类型过滤处追加分支过滤（语义：选中 dev → 共享 ∪ branch:dev）：

```js
    var list = state.entries.filter(function (e) {
      if (state.typeFilter === "draft" && !e.draft) return false;
      if (state.typeFilter && state.typeFilter !== "draft" && e.type !== state.typeFilter) return false;
      if (state.branchFilter) {
        var b = entryBranch(e);
        if (b !== "" && b !== state.branchFilter) return false;
      }
      return true;
    });
```

（注意：原过滤是单 return 链，按上面等价改写；draft 语义保持"仅草稿"。）行渲染的 fmtTime 单元格后加分支格：

```js
        '<td>' + (entryBranch(e) ? '<span class="badge badge-branch">⎇ ' + esc(entryBranch(e)) + "</span>" : "") + "</td>" +
```

条目加载完成与项目切换两处调 `renderBranchFilter()`（落点：`loadEntries` 的 then 里、project-select change handler 里——以实际函数名为准）；`$("branch-filter").addEventListener("change", …)` 里 `state.branchFilter = this.value; state.page = 1; renderEntries();`

- [ ] **Step 3: style.css**

```css
/* 分支列 + 可横滑表格：内容区可滑，操作列 sticky 固定 */
.entries-wrap { overflow-x: auto; }
.entries { min-width: 960px; }
.entries th.ops, .entries td.ops {
  position: sticky;
  right: 0;
  background: #fff;
  box-shadow: -4px 0 6px -4px rgba(0,0,0,.12);
}
.entries thead th.ops { background: #f0f1f4; z-index: 2; }
.badge-branch { background: #e0e7ff; color: #3730a3; border-radius: 4px; padding: 1px 6px; font-size: 12px; white-space: nowrap; }
```

表头"操作"th 与数据行 ops td 都需 `class="ops"`（index.html 的 th 加 class；app.js 行内 td 已有 `class="ops"`）。
**修正既有规则**：`.entries td:nth-child(5)` 原指 mandatory/摘要列——加分支列后索引平移，改为 `.entries td:nth-child(7)`（摘要列），先读现有行确认原意再改。

- [ ] **Step 4: 同步 dist + 手动验证**

`cp web/index.html web/app.js web/style.css dist/web/`。
`go build -o dist/ok.exe ./cmd/ok && ./dist/ok.exe gui`：管理页出现分支列与过滤器；造一条带 `branch:dev` 标签的条目（ok add --tags wiki,branch:dev）验证徽标/过滤/项目联动；窄窗口横滑验证操作列固定。

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add web/index.html web/app.js web/style.css
git commit -m "feat(gui): 管理页分支列（时间后）+ 分支过滤器（随项目联动）+ 操作列 sticky 固定"
```

（dist/ 在 .gitignore，同步为文件系统级。）

---

### Task 6: 文档、版本 2.7.0 与验收

**Files:**
- Modify: `docs/ARCHITECTURE.md`（wiki 段落补二期）
- Create: `docs/changelogs/2.7.0.md`
- Modify: `installer/openknowledge.iss`（2.6.0→2.7.0）、`cmd/ok/winres.json`（2.6.0.0→2.7.0.0）

- [ ] **Step 1: ARCHITECTURE.md**——wiki 段落在分支感知后续写：

```markdown
二期（分支差异条目）：长期并行分支只维护与基准的结构 delta（tags 含 branch:<名>）；
注入按当前分支过滤（含 INDEX 差异小节裁剪，分支未知不过滤）；`ok wiki diff` 给技能供结构
变化素材，非基准分支只写差异条目（写侧防呆）；基准分支检测 merged_branches 提示清理；
GUI 管理页分支列+过滤器+sticky 操作列；CheckStatus git 调用收敛为 merge-base 判别。
```

- [ ] **Step 2: changelog 2.7.0.md**（格式对齐 2.6.0.md）

```markdown
# 2.7.0

## 新功能
- wiki 分支差异条目：长期并行分支只维护与基准分支的结构差异（`ok wiki diff` 供素材 +
  openknowledge-wiki 技能差异流程），注入按当前分支过滤，他分支差异不再误导 agent
- 分支合并感知：dev 并入 master 后 status 报 merged_branches，注入提示清理失效差异条目
- GUI 管理页：分支列（时间后）+ 分支过滤器（随项目联动）+ 操作列固定（长分支名横滑可用）

## 修复
- 非基准分支跑 wiki 更新不再污染全量条目（技能流程改为只写差异条目）
- prompt 热路径 git 调用收敛（每次提问少 1-2 次 git 子进程）

## 说明
- 无 `branch:` 标签条目的项目行为与旧版完全一致
```

- [ ] **Step 3: 版本 bump + 全量验证**

iss 2.7.0、winres 2.7.0.0。
Run: `cd D:/develop/OpenKnowledge && go vet ./... && go test ./... -count=1 && python scripts/build.py --skip-installer`
Expected: vet 无输出；全绿；dist 构建成功且 dist/changelogs/2.7.0.md 存在

- [ ] **Step 4: 真机验收（controller 可做或留给用户）**

1. 本仓库建 `demo-delta` 分支造结构变化 → `ok wiki diff` 输出摘要
2. 模拟差异条目（ok add --tags wiki,branch:demo-delta）→ master 注入不含、该分支注入含
3. 合并回 master → status 报 merged_branches → 删条目后提示消失
4. GUI 管理页：分支列/过滤器/横滑 sticky 操作列
5. `python scripts/build.py` 打 OpenKnowledgeSetup-2.7.0.exe

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add docs/ARCHITECTURE.md docs/changelogs/2.7.0.md installer/openknowledge.iss cmd/ok/winres.json
git commit -m "docs: wiki 分支差异条目架构说明与 v2.7.0 changelog；版本 bump 2.7.0"
```

---

## Self-Review 记录

- **Spec 覆盖**：§3 数据模型→Task 1；§4 检索过滤+INDEX 裁剪→Task 1（原语）+Task 2（注入）；§5 wiki diff→Task 3；§6 技能流程→Task 4；§7 已并入→Task 3（检测+status）+Task 4（清理引导）——**nudge 提示变体**（spec §7"wikiNudge 增加变体"）未在 Task 3 落：Task 3 Step 5 只覆盖了 CLI status。→ 已在 Task 3 增补？没有。裁决：nudge 变体并入 Task 3 的 hook 侧会越界（Task 3 不含 hook 文件）；改派到 Task 2？Task 2 已结束。→ **增补 Task 3 Step 5b**（见下）。§8 GUI→Task 5；§9 性能收敛→Task 3 Step 4；§11 测试→各任务；§13 验收→Task 6。
- **增补**（Task 3 追加 Step 5b）：hook.go wikiNudge 加 merged 变体——仅在基准分支、`wiki.MergedIntoBase(...)` 非空时（hasDelta 经 `db.HasBranchWiki`），每会话一次提示：`[OpenKnowledge] 分支 <names> 已并入 <base>，其差异条目已失效，建议用 openknowledge-wiki 技能清理。`；注意 wikiNudge 当前签名 `(pc, st, s *wiki.Status)` 内无法直接开 db——core.go 的 InjectForPrompt 里 db 仍在作用域（defer Close 之前），把 merged 计算放 InjectForPrompt 尾部、`wikiNudge` 加参或前置拼接均可，实现时选最小 diff（建议：InjectForPrompt 在 `wikiNudge(...)` 调用前算 merged 并作为 `wikiNudgeMerged(pc, st, ws, merged)` 的入参，或直接在尾部追加一行提示文本并置 WikiNudged——保持每会话一次语义）。hook_test 补一例（基准分支+真合并+差异条目→提示出现；第二次不出现）。文件追加 internal/hook/hook.go、core.go、hook_test.go 到 Task 3 的 git add。
- **类型一致性**：`BranchOf/FilterHitsByBranch/TrimIndexBranchSections`（T1 定义，T2 消费）；`HasBranchWiki`（T1 定义，T3 cli 与增补 nudge 消费）；`DiffSummary/MergedIntoBase`（T3 定义，T4 技能文本引用命令形态）；`entryBranch`（T5 内部）——已对齐。
- **性能口径修正**：终审建议"先试 countCommits 隐含存在+可达"不成立（rev-list 对非祖先也成功），Task 3 Step 4 采用 merge-base 判别法（3 次 spawn），已写明。
- **已知留白**：Task 3 测试的 mkdirWrite/writeFile helper 名、TestDiffSummaryNoBase 的精确断言（给了推荐行为）；Task 5 的 loadEntries/project change 落点函数名以 app.js 现状为准。
