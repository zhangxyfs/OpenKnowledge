# RRF 融合（v2.17.0）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把混合检索的融合方式从手工标定的 `α·归一BM25 + β·余弦` 换成 RRF（Reciprocal Rank Fusion，只看名次不看分数），消除 embedding 模型更换导致的分数尺度漂移；保留 `fusion = "weighted"` 旧行为回滚档。

**Architecture:** 准入逻辑完全不动（关键词通道归一 BM25 ≥ MinScoreFloor、语义通道 SemanticFloor 模型无关相对门槛）；`queryAll` 两通道扫描时顺带记录各通道准入集合的原始信号（kwNorm / cos），尾部按 `fusion` 配置二选一：weighted 走原有内联算分（逐行保留），rrf 用 `applyRRF` 按通道内名次重算总分 `Σ 1/(rrf_k + rank)`。调用方（ok search / GUI 搜索 / hook 注入）共用 queryAll，自动生效。

**Tech Stack:** Go（标准库 only）、TOML 配置。

**Spec:** `docs/superpowers/specs/2026-08-16-retrieval-evolution.md` §3（特性①）。

## Global Constraints

- **准入按通道独立判定的语义不动**：`MinScoreFloor` / `SemanticFloor` / `QueryInfo` 诊断全部保留，一行不改。
- **tie-break 不变**（总分降序、标题升序）；top_n 截断与分支过滤位置不变（都在 queryAll 之外）。
- `scoreFloor = 1e-6` 保护逻辑**仅属 weighted 模式**；RRF 下负余弦条目根本不进语义名次表，无需保护。
- `fusion` 缺省/非法值一律按 rrf（fail-open）；`rrf_k` 默认 60，<=0 按 60。
- `alpha`/`beta` 仅 weighted 模式生效；rrf 模式下配置了非默认值记一行 ok.log 提示被忽略。
- fail-open：任何新环节出错仅记 ok.log，不阻断注入。
- 测试风格：标准库 `testing` + `t.TempDir()`，非表驱动、不用 testify。
- 本 plan 不含版本号 bump / sync-version / dist 同步——发布时例行处理。
- 已知后续：特性②（时效）需要给两条 SELECT 加 `e.mtime`——本 plan **不顺手加**，保持 diff 最小。

---

### Task 1: config 增 `Fusion` / `RrfK` + 默认值

**Files:**
- Modify: `internal/config/config.go`（Retrieve 结构体 :100-120 区域、Default :181 区域）
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: 无
- Produces（Task 2/3 依赖，不得改）：
  - `Retrieve.Fusion string`（toml `fusion`，默认 `"rrf"`，`"weighted"` 为回滚档）
  - `Retrieve.RrfK int`（toml `rrf_k`，默认 `60`）

- [ ] **Step 1: 写失败测试**（追加到 `internal/config/config_test.go`，模板照抄 `TestGateConfigDefaultAndOverride`）

```go
func TestFusionConfigDefaultAndOverride(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	// 全缺省：fusion=rrf、rrf_k=60
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Fusion != "rrf" || cfg.Retrieve.RrfK != 60 {
		t.Fatalf("unexpected defaults %+v", cfg.Retrieve)
	}
	// 全局覆盖，项目缺键继承
	if err := os.WriteFile(global, []byte("[retrieve]\nfusion = \"weighted\"\nrrf_k = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Fusion != "weighted" || cfg.Retrieve.RrfK != 30 {
		t.Fatalf("global override failed %+v", cfg.Retrieve)
	}
	// 项目覆盖回 rrf
	if err := os.WriteFile(project, []byte("[retrieve]\nfusion = \"rrf\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Fusion != "rrf" || cfg.Retrieve.RrfK != 30 {
		t.Fatalf("project override failed %+v", cfg.Retrieve)
	}
	// 项目清掉 → 重新继承全局
	if err := os.WriteFile(project, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Fusion != "weighted" {
		t.Fatalf("project 缺键应继承全局 %+v", cfg.Retrieve)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestFusionConfig -v`
Expected: FAIL（`cfg.Retrieve.Fusion undefined`）

- [ ] **Step 3: 实现**

