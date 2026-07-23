package hook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/registry"
)

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
	if code := HandlePrompt(strings.NewReader(mkPrompt("git 提交规范是什么")), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "改完代码先写日志。") || !strings.Contains(got, "知识索引") {
		t.Fatalf("first prompt missing base injection: %q", got)
	}
	if !strings.Contains(got, "Conventional Commits") {
		t.Fatalf("first prompt missing retrieval: %q", got)
	}
	// 第二次提问（同会话）：不再重复基础注入，检索仍生效
	out.Reset()
	if code := HandlePrompt(strings.NewReader(mkPrompt("git 提交规范是什么")), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got = out.String()
	if strings.Contains(got, "改完代码先写日志。") || strings.Contains(got, "知识索引") {
		t.Fatalf("base injection repeated: %q", got)
	}
	if !strings.Contains(got, "Conventional Commits") {
		t.Fatalf("retrieval lost on second prompt: %q", got)
	}
}

func TestPromptStringFormCompat(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "Conventional Commits") {
		t.Fatalf("string prompt form broken: %q", out.String())
	}
}

func TestPromptKeywordFallback(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交规范是什么"}]}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "Conventional Commits") {
		t.Fatalf("expected git entry injected, got %q", out.String())
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
	code := HandlePrompt(strings.NewReader(in), &out)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	// 即使 embedding 全挂，基础注入与关键词检索也必须到场
	got := out.String()
	if !strings.Contains(got, "改完代码先写日志。") {
		t.Fatalf("base injection suppressed by embed outage: %q", got)
	}
	if !strings.Contains(got, "Conventional Commits") {
		t.Fatalf("keyword retrieval suppressed by embed outage: %q", got)
	}
}

func TestFirstPromptBadEntryFailsOpen(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "rule.md", mandatoryEntry)
	writeEntry(t, kbRoot, "broken.md", "no frontmatter at all")
	if err := os.WriteFile(filepath.Join(kbRoot, "INDEX.md"), []byte("# 知识索引\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"随便问问"}]}`, projDir)
	var out bytes.Buffer
	// Sync 对损坏条目中止并回滚：hook fail-open（退出 0、不注入、不崩溃），
	// 已索引内容不被破坏
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "改完代码先写日志。") {
		t.Fatalf("corrupt sync must suppress injection, got %q", out.String())
	}
	// 移除损坏文件后下一次提问恢复注入（回滚没有毁掉索引）
	if err := os.Remove(filepath.Join(kbRoot, "knowledge", "broken.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "改完代码先写日志。") {
		t.Fatalf("injection should recover after bad entry removed, got %q", out.String())
	}
}

func TestPromptBadEntryFailsOpen(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	writeEntry(t, kbRoot, "broken.md", "---\ntitle: x\ntype: bogus\n---\n")
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交"}]}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out.String(), "Conventional Commits") {
		t.Fatalf("corrupt sync must suppress injection, got %q", out.String())
	}
	// 修复坏文件后恢复注入
	if err := os.Remove(filepath.Join(kbRoot, "knowledge", "broken.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "Conventional Commits") {
		t.Fatalf("injection should recover after bad entry removed, got %q", out.String())
	}
}

func TestPromptUnregisteredProjectSilent(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	in := `{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"/nowhere","prompt":[{"type":"text","text":"git"}]}`
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 || out.Len() != 0 {
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
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 2 {
		t.Fatalf("expected block(2), got %d", code)
	}
	if !strings.Contains(stderr.String(), "请补变更日志") {
		t.Fatalf("missing message %q", stderr.String())
	}
	// 第二次 Stop 放行（防死循环）
	stderr.Reset()
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 0 {
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
	if code := HandleStop(strings.NewReader(stop2), &stderr); code != 0 {
		t.Fatalf("expected pass after changelog, got %d (%q)", code, stderr.String())
	}
}

func TestStopWithoutEnforceRulesPass(t *testing.T) {
	projDir, _ := setupProject(t)
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s3","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 0 {
		t.Fatalf("expected 0 without enforce rules, got %d", code)
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
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 || out.Len() != 0 {
		t.Fatalf("disabled prompt: code=%d out=%q", code, out.String())
	}
	codeFile := filepath.Join(projDir, "main.go")
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s9","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, projDir, codeFile)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("disabled post-tool exit %d", code)
	}
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s9","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 0 {
		t.Fatalf("disabled stop should pass, got %d (%q)", code, stderr.String())
	}
}
