package embed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindBuiltinModel(t *testing.T) {
	m := FindBuiltinModel("qwen3-emb-0.6b-q8")
	if m == nil || m.Dim != 1024 || m.Pooling != "last" {
		t.Fatalf("%+v", m)
	}
	if m.QueryPrefix == "" {
		t.Fatal("qwen3 应有查询指令前缀")
	}
	if FindBuiltinModel("不存在") != nil {
		t.Fatal("未知 id 应返回 nil")
	}
	n := FindBuiltinModel("nomic-embed-q8")
	if n.DocPrefix != "search_document: " || n.QueryPrefix != "search_query: " {
		t.Fatalf("nomic 双前缀: %+v", n)
	}
}

func TestMirrorBaseAndURL(t *testing.T) {
	m := FindBuiltinModel("bge-m3-q4_k_m")
	if got := m.URL(""); got != "https://hf-mirror.com/gpustack/bge-m3-GGUF/resolve/main/bge-m3-Q4_K_M.gguf" {
		t.Fatal(got)
	}
	if got := m.URL("huggingface"); got != "https://huggingface.co/gpustack/bge-m3-GGUF/resolve/main/bge-m3-Q4_K_M.gguf" {
		t.Fatal(got)
	}
	if got := m.URL("http://127.0.0.1:9999/"); got != "http://127.0.0.1:9999/gpustack/bge-m3-GGUF/resolve/main/bge-m3-Q4_K_M.gguf" {
		t.Fatal("自定义镜像应去尾斜杠")
	}
}

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	m := FindBuiltinModel("nomic-embed-q8")
	if m.Installed(dir) {
		t.Fatal("空目录不应判定已安装")
	}
	p := m.InstalledPath(dir)
	if err := os.WriteFile(p, make([]byte, m.Size), 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.Installed(dir) {
		t.Fatal("尺寸一致应判定已安装")
	}
	if filepath.Base(p) != "nomic-embed-q8.gguf" {
		t.Fatal(p)
	}
}
