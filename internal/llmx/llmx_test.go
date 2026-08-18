package llmx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
)

func TestChatOpenAI(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "pong", "reasoning_content": "想了下"}}},
		})
	}))
	defer srv.Close()
	c := New(config.LLMProfile{Kind: "openai", BaseURL: srv.URL + "/v1/", Model: "m1", APIKey: "k1"}, 0)
	out, reasoning, err := c.Chat(context.Background(), "sys", "usr", 100)
	if err != nil || out != "pong" {
		t.Fatalf("got (%q, %v)", out, err)
	}
	if reasoning != "想了下" {
		t.Fatalf("reasoning_content 应被捕获, got %q", reasoning)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q（base_url 尾部斜杠应被规整）", gotPath)
	}
	if gotAuth != "Bearer k1" {
		t.Fatalf("auth = %q", gotAuth)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 || gotBody["model"] != "m1" {
		t.Fatalf("body 结构不对: %v", gotBody)
	}
}

func TestChatAnthropic(t *testing.T) {
	var gotKey, gotVer, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVer = r.Header.Get("anthropic-version")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"text": "pong"}},
		})
	}))
	defer srv.Close()
	c := New(config.LLMProfile{Kind: "anthropic", BaseURL: srv.URL, Model: "claude-x", APIKey: "ak"}, 0)
	out, _, err := c.Chat(context.Background(), "sys", "usr", 100)
	if err != nil || out != "pong" {
		t.Fatalf("got (%q, %v)", out, err)
	}
	if gotPath != "/v1/messages" || gotKey != "ak" || gotVer != "2023-06-01" {
		t.Fatalf("path=%q key=%q ver=%q", gotPath, gotKey, gotVer)
	}
	if gotBody["system"] != "sys" {
		t.Fatalf("anthropic system 应为顶层字段: %v", gotBody)
	}
}

func TestChatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()
	c := New(config.LLMProfile{Kind: "openai", BaseURL: srv.URL, Model: "m", APIKey: "bad"}, 0)
	_, _, err := c.Chat(context.Background(), "s", "u", 10)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("应透传状态码, got %v", err)
	}
}

func TestUnknownKind(t *testing.T) {
	c := New(config.LLMProfile{Kind: "gemini"}, 0)
	if _, _, err := c.Chat(context.Background(), "s", "u", 10); err == nil {
		t.Fatal("未知 kind 应报错")
	}
}

func TestTimeoutClamp(t *testing.T) {
	c := New(config.LLMProfile{Kind: "openai"}, 0)
	if c.timeout != 30*time.Second {
		t.Fatalf("零超时应钳 30s, got %v", c.timeout)
	}
	c = New(config.LLMProfile{Kind: "openai"}, 5*time.Second)
	if c.timeout != 5*time.Second {
		t.Fatalf("显式超时不应被覆盖, got %v", c.timeout)
	}
}

func TestTestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "p"}}},
		})
	}))
	defer srv.Close()
	c := New(config.LLMProfile{Kind: "openai", BaseURL: srv.URL, Model: "m"}, 0)
	if err := c.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// 高级参数：temperature 显式配置才进请求体（缺省不传，兼容锁死参数的模型）；
// profile.MaxTokens>0 覆盖调用方值；非法 temperature 报错。
func TestAdvancedParams(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "p"}}},
		})
	}))
	defer srv.Close()

	// 缺省：不带 temperature，max_tokens 用调用方值
	c := New(config.LLMProfile{Kind: "openai", BaseURL: srv.URL, Model: "m"}, 0)
	if _, _, err := c.Chat(context.Background(), "s", "u", 100); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["temperature"]; ok {
		t.Fatalf("未配置 temperature 不应出现在请求体: %v", gotBody)
	}
	if gotBody["max_tokens"] != float64(100) {
		t.Fatalf("max_tokens = %v, want 100", gotBody["max_tokens"])
	}

	// 显式配置：temperature 透传，MaxTokens 覆盖调用方
	c = New(config.LLMProfile{Kind: "openai", BaseURL: srv.URL, Model: "m", Temperature: "1", MaxTokens: 512}, 0)
	if _, _, err := c.Chat(context.Background(), "s", "u", 100); err != nil {
		t.Fatal(err)
	}
	if gotBody["temperature"] != float64(1) {
		t.Fatalf("temperature = %v, want 1", gotBody["temperature"])
	}
	if gotBody["max_tokens"] != float64(512) {
		t.Fatalf("profile.MaxTokens 应覆盖调用方值, got %v", gotBody["max_tokens"])
	}

	// 非法 temperature（手改 config.toml 的情况）应报错
	c = New(config.LLMProfile{Kind: "openai", BaseURL: srv.URL, Model: "m", Temperature: "abc"}, 0)
	if _, _, err := c.Chat(context.Background(), "s", "u", 100); err == nil {
		t.Fatal("非法 temperature 应报错")
	}
}
