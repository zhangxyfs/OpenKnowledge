# GUI 更新日志弹窗实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 升级后打开 GUI 弹窗展示更新日志（跳级累计），并提供"其他"标签页常驻查看入口。

**Architecture:** changelogs 随安装包分发到 `{app}/changelogs/`（与 web/ 同套路）；`internal/gui/changelog.go` 负责目录定位/版本解析/pending 计算与 seen 状态（`~/.openknowledge/gui.json`）；前端 modal 复用既有 `.modal` 样式，极简 markdown 渲染。

**Tech Stack:** Go（net/http，无新依赖），原生 JS SPA（web/app.js），Inno Setup。

## Global Constraints

- 只认 `^\d+\.\d+\.\d+\.md$` 命名的 changelog；版本比较为数值三元组，非规范版本（`dev`）不参与。
- pending = 版本号严格大于 `last_seen_version` 且不超过当前版本的条目，升序；无 gui.json → 空；`current == "dev"` → 恒空；降级安装 → 空。
- 状态文件 `~/.openknowledge/gui.json`：`{"last_seen_version": "N.N.N"}`，0600；缺失/损坏视为无记录。
- 前端 markdown 渲染必须先 HTML 转义（复用 app.js 既有 `esc()`）再套格式，零新依赖。
- 测试隔离：gui 包测试用 `t.Setenv("OK_HOME", t.TempDir())` 模式；`version.Version` 是可赋值包变量，测试须保存/恢复原值。
- spec：`docs/superpowers/specs/2026-08-04-gui-changelog-popup-design.md`。

---

### Task 1: 后端 changelog API

**Files:**
- Create: `internal/gui/changelog.go`
- Modify: `internal/gui/api.go:67-68` 附近（`api(...)` 路由注册块内加两行）
- Test: `internal/gui/changelog_test.go`（新建）

**Interfaces:**
- Consumes: `version.Version`（包变量，构建期注入）、`registry.Home()`、Handler 既有字段 `webDir`、既有助手 `writeJSON(w, code, v)` / `writeErr(w, code, msg)`（`internal/gui/api.go`）。
- Produces（Task 2 前端依赖的契约）:
  - `GET /api/changelog` → `{"current": string, "pending": [{"version": string, "log": string}], "all": [{"version": string, "log": string}]}`（pending/all 无条目时为 `[]` 而非 null：Go 侧初始化为空切片）。
  - `POST /api/changelog/seen` → `{"ok": true}`；`current == "dev"` 时不写文件直接返回。

- [ ] **Step 1: 写失败测试**

创建 `internal/gui/changelog_test.go`：

