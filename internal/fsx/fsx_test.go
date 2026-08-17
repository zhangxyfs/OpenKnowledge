package fsx

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBumpMtimeSameSecond(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.md")
	if err := WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	prev := fi.ModTime()
	// 同秒重写（写后 mtime 与 prev 同秒是常态——秒级粒度下同秒写不发生跳变）
	if err := WriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	BumpMtime(path, prev)
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi2.ModTime().Unix() != prev.Unix()+1 {
		t.Fatalf("mtime = %v, want prev+1s (%v)", fi2.ModTime(), prev.Add(time.Second))
	}
}

func TestBumpMtimeZeroPrevNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.md")
	if err := WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	BumpMtime(path, time.Time{})
	fi2, _ := os.Stat(path)
	if !fi2.ModTime().Equal(fi.ModTime()) {
		t.Fatalf("零 prev 不应改 mtime: %v → %v", fi.ModTime(), fi2.ModTime())
	}
}

func TestBumpMtimeLaterSecondNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.md")
	if err := WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// prev 比实际 mtime 早一秒以上：写入已跨秒，无需推进
	fi, _ := os.Stat(path)
	prev := fi.ModTime().Add(-2 * time.Second)
	BumpMtime(path, prev)
	fi2, _ := os.Stat(path)
	if !fi2.ModTime().Equal(fi.ModTime()) {
		t.Fatalf("跨秒写入不应改 mtime: %v → %v", fi.ModTime(), fi2.ModTime())
	}
}
