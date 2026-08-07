package rxext

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/registry"
	extension "openknowledge/internal/rxext/sdk"
)

// TestMain 隔离四个 agent home：selfHealHooks 会遍历 detected agents 写 hook 集成，
// 测试绝不可触碰真实配置（与 hook 包 TestMain 同款，另加 OK_REASONIX_HOME 预留）。
func TestMain(m *testing.M) {
	for i, env := range []string{"OK_REASONIX_HOME", "KIMI_CODE_HOME", "PI_CODING_AGENT_DIR", "OK_ZCODE_HOME"} {
		dir, err := os.MkdirTemp("", "rxext-test-"+string(rune('a'+i)))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Setenv(env, dir)
	}
	os.Exit(m.Run())
}

// setupProject 在临时 OK_HOME 下注册项目并返回项目目录与 KB 根（同 hook 包夹具）。
func setupProject(t *testing.T) (projDir, kbRoot string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	projDir = filepath.Join(home, "work", "demo")
	kbRoot = filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(filepath.Join(kbRoot, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(kbRoot, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "demo", Paths: []string{projDir}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	return projDir, kbRoot
}

func writeEntry(t *testing.T, kbRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "knowledge", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInitializeRecordsSession(t *testing.T) {
	h := &handler{}
	res, err := h.Initialize(context.Background(), extension.InitializeParams{
		Session: extension.SessionContext{SessionID: "sess-1", WorkspaceRoot: `D:\work\demo`, Generation: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.sessionID != "sess-1" || h.cwd != `D:\work\demo` {
		t.Errorf("会话上下文未记录: %+v", h)
	}
	// 断言订阅集合恰为 {input.receive, tool.after}（顺序无关）
	want := map[string]bool{"input.receive": true, "tool.after": true}
	if len(res.Subscriptions) != len(want) {
		t.Fatalf("订阅应为 input.receive 与 tool.after，got: %v", res.Subscriptions)
	}
	for _, s := range res.Subscriptions {
		if !want[s] {
			t.Errorf("意外订阅 %q，got: %v", s, res.Subscriptions)
		}
	}
}

func TestStubInterceptorsContinue(t *testing.T) {
	h := &handler{sessionID: "s", cwd: "."}
	for _, fn := range []extension.InterceptorFunc{h.onInput, h.onToolAfter} {
		res, err := fn(context.Background(), "", []byte(`{}`))
		if err != nil || res == nil {
			t.Fatalf("拦截器错误: %v", err)
		}
		if res.Decision != extension.DecisionContinue {
			t.Errorf("桩拦截器应 Continue，got: %v", res.Decision)
		}
	}
}

func TestOnInputInjectsKnowledge(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	h := &handler{sessionID: "s1", cwd: projDir}
	res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"项目规约是什么"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != extension.DecisionReplace {
		t.Fatalf("应 Replace，got: %v", res.Decision)
	}
	var rep struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(res.Replacement, &rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Text, "<ok-context>") || !strings.Contains(rep.Text, "架构规约") {
		t.Errorf("注入文本缺 ok-context 或知识内容: %q", rep.Text)
	}
	if !strings.HasSuffix(rep.Text, "项目规约是什么") {
		t.Errorf("原输入必须完整保留在尾部: %q", rep.Text)
	}
}

func TestOnInputFailOpen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	h := &handler{sessionID: "s", cwd: filepath.Join(home, "不存在的项目")}
	res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"hi"}`))
	if err == nil && res.Decision != extension.DecisionContinue {
		t.Errorf("项目解析失败应 Continue（fail-open），got: %v", res.Decision)
	}
	// 坏 payload 也不得 panic
	res2, _ := h.onInput(context.Background(), "input.receive", []byte(`不是JSON`))
	if res2 == nil || res2.Decision != extension.DecisionContinue {
		t.Errorf("坏 payload 应 Continue")
	}
}
