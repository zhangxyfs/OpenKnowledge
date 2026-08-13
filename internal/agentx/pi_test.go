package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiAgentInstallDetectRemove(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	a, ok := Find("pi")
	if !ok {
		t.Fatal("pi agent not registered")
	}
	if !a.Detect() {
		t.Fatal("Detect should be true when PI_CODING_AGENT_DIR exists")
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false before install")
	}
	if err := a.InstallHooks(`D:\x\ok.exe`); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(PiHome(), "extensions", "openknowledge.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("extension not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "// fingerprint: ") || !strings.Contains(content, filepath.ToSlash(`D:\x\ok.exe`)) {
		t.Fatalf("bad extension content: %.200s", content)
	}
	if strings.Contains(content, "{{EXE}}") {
		t.Fatal("unrendered placeholder remains")
	}
	if !a.HooksInstalled() {
		t.Fatal("HooksInstalled should be true after install")
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("extension file should be removed")
	}
}

func TestPiAgentForeignFilePreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	extDir := filepath.Join(dir, "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(extDir, "openknowledge.ts")
	if err := os.WriteFile(path, []byte("// user hand-written extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := piAgent{}
	if a.HooksInstalled() {
		t.Fatal("foreign file should not count as installed")
	}
	if err := a.InstallHooks(`D:\x\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak-openknowledge"); err != nil {
		t.Fatal("foreign file should be backed up before overwrite")
	}
	// 新文件是本工具生成后可删除；但手工文件恢复后不删
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if err := os.WriteFile(path, []byte("// user hand-written extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err = a.RemoveHooks()
	if err != nil || removed {
		t.Fatalf("foreign file must not be removed: %v, %v", removed, err)
	}
}

func TestPiAgentEnsureHooksStaleRewrite(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	a := piAgent{}
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(a.HooksTarget())
	if !strings.Contains(string(data), filepath.ToSlash(`D:\new\ok.exe`)) {
		t.Fatal("EnsureHooks should rewrite stale extension with new exe path")
	}
	// 文件不存在时 EnsureHooks 为 no-op（pi 不会触发 hook）
	if err := os.Remove(a.HooksTarget()); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.HooksTarget()); !os.IsNotExist(err) {
		t.Fatal("EnsureHooks must not recreate a deleted extension")
	}
}
