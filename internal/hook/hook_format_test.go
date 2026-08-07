package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromptClaudeFormat claude 格式（ZCode）下 prompt 注入包成 hookSpecificOutput JSON。
func TestPromptClaudeFormat(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	if err := os.WriteFile(filepath.Join(kbRoot, "INDEX.md"), []byte("# 知识索引\n\n- **Git 提交规范**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交规范"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, FormatClaude); code != 0 {
		t.Fatalf("exit %d", code)
	}
	raw := bytes.TrimSpace(out.Bytes())
	if len(raw) == 0 || raw[0] != '{' {
		t.Fatalf("claude 格式输出必须是 JSON 对象: %q", out.String())
	}
	var resp struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("输出不是合法 JSON: %v (%q)", err, out.String())
	}
	if resp.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Fatalf("hookEventName = %q", resp.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(resp.HookSpecificOutput.AdditionalContext, "git.md") {
		t.Fatalf("additionalContext 应含注入内容: %q", resp.HookSpecificOutput.AdditionalContext)
	}
}

// TestPromptClaudeFormatEmpty 无法定位项目（cwd 未注册）时 fail-open：
// claude 格式也不输出任何字节（ZCode：stdout 为空 = 成功且无附加效果）。
func TestPromptClaudeFormatEmpty(t *testing.T) {
	setupProject(t)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"随便聊聊"}`, t.TempDir())
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out, FormatClaude); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("无内容时不应输出: %q", out.String())
	}
}

// TestStopClaudeFormatBlock claude 格式下 Stop 阻断走 stdout decision:block JSON、exit 0。
func TestStopClaudeFormatBlock(t *testing.T) {
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
	var stdout, stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr, &stdout, FormatClaude); code != 0 {
		t.Fatalf("claude 格式阻断应 exit 0, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("claude 格式不写 stderr: %q", stderr.String())
	}
	var resp struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v (%q)", err, stdout.String())
	}
	if resp.Decision != "block" || !strings.Contains(resp.Reason, "请补变更日志") {
		t.Fatalf("应阻断并带原因: %+v", resp)
	}
}

// TestStopPlainFormatUnchanged 纯文本格式保持 stderr + exit 2（kimi/pi 语义不变）。
func TestStopPlainFormatUnchanged(t *testing.T) {
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
	var stdout, stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr, &stdout, ""); code != 2 {
		t.Fatalf("纯文本格式阻断应 exit 2, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("纯文本格式不写 stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "请补变更日志") {
		t.Fatalf("stderr 应含原因: %q", stderr.String())
	}
}
