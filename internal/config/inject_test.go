package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetInjectMandatoryMaxUpsert [inject] 小节内单键 upsert：已有小节只换该键行，
// max_tokens / reinject_turns 与注释原样保留；重复设置幂等。
func TestSetInjectMandatoryMaxUpsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "# 顶部注释\n[inject]\nmax_tokens = 800 # 行内注释\nreinject_turns = 3\n\n[retrieve]\ntop_n = 2\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetInjectMandatoryMax(path, 3000); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"# 顶部注释", "max_tokens = 800 # 行内注释", "reinject_turns = 3", "mandatory_max_tokens = 3000", "[retrieve]\ntop_n = 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("upsert 后丢失 %q:\n%s", want, got)
		}
	}
	// 键行必须在 [inject] 小节内（[retrieve] 之前）
	if strings.Index(got, "mandatory_max_tokens = 3000") > strings.Index(got, "[retrieve]") {
		t.Fatalf("键行落在 [inject] 小节外:\n%s", got)
	}
	// 重复设置：替换而非追加
	if err := SetInjectMandatoryMax(path, 1500); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "mandatory_max_tokens") != 1 || !strings.Contains(got, "mandatory_max_tokens = 1500") {
		t.Fatalf("重复设置应幂等替换:\n%s", got)
	}
}

// TestSetInjectMandatoryMaxNoSection 无 [inject] 小节时文件尾追加整块。
func TestSetInjectMandatoryMaxNoSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[retrieve]\ntop_n = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetInjectMandatoryMax(path, 2000); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "[retrieve]\ntop_n = 2") || !strings.Contains(got, "[inject]\nmandatory_max_tokens = 2000") {
		t.Fatalf("追加整块失败:\n%s", got)
	}
	// 落盘结果必须能正常解析且值生效
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inject.MandatoryMaxTokens != 2000 || cfg.Retrieve.TopN != 2 {
		t.Fatalf("回读值不对: %+v", cfg.Inject)
	}
}

// TestSetInjectMandatoryMaxNewFile 文件不存在时直接创建。
func TestSetInjectMandatoryMaxNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetInjectMandatoryMax(path, 4000); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inject.MandatoryMaxTokens != 4000 {
		t.Fatalf("got %v, want 4000", cfg.Inject.MandatoryMaxTokens)
	}
}
