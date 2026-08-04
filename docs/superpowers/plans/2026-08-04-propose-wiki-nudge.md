# 经验沉淀分流"新需求"到 wiki 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 经验沉淀时把"结构型"内容（新功能/新模块）分流到 wiki：propose 技能加分类指引，`ok search` 加 wiki 覆盖兜底提示行。

**Architecture:** 两组件——`internal/index` 新增 `HasWikiMatch`（FTS 关键词 + `draft=0` + `tags LIKE '%wiki%'` 的 EXISTS 查询）供 `cli.Search` 在输出末尾追加提示行；`internal/setupx/setupx.go` 内联技能模板 `skillTemplates["openknowledge-propose"]` 加"先分类"与查重扩展指引。

**Tech Stack:** Go（modernc.org/sqlite，FTS5），技能为 Go 内联字符串模板（`{{EXE}}` 占位）。

## Global Constraints

- 提示行文案逐字固定：`提示：该主题暂无 wiki 条目覆盖；若内容属于新功能/新模块，建议用 openknowledge-wiki 技能补充 wiki。`
- `HasWikiMatch` 只看 FTS 关键词、不看向量；`terms` 为空（含 `buildMatch` 结果为空串）返回 `true, nil`。
- wiki 判定与 INDEX Wiki 目录一致：`tags LIKE '%wiki%'` 且 `draft = 0`。
- `HasWikiMatch` 出错时 `Search` 静默不提示（fail-open），search 主输出格式不变。
- 测试离线：`t.Setenv("OK_HOME", t.TempDir())`、禁真实 embedding 网络调用（`OPENAI_API_KEY=""`）。
- spec：`docs/superpowers/specs/2026-08-04-propose-wiki-nudge-design.md`（组件 1 位置已更正为 setupx.go 内联模板）。

---

### Task 1: `index.HasWikiMatch`

**Files:**
- Test: `internal/index/wiki_match_test.go`（新建）
- Modify: `internal/index/query.go`（文件末尾追加方法；`buildMatch` 已存在于该文件）

**Interfaces:**
- Consumes: `index.Open(path)`、`db.Sync(kdir, nil)`、测试助手 `writeEntryFile(t, dir, name, content)`（`internal/index/index_test.go:45`，同包可用）、`retrieve.Terms(string) []string`。
- Produces: `func (db *DB) HasWikiMatch(terms []string) (bool, error)`——Task 2 的 `cli.Search` 依赖此签名。

- [ ] **Step 1: 写失败测试**

创建 `internal/index/wiki_match_test.go`：

```go
package index

import (
	"path/filepath"
	"testing"

	"openknowledge/internal/retrieve"
)

// HasWikiMatch 只在"非草稿且 tags 含 wiki 的条目"命中检索词时返回 true。
func TestHasWikiMatch(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "arch.md", "---\ntitle: 架构总览\ntype: reference\ntags: [wiki, 架构]\nsummary: 架构\ndraft: false\nmandatory: false\n---\n守护进程与托盘架构。\n")
	writeEntryFile(t, kdir, "git.md", "---\ntitle: Git 规范\ntype: note\ntags: [git]\nsummary: 规范\ndraft: false\nmandatory: false\n---\n使用 Conventional Commits。\n")
	writeEntryFile(t, kdir, "draft-wiki.md", "---\ntitle: 草稿维基\ntype: reference\ntags: [wiki]\nsummary: 草稿\ndraft: true\nmandatory: false\n---\n甲子园草稿内容。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}

	// wiki 条目覆盖
	if ok, err := db.HasWikiMatch(retrieve.Terms("守护进程")); err != nil || !ok {
		t.Fatalf("守护进程 should be wiki-covered: ok=%v err=%v", ok, err)
	}
	// 仅非 wiki 条目命中 → false
	if ok, err := db.HasWikiMatch(retrieve.Terms("Commits")); err != nil || ok {
		t.Fatalf("Commits should not be wiki-covered: ok=%v err=%v", ok, err)
	}
	// draft 的 wiki 条目不计入
	if ok, err := db.HasWikiMatch(retrieve.Terms("甲子园")); err != nil || ok {
		t.Fatalf("甲子园 draft wiki should not count: ok=%v err=%v", ok, err)
	}
	// 空 terms → true（不提示）
	if ok, err := db.HasWikiMatch(nil); err != nil || !ok {
		t.Fatalf("nil terms should be true: ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/index/ -run TestHasWikiMatch -v`
Expected: 编译失败 `undefined: db.HasWikiMatch`（方法尚未实现）

- [ ] **Step 3: 实现**

在 `internal/index/query.go` 末尾（`Query` 方法之后）追加：

