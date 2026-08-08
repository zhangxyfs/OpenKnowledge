# GUI 使用帮助实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** GUI"其他"页加"使用帮助"卡，弹出渲染 `web/help.md`——讲清楚怎么调用、怎么配置。

**Architecture:** 帮助内容走既有静态资源链（`web/help.md` → build.py 拷贝 → dist/web → 安装包）；路由表加一行字面量 `GET /help.md`（路由即白名单）；前端 fetch 后复用 changelog 弹窗与 renderMd。无新 API、无 Go 构建/安装器改动。

**Tech Stack:** Go 1.25；原生 JS SPA；Markdown 内容文件。

**Spec:** `docs/superpowers/specs/2026-08-08-gui-help-doc-design.md`（已批准，目标 v2.7.1）

## Global Constraints

- help.md 内容中的命令、配置键、路径、默认值必须与代码逐字一致（以 internal/cli/cli.go、internal/config/config.go、internal/setupx 为准）
- 不出现未发布功能；功能引入版本用"2.6+"式标注，不写死当前版本号
- 静态资源沿用 serveStatic/no-cache 现状；路由即白名单（字面量注册）
- 弹窗复用 changelog modal；帮助关闭不得触发 `/api/changelog/seen`（changelogFromPending 保持 false）
- dist/ 在 .gitignore：dist/web 同步为文件系统级（cp），不进 git
- 注释/提交信息中文；本机 autocrlf：gofmt 用去 CR 比对；只 git add 本任务文件

---

### Task 1: web/help.md 内容本体

**Files:**
- Create: `web/help.md`

**Interfaces:**
- Produces: 帮助文档全文（下方 Step 1 为完整内容——逐字写入，含 Markdown 结构）

- [ ] **Step 1: 写 web/help.md（以下为完整内容，逐字采用）**

````markdown
# OpenKnowledge 使用帮助

OpenKnowledge 是 AI 编码助手的**本地知识库**：把项目经验、规约、结构文档存下来，在你下次提问时自动注入给 AI——换一次会话、换一个工具，知识都还在。

## 30 秒上手

1. 安装后运行 `ok setup`（或在 GUI 引导页选你的 agent 点"安装"）
2. 到你的项目目录运行 `ok init`（自动用目录名注册，无需起名字）
3. 正常和 AI 对话即可——相关知识会随你的提问自动出现在上下文里

## 怎么调用

### 在 AI 会话里（最常用）

- **知识注入是全自动的**：每次你提问，相关知识摘要自动进入上下文，无需任何操作
- **斜杠技能**（各 agent 均支持，也可用自然语言，如"初始化知识库""把项目沉淀成 wiki"）：

| 技能 | 作用 |
|---|---|
| `/openknowledge-init` | 初始化当前项目（等价 `ok init`） |
| `/openknowledge-propose` | 把本次会话的经验提议为草稿条目（待你批准） |
| `/openknowledge-wiki` | 生成/增量更新项目 wiki（结构文档） |
| `/openknowledge-capture` | 查看/切换经验沉淀模式与轮次间隔 |
| `/openknowledge-on` / `/openknowledge-off` | 全局开启/关闭知识库 hooks |

- **生效时机**：kimi / pi / zcode 的 hook 配置在**新开会话**时加载；reasonix 以插件形式安装，新会话生效（会话中 `/reload` 可重载）

### GUI（`ok gui` 或托盘双击）

- **管理**：条目增删改查、草稿"采纳"、按项目/类型/分支过滤、搜索
- **引导**：安装/重装各 agent 集成、hook 超时、enforce 三档（reasonix）、embedding 语义检索
- **其他**：数据导出/导入（zip 备份）、更新日志、本帮助

### CLI 速查

