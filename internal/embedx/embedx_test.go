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

// TestClientForIndexTimeoutFloor：索引/重建路径超时下限 120s（批量重建不被
// 查询侧 5s 预算撞断）；用户配置高于下限时保留原值。
func TestClientForIndexTimeoutFloor(t *testing.T) {
	cfg := config.Config{Embedding: config.Embedding{
		Active: "a", TimeoutSec: 5,
		Profiles: []config.EmbeddingProfile{{Name: "a", Type: "openai", BaseURL: "http://h/v1", Model: "m", APIKey: "k"}},
	}}
	oc, ok := ClientForIndex(cfg).(*embed.OpenAIClient)
	if !ok || oc.Timeout != 120*time.Second {
		t.Fatalf("索引路径超时下限应为 120s: %+v", oc)
	}
	cfg.Embedding.TimeoutSec = 300
	oc = ClientForIndex(cfg).(*embed.OpenAIClient)
	if oc.Timeout != 300*time.Second {
		t.Fatalf("高于下限应保留: %v", oc.Timeout)
	}
	if ClientForIndex(config.Config{}) != nil {
		t.Fatal("未配置应为 nil")
	}
}

// TestClientOllamaStripsDuplicateV1：用户把 base_url 填成 …/v1（带或不带尾斜杠）
// 时不再拼出 …/v1/v1（404 根因）。
func TestClientOllamaStripsDuplicateV1(t *testing.T) {
	for _, base := range []string{"http://localhost:11434/v1", "http://localhost:11434/v1/", "http://localhost:11434", "http://localhost:11434/"} {
		p := config.EmbeddingProfile{Name: "o", Type: "ollama", BaseURL: base, Model: "bge-m3"}
		oc := ClientForProfile(p, time.Second).(*embed.OpenAIClient)
		if oc.BaseURL != "http://localhost:11434/v1" {
			t.Fatalf("%s → %s", base, oc.BaseURL)
		}
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

// timeout_sec 显式配 0 时钳回默认 5s（v2.18.2 回归：http.Client 零值=永不超时，
// hook 检索 / ok search / ok doctor 会无限挂起）。
func TestClientZeroTimeoutClamped(t *testing.T) {
	cfg := config.Config{Embedding: config.Embedding{
		Active: "a", TimeoutSec: 0,
		Profiles: []config.EmbeddingProfile{{Name: "a", Type: "openai", BaseURL: "http://h/v1", Model: "m"}},
	}}
	oc, ok := Client(cfg).(*embed.OpenAIClient)
	if !ok || oc.Timeout != 5*time.Second {
		t.Fatalf("timeout_sec=0 应钳回 5s: %+v", oc)
	}
}
