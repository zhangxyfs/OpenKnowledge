package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitCommit 在 dir 提交一个 commit（环境变量固定 author/committer，离线可跑）。
func gitCommit(t *testing.T, dir string, content byte) {
	t.Helper()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte{content}, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "c"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestWikiEndToEnd(t *testing.T) {
	home := t.TempDir()   // OK_HOME
	proj := t.TempDir()   // git 项目
	name := filepath.Base(proj)

	// hook prompt 会后台拉起常驻 daemon（持有 $OK_HOME/daemon.log）：
	// 测试结束前停掉并等它释放文件，否则 Windows 上 TempDir 清理失败。
	defer func() {
		runOK(t, home, proj, "", "daemon", "stop")
		logPath := filepath.Join(home, "daemon.log")
		for i := 0; i < 50; i++ {
			if err := os.Remove(logPath); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Log("warning: daemon.log still locked after daemon stop")
	}()

	// git init + 3 commits
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	initCmd := exec.Command("git", "init")
	initCmd.Dir = proj
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for i := 0; i < 3; i++ {
		gitCommit(t, proj, byte(i))
	}

	// init：注册项目（名字取目录基名）
	if stdout, _, code := runOK(t, home, proj, "", "init"); code != 0 {
		t.Fatalf("init failed: code=%d out=%q", code, stdout)
	}
	kbDir := filepath.Join(home, "projects", name)

	// status：无游标 → has_wiki=false，behind=全历史 3 个 commit
	stdout, _, code := runOK(t, home, proj, "", "wiki", "status")
	if code != 0 || !strings.Contains(stdout, `"has_wiki":false`) || !strings.Contains(stdout, `"behind":3`) {
		t.Fatalf("status before mark: code=%d out=%q", code, stdout)
	}

	// add 两条 wiki 条目（tag wiki → 进入 INDEX.md 的 Wiki 目录）
	for _, title := range []string{"架构总览", "模块说明"} {
		body := filepath.Join(proj, title+".md")
		if err := os.WriteFile(body, []byte(title+"正文。"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, code = runOK(t, home, proj, "", "add", "--title", title, "--type", "reference", "--tags", "wiki,wiki-test", "--file", body); code != 0 {
			t.Fatalf("add %s failed: code=%d", title, code)
		}
	}

	// wiki mark：记录游标到 HEAD
	stdout, _, code = runOK(t, home, proj, "", "wiki", "mark")
	if code != 0 || !strings.Contains(stdout, "已记录 wiki 游标") {
		t.Fatalf("wiki mark: code=%d out=%q", code, stdout)
	}

	// INDEX.md 应含 Wiki 目录节（add 时索引已重建）
	if data, err := os.ReadFile(filepath.Join(kbDir, "INDEX.md")); err != nil || !strings.Contains(string(data), "## Wiki 目录") {
		t.Fatalf("INDEX.md missing Wiki 目录: %v %q", err, data)
	}

	// 追加 1 个 commit → behind=1
	gitCommit(t, proj, 9)
	stdout, _, code = runOK(t, home, proj, "", "wiki", "status")
	if code != 0 || !strings.Contains(stdout, `"has_wiki":true`) || !strings.Contains(stdout, `"behind":1`) {
		t.Fatalf("status after mark+commit: code=%d out=%q", code, stdout)
	}

	// 阈值降为 1 → 下次 prompt 应 nudge（默认 20 时 behind=1 不提示）
	cfgPath := filepath.Join(kbDir, "config.toml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg = append(cfg, []byte("\n[wiki]\nstale_commits = 1\n")...)
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	mkPrompt := func() string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"w1","cwd":%q,"prompt":[{"type":"text","text":"hello"}]}`, proj)
	}
	stdout, _, code = runOK(t, home, proj, mkPrompt(), "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "落后 1 个 commit") {
		t.Fatalf("prompt should nudge stale wiki: code=%d out=%q", code, stdout)
	}

	// 同会话第二次：不再重复提示
	stdout, _, code = runOK(t, home, proj, mkPrompt(), "hook", "prompt")
	if code != 0 || strings.Contains(stdout, "wiki 已落后") {
		t.Fatalf("nudge repeated in same session: code=%d out=%q", code, stdout)
	}
}