`internal/config/config.go` Retrieve 结构体 `MinScore` 字段之后、`Gate` 字段之前加：

```go
	// Fusion 是融合方式：rrf（默认，Reciprocal Rank Fusion，只看各准入通道内
	// 名次不看分数——换 embedding 模型后余弦分布漂移不影响平衡）| weighted
	//（旧行为回滚档：score = α·归一BM25 + β·余弦）。准入逻辑两模式完全一致；
	// Alpha/Beta 仅 weighted 生效。缺省/非法值一律按 rrf（fail-open）。
	Fusion string `toml:"fusion"`
	// RrfK 是 RRF 名次平滑常数（默认 60，Zep 同款惯例）；仅 rrf 模式生效，<=0 按 60。
	RrfK int `toml:"rrf_k"`
```

`Default()` 的 Retrieve 字面量改为：

```go
		Retrieve:   Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 2, MinScore: 0.5, MinGap: 0.25, Fusion: "rrf", RrfK: 60, Gate: RetrieveGate{Enabled: true}},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS（含既有测试全部绿）

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): retrieve.fusion/rrf_k 配置（默认 rrf，weighted 回滚档）"
```

---

### Task 2: queryAll RRF 融合重构

**Files:**
- Modify: `internal/index/query.go`（Query doc 注释 :99-106、queryAll :142-272）
- Test: `internal/index/query_rrf_test.go`（新建）

**Interfaces:**
- Consumes: `config.Retrieve.Fusion` / `RrfK`（Task 1）
- Produces:
  - `func applyRRF(hits map[string]*Hit, kwNorms, cosScores map[string]float64, k int)`（包内私有）
  - 行为契约：fusion=rrf（含缺省/非法值）时 `score(h) = Σ_channel 1/(rrf_k + rank_c)`（rank 从 1 起，通道内按信号降序、同分文件名升序排名）；双通道同时准入的 hit 两项相加排前；weighted 模式逐行保持旧行为（含 scoreFloor 保护）

**背景（现状代码精要，动手前仍应 Read query.go 核实）：** 关键词通道循环（:154-187）内联写 `h.Score = cfg.Alpha * kw / (kw + 6)`；语义通道（:189-257）双通道命中 `h.Score += cfg.Beta * c.cos`（含 scoreFloor 保护 :232-234）、语义单通道准入 `c.h.Score = cfg.Beta * c.cos`（:235-238）；尾部 :259-271 过滤 `h.Score > 0` 后按总分降序/标题升序排序。两条通道扫描与准入判定全部保留，只改"分数从哪来"。

- [ ] **Step 1: 写失败测试**（新建 `internal/index/query_rrf_test.go`；夹具写法照抄 `index_test.go` 的 `writeEntryFile`/`Open`/`db.Sync(kdir, nil)` + 直接 SQL 插 vectors 的先例 index_test.go:289-296，`encodeVector` 包内私有可用）

