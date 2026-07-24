//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// x/sys/windows 未封装 AttachConsole，经 kernel32 直接调用。
var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

const attachParentProcess = 0xFFFFFFFF

// attachForCLI GUI 子系统编译时，CLI 模式挂回父控制台以恢复可见输出。
// stdout 已有效（管道/重定向/已有控制台）时跳过——hook 子进程的输出走管道，
// 绝不能被重定向到 CONOUT$。
func attachForCLI() {
	if _, err := os.Stdout.Stat(); err == nil {
		return
	}
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r == 0 {
		return
	}
	con, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return
	}
	os.Stdout = con
	os.Stderr = con
}
