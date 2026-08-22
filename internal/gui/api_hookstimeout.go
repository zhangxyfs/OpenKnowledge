package gui

import (
	"net/http"

	"openknowledge/internal/agentx"
	"openknowledge/internal/setupx"
)

// apiHooksTimeoutSet 只写全局 hook 超时（不重装 hooks——那是 /api/setup/hooks 的职责）；
// 1~60 秒，非法 400。下次安装/自愈 hooks 时生效于新写入的配置。
func (h *Handler) apiHooksTimeoutSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TimeoutSec int `json:"timeout_sec"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.TimeoutSec < 1 || req.TimeoutSec > 60 {
		writeErr(w, http.StatusBadRequest, "timeout_sec 必须是 1~60 的整数")
		return
	}
	if err := setupx.SaveHooksTimeout(req.TimeoutSec); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "timeout_sec": req.TimeoutSec})
}

// apiSetupHooksRemove 卸载单个 agent 的 hooks（agentx.RemoveHooks 语义：
// 幂等，返回是否实际移除）。未知 agent → 400。
func (h *Handler) apiSetupHooksRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent string `json:"agent"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	a, ok := agentx.Find(req.Agent)
	if !ok {
		writeErr(w, http.StatusBadRequest, "未知 agent: "+req.Agent)
		return
	}
	removed, err := a.RemoveHooks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed})
}
