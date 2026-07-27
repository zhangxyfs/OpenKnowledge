package daemon

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"openknowledge/internal/daemonx"
	"openknowledge/internal/gui"
)

// OpenBrowserFunc 打开浏览器；测试可替换。
var OpenBrowserFunc = gui.OpenBrowser

// selfCheckInterval 自省间隔；测试可调小。
var selfCheckInterval = 15 * time.Second

// Run 以单实例运行 daemon 并阻塞：端口即锁，第二个实例发现已有健康 daemon 即退出 0。
// 默认端口被非 daemon 占用时回退随机端口。/api/shutdown 或进程信号结束运行。
func Run(webDir string, stdout, stderr io.Writer) int {
	fp, err := daemonx.ExeFingerprint()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", daemonx.DefaultPort))
	if err != nil {
		if info, lerr := daemonx.Load(); lerr == nil && info.Healthy(quickClient()) {
			fmt.Fprintln(stdout, "daemon 已在运行，本实例退出")
			return 0
		}
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	token, err := newToken()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	info := &daemonx.Info{
		PID:         os.Getpid(),
		Port:        ln.Addr().(*net.TCPAddr).Port,
		Token:       token,
		Fingerprint: fp,
		StartedAt:   time.Now().Format(time.RFC3339),
	}
	if err := daemonx.Save(info); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// 仅在 daemon.json 仍属于本进程时删除：被替换的旧实例退出时可能晚于新实例写入，
	// 无条件 Remove 会误删新 daemon 的凭据。
	defer func() {
		if cur, err := daemonx.Load(); err == nil && cur.PID == os.Getpid() {
			_ = daemonx.Remove()
		}
	}()

	gh := gui.NewHandler(webDir, token, nil)
	srv := &http.Server{Handler: NewMux(gh, token, fp)}
	go func() {
		<-gh.Done()
		_ = srv.Shutdown(context.Background())
	}()
	// 自省：daemon.json 丢失或易主（并发 spawn 败者/版本切换残留）→ 自动退出，保证全局唯一
	go func() {
		ticker := time.NewTicker(selfCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			cur, err := daemonx.Load()
			if err != nil || cur.PID != os.Getpid() {
				_ = srv.Shutdown(context.Background())
				return
			}
		}
	}()
	fmt.Fprintf(stdout, "OpenKnowledge daemon: %s\n", info.URL())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// Stop 停止当前 daemon（ok daemon stop）；未运行也返回 0。
func Stop(stdout, _ io.Writer) int {
	info, err := daemonx.Load()
	if err != nil {
		fmt.Fprintln(stdout, "daemon 未运行")
		return 0
	}
	info.Shutdown(&http.Client{Timeout: 2 * time.Second})
	_ = daemonx.Remove()
	fmt.Fprintln(stdout, "daemon 已停止")
	return 0
}

// OpenGUI 确保 daemon 在线（含版本切换）后打开浏览器并立即返回。
func OpenGUI(_, stderr io.Writer) int {
	if info, ok := EnsureCurrent(); ok {
		OpenBrowserFunc(info.URL() + "/?token=" + info.Token)
		return 0
	}
	// daemon 正在后台拉起：轮询就绪（最长 3s）
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if info, err := daemonx.Load(); err == nil && info.Healthy(quickClient()) {
			OpenBrowserFunc(info.URL() + "/?token=" + info.Token)
			return 0
		}
	}
	fmt.Fprintln(stderr, "daemon 启动超时，请重试 ok gui")
	return 1
}
