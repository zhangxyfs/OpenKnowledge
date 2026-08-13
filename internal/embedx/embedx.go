// Package embedx 按配置构造 embedding 客户端——CLI/hook/GUI 的唯一构造点。
// 三形态同构：openai 直连、ollama 补 /v1、builtin 经 embedsidecar 发现端口
// （Task 7 接入；未就绪一律返回 nil，调用方走纯关键词降级）。
package embedx

import (
	"strings"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

// Client 返回使用中（active）profile 的客户端；未配置/暂不可用返回 nil。
func Client(cfg config.Config) embed.Client {
	p := cfg.Embedding.ActiveProfile()
	if p == nil {
		return nil
	}
	return ClientForProfile(*p, time.Duration(cfg.Embedding.TimeoutSec)*time.Second)
}

// ClientForProfile 构造单个 profile 的客户端。
// openai/ollama 要求 base_url 与 model 非空（key 可空——本地兼容服务常无 key）。
func ClientForProfile(p config.EmbeddingProfile, timeout time.Duration) embed.Client {
	switch p.Type {
	case "ollama":
		if p.BaseURL == "" || p.Model == "" {
			return nil
		}
		return &embed.OpenAIClient{
			BaseURL:  strings.TrimRight(p.BaseURL, "/") + "/v1",
			Model:    p.Model,
			Timeout:  timeout,
			Identity: p.ModelIdentity(),
		}
	case "builtin":
		return nil // Task 7 接入 embedsidecar
	default: // openai
		if p.BaseURL == "" || p.Model == "" {
			return nil
		}
		return &embed.OpenAIClient{
			BaseURL:  p.BaseURL,
			APIKey:   p.ResolvedAPIKey(),
			Model:    p.Model,
			Timeout:  timeout,
			Identity: p.ModelIdentity(),
		}
	}
}
