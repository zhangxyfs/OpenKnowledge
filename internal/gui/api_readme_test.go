package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/registry"
)

// mkProjectAt 注册一个项目：项目根目录指向已存在的 dir（README 夹具用），
// 并在 OK_HOME 中创建空 knowledge 目录。可重复调用注册多个项目（读-改-写注册表）。
func mkProjectAt(t *testing.T, okHome, name, dir string) {
	t.Helper()
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil || reg == nil {
		reg = &registry.Registry{}
	}
	if err := reg.AddProject(name, dir); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(okHome, "projects", name, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeKnowledge(t *testing.T, okHome, project, file, content string) {
	t.Helper()
	p := filepath.Join(okHome, "projects", project, "knowledge", file)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type readmeResp struct {
	Found   bool   `json:"found"`
	Source  string `json:"source"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

func getReadme(t *testing.T, base, project string) (int, readmeResp) {
	t.Helper()
	code, data := do(t, "GET", base+"/api/project/readme?project="+project, testToken, nil)
	var res readmeResp
	if code == 200 {
		if err := json.Unmarshal(data, &res); err != nil {
			t.Fatalf("invalid JSON: %v (%s)", err, data)
		}
	}
	return code, res
}

func TestAPIProjectReadme(t *testing.T) {
	h, _, okHome := newEnv(t)
	// readmeProj：根目录有 README.md；enProj：只有 README_EN.md；
	// wikiProj：无 README 但有 wiki 概述条目（另放一条草稿 wiki 与分支差异 wiki，均不应命中）；
	// emptyProj：无 README 无 wiki 条目。
	readmeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# 演示\n\n这是 README 正文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(enDir, "README_EN.md"), []byte("# Demo\n\nEnglish readme.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkProjectAt(t, okHome, "readmeProj", readmeDir)
	mkProjectAt(t, okHome, "enProj", enDir)
	mkProjectAt(t, okHome, "wikiProj", t.TempDir())
	mkProjectAt(t, okHome, "emptyProj", t.TempDir())
	writeKnowledge(t, okHome, "wikiProj", "架构总览.md",
		"---\ntitle: 架构总览\ntype: reference\ntags: [wiki, 架构]\nsummary: 架构\ndraft: false\nmandatory: false\n---\nwiki 概述正文。\n")
	writeKnowledge(t, okHome, "wikiProj", "草稿 wiki.md",
		"---\ntitle: 草稿 wiki\ntype: reference\ntags: [wiki]\nsummary: s\ndraft: true\nmandatory: false\n---\n不应命中。\n")
	writeKnowledge(t, okHome, "wikiProj", "分支差异.md",
		"---\ntitle: 架构总览（dev 分支差异）\ntype: reference\ntags: [wiki, branch:dev]\nsummary: s\ndraft: false\nmandatory: false\n---\n不应命中。\n")

	srv := httptest.NewServer(h)
	defer srv.Close()

	// 1. README.md 命中
	code, res := getReadme(t, srv.URL, "readmeProj")
	if code != 200 || !res.Found || res.Source != "readme" || res.Path != "README.md" ||
		!strings.Contains(res.Content, "这是 README 正文。") {
		t.Fatalf("readmeProj: code=%d res=%+v", code, res)
	}
	// 2. README_EN.md 回落命中
	code, res = getReadme(t, srv.URL, "enProj")
	if code != 200 || !res.Found || res.Source != "readme" || res.Path != "README_EN.md" ||
		!strings.Contains(res.Content, "English readme.") {
		t.Fatalf("enProj: code=%d res=%+v", code, res)
	}
	// 3. wiki 概述回落（草稿与分支差异条目跳过）
	code, res = getReadme(t, srv.URL, "wikiProj")
	if code != 200 || !res.Found || res.Source != "wiki" || res.Title != "架构总览" ||
		!strings.Contains(res.Content, "wiki 概述正文。") ||
		!strings.Contains(res.Path, "projects/wikiProj/knowledge/") {
		t.Fatalf("wikiProj: code=%d res=%+v", code, res)
	}
	// 4. 无 README 无 wiki 条目 → found:false
	code, res = getReadme(t, srv.URL, "emptyProj")
	if code != 200 || res.Found {
		t.Fatalf("emptyProj: code=%d res=%+v", code, res)
	}
	// 5. 参数错误：缺参 400、未注册 404
	if code, _ = getReadme(t, srv.URL, ""); code != 400 {
		t.Fatalf("missing project: code=%d, want 400", code)
	}
	if code, _ = getReadme(t, srv.URL, "ghost"); code != 404 {
		t.Fatalf("unregistered project: code=%d, want 404", code)
	}
	if code, _ = getReadme(t, srv.URL, "../etc"); code != 400 {
		t.Fatalf("traversal project name: code=%d, want 400", code)
	}
}
