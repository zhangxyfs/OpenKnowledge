// Package embedsidecar 管理内置 embedding 推理 sidecar（llama-server）：
// 状态文件发现、want 拉起请求、静默 spawn、空闲回收。daemon 是唯一的
// 拉起/看护主体；hook/cli/gui 只读状态或写 want，绝不等待冷启动。
package embedsidecar

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"openknowledge/internal/registry"
)

// State 是 embed-sidecar.json 的内容：sidecar 发现与身份。
type State struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	ModelID   string    `json:"model_id"`
	StartedAt time.Time `json:"started_at"`
	LastUsed  time.Time `json:"last_used"`
}

func statePath() string { return filepath.Join(registry.Home(), "embed-sidecar.json") }
func wantPath() string  { return filepath.Join(registry.Home(), "embed-sidecar.want") }
func logPath() string   { return filepath.Join(registry.Home(), "embed-sidecar.log") }

// LoadState 读状态文件；不存在/解析失败返回 nil。
func LoadState() *State {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return nil
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

func writeState(s *State) error {
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0o644)
}

// BaseURL 是 sidecar 的 OpenAI 兼容入口。
func (s *State) BaseURL() string { return "http://127.0.0.1:" + strconv.Itoa(s.Port) + "/v1" }

// Healthy 以 800ms 预算探 /health（hook 热路径可接受的快速失败）。
func (s *State) Healthy() bool {
	if s == nil {
		return false
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(s.Port) + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// RequestStart 写 want 标记（幂等）：daemon 下一轮 reconcile 拉起 sidecar。
func RequestStart() {
	_ = os.MkdirAll(registry.Home(), 0o755)
	_ = os.WriteFile(wantPath(), []byte("1"), 0o644)
}

// ClearWant 清除 want 标记。
func ClearWant() { _ = os.Remove(wantPath()) }

// WantPending 报告 want 标记是否存在。
func WantPending() bool {
	_, err := os.Stat(wantPath())
	return err == nil
}

// Touch 更新 last_used（embedding 调用成功后由客户端包装层调用；失败静默）。
func Touch() {
	st := LoadState()
	if st == nil {
		return
	}
	st.LastUsed = time.Now()
	_ = writeState(st)
}
