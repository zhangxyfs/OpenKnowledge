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

func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	if err := InstallSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openknowledge-init", "openknowledge-on", "openknowledge-off"} {
		data, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(string(data), "D:/bin/ok.exe") {
			t.Fatalf("%s missing baked exe path: %q", name, data)
		}
	}
}
