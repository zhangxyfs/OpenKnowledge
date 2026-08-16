# 注入→采纳反馈闭环（v2.19.0）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 打通"注入→采纳"反馈闭环：prompt hook 记录注入事件，post-tool hook 捕获"本会话注入过的条目被读取"（采纳），30 天窗口内持续注入但零采纳的条目降权（v1 只降不升 ×0.8）。

**Architecture:** 新表 `entry_events(filename, kind, ts)`（Open 自动迁移，Sync 顺带 prune 60 天）；采纳捕获**不在 post-tool 开库**——post-tool 只把采纳挂账进 session 状态（`state.Update` 锁内），下一次 `InjectForPrompt`（反正要开库）开头入账。降权为纯函数 `applyFeedback`（镜像 applyRecency 分层：queryAll 顶部一次 `FeedbackStats` GROUP BY 查询，纯函数改分数），叠乘在 recency 之后。

**Tech Stack:** Go（标准库 only）、SQLite（entry_events）、TOML 配置。

**Spec:** `docs/superpowers/specs/2026-08-16-retrieval-evolution.md` §5（特性③）。

## Global Constraints

- **post-tool 不开库**：TrackTouched 只碰 session 状态文件，不加 DB 依赖（新故障面 + 延迟）。采纳挂账在 session，下次 InjectForPrompt 开头入账；会话就此结束则挂账丢失——统计性信号，可接受。
- **归因窗口 = 本会话**：只统计"读本会话注入过的条目"；模型凭记忆（非注入）主动读条目不统计（v1 明确限制）。
- **mandatory 粘性指针重读不计入**：mandatory 不经检索、文件名从不进 InjectedKnowledge，天然排除（不写特判代码）。
- **v1 只降不升**：injections ≥ min_injections（默认 4）且 adoptions == 0 → score ×= demote（默认 0.8）；不做加分。
- **终审裁决：v2.19.0 `feedback.enabled` 默认 false**——宿主 read 派发未接通前采纳信号恒零，降权默认关闭以免误伤（事件照常记录攒数据），read 派发接通后恢复默认 true。
- **fail-open**：事件写失败、统计查询失败、状态写失败均仅 logErr，不阻断注入；FeedbackStats 失败 = 跳过降权。
- **并发铁律**：session 状态一切写必须走 `state.Update`（锁内重放 + 原子落盘），InjectedKnowledge/AdoptedKnowledge 不例外。
- **大小写**：`registry.NormalizePath` 会转小写——session 匹配用 `strings.EqualFold`；entry_events 与 session 两个字段一律存**原始大小写** basename（Hit.Filename 即磁盘 basename），否则 GROUP BY 统计对不上。
- 与特性②的系数叠乘（0.85 × 0.8 = 0.68），不设额外下限。
- 测试风格：标准库 testing + t.TempDir()，非表驱动、不用 testify。
- 本 plan 不含版本号 bump / sync-version / dist 同步；GUI 暴露 feedback 配置是非目标（跟进项）。

---

### Task 1: config 增 `Retrieve.Feedback` 子表 + 默认值

**Files:**
- Modify: `internal/config/config.go`（Retrieve 结构体、Default）
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: 无
- Produces（Task 5 依赖，不得改）：
  - `type RetrieveFeedback struct { Enabled bool; WindowDays int; MinInjections int; Demote float64 }`（toml `enabled`/`window_days`/`min_injections`/`demote`）
  - `Retrieve.Feedback RetrieveFeedback`（toml `feedback`）
  - 默认：Enabled=true、WindowDays=30、MinInjections=4、Demote=0.8（终审裁决：v2.19.0 enabled 默认 false——read 派发未接通，接通后恢复 true）

- [ ] **Step 1: 写失败测试**（追加到 `internal/config/config_test.go`，模板照抄 `TestRecencyConfigDefaultAndOverride`）

```go
func TestFeedbackConfigDefaultAndOverride(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	// 全缺省
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	f := cfg.Retrieve.Feedback
	if !f.Enabled || f.WindowDays != 30 || f.MinInjections != 4 || f.Demote != 0.8 {
		t.Fatalf("unexpected defaults %+v", f)
	}
	// 全局覆盖，项目缺键继承
	if err := os.WriteFile(global, []byte("[retrieve.feedback]\nenabled = false\nwindow_days = 7\nmin_injections = 10\ndemote = 0.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	f = cfg.Retrieve.Feedback
	if f.Enabled || f.WindowDays != 7 || f.MinInjections != 10 || f.Demote != 0.5 {
		t.Fatalf("global override failed %+v", f)
	}
	// 项目覆盖一键、其余继承
	if err := os.WriteFile(project, []byte("[retrieve.feedback]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	f = cfg.Retrieve.Feedback
	if !f.Enabled || f.WindowDays != 7 || f.Demote != 0.5 {
		t.Fatalf("project override failed %+v", f)
	}
	// 项目清掉 → 重新继承全局
	if err := os.WriteFile(project, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Feedback.Enabled {
		t.Fatalf("project 缺键应继承全局 %+v", cfg.Retrieve.Feedback)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestFeedbackConfig -v`
Expected: FAIL（`cfg.Retrieve.Feedback undefined`）

