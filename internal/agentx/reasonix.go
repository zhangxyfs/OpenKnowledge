package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"openknowledge/internal/fsx"
	"openknowledge/internal/version"
)

// ReasonixHome 返回 Reasonix 配置根目录：OK_REASONIX_HOME（测试口）>
// REASONIX_HOME > Windows %APPDATA%\reasonix（回退 ~/AppData/Roaming/reasonix）/
// 其他 ~/.reasonix——与 Reasonix 源码 internal/config/paths.go reasonixHomeDir 对应。
func ReasonixHome() string {
	if h := os.Getenv("OK_REASONIX_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("REASONIX_HOME"); h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil && dir != "" {
			return filepath.Join(dir, "reasonix")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Roaming", "reasonix")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".reasonix")
}

func reasonixPluginDir() string { return filepath.Join(ReasonixHome(), "plugins", "openknowledge") }
func reasonixStatePath() string { return filepath.Join(ReasonixHome(), "plugin-packages.json") }
func reasonixManifestPath() string {
	return filepath.Join(reasonixPluginDir(), "reasonix-plugin.json")
}

// reasonixAgent Reasonix 适配器：集成 = 安装 Extension Protocol 插件包
// （plugins/openknowledge/ + plugin-packages.json 信任门登记），
// 技能目录用共享 SkillsHome（Reasonix 全局扫描 ~/.agents/skills）。
type reasonixAgent struct{}

func init() { Register(reasonixAgent{}) }

func (reasonixAgent) ID() string          { return "reasonix" }
func (reasonixAgent) DisplayName() string { return "Reasonix" }
func (reasonixAgent) HooksTarget() string { return reasonixPluginDir() }
func (reasonixAgent) SkillsDir() string   { return SkillsHome() }

func (reasonixAgent) Detect() bool {
	info, err := os.Stat(ReasonixHome())
	return err == nil && info.IsDir()
}

// reasonixManifest 生成 manifest v1：runtime.command 直指 ok.exe（协议允许
// 插件根外绝对路径，exec form）；required=false——sidecar 崩溃宿主降级不阻断。
func reasonixManifest(exe string) map[string]any {
	return map[string]any{
		"apiVersion":  "reasonix.io/plugin/v1",
		"name":        "openknowledge",
		"version":     version.Version,
		"description": "OpenKnowledge 知识库 sidecar：逐 prompt 检索注入与经验沉淀",
		"contributes": map[string]any{},
		"runtime": map[string]any{
			"command":       exe,
			"args":          []any{"extension-serve"},
			"required":      false,
			"priority":      0,
			"timeoutMillis": HookTimeoutSec() * 1000,
			"intercepts":    []any{"input.receive", "tool.after"},
			"capabilities":  []any{"interceptors"},
		},
	}
}

// loadReasonixState 读 plugin-packages.json；不存在返回空 State，解析失败报错（不覆盖）。
func loadReasonixState() (map[string]any, error) {
	data, err := os.ReadFile(reasonixStatePath())
	if os.IsNotExist(err) {
		return map[string]any{"version": float64(1), "plugins": []any{}}, nil
	}
	if err != nil {
		return nil, err
	}
	st := map[string]any{}
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("reasonix plugin-packages.json 解析失败: %w", err)
	}
	return st, nil
}

