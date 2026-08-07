package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/daemonx"
	"openknowledge/internal/registry"
)

// 超时预算：健康检查 200ms；转发 9s（kimi UserPromptSubmit 上限 10s，留 1s 余量）。
const (
	healthTimeout  = 200 * time.Millisecond
	forwardTimeout = 9 * time.Second
	spawnDebounce  = 15 * time.Second
)

// SpawnDetached 后台拉起 `ok daemon`；测试可替换。
var SpawnDetached = spawnDetached

func quickClient() *http.Client { return &http.Client{Timeout: healthTimeout} }

// Ensure 在 daemon 不在时后台拉起（15s 防抖，防止多会话同时 spawn 风暴）。
func Ensure() {
	if info, err := daemonx.Load(); err == nil && info.Healthy(quickClient()) {
		return
	}
	mark := daemonx.Path() + ".spawning"
	if fi, err := os.Stat(mark); err == nil && time.Since(fi.ModTime()) < spawnDebounce {
		return
	}
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(mark, []byte("1"), 0o644); err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	_ = SpawnDetached(exe, filepath.Join(registry.Home(), "daemon.log"))
}

// EnsureCurrent 返回健康且指纹一致的 daemon 凭证；
// 不在/不健康 → 拉起；指纹不一致（exe 已升级）→ 旧 daemon shutdown 后拉起新版。
func EnsureCurrent() (*daemonx.Info, bool) {
	info, err := daemonx.Load()
	if err != nil {
		Ensure()
		return nil, false
	}
	if !info.Healthy(quickClient()) {
		Ensure()
		return nil, false
	}
	if cur, err := daemonx.ExeFingerprint(); err != nil || cur != info.Fingerprint {
		info.Shutdown(quickClient())
		_ = daemonx.Remove() // 旧凭证作废，否则 Ensure 见旧 daemon 仍健康会直接返回
		Ensure()
		return nil, false
	}
	return info, true
}

// ForwardHook 把 agent hook 事件转发给 daemon；任何失败都返回 handled=false
// （调用方走本地兜底），绝不让 agent 卡住。format 为输出协议格式（如 "claude"），
// 经 query param 透传给 daemon 侧的 hook.Handle*。
func ForwardHook(name, format string, payload []byte, stdout, stderr io.Writer) (bool, int) {
	info, ok := EnsureCurrent()
	if !ok {
		return false, 0
	}
	url := info.URL() + "/api/hook/" + name
	if format != "" {
		url += "?format=" + format
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return false, 0
	}
	req.Header.Set("X-Ok-Token", info.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: forwardTimeout}).Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, 0
	}
	var hr HookResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		return false, 0
	}
	_, _ = io.WriteString(stdout, hr.Stdout)
	_, _ = io.WriteString(stderr, hr.Stderr)
	return true, hr.Code
}
