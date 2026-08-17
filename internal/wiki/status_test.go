package wiki

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/procx"
)

// initRepo 建临时 git 仓库并做 n 个提交；返回目录。
func initRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-b", "master")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "c0")
	for i := 1; i < n; i++ {
		run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "c")
	}
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	procx.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	h, err := HeadCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCurrentBranch(t *testing.T) {
	dir := initRepo(t, 1)
	if b := CurrentBranch(dir); b != "master" {
		t.Errorf("应为 master，got %q", b)
	}
	if b := CurrentBranch(t.TempDir()); b != "" {
		t.Errorf("非 git 应为空，got %q", b)
	}
}

// ResolveRevision：HEAD~1 / 短 hash 归一化为 40 位全 hash；非法 rev 返回错误。
func TestResolveRevision(t *testing.T) {
	dir := initRepo(t, 2)
	head := headOf(t, dir)
	full, err := ResolveRevision(dir, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 40 || full == head {
		t.Errorf("HEAD~1 应解析为前一提交的全 hash，got %q", full)
	}
	// 短 hash 应补全为同一全 hash
	short, err := ResolveRevision(dir, full[:7])
	if err != nil || short != full {
		t.Errorf("短 hash %q 应补全为 %q，got %q err=%v", full[:7], full, short, err)
	}
	if _, err := ResolveRevision(dir, "no-such-rev"); err == nil {
		t.Error("非法 rev 应返回错误")
	}
}

func TestCheckStatusSameBranchUnchanged(t *testing.T) {
	dir := initRepo(t, 3)
	sd := t.TempDir()
	// 游标 = 第 1 个提交之后 → 落后 2
	run(t, dir, "checkout", "-q", "-b", "work", "HEAD~2")
	c0 := headOf(t, dir)
	run(t, dir, "checkout", "-q", "master")
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"master": {LastCommit: c0}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.Branch != "master" || st.BaseBranch != "master" || st.BranchState != "ok" {
		t.Fatalf("基准分支应 ok: %+v", st)
	}
	if !st.HasWiki || st.Behind != 2 || !st.Stale {
		t.Errorf("behind/stale 语义漂移: %+v", st)
	}
}

func TestCheckStatusDiverged(t *testing.T) {
	dir := initRepo(t, 2)
	base := headOf(t, dir)
	run(t, dir, "checkout", "-q", "-b", "dev")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "dev1")
	// master 上也前进一格制造分叉
	run(t, dir, "checkout", "-q", "master")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "m1")
	run(t, dir, "checkout", "-q", "dev")
	sd := t.TempDir()
	// dev 的游标指向 master 独有提交（不可达 dev HEAD）
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"dev": {LastCommit: headOfAt(t, dir, "master")}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "diverged" {
		t.Fatalf("应为 diverged: %+v", st)
	}
	if st.MergeBase != base {
		t.Errorf("分叉点应为 %s，got %q", base, st.MergeBase)
	}
	if st.Behind != -1 {
		t.Errorf("分叉时 Behind 应为 -1，got %d", st.Behind)
	}
}

func TestCheckStatusGone(t *testing.T) {
	dir := initRepo(t, 1)
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"master": {LastCommit: "0123456789abcdef0123456789abcdef01234567"}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "gone" || !st.HasWiki {
		t.Fatalf("应为 gone: %+v", st)
	}
}

func TestCheckStatusNoCursor(t *testing.T) {
	dir := initRepo(t, 1)
	master := headOf(t, dir)
	run(t, dir, "checkout", "-q", "-b", "dev")
	sd := t.TempDir()
	if err := SaveState(sd, &State{BaseBranch: "master", Cursors: map[string]BranchCursor{"master": {LastCommit: master}}}); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "no_cursor" || !st.HasWiki || st.Branch != "dev" {
		t.Fatalf("应为 no_cursor: %+v", st)
	}
	if st.LastCommit != master {
		t.Errorf("no_cursor 应展示基准分支游标: %+v", st)
	}
}

