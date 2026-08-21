package main

import (
	"os"

	"openknowledge/internal/daemon"
)

// OkManager：OpenKnowledge 配置中心入口，薄启动器——确保 okd 在线后以 --app= 模式
// 打开配置中心窗口并退出（不驻留；驻留是 okd 的事）。不含任何业务逻辑，
// 全部功能走 okd 的 HTTP API。见 docs/2026-08-21-gui-split-design.md §4.3。
func main() { os.Exit(daemon.OpenGUI(os.Stdout, os.Stderr)) }
