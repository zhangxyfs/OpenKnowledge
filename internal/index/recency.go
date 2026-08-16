package index

import (
	"fmt"
	"sort"

	"openknowledge/internal/config"
)

const daySeconds int64 = 86400

// Factor 返回条目的时效系数：age ≤ fresh_days → 1.0；age ≥ stale_days → floor；
// 中间线性过渡。陈旧不等于无关——系数只在融合分之后乘（不参与准入），
// 让陈旧条目在近似同分时让位。
// 以下情形一律返回 1.0（fail-open 不衰减）：类型未知、窗口全零/长度不对/
// stale<=fresh、mtime<=0。floor 非法（<=0 或 >1）按默认 0.85。
func Factor(typ string, mtime, now int64, cfg config.RetrieveRecency) float64 {
	floor := cfg.Floor
	if floor <= 0 || floor > 1 {
		floor = 0.85
	}
	if mtime <= 0 {
		return 1.0
	}
	var w []int
	switch typ {
	case "rule":
		w = cfg.Windows.Rule
	case "pitfall":
		w = cfg.Windows.Pitfall
	case "note":
		w = cfg.Windows.Note
	case "reference":
		w = cfg.Windows.Reference
	default:
		return 1.0
	}
	if len(w) != 2 || w[1] <= w[0] || (w[0] <= 0 && w[1] <= 0) {
		return 1.0
	}
	age := (now - mtime) / daySeconds
	if age <= int64(w[0]) {
		return 1.0
	}
	if age >= int64(w[1]) {
		return floor
	}
	return 1.0 - (1.0-floor)*float64(age-int64(w[0]))/float64(w[1]-w[0])
}

// applyRecency 对准入集合逐条乘时效系数（就地改 Score），返回因系数名次变差
// 的条目（"filename×0.85"，按新名次排序），供 QueryInfo 观测。Enabled=false
// 或无条目受影响返回 nil。
func applyRecency(hits map[string]*Hit, now int64, cfg config.RetrieveRecency) []string {
	if !cfg.Enabled {
		return nil
	}
	byScore := func(hs []*Hit) {
		sort.Slice(hs, func(i, j int) bool {
			if hs[i].Score != hs[j].Score {
				return hs[i].Score > hs[j].Score
			}
			return hs[i].Title < hs[j].Title
		})
	}
	pre := make([]*Hit, 0, len(hits))
	for _, h := range hits {
		pre = append(pre, h)
	}
	byScore(pre)
	preRank := make(map[string]int, len(pre))
	for i, h := range pre {
		preRank[h.Filename] = i
	}
	demoted := map[string]float64{}
	for _, h := range hits {
		if f := Factor(h.Type, h.Mtime, now, cfg); f < 1 {
			h.Score *= f
			demoted[h.Filename] = f
		}
	}
	if len(demoted) == 0 {
		return nil
	}
	post := make([]*Hit, 0, len(hits))
	for _, h := range hits {
		post = append(post, h)
	}
	byScore(post)
	var shifted []string
	for i, h := range post {
		f, ok := demoted[h.Filename]
		if ok && i > preRank[h.Filename] {
			shifted = append(shifted, fmt.Sprintf("%s×%.2f", h.Filename, f))
		}
	}
	return shifted
}
