package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inject.MaxTokens != 1500 || cfg.Retrieve.TopN != 3 || cfg.Embedding.TimeoutSec != 5 {
		t.Fatalf("unexpected defaults %+v", cfg)
	}
}

func TestLoadMergesOverDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[retrieve]
top_n = 5

[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.TopN != 5 || cfg.Retrieve.Alpha != 1.0 {
		t.Fatalf("merge failed %+v", cfg.Retrieve)
	}
	if len(cfg.Enforce) != 1 || cfg.Enforce[0].ChangelogGlob != "docs/changelogs/**" {
		t.Fatalf("enforce %+v", cfg.Enforce)
	}
}

func TestLoadMergedPrecedence(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	if err := os.WriteFile(global, []byte("[retrieve]\ntop_n = 5\n[embedding]\nbase_url = \"https://g.example.com/v1\"\napi_key = \"gk\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("[retrieve]\ntop_n = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.TopN != 9 {
		t.Fatalf("project should override global, got %d", cfg.Retrieve.TopN)
	}
	if cfg.Embedding.BaseURL != "https://g.example.com/v1" || cfg.Embedding.APIKey != "gk" {
		t.Fatalf("global embedding should apply, got %+v", cfg.Embedding)
	}
	if cfg.Inject.MaxTokens != 1500 {
		t.Fatalf("builtin default lost, got %+v", cfg.Inject)
	}
}

func TestLoadMergedMissingFiles(t *testing.T) {
	cfg, err := LoadMerged(filepath.Join(t.TempDir(), "a.toml"), filepath.Join(t.TempDir(), "b.toml"))
	if err != nil || cfg.Retrieve.TopN != 3 {
		t.Fatalf("missing files should yield defaults, got %+v err=%v", cfg, err)
	}
}

func TestResolvedAPIKey(t *testing.T) {
	t.Setenv("OK_TEST_KEY", "envkey")
	if got := (Embedding{APIKey: "direct", APIKeyEnv: "OK_TEST_KEY"}).ResolvedAPIKey(); got != "direct" {
		t.Fatalf("direct key should win, got %q", got)
	}
	if got := (Embedding{APIKeyEnv: "OK_TEST_KEY"}).ResolvedAPIKey(); got != "envkey" {
		t.Fatalf("env fallback failed, got %q", got)
	}
	if got := (Embedding{}).ResolvedAPIKey(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestCaptureDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Capture.Mode != "propose" || cfg.Capture.TurnInterval != 5 {
		t.Fatalf("capture defaults %+v", cfg.Capture)
	}
}

func TestCaptureLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[capture]\nmode = \"auto\"\nturn_interval = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Capture.Mode != "auto" || cfg.Capture.TurnInterval != 9 {
		t.Fatalf("capture load %+v", cfg.Capture)
	}
}
