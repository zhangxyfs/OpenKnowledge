// Package embedx 按配置构造 embedding 客户端——CLI/hook/GUI 的唯一构造点。
// 三形态同构：openai 直连、ollama 补 /v1、builtin 经 embedsidecar 状态文件
// 发现端口（未就绪一律返回 nil，调用方走纯关键词降级，绝不等待冷启动）。
package embedx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
	"openknowledge/internal/index"
)

// Client 返回使用中（active）profile 的客户端；未配置/暂不可用返回 nil。
// timeout_sec 显式配 0/负数时钳回默认 5s——零值在 http.Client 语义里是"永不超时"，
// hook 检索 / ok search / ok doctor 的 ping 会无限挂起直到 TCP 自身放弃。
func Client(cfg config.Config) embed.Client {
	p := cfg.Embedding.ActiveProfile()
	if p == nil {
		return nil
	}
	sec := cfg.Embedding.TimeoutSec
	if sec <= 0 {
		sec = 5
	}
	return ClientForProfile(*p, time.Duration(sec)*time.Second)
}

// ClientForIndex 是索引/重建路径的构造：与 Client 同语义，但超时下限 120s
// （hook 查询路径仍用 Client 的短超时，两者预算语义不互相污染）。
func ClientForIndex(cfg config.Config) embed.Client {
	p := cfg.Embedding.ActiveProfile()
	if p == nil {
		return nil
	}
	sec := cfg.Embedding.TimeoutSec
	if sec < 120 {
		sec = 120
	}
	return ClientForProfile(*p, time.Duration(sec)*time.Second)
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
			// 用户填 …/v1 时先去重再补，避免 …/v1/v1 404
			BaseURL:  strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/v1") + "/v1",
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

// QueryVec 判定 queryVec 能否进入语义通道：索引的模型身份与当前客户端不符
// （或维度不符）时返回 nil + 中文提示（调用方决定展示层级：CLI stderr / hook 日志）。
// 无 meta 记录且无向量（从未算过向量）或旧式客户端（身份空）不拦截；
// meta 空但有向量 = ≤2.13 历史索引，身份不明，拦截并提示 ok index 重建。
func QueryVec(db *index.DB, client embed.Client, queryVec []float32) ([]float32, string) {
	if client == nil || len(queryVec) == 0 || client.ModelIdentity() == "" {
		return queryVec, ""
	}
	model, dim, err := db.EmbeddingMeta()
	if err != nil {
		return queryVec, ""
	}
	if model == "" {
		if hv, herr := db.HasVectors(); herr == nil && hv {
			return nil, "历史向量无模型身份记录（≤2.13 索引），本次退化为关键词检索；运行 ok index 重建后恢复"
		}
		return queryVec, ""
	}
	if model != client.ModelIdentity() || (dim > 0 && dim != len(queryVec)) {
		return nil, fmt.Sprintf(
			"embedding 模型已切换（索引=%s，当前=%s），本次退化为关键词检索；运行 ok index 重建后恢复",
			model, client.ModelIdentity())
	}
	return queryVec, ""
}
