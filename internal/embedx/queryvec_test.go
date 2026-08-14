package embedx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/embed"
	"openknowledge/internal/index"
)

func openTestDB(t *testing.T) *index.DB {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestQueryVecGuard(t *testing.T) {
	db := openTestDB(t)
	c := &embed.OpenAIClient{Identity: "openai:m1@h"}
	vec := []float32{1, 2, 3}

	// 无 meta 记录 → 放行
	got, warn := QueryVec(db, c, vec)
	if warn != "" || len(got) != 3 {
		t.Fatal("无记录应放行")
	}
	// 身份一致 → 放行
	db.SetMeta("embedding_model", "openai:m1@h")
	db.SetMeta("embedding_dim", "3")
	got, warn = QueryVec(db, c, vec)
	if warn != "" || len(got) != 3 {
		t.Fatal("一致应放行")
	}
	// 身份不符 → 拦截 + 提示
	got, warn = QueryVec(db, &embed.OpenAIClient{Identity: "builtin:qwen3-emb-0.6b-q8"}, vec)
	if got != nil || !strings.Contains(warn, "ok index") {
		t.Fatalf("应拦截: %v %q", got, warn)
	}
	// 维度不符 → 拦截
	got, warn = QueryVec(db, c, []float32{1, 2})
	if got != nil || warn == "" {
		t.Fatal("维度不符应拦截")
	}
	// 旧式客户端（Identity 空）→ 放行
	got, warn = QueryVec(db, &embed.OpenAIClient{}, vec)
	if warn != "" || len(got) != 3 {
		t.Fatal("空身份应放行")
	}
}

// legacyFake 模拟 ≤2.13 旧式客户端（身份空）：Sync 写向量但不落 meta，
// 正好复刻"有向量、无身份记录"的历史索引。
type legacyFake struct{}

func (legacyFake) ModelIdentity() string { return "" }
func (legacyFake) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 2, 3}, nil
}
func (legacyFake) EmbedDocument(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 2, 3}, nil
}
func (legacyFake) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 2, 3}
	}
	return out, nil
}

// TestQueryVecLegacyVectorsWithoutMeta：meta 空+有向量（≤2.13 索引）→ 拦截并
// 提示 ok index；meta 空+无向量（全新库）→ 放行。
func TestQueryVecLegacyVectorsWithoutMeta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "e.md"),
		[]byte("---\ntitle: t\ntype: note\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := index.Open(filepath.Join(dir, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Sync(dir, legacyFake{}); err != nil {
		t.Fatal(err)
	}
	c := &embed.OpenAIClient{Identity: "openai:m1@h"}
	got, warn := QueryVec(db, c, []float32{1, 2, 3})
	if got != nil || !strings.Contains(warn, "ok index") {
		t.Fatalf("meta 空+有向量应拦截: %v %q", got, warn)
	}
	// meta 空+无向量 → 放行
	db2 := openTestDB(t)
	got, warn = QueryVec(db2, c, []float32{1, 2, 3})
	if warn != "" || len(got) != 3 {
		t.Fatal("meta 空+无向量应放行")
	}
}
