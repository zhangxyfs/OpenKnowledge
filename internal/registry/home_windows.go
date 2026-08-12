//go:build windows

package registry

import (
	"golang.org/x/sys/windows"
)

// realProfileDir 返回真实用户配置目录，对 HOME/USERPROFILE 重定向免疫
// （CodePilot 等宿主的 shadow HOME 会把它们指向临时目录做 provider 隔离）。
func realProfileDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Profile, windows.KF_FLAG_DEFAULT)
}
