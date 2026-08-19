package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetRetrieveDedupTurnsUpsert [retrieve] 小节内单键 upsert：其余键、注释与
// [retrieve.gate] 子表原样保留；键行落在 [retrieve] 顶层段内（子表之前）；
// 重复设置幂等。
func TestSetRetrieveDedupTurnsUpsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "# 顶部注释\n[retrieve]\nalpha = 1.5 # 行内注释\nfusion = \"rrf\"\n\n[retrieve.gate]\nenabled = false\nextra_phrases = [\"走起\"]\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRetrieveDedupTurns(path, 5); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"# 顶部注释", "alpha = 1.5 # 行内注释", "fusion = \"rrf\"", "dedup_turns = 5", "[retrieve.gate]\nenabled = false", `extra_phrases = ["走起"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("upsert 后丢失 %q:\n%s", want, got)
		}
	}
	// 键行必须在 [retrieve] 顶层段内（[retrieve.gate] 子表之前）
	if strings.Index(got, "dedup_turns = 5") > strings.Index(got, "[retrieve.gate]") {
		t.Fatalf("键行落在子表内:\n%s", got)
	}
	// 回读生效且子表不受影响
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 5 || cfg.Retrieve.Alpha != 1.5 || cfg.Retrieve.Gate.Enabled {
		t.Fatalf("回读值不对: %+v", cfg.Retrieve)
	}
	// 重复设置：替换而非追加
	if err := SetRetrieveDedupTurns(path, 0); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "dedup_turns") != 1 || !strings.Contains(got, "dedup_turns = 0") {
		t.Fatalf("重复设置应幂等替换:\n%s", got)
	}
}

// TestSetRetrieveDedupTurnsNoSection 无 [retrieve] 小节时文件尾追加整块
//（[retrieve.gate] 子表已存在也不受影响）。
func TestSetRetrieveDedupTurnsNoSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[retrieve.gate]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRetrieveDedupTurns(path, 7); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 7 || cfg.Retrieve.Gate.Enabled {
		t.Fatalf("回读值不对: %+v", cfg.Retrieve)
	}
}

// TestSetRetrieveDedupTurnsNewFile 文件不存在时直接创建。
func TestSetRetrieveDedupTurnsNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetRetrieveDedupTurns(path, 9); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 9 {
		t.Fatalf("got %v, want 9", cfg.Retrieve.DedupTurns)
	}
}

// TestDedupTurnsDefault 默认 3；配置文件缺省时 Load 回填默认值。
func TestDedupTurnsDefault(t *testing.T) {
	if got := Default().Retrieve.DedupTurns; got != 3 {
		t.Fatalf("默认应为 3, got %v", got)
	}
}

// TestEffectiveDedupTurns <0 归一为 0（关闭），0 与正值原样返回。
func TestEffectiveDedupTurns(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-5, 0}, {-1, 0}, {0, 0}, {3, 3}, {99, 99}} {
		if got := (Retrieve{DedupTurns: tc.in}).EffectiveDedupTurns(); got != tc.want {
			t.Fatalf("DedupTurns=%d: got %d, want %d", tc.in, got, tc.want)
		}
	}
}
