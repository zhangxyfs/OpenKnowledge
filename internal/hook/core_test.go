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
