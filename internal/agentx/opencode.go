package agentx

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openknowledge/internal/fsx"
)

//go:embed opencode_plugin.ts
var opencodePluginTemplate string

// opencodePluginMarker 本工具生成的插件文件头标记（RemoveHooks 据此识别归属）。
const opencodePluginMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"

// OpencodeHome 返回 opencode 全局配置目录。解析序：OK_OPENCODE_HOME（ok 自留
// 测试隔离口，OK_ZCODE_HOME 同款）> OPENCODE_CONFIG_DIR（opencode 官方覆盖，
// 见 opencode packages/core/src/global.ts 的 Flag.OPENCODE_CONFIG_DIR）>
// XDG_CONFIG_HOME/opencode > ~/.config/opencode（xdg-basedir 拼法，win32 无特判）。
func OpencodeHome() string {
	if h := os.Getenv("OK_OPENCODE_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("OPENCODE_CONFIG_DIR"); h != "" {
		return h
	}
	if h := os.Getenv("XDG_CONFIG_HOME"); h != "" {
		return filepath.Join(h, "opencode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "opencode")
}

// opencodePluginPath 插件写入目标：<配置根>/plugins/openknowledge.ts。
// opencode 对每个配置目录 glob {plugin,plugins}/*.{ts,js} 并直接 import 单文件
// （packages/opencode/src/config/plugin.ts:21-29），免 package.json/tsconfig。
func opencodePluginPath() string { return filepath.Join(OpencodeHome(), "plugins", "openknowledge.ts") }

// opencodeTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func opencodeTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(opencodePluginTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderOpencodePlugin 渲染插件：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderOpencodePlugin(exe string) string {
	body := strings.ReplaceAll(opencodePluginTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return opencodePluginMarker + "\n// fingerprint: " + opencodeTemplateFingerprint() + "\n" + body
}

// opencodeAgent opencode 适配器：hook 集成 = 写 TS 插件到 <配置根>/plugins/
// （opencode 无 hooks 配置字段，hooks 形态为插件返回 hooks 对象）；
// 技能共享 SkillsHome（opencode 原生扫描 ~/.agents/skills）。
type opencodeAgent struct{}

func init() { Register(opencodeAgent{}) }

func (opencodeAgent) ID() string          { return "opencode" }
func (opencodeAgent) DisplayName() string { return "opencode" }
func (opencodeAgent) SkillsDir() string   { return SkillsHome() }
func (opencodeAgent) HooksTarget() string { return opencodePluginPath() }

func (opencodeAgent) Detect() bool {
	info, err := os.Stat(OpencodeHome())
	return err == nil && info.IsDir()
}

func (opencodeAgent) HooksInstalled() bool {
	data, err := os.ReadFile(opencodePluginPath())
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, opencodePluginMarker) &&
		strings.Contains(content, "// fingerprint: "+opencodeTemplateFingerprint())
}

func (opencodeAgent) InstallHooks(exe string) error {
	path := opencodePluginPath()
	if data, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(data), opencodePluginMarker) {
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
	return fsx.WriteFile(path, []byte(renderOpencodePlugin(exe)), 0o644)
}

func (opencodeAgent) RemoveHooks() (bool, error) {
	path := opencodePluginPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(data), opencodePluginMarker) {
		return false, nil // 非本工具生成，不删
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("删除 opencode 插件: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：文件存在且为本工具生成、但内容与当前渲染结果不同（模板升级
// 或 exe 迁移）时重写；文件不存在为 no-op（opencode 无插件即不会触发 hook；
// 用户显式移除不复活）。
func (opencodeAgent) EnsureHooks(exe string) error {
	path := opencodePluginPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if !strings.Contains(string(data), opencodePluginMarker) {
		return nil
	}
	rendered := renderOpencodePlugin(exe)
	if string(data) == rendered {
		return nil
	}
	return fsx.WriteFile(path, []byte(rendered), 0o644)
}
