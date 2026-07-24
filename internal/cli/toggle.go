package cli

import (
	"fmt"
	"io"

	"openknowledge/internal/setupx"
)

// Off: ok off —— 关闭 hooks 全局开关（持续到 ok on）
func Off(args []string, stdout, stderr io.Writer) int {
	if err := setupx.Disable(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "hooks 已全局关闭（ok on 重新开启）")
	return 0
}

// On: ok on —— 开启 hooks 全局开关
func On(args []string, stdout, stderr io.Writer) int {
	if err := setupx.Enable(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "hooks 已开启")
	return 0
}
