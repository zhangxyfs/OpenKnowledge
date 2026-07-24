package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/index"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/setupx"
	"openknowledge/internal/store"
)

// Handler 是 GUI 的 HTTP 处理器：/ 与静态资源来自 webDir，/api/* 走令牌鉴权。
type Handler struct {
	mux      *http.ServeMux
	webDir   string
	token    string
	beats    chan<- struct{}
	done     chan struct{}
	doneOnce sync.Once
}

// NewHandler 构建路由。beats 收到每次 /api/heartbeat 的信号（非阻塞，可传 nil）；
// /api/shutdown 会关闭 Done() 返回的通道，由调用方执行 Server.Shutdown。
func NewHandler(webDir, token string, beats chan<- struct{}) *Handler {
	h := &Handler{webDir: webDir, token: token, beats: beats, done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.serveIndex)
	mux.HandleFunc("GET /index.html", h.serveIndex)
	mux.HandleFunc("GET /app.js", h.serveStatic)
	mux.HandleFunc("GET /style.css", h.serveStatic)
	api := func(pattern string, fn http.HandlerFunc) {
		mux.HandleFunc(pattern, h.withAuth(fn))
	}
	api("GET /api/status", h.apiStatus)
	api("GET /api/projects", h.apiProjects)
	api("GET /api/entries", h.apiEntries)
	api("GET /api/entry", h.apiEntryGet)
	api("POST /api/entry", h.apiEntryCreate)
	api("PUT /api/entry", h.apiEntryUpdate)
	api("DELETE /api/entry", h.apiEntryDelete)
	api("GET /api/search", h.apiSearch)
	api("POST /api/approve", h.apiApprove)
	api("GET /api/capture", h.apiCaptureGet)
	api("POST /api/capture", h.apiCaptureSet)
	api("POST /api/heartbeat", h.apiHeartbeat)
	api("POST /api/shutdown", h.apiShutdown)
	api("POST /api/uninstall", h.apiUninstall)
	api("POST /api/setup/hooks", h.apiSetupHooks)
	api("POST /api/setup/skills", h.apiSetupSkills)
	api("POST /api/setup/embedding", h.apiSetupEmbedding)
	api("POST /api/toggle", h.apiToggle)
	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// Done 在收到 /api/shutdown 后关闭。
func (h *Handler) Done() <-chan struct{} { return h.done }

// withAuth 校验 X-Ok-Token 头，失败 401。
func (h *Handler) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ok-Token") != h.token {
			writeErr(w, http.StatusUnauthorized, "缺少或错误的 X-Ok-Token")
			return
		}
		fn(w, r)
	}
}

// serveIndex 返回注入令牌的 index.html（{{TOKEN}} 占位符替换）。
func (h *Handler) serveIndex(w http.ResponseWriter, _ *http.Request) {
	data, err := os.ReadFile(filepath.Join(h.webDir, "index.html"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(data), "{{TOKEN}}", h.token)))
}

// serveStatic 只服务 webDir 白名单内的静态文件（路由本身是字面量，无路径参数）。
func (h *Handler) serveStatic(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(h.webDir, filepath.Base(r.URL.Path)))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ---------- 项目解析 ----------

// findProject 按名称解析注册表项目；found=false 表示未注册。
func findProject(name string) (st *store.Store, found bool, err error) {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return nil, false, err
	}
	for _, p := range reg.Projects {
		if p.Name == name {
			return store.New(filepath.Join(registry.Home(), "projects", name)), true, nil
		}
	}
	return nil, false, nil
}

// resolveProject 是 findProject 的 HTTP 层封装：缺参 400、未注册 404。
func resolveProject(w http.ResponseWriter, name string) *store.Store {
	if name == "" {
		writeErr(w, http.StatusBadRequest, "缺少 project 参数")
		return nil
	}
	st, found, err := findProject(name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	if !found {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("项目未注册: %s", name))
		return nil
	}
	return st
}

