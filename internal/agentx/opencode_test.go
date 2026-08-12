package agentx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpencodeHomePrecedence(t *testing.T) {
	// OK_OPENCODE_HOME（ok 自留测试口）最优先
	t.Setenv("OK_OPENCODE_HOME", `D:\t\ok-port`)
	t.Setenv("OPENCODE_CONFIG_DIR", `D:\t\official`)
	t.Setenv("XDG_CONFIG_HOME", `D:\t\xdg`)
	if got := OpencodeHome(); got != `D:\t\ok-port` {
		t.Fatalf("OK_OPENCODE_HOME 应最优先, got %q", got)
	}

	// OPENCODE_CONFIG_DIR（opencode 官方覆盖）次之
	t.Setenv("OK_OPENCODE_HOME", "")
	if got := OpencodeHome(); got != `D:\t\official` {
		t.Fatalf("OPENCODE_CONFIG_DIR 次之, got %q", got)
	}

	// XDG_CONFIG_HOME/opencode 再次
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	if got, want := OpencodeHome(), filepath.Join(`D:\t\xdg`, "opencode"); got != want {
		t.Fatalf("XDG_CONFIG_HOME/opencode, got %q want %q", got, want)
	}

	// 默认 ~/.config/opencode
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	if got, want := OpencodeHome(), filepath.Join(home, ".config", "opencode"); got != want {
		t.Fatalf("默认 ~/.config/opencode, got %q want %q", got, want)
	}
}

func TestOpencodePluginPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_OPENCODE_HOME", home)
	want := filepath.Join(home, "plugins", "openknowledge.ts")
	if got := opencodePluginPath(); got != want {
		t.Fatalf("opencodePluginPath = %q, want %q", got, want)
	}
}
