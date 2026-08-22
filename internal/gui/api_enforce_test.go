package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnforceRulesAPI enforce 规则的读写契约：GET 读合并配置（无规则给 []），
// POST 校验后整体重写项目 config.toml 的 [[enforce]]；空数组合法（清空）。
func TestEnforceRulesAPI(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	type rule struct {
		Type          string   `json:"type"`
		CodeGlobs     []string `json:"code_globs"`
		ChangelogGlob string   `json:"changelog_glob"`
		Message       string   `json:"message"`
	}
	getRules := func() (int, []rule, []byte) {
		t.Helper()
		code, data := do(t, "GET", srv.URL+"/api/enforce/rules?project=demo", testToken, nil)
		var res struct {
			Rules []rule `json:"rules"`
		}
		if code == 200 {
			if err := json.Unmarshal(data, &res); err != nil {
				t.Fatal(err)
			}
		}
		return code, res.Rules, data
	}

	// 初始 GET → {"rules":[]}（空数组而非 null）
	code, rules, data := getRules()
	if code != 200 {
		t.Fatalf("initial get: status = %d, body %s", code, data)
	}
	if len(rules) != 0 || !strings.Contains(string(data), `"rules":[]`) {
		t.Fatalf("initial rules must be empty array: %s", data)
	}

	// POST 2 条 → 200；GET 复读到 2 条
	two := []rule{
		{Type: "changelog", CodeGlobs: []string{"**/*.go"}, ChangelogGlob: "docs/changelogs/**", Message: "改代码必须写变更日志"},
		{Type: "changelog", CodeGlobs: []string{"web/**"}, ChangelogGlob: "docs/changelogs/**", Message: "前端也要写"},
	}
	code, data = do(t, "POST", srv.URL+"/api/enforce/rules", testToken, map[string]any{"project": "demo", "rules": two})
	if code != 200 {
		t.Fatalf("post rules: status = %d, body %s", code, data)
	}
	code, rules, _ = getRules()
	if code != 200 || len(rules) != 2 || rules[0].Message != "改代码必须写变更日志" || rules[1].ChangelogGlob != "docs/changelogs/**" {
		t.Fatalf("get after post: code=%d rules=%+v", code, rules)
	}

	// 校验：type 非法 / code_globs 空 / message 空 → 400
	badBodies := []map[string]any{
		{"project": "demo", "rules": []rule{{Type: "bogus", CodeGlobs: []string{"**/*.go"}, Message: "x"}}},
		{"project": "demo", "rules": []rule{{Type: "changelog", Message: "x"}}},
		{"project": "demo", "rules": []rule{{Type: "changelog", CodeGlobs: []string{"**/*.go"}}}},
	}
	for _, body := range badBodies {
		if code, data := do(t, "POST", srv.URL+"/api/enforce/rules", testToken, body); code != 400 {
			t.Fatalf("body %v: status = %d, want 400 (body %s)", body, code, data)
		}
	}
	// 校验失败不得破坏既有规则
	if _, rules, _ = getRules(); len(rules) != 2 {
		t.Fatalf("failed posts must not clobber rules: %+v", rules)
	}

	// 空数组合法（清空规则）→ GET 回到 []
	code, data = do(t, "POST", srv.URL+"/api/enforce/rules", testToken, map[string]any{"project": "demo", "rules": []rule{}})
	if code != 200 {
		t.Fatalf("clear rules: status = %d, body %s", code, data)
	}
	if _, rules, _ = getRules(); len(rules) != 0 {
		t.Fatalf("rules should be cleared: %+v", rules)
	}
}

// TestEnforceRulesGlobalDefault project 缺省（不传/空串）→ [[enforce]] 读写全局
// config.toml；注册项目的 config.toml 不得被写。
func TestEnforceRulesGlobalDefault(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()
	globalCfg := filepath.Join(okHome, "config.toml")
	projCfg := filepath.Join(okHome, "projects", "demo", "config.toml")

	// 缺省 GET → 空数组（非 null）
	code, data := do(t, "GET", srv.URL+"/api/enforce/rules", testToken, nil)
	if code != 200 || !strings.Contains(string(data), `"rules":[]`) {
		t.Fatalf("global rules get: status = %d, body %s", code, data)
	}
	// 缺省 POST → 全局落盘
	code, data = do(t, "POST", srv.URL+"/api/enforce/rules", testToken,
		map[string]any{"rules": []map[string]any{{
			"type": "changelog", "code_globs": []string{"**/*.go"},
			"changelog_glob": "docs/changelogs/**", "message": "改代码必须写变更日志",
		}}})
	if code != 200 {
		t.Fatalf("global rules set: status = %d, body %s", code, data)
	}
	gData, err := os.ReadFile(globalCfg)
	if err != nil {
		t.Fatalf("global config not written: %v", err)
	}
	if !strings.Contains(string(gData), "[[enforce]]") || !strings.Contains(string(gData), "改代码必须写变更日志") {
		t.Fatalf("global config should contain enforce rules: %q", gData)
	}
	// 项目 config 不得被写
	if _, err := os.Stat(projCfg); !os.IsNotExist(err) {
		t.Fatalf("project config must stay untouched, stat err = %v", err)
	}
	// 缺省 GET 复读 → 1 条
	code, data = do(t, "GET", srv.URL+"/api/enforce/rules?project=", testToken, nil)
	if code != 200 || !strings.Contains(string(data), "改代码必须写变更日志") {
		t.Fatalf("global rules re-get: status = %d, body %s", code, data)
	}
	// 校验在全局分支同样生效（非法 type → 400）
	code, _ = do(t, "POST", srv.URL+"/api/enforce/rules", testToken,
		map[string]any{"rules": []map[string]any{{"type": "bogus", "code_globs": []string{"x"}, "message": "y"}}})
	if code != 400 {
		t.Fatalf("invalid global rule: status = %d, want 400", code)
	}
}
