# 配置中心新 UI（OkManager 五页）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 `web/prototype-manager-v2.html` 定稿的五页配置中心替换现有四标签 GUI（`web/index.html` + `app.js` + `style.css`），接真后端，让 OkManager.exe 打开的就是新 UI。

**Architecture:** 保持零依赖、无前端构建链、daemon 实时读盘分发的既有机制（`serveIndex` 的 `{{TOKEN}}` 注入、`serveStatic` 字面量白名单、`no-cache` 均不动，`internal/gui/api.go:50-150`）。前端三文件原位重写；原型已逐屏定稿，是本计划的**规范引用**（结构/样式/交互以它为准），计划只规定原型里没有的部分：API 接线、后端缺口端点、现有 GUI 独有行为的保留。后端补 4 个端点（全部有现成底层函数，纯 HTTP 封装）。

**Tech Stack:** Go 1.25（无新增依赖）、原生 HTML/CSS/JS（单页三文件，无框架无构建）。

**上游:** 设计文档 `docs/2026-08-21-gui-split-design.md` §6；原型 `web/prototype-manager-v2.html`（规范引用，下称"原型"）；进程分离已完成于 feat/gui-split（BASE 3ccddcf..14bca1e）。**分支建议**：feat/gui-split 合并后从其开 `feat/config-center-ui`；若分支暂留则直接在其上继续。

## Global Constraints

- 不新增任何第三方 Go 依赖；不引入前端框架/构建链/包管理器
- 静态分发白名单机制（`internal/gui/api.go:53-59`）不动：生产只用 `index.html` / `app.js` / `style.css` / `favicon.ico` / `help.md` 五个已白名单文件；agent 品牌 logo 沿用原型的内联 data-URI，不新增静态文件路由
- 提交信息：Conventional Commits 前缀 + 中文描述
- 每个 Task 结束 `go build ./... && go test ./...` 全绿才允许 commit
- 原型已定稿的交互范式（违者视为 spec 不符）：
  1. 卡片 = 标题行 → 详情行 → 控件行；简单控件（开关/按钮）放详情行或控件行右端
  2. 弹窗"确定"即生效，卡片闪 ✓ 反馈（embedding/LLM/短语表）；总闸与门控开关即开即存；简单输入（Hook 超时/冷却轮数/沉淀间隔）行内保存、**改回原值自动变灰**（oninput 实时判脏，不重渲丢焦点）
  3. hover 样式必须排除 `:disabled`；禁用保存按钮为中性灰
  4. 整页重渲保持各滚动容器 scrollTop（render 外壳同步恢复）
  5. 刷新经 `location.hash` 恢复当前菜单
  6. 中英切换只翻界面 chrome 不翻数据；昼夜主题走 CSS 变量双套
- 前端验证方式（无 JS 测试框架）：Edge headless 截图自查（原型期同款流程：`--headless --screenshot --virtual-time-budget`，注入脚本点击目标菜单）+ 隔离 `OK_HOME` 起 okd 冒烟
- e2e 子进程必须带全量 agent home 隔离变量（既有 tests/e2e 的 runOK 已处理，不得删减）

## 现有 API 面与新 UI 的映射（已核实，`internal/gui/api.go:63-111`）

| 页面 | 端点 |
|---|---|
| 管理 | `GET /api/projects`、`GET /api/entries`、`GET /api/entry`、`POST/PUT/DELETE /api/entry`、`POST /api/approve`、`POST /api/entry/archive`、`GET /api/search`、`GET /api/project/branch-info` |
| 引导 | `GET /api/status`（agents[id/name/detected/hooksInstalled]、skillsInstalled、hooksTimeout、rxEnforceMode、disabled、app_version、home）、`POST /api/setup/hooks`（`{"agent":id}` 单装 / 缺省全装）、`POST /api/setup/skills`、`POST /api/reasonix/enforce-mode` |
| 设置 | 全局开关 `POST /api/toggle`；embedding `GET /api/setup/embedding` + `profile`/`active`/`delete`（DELETE）/`test`/`download`/`download/cancel`/`models-dir`/`open-models-dir`/`ollama-models`；LLM `GET /api/llm` + `profile`/`delete`/`active`/`max-tokens`/`test`；Hook 超时读=status.hooksTimeout；冷却 `GET/POST /api/retrieve`（dedup_turns）；沉淀 `GET/POST /api/capture`；门控 `GET/POST /api/gate` |
| 日志 | `GET /api/logs?tail=400&sig=…`（2s 轮询，unchanged 跳过重绘） |
| 其他 | `GET /api/export`、`POST /api/import`、`GET /api/changelog` + `POST /api/changelog/seen`、`DELETE /api/project`、`/help.md`（静态）、关于=`/api/status`（app_version/home/projects 计数） |

