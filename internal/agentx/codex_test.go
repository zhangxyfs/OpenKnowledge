package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateCodex 隔离 codex 配置根与 ok 全局配置（HookTimeoutSec 读它）；
// CODEX_HOME 指向不存在目录防真实环境变量泄漏（OK_CODEX_HOME 优先于它）。
func isolateCodex(t *testing.T) string {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex-official"))
	home := filepath.Join(t.TempDir(), "codex")
	t.Setenv("OK_CODEX_HOME", home)
	return home
}

func codexTestExe() string { return `D:\develop\OpenKnowledge\dist\ok.exe` }

func TestCodexHomeEnvOverride(t *testing.T) {
	home := isolateCodex(t)
	if CodexHome() != home {
		t.Fatalf("CodexHome() = %q, want %q（OK_CODEX_HOME 应最优先）", CodexHome(), home)
	}
}

func TestCodexHomeOfficialEnv(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("OK_CODEX_HOME", "") // 置空落到下一级
	official := filepath.Join(t.TempDir(), "codex-official")
	t.Setenv("CODEX_HOME", official)
	if CodexHome() != official {
		t.Fatalf("CodexHome() = %q, want %q（CODEX_HOME 次之）", CodexHome(), official)
	}
}

func TestIsOKCodexHook(t *testing.T) {
	cases := []struct {
		name string
		hook map[string]any
		want bool
	}{
		{"prompt", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude`}, true},
		{"post-tool", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook post-tool claude`}, true},
		{"stop", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook stop claude`}, true},
		{"尾部空白容忍", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude  `}, true},
		{"非 command 类型", map[string]any{"type": "process", "command": `"D:/x/ok.exe" hook prompt claude`}, false},
		{"非 ok 命令", map[string]any{"type": "command", "command": "echo hi"}, false},
		{"相邻词误匹配", map[string]any{"type": "command", "command": "myhook prompt claude"}, false},
		{"缺 claude 协议段", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt`}, false},
	}
	for _, c := range cases {
		if got := isOKCodexHook(c.hook); got != c.want {
			t.Errorf("%s: isOKCodexHook() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCodexDetect(t *testing.T) {
	home := isolateCodex(t)
	a := codexAgent{}
	if a.Detect() {
		t.Error("目录不存在时不应 Detect")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.Detect() {
		t.Error("~/.codex 存在应 Detect")
	}
}

func TestCodexRegistered(t *testing.T) {
	isolateCodex(t)
	a, ok := Find("codex")
	if !ok {
		t.Fatal("codexAgent 未注册（init/Register 缺失）")
	}
	if a.ID() != "codex" {
		t.Errorf("ID() = %q, want codex", a.ID())
	}
}

func TestCodexInstallHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := `{"note":"keep","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	if err := os.WriteFile(hp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	data, err := os.ReadFile(hp)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("写回后 JSON 非法: %v", err)
	}
	if cfg["note"] != "keep" {
		t.Error("既有未知字段 note 被丢")
	}
	events, _ := cfg["hooks"].(map[string]any)
	pre, _ := events["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Error("第三方 PreToolUse 组被删")
	}
	wantCmd := map[string]string{
		"UserPromptSubmit": `"D:/develop/OpenKnowledge/dist/ok.exe" hook prompt claude`,
		"PostToolUse":      `"D:/develop/OpenKnowledge/dist/ok.exe" hook post-tool claude`,
		"Stop":             `"D:/develop/OpenKnowledge/dist/ok.exe" hook stop claude`,
	}
	wantMatcher := map[string]string{"UserPromptSubmit": "*", "PostToolUse": "apply_patch", "Stop": "*"}
	wantTimeout := float64(HookTimeoutSec())
	for ev, cmd := range wantCmd {
		groups, _ := events[ev].([]any)
		found := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if m, _ := gm["matcher"].(string); m != wantMatcher[ev] {
				continue
			}
			for _, h := range gm["hooks"].([]any) {
				hm, _ := h.(map[string]any)
				if c, _ := hm["command"].(string); c == cmd {
					if to, _ := hm["timeout"].(float64); to != wantTimeout {
						t.Errorf("%s: timeout = %v, want %v", ev, to, wantTimeout)
					}
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s: 未找到期望的 ok hook 组", ev)
		}
	}
	if _, err := os.Stat(hp + ".bak-openknowledge"); err != nil {
		t.Error("未生成 .bak-openknowledge 备份")
	}
}

func TestCodexInstallIdempotent(t *testing.T) {
	home := isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "hooks.json"))
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	for _, ev := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		groups, _ := events[ev].([]any)
		if len(groups) != 1 {
			t.Fatalf("重复安装产生堆积: %s 组数 = %d, want 1", ev, len(groups))
		}
	}
}

func TestCodexCorruptHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(hp, []byte("{broken"), 0o644)
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err == nil {
		t.Fatal("损坏文件应报错")
	}
	data, _ := os.ReadFile(hp)
	if string(data) != "{broken" {
		t.Error("损坏文件被覆盖")
	}
}

func TestCodexRemoveHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	_ = os.MkdirAll(home, 0o755)
	preset := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	_ = os.WriteFile(hp, []byte(preset), 0o644)
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = (%v, %v), want (true, nil)", removed, err)
	}
	data, _ := os.ReadFile(hp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	if hasOKCodexHook(events) {
		t.Error("ok hooks 未移除干净")
	}
	if pre, _ := events["PreToolUse"].([]any); len(pre) != 1 {
		t.Error("第三方 PreToolUse 被误删")
	}
	removed, err = a.RemoveHooks()
	if err != nil || removed {
		t.Fatalf("二次 RemoveHooks = (%v, %v), want (false, nil)", removed, err)
	}
}

func TestCodexEnsureHooks(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	a := codexAgent{}
	// 从未安装（文件不存在）→ no-op，不创建文件
	if err := a.EnsureHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hp); !os.IsNotExist(err) {
		t.Error("从未安装时 EnsureHooks 不应创建文件")
	}
	// 安装后把 exe 改旧 → EnsureHooks 重写为新 exe。
	// 注意：HooksInstalled 以 os.Executable() 为判定基准（见 TestCodexHooksInstalled），
	// 故此处用 currentExe(t) 作为"新 exe"（zcode_test.go 同款模式）。
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Error("自愈后 HooksInstalled 应为 true")
	}
	data, _ := os.ReadFile(hp)
	if strings.Contains(string(data), `D:\old`) || strings.Contains(string(data), `D:/old`) {
		t.Error("旧 exe 路径残留")
	}
	// 用户显式移除 → 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(hp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if hasOKCodexHook(codexEventsOf(cfg)) {
		t.Error("用户显式移除的集成被复活")
	}
}

func TestCodexHooksInstalled(t *testing.T) {
	isolateCodex(t)
	a := codexAgent{}
	if a.HooksInstalled() {
		t.Error("未安装时不应为 true")
	}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	// HooksInstalled 用 os.Executable() 比对——测试二进制路径与安装路径不同，应为 false；
	// 用安装时的同一路径判定逻辑直接测 codexHooksCurrent。
	data, _ := os.ReadFile(codexHooksPath())
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if !codexHooksCurrent(codexEventsOf(cfg), codexTestExe()) {
		t.Error("安装后 codexHooksCurrent(安装 exe) 应为 true")
	}
	if codexHooksCurrent(codexEventsOf(cfg), `D:\other\ok.exe`) {
		t.Error("换 exe 后 codexHooksCurrent 应为 false")
	}
}
