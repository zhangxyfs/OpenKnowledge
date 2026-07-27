package daemon

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/daemonx"
)

// waitHealthy 轮询直到 daemon.json 指向的实例健康或超时。
func waitHealthy(t *testing.T, d time.Duration) *daemonx.Info {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if info, err := daemonx.Load(); err == nil && info.Healthy(&http.Client{Timeout: 100 * time.Millisecond}) {
			return info
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("daemon not healthy in time")
	return nil
}

func TestRunStopAndSecondInstance(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	webDir := t.TempDir()

	// 起第一个 daemon
	var out1 bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Run(webDir, &out1, io.Discard) }()
	_ = waitHealthy(t, 5*time.Second)
	if !strings.Contains(out1.String(), "OpenKnowledge daemon:") {
		t.Fatalf("banner %q", out1.String())
	}

	// 第二个实例：抢不到端口且已有健康实例 → 立即退出 0
	var out2 bytes.Buffer
	if code := Run(webDir, &out2, io.Discard); code != 0 {
		t.Fatalf("second instance code=%d", code)
	}
	if !strings.Contains(out2.String(), "已在运行") {
		t.Fatalf("second instance out %q", out2.String())
	}

	// Stop → 第一个 Run 返回 0，daemon.json 删除
	var out3 bytes.Buffer
	if code := Stop(&out3, io.Discard); code != 0 {
		t.Fatalf("stop code=%d", code)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("run exited %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit after stop")
	}
	if _, err := daemonx.Load(); err == nil {
		t.Fatal("daemon.json should be removed")
	}

	// 再 Stop：未运行也返回 0
	if code := Stop(io.Discard, io.Discard); code != 0 {
		t.Fatal("stop when not running should be 0")
	}
}

func TestRunPortFallback(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	// 占用默认端口（无 daemon.json → 不是"已有 daemon"）→ Run 应回退随机端口
	ln, err := net.Listen("tcp", "127.0.0.1:17888")
	if err != nil {
		t.Skip("17888 unavailable")
	}
	defer ln.Close()
	done := make(chan int, 1)
	go func() { done <- Run(t.TempDir(), io.Discard, io.Discard) }()
	info := waitHealthy(t, 5*time.Second)
	if info.Port == 17888 {
		t.Fatal("should fall back to random port")
	}
	Stop(io.Discard, io.Discard)
	<-done
}

func TestOpenGUI(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	var opened string
	old := OpenBrowserFunc
	OpenBrowserFunc = func(url string) { opened = url }
	t.Cleanup(func() { OpenBrowserFunc = old })

	done := make(chan int, 1)
	go func() { done <- Run(t.TempDir(), io.Discard, io.Discard) }()
	waitHealthy(t, 5*time.Second)

	var out bytes.Buffer
	if code := OpenGUI(&out, io.Discard); code != 0 {
		t.Fatalf("OpenGUI code=%d", code)
	}
	if !strings.Contains(opened, "/?token=") {
		t.Fatalf("opened url %q", opened)
	}
	Stop(io.Discard, io.Discard)
	<-done
}
