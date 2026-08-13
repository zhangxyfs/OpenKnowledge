package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// batchFake 记录 EmbedDocuments 批次的假客户端（实现 embed.Client）。
type batchFake struct {
	identity string
	batches  []int
}

func (f *batchFake) ModelIdentity() string { return f.identity }
func (f *batchFake) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return f.EmbedDocument(ctx, text)
}
func (f *batchFake) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	v, err := f.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}
func (f *batchFake) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	f.batches = append(f.batches, len(texts))
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t)), 1, 0}
	}
	return out, nil
}

func writeEntries(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("---\ntitle: 条目%d\ntype: note\n---\n正文%d", i, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("e%03d.md", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncBatchAndMeta(t *testing.T) {
	dir := t.TempDir()
	writeEntries(t, dir, 70)
	db, err := Open(filepath.Join(dir, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f := &batchFake{identity: "openai:m@h"}
	if err := db.Sync(dir, f); err != nil {
		t.Fatal(err)
	}
	if len(f.batches) != 3 || f.batches[0] != 32 || f.batches[2] != 6 {
		t.Fatalf("应按 32 分批: %v", f.batches)
	}
	model, dim, err := db.EmbeddingMeta()
	if err != nil || model != "openai:m@h" || dim != 3 {
		t.Fatalf("meta: %s %d %v", model, dim, err)
	}
}

func TestSyncIdentityMismatchSkipsVectors(t *testing.T) {
	dir := t.TempDir()
	writeEntries(t, dir, 3)
	db, _ := Open(filepath.Join(dir, "kb.db"))
	defer db.Close()
	f1 := &batchFake{identity: "openai:m1@h"}
	if err := db.Sync(dir, f1); err != nil {
		t.Fatal(err)
	}
	// 换一个身份的 client 同步：向量与 meta 都应保持旧模型的
	f2 := &batchFake{identity: "builtin:qwen3-emb-0.6b-q8"}
	if err := db.Sync(dir, f2); err != nil {
		t.Fatal(err)
	}
	if len(f2.batches) != 0 {
		t.Fatal("身份不符不应算向量")
	}
	model, _, _ := db.EmbeddingMeta()
	if model != "openai:m1@h" {
		t.Fatal("meta 不应被覆盖")
	}
	// ClearVectors 后全量重建
	if err := db.ClearVectors(); err != nil {
		t.Fatal(err)
	}
	if err := db.Sync(dir, f2); err != nil {
		t.Fatal(err)
	}
	if len(f2.batches) != 1 || f2.batches[0] != 3 {
		t.Fatalf("清向量后应全量重建: %v", f2.batches)
	}
	model, _, _ = db.EmbeddingMeta()
	if model != "builtin:qwen3-emb-0.6b-q8" {
		t.Fatal("重建后 meta 应更新")
	}
}
