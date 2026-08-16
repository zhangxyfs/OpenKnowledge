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
