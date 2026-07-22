package retrieve

import (
	"sort"
	"strings"
	"unicode"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
)

type Scored struct {
	Entry *entry.Entry
	Score float64
}

// Terms 将文本切分为检索词：小写拉丁/数字词（≥2 字符）与 CJK 二元组。
func Terms(s string) []string {
	var terms []string
	var latin []rune
	var cjk []rune
	flushLatin := func() {
		if len(latin) >= 2 {
			terms = append(terms, string(latin))
		}
		latin = latin[:0]
	}
	flushCJK := func() {
		if len(cjk) == 1 {
			terms = append(terms, string(cjk))
		}
		for i := 0; i+1 < len(cjk); i++ {
			terms = append(terms, string(cjk[i:i+2]))
		}
		cjk = cjk[:0]
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin = append(latin, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return terms
}

// KeywordScore：tag 命中 +3，title 命中 +2，summary 命中 +1。
func KeywordScore(query string, e *entry.Entry) float64 {
	qt := map[string]bool{}
	for _, t := range Terms(query) {
		qt[t] = true
	}
	var score float64
	for _, tag := range e.Tags {
		for _, t := range Terms(tag) {
			if qt[t] {
				score += 3
			}
		}
	}
	for _, t := range Terms(e.Title) {
		if qt[t] {
			score += 2
		}
	}
	for _, t := range Terms(e.Summary) {
		if qt[t] {
			score += 1
		}
	}
	return score
}

// Rank 混合打分排序：score = alpha·关键词 + beta·语义。queryVec 为 nil 时退化为纯关键词。
// mandatory 条目不参与。仅返回 score > 0 的前 cfg.TopN 条。
func Rank(entries []*entry.Entry, query string, queryVec []float32, vs *embed.VectorSet, cfg config.Retrieve) []Scored {
	var out []Scored
	for _, e := range entries {
		if e.Mandatory {
			continue
		}
		score := cfg.Alpha * KeywordScore(query, e)
		if queryVec != nil && vs != nil {
			if v, ok := vs.Vectors[e.FileName()]; ok {
				score += cfg.Beta * embed.Cosine(queryVec, v.Vector)
			}
		}
		if score > 0 {
			out = append(out, Scored{Entry: e, Score: score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Entry.Title < out[j].Entry.Title
	})
	if cfg.TopN > 0 && len(out) > cfg.TopN {
		out = out[:cfg.TopN]
	}
	return out
}
