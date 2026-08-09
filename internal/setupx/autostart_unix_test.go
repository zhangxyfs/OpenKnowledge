//go:build !windows

package setupx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndRemoveAutostart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := WriteAutostart("/usr/lib/openknowledge/ok"); err != nil {
		t.Fatal(err)
	}
	p := autostartPath()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Exec=/usr/lib/openknowledge/ok daemon") {
		t.Fatalf("unexpected content:\n%s", data)
	}
	// 幂等覆盖
	if err := WriteAutostart("/opt/ok"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(p)
	if !strings.Contains(string(data), "Exec=/opt/ok daemon") {
		t.Fatalf("overwrite failed:\n%s", data)
	}
	if err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, err=%v", err)
	}
	// 重复移除不报错
	if err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != filepath.Join(os.Getenv("HOME"), ".config", "autostart") {
		t.Fatalf("unexpected path: %s", p)
	}
}
