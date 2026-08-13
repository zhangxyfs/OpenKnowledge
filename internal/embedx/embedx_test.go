package embedx

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
)

func TestClientNilWhenNoActive(t *testing.T) {
	if Client(config.Config{}) != nil {
		t.Fatal("未配置应为 nil")
	}
}

func TestClientOpenAI(t *testing.T) {
	cfg := config.Config{Embedding: config.Embedding{
		Active: "a", TimeoutSec: 5,
		Profiles: []config.EmbeddingProfile{{Name: "a", Type: "openai", BaseURL: "http://h/v1", Model: "m", APIKey: "k"}},
	}}
	c := Client(cfg)
	oc, ok := c.(*embed.OpenAIClient)
	if !ok || oc.BaseURL != "http://h/v1" || oc.APIKey != "k" || oc.Timeout != 5*time.Second {
		t.Fatalf("%+v", oc)
	}
	if c.ModelIdentity() != "openai:m@http://h/v1" {
		t.Fatal(c.ModelIdentity())
	}
}

func TestClientOllamaAppendsV1(t *testing.T) {
	p := config.EmbeddingProfile{Name: "o", Type: "ollama", BaseURL: "http://localhost:11434/", Model: "bge-m3"}
	c := ClientForProfile(p, time.Second)
	oc := c.(*embed.OpenAIClient)
	if oc.BaseURL != "http://localhost:11434/v1" || oc.APIKey != "" {
		t.Fatalf("%+v", oc)
	}
}

func TestClientMissingFieldsNil(t *testing.T) {
	if ClientForProfile(config.EmbeddingProfile{Name: "x", Type: "openai"}, time.Second) != nil {
		t.Fatal("缺 base_url/model 应为 nil")
	}
}

// 假 llama-server：httptest 同时服务 /health 与 /v1/embeddings
func fakeSidecar(t *testing.T) (port int, closeFn func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[{"embedding":[0.1,0.2],"index":0}]}`)
	})
	srv := httptest.NewServer(mux)
	port = srv.Listener.Addr().(*net.TCPAddr).Port
	return port, srv.Close
}

func TestBuiltinClientViaState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	port, closeFn := fakeSidecar(t)
	defer closeFn()
	m := embed.BuiltinModel{ID: "fake-x", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2, QueryPrefix: "Q:"}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	// 写 state（假装 daemon 已拉起）
	st := map[string]any{"pid": 1, "port": port, "model_id": "fake-x",
		"started_at": time.Now(), "last_used": time.Now()}
	data, _ := json.Marshal(st)
	os.WriteFile(filepath.Join(home, "embed-sidecar.json"), data, 0o644)

	p := config.EmbeddingProfile{Name: "内", Type: "builtin", Model: "fake-x"}
	c := ClientForProfile(p, 3*time.Second)
	if c == nil {
		t.Fatal("state 健康应返回客户端")
	}
	if c.ModelIdentity() != "builtin:fake-x" {
		t.Fatal(c.ModelIdentity())
	}
	if _, err := c.EmbedQuery(context.Background(), "ping"); err != nil {
		t.Fatal(err)
	}
	// Touch 生效：last_used 被刷新（读回不报错即覆盖路径走到）
	if got := embedsidecar.LoadState(); got == nil || got.LastUsed.IsZero() {
		t.Fatal("Touch 未生效")
	}
}

func TestBuiltinClientNotReadyWritesWant(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	m := embed.BuiltinModel{ID: "fake-y", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	p := config.EmbeddingProfile{Name: "内", Type: "builtin", Model: "fake-y"}
	if c := ClientForProfile(p, time.Second); c != nil {
		t.Fatal("无 state 应为 nil（降级）")
	}
	if !embedsidecar.WantPending() {
		t.Fatal("应写 want 请求 daemon 拉起")
	}
}
