//go:build windows

package gui

import (
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLenW        = user32.NewProc("GetWindowTextLengthW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindow                 = user32.NewProc("IsWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")

	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

const swMaximize = 3

// browserExes 允许最大化的窗口所属进程（小写 exe 名）。
// 仅靠标题匹配会误中资源管理器（打开安装目录时标题含 "OpenKnowledge"）、
// 终端（cwd 在项目目录）等窗口，EnumWindows 按 Z 序先命中谁就最大化谁。
var browserExes = map[string]bool{
	"msedge.exe":   true,
	"chrome.exe":   true,
	"chromium.exe": true,
	"brave.exe":    true,
	"firefox.exe":  true,
}

// maximizeWindowByTitle 最大化本次启动新开的浏览器窗口（最多等待 timeout），
// 返回被最大化的窗口句柄；超时未见新窗口则兜底最大化第一个匹配并返回之；
// 无任何匹配返回 0。
func maximizeWindowByTitle(substr string, timeout time.Duration) uintptr {
	existing := map[uintptr]bool{}
	for _, hwnd := range findWindowsByTitle(substr) {
		existing[hwnd] = true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, hwnd := range findWindowsByTitle(substr) {
			if !existing[hwnd] {
				procShowWindow.Call(hwnd, swMaximize)
				return hwnd
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	// 未见新窗口（浏览器复用既有窗口/新窗口标题迟迟未设置）→ 兜底最大化第一个匹配
	if hwnds := findWindowsByTitle(substr); len(hwnds) > 0 {
		procShowWindow.Call(hwnds[0], swMaximize)
		return hwnds[0]
	}
	return 0
}

// IsWindow 报告 hwnd 是否仍是有效窗口（窗口已关闭则句柄失效）。
func IsWindow(hwnd uintptr) bool {
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

// FocusWindow 还原并置前台。非前台进程调 SetForegroundWindow 常被系统前台锁
// 拒绝，附加到当前前台线程后再试（AttachThreadInput 标准兜底）。
func FocusWindow(hwnd uintptr) {
	const swRestore = 9
	procShowWindow.Call(hwnd, swRestore)
	if r, _, _ := procSetForegroundWindow.Call(hwnd); r != 0 {
		return
	}
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 {
		return
	}
	var fgTid uint32
	procGetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&fgTid)))
	curTid, _, _ := procGetCurrentThreadId.Call()
	procAttachThreadInput.Call(curTid, uintptr(fgTid), 1)
	procSetForegroundWindow.Call(hwnd)
	procAttachThreadInput.Call(curTid, uintptr(fgTid), 0)
}

// findWindowsByTitle 按 Z 序返回所有可见、标题包含 substr 且属于浏览器进程的
// 顶层窗口句柄。
func findWindowsByTitle(substr string) []uintptr {
	var found []uintptr
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
		if containsIgnoreCase(title, substr) && isBrowserWindow(hwnd) {
			found = append(found, hwnd)
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// isBrowserWindow 判断顶层窗口是否属于已知浏览器进程。
func isBrowserWindow(hwnd uintptr) bool {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return false
	}
	exe := strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:size])))
	return browserExes[exe]
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
