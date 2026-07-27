package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"openknowledge/internal/cli"
	"openknowledge/internal/daemon"
	"openknowledge/internal/hook"
	"openknowledge/internal/registry"
)

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	if len(argv) < 2 {
		// 无参数（双击 exe 场景）→ 启动 Web GUI
		return runGUI()
	}
	// CLI 模式：GUI 子系统编译时挂回父控制台（管道/重定向场景自动跳过）
	attachForCLI()
	switch argv[1] {
	case "gui":
		return runGUI()
	case "hook":
		return runHook(argv[2:])
	case "daemon":
		if len(argv) > 2 && argv[2] == "stop" {
			return daemon.Stop(os.Stdout, os.Stderr)
		}
		webDir, _ := findWebDir() // 找不到 web 目录也能跑（仅无 GUI 静态页）
		return daemon.Run(webDir, os.Stdout, os.Stderr)
	case "setup":
		return cli.Setup(argv[2:], os.Stdin, os.Stdout, os.Stderr)
	case "init":
		return cli.Init(argv[2:], os.Stdout, os.Stderr)
	case "add":
		return cli.Add(argv[2:], os.Stdout, os.Stderr)
	case "propose":
		return cli.Propose(argv[2:], os.Stdout, os.Stderr)
	case "approve":
		return cli.Approve(argv[2:], os.Stdout, os.Stderr)
	case "capture":
		return cli.CaptureCmd(argv[2:], os.Stdout, os.Stderr)
	case "search":
		return cli.Search(argv[2:], os.Stdout, os.Stderr)
	case "index":
		return cli.Index(argv[2:], os.Stdout, os.Stderr)
	case "list":
		return cli.List(argv[2:], os.Stdout, os.Stderr)
	case "doctor":
		return cli.Doctor(argv[2:], os.Stdout, os.Stderr)
	case "on":
		return cli.On(argv[2:], os.Stdout, os.Stderr)
	case "off":
		return cli.Off(argv[2:], os.Stdout, os.Stderr)
	default:
		usage()
		return 1
	}
}

// runHook hook 路径全面 fail-open：panic 也只放行。
// 优先转发给常驻 daemon；daemon 不在则本地直接处理并后台拉起。
func runHook(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = 0
		}
	}()
	if len(args) < 1 {
		return 0
	}
	if registry.HooksDisabled() {
		return 0
	}
	payload, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if handled, c := daemon.ForwardHook(args[0], payload, os.Stdout, os.Stderr); handled {
		return c
	}
	r := bytes.NewReader(payload)
	switch args[0] {
	case "prompt":
		return hook.HandlePrompt(r, os.Stdout)
	case "post-tool":
		return hook.HandlePostTool(r)
	case "stop":
		return hook.HandleStop(r, os.Stderr)
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: ok [gui] <setup|init|add|propose|approve|capture|search|index|list|doctor|on|off|hook> ...")
}

// runGUI 确保 daemon 在线后打开浏览器并立即返回（进程生命周期由 daemon 托管）。
func runGUI() int {
	return daemon.OpenGUI(os.Stdout, os.Stderr)
}

// findWebDir 依次尝试 <exe目录>/web 与 <当前目录>/web。
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
