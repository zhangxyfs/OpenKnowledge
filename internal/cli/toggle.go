package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/registry"
)

func disabledFlagPath() string { return filepath.Join(registry.Home(), "hooks-disabled") }

// Off: ok off —— 关闭 hooks 全局开关（持续到 ok on）
func Off(args []string, stdout, stderr io.Writer) int {
	content := fmt.Sprintf("disabled at %s\nrun `ok on` to re-enable\n", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(disabledFlagPath(), []byte(content), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "hooks 已全局关闭（ok on 重新开启）")
	return 0
}

// On: ok on —— 开启 hooks 全局开关
func On(args []string, stdout, stderr io.Writer) int {
	if err := os.Remove(disabledFlagPath()); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "hooks 已开启")
	return 0
}
