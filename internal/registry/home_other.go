//go:build !windows

package registry

import "os/user"

// realProfileDir 返回真实用户主目录：os/user 在无 cgo 时解析 /etc/passwd，
// 对 $HOME 重定向免疫；失败返回 error 由 Home() 回退 os.UserHomeDir()。
func realProfileDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}
