package hook

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/registry"
	"openknowledge/internal/state"
	"openknowledge/internal/wiki"
)

// TestMain 隔离 KIMI_CODE_HOME 与 PI_CODING_AGENT_DIR：HandlePrompt 的 hooks 自愈
// 会写 kimi config.toml / pi 扩展，测试绝不可触碰真实配置。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hook-test-kimi-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("KIMI_CODE_HOME", dir)
	piDir, err := os.MkdirTemp("", "hook-test-pi-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("PI_CODING_AGENT_DIR", piDir)
	zcodeDir, err := os.MkdirTemp("", "hook-test-zcode-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("OK_ZCODE_HOME", zcodeDir)
	reasonixDir, err := os.MkdirTemp("", "hook-test-reasonix-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("OK_REASONIX_HOME", reasonixDir)
	opencodeDir, err := os.MkdirTemp("", "hook-test-opencode-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("OK_OPENCODE_HOME", opencodeDir)
	claudeDir, err := os.MkdirTemp("", "hook-test-claude-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("OK_CLAUDE_HOME", claudeDir)
	codepilotDir, err := os.MkdirTemp("", "hook-test-codepilot-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("OK_CODEPILOT_HOME", codepilotDir)
	os.Exit(m.Run())
}

// setupProject 在临时 OK_HOME 下注册项目并返回项目目录与 KB 根。
func setupProject(t *testing.T) (projDir, kbRoot string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	projDir = filepath.Join(home, "work", "demo")
	kbRoot = filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(filepath.Join(kbRoot, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(kbRoot, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "demo", Paths: []string{projDir}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	return projDir, kbRoot
}

func writeEntry(t *testing.T, kbRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "knowledge", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const mandatoryEntry = `---
title: 变更日志强制规则
type: rule
mandatory: true
summary: 改代码必须写日志
---

改完代码先写日志。
`

const gitEntry = `---
title: Git 提交规范
type: note
tags: [git]
summary: 提交信息格式
---

使用 Conventional Commits。
`

func TestFirstPromptInjectsBaseOnce(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "rule.md", mandatoryEntry)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	if err := os.WriteFile(filepath.Join(kbRoot, "INDEX.md"), []byte("# 知识索引\n\n- **Git 提交规范**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkPrompt := func(text string) string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":%q}]}`, projDir, text)
	}
	var out bytes.Buffer
	// 首次提问：基础注入（mandatory 全文 + 索引）+ 检索命中
	if code := HandlePrompt(strings.NewReader(mkPrompt("git 提交规范是什么")), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "改完代码先写日志。") || !strings.Contains(got, "知识索引") {
		t.Fatalf("first prompt missing base injection: %q", got)
	}
	if !strings.Contains(got, "提交信息格式") || !strings.Contains(got, "git.md") {
		t.Fatalf("first prompt missing retrieval summary line: %q", got)
	}
	if strings.Contains(got, "Conventional Commits") {
		t.Fatalf("retrieval should not inject full body: %q", got)
	}
	// 第二次提问（同会话）：不再重复基础注入，检索仍生效
	out.Reset()
	if code := HandlePrompt(strings.NewReader(mkPrompt("git 提交规范是什么")), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got = out.String()
	if strings.Contains(got, "改完代码先写日志。") || strings.Contains(got, "知识索引") {
		t.Fatalf("base injection repeated: %q", got)
	}
	if !strings.Contains(got, "提交信息格式") || !strings.Contains(got, "git.md") {
		t.Fatalf("retrieval lost on second prompt: %q", got)
	}
	if strings.Contains(got, "Conventional Commits") {
		t.Fatalf("retrieval should not inject full body: %q", got)
	}
}

func TestPromptStringFormCompat(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "提交信息格式") {
		t.Fatalf("string prompt form broken: %q", out.String())
	}
}

func TestPromptKeywordFallback(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交规范是什么"}]}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "提交信息格式") {
		t.Fatalf("expected git entry summary injected, got %q", out.String())
	}
}

func TestPromptSurvivesEmbedOutage(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "rule.md", mandatoryEntry)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	// 配置一个必然失败的 embedding 服务 + 提供 key，使 client 非 nil
	cfg := `
[embedding]
base_url = "http://127.0.0.1:1"
api_key_env = "OK_TEST_EMBED_KEY"
model = "m"
timeout_sec = 1
`
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OK_TEST_EMBED_KEY", "dummy")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交"}]}`, projDir)
	var out bytes.Buffer
	code := HandlePrompt(strings.NewReader(in), &out, "")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	// 即使 embedding 全挂，基础注入与关键词检索也必须到场
	got := out.String()
	if !strings.Contains(got, "改完代码先写日志。") {
		t.Fatalf("base injection suppressed by embed outage: %q", got)
	}
	if !strings.Contains(got, "提交信息格式") {
		t.Fatalf("keyword retrieval suppressed by embed outage: %q", got)
	}
}

func TestFirstPromptSkipsBadEntry(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "rule.md", mandatoryEntry)
	writeEntry(t, kbRoot, "broken.md", "no frontmatter at all")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"随便问问"}]}`, projDir)
	var out bytes.Buffer
	// 同步容忍损坏条目（跳过坏文件、其余提交）：一个 YAML 笔误不能压制全部注入
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "改完代码先写日志。") {
		t.Fatalf("corrupt sibling must not suppress base injection, got %q", out.String())
	}
}

func TestPromptSkipsBadEntry(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	writeEntry(t, kbRoot, "broken.md", "---\ntitle: x\ntype: bogus\n---\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交"}]}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "提交信息格式") {
		t.Fatalf("corrupt sibling must not suppress retrieval, got %q", out.String())
	}
}

func TestPromptUnregisteredProjectSilent(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	in := `{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"/nowhere","prompt":[{"type":"text","text":"git"}]}`
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 || out.Len() != 0 {
		t.Fatalf("expected silent 0, got %d %q", code, out.String())
	}
}

func TestPostToolAndStopEnforcement(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	cfg := `
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	codeFile := filepath.Join(projDir, "main.go")
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s1","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, projDir, codeFile)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("post-tool exit %d", code)
	}
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s1","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, ""); code != 2 {
		t.Fatalf("expected block(2), got %d", code)
	}
	if !strings.Contains(stderr.String(), "请补变更日志") {
		t.Fatalf("missing message %q", stderr.String())
	}
	// MarkBlocked 所有权在 HandleStop：阻断后 BlockedRules 必须落盘
	stBlocked := state.Load(filepath.Join(kbRoot, "state"), "s1")
	if !stBlocked.HasBlocked("changelog_required") {
		t.Fatalf("阻断后应落 MarkBlocked（每会话每规则最多一次的前提）: %+v", stBlocked.BlockedRules)
	}
	// 第二次 Stop 放行（防死循环）
	stderr.Reset()
	if code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, ""); code != 0 {
		t.Fatalf("expected pass on second stop, got %d", code)
	}
	// 新会话：触碰代码 + 触碰变更日志 → 放行
	cl := filepath.Join(projDir, "docs", "changelogs", "2026-07-22.md")
	post2 := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s2","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, projDir, codeFile)
	_ = HandlePostTool(strings.NewReader(post2))
	post3 := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s2","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, projDir, cl)
	_ = HandlePostTool(strings.NewReader(post3))
	stderr.Reset()
	stop2 := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s2","cwd":%q}`, projDir)
	if code := HandleStop(strings.NewReader(stop2), &stderr, &bytes.Buffer{}, ""); code != 0 {
		t.Fatalf("expected pass after changelog, got %d (%q)", code, stderr.String())
	}
}