```go
package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"openknowledge/internal/version"
)

// changelogEnv 搭 root/web（webDir）结构，OK_HOME 隔离；返回 handler 与 root。
func changelogEnv(t *testing.T) (*Handler, string) {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	root := t.TempDir()
	webDir := filepath.Join(root, "web")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"index.html": "<html></html>", "app.js": "", "style.css": "", "favicon.ico": "",
	} {
		if err := os.WriteFile(filepath.Join(webDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(webDir, testToken, nil), root
}

func writeChangelog(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func doJSON(t *testing.T, h *Handler, method, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Ok-Token", testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s -> %d: %s", method, path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func pendingVersions(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["pending"].([]any)
	if !ok {
		t.Fatalf("pending not an array: %v", body["pending"])
	}
	var out []string
	for _, e := range raw {
		out = append(out, e.(map[string]any)["version"].(string))
	}
	return out
}

// withVersion 临时设置 version.Version 并注册恢复。
func withVersion(t *testing.T, v string) {
	t.Helper()
	orig := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = orig })
}

func TestChangelogAPI(t *testing.T) {
	h, root := changelogEnv(t)
	withVersion(t, "2.10.0")
	cl := filepath.Join(root, "changelogs")
	writeChangelog(t, cl, "2.10.0.md", "# 2.10.0\n\n## 新功能\n- 十\n")
	writeChangelog(t, cl, "2.9.0.md", "# 2.9.0\n\n## 修复\n- 九\n")
	writeChangelog(t, cl, "2.2.3.md", "# 2.2.3\n\n- 三\n")
	writeChangelog(t, cl, "2026-07-22-v1.1-setup-toggle.md", "# 旧格式\n") // 应被过滤
	writeChangelog(t, cl, "README.txt", "noise")                            // 应被过滤

	// all：数值升序（2.10.0 > 2.9.0，非字典序），只含 N.N.N.md
	body := doJSON(t, h, "GET", "/api/changelog")
	all := body["all"].([]any)
	if len(all) != 3 {
		t.Fatalf("all len = %d, want 3: %v", len(all), all)
	}
	if all[0].(map[string]any)["version"] != "2.2.3" || all[2].(map[string]any)["version"] != "2.10.0" {
		t.Fatalf("all order wrong: %v", all)
	}
	if body["current"] != "2.10.0" {
		t.Fatalf("current = %v", body["current"])
	}

	// 无 gui.json → pending 为空（首次不弹历史）
	if got := pendingVersions(t, body); len(got) != 0 {
		t.Fatalf("no gui.json: pending = %v, want empty", got)
	}

	// last_seen=2.2.4 → 累计 2.9.0 + 2.10.0（升序，且 log 带内容）
	writeState(t, "2.2.4")
	body = doJSON(t, h, "GET", "/api/changelog")
	got := pendingVersions(t, body)
	if len(got) != 2 || got[0] != "2.9.0" || got[1] != "2.10.0" {
		t.Fatalf("pending = %v, want [2.9.0 2.10.0]", got)
	}

	// 降级（last_seen 比当前新）→ 空
	writeState(t, "9.9.9")
	if got := pendingVersions(t, doJSON(t, h, "GET", "/api/changelog")); len(got) != 0 {
		t.Fatalf("downgrade: pending = %v, want empty", got)
	}

	// POST seen → 写 gui.json；再 GET pending 为空
	writeState(t, "2.2.4")
	doJSON(t, h, "POST", "/api/changelog/seen")
	data, err := os.ReadFile(filepath.Join(os.Getenv("OK_HOME"), "gui.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]string
	if err := json.Unmarshal(data, &st); err != nil || st["last_seen_version"] != "2.10.0" {
		t.Fatalf("gui.json = %q err=%v", data, err)
	}
	if got := pendingVersions(t, doJSON(t, h, "GET", "/api/changelog")); len(got) != 0 {
		t.Fatalf("after seen: pending = %v, want empty", got)
	}

	// current=dev → pending 恒空，POST seen 不写文件
	withVersion(t, "dev")
	writeState(t, "2.2.4")
	if got := pendingVersions(t, doJSON(t, h, "GET", "/api/changelog")); len(got) != 0 {
		t.Fatalf("dev: pending = %v, want empty", got)
	}
	if err := os.Remove(filepath.Join(os.Getenv("OK_HOME"), "gui.json")); err != nil {
		t.Fatal(err)
	}
	doJSON(t, h, "POST", "/api/changelog/seen")
	if _, err := os.Stat(filepath.Join(os.Getenv("OK_HOME"), "gui.json")); !os.IsNotExist(err) {
		t.Fatal("dev: seen should not write gui.json")
	}
}

// writeState 直接写 gui.json（绕过 API）。
func writeState(t *testing.T, seen string) {
	t.Helper()
	data := []byte(`{"last_seen_version":"` + seen + `"}`)
	if err := os.WriteFile(filepath.Join(os.Getenv("OK_HOME"), "gui.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// dev 回退：安装态目录不存在时读 <root>/docs/changelogs。
func TestChangelogDevFallback(t *testing.T) {
	h, root := changelogEnv(t)
	withVersion(t, "2.3.2")
	writeChangelog(t, filepath.Join(root, "docs", "changelogs"), "2.3.2.md", "# 2.3.2\n")
	body := doJSON(t, h, "GET", "/api/changelog")
	all := body["all"].([]any)
	if len(all) != 1 || all[0].(map[string]any)["version"] != "2.3.2" {
		t.Fatalf("dev fallback all = %v", all)
	}
}

// changelogs 目录完全缺失 → all/pending 为空数组且不报错。
func TestChangelogMissingDir(t *testing.T) {
	h, _ := changelogEnv(t)
	withVersion(t, "2.3.2")
	body := doJSON(t, h, "GET", "/api/changelog")
	if len(body["all"].([]any)) != 0 || len(body["pending"].([]any)) != 0 {
		t.Fatalf("missing dir should be empty: %v", body)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/gui/ -run "TestChangelog" -v`
