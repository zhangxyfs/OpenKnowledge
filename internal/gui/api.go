package gui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"openknowledge/internal/agentx"
	"openknowledge/internal/backup"
	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedx"
	"openknowledge/internal/entry"
	"openknowledge/internal/fsx"
	"openknowledge/internal/index"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/setupx"
	"openknowledge/internal/store"
	"openknowledge/internal/version"
	"openknowledge/internal/wiki"
)

// Handler 是 GUI 的 HTTP 处理器：/ 与静态资源来自 webDir，/api/* 走令牌鉴权。
type Handler struct {
	mux      *http.ServeMux
	webDir   string
	token    string
	beats    chan<- struct{}
	done     chan struct{}
	doneOnce sync.Once
	dlMu     sync.Mutex
	dl       map[string]*dlJob
}

// NewHandler 构建路由。beats 收到每次 /api/heartbeat 的信号（非阻塞，可传 nil）；
// /api/shutdown 会关闭 Done() 返回的通道，由调用方执行 Server.Shutdown。
func NewHandler(webDir, token string, beats chan<- struct{}) *Handler {
	h := &Handler{webDir: webDir, token: token, beats: beats, done: make(chan struct{}), dl: map[string]*dlJob{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.serveIndex)
	mux.HandleFunc("GET /index.html", h.serveIndex)
	mux.HandleFunc("GET /app.js", h.serveStatic)
	mux.HandleFunc("GET /style.css", h.serveStatic)
	mux.HandleFunc("GET /favicon.ico", h.serveStatic)
	// 使用帮助页：字面量路由即白名单，前端"使用帮助"卡 fetch 后复用 changelog 弹窗渲染
	mux.HandleFunc("GET /help.md", h.serveStatic)
	api := func(pattern string, fn http.HandlerFunc) {
		mux.HandleFunc(pattern, h.withAuth(fn))
	}
	api("GET /api/status", h.apiStatus)
	api("GET /api/logs", h.apiLogs)
	api("GET /api/projects", h.apiProjects)
	api("GET /api/entries", h.apiEntries)
	api("GET /api/entry", h.apiEntryGet)
	api("POST /api/entry", h.apiEntryCreate)
	api("PUT /api/entry", h.apiEntryUpdate)
	api("DELETE /api/entry", h.apiEntryDelete)
	api("DELETE /api/project", h.apiProjectDelete)
	api("GET /api/search", h.apiSearch)
	api("POST /api/approve", h.apiApprove)
	api("POST /api/entry/archive", h.apiEntryArchive)
	api("GET /api/capture", h.apiCaptureGet)
	api("POST /api/capture", h.apiCaptureSet)
	api("GET /api/gate", h.apiGateGet)
	api("POST /api/gate", h.apiGateSet)
	api("GET /api/inject", h.apiInjectGet)
	api("POST /api/inject", h.apiInjectSet)
	api("GET /api/retrieve", h.apiRetrieveGet)
	api("POST /api/retrieve", h.apiRetrieveSet)
	api("GET /api/llm", h.apiLLMGet)
	api("POST /api/llm/profile", h.apiLLMProfileSave)
	api("POST /api/llm/delete", h.apiLLMProfileDelete)
	api("POST /api/llm/active", h.apiLLMActive)
	api("POST /api/llm/test", h.apiLLMTest)
	api("POST /api/entry/optimize", h.apiEntryOptimize)
	api("GET /api/project/branch-info", h.apiProjectBranchInfo)
	api("POST /api/heartbeat", h.apiHeartbeat)
	api("POST /api/shutdown", h.apiShutdown)
	api("POST /api/uninstall", h.apiUninstall)
	api("POST /api/setup/hooks", h.apiSetupHooks)
	api("POST /api/setup/skills", h.apiSetupSkills)
	api("GET /api/setup/embedding", h.apiEmbeddingGet)
	api("POST /api/setup/embedding/profile", h.apiEmbeddingSave)
	api("DELETE /api/setup/embedding/profile", h.apiEmbeddingDelete)
	api("POST /api/setup/embedding/active", h.apiEmbeddingActive)
	api("POST /api/setup/embedding/test", h.apiEmbeddingTest)
	api("POST /api/setup/embedding/download", h.apiEmbeddingDownload)
	api("POST /api/setup/embedding/download/cancel", h.apiEmbeddingDownloadCancel)
	api("POST /api/setup/embedding/models-dir", h.apiEmbeddingModelsDirSet)
	api("POST /api/setup/embedding/open-models-dir", h.apiEmbeddingOpenModelsDir)
	api("GET /api/setup/embedding/ollama-models", h.apiOllamaModels)
	api("POST /api/reasonix/enforce-mode", h.apiReasonixEnforceMode)
	api("POST /api/toggle", h.apiToggle)
	api("GET /api/changelog", h.apiChangelog)
	api("POST /api/changelog/seen", h.apiChangelogSeen)
	api("GET /api/export", h.apiExport)
	api("POST /api/import", h.apiImport)
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
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(data), "{{TOKEN}}", h.token)))
}

