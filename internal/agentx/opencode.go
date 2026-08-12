package agentx

import (
	"os"
	"path/filepath"
)

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
