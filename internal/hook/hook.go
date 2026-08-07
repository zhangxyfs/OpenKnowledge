package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/agentx"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/state"
	"openknowledge/internal/wiki"
)

type Event struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Prompt        json.RawMessage `json:"prompt"`
}

func ParseEvent(r io.Reader) (*Event, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, err
	}
	e := &Event{}
	if err := json.Unmarshal(data, e); err != nil {
		return nil, err
	}
	return e, nil
}

// PromptText 提取提问文本：兼容字符串与内容块数组 [{"type":"text","text":"..."}]。
func (e *Event) PromptText() string {
	if len(e.Prompt) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(e.Prompt, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(e.Prompt, &parts); err != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// FilePath 从 tool_input 提取文件路径：kimi 用 path，兼容 file_path。
func (e *Event) FilePath() string {
	var ti struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if len(e.ToolInput) > 0 {
		_ = json.Unmarshal(e.ToolInput, &ti)
	}
	if ti.Path != "" {
		return ti.Path
	}
	return ti.FilePath
}

// FormatClaude 是 hook 输出的 Claude/ZCode JSON 协议格式（空串 = 纯文本，kimi/pi 用）。
// ZCode 只把以 { 开头的合法 JSON stdout 解析为协议结果，纯文本 stdout 不进模型上下文。
const FormatClaude = "claude"

// writeClaudeContext 以 Claude 协议把注入文本包成 hookSpecificOutput JSON 写 stdout。
func writeClaudeContext(w io.Writer, context string) {
	data, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]string{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": context,
		},
	})
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(data))
}

// writeClaudeBlock 以 Claude 协议表达 Stop 阻断（decision:block + reason），exit 0。
func writeClaudeBlock(w io.Writer, reason string) {
	data, err := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(data))
}

// stopBlock 按格式输出 Stop 阻断：claude 协议 JSON 走 stdout + exit 0；
// 纯文本走 stderr + exit 2（kimi/pi 的阻断语义）。
func stopBlock(stderr, stdout io.Writer, format, reason string) int {
	if format == FormatClaude {
		writeClaudeBlock(stdout, reason)
		return 0
	}
	fmt.Fprintln(stderr, reason)
	return 2
}

func logErr(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(registry.Home(), "ok.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("2006-01-02 15:04:05 ")+format+"\n", args...)
}

// selfHealHooks 逐 agent 自检 hooks 集成（如 kimi 清掉标记块时自动修复）。fail-open。
func selfHealHooks() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	for _, a := range agentx.Detected() {
		if err := a.EnsureHooks(exe); err != nil {
			logErr("self-heal hooks (%s): %v", a.ID(), err)
		}
	}
}

// HandlePrompt 解析 hook 事件并输出注入文本；核心逻辑见 InjectForPrompt。
// format 为 claude 时输出包成 Claude 协议 JSON（hookSpecificOutput），否则纯文本。
func HandlePrompt(r io.Reader, w io.Writer, format string) int {
	if registry.HooksDisabled() {
		return 0
	}
	selfHealHooks()
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("prompt parse: %v", err)
		return 0
	}
	promptText := ev.PromptText()
	if strings.TrimSpace(promptText) == "" {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	out := InjectForPrompt(pc, ev.SessionID, ev.Cwd, promptText)
	if strings.TrimSpace(out) != "" {
		if format == FormatClaude {
			writeClaudeContext(w, out)
		} else {
			fmt.Fprintln(w, out)
		}
	}
	return 0
}

// HandlePostTool 解析 hook 事件并记录触碰文件；核心逻辑见 TrackTouched。
func HandlePostTool(r io.Reader) int {
	if registry.HooksDisabled() {
		return 0
	}
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("post-tool parse: %v", err)
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		logErr("post-tool project (cwd=%q): %v", ev.Cwd, err)
		return 0
	}
	TrackTouched(pc, ev.SessionID, ev.ToolName, ev.FilePath())
	return 0
}

// relativize 将绝对路径转为相对项目根的路径；无法转换时返回 ""。
func relativize(pc *project.Context, abs string) string {
	if abs == "" {
		return ""
	}
	normAbs := registry.NormalizePath(abs)
	for _, root := range pc.Project.Paths {
		nr := registry.NormalizePath(root)
		if strings.HasPrefix(normAbs, nr+"/") {
			return normAbs[len(nr)+1:]
		}
	}
	return ""
}

// HandleStop 解析 hook 事件，按 CheckStop 评估结果阻断：纯文本格式 stderr + exit 2
// （kimi/pi）；claude 格式 stdout decision:block JSON + exit 0。
// isBlock 在本 Handler 不区分——两种结果都走 stopBlock，行为与现状一致；
// isBlock 供 reasonix sidecar 三档分流用。
func HandleStop(r io.Reader, stderr, stdout io.Writer, format string) int {
	if registry.HooksDisabled() {
		return 0
	}
	ev, err := ParseEvent(r)
	if err != nil {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	reason, _ := CheckStop(pc, ev.SessionID)
	if reason != "" {
		return stopBlock(stderr, stdout, format, reason)
	}
	return 0
}

// wikiNudge 返回 wiki 落后提示（每会话最多一次，预算外放行）；不适用返回空串。
// fail-open：git 不可用/非 git 项目时 CheckStatus 不 stale，自然无提示。
func wikiNudge(pc *project.Context, st *state.Session, cwd string) string {
	threshold := pc.Config.Wiki.StaleCommits
	if threshold <= 0 || st.WikiNudged {
		return ""
	}
	s := wiki.CheckStatus(pc.Store.StateDir(), cwd, threshold)
	if !s.Stale {
		return ""
	}
	st.WikiNudged = true
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("prompt save state: %v", err)
	}
	if !s.HasWiki {
		return "\n[OpenKnowledge] 本项目还没有 wiki，建议用 openknowledge-wiki 技能生成项目 wiki（含架构、模块与演进历史）。\n"
	}
	return fmt.Sprintf("\n[OpenKnowledge] wiki 已落后 %d 个 commit，建议用 openknowledge-wiki 技能增量更新。\n", s.Behind)
}
