package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/agentx"
	"openknowledge/internal/entry"
	"openknowledge/internal/index"
	"openknowledge/internal/registry"
	"openknowledge/internal/wiki"
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
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
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
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
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
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
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
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
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
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
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
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
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

// runGit 在 dir 下执行 git 命令，失败即 fatal（带测试身份环境变量）。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setupWikiProject 建 OK_HOME 隔离环境 + 临时 git 仓库（master，1 个提交）并注册项目；
// 返回仓库目录与该项目知识库的 state 目录。
func setupWikiProject(t *testing.T) (repo, stateDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi"))
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OPENAI_API_KEY", "") // 防止真实网络调用，保证测试离线
	repo = t.TempDir()
	runGit(t, repo, "init", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "c1")
	chdir(t, repo)
	var out, errBuf bytes.Buffer
	if code := Init(nil, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	stateDir = filepath.Join(home, "projects", filepath.Base(repo), "state")
	return repo, stateDir
}

// ok wiki base：查询未设置 → 设置 dev → 再查询应显示 dev。
func TestWikiBaseSetAndShow(t *testing.T) {
	setupWikiProject(t)
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"base"}, &out, &errb); code != 0 {
		t.Fatalf("base 查询 exit %d err=%q", code, errb.String())
	}
	out.Reset()
	if code := WikiCmd([]string{"base", "dev"}, &out, &errb); code != 0 {
		t.Fatalf("base 设置 exit %d err=%q", code, errb.String())
	}
	out.Reset()
	if code := WikiCmd([]string{"base"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Errorf("base 设置后查询应显示 dev: %q", out.String())
	}
}

