package index

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

const e1 = `---
title: Git 提交规范
type: note
tags: [git]
summary: 提交信息格式
---

使用 Conventional Commits。
`

const e2 = `---
title: 变更日志强制规则
type: rule
mandatory: true
summary: 改代码必须写日志
---

改完代码先写日志。
`

const e3 = `---
title: 构建命令速查
type: reference
tags: [build]
summary: 常用构建命令
---

go build ./... 即可。
`

func writeEntryFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeEmbedder 返回确定性向量，避免真实网络。
type fakeEmbedder struct{}

func (fakeEmbedder) ModelIdentity() string { return "" }

func (fakeEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return fakeEmbedder{}.EmbedDocument(ctx, text)
}

func (fakeEmbedder) EmbedDocument(_ context.Context, text string) ([]float32, error) {
	// 含 "git" 的文本给 [1,0]，其余给 [0,1]
	if strings.Contains(strings.ToLower(text), "git") {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func (fakeEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := fakeEmbedder{}.EmbedDocument(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func setupDB(t *testing.T) (*DB, string) {
	t.Helper()
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	writeEntryFile(t, kdir, "rule.md", e2)
	writeEntryFile(t, kdir, "build.md", e3)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, kdir
}

func TestSyncAndCount(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	n, err := db.Count()
	if err != nil || n != 3 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	// INDEX.md 已重建
	data, err := os.ReadFile(filepath.Join(filepath.Dir(kdir), "INDEX.md"))
	if err != nil || !strings.Contains(string(data), "Git 提交规范") {
		t.Fatalf("INDEX.md not rebuilt: %v %q", err, data)
	}
	// 幂等：mtime 未变时再次 Sync 不报错且数量不变
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	// 删除一个文件后同步应删除对应行
	if err := os.Remove(filepath.Join(kdir, "build.md")); err != nil {
		t.Fatal(err)
	}
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Count(); n != 2 {
		t.Fatalf("after delete count=%d", n)
	}
}

func TestQueryKeywordAndHybrid(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 3}
	// 关键词命中 git 条目
	hits, err := db.Query(retrieve.Terms("git 提交规范"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("keyword query wrong: %+v", hits)
	}
	if hits[0].Body == "" {
		t.Fatal("hit should carry body for injection")
	}
	// mandatory 条目不出现在检索结果
	for _, h := range hits {
		if h.Title == "变更日志强制规则" {
			t.Fatal("mandatory entry must be excluded from query")
		}
	}
	// 语义通道：queryVec=[1,0] 与 git 条目向量同向 → 即使关键词不命中也能召回
	hits, err = db.Query(retrieve.Terms("zzz"), []float32{1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("semantic recall wrong: %+v", hits)
	}
}

func TestMinScoreFloor(t *testing.T) {
	cases := []struct {
		minScore float64
		n        int
		want     float64
	}{
		{0.5, 0, 0}, {0.5, 2, 0}, {0.5, 9, 0}, {0.5, 10, 0},
		{0.5, 20, 0.25}, {0.5, 25, 0.375}, {0.5, 30, 0.5},
		{0.5, 40, 0.5}, {0.5, 200, 0.5},
		{0, 40, 0}, {-1, 40, 0}, {0.9, 20, 0.45},
	}
	for _, c := range cases {
		if got := MinScoreFloor(c.minScore, c.n); got != c.want {
			t.Errorf("MinScoreFloor(%v, %d) = %v, want %v", c.minScore, c.n, got, c.want)
		}
	}
}

// TestSemanticFloor 用 2026-08-15 实测的 查询×条目 余弦分布（bge-m3 硅基流动
// vs qwen3 本地）验证模型无关门槛：跨域噪声与同域闲聊无显著头部 → 语义通道
// 全拒；真相关查询有显著头部 → 门槛落在头部与背景之间。floor=0.5。
func TestSemanticFloor(t *testing.T) {
	bgeUser := []float64{0.5800, 0.5788, 0.5672, 0.5481, 0.5378, 0.5205, 0.5160, 0.5131, 0.4561}
	qwenUser := []float64{0.4997, 0.4733, 0.4540, 0.4216, 0.4005, 0.3768, 0.3718, 0.3428, 0.2564}
	qwenRel := []float64{0.7254, 0.5396, 0.5291, 0.4080, 0.3699, 0.3572, 0.3313, 0.2708, 0.2410}
	bgeRel := []float64{0.6668, 0.5831, 0.5553, 0.5089, 0.4550, 0.4451, 0.4232, 0.4175, 0.4105}
	qwenPy := []float64{0.2584, 0.2493, 0.1969, 0.1905, 0.1901, 0.1877, 0.1816, 0.1746, 0.0610}
	bgePy := []float64{0.5233, 0.4860, 0.4830, 0.4692, 0.4683, 0.4657, 0.4638, 0.4220, 0.4044}
	cases := []struct {
		name   string
		coses  []float64
		floor  float64
		minGap float64
		want   float64
	}{
		// 同域闲聊（用户原话）：无显著头部 → 语义通道全拒
		{"bge-m3 用户原话", bgeUser, 0.5, 0.25, math.Inf(1)},
		{"qwen3 用户原话", qwenUser, 0.5, 0.25, math.Inf(1)},
		// 跨域无关：无显著头部（qwen3 相对 gap 0.26 过判定但绝对下限兜底）
		{"bge-m3 python爬虫", bgePy, 0.5, 0.25, math.Inf(1)},
		{"qwen3 python爬虫", qwenPy, 0.5, 0.25, 0.5},
		// 真相关：门槛 = median + 0.5·(max-median)，高于绝对下限
		{"qwen3 多agent相关", qwenRel, 0.5, 0.25, 0.5477},
		{"bge-m3 多agent相关", bgeRel, 0.5, 0.25, 0.5609},
		// min_gap 可配：调高更严（qwen3 python 的 0.26 头部也被拒）
		{"minGap=0.3 收紧", qwenPy, 0.5, 0.3, math.Inf(1)},
		// minGap<=0 关闭 gap 判定：仅绝对下限（bgePy 门槛落到 floor）
		{"minGap=0 关闭gap判定", bgePy, 0.5, 0, 0.5},
		// 低对比度模型自救：调低 min_gap + 调低 min_score 后门槛落在头部内
		{"低对比度自救 minGap=0.15", qwenUser, 0.4, 0.15, 0.4501},
		// floor<=0 关闭（旧语义）；样本不足退回绝对下限
		{"floor关闭", []float64{0.9}, 0, 0.25, 0},
		{"样本不足", []float64{0.9, 0.1}, 0.5, 0.25, 0.5},
	}
	for _, c := range cases {
		got := SemanticFloor(c.coses, c.floor, c.minGap)
		if math.Abs(got-c.want) > 1e-3 {
			t.Errorf("%s: SemanticFloor = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestQueryMinScore(t *testing.T) {
	// 12 条目（1 相关 + 11 噪音）：n=12 → 阈值 = MinScore*(12-10)/30 = MinScore/15
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	for i := 0; i < 11; i++ {
		writeEntryFile(t, kdir, fmt.Sprintf("noise%d.md", i),
			fmt.Sprintf("---\ntitle: 噪音条目%d\ntype: note\nsummary: 噪音\n---\n\n噪音正文%d\n", i, i))
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	// 默认阈值 0.5 → floor = MinScoreFloor(0.5, 12) = 0.05：真实命中保留
	got, err := db.Query(retrieve.Terms("git 提交规范"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Title != "Git 提交规范" {
		t.Fatalf("related hit lost under default threshold: %+v", got)
	}
	floor := MinScoreFloor(0.5, 12)
	for _, h := range got {
		if h.Score < floor {
			t.Fatalf("hit below floor survived: %+v (floor=%v)", h, floor)
		}
	}
	// 大 MinScore → floor = 10：任何归一 BM25 分（<1）都不达标 → 空
	got, err = db.Query(retrieve.Terms("git 提交规范"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 100})
	if err != nil || len(got) != 0 {
		t.Fatalf("huge MinScore should yield nothing: %+v err=%v", got, err)
	}
	// MinScore <= 0 → 显式关闭阈值
	got, err = db.Query(retrieve.Terms("git 提交规范"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: -1})
	if err != nil || len(got) == 0 {
		t.Fatalf("non-positive MinScore disables threshold: %+v err=%v", got, err)
	}
}

// TestQueryChannelAdmission 准入按通道独立判定：关键词强命中即使向量正交仍注入；
// 语义弱信号（cos 低于 floor）不得凑数，语义强信号可单独准入。
func TestQueryChannelAdmission(t *testing.T) {
	// 12 条目（1 git + 11 噪音）：n=12 → floor = MinScoreFloor(0.5,12) = 0.05
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	for i := 0; i < 11; i++ {
		writeEntryFile(t, kdir, fmt.Sprintf("noise%d.md", i),
			fmt.Sprintf("---\ntitle: 噪音条目%d\ntype: note\nsummary: 噪音\n---\n\n噪音正文%d\n", i, i))
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	// 手工铺 3D 向量：git=[1,0,0]，噪音=[0,1,0]（Sync(nil) 只建关键词索引）
	if _, err := db.sql.Exec("INSERT INTO vectors(filename,dim,blob) VALUES('git.md',3,?)", encodeVector([]float32{1, 0, 0})); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		if _, err := db.sql.Exec(fmt.Sprintf("INSERT INTO vectors(filename,dim,blob) VALUES('noise%d.md',3,?)", i), encodeVector([]float32{0, 1, 0})); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5}
	// 1) 语义弱信号：cos(git)=0.03、cos(噪音)=0，均 < floor 0.05 且无关键词命中 → 空
	got, err := db.Query(retrieve.Terms("zzz"), []float32{0.03, 0, 1}, cfg)
	if err != nil || len(got) != 0 {
		t.Fatalf("weak semantic must not admit: %+v err=%v", got, err)
	}
	// 2) 语义强信号（cos 1 ≥ floor）：无关键词命中也准入
	got, err = db.Query(retrieve.Terms("zzz"), []float32{1, 0, 0}, cfg)
	if err != nil || len(got) == 0 || got[0].Title != "Git 提交规范" {
		t.Fatalf("strong semantic should admit: %+v err=%v", got, err)
	}
	// 3) 关键词强命中即使语义向量正交（cos 0）仍准入
	got, err = db.Query(retrieve.Terms("git 提交规范"), []float32{0, 0, 1}, cfg)
	if err != nil || len(got) == 0 || got[0].Title != "Git 提交规范" {
		t.Fatalf("strong keyword should admit despite orthogonal vec: %+v err=%v", got, err)
	}
}

// TestQueryExSemanticRejected 诊断信息：语义样本 ≥3 且全部被拒（分布无头部）时
// SemanticRejected=true 并携带分布统计；有准入或样本不足时不置位。
func TestQueryExSemanticRejected(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	for i := 0; i < 11; i++ {
		writeEntryFile(t, kdir, fmt.Sprintf("noise%d.md", i),
			fmt.Sprintf("---\ntitle: 噪音条目%d\ntype: note\nsummary: 噪音\n---\n\n噪音正文%d\n", i, i))
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec("INSERT INTO vectors(filename,dim,blob) VALUES('git.md',3,?)", encodeVector([]float32{1, 0, 0})); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		if _, err := db.sql.Exec(fmt.Sprintf("INSERT INTO vectors(filename,dim,blob) VALUES('noise%d.md',3,?)", i), encodeVector([]float32{0, 1, 0})); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5, MinGap: 0.25}
	// 查询向量与所有条目等距（cos 全 = 0.707）：分布无头部 → 全拒 + 诊断
	_, info, err := db.QueryEx(retrieve.Terms("zzz"), []float32{0.5, 0.5, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !info.SemanticRejected || info.Coses != 12 || math.Abs(info.RelGap) > 1e-6 {
		t.Fatalf("expected semantic rejection with stats: %+v", info)
	}
	// 样本不足（仅 1 个正 cos）→ 不置位（语义仍按绝对下限准入）
	_, info, err = db.QueryEx(retrieve.Terms("zzz"), []float32{1, 0, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if info.SemanticRejected {
		t.Fatalf("single-sample should not flag rejection: %+v", info)
	}
}

func TestMandatory(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Mandatory()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "变更日志强制规则" || rows[0].Body == "" {
		t.Fatalf("mandatory wrong: %+v", rows)
	}
}

func TestVectorsJSONMigration(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	// 造一个旧版 vectors.json（格式与 embed.VectorSet 相同）
	vj := `{"vectors":{"git.md":{"mod_time":1,"vector":[1,0]}}}`
	if err := os.WriteFile(filepath.Join(root, "vectors.json"), []byte(vj), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Stat(filepath.Join(root, "vectors.json.bak")); err != nil {
		t.Fatalf("vectors.json should be renamed to .bak: %v", err)
	}
}

func TestManyEntriesQuery(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	for i := 0; i < 2000; i++ {
		body := "---\ntitle: 噪音条目" + strings.Repeat("x", 1) + string(rune('a'+i%26)) +
			"\ntype: note\nsummary: 噪音\n---\n\n噪音正文\n"
		writeEntryFile(t, kdir, "noise"+strings.Repeat("a", i%7+1)+string(rune('a'+i%26))+".md", body)
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := db.Query(retrieve.Terms("git 提交"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("2k entries query wrong: %+v", hits)
	}
}

// 两个连接并发写同一库：无 busy_timeout 时第二个立即 SQLITE_BUSY；
// 有 busy_timeout 时等待第一个提交后成功。
func TestOpenBusyTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	db1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	tx, err := db1.sql.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO entries(filename,title,type) VALUES('a.md','A','note')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		done <- tx.Commit()
	}()
	start := time.Now()
	_, werr := db2.sql.Exec(`INSERT INTO entries(filename,title,type) VALUES('b.md','B','note')`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if werr != nil {
		t.Fatalf("concurrent write should wait for busy_timeout, got %v", werr)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("write did not wait — busy_timeout likely not applied")
	}
}
