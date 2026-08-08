# GUI 使用帮助设计（其他页"使用帮助"卡）

- 日期：2026-08-08
- 状态：已批准（机制=go:embed + 弹窗复用；内容定位=调用方式与配置方法写实写细）
- 目标版本：v2.7.1

## 1. 背景与目标

GUI"其他"页目前没有使用指导，新用户不知道 OpenKnowledge 怎么用。用户明确要求：帮助文档要**说清楚怎么调用、怎么配置**。

## 2. 机制

- **内容**：新建 `web/help.md`（内容唯一源；随仓评审，版本与代码同步演进）
- **分发**：走既有静态资源链——`build.py` 拷贝 `web/` → `dist/web/` → iss 打进安装包；**零 Go 构建改动、零安装器改动**（注：`internal/embed` 实为 embedding 客户端包，web 资源本来就是 dist/web 文件伺服，`api.go` 的 serveIndex/serveStatic 从 webDir 读盘）
- **伺服**：`serveStatic` 白名单（`api.go:107`）加 `help.md`；静态资源无 token（同 index.html/app.js 现状），前端直接 `fetch("/help.md")`——**不需要新 API**
- **前端**：其他页"更新日志"卡后加**"使用帮助"卡**（描述："怎么调用、怎么配置、常见问题"），点击 fetch `/help.md` 复用 changelog modal + `renderMd` 渲染；拉取失败显示"帮助文档加载失败"，不影响主界面
- 弹窗标题"使用帮助"；关闭不触发任何状态写（与常驻入口同级，无 seen 概念）

## 3. docs/HELP.md 内容大纲（评审重点）

> 目标读者：装完 OpenKnowledge 的用户。目标：5 分钟知道怎么调用、去哪配置。

### 3.1 30 秒上手
1. 安装后 `ok setup`（或在 GUI 引导页选 agent 点安装）
2. 到项目目录 `ok init`（自动取目录名注册）
3. 正常用 agent——新会话开始，相关知识自动出现在上下文里

### 3.2 怎么调用（按入口分四小节）

**a) agent 会话内（最常用）**：
- 注入是自动的：每次提问，相关知识摘要自动进上下文（无需任何操作）
- 技能调用（斜杠或自然语言）：`/openknowledge-init`（初始化）、`/openknowledge-propose`（沉淀经验）、`/openknowledge-wiki`（生成/更新项目 wiki）、`/openknowledge-on`/`/openknowledge-off`（全局开关）、`/openknowledge-capture`（切沉淀模式）；自然语言等价说法（"把项目沉淀成 wiki""初始化知识库"）
- 各 agent 生效差异：kimi/pi/zcode 配置即写即生效（**新开 session** 才加载 hook）；reasonix 是插件包（装后新会话生效，`/reload` 可重生 sidecar）

**b) GUI**：管理页（条目增删改、草稿采纳、项目/类型/分支过滤、搜索）、引导页（agent 安装、hook 超时、enforce 三档、embedding）、其他页（导出/导入、更新日志、本帮助）

**c) CLI 速查**（表格）：`ok init / setup [--agent] / add / propose / approve / list / search / index / capture / wiki status|mark|base|diff / doctor / on / off / gui`，每个一句话

**d) daemon 与托盘**：常驻 daemon 自动拉起无需管理；托盘右键看版本/退出、双击聚焦 GUI

### 3.3 怎么配置（按配置文件分三小节）

**a) 全局 `~/.openknowledge/config.toml`**（表格：键、默认、说明、改动方式）：
- `[hooks] timeout_sec`（默认 10；GUI 超时卡或手改，改后重跑 setup 生效；reasonix 换算为插件 timeoutMillis）
- `[embedding] base_url/model/api_key/timeout_sec`（可选；不配则纯关键词检索；`ok setup` 有交互向导）
- `[reasonix] enforce_mode`（soft|hard|mixed；GUI 三档卡即时生效）

**b) 项目级 `<kb>/config.toml`**（同表格）：
- `[capture] mode`（propose|auto）与 `turn_interval`——对应管理页"经验沉淀"卡
- `[enforce]`（如 changelog_required）——改代码必须更日志，否则阻断（reasonix 按三档表达）
- `[wiki] stale_commits`（默认 20，0 关闭落后提醒）
- `[retrieve] top_n` / `[inject] max_tokens`（默认 2 / 800）

**c) 图形界面等价物**：哪些配置有 GUI 入口（沉淀卡/超时卡/三档卡/embedding 卡），哪些要手改文件

### 3.4 常见问题（FAQ）
- 注入没出现 → `ok doctor` 查 hooks；确认 agent 是新开会话；看 `~/.openknowledge/ok.log`
- 想临时停用 → `ok off`（全部链路暂停），`ok on` 恢复
- 数据在哪 → `~/.openknowledge/`（registry.toml、projects/<名>/knowledge/*.md、kb.db、state）；备份用其他页导出 zip
- 换分支后 wiki 内容不对 → 注入会提示基准分支；长期并行分支用 `ok wiki diff` + 差异条目（见 wiki 技能）
- 多机器迁移 → 导出 zip → 目标机导入（索引自动重建）

### 3.5 写作约束
- 全文 ≤ 250 行，命令/键名/路径与代码逐字一致（以 cli.go/config.go/setupx 为准）
- 不出现未发布功能；截图不放；**不用表格**——renderMd 只支持标题/列表/粗体/行内代码，配置对照一律用列表（终审发现）
- 版本号不写死具体值（用"2.6+""2.7+"标注功能引入版本）

## 4. 组件与文件

| 文件 | 职责 |
|---|---|
| `web/help.md`（新） | 帮助内容本体（唯一源，随 web/ 分发链入包） |
| `internal/gui/api.go`（改） | serveStatic 白名单加 `help.md`（一行） |
| `web/index.html`（改） | 其他页加"使用帮助"卡 |
| `web/app.js`（改） | 点击 fetch `/help.md` 复用 changelog modal 渲染 |
| `dist/web/`（同步） | 文件系统级同步 |

## 5. 错误处理与测试

- 静态资源走既有 serveStatic 白名单（无 token，同 app.js）；前端 fetch 失败 → 弹窗显示"帮助文档加载失败"，不阻断主界面
- 测试：api_test 覆盖 `GET /help.md` 200（Content-Type 与白名单行为同其他静态资源）+ 非白名单文件仍 404；HELP 内容评审走 spec 审阅；手动验收：GUI 其他页点卡出弹窗、markdown 渲染正确（标题/表格/代码段）、关闭正常

## 6. 明确不做

- 不做在线更新/多语言；不做应用内搜索帮助内容；不改 README（README 可后续加一行指向 docs/HELP.md，不在本期）

## 7. 验收标准

1. 其他页"使用帮助"卡点开弹窗，markdown 渲染正确（标题/表格/代码段）
2. 内容中的命令、配置键、路径与代码逐字一致（抽查 5 处）
3. `GET /help.md` 在白名单内可访问、非白名单文件仍 404；全新安装包构建后帮助可用（dist/web/help.md 存在）
