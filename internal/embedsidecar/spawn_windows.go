//go:build windows

package embedsidecar

import (
	"os/exec"
	"syscall"
)

// hideWindow 阻止 llama-server 弹出控制台窗口（与 daemon 子进程同策略）。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
