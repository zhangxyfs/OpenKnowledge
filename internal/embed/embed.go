package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client 是 embedding 服务抽象。查询与建索引是两条路径：
// 指令感知模型（Qwen3-Embedding、nomic）只在对应路径加前缀。
type Client interface {
	// ModelIdentity 返回建索引的模型身份串（写入 kb.db meta，供切换检测）；
	// 空串表示旧式构造（不参与身份判定）。
	ModelIdentity() string
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	EmbedDocument(ctx context.Context, text string) ([]float32, error)
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
}

// OpenAIClient 面向 OpenAI 兼容 /v1/embeddings（线上服务、Ollama、
// 内置 llama-server 三形态共用）。QueryPrefix/DocPrefix 为空即不加前缀。
type OpenAIClient struct {
	BaseURL     string
	APIKey      string
	Model       string
	Timeout     time.Duration
	Identity    string
	QueryPrefix string
	DocPrefix   string
}

func (c *OpenAIClient) ModelIdentity() string { return c.Identity }

func (c *OpenAIClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.embedBatch(ctx, []string{c.QueryPrefix + text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (c *OpenAIClient) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.embedBatch(ctx, []string{c.DocPrefix + text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (c *OpenAIClient) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = c.DocPrefix + t
	}
	return c.embedBatch(ctx, prefixed)
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (c *OpenAIClient) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	body, err := json.Marshal(embedRequest{Model: c.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding API %d: %s", resp.StatusCode, msg)
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding API 返回 %d 条，期望 %d 条", len(er.Data), len(inputs))
	}
	// 按 index 重排（部分实现不保证顺序）
	sort.Slice(er.Data, func(i, j int) bool { return er.Data[i].Index < er.Data[j].Index })
	out := make([][]float32, len(er.Data))
	for i, d := range er.Data {
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("embedding API 返回空向量")
		}
		out[i] = d.Embedding
	}
	return out, nil
}

// Cosine 计算余弦相似度；任一零向量或长度不等返回 0。
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
