# hook 注入瘦身 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** hook 注入体积降一个量级：INDEX 主列表剔除 wiki 条目、默认预算 1500→800/top_n 3→2、检索命中注摘要+路径而非全文。

**Architecture:** 数据全部已在 SQLite entries 表（summary/filename 字段齐全），只改三处展示层：`index.rebuildIndex` 过滤、`config.Default` 数值、`hook.HandlePrompt` 检索命中格式化。mandatory 全文注入不变。

**Tech Stack:** Go 1.x，SQLite（mattn/go-sqlite3 经由 internal/index），无新依赖。

**Spec:** `docs/superpowers/specs/2026-07-28-injection-slim-design.md`

## Global Constraints

- Windows + Git Bash 环境；`go test ./...` 全绿才算完成
- fail-open 铁律不变：hook 任何失败退出码恒 0
- 用户配置里已显式写的 `max_tokens`/`top_n` 不受默认值变更影响（覆盖链不动）
- 提交信息沿用项目惯例：中文正文、类型前缀（feat/fix/docs/test）

---

### Task 1: index.Query 的 Hit 增加 Summary 字段

**Files:**
- Modify: `internal/index/query.go`（Hit 结构、FTS 与向量两路 SELECT/Scan）
- Test: `internal/index/query_summary_test.go`（新建）

**Interfaces:**
- Consumes: 现有 `db.Query(terms []string, queryVec []float32, cfg config.Retrieve) ([]Hit, error)`，entries 表有 `summary` 列
- Produces: `Hit.Summary string`（Task 4 的 HandlePrompt 注入摘要行依赖此字段）

- [ ] **Step 1: 写失败测试**

新建 `internal/index/query_summary_test.go`：

```go
package index

import (
	"path/filepath"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// 检索命中必须携带 summary（注入摘要行依赖），FTS 与向量通道都要有。
func TestQueryReturnsSummary(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", "---\ntitle: Git 提交规范\ntype: note\ntags: [git]\nsummary: 提交信息格式\ndraft: false\nmandatory: false\n---\n使用 Conventional Commits。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := db.Query(retrieve.Terms("git 提交"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Summary != "提交信息格式" {
		t.Fatalf("Summary = %q, want 提交信息格式", hits[0].Summary)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run TestQueryReturnsSummary -v`
Expected: FAIL（`hits[0].Summary` 为空串，或编译错误 unknown field Summary）

- [ ] **Step 3: 实现**

`internal/index/query.go`：

1. Hit 结构加字段（注释同步）：

```go
// Hit 是一条检索命中，携带注入所需的正文与摘要。
type Hit struct {
	Filename string
	Title    string
	Type     string
	Summary  string
	Body     string
	Score    float64
}
```

2. FTS 通道 SELECT 改（query.go 原 `SELECT e.filename, e.title, e.type, e.body,` 处）：

```go
		rows, err := db.sql.Query(
			`SELECT e.filename, e.title, e.type, e.summary, e.body,
				bm25(entries_fts, 10.0, 8.0, 3.0, 1.0) AS r
			FROM entries_fts JOIN entries e ON e.filename = entries_fts.filename
			WHERE entries_fts MATCH ? AND e.mandatory = 0 AND e.draft = 0`, match)
```

对应 Scan 改：`rows.Scan(&h.Filename, &h.Title, &h.Type, &h.Summary, &h.Body, &rank)`

3. 向量通道 SELECT 改：

```go
		rows, err := db.sql.Query(
			`SELECT e.filename, e.title, e.type, e.summary, e.body, v.blob
			FROM vectors v JOIN entries e ON e.filename = v.filename
			WHERE e.mandatory = 0 AND e.draft = 0`)
```

Scan 变量改：`var filename, title, typ, summary, body string`，`rows.Scan(&filename, &title, &typ, &summary, &body, &blob)`；构造处：

```go
				hits[filename] = &Hit{
					Filename: filename, Title: title, Type: typ, Summary: summary, Body: body,
					Score: cfg.Beta * cos,
				}
```