**后端缺口（Task 1 补，全部纯 HTTP 封装）：**
1. Hook 超时的独立写端点——现有写法耦合在 `POST /api/setup/hooks` 的 `timeout_sec`（会顺带重装 hooks，api.go:1452-1461）；底层 `setupx.SaveHooksTimeout` 已存在
2. 规则配置（`[[enforce]]` 数组）读写端点——配置结构 `config.EnforceRule{Type,CodeGlobs,ChangelogGlob,Message}`（config.go:217-222,256），无 GUI 端点；需 config 层新增整体替换写入（参照 SetGate/SetCapture 的小节写入模式）
3. 单 agent 卸载端点——`agentx.Agent.RemoveHooks() (bool, error)` 已存在（agentx/codex.go:776 等全适配器实现），`/api/uninstall` 是全局卸载不能用

---

### Task 1: 原型归档 + 后端缺口四端点（TDD）

**Files:**
- Move: `web/prototype-manager-v2.html`、`web/prototype-manager.html`、`web/prototype-setup.html` → `docs/prototypes/`（git mv 性质的新增；**必须移出 web/**——build.py copytree 整个 web/ 进发行包）
- Create: `internal/gui/api_hookstimeout.go`（端点 1+3）、`internal/gui/api_enforce.go`（端点 2）
- Modify: `internal/gui/api.go:107` 附近（注册 4 条路由）
- Modify: `internal/config/config.go`（新增 `SetEnforceRules`，写入逻辑放同包既有 SetGate/SetCapture 同文件）
- Test: `internal/gui/api_hookstimeout_test.go`、`internal/gui/api_enforce_test.go`、`internal/config/config_test.go`（追加 SetEnforceRules 用例）

**Interfaces:**
- Consumes: `setupx.SaveHooksTimeout(int) error`、`agentx.Find(id) (Agent, bool)`、`Agent.RemoveHooks() (bool, error)`、`config.LoadMerged`、`writeJSON/writeErr/decodeJSON`（api.go 既有）、gui 测试既有 `newEnv` 模式（api_test.go）
- Produces（后续 Task 的前端契约）：
  - `POST /api/hooks/timeout` `{"timeout_sec":N}` → `{"ok":true,"timeout_sec":N}`；N 不在 1~60 → 400
  - `POST /api/setup/hooks/remove` `{"agent":"<id>"}` → `{"ok":true,"removed":bool}`；未知 id → 400
  - `GET /api/enforce/rules?project=` → `{"rules":[{"type","code_globs":[],"changelog_glob","message"}]}`（读取合并配置 `cfg.Enforce`，无规则给 `[]`）
  - `POST /api/enforce/rules` `{"project":"","rules":[...]}` → `{"ok":true}`；校验：type 仅允许 `"changelog"`、code_globs 非空、message 非空，违反 400；空数组合法（清空规则）
  - `config.SetEnforceRules(configPath string, rules []EnforceRule) error`——`[[enforce]]` 数组整体重写，文件其他小节/键保留（SetGate/SetCapture 同款行级写入模式）

- [ ] **Step 1: 原型归档搬迁**

```bash
mkdir -p docs/prototypes
mv web/prototype-manager-v2.html web/prototype-manager.html web/prototype-setup.html docs/prototypes/
git add docs/prototypes web
```

（三个原型当前未跟踪；`git add -A` 后表现为新增 docs/prototypes 三文件。）

- [ ] **Step 2: 失败测试——hooks/timeout 与 hooks/remove**

`internal/gui/api_hookstimeout_test.go`（沿用 api_test.go 的 newEnv/token 模式）：

```go
func TestHooksTimeoutSet(t *testing.T) {
	// POST /api/hooks/timeout {"timeout_sec":20} → 200 且全局 config.toml [hooks] timeout_sec=20；
	// 边界：0/61/非数字 → 400；不触碰任何 agent hooks 配置文件（断言 agent home 无 hooks 文件生成）
}

func TestHooksRemoveSingleAgent(t *testing.T) {
	// 预置某 agent（如 kimi，隔离 home）已安装 hooks → POST /api/setup/hooks/remove {"agent":"kimi"}
	// → 200 removed=true、hooks 目标文件内 ok 段移除；重复调用 removed=false；
	// {"agent":"nobody"} → 400
}
```

Run: `go test ./internal/gui/ -run 'TestHooksTimeoutSet|TestHooksRemoveSingleAgent' -v`
Expected: FAIL（404/未定义——先见红）

- [ ] **Step 3: 失败测试——enforce rules**

`internal/gui/api_enforce_test.go` + `internal/config/config_test.go` 追加：

```go
func TestSetEnforceRules(t *testing.T) {
	// config 层：含既有 [retrieve] 等键的 config.toml 写入 2 条规则 → 重读 cfg.Enforce 一致、
	// [retrieve] dedup_turns 等其他小节键原样保留；写空数组 → cfg.Enforce 为空
}

func TestEnforceRulesAPI(t *testing.T) {
	// GET 初始 → {"rules":[]}（或项目默认）；POST 2 条 → 200；GET 复读到 2 条；
	// type="bogus" → 400；code_globs=[] → 400；POST 空数组 → 200 且 GET 回到 []
}
```

Run: `go test ./internal/config/ -run TestSetEnforceRules -v` 与 `go test ./internal/gui/ -run TestEnforceRulesAPI -v`
Expected: FAIL（先见红）

- [ ] **Step 4: 实现 config.SetEnforceRules**

参照同包 SetGate 的行级小节写入模式：定位 `[[enforce]]` 区块（可能 0 个或多个连续表数组块），整体删除后按 rules 重写；无区块时在文件末尾追加；空数组 = 删除全部 `[[enforce]]` 块。每条写出四键：

```toml
[[enforce]]
type = "changelog"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "改代码必须写变更日志"
```

- [ ] **Step 5: 实现四个 HTTP 端点**

`api_hookstimeout.go`：

```go
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
```

`api_enforce.go`：GET 走 `resolveProject` + `config.LoadMerged` 读 `cfg.Enforce`（nil 归一为 `[]`）；POST 解码后逐条校验（type=="changelog"、len(CodeGlobs)>0、Message!=""）→ `config.SetEnforceRules(st.ConfigPath(), rules)`。响应形状照 Produces 契约。

`api.go` 注册（:107 附近的 api(...) 序列中追加）：

```go
	api("POST /api/hooks/timeout", h.apiHooksTimeoutSet)
	api("POST /api/setup/hooks/remove", h.apiSetupHooksRemove)
	api("GET /api/enforce/rules", h.apiEnforceRulesGet)
	api("POST /api/enforce/rules", h.apiEnforceRulesSet)
```

- [ ] **Step 6: 变绿 + 全量**

Run: `go test ./internal/gui/ ./internal/config/ -v`，然后 `go build ./... && go test ./...`
Expected: 新测试 PASS，全量绿

- [ ] **Step 7: Commit**

```bash
git add internal/gui/api_hookstimeout.go internal/gui/api_enforce.go internal/gui/api.go internal/config/config.go internal/config/config_test.go internal/gui/api_hookstimeout_test.go internal/gui/api_enforce_test.go docs/prototypes web
git commit -m "feat(gui): 配置中心后端缺口——hooks/timeout 独立写、enforce rules 读写、单 agent 卸载；原型归档 docs/prototypes"
```

---

### Task 2: 新外壳（index.html / style.css / app.js 框架层）

**Files:**
- Rewrite: `web/index.html`（骨架：顶栏 + 侧栏 + 主区挂载点 + `{{TOKEN}}` 注入）
- Rewrite: `web/style.css`（全量：CSS 变量双主题 + 顶栏/侧栏/卡片/弹窗/开关/树/日志等组件样式——照抄原型 `<style>` 段）
- Rewrite: `web/app.js`（框架层：token 读取、api helper、i18n 字典与 t()、主题切换、hash 路由、侧栏菜单、render/renderBody 滚动保持、五个空占位页）
- Delete: 无（旧三文件原位重写；旧 UI 行为差异由 Task 8 核对清单兜底）

**Interfaces:**
- Consumes: `serveIndex` 的 `{{TOKEN}}` 替换（index.html 内联脚本把 token 赋给 `window.OK_TOKEN`，app.js 的 api helper 每次请求带 `X-Ok-Token` 头——沿用旧 app.js 的 api() 语义）
- Produces（后续 Task 依赖）：`api(path, opts?)`、`t(key)`、`state{menu,lang,theme,...}`、`render()`（滚动保持）、`el(tag,class)`、`pswitch/pcard/prow/pnumLive/ptext/pDirtyLive` 等原型助手原样可用；菜单 key = manage/setup/prefs/logs/misc；占位页文案走 i18n

- [ ] **Step 1: index.html 骨架**——`<div id="app">` 挂载点 + 内联 `<script>window.OK_TOKEN="{{TOKEN}}";</script>` + 引 app.js/style.css/favicon
- [ ] **Step 2: style.css**——原型 `<style>` 段全量平移（含 `.proto-banner` 之类**原型专用**样式删除；原型浮条横幅不进生产）
- [ ] **Step 3: app.js 框架**——原型的 I18N/ICON/MENUS/el/esc/state/render/renderBody（滚动保持）/hash 恢复/主题切换平移；五个菜单页先挂 `placeholder`（复用原型 notImpl 文案键）；`api()` helper：

```js
async function api(path, opts){
  const r = await fetch(path, {
    method: (opts && opts.method) || "GET",
    headers: { "X-Ok-Token": window.OK_TOKEN, "Content-Type": "application/json" },
    body: opts && opts.body ? JSON.stringify(opts.body) : undefined,
  });
  if(!r.ok) throw new Error((await r.json().catch(()=>({}))).error || r.statusText);
  return r.json();
}
```

- [ ] **Step 4: 冒烟**——隔离 OK_HOME 起 okd，Edge headless 截图：五菜单切换、hash 恢复（直接带 `#prefs` 打开落在设置页）、昼夜切换、中英切换各一张，逐张 ReadMediaFile 自查
- [ ] **Step 5: Commit** `feat(gui): 配置中心新外壳——五菜单左右栏、双主题、i18n、hash 路由、滚动保持`

---

### Task 3: 日志页

**Files:** Modify `web/app.js`（renderLogs/paintLogs/轮询）

**Interfaces:**
- Consumes: `GET /api/logs?tail=400&sig=…` → `{"files":[...],"lines":[{"src","text","semantic"}...],"sig","unchanged"}`（api.go:425-463）
- Produces: 无新接口（页面内部函数）

- [ ] **Step 1: 结构/样式/交互照抄原型日志页**（工具行三来源 chip + ◆语义 chip + 过滤框 + 行数统计 + 自动刷新开关；深色控制台；贴底滚动 40px 阈值）
- [ ] **Step 2: mock 数据源换真轮询**：2s `setInterval` 带 sig 请求；`unchanged` 跳过重绘；`lines` 全量替换后按 state.logSrc/logSem/logQ 过滤渲染；仅菜单在 logs 且自动刷新开启时轮询；离开页面不清状态
- [ ] **Step 3: 冒烟**——隔离环境起 okd，先制造几行真实日志（跑 `ok list` 等），headless 截图验证：默认三来源着色、关 daemon chip 后行数变化、过滤词命中、meta 计数
- [ ] **Step 4: Commit** `feat(gui): 日志页——三来源实时查看器（2s sig 轮询，unchanged 跳过重绘）`

---

### Task 4: 其他页

**Files:** Modify `web/app.js`（renderMisc + 文档弹窗 + 删除确认弹窗）

**Interfaces:**
- Consumes: `GET /api/export?project=`（zip 下载，前端 `window.location` 或 a[download] 触发）、`POST /api/import`（multipart 文件——**先核实现有 app.js 的上传方式**（form-data 还是裸 body），照其语义）、`GET /api/changelog`、`POST /api/changelog/seen`、`/help.md`、`DELETE /api/project {"project":name}`、`GET /api/status`
- Produces: 无

- [ ] **Step 1: 六卡结构照抄原型**（导出/导入/更新日志/使用帮助/删除项目知识库/关于；删除卡红边红题）
- [ ] **Step 2: 接线**——导出 select 数据来自 /api/projects（含"全部项目"）；导入走真实文件上传，结果显示行用服务端返回（条数等）；更新日志弹窗渲染 /api/changelog 返回的 markdown（复用原型 renderMd；**实现前先核 /api/changelog 响应形状**， seen 调用时机沿用旧 app.js:595-619 语义）；使用帮助弹窗 fetch /help.md 同渲染；删除弹窗 = 原型确认流（备份勾选 + ack 勾选 + 输入完整项目名，三齐备解锁）→ DELETE /api/project，成功后项目下拉/管理页树联动少一个；关于 = /api/status 的 app_version/home/projects 计数
- [ ] **Step 3: 冒烟**——隔离环境造 2 个项目若干条目：导出得 zip、删除项目全流程（解锁逻辑逐项验证）、两个文档弹窗渲染、关于三行数据
- [ ] **Step 4: Commit** `feat(gui): 其他页——导出/导入/更新日志/使用帮助/删除项目知识库/关于`

---

### Task 5: 管理页

**Files:** Modify `web/app.js`（renderTree/fillTree/renderDetail + 条目操作）

**Interfaces:**
- Consumes: `GET /api/projects`、`GET /api/entries?project=&include_archived=`（**先核实现有响应字段**：title/type/tags/mandatory/draft/archived/file/size/mtime/born 等，树徽标与详情全用得上）、`GET /api/entry?project=&file=`（正文全文）、`POST/PUT/DELETE /api/entry`、`POST /api/approve`、`POST /api/entry/archive`、`GET /api/project/branch-info?project=`（继承徽标 hover 数据，v2.18.2 既有）
- Produces: 无

**方案决策（原型未覆盖处）**：条目操作沿用现有 GUI 语义，位置=详情页右上操作组（编辑/归档或取消归档/删除；草稿条目额外"批准"按钮）；新建条目按钮在树头部搜索框旁；新建/编辑复用同一弹窗（标题/类型/tags/mandatory/摘要/正文 + ✨优化按钮走 `/api/entry/optimize`，沿用旧 app.js 对应逻辑）。

- [ ] **Step 1: 树**（项目→条目两级、类型徽标+mandatory★+draft 徽标+归档置灰、过滤框、计数）照抄原型，数据换真
- [ ] **Step 2: 详情**（相对路径/文件名+大小+mtime/标题/tags+mandatory/摘要/正文 markdown）照抄原型；born/继承徽标 hover 浮动窗沿用现有 v2.18.2 行为（branch-info）
- [ ] **Step 3: 条目操作组 + 新建/编辑弹窗 + 批准/删除/归档**接线；操作后刷新树与详情
- [ ] **Step 4: 冒烟**——隔离环境造项目+条目（含草稿/归档/mandatory/多类型）：树徽标逐项、详情 markdown 渲染、编辑保存、批准草稿、归档往返、删除
- [ ] **Step 5: Commit** `feat(gui): 管理页——项目树 + markdown 详情 + 条目操作（新建/编辑/批准/归档/删除）`

---

### Task 6: 设置页

**Files:** Modify `web/app.js`（renderPrefs + 门控/embedding/LLM 三弹窗）

**Interfaces:**
- Consumes: 映射表"设置"行全部端点 + Task 1 新增的 `POST /api/hooks/timeout`、`GET/POST /api/enforce/rules`；embedding 下载进度轮询沿用旧 app.js 的 dlJob 机制（`/api/setup/embedding/download` 返回后轮询状态——**先核旧 embRefresh/dlJob 语义**再平移）
- Produces: 无

- [ ] **Step 1: 八卡结构照抄原型**（顺序：全局开关/语义检索/模型配置/Hook 超时/跨轮注入冷却/经验沉淀/泛化门控/规则配置；交互范式按 Global Constraints 2-3）
- [ ] **Step 2: 接线**——初始数据一次聚合拉取（status + embedding + llm + retrieve + capture + gate + enforce/rules）；各卡保存语义：开关即存（toggle/gate）、行内保存（hooks/timeout、retrieve、capture）、弹窗确定生效（embedding/LLM/gate 短语表/enforce rules）；pDirtyLive 判脏对"读回的服务端原值"比
- [ ] **Step 3: 三弹窗**（短语表列表式、embedding 服务列表+按类型表单+全局段+下载进度、LLM 列表+表单+测试连接）结构照抄原型，字段与既有端点对齐
- [ ] **Step 4: 冒烟**——隔离环境逐项：开关即存后 config.toml 落盘核对；Hook 超时改值（**不产生** hooks 重装副作用——agent home 无新写入）；冷却/沉淀/规则行内保存与"改回原值变灰"；embedding 三类型各建一个 profile；门控短语增删
- [ ] **Step 5: Commit** `feat(gui): 设置页——八卡（开关/embedding/LLM/超时/冷却/沉淀/门控/规则）+ 三弹窗`

---

### Task 7: 引导页

**Files:** Modify `web/app.js`（renderSetup + agent 卡片）

**Interfaces:**
- Consumes: `GET /api/status`（agents/skillsInstalled/rxEnforceMode 等）、`POST /api/setup/hooks {"agent":id}`、Task 1 的 `POST /api/setup/hooks/remove`、`POST /api/setup/skills`、`POST /api/reasonix/enforce-mode`
- Produces: 无

**保留项（现有 GUI 独有，不得丢）**：Reasonix enforce-mode 卡（仅 reasonix 检测到时显示，旧 app.js:116 附近语义）；Codex 信任门说明按钮与说明卡（web/index.html:92-94 codex-help-card）；技能安装状态 chips（hookOn/skillOn 双态）。

- [ ] **Step 1: agent 卡片照抄原型**（真实品牌字形内联 data-URI、HOOK/插件 类型徽标、未检测到不渲染、安装/卸载按钮按 hooksInstalled 双态、安装中/卸载中态、明细展开"安装会动哪些文件"）
- [ ] **Step 2: 接线**——卡片数据 = /api/status.agents；安装走 setup/hooks {"agent":id}，卸载走 hooks/remove；操作后重拉 status 刷新卡片；头部统计（已检测/已接入）+ 重新检测按钮（重拉 status）
- [ ] **Step 3: Reasonix/Codex 专属件**按现有语义挂入对应卡片
- [ ] **Step 4: 冒烟**——隔离环境用假 agent home 造"已检测未接入/已接入"两态（沿用 e2e 的隔离变量手法），安装→状态翻转→卸载→复原
- [ ] **Step 5: Commit** `feat(gui): 引导页——agent 卡片（检测/安装/卸载）+ Reasonix/Codex 专属件`

---

### Task 8: 横切行为核对 + 收口

**Files:** Modify `web/app.js`（升级首弹）、`web/help.md`（页签结构描述更新）、`docs/ARCHITECTURE.md`（Web GUI 段）、可能 `README.md`/`README_EN.md`（GUI 描述行）

**旧 GUI 横切行为核对清单（逐项给"保留/有意放弃"结论写进报告）：**
1. 升级后首次打开自动弹更新日志（/api/changelog 的 pending + /api/changelog/seen，旧 app.js:595-619）——**必须保留**
2. 管理页分页/时间排序/筛选（旧 GUI 有，新树形态是否等价覆盖）
3. 条目 optimize（✨）按钮 loading/错误态
4. embedding 下载取消、离线提示
5. heartbeat（/api/heartbeat → beats 通道，daemon 存活感知）是否还需要
6. `ok gui` 打开即管理页 vs 新默认页（现状：无项目时旧 GUI 跳引导页——新 UI 同等策略：/api/projects 为空时落 setup）

- [x] **Step 1: 实现升级首弹**（保留项 1）
- [x] **Step 2: 逐项核对 2-6 并落实**
- [x] **Step 3: 文档触点**：help.md 四标签描述改五菜单；ARCHITECTURE.md Web GUI 段重写（五页结构 + 端点面 + 双进程入口 ok gui/OkManager）；README 若有 GUI 截图/描述行同步
- [x] **Step 4: 终验**——`python scripts/build.py` 全量构建（dist/web 同步）；隔离 OK_HOME 起 dist/okd.exe，五个页面逐个 headless 截图自查 + 真实浏览器人工过一遍
- [x] **Step 5: Commit** `feat(gui): 横切收尾——升级首弹保留、help/ARCHITECTURE 文档同步、dist 全量验证`

---

## Self-Review 记录

- **Spec 覆盖**：设计文档 §6 五页全覆盖（Task 2-7）；原型范式→Global Constraints；现有 GUI 独有行为→Task 7 保留项 + Task 8 核对清单；后端缺口→Task 1（4 端点均有核实的底层函数）
- **占位符扫描**：凡"先核实现有语义"处（import 上传方式、changelog 响应形状、entries 字段、dlJob 轮询）均为执行期一次 Grep/Read 可得的接线事实，非设计留白；前端结构/样式规范由原型文件承载（用户已逐屏验收），不重复 1500 行进计划
- **类型一致性**：Task 1 Produces 的端点形状与 Task 6/7 Consumes 逐一对应；`SetEnforceRules(configPath string, rules []EnforceRule) error` 与 config 包既有 Set 函数签名风格一致
- **风险注记**：Task 6 Step 2 的"初始聚合拉取"是多次请求而非新聚合端点（避免后端再动）；管理页条目操作位置是方案决策（原型未覆盖），已在 Task 5 头部声明