```go
package index

import (
	"path/filepath"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// rrfFixture 两条目：a.md 关键词命中"苹果"，b.md 无关键词命中；
// 向量 b 与查询同向（cos 1.0，语义 rank 1），a 次之（cos 0.8，语义 rank 2）。
func rrfFixture(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	kdir := filepath.Join(dir, "knowledge")
	writeEntryFile(t, kdir, "a.md", "---\ntitle: 苹果条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n苹果 香蕉 水果。\n")
	writeEntryFile(t, kdir, "b.md", "---\ntitle: 无关条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n完全无关的内容。\n")
	db, err := Open(filepath.Join(dir, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec("INSERT INTO vectors(filename,dim,blob) VALUES('a.md',2,?)", encodeVector([]float32{0.8, 0.6})); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec("INSERT INTO vectors(filename,dim,blob) VALUES('b.md',2,?)", encodeVector([]float32{1, 0})); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestQueryRRFCrossValidation 双通道同时准入的 hit 两项相加，排单通道命中之前
//（交叉验证优先）：A = 1/(60+1) + 1/(60+2)，B = 1/(60+1)。
func TestQueryRRFCrossValidation(t *testing.T) {
	db := rrfFixture(t)
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5, Fusion: "rrf", RrfK: 60}
	hits, err := db.Query(retrieve.Terms("苹果"), []float32{1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Filename != "a.md" || hits[1].Filename != "b.md" {
		t.Fatalf("RRF 双通道命中应排第一: %+v", hits)
	}
}

// TestQueryRRFDefaultFusion Fusion 零值（测试字面量/老配置缺键）按 rrf 处理。
func TestQueryRRFDefaultFusion(t *testing.T) {
	db := rrfFixture(t)
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5} // Fusion 零值
	hits, err := db.Query(retrieve.Terms("苹果"), []float32{1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Filename != "a.md" {
		t.Fatalf("Fusion 缺省应按 rrf: %+v", hits)
	}
}

// TestQueryRRFSingleChannelOrder 单通道准入时 RRF 名次序 = 通道内排序：
// 纯关键词（queryVec nil）下 RRF 与 weighted 同序。
func TestQueryRRFSingleChannelOrder(t *testing.T) {
	db := rrfFixture(t)
	rrfCfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5, Fusion: "rrf", RrfK: 60}
	wCfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5, Fusion: "weighted"}
	rrfHits, err := db.Query(retrieve.Terms("苹果 香蕉"), nil, rrfCfg)
	if err != nil {
		t.Fatal(err)
	}
	wHits, err := db.Query(retrieve.Terms("苹果 香蕉"), nil, wCfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rrfHits) == 0 || len(rrfHits) != len(wHits) {
		t.Fatalf("单通道命中数不一致: rrf=%+v weighted=%+v", rrfHits, wHits)
	}
	for i := range rrfHits {
		if rrfHits[i].Filename != wHits[i].Filename {
			t.Fatalf("单通道 RRF 与 weighted 应同序: rrf=%+v weighted=%+v", rrfHits, wHits)
		}
	}
}

// TestQueryWeightedNegativeCosFloor weighted 回滚档保留 scoreFloor 保护：
// 关键词准入不可被强负余弦否决（RRF 模式下负余弦本就不进名次表，无需保护——
// 由 TestQueryRRFNegativeCos 覆盖对应语义）。
func TestQueryWeightedNegativeCosFloor(t *testing.T) {
	db := rrfFixture(t)
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5, Fusion: "weighted"}
	hits, err := db.Query(retrieve.Terms("苹果"), []float32{-1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Filename != "a.md" {
		t.Fatalf("weighted: 关键词命中须扛住强负余弦: %+v", hits)
	}
}

// TestQueryRRFNegativeCos RRF 下负余弦不进语义名次表、也不否决关键词命中。
func TestQueryRRFNegativeCos(t *testing.T) {
	db := rrfFixture(t)
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5, Fusion: "rrf", RrfK: 60}
	hits, err := db.Query(retrieve.Terms("苹果"), []float32{-1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Filename != "a.md" {
		t.Fatalf("RRF: 负余弦不应产生语义命中，关键词命中保留: %+v", hits)
	}
}
```

注意：fixture 只有 2 条目 → `MinScoreFloor` n<10 关闭阈值（floor=0）→ `SemanticFloor` floor=0 返回 0 → 所有 cos>0 均语义准入，名次构造完全由向量决定，测试确定性不依赖 BM25 数值。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/index/ -run 'TestQueryRRF|TestQueryWeighted' -v`
Expected: FAIL（Fusion/RrfK 字段未接入 queryAll——TestQueryRRFCrossValidation 等排序断言失败；若 Task 1 未合入则为编译失败，同样符合 RED）

- [ ] **Step 3: 实现**

`internal/index/query.go` 三处改动：

**① Query doc 注释（:99-106）改为：**

```go
// Query 混合检索：准入按通道独立判定——关键词通道需归一 BM25 分（未乘 α）≥
// floor，语义通道需余弦 ≥ SemanticFloor(cos 分布, floor)（模型无关相对门槛），
// 满足其一即可注入。融合（只用于排序）默认 RRF：score = Σ_channel 1/(rrf_k+rank)，
// 只看各准入通道内名次不看分数（换 embedding 模型不影响平衡）；fusion="weighted"
// 回滚旧行为 score = α·归一BM25 + β·余弦。
```

**② queryAll 头部（`hits := map[string]*Hit{}` 之后）加信号记录 map 与 fusion 解析：**

```go
	hits := map[string]*Hit{}
	// 各通道准入集合的原始信号（kwNorm / cos），供 RRF 排名次；weighted 模式
	// 不用但记录成本可忽略，准入路径两模式共用一份代码。
	kwNorms := map[string]float64{}
	cosScores := map[string]float64{}
	// 融合方式：缺省/非法值一律 rrf（fail-open）；weighted 为旧行为回滚档。
	fusion := cfg.Fusion
	if fusion != "weighted" {
		fusion = "rrf"
	}