// serveStatic 只服务 webDir 白名单内的静态文件（路由本身是字面量，无路径参数）。
// no-cache 强制每次重验证（配合 Last-Modified 走 304，成本极低），防止升级后
// 浏览器继续用旧 app.js 与新 index.html 错配（agents 下拉失去数据填充）。
func (h *Handler) serveStatic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
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

// resolveProject 是 findProject 的 HTTP 层封装：缺参 400、形状非法 400、未注册 404。
func resolveProject(w http.ResponseWriter, name string) *store.Store {
	if name == "" {
		writeErr(w, http.StatusBadRequest, "缺少 project 参数")
		return nil
	}
	if !validProjectName(name) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法项目名: %q", name))
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

// validProjectName 校验项目名形状：必须是不含路径分隔符与盘符的基本名。
// 注册表名字来自 ok init 的裸输入（无形状约束），registry.toml 也可能被手改；
// GUI 层拿名字拼 projects/<name>/ 前，穿越段名字（../、绝对路径、C: 盘符）一律拒绝。
func validProjectName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name != filepath.Base(name) {
		return false
	}
	return !strings.ContainsAny(name, `\/:`)
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

// syncOpts 取项目合并配置的索引渲染选项（[index] max_lines）；配置读取失败
// 按零值（渲染层默认 50），不阻断同步。
func syncOpts(st *store.Store) index.SyncOptions {
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return index.SyncOptions{}
	}
	return index.SyncOptions{MaxLines: cfg.Index.MaxLines}
}

