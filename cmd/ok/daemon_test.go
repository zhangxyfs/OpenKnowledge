package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/daemonx"
)

// killPid 跨平台杀进程树（清理后台拉起的 daemon）。
func killPid(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

func nil2client() *http.Client { return &http.Client{Timeout: 100 * time.Millisecond} }

func TestDaemonEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home) // 测试进程内的 daemonx.Load 用
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := runOK(t, home, proj, "", "init", "demo"); code != 0 {
		t.Fatalf("init: %d %q", code, stdout)
	}
	body := filepath.Join(proj, "body.md")
	if err := os.WriteFile(body, []byte("使用 Conventional Commits。"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runOK(t, home, proj, "", "add", "--title", "Git 提交规范", "--type", "note", "--tags", "git", "--file", body); code != 0 {
		t.Fatal("add failed")
	}

	// 前台拉起 daemon（子进程，测试结束杀掉）
	daemonCmd := exec.Command(binPath, "daemon")
	daemonCmd.Env = append(os.Environ(), "OK_HOME="+home, "KIMI_CODE_HOME="+filepath.Join(home, "kimi"), "OK_ZCODE_HOME="+filepath.Join(home, "zcode-nonexistent"), "OK_REASONIX_HOME="+filepath.Join(home, "reasonix-nonexistent"), "OK_OPENCODE_HOME="+filepath.Join(home, "opencode-nonexistent"), "OPENAI_API_KEY=") // reasonix 指向不存在路径防污染真实用户配置
	if err := daemonCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemonCmd.Process.Kill(); _, _ = daemonCmd.Process.Wait() })

	var info *daemonx.Info
	for i := 0; i < 40; i++ {
		if in, err := daemonx.Load(); err == nil && in.Healthy(nil2client()) {
			info = in
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if info == nil {
		t.Fatal("daemon did not come up")
	}

	// hook prompt：daemon 在线 → 注入生效
	ev := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交规范"}]}`, proj)
	stdout, _, code := runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "Git 提交规范") {
		t.Fatalf("hook via daemon: code=%d out=%q", code, stdout)
	}

	// 停 daemon → hook 本地兜底仍注入，且后台重新拉起
	if _, _, code := runOK(t, home, proj, "", "daemon", "stop"); code != 0 {
		t.Fatal("daemon stop failed")
	}
	_ = daemonCmd.Process.Kill()
	_, _ = daemonCmd.Process.Wait()
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "Git 提交规范") {
		t.Fatalf("local fallback: code=%d out=%q", code, stdout)
	}
	// 兜底路径已后台拉起新 daemon：必须等到一个"新 PID 且健康"的实例，
	// 既证明兜底拉起真的可用，也避免误杀已死的前台 daemon 而放跑新实例。
	var respawned *daemonx.Info
	for i := 0; i < 40; i++ {
		if in, err := daemonx.Load(); err == nil && in.PID != 0 && in.PID != info.PID && in.Healthy(nil2client()) {
			respawned = in
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if respawned == nil {
		t.Fatal("fallback did not respawn a healthy daemon")
	}
	killPid(t, respawned.PID)
	// 等 daemon.log 句柄释放，避免 Windows 上 TempDir 清理因文件占用失败
	logPath := filepath.Join(home, "daemon.log")
	for i := 0; i < 50; i++ {
		if err := os.Remove(logPath); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("warning: daemon.log still locked after killing respawned daemon")
}
