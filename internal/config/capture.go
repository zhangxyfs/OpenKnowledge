package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// SetCapture 重写 config.toml 的 [capture] 小节：已存在则整段替换（到下一个
// [section] 或文件尾），不存在则在文件尾追加；其余内容（含注释）原样保留。
// 文件不存在时以 header 为初始内容创建（调用方决定头部模板，可为空）。
func SetCapture(path, mode string, turnInterval int, header string) error {
	block := "[capture]\nmode = " + strconv.Quote(mode) + "\nturn_interval = " + strconv.Itoa(turnInterval) + "\n"
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, []byte(header+block), 0o644)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == "[capture]" {
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
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
