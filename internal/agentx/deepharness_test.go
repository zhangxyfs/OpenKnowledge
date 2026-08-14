package agentx

import (
	"path/filepath"
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
