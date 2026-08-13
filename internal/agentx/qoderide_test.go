package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateQoderIde 隔离 Qoder CN IDE（灵码内核）配置根与 ok 全局配置（HookTimeoutSec
// 读它）；~/.lingma 无官方目录重定位环境变量，仅 OK_QODER_IDE_HOME 测试口。
func isolateQoderIde(t *testing.T) string {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), "lingma")
	t.Setenv("OK_QODER_IDE_HOME", home)
	return home
}

func qoderIdeTestExe() string { return `D:\develop\OpenKnowledge\dist\ok.exe` }

func TestLingmaHomeEnvOverride(t *testing.T) {
	home := isolateQoderIde(t)
	if LingmaHome() != home {
		t.Fatalf("LingmaHome() = %q, want %q（OK_QODER_IDE_HOME 应最优先）", LingmaHome(), home)
	}
}

func TestIsOKQoderIdeHook(t *testing.T) {
	// Windows 形态（包装文件裸路径，认 / 与 \ 分隔、大小写不敏感）仅在 Windows 期望
	// 命中；其他平台 isOKQoderIdeHook 只认 quoted 后缀，.cmd 案例期望 false。
	isWin := runtime.GOOS == "windows"
	cases := []struct {
		name string
		hook map[string]any
		want bool
	}{
		{"prompt 反斜杠", map[string]any{"type": "command", "command": `C:\Users\X\.lingma\ok-hook-prompt.cmd`}, isWin},
		{"post-tool 反斜杠", map[string]any{"type": "command", "command": `C:\Users\X\.lingma\ok-hook-post-tool.cmd`}, isWin},
		{"stop 反斜杠", map[string]any{"type": "command", "command": `C:\Users\X\.lingma\ok-hook-stop.cmd`}, isWin},
		{"正斜杠分隔", map[string]any{"type": "command", "command": `C:/Users/X/.lingma/ok-hook-prompt.cmd`}, isWin},
		{"大小写变体命中", map[string]any{"type": "command", "command": `C:\USERS\X\.LINGMA\OK-HOOK-PROMPT.CMD`}, isWin},
		{"尾部空白容忍", map[string]any{"type": "command", "command": `C:\x\ok-hook-stop.cmd  `}, isWin},
		{"非 command 类型", map[string]any{"type": "prompt", "command": `C:\x\ok-hook-prompt.cmd`}, false},
		{"第三方命令", map[string]any{"type": "command", "command": "echo hi"}, false},
		{"相似文件名 prompter 不误判", map[string]any{"type": "command", "command": `C:\x\ok-hook-prompter.cmd`}, false},
		{"相似前缀 my- 不误判", map[string]any{"type": "command", "command": `C:\x\my-ok-hook-prompt.cmd`}, false},
		{"后缀 .bak 不误判", map[string]any{"type": "command", "command": `C:\x\ok-hook-prompt.cmd.bak`}, false},
		{"裸文件名无分隔不命中", map[string]any{"type": "command", "command": `ok-hook-prompt.cmd`}, false},
		{"quoted 形态 prompt", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook prompt claude`}, true},
		{"quoted 形态 post-tool", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook post-tool claude`}, true},
		{"quoted 形态 stop", map[string]any{"type": "command", "command": `"D:/x/ok.exe" hook stop claude`}, true},
	}
	for _, c := range cases {
		if got := isOKQoderIdeHook(c.hook); got != c.want {
			t.Errorf("%s: isOKQoderIdeHook() = %v, want %v", c.name, got, c.want)
		}
	}
}

