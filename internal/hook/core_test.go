package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/project"
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