func TestStopWithoutEnforceRulesPass(t *testing.T) {
	projDir, _ := setupProject(t)
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s3","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, ""); code != 0 {
		t.Fatalf("expected 0 without enforce rules, got %d", code)
	}
}

// writeCaptureConfig 写入只含 [capture] 的配置（无 [[enforce]]）。
func writeCaptureConfig(t *testing.T, kbRoot, mode string, interval int) {
	t.Helper()
	cfg := fmt.Sprintf("[capture]\nmode = %q\nturn_interval = %d\n", mode, interval)
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touchGoFile(t *testing.T, projDir, sessionID string) {
	t.Helper()
	codeFile := filepath.Join(projDir, "main.go")
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":%q,"cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, sessionID, projDir, codeFile)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("post-tool exit %d", code)
	}
}

func stopOnce(t *testing.T, projDir, sessionID string) (int, string) {
	t.Helper()
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":%q,"cwd":%q}`, sessionID, projDir)
	var stderr bytes.Buffer
	code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, "")
	return code, stderr.String()
}

func TestStopAutoCaptureRemindsOnInterval(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeCaptureConfig(t, kbRoot, "auto", 2)
	touchGoFile(t, projDir, "s1")
	// 第 1 回合：间隔未满 → 放行
	if code, out := stopOnce(t, projDir, "s1"); code != 0 {
		t.Fatalf("stop 1: expected 0, got %d (%q)", code, out)
	}
	// 第 2 回合：间隔满 → 阻断并提示 ok propose
	code, out := stopOnce(t, projDir, "s1")
	if code != 2 {
		t.Fatalf("stop 2: expected 2, got %d (%q)", code, out)
	}
	if !strings.Contains(out, "ok propose") {
		t.Fatalf("stop 2: missing propose hint: %q", out)
	}
	// 提醒后计数重置：第 3 回合放行，第 4 回合再次提醒
	if code, out := stopOnce(t, projDir, "s1"); code != 0 {
		t.Fatalf("stop 3: expected 0 after reminder reset, got %d (%q)", code, out)
	}
	if code, out := stopOnce(t, projDir, "s1"); code != 2 {
		t.Fatalf("stop 4: expected 2 on next interval, got %d (%q)", code, out)
	}
}

