package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	return NewHandler(t.TempDir(), "tok", nil)
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
	m := embed.BuiltinModel{ID: "fake-g", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	embPost(t, h, "/api/setup/embedding/profile", `{"name":"内","type":"builtin","model":"fake-g","mirror":"hf-mirror"}`)
	w := embPost(t, h, "/api/setup/embedding/active", `{"name":"内"}`)
	if w.Code != 400 {
		t.Fatal("未下载应 400")
	}
	// 落盘模型后可激活
	os.MkdirAll(filepath.Join(os.Getenv("OK_HOME"), "models"), 0o755)
	os.WriteFile(m.InstalledPath(filepath.Join(os.Getenv("OK_HOME"), "models")), []byte("fake"), 0o644)
	if w := embPost(t, h, "/api/setup/embedding/active", `{"name":"内"}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
}

func TestDownloadLifecycle(t *testing.T) {
	h := newTestHandler(t)
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
	if !m.Installed(filepath.Join(os.Getenv("OK_HOME"), "models")) {
		t.Fatal("模型应已落盘")
	}
}

// TestDownloadRetryAfterError：下载失败（sha 不符）后同一模型可重试成功（换好源）。
func TestDownloadRetryAfterError(t *testing.T) {
	h := newTestHandler(t)
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
