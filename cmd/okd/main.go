package main

import (
	"fmt"
	"os"
	"path/filepath"

	"openknowledge/internal/daemon"
	"openknowledge/internal/logx"
)

func main() { os.Exit(run(os.Args)) }

// okd：OpenKnowledge 常驻 daemon——管理 API + 托盘 + sidecar 托管 + Web 静态页分发，
// 见 docs/2026-08-21-gui-split-design.md。无参数 → 常驻服务；stop → 经 API 停服。
// 后台拉起时 stdio 即 daemon.log：按行加时间戳，排查"何时发生"不再靠猜。
func run(argv []string) int {
	if len(argv) > 1 && argv[1] == "stop" {
		return daemon.Stop(os.Stdout, os.Stderr)
	}
	webDir, _ := findWebDir() // 找不到 web 目录也能跑（仅无 GUI 静态页）
	return daemon.Run(webDir, logx.New(os.Stdout), logx.New(os.Stderr))
}

// findWebDir 依次尝试 <exe目录>/web 与 <当前目录>/web。（与 cmd/ok/main.go 同源：
// 两个 main 包无法共享未导出函数，保留一份拷贝，改动需同步）
func findWebDir() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if dir := filepath.Join(filepath.Dir(exe), "web"); isDir(dir) {
			return dir, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir := filepath.Join(cwd, "web"); isDir(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("未找到 web 资源目录（<exe目录>/web 或 <当前目录>/web）")
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
