//go:build !windows

package gui

import (
	"fmt"
	"os/exec"
)

// OpenBrowser 非 Windows 平台经 xdg-open 打开默认浏览器；失败仅打印 URL
// （沿用 Windows 版"失败退默认浏览器/只打印 URL"的兜底语义）。
// 无窗口句柄概念，恒返回 0（与 window_other.go 的 IsWindow=false 配套）。
// Start 后异步 Wait：调用方是常驻 daemon，不 reap 会累积僵尸进程。
func OpenBrowser(url string) uintptr {
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		fmt.Println("请在浏览器打开:", url)
		return 0
	}
	go func() { _ = cmd.Wait() }()
	return 0
}
