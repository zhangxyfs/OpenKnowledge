package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/retrieve"
)

// 未变化条目（filename+mtime 相同）在增量同步中不得被读取/解析：
// 全量同步一次后破坏某条目 YAML，但用 os.Chtimes 恢复原 mtime，
// 再次同步必须成功且该条目仍留在库中——全量扫描实现会解析失败或把条目删出库。
func TestSyncDoesNotReadUnchangedFiles(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(kdir, "git.md")
	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("---\ntitle: [unclosed\n---\n\n已损坏\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(victim, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}

	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatalf("unchanged file must not be read/parsed: %v", err)
	}
	if n, _ := db.Count(); n != 3 {
		t.Fatalf("unchanged entry must stay indexed, count=%d", n)
	}
	hits, err := db.Query(retrieve.Terms("git 提交规范"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("corrupted-but-unchanged entry lost from index: %+v", hits)
	}
}

// mtime 变化的条目必须被重新读取/解析，entries/fts/INDEX.md 同步更新。
func TestSyncDetectsChangedFile(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(kdir, "git.md")
	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	updated := `---
title: Git 分支策略
type: note
tags: [git]
summary: 分支模型选择
---

主干开发，特性分支按天合并。
`
	if err := os.WriteFile(victim, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	// mtime 比较精度为秒：显式推进 mtime，避免与上次写入同秒而漏检
	later := fi.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(victim, later, later); err != nil {
		t.Fatal(err)
	}

	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := db.Query(retrieve.Terms("分支策略"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 分支策略" || hits[0].Body != "主干开发，特性分支按天合并。" {
		t.Fatalf("changed entry not re-indexed: %+v", hits)
	}
	// diff 非空时 INDEX.md 必须重建
	data, err := os.ReadFile(filepath.Join(filepath.Dir(kdir), "INDEX.md"))
	if err != nil || !strings.Contains(string(data), "Git 分支策略") {
		t.Fatalf("INDEX.md not updated: %v %q", err, data)
	}
}
