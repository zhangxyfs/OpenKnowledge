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

	"openknowledge/internal/embedx"
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
	client := embedx.Client(pc.Config)
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
	// CheckStatus 每次注入只算一次：INDEX 分支裁剪、检索分支过滤、分支上下文行
	// 与 nudge 共用同一份 Status。分支未知（非 git）时 ws.Branch 为空，
	// 裁剪与过滤均为恒等（宁多勿漏，零回归）。
	ws := wiki.CheckStatus(pc.Store.StateDir(), cwd, pc.Config.Wiki.StaleCommits)
	// mandatory 段与其余段分离：mandatory 是"必须遵守"类规则，注入优先级最高，
	// 预算截断绝不波及（否则长 INDEX/检索会把尾部 mandatory 静默砍掉）。
	var mandatoryText, restText strings.Builder
	mandatory, err := db.Mandatory()
	if err != nil {
		logErr("prompt mandatory: %v", err)
	}
	// L2 兜底：无显式压缩事件的宿主按轮次重注入 mandatory 全文（reinject_turns>0
	// 时启用；0=关闭保持旧语义）。显式压缩事件（Reasonix compaction.complete）在
	// rxext sidecar 直接重置 BaseInjected，不走此轮次计数。
	// 基础注入素材先物化，决策与落盘在跨进程锁内一次完成（防并发 hook 丢更新）。
	idxText := ""
	if idx, err := os.ReadFile(pc.Store.IndexPath()); err == nil {
		// 按当前分支裁剪 INDEX 的"分支差异（X）"小节，防止检索过滤被 INDEX 绕过
		idxText = index.TrimIndexBranchSections(string(idx), ws.Branch)
	}
	wroteBase := len(mandatory) > 0 || idxText != ""
	reinjectActive := pc.Config.Inject.ReinjectTurns > 0 && len(mandatory) > 0
	var doBase bool
	if err := state.Update(pc.Store.StateDir(), sessionID, func(s *state.Session) {
		doBase = !s.BaseInjected
		if reinjectActive && s.BaseInjected {
			s.InjectCount++
			if s.InjectCount >= pc.Config.Inject.ReinjectTurns {
				doBase = true
			}
		}
		if doBase {
			// 素材为空时维持未注入态，下一轮 prompt 重试基础注入
			s.BaseInjected = wroteBase
			s.InjectCount = 0
		}
	}); err != nil {
		logErr("prompt save state: %v", err)
	}
	if doBase {
		_ = state.Clean(pc.Store.StateDir(), 7*24*time.Hour)
		for _, h := range mandatory {
			fmt.Fprintf(&mandatoryText, "## %s\n\n%s\n\n", h.Title, h.Body)
		}
		if idxText != "" {
			restText.WriteString(idxText)
		}
	} else if len(mandatory) > 0 {
		// L3 粘性指针：全文只在基础注入轮出现，其余每轮仅注标题 + 路径（几十 token），
		// 即使宿主压缩上下文把首轮全文摘要掉/沉入 lost-middle，模型仍能据此重读原文。
		mandatoryText.WriteString("## 必守规约（全文见文件，必要时读取）\n\n")
		for _, h := range mandatory {
			p := filepath.ToSlash(filepath.Join(pc.Store.KnowledgeDir(), h.Filename))
			fmt.Fprintf(&mandatoryText, "- **%s** (%s)（%s）\n", h.Title, h.Type, p)
		}
		mandatoryText.WriteString("\n")
	}
	var queryVec []float32
	var embedWarn string
	if client != nil {
		if vec, err := client.EmbedQuery(context.Background(), promptText); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			queryVec, embedWarn = embedx.QueryVec(db, client, vec)
			if embedWarn != "" {
				logErr("prompt embed identity: %s", embedWarn)
			}
		}
	}
	// top_n 截断在分支过滤之后（QueryExBranch 内部保证），其他分支的差异条目
	// 不再白白挤占名额；无 branch 标签的条目与未知分支场景不受影响。
	hits, info, err := db.QueryExBranch(retrieve.Terms(promptText), queryVec, pc.Config.Retrieve, ws.Branch)
	if err != nil {
		logErr("prompt query: %v", err)
	}
	// 语义通道未准入任何条目（无显著头部）：记 ok.log，GUI 日志页可按"语义"过滤
	// 查看；低对比度自定义模型可调低 retrieve.min_gap 放宽。
	if info.SemanticRejected {
		logErr("prompt semantic: 语义通道未准入任何条目（样本 %d，max=%.3f median=%.3f relGap=%.3f）；低对比度模型可调低 retrieve.min_gap 放宽",
			info.Coses, info.MaxCos, info.MedianCos, info.RelGap)
	}
	if len(hits) > 0 {
		restText.WriteString("## 相关知识（需要全文时读取对应文件）\n\n")
		for _, h := range hits {
			p := filepath.ToSlash(filepath.Join(pc.Store.KnowledgeDir(), h.Filename))
			if h.Summary != "" {
				fmt.Fprintf(&restText, "- **%s** (%s) — %s（%s）\n", h.Title, h.Type, h.Summary, p)
			} else {
				fmt.Fprintf(&restText, "- **%s** (%s)（%s）\n", h.Title, h.Type, p)
			}
		}
		restText.WriteString("\n")
	}
	// L4：mandatory 永不被截断；其余段（INDEX + 检索）在剩余预算内截断。
	budget := pc.Config.Inject.MaxTokens
	out := ""
	if mandatoryText.Len() > 0 {
		out = mandatoryText.String()
		if restBudget := budget - store.EstimateTokens(mandatoryText.String()); restBudget > 0 {
			out += store.TruncateToBudget(restText.String(), restBudget)
		}
	} else {
		out = store.TruncateToBudget(restText.String(), budget)
	}
	// 分支上下文行（注入开头）与 nudge（末尾）复用前移的同一份 Status
	if line := wikiContextLine(ws); line != "" && strings.TrimSpace(out) != "" {
		out = line + "\n" + out
	}
	if nudge := wikiNudge(pc, st, ws); nudge != "" {
		out += nudge
		if err := state.Update(pc.Store.StateDir(), sessionID, func(s *state.Session) {
			s.WikiNudged = true
		}); err != nil {
			logErr("prompt save state: %v", err)
		}
	}
	// 已并入提示（merged 变体）：仅在基准分支、有其他分支 tip 已并入且其差异条目
	// 仍在库中时触发；db 仍在 InjectForPrompt 作用域（defer Close 之前）可直接复用。
	// 与 wikiNudge 共用 WikiNudged 每会话一次预算（本回合已提示则跳过计算）。
	// MergedChecked 是检测本身的每会话熔断（独立于 WikiNudged）：merged 为空时
	// WikiNudged 不置位，若无此熔断每次 prompt 都会为每条非基准游标付两次 git spawn；
	// 因此计算后无论结果均置位（仅值变化才 Save，与 WikiNudged 保存惯例一致）。
	if !st.WikiNudged && !st.MergedChecked && ws.Branch != "" && ws.Branch == ws.BaseBranch {
		if s := wiki.LoadState(pc.Store.StateDir()); s != nil {
			merged := wiki.MergedIntoBase(s, cwd, func(b string) bool {
				ok, _ := db.HasBranchWiki(b)
				return ok
			})
			mergedNudge := wikiNudgeMerged(pc, st, ws.BaseBranch, merged)
			if err := state.Update(pc.Store.StateDir(), sessionID, func(s2 *state.Session) {
				s2.MergedChecked = true
				if mergedNudge != "" {
					s2.WikiNudged = true
				}
			}); err != nil {
				logErr("prompt save state: %v", err)
			}
			if mergedNudge != "" {
				out += mergedNudge
			}
		}
	}
	// 语义检索退化提示（每会话一次，独立于 wiki nudge 预算）：QueryVec 因模型
	// 身份缺失/切换而拦截向量通道时，仅写 ok.log 用户不可见，注入一行让模型
	// 知道当前是纯关键词检索、可运行 ok index 重建恢复。
	if embedWarn != "" && !st.RetrieveWarned {
		if err := state.Update(pc.Store.StateDir(), sessionID, func(s *state.Session) {
			s.RetrieveWarned = true
		}); err != nil {
			logErr("prompt save state: %v", err)
		}
		out += "\n[OpenKnowledge] 语义检索退化：" + embedWarn + "\n"
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
	// 并行工具调用会并发派发多个 post-tool hook 进程，读-改-写须在锁内合并
	if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
		st.AddTouched(rel)
	}); err != nil {
		logErr("post-tool save state: %v", err)
	}
}

