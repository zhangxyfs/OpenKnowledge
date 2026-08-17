package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"openknowledge/internal/fsx"
)

// SetInjectMandatoryMax 在 config.toml 的 [inject] 小节内 upsert 单个键
// mandatory_max_tokens：小节已存在则只替换/追加该键行（max_tokens、
// reinject_turns 与注释原样保留），小节不存在则文件尾追加 [inject] 块。
// 与 SetGate 的整段替换不同——[inject] 是多人共用笔触的小节，不能整段覆盖。
func SetInjectMandatoryMax(path string, n int) error {
	keyLine := "mandatory_max_tokens = " + strconv.Itoa(n)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsx.WriteFile(path, []byte("[inject]\n"+keyLine+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == "[inject]" {
				start = i
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	if start < 0 {
		// 无 [inject] 小节：文件尾追加（与上文保持空行分隔）
		if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "[inject]", keyLine)
		return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
	// 小节内 upsert：命中键行替换，未命中插到小节末尾
	hit := false
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "mandatory_max_tokens") && strings.Contains(t, "=") {
			lines[i] = keyLine
			hit = true
			break
		}
	}
	if !hit {
		tail := append([]string{keyLine}, lines[end:]...)
		lines = append(lines[:end], tail...)
	}
	return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
