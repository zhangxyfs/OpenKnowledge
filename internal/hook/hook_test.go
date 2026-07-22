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

func TestSessionStartInjectsMandatoryAndIndex(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "rule.md", mandatoryEntry)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	if err := os.WriteFile(filepath.Join(kbRoot, "INDEX.md"), []byte("# 知识索引\n\n- **Git 提交规范**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"SessionStart","session_id":"s1","cwd":%q}`, projDir)
	var out bytes.Buffer
	if code := HandleSessionStart(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "改完代码先写日志。") || !strings.Contains(got, "知识索引") {
		t.Fatalf("unexpected output %q", got)
	}
	if strings.Contains(got, "Conventional Commits") {
		t.Fatal("non-mandatory body should not be injected at session start")
	}
}

func TestPromptKeywordFallback(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交规范是什么"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "Conventional Commits") {
		t.Fatalf("expected git entry injected, got %q", out.String())
	}
}

func TestPromptUnregisteredProjectSilent(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	in := `{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"/nowhere","prompt":"git"}`
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
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s1","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, projDir, codeFile)
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
	post2 := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s2","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, projDir, codeFile)
	_ = HandlePostTool(strings.NewReader(post2))
	post3 := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s2","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, projDir, cl)
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