func TestStopAutoCaptureRequiresTouched(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeCaptureConfig(t, kbRoot, "auto", 1)
	// 无文件修改：即使间隔为 1 也不提醒
	for i := 0; i < 3; i++ {
		if code, out := stopOnce(t, projDir, "s1"); code != 0 {
			t.Fatalf("stop %d: expected 0 without touched files, got %d (%q)", i+1, code, out)
		}
	}
}

func TestStopProposeModeNeverReminds(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeCaptureConfig(t, kbRoot, "propose", 1)
	touchGoFile(t, projDir, "s1")
	for i := 0; i < 3; i++ {
		if code, out := stopOnce(t, projDir, "s1"); code != 0 {
			t.Fatalf("stop %d: propose mode must not remind, got %d (%q)", i+1, code, out)
		}
	}
}

func TestStopAutoTurnIntervalZeroMeansEveryStop(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeCaptureConfig(t, kbRoot, "auto", 0)
	touchGoFile(t, projDir, "s1")
	// turn_interval <= 0 视为 1：每回合都提醒
	for i := 0; i < 2; i++ {
		code, out := stopOnce(t, projDir, "s1")
		if code != 2 {
			t.Fatalf("stop %d: expected 2 with interval 0, got %d (%q)", i+1, code, out)
		}
		if !strings.Contains(out, "ok propose") {
			t.Fatalf("stop %d: missing propose hint: %q", i+1, out)
		}
	}
}

