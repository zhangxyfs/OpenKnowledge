// Package fsx 提供文件系统写入的公共工具。
package fsx

import (
	"os"
	"path/filepath"
	"time"
)

// WriteFile 原子写入：同目录随机临时文件 + fsync + rename 替换。
// 直接 O_TRUNC 覆盖在崩溃/断电/并发读时会让读者看到半截文件（状态、注册表、
// 各宿主 settings 均曾被这样写坏）；rename 保证读者要么看到旧文件要么看到新文件。
// 临时文件名带随机后缀，并发写者不会共用同一 tmp 名交错出混合内容。
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	// rename 成功后 Remove 报 ENOENT，失败路径上清理半成品
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil { // CreateTemp 固定 0600，按调用方要求恢复
		return err
	}
	return os.Rename(name, path)
}

// BumpMtime 对抗秒级 mtime 粒度：写完文件后调用，若当前 mtime 与 prev（写前
// mtime）仍在同一秒，按 mtime diff 判变化的机制（index.Sync 等）会漏掉本次
// 写入——把 mtime 推进一秒兜底。prev 传零值（新文件/写前 stat 失败）时不动作。
func BumpMtime(path string, prev time.Time) {
	if prev.IsZero() {
		return
	}
	if fi, err := os.Stat(path); err == nil && fi.ModTime().Unix() == prev.Unix() {
		t := prev.Add(time.Second)
		_ = os.Chtimes(path, t, t)
	}
}