// validEntryFile 校验条目文件参数：必须是不含 ".." 与路径分隔符的 .md 基本名。
func validEntryFile(f string) bool {
	return strings.HasSuffix(f, ".md") &&
		!strings.Contains(f, "..") &&
		filepath.Base(f) == f &&
		!strings.ContainsAny(f, `/\`)
}

// entryPath 校验并拼接条目文件路径；失败时已写错误响应，返回空串。
func entryPath(w http.ResponseWriter, st *store.Store, file string) string {
	if !validEntryFile(file) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法条目文件名: %q", file))
		return ""
	}
	return filepath.Join(st.KnowledgeDir(), file)
}

// syncIndex 以无 embedding 客户端模式同步索引库；损坏条目警告不视为失败。
func syncIndex(st *store.Store) error {
	db, err := index.Open(st.KbPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Sync(st.KnowledgeDir(), nil); err != nil {
		var corrupt *index.CorruptEntriesError
		if errors.As(err, &corrupt) {
			return nil
		}
		return err
	}
	return nil
}

// exePath 返回当前可执行文件的真实路径（解析符号链接）。
func exePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// ---------- JSON 类型 ----------

type projectJSON struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

type entrySummaryJSON struct {
	File      string   `json:"file"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Tags      []string `json:"tags"`
	Mandatory bool     `json:"mandatory"`
	Draft     bool     `json:"draft"`
	Summary   string   `json:"summary"`
}

type entryDetailJSON struct {
	entrySummaryJSON
	Body string `json:"body"`
}

type entryRequest struct {
	Project   string   `json:"project"`
	File      string   `json:"file"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Tags      []string `json:"tags"`
	Mandatory bool     `json:"mandatory"`
	Summary   string   `json:"summary"`
	Body      string   `json:"body"`
}

func summaryOf(e *entry.Entry) entrySummaryJSON {
	tags := e.Tags
	if tags == nil {
		tags = []string{}
	}
	return entrySummaryJSON{
		File:      e.FileName(),
		Title:     e.Title,
		Type:      e.Type,
		Tags:      tags,
		Mandatory: e.Mandatory,
		Draft:     e.Draft,
		Summary:   e.Summary,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("JSON 解析失败: %v", err))
		return false
	}
	return true
}

// ---------- 状态与项目 ----------

func (h *Handler) apiStatus(w http.ResponseWriter, _ *http.Request) {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	projects := make([]projectJSON, 0, len(reg.Projects))
	for _, p := range reg.Projects {
		projects = append(projects, projectJSON{Name: p.Name, Paths: p.Paths})
	}
	hooksInstalled := false
	if data, err := os.ReadFile(filepath.Join(setupx.KimiHome(), "config.toml")); err == nil {
		hooksInstalled = strings.Contains(string(data), setupx.MarkerBegin)
	}
	skillsInstalled := true
	for _, name := range []string{"openknowledge-init", "openknowledge-on", "openknowledge-off", "openknowledge-propose", "openknowledge-capture"} {
		if _, err := os.Stat(filepath.Join(setupx.SkillsHome(), name, "SKILL.md")); err != nil {
			skillsInstalled = false
			break
		}
	}
	embeddingConfigured := false
	if cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml")); err == nil {
		embeddingConfigured = cfg.Embedding.BaseURL != "" && cfg.Embedding.ResolvedAPIKey() != ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projects":            projects,
		"hooksInstalled":      hooksInstalled,
		"skillsInstalled":     skillsInstalled,
		"embeddingConfigured": embeddingConfigured,
		"disabled":            registry.HooksDisabled(),
	})
}

func (h *Handler) apiProjects(w http.ResponseWriter, _ *http.Request) {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	projects := make([]projectJSON, 0, len(reg.Projects))
	for _, p := range reg.Projects {
		projects = append(projects, projectJSON{Name: p.Name, Paths: p.Paths})
	}
	writeJSON(w, http.StatusOK, projects)
}

// ---------- 条目 ----------

func (h *Handler) apiEntries(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	entries, err := entry.Load(st.KnowledgeDir())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]entrySummaryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, summaryOf(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) apiEntryGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	path := entryPath(w, st, r.URL.Query().Get("file"))
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "条目不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	e, err := entry.Parse(data)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	e.Path = path
	writeJSON(w, http.StatusOK, entryDetailJSON{entrySummaryJSON: summaryOf(e), Body: e.Body})
}

// validateEntryRequest 校验写请求公共字段；失败时已写错误响应。
func validateEntryRequest(w http.ResponseWriter, req *entryRequest) *store.Store {
	st := resolveProject(w, req.Project)
	if st == nil {
		return nil
	}
	if strings.TrimSpace(req.Title) == "" {
		writeErr(w, http.StatusBadRequest, "title 不能为空")
		return nil
	}
	if !entry.ValidType(req.Type) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法 type %q（rule|pitfall|note|reference）", req.Type))
		return nil
	}
	return st
}

// writeEntry 统一写流程：序列化 → 写盘 → 同步索引 → 返回列表项。
func writeEntry(w http.ResponseWriter, st *store.Store, path string, req *entryRequest) {
	e := &entry.Entry{
		Title:     strings.TrimSpace(req.Title),
		Type:      req.Type,
		Tags:      req.Tags,
		Mandatory: req.Mandatory,
		Summary:   req.Summary,
		Body:      strings.TrimSpace(req.Body),
		Path:      path,
	}
	if err := os.WriteFile(path, e.Serialize(), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := syncIndex(st); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("索引同步失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, summaryOf(e))
}

func (h *Handler) apiEntryCreate(w http.ResponseWriter, r *http.Request) {
	var req entryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := validateEntryRequest(w, &req)
	if st == nil {
		return
	}
	slug := entry.Slug(req.Title)
	if slug == "" {
		writeErr(w, http.StatusBadRequest, "标题无法生成有效文件名")
		return
	}
	if err := os.MkdirAll(st.KnowledgeDir(), 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	path := filepath.Join(st.KnowledgeDir(), slug+".md")
	if _, err := os.Stat(path); err == nil {
		writeErr(w, http.StatusConflict, fmt.Sprintf("条目已存在: %s", slug+".md"))
		return
	}
	writeEntry(w, st, path, &req)
}

func (h *Handler) apiEntryUpdate(w http.ResponseWriter, r *http.Request) {
	var req entryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := validateEntryRequest(w, &req)
	if st == nil {
		return
	}
	path := entryPath(w, st, req.File)
	if path == "" {
		return
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "条目不存在")
		return
	}
	writeEntry(w, st, path, &req)
}

func (h *Handler) apiEntryDelete(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	path := entryPath(w, st, r.URL.Query().Get("file"))
	if path == "" {
		return
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "条目不存在")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := syncIndex(st); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("索引同步失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 检索 ----------

func (h *Handler) apiSearch(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	q := r.URL.Query().Get("q")
	type hitJSON struct {
		File  string  `json:"file"`
		Title string  `json:"title"`
		Score float64 `json:"score"`
	}
	out := make([]hitJSON, 0)
	if strings.TrimSpace(q) == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db, err := index.Open(st.KbPath())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	hits, err := db.Query(retrieve.Terms(q), nil, cfg.Retrieve)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, hit := range hits {
		out = append(out, hitJSON{File: hit.Filename, Title: hit.Title, Score: hit.Score})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- 草稿批准与捕获模式 ----------

// embeddingClientFor 按合并配置构建 embedding 客户端；未配置返回 nil。
// 与 cli.embeddingClient 同语义，供 approve 批准草稿时补算向量。
func embeddingClientFor(st *store.Store) embed.Client {
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return nil
	}
	key := cfg.Embedding.ResolvedAPIKey()
	if key == "" || cfg.Embedding.BaseURL == "" {
		return nil
	}
	return &embed.OpenAIClient{
		BaseURL: cfg.Embedding.BaseURL,
		APIKey:  key,
		Model:   cfg.Embedding.Model,
		Timeout: time.Duration(cfg.Embedding.TimeoutSec) * time.Second,
	}
}

// syncApprove 批准后的索引同步：带 embedding 客户端算向量，失败降级为只同步
// INDEX（与 cli.afterAdd 同策略）；损坏条目警告不视为失败。
func syncApprove(st *store.Store) error {
	db, err := index.Open(st.KbPath())
	if err != nil {
		return err
	}
	defer db.Close()
	client := embeddingClientFor(st)
	if err := db.Sync(st.KnowledgeDir(), client); err != nil {
		var corrupt *index.CorruptEntriesError
		if errors.As(err, &corrupt) {
			return nil
		}
		if client == nil {
			return err
		}
		// embedding 失败：降级为只同步 INDEX，向量稍后 ok index 补齐
		if err2 := db.Sync(st.KnowledgeDir(), nil); err2 != nil && !errors.As(err2, &corrupt) {
			return err2
		}
	}
	return nil
}

// apiApprove 等价 ok approve：草稿转正（draft=false）→ 同步索引与向量 → 返回更新后条目。
func (h *Handler) apiApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		File    string `json:"file"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	path := entryPath(w, st, req.File)
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("条目不存在: %s", req.File))
		return
	}
	e, err := entry.Parse(data)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !e.Draft {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("不是草稿条目: %s", req.File))
		return
	}
	e.Draft = false
	// Sync 的 diff 按秒级 mtime 判断变化；propose 后同一秒内 approve 会被误判为
	// 未变化而跳过重建，此时手动把 mtime 推进一秒（同 cli.Approve）。
	oldInfo, statErr := os.Stat(path)
	if err := os.WriteFile(path, e.Serialize(), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if statErr == nil {
		if newInfo, err := os.Stat(path); err == nil && newInfo.ModTime().Unix() == oldInfo.ModTime().Unix() {
			t := oldInfo.ModTime().Add(time.Second)
			_ = os.Chtimes(path, t, t)
		}
	}
	if err := syncApprove(st); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("索引同步失败: %v", err))
		return
	}
	e.Path = path
	writeJSON(w, http.StatusOK, summaryOf(e))
}

