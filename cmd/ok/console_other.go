//go:build !windows

package main

// attachForCLI 非 Windows 平台无需处理（始终有控制台语义）。
func attachForCLI() {}
