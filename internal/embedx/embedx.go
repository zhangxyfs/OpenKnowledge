// Package embedx 按配置构造 embedding 客户端——CLI/hook/GUI 的唯一构造点。
// 三形态同构：openai 直连、ollama 补 /v1、builtin 经 embedsidecar 状态文件
// 发现端口（未就绪一律返回 nil，调用方走纯关键词降级，绝不等待冷启动）。
package embedx

import (
	"context"
	"strings"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
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
		m := embed.FindBuiltinModel(p.Model)
		if m == nil {
			return nil
		}
		return builtinClient(*m, p, timeout)
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

// builtinClient 经 sidecar 状态文件发现端口；未就绪写 want 请求 daemon 拉起
// 并返回 nil（调用方走纯关键词降级，绝不等待冷启动）。
func builtinClient(m embed.BuiltinModel, p config.EmbeddingProfile, timeout time.Duration) embed.Client {
	st := embedsidecar.LoadState()
	if st == nil || st.ModelID != m.ID || !st.Healthy() {
		embedsidecar.RequestStart()
		return nil
	}
	return sidecarClient{&embed.OpenAIClient{
		BaseURL:     st.BaseURL(),
		Model:       m.File,
		Timeout:     timeout,
		Identity:    p.ModelIdentity(),
		QueryPrefix: m.QueryPrefix,
		DocPrefix:   m.DocPrefix,
	}}
}

// sidecarClient 在成功调用后 Touch last_used（daemon 空闲回收依据）。
type sidecarClient struct{ *embed.OpenAIClient }

func (s sidecarClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	v, err := s.OpenAIClient.EmbedQuery(ctx, text)
	if err == nil {
		embedsidecar.Touch()
	}
	return v, err
}

func (s sidecarClient) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	v, err := s.OpenAIClient.EmbedDocument(ctx, text)
	if err == nil {
		embedsidecar.Touch()
	}
	return v, err
}

func (s sidecarClient) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	v, err := s.OpenAIClient.EmbedDocuments(ctx, texts)
	if err == nil {
		embedsidecar.Touch()
	}
	return v, err
}
