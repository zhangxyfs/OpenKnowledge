package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupReasonixHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_REASONIX_HOME", home)
	return home
}

const testExe = `D:\tools\ok.exe`

func TestReasonixInstallAndInstalled(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if !a.Detect() {
		t.Fatal("home 存在应 Detect")
	}
	if a.HooksInstalled() {
		t.Fatal("未安装前 HooksInstalled 应为 false")
	}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	// manifest 关键字段
	data, err := os.ReadFile(filepath.Join(home, "plugins", "openknowledge", "reasonix-plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mf map[string]any
	if err := json.Unmarshal(data, &mf); err != nil {
		t.Fatal(err)
	}
	rt, _ := mf["runtime"].(map[string]any)
	if rt["command"] != testExe {
		t.Errorf("runtime.command 错误: %v", rt["command"])
	}
	if rt["required"] != false {
		t.Errorf("required 必须为 false（宿主降级语义）: %v", rt["required"])
	}
	// state 登记
	st, err := os.ReadFile(filepath.Join(home, "plugin-packages.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(st), `"openknowledge"`) {
		t.Errorf("plugin-packages.json 未登记: %s", st)
	}
	// HooksInstalled 以当前进程 exe 为判定基准（zcode 模式）：
	// 换当前 exe 重装后应为 true。
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Error("安装后 HooksInstalled 应为 true")
	}
	// 幂等重装
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
}

func TestReasonixRemove(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if removed, _ := a.RemoveHooks(); removed {
		t.Error("未安装时 RemoveHooks 应返回 false")
	}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks: %v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(home, "plugins", "openknowledge")); !os.IsNotExist(err) {
		t.Error("插件目录应删除")
	}
	st, _ := os.ReadFile(filepath.Join(home, "plugin-packages.json"))
	if strings.Contains(string(st), `"openknowledge"`) {
		t.Error("state 条目应移除")
	}
	if a.HooksInstalled() {
		t.Error("移除后 HooksInstalled 应为 false")
	}
}

func TestReasonixEnsureRewritesStaleAndKeepsRemoved(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	// 从未安装：Ensure 不复活
	if err := a.EnsureHooks(testExe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "plugin-packages.json")); !os.IsNotExist(err) {
		t.Error("从未安装时 Ensure 不得创建 state")
	}
	// 安装后 exe 迁移：Ensure 重写
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	newExe := `D:\moved\ok.exe`
	if err := a.EnsureHooks(newExe); err != nil {
		t.Fatal(err)
	}
	// JSON 字符串内反斜杠必被转义，不能对原始文本做子串匹配；反序列化后比对。
	data, _ := os.ReadFile(filepath.Join(home, "plugins", "openknowledge", "reasonix-plugin.json"))
	var mf2 map[string]any
	if err := json.Unmarshal(data, &mf2); err != nil {
		t.Fatal(err)
	}
	rt2, _ := mf2["runtime"].(map[string]any)
	if rt2["command"] != newExe {
		t.Error("exe 迁移后 manifest 应重写")
	}
	// 用户手动删 state 条目：Ensure 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(newExe); err != nil {
		t.Fatal(err)
	}
	st, _ := os.ReadFile(filepath.Join(home, "plugin-packages.json"))
	if strings.Contains(string(st), `"openknowledge"`) {
		t.Error("用户显式移除后 Ensure 不得复活")
	}
}

func TestReasonixCorruptStateNotOverwritten(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(home, "plugin-packages.json")
	if err := os.WriteFile(sp, []byte("{损坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(testExe); err == nil {
		t.Fatal("损坏 state 应报错")
	}
	data, _ := os.ReadFile(sp)
	if string(data) != "{损坏" {
		t.Error("损坏文件不得被覆盖")
	}
}

func TestReasonixPreservesForeignPlugins(t *testing.T) {
	home := setupReasonixHome(t)
	a := reasonixAgent{}
	if err := a.InstallHooks(testExe); err != nil {
		t.Fatal(err)
	}
	// 注入一个第三方插件条目
	sp := filepath.Join(home, "plugin-packages.json")
	data, _ := os.ReadFile(sp)
	var st map[string]any
	_ = json.Unmarshal(data, &st)
	plugins, _ := st["plugins"].([]any)
	st["plugins"] = append(plugins, map[string]any{"name": "other-plugin", "root": `D:\x`, "enabled": true})
	out, _ := json.Marshal(st)
	if err := os.WriteFile(sp, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(sp)
	if !strings.Contains(string(after), "other-plugin") {
		t.Error("第三方插件条目必须保留")
	}
}
