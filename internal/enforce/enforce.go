package enforce

import (
	"github.com/bmatcuk/doublestar/v4"

	"openknowledge/internal/config"
	"openknowledge/internal/state"
)

// EvalChangelog 判定 changelog_required 规则：触碰过 code_globs 且未触碰
// changelog_glob → 阻断（同会话同规则已被阻断过则放行，防死循环）。
func EvalChangelog(rule config.EnforceRule, st *state.Session) (block bool, reason string) {
	if st.HasBlocked(rule.Type) {
		return false, ""
	}
	code := false
	for _, p := range st.Touched {
		if ok, _ := doublestar.Match(rule.ChangelogGlob, p); ok {
			return false, ""
		}
		if !code {
			for _, g := range rule.CodeGlobs {
				if ok, _ := doublestar.Match(g, p); ok {
					code = true
					break
				}
			}
		}
	}
	if !code {
		return false, ""
	}
	return true, rule.Message
}