注意：FTS 通道先命中、向量通道后合并时，向量通道只 `h.Score +=` 不覆盖已有 Hit——FTS 命中已带 Summary，无需合并摘要。

4. Mandatory() 不动（注入全文，不需要 summary）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -count=1`
Expected: 全 PASS（含既有测试——既有断言不引用 Summary 字段，不受影响）

- [ ] **Step 5: 提交**

```bash
git add internal/index/query.go internal/index/query_summary_test.go
git commit -m "feat: carry entry summary in retrieval hits"
```

---

### Task 2: INDEX.md 主列表剔除 wiki 条目

**Files:**
- Modify: `internal/index/sync.go:226-258`（rebuildIndex）
- Test: `internal/index/wiki_test.go`（既有断言更新）

**Interfaces:**
- Consumes: entries 表 `tags`/`draft` 列；`WikiEntries()`（query.go:148，过滤口径 `draft = 0 AND tags LIKE '%wiki%'`）
- Produces: INDEX.md 新布局——主列表只含非 wiki 条目（draft 的 wiki 条目仍留主列表，带【草稿】前缀，因为它不在 Wiki 目录节）；Wiki 目录节不变

- [ ] **Step 1: 改测试（先红）**

`internal/index/wiki_test.go` 的 `TestRebuildIndexWikiSection` 末尾（`section := s[i:]` 断言块之后）追加：

```go
	// 主列表（Wiki 目录节之前的部分）不再出现已转正的 wiki 条目；草稿 wiki 仍留主列表
	main := s[:i]
	if strings.Contains(main, "A 架构") || strings.Contains(main, "B 模块") {
		t.Fatalf("wiki entries should not appear in main list:\n%s", main)
	}
	if !strings.Contains(main, "【草稿】草稿wiki") {
		t.Fatalf("draft wiki entry should stay in main list:\n%s", main)
	}
	if !strings.Contains(main, "普通条目") {
		t.Fatalf("plain entry should stay in main list:\n%s", main)
	}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run TestRebuildIndexWikiSection -v`
Expected: FAIL（wiki entries should not appear in main list）

- [ ] **Step 3: 实现**

`internal/index/sync.go` rebuildIndex 的主列表 SQL 与扫描循环改为按行过滤。最小改动：SQL 查询不变，循环内跳过：

```go
	for rows.Next() {
		var title, typ, tags, summary string
		var draft int
		if err := rows.Scan(&title, &typ, &tags, &summary, &draft); err != nil {
			return err
		}
		// 已转正的 wiki 条目只进 Wiki 目录节（带链接），主列表不重复
		if draft == 0 && strings.Contains(tags, "wiki") {
			continue
		}
		if draft != 0 {
			title = "【草稿】" + title
		}
		fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, typ, tags, summary)
	}
```

（`strings` 包 sync.go 已导入；`WikiEntries()` 用 `tags LIKE '%wiki%'`，此处 `strings.Contains(tags, "wiki")` 与之口径一致——tags 列是 `[wiki, 架构]` 形式的文本。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -count=1`
Expected: 全 PASS（`TestRebuildIndexNoWikiSection` 不受影响——无 wiki 条目时主列表照旧）

- [ ] **Step 5: 提交**

```bash
git add internal/index/sync.go internal/index/wiki_test.go
git commit -m "feat: drop wiki entries from INDEX main list (kept in wiki TOC)"
```

---

### Task 3: 默认值 800 / 2

