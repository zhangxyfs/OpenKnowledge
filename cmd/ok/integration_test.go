package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ok-bin")
	if err != nil {
		panic(err)
	}
	name := "ok"
	if runtime.GOOS == "windows" {
		name = "ok.exe"
	}
	binPath = filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build failed: %v\n%s", err, out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// runOK 以隔离的 OK_HOME 运行二进制，返回 stdout、stderr 与退出码。
func runOK(t *testing.T, home, cwd, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "OK_HOME="+home, "OPENAI_API_KEY=") // 清空 key 保证测试离线
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return so.String(), se.String(), code
}

func TestEndToEnd(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	// init：注册项目并打印 hooks 块
	stdout, _, code := runOK(t, home, proj, "", "init", "demo")
	if code != 0 || !strings.Contains(stdout, "已注册项目") || !strings.Contains(stdout, "[[hooks]]") {
		t.Fatalf("init failed: code=%d out=%q", code, stdout)
	}

	// add 普通条目（无 embedding key → 跳过向量但成功）
	body := filepath.Join(proj, "body.md")
	if err := os.WriteFile(body, []byte("使用 Conventional Commits。"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = runOK(t, home, proj, "", "add", "--title", "Git 提交规范", "--type", "note", "--tags", "git", "--file", body)
	if code != 0 {
		t.Fatalf("add failed: code=%d out=%q", code, stdout)
	}

	// add mandatory 条目
	mb := filepath.Join(proj, "mb.md")
	if err := os.WriteFile(mb, []byte("改完代码先写日志。"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code = runOK(t, home, proj, "", "add", "--title", "变更日志强制规则", "--type", "rule", "--mandatory", "--file", mb); code != 0 {
		t.Fatalf("add mandatory failed: code=%d", code)
	}

	// session-start：注入 mandatory 全文 + 索引，不含非 mandatory 正文
	ev := fmt.Sprintf(`{"hook_event_name":"SessionStart","session_id":"s1","cwd":%q}`, proj)
	stdout, _, code = runOK(t, home, proj, ev, "hook", "session-start")
	if code != 0 || !strings.Contains(stdout, "改完代码先写日志。") || !strings.Contains(stdout, "知识索引") {
		t.Fatalf("session-start: code=%d out=%q", code, stdout)
	}
	if strings.Contains(stdout, "Conventional Commits") {
		t.Fatalf("non-mandatory body leaked: %q", stdout)
	}

	// prompt：关键词命中注入
	ev = fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交规范"}`, proj)
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "Conventional Commits") {
		t.Fatalf("prompt: code=%d out=%q", code, stdout)
	}

	// 配置强制规则
	cfgPath := filepath.Join(home, "projects", "demo", "config.toml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg = append(cfg, []byte("\n[[enforce]]\ntype = \"changelog_required\"\ncode_globs = [\"**/*.go\"]\nchangelog_glob = \"docs/changelogs/**\"\nmessage = \"请补变更日志\"\n")...)
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	// post-tool 触碰代码 → stop 阻断一次 → 第二次放行
	codeFile := filepath.Join(proj, "main.go")
	ev = fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s9","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, proj, codeFile)
	if _, _, code = runOK(t, home, proj, ev, "hook", "post-tool"); code != 0 {
		t.Fatalf("post-tool: code=%d", code)
	}
	ev = fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s9","cwd":%q}`, proj)
	_, stderr, code := runOK(t, home, proj, ev, "hook", "stop")
	if code != 2 || !strings.Contains(stderr, "请补变更日志") {
		t.Fatalf("stop should block: code=%d err=%q", code, stderr)
	}
	if _, _, code = runOK(t, home, proj, ev, "hook", "stop"); code != 0 {
		t.Fatalf("second stop should pass: code=%d", code)
	}

	// 未注册目录：所有 hook 静默放行
	ev = fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git"}`, filepath.Join(home, "nowhere"))
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || stdout != "" {
		t.Fatalf("unregistered should be silent: code=%d out=%q", code, stdout)
	}

	// list / doctor 冒烟
	stdout, _, code = runOK(t, home, proj, "", "list")
	if code != 0 || !strings.Contains(stdout, "demo") {
		t.Fatalf("list: code=%d out=%q", code, stdout)
	}
	stdout, _, _ = runOK(t, home, proj, "", "doctor")
	if !strings.Contains(stdout, "demo") {
		t.Fatalf("doctor: out=%q", stdout)
	}
}
