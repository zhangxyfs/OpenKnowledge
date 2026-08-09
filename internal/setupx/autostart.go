package setupx

import "fmt"

// AutostartDesktop 生成 XDG autostart desktop 文件内容（平台无关纯函数，便于测试）。
func AutostartDesktop(exe string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=OpenKnowledge
Exec=%s daemon
X-GNOME-Autostart-enabled=true
`, exe)
}