// ok wiki base 在旧格式 wiki.json 上设置：legacy 游标可达 → 先归入当前分支再落盘，
// 绝不弄丢 last_commit（spec §4）。
func TestWikiBasePreservesLegacyCursor(t *testing.T) {
	repo, stateDir := setupWikiProject(t)
	head, err := wiki.HeadCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"last_commit":"` + head + `","generated_at":"2026-08-08T09:00:00+08:00","entry_count":7}`
	if err := os.WriteFile(wiki.CursorPath(stateDir), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"base", "dev"}, &out, &errb); code != 0 {
		t.Fatalf("base 设置 exit %d err=%q", code, errb.String())
	}
	s := wiki.LoadState(stateDir)
	if s == nil || s.BaseBranch != "dev" {
		t.Fatalf("基准应为 dev: %+v", s)
	}
	cur, ok := s.Cursors["master"]
	if !ok || cur.LastCommit != head || cur.EntryCount != 7 {
		t.Fatalf("legacy 游标应归入当前分支 master: %+v", s.Cursors)
	}
	// 无参查询不受影响
	out.Reset()
	if code := WikiCmd([]string{"base"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Errorf("查询应显示 dev: %q", out.String())
	}
}

// ok wiki base 在旧格式 wiki.json 上设置：legacy 游标不可达 → 拒绝写盘并提示，
// 旧文件原样保留（不丢 last_commit）。
func TestWikiBaseLegacyDivergedRefused(t *testing.T) {
	repo, stateDir := setupWikiProject(t)
	// 制造与 master 分叉的 orphan commit（对象留在库中但不可达 HEAD）
	runGit(t, repo, "checkout", "-q", "-b", "tmp")
	runGit(t, repo, "commit", "--allow-empty", "-m", "orphan")
	orphan, err := wiki.HeadCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "checkout", "-q", "master")
	runGit(t, repo, "branch", "-q", "-D", "tmp")
	legacy := `{"last_commit":"` + orphan + `","generated_at":"2026-08-08T09:00:00+08:00","entry_count":7}`
	if err := os.WriteFile(wiki.CursorPath(stateDir), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"base", "dev"}, &out, &errb); code != 1 {
		t.Fatalf("分叉 legacy 应拒绝写盘 exit=1, got %d out=%q", code, out.String())
	}
	if !strings.Contains(errb.String(), "为避免丢失未写入") || !strings.Contains(errb.String(), "ok wiki mark") {
		t.Errorf("stderr 应提示拒绝原因与解决办法: %q", errb.String())
	}
	data, _ := os.ReadFile(wiki.CursorPath(stateDir))
	if string(data) != legacy {
		t.Errorf("拒绝时不得改写旧文件: %s", data)
	}
}

// ok wiki mark：游标应记入当前分支；空基准应设为当前分支；落盘为新格式。
func TestWikiMarkRecordsCurrentBranch(t *testing.T) {
	repo, stateDir := setupWikiProject(t)
	runGit(t, repo, "checkout", "-q", "-b", "dev")
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 {
		t.Fatalf("mark exit %d err=%q", code, errb.String())
	}
	s := wiki.LoadState(stateDir)
	if s == nil || s.Cursors["dev"].LastCommit == "" {
		t.Fatalf("mark 应记入当前分支 dev: %+v", s)
	}
	if s.BaseBranch != "dev" {
		t.Errorf("空基准应设为当前分支: %+v", s)
	}
	// 旧字段不存在于落盘
	data, _ := os.ReadFile(wiki.CursorPath(stateDir))
	if strings.Contains(string(data), `"last_commit":`) && !strings.Contains(string(data), `"cursors"`) {
		t.Errorf("落盘应为新格式: %s", data)
	}
}

// ok wiki mark <rev>：相对 rev（HEAD~1）应归一化为 40 位完整 hash 落盘，
// 且随后 status 的 branch_state 为 ok（回归：原样落盘会被 mb == lc 误判为 diverged）。
func TestWikiMarkNormalizesRev(t *testing.T) {
	repo, stateDir := setupWikiProject(t)
	// 夹具需 ≥2 提交：再补一个提交，随后 mark HEAD~1
	if err := os.WriteFile(filepath.Join(repo, "g"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "c2")
	// 期望值独立取自 git rev-parse（不经过被测函数）
	cmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD~1")
	wantB, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(wantB))

	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark", "HEAD~1"}, &out, &errb); code != 0 {
		t.Fatalf("mark HEAD~1 exit %d err=%q", code, errb.String())
	}
	s := wiki.LoadState(stateDir)
	if s == nil || s.Cursors["master"].LastCommit != want {
		t.Fatalf("落盘游标应为 rev-parse 全 hash %q: %+v", want, s)
	}
	if got := s.Cursors["master"].LastCommit; len(got) != 40 {
		t.Errorf("落盘游标应为 40 位全 hash，got %q", got)
	}
	out.Reset()
	errb.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatalf("status exit %d err=%q", code, errb.String())
	}
	var st map[string]any
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st["branch_state"] != "ok" {
		t.Errorf("mark HEAD~1 后 branch_state 应为 ok: %v", st)
	}
}

// ok wiki mark <非法rev>：fail-fast 返回 1，stderr 报错，wiki.json 不被写入垃圾。
func TestWikiMarkInvalidRevRefused(t *testing.T) {
	_, stateDir := setupWikiProject(t)
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark", "不存在的rev"}, &out, &errb); code != 1 {
		t.Fatalf("非法 rev 应 exit 1，got %d", code)
	}
	if !strings.Contains(errb.String(), "不存在的rev") {
		t.Errorf("stderr 应指出非法 rev: %q", errb.String())
	}
	if _, err := os.Stat(wiki.CursorPath(stateDir)); !os.IsNotExist(err) {
		t.Errorf("非法 rev 不应写入 wiki.json: stat err=%v", err)
	}
}

// ok wiki status：JSON 新增 branch/base_branch/branch_state；has_wiki 语义不变。
func TestWikiStatusBranchFields(t *testing.T) {
	setupWikiProject(t)
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	var st map[string]any
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st["branch"] != "master" || st["base_branch"] != "master" || st["branch_state"] != "ok" {
		t.Errorf("status 分支字段缺失或错误: %v", st)
	}
	if st["has_wiki"] != true {
		t.Errorf("has_wiki 语义漂移: %v", st)
	}
}

// ok wiki diff：dev 分支相对 master 分叉点的结构变化摘要。
func TestWikiDiff(t *testing.T) {
	repo, _ := setupWikiProject(t)
	runGit(t, repo, "checkout", "-q", "-b", "dev")
	if err := os.MkdirAll(filepath.Join(repo, "internal", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "internal", "foo", "a.go"), []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "dev work")
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"base", "master"}, &out, &errb); code != 0 {
		t.Fatalf("base 设置 exit %d err=%q", code, errb.String())
	}
	out.Reset()
	if code := WikiCmd([]string{"diff"}, &out, &errb); code != 0 {
		t.Fatalf("diff exit %d err=%q", code, errb.String())
	}
	got := out.String()
	for _, want := range []string{"基准分支: master", "当前分支: dev", "internal/foo"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff 摘要缺 %q:\n%s", want, got)
		}
	}
}

// ok wiki diff：未设基准分支 → 说明性文本，exit 0（fail-open）。
func TestWikiDiffNoBase(t *testing.T) {
	setupWikiProject(t)
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"diff"}, &out, &errb); code != 0 {
		t.Fatalf("无基准 diff 应 exit 0，got %d err=%q", code, errb.String())
	}
	if !strings.Contains(out.String(), "无法计算分叉点") {
		t.Errorf("应打印无法计算分叉点说明: %q", out.String())
	}
}

// ok wiki status：基准分支上检出"tip 已并入且有差异条目"的分支 → merged_branches；
// 差异条目删除后不再报。
func TestWikiStatusMergedBranches(t *testing.T) {
	repo, stateDir := setupWikiProject(t)
	runGit(t, repo, "checkout", "-q", "-b", "dev")
	runGit(t, repo, "commit", "--allow-empty", "-m", "d1")
	devTip, err := wiki.HeadCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "checkout", "-q", "master")
	runGit(t, repo, "merge", "-q", "--no-ff", "dev", "-m", "merge dev")
	masterTip, err := wiki.HeadCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	// dev 差异条目入库（status 的已并入检测只读已同步的索引库）
	kbRoot := filepath.Dir(stateDir)
	kdir := filepath.Join(kbRoot, "knowledge")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	entryMD := "---\ntitle: 架构总览（dev 分支差异）\ntype: reference\ntags: [wiki, branch:dev]\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n"
	if err := os.WriteFile(filepath.Join(kdir, "dev差异.md"), []byte(entryMD), 0o644); err != nil {
		t.Fatal(err)
	}
	syncKB := func() {
		db, err := index.Open(filepath.Join(kbRoot, "kb.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.Sync(kdir, nil); err != nil {
			t.Fatal(err)
		}
	}
	syncKB()
	if err := wiki.SaveState(stateDir, &wiki.State{BaseBranch: "master", Cursors: map[string]wiki.BranchCursor{
		"master": {LastCommit: masterTip},
		"dev":    {LastCommit: devTip},
	}}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatalf("status exit %d err=%q", code, errb.String())
	}
	var st map[string]any
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	merged, _ := st["merged_branches"].([]any)
	if len(merged) != 1 || merged[0] != "dev" {
		t.Fatalf("应检出 dev 已并入: %v", st)
	}
	// 删掉差异条目后不再报
	if err := os.Remove(filepath.Join(kdir, "dev差异.md")); err != nil {
		t.Fatal(err)
	}
	syncKB()
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	st = map[string]any{}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if _, ok := st["merged_branches"]; ok {
		t.Fatalf("差异条目删除后不得再报 merged_branches: %v", st)
	}
	// 非基准分支（dev 上）即使条目存在也不报
	runGit(t, repo, "checkout", "-q", "dev")
	if err := os.WriteFile(filepath.Join(kdir, "dev差异.md"), []byte(entryMD), 0o644); err != nil {
		t.Fatal(err)
	}
	syncKB()
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	st = map[string]any{}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if _, ok := st["merged_branches"]; ok {
		t.Fatalf("非基准分支不得报 merged_branches: %v", st)
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

// setupOKProject 在隔离 OK_HOME 下初始化 demo 项目并 chdir 进去；
// 返回项目目录与知识库根目录（git 仓库由 initGitForTest 另行初始化）。
func setupOKProject(t *testing.T) (projDir, kbRoot string) {
	t.Helper()
	home, kb := setupProject(t)
	return filepath.Join(home, "demo"), kb
}

// initGitForTest 把 dir 初始化为 git 仓库（master 分支）并提交 commits 个空提交。
func initGitForTest(t *testing.T, dir string, commits int) {
	t.Helper()
	runGit(t, dir, "init", "-b", "master")
	for i := 0; i < commits; i++ {
		runGit(t, dir, "commit", "--allow-empty", "-m", fmt.Sprintf("c%d", i+1))
	}
}

// ok add 在 git 仓库中应自动记录 born:<当前分支> 标签（auto_born 默认开）。
func TestAddAutoBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1) // 当前分支 master
	var out, errb bytes.Buffer
	if code := Add([]string{"--title", "测试条目", "--type", "note"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(kbRoot, "knowledge", "测试条目.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "born:master") {
		t.Errorf("应自动带 born:master: %s", data)
	}
}

// 配置文件显式写 auto_born = false 时，add 不得自动标 born。
func TestAddBornDisabled(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[provenance]\nauto_born = false\n"), 0o644)
	var out, errb bytes.Buffer
	if code := Add([]string{"--title", "测试条目", "--type", "note"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "测试条目.md"))
	if strings.Contains(string(data), "born:") {
		t.Errorf("关闭后不得标 born: %s", data)
	}
}

// 用户显式传入 born 标签时，自动记录不得覆盖也不得叠加。
func TestAddBornNotOverrideExplicit(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	var out, errb bytes.Buffer
	if code := Add([]string{"--title", "测试条目", "--type", "note", "--tags", "born:hotfix"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "测试条目.md"))
	if !strings.Contains(string(data), "born:hotfix") || strings.Contains(string(data), "born:master") {
		t.Errorf("显式 born 不得被覆盖/叠加: %s", data)
	}
}

// ok propose 同样应在创建草稿时自动记录 born 标签。
func TestProposeAutoBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	var out, errb bytes.Buffer
	if code := Propose([]string{"--title", "草稿条目", "--type", "pitfall", "--body", "正文"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(kbRoot, "knowledge", "草稿条目.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "born:master") {
		t.Errorf("propose 应自动带 born: %s", data)
	}
}

// writeEntryFile 在知识库 knowledge 目录下直接落一个条目文件（绕过 Add，模拟存量条目）。
func writeEntryFile(t *testing.T, kbRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "knowledge", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ok backfill-born 应按当前分支给无 born 的存量条目回填，已有 born 的不得覆盖。
func TestBackfillBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	// 两条无 born 条目 + 一条已有 born
	writeEntryFile(t, kbRoot, "老条目1.md", "---\ntitle: 老条目1\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	writeEntryFile(t, kbRoot, "老条目2.md", "---\ntitle: 老条目2\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	writeEntryFile(t, kbRoot, "已标.md", "---\ntitle: 已标\ntype: note\ntags: [\"born:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	var out, errb bytes.Buffer
	in := strings.NewReader("y\n")
	if code := BackfillBorn(nil, in, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "master") || !strings.Contains(out.String(), "2") {
		t.Errorf("预览应含分支与数量: %q", out.String())
	}
	d1, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "老条目1.md"))
	if !strings.Contains(string(d1), "born:master") {
		t.Errorf("老条目1 应被回填: %s", d1)
	}
	d3, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "已标.md"))
	if !strings.Contains(string(d3), "born:dev") || strings.Contains(string(d3), "born:master") {
		t.Errorf("已有 born 不得覆盖: %s", d3)
	}
}

// 预览后输入 n 取消，不得写入任何 born。
func TestBackfillBornAbort(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	writeEntryFile(t, kbRoot, "老条目.md", "---\ntitle: 老条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	var out, errb bytes.Buffer
	in := strings.NewReader("n\n")
	if code := BackfillBorn(nil, in, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "老条目.md"))
	if strings.Contains(string(data), "born:") {
		t.Errorf("取消后不得写入: %s", data)
	}
}

// 非 git 项目无法确定回填分支，应报错退出 1。
func TestBackfillBornNonGit(t *testing.T) {
	setupOKProject(t) // 不 init git
	var out, errb bytes.Buffer
	if code := BackfillBorn(nil, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("非 git 应返回 1，got %d", code)
	}
}

// 回归钉：approve 转正只翻 draft 标志，不得改写/丢失 born（出生以创建时刻为准）。
func TestApproveKeepsBorn(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 1)
	var out, errb bytes.Buffer
	Propose([]string{"--title", "草稿条目", "--type", "note", "--body", "x"}, &out, &errb)
	out.Reset()
	if code := Approve([]string{"草稿条目.md"}, &out, &errb); code != 0 {
		t.Fatalf("approve exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(kbRoot, "knowledge", "草稿条目.md"))
	if !strings.Contains(string(data), "born:master") {
		t.Errorf("approve 不得丢 born: %s", data)
	}
	if strings.Contains(string(data), "draft: true") {
		t.Errorf("approve 后不应仍为草稿")
	}
}

// ok wiki status 检出"已并入基准"的分支时应落盘合并谱系（dev→master），
// 重复执行按 from+commit 判重不得重复记录。
func TestWikiStatusRecordsMerge(t *testing.T) {
	projDir, kbRoot := setupOKProject(t)
	initGitForTest(t, projDir, 2)
	var out, errb bytes.Buffer
	// master 上先 mark：确立基准分支为 master 并记 master 游标
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	runGit(t, projDir, "checkout", "-q", "-b", "dev")
	runGit(t, projDir, "commit", "--allow-empty", "-q", "-m", "d1")
	// dev 差异条目（HasBranchWiki 为真的前提；随后 dev 上 mark 的 db.Sync 收进索引）
	writeEntryFile(t, kbRoot, "差异.md", "---\ntitle: 架构（dev 分支差异）\ntype: reference\ntags: [\"wiki\", \"branch:dev\"]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	out.Reset()
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 { // 在 dev 上记游标
		t.Fatal(code)
	}
	runGit(t, projDir, "checkout", "-q", "master")
	runGit(t, projDir, "merge", "-q", "--no-ff", "dev", "-m", "merge dev")
	out.Reset()
	if code := WikiCmd([]string{"status"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	s := wiki.LoadState(filepath.Join(kbRoot, "state"))
	found := false
	for _, m := range s.Merges {
		if m.From == "dev" && m.To == "master" {
			found = true
		}
	}
	if !found {
		t.Fatalf("谱系应记录 dev→master: %+v", s.Merges)
	}
	// 再次 status 不重复记录
	n1 := len(s.Merges)
	WikiCmd([]string{"status"}, &out, &errb)
	s2 := wiki.LoadState(filepath.Join(kbRoot, "state"))
	if len(s2.Merges) != n1 {
		t.Errorf("重复执行不得重复记录: %d→%d", n1, len(s2.Merges))
	}
}

// ok wiki mark 输出应明示基准分支（"基准分支: X"行）。
func TestWikiMarkPrintsBase(t *testing.T) {
	setupWikiProject(t)
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"mark"}, &out, &errb); code != 0 {
		t.Fatalf("mark exit %d err=%q", code, errb.String())
	}
	if !strings.Contains(out.String(), "已记录 wiki 游标") || !strings.Contains(out.String(), "基准分支: master") {
		t.Errorf("mark 输出应含游标记录与基准分支行: %q", out.String())
	}
}

// ok wiki base 无参：输出当前基准 + 本地候选分支清单；未设基准时提示。
func TestWikiBaseListsCandidates(t *testing.T) {
	setupWikiProject(t)
	var out, errb bytes.Buffer
	if code := WikiCmd([]string{"base"}, &out, &errb); code != 0 {
		t.Fatalf("base 查询 exit %d err=%q", code, errb.String())
	}
	if !strings.Contains(out.String(), "(未设置基准分支)") {
		t.Errorf("未设基准应提示: %q", out.String())
	}
	if !strings.Contains(out.String(), "候选分支:") || !strings.Contains(out.String(), "  master") {
		t.Errorf("应列出本地候选分支 master: %q", out.String())
	}
	out.Reset()
	if code := WikiCmd([]string{"base", "dev"}, &out, &errb); code != 0 {
		t.Fatalf("base 设置 exit %d err=%q", code, errb.String())
	}
	out.Reset()
	if code := WikiCmd([]string{"base"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "基准分支: dev") || !strings.Contains(out.String(), "候选分支:") {
		t.Errorf("设置后查询应显示基准行与候选清单: %q", out.String())
	}
}
