// Package gui 提供 ok gui / 无参数启动的 Web 管理界面：
// 127.0.0.1 随机端口 HTTP 服务、令牌鉴权、心跳看门狗与浏览器自动打开。
package gui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"time"
)

// heartbeatTimeout 是心跳看门狗的超时阈值：页面每 5s 一次心跳，
// 超过该时长无心跳（页面被关闭）则自动停服。
const heartbeatTimeout = 30 * time.Second

// Run 启动 Web GUI 服务并阻塞直到 /api/shutdown 或心跳超时；返回进程退出码。
func Run(webDir string, stdout, stderr io.Writer) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	token, err := newToken()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	beats := make(chan struct{}, 1)
	h := NewHandler(webDir, token, beats)
	srv := &http.Server{Handler: h}

	url := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", ln.Addr().(*net.TCPAddr).Port, token)
	fmt.Fprintf(stdout, "OpenKnowledge GUI: %s\n关闭此窗口退出（页面 30 秒无心跳也会自动退出）\n", url)
	openBrowser(url)

	// 看门狗：收到首个心跳后才上膛（浏览器打开慢或失败时不误杀）；
	// 之后每次心跳重置计时，超时即停服。
	go func() {
		<-beats
		timer := time.NewTimer(heartbeatTimeout)
		defer timer.Stop()
		for {
			select {
			case <-beats:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(heartbeatTimeout)
			case <-timer.C:
				_ = srv.Shutdown(context.Background())
				return
			case <-h.Done():
				return
			}
		}
	}()
	// /api/shutdown → 立即停服（Shutdown 会等进行中的响应写完）。
	go func() {
		<-h.Done()
		_ = srv.Shutdown(context.Background())
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// newToken 生成 16 字节随机的 hex 令牌（32 字符）。
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// openBrowser 以最大化窗口打开 Edge/Chrome 应用模式；失败退回默认浏览器（不保证最大化）。
// 注：cmd start 会把 --start-maximized 当自家参数吞掉且 Edge 常忽略该 flag，
// 经 PowerShell Start-Process -WindowStyle Maximized 才可靠。
func openBrowser(url string) {
	for _, browser := range []string{"msedge", "chrome"} {
		ps := fmt.Sprintf("Start-Process %s -ArgumentList '--app=%s' -WindowStyle Maximized", browser, url)
		if err := exec.Command("powershell", "-NoProfile", "-Command", ps).Run(); err == nil {
			return
		}
	}
	_ = exec.Command("cmd", "/c", "start", url).Run()
}
