package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// isolateClaude 隔离 claude/codepilot 配置根与 ok 全局配置（HookTimeoutSec 读它）。
func isolateClaude(t *testing.T) string {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	home := filepath.Join(t.TempDir(), "claude")
	t.Setenv("OK_CLAUDE_HOME", home)
	return home
}

func claudeTestExe() string { return `D:\develop\OpenKnowledge\dist\ok.exe` }

func TestClaudeHomeEnvOverride(t *testing.T) {
	home := isolateClaude(t)
	if ClaudeHome() != home {
		t.Fatalf("ClaudeHome() = %q, want %q", ClaudeHome(), home)
	}
}

func TestIsOKClaudeHook(t *testing.T) {
	cases := []struct {
		name string
		hook map[string]any
		want bool
	}{
		{"prompt", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude`}, true},
		{"post-tool", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook post-tool claude`}, true},
		{"stop", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook stop claude`}, true},
		{"尾部空白容忍", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude  `}, true},
		{"非 command 类型", map[string]any{"type": "process", "command": `"D:/x/ok.exe" hook prompt claude`}, false},
		{"非 ok 命令", map[string]any{"type": "command", "command": "echo hi"}, false},
		{"相邻词误匹配", map[string]any{"type": "command", "command": "myhook prompt claude"}, false},
		{"缺 claude 协议段", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt`}, false},
	}
	for _, c := range cases {
		if got := isOKClaudeHook(c.hook); got != c.want {
			t.Errorf("%s: isOKClaudeHook() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClaudeInstallHooks(t *testing.T) {
	home := isolateClaude(t)
	// 预置含第三方内容的 settings.json
	sp := filepath.Join(home, "settings.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := `{"theme":"light","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	if err := os.WriteFile(sp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := claudeAgent{}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("写回后 JSON 非法: %v", err)
	}
	if cfg["theme"] != "light" {
		t.Error("既有未知字段 theme 被丢")
	}
	events, _ := cfg["hooks"].(map[string]any)
	// 第三方 PreToolUse 保留
	pre, _ := events["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Error("第三方 PreToolUse 组被删")
	}
	// 三事件各多出一个 ok 组
	wantCmd := map[string]string{
		"UserPromptSubmit": `"D:/develop/OpenKnowledge/dist/ok.exe" hook prompt claude`,
		"PostToolUse":      `"D:/develop/OpenKnowledge/dist/ok.exe" hook post-tool claude`,
		"Stop":             `"D:/develop/OpenKnowledge/dist/ok.exe" hook stop claude`,
	}
	wantMatcher := map[string]string{"UserPromptSubmit": "*", "PostToolUse": "Write|Edit", "Stop": "*"}
	wantTimeout := float64(HookTimeoutSec())
	for ev, cmd := range wantCmd {
		groups, _ := events[ev].([]any)
		found := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if m, _ := gm["matcher"].(string); m != wantMatcher[ev] {
				continue
			}
			for _, h := range gm["hooks"].([]any) {
				hm, _ := h.(map[string]any)
				if c, _ := hm["command"].(string); c == cmd {
					if to, _ := hm["timeout"].(float64); to != wantTimeout {
						t.Errorf("%s: timeout = %v, want %v", ev, to, wantTimeout)
					}
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s: 未找到期望的 ok hook 组", ev)
		}
	}
	// 备份已生成
	if _, err := os.Stat(sp + ".bak-openknowledge"); err != nil {
		t.Error("未生成 .bak-openknowledge 备份")
	}
}

func TestClaudeInstallIdempotent(t *testing.T) {
	home := isolateClaude(t)
	a := claudeAgent{}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(claudeTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "settings.json"))
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	groups, _ := events["UserPromptSubmit"].([]any)
	if len(groups) != 1 {
		t.Fatalf("重复安装产生堆积: UserPromptSubmit 组数 = %d, want 1", len(groups))
	}
}

func TestClaudeCorruptSettings(t *testing.T) {
	home := isolateClaude(t)
	sp := filepath.Join(home, "settings.json")
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(sp, []byte("{broken"), 0o644)
	a := claudeAgent{}
	if err := a.InstallHooks(claudeTestExe()); err == nil {
		t.Fatal("损坏文件应报错")
	}
	data, _ := os.ReadFile(sp)
	if string(data) != "{broken" {
		t.Error("损坏文件被覆盖")
	}
}

func TestClaudeDetect(t *testing.T) {
	home := isolateClaude(t)
	a := claudeAgent{}
	if a.Detect() {
		t.Error("两目录均不存在时不应 Detect")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.Detect() {
		t.Error("~/.claude 存在应 Detect")
	}
}
