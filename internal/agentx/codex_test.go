package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	// Windows 形态（包装文件裸路径，认 / 与 \ 分隔、大小写不敏感）仅在 Windows 期望命中；
	// 其他平台 isOKCodexHook 只认 quoted 后缀，.cmd 案例期望 false。
	isWin := runtime.GOOS == "windows"
	cases := []struct {
		name string
		hook map[string]any
		want bool
	}{
		{"prompt 反斜杠", map[string]any{"type": "command", "command": `C:\Users\X\.codex\ok-hook-prompt.cmd`}, isWin},
		{"post-tool 反斜杠", map[string]any{"type": "command", "command": `C:\Users\X\.codex\ok-hook-post-tool.cmd`}, isWin},
		{"stop 反斜杠", map[string]any{"type": "command", "command": `C:\Users\X\.codex\ok-hook-stop.cmd`}, isWin},
		{"正斜杠分隔", map[string]any{"type": "command", "command": `C:/Users/X/.codex/ok-hook-prompt.cmd`}, isWin},
		{"大小写变体命中", map[string]any{"type": "command", "command": `C:\USERS\X\.CODEX\OK-HOOK-PROMPT.CMD`}, isWin},
		{"尾部空白容忍", map[string]any{"type": "command", "command": `C:\x\ok-hook-stop.cmd  `}, isWin},
		{"非 command 类型", map[string]any{"type": "process", "command": `C:\x\ok-hook-prompt.cmd`}, false},
		{"第三方命令", map[string]any{"type": "command", "command": "echo hi"}, false},
		{"相似文件名 prompter 不误判", map[string]any{"type": "command", "command": `C:\x\ok-hook-prompter.cmd`}, false},
		{"相似前缀 my- 不误判", map[string]any{"type": "command", "command": `C:\x\my-ok-hook-prompt.cmd`}, false},
		{"后缀 .bak 不误判", map[string]any{"type": "command", "command": `C:\x\ok-hook-prompt.cmd.bak`}, false},
		{"裸文件名无分隔不命中", map[string]any{"type": "command", "command": `ok-hook-prompt.cmd`}, false},
		// 旧 quoted 形态（Fix D 前安装的条目）仍识别——重装/自愈迁移清理用，
		// 不识别则旧组剥离不掉、与 .cmd 新组堆积重复。
		{"旧 quoted 形态 prompt 迁移识别", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude`}, true},
		{"旧 quoted 形态 post-tool 迁移识别", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook post-tool claude`}, true},
		{"旧 quoted 形态 stop 迁移识别", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook stop claude`}, true},
	}
	for _, c := range cases {
		if got := isOKCodexHook(c.hook); got != c.want {
			t.Errorf("%s: isOKCodexHook() = %v, want %v", c.name, got, c.want)
		}
	}
}

// codexWantWrappers 断言 home 下三个包装文件存在且内容为 exe 的期望形态。
func codexWantWrappers(t *testing.T, home, exe string) {
	t.Helper()
	for _, okHook := range []string{"prompt", "post-tool", "stop"} {
		wp := filepath.Join(home, "ok-hook-"+okHook+".cmd")
		data, err := os.ReadFile(wp)
		if err != nil {
			t.Errorf("包装文件 %s 未生成: %v", wp, err)
			continue
		}
		want := `@"` + exe + `" hook ` + okHook + " claude\r\n"
		if string(data) != want {
			t.Errorf("包装文件 %s 内容 = %q, want %q", wp, string(data), want)
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
	// 期望命令形态随平台分叉：Windows = 包装文件裸路径（无引号、反斜杠形态）；
	// 其他平台 = quoted shell 串（经 codexCommand 生成，与生产形态一致）。
	var wantCmd map[string]string
	if runtime.GOOS == "windows" {
		wantCmd = map[string]string{
			"UserPromptSubmit": filepath.Join(home, "ok-hook-prompt.cmd"),
			"PostToolUse":      filepath.Join(home, "ok-hook-post-tool.cmd"),
			"Stop":             filepath.Join(home, "ok-hook-stop.cmd"),
		}
	} else {
		wantCmd = map[string]string{
			"UserPromptSubmit": codexCommand(codexTestExe(), "prompt"),
			"PostToolUse":      codexCommand(codexTestExe(), "post-tool"),
			"Stop":             codexCommand(codexTestExe(), "stop"),
		}
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
	// 包装文件：3 个，内容为 @"<exe>" hook <okHook> claude（CRLF 结尾）——仅 Windows
	if runtime.GOOS == "windows" {
		codexWantWrappers(t, home, codexTestExe())
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

// TestCodexInstallStripsLegacyForm 迁移集成测试：预置 Fix D（add5d38）前安装的
// 旧 quoted 形态 ok 三事件组，InstallHooks 必须识别并剥离它们——否则旧组残留 +
// 新 .cmd 组追加，每事件堆积两个 ok 组（prompt 重复注入、post-tool 重复追记）。
func TestCodexInstallStripsLegacyForm(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyGroup := func(matcher, okHook string) string {
		return `{"matcher":"` + matcher + `","hooks":[{"type":"command","command":"\"D:/old/ok.exe\" hook ` + okHook + ` claude","timeout":20}]}`
	}
	preset := `{"hooks":{` +
		`"UserPromptSubmit":[` + legacyGroup("*", "prompt") + `],` +
		`"PostToolUse":[` + legacyGroup("apply_patch", "post-tool") + `],` +
		`"Stop":[` + legacyGroup("*", "stop") + `]}}`
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
	events, _ := cfg["hooks"].(map[string]any)
	var wantCmd map[string]string
	if runtime.GOOS == "windows" {
		wantCmd = map[string]string{
			"UserPromptSubmit": filepath.Join(home, "ok-hook-prompt.cmd"),
			"PostToolUse":      filepath.Join(home, "ok-hook-post-tool.cmd"),
			"Stop":             filepath.Join(home, "ok-hook-stop.cmd"),
		}
	} else {
		wantCmd = map[string]string{
			"UserPromptSubmit": codexCommand(codexTestExe(), "prompt"),
			"PostToolUse":      codexCommand(codexTestExe(), "post-tool"),
			"Stop":             codexCommand(codexTestExe(), "stop"),
		}
	}
	for ev, cmd := range wantCmd {
		groups, _ := events[ev].([]any)
		if len(groups) != 1 {
			t.Fatalf("%s: 迁移后 ok 组数 = %d, want 1（旧 quoted 组应被剥离）", ev, len(groups))
		}
		gm, _ := groups[0].(map[string]any)
		hooks, _ := gm["hooks"].([]any)
		if len(hooks) != 1 {
			t.Fatalf("%s: 组内 hooks 数 = %d, want 1", ev, len(hooks))
		}
		hm, _ := hooks[0].(map[string]any)
		if c, _ := hm["command"].(string); c != cmd {
			t.Errorf("%s: command = %q, want %q", ev, c, cmd)
		}
	}
	// 旧 quoted 残留检查：Windows 期望全文无 quoted 后缀（.cmd 形态替换了它）；
	// 其他平台新形态本身就是 quoted——只查旧 exe 路径不残留。
	if s := string(data); strings.Contains(s, "D:/old") ||
		(runtime.GOOS == "windows" && (strings.Contains(s, " hook prompt claude") ||
			strings.Contains(s, " hook post-tool claude") ||
			strings.Contains(s, " hook stop claude"))) {
		t.Errorf("hooks.json 旧 quoted 形态残留:\n%s", s)
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
	// 3 个包装文件随卸载删除 + 用户同名文件不误删——仅 Windows 有包装形态
	if runtime.GOOS == "windows" {
		for _, okHook := range []string{"prompt", "post-tool", "stop"} {
			if _, err := os.Stat(filepath.Join(home, "ok-hook-"+okHook+".cmd")); !os.IsNotExist(err) {
				t.Errorf("包装文件 ok-hook-%s.cmd 未删除", okHook)
			}
		}
		removed, err = a.RemoveHooks()
		if err != nil || removed {
			t.Fatalf("二次 RemoveHooks = (%v, %v), want (false, nil)", removed, err)
		}
		// 用户同名文件（内容非 ok 生成）不误删
		userFile := filepath.Join(home, "ok-hook-prompt.cmd")
		if err := os.WriteFile(userFile, []byte("@echo my own hook\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		removed, err = a.RemoveHooks()
		if err != nil || removed {
			t.Fatalf("三次 RemoveHooks = (%v, %v), want (false, nil)", removed, err)
		}
		if data, err := os.ReadFile(userFile); err != nil || !strings.Contains(string(data), "my own hook") {
			t.Error("用户同名包装文件被误删")
		}
		return
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
	// 包装内容刷新为新 exe（仅 Windows 有包装形态）
	if runtime.GOOS == "windows" {
		codexWantWrappers(t, home, currentExe(t))
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

// TestCodexExeMigrationKeepsTrust 是本设计的核心收益：exe 迁移后 EnsureHooks 自愈
// 只重写包装文件内容，hooks.json（命令=包装路径）与 config.toml 信任哈希逐字节不动
// ——迁移不再破信任（旧 quoted 形态下 hooks.json 内容变 → 哈希过期 → 静默跳过）。
func TestCodexExeMigrationKeepsTrust(t *testing.T) {
	home := isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	hooksBefore, err := os.ReadFile(codexHooksPath())
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(codexConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	newExe := currentExe(t)
	if err := a.EnsureHooks(newExe); err != nil {
		t.Fatal(err)
	}
	hooksAfter, err := os.ReadFile(codexHooksPath())
	if err != nil {
		t.Fatal(err)
	}
	configAfter, err := os.ReadFile(codexConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// Windows：包装内容刷新为新 exe；hooks.json（命令=包装路径）与 config.toml
		// 信任记录逐字节不动——迁移不破信任。
		codexWantWrappers(t, home, newExe)
		if string(hooksAfter) != string(hooksBefore) {
			t.Error("exe 迁移不应触发 hooks.json 重写（命令=包装路径，与 exe 无关）")
		}
		if string(configAfter) != string(configBefore) {
			t.Errorf("exe 迁移后信任记录变化（信任不应过期）:\nbefore:\n%s\nafter:\n%s", configBefore, configAfter)
		}
	} else {
		// 其他平台（quoted 形态）：exe 直接进命令串，迁移必然重写 hooks.json 并同步
		// 重算信任——断言旧路径无残留且信任与当前内容一致。
		if string(hooksAfter) == string(hooksBefore) {
			t.Error("quoted 形态下 exe 迁移应重写 hooks.json")
		}
		if strings.Contains(string(hooksAfter), `D:\old`) || strings.Contains(string(hooksAfter), `D:/old`) {
			t.Error("旧 exe 路径残留")
		}
		cfg, err := loadCodexHooks()
		if err != nil {
			t.Fatal(err)
		}
		if !codexTrustConsistent(string(configAfter), codexTrustEntries(codexEventsOf(cfg), newExe)) {
			t.Errorf("exe 迁移后信任记录与 hooks.json 内容不一致:\n%s", string(configAfter))
		}
	}
	if !a.HooksInstalled() {
		t.Error("exe 迁移自愈后 HooksInstalled 应为 true")
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

func TestCodexHooksFlagOn(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"段内 true", "[features]\ncodex_hooks = true\n", true},
		{"段内 false", "[features]\ncodex_hooks = false\n", false},
		{"段内无此键", "[features]\nunified_exec = true\n", false},
		{"无 features 段", "[projects.'x']\ntrust_level = \"trusted\"\n", false},
		{"注释行不算", "[features]\n# codex_hooks = true\n", false},
		{"键在别的段", "[hooks.state]\ncodex_hooks = true\n", false},
		{"值带引号只认布尔", "[features]\ncodex_hooks = \"true\"\n", false},
		{"空文本", "", false},
		{"段头带尾注释也算", "[features] # 我的特性\ncodex_hooks = true", true},
		{"features 子表不算", "[features.local]\ncodex_hooks = true", false},
		{"键名词边界 codex_hooks_extra 不命中", "[features]\ncodex_hooks_extra = true\n", false},
	}
	for _, c := range cases {
		if got := codexHooksFlagOn(c.text); got != c.want {
			t.Errorf("%s: codexHooksFlagOn(%q) = %v, want %v", c.name, c.text, got, c.want)
		}
	}
}

func TestCodexEnableHooksFlag(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		want        string
		wantChanged bool
	}{
		{"空文本追加段", "", "[features]\ncodex_hooks = true\n", true},
		{
			"无段文末追加且原文逐字节保留",
			"[projects.'x']\ntrust_level = \"trusted\"\n",
			"[projects.'x']\ntrust_level = \"trusted\"\n[features]\ncodex_hooks = true\n",
			true,
		},
		{
			"无段且原文末无换行先补换行",
			"[projects.'x']\ntrust_level = \"trusted\"",
			"[projects.'x']\ntrust_level = \"trusted\"\n[features]\ncodex_hooks = true\n",
			true,
		},
		{
			"段存在无键插在段标题行后",
			"[features]\nunified_exec = true\n",
			"[features]\ncodex_hooks = true\nunified_exec = true\n",
			true,
		},
		{
			"false 整行替换为 true",
			"[features]\ncodex_hooks = false\n",
			"[features]\ncodex_hooks = true\n",
			true,
		},
		{
			"已 true 原文返回无改动",
			"[features]\ncodex_hooks = true\n",
			"[features]\ncodex_hooks = true\n",
			false,
		},
		{
			"段内其他键保留不动",
			"[features]\nunified_exec = true\ncodex_hooks = false\n",
			"[features]\nunified_exec = true\ncodex_hooks = true\n",
			true,
		},
		{
			"带尾注释段头无键插入其后不追加新段",
			"[features] # note\nunified_exec = true\n",
			"[features] # note\ncodex_hooks = true\nunified_exec = true\n",
			true,
		},
		{
			"codex_hooks_extra 原样保留真键正常插入",
			"[features]\ncodex_hooks_extra = false\n",
			"[features]\ncodex_hooks = true\ncodex_hooks_extra = false\n",
			true,
		},
	}
	for _, c := range cases {
		got, changed := codexEnableHooksFlag(c.text)
		if changed != c.wantChanged || got != c.want {
			t.Errorf("%s: codexEnableHooksFlag(%q) = (%q, %v), want (%q, %v)",
				c.name, c.text, got, changed, c.want, c.wantChanged)
		}
	}
}

func TestCodexInstallEnablesFlag(t *testing.T) {
	home := isolateCodex(t)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cp := filepath.Join(home, "config.toml")
	preset := "[projects.'d:\\x']\ntrust_level = \"trusted\"\n\n[features]\ncodex_hooks = false\n"
	if err := os.WriteFile(cp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	data, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !codexHooksFlagOn(text) {
		t.Errorf("InstallHooks 后 codex_hooks 特性未开启: %q", text)
	}
	if !strings.Contains(text, "[projects.'d:\\x']\ntrust_level = \"trusted\"") {
		t.Errorf("projects 段被改动: %q", text)
	}
	bak, err := os.ReadFile(cp + ".bak-openknowledge")
	if err != nil {
		t.Fatal("未生成 config.toml.bak-openknowledge 备份")
	}
	if string(bak) != preset {
		t.Error("备份内容不是改动前原文")
	}
}

func TestCodexHooksInstalledFlagOff(t *testing.T) {
	isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为 true")
	}
	// 把 config.toml 的 flag 行改回 false → HooksInstalled 应变 false。
	cp := codexConfigPath()
	data, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	off := strings.Replace(string(data), "codex_hooks = true", "codex_hooks = false", 1)
	if off == string(data) {
		t.Fatal("config.toml 中未找到 codex_hooks = true 行")
	}
	if err := os.WriteFile(cp, []byte(off), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Error("特性开关关闭后 HooksInstalled 应为 false")
	}
}

func TestCodexEnsureHooksReenablesFlag(t *testing.T) {
	isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	// 关掉 flag，记录 hooks.json 内容用于比对。
	cp := codexConfigPath()
	data, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	off := strings.Replace(string(data), "codex_hooks = true", "codex_hooks = false", 1)
	if err := os.WriteFile(cp, []byte(off), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(codexHooksPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	if !codexHooksFlagOn(string(data)) {
		t.Errorf("EnsureHooks 后 codex_hooks 特性未恢复开启: %q", string(data))
	}
	after, err := os.ReadFile(codexHooksPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("hooks.json 内容未过期却被无谓重写")
	}
}

func TestCodexInstallCreatesConfig(t *testing.T) {
	home := isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml 未创建: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "[features]") || !codexHooksFlagOn(text) {
		t.Errorf("config.toml 缺少 [features] codex_hooks = true: %q", text)
	}
}

// ---------- hooks.state 信任记录（Codex 信任门）----------

func TestCodexTrustHash(t *testing.T) {
	// 夹具值经 Codex 源码（hooks/src/engine/discovery.rs hook_hash →
	// config/src/fingerprint.rs version_for_toml）与真实 config.toml 信任记录
	// （含 .bak-openknowledge 里的 D:/software 旧记录）逐位双向验证：
	// sha256:<hex(sorted-keys compact JSON of {event_name, hooks, matcher?})>，
	// matcher 仅过滤型事件（PostToolUse）入哈希。
	cases := []struct {
		name    string
		label   string
		filters bool
		matcher string
		command string
		timeout int
		want    string
	}{
		{"prompt develop exe", "user_prompt_submit", false, "*", `"D:/develop/OpenKnowledge/dist/ok.exe" hook prompt claude`, 20, "sha256:e4b77161ebbc88a1b5f5fa2fde760033574935bcfa0ce4acf547f6b0ed38a4ad"},
		{"prompt software exe", "user_prompt_submit", false, "*", `"D:/software/OpenKnowledge/ok.exe" hook prompt claude`, 20, "sha256:3e6553b430210b5fbf51d06dd7b2be42ad85d3112ab90234375f2b34ca9c160f"},
		{"post-tool matcher 入哈希", "post_tool_use", true, "apply_patch", `"D:/software/OpenKnowledge/ok.exe" hook post-tool claude`, 20, "sha256:561c087a92ca0ef83ff19c802002dc06f7098559e1870f4b1bbee47b09df0197"},
		{"stop matcher 不入哈希", "stop", false, "*", `"D:/software/OpenKnowledge/ok.exe" hook stop claude`, 20, "sha256:1c1f6a1484c19665eff535089027e6b4d4db870ee8acda5b5c03c4f434f03fed"},
	}
	for _, c := range cases {
		if got := codexTrustHash(c.label, c.filters, c.matcher, c.command, c.timeout); got != c.want {
			t.Errorf("%s: codexTrustHash() = %q, want %q", c.name, got, c.want)
		}
	}
}

func codexTrustTestEntries() [][2]string {
	return [][2]string{
		{`C:\codex\hooks.json:user_prompt_submit:0:0`, "sha256:aaa"},
		{`C:\codex\hooks.json:post_tool_use:0:0`, "sha256:bbb"},
		{`C:\codex\hooks.json:stop:0:0`, "sha256:ccc"},
	}
}

func codexTrustBlock(key, hash string) string {
	return "[hooks.state.'" + key + "']\ntrusted_hash = \"" + hash + "\"\nenabled = true\n"
}

func TestCodexTrustEdits(t *testing.T) {
	entries := codexTrustTestEntries()
	wantAll := codexTrustBlock(entries[0][0], entries[0][1]) +
		codexTrustBlock(entries[1][0], entries[1][1]) +
		codexTrustBlock(entries[2][0], entries[2][1])

	// 空文本 → 文末追加三节
	got, changed := codexTrustEdits("", entries)
	if !changed || got != wantAll {
		t.Errorf("空文本: codexTrustEdits = (%q, %v), want (%q, true)", got, changed, wantAll)
	}
	// 二次运行 → (原文, false)
	again, changedAgain := codexTrustEdits(got, entries)
	if changedAgain || again != got {
		t.Errorf("幂等: 二次运行 = (%q, %v), want (原文, false)", again, changedAgain)
	}

	// 第三方 hooks.state 节逐字节保留，ok 节追加其后
	third := "[hooks.state.'C:\\codex\\hooks.json:pre_tool_use:0:0']\ntrusted_hash = \"sha256:third\"\nenabled = false\n"
	got, changed = codexTrustEdits(third, entries)
	if !changed || got != third+wantAll {
		t.Errorf("第三方节保留: got (%q, %v), want (%q, true)", got, changed, third+wantAll)
	}

	// 过期哈希替换、enabled 缺失补（紧跟 trusted_hash 行后）
	stale := "[hooks.state.'" + entries[0][0] + "']\ntrusted_hash = \"sha256:old\"\n"
	got, changed = codexTrustEdits(stale, entries)
	want := codexTrustBlock(entries[0][0], entries[0][1]) +
		codexTrustBlock(entries[1][0], entries[1][1]) +
		codexTrustBlock(entries[2][0], entries[2][1])
	if !changed || got != want {
		t.Errorf("过期替换+补 enabled: got (%q, %v), want (%q, true)", got, changed, want)
	}

	// 节头带尾注释 → 识别为已存在，不重复追加；节内其他行不动
	commented := "[hooks.state.'" + entries[0][0] + "'] # ok 的 prompt\ntrusted_hash = \"" + entries[0][1] + "\"\nenabled = true\n"
	got, changed = codexTrustEdits(commented, entries)
	want = commented + codexTrustBlock(entries[1][0], entries[1][1]) +
		codexTrustBlock(entries[2][0], entries[2][1])
	if !changed || got != want {
		t.Errorf("尾注释节头: got (%q, %v), want (%q, true)", got, changed, want)
	}
}

func TestCodexInstallWritesTrust(t *testing.T) {
	t.Run("三节哈希与 codexTrustHash 重算一致", func(t *testing.T) {
		home := isolateCodex(t)
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		a := codexAgent{}
		if err := a.InstallHooks(codexTestExe()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(codexConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		cfg, err := loadCodexHooks()
		if err != nil {
			t.Fatal(err)
		}
		entries := codexTrustEntries(codexEventsOf(cfg), codexTestExe())
		if len(entries) != 3 {
			t.Fatalf("信任记录条数 = %d, want 3", len(entries))
		}
		if !codexTrustConsistent(text, entries) {
			t.Errorf("config.toml 信任记录与重算不一致:\n%s", text)
		}
		for _, e := range entries {
			if !strings.Contains(text, "[hooks.state.'"+e[0]+"']") {
				t.Errorf("缺节 [hooks.state.'%s']", e[0])
			}
			// key 形态：hooks.json 路径 + label + 组索引 0 + hook 索引 0
			if !strings.HasPrefix(e[0], codexHooksPath()+":") || !strings.HasSuffix(e[0], ":0:0") {
				t.Errorf("节 key 形态异常: %s", e[0])
			}
		}
	})
	t.Run("预置第三方组时 ok 节索引顺移为 1", func(t *testing.T) {
		home := isolateCodex(t)
		hp := filepath.Join(home, "hooks.json")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		preset := `{"hooks":{"UserPromptSubmit":[{"matcher":"*","hooks":[{"type":"command","command":"third-party"}]}]}}`
		if err := os.WriteFile(hp, []byte(preset), 0o644); err != nil {
			t.Fatal(err)
		}
		a := codexAgent{}
		if err := a.InstallHooks(codexTestExe()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(codexConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("%s:user_prompt_submit:1:0", codexHooksPath())
		if !strings.Contains(string(data), "[hooks.state.'"+key+"']") {
			t.Errorf("第三方组在前时 ok 节 key 应为 :1:0:\n%s", string(data))
		}
	})
	t.Run("重复安装幂等", func(t *testing.T) {
		home := isolateCodex(t)
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		a := codexAgent{}
		if err := a.InstallHooks(codexTestExe()); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(codexConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		if err := a.InstallHooks(codexTestExe()); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(codexConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("重复安装改动了 config.toml:\nbefore: %s\nafter: %s", string(before), string(after))
		}
	})
}

func TestCodexHooksInstalledTrust(t *testing.T) {
	isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为 true")
	}
	// 篡改一条 trusted_hash → 信任过期（Codex 侧将静默跳过全部 hooks）→ false。
	cp := codexConfigPath()
	data, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(data), `trusted_hash = "sha256:`, `trusted_hash = "sha256:0000`, 1)
	if stale == string(data) {
		t.Fatal("config.toml 中未找到 trusted_hash 行")
	}
	if err := os.WriteFile(cp, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Error("trusted_hash 过期后 HooksInstalled 应为 false")
	}
}

func TestCodexEnsureHooksRestoresTrust(t *testing.T) {
	isolateCodex(t)
	a := codexAgent{}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	// 破坏信任：删一条 enabled 行 + 篡改一条哈希（模拟 hooks.json 内容变化后信任过期）。
	cp := codexConfigPath()
	data, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(data), "enabled = true\n", "", 1)
	broken = strings.Replace(broken, `trusted_hash = "sha256:`, `trusted_hash = "sha256:ffff`, 1)
	if broken == string(data) {
		t.Fatal("破坏未生效")
	}
	if err := os.WriteFile(cp, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Fatal("信任破坏后 HooksInstalled 应为 false")
	}
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Error("EnsureHooks 自愈后 HooksInstalled 应为 true")
	}
	data, err = os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadCodexHooks()
	if err != nil {
		t.Fatal(err)
	}
	if !codexTrustConsistent(string(data), codexTrustEntries(codexEventsOf(cfg), currentExe(t))) {
		t.Errorf("自愈后信任记录与 hooks.json 内容不一致:\n%s", string(data))
	}
}

func TestCodexRemoveHooksCleansTrust(t *testing.T) {
	home := isolateCodex(t)
	hp := filepath.Join(home, "hooks.json")
	cp := filepath.Join(home, "config.toml")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hp, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	thirdKey := fmt.Sprintf("%s:pre_tool_use:0:0", codexHooksPath())
	preset := "[hooks.state.'" + thirdKey + "']\ntrusted_hash = \"sha256:third\"\nenabled = true\n"
	if err := os.WriteFile(cp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := codexAgent{}
	if err := a.InstallHooks(codexTestExe()); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = (%v, %v), want (true, nil)", removed, err)
	}
	data, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, label := range []string{"user_prompt_submit", "post_tool_use", "stop"} {
		if strings.Contains(text, ":"+label+":") {
			t.Errorf("ok 信任节 %s 未清理:\n%s", label, text)
		}
	}
	if !strings.Contains(text, "[hooks.state.'"+thirdKey+"']") {
		t.Error("第三方 hooks.state 节被误删")
	}
	if !strings.Contains(text, "[features]") || !codexHooksFlagOn(text) {
		t.Error("[features] 特性开关不应随卸载关闭")
	}
}
