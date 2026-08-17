package index

import "strings"

// BranchOf 提取条目的分支标签（branch:<名>，取第一个）；无则空串。
func BranchOf(tags []string) string {
	for _, t := range tags {
		if strings.HasPrefix(t, "branch:") {
			return strings.TrimPrefix(t, "branch:")
		}
	}
	return ""
}

// splitTags 把 entries.tags 列（", " 拼接）拆回切片。
func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ", ")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// hasWikiTag 判定 tags 列是否含精确的 wiki 标签。不能用 strings.Contains/LIKE
// '%wiki%' 子串匹配——`sewiki`/`nowiki` 这类标签会被误判为 wiki 条目。
func hasWikiTag(tags string) bool {
	for _, t := range splitTags(tags) {
		if t == "wiki" {
			return true
		}
	}
	return false
}

// FilterHitsByBranch 丢弃其他分支的差异条目；branch 为空（非 git/未知）不过滤（宁多勿漏）。
func FilterHitsByBranch(hits []Hit, branch string) []Hit {
	if branch == "" {
		return hits
	}
	out := hits[:0]
	for _, h := range hits {
		if b := BranchOf(h.Tags); b != "" && b != branch {
			continue
		}
		out = append(out, h)
	}
	return out
}

// TrimIndexBranchSections 裁剪 INDEX.md 的"## 分支差异（X）"小节：只保留 branch 的，
// 其余整节移除；branch 为空、无差异小节或全程未裁任何节时逐字节返回原文
// （零回归 + 幂等：重复调用结果稳定，不会每次多一个尾部换行）。
// 小节边界：下一个 "## " 级标题或 EOF。
func TrimIndexBranchSections(idx, branch string) string {
	if branch == "" || !strings.Contains(idx, "## 分支差异（") {
		return idx
	}
	lines := strings.Split(idx, "\n")
	var b strings.Builder
	dropping := false
	dropped := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "## ") {
			dropping = false
			if strings.HasPrefix(ln, "## 分支差异（") {
				name := strings.TrimPrefix(ln, "## 分支差异（")
				if i := strings.Index(name, "）"); i >= 0 {
					name = name[:i]
				}
				dropping = name != branch
			}
		}
		if !dropping {
			b.WriteString(ln)
			b.WriteString("\n")
		} else {
			dropped = true
		}
	}
	// 全程未裁任何节：直接返回原文字节（重组会在尾部多拼一个 "\n"，破坏幂等）
	if !dropped {
		return idx
	}
	// 重组后规整末尾换行：与原文末尾形态保持一致（原文无末换行则去掉，有则恰好一个）
	return strings.TrimSuffix(b.String(), "\n") + trailingNL(idx)
}

// trailingNL 保留原文末尾换行形态。
func trailingNL(s string) string {
	if strings.HasSuffix(s, "\n") {
		return "\n"
	}
	return ""
}