// CheckStop 评估 auto 自省提醒与 enforce 规则并维护回合计数。
// 返回 (reason, blockedRule)：均空 = 放行；reason 非空 + blockedRule 空 = auto 自省
// 提醒（软）；两者皆非空 = enforce 规则命中（硬，blockedRule 为规则 Type）。
// MarkBlocked 所有权在调用方：本函数只评估不落防重标记——硬阻断生效前由调用方
// （HandleStop / rxext onInput）落标记；不落标记则下次评估重复命中（rxext soft 档
// "每条输入重复提醒"依赖此语义）。auto 提醒先于 enforce 评估（与既有 Stop 行为一致）。
// 评估与回合计数在跨进程锁内一次完成：Stop hook 可能与下一轮 prompt 的 post-tool
// 并发，无锁读-改-写会互相覆盖。
func CheckStop(pc *project.Context, sessionID string) (reason string, blockedRule string) {
	if registry.HooksDisabled() {
		return "", ""
	}
	// 无 enforce 规则且非 auto 自省模式：无需加载状态，直接放行
	if len(pc.Config.Enforce) == 0 && pc.Config.Capture.Mode != "auto" {
		return "", ""
	}
	interval := pc.Config.Capture.TurnInterval
	if interval <= 0 {
		interval = 1
	}
	if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
		st.StopCount++
		// auto 自省模式：有文件修改且距上次提醒满 turn_interval 回合 → 软阻断一次。
		// 周期性提醒，不进 BlockedRules；先于 enforce 评估触发。
		if pc.Config.Capture.Mode == "auto" && len(st.Touched) > 0 &&
			st.StopCount-st.LastExtractReminder >= interval {
			st.LastExtractReminder = st.StopCount
			reason = "本会话修改过文件。请回顾是否有值得记录的经验（非显而易见的坑或解法），有则立即运行 ok propose 记录草稿条目；没有则继续。"
			blockedRule = ""
			return
		}
		for _, rule := range pc.Config.Enforce {
			if rule.Type != "changelog_required" {
				continue
			}
			if block, why := enforce.EvalChangelog(rule, st); block {
				reason, blockedRule = why, rule.Type
				return
			}
		}
	}); err != nil {
		logErr("stop save state: %v", err)
	}
	return reason, blockedRule
}