// apiCaptureGet 返回项目合并配置中的捕获模式与 turn_interval。
func (h *Handler) apiCaptureGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":          cfg.Capture.Mode,
		"turn_interval": cfg.Capture.TurnInterval,
	})
}

// apiCaptureSet 设置 capture 模式与轮次间隔：写项目 config.toml 的 [capture] 小节。
// mode 为空表示保持不变；turn_interval 为 0 表示保持不变。
func (h *Handler) apiCaptureSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project      string `json:"project"`
		Mode         string `json:"mode"`
		TurnInterval int    `json:"turn_interval"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	if req.Mode != "" && req.Mode != "propose" && req.Mode != "auto" {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法 capture 模式 %q（propose|auto）", req.Mode))
		return
	}
	if req.TurnInterval < 0 || req.TurnInterval > 100 {
		writeErr(w, http.StatusBadRequest, "turn_interval 必须在 1~100 之间")
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	mode := cfg.Capture.Mode
	if req.Mode != "" {
		mode = req.Mode
	}
	interval := cfg.Capture.TurnInterval
	if req.TurnInterval > 0 {
		interval = req.TurnInterval
	}
	if err := config.SetCapture(st.ConfigPath(), mode, interval, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "turn_interval": interval})
}

// ---------- 心跳与停服 ----------

func (h *Handler) apiHeartbeat(w http.ResponseWriter, _ *http.Request) {
	select {
	case h.beats <- struct{}{}:
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) apiShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	h.doneOnce.Do(func() { close(h.done) })
}

// apiUninstall 卸载集成部分（hooks 配置、技能、全局 embedding），KB 数据保留。
func (h *Handler) apiUninstall(w http.ResponseWriter, _ *http.Request) {
	r, err := setupx.Uninstall()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"hooks_removed":     r.HooksRemoved,
		"skills_removed":    r.SkillsRemoved,
		"embedding_removed": r.EmbeddingRemoved,
	})
}

// ---------- 安装与配置 ----------

func (h *Handler) apiSetupHooks(w http.ResponseWriter, _ *http.Request) {
	exe, err := exePath()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfgPath := filepath.Join(setupx.KimiHome(), "config.toml")
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = os.WriteFile(cfgPath+".bak-openknowledge", data, 0o644)
	}
	if err := setupx.UpsertHooksBlock(cfgPath, setupx.HooksBlockFor(exe)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": cfgPath})
}

func (h *Handler) apiSetupSkills(w http.ResponseWriter, _ *http.Request) {
	exe, err := exePath()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := setupx.InstallSkills(exe); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dir": setupx.SkillsHome()})
}

func (h *Handler) apiSetupEmbedding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
		APIKey  string `json:"api_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" {
		writeErr(w, http.StatusBadRequest, "api_key 不能为空")
		return
	}
	if req.BaseURL == "" {
		req.BaseURL = "https://api.openai.com/v1"
	}
	if req.Model == "" {
		req.Model = "text-embedding-3-small"
	}
	result := func(err error) {
		resp := map[string]any{"ok": err == nil, "error": ""}
		if err != nil {
			resp["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	}
	if err := setupx.SaveEmbedding(req.BaseURL, req.Model, req.APIKey); err != nil {
		result(err)
		return
	}
	result(setupx.TestEmbedding(req.BaseURL, req.Model, req.APIKey))
}

func (h *Handler) apiToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var err error
	if req.On {
		err = setupx.Enable()
	} else {
		err = setupx.Disable()
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "disabled": !req.On})
}