Expected: FAIL——`/api/changelog` 路由不存在，三个测试均 404（`-> 404`）

- [ ] **Step 3: 实现**

创建 `internal/gui/changelog.go`：

```go
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
```

在 `internal/gui/api.go` 的 `api(...)` 路由注册块（`api("POST /api/toggle", h.apiToggle)` 一行之后）加两行：

```go
	api("GET /api/changelog", h.apiChangelog)
	api("POST /api/changelog/seen", h.apiChangelogSeen)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/gui/ -run "TestChangelog" -v && go test ./internal/gui/`
Expected: 三个新测试 PASS；包内既有测试全部 ok

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge && git add internal/gui/changelog.go internal/gui/changelog_test.go internal/gui/api.go && git commit -m "feat(gui): /api/changelog——版本日志解析/pending 计算/seen 状态（gui.json）"
```

---

### Task 2: 前端弹窗与常驻入口 + 构建分发

**Files:**
- Modify: `web/index.html`（misc 卡 + modal 容器）
- Modify: `web/app.js`（renderMd/弹窗逻辑/启动挂钩/两个事件监听）
- Modify: `web/style.css`（changelog 内容样式）
- Modify: `scripts/build-dist.sh`（拷贝 changelogs）
- Modify: `installer/openknowledge.iss:37` 后（安装 changelogs）

**Interfaces:**
- Consumes: Task 1 的 `GET /api/changelog` / `POST /api/changelog/seen` 契约；前端既有助手 `api(path, opts)`、`esc(s)`、`showError(msg)`、`$(id)`；既有 `.modal`/`.modal-box`/`.hidden` 样式（`web/style.css:296-343`）。
- Produces: 无代码接口（交付为 GUI 行为 + 安装包内容）。

- [ ] **Step 1: index.html 加入口与 modal**

在 misc 页 cards 中"关于"卡（`<div class="card">` 含 `misc-version`）之前插入：

```html
        <div class="card">
          <div class="card-head"><h3>更新日志</h3></div>
          <p class="card-desc muted">查看各版本的新功能与修复；升级后首次打开会自动弹出。</p>
          <div class="card-actions"><button id="btn-changelog" type="button" class="btn">查看更新日志</button></div>
        </div>
```

在 `entry-modal` 的 `</div>` 结束之后、`<script src="app.js"></script>` 之前插入：

```html
  <div id="changelog-modal" class="modal hidden">
    <div class="modal-box changelog-box">
      <h3 id="changelog-modal-title">更新日志</h3>
      <div id="changelog-content" class="changelog-content"></div>
      <div class="modal-actions">
        <button id="changelog-close" type="button" class="btn btn-primary">知道了</button>
      </div>
    </div>
  </div>
