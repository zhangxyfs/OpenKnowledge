// Package llmx 收口大模型生成调用：openai（/chat/completions 兼容）与
// anthropic（/v1/messages 兼容）两种协议，供条目优化等场景使用。
package llmx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/config"
)

// Client 单个 profile 的生成客户端。
type Client struct {
	p       config.LLMProfile
	timeout time.Duration
	hc      *http.Client
}

// New 构造客户端；timeout<=0 钳 30s（生成比 embed 慢，沿用 embedx 的
// 零值钳制教训但阈值不同）。kind 非法返回 nil（调用方校验兜底）。
func New(p config.LLMProfile, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{p: p, timeout: timeout, hc: &http.Client{Timeout: timeout}}
}

// Chat 单轮非流式对话：system + user 进，正文出；reasoning 为模型的思考过程
//（openai 的 reasoning_content / anthropic 的 thinking 块，无则为空），供日志记录。
// profile.MaxTokens>0 时覆盖调用方 maxTokens（用户显式配置优先）。
func (c *Client) Chat(ctx context.Context, system, user string, maxTokens int) (text, reasoning string, err error) {
	if c.p.MaxTokens > 0 {
		maxTokens = c.p.MaxTokens
	}
	switch c.p.Kind {
	case "openai":
		return c.chatOpenAI(ctx, system, user, maxTokens)
	case "anthropic":
		return c.chatAnthropic(ctx, system, user, maxTokens)
	default:
		return "", "", fmt.Errorf("未知 llm 类型: %q（openai|anthropic）", c.p.Kind)
	}
}

// Test 连通性检查：发一条极短请求验证地址/鉴权/模型名。
func (c *Client) Test(ctx context.Context) error {
	_, _, err := c.Chat(ctx, "ping", "ping", 1)
	return err
}

func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.p.BaseURL, "/") + path
}

// applyTemperature profile.Temperature 非空时解析并写入请求体；非法值报错
// （GUI 保存时已校验，此处兜住手改 config.toml 的情况）。
func (c *Client) applyTemperature(body map[string]any) error {
	if strings.TrimSpace(c.p.Temperature) == "" {
		return nil
	}
	t, err := strconv.ParseFloat(strings.TrimSpace(c.p.Temperature), 64)
	if err != nil {
		return fmt.Errorf("temperature 配置非法: %q", c.p.Temperature)
	}
	body["temperature"] = t
	return nil
}

func (c *Client) doJSON(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		snip := string(raw)
		if len(snip) > 300 {
			snip = snip[:300] + "…"
		}
		return fmt.Errorf("%s %d: %s", c.p.Kind, resp.StatusCode, snip)
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) chatOpenAI(ctx context.Context, system, user string, maxTokens int) (string, string, error) {
	body := map[string]any{
		"model": c.p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens": maxTokens,
		// temperature 缺省不传（服务端默认值兼容性最好，如 Kimi k3 锁死=1）；
		// 用户在高级参数里显式配置时才带上。
	}
	if err := c.applyTemperature(body); err != nil {
		return "", "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"` // 思考过程（DeepSeek/Kimi 等推理模型）
			} `json:"message"`
		} `json:"choices"`
	}
	if err := c.doJSON(ctx, c.endpoint("/chat/completions"),
		map[string]string{"Authorization": "Bearer " + c.p.APIKey}, body, &out); err != nil {
		return "", "", err
	}
	if len(out.Choices) == 0 {
		return "", "", fmt.Errorf("openai 响应无 choices")
	}
	return out.Choices[0].Message.Content, out.Choices[0].Message.ReasoningContent, nil
}

func (c *Client) chatAnthropic(ctx context.Context, system, user string, maxTokens int) (string, string, error) {
	body := map[string]any{
		"model":      c.p.Model,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
		"max_tokens": maxTokens,
	}
	if err := c.applyTemperature(body); err != nil {
		return "", "", err
	}
	var out struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"` // 扩展思考块（thinking 开启时）
		} `json:"content"`
	}
	if err := c.doJSON(ctx, c.endpoint("/v1/messages"), map[string]string{
		"x-api-key":         c.p.APIKey,
		"anthropic-version": "2023-06-01",
	}, body, &out); err != nil {
		return "", "", err
	}
	var text, reasoning string
	for _, b := range out.Content {
		switch b.Type {
		case "thinking":
			reasoning += b.Thinking
		default: // text 及未标注类型的块
			if b.Text != "" {
				text += b.Text
			}
		}
	}
	if text == "" && len(out.Content) == 0 {
		return "", "", fmt.Errorf("anthropic 响应无 content")
	}
	return text, reasoning, nil
}
