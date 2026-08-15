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
	if cfg.Inject.MaxTokens != 800 || cfg.Retrieve.TopN != 2 || cfg.Embedding.TimeoutSec != 5 {
		t.Fatalf("unexpected defaults %+v", cfg)
	}
	if cfg.Retrieve.MinScore != 0.5 {
		t.Fatalf("default min_score = %v, want 0.5", cfg.Retrieve.MinScore)
	}
	if cfg.Retrieve.MinGap != 0.25 {
		t.Fatalf("default min_gap = %v, want 0.25", cfg.Retrieve.MinGap)
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
	// 全局旧版平铺配置迁移为 "默认" profile 后生效
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.BaseURL != "https://g.example.com/v1" || p.ResolvedAPIKey() != "gk" {
		t.Fatalf("global embedding should apply, got %+v", cfg.Embedding)
	}
	if cfg.Inject.MaxTokens != 800 {
		t.Fatalf("builtin default lost, got %+v", cfg.Inject)
	}
}

func TestLoadMergedMissingFiles(t *testing.T) {
	cfg, err := LoadMerged(filepath.Join(t.TempDir(), "a.toml"), filepath.Join(t.TempDir(), "b.toml"))
	if err != nil || cfg.Retrieve.TopN != 2 {
		t.Fatalf("missing files should yield defaults, got %+v err=%v", cfg, err)
	}
}

func TestResolvedAPIKey(t *testing.T) {
	t.Setenv("OK_TEST_KEY", "envkey")
	if got := (EmbeddingProfile{APIKey: "direct", APIKeyEnv: "OK_TEST_KEY"}).ResolvedAPIKey(); got != "direct" {
		t.Fatalf("direct key should win, got %q", got)
	}
	if got := (EmbeddingProfile{APIKeyEnv: "OK_TEST_KEY"}).ResolvedAPIKey(); got != "envkey" {
		t.Fatalf("env fallback failed, got %q", got)
	}
	if got := (EmbeddingProfile{}).ResolvedAPIKey(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestLegacyEmbeddingMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	old := "[embedding]\nbase_url = \"https://api.siliconflow.cn/v1\"\nmodel = \"BAAI/bge-m3\"\napi_key = \"sk-x\"\ntimeout_sec = 7\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Name != "默认" || p.Type != "openai" || p.BaseURL != "https://api.siliconflow.cn/v1" || p.Model != "BAAI/bge-m3" || p.ResolvedAPIKey() != "sk-x" {
		t.Fatalf("迁移结果: %+v", p)
	}
	if cfg.Embedding.BaseURL != "" || cfg.Embedding.Model != "" || cfg.Embedding.APIKey != "" {
		t.Fatal("迁移后平铺字段应清空")
	}
	if cfg.Embedding.TimeoutSec != 7 {
		t.Fatal("timeout_sec 保留")
	}
	if p.ModelIdentity() != "openai:BAAI/bge-m3@https://api.siliconflow.cn/v1" {
		t.Fatal(p.ModelIdentity())
	}
}

func TestProfilesMergeByName(t *testing.T) {
	dir := t.TempDir()
	global := "[embedding]\nactive = \"a\"\n[[embedding.profiles]]\nname = \"a\"\ntype = \"openai\"\nmodel = \"m1\"\n[[embedding.profiles]]\nname = \"b\"\ntype = \"ollama\"\nbase_url = \"http://localhost:11434\"\nmodel = \"bge-m3\"\n"
	project := "[[embedding.profiles]]\nname = \"a\"\ntype = \"openai\"\nmodel = \"m2\"\n"
	gp := filepath.Join(dir, "g.toml")
	pp := filepath.Join(dir, "p.toml")
	os.WriteFile(gp, []byte(global), 0o600)
	os.WriteFile(pp, []byte(project), 0o600)
	cfg, err := LoadMerged(pp, gp)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Embedding.Profiles) != 2 {
		t.Fatalf("按名合并应为 2 条: %+v", cfg.Embedding.Profiles)
	}
	for _, p := range cfg.Embedding.Profiles {
		if p.Name == "a" && p.Model != "m2" {
			t.Fatal("项目级同名覆盖")
		}
	}
	if cfg.Embedding.Active != "a" {
		t.Fatal("active 继承全局")
	}
}

func TestActiveProfileAndIdentity(t *testing.T) {
	var e Embedding
	if e.ActiveProfile() != nil {
		t.Fatal("空 active 应为 nil")
	}
	b := EmbeddingProfile{Name: "内", Type: "builtin", Model: "qwen3-emb-0.6b-q8"}
	if b.ModelIdentity() != "builtin:qwen3-emb-0.6b-q8" {
		t.Fatal(b.ModelIdentity())
	}
	o := EmbeddingProfile{Name: "o", Type: "ollama", BaseURL: "http://h:11434", Model: "bge-m3"}
	if o.ModelIdentity() != "ollama:bge-m3@http://h:11434" {
		t.Fatal(o.ModelIdentity())
	}
}

func TestCaptureDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Capture.Mode != "propose" || cfg.Capture.TurnInterval != 5 {
		t.Fatalf("capture defaults %+v", cfg.Capture)
	}
}

func TestWikiConfigDefaultAndOverride(t *testing.T) {
	def := Default()
	if def.Wiki.StaleCommits != 20 {
		t.Fatalf("default stale_commits = %d, want 20", def.Wiki.StaleCommits)
	}
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	os.WriteFile(global, []byte("[wiki]\nstale_commits = 50\n"), 0o644)
	os.WriteFile(project, []byte("[wiki]\nstale_commits = 0\n"), 0o644)
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Wiki.StaleCommits != 0 {
		t.Fatalf("project override: %d, want 0", cfg.Wiki.StaleCommits)
	}
	// 项目文件不写 wiki 时继承全局
	os.WriteFile(project, []byte(""), 0o644)
	cfg, _ = LoadMerged(project, global)
	if cfg.Wiki.StaleCommits != 50 {
		t.Fatalf("global inherit: %d, want 50", cfg.Wiki.StaleCommits)
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
