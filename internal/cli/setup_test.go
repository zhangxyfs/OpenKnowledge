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
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
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
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
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
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
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

func TestSetupUnknownAgent(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	var out, errBuf bytes.Buffer
	code := Setup([]string{"--agent", "nope"}, strings.NewReader(""), &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "未知 agent") {
		t.Fatalf("stderr should mention unknown agent: %q", errBuf.String())
	}
}

func TestSetupAgentKimiOnly(t *testing.T) {
	kimiHome := t.TempDir()
	piHome := t.TempDir()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", kimiHome)
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	var out, errBuf bytes.Buffer
	code := Setup([]string{"--agent", "kimi"}, strings.NewReader("\n\n\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(kimiHome, "config.toml")); err != nil {
		t.Fatal("kimi hooks should be written")
	}
	if _, err := os.Stat(filepath.Join(piHome, "extensions", "openknowledge.ts")); !os.IsNotExist(err) {
		t.Fatal("pi extension should NOT be written with --agent kimi")
	}
}

func TestSetupAllDetectedAgents(t *testing.T) {
	kimiHome := t.TempDir()
	piHome := t.TempDir()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", kimiHome)
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	var out, errBuf bytes.Buffer
	code := Setup(nil, strings.NewReader("\n\n\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(kimiHome, "config.toml")); err != nil {
		t.Fatal("kimi hooks should be written")
	}
	if _, err := os.Stat(filepath.Join(piHome, "extensions", "openknowledge.ts")); err != nil {
		t.Fatal("pi extension should be written")
	}
}

func TestSetupAgentUndetectedStillWrites(t *testing.T) {
	piHome := filepath.Join(t.TempDir(), "pi-agent") // 不存在 → Detect()=false
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	var out, errBuf bytes.Buffer
	code := Setup([]string{"--agent", "pi"}, strings.NewReader("\n\n\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "未检测到") {
		t.Fatalf("stderr should warn about undetected agent: %q", errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(piHome, "extensions", "openknowledge.ts")); err != nil {
		t.Fatal("pi extension should be written even when undetected")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("KIMI_CODE_HOME"), "config.toml")); !os.IsNotExist(err) {
		t.Fatal("kimi config should NOT be written with --agent pi")
	}
}
