package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonTargetFor(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "ok.exe")
	if err := os.WriteFile(self, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 无同目录 okd：回退自身 + "daemon"（单二进制/旧部署）
	exe, args := daemonTargetFor(self)
	if exe != self || len(args) != 1 || args[0] != "daemon" {
		t.Fatalf("fallback: exe=%q args=%v", exe, args)
	}
	// 有同目录 okd：用 okd、无参数（三 exe 部署）
	okd := filepath.Join(dir, daemonExeName())
	if err := os.WriteFile(okd, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, args = daemonTargetFor(self)
	if exe != okd || len(args) != 0 {
		t.Fatalf("sibling okd: exe=%q args=%v", exe, args)
	}
}
