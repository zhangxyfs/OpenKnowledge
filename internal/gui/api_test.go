package gui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/entry"
	"openknowledge/internal/registry"
	"openknowledge/internal/setupx"
)

const testToken = "0123456789abcdef0123456789abcdef"

// newEnv 隔离 OK_HOME / KIMI_CODE_HOME / OK_SKILLS_HOME 并搭建 webDir，
// 返回 handler 与各目录路径。
func newEnv(t *testing.T) (*Handler, string, string) {
	t.Helper()
	okHome := t.TempDir()
	t.Setenv("OK_HOME", okHome)
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	webDir := t.TempDir()
	files := map[string]string{
		"index.html": "<html>token={{TOKEN}}</html>",
		"app.js":     "console.log(1)",
		"style.css":  "body{}",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(webDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(webDir, testToken, nil), webDir, okHome
}

// mkProject 在 OK_HOME 中注册一个项目并创建空 knowledge 目录。
func mkProject(t *testing.T, okHome, name string) {
	t.Helper()
	reg := &registry.Registry{}
	if err := reg.AddProject(name, filepath.Join(t.TempDir(), "src")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(okHome, "projects", name, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// do 发请求并按需带令牌与 JSON body，返回状态码与响应体。
func do(t *testing.T, method, url, token string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Ok-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

func TestStatusEmptyRegistry(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if code != 200 {
		t.Fatalf("status = %d, body %s", code, data)
	}
	var st struct {
		Projects            []any  `json:"projects"`
		HooksInstalled      bool   `json:"hooksInstalled"`
		SkillsInstalled     bool   `json:"skillsInstalled"`
		EmbeddingConfigured bool   `json:"embeddingConfigured"`
		Disabled            bool   `json:"disabled"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Projects) != 0 {
		t.Fatalf("expected empty projects, got %v", st.Projects)
	}
	if st.HooksInstalled || st.SkillsInstalled || st.EmbeddingConfigured || st.Disabled {
		t.Fatalf("expected all flags false, got %+v", st)
	}
}

func TestAuthRequired(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, tok := range []string{"", "wrong-token"} {
		for _, path := range []string{"/api/status", "/api/projects", "/api/entries?project=x"} {
			code, data := do(t, "GET", srv.URL+path, tok, nil)
			if code != 401 {
				t.Fatalf("GET %s token=%q: status = %d, body %s", path, tok, code, data)
			}
			if !strings.Contains(string(data), "error") {
				t.Fatalf("GET %s token=%q: expected JSON error, got %s", path, tok, data)
			}
		}
	}
}

func TestIndexTokenInjection(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "GET", srv.URL+"/", "", nil)
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	body := string(data)
	if !strings.Contains(body, "token="+testToken) {
		t.Fatalf("token not injected: %s", body)
	}
	if strings.Contains(body, "{{TOKEN}}") {
		t.Fatalf("placeholder left in output: %s", body)
	}
}

func TestStaticAllowlist(t *testing.T) {
	h, webDir, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, p := range []string{"/app.js", "/style.css"} {
		if code, _ := do(t, "GET", srv.URL+p, "", nil); code != 200 {
			t.Fatalf("GET %s: status = %d", p, code)
		}
	}
	// webDir 中其他文件不可访问
	if err := os.WriteFile(filepath.Join(webDir, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/secret.txt", "/index.html/extra", "/nope.js"} {
		if code, _ := do(t, "GET", srv.URL+p, "", nil); code != 404 {
			t.Fatalf("GET %s: status = %d, want 404", p, code)
		}
	}
}

func entryPayload(title string) map[string]any {
	return map[string]any{
		"project":   "demo",
		"title":     title,
		"type":      "note",
		"tags":      []string{"橙子", "测试"},
		"mandatory": false,
		"summary":   "关于橙子的摘要",
		"body":      "橙子是一种水果，富含维C。",
	}
}

func TestEntryCRUD(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 创建
	code, data := do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("橙子种植"))
	if code != 200 {
		t.Fatalf("create: status = %d, body %s", code, data)
	}
	var created struct {
		File  string `json:"file"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	if created.File == "" || created.Title != "橙子种植" {
		t.Fatalf("unexpected created entry: %+v", created)
	}
	knowledge := filepath.Join(okHome, "projects", "demo", "knowledge")
	if _, err := os.Stat(filepath.Join(knowledge, created.File)); err != nil {
		t.Fatalf("entry file not on disk: %v", err)
	}

	// 列表
	code, data = do(t, "GET", srv.URL+"/api/entries?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("list: status = %d, body %s", code, data)
	}
	var list []struct {
		File     string   `json:"file"`
		Title    string   `json:"title"`
		Type     string   `json:"type"`
		Tags     []string `json:"tags"`
		Mandatory bool    `json:"mandatory"`
		Summary  string   `json:"summary"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].File != created.File || list[0].Type != "note" || len(list[0].Tags) != 2 {
		t.Fatalf("unexpected list: %s", data)
	}

	// 详情
	code, data = do(t, "GET", srv.URL+"/api/entry?project=demo&file="+created.File, testToken, nil)
	if code != 200 {
		t.Fatalf("detail: status = %d, body %s", code, data)
	}
	var detail struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(data, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Title != "橙子种植" || !strings.Contains(detail.Body, "维C") {
		t.Fatalf("unexpected detail: %s", data)
	}

	// 更新
	upd := entryPayload("橙子种植（修订）")
	upd["file"] = created.File
	upd["body"] = "更新后的正文。"
	code, data = do(t, "PUT", srv.URL+"/api/entry", testToken, upd)
	if code != 200 {
		t.Fatalf("update: status = %d, body %s", code, data)
	}
	code, data = do(t, "GET", srv.URL+"/api/entry?project=demo&file="+created.File, testToken, nil)
	if code != 200 || !strings.Contains(string(data), "更新后的正文") {
		t.Fatalf("after update: status = %d, body %s", code, data)
	}

	// 删除
	code, data = do(t, "DELETE", srv.URL+"/api/entry?project=demo&file="+created.File, testToken, nil)
	if code != 200 {
		t.Fatalf("delete: status = %d, body %s", code, data)
	}
	if _, err := os.Stat(filepath.Join(knowledge, created.File)); !os.IsNotExist(err) {
		t.Fatalf("file should be removed, stat err = %v", err)
	}
	code, data = do(t, "GET", srv.URL+"/api/entries?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("list after delete: status = %d", code)
	}
	if strings.Contains(string(data), created.File) {
		t.Fatalf("entry still listed after delete: %s", data)
	}
	// 索引同步：INDEX.md 应已重建
	if _, err := os.Stat(filepath.Join(okHome, "projects", "demo", "INDEX.md")); err != nil {
		t.Fatalf("INDEX.md not rebuilt: %v", err)
	}
}

func TestEntryDuplicate409(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	if code, data := do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("重复条目")); code != 200 {
		t.Fatalf("first create: status = %d, body %s", code, data)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("重复条目"))
	if code != 409 {
		t.Fatalf("duplicate create: status = %d, body %s", code, data)
	}
	if !strings.Contains(string(data), "error") {
		t.Fatalf("expected JSON error, got %s", data)
	}
}

func TestEntryValidation(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	bad := entryPayload("")
	if code, data := do(t, "POST", srv.URL+"/api/entry", testToken, bad); code != 400 {
		t.Fatalf("empty title: status = %d, body %s", code, data)
	}
	bad = entryPayload("x")
	bad["type"] = "bogus"
	if code, data := do(t, "POST", srv.URL+"/api/entry", testToken, bad); code != 400 {
		t.Fatalf("bad type: status = %d, body %s", code, data)
	}
	bad = entryPayload("x")
	delete(bad, "project")
	if code, data := do(t, "POST", srv.URL+"/api/entry", testToken, bad); code != 400 {
		t.Fatalf("missing project: status = %d, body %s", code, data)
	}
	bad = entryPayload("x")
	bad["project"] = "ghost"
	if code, data := do(t, "POST", srv.URL+"/api/entry", testToken, bad); code != 404 {
		t.Fatalf("unknown project: status = %d, body %s", code, data)
	}
}

func TestEntryFileTraversal(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, f := range []string{"../registry.toml", "..\\registry.toml", "a/b.md", "a\\b.md"} {
		code, _ := do(t, "GET", srv.URL+"/api/entry?project=demo&file="+f, testToken, nil)
		if code != 400 {
			t.Fatalf("GET file=%q: status = %d, want 400", f, code)
		}
		code, _ = do(t, "DELETE", srv.URL+"/api/entry?project=demo&file="+f, testToken, nil)
		if code != 400 {
			t.Fatalf("DELETE file=%q: status = %d, want 400", f, code)
		}
	}
}

func TestSearchHit(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	if code, data := do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("橙子种植")); code != 200 {
		t.Fatalf("create: status = %d, body %s", code, data)
	}
	code, data := do(t, "GET", srv.URL+"/api/search?project=demo&q=橙子", testToken, nil)
	if code != 200 {
		t.Fatalf("search: status = %d, body %s", code, data)
	}
	var hits []struct {
		File  string  `json:"file"`
		Title string  `json:"title"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal(data, &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "橙子种植" || hits[0].Score <= 0 {
		t.Fatalf("expected hit, got %s", data)
	}
}

func TestToggle(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "POST", srv.URL+"/api/toggle", testToken, map[string]any{"on": false})
	if code != 200 {
		t.Fatalf("off: status = %d, body %s", code, data)
	}
	if _, err := os.Stat(filepath.Join(okHome, "hooks-disabled")); err != nil {
		t.Fatalf("hooks-disabled flag missing: %v", err)
	}
	// status 应反映关闭状态
	_, data = do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if !strings.Contains(string(data), `"disabled":true`) {
		t.Fatalf("status should show disabled: %s", data)
	}

	code, data = do(t, "POST", srv.URL+"/api/toggle", testToken, map[string]any{"on": true})
	if code != 200 {
		t.Fatalf("on: status = %d, body %s", code, data)
	}
	if _, err := os.Stat(filepath.Join(okHome, "hooks-disabled")); !os.IsNotExist(err) {
		t.Fatalf("hooks-disabled flag should be removed, err = %v", err)
	}
}

func TestSetupHooksAndSkills(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "POST", srv.URL+"/api/setup/hooks", testToken, nil)
	if code != 200 {
		t.Fatalf("setup/hooks: status = %d, body %s", code, data)
	}
	cfg, err := os.ReadFile(filepath.Join(setupx.KimiHome(), "config.toml"))
	if err != nil {
		t.Fatalf("hooks config not written: %v", err)
	}
	if !strings.Contains(string(cfg), setupx.MarkerBegin) || !strings.Contains(string(cfg), "hook prompt") {
		t.Fatalf("hooks block missing: %s", cfg)
	}

	code, data = do(t, "POST", srv.URL+"/api/setup/skills", testToken, nil)
	if code != 200 {
		t.Fatalf("setup/skills: status = %d, body %s", code, data)
	}
	skill, err := os.ReadFile(filepath.Join(setupx.SkillsHome(), "openknowledge-init", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	if !strings.Contains(string(skill), "openknowledge-init") {
		t.Fatalf("unexpected skill content: %s", skill)
	}
}

func TestEmbeddingBadURL(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "POST", srv.URL+"/api/setup/embedding", testToken, map[string]any{
		"base_url": "http://127.0.0.1:1",
		"model":    "m",
		"api_key":  "sekret-key",
	})
	if code != 200 {
		t.Fatalf("status = %d, body %s", code, data)
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatalf("expected ok=false against dead server: %s", data)
	}
	// 配置应已落盘（SaveEmbedding 先于 TestEmbedding）
	if _, err := os.Stat(filepath.Join(okHome, "config.toml")); err != nil {
		t.Fatalf("global config not saved: %v", err)
	}
	// 响应不得包含 key
	if strings.Contains(string(data), "sekret-key") {
		t.Fatalf("api key leaked in response: %s", data)
	}
}

func TestHeartbeat(t *testing.T) {
	beats := make(chan struct{}, 1)
	okHome := t.TempDir()
	t.Setenv("OK_HOME", okHome)
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	h := NewHandler(t.TempDir(), testToken, beats)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/heartbeat", testToken, nil)
	if code != 204 {
		t.Fatalf("status = %d", code)
	}
	select {
	case <-beats:
	default:
		t.Fatal("heartbeat did not signal beats channel")
	}
}

func TestShutdown(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/shutdown", testToken, nil)
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	select {
	case <-h.Done():
	default:
		t.Fatal("shutdown did not close Done channel")
	}
}

func TestProjects(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "GET", srv.URL+"/api/projects", testToken, nil)
	if code != 200 {
		t.Fatalf("status = %d, body %s", code, data)
	}
	var projects []struct {
		Name  string   `json:"name"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(data, &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "demo" || len(projects[0].Paths) != 1 {
		t.Fatalf("unexpected projects: %s", data)
	}
}

// writeDraft 在 demo 项目的 knowledge 目录手写一个草稿条目文件。
func writeDraft(t *testing.T, okHome, title, body string) string {
	t.Helper()
	e := &entry.Entry{
		Title:   title,
		Type:    "pitfall",
		Tags:    []string{"测试"},
		Draft:   true,
		Summary: title,
		Body:    body,
	}
	file := entry.Slug(title) + ".md"
	path := filepath.Join(okHome, "projects", "demo", "knowledge", file)
	if err := os.WriteFile(path, e.Serialize(), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestApproveDraft(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	file := writeDraft(t, okHome, "测试坑", "Git Bash 下路径必须正斜杠。")

	// 条目列表带 draft:true
	code, data := do(t, "GET", srv.URL+"/api/entries?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("list: status = %d, body %s", code, data)
	}
	var list []struct {
		File  string `json:"file"`
		Draft bool   `json:"draft"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].File != file || !list[0].Draft {
		t.Fatalf("expected draft entry in list, got %s", data)
	}
	// 详情也带 draft:true
	code, data = do(t, "GET", srv.URL+"/api/entry?project=demo&file="+file, testToken, nil)
	if code != 200 || !strings.Contains(string(data), `"draft":true`) {
		t.Fatalf("detail should show draft:true: status = %d, body %s", code, data)
	}
	// 检索不命中草稿
	code, data = do(t, "GET", srv.URL+"/api/search?project=demo&q=测试坑", testToken, nil)
	if code != 200 || strings.Contains(string(data), "测试坑") {
		t.Fatalf("draft must not be searchable: status = %d, body %s", code, data)
	}

	// approve → draft:false，检索可命中
	code, data = do(t, "POST", srv.URL+"/api/approve", testToken,
		map[string]any{"project": "demo", "file": file})
	if code != 200 {
		t.Fatalf("approve: status = %d, body %s", code, data)
	}
	if strings.Contains(string(data), `"draft":true`) {
		t.Fatalf("approve response should show draft:false: %s", data)
	}
	disk, err := os.ReadFile(filepath.Join(okHome, "projects", "demo", "knowledge", file))
	if err != nil {
		t.Fatal(err)
	}
	e, err := entry.Parse(disk)
	if err != nil {
		t.Fatal(err)
	}
	if e.Draft || e.Title != "测试坑" || e.Body != "Git Bash 下路径必须正斜杠。" {
		t.Fatalf("approve must flip draft and preserve fields, got %+v", e)
	}
	code, data = do(t, "GET", srv.URL+"/api/search?project=demo&q=测试坑", testToken, nil)
	if code != 200 || !strings.Contains(string(data), "测试坑") {
		t.Fatalf("approved entry should be searchable: status = %d, body %s", code, data)
	}

	// 重复 approve（非草稿）→ 400
	code, _ = do(t, "POST", srv.URL+"/api/approve", testToken,
		map[string]any{"project": "demo", "file": file})
	if code != 400 {
		t.Fatalf("re-approve non-draft: status = %d, want 400", code)
	}
	// approve 不存在的文件 → 400
	code, _ = do(t, "POST", srv.URL+"/api/approve", testToken,
		map[string]any{"project": "demo", "file": "nope.md"})
	if code != 400 {
		t.Fatalf("approve missing file: status = %d, want 400", code)
	}
	// approve 路径穿越 → 400
	code, _ = do(t, "POST", srv.URL+"/api/approve", testToken,
		map[string]any{"project": "demo", "file": "../x.md"})
	if code != 400 {
		t.Fatalf("approve traversal: status = %d, want 400", code)
	}
}

func TestCaptureRoundTrip(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 默认 GET：propose / turn_interval 5
	code, data := do(t, "GET", srv.URL+"/api/capture?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("capture get: status = %d, body %s", code, data)
	}
	var cap1 struct {
		Mode         string `json:"mode"`
		TurnInterval int    `json:"turn_interval"`
	}
	if err := json.Unmarshal(data, &cap1); err != nil {
		t.Fatal(err)
	}
	if cap1.Mode != "propose" || cap1.TurnInterval != 5 {
		t.Fatalf("unexpected defaults: %s", data)
	}

	// 非法模式 → 400
	code, _ = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "mode": "bogus"})
	if code != 400 {
		t.Fatalf("invalid mode: status = %d, want 400", code)
	}

	// 设 auto → 项目 config.toml 出现 [capture]；GET 反映新模式
	code, data = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "mode": "auto"})
	if code != 200 {
		t.Fatalf("capture set: status = %d, body %s", code, data)
	}
	cfgPath := filepath.Join(okHome, "projects", "demo", "config.toml")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("project config not written: %v", err)
	}
	if !strings.Contains(string(cfgData), "[capture]") || !strings.Contains(string(cfgData), `mode = "auto"`) {
		t.Fatalf("config should contain capture section: %q", cfgData)
	}
	code, data = do(t, "GET", srv.URL+"/api/capture?project=demo", testToken, nil)
	if code != 200 || !strings.Contains(string(data), `"mode":"auto"`) {
		t.Fatalf("capture should read auto after set: status = %d, body %s", code, data)
	}

	// 再设回 propose → 替换而非重复追加
	code, data = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "mode": "propose"})
	if code != 200 {
		t.Fatalf("capture set propose: status = %d, body %s", code, data)
	}
	cfgData, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(cfgData), "[capture]") != 1 {
		t.Fatalf("capture section should not be duplicated: %q", cfgData)
	}
}
