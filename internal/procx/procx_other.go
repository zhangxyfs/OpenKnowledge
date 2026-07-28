//go:build !windows

package procx

import "os/exec"

// HideWindow 非 Windows 平台无控制台窗口问题，空实现。
func HideWindow(cmd *exec.Cmd) {}
