package rxext

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"openknowledge/internal/hook"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	extension "openknowledge/internal/rxext/sdk"
	"openknowledge/internal/state"
)

// TestMain 隔离十个 agent home：selfHealHooks 会遍历 detected agents 写 hook 集成，
// 测试绝不可触碰真实配置（与 hook 包 TestMain 同款，另加 OK_REASONIX_HOME 预留）。
func TestMain(m *testing.M) {
	for i, env := range []string{"OK_REASONIX_HOME", "KIMI_CODE_HOME", "PI_CODING_AGENT_DIR", "OK_ZCODE_HOME", "OK_OPENCODE_HOME", "OK_CLAUDE_HOME", "OK_CODEPILOT_HOME", "OK_CODEX_HOME", "OK_QODER_HOME", "OK_QODER_IDE_HOME"} {
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

// writeProjectConfig 写项目配置（kbRoot/config.toml，TOML；与 hook 包测试同款夹具）。
func writeProjectConfig(t *testing.T, kbRoot, cfg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// touchFile 直接经 hook.TrackTouched 记录会话触碰文件（不走 tool.after 拦截器）。
func touchFile(t *testing.T, projDir, sessionID, path string) {
	t.Helper()
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	hook.TrackTouched(pc, sessionID, "Write", path)
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

// changelog 规则夹具：触碰 **/*.go 且未触碰 docs/changelogs/** → 命中硬阻断。
const changelogEnforceCfg = `
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志（changelog）"
`

// TestOnInputEnforceModes 表驱动覆盖 enforce_mode 三档 + 非法值（按 mixed）：
// changelog 硬规则命中时 mixed/hard → Block；soft → 提醒合并进 <ok-context> 前缀。
func TestOnInputEnforceModes(t *testing.T) {
	cases := []struct {
		mode         string
		wantDecision extension.InterceptDecision
	}{
		{"mixed", extension.DecisionBlock},
		{"hard", extension.DecisionBlock},
		{"soft", extension.DecisionReplace},
		{"bogus", extension.DecisionBlock}, // 非法值按 mixed
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			projDir, kbRoot := setupProject(t)
			writeProjectConfig(t, kbRoot, changelogEnforceCfg)
			if tc.wantDecision == extension.DecisionReplace {
				// soft 档：补一条 mandatory 条目使注入 prefix 非空，真正走通双部件合并路径
				writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
			}
			t.Setenv("OK_RX_ENFORCE_TEST_MODE", tc.mode)
			h := &handler{sessionID: "s1", cwd: projDir}
			touchFile(t, projDir, "s1", filepath.Join(projDir, "main.go"))
			res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"继续"}`))
			if err != nil {
				t.Fatal(err)
			}
			if res.Decision != tc.wantDecision {
				t.Fatalf("mode=%s 应 %v，got %v（reason=%q）", tc.mode, tc.wantDecision, res.Decision, res.Reason)
			}
			if tc.wantDecision == extension.DecisionBlock && !strings.Contains(res.Reason, "changelog") {
				t.Errorf("Block reason 应带规则 message: %q", res.Reason)
			}
			if tc.wantDecision == extension.DecisionReplace {
				var rep struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(res.Replacement, &rep); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(rep.Text, "<ok-context>") || !strings.Contains(rep.Text, "changelog") {
					t.Errorf("软提醒应合并进 ok-context 且含 changelog 提示: %q", rep.Text)
				}
				if n := strings.Count(rep.Text, "<ok-context>"); n != 1 {
					t.Errorf("提醒与注入应合并为恰一个 ok-context 块，got %d: %q", n, rep.Text)
				}
				remindIdx := strings.Index(rep.Text, "changelog")
				injectIdx := strings.Index(rep.Text, "架构规约")
				if remindIdx < 0 || injectIdx < 0 || remindIdx > injectIdx {
					t.Errorf("changelog 提醒应排在注入条目文本之前（remind=%d inject=%d）: %q", remindIdx, injectIdx, rep.Text)
				}
				if !strings.HasSuffix(rep.Text, "继续") {
					t.Errorf("原输入必须完整保留在尾部: %q", rep.Text)
				}
			}
		})
	}
}

// TestOnToolAfterTracksTouched tool.after 恒 Continue：写文件工具成功执行记录 touched，
// isError / 非写工具 / 坏 payload 一律不记录。
func TestOnToolAfterTracksTouched(t *testing.T) {
	projDir, _ := setupProject(t)
	h := &handler{sessionID: "s9", cwd: projDir}
	payload := fmt.Sprintf(`{"name":"write_file","arguments":%q,"result":"ok","isError":false}`,
		`{"path":"`+strings.ReplaceAll(filepath.Join(projDir, "b.go"), `\`, `\\`)+`"}`)
	res, err := h.onToolAfter(context.Background(), "tool.after", []byte(payload))
	if err != nil || res.Decision != extension.DecisionContinue {
		t.Fatalf("tool.after 应恒 Continue: %v %v", res, err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	st := state.Load(pc.Store.StateDir(), "s9")
	if len(st.Touched) != 1 || st.Touched[0] != "b.go" {
		t.Fatalf("touched 记录错误: %+v", st.Touched)
	}
	// isError 与非写工具不记录
	h2 := &handler{sessionID: "s10", cwd: projDir}
	_, _ = h2.onToolAfter(context.Background(), "tool.after", []byte(`{"name":"write_file","arguments":"{\"path\":\"x.go\"}","isError":true}`))
	_, _ = h2.onToolAfter(context.Background(), "tool.after", []byte(`{"name":"bash","arguments":"{\"command\":\"ls\"}","isError":false}`))
	st2 := state.Load(pc.Store.StateDir(), "s10")
	if len(st2.Touched) != 0 {
		t.Errorf("isError/非写工具不应记录: %+v", st2.Touched)
	}
}

// TestOnInputEnforceHardBlocksAutoReminder hard 档下 auto 自省软提醒升级为硬阻断。
func TestOnInputEnforceHardBlocksAutoReminder(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	// turn_interval=1：首个 input.receive 即触发自省提醒（StopCount 1 >= 1）
	writeProjectConfig(t, kbRoot, "[capture]\nmode = \"auto\"\nturn_interval = 1\n")
	t.Setenv("OK_RX_ENFORCE_TEST_MODE", "hard")
	h := &handler{sessionID: "s1", cwd: projDir}
	touchFile(t, projDir, "s1", filepath.Join(projDir, "main.go"))
	res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"继续"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != extension.DecisionBlock {
		t.Fatalf("hard 档 auto 自省应 Block，got %v", res.Decision)
	}
	if !strings.Contains(res.Reason, "ok propose") {
		t.Errorf("Block reason 应为自省提醒: %q", res.Reason)
	}
}

// TestOnInputSoftRepeatsReminder soft 档不落 MarkBlocked：同一 sessionID 连续两次
// onInput 都应 Replace 且含 changelog 提醒（无视则每条输入重复提醒）。
func TestOnInputSoftRepeatsReminder(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeProjectConfig(t, kbRoot, changelogEnforceCfg)
	t.Setenv("OK_RX_ENFORCE_TEST_MODE", "soft")
	h := &handler{sessionID: "s1", cwd: projDir}
	touchFile(t, projDir, "s1", filepath.Join(projDir, "main.go"))
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"继续"}`))
		if err != nil {
			t.Fatal(err)
		}
		if res.Decision != extension.DecisionReplace {
			t.Fatalf("第 %d 次 onInput 应 Replace（soft 重复提醒），got %v（reason=%q）", i, res.Decision, res.Reason)
		}
		var rep struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(res.Replacement, &rep); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rep.Text, "changelog") {
			t.Errorf("第 %d 次 onInput 软提醒应含 changelog 提示: %q", i, rep.Text)
		}
		// soft 路径每次调用后都不得落 MarkBlocked
		st := state.Load(pc.Store.StateDir(), "s1")
		if st.HasBlocked("changelog_required") {
			t.Fatalf("第 %d 次 onInput 后不应落 MarkBlocked: %+v", i, st.BlockedRules)
		}
	}
}

