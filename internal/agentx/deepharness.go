package agentx

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/url"
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

// dshPluginFileURL 插件绝对路径的 file:// URL 形态。实机验证（Task 6）：vendored
// cordis loader 把 patch 的 name 直接交给 Node ESM 解析（vendor/loader/src/config/
// tree.ts 的 import(name)），Windows 绝对路径（D:/...）报
// ERR_UNSUPPORTED_ESM_URL_SCHEME，必须 file:/// URL；POSIX 绝对路径同样适用。
func dshPluginFileURL() string {
	p := filepath.ToSlash(dshPluginPath())
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// dshPatchBlock 家目录 patch 行：file:// URL 挂载本地插件（cordis patch 的 name
// 字段直接进 Node ESM import；YAML 单引号字符串原样保留，规避转义）。
func dshPatchBlock() string {
	return "- insert:\n    - id: ok-hooks\n      name: '" + dshPluginFileURL() + "'\n"
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

func (dshAgent) HooksInstalled() bool {
	data, err := os.ReadFile(dshPluginPath())
	if err != nil {
		return false
	}
	content := string(data)
	if !strings.Contains(content, dshPluginMarker) ||
		!strings.Contains(content, "// fingerprint: "+dshTemplateFingerprint()) {
		return false
	}
	// 旧 exe 路径视为过期（与 zcodeAgent 同款，以解析后的当前可执行文件为基准）
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if content != renderDSHPlugin(exe) {
		return false
	}
	patch, err := os.ReadFile(dshPatchPath())
	return err == nil && strings.Contains(string(patch), "id: ok-hooks")
}

func (dshAgent) InstallHooks(exe string) error {
	// 插件文件（自有新文件整写；既有文件非自家则先备份）
	path := dshPluginPath()
	if data, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(data), dshPluginMarker) {
			if err := os.WriteFile(path+".bak-openknowledge", data, 0o644); err != nil {
				return fmt.Errorf("备份既有插件失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取既有插件失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(renderDSHPlugin(exe)), 0o644); err != nil {
		return err
	}
	// patch 行（标记块幂等 upsert；# 标记在 YAML 是合法注释；UpsertHooksBlock
	// 的 StripLegacyOKHooks 只认 TOML [[hooks]] 表，对 YAML 是安全 no-op）
	patch := dshPatchPath()
	if data, err := os.ReadFile(patch); err == nil {
		_ = os.WriteFile(patch+".bak-openknowledge", data, 0o644)
	}
	return UpsertHooksBlock(patch, dshPatchBlock())
}

// removeDSHMarkerBlock 从 patch 内容移除 ok 标记块，返回 (新内容, 是否移除)。
func removeDSHMarkerBlock(content string) (string, bool) {
	i := strings.Index(content, MarkerBegin)
	j := strings.Index(content, MarkerEnd)
	if i < 0 || j <= i {
		return content, false
	}
	tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
	head := strings.TrimRight(content[:i], "\n")
	out := head
	if strings.TrimSpace(tail) != "" {
		if out != "" {
			out += "\n"
		}
		out += "\n" + tail
	}
	out = strings.TrimLeft(out, "\n")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, true
}

func (dshAgent) RemoveHooks() (bool, error) {
	removed := false
	path := dshPluginPath()
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return false, err
	case strings.Contains(string(data), dshPluginMarker):
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("删除 dsh 插件: %w", err)
		}
		removed = true
	}
	patch := dshPatchPath()
	data, err = os.ReadFile(patch)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return removed, err
	default:
		if out, ok := removeDSHMarkerBlock(string(data)); ok {
			if err := os.WriteFile(patch, []byte(out), 0o644); err != nil {
				return removed, fmt.Errorf("移除 patch 行: %w", err)
			}
			removed = true
		}
	}
	return removed, nil
}

// EnsureHooks 自愈：仅在曾安装（patch 标记块存在或插件文件为自家）且内容过期
// 时整体重写；从未安装 / 经 RemoveHooks 显式移除（两者均不在）不复活。
func (dshAgent) EnsureHooks(exe string) error {
	pluginData, pluginErr := os.ReadFile(dshPluginPath())
	patchData, patchErr := os.ReadFile(dshPatchPath())
	ours := (pluginErr == nil && strings.Contains(string(pluginData), dshPluginMarker)) ||
		(patchErr == nil && strings.Contains(string(patchData), MarkerBegin))
	if !ours {
		return nil
	}
	if pluginErr == nil && string(pluginData) == renderDSHPlugin(exe) &&
		patchErr == nil && strings.Contains(string(patchData), "id: ok-hooks") {
		return nil
	}
	return dshAgent{}.InstallHooks(exe)
}
