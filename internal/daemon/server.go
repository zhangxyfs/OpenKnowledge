// Package daemon 是 ok 的常驻进程编排：HTTP mux、单实例运行、
// 后台拉起、hook 转发与 GUI 打开。
package daemon

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"openknowledge/internal/hook"
)

// HookResponse 是 /api/hook/* 的响应：客户端据此还原 stdout/stderr 与退出码。
type HookResponse struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

// NewMux 组装 daemon 的全部路由：/api/health、/api/hook/* 由本包处理，
// 其余（GUI 静态页与管理 API）委托给 gh。
func NewMux(gh http.Handler, token, fingerprint string) http.Handler {
	mux := http.NewServeMux()
	auth := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Ok-Token") != token {
				http.Error(w, `{"error":"缺少或错误的 X-Ok-Token"}`, http.StatusUnauthorized)
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /api/health", auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"fingerprint": fingerprint})
	}))
	mux.HandleFunc("POST /api/hook/prompt", auth(hookHandler(func(body []byte) HookResponse {
		var out strings.Builder
		code := hook.HandlePrompt(bytes.NewReader(body), &out)
		return HookResponse{Stdout: out.String(), Code: code}
	})))
	mux.HandleFunc("POST /api/hook/post-tool", auth(hookHandler(func(body []byte) HookResponse {
		return HookResponse{Code: hook.HandlePostTool(bytes.NewReader(body))}
	})))
	mux.HandleFunc("POST /api/hook/stop", auth(hookHandler(func(body []byte) HookResponse {
		var errOut strings.Builder
		code := hook.HandleStop(bytes.NewReader(body), &errOut)
		return HookResponse{Stderr: errOut.String(), Code: code}
	})))
	mux.Handle("/", gh)
	return mux
}

// hookHandler 把"读 body → 业务函数 → HookResponse JSON"的模板收敛到一处。
func hookHandler(fn func([]byte) HookResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			body = nil
		}
		writeJSON(w, fn(body))
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newToken 生成 16 字节随机的 hex 令牌（32 字符）。
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
