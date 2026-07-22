package project

import (
	"os"
	"path/filepath"
	"testing"

	"openknowledge/internal/registry"
)

func TestFromCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	proj := filepath.Join(home, "work", "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "demo", Paths: []string{proj}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	ctx, err := FromCwd(filepath.Join(proj, "sub", "dir"))
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Project.Name != "demo" {
		t.Fatalf("unexpected project %+v", ctx.Project)
	}
	if ctx.Config.Inject.MaxTokens != 1500 {
		t.Fatalf("expected default config, got %+v", ctx.Config)
	}
	if _, err := FromCwd(filepath.Join(home, "nowhere")); err == nil {
		t.Fatal("expected error for unregistered dir")
	}
}
