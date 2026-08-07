package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/agentx"
	"openknowledge/internal/entry"
	"openknowledge/internal/registry"
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
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	// KIMI_CODE_HOME 目录需存在（模拟已安装 kimi），否则 agent 检测为假、init 跳过 hooks 写入
	if err := os.MkdirAll(filepath.Join(home, "kimi"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	if !strings.Contains(string(kimiCfg), agentx.MarkerBegin) || strings.Count(string(kimiCfg), "[[hooks]]") != 3 {
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
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi")) // 隔离真实 kimi 配置，Init 会写 hooks
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
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
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi")) // 隔离真实 kimi 配置，Init 会写 hooks
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
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

// ok wiki status/mark：临时 git 仓库里验证游标与落后计数。
func TestWikiStatusAndMark(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi"))
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OPENAI_API_KEY", "") // 防止真实网络调用，保证测试离线
	// 在临时 git 仓库里跑（cwd 即项目）
	repo := t.TempDir()
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "c1")
	// 切 cwd 到 repo（cli 走 resolveFromCwd）
	chdir(t, repo)

	var out, errBuf bytes.Buffer
	if code := Init(nil, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errBuf); code != 0 {
		t.Fatalf("status code=%d err=%q", code, errBuf.String())
	}
	var st struct {
		Project   string `json:"project"`
		HasWiki   bool   `json:"has_wiki"`
		Behind    int    `json:"behind"`
		Stale     bool   `json:"stale"`
		Threshold int    `json:"threshold"`
	}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("status output %q: %v", out.String(), err)
	}
	if st.HasWiki || st.Behind != 1 || st.Threshold != 20 {
		t.Fatalf("status: %+v", st)
	}

	// mark 无参 → 取 HEAD
	out.Reset()
	if code := WikiCmd([]string{"mark"}, &out, &errBuf); code != 0 {
		t.Fatalf("mark code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "已记录 wiki 游标") {
		t.Fatalf("mark output: %q", out.String())
	}
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errBuf); code != 0 {
		t.Fatalf("status2 code=%d err=%q", code, errBuf.String())
	}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("status2 output %q: %v", out.String(), err)
	}
	if !st.HasWiki || st.Behind != 0 {
		t.Fatalf("after mark: %+v", st)
	}
}

// ok add --summary：显式摘要写入 frontmatter；缺省时取标题（wiki 技能依赖此 flag）。
func TestAddSummaryFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi")) // 隔离真实 kimi 配置，Init 会写 hooks
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OPENAI_API_KEY", "") // 防止真实网络调用，保证测试离线
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	kb := filepath.Join(home, "projects", "demo", "knowledge")

	out.Reset()
	if code := Add([]string{"--title", "自定义摘要条目", "--summary", "自定义"}, &out, &errBuf); code != 0 {
		t.Fatalf("add --summary code=%d err=%q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(kb, entry.Slug("自定义摘要条目")+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "summary: 自定义") {
		t.Fatalf("frontmatter should contain custom summary, got %q", data)
	}

	out.Reset()
	if code := Add([]string{"--title", "默认摘要条目"}, &out, &errBuf); code != 0 {
		t.Fatalf("add code=%d err=%q", code, errBuf.String())
	}
	data, err = os.ReadFile(filepath.Join(kb, entry.Slug("默认摘要条目")+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "summary: 默认摘要条目") {
		t.Fatalf("frontmatter should default summary to title, got %q", data)
	}
}

// ok add --force：同名条目不带 --force 报错（既有行为）；带 --force 覆盖且内容为新值。
func TestAddForceOverwrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi")) // 隔离真实 kimi 配置，Init 会写 hooks
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OPENAI_API_KEY", "") // 防止真实网络调用，保证测试离线
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	kb := filepath.Join(home, "projects", "demo", "knowledge")
	entryPath := filepath.Join(kb, entry.Slug("架构总览")+".md")

	out.Reset()
	if code := Add([]string{"--title", "架构总览", "--type", "reference", "--tags", "wiki,架构"}, &out, &errBuf); code != 0 {
		t.Fatalf("add code=%d err=%q", code, errBuf.String())
	}

	// 同名 add 不带 --force → 报错，原内容不变
	out.Reset()
	errBuf.Reset()
	if code := Add([]string{"--title", "架构总览", "--type", "reference", "--tags", "wiki,架构", "--file", writeBody(t, proj, "v2 无 force")}, &out, &errBuf); code == 0 {
		t.Fatal("expected duplicate add without --force to fail")
	}
	if !strings.Contains(errBuf.String(), "条目已存在") {
		t.Fatalf("stderr should mention 条目已存在, got %q", errBuf.String())
	}

	// 带 --force → 覆盖成功，内容为新值
	newBody := writeBody(t, proj, "重写后的架构正文。")
	out.Reset()
	errBuf.Reset()
	if code := Add([]string{"--title", "架构总览", "--type", "reference", "--tags", "wiki,架构", "--summary", "新摘要", "--file", newBody, "--force"}, &out, &errBuf); code != 0 {
		t.Fatalf("add --force code=%d err=%q", code, errBuf.String())
	}
	data, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "重写后的架构正文。") {
		t.Fatalf("entry should be overwritten with new body, got %q", data)
	}
	if !strings.Contains(string(data), "summary: 新摘要") {
		t.Fatalf("entry should carry new summary, got %q", data)
	}
}

// writeBody 写一个临时正文文件并返回路径。
func writeBody(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "body-"+strings.ReplaceAll(t.Name(), "/", "-")+".md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
