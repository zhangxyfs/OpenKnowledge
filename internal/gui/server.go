// Package gui 提供 ok gui / 无参数启动的 Web 管理界面：
// 127.0.0.1 HTTP 服务、令牌鉴权、心跳版本号与浏览器自动打开。
// 进程生命周期由 internal/daemon 托管（常驻，不再随页面关闭退出）。
package gui

import (
	"fmt"
	"os/exec"
	"time"
)

// OpenBrowser 以最大化窗口打开 Edge/Chrome 应用模式；失败退回默认浏览器（不保证最大化）。
// 注：cmd start 会吞 --start-maximized 参数，Edge 单实例时 Start-Process 的
// -WindowStyle 也会被既有进程吞掉，只能在窗口出现后事后 ShowWindow 兜底。
// maximizeWindowByTitle 必须同步调用：ok gui 开浏览器即退（daemon 托管生命周期），
// 协程会随进程退出而被杀——v2.1 的"不自动最大化"回归正源于此。
func OpenBrowser(url string) {
	for _, browser := range []string{"msedge", "chrome"} {
		ps := fmt.Sprintf("Start-Process %s -ArgumentList '--app=%s' -WindowStyle Maximized", browser, url)
		if err := exec.Command("powershell", "-NoProfile", "-Command", ps).Run(); err == nil {
			maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
			return
		}
	}
	_ = exec.Command("cmd", "/c", "start", url).Run()
	maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
}
