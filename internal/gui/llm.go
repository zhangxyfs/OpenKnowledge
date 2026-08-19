package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/index"
	"openknowledge/internal/llmx"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/setupx"
	"openknowledge/internal/store"
)

// ---------- 模型配置（全局 [llm] 段，跨项目共用） ----------

const llmKeyMask = "********"

type llmProfileJSON struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key"` // GET 掩码回显；POST 掩码/空 = 保留原值
	Temperature string `json:"temperature"`
	MaxTokens   int    `json:"max_tokens"`
	Active      bool   `json:"active"`
}

// validateLLMAdv 校验高级参数：temperature 非空必须可解析为 0~2 浮点；
// max_tokens 不允许负数。返回规范化后的 temperature（去空白）。
func validateLLMAdv(temperature string, maxTokens int) (string, error) {
	temperature = strings.TrimSpace(temperature)
	if temperature != "" {
		t, err := strconv.ParseFloat(temperature, 64)
		if err != nil || t < 0 || t > 2 {
			return "", fmt.Errorf("temperature 必须是 0~2 的数字（留空 = 服务端默认）")
		}
	}
	if maxTokens < 0 {
		return "", fmt.Errorf("max_tokens 不能为负数（0 = 默认）")
	}
	return temperature, nil
}

func loadGlobalConfig() (config.Config, error) {
	return config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
}

