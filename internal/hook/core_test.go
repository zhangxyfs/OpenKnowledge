package hook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/index"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/state"
)

// core_test.go 直接覆盖 InjectForPrompt / TrackTouched / CheckStop（不经 Handler），
// 防止 sidecar（Task 3-6）依赖的核心逻辑回归。
// helper 复用 hook_test.go 的 setupProject / writeEntry / writeCaptureConfig。

func TestInjectForPromptBaseAndRetrieve(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	// 一条 mandatory 条目 + 一条普通条目（正文含独特词）
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s1", projDir, "RetrievalQuirk 是什么")
	if !strings.Contains(out, "架构规约") {
		t.Errorf("首次注入应含 mandatory 全文，got: %q", out)
	}
	if !strings.Contains(out, "检索经验") {
		t.Errorf("注入应含检索命中条目，got: %q", out)
	}
	// 第二次：mandatory 不再重复，检索仍在
	out2 := InjectForPrompt(pc, "s1", projDir, "RetrievalQuirk 再问")
	if strings.Contains(out2, "永远先跑 gofmt") {
		t.Errorf("第二次注入不应重复 mandatory 全文，got: %q", out2)
	}
	if !strings.Contains(out2, "检索经验") {
		t.Errorf("第二次注入应仍含检索命中，got: %q", out2)
	}
}

// TestInjectGateSkipsRetrieval 门控命中时跳过检索注入段（连查询词本身被登记为
// 泛化短语也不例外）；关闭门控后同 prompt 恢复注入——证明跳过的是检索段而非"没命中"。
func TestInjectGateSkipsRetrieval(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	// 把查询词登记为 extra 泛化短语 → 门控必命中
	cfg := "[retrieve.gate]\nenabled = true\nextra_phrases = [\"RetrievalQuirk 是什么\"]\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s-gate", projDir, "RetrievalQuirk 是什么")
	// 注：不断言标题"检索经验"缺席——首轮基础注入的 INDEX 主列表合法地列出全部
	// 条目标题（门控按设计不影响 INDEX）；检索段的判据是"相关知识"小节与条目路径。
	if strings.Contains(out, "相关知识") || strings.Contains(out, "检索.md") {
		t.Errorf("门控命中不应注入检索段，got: %q", out)
	}
	if !strings.Contains(out, "永远先跑 gofmt") {
		t.Errorf("门控不应影响 mandatory 基础注入，got: %q", out)
	}
	// 对照：关闭门控后同 prompt 应命中检索
	cfg2 := "[retrieve.gate]\nenabled = false\nextra_phrases = [\"RetrievalQuirk 是什么\"]\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg2), 0o644); err != nil {
		t.Fatal(err)
	}
	pc2, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out2 := InjectForPrompt(pc2, "s-gate-off", projDir, "RetrievalQuirk 是什么")
	// 对照组同样以"相关知识"小节为判据（标题在 INDEX 中恒在，不足以证明检索恢复）
	if !strings.Contains(out2, "相关知识") || !strings.Contains(out2, "检索.md") {
		t.Errorf("关闭门控后应恢复检索注入，got: %q", out2)
	}
}

// TestInjectSemanticDegradeHintOnce 语义检索退化（模型身份不符）时，注入末尾
// 应附提示（每会话一次），把"退化为关键词检索、可 ok index 重建"暴露给用户/模型。
func TestInjectSemanticDegradeHintOnce(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	// 索引 meta 标记为另一个模型 → QueryVec 判定身份不符并拦截向量通道
	db, err := index.Open(filepath.Join(kbRoot, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetMeta("embedding_model", "openai:old@http://old"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// 假 embedding 服务：请求成功返回向量，使 EmbedQuery 正常、QueryVec 触发 warn
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{1, 0, 0}, "index": 0}},
		})
	}))
	defer srv.Close()
	cfg := "[embedding]\nbase_url = \"" + srv.URL + "\"\nmodel = \"m\"\napi_key = \"k\"\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out1 := InjectForPrompt(pc, "s-degrade", projDir, "git 提交")
	if !strings.Contains(out1, "[OpenKnowledge] 语义检索退化") {
		t.Fatalf("first injection should carry degrade hint: %q", out1)
	}
	out2 := InjectForPrompt(pc, "s-degrade", projDir, "git 提交")
	if strings.Contains(out2, "[OpenKnowledge] 语义检索退化") {
		t.Fatalf("degrade hint must be once per session: %q", out2)
	}
}

