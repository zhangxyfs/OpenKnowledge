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
