//go:build windows

package setupx

// WriteAutostart Windows 平台自启由安装器写注册表（HKCU Run），此处 no-op。
func WriteAutostart(_ string) error { return nil }

// RemoveAutostart Windows 平台 no-op（注册表项随安装器卸载清除）。
func RemoveAutostart() error { return nil }
