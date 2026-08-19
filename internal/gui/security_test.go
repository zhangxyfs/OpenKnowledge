package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"openknowledge/internal/registry"
)

// TestSafeAppURL 校验应用 URL 判定：仅放行 http/https + 本机回环，拒绝可击穿
// PowerShell 单引号包裹的字符与非回环目标。
func TestSafeAppURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://127.0.0.1:8420/?token=0123456789abcdef", true},
		{"http://localhost:8420/", true},
		{"https://127.0.0.1:8420/app", true},
		{"http://[::1]:8420/", true},
		{"http://evil.example.com/", false},
		{"http://169.254.169.254/latest/meta-data", false},
		{"file:///C:/Windows/System32", false},
		{"javascript:alert(1)", false},
		{"", false},
		{"http://127.0.0.1:8420/?token=a'b", false}, // 单引号击穿 PS 包裹
		{"http://127.0.0.1:8420/x\"y", false},
		{"http://127.0.0.1:8420/a\r\nb", false},
	}
	for _, c := range cases {
		if got := safeAppURL(c.url); got != c.want {
			t.Errorf("safeAppURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

// TestValidProjectName 校验项目名形状：与路径穿越/盘符相关的名字一律拒绝，
// 常规名字（含中文、空格）不受影响。
func TestValidProjectName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"OpenKnowledge", true},
		{"my project", true},
		{"项目A", true},
		{"a_b-2", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../escape", false},
		{"..\\escape", false},
		{"/etc", false},
		{`C:\Windows`, false},
		{"C:evil", false},
		{`\\server\share`, false},
	}
	for _, c := range cases {
		if got := validProjectName(c.name); got != c.want {
			t.Errorf("validProjectName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// registerRawName 按给定名字原样追加进注册表（模拟被篡改的 registry.toml；
// 读-改-写，不覆盖已有注册）。
func registerRawName(t *testing.T, name string) {
	t.Helper()
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject(name, filepath.Join(t.TempDir(), "src")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
}

// TestResolveProjectRejectsTraversalName 注册表被毒化（含穿越段的项目名）时，
// HTTP 层必须 400 拒绝，而不是拿去拼项目目录。
func TestResolveProjectRejectsTraversalName(t *testing.T) {
	h, _, okHome := newEnv(t)
	// 正常项目先注册（mkProject 会整档重写注册表，须在毒化条目之前）
	mkProject(t, okHome, "demo")
	registerRawName(t, "../escape")
	_ = os.MkdirAll(filepath.Join(okHome, "escape", "knowledge"), 0o755)

	srv := httptest.NewServer(h)
	defer srv.Close()
	code, body := do(t, "GET", srv.URL+"/api/entries?project="+url.QueryEscape("../escape"), testToken, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("穿越项目名应 400, got %d: %s", code, body)
	}
	code, _ = do(t, "GET", srv.URL+"/api/entries?project=demo", testToken, nil)
	if code != http.StatusOK {
		t.Fatalf("正常项目应 200, got %d", code)
	}
}

// TestProjectDeleteRejectsTraversalName 删除接口面对毒化注册名必须 400 且
// 不得删除 projects 目录之外的任何内容。
func TestProjectDeleteRejectsTraversalName(t *testing.T) {
	h, _, okHome := newEnv(t)
	registerRawName(t, "../victim")
	outside := filepath.Join(okHome, "victim")
	marker := filepath.Join(outside, "marker.txt")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()
	code, body := do(t, "DELETE", srv.URL+"/api/project?project="+url.QueryEscape("../victim"), testToken, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("穿越项目名删除应 400, got %d: %s", code, body)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("projects 目录外的文件被删除了:", err)
	}
}