```

**③ 关键词循环内**（原 `h.Score = cfg.Alpha * kw / (kw + 6)` 一行，:179 区域）改为：

```go
			kwNorm := kw / (kw + 6)
			h.Score = cfg.Alpha * kwNorm
			kwNorms[h.Filename] = kwNorm
			hits[h.Filename] = &h
```

（准入判定 `if floor > 0 && kw/(kw+6) < floor { continue }` 原样保留。）

**④ 语义双通道分支**（:229-234 区域）加 cosScores 记录——只有**语义也准入**（cos>0 且过 semFloor）的才算双通道：

```go
		for _, c := range cands {
			if h, ok := hits[c.h.Filename]; ok {
				h.Score += cfg.Beta * c.cos
				if h.Score <= 0 {
					h.Score = scoreFloor
				}
				if c.cos > 0 && (semFloor == 0 || c.cos >= semFloor) {
					cosScores[c.h.Filename] = c.cos
				}
			} else if c.cos > 0 && (semFloor == 0 || c.cos >= semFloor) {
				c.h.Score = cfg.Beta * c.cos
				hits[c.h.Filename] = &c.h
				cosScores[c.h.Filename] = c.cos
				semAdmitted = true
			}
		}
```

**⑤ 尾部过滤排序之前**（`out := make([]Hit, 0, len(hits))` 之前）加 RRF 重算 + applyRRF 函数本体：

```go
	if fusion == "rrf" {
		applyRRF(hits, kwNorms, cosScores, cfg.RrfK)
	}
```

```go
// applyRRF 用 RRF（Reciprocal Rank Fusion）重算总分：只看各准入通道内的名次，
// score(h) = Σ_channel 1/(k + rank_c)，rank 从 1 起；通道内按信号降序、同分按
// 文件名升序排名（确定性）。双通道同时准入的 hit 两项相加，自然排在单通道命中
// 之前（交叉验证优先）。k<=0 按 60。负余弦条目不进语义名次表（准入段已过滤），
// 无需 scoreFloor 保护——该保护仅属 weighted 模式。
func applyRRF(hits map[string]*Hit, kwNorms, cosScores map[string]float64, k int) {
	if k <= 0 {
		k = 60
	}
	ranks := func(scores map[string]float64) map[string]int {
		names := make([]string, 0, len(scores))
		for n := range scores {
			names = append(names, n)
		}
		sort.Slice(names, func(i, j int) bool {
			if scores[names[i]] != scores[names[j]] {
				return scores[names[i]] > scores[names[j]]
			}
			return names[i] < names[j]
		})
		out := make(map[string]int, len(names))
		for i, n := range names {
			out[n] = i + 1
		}
		return out
	}
	kwRanks, cosRanks := ranks(kwNorms), ranks(cosScores)
	for name, h := range hits {
		score := 0.0
		if r, ok := kwRanks[name]; ok {
			score += 1 / (float64(k) + float64(r))
		}
		if r, ok := cosRanks[name]; ok {
			score += 1 / (float64(k) + float64(r))
		}
		h.Score = score
	}
}
```

不改的部分（明确）：准入阈值计算、SemanticFloor、QueryInfo 诊断、尾部 `h.Score > 0` 过滤（RRF 分恒正，天然通过）与排序 tie-break、QueryEx/QueryExBranch/truncateHits/FilterHitsByBranch。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ -v`
Expected: PASS（新增 5 个 + 既有全部绿——特别确认 `TestQueryChannelAdmission` / `TestQueryExSemanticRejected` / `TestQueryExBranchFiltersBeforeTopN` / `TestQueryNegativeCosKeepsKeywordHit` 在默认 rrf 下仍绿）