func TestCheckStatusEmptyCursorsFile(t *testing.T) {
	// {"base_branch":"dev"}：ok wiki base 首次落盘、尚无任何游标 →
	// 应走"无 wiki"现状全历史路径（与无状态文件同语义），否则 no-wiki 提示被永久抑制。
	dir := initRepo(t, 2)
	sd := t.TempDir()
	if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(`{"base_branch":"dev"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.HasWiki || st.Behind != 2 || !st.Stale {
		t.Fatalf("空 cursors 状态文件应走全历史现状路径: %+v", st)
	}
	if st.BaseBranch != "dev" || st.Branch != "master" {
		t.Errorf("BaseBranch/Branch 应透传: %+v", st)
	}
	if st.BranchState != "" {
		t.Errorf("现状路径不应带分支状态: %+v", st)
	}
}

func TestCheckStatusExplicitEmptyCursors(t *testing.T) {
	// {"cursors":{}} 显式空表：与空 cursors 状态文件同语义（锁定边界）。
	dir := initRepo(t, 2)
	sd := t.TempDir()
	if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(`{"cursors":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.HasWiki || st.Behind != 2 || !st.Stale {
		t.Fatalf("显式空 cursors 应走全历史现状路径: %+v", st)
	}
}

func TestAttributeLegacy(t *testing.T) {
	// 可达：归入当前分支并保留 GeneratedAt/EntryCount。
	dir := initRepo(t, 2)
	c0 := headOfAt(t, dir, "HEAD~1")
	gen := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	s := &State{Legacy: &BranchCursor{LastCommit: c0, GeneratedAt: gen, EntryCount: 5}}
	if !AttributeLegacy(s, dir) {
		t.Fatal("可达 legacy 应归入当前分支")
	}
	cur, ok := s.Cursors["master"]
	if !ok || cur.LastCommit != c0 || cur.EntryCount != 5 || !cur.GeneratedAt.Equal(gen) {
		t.Errorf("归入内容错误: %+v", s.Cursors)
	}
	// 不可达：不入任何分支，State 不被改动。
	dir2 := initRepo(t, 1)
	run(t, dir2, "checkout", "-q", "-b", "tmp")
	run(t, dir2, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "orphan")
	orphan := headOf(t, dir2)
	run(t, dir2, "checkout", "-q", "master")
	run(t, dir2, "branch", "-q", "-D", "tmp")
	s2 := &State{Legacy: &BranchCursor{LastCommit: orphan, EntryCount: 3}}
	if AttributeLegacy(s2, dir2) {
		t.Error("不可达 legacy 不得归入")
	}
	if len(s2.Cursors) != 0 {
		t.Errorf("不可达时 Cursors 应保持为空: %+v", s2.Cursors)
	}
	// git 不可判（非 git 目录）与空 legacy 同样不归。
	if AttributeLegacy(&State{Legacy: &BranchCursor{LastCommit: c0}}, t.TempDir()) {
		t.Error("非 git 目录不得归入")
	}
	if AttributeLegacy(&State{}, dir) {
		t.Error("无 Legacy 应返回 false")
	}
}

func TestCheckStatusLegacyReachable(t *testing.T) {
	dir := initRepo(t, 3)
	sd := t.TempDir()
	legacy := `{"last_commit":"` + headOfAt(t, dir, "HEAD~2") + `","generated_at":"2026-08-08T09:00:00+08:00","entry_count":5}`
	if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "ok" || st.Behind != 2 {
		t.Fatalf("可达 legacy 应视同归入当前分支: %+v", st)
	}
	// 不写盘：文件内容仍是旧格式
	data, _ := os.ReadFile(filepath.Join(sd, "wiki.json"))
	if string(data) != legacy {
		t.Error("CheckStatus 不得写盘")
	}
}

func TestCheckStatusLegacyOrphan(t *testing.T) {
	dir := initRepo(t, 1)
	sd := t.TempDir()
	// 存在但不可达的 commit：在 dir 内建 dangling commit（分支删除后对象仍在库中）。
	// 注意不能用另建仓库的 hash——同秒同作者的空提交会产生相同 hash，变成可达。
	run(t, dir, "checkout", "-q", "-b", "tmp")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "orphan")
	orphan := headOf(t, dir)
	run(t, dir, "checkout", "-q", "master")
	run(t, dir, "branch", "-q", "-D", "tmp")
	legacy := `{"last_commit":"` + orphan + `","generated_at":"2026-08-08T09:00:00+08:00","entry_count":5}`
	if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if st.BranchState != "legacy_orphan" || !st.HasWiki || st.Behind != -1 {
		t.Fatalf("应为 legacy_orphan: %+v", st)
	}
}

func TestCheckStatusLegacyEmptyCommit(t *testing.T) {
	// 非 git 项目旧版 mark 写空 last_commit：键存在即旧格式，走旧行为路径
	legacy := `{"last_commit":"","generated_at":"2026-08-08T09:00:00+08:00","entry_count":3}`
	cases := map[string]func(t *testing.T) string{
		"非 git 目录": func(t *testing.T) string { return t.TempDir() },
		"git 仓库":   func(t *testing.T) string { return initRepo(t, 1) },
	}
	for name, mkSrc := range cases {
		t.Run(name, func(t *testing.T) {
			sd := t.TempDir()
			if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(legacy), 0o644); err != nil {
				t.Fatal(err)
			}
			s := LoadState(sd)
			if s == nil || s.Legacy == nil || s.Legacy.LastCommit != "" {
				t.Fatalf("空 last_commit 旧格式应挂 Legacy（空串）: %+v", s)
			}
			st := CheckStatus(sd, mkSrc(t), 1)
			if !st.HasWiki || st.Behind != -1 || st.BranchState != "" {
				t.Fatalf("空 commit legacy 应走旧行为路径: %+v", st)
			}
		})
	}
}

func TestCheckStatusLegacyCommitMissing(t *testing.T) {
	// Legacy 指向不存在的 commit（40 位假 hash）→ 归属不可判，报 legacy_orphan
	//（v2.18.2 起；此前与"git 不可判"混同、BranchState 空、nudge 永不触发）
	dir := initRepo(t, 1)
	sd := t.TempDir()
	legacy := `{"last_commit":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef","generated_at":"2026-08-08T09:00:00+08:00","entry_count":3}`
	if err := os.WriteFile(filepath.Join(sd, "wiki.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st := CheckStatus(sd, dir, 1)
	if !st.HasWiki || st.Behind != -1 || st.BranchState != "legacy_orphan" {
		t.Fatalf("commit 不存在的 legacy 应报 legacy_orphan: %+v", st)
	}
}

func headOfAt(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", rev)
	procx.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSpace(out))
}
