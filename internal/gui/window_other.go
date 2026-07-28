//go:build !windows

package gui

import "time"

// maximizeWindowByTitle 非 Windows 平台无操作。
func maximizeWindowByTitle(_ string, _ time.Duration) uintptr { return 0 }

// IsWindow 非 Windows 平台恒 false。
func IsWindow(hwnd uintptr) bool { return false }

// FocusWindow 非 Windows 平台空实现。
func FocusWindow(hwnd uintptr) {}
