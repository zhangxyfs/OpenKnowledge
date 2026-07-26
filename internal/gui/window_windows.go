//go:build windows

package gui

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows        = user32.NewProc("EnumWindows")
	procGetWindowTextW     = user32.NewProc("GetWindowTextW")
	procGetWindowTextLenW  = user32.NewProc("GetWindowTextLengthW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procIsWindowVisible    = user32.NewProc("IsWindowVisible")
)

const swMaximize = 3

// maximizeWindowByTitle 轮询查找标题包含 substr 的顶层窗口并最大化（最多等待 timeout）。
// 用于浏览器应用模式启动后兜底最大化——Edge 单实例时 Start-Process 的
// -WindowStyle 会被已有进程吞掉，只能事后 ShowWindow。
func maximizeWindowByTitle(substr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if hwnd := findWindowByTitle(substr); hwnd != 0 {
			procShowWindow.Call(hwnd, swMaximize)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// findWindowByTitle 返回第一个可见且标题包含 substr 的顶层窗口句柄，找不到返回 0。
func findWindowByTitle(substr string) uintptr {
	var found uintptr
	cb := windows.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		if r, _, _ := procIsWindowVisible.Call(hwnd); r == 0 {
			return 1 // 不可见，继续枚举
		}
		n, _, _ := procGetWindowTextLenW.Call(hwnd)
		if n == 0 {
			return 1
		}
		buf := make([]uint16, n+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
		title := windows.UTF16ToString(buf)
		if containsIgnoreCase(title, substr) {
			found = hwnd
			return 0 // 停止枚举
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// containsIgnoreCase 大小写不敏感的子串判断（标题匹配用，仅 ASCII 折叠）。
func containsIgnoreCase(s, sub string) bool {
	ls, lsub := len(s), len(sub)
	if lsub == 0 {
		return true
	}
	if ls < lsub {
		return false
	}
	for i := 0; i+lsub <= ls; i++ {
		match := true
		for j := 0; j < lsub; j++ {
			a, b := s[i+j], sub[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
