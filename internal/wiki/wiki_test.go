package wiki

import (
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

func TestCursorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if LoadCursor(dir) != nil {
		t.Fatal("expected nil cursor for missing file")
	}
	c := &Cursor{LastCommit: "abc123", GeneratedAt: time.Now().Truncate(time.Second), EntryCount: 7}
	if err := SaveCursor(dir, c); err != nil {
		t.Fatal(err)
	}
	got := LoadCursor(dir)
	if got == nil || got.LastCommit != "abc123" || got.EntryCount != 7 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if CursorPath(dir) != filepath.Join(dir, "wiki.json") {
		t.Fatalf("unexpected path %q", CursorPath(dir))
	}
}

func TestLoadCursorCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(CursorPath(dir), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadCursor(dir) != nil {
		t.Fatal("corrupt cursor should load as nil")
	}
}

func TestCheckStatusNoCursor(t *testing.T) {
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
	if err := SaveCursor(stateDir, &Cursor{LastCommit: head, GeneratedAt: time.Now()}); err != nil {
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

func TestCheckStatusGitUnavailable(t *testing.T) {
	// 非 git 目录：behind = -1，不 stale
	st := CheckStatus(t.TempDir(), t.TempDir(), 20)
	if st.Behind != -1 || st.Stale {
		t.Fatalf("non-git: %+v", st)
	}
	// 游标 commit 已不在历史（伪造 hash）
	stateDir := t.TempDir()
	if err := SaveCursor(stateDir, &Cursor{LastCommit: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}); err != nil {
		t.Fatal(err)
	}
	st = CheckStatus(stateDir, gitRepo(t, 1), 20)
	if !st.HasWiki || st.Behind != -1 || st.Stale {
		t.Fatalf("lost commit: %+v", st)
	}
}