| 命令 | 作用 |
|---|---|
| `ok init` | 注册当前项目（自动取目录名） |
| `ok setup [--agent <id>]` | 写 hooks/插件 + 装技能 + 配 embedding（交互向导） |
| `ok add --title T --type note` | 直接添加条目（`--tags/--summary/--mandatory/--force/--file`） |
| `ok propose / approve` | 沉淀草稿 / 采纳草稿 |
| `ok list / search <词>` | 列条目 / 检索 |
| `ok capture` | 查看或设置沉淀模式（propose/auto、轮次间隔） |
| `ok wiki status / mark / base / diff` | wiki 状态 / 记游标 / 基准分支 / 分支结构差异 |
| `ok doctor` | 体检：逐 agent 的 hooks 安装状态 |
| `ok on / off` | 全局开关（off 后所有注入与检查暂停） |
| `ok gui` | 打开管理界面 |

### daemon 与托盘

daemon 常驻后台、按需自动拉起，无需手动管理；托盘图标右键看版本/退出，双击聚焦 GUI 窗口。

## 怎么配置

### 全局配置 `~/.openknowledge/config.toml`

| 键 | 默认 | 说明 | 改法 |
|---|---|---|---|
| `[hooks] timeout_sec` | 10 | hook 超时（秒），过短在高负载下会被宿主静默杀死 | GUI 引导页"hook 超时"卡（保存后自动重写所有 agent），或手改后重跑 `ok setup` |
| `[embedding] base_url / model / api_key / timeout_sec` | 空 / 空 / 空 / 5 | 语义检索（可选）；不配则纯关键词检索，照样可用 | `ok setup` 交互向导（带连通性验证） |
| `[reasonix] enforce_mode` | mixed | reasonix 强制检查表达：soft 全软提示 / hard 全硬阻断 / mixed 软+硬 | GUI 引导页三档卡（仅选中 reasonix 时显示），**即时生效** |

### 项目配置（知识库根目录 `config.toml`）

| 键 | 默认 | 说明 | 改法 |
|---|---|---|---|
| `[capture] mode` | propose | 沉淀模式：propose=AI 自主判断 / auto=到间隔自动提醒 | GUI 管理页"经验沉淀"卡，或 `ok capture` |
| `[capture] turn_interval` | 5 | auto 模式的提醒间隔（回合数） | 同上 |
| `[enforce]` | 空 | 强制检查规则（如 changelog_required：改了代码必须更新 CHANGELOG，否则阻断） | 手改文件 |
| `[wiki] stale_commits` | 20 | wiki 落后多少 commit 开始提醒；0 = 关闭提醒 | 手改文件 |
| `[retrieve] top_n` | 2 | 每次注入最多检索命中条数 | 手改文件 |
| `[inject] max_tokens` | 800 | 注入预算（超出截断） | 手改文件 |

### 条目级控制（frontmatter）

- `mandatory: true` → 每会话首次提问**全文注入**（规约类内容用）
- `draft: true` → 草稿，不参与检索注入；GUI 点"采纳"或 `ok approve` 转正
- `tags` 含 `branch:<分支名>` → 该条目只在对应分支注入（2.7+，长期并行分支用）

## 常见问题

- **注入没出现**：①`ok doctor` 看 hooks 是否安装；②agent 必须是**新开会话**（hook 配置在会话启动时加载）；③看日志 `~/.openknowledge/ok.log`
- **想临时停用**：`ok off`（全部链路暂停），`ok on` 恢复
- **数据都在哪**：`~/.openknowledge/`——`registry.toml`（项目注册表）、`projects/<项目>/knowledge/*.md`（条目真源）、`kb.db`（索引，删了会自动重建）、`state/`（会话状态）
- **备份/迁移**：其他页"数据导出"存 zip → 另一台机器"数据导入"（索引自动重建）
- **切了 git 分支**：wiki 是分支感知的（2.6+）——注入会提示"wiki 基于 <基准分支>"；长期并行分支用 `/openknowledge-wiki` 在该分支生成差异条目（2.7+），互不影响
- **卸载**：Windows"应用与功能"里卸载 OpenKnowledge——会清理 hooks/插件登记/技能/embedding 配置并停 daemon；知识库数据保留在 `~/.openknowledge/` 可手动删

