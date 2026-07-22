package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/enforce"
	"openknowledge/internal/entry"
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
	Prompt        string          `json:"prompt"`
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

// FilePath 从 tool_input 提取文件路径（Write/Edit 工具）。
func (e *Event) FilePath() string {
	var ti struct {
		FilePath string `json:"file_path"`
	}
	if len(e.ToolInput) > 0 {
		_ = json.Unmarshal(e.ToolInput, &ti)
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

// HandleSessionStart 注入 mandatory 条目全文 + 索引；顺带清理过期会话状态。
func HandleSessionStart(r io.Reader, w io.Writer) int {
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("session-start parse: %v", err)
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	_ = state.Clean(pc.Store.StateDir(), 7*24*time.Hour)
	entries, err := entry.Load(pc.Store.KnowledgeDir())
	if err != nil {
		logErr("session-start load entries: %v", err)
		return 0
	}
	var b strings.Builder
	for _, e := range entries {
		if !e.Mandatory {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", e.Title, e.Body)
	}
	if idx, err := os.ReadFile(pc.Store.IndexPath()); err == nil {
		b.Write(idx)
	}
	out := store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens)
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(w, out)
	}
	return 0
}

// HandlePrompt 混合检索并注入 top-N 条目；embedding 失败降级为关键词检索。
func HandlePrompt(r io.Reader, w io.Writer) int {
	ev, err := ParseEvent(r)
	if err != nil || strings.TrimSpace(ev.Prompt) == "" {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	entries, err := entry.Load(pc.Store.KnowledgeDir())
	if err != nil {
		logErr("prompt load entries: %v", err)
		return 0
	}
	vs, err := embed.LoadVectors(pc.Store.VectorsPath())
	if err != nil {
		logErr("prompt load vectors: %v", err)
		vs = nil
	}
	var queryVec []float32
	if key := os.Getenv(pc.Config.Embedding.APIKeyEnv); key != "" && pc.Config.Embedding.BaseURL != "" {
		client := &embed.OpenAIClient{
			BaseURL: pc.Config.Embedding.BaseURL,
			APIKey:  key,
			Model:   pc.Config.Embedding.Model,
			Timeout: time.Duration(pc.Config.Embedding.TimeoutSec) * time.Second,
		}
		if vec, err := client.Embed(context.Background(), ev.Prompt); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			queryVec = vec
		}
	}
	ranked := retrieve.Rank(entries, ev.Prompt, queryVec, vs, pc.Config.Retrieve)
	if len(ranked) == 0 {
		return 0
	}
	var b strings.Builder
	for _, s := range ranked {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.Entry.Title, s.Entry.Body)
	}
	fmt.Fprintln(w, store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens))
	return 0
}

// HandlePostTool 记录触碰的文件（相对项目根、小写、"/" 分隔）。
func HandlePostTool(r io.Reader) int {
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
