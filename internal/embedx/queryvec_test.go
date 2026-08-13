package embedx

import (
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
