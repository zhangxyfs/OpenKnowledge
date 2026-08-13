package embedx

import (
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
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
