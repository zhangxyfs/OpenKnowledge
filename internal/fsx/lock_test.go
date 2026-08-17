package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWithFileLockExecutesAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	ran := false
	if err := WithFileLock(path, func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn 未执行")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("锁文件未释放: %v", err)
	}
}

func TestWithFileLockPropagatesError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := errors.New("boom")
	if err := WithFileLock(path, func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// 锁龄超 lockStaleAge 视为持有者崩溃残留：应被抢占而非等满 lockTimeout。
func TestWithFileLockPreemptsStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lp := path + ".lock"
	if err := os.WriteFile(lp, []byte("dead-holder"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-lockStaleAge - time.Second)
	if err := os.Chtimes(lp, stale, stale); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ran := false
	if err := WithFileLock(path, func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn 未执行")
	}
	if elapsed := time.Since(start); elapsed > lockTimeout {
		t.Fatalf("抢占陈旧锁耗时 %v，疑似走了等锁超时路径", elapsed)
	}
	if _, err := os.Stat(lp); !os.IsNotExist(err) {
		t.Fatalf("抢占后锁文件未释放: %v", err)
	}
}

// 临界区内锁被新持有者替换（超时被抢占的场景）：释放时只删自己的锁，
// 不得误删新持有者的锁文件。
func TestWithFileLockKeepsForeignLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lp := path + ".lock"
	err := WithFileLock(path, func() error {
		return os.WriteFile(lp, []byte("new-holder"), 0o644) // 模拟锁已被抢占易主
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(lp)
	if err != nil {
		t.Fatalf("新持有者的锁被误删: %v", err)
	}
	if string(data) != "new-holder" {
		t.Fatalf("锁内容 = %q, want new-holder", data)
	}
}

// 静态他方锁在锁龄到 lockStaleAge 后被抢占：拿锁总耗时应落在
// [lockStaleAge, lockTimeout) 区间——既不立即抢（给正常持有者窗口），也不等满超时。
func TestWithFileLockWaitsThenPreempts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lp := path + ".lock"
	if err := os.WriteFile(lp, []byte("busy-holder"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ran := false
	if err := WithFileLock(path, func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn 未执行")
	}
	elapsed := time.Since(start)
	if elapsed < lockStaleAge || elapsed >= lockTimeout {
		t.Fatalf("抢占耗时 %v，应在 [%v, %v) 区间", elapsed, lockStaleAge, lockTimeout)
	}
}

// 他方锁持续刷新 mtime（长事务仍活着）时永不满足抢占条件：等满 lockTimeout
// 后 fail-open 执行——绝不能卡死宿主 hook；fail-open 路径不碰他方锁文件。
func TestWithFileLockFailOpenOnTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lp := path + ".lock"
	if err := os.WriteFile(lp, []byte("busy-holder"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() { // 模拟存活的长事务：持续刷新锁 mtime 使其永不陈旧
		for {
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Millisecond):
				now := time.Now()
				_ = os.Chtimes(lp, now, now)
			}
		}
	}()
	start := time.Now()
	ran := false
	if err := WithFileLock(path, func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn 未执行")
	}
	if elapsed := time.Since(start); elapsed < lockTimeout {
		t.Fatalf("fail-open 耗时 %v < %v，疑似未等锁", elapsed, lockTimeout)
	}
	data, err := os.ReadFile(lp)
	if err != nil || string(data) != "busy-holder" {
		t.Fatalf("他方锁文件被改动: data=%q err=%v", data, err)
	}
}