```go
// HasWikiMatch 报告检索词是否有 wiki 条目（draft=0 且 tags 含 wiki）覆盖。
// 仅看 FTS 关键词、不看向量——兜底启发式，供 ok search 输出提示；terms 为空返回 true。
func (db *DB) HasWikiMatch(terms []string) (bool, error) {
	match := buildMatch(terms)
	if match == "" {
		return true, nil
	}
	var exists bool
	err := db.sql.QueryRow(
		`SELECT EXISTS(
			SELECT 1 FROM entries_fts JOIN entries e ON e.filename = entries_fts.filename
			WHERE entries_fts MATCH ? AND e.draft = 0 AND e.tags LIKE '%wiki%'
		)`, match).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/index/ -run TestHasWikiMatch -v && go test ./internal/index/`
Expected: 新测试 PASS；包内既有测试全部 ok

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge && git add internal/index/query.go internal/index/wiki_match_test.go && git commit -m "feat(index): HasWikiMatch——检索词的 wiki 覆盖探测（FTS+draft=0+tags 含 wiki）"
```

---

### Task 2: `cli.Search` wiki 覆盖提示行

**Files:**
- Modify: `internal/cli/cli.go:243-251`（`Search` 函数尾部）
- Test: `internal/cli/search_wiki_hint_test.go`（新建）

**Interfaces:**
- Consumes: Task 1 的 `func (db *DB) HasWikiMatch(terms []string) (bool, error)`；同包测试助手 `setupProject(t) (home, kb string)`（`internal/cli/propose_test.go:17`，返回的 `kb` 是 `<OK_HOME>/projects/demo` 目录，条目写入 `kb/knowledge/` 后用 `Index(nil, &out, &errBuf)` 同步）。
- Produces: `Search` 输出末尾提示行（文案见 Global Constraints，逐字固定）。

- [ ] **Step 1: 写失败测试**

创建 `internal/cli/search_wiki_hint_test.go`：

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wikiHint = "暂无 wiki 条目覆盖"

// 无 wiki 覆盖时 search 输出兜底提示行；有 wiki 覆盖时不输出。
func TestSearchWikiHint(t *testing.T) {
	_, kb := setupProject(t)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(kb, "knowledge", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("git.md", "---\ntitle: Git 规范\ntype: note\ntags: [git]\nsummary: 规范\ndraft: false\nmandatory: false\n---\n使用 Conventional Commits。\n")
	write("arch.md", "---\ntitle: 架构总览\ntype: reference\ntags: [wiki, 架构]\nsummary: 架构\ndraft: false\nmandatory: false\n---\n守护进程架构。\n")
	var out, errBuf bytes.Buffer
	if code := Index(nil, &out, &errBuf); code != 0 {
		t.Fatalf("index code=%d err=%q", code, errBuf.String())
	}

	// wiki 覆盖的主题 → 无提示行
	out.Reset()
	if code := Search([]string{"守护进程"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if strings.Contains(out.String(), wikiHint) {
		t.Fatalf("covered topic should not hint: %q", out.String())
	}

	// 仅非 wiki 条目命中 → 有提示行
	out.Reset()
	if code := Search([]string{"Commits"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), wikiHint) {
		t.Fatalf("non-wiki topic should hint: %q", out.String())
	}

	// 全新主题（零命中）→ 有提示行
	out.Reset()
	if code := Search([]string{"甲子园"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), wikiHint) {
		t.Fatalf("brand-new topic should hint: %q", out.String())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ -run TestSearchWikiHint -v`
Expected: FAIL——"仅非 wiki 条目命中"与"全新主题"两处断言不含提示行

- [ ] **Step 3: 实现**

`internal/cli/cli.go` 的 `Search`：把 `db.Query(retrieve.Terms(query), ...)` 的 terms 提取为变量复用，并在输出 hits 后追加提示逻辑。改动后为：

```go
	terms := retrieve.Terms(query)
	hits, err := db.Query(terms, queryVec, pc.Config.Retrieve)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, h := range hits {
		fmt.Fprintf(stdout, "%.2f\t%s (%s)\n", h.Score, h.Title, h.Filename)
	}
	// wiki 覆盖兜底提示：无 wiki 条目命中该主题时，提示可经 openknowledge-wiki 补充。
	// fail-open：检查失败不提示，search 主输出格式不变。
	if covered, err := db.HasWikiMatch(terms); err == nil && !covered {
		fmt.Fprintln(stdout, "提示：该主题暂无 wiki 条目覆盖；若内容属于新功能/新模块，建议用 openknowledge-wiki 技能补充 wiki。")
	}
	return 0
}
```

（即：原 `hits, err := db.Query(retrieve.Terms(query), queryVec, pc.Config.Retrieve)` 一行拆成 `terms := ...` + `db.Query(terms, ...)`，其余不变。）

