package hook

// core.go 是 hook 三条链路的核心逻辑，与传输协议（kimi/zcode 的 stdin/stdout、
// reasonix 的 sidecar JSON-RPC）解耦：Handler 负责解析事件与格式化输出，
// 核心函数负责注入组装、文件追踪与强制检查。fail-open：内部错误仅记 ok.log。

import (
	"context"
	"errors"
	"fmt"
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
	"openknowledge/internal/wiki"
)

// InjectForPrompt 组装 prompt 注入文本：会话首次基础注入（mandatory 全文 + 索引）
// + 每次检索注入 + wiki 落后提醒；外层截断到注入预算。
// 由 HandlePrompt（kimi/zcode/pi）与 rxext sidecar（reasonix）共用。
func InjectForPrompt(pc *project.Context, sessionID, cwd, promptText string) string {
	if registry.HooksDisabled() {
		return ""
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
		return ""
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
			return ""
		default:
			// embedding 失败：降级重试（仅同步 INDEX），保证基础注入与关键词检索不被阻断
			logErr("prompt sync index with embedding: %v", err)
			if err2 := db.Sync(pc.Store.KnowledgeDir(), nil); err2 != nil {
				logErr("prompt sync index: %v", err2)
				if !errors.As(err2, &corrupt) {
					return ""
				}
			}
		}
	}
	st := state.Load(pc.Store.StateDir(), sessionID)
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
	// CheckStatus 只算一次：分支上下文行（注入开头）与 nudge（末尾）共用同一份 Status。
	threshold := pc.Config.Wiki.StaleCommits
	ws := wiki.CheckStatus(pc.Store.StateDir(), cwd, threshold)
	if line := wikiContextLine(ws); line != "" && strings.TrimSpace(out) != "" {
		out = line + "\n" + out
	}
	if nudge := wikiNudge(pc, st, ws); nudge != "" {
		out += nudge
	}
	return out
}

// TrackTouched 记录工具触碰的文件（相对项目根、小写、"/" 分隔）。
// 静默分支均记 ok.log（无 post-tool 日志 = 宿主未派发或进程被超时杀死）。
func TrackTouched(pc *project.Context, sessionID, toolName, filePath string) {
	if registry.HooksDisabled() {
		return
	}
	rel := relativize(pc, filePath)
	if rel == "" {
		logErr("post-tool skip: tool=%s path=%q 不在项目 %s 的路径内", toolName, filePath, pc.Project.Name)
		return
	}
	st := state.Load(pc.Store.StateDir(), sessionID)
	st.AddTouched(rel)
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("post-tool save state: %v", err)
	}
}

// CheckStop 评估 auto 自省提醒与 enforce 规则并维护回合计数。
// 返回 (reason, blockedRule)：均空 = 放行；reason 非空 + blockedRule 空 = auto 自省
// 提醒（软）；两者皆非空 = enforce 规则命中（硬，blockedRule 为规则 Type）。
// MarkBlocked 所有权在调用方：本函数只评估不落防重标记——硬阻断生效前由调用方
// （HandleStop / rxext onInput）落标记；不落标记则下次评估重复命中（rxext soft 档
// "每条输入重复提醒"依赖此语义）。auto 提醒先于 enforce 评估（与既有 Stop 行为一致）。
func CheckStop(pc *project.Context, sessionID string) (reason string, blockedRule string) {
	if registry.HooksDisabled() {
		return "", ""
	}
	// 无 enforce 规则且非 auto 自省模式：无需加载状态，直接放行
	if len(pc.Config.Enforce) == 0 && pc.Config.Capture.Mode != "auto" {
		return "", ""
	}
	st := state.Load(pc.Store.StateDir(), sessionID)
	// auto 自省模式：有文件修改且距上次提醒满 turn_interval 回合 → 软阻断一次。
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
		return "本会话修改过文件。请回顾是否有值得记录的经验（非显而易见的坑或解法），有则立即运行 ok propose 记录草稿条目；没有则继续。", ""
	}
	for _, rule := range pc.Config.Enforce {
		if rule.Type != "changelog_required" {
			continue
		}
		if block, reason := enforce.EvalChangelog(rule, st); block {
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("stop save state: %v", err)
			}
			return reason, rule.Type
		}
	}
	// 未阻断也要持久化 StopCount
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("stop save state: %v", err)
	}
	return "", ""
}
