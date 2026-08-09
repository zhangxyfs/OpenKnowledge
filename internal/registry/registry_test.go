package registry

import (
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	got := NormalizePath(`D:\develop\OpenKnowledge\`)
	want := "d:/develop/openknowledge"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFindByCwdLongestPrefix(t *testing.T) {
	r := &Registry{Projects: []Project{
		{Name: "root", Paths: []string{`D:\develop`}},
		{Name: "ok", Paths: []string{`D:\develop\OpenKnowledge`}},
	}}
	p := r.FindByCwd(`d:\DEVELOP\OpenKnowledge\docs`)
	if p == nil || p.Name != "ok" {
		t.Fatalf("expected ok, got %+v", p)
	}
	if p := r.FindByCwd(`E:\other`); p != nil {
		t.Fatalf("expected nil, got %+v", p)
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.toml")
	r := &Registry{}
	if err := r.AddProject("ok", `D:\develop\OpenKnowledge`); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != 1 || loaded.Projects[0].Name != "ok" {
		t.Fatalf("unexpected %+v", loaded)
	}
	if err := loaded.AddProject("ok", `E:\x`); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestLoadMissing(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || len(r.Projects) != 0 {
		t.Fatalf("expected empty registry, got %+v err=%v", r, err)
	}
}

func TestRemoveProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_HOME", dir)
	reg := &Registry{}
	if err := reg.AddProject("alpha", "D:/src/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject("beta", "D:/src/beta"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(DefaultPath()); err != nil {
		t.Fatal(err)
	}

	if !reg.RemoveProject("alpha") {
		t.Fatal("RemoveProject(alpha) = false, want true")
	}
	if reg.RemoveProject("alpha") {
		t.Fatal("RemoveProject(alpha) again = true, want false")
	}

	// Save 往返后注册表只剩 beta
	if err := reg.Save(DefaultPath()); err != nil {
		t.Fatal(err)
	}
	back, err := Load(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Projects) != 1 || back.Projects[0].Name != "beta" {
		t.Fatalf("unexpected projects after remove: %+v", back.Projects)
	}
}