**Files:**
- Modify: `internal/config/config.go:71-72`（Default）
- Test: `internal/config/config_test.go`、`internal/project/project_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `Default()` 返回 `Inject{MaxTokens: 800}`、`Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 2}`

- [ ] **Step 1: 改测试（先红）**

`internal/config/config_test.go:14`：

```go
	if cfg.Inject.MaxTokens != 800 || cfg.Retrieve.TopN != 2 || cfg.Embedding.TimeoutSec != 5 {
```

`internal/config/config_test.go:67`：

```go
	if cfg.Inject.MaxTokens != 800 {
```

`internal/config/config_test.go:74`：

```go
	if err != nil || cfg.Retrieve.TopN != 2 {
```

`internal/project/project_test.go:29`：

```go
	if ctx.Config.Inject.MaxTokens != 800 {
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ ./internal/project/ -count=1`
Expected: FAIL（断言 800/2 与实际 1500/3 不符）

- [ ] **Step 3: 实现**

`internal/config/config.go` Default()：

```go
		Inject:    Inject{MaxTokens: 800},
		Retrieve:  Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 2},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ ./internal/project/ -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/config/config.go internal/config/config_test.go internal/project/project_test.go
git commit -m "feat: lower default injection budget to 800 tokens and top_n to 2"
```

---

### Task 4: 检索命中注摘要行而非全文

**Files:**
- Modify: `internal/hook/hook.go:180-186`（HandlePrompt 命中注入段）
- Test: `internal/hook/hook_test.go`（TestFirstPromptInjectsBaseOnce、TestPromptStringFormCompat、TestPromptKeywordFallback 断言更新）

**Interfaces:**
- Consumes: Task 1 的 `Hit.Summary`、`Hit.Filename`、`Hit.Type`；`pc.Store.KnowledgeDir()`
- Produces: 注入格式（ hits 非空时）：

```
## 相关知识（需要全文时读取对应文件）
- **Git 提交规范** (note) — 提交信息格式（`<KnowledgeDir 绝对路径>/git.md`）
```

空摘要降级为 `- **标题** (type)（路径）`；路径用 `filepath.ToSlash`。

- [ ] **Step 1: 改测试（先红）**

`internal/hook/hook_test.go` 三处检索断言由"含正文"改为"含摘要行与路径、不含正文"：

TestFirstPromptInjectsBaseOnce（hook_test.go:85-87 与 :97-99 两处）：

```go
	if !strings.Contains(got, "提交信息格式") || !strings.Contains(got, "git.md") {
		t.Fatalf("first prompt missing retrieval summary line: %q", got)
	}
	if strings.Contains(got, "Conventional Commits") {
		t.Fatalf("retrieval should not inject full body: %q", got)
	}
```

（第二处同步改，错误文案 `retrieval lost on second prompt`。）

TestPromptStringCompat（:110-112）与 TestPromptKeywordFallback（:123-125）：

```go
	if !strings.Contains(out.String(), "提交信息格式") {
		t.Fatalf("string prompt form broken: %q", out.String())
	}
```

```go
	if !strings.Contains(out.String(), "提交信息格式") {
		t.Fatalf("expected git entry summary injected, got %q", out.String())
	}
```

注意 fixture `gitEntry` 的 summary 是"提交信息格式"、正文是"使用 Conventional Commits。"（hook_test.go:56-64），不用改 fixture。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hook/ -run 'TestFirstPromptInjectsBaseOnce|TestPromptStringFormCompat|TestPromptKeywordFallback' -v`
Expected: FAIL（输出已无全文断言成立/摘要未注入）

- [ ] **Step 3: 实现**

`internal/hook/hook.go` 替换 hits 注入循环（原 `for _, h := range hits { fmt.Fprintf(&b, "## %s\n\n%s\n\n", h.Title, h.Body) }`）：

```go
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
```

（`filepath` 在 hook.go 已导入——确认；未导入则加。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/hook/ -count=1`
Expected: 全 PASS（wiki nudge 测试不受影响——nudge 在预算外追加，逻辑未动）

- [ ] **Step 5: 全量回归 + 提交**

Run: `go test ./... -count=1`
Expected: 全部 PASS

```bash
git add internal/hook/hook.go internal/hook/hook_test.go
git commit -m "feat: inject retrieval hits as summary lines instead of full bodies"
```

---

## 收尾（全计划完成后）

- [ ] `go build ./... && go test ./... -count=1` 全绿
- [ ] 实测：在项目目录跑一次 hook prompt 模拟（`echo '{"hook_event_name":"UserPromptSubmit","session_id":"t","cwd":"D:/develop/OpenKnowledge","prompt":"窗口最大化"}' | dist/ok.exe hook prompt`），确认输出只剩摘要行量级
