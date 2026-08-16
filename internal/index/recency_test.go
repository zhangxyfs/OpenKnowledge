package index

import (
	"math"
	"testing"

	"openknowledge/internal/config"
)

var recencyCfg = config.RetrieveRecency{Enabled: true, Floor: 0.85, Windows: config.RecencyWindows{
	Rule: []int{180, 730}, Pitfall: []int{90, 365}, Note: []int{60, 180}, Reference: []int{180, 730},
}}

const daySec int64 = 86400

func TestFactorBoundaries(t *testing.T) {
	const now int64 = 1_800_000_000
	cases := []struct {
		typ  string
		ageD int64
		want float64
	}{
		{"note", 0, 1.0},     // fresh 内
		{"note", 60, 1.0},    // fresh 边界（age<=fresh → 1.0）
		{"note", 120, 0.925}, // 线性中点：1-0.15*(120-60)/(180-60)
		{"note", 180, 0.85},  // stale 边界
		{"note", 9999, 0.85}, // 远超 stale
		{"pitfall", 90, 1.0},
		{"pitfall", 365, 0.85},
		{"rule", 180, 1.0},
		{"rule", 730, 0.85},
		{"reference", 400, 1.0 - 0.15*220.0/550.0}, // rule 线性段
		{"wiki", 9999, 1.0},                        // 未知类型不衰减
	}
	for _, c := range cases {
		got := Factor(c.typ, now-c.ageD*daySec, now, recencyCfg)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Factor(%s, age=%dd) = %v, want %v", c.typ, c.ageD, got, c.want)
		}
	}
}

func TestFactorFailOpen(t *testing.T) {
	const now int64 = 1_800_000_000
	old := now - 9999*daySec
	// mtime<=0
	if got := Factor("note", 0, now, recencyCfg); got != 1.0 {
		t.Errorf("mtime=0 应不衰减, got %v", got)
	}
	// floor 非法 → 按 0.85
	bad := recencyCfg
	bad.Floor = 0
	if got := Factor("note", old, now, bad); got != 0.85 {
		t.Errorf("floor=0 应按 0.85, got %v", got)
	}
	bad.Floor = 1.5
	if got := Factor("note", old, now, bad); got != 0.85 {
		t.Errorf("floor=1.5 应按 0.85, got %v", got)
	}
	// 窗口全零 / 长度不对 / stale<=fresh → 不衰减
	for _, w := range [][]int{{0, 0}, {60}, {60, 180, 1}, {180, 60}} {
		c := recencyCfg
		c.Windows.Note = w
		if got := Factor("note", old, now, c); got != 1.0 {
			t.Errorf("非法窗口 %v 应不衰减, got %v", w, got)
		}
	}
}

func TestApplyRecency(t *testing.T) {
	const now int64 = 1_800_000_000
	mk := func() map[string]*Hit {
		return map[string]*Hit{
			"old.md": {Filename: "old.md", Title: "旧", Type: "note", Score: 0.5, Mtime: now - 400*daySec},
			"new.md": {Filename: "new.md", Title: "新", Type: "note", Score: 0.4, Mtime: now},
		}
	}
	// 翻名次：old 0.5×0.85=0.425 仍 > new 0.4？不——0.425>0.4 不翻。
	// 调整：old 0.45×0.85=0.3825 < 0.4 → 翻
	hits := mk()
	hits["old.md"].Score = 0.45
	shifted := applyRecency(hits, now, recencyCfg)
	if hits["old.md"].Score < 0.38 || hits["old.md"].Score > 0.39 {
		t.Errorf("old.md 应乘 0.85: %v", hits["old.md"].Score)
	}
	if len(shifted) != 1 || shifted[0] != "old.md×0.85" {
		t.Errorf("old.md 名次应变差并上榜: %v", shifted)
	}
	// 不翻名次：old 领先够多 → 系数照乘但不上榜
	hits2 := mk()
	if got := applyRecency(hits2, now, recencyCfg); got != nil {
		t.Errorf("名次未变应返回 nil: %v", got)
	}
	if hits2["old.md"].Score != 0.5*0.85 {
		t.Errorf("系数仍应乘: %v", hits2["old.md"].Score)
	}
	// 关闭 → 不动
	hits3 := mk()
	off := recencyCfg
	off.Enabled = false
	if got := applyRecency(hits3, now, off); got != nil || hits3["old.md"].Score != 0.5 {
		t.Errorf("Enabled=false 不应动分数: %v %v", got, hits3["old.md"].Score)
	}
}
