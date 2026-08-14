package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/gui"
	"openknowledge/internal/registry"
	"openknowledge/internal/store"
)

// setupProject 建一个注册项目并写入一个知识条目，返回项目目录。
func setupProject(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, "pi"))
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	t.Setenv("OPENAI_API_KEY", "")
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject("demo", proj); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	st := store.New(filepath.Join(home, "projects", "demo"))
	if err := st.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	body := "---\ntitle: Git 提交规范\ntype: note\nsummary: 提交规范\n---\n\n使用 Conventional Commits。\n"
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "git.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.ConfigPath(), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj
}

func newTestMux(t *testing.T) *httptest.Server {
	t.Helper()
	gh := gui.NewHandler(t.TempDir(), "tok", nil)
	return httptest.NewServer(NewMux(gh, "tok", "fp123"))
}

func promptEvent(t *testing.T, cwd, text string) []byte {
	t.Helper()
	ev := map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "s1",
		"cwd":             cwd,
		"prompt":          text,
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHealth(t *testing.T) {
	srv := newTestMux(t)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	req.Header.Set("X-Ok-Token", "tok")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["fingerprint"] != "fp123" {
		t.Fatalf("health %v", body)
	}
	// 无 token → 401
	resp2, err := srv.Client().Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp2.StatusCode)
	}
}

func TestHookPromptViaHTTP(t *testing.T) {
	proj := setupProject(t)
	srv := newTestMux(t)
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/hook/prompt", bytes.NewReader(promptEvent(t, proj, "git 提交规范")))
	req.Header.Set("X-Ok-Token", "tok")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var hr HookResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		t.Fatal(err)
	}
	if hr.Code != 0 || !strings.Contains(hr.Stdout, "提交规范") || !strings.Contains(hr.Stdout, "git.md") {
		t.Fatalf("hook prompt: %+v", hr)
	}
}

func TestHookStopBlocksViaHTTP(t *testing.T) {
	proj := setupProject(t)
	home := filepath.Dir(proj)
	cfgPath := filepath.Join(home, "projects", "demo", "config.toml")
	cfg := "\n[[enforce]]\ntype = \"changelog_required\"\ncode_globs = [\"**/*.go\"]\nchangelog_glob = \"docs/changelogs/**\"\nmessage = \"请补变更日志\"\n"
	f, _ := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o644)
	fmt.Fprint(f, cfg)
	f.Close()

	srv := newTestMux(t)
	defer srv.Close()
	post := func(path string, ev []byte) HookResponse {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(ev))
		req.Header.Set("X-Ok-Token", "tok")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var hr HookResponse
		if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
			t.Fatal(err)
		}
		return hr
	}
	codeFile := filepath.Join(proj, "main.go")
	toolEv, _ := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s9", "cwd": proj,
		"tool_name": "Write", "tool_input": map[string]string{"path": codeFile},
	})
	if hr := post("/api/hook/post-tool", toolEv); hr.Code != 0 {
		t.Fatalf("post-tool: %+v", hr)
	}
	stopEv, _ := json.Marshal(map[string]any{
		"hook_event_name": "Stop", "session_id": "s9", "cwd": proj,
	})
	hr := post("/api/hook/stop", stopEv)
	if hr.Code != 2 || !strings.Contains(hr.Stderr, "请补变更日志") {
		t.Fatalf("stop should block: %+v", hr)
	}
	if hr := post("/api/hook/stop", stopEv); hr.Code != 0 {
		t.Fatalf("second stop should pass: %+v", hr)
	}
}

// TestHookClaudeFormatViaHTTP ?format=claude 透传：prompt 注入包成 hookSpecificOutput
// JSON，stop 阻断走 stdout decision:block + code 0（ZCode 协议）。
func TestHookClaudeFormatViaHTTP(t *testing.T) {
	proj := setupProject(t)
	home := filepath.Dir(proj)
	cfgPath := filepath.Join(home, "projects", "demo", "config.toml")
	cfg := "\n[[enforce]]\ntype = \"changelog_required\"\ncode_globs = [\"**/*.go\"]\nchangelog_glob = \"docs/changelogs/**\"\nmessage = \"请补变更日志\"\n"
	f, _ := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o644)
	fmt.Fprint(f, cfg)
	f.Close()

	srv := newTestMux(t)
	defer srv.Close()
	post := func(path string, ev []byte) HookResponse {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(ev))
		req.Header.Set("X-Ok-Token", "tok")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var hr HookResponse
		if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
			t.Fatal(err)
		}
		return hr
	}

	// prompt：stdout 是 hookSpecificOutput JSON
	hr := post("/api/hook/prompt?format=claude", promptEvent(t, proj, "git 提交规范"))
	if hr.Code != 0 {
		t.Fatalf("prompt: %+v", hr)
	}
	var wrapper struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(hr.Stdout)), &wrapper); err != nil {
		t.Fatalf("prompt stdout 应为 JSON: %v (%q)", err, hr.Stdout)
	}
	if wrapper.HookSpecificOutput.HookEventName != "UserPromptSubmit" ||
		!strings.Contains(wrapper.HookSpecificOutput.AdditionalContext, "git.md") {
		t.Fatalf("prompt 包装内容不对: %+v", wrapper)
	}

	// stop：阻断走 stdout decision:block，code 0，stderr 空
	codeFile := filepath.Join(proj, "main.go")
	toolEv, _ := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s9", "cwd": proj,
		"tool_name": "Write", "tool_input": map[string]string{"file_path": codeFile},
	})
	if hr := post("/api/hook/post-tool?format=claude", toolEv); hr.Code != 0 {
		t.Fatalf("post-tool: %+v", hr)
	}
	stopEv, _ := json.Marshal(map[string]any{
		"hook_event_name": "Stop", "session_id": "s9", "cwd": proj,
	})
	hr = post("/api/hook/stop?format=claude", stopEv)
	if hr.Code != 0 || hr.Stderr != "" {
		t.Fatalf("claude stop 应 code 0 且 stderr 空: %+v", hr)
	}
	var block struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(hr.Stdout)), &block); err != nil {
		t.Fatalf("stop stdout 应为 JSON: %v (%q)", err, hr.Stdout)
	}
	if block.Decision != "block" || !strings.Contains(block.Reason, "请补变更日志") {
		t.Fatalf("stop 应阻断并带原因: %+v", block)
	}
}
