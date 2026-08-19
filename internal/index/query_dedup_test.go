package index

import (
	"path/filepath"
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// 冷却排除必须发生在 top_n 截断之前：排除第 1 名后第 3 名应补位进入 top_n=2，
// 且被排除条目记入 QueryInfo.CooledSkipped。
func TestQueryExBranchExcludeBeforeTopN(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "a.md",
		"---\ntitle: 冷却甲\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 紫晶冷却 紫晶冷却 相关内容。\n")
	writeEntryFile(t, kdir, "b.md",
		"---\ntitle: 冷却乙\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 紫晶冷却 相关内容。\n")
	writeEntryFile(t, kdir, "c.md",
		"---\ntitle: 冷却丙\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 相关内容。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 2}
	// 基线：3 条命中截到 2 条（纯关键词通道，BM25 词频排序 a>b>c）
	hits0, _, err := db.QueryExBranch(retrieve.Terms("紫晶冷却"), nil, cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits0) != 2 {
		t.Fatalf("基线应命中 2 条: %+v", hits0)
	}
	excluded := hits0[0].Filename
	// 排除第 1 名：第 3 名（c.md）补位，而不是只剩 1 条
	hits, info, err := db.QueryExBranch(retrieve.Terms("紫晶冷却"), nil, cfg, "", map[string]bool{excluded: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("排除后第 3 名应补位进 top_n=2: %+v", hits)
	}
	for _, h := range hits {
		if h.Filename == excluded {
			t.Fatalf("冷却条目应被排除: %+v", hits)
		}
	}
	if hits[0].Filename != hits0[1].Filename || hits[1].Filename != "c.md" {
		t.Fatalf("补位顺序应为原第 2、3 名: %+v", hits)
	}
	if len(info.CooledSkipped) != 1 || info.CooledSkipped[0] != excluded {
		t.Fatalf("CooledSkipped 应记录被排除条目: %+v", info.CooledSkipped)
	}
}

// exclude 传 nil / 空集合行为一致，且不产生 CooledSkipped（ok search / GUI 检索
// 的行为不变）。
func TestQueryExBranchNilExcludeUnchanged(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "a.md",
		"---\ntitle: 冷却甲\ntype: note\ntags: []\ndraft: false\n---\n\n紫晶冷却 相关内容。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 2}
	for _, exclude := range []map[string]bool{nil, {}} {
		hits, info, err := db.QueryExBranch(retrieve.Terms("紫晶冷却"), nil, cfg, "", exclude)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].Filename != "a.md" {
			t.Fatalf("无排除时应照常命中: %+v", hits)
		}
		if len(info.CooledSkipped) != 0 {
			t.Fatalf("无排除时 CooledSkipped 应为空: %+v", info.CooledSkipped)
		}
	}
}
