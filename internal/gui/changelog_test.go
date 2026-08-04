package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"openknowledge/internal/version"
)

// changelogEnv 搭 root/web（webDir）结构，OK_HOME 隔离；返回 handler 与 root。
func changelogEnv(t *testing.T) (*Handler, string) {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	root := t.TempDir()
	webDir := filepath.Join(root, "web")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"index.html": "<html></html>", "app.js": "", "style.css": "", "favicon.ico": "",
	} {
		if err := os.WriteFile(filepath.Join(webDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(webDir, testToken, nil), root
}

func writeChangelog(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func doJSON(t *testing.T, h *Handler, method, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Ok-Token", testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s -> %d: %s", method, path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func pendingVersions(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["pending"].([]any)
	if !ok {
		t.Fatalf("pending not an array: %v", body["pending"])
	}
	var out []string
	for _, e := range raw {
		out = append(out, e.(map[string]any)["version"].(string))
	}
	return out
}

// withVersion 临时设置 version.Version 并注册恢复。
func withVersion(t *testing.T, v string) {
	t.Helper()
	orig := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = orig })
}

func TestChangelogAPI(t *testing.T) {
	h, root := changelogEnv(t)
	withVersion(t, "2.10.0")
	cl := filepath.Join(root, "changelogs")
	writeChangelog(t, cl, "2.10.0.md", "# 2.10.0\n\n## 新功能\n- 十\n")
	writeChangelog(t, cl, "2.9.0.md", "# 2.9.0\n\n## 修复\n- 九\n")
	writeChangelog(t, cl, "2.2.3.md", "# 2.2.3\n\n- 三\n")
	writeChangelog(t, cl, "2026-07-22-v1.1-setup-toggle.md", "# 旧格式\n") // 应被过滤
	writeChangelog(t, cl, "README.txt", "noise")                            // 应被过滤

	// all：数值升序（2.10.0 > 2.9.0，非字典序），只含 N.N.N.md
	body := doJSON(t, h, "GET", "/api/changelog")
	all := body["all"].([]any)
	if len(all) != 3 {
		t.Fatalf("all len = %d, want 3: %v", len(all), all)
	}
	if all[0].(map[string]any)["version"] != "2.2.3" || all[2].(map[string]any)["version"] != "2.10.0" {
		t.Fatalf("all order wrong: %v", all)
	}
	if body["current"] != "2.10.0" {
		t.Fatalf("current = %v", body["current"])
	}

	// 无 gui.json → pending 为空（首次不弹历史）
	if got := pendingVersions(t, body); len(got) != 0 {
		t.Fatalf("no gui.json: pending = %v, want empty", got)
	}

	// last_seen=2.2.4 → 累计 2.9.0 + 2.10.0（升序，且 log 带内容）
	writeState(t, "2.2.4")
	body = doJSON(t, h, "GET", "/api/changelog")
	got := pendingVersions(t, body)
	if len(got) != 2 || got[0] != "2.9.0" || got[1] != "2.10.0" {
		t.Fatalf("pending = %v, want [2.9.0 2.10.0]", got)
	}

	// 降级（last_seen 比当前新）→ 空
	writeState(t, "9.9.9")
	if got := pendingVersions(t, doJSON(t, h, "GET", "/api/changelog")); len(got) != 0 {
		t.Fatalf("downgrade: pending = %v, want empty", got)
	}

	// POST seen → 写 gui.json；再 GET pending 为空
	writeState(t, "2.2.4")
	doJSON(t, h, "POST", "/api/changelog/seen")
	data, err := os.ReadFile(filepath.Join(os.Getenv("OK_HOME"), "gui.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]string
	if err := json.Unmarshal(data, &st); err != nil || st["last_seen_version"] != "2.10.0" {
		t.Fatalf("gui.json = %q err=%v", data, err)
	}
	if got := pendingVersions(t, doJSON(t, h, "GET", "/api/changelog")); len(got) != 0 {
		t.Fatalf("after seen: pending = %v, want empty", got)
	}

	// current=dev → pending 恒空，POST seen 不写文件
	withVersion(t, "dev")
	writeState(t, "2.2.4")
	if got := pendingVersions(t, doJSON(t, h, "GET", "/api/changelog")); len(got) != 0 {
		t.Fatalf("dev: pending = %v, want empty", got)
	}
	if err := os.Remove(filepath.Join(os.Getenv("OK_HOME"), "gui.json")); err != nil {
		t.Fatal(err)
	}
	doJSON(t, h, "POST", "/api/changelog/seen")
	if _, err := os.Stat(filepath.Join(os.Getenv("OK_HOME"), "gui.json")); !os.IsNotExist(err) {
		t.Fatal("dev: seen should not write gui.json")
	}
}

// writeState 直接写 gui.json（绕过 API）。
func writeState(t *testing.T, seen string) {
	t.Helper()
	data := []byte(`{"last_seen_version":"` + seen + `"}`)
	if err := os.WriteFile(filepath.Join(os.Getenv("OK_HOME"), "gui.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// dev 回退：安装态目录不存在时读 <root>/docs/changelogs。
func TestChangelogDevFallback(t *testing.T) {
	h, root := changelogEnv(t)
	withVersion(t, "2.3.2")
	writeChangelog(t, filepath.Join(root, "docs", "changelogs"), "2.3.2.md", "# 2.3.2\n")
	body := doJSON(t, h, "GET", "/api/changelog")
	all := body["all"].([]any)
	if len(all) != 1 || all[0].(map[string]any)["version"] != "2.3.2" {
		t.Fatalf("dev fallback all = %v", all)
	}
}

// changelogs 目录完全缺失 → all/pending 为空数组且不报错。
func TestChangelogMissingDir(t *testing.T) {
	h, _ := changelogEnv(t)
	withVersion(t, "2.3.2")
	body := doJSON(t, h, "GET", "/api/changelog")
	if len(body["all"].([]any)) != 0 || len(body["pending"].([]any)) != 0 {
		t.Fatalf("missing dir should be empty: %v", body)
	}
}
