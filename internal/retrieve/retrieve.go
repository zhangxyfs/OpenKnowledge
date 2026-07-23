package retrieve

import (
	"strings"
	"unicode"
)

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
