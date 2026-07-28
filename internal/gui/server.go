// Package gui 提供 ok gui / 无参数启动的 Web 管理界面：
// 127.0.0.1 HTTP 服务、令牌鉴权、心跳版本号与浏览器自动打开。
// 进程生命周期由 internal/daemon 托管（常驻，不再随页面关闭退出）。
package gui

import (
	"fmt"
	"os/exec"
	"time"

	"openknowledge/internal/procx"
)

// OpenBrowser 以最大化窗口打开 Edge/Chrome 应用模式，返回新窗口句柄（未找到返回 0）；
// 失败退回默认浏览器（不保证最大化）。调用方可用返回的 hwnd 做后续聚焦复用。
// 注：maximizeWindowByTitle 必须同步调用：ok gui 开浏览器即退（daemon 托管生命周期），
// 协程会随进程退出而被杀——v2.1 的"不自动最大化"回归正源于此。
func OpenBrowser(url string) uintptr {
	for _, browser := range []string{"msedge", "chrome"} {
		ps := fmt.Sprintf("Start-Process %s -ArgumentList '--app=%s' -WindowStyle Maximized", browser, url)
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		procx.HideWindow(cmd)
		if err := cmd.Run(); err == nil {
			return maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
		}
	}
	fallback := exec.Command("cmd", "/c", "start", url)
	procx.HideWindow(fallback)
	_ = fallback.Run()
	return maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
}
