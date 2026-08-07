package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// setupZcode 隔离 ZCode 配置根与 OK_HOME（HookTimeoutSec 读全局配置），
// 返回 ZcodeHome。
func setupZcode(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_ZCODE_HOME", home)
	t.Setenv("OK_HOME", t.TempDir())
	return home
}

// currentExe 返回当前测试二进制的解析路径（HooksInstalled 以它为判定基准）。
func currentExe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

// readZcodeConfig 读入测试配置的 map 形态。
func readZcodeConfig(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(zcodeConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config.json 不是合法 JSON: %v", err)
	}
	return cfg
}

// countOKHooks 统计某事件里的 ok 自有 hook 数。
func countOKHooks(t *testing.T, cfg map[string]any, event string) int {
	t.Helper()
	events := zcodeEventsOf(cfg)
	groups, _ := events[event].([]any)
	n := 0
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hooks, _ := gm["hooks"].([]any)
		for _, h := range hooks {
			if hm, _ := h.(map[string]any); hm != nil && isOKZcodeHook(hm) {
				n++
			}
		}
	}
	return n
}

func TestZcodeDetect(t *testing.T) {
	setupZcode(t)
	if !(zcodeAgent{}).Detect() {
		t.Fatal("ZcodeHome 存在应检测为真")
	}
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (zcodeAgent{}).Detect() {
		t.Fatal("ZcodeHome 不存在应检测为假")
	}
}

func TestZcodeInstallHooks(t *testing.T) {
	setupZcode(t)
	exe := currentExe(t)
	if err := (zcodeAgent{}).InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	cfg := readZcodeConfig(t)
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks["enabled"] != true {
		t.Fatal("hooks.enabled 应为 true")
	}
	events := zcodeEventsOf(cfg)
	for _, e := range zcodeHookEvents {
		groups, _ := events[e.event].([]any)
		if len(groups) != 1 {
			t.Fatalf("%s 应有 1 个 hook 组, got %d", e.event, len(groups))
		}
		gm := groups[0].(map[string]any)
		matcher, _ := gm["matcher"].(string)
		if matcher != e.matcher {
			t.Fatalf("%s matcher = %q, want %q", e.event, matcher, e.matcher)
		}
		hm := gm["hooks"].([]any)[0].(map[string]any)
		if hm["type"] != "process" {
			t.Fatalf("%s type = %v", e.event, hm["type"])
		}
		args, _ := hm["args"].([]any)
		if len(args) != 3 || args[0] != "hook" || args[1] != e.okHook || args[2] != "claude" {
			t.Fatalf("%s args = %v", e.event, args)
		}
		if timeout, _ := hm["timeoutMs"].(float64); timeout != float64(HookTimeoutSec()*1000) {
			t.Fatalf("%s timeoutMs = %v", e.event, hm["timeoutMs"])
		}
	}
	if !(zcodeAgent{}).HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为真")
	}
}

func TestZcodeInstallIdempotent(t *testing.T) {
	setupZcode(t)
	exe := currentExe(t)
	a := (zcodeAgent{})
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	cfg := readZcodeConfig(t)
	for _, e := range zcodeHookEvents {
		if n := countOKHooks(t, cfg, e.event); n != 1 {
			t.Fatalf("%s 重复安装后应有 1 条 ok hook, got %d", e.event, n)
		}
	}
}

func TestZcodeInstallPreservesForeign(t *testing.T) {
	home := setupZcode(t)
	// 预置：未知顶层字段 + 用户在 UserPromptSubmit 里的自有 hook + 其他事件
	pre := `{
  "theme": "dark",
  "hooks": {
    "enabled": true,
    "events": {
      "UserPromptSubmit": [
        {"hooks": [{"type": "process", "command": "node", "args": ["guard.mjs"]}]}
      ],
      "PreToolUse": [
        {"matcher": "Bash", "hooks": [{"type": "process", "command": "node", "args": ["check.mjs"]}]}
      ]
    }
  }
}`
	cfgPath := filepath.Join(home, "cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (zcodeAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	cfg := readZcodeConfig(t)
	if cfg["theme"] != "dark" {
		t.Fatal("未知顶层字段应保留")
	}
	events := zcodeEventsOf(cfg)
	if n := countOKHooks(t, cfg, "UserPromptSubmit"); n != 1 {
		t.Fatalf("UserPromptSubmit 应有 1 条 ok hook, got %d", n)
	}
	// 用户自有 hook 与 ok 组同事件共存；PreToolUse 原样保留
	groups, _ := events["UserPromptSubmit"].([]any)
	if len(groups) != 2 {
		t.Fatalf("UserPromptSubmit 应为用户组+ok组共 2 组, got %d", len(groups))
	}
	pre2, _ := events["PreToolUse"].([]any)
	if len(pre2) != 1 {
		t.Fatal("PreToolUse 的用户 hook 不应受影响")
	}
	// 安装应留下备份
	if _, err := os.Stat(cfgPath + ".bak-openknowledge"); err != nil {
		t.Fatal("应生成 .bak-openknowledge 备份")
	}
}

func TestZcodeRemoveHooks(t *testing.T) {
	home := setupZcode(t)
	a := (zcodeAgent{})
	// 无配置 → false
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("无配置 RemoveHooks = %v, %v", removed, err)
	}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	cfg := readZcodeConfig(t)
	for _, e := range zcodeHookEvents {
		if n := countOKHooks(t, cfg, e.event); n != 0 {
			t.Fatalf("%s 移除后仍有 %d 条 ok hook", e.event, n)
		}
	}
	if a.HooksInstalled() {
		t.Fatal("移除后 HooksInstalled 应为假")
	}
	// 再次移除 → false
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("重复 RemoveHooks = %v, %v", removed, err)
	}
	_ = home
}

func TestZcodeEnsureHooks(t *testing.T) {
	setupZcode(t)
	exe := currentExe(t)
	a := (zcodeAgent{})
	cfgPath := zcodeConfigPath()

	// 无配置 → no-op（不创建文件）
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("无配置时 EnsureHooks 不应创建文件")
	}

	// 有配置但无 ok hook（用户从未安装或显式移除）→ no-op
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := `{"hooks":{"enabled":true,"events":{"Stop":[{"hooks":[{"type":"process","command":"node","args":["x.mjs"]}]}]}}}`
	if err := os.WriteFile(cfgPath, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfgPath)
	if string(data) != foreign {
		t.Fatal("无 ok hook 时 EnsureHooks 不应改动配置")
	}

	// 过期 ok hook（旧 exe 路径）→ 重写为当前
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Fatal("旧 exe 路径应判为未安装/过期")
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("自愈后 HooksInstalled 应为真")
	}

	// 当前 → no-op（文件不再变化）
	before, _ := os.ReadFile(cfgPath)
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Fatal("内容当前时 EnsureHooks 应 no-op")
	}
}

func TestZcodeSkillsDir(t *testing.T) {
	home := setupZcode(t)
	if got := (zcodeAgent{}).SkillsDir(); got != filepath.Join(home, "skills") {
		t.Fatalf("SkillsDir = %q", got)
	}
}
