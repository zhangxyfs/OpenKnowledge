# 删除项目知识库（GUI 三重确认）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** GUI 其他页新增「删除项目知识库」危险卡，勾选了解后果 + 输入项目名双重解锁，可选默认勾的删除前 zip 备份，后端物理删除（先注销注册表再删目录）。

**Architecture:** 后端 `registry.RemoveProject` 纯内存操作 + `DELETE /api/project` 路由（先 Save 注销、后 RemoveAll 目录，失败兜底偏向数据残留）；前端复用 `.card-danger`/`.modal` 既有模式，确认弹窗内统计影响面（`/api/entries` 计数），执行序为先导出下载后删除。

**Tech Stack:** Go 1.x（net/http ServeMux 方法路由）、原生 JS 单页（web/index.html + web/app.js + web/style.css）、BurntSushi/toml。

## Global Constraints

- Spec：`docs/superpowers/specs/2026-08-09-delete-project-kb-design.md`（2026-08-09 已批准）
- 删除范围：`~/.openknowledge/projects/<name>/` 整个目录 + `registry.toml` 项目条目；**不动**项目源码目录与全局配置
- 解锁条件（前端）：「我已了解后果」勾选 **且** 项目名输入 trim 后大小写敏感精确匹配；备份勾选不参与解锁
- 后端顺序铁律：先 `Save` 注销注册表（失败 → 500 中止，目录不动），后 `os.RemoveAll`（失败 → 200 + `warning`/`dir` 字段）
- 目录名取注册表匹配后的 `p.Name`，禁止用用户原始输入拼路径
- 提交信息风格：`feat(gui): ...` / `test: ...`，中文描述
- 前端无自动化测试框架，验收靠 Go 测试 + 手动验证清单

---

### Task 1: registry.RemoveProject

**Files:**
- Modify: `internal/registry/registry.go`（在 `AddProject` 后追加）
- Test: `internal/registry/registry_test.go`

**Interfaces:**
- Consumes: 既有 `Registry.Projects []Project`、`Registry.Save(path string) error`
- Produces: `func (r *Registry) RemoveProject(name string) bool` —— 按名移除，返回是否找到；Task 2 的 HTTP 处理器依赖此签名

- [ ] **Step 1: 写失败测试**

在 `internal/registry/registry_test.go` 追加（文件已有包声明与 import，沿用既有测试风格）：

```go
func TestRemoveProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_HOME", dir)
	reg := &Registry{}
	if err := reg.AddProject("alpha", "D:/src/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject("beta", "D:/src/beta"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(DefaultPath()); err != nil {
		t.Fatal(err)
	}

	if !reg.RemoveProject("alpha") {
		t.Fatal("RemoveProject(alpha) = false, want true")
	}
	if reg.RemoveProject("alpha") {
		t.Fatal("RemoveProject(alpha) again = true, want false")
	}

	// Save 往返后注册表只剩 beta
	if err := reg.Save(DefaultPath()); err != nil {
		t.Fatal(err)
	}
	back, err := Load(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Projects) != 1 || back.Projects[0].Name != "beta" {
		t.Fatalf("unexpected projects after remove: %+v", back.Projects)
	}
}
```

