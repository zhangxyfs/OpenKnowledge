package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGateAppendAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	// 既有 [retrieve] 顶级小节 + 注释 + [[enforce]]——追加子表不得动它们
	original := "# 头部注释\n\n[retrieve]\ntop_n = 3\n\n[[enforce]]\ntype = \"changelog_required\"\nmessage = \"x\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetGate(path, true, []string{"走起", "go ahead"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"# 头部注释", "[retrieve]\ntop_n = 3", "[[enforce]]", "[retrieve.gate]\nenabled = true\nextra_phrases = [\"走起\", \"go ahead\"]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q: %q", want, got)
		}
	}
	// 替换而非叠加；边界判定不得吞掉后面的 [[enforce]]
	if err := SetGate(path, false, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "[retrieve.gate]") != 1 {
		t.Fatalf("duplicate gate block: %q", got)
	}
	if !strings.Contains(got, "enabled = false") || !strings.Contains(got, "extra_phrases = []") || strings.Contains(got, "走起") {
		t.Fatalf("replace failed: %q", got)
	}
	if !strings.Contains(got, "[[enforce]]") || !strings.Contains(got, "[retrieve]\ntop_n = 3") {
		t.Fatalf("unrelated content lost: %q", got)
	}
	// 合并读取应生效
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Gate.Enabled || len(cfg.Retrieve.Gate.ExtraPhrases) != 0 {
		t.Fatalf("merged config wrong: %+v", cfg.Retrieve.Gate)
	}
}

func TestSetGateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetGate(path, true, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got := string(data); !strings.Contains(got, "[retrieve.gate]") || !strings.Contains(got, `extra_phrases = ["x"]`) {
		t.Fatalf("unexpected: %q", got)
	}
}
