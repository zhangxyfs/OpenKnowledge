//go:build !windows

package gui

import "time"

// maximizeWindowByTitle 非 Windows 平台无操作。
func maximizeWindowByTitle(_ string, _ time.Duration) {}