## 更多

- 架构与实现细节：仓库 `docs/ARCHITECTURE.md`
- 各版本更新内容：本页"更新日志"卡
````

- [ ] **Step 2: 事实核对（必须逐条过）**

对照代码核实文档中的每一处事实，错了就改文档（不许改代码凑文档）：
1. `grep -n "case \"" cmd/ok/main.go`——CLI 命令清单
2. `sed -n '/func Default/,/^}/p' internal/config/config.go`——默认值（timeout_sec 10、turn_interval 5、stale_commits 20、top_n 2、max_tokens 800、embedding timeout 5）
3. `grep -n "skillTemplates\|openknowledge-" internal/setupx/setupx.go | head`——技能名单（6 个）
4. `grep -n "case \"" internal/cli/cli.go | grep wiki`——wiki 子命令（status/mark/base/diff）
5. `grep -n "add --title" internal/cli/cli.go`——add 标志集

Run: 无测试（纯内容）；核对清单逐项打勾写进报告
Expected: 五处核对全部一致

- [ ] **Step 3: Commit**

```bash
cd D:/develop/OpenKnowledge
git add web/help.md
git commit -m "docs(gui): 使用帮助内容本体（调用/配置/FAQ，事实逐字对码）"
```

---

### Task 2: 路由 + 其他页卡片 + 弹窗渲染

**Files:**
- Modify: `internal/gui/api.go`（mux 路由表加一行）
- Modify: `web/index.html`（其他页加卡）
- Modify: `web/app.js`（点击处理）
- Test: `internal/gui/api_test.go`（追加）
- 同步： `dist/web/`（cp 三文件）

**Interfaces:**
- Consumes: Task 1 的 web/help.md；既有 `serveStatic`、`renderMd`、`openChangelogModal(title, entries)`、`changelogFromPending` 标志
- Produces: `GET /help.md`（字面量路由）；前端帮助卡

- [ ] **Step 1: 写失败测试**

`internal/gui/api_test.go` 追加（fixture 写法以该文件现有静态资源测试为准——现有测试在临时 webDir 造 app.js/style.css 等假文件，照造一个 help.md）：

```go
func TestHelpMdServed(t *testing.T) {
	// webDir 夹具与既有 TestStatic* 相同；写 help.md 假内容 "# 帮助\n"
	req := httptest.NewRequest("GET", "/help.md", nil)
	rec := httptest.NewRecorder()
	// 调 handler（与既有静态测试同路径）
	if rec.Code != 200 {
		t.Fatalf("help.md 应 200，got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "# 帮助") {
		t.Errorf("内容不符: %q", rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("静态资源必须 no-cache")
	}
}
```

（既有测试若有遍历静态路径的列表——如 `[]string{"/app.js", "/style.css", "/favicon.ico"}`——把 `/help.md` 加进对应列表，视各测试语义决定。）

- [ ] **Step 2: 跑测试确认红**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/gui/ -run TestHelpMd -count=1`
Expected: FAIL（404——路由未注册）

- [ ] **Step 3: 实现**

api.go 路由表（`GET /app.js` 那行附近，46-63 行区域）加：

```go
	mux.HandleFunc("GET /help.md", h.serveStatic)
```

index.html 其他页"更新日志"卡之后加：

```html
        <div class="card">
          <div class="card-head"><h3>使用帮助</h3></div>
          <p class="card-desc muted">怎么调用、怎么配置、常见问题。</p>
          <div class="card-actions"><button id="btn-help" type="button" class="btn">查看使用帮助</button></div>
        </div>
