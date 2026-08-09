//go:build windows

package setupx

import "testing"

func TestAutostartWindowsNoop(t *testing.T) {
	if err := WriteAutostart("x"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
}