// qoderIdeWantWrappers 断言 home 下三个包装文件存在且内容为 exe 的期望形态。
func qoderIdeWantWrappers(t *testing.T, home, exe string) {
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

func TestQoderIdeDetect(t *testing.T) {
	home := isolateQoderIde(t)
	a := qoderIdeAgent{}
	if a.Detect() {
		t.Error("目录不存在时不应 Detect")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.Detect() {
		t.Error("~/.lingma 存在应 Detect")
	}
}

func TestQoderIdeRegistered(t *testing.T) {
	isolateQoderIde(t)
	a, ok := Find("qoder-ide")
	if !ok {
		t.Fatal("qoderIdeAgent 未注册（init/Register 缺失）")
	}
	if a.ID() != "qoder-ide" {
		t.Errorf("ID() = %q, want qoder-ide", a.ID())
	}
	if a.DisplayName() != "Qoder CN IDE（灵码内核）" {
		t.Errorf("DisplayName() = %q, want Qoder CN IDE（灵码内核）", a.DisplayName())
	}
}

func TestQoderIdeInstallHooks(t *testing.T) {
	home := isolateQoderIde(t)
	sp := filepath.Join(home, "settings.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := `{"note":"keep","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	if err := os.WriteFile(sp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := qoderIdeAgent{}
	if err := a.InstallHooks(qoderIdeTestExe()); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	data, err := os.ReadFile(sp)
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
	// 其他平台 = quoted shell 串（经 lingmaCommand 生成，与生产形态一致）。
	var wantCmd map[string]string
	if runtime.GOOS == "windows" {
		wantCmd = map[string]string{
			"UserPromptSubmit": filepath.Join(home, "ok-hook-prompt.cmd"),
			"PostToolUse":      filepath.Join(home, "ok-hook-post-tool.cmd"),
			"Stop":             filepath.Join(home, "ok-hook-stop.cmd"),
		}
	} else {
		wantCmd = map[string]string{
			"UserPromptSubmit": lingmaCommand(qoderIdeTestExe(), "prompt"),
			"PostToolUse":      lingmaCommand(qoderIdeTestExe(), "post-tool"),
			"Stop":             lingmaCommand(qoderIdeTestExe(), "stop"),
		}
	}
	wantMatcher := map[string]string{"UserPromptSubmit": "*", "PostToolUse": "Write|Edit", "Stop": "*"}
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
	// IDE 无 enabled 门——不应写入 hooksConfig 等多余顶层键（只动 hooks）。
	if _, has := cfg["hooksConfig"]; has {
		t.Error("IDE 不应写入 hooksConfig 键")
	}
	// 包装文件：3 个，内容为 @"<exe>" hook <okHook> claude（CRLF 结尾）——仅 Windows
	if runtime.GOOS == "windows" {
		qoderIdeWantWrappers(t, home, qoderIdeTestExe())
	}
	if _, err := os.Stat(sp + ".bak-openknowledge"); err != nil {
		t.Error("未生成 .bak-openknowledge 备份")
	}
}

func TestQoderIdeInstallIdempotent(t *testing.T) {
	home := isolateQoderIde(t)
	a := qoderIdeAgent{}
	if err := a.InstallHooks(qoderIdeTestExe()); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(qoderIdeTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, "settings.json"))
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

func TestQoderIdeCorruptSettings(t *testing.T) {
	home := isolateQoderIde(t)
	sp := filepath.Join(home, "settings.json")
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(sp, []byte("{broken"), 0o644)
	a := qoderIdeAgent{}
	if err := a.InstallHooks(qoderIdeTestExe()); err == nil {
		t.Fatal("损坏文件应报错")
	}
	data, _ := os.ReadFile(sp)
	if string(data) != "{broken" {
		t.Error("损坏文件被覆盖")
	}
}

func TestQoderIdeRemoveHooks(t *testing.T) {
	home := isolateQoderIde(t)
	sp := filepath.Join(home, "settings.json")
	_ = os.MkdirAll(home, 0o755)
	preset := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	_ = os.WriteFile(sp, []byte(preset), 0o644)
	a := qoderIdeAgent{}
	if err := a.InstallHooks(qoderIdeTestExe()); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = (%v, %v), want (true, nil)", removed, err)
	}
	data, _ := os.ReadFile(sp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	events, _ := cfg["hooks"].(map[string]any)
	if hasOKQoderIdeHook(events) {
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

func TestQoderIdeEnsureHooks(t *testing.T) {
	home := isolateQoderIde(t)
	sp := filepath.Join(home, "settings.json")
	a := qoderIdeAgent{}
	// 从未安装（文件不存在）→ no-op，不创建文件
	if err := a.EnsureHooks(qoderIdeTestExe()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sp); !os.IsNotExist(err) {
		t.Error("从未安装时 EnsureHooks 不应创建文件")
	}
	// 安装后把 exe 改旧 → EnsureHooks 重写为新 exe（currentExe(t) 模式）。
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Error("自愈后 HooksInstalled 应为 true")
	}
	data, _ := os.ReadFile(sp)
	if strings.Contains(string(data), `D:\old`) || strings.Contains(string(data), `D:/old`) {
		t.Error("旧 exe 路径残留")
	}
	// 包装内容刷新为新 exe（仅 Windows 有包装形态）
	if runtime.GOOS == "windows" {
		qoderIdeWantWrappers(t, home, currentExe(t))
	}
	// 用户显式移除 → 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(qoderIdeTestExe()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(sp)
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if hasOKQoderIdeHook(lingmaEventsOf(cfg)) {
		t.Error("用户显式移除的集成被复活")
	}
}

// TestQoderIdeExeMigrationKeepsSettings exe 迁移后自愈只重写包装文件内容，
// settings.json（命令=包装路径）逐字节不动。
func TestQoderIdeExeMigrationKeepsSettings(t *testing.T) {
	home := isolateQoderIde(t)
	a := qoderIdeAgent{}
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(lingmaSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	newExe := currentExe(t)
	if err := a.EnsureHooks(newExe); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(lingmaSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// Windows：包装内容刷新为新 exe；settings.json（命令=包装路径）逐字节不动。
		qoderIdeWantWrappers(t, home, newExe)
		if string(after) != string(before) {
			t.Error("exe 迁移不应触发 settings.json 重写（命令=包装路径，与 exe 无关）")
		}
	} else {
		// 其他平台（quoted 形态）：exe 直接进命令串，迁移必然重写 settings.json。
		if string(after) == string(before) {
			t.Error("quoted 形态下 exe 迁移应重写 settings.json")
		}
		if strings.Contains(string(after), `D:\old`) || strings.Contains(string(after), `D:/old`) {
			t.Error("旧 exe 路径残留")
		}
	}
	if !a.HooksInstalled() {
		t.Error("exe 迁移自愈后 HooksInstalled 应为 true")
	}
}

func TestQoderIdeHooksInstalled(t *testing.T) {
	isolateQoderIde(t)
	a := qoderIdeAgent{}
	if a.HooksInstalled() {
		t.Error("未安装时不应为 true")
	}
	if err := a.InstallHooks(qoderIdeTestExe()); err != nil {
		t.Fatal(err)
	}
	// HooksInstalled 用 os.Executable() 比对——测试二进制路径与安装路径不同，应为 false；
	// 用安装时的同一路径判定逻辑直接测 lingmaHooksCurrent。
	data, _ := os.ReadFile(lingmaSettingsPath())
	var cfg map[string]any
	_ = json.Unmarshal(data, &cfg)
	if !lingmaHooksCurrent(lingmaEventsOf(cfg), qoderIdeTestExe()) {
		t.Error("安装后 lingmaHooksCurrent(安装 exe) 应为 true")
	}
	if lingmaHooksCurrent(lingmaEventsOf(cfg), `D:\other\ok.exe`) {
		t.Error("换 exe 后 lingmaHooksCurrent 应为 false")
	}
}
