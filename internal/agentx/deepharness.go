package agentx

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed dsh_plugin.js
var dshPluginTemplate string

// dshPluginMarker 本工具生成的插件文件头标记（RemoveHooks 据此识别归属）。
const dshPluginMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"

// DSHHome 返回 DeepSeek Harness 家目录。解析序：OK_DSH_HOME（ok 自留测试隔离口，
// OK_ZCODE_HOME 同款）> DSH_HOME（官方重定位变量，packages/util/home-paths 的
// resolveDshHome）> ~/.dsh。
func DSHHome() string {
	if h := os.Getenv("OK_DSH_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("DSH_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dsh")
}

// dshPluginPath 插件写入目标：<home>/plugins/openknowledge/index.js。
// DSH 无插件目录自动扫描，位置为 ok 自选，经 cordis.patch.yml 绝对路径挂载。
func dshPluginPath() string { return filepath.Join(DSHHome(), "plugins", "openknowledge", "index.js") }

// dshPatchPath 家目录级 patch 文件：<home>/cordis.patch.yml（所有 profile 共享，
// DSH 文档明示的家目录级 patch 层）。
func dshPatchPath() string { return filepath.Join(DSHHome(), "cordis.patch.yml") }

// dshTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func dshTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(dshPluginTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderDSHPlugin 渲染插件：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderDSHPlugin(exe string) string {
	body := strings.ReplaceAll(dshPluginTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return dshPluginMarker + "\n// fingerprint: " + dshTemplateFingerprint() + "\n" + body
}

// dshPatchBlock 家目录 patch 行：绝对路径挂载本地插件（cordis patch 的 name 字段
// 接受绝对路径；YAML 单引号字符串 + 正斜杠，规避 Windows 反斜杠转义）。
func dshPatchBlock() string {
	return "- insert:\n    - id: ok-hooks\n      name: '" + filepath.ToSlash(dshPluginPath()) + "'\n"
}

// dshAgent DeepSeek Harness 适配器：hook 集成 = 本地 JS 插件 + 家目录 patch 行挂载；
// 技能共享 SkillsHome（DSH 原生扫描 ~/.agents/skills）。
type dshAgent struct{}

func init() { Register(dshAgent{}) }

func (dshAgent) ID() string          { return "dsh" }
func (dshAgent) DisplayName() string { return "DeepSeek Harness" }
func (dshAgent) SkillsDir() string   { return SkillsHome() }
func (dshAgent) HooksTarget() string { return dshPluginPath() }

func (dshAgent) Detect() bool {
	info, err := os.Stat(DSHHome())
	return err == nil && info.IsDir()
}

func (dshAgent) HooksInstalled() bool          { return false }            // Task 3 实现
func (dshAgent) InstallHooks(exe string) error { _ = exe; return nil }     // Task 2/3 实现
func (dshAgent) RemoveHooks() (bool, error)    { return false, nil }       // Task 3 实现
func (dshAgent) EnsureHooks(exe string) error  { _ = exe; return nil }     // Task 3 实现