// TestSemanticRejectLog 语义通道参与但分布无头部（全部候选被拒）时，应把
// "prompt semantic" 诊断写进 ok.log（GUI 日志页按"语义"过滤可见）。
func TestSemanticRejectLog(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	// 12 条可检索条目（n≥10 阈值才生效；3 条小库 floor=0 不设门槛）
	writeEntry(t, kbRoot, "git.md", gitEntry)
	for i := 0; i < 11; i++ {
		writeEntry(t, kbRoot, fmt.Sprintf("noise%d.md", i),
			fmt.Sprintf("---\ntitle: 噪音条目%d\ntype: note\ntags: []\nsummary: 噪音\n---\n\n噪音正文%d\n", i, i))
	}
	// 假 embedding 服务：按输入条数返回相同向量 [1,0,0]（sync 批量 + 查询两用）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, 0, len(req.Input))
		for i := range req.Input {
			data = append(data, map[string]any{"embedding": []float32{1, 0, 0}, "index": i})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()
	// meta 与当前客户端身份一致 → 语义通道参与（不触发退化拦截）
	db, err := index.Open(filepath.Join(kbRoot, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetMeta("embedding_model", "openai:m@"+srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMeta("embedding_dim", "3"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := "[embedding]\nbase_url = \"" + srv.URL + "\"\nmodel = \"m\"\napi_key = \"k\"\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = InjectForPrompt(pc, "s-semlog", projDir, "随便问问")
	data, err := os.ReadFile(filepath.Join(registry.Home(), "ok.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "prompt semantic") {
		t.Fatalf("expected semantic-reject log in ok.log, got: %q", string(data))
	}
}

func TestTrackTouchedAndCheckStopRemind(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	// 项目配置：auto 自省模式
	writeCaptureConfig(t, kbRoot, "auto", 1)
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	TrackTouched(pc, "s2", "write_file", filepath.Join(projDir, "a.go"))
	st := state.Load(pc.Store.StateDir(), "s2")
	if len(st.Touched) != 1 || st.Touched[0] != "a.go" {
		t.Fatalf("touched 记录错误: %+v", st.Touched)
	}
	reason, blockedRule := CheckStop(pc, "s2")
	if reason == "" || blockedRule != "" {
		t.Fatalf("auto 自省应返回软提醒（blockedRule 为空），got (%q, %q)", reason, blockedRule)
	}
	if !strings.Contains(reason, "ok propose") {
		t.Errorf("自省提醒文案应引导 ok propose，got: %q", reason)
	}
}

// TestCheckStopDoesNotMarkBlocked MarkBlocked 所有权在调用方：CheckStop 命中 enforce
// 规则只返回 blockedRule，自身不落每会话防重标记（rxext soft 档重复提醒依赖此语义）。
func TestCheckStopDoesNotMarkBlocked(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	cfg := `
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	TrackTouched(pc, "s3", "write_file", filepath.Join(projDir, "main.go"))
	reason, blockedRule := CheckStop(pc, "s3")
	if reason == "" || blockedRule != "changelog_required" {
		t.Fatalf("enforce 命中应返回 (reason, changelog_required)，got (%q, %q)", reason, blockedRule)
	}
	st := state.Load(pc.Store.StateDir(), "s3")
	if st.HasBlocked("changelog_required") {
		t.Fatalf("CheckStop 不得落 MarkBlocked: %+v", st.BlockedRules)
	}
	// 未落标记 → 再次评估仍命中（soft 档每条输入重复提醒的前提）
	reason2, blockedRule2 := CheckStop(pc, "s3")
	if reason2 == "" || blockedRule2 != "changelog_required" {
		t.Fatalf("未落标记时应重复命中，got (%q, %q)", reason2, blockedRule2)
	}
}

// TestMandatoryPointerAfterBase L3：首轮注入全文后，后续每轮仍注入"标题 + 路径"的
// 粘性指针（不重复全文），即使宿主压缩上下文把首轮全文摘要掉/沉入 lost-middle，
// 模型也能据此重读原文。
func TestMandatoryPointerAfterBase(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\nsummary: s\n---\n\n永远先跑 gofmt。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = InjectForPrompt(pc, "s-ptr", projDir, "第一次")
	out2 := InjectForPrompt(pc, "s-ptr", projDir, "第二次")
	if strings.Contains(out2, "永远先跑 gofmt") {
		t.Fatalf("第二轮不应重复 mandatory 全文，got: %q", out2)
	}
	if !strings.Contains(out2, "必守规约") || !strings.Contains(out2, "架构规约") || !strings.Contains(out2, "规约.md") {
		t.Fatalf("第二轮应注入粘性指针（标题 + 路径），got: %q", out2)
	}
}

// TestMandatoryNeverTruncated L4：mandatory 条目再长也不被预算截断（其余段在剩余
// 预算内截断）。旧实现用单 builder 砍头，长 INDEX/检索会把尾部 mandatory 静默砍掉。
func TestMandatoryNeverTruncated(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	longBody := strings.Repeat("甲", 3000)
	entry := "---\ntitle: 长规约\ntype: rule\nmandatory: true\nsummary: s\n---\n\n" + longBody + "\n"
	writeEntry(t, kbRoot, "rule.md", entry)
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s-long", projDir, "随便问问")
	if !strings.Contains(out, longBody) {
		t.Fatalf("mandatory 正文必须完整在场，len(out)=%d", len(out))
	}
	if strings.Contains(out, "已截断") {
		t.Fatalf("mandatory 不得携带截断标记，got: %q", out)
	}
}

// TestReinjectTurnsPeriodic L2 兜底：reinject_turns>0 时，按轮次周期性重注入
// mandatory 全文（轮次间为粘性指针）。reinject_turns=2 → 第1/3/5…轮全文。
func TestReinjectTurnsPeriodic(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: rule\nmandatory: true\nsummary: s\n---\n\n永远先跑 gofmt。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[inject]\nreinject_turns = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if out := InjectForPrompt(pc, "s-re", projDir, "q1"); !strings.Contains(out, "永远先跑 gofmt") {
		t.Fatalf("第1轮应注入全文，got: %q", out)
	}
	if out := InjectForPrompt(pc, "s-re", projDir, "q2"); strings.Contains(out, "永远先跑 gofmt") {
		t.Fatalf("第2轮不应重复全文，got: %q", out)
	}
	if out := InjectForPrompt(pc, "s-re", projDir, "q3"); !strings.Contains(out, "永远先跑 gofmt") {
		t.Fatalf("第3轮应周期性重注入全文，got: %q", out)
	}
}

// TestInjectRRFIgnoresAlphaBetaHint rrf 模式下配置了非默认 alpha/beta 时，
// 注入流程应记一行 ok.log 提示被忽略（仅 weighted 生效）。
func TestInjectRRFIgnoresAlphaBetaHint(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	// rrf（缺省）+ 非默认 alpha → 应提示被忽略
	cfg := "[retrieve]\nalpha = 2.0\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = InjectForPrompt(pc, "s-fusion", projDir, "RetrievalQuirk 是什么")
	logData, err := os.ReadFile(filepath.Join(registry.Home(), "ok.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "alpha/beta") {
		t.Errorf("rrf 模式下非默认 alpha 应记 ok.log 提示，got: %q", logData)
	}
}


// TestAdoptionLoop 注入→采纳全链路：检索注入挂账 InjectedKnowledge →
// post-tool 读知识库文件记 AdoptedKnowledge → 下一轮 prompt 开头入账
// entry_events(adopted)；mandatory 重读与项目外路径不计入。
func TestAdoptionLoop(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "规约.md", "---\ntitle: 架构规约\ntype: reference\nmandatory: true\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n永远先跑 gofmt。\n")
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 RetrievalQuirk 词。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(kbRoot, "state")
	// 第一轮：检索注入 → InjectedKnowledge 挂账 + injected 事件
	out := InjectForPrompt(pc, "s-adopt", projDir, "RetrievalQuirk 是什么")
	if !strings.Contains(out, "检索经验") {
		t.Fatalf("首轮应注入检索命中: %q", out)
	}
	st := state.Load(stateDir, "s-adopt")
	if len(st.InjectedKnowledge) != 1 || st.InjectedKnowledge[0] != "检索.md" {
		t.Fatalf("InjectedKnowledge 挂账失败: %+v", st.InjectedKnowledge)
	}
	// post-tool 读知识库内已注入条目 → 采纳挂账（知识库目录在项目路径之外）
	TrackTouched(pc, "s-adopt", "read_file", filepath.Join(kbRoot, "knowledge", "检索.md"))
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 1 || st.AdoptedKnowledge[0] != "检索.md" {
		t.Fatalf("采纳挂账失败: %+v", st.AdoptedKnowledge)
	}
	// mandatory 粘性指针重读不计入（mandatory 不经检索，不在 InjectedKnowledge）
	TrackTouched(pc, "s-adopt", "read_file", filepath.Join(kbRoot, "knowledge", "规约.md"))
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 1 {
		t.Fatalf("mandatory 重读不应计入采纳: %+v", st.AdoptedKnowledge)
	}
	// 未注入过的知识库文件不计入
	writeEntry(t, kbRoot, "别的.md", "---\ntitle: 别的\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n无关。\n")
	TrackTouched(pc, "s-adopt", "read_file", filepath.Join(kbRoot, "knowledge", "别的.md"))
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 1 {
		t.Fatalf("未注入条目不挂账: %+v", st.AdoptedKnowledge)
	}
	// 第二轮 prompt：开头入账 → entry_events 有 adopted 行，挂账清空
	_ = InjectForPrompt(pc, "s-adopt", projDir, "随便问问")
	st = state.Load(stateDir, "s-adopt")
	if len(st.AdoptedKnowledge) != 0 {
		t.Fatalf("入账后挂账应清空: %+v", st.AdoptedKnowledge)
	}
	db, err := index.Open(filepath.Join(kbRoot, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stats, err := db.FeedbackStats(30)
	if err != nil {
		t.Fatal(err)
	}
	s := stats["检索.md"]
	if s.Adoptions != 1 {
		t.Fatalf("entry_events 应有 1 条 adopted: %+v", s)
	}
	if s.Injections < 1 {
		t.Fatalf("entry_events 应有 injected 行: %+v", s)
	}
	// 回归：项目内文件仍走 Touched（既有行为不变）
	TrackTouched(pc, "s-adopt", "write_file", filepath.Join(projDir, "a.go"))
	st = state.Load(stateDir, "s-adopt")
	found := false
	for _, v := range st.Touched {
		if v == "a.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("项目内文件仍应记 Touched: %+v", st.Touched)
	}
}
