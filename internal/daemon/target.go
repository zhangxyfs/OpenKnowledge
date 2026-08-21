package daemon

import (
	"os"
	"path/filepath"
	"runtime"
)

// daemonExeName 三 exe 部署中 daemon 的可执行文件名。
func daemonExeName() string {
	if runtime.GOOS == "windows" {
		return "okd.exe"
	}
	return "okd"
}

// daemonTargetFor 由 self（当前 exe 的绝对路径）推导拉起 daemon 的目标与参数：
// 同目录存在 okd（三 exe 部署）→ 它、无参数；否则 → 自身 + "daemon"（单二进制开发/旧部署回退）。
func daemonTargetFor(self string) (string, []string) {
	sibling := filepath.Join(filepath.Dir(self), daemonExeName())
	if _, err := os.Stat(sibling); err == nil {
		return sibling, nil
	}
	return self, []string{"daemon"}
}

// daemonTarget 当前进程的拉起目标（符号链接解析后）。os.Executable 失败返回空串。
func daemonTarget() (string, []string) {
	self, err := os.Executable()
	if err != nil {
		return "", nil
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return daemonTargetFor(self)
}
