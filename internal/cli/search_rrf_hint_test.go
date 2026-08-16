package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rrfAlphaBetaHint = "alpha/beta 配置被忽略"

// rrf 模式下项目配置了非默认 alpha/beta 时，search 走注入的 stderr 打印忽略提示；
// 默认配置（或 weighted）则不打印。
func TestSearchRrfAlphaBetaHint(t *testing.T) {
	_, kb := setupProject(t)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(kb, "knowledge", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("git.md", "---\ntitle: Git 规范\ntype: note\ntags: [git]\nsummary: 规范\ndraft: false\nmandatory: false\n---\n使用 Conventional Commits。\n")
	var out, errBuf bytes.Buffer
	// 离线环境无 embedding key：Index 完成同步后因缺 key 返回 1，索引已可用。
	if code := Index(nil, &out, &errBuf); code != 0 && code != 1 {
		t.Fatalf("index code=%d err=%q", code, errBuf.String())
	}
	cfgPath := filepath.Join(kb, "config.toml")

	// 非默认 alpha/beta（fusion 缺省 rrf）→ stderr 有忽略提示
	if err := os.WriteFile(cfgPath, []byte("[retrieve]\nalpha = 2\nbeta = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errBuf.Reset()
	if code := Search([]string{"Commits"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), rrfAlphaBetaHint) {
		t.Fatalf("non-default alpha/beta should hint on stderr: %q", errBuf.String())
	}

	// 默认配置 → stderr 无提示
	if err := os.WriteFile(cfgPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	errBuf.Reset()
	if code := Search([]string{"Commits"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if strings.Contains(errBuf.String(), rrfAlphaBetaHint) {
		t.Fatalf("default config should not hint: %q", errBuf.String())
	}

	// weighted 模式下非默认 alpha/beta → stderr 无提示
	if err := os.WriteFile(cfgPath, []byte("[retrieve]\nfusion = \"weighted\"\nalpha = 2\nbeta = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errBuf.Reset()
	if code := Search([]string{"Commits"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if strings.Contains(errBuf.String(), rrfAlphaBetaHint) {
		t.Fatalf("weighted mode should not hint: %q", errBuf.String())
	}
}