```

app.js（`$("btn-changelog")` 处理器附近）加：

```js
  // 使用帮助：拉取 help.md 复用 changelog 弹窗渲染；不属于升级弹窗，不影响 seen
  $("btn-help").addEventListener("click", function () {
    changelogFromPending = false;
    fetch("/help.md").then(function (r) {
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.text();
    }).then(function (md) {
      openChangelogModal("使用帮助", [{ log: md }]);
    }).catch(function () {
      openChangelogModal("使用帮助", [{ log: "帮助文档加载失败，请检查安装是否完整。" }]);
    });
  });
```

注意确认 openChangelogModal 的 entries 形态（`[{log: …}]` 配 renderMd(e.log)）与 changelogFromPending 的作用域（二者在 btn-changelog 处理器中已这样用，照抄形态）。

- [ ] **Step 4: 跑测试 + 同步 dist**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/gui/ -count=1 && cp web/help.md web/index.html web/app.js dist/web/ && go build -o dist/ok.exe ./cmd/ok`
Expected: 测试全 PASS；dist 同步完成

- [ ] **Step 5: Commit**

```bash
cd D:/develop/OpenKnowledge
git add internal/gui/api.go internal/gui/api_test.go web/index.html web/app.js web/help.md
git commit -m "feat(gui): 其他页使用帮助卡——/help.md 静态路由 + 弹窗复用 renderMd"
```

（web/help.md 若 Task 1 已提交则不再重复。）

---

### Task 3: changelog、版本与验收

**Files:**
- Create: `docs/changelogs/2.7.1.md`
- Modify: `installer/openknowledge.iss`（2.7.0→2.7.1）、`cmd/ok/winres.json`（2.7.0.0→2.7.1.0）

- [ ] **Step 1: changelog 2.7.1.md**（格式对齐 2.7.0.md）

```markdown
# 2.7.1

## 新功能
- GUI"其他"页新增"使用帮助"卡：怎么调用（会话内注入/技能/GUI/CLI）、怎么配置
  （全局与项目配置逐项对照表）、常见问题——随安装包分发，离线可查
```

- [ ] **Step 2: 版本 bump + 全量验证**

iss `2.7.1`、winres `2.7.1.0`。
Run: `cd D:/develop/OpenKnowledge && go vet ./... && go test ./... -count=1 && python scripts/build.py --skip-installer`
Expected: vet 无输出；全绿；dist/changelogs/2.7.1.md 存在

- [ ] **Step 3: 真机验收（controller 可做）**

`./dist/ok.exe gui` 或刷新已开 GUI：其他页出现"使用帮助"卡 → 点击出弹窗 → markdown 渲染（标题/表格/代码段）→ 关闭 → 再点"查看更新日志"确认 seen 逻辑未被帮助触发污染。

- [ ] **Step 4: 打包 + Commit**

```bash
cd D:/develop/OpenKnowledge
python scripts/build.py   # 产出 OpenKnowledgeSetup-2.7.1.exe
git add docs/changelogs/2.7.1.md installer/openknowledge.iss cmd/ok/winres.json
git commit -m "docs: v2.7.1 changelog（使用帮助卡）；版本 bump 2.7.1"
```

---

## Self-Review 记录

- **Spec 覆盖**：§2 机制（静态链+路由）→Task 2；§3 内容大纲→Task 1（全文在计划内）；§4 文件表→Task 1/2；§5 测试→Task 2 Step 1；§7 验收→Task 3 Step 3。
- **类型一致性**：`openChangelogModal(title, entries)` 与 `changelogFromPending` 形态以 app.js 现状为准（Task 2 Step 3 注明照抄 btn-changelog 用法）。
- **自审修正**：(a) spec §4 曾写"serveStatic 白名单加一行"——实为路由表字面量注册（路由即白名单），Task 2 按真实机制写；(b) spec 说 `internal/embed` 是 go:embed 包——实为 embedding 客户端包，机制已改静态文件链（spec 已修正）。
- **已知留白**：api_test.go 静态测试的 fixture 函数名以该文件现状为准（Task 2 Step 1 注明）。
