package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/config"
)

func TestUpsertHooksBlockAppendAndReplace(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("default_model = \"kimi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := upsertHooksBlock(cfg, "BLOCK_V1\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if !strings.Contains(got, "default_model") || !strings.Contains(got, "BLOCK_V1") {
		t.Fatalf("append failed: %q", got)
	}
	if err := upsertHooksBlock(cfg, "BLOCK_V2\n"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(cfg)
	got = string(data)
	if strings.Contains(got, "BLOCK_V1") || !strings.Contains(got, "BLOCK_V2") {
		t.Fatalf("replace failed: %q", got)
	}
	if strings.Count(got, markerBegin) != 1 {
		t.Fatalf("duplicate marker block: %q", got)
	}
}

func TestUpsertHooksBlockNewFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := upsertHooksBlock(cfg, "BLOCK\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), "BLOCK") {
		t.Fatalf("unexpected %q", data)
	}
}

func TestUpsertHooksBlockCorruptMarker(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(markerBegin+"\nno end"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := upsertHooksBlock(cfg, "X\n"); err == nil {
		t.Fatal("expected corrupt marker error")
	}
}

func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	if err := installSkills(`D:\bin\ok.exe`); err != nil {
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

func TestSetupWithEmbeddingFlags(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "kimi"))
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	code := Setup([]string{"--embedding-base-url", "https://g.example.com/v1", "--embedding-model", "m1", "--embedding-key", "sk-test"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("OK_HOME"), "config.toml"))
	if err != nil {
		t.Fatalf("global config not written: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `api_key = "sk-test"`) || !strings.Contains(got, `base_url = "https://g.example.com/v1"`) || !strings.Contains(got, `model = "m1"`) {
		t.Fatalf("global config wrong: %q", got)
	}
}

func TestSetupInteractiveSkipKeepsGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "kimi"))
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	// 三行全回车 → 跳过 embedding 配置，且不得创建/破坏全局配置
	code := Setup(nil, strings.NewReader("\n\n\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("global config should not be created when skipped")
	}
}

func TestInitTemplateHasNoActiveEmbedding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init(nil, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	cfg, err := config.Load(filepath.Join(home, "projects", "demo", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Embedding.BaseURL != "" || cfg.Embedding.APIKey != "" || cfg.Embedding.APIKeyEnv != "" {
		t.Fatalf("project template should leave embedding empty for global inheritance, got %+v", cfg.Embedding)
	}
}