// apiLLMGet 返回全局 llm 配置：active + profiles（api_key 掩码，不回明文）。
func (h *Handler) apiLLMGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := loadGlobalConfig()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	profiles := make([]llmProfileJSON, 0, len(cfg.LLM.Profiles))
	for _, p := range cfg.LLM.Profiles {
		key := ""
		if p.APIKey != "" {
			key = llmKeyMask
		}
		profiles = append(profiles, llmProfileJSON{
			Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL, Model: p.Model,
			APIKey: key, Temperature: p.Temperature, MaxTokens: p.MaxTokens,
			Active: p.Name == cfg.LLM.Active,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": cfg.LLM.Active, "profiles": profiles})
}

// apiLLMProfileSave 新增/同名覆盖保存 profile；activate=true 同时设为使用中。
// api_key 为掩码或空 = 保留同名旧值（setupx.SaveLLMProfile 收口空值语义，掩码在此转换）。
func (h *Handler) apiLLMProfileSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		BaseURL     string `json:"base_url"`
		Model       string `json:"model"`
		APIKey      string `json:"api_key"`
		Temperature string `json:"temperature"`
		MaxTokens   int    `json:"max_tokens"`
		Activate    bool   `json:"activate"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Model = strings.TrimSpace(req.Model)
	if req.Name == "" || req.BaseURL == "" || req.Model == "" {
		writeErr(w, http.StatusBadRequest, "名称、base_url、模型均不能为空")
		return
	}
	if req.Kind != "openai" && req.Kind != "anthropic" {
		writeErr(w, http.StatusBadRequest, "类型仅支持 openai | anthropic")
		return
	}
	temperature, err := validateLLMAdv(req.Temperature, req.MaxTokens)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIKey == llmKeyMask {
		req.APIKey = "" // 掩码提交 = 保留原值
	}
	if err := setupx.SaveLLMProfile(config.LLMProfile{
		Name: req.Name, Kind: req.Kind, BaseURL: req.BaseURL, Model: req.Model, APIKey: req.APIKey,
		Temperature: temperature, MaxTokens: req.MaxTokens,
	}, req.Activate); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.apiLLMGet(w, r)
}

// apiLLMProfileDelete 删除 profile；删除使用中项时 active 置空（setupx 收口）。
func (h *Handler) apiLLMProfileDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "缺少 name")
		return
	}
	if err := setupx.DeleteLLMProfile(req.Name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.apiLLMGet(w, r)
}

// apiLLMActive 切换使用中 profile；空 name = 停用。
func (h *Handler) apiLLMActive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := setupx.SetActiveLLM(req.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.apiLLMGet(w, r)
}

// apiLLMTest 用表单直传的配置（无需先保存）做连通性检查；api_key 掩码/空时
// 回查同名已存 profile 的真实 key。
func (h *Handler) apiLLMTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		BaseURL     string `json:"base_url"`
		Model       string `json:"model"`
		APIKey      string `json:"api_key"`
		Temperature string `json:"temperature"`
		MaxTokens   int    `json:"max_tokens"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	temperature, err := validateLLMAdv(req.Temperature, req.MaxTokens)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIKey == "" || req.APIKey == llmKeyMask {
		if cfg, err := loadGlobalConfig(); err == nil {
			for _, p := range cfg.LLM.Profiles {
				if p.Name == req.Name {
					req.APIKey = p.APIKey
					break
				}
			}
		}
	}
	p := config.LLMProfile{Name: req.Name, Kind: req.Kind, BaseURL: req.BaseURL, Model: req.Model, APIKey: req.APIKey,
		Temperature: temperature, MaxTokens: req.MaxTokens}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := llmx.New(p, 0).Test(ctx); err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("连接失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 条目 AI 优化 ----------

// logOptimize 追加优化调用记录到 ok.log（GUI「日志」页 ok 来源即此文件，
// 前端轮询 /api/logs 自动显示，无需改动）。与 hook.logErr 同一格式。
func logOptimize(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(registry.Home(), "ok.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("2006-01-02 15:04:05 ")+format+"\n", args...)
}

// clipLog 日志截断：按 rune 截 n 字、换行压成 ⏎（ok.log 按行展示，多行会散行）。
func clipLog(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ⏎ ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// optimizeSystemPrompt 条目优化的系统提示词：条目模型语义 + 事实纪律（参照优先、
// 不杜撰）+ 表达硬约束 + 纯 JSON 输出。type/mandatory 锁死不许模型改。
const optimizeSystemPrompt = `你是 OpenKnowledge 知识库的条目编辑。知识条目是一文件一条的 Markdown，frontmatter 字段语义：
- title：标题，检索命中的第一印象
- type：rule/pitfall/note/reference 四类，不允许改
- tags：检索与过滤维度，可增删
- mandatory：是否每会话强制注入，不允许改
- summary：一句话摘要，进 INDEX 与 Wiki 目录，不复读标题
- body：正文

任务：先通读我提供的事实参照（项目真实代码片段 + 知识库相关条目 + INDEX 功能摘录），再据实优化条目的 title/tags/summary/body。

重要：优化是为了「更简练且不失重点」。若通读后判断原文已足够简练准确、没有实质改进空间（只是同义改写、字数差不多的不算改进），不要强行改写，直接输出 {"no_change":true}。

事实纪律：
- 事实以「原文 + 事实参照」的并集为准，参照优先：原文与项目实际冲突（接口改名、路径迁移、版本过时）时按实际修正
- 允许补充事实参照里存在的真实细节（函数名、路径、参数）；不得添加原文与参照都没有的事实
- 参照未覆盖的原文内容保持原意，不得凭空"完善"

表达硬约束：最简洁的语言；不丢技术重点（命令、路径、版本号、阈值、因果链）；保持中文；frontmatter 语义不变。

输出：仅一个 JSON 对象 {"title":"...","tags":["..."],"summary":"...","body":"..."}；或判定无需优化时 {"no_change":true}。无 Markdown 围栏、无任何解释。`

// entryPathRef 匹配条目正文中的代码引用：internal/gui/api.go 或 path:行号（区间）。
var entryPathRef = regexp.MustCompile(`[0-9A-Za-z_./-]+\.(?:go|js|ts|tsx|py|md|html|css|sh|toml|json|iss)(?::\d+(?:-\d+)?)?`)

// entryCodeHint 匹配正文反引号内代码片段（2~60 字符），作为无行号引用时的
// 命中线索：把含这些标识符的行窗口补进摘录，而不是只看文件头。
var entryCodeHint = regexp.MustCompile("`([^`\n]{2,60})`")

// gatherGrounding 事实检索：代码引用片段 + 知识库相关条目 top3 + INDEX 摘录。
// 各来源失败只跳过不报错（优化主流程不应被参照缺失拖死）。
func gatherGrounding(st *store.Store, project, selfFile, title, summary, body string) string {
	var sb strings.Builder

	// ① 正文路径引用 → 项目真实代码片段（合计 ≤3000 token，最多 5 处）
	reg, err := registry.Load(registry.DefaultPath())
	if err == nil {
		var roots []string
		for _, p := range reg.Projects {
			if p.Name == project {
				roots = p.Paths
				break
			}
		}
		seen := map[string]bool{}
		used := 0
		var codeSb strings.Builder
		// 正文反引号标识符作为无行号引用的命中线索（去重保序）
		var hints []string
		hintSeen := map[string]bool{}
		for _, m := range entryCodeHint.FindAllStringSubmatch(body, -1) {
			h := strings.TrimSpace(m[1])
			if h != "" && !hintSeen[h] {
				hintSeen[h] = true
				hints = append(hints, h)
			}
		}
		for _, ref := range entryPathRef.FindAllString(body, -1) {
			if len(seen) >= 5 {
				break
			}
			file, lines := ref, ""
			if i := strings.LastIndex(ref, ":"); i > 0 {
				file, lines = ref[:i], ref[i+1:]
			}
			file = filepath.FromSlash(file)
			if seen[file] {
				continue
			}
			for _, root := range roots {
				full := filepath.Join(root, file)
				data, err := os.ReadFile(full)
				if err != nil {
					continue
				}
				seen[file] = true
				snip := excerptLines(string(data), lines, hints)
				tokens := store.EstimateTokens(snip)
				if used+tokens > 3000 {
					snip = store.TruncateToBudget(snip, 3000-used)
					tokens = store.EstimateTokens(snip)
				}
				if tokens == 0 {
					break
				}
				used += tokens
				fmt.Fprintf(&codeSb, "--- %s ---\n%s\n", ref, snip)
				break
			}
		}
		if codeSb.Len() > 0 {
			sb.WriteString("\n\n【项目真实代码片段】\n" + codeSb.String())
		}
	}

	// ② 知识库混合检索 top3（排除自身，关键词通道，各截 500 token）
	if db, err := index.Open(st.KbPath()); err == nil {
		defer db.Close()
		cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
		if err == nil {
			q := title + " " + summary
			if hits, err := db.Query(retrieve.Terms(q), nil, cfg.Retrieve); err == nil {
				var relSb strings.Builder
				n := 0
				for _, hit := range hits {
					if hit.Filename == selfFile || n >= 3 {
						continue
					}
					n++
					fmt.Fprintf(&relSb, "--- %s ---\n%s\n", hit.Title, store.TruncateToBudget(hit.Body, 500))
				}
				if relSb.Len() > 0 {
					sb.WriteString("\n\n【知识库相关条目】\n" + relSb.String())
				}
			}
		}
	}

	// ③ INDEX 功能摘录（≤2000 token）
	if data, err := os.ReadFile(st.IndexPath()); err == nil {
		sb.WriteString("\n\n【项目 INDEX 摘录】\n" + store.TruncateToBudget(string(data), 2000))
	}
	return sb.String()
}

// cmpPunct 归一 CJK 标点区块的符号；全角 ASCII 区（！～，含全角字母数字与
// ％＝＋等）由 foldFullWidth 统一折半角，二者字符集不相交。
var cmpPunct = strings.NewReplacer(
	"。", ".", "、", ",", "·", ".",
	"《", "<", "》", ">", "〈", "<", "〉", ">",
	"【", "[", "】", "]",
	"「", "\"", "」", "\"", "『", "\"", "』", "\"",
	"“", "\"", "”", "\"", "‘", "'", "’", "'",
	"—", "-", "–", "-", "…", ".",
)

// foldFullWidth 全角 ASCII 区（U+FF01 ！ ～ U+FF5E）折对应半角——模型输出
// 常混全半角（50％ vs 50%、ＡＢＣ vs ABC）。
func foldFullWidth(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// normalizeForCmp 语义级相同判定用的归一化：全角折半角、CJK 标点归半角、
// 连续点号折叠（... ↔ …… ↔ 。。。）、全部空白（含换行）折叠为单空格——
// 只动排版/标点的伪优化不算优化，应判 no_change。
var cmpDots = regexp.MustCompile(`\.{2,}`)

func normalizeForCmp(s string) string {
	folded := cmpPunct.Replace(foldFullWidth(s))
	return strings.Join(strings.Fields(cmpDots.ReplaceAllString(folded, ".")), " ")
}

// excerptLines 按 "30" 或 "30-50" 行号取窗口（前后各放宽 5 行）。无行号时取
// 文件头 80 行，并追加 hints（正文反引号标识符）的命中行窗口（±5，最多 3 处，
// 已在头部覆盖的行不重复）——无行号引用时关键代码常在文件深处，只看头会截断。
func excerptLines(content, lines string, hints []string) string {
	all := strings.Split(content, "\n")
	if lines == "" {
		head := all
		if len(head) > 80 {
			head = head[:80]
		}
		marked := make([]bool, len(all))
		for i := range head {
			marked[i] = true
		}
		var sb strings.Builder
		sb.WriteString(strings.Join(head, "\n"))
		added := 0
		for _, h := range hints {
			if added >= 3 {
				break
			}
			for i, ln := range all {
				if marked[i] || !strings.Contains(ln, h) {
					continue
				}
				lo, hi := i-5, i+5
				if lo < 0 {
					lo = 0
				}
				if hi >= len(all) {
					hi = len(all) - 1
				}
				fmt.Fprintf(&sb, "\n── 命中 `%s`（L%d-%d）──\n", h, lo+1, hi+1)
				for j := lo; j <= hi; j++ {
					if !marked[j] {
						sb.WriteString(all[j] + "\n")
						marked[j] = true
					}
				}
				added++
				break // 每个标识符只取第一个命中行
			}
		}
		return strings.TrimRight(sb.String(), "\n")
	}
	var lo, hi int
	if i := strings.Index(lines, "-"); i > 0 {
		fmt.Sscanf(lines[:i], "%d", &lo)
		fmt.Sscanf(lines[i+1:], "%d", &hi)
	} else {
		fmt.Sscanf(lines, "%d", &lo)
		hi = lo
	}
	if lo <= 0 {
		lo = 1
	}
	if hi < lo {
		hi = lo
	}
	lo -= 5
	if lo < 1 {
		lo = 1
	}
	hi += 5
	if hi > len(all) {
		hi = len(all)
	}
	if lo > hi {
		// 行号越界（引用了超出文件长度的行）：回退为文件头部，优于钳到空尾行
		lo, hi = 1, len(all)
		if hi > 80 {
			hi = 80
		}
	}
	return strings.Join(all[lo-1:hi], "\n")
}

// apiEntryOptimize 条目 AI 优化：事实检索 → LLM → 返回优化后字段（不写盘，
// 落盘由前端回填表单后走既有保存路径）。无 active 配置 → 409 {error:"no_llm"}。
func (h *Handler) apiEntryOptimize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		File    string `json:"file"`
		Title   string `json:"title"`
		Tags    string `json:"tags"`
		Summary string `json:"summary"`
		Body    string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "正文为空，无可优化内容")
		return
	}
	cfg, err := loadGlobalConfig()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	prof := cfg.LLM.ActiveProfile()
	if prof == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "no_llm"})
		return
	}

	grounding := gatherGrounding(st, req.Project, req.File, req.Title, req.Summary, req.Body)
	user := fmt.Sprintf("【待优化条目】\ntitle: %s\ntags: %s\nsummary: %s\nbody:\n%s\n%s",
		req.Title, req.Tags, req.Summary, req.Body, grounding)

	// 生成调用默认 120s：事实检索后 prompt 数千 token + 长 JSON 输出，
	// 30s（llmx 零值钳制，适合 ping 测试）不够，慢模型会直接超时。
	// [llm] timeout_sec 显式配置时优先。
	timeout := time.Duration(cfg.LLM.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout+15*time.Second)
	defer cancel()
	logOptimize("optimize 开始 project=%s file=%s title=%q model=%s", req.Project, req.File, req.Title, prof.Model)
	rep, err := llmx.New(*prof, timeout).Chat(ctx, optimizeSystemPrompt, user, 4096)
	if err != nil {
		logOptimize("optimize 失败: %v", err)
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("模型调用失败: %v", err))
		return
	}
	raw, reasoning := rep.Text, rep.Reasoning
	if reasoning != "" {
		logOptimize("optimize 思考: %s", clipLog(reasoning, 800))
	}
	logOptimize("optimize 回答: %s", clipLog(raw, 1500))
	logOptimize("optimize 消耗: prompt=%d completion=%d total=%d",
		rep.Usage.Prompt, rep.Usage.Completion, rep.Usage.Prompt+rep.Usage.Completion)
	// 剥 Markdown 围栏后严格解析
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var out struct {
		Title    string     `json:"title"`
		Tags     []string   `json:"tags"`
		Summary  string     `json:"summary"`
		Body     string     `json:"body"`
		NoChange bool       `json:"no_change"`
		Usage    llmx.Usage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		snip := raw
		if len(snip) > 300 {
			snip = snip[:300] + "…"
		}
		logOptimize("optimize 结果: 输出非合法 JSON")
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("模型输出非合法 JSON: %s", snip))
		return
	}
	// 无需优化判定：模型自报 no_change，或与原文语义级相同（空白折叠 + 全半角
	// 标点归一，tags 为无序集合）——只动排版/标点的伪优化不进对照预览。
	if !out.NoChange {
		var inTags, outTags []string
		for _, t := range strings.FieldsFunc(req.Tags, func(r rune) bool { return r == ',' || r == '，' }) {
			if s := strings.TrimSpace(t); s != "" {
				inTags = append(inTags, s)
			}
		}
		for _, t := range out.Tags {
			if s := strings.TrimSpace(t); s != "" {
				outTags = append(outTags, s)
			}
		}
		sort.Strings(inTags)
		sort.Strings(outTags)
		out.NoChange = normalizeForCmp(out.Title) == normalizeForCmp(req.Title) &&
			normalizeForCmp(out.Summary) == normalizeForCmp(req.Summary) &&
			normalizeForCmp(out.Body) == normalizeForCmp(req.Body) &&
			strings.Join(inTags, ",") == strings.Join(outTags, ",")
	}
	// no_change 也回完整字段：前端一律打开对照弹窗展示（排版级差异仍允许逐字段回填），
	// 模型自报 no_change 时字段可能为空，由前端回退显示原值。
	if out.NoChange {
		logOptimize("optimize 结果: no_change（无需优化）")
	} else {
		logOptimize("optimize 结果: ok（返回 title/tags/summary/body）")
	}
	out.Usage = rep.Usage
	writeJSON(w, http.StatusOK, out)
}
