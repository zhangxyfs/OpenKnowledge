package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/entry"
	"openknowledge/internal/registry"
	"openknowledge/internal/setupx"
)

// chdir 切换工作目录并在结束时还原。
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestInitAddSearchList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi"))
	t.Setenv("OPENAI_API_KEY", "") // 防止真实网络调用，保证测试离线
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer

	// init：注册项目并幂等写入 hooks 配置（不再打印可粘贴的裸 hooks 块）
	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	if strings.Contains(out.String(), "[[hooks]]") {
		t.Fatalf("init should not print raw hooks block, got %q", out.String())
	}
	if !strings.Contains(out.String(), "hooks 配置已写入") {
		t.Fatalf("init should write hooks config, got %q", out.String())
	}
	kimiCfg, err := os.ReadFile(filepath.Join(home, "kimi", "config.toml"))
	if err != nil {
		t.Fatalf("init should write kimi config: %v", err)
	}
	if !strings.Contains(string(kimiCfg), setupx.MarkerBegin) || strings.Count(string(kimiCfg), "[[hooks]]") != 3 {
		t.Fatalf("kimi config should contain one marker block with 3 hooks: %q", kimiCfg)
	}

	// add（无 embedding key → 提示跳过向量，仍成功）
	body := filepath.Join(proj, "body.md")
	if err := os.WriteFile(body, []byte("使用 Conventional Commits。"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	code := Add([]string{"--title", "Git 提交规范", "--type", "note", "--tags", "git", "--file", body}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("add code=%d err=%q", code, errBuf.String())
	}
	kb := filepath.Join(home, "projects", "demo")
	entries, err := entry.Load(filepath.Join(kb, "knowledge"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %+v err=%v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(kb, "INDEX.md")); err != nil || !strings.Contains(string(data), "Git 提交规范") {
		t.Fatalf("INDEX not rebuilt: %v %q", err, data)
	}

	// 重复 add 同名条目 → 失败
	out.Reset()
	if code := Add([]string{"--title", "Git 提交规范", "--type", "note"}, &out, &errBuf); code == 0 {
		t.Fatal("expected duplicate add to fail")
	}

	// search（无 embedding key → 纯关键词）
	out.Reset()
	if code := Search([]string{"git", "提交"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Git 提交规范") {
		t.Fatalf("search should hit, got %q", out.String())
	}

	// list
	out.Reset()
	if code := List(nil, &out, &errBuf); code != 0 {
		t.Fatalf("list code=%d", code)
	}
	if !strings.Contains(out.String(), "demo") || !strings.Contains(out.String(), "Git 提交规范") {
		t.Fatalf("list output %q", out.String())
	}
}

func TestAddOutsideProjectFails(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	chdir(t, t.TempDir())
	var out, errBuf bytes.Buffer
	if code := Add([]string{"--title", "X", "--type", "note"}, &out, &errBuf); code == 0 {
		t.Fatal("expected failure outside registered project")
	}
}

// ok init 不带参数时应以当前目录基名作为项目名。
func TestInitDefaultsToDirBaseName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	proj := filepath.Join(home, "myproj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init(nil, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != 1 || reg.Projects[0].Name != "myproj" {
		t.Fatalf("expected basename-derived project, got %+v", reg.Projects)
	}
	if _, err := os.Stat(filepath.Join(home, "projects", "myproj", "knowledge")); err != nil {
		t.Fatalf("KB skeleton missing: %v", err)
	}
}

// 无 API key 时 ok index 仍应重建 INDEX.md（向量跳过，退出码保持 1）。
func TestIndexRebuildsIndexWithoutAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("OPENAI_API_KEY", "")
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	// 手工写入条目（绕过 ok add，模拟手工维护后 INDEX 过期）
	kb := filepath.Join(home, "projects", "demo")
	manual := "---\ntitle: 手工条目\ntype: note\nsummary: 手工添加\n---\n\n手工正文。\n"
	if err := os.WriteFile(filepath.Join(kb, "knowledge", "manual.md"), []byte(manual), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errBuf.Reset()
	if code := Index(nil, &out, &errBuf); code != 1 {
		t.Fatalf("expected exit 1 without API key, got %d", code)
	}
	data, err := os.ReadFile(filepath.Join(kb, "INDEX.md"))
	if err != nil {
		t.Fatalf("INDEX.md should exist after ok index: %v", err)
	}
	if !strings.Contains(string(data), "手工条目") {
		t.Fatalf("INDEX should be rebuilt with manual entry, got %q", data)
	}
	if !strings.Contains(errBuf.String(), "INDEX") {
		t.Fatalf("stderr should mention INDEX rebuilt, got %q", errBuf.String())
	}
}