- [ ] **Step 5: Commit**

```bash
git add internal/index/query.go internal/index/query_rrf_test.go
git commit -m "feat(index): RRF 融合替换加权融合（fusion=rrf 默认，weighted 回滚档保留）"
```

---

### Task 3: alpha/beta 忽略提示 + ok search 分数格式

**Files:**
- Modify: `internal/hook/core.go`（门控分支之后、检索调用之前，:128-140 区域）
- Modify: `internal/cli/cli.go`（ok search 输出 :351 区域 + 提示）
- Test: `internal/hook/core_test.go`

**Interfaces:**
- Consumes: `config.Retrieve.Fusion`（Task 1）
- Produces: 无新符号；行为契约 = rrf 模式且 alpha/beta 非默认时，hook 记 ok.log 一行、ok search 打 stderr 一行

- [ ] **Step 1: 写失败测试**（追加到 `internal/hook/core_test.go`；ok.log 读取先例：logErr 写 `registry.Home()/ok.log`，core_test/hook_test 的夹具已隔离 OK_HOME，直接读该文件）

```go
// TestInjectRRFIgnoresAlphaBetaHint rrf 模式下配置了非默认 alpha/beta 时，
// 注入流程应记一行 ok.log 提示被忽略（仅 weighted 生效）。
func TestInjectRRFIgnoresAlphaBetaHint(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	// rrf（缺省）+ 非默认 alpha → 应提示被忽略
	cfg := "[retrieve]\nalpha = 2.0\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = InjectForPrompt(pc, "s-fusion", projDir, "RetrievalQuirk 是什么")
	logData, err := os.ReadFile(filepath.Join(registry.Home(), "ok.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "alpha/beta") {
		t.Errorf("rrf 模式下非默认 alpha 应记 ok.log 提示，got: %q", logData)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hook/ -run TestInjectRRFIgnoresAlphaBetaHint -v`
Expected: FAIL（ok.log 无提示行）

- [ ] **Step 3: 实现**

`internal/hook/core.go`：在 `hits` 检索调用之前（门控 else 分支内、`if client != nil` 之前）加：

```go
		if pc.Config.Retrieve.Fusion != "weighted" &&
			(pc.Config.Retrieve.Alpha != 1 || pc.Config.Retrieve.Beta != 1) {
			// rrf 模式下 alpha/beta 不生效（仅 weighted 回滚档读取）——每 prompt
			// 一行属自限性提示：用户删掉配置即消失。GUI 日志页可按"fusion"过滤。
			logErr("prompt fusion: rrf 模式下 alpha/beta 配置被忽略（仅 weighted 生效），建议从 config.toml 移除")
		}
```

`internal/cli/cli.go` ok search（:341 区域，`db.QueryEx` 调用前后）加同款 stderr 提示：

```go
	if cfg.Retrieve.Fusion != "weighted" && (cfg.Retrieve.Alpha != 1 || cfg.Retrieve.Beta != 1) {
		fmt.Fprintf(stderr, "[OpenKnowledge] rrf 模式下 alpha/beta 配置被忽略（仅 weighted 生效）\n")
	}
```

（`Search(args, stdout, stderr io.Writer)` 的输出一律走注入的 `stderr` 参数——不要直写 `os.Stderr`（否则测试无法捕获）；变量名以实际代码为准——先 Read 确认 cfg 的持有者；fmt 已 import。）

