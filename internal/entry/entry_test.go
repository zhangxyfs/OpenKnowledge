package entry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `---
title: 变更日志强制规则
type: rule
tags:
  - changelog
  - workflow
mandatory: true
summary: 每次代码修改必须立即记录变更日志
---

正文内容
`

func TestParse(t *testing.T) {
	e, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "变更日志强制规则" || e.Type != "rule" || !e.Mandatory {
		t.Fatalf("unexpected %+v", e)
	}
	if len(e.Tags) != 2 || e.Tags[0] != "changelog" {
		t.Fatalf("tags %+v", e.Tags)
	}
	if e.Body != "正文内容" {
		t.Fatalf("body %q", e.Body)
	}
}

func TestParseCRLF(t *testing.T) {
	e, err := Parse([]byte(strings.ReplaceAll(sample, "\n", "\r\n")))
	if err != nil || e.Body != "正文内容" {
		t.Fatalf("crlf parse: %v body=%q", err, e.Body)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("no frontmatter")); err == nil {
		t.Fatal("expected error")
	}
	bad := "---\ntitle: x\ntype: bogus\n---\nbody\n"
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected type error")
	}
}

func TestSerializeRoundtrip(t *testing.T) {
	e, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	e2, err := Parse(e.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if e2.Title != e.Title || e2.Body != e.Body || len(e2.Tags) != len(e.Tags) || e2.Mandatory != e.Mandatory {
		t.Fatalf("roundtrip mismatch %+v", e2)
	}
}

func TestSlug(t *testing.T) {
	if got := Slug(`a/b:c d`); got != "abc-d" {
		t.Fatalf("slug %q", got)
	}
	if got := Slug("变更日志 规则"); got != "变更日志-规则" {
		t.Fatalf("slug %q", got)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path == "" || entries[0].FileName() != "b.md" {
		t.Fatalf("unexpected %+v", entries)
	}
}

func TestLoadTolerant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.md"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(badPath, []byte("---\ntitle: x\ntype: bogus\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, errs := LoadTolerant(dir)
	if len(entries) != 1 || entries[0].Title != "变更日志强制规则" {
		t.Fatalf("expected 1 valid entry, got %+v", entries)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), badPath) {
		t.Fatalf("expected 1 error mentioning %q, got %v", badPath, errs)
	}

	// 全部有效 → errs 为空
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "a.md"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	entries2, errs2 := LoadTolerant(dir2)
	if len(entries2) != 1 || len(errs2) != 0 {
		t.Fatalf("all-valid: entries=%d errs=%v", len(entries2), errs2)
	}
}
