package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"openknowledge/internal/fsx"
)

// SetGate 重写 config.toml 的 [retrieve.gate] 子表：已存在则整段替换（到下一个
// [section] 或文件尾），不存在则在文件尾追加；其余内容（含注释）原样保留。
// 算法与 SetCapture 同款；子表头的匹配串是 "[retrieve.gate]"，边界判定
// （下一个 [ 开头行）无需改动——[retrieve.gate] 之后的任何小节头（含
// [retrieve.xxx] / [[enforce]]）都以 [ 开头，都会被正确识别为边界。
func SetGate(path string, enabled bool, extra []string) error {
	var sb strings.Builder
	sb.WriteString("[retrieve.gate]\nenabled = ")
	sb.WriteString(strconv.FormatBool(enabled))
	sb.WriteString("\nextra_phrases = [")
	for i, p := range extra {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strconv.Quote(p))
	}
	sb.WriteString("]\n")
	block := sb.String()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsx.WriteFile(path, []byte(block), 0o644)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == "[retrieve.gate]" {
				start = i
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	var out []string
	if start >= 0 {
		out = append(out, lines[:start]...)
		out = append(out, strings.TrimSuffix(block, "\n"))
		out = append(out, lines[end:]...)
	} else {
		out = append(out, lines...)
		// 与上文保持空行分隔
		if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) != "" {
			out = append(out, "")
		}
		out = append(out, strings.TrimSuffix(block, "\n"))
	}
	return fsx.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
