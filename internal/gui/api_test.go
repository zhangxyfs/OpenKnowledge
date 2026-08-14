package gui

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/agentx"
	"openknowledge/internal/entry"
	"openknowledge/internal/registry"
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
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
	webDir := t.TempDir()
	files := map[string]string{
		"index.html":  "<html>token={{TOKEN}}</html>",
		"app.js":      "console.log(1)",
		"style.css":   "body{}",
		"favicon.ico": "ico",
		"help.md":     "# 帮助\n",
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

// mkGitProject 注册一个项目，其注册路径指向新建的临时 git 仓库（master，1 个提交）。
func mkGitProject(t *testing.T, okHome, name string) {
	t.Helper()
	repo := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-b", "master")
	runGit("commit", "--allow-empty", "-m", "c1")
	reg := &registry.Registry{}
	if err := reg.AddProject(name, repo); err != nil {
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
	var res struct {
		Projects []any `json:"projects"`
		Agents   []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Detected       bool   `json:"detected"`
			HooksInstalled bool   `json:"hooksInstalled"`
		} `json:"agents"`
		SkillsInstalled     bool `json:"skillsInstalled"`
		EmbeddingConfigured bool `json:"embeddingConfigured"`
		Disabled            bool `json:"disabled"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Projects) != 0 {
		t.Fatalf("expected empty projects, got %v", res.Projects)
	}
	if len(res.Agents) != 10 {
		t.Fatalf("expected 10 agents, got %d: %s", len(res.Agents), data)
	}
	if res.SkillsInstalled || res.EmbeddingConfigured || res.Disabled {
		t.Fatalf("expected flags false, got %+v", res)
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

// TestStaticNoCache 静态页与 index 必须禁缓存：升级后浏览器若继续用旧 app.js，
// 新 index.html 的控件（如 agents 下拉）会失去数据填充，界面"空掉"。
func TestStaticNoCache(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, p := range []string{"/", "/app.js", "/style.css", "/help.md"} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		cc := res.Header.Get("Cache-Control")
		res.Body.Close()
		if !strings.Contains(cc, "no-cache") {
			t.Fatalf("GET %s: Cache-Control = %q, want no-cache", p, cc)
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

	for _, p := range []string{"/app.js", "/style.css", "/favicon.ico", "/help.md"} {
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

// TestHelpMdServed 使用帮助页 help.md 作为白名单静态资源可直接 GET：
// 前端"使用帮助"卡点击后 fetch 此文件复用 changelog 弹窗渲染。
func TestHelpMdServed(t *testing.T) {
	h, _, _ := newEnv(t)
	req := httptest.NewRequest("GET", "/help.md", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("help.md 应 200，got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "# 帮助") {
		t.Errorf("内容不符: %q", rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("静态资源必须 no-cache")
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
		File      string   `json:"file"`
		Title     string   `json:"title"`
		Type      string   `json:"type"`
		Tags      []string `json:"tags"`
		Mandatory bool     `json:"mandatory"`
		Summary   string   `json:"summary"`
		Mtime     int64    `json:"mtime"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].File != created.File || list[0].Type != "note" || len(list[0].Tags) != 2 {
		t.Fatalf("unexpected list: %s", data)
	}
	if list[0].Mtime <= 0 {
		t.Fatalf("list item should carry mtime: %s", data)
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

// TestEntryCreateBorn GUI 新建条目按项目注册路径探测分支自动记 born；
// 用户显式传 born 不覆盖；auto_born=false 后不标。
func TestEntryCreateBorn(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkGitProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	readEntry := func(title string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(okHome, "projects", "demo", "knowledge", title+".md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	// git 夹具项目下创建 → 落盘文件含 born:master
	code, data := do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("架构约定"))
	if code != 200 {
		t.Fatalf("create: status = %d, body %s", code, data)
	}
	if got := readEntry("架构约定"); !strings.Contains(got, "born:master") {
		t.Errorf("应自动带 born:master: %s", got)
	}

	// 显式 born 不覆盖也不叠加
	explicit := entryPayload("显式born")
	explicit["tags"] = []string{"born:hotfix"}
	code, data = do(t, "POST", srv.URL+"/api/entry", testToken, explicit)
	if code != 200 {
		t.Fatalf("create explicit: status = %d, body %s", code, data)
	}
	if got := readEntry("显式born"); !strings.Contains(got, "born:hotfix") || strings.Contains(got, "born:master") {
		t.Errorf("显式 born 不得被覆盖/叠加: %s", got)
	}

	// auto_born=false → 不标
	cfgPath := filepath.Join(okHome, "projects", "demo", "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[provenance]\nauto_born = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, data = do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("关闭后不标"))
	if code != 200 {
		t.Fatalf("create disabled: status = %d, body %s", code, data)
	}
	if got := readEntry("关闭后不标"); strings.Contains(got, "born:") {
		t.Errorf("auto_born=false 不得标 born: %s", got)
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
	cfg, err := os.ReadFile(filepath.Join(agentx.KimiHome(), "config.toml"))
	if err != nil {
		t.Fatalf("hooks config not written: %v", err)
	}
	if !strings.Contains(string(cfg), agentx.MarkerBegin) || !strings.Contains(string(cfg), "hook prompt") {
		t.Fatalf("hooks block missing: %s", cfg)
	}

	code, data = do(t, "POST", srv.URL+"/api/setup/hooks", testToken, map[string]any{"agent": "pi"})
	if code != 200 {
		t.Fatalf("setup/hooks pi: status = %d, body %s", code, data)
	}
	ext, err := os.ReadFile(filepath.Join(agentx.PiHome(), "extensions", "openknowledge.ts"))
	if err != nil {
		t.Fatalf("pi extension not written: %v", err)
	}
	if !strings.Contains(string(ext), `"hook", "prompt"`) {
		t.Fatalf("unexpected pi extension content: %.200s", ext)
	}
	code, _ = do(t, "POST", srv.URL+"/api/setup/hooks", testToken, map[string]any{"agent": "nope"})
	if code != 400 {
		t.Fatalf("unknown agent should be 400, got %d", code)
	}

	code, data = do(t, "POST", srv.URL+"/api/setup/skills", testToken, nil)
	if code != 200 {
		t.Fatalf("setup/skills: status = %d, body %s", code, data)
	}
	skill, err := os.ReadFile(filepath.Join(agentx.SkillsHome(), "openknowledge-init", "SKILL.md"))
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

	code, data := do(t, "POST", srv.URL+"/api/setup/embedding/profile", testToken, map[string]any{
		"name":     "a",
		"type":     "openai",
		"base_url": "http://127.0.0.1:1",
		"model":    "m",
		"api_key":  "sekret-key",
	})
	if code != 200 {
		t.Fatalf("status = %d, body %s", code, data)
	}
	// 配置应已落盘（保存先于连通性测试）
	if _, err := os.Stat(filepath.Join(okHome, "config.toml")); err != nil {
		t.Fatalf("global config not saved: %v", err)
	}
	// 死服务器连通性测试应 ok=false
	code, data = do(t, "POST", srv.URL+"/api/setup/embedding/test", testToken, map[string]any{"name": "a"})
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
	// 响应不得包含 key
	if strings.Contains(string(data), "sekret-key") {
		t.Fatalf("api key leaked in response: %s", data)
	}
}

// 留空 api_key 应保留已保存的 key；GET /api/setup/embedding 的 profiles
// 应回填 base_url/model/has_key（不回显 key 本体）。
func TestEmbeddingEmptyKeyKeepsExisting(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 先保存一个 key
	if code, data := do(t, "POST", srv.URL+"/api/setup/embedding/profile", testToken, map[string]any{
		"name":     "a",
		"type":     "openai",
		"base_url": "http://127.0.0.1:1",
		"model":    "m1",
		"api_key":  "sekret-key",
	}); code != 200 {
		t.Fatalf("seed save failed: %s", data)
	}
	// 留空 key + 改 model → 保留 sekret-key
	code, data := do(t, "POST", srv.URL+"/api/setup/embedding/profile", testToken, map[string]any{
		"name":     "a",
		"type":     "openai",
		"base_url": "http://127.0.0.1:1",
		"model":    "m2",
		"api_key":  "",
	})
	if code != 200 {
		t.Fatalf("empty key keep-existing: status = %d, body %s", code, data)
	}
	cfgData, err := os.ReadFile(filepath.Join(okHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgData), "sekret-key") || !strings.Contains(string(cfgData), `model = "m2"`) || !strings.Contains(string(cfgData), "[[embedding.profiles]]") {
		t.Fatalf("key should be kept and model updated: %q", cfgData)
	}

	// GET /api/setup/embedding 的 profiles 回填 base_url/model/has_key（不回显 key 本体）
	code, data = do(t, "GET", srv.URL+"/api/setup/embedding", testToken, nil)
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	var st struct {
		Profiles []struct {
			BaseURL string `json:"base_url"`
			Model   string `json:"model"`
			HasKey  bool   `json:"has_key"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Profiles) != 1 || st.Profiles[0].BaseURL != "http://127.0.0.1:1" || st.Profiles[0].Model != "m2" || !st.Profiles[0].HasKey {
		t.Fatalf("profiles embedding wrong: %s", data)
	}
	if strings.Contains(string(data), "sekret-key") {
		t.Fatalf("api key leaked in embedding get: %s", data)
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
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	select {
	case <-beats:
	default:
		t.Fatal("heartbeat did not signal beats channel")
	}
}

func TestHeartbeatVersion(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 无 kb.db 时 version 为 0
	code, data := do(t, "POST", srv.URL+"/api/heartbeat?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	var res struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if res.Version != 0 {
		t.Fatalf("expected version 0 without kb.db, got %d", res.Version)
	}
	// 有 kb.db 时返回其 mtime（非 0）
	kb := filepath.Join(okHome, "projects", "demo", "kb.db")
	if err := os.WriteFile(kb, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, data = do(t, "POST", srv.URL+"/api/heartbeat?project=demo", testToken, nil)
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if res.Version == 0 {
		t.Fatal("expected non-zero version with kb.db present")
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

	// 设置 turn_interval（模式保持）
	code, data = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "turn_interval": 10})
	if code != 200 {
		t.Fatalf("capture interval: status = %d, body %s", code, data)
	}
	cfgData, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgData), "turn_interval = 10") || !strings.Contains(string(cfgData), `mode = "propose"`) {
		t.Fatalf("config should have interval 10 with mode preserved: %q", cfgData)
	}
	// 非法 interval → 400
	code, _ = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "turn_interval": 101})
	if code != 400 {
		t.Fatalf("invalid interval: status = %d, want 400", code)
	}
}

// TestBranchInfoAPI 分支上下文端点：base_branch 与 merges 透传 wiki.json，
// current_branch 键恒存在（非 git 项目为空串）；无谱系时 merges 给 [] 而非 null。
func TestBranchInfoAPI(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 夹具：wiki.json 含基准分支、游标与一条合并谱系
	stateDir := filepath.Join(okHome, "projects", "demo", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wikiJSON := `{
	  "base_branch": "master",
	  "cursors": {"master": {"last_commit": "abc123", "generated_at": "2026-08-01T00:00:00Z"}},
	  "merges": [{"from": "dev", "to": "master", "commit": "deadbeefcafe", "time": "2026-08-05T10:00:00Z"}]
	}`
	if err := os.WriteFile(filepath.Join(stateDir, "wiki.json"), []byte(wikiJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	code, data := do(t, "GET", srv.URL+"/api/project/branch-info?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("branch-info: status = %d, body %s", code, data)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body["base_branch"] != "master" {
		t.Errorf("base_branch 错误: %v", body)
	}
	merges, _ := body["merges"].([]any)
	if len(merges) != 1 {
		t.Errorf("merges 应透传: %v", body["merges"])
	} else if m, _ := merges[0].(map[string]any); m["from"] != "dev" || m["to"] != "master" {
		t.Errorf("merge 内容错误: %v", merges[0])
	}
	if _, ok := body["current_branch"]; !ok {
		t.Errorf("应含 current_branch 键")
	}

	// 缺 project → 400；未注册 → 404
	if code, _ := do(t, "GET", srv.URL+"/api/project/branch-info", testToken, nil); code != 400 {
		t.Errorf("缺 project 应 400，got %d", code)
	}
	if code, _ := do(t, "GET", srv.URL+"/api/project/branch-info?project=ghost", testToken, nil); code != 404 {
		t.Errorf("未注册项目应 404，got %d", code)
	}

	// 无 wiki.json 的项目：merges 应为 [] 而非 null（mkProject 会重建注册表，改为增量注册）
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject("empty", filepath.Join(t.TempDir(), "src")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(okHome, "projects", "empty", "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, data = do(t, "GET", srv.URL+"/api/project/branch-info?project=empty", testToken, nil)
	if code != 200 {
		t.Fatalf("empty branch-info: status = %d, body %s", code, data)
	}
	if !strings.Contains(string(data), `"merges":[]`) {
		t.Errorf("无谱系时 merges 应为 []: %s", data)
	}
}

// TestCaptureAutoBorn provenance 开关：GET 恒返回当前值（无 [provenance] 节默认 true）；
// POST auto_born（*bool，nil=不变）落盘项目 config.toml 的 [provenance] 节，替换不追加。
func TestCaptureAutoBorn(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()

	// GET 默认 true（无 [provenance] 节）
	code, data := do(t, "GET", srv.URL+"/api/capture?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("capture get: status = %d, body %s", code, data)
	}
	var cap1 struct {
		Mode         string `json:"mode"`
		TurnInterval int    `json:"turn_interval"`
		AutoBorn     bool   `json:"auto_born"`
	}
	if err := json.Unmarshal(data, &cap1); err != nil {
		t.Fatal(err)
	}
	if !cap1.AutoBorn {
		t.Fatalf("无 [provenance] 节时 auto_born 应默认 true: %s", data)
	}

	// POST auto_born=false → GET 为 false 且项目 config.toml 落盘 [provenance]
	code, data = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "auto_born": false})
	if code != 200 {
		t.Fatalf("capture set auto_born: status = %d, body %s", code, data)
	}
	cfgPath := filepath.Join(okHome, "projects", "demo", "config.toml")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("project config not written: %v", err)
	}
	if !strings.Contains(string(cfgData), "[provenance]") || !strings.Contains(string(cfgData), "auto_born = false") {
		t.Fatalf("config 应含 [provenance] auto_born = false: %q", cfgData)
	}
	code, data = do(t, "GET", srv.URL+"/api/capture?project=demo", testToken, nil)
	if code != 200 || !strings.Contains(string(data), `"auto_born":false`) {
		t.Fatalf("GET 应反映 auto_born=false: status = %d, body %s", code, data)
	}

	// 再设回 true → 整段替换而非重复追加；[capture] 节保留
	code, data = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "auto_born": true})
	if code != 200 {
		t.Fatalf("capture re-set auto_born: status = %d, body %s", code, data)
	}
	cfgData, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(cfgData), "[provenance]") != 1 || !strings.Contains(string(cfgData), "auto_born = true") {
		t.Fatalf("[provenance] 应唯一且 auto_born = true: %q", cfgData)
	}
	if !strings.Contains(string(cfgData), "[capture]") {
		t.Fatalf("[capture] 节应保留: %q", cfgData)
	}

	// 不传 auto_born（nil）→ 保持不变
	code, data = do(t, "POST", srv.URL+"/api/capture", testToken,
		map[string]any{"project": "demo", "mode": "auto"})
	if code != 200 {
		t.Fatalf("capture set mode: status = %d, body %s", code, data)
	}
	cfgData, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgData), "auto_born = true") || !strings.Contains(string(cfgData), `mode = "auto"`) {
		t.Fatalf("mode 保存不应改动 auto_born: %q", cfgData)
	}
}

// reasonix 三档：status 暴露 rxEnforceMode（缺省 mixed）；POST 保存后 status 反映；
// 非法值 400 且不落盘。
func TestReasonixEnforceModeAPI(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 缺省 mixed
	code, data := do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if code != 200 {
		t.Fatalf("status = %d, body %s", code, data)
	}
	if !strings.Contains(string(data), `"rxEnforceMode":"mixed"`) {
		t.Fatalf("status 默认应为 mixed: %s", data)
	}

	// 保存 hard → status 反映；config.toml 落盘
	code, data = do(t, "POST", srv.URL+"/api/reasonix/enforce-mode", testToken, map[string]any{"mode": "hard"})
	if code != 200 {
		t.Fatalf("保存失败: %d %s", code, data)
	}
	if !strings.Contains(string(data), `"mode":"hard"`) {
		t.Fatalf("响应应回显 hard: %s", data)
	}
	cfgData, err := os.ReadFile(filepath.Join(okHome, "config.toml"))
	if err != nil {
		t.Fatalf("全局配置应已落盘: %v", err)
	}
	if !strings.Contains(string(cfgData), `enforce_mode = "hard"`) {
		t.Fatalf("config.toml 应含 enforce_mode = \"hard\":\n%s", cfgData)
	}
	_, data = do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if !strings.Contains(string(data), `"rxEnforceMode":"hard"`) {
		t.Fatalf("status 应反映 hard: %s", data)
	}

	// 非法值 → 400，且不改动已保存的 hard
	code, _ = do(t, "POST", srv.URL+"/api/reasonix/enforce-mode", testToken, map[string]any{"mode": "歪"})
	if code != 400 {
		t.Fatalf("非法值应 400，got %d", code)
	}
	_, data = do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if !strings.Contains(string(data), `"rxEnforceMode":"hard"`) {
		t.Fatalf("非法保存不应改动配置: %s", data)
	}

	// 鉴权：无令牌 → 401
	if code, _ := do(t, "POST", srv.URL+"/api/reasonix/enforce-mode", "", map[string]any{"mode": "soft"}); code != 401 {
		t.Fatalf("无令牌应 401，got %d", code)
	}
}

func TestStatusVersionAndHome(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	code, data := do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if code != 200 {
		t.Fatalf("code=%d body=%s", code, data)
	}
	var s struct {
		AppVersion string `json:"app_version"`
		Home       string `json:"home"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.AppVersion == "" || s.Home == "" {
		t.Fatalf("status missing app_version/home: %s", data)
	}
}

func TestExportImportEndpoints(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "alpha")
	// 写一条条目
	kdir := filepath.Join(okHome, "projects", "alpha", "knowledge")
	if err := os.WriteFile(filepath.Join(kdir, "a.md"), []byte("---\ntitle: A\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 鉴权
	if code, _ := do(t, "GET", srv.URL+"/api/export?project=all", "wrong", nil); code != 401 {
		t.Fatal("export auth")
	}
	if code, _ := do(t, "POST", srv.URL+"/api/import", "wrong", nil); code != 401 {
		t.Fatal("import auth")
	}
	// 导出
	code, data := do(t, "GET", srv.URL+"/api/export?project=all", testToken, nil)
	if code != 200 {
		t.Fatalf("export code=%d", code)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) == 0 {
		t.Fatalf("not a zip: %v", err)
	}
	// 项目不存在
	if code, _ := do(t, "GET", srv.URL+"/api/export?project=nope", testToken, nil); code != 404 {
		t.Fatal("export 404")
	}
	// 删掉条目后用刚导出的包导入
	if err := os.Remove(filepath.Join(kdir, "a.md")); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "backup.zip")
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/import", &body)
	req.Header.Set("X-Ok-Token", testToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rdata, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("import code=%d body=%s", res.StatusCode, rdata)
	}
	var rep struct {
		Imported int `json:"imported"`
	}
	if err := json.Unmarshal(rdata, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 1 {
		t.Fatalf("report: %s", rdata)
	}
	if _, err := os.Stat(filepath.Join(kdir, "a.md")); err != nil {
		t.Fatal("entry not restored via endpoint")
	}
}

func TestProjectDelete(t *testing.T) {
	h, _, okHome := newEnv(t)
	// 注意：mkProject 每次从空注册表重建，连续调用会互相覆盖——两个项目必须一次写入
	reg := &registry.Registry{}
	if err := reg.AddProject("demo", filepath.Join(t.TempDir(), "demo-src")); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject("keep", filepath.Join(t.TempDir(), "keep-src")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo", "keep"} {
		if err := os.MkdirAll(filepath.Join(okHome, "projects", name, "knowledge"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 造一条知识，让 projects/demo 目录有内容
	code, data := do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("橙子种植"))
	if code != 200 {
		t.Fatalf("create: status = %d, body %s", code, data)
	}

	// 无 token → 401
	code, _ = do(t, "DELETE", srv.URL+"/api/project?project=demo", "", nil)
	if code != 401 {
		t.Fatalf("no token: status = %d, want 401", code)
	}

	// 缺参 → 400；未注册 → 404
	code, _ = do(t, "DELETE", srv.URL+"/api/project", testToken, nil)
	if code != 400 {
		t.Fatalf("missing param: status = %d, want 400", code)
	}
	code, _ = do(t, "DELETE", srv.URL+"/api/project?project=ghost", testToken, nil)
	if code != 404 {
		t.Fatalf("unknown project: status = %d, want 404", code)
	}

	// 正常删除
	code, data = do(t, "DELETE", srv.URL+"/api/project?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("delete: status = %d, body %s", code, data)
	}
	var resp struct {
		OK      bool   `json:"ok"`
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Warning != "" {
		t.Fatalf("unexpected delete response: %s", data)
	}

	// 目录已删
	if _, err := os.Stat(filepath.Join(okHome, "projects", "demo")); !os.IsNotExist(err) {
		t.Fatalf("project dir should be gone, stat err = %v", err)
	}
	// 注册表已注销（从磁盘重读验证）
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.Projects {
		if p.Name == "demo" {
			t.Fatal("demo should be unregistered")
		}
	}
	// /api/status 不再列出 demo、仍列出 keep
	code, data = do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if code != 200 {
		t.Fatalf("status: %d", code)
	}
	var st struct {
		Projects []struct {
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Projects) != 1 || st.Projects[0].Name != "keep" {
		t.Fatalf("status projects after delete: %s", data)
	}
	// 重复删除 → 404
	code, _ = do(t, "DELETE", srv.URL+"/api/project?project=demo", testToken, nil)
	if code != 404 {
		t.Fatalf("re-delete: status = %d, want 404", code)
	}
}
