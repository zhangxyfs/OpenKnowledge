package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpencodeHomePrecedence(t *testing.T) {
	// OK_OPENCODE_HOME（ok 自留测试口）最优先
	t.Setenv("OK_OPENCODE_HOME", `D:\t\ok-port`)
	t.Setenv("OPENCODE_CONFIG_DIR", `D:\t\official`)
	t.Setenv("XDG_CONFIG_HOME", `D:\t\xdg`)
	if got := OpencodeHome(); got != `D:\t\ok-port` {
		t.Fatalf("OK_OPENCODE_HOME 应最优先, got %q", got)
	}

	// OPENCODE_CONFIG_DIR（opencode 官方覆盖）次之
	t.Setenv("OK_OPENCODE_HOME", "")
	if got := OpencodeHome(); got != `D:\t\official` {
		t.Fatalf("OPENCODE_CONFIG_DIR 次之, got %q", got)
	}

	// XDG_CONFIG_HOME/opencode 再次
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	if got, want := OpencodeHome(), filepath.Join(`D:\t\xdg`, "opencode"); got != want {
		t.Fatalf("XDG_CONFIG_HOME/opencode, got %q want %q", got, want)
	}

	// 默认 ~/.config/opencode
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	if got, want := OpencodeHome(), filepath.Join(home, ".config", "opencode"); got != want {
		t.Fatalf("默认 ~/.config/opencode, got %q want %q", got, want)
	}
}

func TestOpencodePluginPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_OPENCODE_HOME", home)
	want := filepath.Join(home, "plugins", "openknowledge.ts")
	if got := opencodePluginPath(); got != want {
		t.Fatalf("opencodePluginPath = %q, want %q", got, want)
	}
}

// setupOpencode 隔离 opencode 配置根与 OK_HOME，返回 OpencodeHome。
func setupOpencode(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_OPENCODE_HOME", home)
	t.Setenv("OK_HOME", t.TempDir())
	return home
}

func readOpencodePlugin(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(opencodePluginPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOpencodeRegistered(t *testing.T) {
	a, ok := Find("opencode")
	if !ok {
		t.Fatal("opencode 应已注册")
	}
	if a.ID() != "opencode" || a.DisplayName() != "opencode" {
		t.Fatalf("ID/DisplayName = %q/%q", a.ID(), a.DisplayName())
	}
}

func TestOpencodeDetect(t *testing.T) {
	setupOpencode(t)
	if !(opencodeAgent{}).Detect() {
		t.Fatal("配置根存在应检测为真")
	}
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (opencodeAgent{}).Detect() {
		t.Fatal("配置根不存在应检测为假")
	}
}

func TestOpencodeSkillsDir(t *testing.T) {
	skills := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", skills)
	if got := (opencodeAgent{}).SkillsDir(); got != skills {
		t.Fatalf("SkillsDir 应共享 SkillsHome, got %q", got)
	}
}

func TestOpencodeInstallHooks(t *testing.T) {
	setupOpencode(t)
	exe := currentExe(t)
	a := opencodeAgent{}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	content := readOpencodePlugin(t)
	for _, want := range []string{
		opencodePluginMarker,
		"// fingerprint: " + opencodeTemplateFingerprint(),
		"const OK = \"" + filepath.ToSlash(exe) + "\"",
		`"chat.message"`,
		`"tool.execute.after"`,
		`"session.idle"`,
		"promptAsync",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("插件文件应包含 %q", want)
		}
	}
	if a.HooksTarget() != opencodePluginPath() {
		t.Fatalf("HooksTarget = %q", a.HooksTarget())
	}
	if !a.HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为真")
	}
}

func TestOpencodeInstallIdempotent(t *testing.T) {
	setupOpencode(t)
	exe := currentExe(t)
	a := opencodeAgent{}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	first := readOpencodePlugin(t)
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got := readOpencodePlugin(t); got != first {
		t.Fatal("重复安装内容应一致")
	}
}

func TestOpencodeInstallBacksUpForeign(t *testing.T) {
	setupOpencode(t)
	path := opencodePluginPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// user plugin\nexport const X = async () => ({})\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (opencodeAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(path + ".bak-openknowledge")
	if err != nil || string(bak) != foreign {
		t.Fatalf("应备份外部插件文件, err=%v", err)
	}
	if !strings.Contains(readOpencodePlugin(t), opencodePluginMarker) {
		t.Fatal("安装后应为 ok 插件内容")
	}
}

func TestOpencodeRemoveHooks(t *testing.T) {
	setupOpencode(t)
	a := opencodeAgent{}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("无文件 RemoveHooks = %v, %v", removed, err)
	}
	// 外部文件不删不动
	path := opencodePluginPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// user plugin\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("外部文件 RemoveHooks = %v, %v", removed, err)
	}
	if got, _ := os.ReadFile(path); string(got) != foreign {
		t.Fatal("外部文件不应被删除/改动")
	}
	// 本工具生成文件删除
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if removed, err := a.RemoveHooks(); err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("移除后插件文件应不存在")
	}
	if a.HooksInstalled() {
		t.Fatal("移除后 HooksInstalled 应为假")
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("重复 RemoveHooks = %v, %v", removed, err)
	}
}

func TestOpencodeEnsureHooks(t *testing.T) {
	setupOpencode(t)
	exe := currentExe(t)
	a := opencodeAgent{}
	path := opencodePluginPath()

	// 无文件 → no-op（不创建，显式移除不复活）
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("无文件时 EnsureHooks 不应创建")
	}

	// 外部文件 → 不动
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// user plugin\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != foreign {
		t.Fatal("外部文件 EnsureHooks 不应改动")
	}

	// 过期（旧 exe 路径渲染）→ 重写为当前
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got := readOpencodePlugin(t); !strings.Contains(got, "const OK = \""+filepath.ToSlash(exe)+"\"") {
		t.Fatal("自愈后应为当前 exe 渲染内容")
	}

	// 当前 → no-op（文件不再变化）
	before := readOpencodePlugin(t)
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if got := readOpencodePlugin(t); got != before {
		t.Fatal("内容当前时 EnsureHooks 应 no-op")
	}
}

// TestOpencodePluginRuntimePortable 插件必须同时可加载于 Node（opencode 桌面端
// 服务器跑在 Electron/Node 里）与 Bun（CLI/TUI）：禁止 "bun" 模块导入
// （Node 下 Cannot find package 'bun' → 整个插件加载失败，2026-08-12 实报），
// 子进程调用统一走 node:child_process（双运行时兼容）。
func TestOpencodePluginRuntimePortable(t *testing.T) {
	if strings.Contains(opencodePluginTemplate, `from "bun"`) {
		t.Fatal("插件不得导入 bun 模块（桌面端 Node 运行时下不可解析）")
	}
	for _, want := range []string{`"node:child_process"`, "windowsHide"} {
		if !strings.Contains(opencodePluginTemplate, want) {
			t.Fatalf("插件模板应包含 %q", want)
		}
	}
}
