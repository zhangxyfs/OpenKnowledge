package setupx

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
	if err := UpsertHooksBlock(cfg, HooksBlockFor(`D:\new\ok.exe`)); err != nil {
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
	initial := "default_model = \"kimi\"\n\n" + MarkerBegin + "\n" + HooksBlockFor("D:/old/ok.exe") + MarkerEnd + "\n\n[providers]\n"
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor("D:/new/ok.exe")); err != nil {
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
	initial := MarkerBegin + "\n" + HooksBlockFor("D:/x/ok.exe")
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor("D:/new/ok.exe")); err == nil {
		t.Fatal("expected corrupt marker error")
	}
	data, _ := os.ReadFile(cfg)
	if string(data) != initial {
		t.Fatalf("file should be unmodified on error: %q", data)
	}
}

func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	if err := InstallSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openknowledge-init", "openknowledge-on", "openknowledge-off", "openknowledge-propose", "openknowledge-capture", "openknowledge-wiki"} {
		data, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(string(data), "D:/bin/ok.exe") {
			t.Fatalf("%s missing baked exe path: %q", name, data)
		}
	}
}

func TestInstallWikiSkillContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	if err := InstallSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "openknowledge-wiki", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"name: openknowledge-wiki", "wiki status", "wiki mark", "D:/bin/ok.exe", "--type reference", "wiki"} {
		if !strings.Contains(s, want) {
			t.Fatalf("skill missing %q", want)
		}
	}
	if strings.Contains(s, "{{EXE}}") {
		t.Fatal("exe placeholder not baked")
	}
}