同文件输出格式（:351）：`"%.2f\t%s (%s)\n"` 改为 `"%.4f\t%s (%s)\n"`——RRF 分值域 ~0.016-0.033，`%.2f` 下全部糊成 0.02/0.03 无法区分。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/hook/ ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hook/core.go internal/hook/core_test.go internal/cli/cli.go
git commit -m "feat(hook,cli): rrf 模式非默认 alpha/beta 记提示；ok search 分数改 %.4f"
```

---

### Task 4: 文档同步（ARCHITECTURE + 双 README）

**Files:**
- Modify: `docs/ARCHITECTURE.md`（:239、:799、:844-851、:862-866、:881-885、:976-977 区域）
- Modify: `README.md`（:240 区域）、`README_EN.md`（:241 区域）

**Interfaces:**
- Consumes: Task 1-3 的最终行为
- Produces: 无代码；文档与实现一致

行号是探索时的快照，动手前用 Grep 定位实际文本再改（锚点文本如下）。

- [ ] **Step 1: `docs/ARCHITECTURE.md` 六处**

1. `:239` 附近 "查询为 `score = α·归一BM25 + β·余弦` 的混合打分" → 改为：

   > 查询为准入按通道独立判定 + 融合排序：融合默认 RRF（`score = Σ 1/(rrf_k+rank)`，只看名次不看分数），`fusion = "weighted"` 回滚旧加权（`α·归一BM25 + β·余弦`）

2. `:799`（17.1 架构图内）"score = α·kw + β·cos 只排序；准入任一通道达标即可 → top_n → 摘要注入" → 改为：

   > 准入任一通道达标即可 → RRF 名次融合（weighted 可回滚）只排序 → top_n → 摘要注入

3. `:844-851`（17.3）归一化说明段落（"用 `kw/(kw+6)` 压缩到 [0,1)——与余弦同量纲，α/β 才有真实意义"）→ 段末追加：

   > 注：归一化仍用于关键词通道准入判定；v2.17.0 起融合默认改 RRF（只看名次），α/β 仅 `fusion = "weighted"` 回滚档生效。

4. `:862-866`（17.5 节，标题含"准入与排序"）代码块 `score = α · normBM25 + β · cosine （α、β 默认 1.0，只用于排序）` → 改为：

   ```
   融合（只用于排序）：rrf（默认）score = Σ_channel 1/(rrf_k + rank)，rrf_k 默认 60
                     weighted（回滚档）score = α · normBM25 + β · cosine
   ```

5. `:881-885` 过滤排序严格顺序描述——确认"总分降序（同分标题升序）→ 截 top_n"表述仍准确（未变），仅在有"α/β"措辞处同步。

6. `:976-977`（配置表）`retrieve.alpha` / `retrieve.beta` 两行的说明列末尾加"（仅 fusion=weighted 生效）"，并在其后插两行：

   ```
   | retrieve.fusion | rrf | 融合方式：rrf（默认，按名次融合，模型无关）或 weighted（旧 α/β 加权回滚档） |
   | retrieve.rrf_k | 60 | RRF 名次平滑常数（仅 fusion=rrf 生效） |
   ```

- [ ] **Step 2: 双 README**

`README.md:240` 附近 "两路分数加权混合（α/β 可调）只用于排序" → 改为：

> 两路准入独立判定，融合默认按名次（RRF），模型无关无需调参；`fusion = "weighted"` 可回滚旧 α/β 加权

`README_EN.md:241` 附近 "the blended score (α/β tunable) only ranks" → 改为：

> admission is per-channel; fusion defaults to RRF (rank-based, model-agnostic); `fusion = "weighted"` restores the legacy α/β blend

- [ ] **Step 3: 核对无遗漏**

Run: `grep -n "α" docs/ARCHITECTURE.md README.md README_EN.md | grep -v "weighted\|回滚"`
Expected: 无遗留的"α/β 是当前唯一融合方式"表述（历史 changelog 与 specs/plans 档案不改）

- [ ] **Step 4: Commit**

```bash
git add docs/ARCHITECTURE.md README.md README_EN.md
git commit -m "docs: RRF 融合文档同步（ARCHITECTURE + 双 README）"
```

---

### Task 5: 双模式标定对比 + changelog

**Files:**
- Create: `docs/changelogs/2026-08-16-rrf-fusion.md`

**Interfaces:**
- Consumes: Task 1-4 全部
- Produces: 发布素材；标定结论（无回归证据或回滚决策）

- [ ] **Step 1: 真实知识库双模式对比**

用本项目的真实知识库（`~/.openknowledge/projects/OpenKnowledge`）跑 `ok search`，对比两模式的注入集合与排序。复用 2026-08-15 标定过的场景（该轮实测查询，见 docs/changelogs/2026-08-15-retrieve-channel-gating.md）：

1. 构建：`go build -o dist/ok.exe ./cmd/ok`
2. 默认（rrf）跑一遍，每个查询记录 top_n 结果：

   ```bash
   ./dist/ok.exe search "我已经验证了A 没啥问题，你也可以看看 deepseek apiKey"
   ./dist/ok.exe search "多-Agent支持 十个适配器"
   ./dist/ok.exe search "帮我写一个 python 爬虫"
   ./dist/ok.exe search "构建命令是什么"
   ./dist/ok.exe search "windows 权限位 chmod"
   ```

3. 项目 config.toml 临时加 `[retrieve]\nfusion = "weighted"`，同组查询重跑
4. 对比：准入集合预期**完全一致**（准入逻辑未动），差异只在排序；若某场景排序明显退化（真实相关被挤出 top_n），记录并评估是否调 rrf_k 或回滚
5. 撤掉临时配置

判定标准（spec §9）：无场景明显退化 → 通过；轻微次序变化可接受（bench 差异预期仅在排序）。

- [ ] **Step 2: 写 changelog**（格式对照 docs/changelogs/2026-08-15-retrieve-channel-gating.md；把 Step 1 的对比结论写进"验证"段）

```markdown
# 2026-08-16 RRF 融合：按名次融合替代 α/β 手工加权（fusion 配置 + weighted 回滚档）

