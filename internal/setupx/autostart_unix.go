//go:build !windows

package setupx

import (
	"os"
	"path/filepath"

	"openknowledge/internal/fsx"
)

// autostartPath 返回 XDG 自启文件路径（~/.config/autostart/openknowledge.desktop）。
func autostartPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autostart", "openknowledge.desktop")
}

// WriteAutostart 写入 XDG 登录自启项（幂等覆盖）。
func WriteAutostart(exe string) error {
	p := autostartPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(p, []byte(AutostartDesktop(exe)), 0o644)
}

// RemoveAutostart 移除登录自启项；不存在不视为错误。
func RemoveAutostart() error {
	if err := os.Remove(autostartPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
