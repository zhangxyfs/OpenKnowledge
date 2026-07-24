package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetCaptureAppendAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# 头部注释\n\n[[enforce]]\ntype = \"changelog_required\"\nmessage = \"x\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetCapture(path, "auto", 10, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "# 头部注释") || !strings.Contains(got, "[[enforce]]") {
		t.Fatalf("unrelated content lost: %q", got)
	}
	if !strings.Contains(got, "[capture]\nmode = \"auto\"\nturn_interval = 10") {
		t.Fatalf("capture block missing: %q", got)
	}
	// 替换而非叠加
	if err := SetCapture(path, "propose", 3, ""); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "[capture]") != 1 {
		t.Fatalf("duplicate capture block: %q", got)
	}
	if !strings.Contains(got, "mode = \"propose\"") || !strings.Contains(got, "turn_interval = 3") || strings.Contains(got, "turn_interval = 10") {
		t.Fatalf("replace failed: %q", got)
	}
	// 合并读取应生效
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Capture.Mode != "propose" || cfg.Capture.TurnInterval != 3 {
		t.Fatalf("merged config wrong: %+v", cfg.Capture)
	}
}

func TestSetCaptureMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetCapture(path, "auto", 7, "# header\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.HasPrefix(got, "# header\n") || !strings.Contains(got, "turn_interval = 7") {
		t.Fatalf("unexpected: %q", got)
	}
}
