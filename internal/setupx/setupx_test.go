package setupx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	if err := InstallSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openknowledge-init", "openknowledge-on", "openknowledge-off", "openknowledge-propose", "openknowledge-capture", "openknowledge-wiki"} {
		data, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(string(data), "D:/bin/ok.exe") {
			t.Fatalf("%s missing baked exe path: %q", name, data)
		}
	}
}

func TestInstallWikiSkillContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	if err := InstallSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "openknowledge-wiki", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"name: openknowledge-wiki", "wiki status", "wiki mark", "D:/bin/ok.exe", "--type reference", "wiki"} {
		if !strings.Contains(s, want) {
			t.Fatalf("skill missing %q", want)
		}
	}
	if strings.Contains(s, "{{EXE}}") {
		t.Fatal("exe placeholder not baked")
	}
}

// ReasonixEnforceMode/SaveReasonixEnforceMode：缺省 mixed，保存后可读回，非法值报错。
func TestReasonixEnforceMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	if got := ReasonixEnforceMode(); got != "mixed" {
		t.Errorf("缺省应为 mixed，got %q", got)
	}
	if err := SaveReasonixEnforceMode("soft"); err != nil {
		t.Fatal(err)
	}
	if got := ReasonixEnforceMode(); got != "soft" {
		t.Errorf("保存后应为 soft，got %q", got)
	}
	// 磁盘上确实写入 [reasonix] enforce_mode
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `enforce_mode = "soft"`) {
		t.Errorf("config.toml 应含 enforce_mode = \"soft\"，实际:\n%s", data)
	}
	if err := SaveReasonixEnforceMode("歪值"); err == nil {
		t.Error("非法值应报错")
	}
	// 非法值落盘时读回按 mixed
	if err := SaveReasonixEnforceMode("hard"); err != nil {
		t.Fatal(err)
	}
	if got := ReasonixEnforceMode(); got != "hard" {
		t.Errorf("保存 hard 后应为 hard，got %q", got)
	}
}

// propose 技能模板必须包含"先分类"指引与 wiki 覆盖提示的联动说明。
func TestProposeSkillTemplateHasClassification(t *testing.T) {
	tpl := skillTemplates["openknowledge-propose"]
	for _, want := range []string{"先分类", "结构型", "openknowledge-wiki", "暂无 wiki 条目覆盖"} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("propose skill template missing %q", want)
		}
	}
}
