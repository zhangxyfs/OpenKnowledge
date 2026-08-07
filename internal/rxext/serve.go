// Package rxext 实现 ok 的 Reasonix Extension Protocol sidecar：
// Reasonix 宿主以 exec form 拉起 `ok extension-serve`，经 NDJSON/JSON-RPC
// 下发 input.receive / tool.after 拦截。fail-open：任何内部错误一律 Continue。
package rxext

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openknowledge/internal/agentx"
	"openknowledge/internal/hook"
	"openknowledge/internal/project"
	extension "openknowledge/internal/rxext/sdk"
	"openknowledge/internal/version"
)

// Serve 是 `ok extension-serve` 的入口：跑 SDK 的 stdio 服务循环直到宿主关闭。
func Serve(ctx context.Context) error {
	h := &handler{}
	return extension.Serve(ctx, h, extension.Options{
		Name:    "openknowledge",
		Version: version.Version,
		Interceptors: map[string]extension.InterceptorFunc{
			"input.receive": h.onInput,
			"tool.after":    h.onToolAfter,
		},
	})
}

// handler 持有 initialize 握手确定的单会话上下文（一个 Reasonix 会话一个 sidecar 进程）。
type handler struct {
	sessionID string
	cwd       string
}

func (h *handler) Initialize(_ context.Context, p extension.InitializeParams) (*extension.InitializeResult, error) {
	h.sessionID = p.Session.SessionID
	h.cwd = p.Session.WorkspaceRoot
	return &extension.InitializeResult{
		Subscriptions: []string{"input.receive", "tool.after"},
	}, nil
}

// onInput input.receive 拦截器：检索注入（replace 前缀 <ok-context>）。
// enforce 分支由 Task 5 加入。fail-open：panic/错误一律 Continue。
func (h *handler) onInput(_ context.Context, _ string, payload json.RawMessage) (res *extension.InterceptResult, err error) {
	defer continueOnPanic(&res, &err)
	res = extension.Continue()
	selfHealHooks()
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		return res, nil
	}
	pc, err := project.FromCwd(h.cwd)
	if err != nil {
		return res, nil
	}
	prefix := hook.InjectForPrompt(pc, h.sessionID, h.cwd, in.Text)
	if strings.TrimSpace(prefix) == "" {
		return res, nil
	}
	return buildInputReplacement(in.Text, []string{prefix})
}

// buildInputReplacement 把若干注入片段合并为一个 <ok-context> 块前缀进原输入。
func buildInputReplacement(original string, parts []string) (*extension.InterceptResult, error) {
	var b strings.Builder
	b.WriteString("<ok-context>\n")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		b.WriteString(strings.TrimRight(p, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString("</ok-context>\n\n")
	b.WriteString(original)
	return extension.Replace(map[string]string{"text": b.String()})
}

// selfHealHooks 逐 agent 自检 hooks/插件集成（如 ok.exe 迁移后重写登记）。fail-open。
func selfHealHooks() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	for _, a := range agentx.Detected() {
		_ = a.EnsureHooks(exe)
	}
}

// onToolAfter tool.after 拦截器（Task 6 点亮 touched 追踪）。
func (h *handler) onToolAfter(_ context.Context, _ string, _ json.RawMessage) (*extension.InterceptResult, error) {
	return extension.Continue(), nil
}

// continueOnPanic 把 panic 折叠为 Continue（fail-open 铁律）。供拦截器 defer 使用：
//
//	func (h *handler) onInput(...) (res *extension.InterceptResult, err error) {
//		defer continueOnPanic(&res, &err)
//		...
//	}
//
// panic 时 err 必须置 nil：SDK 对非 nil err 会丢弃 result 改回 JSON-RPC 错误，
// 而 fail-open 要的是 Continue。panic 痕迹写 stderr（由宿主脱敏留尾）。
func continueOnPanic(res **extension.InterceptResult, err *error) {
	if r := recover(); r != nil {
		*res = extension.Continue()
		*err = nil
		fmt.Fprintf(os.Stderr, "rxext panic: %v\n", r)
	}
}