- **问题**：融合分 `score = α·kw/(kw+6) + β·cos` 依赖手工归一化——归一化常数 +6 与
  α/β 是分数尺度桥接，换 embedding 模型后余弦分布漂移（bge-m3 噪声基线 0.52 vs
  qwen3 0.26），α/β 平衡即失效。
- **修复**：
  - 融合改 RRF（Reciprocal Rank Fusion，Zep 同款）：对已准入集合
    `score = Σ_channel 1/(rrf_k + rank)`，只看名次不看分数，rrf_k 默认 60；
    双通道同时准入的 hit 两项相加，自然排在单通道命中之前（交叉验证优先）；
  - **准入完全不变**：关键词通道仍按未乘 α 的归一 BM25 ≥ MinScoreFloor 准入，
    语义通道仍按 SemanticFloor（模型无关相对门槛）准入；QueryInfo 诊断保留；
    tie-break（总分降序、标题升序）与 top_n/分支过滤位置不变；
  - 配置 `[retrieve] fusion = "rrf"（默认）| "weighted"（旧行为回滚档）`、
    `rrf_k = 60`；非法值按 rrf（fail-open）；scoreFloor=1e-6 保护仅属
    weighted（RRF 下负余弦本就不进语义名次表）；
  - alpha/beta 仅 weighted 生效：rrf 模式下配置了非默认值时 hook 记 ok.log、
    ok search 打 stderr 提示被忽略；ok search 分数输出改 %.4f（RRF 分值域
    ~0.016-0.033，%.2f 下无法区分）。
- **验证**：（Step 1 的真实库双模式对比结论填这里：逐场景列出注入集合是否一致、
  排序差异是否可接受）
- **测试**：`TestQueryRRFCrossValidation`（双通道交叉项排前）/
  `TestQueryRRFDefaultFusion`（零值按 rrf）/`TestQueryRRFSingleChannelOrder`
  （单通道与 weighted 同序）/`TestQueryWeightedNegativeCosFloor`（回滚档
  scoreFloor 保护）/`TestQueryRRFNegativeCos`（负余弦不进名次表）/
  `TestFusionConfigDefaultAndOverride`（配置四态）/
  `TestInjectRRFIgnoresAlphaBetaHint`（忽略提示）；既有准入/诊断/分支过滤
  测试在默认 rrf 下全绿；全仓 go test ./... 绿。
- **升级注意**：升级后默认排序行为变化（RRF）；要旧行为显式
  `fusion = "weighted"`。GUI 搜索为纯关键词单通道，次序不变仅分数值变化。
```

- [ ] **Step 3: 全仓回归**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add docs/changelogs/2026-08-16-rrf-fusion.md
git commit -m "docs: RRF 融合 changelog（含双模式标定结论）"
```
