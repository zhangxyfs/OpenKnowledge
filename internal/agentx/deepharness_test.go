package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDSH 隔离 DSH 家目录与 OK_HOME（HookTimeoutSec 读全局配置），返回 DSHHome。
func setupDSH(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_DSH_HOME", home)
	t.Setenv("OK_HOME", t.TempDir())
	return home
}

func TestDSHHome(t *testing.T) {
	t.Setenv("OK_DSH_HOME", "")
	t.Setenv("DSH_HOME", `D:\dsh-official`)
	if got := DSHHome(); got != `D:\dsh-official` {
		t.Fatalf("DSH_HOME 优先于默认目录: %q", got)
	}
	t.Setenv("OK_DSH_HOME", `D:\dsh-ok`)
	t.Setenv("DSH_HOME", `D:\dsh-official`)
	if got := DSHHome(); got != `D:\dsh-ok` {
		t.Fatalf("OK_DSH_HOME 应最优先: %q", got)
	}
}

func TestDSHDetect(t *testing.T) {
	setupDSH(t)
	if !(dshAgent{}).Detect() {
		t.Fatal("DSHHome 存在应检测为真")
	}
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (dshAgent{}).Detect() {
		t.Fatal("DSHHome 不存在应检测为假")
	}
}

func TestDSHSkillsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	setupDSH(t)
	if got := (dshAgent{}).SkillsDir(); got != dir {
		t.Fatalf("SkillsDir 应为共享 SkillsHome: %q", got)
	}
}

func TestDSHRegistered(t *testing.T) {
	if _, ok := Find("dsh"); !ok {
		t.Fatal("dsh 适配器应已注册")
	}
}

func TestDSHInstallWritesPlugin(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	if err := (dshAgent{}).InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dshPluginPath())
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, dshPluginMarker) {
		t.Fatal("插件应含头标记")
	}
	if !strings.Contains(content, "// fingerprint: "+dshTemplateFingerprint()) {
		t.Fatal("插件应含当前指纹")
	}
	if !strings.Contains(content, filepath.ToSlash(exe)) {
		t.Fatal("插件应烘焙 exe 正斜杠路径")
	}
	for _, want := range []string{`ctx.on("agent/pre-step"`, `ctx.on("tools/post-execute"`, `ctx.on("agent/turn-stopping"`, `["hook", "prompt"]`, `["hook", "post-tool"]`, `["hook", "stop"]`} {
		if !strings.Contains(content, want) {
			t.Fatalf("插件缺少 %q", want)
		}
	}
}

func TestDSHInstallBacksUpForeignPlugin(t *testing.T) {
	setupDSH(t)
	if err := os.MkdirAll(filepath.Dir(dshPluginPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// someone else's plugin\n"
	if err := os.WriteFile(dshPluginPath(), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(dshPluginPath() + ".bak-openknowledge")
	if err != nil || string(bak) != foreign {
		t.Fatalf("应备份既有非自家插件: %v %q", err, bak)
	}
}

func readDSHPatch(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(dshPatchPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDSHInstallWritesPatchLine(t *testing.T) {
	setupDSH(t)
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if !strings.Contains(patch, MarkerBegin) || !strings.Contains(patch, MarkerEnd) {
		t.Fatalf("patch 应含标记块: %q", patch)
	}
	if !strings.Contains(patch, "id: ok-hooks") {
		t.Fatalf("patch 应含 ok-hooks 行: %q", patch)
	}
	if !strings.Contains(patch, "name: '"+filepath.ToSlash(dshPluginPath())+"'") {
		t.Fatalf("patch 应含插件绝对路径（正斜杠单引号）: %q", patch)
	}
	if !(dshAgent{}).HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为真")
	}
}

func TestDSHInstallIdempotent(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	a := dshAgent{}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if n := strings.Count(patch, "id: ok-hooks"); n != 1 {
		t.Fatalf("重复安装后应有 1 条 ok-hooks 行, got %d", n)
	}
}

func TestDSHInstallPreservesForeignPatch(t *testing.T) {
	setupDSH(t)
	pre := "# 用户自己的 patch\n- insert:\n    - id: my-plugin\n      name: 'D:/x/my.js'\n"
	if err := os.WriteFile(dshPatchPath(), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if !strings.Contains(patch, "id: my-plugin") || !strings.Contains(patch, "# 用户自己的 patch") {
		t.Fatalf("第三方行与注释应保留: %q", patch)
	}
	if !strings.Contains(patch, "id: ok-hooks") {
		t.Fatalf("ok 行应追加: %q", patch)
	}
	if _, err := os.Stat(dshPatchPath() + ".bak-openknowledge"); err != nil {
		t.Fatal("应生成 .bak-openknowledge 备份")
	}
}

func TestDSHRemoveHooks(t *testing.T) {
	setupDSH(t)
	a := dshAgent{}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("无配置 RemoveHooks = %v, %v", removed, err)
	}
	pre := "- insert:\n    - id: my-plugin\n      name: 'D:/x/my.js'\n"
	if err := os.WriteFile(dshPatchPath(), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("插件文件应被删除")
	}
	patch := readDSHPatch(t)
	if strings.Contains(patch, "ok-hooks") {
		t.Fatalf("patch 不应残留 ok 行: %q", patch)
	}
	if !strings.Contains(patch, "id: my-plugin") {
		t.Fatalf("第三方行应保留: %q", patch)
	}
	if a.HooksInstalled() {
		t.Fatal("移除后 HooksInstalled 应为假")
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("重复 RemoveHooks = %v, %v", removed, err)
	}
}

func TestDSHRemoveKeepsForeignPlugin(t *testing.T) {
	setupDSH(t)
	if err := os.MkdirAll(filepath.Dir(dshPluginPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// someone else's plugin\n"
	if err := os.WriteFile(dshPluginPath(), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := (dshAgent{}).RemoveHooks()
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dshPluginPath()); string(data) != foreign {
		t.Fatal("非自家插件不应被删")
	}
	_ = removed
}

func TestDSHEnsureHooks(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	a := dshAgent{}

	// 从未安装 → no-op（不创建任何文件）
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("从未安装时 EnsureHooks 不应创建插件")
	}
	if _, err := os.Stat(dshPatchPath()); !os.IsNotExist(err) {
		t.Fatal("从未安装时 EnsureHooks 不应创建 patch")
	}

	// 过期（旧 exe 路径）→ 重写为当前
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

	// 当前 → no-op
	beforePlugin, _ := os.ReadFile(dshPluginPath())
	beforePatch, _ := os.ReadFile(dshPatchPath())
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	afterPlugin, _ := os.ReadFile(dshPluginPath())
	afterPatch, _ := os.ReadFile(dshPatchPath())
	if string(beforePlugin) != string(afterPlugin) || string(beforePatch) != string(afterPatch) {
		t.Fatal("内容当前时 EnsureHooks 应 no-op")
	}

	// 显式移除 → 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("显式移除后 EnsureHooks 不应复活插件")
	}
}
