package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/config"
)

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
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi")) // 隔离真实 kimi 配置，Init 会写 hooks
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
