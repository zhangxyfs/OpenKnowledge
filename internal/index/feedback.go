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
// 返回被降权条目（"filename×0.80"，按当前分数降序、标题升序、文件名升序的确定性顺序）。
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
		if sorted[i].Title != sorted[j].Title {
			return sorted[i].Title < sorted[j].Title
		}
		return sorted[i].Filename < sorted[j].Filename
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
