package main

import (
	"fmt"
	"os"
	"path/filepath"

	"openknowledge/internal/cli"
	"openknowledge/internal/gui"
	"openknowledge/internal/hook"
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
	case "setup":
		return cli.Setup(argv[2:], os.Stdin, os.Stdout, os.Stderr)
	case "init":
		return cli.Init(argv[2:], os.Stdout, os.Stderr)
	case "add":
		return cli.Add(argv[2:], os.Stdout, os.Stderr)
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
func runHook(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = 0
		}
	}()
	if len(args) < 1 {
		return 0
	}
	switch args[0] {
	case "prompt":
		return hook.HandlePrompt(os.Stdin, os.Stdout)
	case "post-tool":
		return hook.HandlePostTool(os.Stdin)
	case "stop":
		return hook.HandleStop(os.Stdin, os.Stderr)
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: ok [gui] <setup|init|add|search|index|list|doctor|on|off|hook> ...")
}

// runGUI 定位 web 资源目录并启动 Web GUI。
func runGUI() int {
	webDir, err := findWebDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return gui.Run(webDir, os.Stdout, os.Stderr)
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
