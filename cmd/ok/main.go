package main

import (
	"fmt"
	"os"

	"openknowledge/internal/cli"
	"openknowledge/internal/hook"
)

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	if len(argv) < 2 {
		usage()
		return 1
	}
	switch argv[1] {
	case "hook":
		return runHook(argv[2:])
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
	case "session-start":
		return hook.HandleSessionStart(os.Stdin, os.Stdout)
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
	fmt.Fprintln(os.Stderr, "用法: ok <init|add|search|index|list|doctor|hook> ...")
}
