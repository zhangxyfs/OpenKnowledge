package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// TestQueryRecencyDemotesStale 陈旧条目乘系数后在近似同分时让位：
// 两条 note 都命中"苹果"，单关键词通道 RRF 名次分 1/61 与 1/62（谁前谁后由
// BM25 决定，两种排位的分差都 < 0.85 翻转所需）——stale 条目（400 天，
// ≥ note stale 180）×0.85 后必排 fresh 条目之后；关闭 recency 则不上榜。
func TestQueryRecencyDemotesStale(t *testing.T) {
	dir := t.TempDir()
	kdir := filepath.Join(dir, "knowledge")
	writeEntryFile(t, kdir, "old.md", "---\ntitle: 苹果旧笔记\ntype: note\ntags: []\n---\n\n苹果 香蕉。\n")
	writeEntryFile(t, kdir, "new.md", "---\ntitle: 苹果新笔记\ntype: note\ntags: []\n---\n\n苹果 橘子。\n")
	old := time.Now().Add(-400 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(kdir, "old.md"), old, old); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(dir, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	rc := config.RetrieveRecency{Enabled: true, Floor: 0.85, Windows: config.RecencyWindows{Note: []int{60, 180}}}
	// 开启：new.md 在前，old.md 分数被乘 0.85
	on := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, Recency: rc}
	hits, info, err := db.QueryEx(retrieve.Terms("苹果"), nil, on)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("应命中 2 条: %+v", hits)
	}
	if hits[0].Filename != "new.md" || hits[1].Filename != "old.md" {
		t.Fatalf("陈旧条目应让位: %+v", hits)
	}
	if hits[1].Score >= hits[0].Score {
		t.Fatalf("old.md 应被乘 0.85: %+v", hits)
	}
	if hits[0].Mtime <= 0 || hits[1].Mtime <= 0 {
		t.Fatalf("Hit 应携带 Mtime: %+v", hits)
	}
	// 关闭：分数不乘系数、不上榜
	off := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5}
	_, infoOff, err := db.QueryEx(retrieve.Terms("苹果"), nil, off)
	if err != nil {
		t.Fatal(err)
	}
	if len(infoOff.RecencyShifted) != 0 {
		t.Fatalf("关闭时不应有时效观测: %+v", infoOff.RecencyShifted)
	}
	_ = info
}
