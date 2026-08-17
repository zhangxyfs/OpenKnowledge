package setupx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
)

func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
	if err := InstallSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openknowledge-init", "openknowledge-on", "openknowledge-off", "openknowledge-propose", "openknowledge-capture", "openknowledge-wiki"} {
		data, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	if !strings.Contains(string(data), filepath.ToSlash(`D:\bin\ok.exe`)) {
		t.Fatalf("%s missing baked exe path: %q", name, data)
	}
	}
}

func TestInstallWikiSkillContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
	if err := InstallSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "openknowledge-wiki", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"name: openknowledge-wiki", "wiki status", "wiki mark", filepath.ToSlash(`D:\bin\ok.exe`), "--type reference", "wiki"} {
		if !strings.Contains(s, want) {
			t.Fatalf("skill missing %q", want)
		}
	}
	if strings.Contains(s, "{{EXE}}") {
		t.Fatal("exe placeholder not baked")
	}
}

// ReasonixEnforceMode/SaveReasonixEnforceMode：缺省 mixed，保存后可读回，非法值报错。
func TestReasonixEnforceMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	if got := ReasonixEnforceMode(); got != "mixed" {
		t.Errorf("缺省应为 mixed，got %q", got)
	}
	if err := SaveReasonixEnforceMode("soft"); err != nil {
		t.Fatal(err)
	}
	if got := ReasonixEnforceMode(); got != "soft" {
		t.Errorf("保存后应为 soft，got %q", got)
	}
	// 磁盘上确实写入 [reasonix] enforce_mode
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `enforce_mode = "soft"`) {
		t.Errorf("config.toml 应含 enforce_mode = \"soft\"，实际:\n%s", data)
	}
	if err := SaveReasonixEnforceMode("歪值"); err == nil {
		t.Error("非法值应报错")
	}
	// 合法值 hard 保存后读回一致
	if err := SaveReasonixEnforceMode("hard"); err != nil {
		t.Fatal(err)
	}
	if got := ReasonixEnforceMode(); got != "hard" {
		t.Errorf("保存 hard 后应为 hard，got %q", got)
	}
}

// propose 技能模板必须包含"先分类"指引与 wiki 覆盖提示的联动说明。
func TestProposeSkillTemplateHasClassification(t *testing.T) {
	tpl := skillTemplates["openknowledge-propose"]
	for _, want := range []string{"先分类", "结构型", "openknowledge-wiki", "暂无 wiki 条目覆盖"} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("propose skill template missing %q", want)
		}
	}
}

// TestSaveEmbeddingProfilePreservesAPIKeyEnv：与 api_key 留空保留旧值同理，
// api_key_env 留空也应保留旧值（否则同名覆盖保存会静默丢掉 env 引用）。
func TestSaveEmbeddingProfilePreservesAPIKeyEnv(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	globalPath := filepath.Join(os.Getenv("OK_HOME"), "config.toml")
	p := config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: "http://h/v1", Model: "m1", APIKeyEnv: "OK_EMBED_KEY"}
	if err := SaveEmbeddingProfile(p, true); err != nil {
		t.Fatal(err)
	}
	// 同名覆盖：key 与 env 均留空 → 两者都应保留旧值
	p2 := config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: "http://h/v1", Model: "m2"}
	if err := SaveEmbeddingProfile(p2, false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.LoadMerged("", globalPath)
	ap := cfg.Embedding.ActiveProfile()
	if ap == nil || ap.APIKeyEnv != "OK_EMBED_KEY" || ap.Model != "m2" {
		t.Fatalf("同名覆盖应保留旧 api_key_env: %+v", cfg.Embedding)
	}
}