```

- [ ] **Step 2: app.js 加渲染与弹窗逻辑**

在 `closeForm` 函数定义（`web/app.js:316` 附近）之后插入：

```js
  // ---------- 更新日志 ----------

  // renderMd 极简 markdown 渲染：# / ## 标题、- 列表、**粗体**、`行内代码`；先 esc 转义防注入。
  function renderMd(md) {
    function inline(s) {
      return esc(s)
        .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
        .replace(/`([^`]+)`/g, "<code>$1</code>");
    }
    var html = [];
    var inList = false;
    function closeList() { if (inList) { html.push("</ul>"); inList = false; } }
    md.split(/\r?\n/).forEach(function (line) {
      var t = line.trim();
      if (t.indexOf("## ") === 0) { closeList(); html.push("<h4>" + inline(t.slice(3)) + "</h4>"); }
      else if (t.indexOf("# ") === 0) { closeList(); html.push("<h3>" + inline(t.slice(2)) + "</h3>"); }
      else if (t.indexOf("- ") === 0) { if (!inList) { html.push("<ul>"); inList = true; } html.push("<li>" + inline(t.slice(2)) + "</li>"); }
      else if (t === "") { closeList(); }
      else { closeList(); html.push("<p>" + inline(t) + "</p>"); }
    });
    closeList();
    return html.join("");
  }

  function openChangelogModal(title, entries) {
    $("changelog-modal-title").textContent = title;
    var content = $("changelog-content");
    if (!entries || entries.length === 0) {
      content.innerHTML = '<p class="muted">暂无更新日志</p>';
    } else {
      content.innerHTML = entries.map(function (e) { return renderMd(e.log); }).join("<hr>");
    }
    $("changelog-modal").classList.remove("hidden");
  }

  // checkChangelog 启动时拉取：pending 非空弹升级日志；结果缓存供常驻入口使用。
  function checkChangelog() {
    api("/api/changelog").then(function (c) {
      state.changelog = c;
      if (c.pending && c.pending.length > 0) {
        var latest = c.pending[c.pending.length - 1].version;
        var title = c.pending.length > 1
          ? ("已更新到 v" + latest + "（含最近 " + c.pending.length + " 个版本）")
          : ("新版本 v" + latest + " 更新内容");
        openChangelogModal(title, c.pending);
      }
    }).catch(function () { /* 拉取失败不阻断主界面 */ });
  }

  $("changelog-close").addEventListener("click", function () {
    $("changelog-modal").classList.add("hidden");
    api("/api/changelog/seen", { method: "POST" }).catch(function (err) { showError(err.message); });
  });

  $("btn-changelog").addEventListener("click", function () {
    openChangelogModal("更新日志", state.changelog ? state.changelog.all : null);
  });
```

把文件末尾的启动调用 `refreshStatus();` 改为：

```js
  refreshStatus().then(checkChangelog);
```

- [ ] **Step 3: style.css 加内容样式**

在 `.modal-actions` 规则（`web/style.css:343`）之后追加（分隔线用既有 `--border` 变量，不引入新颜色）：

```css
.changelog-box { width: 92%; max-height: 82vh; display: flex; flex-direction: column; overflow: hidden; }
.changelog-content { overflow-y: auto; text-align: left; }
.changelog-content h3 { margin: 14px 0 8px; }
.changelog-content h4 { margin: 12px 0 6px; }
.changelog-content p { margin: 6px 0; }
.changelog-content ul { margin: 6px 0; padding-left: 20px; }
.changelog-content hr { border: none; border-top: 1px solid var(--border); margin: 16px 0; }
```

- [ ] **Step 4: 构建与安装器分发**

`scripts/build-dist.sh`：把

```bash
rm -rf dist/web
cp -r web dist/web
echo "dist/ built: ok.exe + web/"
```

改为：

```bash
rm -rf dist/web dist/changelogs
cp -r web dist/web
cp -r docs/changelogs dist/changelogs
echo "dist/ built: ok.exe + web/ + changelogs/"
```

`installer/openknowledge.iss`：在 `Source: "..\dist\web\*"; DestDir: "{app}\web"; Flags: ignoreversion recursesubdirs` 一行之后加：

```
Source: "..\dist\changelogs\*"; DestDir: "{app}\changelogs"; Flags: ignoreversion recursesubdirs
```

- [ ] **Step 5: 构建验证**

Run: `cd D:/develop/OpenKnowledge && go build ./... && bash scripts/build-dist.sh && ls dist/changelogs/ | head -5 && grep -c "changelog-modal" dist/web/index.html dist/web/app.js`
Expected: 构建成功；`dist/changelogs/` 含 `2.3.2.md` 等版本文件；两个前端文件都含 `changelog-modal`（计数 ≥1）

- [ ] **Step 6: Commit**

```bash
cd D:/develop/OpenKnowledge && git add web/index.html web/app.js web/style.css scripts/build-dist.sh installer/openknowledge.iss && git commit -m "feat(gui): 更新日志弹窗（升级累计弹出 + 其他页常驻入口）+ changelogs 随安装包分发"
```

---

## 备注（不在任务内）

- 手动验收（发版时）：安装后把 `~/.openknowledge/gui.json` 的 `last_seen_version` 改为旧版本再开 GUI，验证跨版本累计弹窗；"其他"页入口随时可看全部版本。
- 版本 bump 与 changelog 归发版流程，本计划不含。
