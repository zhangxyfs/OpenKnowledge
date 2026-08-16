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
