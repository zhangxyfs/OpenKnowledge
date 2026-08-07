package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/agentx"
	"openknowledge/internal/embed"
	"openknowledge/internal/enforce"
	"openknowledge/internal/index"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/state"
	"openknowledge/internal/store"
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

// HandlePrompt 基础注入（每会话首次：mandatory 全文 + 索引）+ 检索注入（每次）。
// 检索前先对 kb.db 做增量同步（按 filename+mtime，仅为变化条目重算向量）。
// embedding 失败降级为关键词检索；任何内部错误 fail-open。
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
	var client embed.Client
	if key := pc.Config.Embedding.ResolvedAPIKey(); key != "" && pc.Config.Embedding.BaseURL != "" {
		client = &embed.OpenAIClient{
			BaseURL: pc.Config.Embedding.BaseURL,
			APIKey:  key,
			Model:   pc.Config.Embedding.Model,
			Timeout: time.Duration(pc.Config.Embedding.TimeoutSec) * time.Second,
		}
	}
	db, err := index.Open(pc.Store.KbPath())
	if err != nil {
		logErr("prompt open index: %v", err)
		return 0
	}
	defer db.Close()
	if err := db.Sync(pc.Store.KnowledgeDir(), client); err != nil {
		var corrupt *index.CorruptEntriesError
		switch {
		case errors.As(err, &corrupt):
			// 损坏条目已跳过、其余已提交：记日志后继续正常注入（无需降级重试）
			logErr("prompt sync index: %v", err)
		case client == nil:
			logErr("prompt sync index: %v", err)
			return 0
		default:
			// embedding 失败：降级重试（仅同步 INDEX），保证基础注入与关键词检索不被阻断
			logErr("prompt sync index with embedding: %v", err)
			if err2 := db.Sync(pc.Store.KnowledgeDir(), nil); err2 != nil {
				logErr("prompt sync index: %v", err2)
				if !errors.As(err2, &corrupt) {
					return 0
				}
			}
		}
	}
	st := state.Load(pc.Store.StateDir(), ev.SessionID)
	var b strings.Builder
	if !st.BaseInjected {
		_ = state.Clean(pc.Store.StateDir(), 7*24*time.Hour)
		base := b.Len()
		mandatory, err := db.Mandatory()
		if err != nil {
			logErr("prompt mandatory: %v", err)
		}
		for _, h := range mandatory {
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", h.Title, h.Body)
		}
		if idx, err := os.ReadFile(pc.Store.IndexPath()); err == nil {
			b.Write(idx)
		}
		if b.Len() > base {
			st.BaseInjected = true
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("prompt save state: %v", err)
			}
		}
	}
	var queryVec []float32
	if client != nil {
		if vec, err := client.Embed(context.Background(), promptText); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			queryVec = vec
		}
	}
	hits, err := db.Query(retrieve.Terms(promptText), queryVec, pc.Config.Retrieve)
	if err != nil {
		logErr("prompt query: %v", err)
	}
	if len(hits) > 0 {
		b.WriteString("## 相关知识（需要全文时读取对应文件）\n\n")
		for _, h := range hits {
			p := filepath.ToSlash(filepath.Join(pc.Store.KnowledgeDir(), h.Filename))
			if h.Summary != "" {
				fmt.Fprintf(&b, "- **%s** (%s) — %s（%s）\n", h.Title, h.Type, h.Summary, p)
			} else {
				fmt.Fprintf(&b, "- **%s** (%s)（%s）\n", h.Title, h.Type, p)
			}
		}
		b.WriteString("\n")
	}
	out := store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens)
	if nudge := wikiNudge(pc, st, ev.Cwd); nudge != "" {
		out += nudge
	}
	if strings.TrimSpace(out) != "" {
		if format == FormatClaude {
			writeClaudeContext(w, out)
		} else {
			fmt.Fprintln(w, out)
		}
	}
	return 0
}

// HandlePostTool 记录触碰的文件（相对项目根、小写、"/" 分隔）。
// 各静默分支均记 ok.log（2026-08-04 曾出现整会话 touched 丢失且无迹可查）：
// 若无任何 post-tool 日志，说明 kimi 未派发或 hook 进程被超时杀死。
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
	rel := relativize(pc, ev.FilePath())
	if rel == "" {
		logErr("post-tool skip: tool=%s path=%q 不在项目 %s 的路径内", ev.ToolName, ev.FilePath(), pc.Project.Name)
		return 0
	}
	st := state.Load(pc.Store.StateDir(), ev.SessionID)
	st.AddTouched(rel)
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("post-tool save state: %v", err)
	}
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

// HandleStop 先按周期发出 auto 自省提醒，再评估 enforce 规则，需要时阻断：
// 纯文本格式 stderr + exit 2（kimi/pi）；claude 格式 stdout decision:block JSON + exit 0。
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
	// 无 enforce 规则且非 auto 自省模式：无需加载状态，直接放行
	if len(pc.Config.Enforce) == 0 && pc.Config.Capture.Mode != "auto" {
		return 0
	}
	st := state.Load(pc.Store.StateDir(), ev.SessionID)
	// auto 自省模式：有文件修改且距上次提醒满 turn_interval 回合 → 阻断一次。
	// 周期性提醒，不进 BlockedRules；先于 enforce 评估触发。
	st.StopCount++
	interval := pc.Config.Capture.TurnInterval
	if interval <= 0 {
		interval = 1
	}
	if pc.Config.Capture.Mode == "auto" && len(st.Touched) > 0 &&
		st.StopCount-st.LastExtractReminder >= interval {
		st.LastExtractReminder = st.StopCount
		if err := st.Save(pc.Store.StateDir()); err != nil {
			logErr("stop save state: %v", err)
		}
		reason := "本会话修改过文件。请回顾是否有值得记录的经验（非显而易见的坑或解法），有则立即运行 ok propose 记录草稿条目；没有则继续。"
		return stopBlock(stderr, stdout, format, reason)
	}
	for _, rule := range pc.Config.Enforce {
		if rule.Type != "changelog_required" {
			continue
		}
		if block, reason := enforce.EvalChangelog(rule, st); block {
			st.MarkBlocked(rule.Type)
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("stop save state: %v", err)
			}
			return stopBlock(stderr, stdout, format, reason)
		}
	}
	// 未阻断也要持久化 StopCount
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("stop save state: %v", err)
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
