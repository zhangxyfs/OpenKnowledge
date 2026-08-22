package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/agentx"
	"openknowledge/internal/config"
)

// TestHooksTimeoutSet 独立写全局 hook 超时：只落 [hooks] timeout_sec，
// 不触碰任何 agent hooks 配置文件（重装是 /api/setup/hooks 的职责）。
func TestHooksTimeoutSet(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "POST", srv.URL+"/api/hooks/timeout", testToken, map[string]any{"timeout_sec": 20})
	if code != 200 {
		t.Fatalf("set timeout: status = %d, body %s", code, data)
	}
	var res struct {
		OK         bool `json:"ok"`
		TimeoutSec int  `json:"timeout_sec"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.TimeoutSec != 20 {
		t.Fatalf("unexpected response: %s", data)
	}
	cfg, err := config.LoadMerged("", filepath.Join(okHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hooks.TimeoutSec != 20 {
		t.Fatalf("global config.toml timeout_sec = %d, want 20", cfg.Hooks.TimeoutSec)
	}

	// 边界：0 / 61 / 非数字 → 400
	for _, body := range []any{
		map[string]any{"timeout_sec": 0},
		map[string]any{"timeout_sec": 61},
		map[string]any{"timeout_sec": "abc"},
	} {
		if code, data := do(t, "POST", srv.URL+"/api/hooks/timeout", testToken, body); code != 400 {
			t.Fatalf("body %v: status = %d, want 400 (body %s)", body, code, data)
		}
	}

	// 不触碰任何 agent hooks 配置文件
	if _, err := os.Stat(filepath.Join(agentx.KimiHome(), "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("hooks/timeout must not touch agent hooks config, stat err = %v", err)
	}
}

// TestHooksRemoveSingleAgent 单 agent 卸载：幂等，返回是否实际移除；未知 id → 400。
func TestHooksRemoveSingleAgent(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 前置：隔离 agent home 下先走安装路径（等价引导页"安装 hooks"）
	code, data := do(t, "POST", srv.URL+"/api/setup/hooks", testToken, map[string]any{"agent": "kimi"})
	if code != 200 {
		t.Fatalf("setup/hooks: status = %d, body %s", code, data)
	}
	kimiCfg := filepath.Join(agentx.KimiHome(), "config.toml")
	cfgData, err := os.ReadFile(kimiCfg)
	if err != nil || !strings.Contains(string(cfgData), agentx.MarkerBegin) {
		t.Fatalf("precondition: kimi hooks not installed: %v", err)
	}

	// 卸载 → removed=true，hooks 目标文件内 ok 段移除
	code, data = do(t, "POST", srv.URL+"/api/setup/hooks/remove", testToken, map[string]any{"agent": "kimi"})
	if code != 200 {
		t.Fatalf("hooks/remove: status = %d, body %s", code, data)
	}
	var res struct {
		OK      bool `json:"ok"`
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Removed {
		t.Fatalf("expected ok+removed=true: %s", data)
	}
	cfgData, err = os.ReadFile(kimiCfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfgData), agentx.MarkerBegin) {
		t.Fatalf("ok hooks block should be removed: %s", cfgData)
	}

	// 幂等：重复调用 removed=false
	code, data = do(t, "POST", srv.URL+"/api/setup/hooks/remove", testToken, map[string]any{"agent": "kimi"})
	if code != 200 {
		t.Fatalf("hooks/remove again: status = %d, body %s", code, data)
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	if res.Removed {
		t.Fatalf("second remove should report removed=false: %s", data)
	}

	// 未知 agent → 400
	code, _ = do(t, "POST", srv.URL+"/api/setup/hooks/remove", testToken, map[string]any{"agent": "nobody"})
	if code != 400 {
		t.Fatalf("unknown agent: status = %d, want 400", code)
	}
}
