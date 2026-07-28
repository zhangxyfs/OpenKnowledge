//go:build windows

// Package procx 处理子进程的平台差异。
package procx

import (
	"os/exec"
	"syscall"
)

// createNoWindow 防止 GUI 子系统进程派生控制台子进程时弹出黑窗口。
const createNoWindow = 0x08000000

// HideWindow 抑制子进程的控制台窗口：ok.exe 以 windowsgui 子系统构建，
// 派生 git/powershell/cmd 等控制台程序时系统会为其分配新的控制台窗口
// （hook 每次调 git 都闪黑窗口的根因），CREATE_NO_WINDOW 让其静默运行。
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
