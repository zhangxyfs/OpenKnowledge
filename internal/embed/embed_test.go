package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeEmbeddings 返回一个 httptest 服务器：记录最后一次请求体，
// 按 input 顺序返回 [len(text),index] 形式的二维向量（故意乱序 index 以验证重排）。
func fakeEmbeddings(t *testing.T, gotBody *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		b, _ := json.Marshal(req)
		*gotBody = string(b)
		type datum struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		data := make([]datum, len(req.Input))
		for i, text := range req.Input {
			data[len(req.Input)-1-i] = datum{Embedding: []float32{float32(len(text)), float32(i)}, Index: i}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestEmbedDocumentsBatchAndOrder(t *testing.T) {
	var got string
	srv := fakeEmbeddings(t, &got)
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second}
	vecs, err := c.EmbedDocuments(context.Background(), []string{"ab", "cdef"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 2 || vecs[1][0] != 4 {
		t.Fatalf("应按 index 重排回原顺序: %v", vecs)
	}
	var req struct {
		Input []string `json:"input"`
	}
	_ = json.Unmarshal([]byte(got), &req)
	if len(req.Input) != 2 || req.Input[0] != "ab" {
		t.Fatalf("input 应为数组: %s", got)
	}
}

func TestQueryPrefixApplied(t *testing.T) {
	var got string
	srv := fakeEmbeddings(t, &got)
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, Model: "m", QueryPrefix: "Instruct: x\nQuery: ", DocPrefix: "doc: ", Timeout: 5 * time.Second}
	if _, err := c.EmbedQuery(context.Background(), "你好"); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Input []string `json:"input"`
	}
	_ = json.Unmarshal([]byte(got), &req)
	if req.Input[0] != "Instruct: x\nQuery: 你好" {
		t.Fatalf("查询侧应加前缀: %q", req.Input[0])
	}
	if _, err := c.EmbedDocument(context.Background(), "正文"); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(got), &req)
	if req.Input[0] != "doc: 正文" {
		t.Fatalf("文档侧应加文档前缀: %q", req.Input[0])
	}
}

func TestModelIdentity(t *testing.T) {
	c := &OpenAIClient{Identity: "openai:m@h"}
	if c.ModelIdentity() != "openai:m@h" {
		t.Fatal(c.ModelIdentity())
	}
	var zero OpenAIClient
	if zero.ModelIdentity() != "" {
		t.Fatal("空 Identity 应返回空串")
	}
}

func TestCosine(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{1, 0}); got != 1 {
		t.Fatalf("got %v", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); got != 0 {
		t.Fatalf("got %v", got)
	}
	if got := Cosine([]float32{}, []float32{}); got != 0 {
		t.Fatalf("got %v", got)
	}
}
