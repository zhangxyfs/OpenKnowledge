package tray

import "testing"

func TestDecideReuseGUI(t *testing.T) {
	alive := func(uintptr) bool { return true }
	dead := func(uintptr) bool { return false }
	if !decideReuseGUI(42, alive) {
		t.Fatal("live hwnd should be reused")
	}
	if decideReuseGUI(42, dead) {
		t.Fatal("dead hwnd should not be reused")
	}
	if decideReuseGUI(0, alive) {
		t.Fatal("zero hwnd should not be reused")
	}
}
