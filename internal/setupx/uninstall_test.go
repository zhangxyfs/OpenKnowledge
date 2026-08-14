package setupx

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/agentx"
	"openknowledge/internal/daemonx"
)

// setupUninstallEnv 构造 hooks 配置、技能、全局配置齐全的沙盒。
func setupUninstallEnv(t *testing.T) (kimiHome, okHome string) {
	t.Helper()
	kimiHome = t.TempDir()
	okHome = t.TempDir()
	t.Setenv("KIMI_CODE_HOME", kimiHome)
	t.Setenv("OK_HOME", okHome)
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))

	// kimi config：用户内容 + 标记块 + 用户内容
	kimiCfg := "default_model = \"kimi\"\n\n" + agentx.MarkerBegin + "\n[[hooks]]\nevent = \"Stop\"\n" + agentx.MarkerEnd + "\n\n[providers]\n"
	if err := os.WriteFile(filepath.Join(kimiHome, "config.toml"), []byte(kimiCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// 技能目录
	for name := range skillTemplates {
		dir := filepath.Join(agentx.SkillsHome(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 全局配置：embedding（profiles 形态）+ inject（inject 应保留）
	global := "[embedding]\nactive = \"默认\"\ntimeout_sec = 5\n\n[[embedding.profiles]]\nname = \"默认\"\ntype = \"openai\"\nbase_url = \"https://x\"\napi_key = \"sk\"\n\n[inject]\nmax_tokens = 2000\n"
	if err := os.WriteFile(filepath.Join(okHome, "config.toml"), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	return kimiHome, okHome
}

func TestUninstall(t *testing.T) {
	kimiHome, okHome := setupUninstallEnv(t)
	// KB 数据（应保留）
	projDir := filepath.Join(okHome, "projects", "demo")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(okHome, "registry.toml"), []byte("[[project]]\nname = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if !r.HooksRemoved || r.SkillsRemoved == 0 || !r.EmbeddingRemoved {
		t.Fatalf("unexpected result: %+v", r)
	}

	// hooks 标记块移除，用户内容保留
	data, _ := os.ReadFile(filepath.Join(kimiHome, "config.toml"))
	got := string(data)
	if strings.Contains(got, agentx.MarkerBegin) || strings.Contains(got, "[[hooks]]") {
		t.Fatalf("hooks block should be removed: %q", got)
	}
	if !strings.Contains(got, "default_model") || !strings.Contains(got, "[providers]") {
		t.Fatalf("user content should be preserved: %q", got)
	}

	// 技能目录删除
	for name := range skillTemplates {
		if _, err := os.Stat(filepath.Join(agentx.SkillsHome(), name)); !os.IsNotExist(err) {
			t.Fatalf("skill %s should be removed", name)
		}
	}

	// embedding 移除（含 [[embedding.profiles]] 子小节），inject 保留
	data, _ = os.ReadFile(filepath.Join(okHome, "config.toml"))
	got = string(data)
	if strings.Contains(got, "[embedding]") || strings.Contains(got, "[[embedding.profiles]]") || strings.Contains(got, "api_key") {
		t.Fatalf("embedding should be removed: %q", got)
	}
	if !strings.Contains(got, "[inject]") || !strings.Contains(got, "max_tokens = 2000") {
		t.Fatalf("inject should be preserved: %q", got)
	}

	// KB 数据保留
	if _, err := os.Stat(projDir); err != nil {
		t.Fatal("projects data must be preserved")
	}
	if _, err := os.Stat(filepath.Join(okHome, "registry.toml")); err != nil {
		t.Fatal("registry must be preserved")
	}
}

func TestUninstallRemovesLegacyOKHooks(t *testing.T) {
	kimiHome, _ := setupUninstallEnv(t)
	cfgPath := filepath.Join(kimiHome, "config.toml")
	data, _ := os.ReadFile(cfgPath)
	legacy := string(data) + `
[[hooks]]
event = "Stop"
command = "D:/old/ok.exe hook stop"
timeout = 5

[[hooks]]
event = "SessionStart"
command = "other-tool run"
timeout = 3
`
	if err := os.WriteFile(cfgPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if !r.HooksRemoved {
		t.Fatal("HooksRemoved should be true")
	}
	out, _ := os.ReadFile(cfgPath)
	got := string(out)
	if strings.Contains(got, "ok.exe hook") || strings.Contains(got, agentx.MarkerBegin) {
		t.Fatalf("ok hooks should be removed: %q", got)
	}
	if !strings.Contains(got, "other-tool run") {
		t.Fatalf("foreign hooks should be preserved: %q", got)
	}
}

func TestUninstallIdempotent(t *testing.T) {
	setupUninstallEnv(t)
	if _, err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	r, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if r.HooksRemoved || r.SkillsRemoved != 0 || r.EmbeddingRemoved {
		t.Fatalf("second uninstall should be no-op: %+v", r)
	}
}

func TestRemoveSectionDeletesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[embedding]\napi_key = \"sk\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveSection(path, "[embedding]")
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should be deleted when no content remains")
	}
}

func TestUninstallStopsDaemon(t *testing.T) {
	setupUninstallEnv(t)
	// 伪造一个活的"daemon"：httptest + daemon.json
	stopped := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/shutdown" {
			stopped = true
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := daemonx.Save(&daemonx.Info{PID: os.Getpid(), Port: port, Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("daemon should be asked to shutdown")
	}
	if _, err := daemonx.Load(); err == nil {
		t.Fatal("daemon.json should be removed")
	}
}
