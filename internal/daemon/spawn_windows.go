//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// spawnDetached 以 DETACHED_PROCESS 后台启动 daemon 进程，stdio 写入日志文件。
func spawnDetached(exe string, args []string, logPath string) error {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = f, f
	// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP：不继承父控制台，父退出后继续存活
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