func TestStopAutoCaptureWorksWithoutEnforce(t *testing.T) {
	// 回归：无 [[enforce]] 时 auto 自省仍须生效（enforce 提前 return 不得跳过 auto 分支）
	projDir, kbRoot := setupProject(t)
	writeCaptureConfig(t, kbRoot, "auto", 1)
	touchGoFile(t, projDir, "s1")
	code, out := stopOnce(t, projDir, "s1")
	if code != 2 {
		t.Fatalf("expected 2 without enforce rules, got %d (%q)", code, out)
	}
	if !strings.Contains(out, "ok propose") {
		t.Fatalf("missing propose hint: %q", out)
	}
}

func TestHooksDisabledStopsAll(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	cfg := `
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// 关闭全局开关
	if err := os.WriteFile(filepath.Join(registry.Home(), "hooks-disabled"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交"}]}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 || out.Len() != 0 {
		t.Fatalf("disabled prompt: code=%d out=%q", code, out.String())
	}
	codeFile := filepath.Join(projDir, "main.go")
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s9","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, projDir, codeFile)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("disabled post-tool exit %d", code)
	}
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s9","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, ""); code != 0 {
		t.Fatalf("disabled stop should pass, got %d (%q)", code, stderr.String())
	}
}

func TestStopAutoCaptureThenEnforce(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	cfg := `
[capture]
mode = "auto"
turn_interval = 5

[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	codeFile := filepath.Join(projDir, "main.go")
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s1","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, projDir, codeFile)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("post-tool exit %d", code)
	}
	// 预置 StopCount，使第 1 次 Stop 时自省间隔恰好到期（5-0>=5）
	st := state.Load(filepath.Join(kbRoot, "state"), "s1")
	st.StopCount = 4
	if err := st.Save(filepath.Join(kbRoot, "state")); err != nil {
		t.Fatal(err)
	}
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s1","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	// 第 1 次 Stop：auto 自省先触发（不是 enforce 文案）
	if code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, ""); code != 2 {
		t.Fatalf("first stop should block with extraction reminder, got %d (%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ok propose") {
		t.Fatalf("first stop should be the extraction reminder, got %q", stderr.String())
	}
	// 第 2 次 Stop：自省间隔未满跳过，enforce 触发
	stderr.Reset()
	if code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, ""); code != 2 {
		t.Fatalf("second stop should block with enforce message, got %d (%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "请补变更日志") {
		t.Fatalf("second stop should be the enforce message, got %q", stderr.String())
	}
	// 第 3 次 Stop：enforce 已阻断过（BlockedRules），自省间隔未满 → 放行
	stderr.Reset()
	if code := HandleStop(strings.NewReader(stop), &stderr, &bytes.Buffer{}, ""); code != 0 {
		t.Fatalf("third stop should pass, got %d (%q)", code, stderr.String())
	}
}

// initGitRepo 在项目目录初始化 git 并提交 n 个 commit（供 wiki 落后提示测试）。
// 显式 -b master：分支上下文测试依赖基准分支名，不随本机 init.defaultBranch 漂移。
func initGitRepo(t *testing.T, dir string, n int) {
	t.Helper()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "master")
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte{byte(i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", "c")
	}
}

// runGit 在 dir 执行 git 命令（initGitRepo 的 run 是闭包不可复用，提为文件级）。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitHead 返回 dir 的 HEAD 完整 hash。
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	h, err := wiki.HeadCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

const wikiNudgeHint = "本项目还没有 wiki"

