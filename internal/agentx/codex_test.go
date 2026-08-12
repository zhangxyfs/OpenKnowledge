package agentx

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateCodex 隔离 codex 配置根与 ok 全局配置（HookTimeoutSec 读它）；
// CODEX_HOME 指向不存在目录防真实环境变量泄漏（OK_CODEX_HOME 优先于它）。
func isolateCodex(t *testing.T) string {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex-official"))
	home := filepath.Join(t.TempDir(), "codex")
	t.Setenv("OK_CODEX_HOME", home)
	return home
}

func codexTestExe() string { return `D:\develop\OpenKnowledge\dist\ok.exe` }

func TestCodexHomeEnvOverride(t *testing.T) {
	home := isolateCodex(t)
	if CodexHome() != home {
		t.Fatalf("CodexHome() = %q, want %q（OK_CODEX_HOME 应最优先）", CodexHome(), home)
	}
}

func TestCodexHomeOfficialEnv(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("OK_CODEX_HOME", "") // 置空落到下一级
	official := filepath.Join(t.TempDir(), "codex-official")
	t.Setenv("CODEX_HOME", official)
	if CodexHome() != official {
		t.Fatalf("CodexHome() = %q, want %q（CODEX_HOME 次之）", CodexHome(), official)
	}
}

func TestIsOKCodexHook(t *testing.T) {
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
		if got := isOKCodexHook(c.hook); got != c.want {
			t.Errorf("%s: isOKCodexHook() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCodexDetect(t *testing.T) {
	home := isolateCodex(t)
	a := codexAgent{}
	if a.Detect() {
		t.Error("目录不存在时不应 Detect")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.Detect() {
		t.Error("~/.codex 存在应 Detect")
	}
}

func TestCodexRegistered(t *testing.T) {
	isolateCodex(t)
	a, ok := Find("codex")
	if !ok {
		t.Fatal("codexAgent 未注册（init/Register 缺失）")
	}
	if a.ID() != "codex" {
		t.Errorf("ID() = %q, want codex", a.ID())
	}
}
