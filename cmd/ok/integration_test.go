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
	"time"
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
	cmd.Env = append(os.Environ(), "OK_HOME="+home, "KIMI_CODE_HOME="+filepath.Join(home, "kimi"), "PI_CODING_AGENT_DIR="+filepath.Join(home, "pi"), "OK_ZCODE_HOME="+filepath.Join(home, "zcode-nonexistent"), "OK_REASONIX_HOME="+filepath.Join(home, "reasonix-nonexistent"), "OK_OPENCODE_HOME="+filepath.Join(home, "opencode-nonexistent"), "OPENAI_API_KEY=") // 清空 key 保证测试离线；reasonix 指向不存在路径防污染真实用户配置
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
	// KIMI_CODE_HOME 目录需存在（模拟已安装 kimi），否则 agent 检测为假、init 跳过 hooks 写入
	if err := os.MkdirAll(filepath.Join(home, "kimi"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// hook 转发（ForwardHook→Ensure）会后台拉起常驻 daemon，其进程持有
	// $OK_HOME/daemon.log；测试结束前停掉并等它释放文件，否则 Windows 上
	// TempDir 清理会因文件占用失败。
	defer func() {
		runOK(t, home, proj, "", "daemon", "stop")
		logPath := filepath.Join(home, "daemon.log")
		for i := 0; i < 50; i++ {
			if err := os.Remove(logPath); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Log("warning: daemon.log still locked after daemon stop")
	}()

	// init：注册项目并幂等写入 hooks 配置（不打印裸 hooks 块）
	stdout, _, code := runOK(t, home, proj, "", "init", "demo")
	if code != 0 || !strings.Contains(stdout, "已注册项目") || strings.Contains(stdout, "[[hooks]]") {
		t.Fatalf("init failed: code=%d out=%q", code, stdout)
	}
	kimiCfg, err := os.ReadFile(filepath.Join(home, "kimi", "config.toml"))
	if err != nil || strings.Count(string(kimiCfg), "[[hooks]]") != 3 {
		t.Fatalf("init should write 3 hooks into kimi config: %v %q", err, kimiCfg)
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

	// 首次 prompt：基础注入（mandatory 全文 + 索引）+ 检索命中
	mkPrompt := func(text string) string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":%q}]}`, proj, text)
	}
	stdout, _, code = runOK(t, home, proj, mkPrompt("git 提交规范"), "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "改完代码先写日志。") || !strings.Contains(stdout, "知识索引") || !strings.Contains(stdout, "Git 提交规范") {
		t.Fatalf("first prompt: code=%d out=%q", code, stdout)
	}

	// 第二次 prompt（同会话）：不再重复基础注入，检索仍生效
	stdout, _, code = runOK(t, home, proj, mkPrompt("git 提交规范"), "hook", "prompt")
	if code != 0 || strings.Contains(stdout, "改完代码先写日志。") || strings.Contains(stdout, "知识索引") || !strings.Contains(stdout, "Git 提交规范") {
		t.Fatalf("second prompt: code=%d out=%q", code, stdout)
	}

	// 手工写入新条目（不运行 ok add，INDEX 已过期）：
	// 下一次 hook prompt 应先增量同步索引，再命中新条目并重建 INDEX.md
	kbDir := filepath.Join(home, "projects", "demo")
	manual := "---\ntitle: 部署清单\ntype: note\nsummary: 上线步骤\n---\n\n上线前先跑回归测试套件。\n"
	if err := os.WriteFile(filepath.Join(kbDir, "knowledge", "deploy.md"), []byte(manual), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(kbDir, "INDEX.md")); err != nil || strings.Contains(string(data), "部署清单") {
		t.Fatalf("precondition: INDEX should exist and be stale: %v %q", err, data)
	}
	stdout, _, code = runOK(t, home, proj, mkPrompt("部署清单"), "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "上线步骤") {
		t.Fatalf("prompt after manual edit: code=%d out=%q", code, stdout)
	}
	if data, err := os.ReadFile(filepath.Join(kbDir, "INDEX.md")); err != nil || !strings.Contains(string(data), "部署清单") {
		t.Fatalf("query-time sync should rebuild INDEX: %v %q", err, data)
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
	ev := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s9","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, proj, codeFile)
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
	ev = fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git"}]}`, filepath.Join(home, "nowhere"))
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

	// 全局开关：off 后 prompt 无输出，on 后恢复
	if _, _, code = runOK(t, home, proj, "", "off"); code != 0 {
		t.Fatalf("off: code=%d", code)
	}
	ev = fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交规范"}]}`, proj)
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || stdout != "" {
		t.Fatalf("disabled prompt should be silent: code=%d out=%q", code, stdout)
	}
	if _, _, code = runOK(t, home, proj, "", "on"); code != 0 {
		t.Fatalf("on: code=%d", code)
	}
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "Git 提交规范") {
		t.Fatalf("re-enabled prompt: code=%d out=%q", code, stdout)
	}
}
