// Package tray 提供 daemon 内嵌的系统托盘：图标、单击菜单（版本+退出）、
// 双击打开/聚焦唯一 GUI 窗口。仅 Windows 有实现，其余平台空转。
package tray

// decideReuseGUI 判定双击托盘时是否复用既有 GUI 窗口（有效则聚焦，否则重开）。
func decideReuseGUI(hwnd uintptr, isWindow func(uintptr) bool) bool {
	return hwnd != 0 && isWindow(hwnd)
}
