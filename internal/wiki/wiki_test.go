package wiki

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// gitRepo 建一个含 n 个 commit 的临时 git 仓库，返回目录。
func gitRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte{byte(i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", "c")
	}
	return dir
}

func TestCursorPath(t *testing.T) {
	dir := t.TempDir()
	if CursorPath(dir) != filepath.Join(dir, "wiki.json") {
		t.Fatalf("unexpected path %q", CursorPath(dir))
	}
}

func TestCheckStatusNoStateFile(t *testing.T) {
	repo := gitRepo(t, 3)
	st := CheckStatus(t.TempDir(), repo, 20)
	if st.HasWiki || st.Behind != 3 || st.Stale {
		t.Fatalf("no cursor: %+v", st)
	}
	st = CheckStatus(t.TempDir(), repo, 3)
	if !st.Stale {
		t.Fatalf("full history >= threshold should be stale: %+v", st)
	}
}

func TestCheckStatusBehind(t *testing.T) {
	repo := gitRepo(t, 2)
	head, err := HeadCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	// 旧格式游标（顶层 last_commit）：CheckStatus 判定可达后视同归入当前分支
	legacy := `{"last_commit":"` + head + `","generated_at":"2026-08-08T09:00:00+08:00"}`
	if err := os.WriteFile(CursorPath(stateDir), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	// 再追加 2 个 commit
	env := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte{byte(10 + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "."}, {"commit", "-m", "c"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = repo
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	st := CheckStatus(stateDir, repo, 2)
	if !st.HasWiki || st.LastCommit != head || st.Behind != 2 || !st.Stale {
		t.Fatalf("behind: %+v", st)
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &State{
		BaseBranch: "master",
		Cursors: map[string]BranchCursor{
			"master": {LastCommit: "abc123", GeneratedAt: time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC), EntryCount: 15},
			"dev":    {LastCommit: "def456", GeneratedAt: time.Date(2026, 8, 8, 2, 0, 0, 0, time.UTC), EntryCount: 3},
		},
	}
	if err := SaveState(dir, s); err != nil {
		t.Fatal(err)
	}
	got := LoadState(dir)
	if got == nil || got.BaseBranch != "master" || len(got.Cursors) != 2 {
		t.Fatalf("回环失败: %+v", got)
	}
	if got.Cursors["dev"].LastCommit != "def456" || got.Cursors["master"].EntryCount != 15 {
		t.Errorf("游标内容错位: %+v", got.Cursors)
	}
	if got.Legacy != nil {
		t.Errorf("新格式不得带 Legacy: %+v", got.Legacy)
	}
	// 落盘文本不得含旧顶层字段
	data, _ := os.ReadFile(filepath.Join(dir, "wiki.json"))
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["last_commit"]; ok {
		t.Errorf("新格式不应含顶层 last_commit: %s", data)
	}
}

func TestLoadStateLegacyDetected(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"last_commit":"d9f495c","generated_at":"2026-08-08T09:36:47+08:00","entry_count":15}`
	if err := os.WriteFile(filepath.Join(dir, "wiki.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadState(dir)
	if got == nil || got.Legacy == nil {
		t.Fatalf("旧格式应识别为 Legacy: %+v", got)
	}
	if got.Legacy.LastCommit != "d9f495c" || got.Legacy.EntryCount != 15 {
		t.Errorf("Legacy 内容错误: %+v", got.Legacy)
	}
	if got.Cursors != nil || got.BaseBranch != "" {
		t.Errorf("Legacy 未判归属前 Cursors/BaseBranch 应为空: %+v", got)
	}
}

func TestLoadStateMissingAndCorrupt(t *testing.T) {
	if LoadState(t.TempDir()) != nil {
		t.Error("不存在应返回 nil")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wiki.json"), []byte("{坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadState(dir) != nil {
		t.Error("损坏应返回 nil")
	}
}

func TestCheckStatusGitUnavailable(t *testing.T) {
	// 非 git 目录：behind = -1，不 stale
	st := CheckStatus(t.TempDir(), t.TempDir(), 20)
	if st.Behind != -1 || st.Stale {
		t.Fatalf("non-git: %+v", st)
	}
	// 游标 commit 已不在历史（伪造 hash，旧格式文件）
	stateDir := t.TempDir()
	legacy := `{"last_commit":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}`
	if err := os.WriteFile(CursorPath(stateDir), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st = CheckStatus(stateDir, gitRepo(t, 1), 20)
	if !st.HasWiki || st.Behind != -1 || st.Stale {
		t.Fatalf("lost commit: %+v", st)
	}
}