func TestPromptWikiNudge(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2)
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[wiki]\nstale_commits = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkPrompt := func() string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"hello"}`, projDir)
	}
	var out bytes.Buffer
	// 达阈值：第一次提示，且提示在输出末尾
	if code := HandlePrompt(strings.NewReader(mkPrompt()), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, wikiNudgeHint) {
		t.Fatalf("first prompt missing wiki nudge: %q", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "。") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("nudge should be at output end: %q", got)
	}
	// 第二次（同会话）：不再提示
	out.Reset()
	if code := HandlePrompt(strings.NewReader(mkPrompt()), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), wikiNudgeHint) {
		t.Fatalf("nudge repeated in same session: %q", out.String())
	}
}

func TestPromptWikiNudgeDisabled(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2)
	// stale_commits = 0：关闭提示
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[wiki]\nstale_commits = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"hello"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), wikiNudgeHint) {
		t.Fatalf("stale_commits=0 must not nudge: %q", out.String())
	}
}

func TestPromptWikiNudgeNonGitSilent(t *testing.T) {
	projDir, kbRoot := setupProject(t) // projDir 非 git 仓库
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[wiki]\nstale_commits = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"hello"}`, projDir)
	var out bytes.Buffer
	// fail-open：非 git 目录静默，退出码不变
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), wikiNudgeHint) {
		t.Fatalf("non-git project must not nudge: %q", out.String())
	}
}

func TestPromptWikiBranchContextOnDiverged(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2)
	runGit(t, projDir, "checkout", "-q", "-b", "dev") // 切到 dev：master 游标对 dev 为 no_cursor
	head := gitHead(t, projDir)
	// 游标记在 master（dev 上无基线）
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: head}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	writeEntry(t, kbRoot, "条目.md", "---\ntitle: 测试条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文含线索词 BranchCue。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"BranchCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "[OpenKnowledge] wiki 基于 master@") || !strings.Contains(got, "当前分支 dev") {
		t.Errorf("分叉/无基线分支应附上下文行: %q", got)
	}
	if !strings.Contains(got, "测试条目") {
		t.Errorf("注入主流程不应受影响: %q", got)
	}
}

func TestPromptWikiNoContextOnBaseBranch(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2)
	head := gitHead(t, projDir)
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: head}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	writeEntry(t, kbRoot, "条目.md", "---\ntitle: 测试条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文含线索词 BranchCue。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"BranchCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "当前分支") {
		t.Errorf("基准分支不得出现分支上下文行（零回归）: %q", out.String())
	}
}

func TestPromptFiltersOtherBranchEntries(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1) // 当前分支 master
	writeEntry(t, kbRoot, "共享.md", "---\ntitle: 共享规约\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n共享正文含线索词 FilterCue。\n")
	writeEntry(t, kbRoot, "dev差异.md", "---\ntitle: dev 差异经验\ntype: note\ntags: [\"branch:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\ndev 正文也含线索词 FilterCue。\n")
	// 预置已基础注入：隔离首次基础注入的 INDEX 噪音，只断言检索过滤通道
	// （带 branch 标签的条目本就不进 INDEX 主列表，但 INDEX 注入仍会带共享条目，
	// 预置后输出更干净、断言只落在检索通道上）
	st := state.Load(filepath.Join(kbRoot, "state"), "s1")
	st.BaseInjected = true
	if err := st.Save(filepath.Join(kbRoot, "state")); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"FilterCue"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "共享规约") {
		t.Errorf("共享条目应命中: %q", got)
	}
	if strings.Contains(got, "dev 差异经验") {
		t.Errorf("master 会话不得命中 branch:dev 条目: %q", got)
	}
}

func TestPromptIndexTrimmedByBranch(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1)
	// 一条 dev 差异 wiki 条目（触发 INDEX 双段）
	writeEntry(t, kbRoot, "差异.md", "---\ntitle: 架构总览（dev 分支差异）\ntype: reference\ntags: [\"wiki\", \"branch:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	writeEntry(t, kbRoot, "主条目.md", "---\ntitle: 架构总览\ntype: reference\ntags: [\"wiki\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s2","cwd":%q,"prompt":"hello"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "架构总览") {
		t.Errorf("主目录应注入: %q", got)
	}
	if strings.Contains(got, "分支差异（dev）") || strings.Contains(got, "（dev 分支差异）") {
		t.Errorf("master 会话的 INDEX 注入不得含 dev 差异小节: %q", got)
	}
}

