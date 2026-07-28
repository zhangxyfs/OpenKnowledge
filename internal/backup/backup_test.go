package backup

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/registry"
)

// setupHome 建隔离 OK_HOME，注册两个项目并各写条目/config。
func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	reg, _ := registry.Load(registry.DefaultPath())
	for _, name := range []string{"alpha", "beta"} {
		if err := reg.AddProject(name, `D:\src\`+name); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(home, "projects", name, "knowledge")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		md := "---\ntitle: " + name + "条目\ntype: note\ntags: [t]\nsummary: s\ndraft: false\nmandatory: false\n---\n正文" + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "projects", name, "config.toml"), []byte("[wiki]\nstale_commits = 7\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	return home
}

func zipNames(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, rc)
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

func TestExportAll(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "all"); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, buf.Bytes())
	for _, want := range []string{
		"registry.toml",
		"projects/alpha/knowledge/alpha.md",
		"projects/alpha/config.toml",
		"projects/beta/knowledge/beta.md",
		"projects/beta/config.toml",
	} {
		if _, ok := names[want]; !ok {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	if !strings.Contains(names["registry.toml"], "alpha") ||
		!strings.Contains(names["projects/alpha/knowledge/alpha.md"], "正文alpha") {
		t.Fatal("content mismatch")
	}
}

func TestExportSingleProject(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "alpha"); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, buf.Bytes())
	if _, ok := names["projects/alpha/knowledge/alpha.md"]; !ok {
		t.Fatal("alpha missing")
	}
	for n := range names {
		if strings.Contains(n, "beta") {
			t.Fatalf("single export leaked %s", n)
		}
	}
	if strings.Contains(names["registry.toml"], "beta") {
		t.Fatal("filtered registry leaked beta")
	}
}

func TestExportUnknownProject(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	err := Export(&buf, "nope")
	if !errors.Is(err, ErrBadPackage) {
		t.Fatalf("err=%v", err)
	}
}

// 往返：导出 → 改环境（删条目+清空 registry）→ 导入 → 断言还原
func TestImportRoundTrip(t *testing.T) {
	home := setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "all"); err != nil {
		t.Fatal(err)
	}
	// 破坏现场：删 alpha 条目、换全新 registry
	if err := os.Remove(filepath.Join(home, "projects", "alpha", "knowledge", "alpha.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	rep, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 2 || rep.Skipped != 0 || len(rep.Projects) != 2 {
		t.Fatalf("report: %+v", rep)
	}
	data, err := os.ReadFile(filepath.Join(home, "projects", "alpha", "knowledge", "alpha.md"))
	if err != nil || !strings.Contains(string(data), "正文alpha") {
		t.Fatalf("entry not restored: %v", err)
	}
	reg, _ := registry.Load(registry.DefaultPath())
	if len(reg.Projects) != 2 {
		t.Fatalf("registry not restored: %+v", reg.Projects)
	}
	// config 也还原
	if _, err := os.Stat(filepath.Join(home, "projects", "beta", "config.toml")); err != nil {
		t.Fatal("config not restored")
	}
}

// 同名覆盖：改条目内容后导入旧包，内容被旧包覆盖
func TestImportOverwrites(t *testing.T) {
	home := setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "all"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, "projects", "alpha", "knowledge", "alpha.md")
	os.WriteFile(p, []byte("---\ntitle: 新版\ntype: note\ntags: []\nsummary: x\ndraft: false\nmandatory: false\n---\n新正文\n"), 0o644)
	if _, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "正文alpha") {
		t.Fatal("overwrite failed")
	}
}

// 损坏 .md 计 skipped，不中断
func TestImportSkipsCorrupt(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("registry.toml")
	w.Write([]byte("[[project]]\nname = \"alpha\"\npaths = [\"D:/src/alpha\"]\n"))
	w2, _ := zw.Create("projects/alpha/knowledge/bad.md")
	w2.Write([]byte("这不是 frontmatter"))
	w3, _ := zw.Create("projects/alpha/knowledge/good.md")
	w3.Write([]byte("---\ntitle: 好\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n"))
	zw.Close()
	rep, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 1 || rep.Skipped != 1 {
		t.Fatalf("report: %+v", rep)
	}
}

// zip-slip 与非法文件整包拒绝
func TestImportRejectsBadNames(t *testing.T) {
	setupHome(t)
	mk := func(name string) *bytes.Buffer {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("registry.toml")
		w.Write([]byte("[[project]]\nname=\"a\"\npaths=[\"x\"]\n"))
		w2, _ := zw.Create(name)
		w2.Write([]byte("x"))
		zw.Close()
		return &buf
	}
	for _, name := range []string{
		"projects/a/knowledge/../../evil.md",
		"/abs.md",
		"C:/evil.md",
		"projects\\a\\knowledge\\x.md",
		"random.txt",
	} {
		buf := mk(name)
		if _, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len())); !errors.Is(err, ErrBadPackage) {
			t.Fatalf("%s: expected ErrBadPackage", name)
		}
	}
}

func TestImportTooBigAndMissingRegistry(t *testing.T) {
	setupHome(t)
	if _, err := Import(bytes.NewReader(nil), MaxSize+1); !errors.Is(err, ErrBadPackage) {
		t.Fatal("size limit")
	}
	// 无 registry.toml 的合法 zip
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("projects/a/knowledge/x.md")
	w.Write([]byte("---\ntitle: x\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\nb\n"))
	zw.Close()
	if _, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len())); !errors.Is(err, ErrBadPackage) {
		t.Fatal("missing registry.toml")
	}
}
