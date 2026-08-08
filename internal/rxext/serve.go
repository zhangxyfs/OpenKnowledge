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
	"openknowledge/internal/setupx"
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

// onInput input.receive 拦截器：先 enforce 检查（block 优先于注入），再检索注入。
// 三档分流（[reasonix] enforce_mode）：mixed（默认）=自省软提醒/规则硬阻断；
// soft=全软提示；hard=全硬阻断。软路径把提醒与注入合并为一个 <ok-context> 块
// （提醒在前、注入在后）。fail-open：panic/错误一律 Continue。
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
	reason, isBlock := hook.CheckStop(pc, h.sessionID)
	mode := enforceMode()
	if reason != "" {
		switch {
		case isBlock && mode != "soft":
			// 规则硬阻断：mixed/hard 直接 Block
			return extension.Block(reason), nil
		case !isBlock && mode == "hard":
			// auto 自省软提醒：hard 档升级为硬阻断
			return extension.Block(reason), nil
		}
	}
	prefix := hook.InjectForPrompt(pc, h.sessionID, h.cwd, in.Text)
	var parts []string
	if reason != "" { // 软路径：提醒与注入合并为一个 ok-context 块（提醒在前）
		parts = append(parts, reason)
	}
	if strings.TrimSpace(prefix) != "" {
		parts = append(parts, prefix)
	}
	if len(parts) == 0 {
		return res, nil
	}
	rep, err := buildInputReplacement(in.Text, parts)
	if err != nil {
		return extension.Continue(), nil // fail-open：组装失败不阻断输入
	}
	return rep, nil
}

// enforceMode 读全局三档配置 [reasonix] enforce_mode（soft|hard|mixed，缺省/非法
// 按 mixed），生产路径收编在 setupx.ReasonixEnforceMode()；OK_RX_ENFORCE_TEST_MODE
// 是测试注入口（生产不设置）。
func enforceMode() string {
	if m := os.Getenv("OK_RX_ENFORCE_TEST_MODE"); m != "" {
		return normalizeEnforceMode(m)
	}
	return setupx.ReasonixEnforceMode()
}

func normalizeEnforceMode(m string) string {
	switch m {
	case "soft", "hard":
		return m
	default:
		return "mixed"
	}
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

// onToolAfter tool.after 拦截器：写文件工具成功执行后记录 touched。恒 Continue。
func (h *handler) onToolAfter(_ context.Context, _ string, payload json.RawMessage) (res *extension.InterceptResult, err error) {
	defer continueOnPanic(&res, &err)
	res = extension.Continue()
	var p struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		IsError   bool   `json:"isError"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.IsError {
		return res, nil
	}
	switch p.Name {
	case "write_file", "edit_file", "multi_edit", "notebook_edit":
	default:
		return res, nil
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(p.Arguments), &args); err != nil || args.Path == "" {
		return res, nil
	}
	pc, err := project.FromCwd(h.cwd)
	if err != nil {
		return res, nil
	}
	hook.TrackTouched(pc, h.sessionID, p.Name, args.Path)
	return res, nil
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