- [ ] **Step 4: 运行测试确认通过**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/cli/ -run TestSearchWikiHint -v && go test ./internal/cli/`
Expected: 新测试 PASS；包内既有测试全部 ok

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge && git add internal/cli/cli.go internal/cli/search_wiki_hint_test.go && git commit -m "feat(cli): ok search 无 wiki 覆盖时输出兜底提示行，引导 openknowledge-wiki 补充"
```

---

### Task 3: propose 技能"先分类"指引

**Files:**
- Modify: `internal/setupx/setupx.go:132`（`skillTemplates["openknowledge-propose"]` 内联字符串）
- Test: `internal/setupx/setupx_test.go`（追加一个测试函数）

**Interfaces:**
- Consumes: 无（纯文案）。
- Produces: 更新后的 propose 技能模板；分发仍走既有 `InstallSkills`（`{{EXE}}` 替换），无签名变化。

- [ ] **Step 1: 写失败测试**

在 `internal/setupx/setupx_test.go` 末尾追加：

```go
// propose 技能模板必须包含"先分类"指引与 wiki 覆盖提示的联动说明。
func TestProposeSkillTemplateHasClassification(t *testing.T) {
	tpl := skillTemplates["openknowledge-propose"]
	for _, want := range []string{"先分类", "结构型", "openknowledge-wiki", "暂无 wiki 条目覆盖"} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("propose skill template missing %q", want)
		}
	}
}
```

（`strings` 已在该测试文件的 import 中；若没有则补上。）

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/setupx/ -run TestProposeSkillTemplateHasClassification -v`
Expected: FAIL——`propose skill template missing "先分类"`

- [ ] **Step 3: 实现**

把 `internal/setupx/setupx.go` 中 `skillTemplates["openknowledge-propose"]` 的字符串字面量整体替换为：

```go
	"openknowledge-propose": "---\nname: openknowledge-propose\ndescription: 把本次会话中沉淀的经验作为草稿条目提议进 OpenKnowledge 知识库（ok propose，待人批准）。当解决了一个非显而易见的问题、踩到坑、或发现项目隐藏约定时使用。\n---\n\n# openknowledge-propose\n\n## 先分类：经验型还是结构型\n\n- **经验型**（踩坑、隐藏约定、非显而易见问题的解法）→ 走本技能 ok propose 记草稿。\n- **结构型**（新功能、新模块、新子系统、重要架构/流程变化）→ 不是草稿，改用 openknowledge-wiki 技能新增/更新 wiki 条目（同名 add --force 重写）。\n- **两者兼有** → 都记：wiki 条目记\"是什么/怎么协作\"，草稿记\"坑\"。\n\n## 何时提议\n\n- 解决了一个非显而易见的问题（排查过程值得复用）\n- 踩到坑（环境、依赖、工具链的隐性陷阱）\n- 发现了项目的隐藏约定（代码里没有写明但必须遵守的规则）\n\n## 何时不要提议\n\n- 日常例行操作（常规增删改、跑测试、格式化）\n- 知识库已有的内容——先用 Bash 执行 `\"{{EXE}}\" search <关键词>` 查重，确认没有同主题条目再提议；若输出末尾出现\"暂无 wiki 条目覆盖\"提示行且内容属于结构型，告诉用户这是知识空白、建议用 openknowledge-wiki 技能补 wiki\n\n## 命令\n\n先把正文写入一个临时 Markdown 文件，再用 Bash 工具执行：\n\n    \"{{EXE}}\" propose --title <标题> --type pitfall --tags <逗号分隔> --file <正文.md>\n\n正文很短时可直接用 `--body`（与 `--file` 二选一）：\n\n    \"{{EXE}}\" propose --title <标题> --type note --body <正文>\n\ntype 取值：rule（规则）| pitfall（踩坑）| note（笔记）| reference（参考资料）。\n\n提议成功后告诉用户：\"已记为草稿，待批准\"（草稿不参与检索注入，用户在 GUI 点\"采纳\"或执行 `ok approve` 后转正）。\n",
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/setupx/ -v -run TestProposeSkillTemplateHasClassification && go build ./... && go test ./...`
Expected: 新测试 PASS；全仓库构建与测试绿

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge && git add internal/setupx/setupx.go internal/setupx/setupx_test.go docs/superpowers/specs/2026-08-04-propose-wiki-nudge-design.md && git commit -m "feat(setupx): propose 技能加先分类指引——结构型内容引导 openknowledge-wiki 补 wiki"
```

（spec 文档的组件 1 位置更正随本任务一并提交。）

---

## 备注（不在任务内）

- 生效路径：CLI 提示随 ok.exe 升级；技能文案需重跑 `ok setup` 或 GUI"安装技能"重新分发到 `~/.agents/skills/openknowledge-propose/`。
- 版本/changelog 归发版流程处理，本计划不含。
