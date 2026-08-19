package index

import (
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// 强负余弦不得否决已过关键词门槛的命中：准入按通道独立判定，语义分只影响排序。
// queryVec 与命中条目向量反向（cos=-1）时旧实现总分为负、条目被静默丢弃。
func TestQueryNegativeCosKeepsKeywordHit(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 3}
	hits, err := db.Query(retrieve.Terms("git 提交"), []float32{-1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("keyword-admitted hit must survive strong negative cosine: %+v", hits)
	}
}

// top_n 截断必须发生在分支过滤之后：其他分支的条目占名额时，本分支条目应补位，
// 而不是被无谓挤掉（QueryEx 截断后才过滤的老语义会返回空集）。
func TestQueryExBranchFiltersBeforeTopN(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "main.md",
		"---\ntitle: 主分支条目\ntype: note\ntags: [note]\ndraft: false\n---\n\n构建 构建 相关内容。\n")
	writeEntryFile(t, kdir, "feat.md",
		"---\ntitle: 其他分支条目\ntype: note\ntags: [branch:feat]\ndraft: false\n---\n\n构建 构建 构建 构建 构建构建构建。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 1}
	// 语义通道关闭，纯关键词：feat 条目词频更高、BM25 更强，独占 top 1
	hits, _, err := db.QueryEx(retrieve.Terms("构建"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "其他分支条目" {
		t.Fatalf("top1 should be the higher-BM25 cross-branch entry: %+v", hits)
	}
	// 同一查询经分支裁剪：feat 条目被过滤后主分支条目补位，而不是返回空
	hits, _, err = db.QueryExBranch(retrieve.Terms("构建"), nil, cfg, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "主分支条目" {
		t.Fatalf("branch filter should backfill from the same branch, got %+v", hits)
	}
	if strings.Contains(hits[0].Title, "其他分支") {
		t.Fatalf("cross-branch entry must be dropped: %+v", hits)
	}
	// 未知分支（branch 为空）不过滤
	hits, _, err = db.QueryExBranch(retrieve.Terms("构建"), nil, cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "其他分支条目" {
		t.Fatalf("empty branch must not filter: %+v", hits)
	}
}
