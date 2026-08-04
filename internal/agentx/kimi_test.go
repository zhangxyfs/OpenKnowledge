package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertHooksBlockAppendAndReplace(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("default_model = \"kimi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, "BLOCK_V1\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if !strings.Contains(got, "default_model") || !strings.Contains(got, "BLOCK_V1") {
		t.Fatalf("append failed: %q", got)
	}
	if err := UpsertHooksBlock(cfg, "BLOCK_V2\n"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(cfg)
	got = string(data)
	if strings.Contains(got, "BLOCK_V1") || !strings.Contains(got, "BLOCK_V2") {
		t.Fatalf("replace failed: %q", got)
	}
	if strings.Count(got, MarkerBegin) != 1 {
		t.Fatalf("duplicate marker block: %q", got)
	}
}

func TestUpsertHooksBlockStripsLegacyOKHooks(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	legacy := `default_model = "kimi"

[[hooks]]
event = "UserPromptSubmit"
command = "D:/old/ok.exe hook prompt"
timeout = 10

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "D:/old/ok.exe hook post-tool"
timeout = 5

[[hooks]]
event = "Stop"
command = "ok hook stop"
timeout = 5

[[hooks]]
event = "SessionStart"
command = "other-tool run"
timeout = 3

[providers]
`
	if err := os.WriteFile(cfg, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor(`D:\new\ok.exe`, 10)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if strings.Contains(got, "D:/old/ok.exe") || strings.Contains(got, `"ok hook stop"`) {
		t.Fatalf("legacy ok hooks should be stripped: %q", got)
	}
	if !strings.Contains(got, "other-tool run") || !strings.Contains(got, "default_model") || !strings.Contains(got, "[providers]") {
		t.Fatalf("user content should be preserved: %q", got)
	}
	if c := strings.Count(got, MarkerBegin); c != 1 {
		t.Fatalf("expected exactly one marker block, got %d: %q", c, got)
	}
	if c := strings.Count(got, "[[hooks]]"); c != 4 {
		t.Fatalf("expected 3 new + 1 foreign hook tables, got %d: %q", c, got)
	}
	if !strings.Contains(got, "D:/new/ok.exe hook prompt") {
		t.Fatalf("new exe path should overwrite the old one: %q", got)
	}
}

func TestUpsertHooksBlockNewFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := UpsertHooksBlock(cfg, "BLOCK\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), "BLOCK") {
		t.Fatalf("unexpected %q", data)
	}
}

func TestUpsertHooksBlockCorruptMarker(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(MarkerBegin+"\nno end"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, "X\n"); err == nil {
		t.Fatal("expected corrupt marker error")
	}
}

func TestUpsertHooksBlockReplacesMarkerInPlace(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	initial := "default_model = \"kimi\"\n\n" + MarkerBegin + "\n" + HooksBlockFor("D:/old/ok.exe", 10) + MarkerEnd + "\n\n[providers]\n"
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor("D:/new/ok.exe", 10)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if c := strings.Count(got, MarkerBegin); c != 1 {
		t.Fatalf("expected exactly one marker block, got %d: %q", c, got)
	}
	if !strings.Contains(got, "D:/new/ok.exe") || strings.Contains(got, "D:/old/ok.exe") {
		t.Fatalf("exe path not replaced: %q", got)
	}
	if strings.Index(got, MarkerBegin) > strings.Index(got, "[providers]") {
		t.Fatalf("marker block should stay before [providers] (in-place replace): %q", got)
	}
	if !strings.Contains(got, `default_model = "kimi"`) {
		t.Fatalf("default_model lost: %q", got)
	}
}

func TestUpsertHooksBlockCorruptMarkerWithOKCommands(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	initial := MarkerBegin + "\n" + HooksBlockFor("D:/x/ok.exe", 10)
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor("D:/new/ok.exe", 10)); err == nil {
		t.Fatal("expected corrupt marker error")
	}
	data, _ := os.ReadFile(cfg)
	if string(data) != initial {
		t.Fatalf("file should be unmodified on error: %q", data)
	}
}

func TestEnsureHooksBlock(t *testing.T) {
	t.Run("markers present: untouched", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "config.toml")
		initial := "default_model = \"kimi\"\n\n" + MarkerBegin + "\n" + HooksBlockFor("D:/old/ok.exe", 10) + MarkerEnd + "\n"
		if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := EnsureHooksBlock(cfg, "D:/new/ok.exe"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(cfg)
		if string(data) != initial {
			t.Fatalf("file should be untouched when markers exist: %q", data)
		}
		if _, err := os.Stat(cfg + ".bak-openknowledge"); !os.IsNotExist(err) {
			t.Fatal("no backup should be written when untouched")
		}
	})

	t.Run("markers stripped by kimi-code: self-heal", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "config.toml")
		// kimi-code 删掉标记注释后剩下的孤儿 ok hook 表 + 其它工具的 hook
		orphan := "default_model = \"kimi\"\n\n" + HooksBlockFor("D:/old/ok.exe", 10) + `
[[hooks]]
event = "SessionStart"
command = "other-tool run"
timeout = 3
`
		if err := os.WriteFile(cfg, []byte(orphan), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := EnsureHooksBlock(cfg, "D:/new/ok.exe"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(cfg)
		got := string(data)
		if c := strings.Count(got, MarkerBegin); c != 1 {
			t.Fatalf("expected exactly one marker block after heal, got %d: %q", c, got)
		}
		if c := strings.Count(got, "[[hooks]]"); c != 4 {
			t.Fatalf("expected 3 new + 1 foreign hook tables, got %d: %q", c, got)
		}
		if !strings.Contains(got, "D:/new/ok.exe hook prompt") || strings.Contains(got, "D:/old/ok.exe") {
			t.Fatalf("orphan ok hooks should be replaced by new block: %q", got)
		}
		if !strings.Contains(got, "other-tool run") || !strings.Contains(got, `default_model = "kimi"`) {
			t.Fatalf("user content should be preserved: %q", got)
		}
		bak, err := os.ReadFile(cfg + ".bak-openknowledge")
		if err != nil || string(bak) != orphan {
			t.Fatalf("backup should hold the pre-heal content: %v %q", err, bak)
		}
	})

	t.Run("config missing: error for fail-open caller", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "config.toml")
		if err := EnsureHooksBlock(cfg, "D:/new/ok.exe"); err == nil {
			t.Fatal("expected error for missing config")
		}
	})
}

func TestKimiAgentInstallDetectRemove(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	a, ok := Find("kimi")
	if !ok {
		t.Fatal("kimi agent not registered")
	}
	if !a.Detect() {
		t.Fatal("Detect should be true when KIMI_CODE_HOME dir exists")
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false before install")
	}
	if err := a.InstallHooks(`D:\x\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("HooksInstalled should be true after install")
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false after remove")
	}
}

func TestKimiAgentDetectFalse(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (kimiAgent{}).Detect() {
		t.Fatal("Detect should be false when dir missing")
	}
}
