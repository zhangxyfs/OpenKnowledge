package gui

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/registry"
)

// enforceRuleJSON 是 [[enforce]] 规则的前端契约形状（字段对应 config.EnforceRule）。
type enforceRuleJSON struct {
	Type          string   `json:"type"`
	CodeGlobs     []string `json:"code_globs"`
	ChangelogGlob string   `json:"changelog_glob"`
	Message       string   `json:"message"`
}

// apiEnforceRulesGet 返回项目合并配置的 [[enforce]] 规则；无规则给 []（非 null）。
func (h *Handler) apiEnforceRulesGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	rules := make([]enforceRuleJSON, 0, len(cfg.Enforce))
	for _, e := range cfg.Enforce {
		rules = append(rules, enforceRuleJSON{
			Type: e.Type, CodeGlobs: e.CodeGlobs,
			ChangelogGlob: e.ChangelogGlob, Message: e.Message,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// apiEnforceRulesSet 整体重写项目 config.toml 的 [[enforce]]：逐条校验
// （type 仅允许 changelog、code_globs 非空、message 非空，违反 400）；
// 空数组合法（清空规则）。校验全过才落盘，失败不动既有配置。
func (h *Handler) apiEnforceRulesSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string            `json:"project"`
		Rules   []enforceRuleJSON `json:"rules"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	for _, rule := range req.Rules {
		if rule.Type != "changelog" {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法 type %q（仅允许 changelog）", rule.Type))
			return
		}
		if len(rule.CodeGlobs) == 0 {
			writeErr(w, http.StatusBadRequest, "code_globs 不能为空")
			return
		}
		if strings.TrimSpace(rule.Message) == "" {
			writeErr(w, http.StatusBadRequest, "message 不能为空")
			return
		}
	}
	rules := make([]config.EnforceRule, 0, len(req.Rules))
	for _, rule := range req.Rules {
		rules = append(rules, config.EnforceRule{
			Type: rule.Type, CodeGlobs: rule.CodeGlobs,
			ChangelogGlob: rule.ChangelogGlob, Message: rule.Message,
		})
	}
	if err := config.SetEnforceRules(st.ConfigPath(), rules); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
