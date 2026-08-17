package fsx

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	lockTimeout  = 5 * time.Second // 拿锁最长等待：hook 有 10s 宿主上限，不能等满
	lockStaleAge = 2 * time.Second // 锁龄超此值视为持有者崩溃残留，可抢占
)

// WithFileLock 以 path+".lock" 的 O_EXCL 锁文件实现跨进程互斥，锁内容为持有者
// token，释放时只删自己的锁（超时被抢占后不误删新持有者）。临界区应是毫秒级
// 快操作（Load→改→原子写）：正常持有远短于 lockStaleAge，因此锁龄超 2s 即可断定
// 持有者已崩溃、抢占安全；等锁到 lockTimeout 仍拿不到则 fail-open 无锁执行——
// 配合原子写，竞态窗口远小于干脆不锁，且绝不能因等锁卡死宿主 hook。
// 返回 fn 的错误；抢占/fail-open 路径同样执行并返回 fn 的错误。
func WithFileLock(path string, fn func() error) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	lp := path + ".lock"
	token := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(token)
			_ = f.Close()
			err := fn()
			// 只删除仍属于本持有者的锁：超时被抢占后文件归新持有者
			if data, rerr := os.ReadFile(lp); rerr == nil && string(data) == token {
				_ = os.Remove(lp)
			}
			return err
		}
		if fi, statErr := os.Stat(lp); statErr == nil && time.Since(fi.ModTime()) > lockStaleAge {
			_ = os.Remove(lp)
			continue
		}
		if time.Now().After(deadline) {
			return fn()
		}
		time.Sleep(15 * time.Millisecond)
	}
}
