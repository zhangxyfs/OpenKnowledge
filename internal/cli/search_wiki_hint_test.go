package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wikiHint = "暂无 wiki 条目覆盖"

// 无 wiki 覆盖时 search 输出兜底提示行；有 wiki 覆盖时不输出。
func TestSearchWikiHint(t *testing.T) {
	_, kb := setupProject(t)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(kb, "knowledge", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("git.md", "---\ntitle: Git 规范\ntype: note\ntags: [git]\nsummary: 规范\ndraft: false\nmandatory: false\n---\n使用 Conventional Commits。\n")
	write("arch.md", "---\ntitle: 架构总览\ntype: reference\ntags: [wiki, 架构]\nsummary: 架构\ndraft: false\nmandatory: false\n---\n守护进程架构。\n")
	var out, errBuf bytes.Buffer
	// 离线环境无 embedding key：Index 完成同步后因缺 key 返回 1，索引已可用。
	if code := Index(nil, &out, &errBuf); code != 0 && code != 1 {
		t.Fatalf("index code=%d err=%q", code, errBuf.String())
	}

	// wiki 覆盖的主题 → 无提示行
	out.Reset()
	if code := Search([]string{"守护进程"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if strings.Contains(out.String(), wikiHint) {
		t.Fatalf("covered topic should not hint: %q", out.String())
	}

	// 仅非 wiki 条目命中 → 有提示行
	out.Reset()
	if code := Search([]string{"Commits"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), wikiHint) {
		t.Fatalf("non-wiki topic should hint: %q", out.String())
	}

	// 全新主题（零命中）→ 有提示行
	out.Reset()
	if code := Search([]string{"甲子园"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), wikiHint) {
		t.Fatalf("brand-new topic should hint: %q", out.String())
	}
}