func TestEmbeddingProfileSaveActivateDelete(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	globalPath := filepath.Join(os.Getenv("OK_HOME"), "config.toml")
	// 新增并激活
	p := config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: "http://h/v1", Model: "m1", APIKey: "sk-1"}
	if err := SaveEmbeddingProfile(p, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		t.Fatal(err)
	}
	ap := cfg.Embedding.ActiveProfile()
	if ap == nil || ap.Name != "默认" || ap.Model != "m1" || ap.APIKey != "sk-1" {
		t.Fatalf("save+activate: %+v", cfg.Embedding)
	}
	// 同名覆盖且 api_key 留空保留旧 key
	p2 := config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: "http://h/v1", Model: "m2"}
	if err := SaveEmbeddingProfile(p2, true); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.LoadMerged("", globalPath)
	if ap := cfg.Embedding.ActiveProfile(); ap == nil || ap.Model != "m2" || ap.APIKey != "sk-1" {
		t.Fatalf("同名覆盖应保留旧 key: %+v", cfg.Embedding)
	}
	// 第二个 profile + 切换/停用
	if err := SaveEmbeddingProfile(config.EmbeddingProfile{Name: "本地", Type: "ollama", BaseURL: "http://localhost:11434", Model: "bge-m3"}, false); err != nil {
		t.Fatal(err)
	}
	if err := SetActiveEmbedding("本地"); err != nil {
		t.Fatal(err)
	}
	if err := SetActiveEmbedding("不存在"); err == nil {
		t.Fatal("切换不存在 profile 应报错")
	}
	cfg, _ = config.LoadMerged("", globalPath)
	if cfg.Embedding.Active != "本地" || len(cfg.Embedding.Profiles) != 2 {
		t.Fatalf("切换: %+v", cfg.Embedding)
	}
	// 删除使用中项 → Active 置空
	if err := DeleteEmbeddingProfile("本地"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.LoadMerged("", globalPath)
	if cfg.Embedding.Active != "" || len(cfg.Embedding.Profiles) != 1 {
		t.Fatalf("删除 active 应置空: %+v", cfg.Embedding)
	}
	if err := SetActiveEmbedding(""); err != nil {
		t.Fatal(err)
	}
}

// SaveEmbeddingModelsDir 写读回环；空串清回默认（omitempty，落盘不再出现该键）。
func TestSaveEmbeddingModelsDir(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	globalPath := filepath.Join(os.Getenv("OK_HOME"), "config.toml")
	dir := filepath.Join(t.TempDir(), "models")
	if err := SaveEmbeddingModelsDir(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil || cfg.Embedding.ModelsDir != dir {
		t.Fatalf("写入: %q %v", cfg.Embedding.ModelsDir, err)
	}
	if err := SaveEmbeddingModelsDir(""); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.LoadMerged("", globalPath)
	if cfg.Embedding.ModelsDir != "" {
		t.Fatalf("空串应清除: %q", cfg.Embedding.ModelsDir)
	}
	data, _ := os.ReadFile(globalPath)
	if strings.Contains(string(data), "models_dir") {
		t.Fatal("清除后不应残留 models_dir 键")
	}
}

// ListOllamaModels 解析 {base}/api/tags 的模型名列表。
func TestListOllamaModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("意外路径: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"models":[{"name":"bge-m3:latest"}]}`)
	}))
	defer srv.Close()
	names, err := ListOllamaModels(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "bge-m3:latest" {
		t.Fatalf("%v", names)
	}
}

// builtin profile 在 runtime 缺失（便携形态）时应报"仅安装版可用"。
func TestTestEmbeddingProfileBuiltinNoRuntime(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	p := config.EmbeddingProfile{Name: "内", Type: "builtin", Model: "qwen3-emb-0.6b-q8"}
	err := TestEmbeddingProfile(p, time.Second)
	if err == nil || !strings.Contains(err.Error(), "仅安装版可用") {
		t.Fatalf("%v", err)
	}
}

// 并发保存不同 profile 不得互相覆盖丢更新（v2.18.2 回归：Save* 曾裸读改写无锁）。
func TestConcurrentSaveEmbeddingProfile(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			errs <- SaveEmbeddingProfile(config.EmbeddingProfile{
				Name: "p" + string(rune('a'+i)), Type: "openai", BaseURL: "http://h/v1", Model: "m",
			}, false)
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.LoadMerged("", filepath.Join(os.Getenv("OK_HOME"), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Embedding.Profiles) != n {
		t.Fatalf("并发保存丢更新: %d/%d 个 profile", len(cfg.Embedding.Profiles), n)
	}
}
