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

// PatchPaths 从 Codex apply_patch 的 tool_input.command 提取补丁触碰的相对路径：
// 按行扫描 *** Add File: / *** Update File: / *** Delete File: / *** Move to:
// 头标记（move 语义下 Update 与 Move to 两个路径都算触碰）。command 兼容字符串
// 与数组两种 JSON 形态；非补丁输入返回 nil。路径相对会话 cwd。
func (e *Event) PatchPaths() []string {
	if len(e.ToolInput) == 0 {
		return nil
	}
	var ti struct {
		Command json.RawMessage `json:"command"`
	}
	if err := json.Unmarshal(e.ToolInput, &ti); err != nil || len(ti.Command) == 0 {
		return nil
	}
	var text string
	if err := json.Unmarshal(ti.Command, &text); err != nil {
		var parts []string
		if err := json.Unmarshal(ti.Command, &parts); err != nil {
			return nil
		}
		text = strings.Join(parts, "\n")
	}
	var paths []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if p, ok := strings.CutPrefix(line, prefix); ok {
				if p = strings.TrimSpace(p); p != "" {
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
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
// 路径来源：FilePath()（path/file_path——kimi/claude/zcode 写工具）+
// PatchPaths()（Codex apply_patch 补丁头）。补丁头路径相对会话 cwd，
// 先 join 再 relativize，否则项目前缀匹配不上全部 skip。
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
	paths := make([]string, 0, 4)
	if fp := ev.FilePath(); fp != "" {
		paths = append(paths, fp)
	}
	paths = append(paths, ev.PatchPaths()...)
	seen := map[string]bool{}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(ev.Cwd, p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		TrackTouched(pc, ev.SessionID, ev.ToolName, p)
	}
	return 0
}

// ResetBaseInjection 压缩/上下文丢失后重置基础注入标记：下一次 prompt 注入即重新
// 下发 mandatory 全文 + 索引。返回是否实际发生了重置。Reasonix sidecar 的
// compaction.complete 与 Claude Code 的 PreCompact 共用此逻辑。
func ResetBaseInjection(pc *project.Context, sessionID string) bool {
	st := state.Load(pc.Store.StateDir(), sessionID)
	if !st.BaseInjected {
		return false
	}
	st.BaseInjected = false
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("reset base injection: %v", err)
	}
	return true
}

// HandleCompact PreCompact 等压缩前事件的入口：重置基础注入标记，宿主压缩完成后
// 下一次 UserPromptSubmit 即重新注入 mandatory 全文。恒退出 0（fail-open，不阻断压缩）。
func HandleCompact(r io.Reader) int {
	if registry.HooksDisabled() {
		return 0
	}
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("compact parse: %v", err)
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	ResetBaseInjection(pc, ev.SessionID)
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
// enforce 硬阻断（blockedRule 非空）在阻断输出前落 MarkBlocked——MarkBlocked 所有权
// 在调用方，kimi/zcode/pi 保持每会话每规则最多阻断一次的语义；blockedRule 同时供
// reasonix sidecar 三档分流用。
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
	reason, blockedRule := CheckStop(pc, ev.SessionID)
	if reason != "" {
		if blockedRule != "" {
			// 硬阻断生效前落每会话防重标记（fail-open：错误仅记日志）
			st := state.Load(pc.Store.StateDir(), ev.SessionID)
			st.MarkBlocked(blockedRule)
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("stop save blocked rule: %v", err)
			}
		}
		return stopBlock(stderr, stdout, format, reason)
	}
	return 0
}

// wikiNudge 返回 wiki 提示（每会话最多一次，预算外放行）；不适用返回空串。
// fail-open：git 不可用/非 git 项目时 Status 不带分支状态，自然无提示。
func wikiNudge(pc *project.Context, st *state.Session, s *wiki.Status) string {
	if st.WikiNudged {
		return ""
	}
	// gone/legacy_orphan 不受 stale_commits 阈值门控（游标失效与落后计数无关，
	// 必须尽快告知）；无 wiki/落后提示维持 threshold <= 0 即关闭的旧语义。
	threshold := pc.Config.Wiki.StaleCommits
	if threshold <= 0 && s.BranchState != "gone" && s.BranchState != "legacy_orphan" {
		return ""
	}
	var msg string
	switch {
	case s.BranchState == "gone":
		msg = "[OpenKnowledge] wiki 游标失效（分支可能被改写），建议在生成 wiki 的分支上重新运行 openknowledge-wiki 技能。"
	case s.BranchState == "legacy_orphan":
		msg = "[OpenKnowledge] wiki 游标与当前分支分叉、无法确认归属；请在生成 wiki 的分支上运行 openknowledge-wiki 技能。"
	case !s.HasWiki && s.Stale:
		msg = "[OpenKnowledge] 本项目还没有 wiki，建议用 openknowledge-wiki 技能生成项目 wiki（含架构、模块与演进历史）。"
	case s.HasWiki && s.Stale:
		msg = fmt.Sprintf("[OpenKnowledge] wiki 已落后 %d 个 commit，建议用 openknowledge-wiki 技能增量更新。", s.Behind)
	default:
		return ""
	}
	st.WikiNudged = true
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("prompt save state: %v", err)
	}
	return "\n" + msg + "\n"
}

// wikiNudgeMerged 返回"分支已并入基准、其差异条目已失效"的清理提示（每会话一次，
// 与 wikiNudge 共用 WikiNudged 预算；不受 stale_commits 阈值门控——条目失效与
// 落后计数无关）。merged 为空或基准未知时不提示。检测本身只读（spec §7/§10）。
func wikiNudgeMerged(pc *project.Context, st *state.Session, base string, merged []string) string {
	if st.WikiNudged || len(merged) == 0 || base == "" {
		return ""
	}
	msg := fmt.Sprintf("[OpenKnowledge] 分支 %s 已并入 %s，其差异条目已失效，建议用 openknowledge-wiki 技能清理。", strings.Join(merged, "、"), base)
	st.WikiNudged = true
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("prompt save state: %v", err)
	}
	return "\n" + msg + "\n"
}

// wikiContextLine 返回 standing 分支上下文行：当前分支有 wiki 内容注入、
// 但 wiki 基准不在本分支时提示出处；基准分支/无 wiki/非 git 返回空串。
func wikiContextLine(s *wiki.Status) string {
	if !s.HasWiki || s.Branch == "" || s.BaseBranch == "" || s.Branch == s.BaseBranch {
		return ""
	}
	short := s.LastCommit
	if len(short) > 7 {
		short = short[:7]
	}
	switch s.BranchState {
	case "diverged":
		mb := s.MergeBase
		if len(mb) > 7 {
			mb = mb[:7]
		}
		return fmt.Sprintf("[OpenKnowledge] wiki 基于 %s@%s；当前分支 %s（分叉点 %s），结构描述可能与当前分支不符。\n",
			s.BaseBranch, short, s.Branch, mb)
	case "no_cursor":
		return fmt.Sprintf("[OpenKnowledge] wiki 基于 %s@%s；当前分支 %s 尚无基线，结构描述以 %s 为准。\n",
			s.BaseBranch, short, s.Branch, s.BaseBranch)
	}
	return ""
}
