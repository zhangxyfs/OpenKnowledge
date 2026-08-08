package wiki

import (
	"os/exec"
	"strings"

	"openknowledge/internal/procx"
)

// CurrentBranch 返回 srcDir 当前分支名；detach 为 "DETACHED@<short>"；
// 非 git 仓库或命令失败返回 ""（fail-open）。
func CurrentBranch(srcDir string) string {
	if out, err := gitOut(srcDir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && out != "" {
		return out
	}
	head, err := gitOut(srcDir, "rev-parse", "--short", "HEAD")
	if err != nil || head == "" {
		return ""
	}
	return "DETACHED@" + head
}

func gitOut(srcDir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", srcDir}, args...)...)
	procx.HideWindow(cmd)
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// commitExists 报告 commit 在仓库中是否存在。
func commitExists(srcDir, commit string) bool {
	_, err := gitOut(srcDir, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	return err == nil
}

// isAncestor 报告 a 是否是 b 的祖先（a 可达 b）。
func isAncestor(srcDir, a, b string) bool {
	cmd := exec.Command("git", "-C", srcDir, "merge-base", "--is-ancestor", a, b)
	procx.HideWindow(cmd)
	return cmd.Run() == nil
}

// AttributeLegacy 把旧格式游标按 git 可达性归入当前分支（与 CheckStatus 同语义）：
// 当前分支可判、legacy commit 存在且为 HEAD 祖先时，写入 s.Cursors[当前分支]
// （保留 GeneratedAt/EntryCount）并返回 true；其余情况（分叉/非 git/游标失效/
// 无 Legacy）不改动 s、返回 false。供写盘路径（ok wiki base）落盘前调用，
// 保证旧 last_commit 不丢（spec §4）。
func AttributeLegacy(s *State, srcDir string) bool {
	if s == nil || s.Legacy == nil {
		return false
	}
	branch := CurrentBranch(srcDir)
	lc := s.Legacy.LastCommit
	if branch == "" || !commitExists(srcDir, lc) || !isAncestor(srcDir, lc, "HEAD") {
		return false
	}
	if s.Cursors == nil {
		s.Cursors = map[string]BranchCursor{}
	}
	s.Cursors[branch] = *s.Legacy
	return true
}

// mergeBase 返回两引用的分叉点；无共同祖先返回空串。
func mergeBase(srcDir, a, b string) string {
	out, err := gitOut(srcDir, "merge-base", a, b)
	if err != nil {
		return ""
	}
	return out
}
