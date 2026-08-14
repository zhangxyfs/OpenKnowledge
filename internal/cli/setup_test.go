package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
)

func TestSetupWithEmbeddingFlags(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "kimi"))
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
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
	if !strings.Contains(got, `active = "默认"`) || !strings.Contains(got, "[[embedding.profiles]]") ||
		!strings.Contains(got, `api_key = "sk-test"`) || !strings.Contains(got, `base_url = "https://g.example.com/v1"`) || !strings.Contains(got, `model = "m1"`) {
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
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
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
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
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
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
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
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
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
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
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
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
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

func TestSetupEmbeddingMenuSkip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	var out strings.Builder
	setupEmbedding(false, "", "", "", strings.NewReader("\n"), &out)
	if !strings.Contains(out.String(), "跳过") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("跳过不应写配置")
	}
}

func TestSetupEmbeddingMenuOpenAI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"embedding":[0.1],"index":0}]}`)
	}))
	defer srv.Close()
	in := "1\n" + srv.URL + "/v1\nm\nsk-x\n" // 选 1 → base_url → model → key
	var out strings.Builder
	setupEmbedding(false, "", "", "", strings.NewReader(in), &out)
	cfg, err := config.LoadMerged("", filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Type != "openai" || p.Model != "m" || p.ResolvedAPIKey() != "sk-x" {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(out.String(), "验证通过") {
		t.Fatal(out.String())
	}
}

func TestSetupEmbeddingMenuBuiltin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	m := embed.BuiltinModel{ID: "fake-cli", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	// 预置已下载模型（跳过真实下载）；默认模型目录已改为 <exe 所在目录>/models，
	// 这里经全局配置 models_dir 指回 OK_HOME/models（TOML 字面量字符串免转义）。
	modelsDir := filepath.Join(home, "models")
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(m.InstalledPath(modelsDir), []byte("fake"), 0o644)
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("[embedding]\nmodels_dir = '"+modelsDir+"'\n"), 0o600)
	// 假 runtime：测试二进制所在目录建 runtime/llama-server[.exe]
	exe, _ := os.Executable()
	rtDir := filepath.Join(filepath.Dir(exe), "runtime")
	os.MkdirAll(rtDir, 0o755)
	serverName := "llama-server"
	if runtime.GOOS == "windows" {
		serverName = "llama-server.exe"
	}
	os.WriteFile(filepath.Join(rtDir, serverName), []byte("x"), 0o755)
	t.Cleanup(func() { os.RemoveAll(rtDir) })

	idx := len(embed.BuiltinModels)          // 假模型在清单末尾
	in := "3\n" + strconv.Itoa(idx) + "\n\n" // 选 3 → 选模型 → 默认镜像
	var out strings.Builder
	setupEmbedding(false, "", "", "", strings.NewReader(in), &out)
	cfg, _ := config.LoadMerged("", filepath.Join(home, "config.toml"))
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Type != "builtin" || p.Model != "fake-cli" || p.Mirror != "hf-mirror" {
		t.Fatalf("%+v", p)
	}
	if !embedsidecar.WantPending() {
		t.Fatal("激活内置应写 want 标记")
	}
}