注意：先读 `registry_test.go` 既有用例，若已有同名环境变量处理方式（如 `t.Setenv("OK_HOME", ...)`）则沿用；若该文件从未用 OK_HOME，则保留上面的写法。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/registry/ -run TestRemoveProject -v`
Expected: FAIL —— `reg.RemoveProject undefined`

- [ ] **Step 3: 最小实现**

在 `internal/registry/registry.go` 的 `AddProject` 函数之后追加：

```go
// RemoveProject 按名移除项目，返回是否找到；持久化需另调 Save。
func (r *Registry) RemoveProject(name string) bool {
	for i, p := range r.Projects {
		if p.Name == name {
			r.Projects = append(r.Projects[:i], r.Projects[i+1:]...)
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/registry/ -v`
Expected: PASS（含既有全部用例）

- [ ] **Step 5: 提交**

```bash
git add internal/registry/registry.go internal/registry/registry_test.go
git commit -m "feat(registry): RemoveProject 按名移除项目"
```

---

### Task 2: DELETE /api/project 端点

**Files:**
- Modify: `internal/gui/api.go`（路由表 +1 行；文件尾部「条目」区前新增处理器）
- Test: `internal/gui/api_test.go`

**Interfaces:**
- Consumes: Task 1 的 `(*registry.Registry).RemoveProject(name string) bool`；既有 `registry.Load/DefaultPath/Home`、`writeJSON/writeErr`、测试helper `newEnv/mkProject/do/entryPayload`
- Produces: `DELETE /api/project?project=<name>` —— 响应：成功 `200 {"ok":true}`；目录残留 `200 {"ok":true,"warning":"...","dir":"..."}`；缺参 400 `{"error":"缺少 project 参数"}`；未注册 404 `{"error":"项目未注册: \"X\""}`；无 token 401。Task 3 前端依赖此响应形状

- [ ] **Step 1: 写失败测试**

在 `internal/gui/api_test.go` 追加：

```go
func TestProjectDelete(t *testing.T) {
	h, _, okHome := newEnv(t)
	// 注意：mkProject 每次从空注册表重建，连续调用会互相覆盖——两个项目必须一次写入
	reg := &registry.Registry{}
	if err := reg.AddProject("demo", filepath.Join(t.TempDir(), "demo-src")); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject("keep", filepath.Join(t.TempDir(), "keep-src")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo", "keep"} {
		if err := os.MkdirAll(filepath.Join(okHome, "projects", name, "knowledge"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 造一条知识，让 projects/demo 目录有内容
	code, data := do(t, "POST", srv.URL+"/api/entry", testToken, entryPayload("橙子种植"))
	if code != 200 {
		t.Fatalf("create: status = %d, body %s", code, data)
	}

	// 无 token → 401
	code, _ = do(t, "DELETE", srv.URL+"/api/project?project=demo", "", nil)
	if code != 401 {
		t.Fatalf("no token: status = %d, want 401", code)
	}

	// 缺参 → 400；未注册 → 404
	code, _ = do(t, "DELETE", srv.URL+"/api/project", testToken, nil)
	if code != 400 {
		t.Fatalf("missing param: status = %d, want 400", code)
	}
	code, _ = do(t, "DELETE", srv.URL+"/api/project?project=ghost", testToken, nil)
	if code != 404 {
		t.Fatalf("unknown project: status = %d, want 404", code)
	}

	// 正常删除
	code, data = do(t, "DELETE", srv.URL+"/api/project?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("delete: status = %d, body %s", code, data)
	}
	var resp struct {
		OK      bool   `json:"ok"`
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Warning != "" {
		t.Fatalf("unexpected delete response: %s", data)
	}

	// 目录已删
	if _, err := os.Stat(filepath.Join(okHome, "projects", "demo")); !os.IsNotExist(err) {
		t.Fatalf("project dir should be gone, stat err = %v", err)
	}
	// 注册表已注销（从磁盘重读验证）
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.Projects {
		if p.Name == "demo" {
			t.Fatal("demo should be unregistered")
		}
	}
	// /api/status 不再列出 demo、仍列出 keep
	code, data = do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if code != 200 {
		t.Fatalf("status: %d", code)
	}
	var st struct {
		Projects []struct {
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Projects) != 1 || st.Projects[0].Name != "keep" {
		t.Fatalf("status projects after delete: %s", data)
	}
	// 重复删除 → 404
	code, _ = do(t, "DELETE", srv.URL+"/api/project?project=demo", testToken, nil)
	if code != 404 {
		t.Fatalf("re-delete: status = %d, want 404", code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestProjectDelete -v`
Expected: FAIL —— 404/405（路由未注册）或构建错误

- [ ] **Step 3: 实现处理器 + 注册路由**

`internal/gui/api.go` 路由表（`api("DELETE /api/entry", h.apiEntryDelete)` 附近）加一行：

```go
	api("DELETE /api/project", h.apiProjectDelete)
```

文件尾部新增处理器：

```go
// apiProjectDelete 删除项目知识库：先注销注册表（Save 失败则中止、目录不动），
// 再删除 projects/<name>/ 目录；目录删除失败时项目已注销，返回 warning 供前端提示手动清理。
// 目录名取注册表匹配后的 p.Name，不接受用户原始输入，无路径穿越面。
func (h *Handler) apiProjectDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("project")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "缺少 project 参数")
		return
	}
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	found := false
	for _, p := range reg.Projects {
		if p.Name == name {
			name = p.Name // 以注册表登记名为准
			found = true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("项目未注册: %q", name))
		return
	}
	reg.RemoveProject(name)
	if err := reg.Save(registry.DefaultPath()); err != nil {
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
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -run TestProjectDelete -v`
Expected: PASS；再跑 `go test ./internal/gui/` 全量 PASS（既有用例不回归）

- [ ] **Step 5: 提交**

```bash
git add internal/gui/api.go internal/gui/api_test.go
git commit -m "feat(gui): DELETE /api/project 删除项目知识库（先注销后删目录）"
```

---

### Task 3: 前端危险卡 + 三重确认弹窗

**Files:**
- Modify: `web/index.html`（其他页 `.cards` 内加危险卡；`entry-modal` 旁加确认弹窗）
- Modify: `web/app.js`（`renderProjectSelect` 同步危险卡下拉；新增弹窗逻辑区块）
- Modify: `web/style.css`（仅追加解锁前禁用态提示样式，可选）

**Interfaces:**
- Consumes: Task 2 的 `DELETE /api/project` 响应形状；既有 `state.status.projects`、`refreshStatus()`、`showError()`、`api()`、`esc()`；导出走原生 `fetch` + blob 下载（与 `btn-export` 相同写法）
- Produces: DOM id 契约——`del-project-select`、`btn-del-project`、`del-modal`、`del-impact`、`del-backup`、`del-ack`、`del-name`、`del-name-hint`、`btn-del-confirm`、`btn-del-cancel`

- [ ] **Step 1: index.html 加危险卡与弹窗**

「其他」页 `.cards` 容器内、「关于」卡之前插入：

```html
        <div class="card card-danger">
          <div class="card-head"><h3>删除项目知识库</h3></div>
          <p class="card-desc muted">永久删除所选项目的全部知识、索引与配置，并注销注册表（hooks 不再注入）。项目源码目录不受影响。</p>
          <div class="card-actions">
            <select id="del-project-select"></select>
            <button id="btn-del-project" type="button" class="btn btn-danger">删除…</button>
          </div>
        </div>
```

`entry-modal` 那个 `.modal.hidden`  div 之后插入确认弹窗：

```html
  <div id="del-modal" class="modal hidden">
    <div class="modal-box">
      <h3>删除项目知识库</h3>
      <p id="del-impact"></p>
      <label class="checkbox"><input id="del-backup" type="checkbox" checked> 删除前先导出 zip 备份</label>
      <label class="checkbox"><input id="del-ack" type="checkbox"> 我已了解后果，此操作不可撤销</label>
      <label id="del-name-hint">请输入完整项目名以确认 <input id="del-name" type="text" autocomplete="off"></label>
      <div class="modal-actions">
        <button id="btn-del-confirm" type="button" class="btn btn-danger" disabled>永久删除</button>
        <button id="btn-del-cancel" type="button" class="btn">取消</button>
      </div>
    </div>
  </div>
```

- [ ] **Step 2: app.js 危险卡下拉随项目列表同步**

`renderProjectSelect` 函数内、导出卡同步块（`var exp = $("misc-export-project");` 那段）之后追加：

```js
    // 删除项目卡下拉：与管理页项目列表同序同步（无"全部"项）
    var del = $("del-project-select");
    if (del) {
      del.innerHTML = "";
      (projects || []).forEach(function (p) {
        var o = document.createElement("option");
        o.value = p.name;
        o.textContent = p.name;
        del.appendChild(o);
      });
    }
```

- [ ] **Step 3: app.js 弹窗逻辑**

在 `btn-import` 处理器之后新增区块：

```js
  // ---------- 其他页：删除项目知识库（三重确认：备份可选 + 勾选了解 + 输入项目名） ----------

  var delTarget = ""; // 当前弹窗要删的项目名

  function updateDelConfirm() {
    var ok = $("del-ack").checked && $("del-name").value.trim() === delTarget && delTarget !== "";
    $("btn-del-confirm").disabled = !ok;
  }

  $("btn-del-project").addEventListener("click", function () {
    delTarget = $("del-project-select").value || "";
    if (!delTarget) return;
    $("del-impact").textContent = "正在统计条目…";
    $("del-backup").checked = true;
    $("del-ack").checked = false;
    $("del-name").value = "";
    $("del-name-hint").firstChild.textContent = "请输入完整项目名 " + delTarget + " 以确认 ";
    $("btn-del-confirm").textContent = "永久删除";
    $("del-modal").classList.remove("hidden");
    updateDelConfirm();
    // 影响面统计失败不阻塞确认流程
    api("/api/entries?project=" + encodeURIComponent(delTarget)).then(function (list) {
      list = list || [];
      var drafts = list.filter(function (e) { return e.draft; }).length;
      $("del-impact").textContent = "将永久删除项目「" + delTarget + "」的知识库：共 " +
        list.length + " 条知识（含 " + drafts + " 条草稿）、索引与项目配置，并注销注册表" +
        "（hooks 不再注入）。项目源码目录不受影响。";
    }).catch(function () {
      $("del-impact").textContent = "条目统计失败（不影响删除操作）。将删除项目「" + delTarget +
        "」的知识库、索引与项目配置，并注销注册表。项目源码目录不受影响。";
    });
  });

  ["del-ack", "del-name"].forEach(function (id) {
    $(id).addEventListener("input", updateDelConfirm);
    $(id).addEventListener("change", updateDelConfirm);
  });
  $("btn-del-cancel").addEventListener("click", function () {
    $("del-modal").classList.add("hidden");
  });

  $("btn-del-confirm").addEventListener("click", function () {
    var btn = this;
    if (btn.disabled) return;
    btn.disabled = true;
    btn.textContent = "删除中…";
    var name = delTarget;
    // 可选备份：复用导出卡的 blob 下载写法；导出失败中止删除
    var backup = Promise.resolve();
    if ($("del-backup").checked) {
      backup = fetch("/api/export?project=" + encodeURIComponent(name), {
        headers: { "X-Ok-Token": TOKEN },
      }).then(function (res) {
        if (!res.ok) throw new Error("备份导出失败（" + res.status + "），已中止删除");
        return res.blob().then(function (blob) {
          var a = document.createElement("a");
          a.href = URL.createObjectURL(blob);
          a.download = "openknowledge-backup-" + name + ".zip";
          a.click();
          URL.revokeObjectURL(a.href);
        });
      });
    }
    backup.then(function () {
      return api("/api/project?project=" + encodeURIComponent(name), { method: "DELETE" });
    }).then(function (res) {
      $("del-modal").classList.add("hidden");
      state.lastVersion = 0;
      refreshStatus();
      if (res && res.warning) {
        showError("项目已注销，但" + res.warning + "，请手动清理 " + (res.dir || ""));
      }
    }).catch(function (err) {
      showError(err.message);
    }).then(function () {
      btn.disabled = false;
      btn.textContent = "永久删除";
      updateDelConfirm();
    });
  });
```

要点说明（实现时自查）：
- `del-name-hint` 的首个文本节点写法依赖 Step 1 的 HTML 结构（label 文本在 input 之前）；若结构调整，改用独立 `<span>` 包项目名并设 id
- 结尾的 `.then` 兼作 finally 用（`api()` 错误已在 catch 转横幅），保证按钮态必恢复
- `updateDelConfirm` 在删除完成后重算：`delTarget` 仍等于输入值时会重新解锁，但弹窗已关闭，下次打开会重置——无害

- [ ] **Step 4: style.css 微调（可选但推荐）**

文件尾部追加，让禁用态更直观：

```css
#btn-del-confirm:disabled { opacity: 0.45; cursor: not-allowed; }
#del-impact { background: #fdf7f7; border: 1px solid #e5b4b4; border-radius: 6px; padding: 8px 12px; }
#del-modal label { display: block; margin-bottom: 10px; }
#del-modal label.checkbox { display: flex; align-items: center; gap: 8px; }
#del-name { display: block; width: 100%; margin-top: 4px; }
```

- [ ] **Step 5: 语法检查 + 提交**

Run: `node --check web/app.js`
Expected: 无输出（通过）

```bash
git add web/index.html web/app.js web/style.css
git commit -m "feat(gui): 其他页删除项目知识库——勾选+输名双重解锁，默认勾备份"
```

---

### Task 4: 全量回归与手动验收

**Files:**
- Modify: 无（只验证）

**Interfaces:**
- Consumes: Task 1-3 全部产物
- Produces: 可发布的特性完整态

- [ ] **Step 1: 全仓构建与测试**

Run: `go build ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 2: 重建 dist 并重启 daemon 冒烟**

```bash
"D:/develop/OpenKnowledge/dist/ok.exe" daemon stop
bash scripts/build-dist.sh
"D:/develop/OpenKnowledge/dist/ok.exe" daemon   # 后台
```

用 curl 冒烟（token 从 `~/.openknowledge/daemon.json` 读）：
- `GET /api/status` 返回 200
- 对一个**测试用项目**执行 `DELETE /api/project?project=<名>` 返回 `{"ok":true}`，目录消失

- [ ] **Step 3: 手动验证清单（浏览器，逐条过）**

- 仅勾选 / 仅输名 / 输名大小写不符 / 输名带首尾空格但中间不符 → 删除按钮均 disabled
- 默认勾备份 → 点删除先下载 zip，随后项目消失
- 取消勾备份 → 不下载直接删
- 删当前选中项目 → 管理页自动切到下一项目
- 删最后一个项目 → 管理 tab 隐藏、落引导页

注：本会话内置浏览器合成交互不可用（点击/截图失效），自动点击验证不可行，清单由用户过一遍。

- [ ] **Step 4: 提交（如有修复）并汇报**

如有修复：`git commit -am "fix(gui): 删除项目验收修复"`；否则无需提交。向用户汇报验收结果与待确认项。
