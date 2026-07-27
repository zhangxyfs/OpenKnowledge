// Package daemonx 管理 ok daemon 的实例凭证（daemon.json）、健康与版本检查。
// 叶子包：只依赖 registry 与标准库，绝不 import gui/hook/daemon（防循环）。
package daemonx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/registry"
)

// DefaultPort 是 daemon 的首选监听端口（被占则回退随机端口并写入 daemon.json）。
const DefaultPort = 17888

// Info 是 daemon.json 的内容：唯一 daemon 实例的凭证。
type Info struct {
	PID         int    `json:"pid"`
	Port        int    `json:"port"`
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
	StartedAt   string `json:"started_at"`
}

// Path 返回 daemon.json 路径（~/.openknowledge/daemon.json）。
func Path() string { return filepath.Join(registry.Home(), "daemon.json") }

// Load 读取 daemon.json；文件不存在或损坏均返回 error。
func Load() (*Info, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		return nil, err
	}
	var i Info
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}
	if i.Port == 0 || i.Token == "" {
		return nil, errors.New("daemon.json 字段不完整")
	}
	return &i, nil
}

// Save 原子写入 daemon.json（tmp+rename），权限 0600。
func Save(i *Info) error {
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// Remove 删除 daemon.json；不存在视为成功。
func Remove() error {
	if err := os.Remove(Path()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ExeFingerprint 返回当前可执行文件的指纹："路径|size|mtimeUnixNano"。
// exe 升级（重装/重新构建）后指纹必然变化，供版本不一致自动切换。
func ExeFingerprint() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s|%d|%d", filepath.ToSlash(exe), fi.Size(), fi.ModTime().UnixNano()), nil
}

// URL 返回 daemon 的 base URL。
func (i *Info) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", i.Port) }

// Healthy 用给定的 client（调用方控制超时）GET /api/health，200 为 true。
func (i *Info) Healthy(hc *http.Client) bool {
	req, err := http.NewRequest("GET", i.URL()+"/api/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Ok-Token", i.Token)
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Shutdown 尽力而为地请求 daemon 停服（POST /api/shutdown）。
func (i *Info) Shutdown(hc *http.Client) {
	req, err := http.NewRequest("POST", i.URL()+"/api/shutdown", nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Ok-Token", i.Token)
	if resp, err := hc.Do(req); err == nil {
		resp.Body.Close()
	}
}

// StopDaemon 尽力而为地停止当前 daemon 并删除凭证（卸载/手动停止用）。
func StopDaemon() {
	info, err := Load()
	if err != nil {
		return
	}
	info.Shutdown(&http.Client{Timeout: 2 * time.Second})
	_ = Remove()
}
