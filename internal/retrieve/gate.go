package retrieve

import (
	"strings"
	"unicode"
)

// builtinPhrases 内置泛化确认短语表（宁窄勿宽，只收高置信无信息量的确认/推进词，
// 多语言常用确认词）。编译进二进制随版本演进；用户追加层见 [retrieve.gate]
// extra_phrases，两层取并集。表内条目本身即归一化形（小写、无标点）。
var builtinPhrases = []string{
	"继续", "继续吧", "好的", "好", "嗯", "对", "是的", "行", "可以", "收到", "谢谢",
	"ok", "okay", "yes", "no", "thanks", "continue", "go", "go on", "next", "done",
}

// BuiltinPhrases 返回内置短语表副本（GUI 展示用，调用方不得依赖可写性）。
func BuiltinPhrases() []string {
	return append([]string(nil), builtinPhrases...)
}

// Normalize 归一化短语：小写、字母/数字之外的字符视为分隔、折叠连续空白、去首尾
// 空白。门控判定与 extra_phrases 去重共用同一归一化形（"go,  On!" ≡ "go on"）。
func Normalize(s string) string {
	var b strings.Builder
	lastSpace := true // 起始视为空白，吃掉前导分隔
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Gated 判定 prompt 是否为无信息量的泛化输入（应跳过检索注入与 embed 调用）：
//  1. 归一化后精确命中内置/extra 短语表；
//  2. Terms 提取为空（纯标点/emoji/空白/单字符——此时 queryAll 本就返回空，
//     门控只是省掉 embed 网络往返）。
// 不设长度阈值：两字的"构建"是合法查询，长度启发式误杀率高。
func Gated(prompt string, extra []string) bool {
	n := Normalize(prompt)
	if n == "" || len(Terms(prompt)) == 0 {
		return true
	}
	for _, p := range builtinPhrases {
		if n == p {
			return true
		}
	}
	for _, p := range extra {
		if n == Normalize(p) {
			return true
		}
	}
	return false
}
