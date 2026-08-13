package gui

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
	"openknowledge/internal/registry"
	"openknowledge/internal/setupx"
)

// dlJob 是一个模型下载任务的状态（GUI 轮询展示）。
type dlJob struct {
	ModelID string `json:"model_id"`
	State   string `json:"state"` // downloading|done|error
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Err     string `json:"error"`
	cancel  context.CancelFunc
	mu      sync.Mutex
}

// dlSnapshot 返回当前下载任务快照（优先 downloading；无任务返回零值）。
// 逐字段拷贝避免复制 sync.Mutex（go vet copylocks）。
func (h *Handler) dlSnapshot() *dlJob {
	h.dlMu.Lock()
	defer h.dlMu.Unlock()
	var pick *dlJob
	for _, j := range h.dl {
		pick = j
		if j.State == "downloading" {
			break
		}
	}
	if pick == nil {
		return &dlJob{}
	}
	pick.mu.Lock()
	defer pick.mu.Unlock()
	return &dlJob{ModelID: pick.ModelID, State: pick.State, Done: pick.Done, Total: pick.Total, Err: pick.Err}
}

// apiEmbeddingGet：弹窗全量状态。
func (h *Handler) apiEmbeddingGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	modelsDir := filepath.Join(registry.Home(), "models")
	builtinModels := make([]map[string]any, 0, len(embed.BuiltinModels))
	for _, m := range embed.BuiltinModels {
		builtinModels = append(builtinModels, map[string]any{
			"id": m.ID, "label": m.Label, "size": m.Size, "dim": m.Dim,
			"downloaded": m.Installed(modelsDir),
		})
	}
	profiles := make([]map[string]any, 0, len(cfg.Embedding.Profiles))
	for _, p := range cfg.Embedding.Profiles {
		item := map[string]any{
			"name": p.Name, "type": p.Type, "base_url": p.BaseURL, "model": p.Model,
			"has_key": p.ResolvedAPIKey() != "", "mirror": p.Mirror,
		}
		if p.Type == "builtin" {
			if m := embed.FindBuiltinModel(p.Model); m != nil {
				item["downloaded"] = m.Installed(modelsDir)
				item["dim"] = m.Dim
			}
		}
		profiles = append(profiles, item)
	}
	_, rtErr := embedsidecar.RuntimeServerPath(embedsidecar.DefaultRuntimeDir())
	writeJSON(w, http.StatusOK, map[string]any{
		"active":            cfg.Embedding.Active,
		"runtime_available": rtErr == nil,
		"builtin_models":    builtinModels,
		"download":          h.dlSnapshot(),
		"profiles":          profiles,
	})
}

// apiEmbeddingSave：新增/覆盖保存（不自动激活；api_key 空=保留旧值）。
func (h *Handler) apiEmbeddingSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
		APIKey  string `json:"api_key"`
		Mirror  string `json:"mirror"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "名称不能为空")
		return
	}
	switch req.Type {
	case "openai", "ollama":
		if req.BaseURL == "" || req.Model == "" {
			writeErr(w, http.StatusBadRequest, "base_url 与 model 不能为空")
			return
		}
	case "builtin":
		if embed.FindBuiltinModel(req.Model) == nil {
			writeErr(w, http.StatusBadRequest, "未知内置模型: "+req.Model)
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "type 必须是 openai|ollama|builtin")
		return
	}
	p := config.EmbeddingProfile{
		Name: req.Name, Type: req.Type, BaseURL: req.BaseURL,
		Model: req.Model, APIKey: req.APIKey, Mirror: req.Mirror,
	}
	if err := setupx.SaveEmbeddingProfile(p, false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiEmbeddingActive：设为使用中（builtin 要求模型已下载；成功写 want 预热）。
func (h *Handler) apiEmbeddingActive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != "" {
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		var target *config.EmbeddingProfile
		for i := range cfg.Embedding.Profiles {
			if cfg.Embedding.Profiles[i].Name == req.Name {
				target = &cfg.Embedding.Profiles[i]
			}
		}
		if target == nil {
			writeErr(w, http.StatusBadRequest, "profile 不存在: "+req.Name)
			return
		}
		if target.Type == "builtin" {
			m := embed.FindBuiltinModel(target.Model)
			if m == nil || !m.Installed(filepath.Join(registry.Home(), "models")) {
				writeErr(w, http.StatusBadRequest, "模型未下载，请先下载")
				return
			}
		}
	}
	if err := setupx.SetActiveEmbedding(req.Name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Name != "" {
		embedsidecar.RequestStart() // 非内置也无害：janitor 会忽略并清除
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiEmbeddingDelete：删除 profile（使用中项被删则退回未配置）。
func (h *Handler) apiEmbeddingDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := setupx.DeleteEmbeddingProfile(req.Name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiEmbeddingTest：对指定已保存 profile 做连通性/就绪检查。
func (h *Handler) apiEmbeddingTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var target *config.EmbeddingProfile
	for i := range cfg.Embedding.Profiles {
		if cfg.Embedding.Profiles[i].Name == req.Name {
			target = &cfg.Embedding.Profiles[i]
		}
	}
	if target == nil {
		writeErr(w, http.StatusBadRequest, "profile 不存在: "+req.Name)
		return
	}
	resp := map[string]any{"ok": true, "error": ""}
	if err := setupx.TestEmbeddingProfile(*target, 10*time.Second); err != nil {
		resp["ok"] = false
		resp["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// apiEmbeddingDownload：后台下载内置模型（单任务；重复调用幂等）。
func (h *Handler) apiEmbeddingDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelID string `json:"model_id"`
		Mirror  string `json:"mirror"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	m := embed.FindBuiltinModel(req.ModelID)
	if m == nil {
		writeErr(w, http.StatusBadRequest, "未知内置模型: "+req.ModelID)
		return
	}
	modelsDir := filepath.Join(registry.Home(), "models")
	if m.Installed(modelsDir) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "done"})
		return
	}
	h.dlMu.Lock()
	if _, running := h.dl[req.ModelID]; running {
		h.dlMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "downloading"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &dlJob{ModelID: req.ModelID, State: "downloading", Total: m.Size, cancel: cancel}
	h.dl[req.ModelID] = job
	h.dlMu.Unlock()
	go func() {
		err := embed.Download(ctx, nil, *m, req.Mirror, modelsDir, func(done, total int64) {
			job.mu.Lock()
			job.Done = done
			job.Total = total
			job.mu.Unlock()
		})
		h.dlMu.Lock()
		defer h.dlMu.Unlock()
		job.mu.Lock()
		defer job.mu.Unlock()
		switch {
		case err == nil:
			job.State = "done"
			job.Done = job.Total
		case ctx.Err() != nil:
			delete(h.dl, req.ModelID) // 取消：清空状态（.part 保留可续传）
		default:
			job.State = "error"
			job.Err = err.Error()
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "downloading"})
}

// apiEmbeddingDownloadCancel：取消下载（保留 .part 供续传）。
func (h *Handler) apiEmbeddingDownloadCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelID string `json:"model_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	h.dlMu.Lock()
	if job, ok := h.dl[req.ModelID]; ok && job.cancel != nil {
		job.cancel()
	}
	h.dlMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiOllamaModels：代理探测 Ollama 已安装模型（前端跨域受限，经后端转发）。
func (h *Handler) apiOllamaModels(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base_url")
	if base == "" {
		base = "http://localhost:11434"
	}
	models, err := setupx.ListOllamaModels(base)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}
