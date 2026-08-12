package registry

import (
	"os"
	"path/filepath"
	"testing"
)

// shadow HOME 免疫：HOME/USERPROFILE 被重定向到临时目录时（CodePilot shadow 模式），
// Home() 仍解析真实用户目录。
func TestHomeImmuneToShadowEnv(t *testing.T) {
	realHome, err := os.UserHomeDir() // 重定向前先取基准
	if err != nil {
		t.Skip("无法获取用户目录")
	}
	shadow := t.TempDir()
	t.Setenv("HOME", shadow)
	t.Setenv("USERPROFILE", shadow)
	t.Setenv("OK_HOME", "") // 确保不生效
	got := Home()
	want := filepath.Join(realHome, ".openknowledge")
	if got != want {
		t.Fatalf("Home() = %q, want %q（跟随了 shadow HOME 重定向）", got, want)
	}
}

// OK_HOME 覆盖仍第一优先（全仓测试隔离依赖）。
func TestHomeOKHomeOverrideStillWins(t *testing.T) {
	okHome := filepath.Join(t.TempDir(), "okhome")
	t.Setenv("OK_HOME", okHome)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	if got := Home(); got != okHome {
		t.Fatalf("Home() = %q, want OK_HOME %q", got, okHome)
	}
}