// syncIndex 以无 embedding 客户端模式同步索引库；损坏条目警告不视为失败。
func syncIndex(st *store.Store) error {
	db, err := index.Open(st.KbPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Sync(st.KnowledgeDir(), nil, syncOpts(st)); err != nil {
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
	Name       string   `json:"name"`
	Paths      []string `json:"paths"`
	LastUpdate int64    `json:"last_update"` // kb.db mtime（unix 秒），无索引库为 0；项目下拉按它降序
}

// listProjects 汇总注册表项目：附带 kb.db mtime 作为最近更新时间，按它降序
// （最近有知识写入的项目排前面；无索引库的垫底，同名按名称稳定排序）。
func listProjects(reg *registry.Registry) []projectJSON {
	projects := make([]projectJSON, 0, len(reg.Projects))
	for _, p := range reg.Projects {
		var last int64
		kb := filepath.Join(registry.Home(), "projects", p.Name, "kb.db")
		if fi, err := os.Stat(kb); err == nil {
			last = fi.ModTime().Unix()
		}
		projects = append(projects, projectJSON{Name: p.Name, Paths: p.Paths, LastUpdate: last})
	}
	sort.SliceStable(projects, func(i, j int) bool {
		if projects[i].LastUpdate != projects[j].LastUpdate {
			return projects[i].LastUpdate > projects[j].LastUpdate
		}
		return projects[i].Name < projects[j].Name
	})
	return projects
}

type entrySummaryJSON struct {
	File      string   `json:"file"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Tags      []string `json:"tags"`
	Mandatory bool     `json:"mandatory"`
	Draft     bool     `json:"draft"`
	Archived  bool     `json:"archived"`
	Summary   string   `json:"summary"`
	Mtime     int64    `json:"mtime"` // 文件修改时间（unix 秒），界面排序/展示用
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
	var mtime int64
	if e.Path != "" {
		if fi, err := os.Stat(e.Path); err == nil {
			mtime = fi.ModTime().Unix()
		}
	}
	return entrySummaryJSON{
		File:      e.FileName(),
		Title:     e.Title,
		Type:      e.Type,
		Tags:      tags,
		Mandatory: e.Mandatory,
		Draft:     e.Draft,
		Archived:  e.Archived,
		Summary:   e.Summary,
		Mtime:     mtime,
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
	projects := listProjects(reg)
	agents := make([]map[string]any, 0, len(agentx.All()))
	for _, a := range agentx.All() {
		agents = append(agents, map[string]any{
			"id":             a.ID(),
			"name":           a.DisplayName(),
			"detected":       a.Detect(),
			"hooksInstalled": a.HooksInstalled(),
		})
	}
	skillsInstalled := true
	for _, home := range setupx.SkillDirs() {
		for _, name := range setupx.SkillNames() {
			if _, err := os.Stat(filepath.Join(home, name, "SKILL.md")); err != nil {
				skillsInstalled = false
				break
			}
		}
	}
	embeddingConfigured := false
	embedding := map[string]any{"configured": false}
	hooksTimeout := 10
	if cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml")); err == nil {
		if p := cfg.Embedding.ActiveProfile(); p != nil {
			embeddingConfigured = true
			embedding = map[string]any{
				"configured": true, "name": p.Name, "type": p.Type,
				"model": p.Model, "base_url": p.BaseURL,
			}
			if p.Type == "builtin" {
				if m := embed.FindBuiltinModel(p.Model); m != nil {
					embedding["model_label"] = m.Label
					embedding["dim"] = m.Dim
				}
			}
		}
		if cfg.Hooks.TimeoutSec > 0 {
			hooksTimeout = cfg.Hooks.TimeoutSec
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projects":            projects,
		"agents":              agents,
		"skillsInstalled":     skillsInstalled,
		"embeddingConfigured": embeddingConfigured,
		"embedding":           embedding,
		"hooksTimeout":        hooksTimeout,
		"rxEnforceMode":       setupx.ReasonixEnforceMode(),
		"disabled":            registry.HooksDisabled(),
		"app_version":         version.Version,
		"home":                registry.Home(),
	})
}

// apiLogs 返回三类日志（ok / daemon / sidecar）的尾部行，供"日志"页轮询展示。
// 每行带来源（src）与语义标记（含 semantic/embed 关键字）；过滤与高亮在前端做。
// 只读、最多回传 tail 行（1~2000，默认 400），不提供清空/写入。
// 轮询优化：客户端带 sig（上轮各文件 size-mtime 签名）且与当前一致时，
// 只回 {unchanged:true}——大文件下省去重复的 256KB 读取、整包传输与前端全量重绘。
func (h *Handler) apiLogs(w http.ResponseWriter, r *http.Request) {
	tail := 400
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			tail = n
		}
	}
	home := registry.Home()
	sources := []struct{ key, file string }{
		{"ok", "ok.log"}, {"daemon", "daemon.log"}, {"sidecar", "embed-sidecar.log"},
	}
	// 先 stat 出签名：不读内容（分隔符用 . 与 ,，避开 URL query 里 ; 的解析歧义）
	files := make([]map[string]any, 0, len(sources))
	var sigB strings.Builder
	for _, s := range sources {
		p := filepath.Join(home, s.file)
		fi, err := os.Stat(p)
		if err != nil {
			files = append(files, map[string]any{"name": s.key, "exists": false})
			fmt.Fprintf(&sigB, "%s.-,", s.key)
			continue
		}
		files = append(files, map[string]any{
			"name": s.key, "exists": true,
			"size": fi.Size(), "mtime": fi.ModTime().UnixMilli(),
		})
		fmt.Fprintf(&sigB, "%s.%d-%d,", s.key, fi.Size(), fi.ModTime().UnixMilli())
	}
	sig := sigB.String()
	if r.URL.Query().Get("sig") == sig {
		writeJSON(w, http.StatusOK, map[string]any{"unchanged": true, "files": files, "sig": sig})
		return
	}
	lines := make([]map[string]any, 0, tail)
	for _, s := range sources {
		lines = append(lines, tailLines(filepath.Join(home, s.file), tail, s.key)...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "lines": lines, "sig": sig})
}

// tailLines 读文件尾部（最多 256KB）并按行返回，丢弃截断的首行。
func tailLines(path string, tail int, src string) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const maxRead = 256 * 1024
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	off := fi.Size() - maxRead
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	if off > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}
	raw := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(raw) > tail {
		raw = raw[len(raw)-tail:]
	}
	out := make([]map[string]any, 0, len(raw))
	for _, line := range raw {
		lower := strings.ToLower(line)
		out = append(out, map[string]any{
			"src":      src,
			"semantic": strings.Contains(lower, "semantic") || strings.Contains(lower, "embed"),
			"text":     line,
		})
	}
	return out
}

// apiExport 导出知识库 zip（project 缺省 all）。
func (h *Handler) apiExport(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = "all"
	}
	if project != "all" {
		reg, err := registry.Load(registry.DefaultPath())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		found := false
		for _, p := range reg.Projects {
			if p.Name == project {
				found = true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, "项目不存在: "+project)
			return
		}
	}
	filename := "openknowledge-backup-" + project + "-" + time.Now().Format("20060102") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if err := backup.Export(w, project); err != nil {
		log.Printf("export %s: %v", project, err) // 响应头已发，只能记日志
	}
}

// apiImport 导入知识库 zip（multipart 字段 file）。
func (h *Handler) apiImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxSize+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少 file 字段或超过大小上限")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取上传失败或超过 32MB 上限")
		return
	}
	rep, err := backup.Import(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if errors.Is(err, backup.ErrBadPackage) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (h *Handler) apiProjects(w http.ResponseWriter, _ *http.Request) {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	projects := listProjects(reg)
	writeJSON(w, http.StatusOK, projects)
}

// apiProjectDelete 删除项目知识库：先注销注册表（Save 失败则中止、目录不动），
// 再删除 projects/<name>/ 目录；目录删除失败时项目已注销，返回 warning 供前端提示手动清理。
// 目录名取注册表匹配后的 p.Name 而非用户原始输入，但注册名本身可能被手改或经
// ok init 带入穿越段——RemoveAll 落地前仍须过 validProjectName 形状校验。
func (h *Handler) apiProjectDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("project")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "缺少 project 参数")
		return
	}
	if !validProjectName(name) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法项目名: %q", name))
		return
	}
	// 锁内读-改-写：与并发的 ok init / 备份恢复互斥，防止互相覆盖丢注册
	found := false
	err := registry.Update(func(reg *registry.Registry) error {
		for _, p := range reg.Projects {
			if p.Name == name {
				name = p.Name // 以注册表登记名为准
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("项目未注册: %q", name)
		}
		reg.RemoveProject(name)
		return nil
	})
	if err != nil {
		if !found {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("注册表保存失败（未删除任何数据）: %v", err))
		return
	}
	dir := filepath.Join(registry.Home(), "projects", name)
	if err := os.RemoveAll(dir); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"warning": fmt.Sprintf("目录删除失败: %v", err),
			"dir":     dir,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
// created 仅新建路径传入（YYYY-MM-DD）；更新既有条目传 ""，此时从盘上原条目
// 继承 created/draft/archived——不继承会让"编辑草稿"静默转正、"编辑归档条目"
// 静默取消归档（entryRequest 不带这三个字段，GUI 编辑器不管生命周期标记）。
func writeEntry(w http.ResponseWriter, st *store.Store, path string, req *entryRequest, created string) {
	var draft, archived bool
	var prev time.Time
	if created == "" {
		if data, err := os.ReadFile(path); err == nil {
			if old, err := entry.Parse(data); err == nil {
				created = old.Created
				draft = old.Draft
				archived = old.Archived
			}
		}
		// 同秒连续保存会被 Sync 的秒级 mtime diff 判未变化，写后推进一秒兜底
		if fi, err := os.Stat(path); err == nil {
			prev = fi.ModTime()
		}
	}
	e := &entry.Entry{
		Title:     strings.TrimSpace(req.Title),
		Type:      req.Type,
		Tags:      req.Tags,
		Mandatory: req.Mandatory,
		Summary:   req.Summary,
		Body:      strings.TrimSpace(req.Body),
		Created:   created,
		Draft:     draft,
		Archived:  archived,
		Path:      path,
	}
	if err := fsx.WriteFile(path, e.Serialize(), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fsx.BumpMtime(path, prev)
	if err := syncIndex(st); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("索引同步失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, summaryOf(e))
}

// guiBornTag 返回 GUI 新建条目时应自动写入的 born 标签（born:<分支>）。
// 分支按注册表项目第一个路径（Paths[0]）探测——daemon 的 cwd 未必是项目目录；
// auto_born 关闭、非 git 或探测失败返回 ""（fail-open，不阻断创建）。
func guiBornTag(st *store.Store, project string) string {
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil || !cfg.Provenance.AutoBorn {
		return ""
	}
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return ""
	}
	for _, p := range reg.Projects {
		if p.Name == project {
			if len(p.Paths) == 0 {
				return ""
			}
			if b := wiki.CurrentBranch(p.Paths[0]); b != "" {
				return "born:" + b
			}
			return ""
		}
	}
	return ""
}

// hasBornTag 报告 tags 中已存在 born 标签（用户表单显式传入时自动记录不覆盖）。
func hasBornTag(tags []string) bool {
	for _, t := range tags {
		if strings.HasPrefix(t, "born:") {
			return true
		}
	}
	return false
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
	// 新建条目自动记 born（与 ok add 同语义；仅创建路径，更新不补标）
	if !hasBornTag(req.Tags) {
		if bt := guiBornTag(st, req.Project); bt != "" {
			req.Tags = append(req.Tags, bt)
		}
	}
	writeEntry(w, st, path, &req, time.Now().Format("2006-01-02"))
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
	writeEntry(w, st, path, &req, "")
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
// 构造收口在 embedx，供 approve 批准草稿时补算向量（索引路径：超时下限 120s）。
func embeddingClientFor(st *store.Store) embed.Client {
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return nil
	}
	return embedx.ClientForIndex(cfg)
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
	if err := db.Sync(st.KnowledgeDir(), client, syncOpts(st)); err != nil {
		var corrupt *index.CorruptEntriesError
		if errors.As(err, &corrupt) {
			return nil
		}
		if client == nil {
			return err
		}
		// embedding 失败：降级为只同步 INDEX，向量稍后 ok index 补齐
		if err2 := db.Sync(st.KnowledgeDir(), nil, syncOpts(st)); err2 != nil && !errors.As(err2, &corrupt) {
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
	var prev time.Time
	if fi, statErr := os.Stat(path); statErr == nil {
		prev = fi.ModTime()
	}
	if err := fsx.WriteFile(path, e.Serialize(), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fsx.BumpMtime(path, prev)
	if err := syncApprove(st); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("索引同步失败: %v", err))
		return
	}
	e.Path = path
	writeJSON(w, http.StatusOK, summaryOf(e))
}

// apiEntryArchive 等价 ok archive [--undo]：翻转条目的归档标记（归档不进 INDEX
// 主列表与 Wiki 目录、不参与 mandatory 注入，仍可检索）→ 同步索引 → 返回更新后条目。
func (h *Handler) apiEntryArchive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		File    string `json:"file"`
		Undo    bool   `json:"undo"`
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
	e.Archived = !req.Undo
	// 同秒连点归档/恢复会被 Sync 秒级 mtime diff 判未变化，写后推进一秒（同 Approve）
	var prev time.Time
	if fi, statErr := os.Stat(path); statErr == nil {
		prev = fi.ModTime()
	}
	if err := fsx.WriteFile(path, e.Serialize(), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fsx.BumpMtime(path, prev)
	// 归档只改标记不改正文，向量无需重算，无 embedding 客户端同步即可
	if err := syncIndex(st); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("索引同步失败: %v", err))
		return
	}
	e.Path = path
	writeJSON(w, http.StatusOK, summaryOf(e))
}

// apiCaptureGet 返回项目合并配置中的捕获模式、turn_interval 与 provenance auto_born。
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
		"auto_born":     cfg.Provenance.AutoBorn,
	})
}

// setProvenanceAutoBorn 重写 config.toml 的 [provenance] 小节（auto_born 键）：
// 已存在则整段替换（到下一个 [section] 或文件尾），不存在则在文件尾追加；
// 其余内容（含注释）原样保留。算法与 config.SetCapture 同款（[capture] → [provenance]）。
func setProvenanceAutoBorn(path string, autoBorn bool) error {
	block := "[provenance]\nauto_born = " + strconv.FormatBool(autoBorn) + "\n"
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsx.WriteFile(path, []byte(block), 0o644)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == "[provenance]" {
				start = i
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	var out []string
	if start >= 0 {
		out = append(out, lines[:start]...)
		out = append(out, strings.TrimSuffix(block, "\n"))
		out = append(out, lines[end:]...)
	} else {
		out = append(out, lines...)
		// 与上文保持空行分隔
		if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) != "" {
			out = append(out, "")
		}
		out = append(out, strings.TrimSuffix(block, "\n"))
	}
	return fsx.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// apiCaptureSet 设置 capture 模式、轮次间隔与 provenance auto_born：
// [capture] 小节走 config.SetCapture；[provenance] 小节走 setProvenanceAutoBorn。
// mode 为空、turn_interval 为 0、auto_born 缺省（null）均表示保持不变。
func (h *Handler) apiCaptureSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project      string `json:"project"`
		Mode         string `json:"mode"`
		TurnInterval int    `json:"turn_interval"`
		AutoBorn     *bool  `json:"auto_born"`
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
	autoBorn := cfg.Provenance.AutoBorn
	if req.AutoBorn != nil {
		autoBorn = *req.AutoBorn
		if err := setProvenanceAutoBorn(st.ConfigPath(), autoBorn); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "turn_interval": interval, "auto_born": autoBorn})
}

// apiGateGet 返回项目合并配置中的门控开关、内置短语表与 extra 追加层。
func (h *Handler) apiGateGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	extra := cfg.Retrieve.Gate.ExtraPhrases
	if extra == nil {
		extra = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": cfg.Retrieve.Gate.Enabled,
		"builtin": retrieve.BuiltinPhrases(),
		"extra":   extra,
	})
}

// apiGateSet 设置门控开关与 extra 短语（全量替换，幂等）：enabled / extra 任一为
// null 表示该字段不变。extra 校验：逐条 trim+折叠空白，按归一化形去重（与内置
// 重复的直接丢弃），单条 ≤64 字符、总数 ≤200 条；非法即 400。
// 落盘走 config.SetGate（[retrieve.gate] 整段替换）。
func (h *Handler) apiGateSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string    `json:"project"`
		Enabled *bool     `json:"enabled"`
		Extra   *[]string `json:"extra"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	enabled := cfg.Retrieve.Gate.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	extra := cfg.Retrieve.Gate.ExtraPhrases
	if req.Extra != nil {
		cleaned, err := cleanGatePhrases(*req.Extra)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		extra = cleaned
	}
	if req.Enabled != nil || req.Extra != nil {
		if err := config.SetGate(st.ConfigPath(), enabled, extra); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if extra == nil {
		extra = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled,
		"builtin": retrieve.BuiltinPhrases(),
		"extra":   extra,
	})
}

// cleanGatePhrases 校验并清洗 extra 短语：trim+折叠连续空白、按归一化形去重、
// 与内置重复的丢弃；单条 ≤64 字符、总数 ≤200 条（防止 config 被刷爆）。
func cleanGatePhrases(in []string) ([]string, error) {
	if len(in) > 200 {
		return nil, fmt.Errorf("短语总数 %d 超过上限 200", len(in))
	}
	builtin := map[string]bool{}
	for _, p := range retrieve.BuiltinPhrases() {
		builtin[p] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		folded := strings.Join(strings.Fields(p), " ")
		if folded == "" {
			return nil, fmt.Errorf("短语不能为空")
		}
		if utf8.RuneCountInString(folded) > 64 {
			return nil, fmt.Errorf("短语 %q 超过 64 字符", folded)
		}
		n := retrieve.Normalize(folded)
		if builtin[n] || seen[n] {
			continue // 与内置重复 / 列表内重复：直接丢弃
		}
		seen[n] = true
		out = append(out, folded)
	}
	return out, nil
}

// apiInjectGet 返回项目合并配置中的注入预算：mandatory 全文 token 上限
//（[inject] mandatory_max_tokens，默认 2000）。
func (h *Handler) apiInjectGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	limit := cfg.Inject.MandatoryMaxTokens
	if limit <= 0 {
		limit = 2000
	}
	writeJSON(w, http.StatusOK, map[string]any{"mandatory_max_tokens": limit})
}

// apiInjectSet 设置 mandatory 全文 token 上限（100~100000，非法 400）；
// 落盘走 config.SetInjectMandatoryMax（[inject] 小节内单键 upsert，其余键保留）。
func (h *Handler) apiInjectSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project            string `json:"project"`
		MandatoryMaxTokens int    `json:"mandatory_max_tokens"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	if req.MandatoryMaxTokens < 100 || req.MandatoryMaxTokens > 100000 {
		writeErr(w, http.StatusBadRequest, "mandatory_max_tokens 必须在 100~100000 之间")
		return
	}
	if err := config.SetInjectMandatoryMax(st.ConfigPath(), req.MandatoryMaxTokens); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mandatory_max_tokens": req.MandatoryMaxTokens})
}

// apiRetrieveGet 返回项目合并配置中的跨轮注入冷却轮数
//（[retrieve] dedup_turns，默认 3；<0 归一为 0）。
func (h *Handler) apiRetrieveGet(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	n := cfg.Retrieve.DedupTurns
	if n < 0 {
		n = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{"dedup_turns": n})
}

// apiRetrieveSet 设置跨轮注入冷却轮数（0~99，0=关闭，非法 400）；落盘走
// config.SetRetrieveDedupTurns（[retrieve] 小节内单键 upsert，其余键保留）。
func (h *Handler) apiRetrieveSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project    string `json:"project"`
		DedupTurns int    `json:"dedup_turns"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	if req.DedupTurns < 0 || req.DedupTurns > 99 {
		writeErr(w, http.StatusBadRequest, "dedup_turns 必须在 0~99 之间")
		return
	}
	if err := config.SetRetrieveDedupTurns(st.ConfigPath(), req.DedupTurns); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiProjectBranchInfo 返回项目的分支上下文：基准分支（wiki.json）、
// 项目目录实际 checkout 分支、合并谱系（无谱系给 [] 便于前端）。
func (h *Handler) apiProjectBranchInfo(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("project")
	st := resolveProject(w, name)
	if st == nil {
		return
	}
	out := map[string]any{"base_branch": "", "current_branch": "", "merges": []wiki.MergeRecord{}}
	if s := wiki.LoadState(st.StateDir()); s != nil {
		out["base_branch"] = s.BaseBranch
		if len(s.Merges) > 0 {
			out["merges"] = s.Merges
		}
	}
	// 当前分支取注册表项目第一个路径的 checkout 分支（非 git/失败为空串，fail-open）
	if reg, err := registry.Load(registry.DefaultPath()); err == nil {
		for _, p := range reg.Projects {
			if p.Name == name && len(p.Paths) > 0 {
				out["current_branch"] = wiki.CurrentBranch(p.Paths[0])
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- 心跳与停服 ----------

// apiHeartbeat 心跳（前端 5s 轮询），返回指定项目 kb.db 的修改时间作为版本号，
// 供前端做"变更才重拉"的自动刷新。project 为空或不存在时 version 为 0。
func (h *Handler) apiHeartbeat(w http.ResponseWriter, r *http.Request) {
	select {
	case h.beats <- struct{}{}:
	default:
	}
	var version int64
	if name := r.URL.Query().Get("project"); name != "" {
		if reg, err := registry.Load(registry.DefaultPath()); err == nil {
			for _, p := range reg.Projects {
				if p.Name == name {
					kb := filepath.Join(registry.Home(), "projects", p.Name, "kb.db")
					if fi, err := os.Stat(kb); err == nil {
						version = fi.ModTime().UnixNano()
					}
					break
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version})
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

// apiSetupHooks 安装 hooks：body 可指定 {"agent":"<id>"}（未知 id → 400），
// 缺省为全部已检测 agent；可带 {"timeout_sec":N}（1~60）先写入全局配置再安装，
// 三条 hook 统一使用该超时；响应列出每个 agent 的安装目标。单 agent 失败不影响
// 其余（成功项保持落盘，幂等可重试），全部尝试后有失败则 500 聚合报告。
func (h *Handler) apiSetupHooks(w http.ResponseWriter, r *http.Request) {
	exe, err := exePath()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Agent      string `json:"agent"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	if req.TimeoutSec != 0 {
		if req.TimeoutSec < 1 || req.TimeoutSec > 60 {
			writeErr(w, http.StatusBadRequest, "timeout_sec 必须是 1~60 的整数")
			return
		}
		if err := setupx.SaveHooksTimeout(req.TimeoutSec); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	var targets []agentx.Agent
	if req.Agent != "" {
		a, ok := agentx.Find(req.Agent)
		if !ok {
			writeErr(w, http.StatusBadRequest, "未知 agent: "+req.Agent)
			return
		}
		targets = []agentx.Agent{a}
	} else {
		targets = agentx.Detected()
	}
	installed := make([]map[string]string, 0, len(targets))
	var failed []string
	for _, a := range targets {
		if err := a.InstallHooks(exe); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.ID(), err))
			continue
		}
		installed = append(installed, map[string]string{"agent": a.ID(), "path": a.HooksTarget()})
	}
	if len(failed) > 0 {
		writeErr(w, http.StatusInternalServerError, "agent hook 安装失败: "+strings.Join(failed, "; "))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "installed": installed})
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dirs": setupx.SkillDirs()})
}

// apiReasonixEnforceMode 保存 reasonix sidecar 的强制检查表达方式（soft|hard|mixed）。
// sidecar 每条输入实时读配置，即时生效，无需重装插件。
func (h *Handler) apiReasonixEnforceMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := setupx.SaveReasonixEnforceMode(req.Mode); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": setupx.ReasonixEnforceMode()})
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
