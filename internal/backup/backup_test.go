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
