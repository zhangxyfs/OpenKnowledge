package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 写文件（覆盖），父目录需已存在。
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkdirWrite 写文件并先建齐父目录。
func mkdirWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiffSummary(t *testing.T) {
	dir := initRepo(t, 2)
	run(t, dir, "checkout", "-q", "-b", "dev")
	// dev 上：新增目录 internal/foo 两个文件 + 改根目录 README
	mkdirWrite(t, dir, "internal/foo/a.go", "package foo\n")
	mkdirWrite(t, dir, "internal/foo/b.go", "package foo\n")
	writeFile(t, dir, "README.md", "v2\n")
	run(t, dir, "add", ".")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "dev work")
	out, err := DiffSummary(dir, "master")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"master", "dev", "internal/foo", "README.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("摘要缺 %q:\n%s", want, out)
		}
	}
}

func TestDiffSummaryNoBase(t *testing.T) {
	dir := initRepo(t, 1)
	// 定死行为：base 不存在/无共同祖先 → 返回 ("", nil)（fail-open，
	// 由 cli 层打印"无法计算分叉点"说明）
	out, err := DiffSummary(dir, "不存在的分支")
	if err != nil {
		t.Fatalf("base 不存在不得报错（fail-open）: %v", err)
	}
	if out != "" {
		t.Fatalf("base 不存在应返回空摘要，got %q", out)
	}
	// 空基准（未设置）与非 git 目录同样返回 ("", nil)
	if out, err := DiffSummary(dir, ""); err != nil || out != "" {
		t.Fatalf("空基准应返回 (\"\", nil)，got (%q, %v)", out, err)
	}
	if out, err := DiffSummary(t.TempDir(), "master"); err != nil || out != "" {
		t.Fatalf("非 git 目录应返回 (\"\", nil)，got (%q, %v)", out, err)
	}
}

func TestMergedIntoBase(t *testing.T) {
	dir := initRepo(t, 2)
	run(t, dir, "checkout", "-q", "-b", "dev")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "d1")
	run(t, dir, "checkout", "-q", "master")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "merge", "-q", "--no-ff", "dev", "-m", "merge dev")
	s := &State{BaseBranch: "master", Cursors: map[string]BranchCursor{
		"master": {LastCommit: headOf(t, dir)},
		"dev":    {LastCommit: headOfAt(t, dir, "dev")},
	}}
	hasDelta := func(b string) bool { return b == "dev" }
	got := MergedIntoBase(s, dir, hasDelta)
	if len(got) != 1 || got[0] != "dev" {
		t.Fatalf("应检出 dev 已并入: %v", got)
	}
	// 无差异条目则不报
	if got := MergedIntoBase(s, dir, func(string) bool { return false }); len(got) != 0 {
		t.Fatalf("无差异条目不报: %v", got)
	}
	// 分支已删除的静默跳过：再加一条指向已删分支的游标
	s.Cursors["tmp"] = BranchCursor{LastCommit: headOfAt(t, dir, "dev")}
	if got := MergedIntoBase(s, dir, func(string) bool { return true }); len(got) != 1 || got[0] != "dev" {
		t.Fatalf("已删分支应静默跳过: %v", got)
	}
	// 未并入的分支不报：feature 领先 master，tip 不是 HEAD 祖先
	run(t, dir, "checkout", "-q", "-b", "feature")
	run(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "f1")
	run(t, dir, "checkout", "-q", "master")
	s.Cursors["feature"] = BranchCursor{LastCommit: headOfAt(t, dir, "feature")}
	if got := MergedIntoBase(s, dir, func(string) bool { return true }); len(got) != 1 || got[0] != "dev" {
		t.Fatalf("未并入分支不得报: %v", got)
	}
	// nil State 防御
	if got := MergedIntoBase(nil, dir, hasDelta); got != nil {
		t.Fatalf("nil State 应返回 nil: %v", got)
	}
}

// hasDelta 前置顺序：hasDelta 判定必须先于 git spawn（rev-parse / merge-base）。
// 反证法：srcDir 不存在时任何 git 调用必然失败走"分支已删"短路——若 hasDelta
// 在 git 之后判定，hasDelta 永远不会被调用；前置时每条非基准游标都会先问
// hasDelta，false 直接跳过、0 次 spawn 且不报错。
func TestMergedIntoBaseHasDeltaBeforeGit(t *testing.T) {
	s := &State{BaseBranch: "master", Cursors: map[string]BranchCursor{
		"master": {LastCommit: "x"},
		"dev":    {LastCommit: "y"},
	}}
	var asked []string
	got := MergedIntoBase(s, filepath.Join(t.TempDir(), "不存在的目录"), func(b string) bool {
		asked = append(asked, b)
		return false
	})
	if len(got) != 0 {
		t.Fatalf("hasDelta false 不得报 merged: %v", got)
	}
	if len(asked) != 1 || asked[0] != "dev" {
		t.Fatalf("hasDelta 应先于 git 被调用（不存在的 srcDir 下仍问到 dev）: %v", asked)
	}
}
