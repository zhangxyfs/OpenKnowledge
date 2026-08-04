package gui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"openknowledge/internal/registry"
	"openknowledge/internal/version"
)

// changelogEntry 是一个版本号的更新日志（N.N.N.md 全文）。
type changelogEntry struct {
	Version string `json:"version"`
	Log     string `json:"log"`
}

var changelogFileRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)\.md$`)

// changelogDir 定位更新日志目录：安装态 <webDir 父目录>/changelogs 优先，
// 缺失时回退 dev 仓库内运行的 docs/changelogs；都没有返回 ""。
func (h *Handler) changelogDir() string {
	root := filepath.Dir(h.webDir)
	for _, cand := range []string{
		filepath.Join(root, "changelogs"),
		filepath.Join(root, "docs", "changelogs"),
	} {
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand
		}
	}
	return ""
}

// readChangelogs 读取全部 N.N.N.md，按版本号数值升序；目录缺失/为空返回空切片。
func (h *Handler) readChangelogs() []changelogEntry {
	entries := []changelogEntry{}
	dir := h.changelogDir()
	if dir == "" {
		return entries
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return entries
	}
	for _, f := range files {
		m := changelogFileRe.FindStringSubmatch(f.Name())
		if m == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		entries = append(entries, changelogEntry{Version: m[1] + "." + m[2] + "." + m[3], Log: string(data)})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, _ := parseVersion(entries[i].Version)
		b, _ := parseVersion(entries[j].Version)
		return versionLess(a, b)
	})
	return entries
}

// parseVersion 把 "N.N.N" 拆成数值三元组；非规范版本（如 dev）返回 ok=false。
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func versionLess(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// guiState 是 ~/.openknowledge/gui.json 的内容（GUI 侧持久化小状态）。
type guiState struct {
	LastSeenVersion string `json:"last_seen_version"`
}

func guiStatePath() string { return filepath.Join(registry.Home(), "gui.json") }

// loadLastSeen 读取已看版本；文件缺失/损坏返回 ""。
func loadLastSeen() string {
	data, err := os.ReadFile(guiStatePath())
	if err != nil {
		return ""
	}
	var s guiState
	if err := json.Unmarshal(data, &s); err != nil {
		return ""
	}
	return s.LastSeenVersion
}

// apiChangelog 返回当前版本、未看版本（pending，升序）与全部历史（all）。
// pending 规则：有 last_seen 记录才计算（首次不弹历史）；current 非规范版本（dev）恒空；
// 条目版本须严格大于 last_seen 且不超过 current（防止新旧包混装时展示未发布内容）。
func (h *Handler) apiChangelog(w http.ResponseWriter, _ *http.Request) {
	current := version.Version
	entries := h.readChangelogs()
	pending := []changelogEntry{}
	cur, curOK := parseVersion(current)
	seen, seenOK := parseVersion(loadLastSeen())
	if curOK && seenOK {
		for _, e := range entries {
			v, _ := parseVersion(e.Version) // 正则已约束，必成功
			if versionLess(seen, v) && !versionLess(cur, v) {
				pending = append(pending, e)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current": current,
		"pending": pending,
		"all":     entries,
	})
}

// apiChangelogSeen 标记当前版本为已看；dev 构建不写文件直接 ok。
func (h *Handler) apiChangelogSeen(w http.ResponseWriter, _ *http.Request) {
	if _, ok := parseVersion(version.Version); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	data, err := json.Marshal(guiState{LastSeenVersion: version.Version})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(guiStatePath(), data, 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