// writeReasonixState 备份后原子写（temp+rename）。
func writeReasonixState(st map[string]any) error {
	path := reasonixStatePath()
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak-openknowledge", data, 0o644)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp-openknowledge"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// reasonixStatePlugins 取 plugins 数组（只读视图）。
func reasonixStatePlugins(st map[string]any) []any {
	plugins, _ := st["plugins"].([]any)
	return plugins
}

// findOKReasonixEntry 返回 ok 条目下标（无则 -1）：按 name=="openknowledge" 识别。
func findOKReasonixEntry(plugins []any) int {
	for i, p := range plugins {
		if pm, _ := p.(map[string]any); pm != nil && pm["name"] == "openknowledge" {
			return i
		}
	}
	return -1
}

// upsertOKReasonixEntry 追加或更新 ok 条目。
func upsertOKReasonixEntry(st map[string]any) {
	plugins := reasonixStatePlugins(st)
	entry := map[string]any{
		"name":         "openknowledge",
		"root":         reasonixPluginDir(),
		"version":      version.Version,
		"description":  "OpenKnowledge 知识库 sidecar",
		"manifestKind": "reasonix.io/plugin/v1",
		"enabled":      true,
	}
	if i := findOKReasonixEntry(plugins); i >= 0 {
		plugins[i] = entry
	} else {
		plugins = append(plugins, entry)
	}
	st["plugins"] = plugins
	if _, ok := st["version"]; !ok {
		st["version"] = float64(1)
	}
}

// removeOKReasonixEntry 移除 ok 条目，返回是否有改动。
func removeOKReasonixEntry(st map[string]any) bool {
	plugins := reasonixStatePlugins(st)
	i := findOKReasonixEntry(plugins)
	if i < 0 {
		return false
	}
	st["plugins"] = append(plugins[:i], plugins[i+1:]...)
	return true
}

// writeReasonixManifest 写插件 manifest（目录随建）。
func writeReasonixManifest(exe string) error {
	data, err := json.MarshalIndent(reasonixManifest(exe), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(reasonixPluginDir(), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(reasonixManifestPath(), append(data, '\n'), 0o644)
}

// reasonixCurrent 报告插件登记与 manifest 是否均为当前期望形态
// （条目 enabled、root 正确；manifest command=exe、args/timeoutMillis 正确）。
func reasonixCurrent(st map[string]any, exe string) bool {
	i := findOKReasonixEntry(reasonixStatePlugins(st))
	if i < 0 {
		return false
	}
	entry, _ := reasonixStatePlugins(st)[i].(map[string]any)
	if enabled, _ := entry["enabled"].(bool); !enabled {
		return false
	}
	if root, _ := entry["root"].(string); root != reasonixPluginDir() {
		return false
	}
	data, err := os.ReadFile(reasonixManifestPath())
	if err != nil {
		return false
	}
	mf := map[string]any{}
	if err := json.Unmarshal(data, &mf); err != nil {
		return false
	}
	rt, _ := mf["runtime"].(map[string]any)
	if rt == nil {
		return false
	}
	cmd, _ := rt["command"].(string)
	timeout, _ := rt["timeoutMillis"].(float64)
	args, _ := rt["args"].([]any)
	return cmd == exe && timeout == float64(HookTimeoutSec()*1000) &&
		len(args) == 1 && args[0] == "extension-serve"
}

func (reasonixAgent) InstallHooks(exe string) error {
	st, err := loadReasonixState()
	if err != nil {
		return err
	}
	if err := writeReasonixManifest(exe); err != nil {
		return err
	}
	upsertOKReasonixEntry(st)
	return writeReasonixState(st)
}

func (reasonixAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(reasonixStatePath()); os.IsNotExist(err) {
		return false, nil
	}
	st, err := loadReasonixState()
	if err != nil {
		return false, err
	}
	changed := removeOKReasonixEntry(st)
	// 插件目录仅当内含 ok manifest 才删（防误删同名外来目录）
	if data, err := os.ReadFile(reasonixManifestPath()); err == nil {
		mf := map[string]any{}
		if json.Unmarshal(data, &mf) == nil && mf["name"] == "openknowledge" {
			if err := os.RemoveAll(reasonixPluginDir()); err == nil {
				changed = true
			}
		}
	}
	if !changed {
		return false, nil
	}
	if err := writeReasonixState(st); err != nil {
		return false, fmt.Errorf("移除 reasonix 插件登记: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：state 存在、曾登记过 ok 插件且内容过期时重写；
// 从未登记（无 ok 条目）则 no-op——用户显式移除的集成不复活。
func (reasonixAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(reasonixStatePath()); err != nil {
		return nil
	}
	st, err := loadReasonixState()
	if err != nil {
		return err
	}
	if findOKReasonixEntry(reasonixStatePlugins(st)) < 0 || reasonixCurrent(st, exe) {
		return nil
	}
	if err := writeReasonixManifest(exe); err != nil {
		return err
	}
	upsertOKReasonixEntry(st)
	return writeReasonixState(st)
}

func (reasonixAgent) HooksInstalled() bool {
	st, err := loadReasonixState()
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return reasonixCurrent(st, exe)
}