- [ ] **Step 3: 实现**

`internal/config/config.go`，`RecencyWindows` 类型之后加：

```go
// RetrieveFeedback 控制注入→采纳反馈闭环（[retrieve.feedback] 子表）：
// 窗口内持续注入但从未被读取的条目降权（v1 只降不升——加分会自我强化造成
// 条目固化，降权只修"持续噪声"这一种确定的问题）。
type RetrieveFeedback struct {
	Enabled       bool    `toml:"enabled"`        // 默认 true（见 Default）
	WindowDays    int     `toml:"window_days"`    // 统计窗口（天），默认 30；<=0 按 30
	MinInjections int     `toml:"min_injections"` // 触发降权的最低注入次数，默认 4；<=0 按 4
	Demote        float64 `toml:"demote"`         // 降权系数，默认 0.8；<=0 或 >=1 按 0.8
}
```

`Retrieve` 结构体 `Recency` 字段之后加：

```go
	// Feedback 是注入→采纳反馈闭环（[retrieve.feedback] 子表）：采纳归因窗口
	// = 本会话（只统计"读本会话注入过的条目"）。
	Feedback RetrieveFeedback `toml:"feedback"`
```

`Default()` 的 Retrieve 字面量在 Recency 之后加：

```go
			Feedback: RetrieveFeedback{Enabled: true, WindowDays: 30, MinInjections: 4, Demote: 0.8},
```

（注意保持既有 Gate/Recency 段的字面量格式，gofmt 对齐。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): [retrieve.feedback] 反馈闭环配置（window/min_injections/demote）"
```

---

### Task 2: entry_events 表 + events.go（Record/Stats/Prune）+ Sync prune

**Files:**
- Modify: `internal/index/db.go`（schema 常量 :22-43）
- Modify: `internal/index/sync.go`（Sync 尾部，:60 起）
- Create: `internal/index/events.go`
- Test: `internal/index/events_test.go`（新建）

**Interfaces:**
- Consumes: 无（config 不依赖）
- Produces（Task 4/5 依赖，不得改）：
  - `const EventInjected = "injected"` / `const EventAdopted = "adopted"`
  - `func (db *DB) RecordEvents(kind string, filenames []string) error` — 批量插入，单 ts（time.Now().Unix()），事务
  - `type FeedbackStat struct { Injections int; Adoptions int }`
  - `func (db *DB) FeedbackStats(windowDays int) (map[string]FeedbackStat, error)` — `windowDays<=0` 按 30
  - `func (db *DB) PruneEvents(olderThan int64) error`

- [ ] **Step 1: 写失败测试**

```go
package index

import (
	"path/filepath"
	"testing"
	"time"
)

func openEventsDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRecordEventsAndStats(t *testing.T) {
	db := openEventsDB(t)
	// a.md：4 注入 0 采纳；b.md：3 注入 1 采纳
	if err := db.RecordEvents(EventInjected, []string{"a.md", "a.md", "b.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventInjected, []string{"a.md", "b.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventInjected, []string{"a.md", "b.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventAdopted, []string{"b.md"}); err != nil {
		t.Fatal(err)
	}
	stats, err := db.FeedbackStats(30)
	if err != nil {
		t.Fatal(err)
	}
	if s := stats["a.md"]; s.Injections != 4 || s.Adoptions != 0 {
		t.Fatalf("a.md stats 错: %+v", s)
	}
	if s := stats["b.md"]; s.Injections != 3 || s.Adoptions != 1 {
		t.Fatalf("b.md stats 错: %+v", s)
	}
	// 空列表 no-op
	if err := db.RecordEvents(EventInjected, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackStatsWindowAndPrune(t *testing.T) {
	db := openEventsDB(t)
	now := time.Now().Unix()
	old := now - 40*86400 // 40 天前，超出 30 天窗口
	// 直接 SQL 造一条旧事件（RecordEvents 只有 now）
	if _, err := db.sql.Exec(`INSERT INTO entry_events(filename, kind, ts) VALUES('old.md', 'injected', ?)`, old); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventInjected, []string{"new.md"}); err != nil {
		t.Fatal(err)
	}
	stats, err := db.FeedbackStats(30)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stats["old.md"]; ok {
		t.Fatal("窗口外事件不应统计")
	}
	if stats["new.md"].Injections != 1 {
		t.Fatalf("new.md 应统计: %+v", stats)
	}
	// PruneEvents 删 60 天前：40 天前的保留，61 天前的删除
	veryOld := now - 61*86400
	if _, err := db.sql.Exec(`INSERT INTO entry_events(filename, kind, ts) VALUES('ancient.md', 'injected', ?)`, veryOld); err != nil {
		t.Fatal(err)
	}
	if err := db.PruneEvents(now - 60*86400); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM entry_events WHERE filename = 'ancient.md'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("61 天前事件应被 prune")
	}
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM entry_events WHERE filename = 'old.md'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("40 天前事件应保留")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run 'TestRecordEvents|TestFeedbackStats' -v`
Expected: FAIL（`undefined: db.RecordEvents`）

- [ ] **Step 3: 实现**

**① schema**（`internal/index/db.go` schema 常量，`meta` 表之后追加）：

```sql
CREATE TABLE IF NOT EXISTS entry_events(
  filename TEXT NOT NULL, kind TEXT NOT NULL, ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_entry_events_filename_ts ON entry_events(filename, ts);
```

（`CREATE TABLE IF NOT EXISTS` 老库打开即自动迁移，无需 ALTER。）

**② `internal/index/events.go`（新建）：**

```go
package index

import "time"

// 事件类型：injected（条目被检索注入）/ adopted（本会话注入过的条目被读取=采纳）。
const (
	EventInjected = "injected"
	EventAdopted  = "adopted"
)

// RecordEvents 批量记录条目事件（同一 ts）。文件名一律原始大小写 basename
//（与 entries.filename / Hit.Filename 一致，否则统计对不上）。
func (db *DB) RecordEvents(kind string, filenames []string) error {
	if len(filenames) == 0 {
		return nil
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, f := range filenames {
		if _, err := tx.Exec(`INSERT INTO entry_events(filename, kind, ts) VALUES(?,?,?)`, f, kind, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// FeedbackStat 是一条目在统计窗口内的注入/采纳计数。
type FeedbackStat struct {
	Injections int
	Adoptions  int
}

// FeedbackStats 返回最近 windowDays 天内各条目的注入/采纳计数（30 天事件千级，
// 全表分组毫秒级）。windowDays<=0 按 30。
func (db *DB) FeedbackStats(windowDays int) (map[string]FeedbackStat, error) {
	if windowDays <= 0 {
		windowDays = 30
	}
	since := time.Now().Unix() - int64(windowDays)*86400
	rows, err := db.sql.Query(`SELECT filename, kind, COUNT(*) FROM entry_events WHERE ts >= ? GROUP BY filename, kind`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FeedbackStat{}
	for rows.Next() {
		var name, kind string
		var n int
		if err := rows.Scan(&name, &kind, &n); err != nil {
			return nil, err
		}
		s := out[name]
		switch kind {
		case EventInjected:
			s.Injections = n
		case EventAdopted:
			s.Adoptions = n
		}
		out[name] = s
	}
	return out, rows.Err()
}

// PruneEvents 删除 olderThan（Unix 秒）之前的事件。Sync 时顺带调用（60 天）。
func (db *DB) PruneEvents(olderThan int64) error {
	_, err := db.sql.Exec(`DELETE FROM entry_events WHERE ts < ?`, olderThan)
	return err
}
```

**③ Sync 尾部**（`internal/index/sync.go` Sync 函数 return 之前；先 Read 确认 Sync 结尾形态）加：

```go
	// 顺带 prune 60 天前的条目事件（统计性数据，失败不阻断 Sync）
	_ = db.PruneEvents(time.Now().Unix() - 60*86400)
```

（`time` 若未 import 则补。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/index/db.go internal/index/sync.go internal/index/events.go internal/index/events_test.go
git commit -m "feat(index): entry_events 表 + RecordEvents/FeedbackStats/PruneEvents（Sync 顺带 prune）"
```

---

### Task 3: state.Session 增 InjectedKnowledge / AdoptedKnowledge

**Files:**
- Modify: `internal/state/state.go`（Session :14-30）
- Test: `internal/state/state_test.go`

**Interfaces:**
- Consumes: 无
- Produces（Task 4 依赖，不得改）：
  - `Session.InjectedKnowledge []string`（json `injected_knowledge`）——本会话最近一轮注入的检索条目（原始大小写 basename）
  - `Session.AdoptedKnowledge []string`（json `adopted_knowledge`）——待入账的采纳挂账
  - `func (s *Session) AddAdopted(name string)` — 去重追加

- [ ] **Step 1: 写失败测试**（追加到 `internal/state/state_test.go`）

```go
func TestSessionAdoptedKnowledge(t *testing.T) {
	dir := t.TempDir()
	// 去重追加
	if err := Update(dir, "s1", func(s *Session) {
		s.AddAdopted("a.md")
		s.AddAdopted("a.md")
		s.AddAdopted("b.md")
		s.InjectedKnowledge = []string{"a.md", "b.md"}
	}); err != nil {
		t.Fatal(err)
	}
	st := Load(dir, "s1")
	if len(st.AdoptedKnowledge) != 2 || st.AdoptedKnowledge[0] != "a.md" || st.AdoptedKnowledge[1] != "b.md" {
		t.Fatalf("AddAdopted 去重失败: %v", st.AdoptedKnowledge)
	}
	if len(st.InjectedKnowledge) != 2 {
		t.Fatalf("InjectedKnowledge 落盘失败: %v", st.InjectedKnowledge)
	}
	// 入账后清空挂账、保留注入清单（模拟 InjectForPrompt 开头的入账动作）
	if err := Update(dir, "s1", func(s *Session) {
		s.AdoptedKnowledge = nil
	}); err != nil {
		t.Fatal(err)
	}
	st = Load(dir, "s1")
	if len(st.AdoptedKnowledge) != 0 || len(st.InjectedKnowledge) != 2 {
		t.Fatalf("清挂账/留注入失败: %+v", st)
	}
	// 并发场合并不覆盖（既有 TestUpdateMergesConcurrentFields 同款断言路径）：
	// 两次 Update 分别改两字段互不丢
	if err := Update(dir, "s1", func(s *Session) { s.AddAdopted("c.md") }); err != nil {
		t.Fatal(err)
	}
	st = Load(dir, "s1")
	if len(st.InjectedKnowledge) != 2 || len(st.AdoptedKnowledge) != 1 {
		t.Fatalf("Update 合并失败: %+v", st)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/state/ -run TestSessionAdoptedKnowledge -v`
Expected: FAIL（`AddAdopted undefined`）

- [ ] **Step 3: 实现**

`internal/state/state.go` Session 结构体 `MergedChecked` 之后加：

```go
	// InjectedKnowledge 本会话最近一轮注入的检索条目（原始大小写 basename，
	// 供采纳归因）；AdoptedKnowledge 待入账的采纳挂账（post-tool 不开库，
	// 下次 InjectForPrompt 开头入账 entry_events 后清空；会话结束挂账丢失，
	// 统计性信号可接受）。
	InjectedKnowledge []string `json:"injected_knowledge"`
	AdoptedKnowledge  []string `json:"adopted_knowledge"`
```

`AddTouched`（:120-127）之后加同款 helper：

```go
// AddAdopted 去重追加采纳挂账。
func (s *Session) AddAdopted(name string) {
	for _, v := range s.AdoptedKnowledge {
		if v == name {
			return
		}
	}
	s.AdoptedKnowledge = append(s.AdoptedKnowledge, name)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/state/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/state/state.go internal/state/state_test.go
git commit -m "feat(state): Session 增 InjectedKnowledge/AdoptedKnowledge（采纳归因挂账）"
```

---

### Task 4: hook 接线——入账 + 注入记录 + TrackTouched 知识库分支

**Files:**
- Modify: `internal/hook/core.go`（InjectForPrompt 开头 :41-61 区域、检索段 :162-173、TrackTouched :237-254）
- Modify: `internal/hook/hook.go`（relativize :282-295 旁加 knowledgeBase helper）
- Test: `internal/hook/core_test.go`

**Interfaces:**
- Consumes: `db.RecordEvents` / `EventInjected` / `EventAdopted`（Task 2）、`Session.InjectedKnowledge/AdoptedKnowledge/AddAdopted`（Task 3）
- Produces:
  - `func knowledgeBase(pc *project.Context, abs string) (string, bool)`（hook 包私有）——规范化路径位于知识库目录内则返回小写 basename
  - 行为契约：见下方测试断言

**背景（现状代码锚点）**：
- `InjectForPrompt`：`index.Open` :34-40、`db.Sync` :41-60、`st := state.Load(...)` :61；检索段 hits 消费在 :162-173（`if len(hits) > 0` 写"## 相关知识"）。
- `TrackTouched`（core.go:237-254）：`relativize` 返回 "" 时记 `post-tool skip` 日志返回；写状态走 `state.Update`。
- `relativize`（hook.go:282-295）：`registry.NormalizePath`（转小写）+ 项目根前缀判定。
- 动手前 Read 核实行号。

- [ ] **Step 1: 写失败测试**（追加到 `internal/hook/core_test.go`；夹具 setupProject/writeEntry 同包现成）

```go
// TestAdoptionLoop 注入→采纳全链路：检索注入挂账 InjectedKnowledge →
// post-tool 读知识库文件记 AdoptedKnowledge → 下一轮 prompt 开头入账
// entry_events(adopted)；mandatory 重读与项目外路径不计入。
func TestAdoptionLoop(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(kbRoot, "state")
	// 第一轮：检索注入 → InjectedKnowledge 挂账 + injected 事件
	out := InjectForPrompt(pc, "s-adopt", projDir, "RetrievalQuirk 是什么")
	if !strings.Contains(out, "检索经验") {
		t.Fatalf("首轮应注入检索命中: %q", out)
	}
	st := state.Load(stateDir, "s-adopt")
	if len(st.InjectedKnowledge) != 1 || st.InjectedKnowledge[0] != "检索.md" {
		t.Fatalf("InjectedKnowledge 挂账失败: %+v", st.InjectedKnowledge)
	}
	// post-tool 读知识库内已注入条目 → 采纳挂账（知识库目录在项目路径之外）
	TrackTouched(pc, "s-adopt", "read_file", filepath.Join(kbRoot, "knowledge", "检索.md"))
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 1 || st.AdoptedKnowledge[0] != "检索.md" {
		t.Fatalf("采纳挂账失败: %+v", st.AdoptedKnowledge)
	}
	// mandatory 粘性指针重读不计入（mandatory 不经检索，不在 InjectedKnowledge）
	TrackTouched(pc, "s-adopt", "read_file", filepath.Join(kbRoot, "knowledge", "规约.md"))
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 1 {
		t.Fatalf("mandatory 重读不应计入采纳: %+v", st.AdoptedKnowledge)
	}
	// 未注入过的知识库文件不计入
	writeEntry(t, kbRoot, "别的.md", "---\ntitle: 别的\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n无关。\n")
	TrackTouched(pc, "s-adopt", "read_file", filepath.Join(kbRoot, "knowledge", "别的.md"))
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 1 {
		t.Fatalf("未注入条目不挂账: %+v", st.AdoptedKnowledge)
	}
	// 第二轮 prompt：开头入账 → entry_events 有 adopted 行，挂账清空
	_ = InjectForPrompt(pc, "s-adopt", projDir, "随便问问")
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 0 {
		t.Fatalf("入账后挂账应清空: %+v", st.AdoptedKnowledge)
	}
	db, err := index.Open(filepath.Join(kbRoot, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stats, err := db.FeedbackStats(30)
	if err != nil {
		t.Fatal(err)
	}
	s := stats["检索.md"]
	if s.Adoptions != 1 {
		t.Fatalf("entry_events 应有 1 条 adopted: %+v", s)
	}
	if s.Injections < 1 {
		t.Fatalf("entry_events 应有 injected 行: %+v", s)
	}
	// 回归：项目内文件仍走 Touched（既有行为不变）
	TrackTouched(pc, "s-adopt", "write_file", filepath.Join(projDir, "a.go"))
	st = state.Load(stateDir, "s-adopt")
	found := false
	for _, v := range st.Touched {
		if v == "a.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("项目内文件仍应记 Touched: %+v", st.Touched)
	}
}
```

注意：若 `stateDir` 的实际取法与夹具不符（参照 hook_test.go:524-528 的写法），以夹具先例为准；`InjectedKnowledge[0]` 断言依赖首轮检索恰好 1 个 hit——setupProject 夹具下 `top_n` 默认为 2 但只有一条相关条目，预期 1 hit；若实际 ≥2（把"规约.md"也检索出来——不会，mandatory 不进检索），停下报告 DONE_WITH_CONCERNS。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hook/ -run TestAdoptionLoop -v`
Expected: FAIL（InjectedKnowledge 空——挂账未实现）

- [ ] **Step 3: 实现**（三处）

**① InjectForPrompt 开头入账**（`db.Sync(...)` 之后、`st := state.Load(...)` 之前）：

```go
	// 采纳入账：post-tool 不开库，采纳先挂账在 session 状态；本轮反正开了库，
	// 开头先入账（读挂账到闭包外再清空）。会话就此结束则挂账丢失——统计性
	// 信号，可接受。fail-open：失败仅记日志。
	var adopted []string
	if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
		adopted = st.AdoptedKnowledge
		st.AdoptedKnowledge = nil
	}); err != nil {
		logErr("prompt adopt load: %v", err)
	}
	if len(adopted) > 0 {
		if err := db.RecordEvents(index.EventAdopted, adopted); err != nil {
			logErr("prompt adopt record: %v", err)
		}
	}
```

（门控短路在此之后——被门控的轮次同样入账，符合 spec"开头先入账"。）

**② 检索段注入记录**（`if len(hits) > 0` 块，改前为 core.go:162-173 的既有代码）改后完整形态：

```go
	if len(hits) > 0 {
		restText.WriteString("## 相关知识（需要全文时读取对应文件）\n\n")
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			p := filepath.ToSlash(filepath.Join(pc.Store.KnowledgeDir(), h.Filename))
			if h.Summary != "" {
				fmt.Fprintf(&restText, "- **%s** (%s) — %s（%s）\n", h.Title, h.Type, h.Summary, p)
			} else {
				fmt.Fprintf(&restText, "- **%s** (%s)（%s）\n", h.Title, h.Type, p)
			}
			names = append(names, h.Filename)
		}
		restText.WriteString("\n")
		// 注入事件 + 会话挂账（采纳归因的数据源；原始大小写 basename）；
		// fail-open：写失败仅记日志
		if err := db.RecordEvents(index.EventInjected, names); err != nil {
			logErr("prompt inject record: %v", err)
		}
		if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
			st.InjectedKnowledge = names
		}); err != nil {
			logErr("prompt inject state: %v", err)
		}
	}
```

**③ TrackTouched 知识库分支**（`rel == ""` 分支改写）+ **hook.go 的 knowledgeBase helper**：

```go
	rel := relativize(pc, filePath)
	if rel == "" {
		// 知识库目录在项目路径之外：规范化路径位于 KnowledgeDir 且 basename 命中
		// 本会话注入过的条目 → 采纳挂账（锁内读判+写，post-tool 不开库）。
		// mandatory 粘性指针重读不经检索、不在 InjectedKnowledge，天然不计入。
		if base, ok := knowledgeBase(pc, filePath); ok {
			if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
				for _, name := range st.InjectedKnowledge {
					if strings.EqualFold(name, base) {
						st.AddAdopted(name)
						break
					}
				}
			}); err != nil {
				logErr("post-tool adopt state: %v", err)
			}
			return
		}
		logErr("post-tool skip: tool=%s path=%q 不在项目 %s 的路径内", toolName, filePath, pc.Project.Name)
		return
	}
```

```go
// knowledgeBase 判定路径是否位于知识库目录内，是则返回其 basename（已随
// NormalizePath 转小写；与 InjectedKnowledge 的比对用 EqualFold）。
func knowledgeBase(pc *project.Context, abs string) (string, bool) {
	if abs == "" {
		return "", false
	}
	normAbs := registry.NormalizePath(abs)
	kd := registry.NormalizePath(pc.Store.KnowledgeDir())
	if strings.HasPrefix(normAbs, kd+"/") {
		return filepath.Base(normAbs), true
	}
	return "", false
}
```

（knowledgeBase 放 hook.go 的 relativize 旁；`path/filepath`/`strings`/`registry` 均已 import，动手前确认。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/hook/ -v`
Expected: PASS（含既有全部绿）

- [ ] **Step 5: Commit**

```bash
git add internal/hook/core.go internal/hook/hook.go internal/hook/core_test.go
git commit -m "feat(hook): 注入→采纳闭环接线（入账/注入记录/TrackTouched 知识库分支）"
```

---

### Task 5: queryAll 接入 applyFeedback 降权

**Files:**
- Modify: `internal/index/query.go`（顶部 Count 旁 :164-168、尾部 applyRecency :288 之后、QueryInfo :65-78）
- Create: `internal/index/feedback.go`
- Test: `internal/index/feedback_test.go`（新建）

**Interfaces:**
- Consumes: `FeedbackStats` / `FeedbackStat`（Task 2）、`config.Retrieve.Feedback`（Task 1）
- Produces（Task 6 依赖）：
  - `func applyFeedback(hits map[string]*Hit, stats map[string]FeedbackStat, cfg config.RetrieveFeedback) []string`（包内私有）——就地降权，返回被降权条目（`"filename×0.80"`，按分数序）
  - `QueryInfo.FeedbackDemoted []string`

- [ ] **Step 1: 写失败测试**

```go
package index

import (
	"math"
	"path/filepath"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

func TestApplyFeedback(t *testing.T) {
	cfg := config.RetrieveFeedback{Enabled: true, WindowDays: 30, MinInjections: 4, Demote: 0.8}
	mk := func() map[string]*Hit {
		return map[string]*Hit{
			"a.md": {Filename: "a.md", Title: "A", Type: "note", Score: 1.0},
			"b.md": {Filename: "b.md", Title: "B", Type: "note", Score: 1.0},
			"c.md": {Filename: "c.md", Title: "C", Type: "note", Score: 1.0},
		}
	}
	// a：注入 4 次 0 采纳 → 降权；b：注入 4 次有采纳 → 不降；c：注入 3 次 → 不降
	stats := map[string]FeedbackStat{
		"a.md": {Injections: 4, Adoptions: 0},
		"b.md": {Injections: 4, Adoptions: 1},
		"c.md": {Injections: 3, Adoptions: 0},
	}
	hits := mk()
	demoted := applyFeedback(hits, stats, cfg)
	if math.Abs(hits["a.md"].Score-0.8) > 1e-9 {
		t.Errorf("a.md 应降权 0.8: %v", hits["a.md"].Score)
	}
	if hits["b.md"].Score != 1.0 || hits["c.md"].Score != 1.0 {
		t.Errorf("b/c 不应降权: %v %v", hits["b.md"].Score, hits["c.md"].Score)
	}
	if len(demoted) != 1 || demoted[0] != "a.md×0.80" {
		t.Errorf("降权清单错: %v", demoted)
	}
	// 关闭 / stats 查询失败（nil）→ 不动
	hits = mk()
	off := cfg
	off.Enabled = false
	if got := applyFeedback(hits, stats, off); got != nil || hits["a.md"].Score != 1.0 {
		t.Errorf("Enabled=false 不应降权: %v", got)
	}
	hits = mk()
	if got := applyFeedback(hits, nil, cfg); got != nil || hits["a.md"].Score != 1.0 {
		t.Errorf("stats=nil（fail-open）不应降权: %v", got)
	}
	// demote 非法按 0.8
	hits = mk()
	bad := cfg
	bad.Demote = 1.5
	applyFeedback(hits, stats, bad)
	if math.Abs(hits["a.md"].Score-0.8) > 1e-9 {
		t.Errorf("demote 非法应按 0.8: %v", hits["a.md"].Score)
	}
}

// TestQueryFeedbackDemote 真实库集成：a.md 有 4 注入 0 采纳事件 → 开启反馈后
// 分数恰为关闭时的 0.8 倍（融合分不受事件影响，倍率确定性断言）；
// QueryInfo.FeedbackDemoted 上榜。
func TestQueryFeedbackDemote(t *testing.T) {
	dir := t.TempDir()
	kdir := filepath.Join(dir, "knowledge")
	writeEntryFile(t, kdir, "a.md", "---\ntitle: 苹果条目\ntype: note\ntags: []\n---\n\n苹果 香蕉。\n")
	db, err := Open(filepath.Join(dir, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventInjected, []string{"a.md", "a.md", "a.md", "a.md"}); err != nil {
		t.Fatal(err)
	}
	terms := retrieve.Terms("苹果")
	off := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5}
	hitsOff, _, err := db.QueryEx(terms, nil, off)
	if err != nil || len(hitsOff) != 1 {
		t.Fatalf("基线查询: %v %+v", err, hitsOff)
	}
	on := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5,
		Feedback: config.RetrieveFeedback{Enabled: true, WindowDays: 30, MinInjections: 4, Demote: 0.8}}
	hitsOn, info, err := db.QueryEx(terms, nil, on)
	if err != nil || len(hitsOn) != 1 {
		t.Fatalf("反馈查询: %v %+v", err, hitsOn)
	}
	if math.Abs(hitsOn[0].Score-hitsOff[0].Score*0.8) > 1e-12 {
		t.Fatalf("应恰好 ×0.8: off=%v on=%v", hitsOff[0].Score, hitsOn[0].Score)
	}
	if len(info.FeedbackDemoted) != 1 || info.FeedbackDemoted[0] != "a.md×0.80" {
		t.Fatalf("FeedbackDemoted 错: %v", info.FeedbackDemoted)
	}
	// 窗口外事件不计：window_days=0（非法按 30）场景已由 config 层保证，此处
	// 验证 min_injections 未达不触发
	if err := db.RecordEvents(EventAdopted, []string{"a.md"}); err != nil {
		t.Fatal(err)
	}
	hitsOn2, info2, err := db.QueryEx(terms, nil, on)
	if err != nil || len(hitsOn2) != 1 {
		t.Fatal(err)
	}
	if hitsOn2[0].Score != hitsOff[0].Score || len(info2.FeedbackDemoted) != 0 {
		t.Fatalf("有采纳后不应降权: %v %v", hitsOn2[0].Score, info2.FeedbackDemoted)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run 'TestApplyFeedback|TestQueryFeedbackDemote' -v`
Expected: FAIL（`undefined: applyFeedback`）

- [ ] **Step 3: 实现**

**① `internal/index/feedback.go`（新建）：**

```go
package index

import (
	"fmt"
	"sort"

	"openknowledge/internal/config"
)

// applyFeedback 对"持续注入但从未被采纳"的条目降权（v1 只降不升——加分会自我
// 强化造成条目固化，降权只修"持续噪声"这一种确定的问题）：窗口内
// injections >= min_injections 且 adoptions == 0 → score ×= demote。
// 与 recency 系数叠乘（0.85×0.8=0.68），不设额外下限。
// 返回被降权条目（"filename×0.80"，按当前分数降序、标题升序的确定性顺序）。
// fail-open：Enabled=false、stats==nil（统计查询失败）→ 不动；demote 非法
//（<=0 或 >=1）按 0.8；minInjections<=0 按 4。
func applyFeedback(hits map[string]*Hit, stats map[string]FeedbackStat, cfg config.RetrieveFeedback) []string {
	if !cfg.Enabled || stats == nil {
		return nil
	}
	demote := cfg.Demote
	if demote <= 0 || demote >= 1 {
		demote = 0.8
	}
	minInj := cfg.MinInjections
	if minInj <= 0 {
		minInj = 4
	}
	sorted := make([]*Hit, 0, len(hits))
	for _, h := range hits {
		sorted = append(sorted, h)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Score != sorted[j].Score {
			return sorted[i].Score > sorted[j].Score
		}
		return sorted[i].Title < sorted[j].Title
	})
	var demoted []string
	for _, h := range sorted {
		s, ok := stats[h.Filename]
		if ok && s.Injections >= minInj && s.Adoptions == 0 {
			h.Score *= demote
			demoted = append(demoted, fmt.Sprintf("%s×%.2f", h.Filename, demote))
		}
	}
	return demoted
}
```

**② queryAll 顶部**（`db.Count()` floor 计算块之后）：

```go
	// 反馈统计（30 天窗口一条 GROUP BY，千级事件毫秒级）；fail-open：
	// 查询失败仅跳过降权，不动准入。
	var fbStats map[string]FeedbackStat
	if cfg.Feedback.Enabled {
		if s, err := db.FeedbackStats(cfg.Feedback.WindowDays); err == nil {
			fbStats = s
		}
	}
```

**③ queryAll 尾部**（`info.RecencyShifted = applyRecency(...)` 之后）：

```go
	// 反馈降权：持续注入但从未被读的条目 ×demote（v1 只降不升）；
	// 叠乘在时效系数之后。
	info.FeedbackDemoted = applyFeedback(hits, fbStats, cfg.Feedback)
```

**④ QueryInfo** 加字段 + doc 注释更新：

```go
	// RecencyShifted：因时效系数（retrieve.recency）名次变差的条目
	//（"filename×0.85" 格式，按新名次排序）。
	RecencyShifted []string
	// FeedbackDemoted：因反馈闭环（retrieve.feedback）被降权的条目
	//（"filename×0.80" 格式）。
	FeedbackDemoted []string
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -v`
Expected: PASS（含既有全部绿——测试字面量零值 Feedback.Enabled=false，既有测试不受影响）

- [ ] **Step 5: Commit**

```bash
git add internal/index/feedback.go internal/index/feedback_test.go internal/index/query.go
git commit -m "feat(index): applyFeedback 降权接入 queryAll（持续注入零采纳 ×0.8）"
```

---

### Task 6: 观测链路 + 文档 + changelog

**Files:**
- Modify: `internal/hook/core.go`（recency 日志块之后 :160 区域）
- Modify: `internal/cli/cli.go`（recency stderr 块之后 :355 区域）
- Modify: `docs/ARCHITECTURE.md`（配置表 retrieve.recency 两行之后）
- Create: `docs/changelogs/2026-08-16-feedback-loop.md`

**Interfaces:**
- Consumes: `QueryInfo.FeedbackDemoted`（Task 5）
- Produces: 无新符号

- [ ] **Step 1: 实现观测链路**（照 recency 同款，3 行透传，不加 hook 测试——降权触发在 hook 层不可控，同前轮裁决）

`internal/hook/core.go` recency 日志块之后：

```go
		// 反馈降权命中（持续注入但从未被读）：记 ok.log，GUI 日志页可按"反馈"过滤
		if len(info.FeedbackDemoted) > 0 {
			logErr("prompt feedback: 反馈降权（%s）", strings.Join(info.FeedbackDemoted, "、"))
		}
```

`internal/cli/cli.go` recency stderr 块之后：

```go
	if len(info.FeedbackDemoted) > 0 {
		fmt.Fprintf(stderr, "反馈降权（持续注入从未被读：%s）\n", strings.Join(info.FeedbackDemoted, "、"))
	}
```

`docs/ARCHITECTURE.md` 配置表 recency 两行之后加：

```
| retrieve.feedback.enabled | `true` | 注入→采纳反馈闭环：窗口内持续注入但从未被读的条目降权（v1 只降不升） |
| retrieve.feedback.window_days / min_injections / demote | `30` / `4` / `0.8` | 统计窗口（天）/ 触发降权的最低注入次数 / 降权系数 |
```

- [ ] **Step 2: 验证**

Run: `go build ./... && go test ./internal/hook/ ./internal/cli/ -v && grep -n "prompt feedback" internal/hook/core.go && grep -n "反馈降权" internal/cli/cli.go docs/ARCHITECTURE.md`
Expected: 全绿；grep 命中

- [ ] **Step 3: 写 changelog**

```markdown
# 2026-08-16 注入→采纳反馈闭环：持续注入零采纳条目降权（retrieve.feedback）

- **问题**：注入后模型读没读该条目，系统完全不知道——没有反馈闭环，持续
  不相关的条目永远占着 top_n 名额。
- **修复**：
  - 新表 `entry_events(filename, kind, ts)`（kind ∈ injected|adopted，
    索引 (filename, ts)）：Open 的 schema 常量含 CREATE TABLE IF NOT EXISTS，
    老库打开即自动迁移；Sync 顺带 prune 60 天前事件；
  - 数据流：prompt hook 选定 hits 时写 injected 事件 + session.InjectedKnowledge；
    post-tool hook 规范化路径位于知识库目录且 basename 命中本会话注入清单时
    记 session.AdoptedKnowledge（EqualFold 匹配，原始大小写入账）；下一次
    InjectForPrompt 开头把挂账入账 entry_events(adopted) 并清空——
    **post-tool 不开库**（避免每工具调用加 DB 依赖），会话就此结束则挂账
    丢失（统计性信号，可接受）；
  - 归因窗口 = 本会话（只统计"读本会话注入过的条目"）；mandatory 粘性指针
    重读不计入（mandatory 不经检索，天然不在注入清单）；
  - **v1 只降不升**：30 天窗口内 injections ≥ min_injections（默认 4）且
    adoptions == 0 → score ×= demote（默认 0.8）；加分会自我强化造成条目
    固化，降权只修"持续噪声"这一种确定的问题；与时效系数叠乘
    （0.85×0.8=0.68），不设额外下限；
  - 观测：降权命中时 hook 记 ok.log `prompt feedback:` 一行、ok search 打
    stderr（GUI 日志可按"反馈"过滤）；fail-open：事件写失败/统计查询失败
    仅记日志或跳过降权；
  - 配置 `[retrieve.feedback]` enabled（默认 true）/ window_days（30）/
    min_injections（4）/ demote（0.8）；GUI 暴露列为跟进项。
- **测试**：`TestRecordEventsAndStats` / `TestFeedbackStatsWindowAndPrune`
  （窗口截止 + prune 边界）/ `TestSessionAdoptedKnowledge`（去重/落盘/
  清挂账/Update 合并不丢）/ `TestAdoptionLoop`（hook 集成：注入挂账 →
  读知识库文件采纳 → 下轮入账；mandatory 与未注入条目不计；项目内文件
  Touched 回归）/ `TestApplyFeedback`（4 注入 0 采纳触发/有采纳不触发/
  未达次数不触发/关闭/nil stats/demote 非法）/ `TestQueryFeedbackDemote`
  （真实库 ×0.8 倍率 + 有采纳恢复）/ `TestFeedbackConfigDefaultAndOverride`
  （配置四态）；全仓 go test ./... 绿。
```

- [ ] **Step 4: 全仓回归 + Commit**

Run: `go test ./...`
Expected: 全部 PASS

```bash
git add internal/hook/core.go internal/cli/cli.go docs/ARCHITECTURE.md docs/changelogs/2026-08-16-feedback-loop.md
git commit -m "feat(hook,cli,docs): 反馈降权观测链路 + 配置表 + changelog"
```
