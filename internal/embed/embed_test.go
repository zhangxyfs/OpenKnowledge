package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/entry"
)

func newFakeServer(t *testing.T, vec []float32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	}))
}

func TestOpenAIClientEmbed(t *testing.T) {
	srv := newFakeServer(t, []float32{1, 2, 3})
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: 5 * time.Second}
	vec, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 || vec[0] != 1 {
		t.Fatalf("unexpected %v", vec)
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

func TestVectorSetUpdate(t *testing.T) {
	srv := newFakeServer(t, []float32{0.5, 0.5})
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, Model: "m"}

	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &entry.Entry{Title: "A", Path: p}
	vs := &VectorSet{Vectors: map[string]*EntryVector{}}
	if err := vs.Update(context.Background(), c, []*entry.Entry{e}); err != nil {
		t.Fatal(err)
	}
	if len(vs.Vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vs.Vectors))
	}
	// 再次调用应命中缓存（mtime 未变）
	if err := vs.Update(context.Background(), c, []*entry.Entry{e}); err != nil {
		t.Fatal(err)
	}
	// 条目删除后向量被清理
	if err := vs.Update(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	if len(vs.Vectors) != 0 {
		t.Fatal("expected cleanup")
	}
}

func TestLoadVectorsMissing(t *testing.T) {
	vs, err := LoadVectors(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || len(vs.Vectors) != 0 {
		t.Fatalf("got %+v err=%v", vs, err)
	}
}
