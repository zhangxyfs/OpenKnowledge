package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
	"openknowledge/internal/index"
	"openknowledge/internal/store"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	return NewHandler(t.TempDir(), "tok", nil)
}

// writeModelsDir 预写全局配置 [embedding] models_dir（TOML 字面量字符串，Windows 路径免转义）。
// 默认模型目录已改为 <exe 所在目录>/models，依赖 OK_HOME/models 的用例须显式配置。
func writeModelsDir(t *testing.T, dir string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(os.Getenv("OK_HOME"), "config.toml"),
		[]byte("[embedding]\nmodels_dir = '"+dir+"'\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func embGet(t *testing.T, h *Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/setup/embedding", nil)
	req.Header.Set("X-Ok-Token", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func embPost(t *testing.T, h *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("X-Ok-Token", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestProfileSaveActivateDeleteCycle(t *testing.T) {
	h := newTestHandler(t)
	w := embPost(t, h, "/api/setup/embedding/profile",
		`{"name":"a","type":"openai","base_url":"http://h/v1","model":"m","api_key":"k"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	st := embGet(t, h)
	profiles := st["profiles"].([]any)
	if len(profiles) != 1 || profiles[0].(map[string]any)["has_key"] != true {
		t.Fatalf("%v", profiles)
	}
	if st["active"] != "" {
		t.Fatal("保存不自动激活")
	}
	embPost(t, h, "/api/setup/embedding/active", `{"name":"a"}`)
	if st := embGet(t, h); st["active"] != "a" {
		t.Fatal("激活失败")
	}
	// key 留空保留
	embPost(t, h, "/api/setup/embedding/profile", `{"name":"a","type":"openai","base_url":"http://h/v1","model":"m2"}`)
	cfg, _ := config.LoadMerged("", filepath.Join(os.Getenv("OK_HOME"), "config.toml"))
	if cfg.Embedding.Profiles[0].ResolvedAPIKey() != "k" || cfg.Embedding.Profiles[0].Model != "m2" {
		t.Fatal("key 应保留、model 应更新")
	}
	req := httptest.NewRequest("DELETE", "/api/setup/embedding/profile", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("X-Ok-Token", "tok")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	st = embGet(t, h)
	if st["active"] != "" || len(st["profiles"].([]any)) != 0 {
		t.Fatal("删除使用中 profile 应清空 active")
	}
}

func TestActivateBuiltinRequiresDownload(t *testing.T) {
	h := newTestHandler(t)
	modelsDir := filepath.Join(os.Getenv("OK_HOME"), "models")
	writeModelsDir(t, modelsDir)
	m := embed.BuiltinModel{ID: "fake-g", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	embPost(t, h, "/api/setup/embedding/profile", `{"name":"内","type":"builtin","model":"fake-g","mirror":"hf-mirror"}`)
	w := embPost(t, h, "/api/setup/embedding/active", `{"name":"内"}`)
	if w.Code != 400 {
		t.Fatal("未下载应 400")
	}
	// 落盘模型后可激活
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(m.InstalledPath(modelsDir), []byte("fake"), 0o644)
	if w := embPost(t, h, "/api/setup/embedding/active", `{"name":"内"}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
}

func TestDownloadLifecycle(t *testing.T) {
	h := newTestHandler(t)
	modelsDir := filepath.Join(os.Getenv("OK_HOME"), "models")
	writeModelsDir(t, modelsDir)
	content := []byte("0123456789")
	sum := "84d89877f0d4041efb6bf91a16f0248f2fd573e6af05c19f96bedb9f882f7882" // sha256("0123456789")
	m := embed.BuiltinModel{ID: "fake-dl", Repo: "r/p", File: "m.gguf", Size: int64(len(content)), SHA256: sum, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()
	w := embPost(t, h, "/api/setup/embedding/download", `{"model_id":"fake-dl","mirror":"`+srv.URL+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := embGet(t, h)["download"].(map[string]any)
		if st["state"] == "done" {
			break
		}
		if st["state"] == "error" {
			t.Fatal(st["error"])
		}
		if time.Now().After(deadline) {
			t.Fatal("下载超时未完成")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !m.Installed(modelsDir) {
		t.Fatal("模型应已落盘")
	}
}

// TestDownloadRetryAfterError：下载失败（sha 不符）后同一模型可重试成功（换好源）。
func TestDownloadRetryAfterError(t *testing.T) {
	h := newTestHandler(t)
	writeModelsDir(t, filepath.Join(os.Getenv("OK_HOME"), "models"))
	content := []byte("0123456789")
	bad := embed.BuiltinModel{ID: "fake-retry", Repo: "r/p", File: "m.gguf", Size: int64(len(content)), SHA256: strings.Repeat("0", 64), Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, bad)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(content) }))
	defer srv.Close()
	// 第一次：sha256 不符 → error
	embPost(t, h, "/api/setup/embedding/download", `{"model_id":"fake-retry","mirror":"`+srv.URL+`"}`)
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := embGet(t, h)["download"].(map[string]any)
		if st["state"] == "error" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("未进入 error 态: %v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 第二次：修正 sha（换条目）重试 → 应能重新下载而非谎报 downloading
	good := embed.BuiltinModel{ID: "fake-retry", Repo: "r/p", File: "m.gguf", Size: int64(len(content)), SHA256: "84d89877f0d4041efb6bf91a16f0248f2fd573e6af05c19f96bedb9f882f7882", Pooling: "cls", Dim: 2}
	embed.BuiltinModels[len(embed.BuiltinModels)-1] = good
	embPost(t, h, "/api/setup/embedding/download", `{"model_id":"fake-retry","mirror":"`+srv.URL+`"}`)
	deadline = time.Now().Add(10 * time.Second)
	for {
		st := embGet(t, h)["download"].(map[string]any)
		if st["state"] == "done" {
			break
		}
		if s, _ := st["state"].(string); s == "error" {
			t.Fatalf("重试仍失败: %v", st["error"])
		}
		if time.Now().After(deadline) {
			t.Fatalf("重试未完成: %v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestEmbeddingGetIndexModel：?project= 时响应带 index_model（该项目 kb.db 的
// embedding_model）与 active_identity（使用中 profile 身份），供弹窗换模型警示条；
// 无 project / kb.db 缺失一律 fail-open 为空串。
func TestEmbeddingGetIndexModel(t *testing.T) {
	h := newTestHandler(t)
	home := os.Getenv("OK_HOME")
	mkProject(t, home, "demo")
	st := store.New(filepath.Join(home, "projects", "demo"))
	db, err := index.Open(st.KbPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetMeta("embedding_model", "builtin:qwen3-emb-0.6b-q8"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	embPost(t, h, "/api/setup/embedding/profile",
		`{"name":"a","type":"openai","base_url":"http://h/v1","model":"m","api_key":"k"}`)
	embPost(t, h, "/api/setup/embedding/active", `{"name":"a"}`)

	req := httptest.NewRequest("GET", "/api/setup/embedding?project=demo", nil)
	req.Header.Set("X-Ok-Token", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["active_identity"] != "openai:m@http://h/v1" {
		t.Fatalf("active_identity: %v", out["active_identity"])
	}
	if out["index_model"] != "builtin:qwen3-emb-0.6b-q8" {
		t.Fatalf("index_model: %v", out["index_model"])
	}
	// 无 project 参数 → index_model 空（fail-open），active_identity 仍给出
	out = embGet(t, h)
	if out["index_model"] != "" || out["active_identity"] != "openai:m@http://h/v1" {
		t.Fatalf("无项目时应 fail-open: %v %v", out["index_model"], out["active_identity"])
	}
	// 项目无 kb.db → index_model 空（不报错、不建库）
	mkProject(t, home, "empty")
	req = httptest.NewRequest("GET", "/api/setup/embedding?project=empty", nil)
	req.Header.Set("X-Ok-Token", "tok")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	out = map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["index_model"] != "" {
		t.Fatalf("kb.db 缺失应 fail-open: %v", out["index_model"])
	}
	if _, err := os.Stat(filepath.Join(home, "projects", "empty", "kb.db")); !os.IsNotExist(err) {
		t.Fatal("GET 不应创建 kb.db")
	}
}

// fakeEmbeddingsServer 返回 OpenAI 兼容 /v1/embeddings 假服务；hits 记录收到的请求。
func fakeEmbeddingsServer(t *testing.T, hits *[]*http.Request) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*hits = append(*hits, r.Clone(r.Context()))
		mu.Unlock()
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Write([]byte(`{"data":[{"embedding":[1.0,0.0],"index":0}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEmbeddingTestFormWins：测试按表单当前内容走——保存 profile 用 base_url=A，
// 表单改 base_url=B 测试应命中 B 而非 A。
func TestEmbeddingTestFormWins(t *testing.T) {
	h := newTestHandler(t)
	var hitsA, hitsB []*http.Request
	srvA := fakeEmbeddingsServer(t, &hitsA)
	srvB := fakeEmbeddingsServer(t, &hitsB)
	embPost(t, h, "/api/setup/embedding/profile",
		`{"name":"a","type":"openai","base_url":"`+srvA.URL+`/v1","model":"m","api_key":"k"}`)
	w := embPost(t, h, "/api/setup/embedding/test",
		`{"name":"a","type":"openai","base_url":"`+srvB.URL+`/v1","model":"m2","api_key":"k2"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["ok"] != true {
		t.Fatalf("应可用: %v", out)
	}
	if len(hitsB) != 1 {
		t.Fatalf("应命中 B: %d", len(hitsB))
	}
	if len(hitsA) != 0 {
		t.Fatalf("不应命中 A: %d", len(hitsA))
	}
	if got := hitsB[0].Header.Get("Authorization"); got != "Bearer k2" {
		t.Fatalf("表单 key 应优先: %q", got)
	}
}

// TestEmbeddingTestKeyFallback：api_key 留空 + 同名已保存 profile 带 key →
// 回退用已保存 key（"留空=用已保存"语义）。
func TestEmbeddingTestKeyFallback(t *testing.T) {
	h := newTestHandler(t)
	var hits []*http.Request
	srv := fakeEmbeddingsServer(t, &hits)
	embPost(t, h, "/api/setup/embedding/profile",
		`{"name":"a","type":"openai","base_url":"`+srv.URL+`/v1","model":"m","api_key":"saved-k"}`)
	// 表单改 model、api_key 留空
	w := embPost(t, h, "/api/setup/embedding/test",
		`{"name":"a","type":"openai","base_url":"`+srv.URL+`/v1","model":"m2"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["ok"] != true {
		t.Fatalf("应可用: %v", out)
	}
	if len(hits) != 1 {
		t.Fatalf("应命中假服务: %d", len(hits))
	}
	if got := hits[0].Header.Get("Authorization"); got != "Bearer saved-k" {
		t.Fatalf("应回退已保存 key: %q", got)
	}
}

// TestEmbeddingTestNameRequired：名称必填（前端先拦，后端兜底）。
func TestEmbeddingTestNameRequired(t *testing.T) {
	h := newTestHandler(t)
	w := embPost(t, h, "/api/setup/embedding/test", `{"type":"openai","base_url":"http://h/v1","model":"m"}`)
	if w.Code != 400 {
		t.Fatalf("空名称应 400: %d", w.Code)
	}
}

// TestModelsDirEndpoints：GET 暴露 models_dir/models_dir_default；POST models-dir
// 合法路径 200 且配置落盘（目录曾不存在则创建）、坏路径（父级是文件）400 且配置不变、
// 空串恢复默认；open-models-dir 经 openFolder 接缝收到生效目录。
func TestModelsDirEndpoints(t *testing.T) {
	h := newTestHandler(t)
	home := os.Getenv("OK_HOME")

	// GET：未配置时两键均为默认（<exe 所在目录>/models）
	st := embGet(t, h)
	def := embedsidecar.DefaultModelsDir()
	if st["models_dir"] != def || st["models_dir_default"] != def {
		t.Fatalf("默认键: %v %v", st["models_dir"], st["models_dir_default"])
	}

	// POST 合法路径（尚不存在 → 应创建）→ 200 + 配置落盘 + GET 生效值跟随
	dir := filepath.Join(home, "custom-models")
	body, _ := json.Marshal(map[string]string{"path": dir})
	if w := embPost(t, h, "/api/setup/embedding/models-dir", string(body)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatal("目录应已创建")
	}
	cfg, err := config.LoadMerged("", filepath.Join(home, "config.toml"))
	if err != nil || cfg.Embedding.ModelsDir != dir {
		t.Fatalf("配置落盘: %q %v", cfg.Embedding.ModelsDir, err)
	}
	if st := embGet(t, h); st["models_dir"] != dir {
		t.Fatalf("GET 生效值: %v", st["models_dir"])
	}

	// open-models-dir：接缝记录收到的目录（生效值 = 刚配置的目录）
	var got string
	old := openFolder
	openFolder = func(d string) error { got = d; return nil }
	t.Cleanup(func() { openFolder = old })
	if w := embPost(t, h, "/api/setup/embedding/open-models-dir", ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if got != dir {
		t.Fatalf("openFolder 收到 %q, want %q", got, dir)
	}

	// POST 坏路径（父级是文件，MkdirAll 必败）→ 400 且配置保持旧值
	f, err := os.Create(filepath.Join(home, "afile"))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	badBody, _ := json.Marshal(map[string]string{"path": filepath.Join(f.Name(), "x")})
	if w := embPost(t, h, "/api/setup/embedding/models-dir", string(badBody)); w.Code != 400 {
		t.Fatalf("坏路径应 400: %d %s", w.Code, w.Body)
	}
	cfg, _ = config.LoadMerged("", filepath.Join(home, "config.toml"))
	if cfg.Embedding.ModelsDir != dir {
		t.Fatal("400 不应改配置")
	}

	// 空串 = 恢复默认
	if w := embPost(t, h, "/api/setup/embedding/models-dir", `{"path":""}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
	cfg, _ = config.LoadMerged("", filepath.Join(home, "config.toml"))
	if cfg.Embedding.ModelsDir != "" {
		t.Fatal("空串应清默认")
	}
	if st := embGet(t, h); st["models_dir"] != def {
		t.Fatal("恢复后 GET 应回默认")
	}
}

func TestOllamaModelsProxy(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Write([]byte(`{"models":[{"name":"bge-m3:latest"},{"name":"nomic-embed-text:latest"}]}`))
	}))
	defer srv.Close()
	req := httptest.NewRequest("GET", "/api/setup/embedding/ollama-models?base_url="+srv.URL, nil)
	req.Header.Set("X-Ok-Token", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out["models"].([]any)) != 2 {
		t.Fatalf("%v", out)
	}
}
