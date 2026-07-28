//go:build !windows

package tray

import "context"

// Run 非 Windows 平台无托盘，阻塞至 ctx 取消。
func Run(ctx context.Context, version string, openGUI func() uintptr, onQuit func()) {
	<-ctx.Done()
}
