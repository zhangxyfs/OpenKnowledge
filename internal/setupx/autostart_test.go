package setupx

import (
	"strings"
	"testing"
)

func TestAutostartDesktop(t *testing.T) {
	content := AutostartDesktop("/usr/lib/openknowledge/ok")
	for _, want := range []string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=OpenKnowledge",
		"Exec=/usr/lib/openknowledge/ok daemon",
		"X-GNOME-Autostart-enabled=true",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("desktop 文件缺少 %q:\n%s", want, content)
		}
	}
}
