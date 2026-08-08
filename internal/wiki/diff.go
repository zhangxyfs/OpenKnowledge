package wiki

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// dirStat 单目录的增删文件计数（DiffSummary 聚类用；包级声明以便排序 helper 引用）。
type dirStat struct{ add, del int }

// DiffSummary 输出当前分支相对基准分叉点的结构变化摘要（供 openknowledge-wiki 技能消化）。
// base 为空/分叉点不可算/非 git 时返回 ("", nil)——fail-open，由调用方打印说明。
func DiffSummary(srcDir, base string) (string, error) {
	branch := CurrentBranch(srcDir)
	if branch == "" || base == "" {
		return "", nil
	}
	mb := mergeBase(srcDir, base, "HEAD")
	if mb == "" {
		return "", nil
	}
	ns, err := gitOut(srcDir, "diff", "--name-status", mb+"..HEAD")
	if err != nil {
		return "", err
	}
	num, _ := gitOut(srcDir, "diff", "--numstat", mb+"..HEAD") // 失败不致命（Top-N 留空）

	var b strings.Builder
	fmt.Fprintf(&b, "基准分支: %s（分叉点 %s）\n当前分支: %s\n\n", base, short(mb), branch)

	// 目录与文件聚类
	dirs := map[string]*dirStat{}
	exts := map[string][2]int{} // ext -> [新增, 删除]
	for _, ln := range strings.Split(ns, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		status, path := f[0], f[len(f)-1]
		top := strings.SplitN(path, "/", 2)[0]
		d := dirs[top]
		if d == nil {
			d = &dirStat{}
			dirs[top] = d
		}
		ext := ""
		if i := strings.LastIndex(path, "."); i >= 0 {
			ext = path[i:]
		}
		e := exts[ext]
		switch status[0] {
		case 'A':
			d.add++
			exts[ext] = [2]int{e[0] + 1, e[1]}
		case 'D':
			d.del++
			exts[ext] = [2]int{e[0], e[1] + 1}
		}
	}
	b.WriteString("目录变化:\n")
	for _, n := range sortedKeysDS(dirs) {
		d := dirs[n]
		fmt.Fprintf(&b, "  %s（+%d/-%d 文件）\n", n, d.add, d.del)
	}
	b.WriteString("文件增删:\n")
	for _, e := range sortedKeysExt(exts) {
		c := exts[e]
		fmt.Fprintf(&b, "  %s +%d/-%d\n", extOrNone(e), c[0], c[1])
	}
	// Top-10 变更文件
	type nf struct {
		path     string
		add, del int
	}
	var tops []nf
	for _, ln := range strings.Split(num, "\n") {
		f := strings.Fields(ln)
		if len(f) != 3 {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		tops = append(tops, nf{f[2], a, d})
	}
	sort.Slice(tops, func(i, j int) bool { return tops[i].add+tops[i].del > tops[j].add+tops[j].del })
	if len(tops) > 10 {
		tops = tops[:10]
	}
	if len(tops) > 0 {
		b.WriteString("变更最多:\n")
		for _, t := range tops {
			fmt.Fprintf(&b, "  %s（+%d/-%d）\n", t.path, t.add, t.del)
		}
	}
	return b.String(), nil
}

// MergedIntoBase 返回已并入基准的分支清单：cursors 中每条非基准分支，
// 分支引用仍存在、tip 已是 HEAD 祖先、且 hasDelta 报告有差异条目时计入。
// 仅在当前处于基准分支时由调用方触发；分支已删除的静默跳过。
func MergedIntoBase(s *State, srcDir string, hasDelta func(string) bool) []string {
	if s == nil {
		return nil
	}
	var out []string
	for name := range s.Cursors {
		if name == "" || name == s.BaseBranch {
			continue
		}
		if _, err := gitOut(srcDir, "rev-parse", "--verify", "--quiet", name); err != nil {
			continue // 分支已删
		}
		if !isAncestor(srcDir, name, "HEAD") {
			continue
		}
		if hasDelta != nil && !hasDelta(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// short 截 commit hash 前 7 位（不足则原样返回）。
func short(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// sortedKeysDS 返回目录统计 map 的排序键。
func sortedKeysDS(m map[string]*dirStat) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeysExt 返回扩展名统计 map 的排序键。
func sortedKeysExt(m map[string][2]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// extOrNone 空扩展名显示为 "(无扩展名)"。
func extOrNone(ext string) string {
	if ext == "" {
		return "(无扩展名)"
	}
	return ext
}
