# 知识索引膨胀治理（方案 A）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 INDEX.md 渲染具备预算感知：摘要去重、价值排序、溢出折叠、归档过滤，替代目前按 filename 排序 + 外层 token 静默截断的行为。

**Architecture:** 改动集中在 `internal/index/sync.go` 的 `rebuildIndex` 渲染层（去重/排序/折叠/归档过滤），排序信号复用现有 `FeedbackStats`（entry_events 表，30 天窗口）；`Sync` 通过新增变参 `SyncOptions` 接收 `max_lines`（约 50 处既有调用点零改动）；hook 与 cli 的 Sync 调用点透传 `pc.Config.Index.MaxLines`。新增 `entries.archived` 列（仿 `migrateDraftColumn` 迁移模式）与 `ok archive` 子命令。hook 注入层（`internal/hook/core.go`）逻辑零改动，只改 Sync 传参。

**Tech Stack:** Go 1.x、modernc.org/sqlite（FTS5）、gopkg.in/yaml.v3、标准库 flag。

**Spec:** `docs/superpowers/specs/2026-08-16-index-bloat-design.md`

## Global Constraints

- 提交信息用中文、Conventional Commits 前缀（`feat:` / `fix:` / `test:`），与仓库历史一致。
- 注释一律中文，风格与所在文件现有注释一致（行尾/上方短注释，说明"为什么"）。
- `Sync` 签名改为 `Sync(dir string, client embed.Client, opts ...SyncOptions)` 变参形式——**不许**改既有调用点（`internal/backup/backup.go`、`internal/gui/api.go`、各测试文件等保持原样编译通过）。
- 排序时间信号用 `entries.mtime`（库里现成的更新时间）；spec 中"updated 倒序"以此落地。
- `FeedbackStats` 查询失败静默降级为 mtime 排序——与 `Sync` 中 `PruneEvents` 的 `_ =` 风格一致（index 包无 logger），这是对 spec"记日志"一句的有意简化。
- `max_lines` 缺省/<=0 一律按 50，归一化只在 `rebuildIndex` 内做，不改 `config.Load`。
- hook 注入层（`internal/hook/core.go`）除 Task 6 的 Sync 传参外零改动；`Inject.MaxTokens` 外层截断与 mandatory 保护不动。
- 归档候选报告的时间口径：frontmatter `created`（YYYY-MM-DD）超 90 天；解析失败的条目不入选。
- 不做：分级懒加载索引、纯检索化、默认自动归档。
- 工作区已有用户的未提交改动（`internal/index/db.go`、`internal/state/*`、`internal/fsx/lock.go` 等 WAL 相关），**不许**提交、还原或覆盖这些文件里与本计划无关的内容；本计划涉及的 `db.go` 改动与之共存（不同函数），提交时只 `git add` 本计划触及的 hunk/文件并自行确认不夹带。

---

### Task 1: 配置 `[index] max_lines`

**Files:**
- Modify: `internal/config/config.go`（Inject 结构体附近、`Config` 结构体、`Default()`）
- Test: `internal/config/config_test.go:14`

**Interfaces:**
- Produces: `config.Index` 结构体（字段 `MaxLines int`，TOML 键 `index.max_lines`）；`Config.Index` 字段；默认 50。后续 Task 6 消费 `pc.Config.Index.MaxLines`。

- [ ] **Step 1: 写失败测试**

`internal/config/config_test.go` 中 `TestLoadNone`（第 14 行附近）的默认值断言改为：

```go
	if cfg.Inject.MaxTokens != 800 || cfg.Retrieve.TopN != 2 || cfg.Embedding.TimeoutSec != 5 {
		t.Fatalf("unexpected defaults %+v", cfg)
	}
	if cfg.Index.MaxLines != 50 {
		t.Fatalf("default index.max_lines = %v, want 50", cfg.Index.MaxLines)
	}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestLoadNone -v`
Expected: FAIL —— `cfg.Index.MaxLines = 0`（字段尚不存在则编译失败，同样算红）。

- [ ] **Step 3: 实现**

`internal/config/config.go` 在 `type Inject struct` 定义之后新增：

```go
// Index 控制 INDEX.md 渲染预算。
type Index struct {
	MaxLines int `toml:"max_lines"` // 主列表最大行数，默认 50；<=0 按 50（渲染处归一）
}
```

`Config` 结构体（第 206 行附近）加字段：

```go
	Index      Index         `toml:"index"`
```

`Default()`（第 218 行附近）返回值中加：

```go
		Index:      Index{MaxLines: 50},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS（全部，含既有合并覆盖测试——新字段零值不参与旧断言）。

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): 新增 [index] max_lines 渲染预算配置（默认 50）"
```

---

### Task 2: Entry 新增 `archived` / `created` frontmatter 字段

**Files:**
- Modify: `internal/entry/entry.go:14-23`
- Test: `internal/entry/entry_test.go`（若无此文件则新建）