// 已并入提示（merged 变体）：基准分支上、dev tip 已并入且其差异条目仍在库中 →
// 提示一次；同会话第二次不再提示；删掉差异条目后（新会话）不再提示。
func TestPromptWikiMergedNudge(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2) // 当前在 master
	runGit(t, projDir, "checkout", "-q", "-b", "dev")
	runGit(t, projDir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "d1")
	devTip := gitHead(t, projDir)
	runGit(t, projDir, "checkout", "-q", "master")
	runGit(t, projDir, "-c", "user.email=t@t", "-c", "user.name=t", "merge", "-q", "--no-ff", "dev", "-m", "merge dev")
	// 基准分支游标 + dev 游标（dev tip 已并入 master）
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{
		"master": {LastCommit: gitHead(t, projDir)},
		"dev":    {LastCommit: devTip},
	}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	// dev 的差异条目仍在库中
	writeEntry(t, kbRoot, "dev差异.md", "---\ntitle: 架构总览（dev 分支差异）\ntype: reference\ntags: [\"wiki\", \"branch:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	mkPrompt := func(session string) string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":%q,"cwd":%q,"prompt":"hello"}`, session, projDir)
	}
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(mkPrompt("s1")), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "分支 dev 已并入 master") || !strings.Contains(got, "差异条目已失效") {
		t.Fatalf("基准分支应提示已并入: %q", got)
	}
	// 第二次（同会话）：不再提示
	out.Reset()
	if code := HandlePrompt(strings.NewReader(mkPrompt("s1")), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "已并入") {
		t.Fatalf("merged nudge 不得在同会话重复: %q", out.String())
	}
	// 删掉差异条目后（新会话）：不再提示
	if err := os.Remove(filepath.Join(kbRoot, "knowledge", "dev差异.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := HandlePrompt(strings.NewReader(mkPrompt("s2")), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "已并入") {
		t.Fatalf("差异条目删除后不得提示: %q", out.String())
	}
}

// merged 检测每会话熔断（MergedChecked）：基准分支 + 有非基准游标 + 无差异条目
// 时 merged 必为空，首次 prompt 后 MergedChecked 置位（之后每 prompt 0 额外
// git spawn）；且不得误置 WikiNudged——后续 stale nudge 不受影响。
func TestPromptMergedCheckedCircuitBreaker(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 2) // 当前在 master
	runGit(t, projDir, "checkout", "-q", "-b", "dev")
	runGit(t, projDir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "d1")
	devTip := gitHead(t, projDir)
	runGit(t, projDir, "checkout", "-q", "master")
	runGit(t, projDir, "-c", "user.email=t@t", "-c", "user.name=t", "merge", "-q", "--no-ff", "dev", "-m", "merge dev")
	// 基准 + 非基准游标，但库中无任何 dev 差异条目 → merged 检测为空结果
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{
		"master": {LastCommit: gitHead(t, projDir)},
		"dev":    {LastCommit: devTip},
	}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	mkPrompt := func(session string) string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":%q,"cwd":%q,"prompt":"hello"}`, session, projDir)
	}
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(mkPrompt("s1")), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "已并入") {
		t.Fatalf("无差异条目不得提示已并入: %q", out.String())
	}
	// 空结果也要置位熔断：merged 为空不应每 prompt 重付 git spawn
	got := state.Load(filepath.Join(kbRoot, "state"), "s1")
	if !got.MergedChecked {
		t.Fatalf("首次 prompt 后 MergedChecked 应置位（无论检测结果）: %+v", got)
	}
	if got.WikiNudged {
		t.Fatalf("merged 为空不得误置 WikiNudged（会抑制后续 stale nudge）: %+v", got)
	}
}

func TestPromptWikiGoneNudge(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1)
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[wiki]\nstale_commits = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{"master": {LastCommit: "0123456789abcdef0123456789abcdef01234567"}}}
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), st); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"hello"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, ""); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "游标失效") {
		t.Errorf("失效态应提示: %q", out.String())
	}
}
