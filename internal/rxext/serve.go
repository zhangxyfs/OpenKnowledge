// Package rxext 实现 ok 的 Reasonix Extension Protocol sidecar：
// Reasonix 宿主以 exec form 拉起 `ok extension-serve`，经 NDJSON/JSON-RPC
// 下发 input.receive / tool.after 拦截。fail-open：任何内部错误一律 Continue。
package rxext

import (
	"context"
	"encoding/json"
	"fmt"

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

// onInput input.receive 拦截器（Task 4/5 点亮注入与 enforce）。
func (h *handler) onInput(_ context.Context, _ string, _ json.RawMessage) (*extension.InterceptResult, error) {
	return extension.Continue(), nil
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
func continueOnPanic(res **extension.InterceptResult, err *error) {
	if r := recover(); r != nil {
		*res = extension.Continue()
		*err = fmt.Errorf("rxext panic: %v", r)
	}
}