// TestOnInputMixedBlocksOncePerSession mixed 档反向用例：规则硬阻断生效前落
// MarkBlocked，每会话每规则最多阻断一次——第二次同会话 onInput 不再 Block。
func TestOnInputMixedBlocksOncePerSession(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeProjectConfig(t, kbRoot, changelogEnforceCfg)
	t.Setenv("OK_RX_ENFORCE_TEST_MODE", "mixed")
	h := &handler{sessionID: "s1", cwd: projDir}
	touchFile(t, projDir, "s1", filepath.Join(projDir, "main.go"))
	res, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"继续"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != extension.DecisionBlock {
		t.Fatalf("第一次 onInput 应 Block，got %v", res.Decision)
	}
	// Block 生效后 MarkBlocked 必须已落盘
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	st := state.Load(pc.Store.StateDir(), "s1")
	if !st.HasBlocked("changelog_required") {
		t.Fatalf("mixed 档 Block 后应落 MarkBlocked: %+v", st.BlockedRules)
	}
	// 第二次同会话 onInput：已阻断过 → 不再 Block（防死循环语义不变）
	res2, err := h.onInput(context.Background(), "input.receive", []byte(`{"text":"继续"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Decision == extension.DecisionBlock {
		t.Fatalf("第二次 onInput 不应再 Block（每会话每规则最多一次），reason=%q", res2.Reason)
	}
}

// TestOnToolAfterConcurrent SDK 并发派发回调（maxConcurrentHandlers=32）：handler
// 互斥锁须保证 state 读-改-写不丢更新——并发 8 次 onToolAfter 不同文件全落盘。
func TestOnToolAfterConcurrent(t *testing.T) {
	projDir, _ := setupProject(t)
	h := &handler{sessionID: "sc", cwd: projDir}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("f%d.go", i)
			payload := fmt.Sprintf(`{"name":"write_file","arguments":%q,"result":"ok","isError":false}`,
				`{"path":"`+strings.ReplaceAll(filepath.Join(projDir, name), `\`, `\\`)+`"}`)
			res, err := h.onToolAfter(context.Background(), "tool.after", []byte(payload))
			if err != nil || res.Decision != extension.DecisionContinue {
				t.Errorf("tool.after 应恒 Continue: %v %v", res, err)
			}
		}(i)
	}
	wg.Wait()
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	st := state.Load(pc.Store.StateDir(), "sc")
	if len(st.Touched) != 8 {
		t.Fatalf("并发 8 次 onToolAfter 应记录 8 条 touched，got %d: %+v", len(st.Touched), st.Touched)
	}
}