**Interfaces:**
- Produces: `Entry.Archived bool`（`yaml:"archived,omitempty"`）、`Entry.Created string`（`yaml:"created,omitempty"`）。Task 3 消费 `Archived`，Task 8 消费 `Created` 与既有 `Entry.FileName()`。
- `omitempty` 保证旧条目被 `Serialize()` 重写时不多出字段。

- [ ] **Step 1: 写失败测试**

`internal/entry/entry_test.go`（已存在则追加）：

```go
func TestParseArchivedAndCreated(t *testing.T) {
	e, err := Parse([]byte("---\ntitle: 旧坑\ntype: pitfall\narchived: true\ncreated: 2026-01-02\nsummary: s\n---\n\n正文\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !e.Archived || e.Created != "2026-01-02" {
		t.Fatalf("archived=%v created=%q", e.Archived, e.Created)
	}
	// 零值序列化回写不多出 archived/created 键
	e2, err := Parse([]byte("---\ntitle: 新坑\ntype: note\nsummary: s\n---\n\n正文\n"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(e2.Serialize())
	if strings.Contains(out, "archived") || strings.Contains(out, "created") {
		t.Fatalf("omitempty 失效: %q", out)
	}
}
```

（文件无 `strings` 导入则补上。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/entry/ -run TestParseArchivedAndCreated -v`
Expected: FAIL（字段不存在，编译错误）。

- [ ] **Step 3: 实现**

`internal/entry/entry.go` 的 `Entry` 结构体改为：

```go
type Entry struct {
	Title     string   `yaml:"title"`
	Type      string   `yaml:"type"`
	Tags      []string `yaml:"tags"`
	Mandatory bool     `yaml:"mandatory"`
	Draft     bool     `yaml:"draft"` // 草稿不参与检索注入，INDEX.md 标【草稿】
	// Archived 归档条目：不进 INDEX 主列表，仍保留在库中可检索
	Archived bool `yaml:"archived,omitempty"`
	// Created 创建日期（YYYY-MM-DD），供归档候选报告；历史条目缺省为空
	Created string   `yaml:"created,omitempty"`
	Summary string   `yaml:"summary"`
	Body    string   `yaml:"-"`
	Path    string   `yaml:"-"`
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/entry/ -v`
Expected: PASS（含既有 Parse/Serialize 测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/entry/entry.go internal/entry/entry_test.go
git commit -m "feat(entry): frontmatter 新增 archived/created 字段（omitempty）"
```

---

### Task 3: entries 表新增 archived 列并随同步入库

**Files:**
- Modify: `internal/index/db.go`（`Open` 迁移链 + 新增迁移函数，仿 `migrateDraftColumn`）
- Modify: `internal/index/sync.go`（upsert SQL，约第 178-195 行）
- Test: `internal/index/render_test.go`（新建，后续 Task 4/5 的测试也放这里）

**Interfaces:**
- Consumes: `Entry.Archived`（Task 2）。
- Produces: `entries.archived INTEGER NOT NULL DEFAULT 0` 列；Task 5 的 `rebuildIndex` 查询依赖此列。

- [ ] **Step 1: 写失败测试**

新建 `internal/index/render_test.go`：

```go
package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syncAndReadIndex 同步并返回 INDEX.md 内容。
func syncAndReadIndex(t *testing.T, db *DB, kdir string, opts ...SyncOptions) string {
	t.Helper()
	if err := db.Sync(kdir, fakeEmbedder{}, opts...); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(kdir), "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestArchivedColumnStored(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "old.md", `---
title: 老条目
type: note
archived: true
summary: s
---

正文。
`)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	var archived int
	if err := db.sql.QueryRow(`SELECT archived FROM entries WHERE filename='old.md'`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatalf("archived=%d, want 1", archived)
	}
	_ = strings.TrimSpace("") // 占位防未用导入，后续任务测试会用 strings
}
```

注意：`fakeEmbedder`、`writeEntryFile` 已在 `internal/index/index_test.go` 定义，同包直接可用。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run TestArchivedColumnStored -v`
Expected: FAIL（`SyncOptions` 未定义 + `archived` 列不存在）。

- [ ] **Step 3: 实现**

`internal/index/db.go`：`Open` 中 `migrateDraftColumn()` 调用之后加：

```go
	if err := db.migrateArchivedColumn(); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
```

并新增（与 `migrateDraftColumn` 同构）：

```go
// migrateArchivedColumn 为旧库补 entries.archived 列（归档标记，见 migrateDraftColumn）。
func (db *DB) migrateArchivedColumn() error {
	rows, err := db.sql.Query(`PRAGMA table_info(entries)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.RawBytes
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "archived" {
			has = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if has {
		return nil
	}
	_, err = db.sql.Exec(`ALTER TABLE entries ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`)
	return err
}
```

同时把 `schema` 常量里 entries 建表语句改为含新列（新库直接建好）：

```go
CREATE TABLE IF NOT EXISTS entries(
  filename TEXT PRIMARY KEY,
  title TEXT NOT NULL, type TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  mandatory INTEGER NOT NULL DEFAULT 0,
  draft INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0,
  mtime INTEGER NOT NULL DEFAULT 0
);
```

`internal/index/sync.go` upsert 段（约 178-195 行）改为：

```go
		tags := strings.Join(e.Tags, ", ")
		mandatory := 0
		if e.Mandatory {
			mandatory = 1
		}
		draft := 0
		if e.Draft {
			draft = 1
		}
		archived := 0
		if e.Archived {
			archived = 1
		}
		if _, err := tx.Exec(`INSERT INTO entries(filename,title,type,tags,summary,body,mandatory,draft,archived,mtime)
			VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(filename) DO UPDATE SET
			title=excluded.title, type=excluded.type, tags=excluded.tags,
			summary=excluded.summary, body=excluded.body,
			mandatory=excluded.mandatory, draft=excluded.draft,
			archived=excluded.archived, mtime=excluded.mtime`,
			name, e.Title, e.Type, tags, e.Summary, e.Body, mandatory, draft, archived, mtime); err != nil {
			return rollback(err)
		}
```

`sync.go` 顶部新增 `SyncOptions` 类型并把 `Sync` 签名改变参（函数体其余不变，仅开头取出选项暂存，Task 5 才消费）：

```go
// SyncOptions 控制 Sync 重建 INDEX.md 的渲染预算；零值走默认（MaxLines=50）。
type SyncOptions struct {
	MaxLines int // 主列表最大行数，<=0 按 50
}

func (db *DB) Sync(dir string, client embed.Client, opts ...SyncOptions) error {
	o := SyncOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	_ = o // Task 5 起用于 rebuildIndex
```

（`_ = o` 是临时的，Task 5 会把它接到 `rebuildIndex(dir, o.MaxLines)`；本任务保持编译通过即可。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -v`
Expected: PASS（含既有全部测试——既有调用点不变参仍编译）。

- [ ] **Step 5: Commit**

```bash
git add internal/index/db.go internal/index/sync.go internal/index/render_test.go
git commit -m "feat(index): entries 新增 archived 列并随同步入库，Sync 支持 SyncOptions 变参"
```

---

### Task 4: 摘要去重 `dedupSummary`

**Files:**
- Modify: `internal/index/sync.go`（新增函数 + `rebuildIndex` 行渲染处）
- Test: `internal/index/render_test.go`

**Interfaces:**
- Produces: `func dedupSummary(title, summary string) string`——冗余时返回 `""`，否则原样返回 summary。Task 5 的渲染循环消费。

- [ ] **Step 1: 写失败测试**

`internal/index/render_test.go` 追加：

```go
func TestDedupSummary(t *testing.T) {
	cases := []struct{ title, summary, want string }{
		{"Git 提交规范", "Git 提交规范", ""},                       // 完全复读
		{"Git 提交规范", "Git 提交规范。", ""},                      // 末尾标点归一后复读
		{"Bun.spawn 无内建 timeout", "Bun.spawn 无内建 timeout：opencode 插件须手动 kill 防挂死", ""}, // 标题为摘要前缀
		{"索引膨胀治理方案", "索引膨胀治理方案分三级", ""},           // 共有前缀 8/10 ≥80%
		{"Git 提交规范", "提交信息格式", "提交信息格式"},              // 摘要补充新信息，保留
		{"短", "短甲长得多得多的补充说明", "短甲长得多得多的补充说明"}, // 共有前缀<80%，保留
		{"任意标题", "", ""},                                        // 空摘要原样
	}
	for _, c := range cases {
		if got := dedupSummary(c.title, c.summary); got != c.want {
			t.Errorf("dedupSummary(%q,%q)=%q, want %q", c.title, c.summary, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run TestDedupSummary -v`
Expected: FAIL（`dedupSummary` 未定义）。

- [ ] **Step 3: 实现**

`internal/index/sync.go` 新增：

```go
// dedupSummary 摘要与标题冗余（规范化后相同/标题是摘要前缀且覆盖≥40%/共有前缀≥摘要 80%）
// 时返回空串——渲染层兜底，存量"摘要复读标题"的条目无需回填。
// 40% 主干覆盖判据（2026-08-16 用户裁决）：标题很短时裸前缀会误省含新信息的摘要。
func dedupSummary(title, summary string) string {
	norm := func(s string) string {
		return strings.TrimRight(strings.TrimSpace(s), "。．.：:，,；;、 ")
	}
	t, s := norm(title), norm(summary)
	if s == "" || t == "" {
		return summary
	}
	if s == t {
		return ""
	}
	tr, sr := []rune(t), []rune(s)
	if strings.HasPrefix(s, t) && float64(len(tr)) >= 0.4*float64(len(sr)) {
		return ""
	}
	n := 0
	for n < len(tr) && n < len(sr) && tr[n] == sr[n] {
		n++
	}
	if float64(n) >= 0.8*float64(len(sr)) {
		return ""
	}
	return summary
}
```

`rebuildIndex` 渲染行处（原第 301 行 `fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", ...)`）改为：

```go
		if sum := dedupSummary(title, summary); sum != "" {
			fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, typ, tags, sum)
		} else {
			fmt.Fprintf(&b, "- **%s** (%s) [%s]\n", title, typ, tags)
		}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/index/sync.go internal/index/render_test.go
git commit -m "feat(index): 渲染层摘要去重（复读/前缀/共有前缀≥80% 省略摘要）"
```

---

### Task 5: rebuildIndex 价值排序 + 溢出折叠 + 归档过滤

**Files:**
- Modify: `internal/index/sync.go`（`rebuildIndex` 整体重写 + 新增 `indexRow` 类型、`writeFoldedLine`；`Sync` 末尾接通 `o.MaxLines`）
- Test: `internal/index/render_test.go`

**Interfaces:**
- Consumes: `SyncOptions.MaxLines`（Task 3）、`entries.archived`（Task 3）、`dedupSummary`（Task 4）、既有 `FeedbackStats(30)` / `splitTags` / `BranchOf` / `WikiEntries`。
- Produces: 新 `rebuildIndex(dir string, maxLines int)`。折叠行格式：`- 另有 N 条未列出（tags 分布：tag×n, …），可用关键词/向量检索命中`（无 tag 省略括号段）。

- [ ] **Step 1: 写失败测试**

`internal/index/render_test.go` 追加：

```go
func TestIndexValueOrderAndFold(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	mk := func(title, tags string) string {
		return "---\ntitle: " + title + "\ntype: note\ntags: [" + tags + "]\nsummary: " + title + "的补充\n---\n\n正文。\n"
	}
	writeEntryFile(t, kdir, "a.md", mk("甲条目", "agentx"))
	writeEntryFile(t, kdir, "b.md", mk("乙条目", "hooks"))
	writeEntryFile(t, kdir, "c.md", mk("丙条目", "agentx"))
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// 无事件、max_lines=2：mtime 同秒退化为 filename 序，丙条目折叠
	out := syncAndReadIndex(t, db, kdir, SyncOptions{MaxLines: 2})
	if !strings.Contains(out, "甲条目") || !strings.Contains(out, "乙条目") {
		t.Fatalf("前两条应列出: %q", out)
	}
	if strings.Contains(out, "- **丙条目**") {
		t.Fatalf("丙条目应被折叠: %q", out)
	}
	if !strings.Contains(out, "- 另有 1 条未列出（tags 分布：agentx×1），可用关键词/向量检索命中") {
		t.Fatalf("缺折叠行: %q", out)
	}
	// 注入事件提升权重：丙条目 2 次注入+1 次采纳 → 排第一
	if err := db.RecordEvents(EventInjected, []string{"c.md", "c.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventAdopted, []string{"c.md"}); err != nil {
		t.Fatal(err)
	}
	// 触发重建：改一个文件 mtime
	writeEntryFile(t, kdir, "a.md", mk("甲条目", "agentx")+"\n")
	out = syncAndReadIndex(t, db, kdir, SyncOptions{MaxLines: 2})
	if !strings.Contains(out, "- **丙条目**") {
		t.Fatalf("丙条目应凭事件权重进入前两行: %q", out)
	}
}

func TestIndexArchivedAndDraftPlacement(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "arch.md", `---
title: 归档条目
type: note
archived: true
summary: s
---

正文。
`)
	writeEntryFile(t, kdir, "draft.md", `---
title: 草稿条目
type: note
draft: true
summary: s
---

正文。
`)
	writeEntryFile(t, kdir, "live.md", `---
title: 正式条目
type: note
summary: s
---

正文。
`)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	out := syncAndReadIndex(t, db, kdir)
	if strings.Contains(out, "归档条目") {
		t.Fatalf("归档条目不应进 INDEX: %q", out)
	}
	iLive := strings.Index(out, "正式条目")
	iDraft := strings.Index(out, "【草稿】草稿条目")
	if iLive < 0 || iDraft < 0 || iDraft < iLive {
		t.Fatalf("草稿应沉底且带前缀: %q", out)
	}
	// 归档条目仍可检索
	n, _ := db.Count()
	if n != 3 {
		t.Fatalf("归档条目应保留在库: count=%d", n)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run 'TestIndexValueOrderAndFold|TestIndexArchivedAndDraftPlacement' -v`
Expected: FAIL（排序/折叠/归档过滤尚未实现——当前按 filename 全量列出）。

- [ ] **Step 3: 实现**

`internal/index/sync.go`：删除 `Sync` 里的 `_ = o`，把末尾 `db.rebuildIndex(dir)` 改为 `db.rebuildIndex(dir, o.MaxLines)`。

`rebuildIndex` 整体替换为（wiki 目录/分支差异节逻辑原样保留，仅主列表部分重写）：

```go
// indexRow 是 rebuildIndex 主列表渲染用的条目视图。
type indexRow struct {
	filename, title, typ, tags, summary string
	draft, weight                       int
	mtime                               int64
}

// rebuildIndex 从 entries 表重写 <dir>/../INDEX.md。主列表按价值排序
// （30 天窗口 采纳×2+注入×1 降序，平局按 mtime 降序再按 filename 升序；
// 草稿沉底），超过 maxLines 的尾部折叠为一行可检索提示；archived 条目
// 不进主列表（仍保留在库可检索）。wiki 目录节/分支差异节维持原有输出。
func (db *DB) rebuildIndex(dir string, maxLines int) error {
	if maxLines <= 0 {
		maxLines = 50
	}
	rows, err := db.sql.Query(`SELECT filename, title, type, tags, summary, draft, archived, mtime FROM entries`)
	if err != nil {
		return err
	}
	// FeedbackStats 失败静默降级（与 PruneEvents 一致）：权重全零退回 mtime/filename 序
	stats, _ := db.FeedbackStats(30)
	var main, drafts []indexRow
	for rows.Next() {
		var r indexRow
		var archived int
		if err := rows.Scan(&r.filename, &r.title, &r.typ, &r.tags, &r.summary, &r.draft, &archived, &r.mtime); err != nil {
			_ = rows.Close()
			return err
		}
		if archived != 0 {
			continue
		}
		// 已转正的 wiki 条目只进 Wiki 目录节（带链接），主列表不重复
		if r.draft == 0 && strings.Contains(r.tags, "wiki") {
			continue
		}
		// 带 branch: 标签的条目不进全分支共享的主列表（语义见原实现）
		if BranchOf(splitTags(r.tags)) != "" {
			continue
		}
		s := stats[r.filename]
		r.weight = 2*s.Adoptions + s.Injections
		if r.draft != 0 {
			drafts = append(drafts, r)
		} else {
			main = append(main, r)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	byValue := func(rs []indexRow) {
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].weight != rs[j].weight {
				return rs[i].weight > rs[j].weight
			}
			if rs[i].mtime != rs[j].mtime {
				return rs[i].mtime > rs[j].mtime
			}
			return rs[i].filename < rs[j].filename
		})
	}
	byValue(main)
	byValue(drafts)
	ordered := append(main, drafts...)

	var b strings.Builder
	b.WriteString("# 知识索引\n\n")
	shown := ordered
	var folded []indexRow
	if len(ordered) > maxLines {
		shown, folded = ordered[:maxLines], ordered[maxLines:]
	}
	for _, r := range shown {
		title := r.title
		if r.draft != 0 {
			title = "【草稿】" + title
		}
		if sum := dedupSummary(r.title, r.summary); sum != "" {
			fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, r.typ, r.tags, sum)
		} else {
			fmt.Fprintf(&b, "- **%s** (%s) [%s]\n", title, r.typ, r.tags)
		}
	}
	if len(folded) > 0 {
		writeFoldedLine(&b, folded)
	}
	// 以下为 wiki 目录节/分支差异节：保持原实现不变（从
	// `if wikiEntries, err := db.WikiEntries(); ...` 到 WriteFile 原样保留）
	...原 wiki 节代码...
}

// writeFoldedLine 渲染溢出折叠行：条数 + 被折叠条目 tags 计数降序前 5。
func writeFoldedLine(b *strings.Builder, folded []indexRow) {
	counts := map[string]int{}
	for _, r := range folded {
		for _, tg := range splitTags(r.tags) {
			counts[tg]++
		}
	}
	if len(counts) == 0 {
		fmt.Fprintf(b, "- 另有 %d 条未列出，可用关键词/向量检索命中\n", len(folded))
		return
	}
	type kv struct {
		tag string
		n   int
	}
	pairs := make([]kv, 0, len(counts))
	for tg, n := range counts {
		pairs = append(pairs, kv{tg, n})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}
		return pairs[i].tag < pairs[j].tag
	})
	if len(pairs) > 5 {
		pairs = pairs[:5]
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = fmt.Sprintf("%s×%d", p.tag, p.n)
	}
	fmt.Fprintf(b, "- 另有 %d 条未列出（tags 分布：%s），可用关键词/向量检索命中\n", len(folded), strings.Join(parts, ", "))
}
```

注意：`...原 wiki 节代码...` 处保留现有实现中从 `if wikiEntries, err := db.WikiEntries(); err == nil && len(wikiEntries) > 0 {` 到 `return fsx.WriteFile(...)` 的全部代码（含 `writeWikiLine` 闭包），不得改动。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -v`
Expected: PASS。若有既有测试断言旧 filename 序失败：事件权重全零且 mtime 同秒时新排序退化为 filename 升序，理论上不破；确属语义更新的断言按新行为修正（仅限排序/折叠相关）。

- [ ] **Step 5: Commit**

```bash
git add internal/index/sync.go internal/index/render_test.go
git commit -m "feat(index): INDEX 主列表价值排序+溢出折叠+归档过滤，max_lines 预算生效"
```

---

### Task 6: Sync 调用点透传 `pc.Config.Index.MaxLines`

**Files:**
- Modify: `internal/hook/core.go:41,53`
- Modify: `internal/cli/cli.go:262,273,399,588,807`

**Interfaces:**
- Consumes: `config.Config.Index.MaxLines`（Task 1）、`index.SyncOptions`（Task 3）。
- Produces: 无新接口。`internal/backup/backup.go`、`internal/gui/api.go` 不传参（走默认 50），有意保持。

- [ ] **Step 1: 改 hook 两处**

`internal/hook/core.go` 第 41 行：

```go
	if err := db.Sync(pc.Store.KnowledgeDir(), client, index.SyncOptions{MaxLines: pc.Config.Index.MaxLines}); err != nil {
```

第 53 行：

```go
			if err2 := db.Sync(pc.Store.KnowledgeDir(), nil, index.SyncOptions{MaxLines: pc.Config.Index.MaxLines}); err2 != nil {
```

- [ ] **Step 2: 改 cli 五处**

`internal/cli/cli.go` 第 262、273、399、588、807 行的 `db.Sync(pc.Store.KnowledgeDir(), client或nil)` 一律追加第三参 `, index.SyncOptions{MaxLines: pc.Config.Index.MaxLines}`。这些点都在有 `pc` 作用域的函数内；若某处变量名不是 `pc`，以实际作用域里的 `*project.Context` 为准。

- [ ] **Step 3: 编译 + 全量测试**

Run: `go build ./... && go test ./internal/hook/ ./internal/cli/ ./internal/index/`
Expected: 全部 PASS。

- [ ] **Step 4: Commit**

```bash
git add internal/hook/core.go internal/cli/cli.go
git commit -m "feat: hook/cli 的 Sync 调用透传 index.max_lines 配置"
```

---

### Task 7: `ok archive` 子命令

**Files:**
- Modify: `internal/cli/cli.go`（新增 `Archive` 函数）
- Modify: `cmd/ok/main.go`（dispatch switch + `usage()`）
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `entry.Parse`/`Entry.Serialize`/`Entry.Archived`（Task 2）、`index.SyncOptions`（Task 3）、既有 `resolveFromCwd`、`pc.Store.KnowledgeDir()`、`pc.Store.IndexPath()`。
- Produces: `cli.Archive(args []string, stdout, stderr io.Writer) int`，main.go 以 `case "archive": return cli.Archive(argv[2:], os.Stdout, os.Stderr)` 挂载。

- [ ] **Step 1: 写失败测试**

`internal/cli/cli_test.go` 追加（夹具仿 `hook/hook_test.go` 的 `setupProject`，用本包已导入的 `registry`；`chdir` 帮手本包已有）：

```go
// setupCLIProject 在临时 OK_HOME 下注册 demo 项目并 chdir 进去，返回项目目录与 KB 根。
func setupCLIProject(t *testing.T) (projDir, kbRoot string) {
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
	chdir(t, projDir)
	return projDir, kbRoot
}

func writeCLIEntry(t *testing.T, kbRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "knowledge", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveCommand(t *testing.T) {
	_, kbRoot := setupCLIProject(t)
	writeCLIEntry(t, kbRoot, "old.md", `---
title: 老坑
type: pitfall
summary: s
---

正文。
`)
	var out, errBuf bytes.Buffer
	if code := Archive([]string{"old.md"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	// frontmatter 已写 archived: true
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "old.md"))
	e, err := entry.Parse(data)
	if err != nil || !e.Archived {
		t.Fatalf("archived 未写入: %v %+v", err, e)
	}
	// INDEX 不含归档条目
	idx, _ := os.ReadFile(filepath.Join(kbRoot, "INDEX.md"))
	if strings.Contains(string(idx), "老坑") {
		t.Fatalf("归档后 INDEX 仍含条目: %q", idx)
	}
	// --undo 还原
	out.Reset()
	if code := Archive([]string{"--undo", "old.md"}, &out, &errBuf); code != 0 {
		t.Fatalf("undo exit %d: %s", code, errBuf.String())
	}
	idx, _ = os.ReadFile(filepath.Join(kbRoot, "INDEX.md"))
	if !strings.Contains(string(idx), "老坑") {
		t.Fatalf("undo 后 INDEX 应恢复条目: %q", idx)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestArchiveCommand -v`
Expected: FAIL（`Archive` 未定义）。

- [ ] **Step 3: 实现**

`internal/cli/cli.go` 新增：

```go
// Archive: ok archive [--undo] <文件.md...> —— 标记/取消归档并重建索引；
// 归档条目不进 INDEX 主列表，仍保留在库中可检索。
func Archive(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	undo := fs.Bool("undo", false, "取消归档")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "用法: ok archive [--undo] <文件.md...>")
		return 2
	}
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	dir := pc.Store.KnowledgeDir()
	for _, name := range fs.Args() {
		base := filepath.Base(name)
		path := filepath.Join(dir, base)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", base, err)
			return 1
		}
		e, err := entry.Parse(data)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", base, err)
			return 1
		}
		e.Archived = !*undo
		if err := os.WriteFile(path, e.Serialize(), 0o644); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", base, err)
			return 1
		}
	}
	db, err := index.Open(pc.Store.KbPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	if err := db.Sync(dir, nil, index.SyncOptions{MaxLines: pc.Config.Index.MaxLines}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "归档标记已更新，索引已重建")
	return 0
}
```

（`pc.Store.KbPath()` 即 `store.go:15` 的 kb.db 路径，与 `Index` 命令里 `index.Open(...)` 的实参一致；`IndexPath()` 是 INDEX.md 的路径，别混。）

`cmd/ok/main.go` dispatch switch 在 `case "index":` 之后加：

```go
	case "archive":
		return cli.Archive(argv[2:], os.Stdout, os.Stderr)
```

`usage()` 中加一行（对齐既有格式）：

```go
	"fmt.Fprintln... archive [--undo] <文件.md...>  归档/取消归档条目（不进 INDEX，仍可检索）"
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -run TestArchiveCommand -v && go build ./...`
Expected: PASS + 编译通过。

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go cmd/ok/main.go
git commit -m "feat(cli): 新增 ok archive [--undo] 子命令（归档条目不进 INDEX）"
```

---

### Task 8: `ok index` 归档候选报告

**Files:**
- Modify: `internal/cli/cli.go`（`Index` 函数 + 新增 `printArchiveCandidates`）
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `entry.LoadTolerant`、`Entry.Created`/`Entry.Draft`/`Entry.Archived`/`Entry.FileName()`（Task 2）、`db.FeedbackStats(30)`。
- Produces: `printArchiveCandidates(dir string, db *index.DB, stdout io.Writer)`。

- [ ] **Step 1: 写失败测试**

`internal/cli/cli_test.go` 追加（复用 Task 7 的 `setupCLIProject`/`writeCLIEntry` 夹具；`setupCLIProject` 内部已 chdir）：

```go
func TestIndexArchiveCandidates(t *testing.T) {
	_, kbRoot := setupCLIProject(t)
	old := time.Now().AddDate(0, 0, -120).Format("2006-01-02")
	writeCLIEntry(t, kbRoot, "stale.md", "---\ntitle: 陈年条目\ntype: note\ncreated: "+old+"\nsummary: s\n---\n\n正文。\n")
	writeCLIEntry(t, kbRoot, "fresh.md", "---\ntitle: 新条目\ntype: note\ncreated: "+time.Now().Format("2006-01-02")+"\nsummary: s\n---\n\n正文。\n")
	var out, errBuf bytes.Buffer
	if code := Index(nil, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	got := out.String()
	if !strings.Contains(got, "归档候选") || !strings.Contains(got, "stale.md") {
		t.Fatalf("缺归档候选报告: %q", got)
	}
	if strings.Contains(got, "fresh.md") {
		t.Fatalf("新条目不应入选: %q", got)
	}
}
```

（cli_test.go 需补 `"time"` 导入；`Index` 运行需要离线——无 embedding 配置时 client 为 nil 走纯索引路径，与既有测试一致。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestIndexArchiveCandidates -v`
Expected: FAIL（报告未实现，断言 `归档候选` 不成立）。

- [ ] **Step 3: 实现**

`internal/cli/cli.go` 新增：

```go
// printArchiveCandidates 在 ok index 输出末尾附归档候选：created 超 90 天
// 且近 30 天零注入零采纳的条目（仅提示，不动数据）。统计/解析失败只省略报告。
func printArchiveCandidates(dir string, db *index.DB, stdout io.Writer) {
	entries, _ := entry.LoadTolerant(dir)
	stats, err := db.FeedbackStats(30)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -90)
	var names []string
	for _, e := range entries {
		if e.Draft || e.Archived || e.Created == "" {
			continue
		}
		created, err := time.Parse("2006-01-02", e.Created)
		if err != nil || created.After(cutoff) {
			continue
		}
		s := stats[e.FileName()]
		if s.Injections == 0 && s.Adoptions == 0 {
			names = append(names, e.FileName())
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	fmt.Fprintln(stdout, "归档候选（创建超 90 天且近 30 天零使用，可 ok archive <文件> 归档）:")
	for _, n := range names {
		fmt.Fprintf(stdout, "  - %s\n", n)
	}
}
```

（`sort` 未导入则加。）在 `Index` 函数体内**每个** `return 0` 分支前调用一次 `printArchiveCandidates(pc.Store.KnowledgeDir(), db, stdout)`（含 "INDEX 已更新…" 的早退分支与末尾成功分支）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): ok index 输出归档候选报告（90 天未动且 30 天零使用）"
```

---

### Task 9: 源头约定——摘要不得复读标题

**Files:**
- Modify: `internal/cli/cli.go:544`
- Modify: `internal/setupx/skills/openknowledge-wiki/SKILL.md:67`

**Interfaces:**
- Produces: 无代码接口。用户态技能副本（`C:/Users/Administrator/.agents/skills/openknowledge-propose/SKILL.md`）在工作区外，不在本计划内改动，收尾时提示用户同步。

- [ ] **Step 1: 改 `ok add` 模板文案**

`internal/cli/cli.go` 第 544 行：

```go
	content := "TODO: 在此填写正文（summary 也请补充：写标题之外的检索线索，不要复读标题）"
```

- [ ] **Step 2: 改 wiki 技能约定**

`internal/setupx/skills/openknowledge-wiki/SKILL.md` 第 67 行改为：

```markdown
- `summary` 必填一句话——它会出现在 INDEX.md 的 Wiki 目录里；写标题之外的检索线索，不要复读标题。
```

- [ ] **Step 3: 验证**

Run: `go build ./... && go test ./internal/setupx/ ./internal/cli/`
Expected: PASS（setupx 有 embed 模板相关测试，确认文案改动不破断言；若有断言引用旧文案，同步更新）。

- [ ] **Step 4: Commit**

```bash
git add internal/cli/cli.go internal/setupx/skills/openknowledge-wiki/SKILL.md
git commit -m "docs: 摘要约定改为不复读标题（ok add 模板 + wiki 技能）"
```

---

### Task 10: hook 集成测试——注入文本含折叠行

**Files:**
- Test: `internal/hook/hook_test.go`

**Interfaces:**
- Consumes: Task 6 的 hook 透传 + Task 5 的折叠行格式。

- [ ] **Step 1: 写测试（本任务即测试，先红后绿依赖 Task 5/6 已落地；若顺序执行此处应直接绿，作为回归守门保留）**

`internal/hook/hook_test.go` 追加：

```go
// TestPromptIndexFoldedLine 超 max_lines 的条目折叠为可检索提示行注入，
// 替代外层 token 截断的静默丢失。
func TestPromptIndexFoldedLine(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[index]\nmax_lines = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk := func(title, tags string) string {
		return "---\ntitle: " + title + "\ntype: note\ntags: [" + tags + "]\nsummary: " + title + "的补充\n---\n\n正文。\n"
	}
	writeEntry(t, kbRoot, "a.md", mk("甲条目", "agentx"))
	writeEntry(t, kbRoot, "b.md", mk("乙条目", "hooks"))
	writeEntry(t, kbRoot, "c.md", mk("丙条目", "agentx"))
	mkPrompt := func(text string) string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":%q}]}`, projDir, text)
	}
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(mkPrompt("随便问点啥")), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "另有 1 条未列出") {
		t.Fatalf("注入缺折叠行: %q", got)
	}
	// 无事件+mtime 同秒退化为 filename 序：丙条目被折叠
	if strings.Contains(got, "- **丙条目**") {
		t.Fatalf("被折叠条目标题不应出现在注入里: %q", got)
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/hook/ -run TestPromptIndexFoldedLine -v`
Expected: PASS。若 FAIL 且原因是检索门控吞掉了检索段——折叠行属于基础注入（INDEX 段）不受门控影响，仍应出现；真失败则按实际输出定位是透传（Task 6）还是渲染（Task 5）问题。

- [ ] **Step 3: Commit**

```bash
git add internal/hook/hook_test.go
git commit -m "test(hook): 集成断言索引溢出折叠行进入注入文本"
```

---

### Task 11: 全量回归 + 收尾

**Files:**
- 视回归结果而定（仅限本计划语义导致的断言更新）

- [ ] **Step 1: 全量测试**

Run: `go test ./...`
Expected: 全部 PASS。`internal/index`（wiki_test/branch_test/draft_test/index_test）、`internal/gui`、`internal/hook` 中若有断言旧 filename 排序或旧行格式的用例失败，逐一核对：确属本计划语义更新的，按新行为修断言；否则是回归，回到对应任务修实现。

- [ ] **Step 2: 真实库冒烟**

在项目根跑：

```bash
go build -o dist/ok.exe ./cmd/ok && ./dist/ok.exe index
```

Expected: `INDEX.md` 重建，主列表行数 ≤50，复读标题的条目行不再带冗余摘要，超限时出现 `- 另有 N 条未列出…` 行。

- [ ] **Step 3: 收尾提醒**

提醒用户：用户态 `openknowledge-propose` 技能（`C:/Users/Administrator/.agents/skills/openknowledge-propose/SKILL.md`）的摘要约定在工作区外，需要用户确认后另行同步；本次新机制（archive 命令、max_lines、折叠行）属于结构型变化，适合按约定用 openknowledge-wiki 沉淀。
