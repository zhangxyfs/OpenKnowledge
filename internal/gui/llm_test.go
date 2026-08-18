package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLLMRoundTrip llm 配置的 保存/掩码回显/掩码提交保留原 key/active 切换/删除置空。
func TestLLMRoundTrip(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	cfgPath := filepath.Join(okHome, "config.toml")

	// 默认空配置
	code, data := do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if code != 200 {
		t.Fatalf("llm get: %d", code)
	}
	var view struct {
		Active   string `json:"active"`
		Profiles []struct {
			Name   string `json:"name"`
			APIKey string `json:"api_key"`
			Active bool   `json:"active"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.Active != "" || len(view.Profiles) != 0 {
		t.Fatalf("默认应为空: %+v", view)
	}

	// 保存并激活
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "测试服务", "kind": "openai", "base_url": "https://x.example.com/v1",
		"model": "m1", "api_key": "sk-real", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save: %d", code)
	}
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "sk-real") {
		t.Fatalf("key 应落盘: %q", cfgData)
	}

	// GET 掩码不回明文
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if strings.Contains(string(data), "sk-real") {
		t.Fatalf("GET 不得回明文 key: %s", data)
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.Active != "测试服务" || len(view.Profiles) != 1 || view.Profiles[0].APIKey != llmKeyMask || !view.Profiles[0].Active {
		t.Fatalf("回显不对: %+v", view)
	}

	// 掩码提交（改 model + 高级参数）→ 原 key 保留
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "测试服务", "kind": "openai", "base_url": "https://x.example.com/v1",
		"model": "m2", "api_key": llmKeyMask, "temperature": "0.7", "max_tokens": 512,
	})
	if code != 200 {
		t.Fatalf("re-save: %d", code)
	}
	cfgData, _ = os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "sk-real") || !strings.Contains(string(cfgData), `model = "m2"`) ||
		!strings.Contains(string(cfgData), `temperature = "0.7"`) || !strings.Contains(string(cfgData), "max_tokens = 512") {
		t.Fatalf("掩码提交应保留原 key 并更新 model/高级参数: %q", cfgData)
	}

	// 非法 kind → 400
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "x", "kind": "gemini", "base_url": "u", "model": "m",
	})
	if code != 400 {
		t.Fatalf("非法 kind 应 400, got %d", code)
	}

	// 非法高级参数 → 400
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "x", "kind": "openai", "base_url": "u", "model": "m", "temperature": "abc",
	})
	if code != 400 {
		t.Fatalf("非法 temperature 应 400, got %d", code)
	}
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "x", "kind": "openai", "base_url": "u", "model": "m", "max_tokens": -1,
	})
	if code != 400 {
		t.Fatalf("负 max_tokens 应 400, got %d", code)
	}

	// 删除使用中 → active 置空
	code, _ = do(t, "POST", srv.URL+"/api/llm/delete", testToken, map[string]any{"name": "测试服务"})
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	_ = json.Unmarshal(data, &view)
	if view.Active != "" || len(view.Profiles) != 0 {
		t.Fatalf("删除使用中应置空 active: %+v", view)
	}
}

// TestLLMTestEndpoint 连通性测试接口：假 openai 服务 200，错误地址 502。
func TestLLMTestEndpoint(t *testing.T) {
	h, _, _ := newEnv(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "p"}}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/test", testToken, map[string]any{
		"kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k",
	})
	if code != 200 {
		t.Fatalf("假服务应 200, got %d", code)
	}
	code, _ = do(t, "POST", srv.URL+"/api/llm/test", testToken, map[string]any{
		"kind": "openai", "base_url": "http://127.0.0.1:1", "model": "m", "api_key": "k",
	})
	if code != 502 {
		t.Fatalf("连不上应 502, got %d", code)
	}
}

// TestEntryOptimizeNoLLM 无 active 配置 → 409 no_llm。
func TestEntryOptimizeNoLLM(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "t", "body": "正文内容",
	})
	if code != 409 || !strings.Contains(string(data), "no_llm") {
		t.Fatalf("应 409 no_llm, got %d %s", code, data)
	}
}

// TestEntryOptimizeOK 全链路：假 LLM 返回带围栏 JSON → 解析回填字段；盘上 .md 不被改写。
func TestEntryOptimizeOK(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	kdir := filepath.Join(okHome, "projects", "demo", "knowledge")
	entryPath := filepath.Join(kdir, "a.md")
	if err := os.WriteFile(entryPath, []byte("---\ntitle: 旧标题\n---\n\n旧正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(entryPath)
	mtimeBefore := fi.ModTime()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		if !strings.Contains(string(raw), "旧正文") {
			t.Errorf("请求应携带条目正文: %s", raw)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"content": "```json\n{\"title\":\"新标题\",\"tags\":[\"x\"],\"summary\":\"新摘要\",\"body\":\"新正文\"}\n```",
			}}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 配置 active profile 指向假服务（走真实保存链路）
	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}

	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "旧标题", "body": "旧正文",
	})
	if code != 200 {
		t.Fatalf("optimize: %d, %s", code, data)
	}
	var out struct {
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
		Body  string   `json:"body"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Title != "新标题" || len(out.Tags) != 1 || out.Body != "新正文" {
		t.Fatalf("围栏 JSON 应被剥壳解析: %+v", out)
	}

	// 优化不写盘：.md 内容与 mtime 均不变
	fi2, _ := os.Stat(entryPath)
	content, _ := os.ReadFile(entryPath)
	if !fi2.ModTime().Equal(mtimeBefore) || !strings.Contains(string(content), "旧正文") {
		t.Fatalf("optimize 不得写盘: mtime %v→%v", mtimeBefore, fi2.ModTime())
	}

	// 调用记录落 ok.log（GUI 日志页 ok 来源）：开始 + 回答 + 结果
	logData, _ := os.ReadFile(filepath.Join(okHome, "ok.log"))
	for _, want := range []string{"optimize 开始", "optimize 回答", "optimize 结果: ok"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("ok.log 缺少 %q: %q", want, logData)
		}
	}
}

// TestEntryOptimizeNoChange 模型自报 no_change 或逐字段原样返回时，接口回
// {no_change:true}（前端据此提示「无需优化」），且不写盘。
func TestEntryOptimizeNoChange(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")

	// 模型没听指令、原样返回 → 兜底判定 no_change
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"content": `{"title":"旧标题","tags":["hook","设计裁决"],"summary":"旧摘要","body":"旧正文"}`,
			}}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "title": "旧标题", "tags": "hook, 设计裁决", "summary": "旧摘要", "body": "旧正文",
	})
	if code != 200 {
		t.Fatalf("optimize: %d, %s", code, data)
	}
	if !strings.Contains(string(data), `"no_change":true`) {
		t.Fatalf("逐字段相同应判定 no_change: %s", data)
	}
}

// TestExcerptLines 行号窗口截取：带行号取窗口、无行号取头部、越界钳制。
func TestExcerptLines(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString("line\n")
	}
	src := sb.String()
	if got := excerptLines(src, "50-52"); len(strings.Split(strings.TrimRight(got, "\n"), "\n")) != 13 { // 45..57
		t.Fatalf("窗口行数不对: %d", len(strings.Split(got, "\n")))
	}
	if got := excerptLines(src, ""); len(strings.Split(strings.TrimRight(got, "\n"), "\n")) != 80 {
		t.Fatalf("无行号应取头 80 行")
	}
	if got := excerptLines("a\nb\n", "99"); !strings.Contains(got, "a") {
		t.Fatalf("越界应钳制: %q", got)
	}
}
