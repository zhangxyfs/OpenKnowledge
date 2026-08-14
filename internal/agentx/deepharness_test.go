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
