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

	"openknowledge/internal/embed"
	"openknowledge/internal/enforce"
	"openknowledge/internal/index"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/state"
	"openknowledge/internal/store"
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

func logErr(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(registry.Home(), "ok.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("2006-01-02 15:04:05 ")+format+"\n", args...)
}

// HandlePrompt 基础注入（每会话首次：mandatory 全文 + 索引）+ 检索注入（每次）。
// 检索前先对 kb.db 做增量同步（按 filename+mtime，仅为变化条目重算向量）。
// embedding 失败降级为关键词检索；任何内部错误 fail-open。
func HandlePrompt(r io.Reader, w io.Writer) int {
	if registry.HooksDisabled() {
		return 0
	}
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
	for _, h := range hits {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", h.Title, h.Body)
	}
	out := store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens)
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(w, out)
	}
	return 0
}

// HandlePostTool 记录触碰的文件（相对项目根、小写、"/" 分隔）。
func HandlePostTool(r io.Reader) int {
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
	rel := relativize(pc, ev.FilePath())
	if rel == "" {
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

// HandleStop 评估 enforce 规则，需要时以 exit 2 阻断。
func HandleStop(r io.Reader, stderr io.Writer) int {
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
	if len(pc.Config.Enforce) == 0 {
		return 0
	}
	st := state.Load(pc.Store.StateDir(), ev.SessionID)
	for _, rule := range pc.Config.Enforce {
		if rule.Type != "changelog_required" {
			continue
		}
		if block, reason := enforce.EvalChangelog(rule, st); block {
			st.MarkBlocked(rule.Type)
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("stop save state: %v", err)
			}
			fmt.Fprintln(stderr, reason)
			return 2
		}
	}
	return 0
}
