# OpenKnowledge 项目架构与技术说明

## 目录

- [1. 项目概述](#1-项目概述)
- [2. 技术栈](#2-技术栈)
- [3. 模块架构](#3-模块架构)
- [4. 目录结构](#4-目录结构)
- [5. 核心组件详解](#5-核心组件详解)
- [6. 核心业务架构](#6-核心业务架构)
- [7. 数据流与事件流](#7-数据流与事件流)
- [8. 存储层](#8-存储层)
- [9. 外部集成](#9-外部集成)
- [10. 性能与可靠性策略](#10-性能与可靠性策略)
- [11. 依赖关系图](#11-依赖关系图)
- [12. 构建配置与命令](#12-构建配置与命令)
- [13. CLI 命令面](#13-cli-命令面)
- [14. 测试与验证](#14-测试与验证)
- [15. 常见问题排查](#15-常见问题排查)
- [16. 后续维护建议](#16-后续维护建议)
- [17. 检索算法实现（深度）](#17-检索算法实现深度)
- [18. 配置参数参考](#18-配置参数参考)

---

## 1. 项目概述

**OpenKnowledge** 是一个为 AI 编程助手提供项目知识库的命令行工具，编译产物为单二进制 `ok`。知识按项目隔离存储，通过 **Kimi Code 的 hooks 机制**在 AI 会话中自动注入项目约定与踩坑经验，并能对"必须写变更日志"这类强制工作流做机制级检查。

| 功能 | 说明 |
|------|------|
| **基础注入** | 每会话首次提问时，把 mandatory 知识条目全文 + 知识索引注入 AI 上下文 |
| **检索注入** | 每次提问时做关键词 + 向量语义混合检索，注入最相关的知识条目 |
| **强制检查** | 跟踪 AI 修改过的文件，回合结束时检查强制规则（如"改代码必须写变更日志"），不满足则阻断 |
| **知识管理** | `ok init/add/search/index/list/doctor` 命令维护知识库 |
| **首次引导** | `ok setup` 一键写入 hooks 配置、安装 kimi 技能、配置 embedding |
| **全局开关** | `ok on` / `ok off` 随时启停全部 hooks |

**模块名**: `openknowledge`
**二进制名**: `ok`（Windows 为 `ok.exe`）
**设计文档**: `docs/superpowers/specs/2026-07-22-openknowledge-design.md`

---

## 2. 技术栈

**表格 A — 核心技术栈**：

| 类别 | 技术/版本 |
|------|-----------|
| 语言 | **Go**（go.mod 声明 `go 1.25.0`） |
| 构建 | Go 标准工具链，单二进制产出，无 CGO（SQLite 用纯 Go 的 modernc.org/sqlite） |
| CLI 解析 | 标准库 `flag`（刻意不引 cobra） |
| HTTP | 标准库 `net/http`（embedding API 调用） |
| 测试 | 标准库 `testing` + `net/http/httptest` |

**表格 B — 第三方依赖清单**（全部，仅 4 个，与 `go.mod` 一致）：

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/BurntSushi/toml` | v1.6.0 | registry.toml 与各层 config.toml 解析 |
| `gopkg.in/yaml.v3` | v3.0.1 | 知识条目 frontmatter 解析 |
| `github.com/bmatcuk/doublestar/v4` | v4.10.0 | 强制规则的 `**` glob 匹配 |
| `modernc.org/sqlite` | v1.54.0 | kb.db 索引库（FTS5 全文检索 + 向量存储），纯 Go 无 CGO |

---

## 3. 模块架构

单 module（`openknowledge`），15 个包，严格单向依赖、无环：

```
┌─────────────────────────────────────────────────────┐
│ cmd/ok                （二进制入口 + 子命令调度）     │
└───────┬───────────────────┬─────────────────┬───────┘
        │                   │                 │
┌───────▼────────┐  ┌───────▼────────┐  ┌─────▼─────────────┐
│ internal/cli   │  │ internal/gui   │  │ internal/hook     │
│ （人用的命令）  │  │ （Web GUI）    │  │ （kimi hooks 入口）│
└───────┬────────┘  └───────┬────────┘  └─────┬─────────────┘
        │                   │         ┌───────▼────────┐
        │                   │         │ internal/project│（cwd→项目）
        │                   │         └───────┬────────┘
   ┌────▼───────────────────▼─────────────────▼───────────────────┐
   │ 基础层（被上层直接组合）                                       │
   │ registry · entry · config · store · embed · index ·          │
   │ retrieve · state · enforce · setupx                          │
   └───────────────────────────────────────────────────────────────┘
```

**依赖关系**（→ 表示 import）：

- `cmd/ok` → `cli`、`gui`、`hook`
- `cli` → `registry`、`entry`、`store`、`embed`、`index`、`retrieve`、`project`、`config`、`setupx`
- `gui` → `registry`、`entry`、`store`、`index`、`retrieve`、`config`、`setupx`
- `hook` → `project`、`registry`、`store`、`embed`、`index`、`retrieve`、`state`、`enforce`
- `setupx` → `registry`、`config`、`embed`（setup 引导共享逻辑，cli 与 gui 复用）
- `project` → `registry`、`config`、`store`
- `index` → `entry`、`embed`、`retrieve`、`config`（+ modernc.org/sqlite）
- `retrieve` / `embed` / `store` → 仅标准库
- `enforce` → `config`、`state`（+ doublestar）
- `registry` → BurntSushi/toml；`entry` → yaml.v3；`config` → BurntSushi/toml
- `state` → 仅标准库

**分层原则**：`hook`、`cli`、`gui` 是三个互不 import 的应用层；`project` 是 hook 与 cli 共享的项目解析层；`setupx` 是 cli 与 gui 共享的引导逻辑层；其余为单一职责的基础包。

---

## 4. 目录结构

```
OpenKnowledge/
├── go.mod / go.sum                # 模块定义（4 个第三方依赖）
├── cmd/ok/
│   ├── main.go                    # 入口：子命令调度，hook 路径 panic-recover 兜底
│   └── integration_test.go        # 端到端测试（编译真实二进制驱动）
├── internal/
│   ├── registry/                  # ★ 项目注册表与路由
│   │   ├── registry.go            #   Registry/Project、NormalizePath、FindByCwd、HooksDisabled
│   │   └── registry_test.go
│   ├── entry/                     # ★ 知识条目
│   │   ├── entry.go               #   frontmatter 解析/序列化、Load/LoadTolerant、Slug
│   │   └── entry_test.go
│   ├── config/                    # 配置
│   │   ├── config.go              #   Config 各节、Default、Load/LoadMerged、ResolvedAPIKey
│   │   └── config_test.go
│   ├── store/                     # KB 存储布局
│   │   ├── store.go               #   目录路径、token 预算截断
│   │   └── store_test.go
│   ├── embed/                     # 向量
│   │   ├── embed.go               #   Client 接口、OpenAI 兼容客户端、Cosine
│   │   └── embed_test.go
│   ├── index/                     # ★ SQLite+FTS5 索引库（kb.db）
│   │   ├── db.go                  #   Open/Close/Count、旧版 vectors.json 迁移
│   │   ├── sync.go                #   增量同步（filename+mtime）、损坏条目跳过、INDEX.md 重建
│   │   ├── query.go               #   FTS5 BM25 + 余弦混合查询、Mandatory
│   │   └── index_test.go
│   ├── retrieve/                  # 检索分词
│   │   ├── retrieve.go            #   Terms（CJK 二元组）
│   │   └── retrieve_test.go
│   ├── state/                     # 会话状态
│   │   ├── state.go               #   Session（触碰文件/已阻断规则/已基础注入/wiki 已提示）、Clean
│   │   └── state_test.go
│   ├── wiki/                      # wiki 游标与落后计数（叶子包：stdlib + 外部 git 命令）
│   │   ├── wiki.go                #   Cursor 读写（state/wiki.json）、CheckStatus（git rev-list --count）
│   │   └── wiki_test.go
│   ├── enforce/                   # 强制规则
│   │   ├── enforce.go             #   changelog_required 判定（doublestar 匹配）
│   │   └── enforce_test.go
│   ├── project/                   # 项目解析（hook 与 cli 共享）
│   │   ├── project.go             #   Context{Project,Store,Config}、FromCwd
│   │   └── project_test.go
│   ├── hook/                      # ★ hooks 事件处理
│   │   ├── hook.go                #   Event 解析、HandlePrompt/HandlePostTool/HandleStop
│   │   └── hook_test.go
│   ├── cli/                       # 管理命令
│   │   ├── cli.go                 #   Init/Add/Search/Index/List/Doctor
│   │   ├── setup.go               #   Setup 引导（编排 setupx，交互收集 embedding）
│   │   ├── toggle.go              #   On/Off 全局开关
│   │   └── *_test.go
│   ├── setupx/                    # setup 共享逻辑（cli 与 gui 复用）
│   │   ├── setupx.go              #   HooksBlockFor/UpsertHooksBlock/InstallSkills/SaveEmbedding/TestEmbedding/Enable/Disable
│   │   └── setupx_test.go
│   └── gui/                       # ★ Web GUI（ok gui / 无参数启动）
│       ├── server.go              #   127.0.0.1 随机端口服务、令牌生成、浏览器自动打开（同步最大化）
│       ├── api.go                 #   Handler 路由、令牌鉴权、管理 API（条目 CRUD/检索/setup/toggle）
│       ├── window_windows.go      #   Windows 窗口最大化兜底（maximizeWindowByTitle）
│       ├── window_other.go        #   非 Windows 平台无操作实现
│       └── api_test.go
├── web/                           # GUI 前端（零依赖原生 HTML/JS/CSS，三标签页：管理/引导/其他）
│   ├── index.html                 #   页面骨架（{{TOKEN}} 占位符由服务端注入令牌）
│   ├── app.js                     #   条目 CRUD、检索预览、引导流程、心跳（5s）
│   └── style.css
├── scripts/
│   └── build-dist.sh              # 发布构建：-ldflags "-s -w" + 版本注入，产出 dist/ok.exe + dist/web/
├── dist/                          # 发布产物（.gitignore 忽略）：ok.exe + web/
├── docs/
│   ├── ARCHITECTURE.md            # 本文档
│   ├── changelogs/                # 变更日志（强制规则要求的落点）
│   └── superpowers/
│       ├── specs/                 # 设计文档
│       └── plans/                 # 实施计划
└── .superpowers/sdd/              # 执行过程草稿（brief/report/diff，不入库）
```

---

## 5. 核心组件详解

### 5.1 registry — 项目注册表与路由（102 行）

知识库的全局定位层。`Home()` 返回 KB 根目录（`OK_HOME` 环境变量优先，否则 `~/.openknowledge`）。`Registry` 持久化在 `registry.toml`，核心是 **最长前缀匹配** 的项目路由：

```go
func (r *Registry) FindByCwd(cwd string) *Project  // 规范化后最长前缀匹配
func NormalizePath(p string) string                 // "\"→"/"、全小写、去尾 "/"
func HooksDisabled() bool                           // 全局开关标志文件存在性
```

Windows 下大小写不敏感与分隔符混乱问题全部收敛到 `NormalizePath` 一处。匹配不到项目的目录静默放行（fail-open 的起点）。

### 5.2 entry — 知识条目（145 行）

知识的最小单位：`---\n<yaml frontmatter>\n---\n<body>` 格式的 Markdown 文件。frontmatter 含 `title/type/tags/mandatory/summary`，`Body` 与磁盘路径 `Path` 不序列化（`yaml:"-"`）。解析容忍 CRLF 与 UTF-8 BOM；`type` 限定 `rule|pitfall|note|reference` 四种。

- `Load(dir)` — 严格模式，任何文件解析失败即整体报错（`ok list` 用，错误要暴露给用户）
- `LoadTolerant(dir)` — 宽容模式，坏文件跳过并收集错误；已不在生产路径上——注入路径的容错由 `index.Sync` 的损坏跳过实现（见 5.6），保留为可用 API

### 5.3 config — 三层配置合并（95 行）

```go
func LoadMerged(projectPath, globalPath string) (Config, error)
```

生效配置 = **内置默认 ← 全局 `~/.openknowledge/config.toml` ← 项目 `config.toml`**，后者覆盖前者（TOML 依次解码到同一 struct 实现）。`Enforce` 规则只有项目级。API key 解析收敛在一处：

```go
func (e Embedding) ResolvedAPIKey() string  // api_key 字段 > api_key_env 环境变量 > ""
```

### 5.4 store — KB 存储布局（39 行）

纯路径计算层：`KnowledgeDir()/IndexPath()/KbPath()/StateDir()/ConfigPath()`；另有 `TruncateToBudget` 按"字符数(rune) ÷ 2"保守估算 token 并截断注入文本。INDEX.md 的生成已移交给 `index` 包。

### 5.5 embed — 向量客户端（90 行）

- `Client` 接口隔离 HTTP 细节，测试用 `httptest` fake server，不碰真实网络
- `OpenAIClient` 实现 OpenAI 兼容协议：`POST {base_url}/embeddings`，`Authorization: Bearer <key>`，带 context 超时
- 条目向量不再存 vectors.json，而是存于 kb.db 的 `vectors` 表（float32 小端 blob，见 5.6）；旧版 vectors.json 在首次打开 kb.db 时自动导入并改名为 `.bak`

### 5.6 index/retrieve — 索引化混合检索（db.go 138 + sync.go 240 + query.go 138 + retrieve.go 44 行）

检索不再逐文件扫描 Markdown，而是查询 SQLite 索引库 `kb.db`（位于各项目 KB 根目录）。同步按 filename+mtime 增量（枚举优先、只解析变化文件）；查询为 `score = α·归一BM25 + β·余弦` 的混合打分。**草稿条目（frontmatter `draft: true`，由 `ok propose` 写入）不进 FTS 与向量，检索与注入一律排除；INDEX.md 中以【草稿】标记，批准（`ok approve` / GUI 采纳）后才参与检索**。**算法实现细节（分词、BM25、归一化、混合、降级矩阵、实测性能）见第 17 章**，配置参数见第 18 章。

### 5.7 state — 会话状态（96 行）

`Session{SessionID, Touched, BlockedRules, BaseInjected}` 持久化到 `state/session-<净化id>.json`（sessionID 净化为安全文件名，防路径穿越）。三个职责：记录触碰文件（enforce 的证据）、阻断记忆（同会话同规则只阻断一次，防死循环）、基础注入标记（每会话只注入一次）。`Clean` 清理 7 天前的状态文件。

### 5.8 enforce — 强制规则判定（34 行）

v1 仅 `changelog_required`：触碰文件中存在匹配 `code_globs` 的 且 不存在匹配 `changelog_glob` 的 → 阻断并返回用户配置的 message。用 doublestar 做 `**` glob 匹配（`**/*.go` 可匹配根目录文件）。刻意**不理解**变更日志的细则——细则写在知识条目里由注入教给 AI，hook 只做机械检查。

### 5.9 hook — hooks 事件处理（337 行）

三个 handler 共享同一套防御结构：**第一行检查全局开关 → 解析事件 → 路由项目 → 各自逻辑 → 任何错误只记 ok.log 并 exit 0**。

- hook 入口自愈：开关开启时仅在 `HandlePrompt`（hook prompt）入口先跑 `selfHealHooks`——遍历 `agentx.Detected()` 逐 agent 调 `EnsureHooks`（kimi 标记块被 kimi-code 清掉时自动备份并重写；pi 扩展内容过期时重写，文件不存在则不动），错误只记 ok.log（fail-open）

- `Event` 的 `Prompt` 是 `json.RawMessage`，`PromptText()` 兼容两种真实载荷形态（字符串 / `[{"type":"text","text":"..."}]` 数组）
- `FilePath()` 取 `tool_input.path`（kimi 实际字段），兼容 `file_path`
- `HandlePrompt`：打开 kb.db → **查询前增量同步**（`Sync`，无 key 时跳过向量；返回 `*CorruptEntriesError` 时记 ok.log 后继续注入）→ 每会话首次提问做基础注入（`Mandatory()` 全文 + INDEX.md，标记 `BaseInjected` 且仅当内容非空才置位）→ 每次提问 `Query` 混合检索注入；embedding 失败降级纯关键词
- `HandlePostTool`：记录触碰文件（经 `relativize` 转项目相对、小写、`/` 分隔）
- `HandleStop`：先按 `[capture]` 配置评估 **auto 自省**——`mode = "auto"` 且本轮有触碰文件、距上次提醒满 `turn_interval` 个 Stop 时，输出自省提醒并以 exit 2 阻断一次（强制 AI 复盘本轮经验、值得沉淀则当场 `ok propose` 草稿）；随后评估 enforce 规则，命中即 `MarkBlocked` → 保存状态 → stderr 输出 message → **exit 2**（全项目唯一非零出口）

### 5.10 cli — 管理命令（cli.go 369 行 + setup.go 147 行 + toggle.go 38 行）

- `cli.go`：`Init`（项目名缺省取目录基名）、`Add`（重复条目拒绝；后接索引库同步）、`Propose`（AI 面向的草稿写入：`draft:true`、只同步 INDEX 不算向量）、`Approve`（草稿转正，同步 INDEX 并补算向量；同一秒内 mtime 不变时手动推进一秒防 diff 漏判）、`CaptureCmd`（打印或设置项目 `[capture]` 模式，整段替换幂等写入）、`Search`（检索预览，走 `index.Query`）、`Index`（索引库增量同步并打印条目数）、`List`（文件扫描，人用命令开销可忽略）、`Doctor`（注册表/配置/embedding 连通性/hooks 安装状态/开关状态）
- `setup.go`：见第 6.4 节
- `toggle.go`：`On`/`Off` 即删除/创建 `~/.openknowledge/hooks-disabled` 标志文件

### 5.11 gui — Web 管理界面（server.go 27 行 + window_*.go + api.go 891 行）

`ok gui` 或无参数运行（双击 exe）启动的本地 Web 管理界面，供不熟悉命令行的用户完成首次引导与日常知识维护。

- **server.go**：`net.Listen("tcp", "127.0.0.1:0")` 随机端口、仅监听回环；16 字节随机 hex 令牌；启动后自动打开浏览器（Edge/Chrome 应用模式 → 默认浏览器，全失败只打印 URL）；最大化窗口（`maximizeWindowByTitle`，window_windows.go 轮询置顶窗口标题，非 Windows 无操作）为**同步调用**——daemon 化后 `ok gui` 开浏览器即退，协程会随进程死亡。web 资源目录由 `cmd/ok` 定位：`<exe目录>/web` 优先，其次 `<当前目录>/web`（`scripts/build-dist.sh` 产出的 dist/ 布局正好满足前者）。
- **api.go**：`/api/*` 全部经 `X-Ok-Token` 头鉴权（缺失/错误 401）；`/` 返回注入令牌的 index.html（`{{TOKEN}}` 替换）；静态资源仅白名单 `app.js`/`style.css`；条目文件名参数必须是不含 `..` 与路径分隔符的 `.md` 基本名（防路径穿越）；写操作（POST/PUT/DELETE entry）落盘后自动 `index.Sync` 同步索引库。

API 一览：

| 方法 | 路径 | 作用 |
|------|------|------|
| GET | `/api/status` | 项目列表 + `agents` 数组（每个已注册 agent 的 `id/name/detected/hooksInstalled`，顶层 `hooksInstalled` 已移除）+ `skillsInstalled` + embedding 安装状态 + 全局开关状态 + `app_version`（构建期注入版本）与 `home`（KB 根目录）（前端据此决定默认标签页） |
| GET | `/api/projects` | 注册表项目列表 |
| GET | `/api/entries?project=` | 项目条目摘要列表 |
| GET | `/api/entry?project=&file=` | 条目详情（含正文） |
| POST | `/api/entry` | 新建条目（标题 slug 定文件名，重复 409）→ 同步索引 |
| PUT | `/api/entry` | 编辑条目 → 同步索引 |
| DELETE | `/api/entry?project=&file=` | 删除条目 → 同步索引 |
| GET | `/api/search?project=&q=` | 检索预览（走 `index.Query`，无 embedding 客户端） |
| POST | `/api/approve` | `{"project","file"}` 草稿转正（等价 `ok approve`；缺文件/非草稿 400）→ 同步索引与向量 |
| GET | `/api/capture?project=` | 当前捕获模式 `{mode, turn_interval}`（合并配置） |
| POST | `/api/capture` | `{"project","mode"}` 写项目 `[capture]` 小节（等价 `ok capture <mode>`；非法模式 400） |
| POST | `/api/setup/hooks` | 等价 `ok setup` 的 hooks 步骤；body 可指定 `{"agent":"<id>"}` 只装单个 agent（未知 id 400），缺省为全部已检测 agent；响应 `installed` 列出每个 agent 的写入目标 |
| POST | `/api/setup/skills` | 安装五个 kimi 技能 |
| POST | `/api/setup/embedding` | 保存 embedding 配置并当场连通性验证（`{"ok":bool,"error":…}`） |
| POST | `/api/toggle` | `{"on":bool}` 全局开关（等价 `ok on`/`ok off`） |
| POST | `/api/heartbeat` | 页面心跳（204） |
| POST | `/api/shutdown` | 停服 |
| POST | `/api/uninstall` | 卸载集成：移除 hooks 标记块、技能目录、全局 [embedding]；KB 数据保留（`setupx.Uninstall`） |
| GET | `/api/export?project=<名\|all>` | 知识库导出 zip（`backup.Export`；project 缺省 all，项目不存在 404） |
| POST | `/api/import` | multipart `file` 上传备份 zip 导入（`backup.Import`，32MB 上限；`ErrBadPackage` → 400，成功返回 `Report{imported, skipped, projects}`） |

前端 `web/`（零依赖原生 HTML/JS/CSS）：「管理」标签页（项目/条目列表、新建/编辑/删除、检索预览带命中高亮、草稿条目带「草稿」徽标与「采纳」按钮、全局开关）+「引导」标签页（hooks/技能/embedding/全局开关状态卡、「经验沉淀」卡片查看/切换 capture 模式与轮次间隔、危险区「卸载」卡片）+「其他」标签页（数据导出/导入、关于卡片显示版本与项目数）。hooks 未安装时「管理」页隐藏，「引导」为默认页。

### 5.12 backup — 知识库导出/导入（251 行）

GUI「其他」tab 背后的备份包（叶子包：stdlib zip + registry/entry/store/index）：

- `Export(w io.Writer, project string)`：registry.toml + 各项目 `knowledge/*.md` + `config.toml` 打成 zip；`project="all"` 全导，单项目时 registry 随之过滤
- `Import(r io.ReaderAt, size int64)`：`MaxSize` 32MB 上限、zip-slip 防护（拒绝 `..`/绝对路径）、只接受 `registry.toml`/`projects/<名>/knowledge/*.md`/`projects/<名>/config.toml` 三类路径；条目 .md 过 `entry.Parse` 失败计 skipped 不阻断；同名覆盖、缺失项目自动注册（同名已注册则合并进现有目录）；最后逐项目 `index.Sync` 重建索引
- 返回 `Report{imported, skipped, projects}`；客户端侧错误统一包 `ErrBadPackage`（HTTP 层映射 400）

### 5.13 version — 构建期注入的应用版本号（6 行）

`var Version = "dev"`；`scripts/build-dist.sh` 用 sed 从 `installer/openknowledge.iss` 的 `#define AppVersion` 提取版本，经 `-ldflags -X openknowledge/internal/version.Version=` 注入——版本事实源只有 .iss 一处，裸 `go build` 为 `dev`。经 `/api/status` 的 `app_version` 暴露给前端。

---

## 6. 核心业务架构

### 6.1 注入链路（知识 → AI 上下文）

```
用户发消息
  → kimi 触发 UserPromptSubmit
  → 执行 "ok.exe hook prompt"，stdin 喂事件 JSON
  → ok：开关检查 → 项目路由 → 打开 kb.db → 查询前增量同步（filename+mtime）
  → 首次提问？→ 输出 mandatory 全文 + INDEX.md（置 BaseInjected）
  → 每次提问 → embed 提问(失败降级) → index.Query 混合打分 → top-N 全文
  → TruncateToBudget 截断 → stdout
  → kimi 把 stdout 追加进模型上下文
```

**目标**：AI 在被问之前就已知项目约定与相关经验。基础注入每会话一次（内容随后存在于会话历史），检索注入每次都有。

### 6.2 强制链路（规则 → 机制保证）

```
AI 用 Write/Edit 改文件
  → PostToolUse → "ok.exe hook post-tool" → 文件记入 Session.Touched

AI 回合结束
  → Stop → "ok.exe hook stop" → EvalChangelog 判定
  → 命中：stderr 输出 message + exit 2 → kimi 要求 AI 继续执行（补日志）
  → 同会话同规则只阻断一次（BlockedRules 记忆）
  → AI 补写日志后 PostToolUse 记录，下次 Stop 自然放行
```

**目标**：把"每次代码修改必须立即记录变更日志"从靠 AI 自觉变为机制强制。

### 6.3 知识维护链路（人 → 知识库）

```
ok init [名字]   → registry 注册 + KB 骨架（项目名缺省取目录基名）+ 幂等写入/更新 hooks 配置
ok add --title … → 写条目 → 同步索引库（INDEX.md + 有 key 则为变化条目算向量）
手工编辑条目     → 下次提问时 hook 查询前自动增量同步；或 ok index 手动同步
ok search <词>   → 命令行预览检索效果（调试注入质量）
```

### 6.4 首次引导（ok setup）

```
ok setup [--agent <id>]
  → os.Executable 取自身绝对路径（hooks 命令不依赖 PATH）
  → 对目标 agent 写入 hooks 集成：缺省 = 全部已检测 agent（agentx.Detected()）；
    --agent 指定单个（未知 id 报错并列出可用 id；未检测到该 agent 也写入并提示）；
    一个都未检测到时跳过 hooks 写入继续后续步骤
      kimi：备份 ~/.kimi-code/config.toml → 标记块幂等写入 3 条 hook
      pi：渲染 TS 扩展写入 ~/.pi/agent/extensions/openknowledge.ts（既有非本工具文件先备份）
  → 安装 openknowledge-init/on/off/propose/capture/wiki 六个技能到 ~/.agents/skills/（烧入 exe 路径）
  → 交互（或 flags）收集 embedding base_url/model/API key
      → 写全局 ~/.openknowledge/config.toml（0600）→ 立即连通性验证
  → 打印引导
```

幂等性由"先清除存量 ok hooks + 标记块原位替换"保证：写入前 `StripLegacyOKHooks` 移除所有指向 ok hook 的无标记 `[[hooks]]` 表（历史手动粘贴遗留），重复执行或更换 exe 路径只覆盖更新、绝不重复堆积；标记损坏（有头无尾）时报错拒绝修改，不破坏用户配置。`ok init` 复用同一写入逻辑（`writeHooks`，写失败不阻断注册）；卸载遍历 `agentx.All()` 逐 agent `RemoveHooks`（kimi 清除标记块与无标记存量，pi 只删本工具生成的扩展文件）。daemon 化后 `ok gui` 不再阻塞，GUI 页面关闭不退出进程（原 30s 心跳看门狗已随 daemon 化移除）。

### 6.5 常驻 daemon（单进程架构）

全系统只有一个 ok.exe 常驻进程，承载 GUI 与 kimi hook 请求：

- internal/daemonx（叶子包）：daemon.json 凭证（pid/port/token/exe指纹）、健康检查、版本判定
- internal/daemon（编排包）：HTTP mux（/api/health、/api/hook/* + gui.Handler）、Run（端口即单实例锁，
  默认 17888，占用回退随机）、spawn（DETACHED 后台拉起，15s 防抖）、ForwardHook（瘦客户端转发，
  9s 超时 fail-open）、OpenGUI（ensure + 开浏览器即退；窗口最大化为同步调用——开浏览器即退的进程里协程会随之死亡）
- exe 指纹 = 路径|size|mtime：exe 升级后客户端发现指纹不一致 → 旧 daemon shutdown → 拉起新版
- hook 兜底：daemon 不在时本次请求本地直接处理（hook.Handle* 原逻辑），同时后台拉起 daemon
- 安装器写 HKCU Run 登录自启；卸载/ok daemon stop/setupx.Uninstall 均可停 daemon
- kb.db 所有写入收敛到 daemon 单进程；index.Open 另加 busy_timeout(3000) 兜底短暂并发
- 系统托盘（internal/tray）内嵌 daemon 进程：右下角图标，单击弹菜单（版本号 + 退出）、双击打开/聚焦唯一 GUI 窗口；菜单"退出"与 `ok daemon stop` 同走 /api/shutdown 链路

### 6.6 wiki（项目 wiki 的生成驱动与落后提醒）

```
openknowledge-wiki 技能（AI 驱动）
  → 扫描项目，ok add --type reference --tags wiki 写 wiki 条目（直接转正，参与检索）
  → ok wiki mark：游标写入 state/wiki.json（last_commit + generated_at + entry_count）

ok wiki status
  → git rev-list --count <游标>..HEAD 得落后计数（无游标按全历史；git 不可用 behind=-1）
  → 与 [wiki] stale_commits 阈值（默认 20，0=关闭）比较得出 stale

index.Sync 重建 INDEX.md
  → 追加「## Wiki 目录」节（tags LIKE '%wiki%' 且 draft=0，按 title 排序，描述取 summary）
  → 无 wiki 条目时省略该节（输出与之前逐字节一致）

hook prompt（基础注入之后）
  → wikiNudge：stale 且本会话未提示过（session.WikiNudged）→ 输出末尾追加 nudge
  → 从未生成：建议用 openknowledge-wiki 技能生成 wiki；已生成：提示落后 N 个 commit
  → 每会话最多一次；非 git 项目/git 不可用 fail-open 静默
```

**目标**：wiki 由 AI 技能生成、但"该不该更新"由机制提醒——游标 + 阈值把 wiki 新鲜度变成可检查的状态，提示复用现有 prompt 注入通道，不增加新 hook。

---

## 7. 数据流与事件流

### 7.1 一次提问的完整时序

```
用户        kimi                ok.exe hook prompt         存储
 │ 提问      │                       │                      │
 │──────────►│ UserPromptSubmit     │                      │
 │           │────stdin JSON───────►│                      │
 │           │                      │──读 registry.toml────►│
 │           │                      │──打开 kb.db 并 Sync───►│
 │           │                      │──(首次)读 INDEX.md────►│
 │           │                      │──(可选)embedding API──►│ (OpenAI 兼容服务)
 │           │                      │──Query/截断            │
 │           │◄──stdout 注入文本────│                      │
 │           │──追加进上下文────────►│                      │
 │           │───────调用 LLM──────►│                      │
```

### 7.2 会话生命周期中的状态

```
会话开始(首次提问)          会话中                    会话结束(每次回合末)
     │                       │                          │
  state.Clean(7天)      Touched 累积                enforce 判定
  BaseInjected=true     (Write|Edit 触发)           BlockedRules 记忆
     │                       │                          │
     └────────► state/session-<id>.json ◄─────────────┘
```

状态文件按 session_id 隔离，超过 7 天在下次首次提问时清理。hook 不持有任何内存状态——每次触发都是独立进程，全部状态在磁盘。

---

## 8. 存储层

集中存储于 `~/.openknowledge/`（`OK_HOME` 可覆盖，测试靠它隔离）：

```
~/.openknowledge/
├── ok.log                  # hook 错误日志（fail-open 的唯一痕迹）
├── registry.toml           # 项目注册表：[[project]] name + paths
├── config.toml             # 全局配置：embedding/inject/retrieve 默认值
├── hooks-disabled          # 全局开关标志文件（存在即全部静默）
└── projects/<项目名>/
    ├── config.toml         # 项目配置（覆盖全局；[[enforce]] 仅这里有）
    ├── knowledge/*.md      # 知识条目（一文件一条，frontmatter + 正文）
    ├── INDEX.md            # 机器生成的轻量索引（标题+类型+tags+摘要，由 index.Sync 重建）
    ├── kb.db               # SQLite 索引库：entries（原文）+ entries_fts（FTS5）+ vectors（向量 blob）
    └── state/
        ├── session-*.json  # 会话状态（Touched/BlockedRules/BaseInjected/WikiNudged，超 7 天 GC）
        └── wiki.json       # wiki 游标（last_commit/generated_at/entry_count；固定文件名，不受 session 7 天 GC 影响）
```

**写入纪律**：INDEX.md 与 kb.db 由工具维护，不手改；knowledge/ 是人工维护区；config.toml 项目级手写（模板含注释示例）。旧版 vectors.json 首次打开 kb.db 时自动导入并改名为 `.bak`。

**一致性策略**：全部 JSON/TOML 读取对"文件不存在"宽容（视为空）；损坏文件按层处理——entry 解析失败在 `index.Sync` 中跳过单文件（已索引旧行保留，提交后返回 `*CorruptEntriesError` 警告）；state 损坏回退空状态（最坏情况是重复阻断一次，fail-safe 方向正确）；kb.db 损坏/打开失败时 hook 记 ok.log 后静默放行（fail-open）。

---

## 9. 外部集成

### 9.1 Kimi Code hooks（0.28.1 实测校准）

| 事件 | ok 子命令 | 载荷关键字段（实测） | 作用 |
|------|-----------|---------------------|------|
| `UserPromptSubmit` | `ok hook prompt` | `prompt` 是**内容块数组** `[{"type":"text","text":"…"}]` | stdout 追加进上下文 |
| `PostToolUse`（matcher `Write\|Edit`） | `ok hook post-tool` | `tool_input.path`（**不是** `file_path`） | 记录触碰文件 |
| `Stop` | `ok hook stop` | — | exit 2 阻断，stderr 为原因 |

关键实测结论（记录在规格附录 A）：**SessionStart 的 stdout 不进入上下文**（观察型事件），因此基础注入放在首次 UserPromptSubmit；Windows 上 hook 命令由系统 shell 执行，`sh -c` 不可用，绝对路径 exe 可用。

### 9.2 多 agent 抽象（agentx）

`internal/agentx` 把"AI 编码 agent 的 hook 集成"抽象为适配器注册表：CLI（`ok setup`/`ok init`）、GUI API 与 hook 入口自愈统一经注册表驱动；新增 agent = 实现 `Agent` 接口并在适配器文件的 `init` 中 `Register`。

```go
type Agent interface {
    ID() string                   // 稳定标识："kimi" / "pi"，CLI/GUI/API 统一使用
    DisplayName() string          // 展示名："Kimi Code" / "Pi"
    Detect() bool                 // 本机是否已安装该 agent
    HooksInstalled() bool         // hooks 集成是否已安装且为当前版本
    InstallHooks(exe string) error
    RemoveHooks() (bool, error)   // 返回是否真的移除了内容
    EnsureHooks(exe string) error // hook 入口自愈；错误由调用方 fail-open 处理
    HooksTarget() string          // hook 写入目标的展示路径
    SkillsDir() string            // 技能目录（当前均返回共享 SkillsHome）
}
```

注册表：`Register` / `All` / `Find(id)` / `Detected()`（本机已安装的 agent）；技能安装目录为各 agent 共享的 `SkillsHome()`（`OK_SKILLS_HOME` 优先，默认 `~/.agents/skills`）。

两种注入形态：

| agent | 注入形态 | 写入目标 | "已安装且为当前版本"判定 |
|-------|----------|----------|--------------------------|
| kimi | TOML 标记块（3 条 `[[hooks]]`） | `~/.kimi-code/config.toml`（`KIMI_CODE_HOME` 优先） | 标记块 `# >>> openknowledge hooks >>>` 存在 |
| pi | TypeScript 扩展（三事件回调） | `~/.pi/agent/extensions/openknowledge.ts`（`PI_CODING_AGENT_DIR` 优先） | 头标记 + `// fingerprint:` 行与当前模板指纹一致 |

pi 扩展由内嵌模板 `pi_extension.ts`（`go:embed`）渲染：`{{EXE}}` 占位替换为 ok 绝对路径，文件头写头标记（`// openknowledge hooks (managed by ok.exe; do not edit)`）与指纹行（指纹 = 模板内容 sha256 前 12 位十六进制，随模板升级变化）。安装时若目标已存在**非本工具生成**的同名文件，先备份为 `.bak-openknowledge`（备份失败则中止安装）；`RemoveHooks` 只删本工具生成的文件，非本工具文件不动。扩展对 ok 的调用全部 fail-open（超时/异常静默），不拖累 pi 会话。

pi 事件 → ok hook 映射：

| pi 事件 | ok 子命令 | 对应 kimi 事件 | 语义差异 |
|---------|-----------|----------------|----------|
| `before_agent_start` | `ok hook prompt` | `UserPromptSubmit` | stdout 非空时以 `display:false` 自定义消息注入上下文 |
| `tool_result`（toolName = `write`/`edit`） | `ok hook post-tool` | `PostToolUse`（matcher `Write\|Edit`） | 无 |
| `agent_settled` | `ok hook stop` | `Stop` | pi 无法阻断已结束的回合：ok 以 exit 2 + stderr 表达"阻断"时，扩展改为 `sendMessage({content: stderr}, {triggerTurn: true})` 把提示注入会话，驱动 agent 当场完成自省/补日志 |

### 9.3 OpenAI 兼容 embedding API

- 端点：`POST {base_url}/embeddings`，请求 `{model, input}`，响应 `{data:[{embedding}]}`
- key 解析：`api_key` 字段（全局/项目）→ `api_key_env` 环境变量 → 无（纯关键词）
- 超时：客户端 `timeout_sec`（默认 5s）< hook 配置的 10s 上限，保证任何情况下 hook 不会拖累会话

---

## 10. 性能与可靠性策略

| 优化项 | 位置 | 说明 |
|--------|------|------|
| **单二进制 + 进程内无状态** | 全项目 | hook 冷启动 ~10ms；无 daemon、无 IPC |
| **SQLite 索引 + mtime 增量同步** | `index` | 检索不逐文件扫描 Markdown；未变化条目不重算向量，每次调用最多为提问算 1 次 embedding |
| **embedding 失败降级** | `hook.HandlePrompt` | 超时/失败自动退化为纯关键词检索，注入永不缺席 |
| **token 预算截断** | `store.TruncateToBudget` | 注入文本按 `inject.max_tokens` 截断（字符数÷2 保守估算） |
| **全面 fail-open** | 所有 hook handler | 任何内部错误 → ok.log + exit 0；`main.runHook` 还有 panic-recover 兜底 |
| **损坏条目跳过** | `index.Sync` | 变化条目解析失败跳过该文件（已索引旧行保留）并返回 `*CorruptEntriesError`：一个坏条目不拖垮全部注入 |
| **阻断防死循环** | `state.BlockedRules` | 同会话同规则只阻断一次 |
| **状态 GC** | `state.Clean` | 7 天前的会话状态自动清理 |
| **测试零网络** | 全测试套件 | `OK_HOME` 隔离 + `OPENAI_API_KEY` 置空 + `httptest` fake server |

---

## 11. 依赖关系图

```
                 ┌─────────┐
                 │ cmd/ok  │
                 └────┬────┘
              ┌───────┴───────┐
              ▼               ▼
          ┌───┴───┐       ┌───┴────┐
          │  cli  │       │  hook  │
          └───┬───┘       └───┬────┘
              └───────┬───────┘
                      ▼
                 ┌─────────┐
                 │ project │
                 └────┬────┘
        ┌─────────────┼──────────────┬──────────┐
        ▼             ▼              ▼          ▼
   ┌─────────┐   ┌────────┐    ┌─────────┐ ┌────────┐
   │registry │   │ config │    │  index  │ │ enforce│
   └───┬─────┘   └────────┘    └────┬────┘ └───┬────┘
       │                            │          │
       ▼           ┌────────────────┤          ▼
     [toml]        ▼                ▼     ┌───────┐
               ┌───────┐       ┌────────┐ │ state │
               │ entry │       │ embed  │ └───────┘
               └───┬───┘       └────────┘
                   ▼                │
               [yaml.v3]            ▼
                          [modernc.org/sqlite]（index）
                          [doublestar]（enforce）
```

第三方库：`toml`（registry/config）、`yaml.v3`（entry）、`doublestar`（enforce）、`modernc.org/sqlite`（index）。
注：`index` 还依赖 `retrieve`（Terms 分词）与 `config`（检索权重）；`retrieve`/`embed`/`store`/`state` 无内部依赖。

---

## 12. 构建配置与命令

### 12.1 构建

```bash
go build -o ok.exe ./cmd/ok   # Windows
go build -o ok ./cmd/ok       # Linux/macOS
bash scripts/build-dist.sh    # 发布构建：dist/ok.exe（-ldflags "-s -w -H windowsgui" + 版本注入）+ dist/web/
```

无构建标签、无代码生成、无资源嵌入；`go.mod` 声明 `go 1.25.0`。GUI 的 web 资源不内嵌，由 `dist/web/` 随二进制分发。应用版本号由 build-dist.sh 用 sed 从 `installer/openknowledge.iss` 的 `#define AppVersion` 提取，经 `-ldflags -X openknowledge/internal/version.Version=<版本>` 注入 `internal/version.Version`（事实源只有 .iss 一处；裸 `go build` 为 `dev`）。

### 12.2 常用开发命令

```bash
go test ./...          # 全部测试（15 包）
go vet ./...           # 静态检查
go build ./...         # 编译检查
```

### 12.3 依赖解析失败排查

本机 `proxy.golang.org` 不可达时：`GOPROXY=https://goproxy.cn,direct go get ...`

---

## 13. CLI 命令面

| 命令 | 作用 | 关键行为 |
|------|------|----------|
| `ok setup` | 首次引导 | 写 hooks 配置（标记块幂等）+ 装 6 个 kimi 技能 + 交互配 embedding + 连通性验证 |
| `ok gui` | 启动 Web 管理界面 | 无参数运行同效；127.0.0.1:17888 + 令牌鉴权（由常驻 daemon 承载），自动开浏览器后即退；页面关闭不退出进程 |
| `ok daemon [stop]` | 常驻进程管理 | 无参启动常驻 daemon（承载 GUI 与 hook 转发，端口 17888 即单实例锁）；`stop` 停止 daemon |
| `ok init [名字]` | 注册当前项目 | 名字缺省取目录基名；建 KB 骨架；幂等写入/更新 hooks 配置（复用 setup 逻辑，失败仅提示） |
| `ok add --title …` | 新建条目 | `--type/--tags/--mandatory/--file`；自动同步索引库（无 key 时向量跳过） |
| `ok propose --title …` | AI 提议草稿条目 | `--type/--tags/--summary/--file|--body`；写 `draft:true`，只同步 INDEX 不算向量，不参与检索 |
| `ok approve <文件>` | 批准草稿转正 | draft=false 并同步 INDEX 与向量；非草稿/缺文件报错 |
| `ok capture [propose\|auto]` | 查看/切换沉淀模式 | 无参打印当前模式与 turn_interval；带参写项目 `[capture]` 小节（幂等替换） |
| `ok wiki status` / `ok wiki mark [commit]` | wiki 游标管理 | `status` 输出 JSON（has_wiki/behind/stale/threshold，git 不可用 behind=-1）；`mark` 记游标（缺省 HEAD）并统计 wiki 条目数 |
| `ok search <词>` | 检索预览 | 命令行输出打分排序（调试用） |
| `ok index` | 同步索引库 | 增量同步 kb.db 并重建 INDEX.md、打印条目数（无 key 时向量跳过，退出码 1） |
| `ok list` | 列出项目与条目 | `*` 标记 mandatory |
| `ok doctor` | 体检 | 注册表/配置/embedding 连通性/hooks 安装状态/开关状态 |
| `ok on` / `ok off` | 全局开关 | 删除/创建 hooks-disabled 标志文件 |
| `ok hook prompt` | UserPromptSubmit 入口 | 基础注入 + 检索注入 |
| `ok hook post-tool` | PostToolUse 入口 | 记录触碰文件 |
| `ok hook stop` | Stop 入口 | enforce 判定，唯一可能 exit 2 的入口 |

退出码约定：hook 路径一律 0（唯一例外：stop 阻断为 2）；CLI 错误为 1 且信息到 stderr。

---

## 14. 测试与验证

### 14.1 自动化测试

- **单元测试**（每包一个 `*_test.go`，共 11 个文件）：registry 路由、entry 解析（含 CRLF/BOM）、config 三层合并、store 截断、embed（httptest fake server）、index（同步/查询/mandatory/迁移/2k 条目）、retrieve 分词、state 持久化、enforce 全分支、project 解析、hook 三入口、cli 各命令、setup 幂等写入
- **端到端测试**（`cmd/ok/integration_test.go`）：`TestMain` 编译真实二进制，驱动完整流程——init → add → 首次提问基础注入 → 二次提问不重复 → 手改条目后 hook 查询前增量同步命中并重建 INDEX → enforce 阻断一次后放行 → 未注册目录静默 → 开关 off/on
- **隔离保证**：`OK_HOME` + `KIMI_CODE_HOME` + `OK_SKILLS_HOME` 指向 `t.TempDir()`，`OPENAI_API_KEY` 置空，全程零网络

运行：`go test ./... -v`（15 包全绿）；`go vet ./...` 干净。

### 14.2 真实环境验证（曾执行的手动验收）

1. `ok setup` 写入配置后 `kimi doctor` 校验通过
2. `kimi -p "git 提交规范是什么"` → 会话 wire 中出现 mandatory 全文、知识索引、检索命中
3. `kimi -p` 让 AI 写 `.go` 文件 → Stop 阻断并提示补变更日志，AI 补写后放行
4. `ok off` → hook 全静默；`ok on` → 恢复

---

## 15. 常见问题排查

### 15.1 知识完全没注入

检查：
- `ok doctor` 看"hooks 已安装"与开关状态
- `~/.openknowledge/hooks-disabled` 是否存在（存在即全静默，`ok on` 恢复）
- 当前目录是否已注册（`ok list`）；hooks 只在注册项目内生效
- `~/.openknowledge/ok.log` 是否有报错（如条目 YAML 损坏）

### 15.2 有注入但检索不到该命中的条目

检查：
- `ok search <关键词>` 命令行复现打分，确认是检索问题还是注入问题
- 条目 tags/summary 是否覆盖该关键词（关键词分依赖它们）
- 语义分是否为 0：embedding 未配置或失败（`ok doctor` 验证连通性）；kb.db 向量是否过期（`ok index` 同步补齐）

### 15.3 语义检索不生效

检查：
- 全局 `~/.openknowledge/config.toml` 的 `[embedding]` 是否有 `api_key`
- 项目 config.toml 是否覆盖了全局（项目级配置优先级最高，旧模板可能有写死的 embedding 段）
- 切换过 embedding 模型后必须删除 kb.db 再运行 `ok index` 重建向量（增量同步不会重算未变化条目；维度不匹配时余弦静默为 0）

### 15.4 强制检查不触发

检查：
- 项目 config.toml 里 `[[enforce]]` 块是否存在且未被注释
- glob 是否**全小写**（路径统一按小写比较）
- PostToolUse 的 matcher 是否覆盖实际工具名（`Write|Edit`）
- 同会话同规则只阻断一次——是否已被阻断过（看 state/session-*.json 的 blocked_rules）

### 15.5 ok setup 后 hooks 不执行

检查：
- 是否新开了会话（hooks 配置在会话启动时加载）
- config.toml 里标记块是否完整（`# >>> openknowledge hooks >>>` 成对）
- hooks command 指向的 ok.exe 路径是否还存在（移动过 exe 需重跑 `ok setup`）

---

## 16. 后续维护建议

1. **清理 go.mod 标记**：执行 `go mod tidy` 去掉间接依赖上过期的 `// indirect` 标记。
2. **防御性钳制**：`config.Load` 对 `max_tokens < 0`、`top_n < 0` 做钳制（当前手改配置为负数时 `TruncateToBudget` 会 panic——hook 路径虽有 recover 兜底，CLI 路径没有）。
3. **写盘原子化**：INDEX.md / state / registry 改为临时文件 + rename，避免崩溃半截文件。
4. **ok.log 治理**：当前只增不减且会写入 embedding 错误响应体（≤512B），建议只记状态码并加大小滚动。
5. **embedding 模型漂移检测**：`ok doctor` 对比 kb.db 中向量维度与当前模型维度，不一致时提示重建索引。
6. **ok index 强制重算开关**：当前 Sync 按 mtime 增量，换模型后需删 kb.db 才能全量重算向量；可加 `--force` 标志。
7. **Doctor 校验 enforce glob**：用 doublestar 预编译用户配置的 glob，格式错误提前暴露（当前 malformed glob 静默不生效）。
8. **CRLF 归一**：仓库在 Windows 下全量 CRLF，`gofmt -l` 全报未格式化；建议加 `.gitattributes`（`* text=auto eol=lf`）统一为 LF。
9. **v2 候选方向**（当前为非目标，勿提前实现）：hooks 自动沉淀经验、其他 AI 工具适配、本地 embedding、知识库远程同步。

---

## 17. 检索算法实现（深度）

本章是检索链路的实现级说明，对应代码：`internal/retrieve/retrieve.go`（分词）、
`internal/index/sync.go`（同步）、`internal/index/query.go`（查询）。

### 17.1 检索流水线总览

```
[写入侧]  Markdown 文件（唯一真相源）
            │  Sync：枚举 → diff → 只解析变化项
            ▼
        kb.db ──┬── entries（原文 + mtime + mandatory）
                ├── entries_fts（FTS5，切分后文本）
                └── vectors（float32 blob）
[查询侧]  用户提问
            │  Terms 分词 ──► FTS5 BM25 ─┐
            │  embedding  ──► 余弦相似度 ─┤ score = α·kw + β·cos
            ▼                             ▼
        过滤(mandatory/≤0) → 排序 → top_n → 取正文注入
```

### 17.2 分词器（`retrieve.Terms`）

规则（44 行，无第三方分词库）：

- 全部转小写后逐 rune 扫描：
  - `unicode.Han` 汉字 → 进入 CJK 缓冲，冲刷时**两两切二元组**（孤字单独成词）
  - 其他字母/数字 → 进入拉丁缓冲，冲刷时 **≥2 字符**才成词
  - 其余字符（空格、标点）视为分隔
- 示例：
  - `"Git 提交规范"` → `[git, 提交, 交规, 规范]`
  - `"rm -rf 怎么恢复"` → `[怎么, 么恢, 恢复]`（`rm` 成词，单字 `f` 被丢弃）
- **入库与查询同口径**：FTS 表里存的是 `strings.Join(Terms(text), " ")`，
  MATCH 查询也用 `Terms(提问)`——保证两边词元集合一致，这是中文可命中的关键。
- 固有局限：二元组有歧义（"提交规范"切出的"交规"也是"交通规定"的词元）；
  不处理同义词（"删除"与"rm"互不知道对方）。这是零依赖取舍，见 17.9。

### 17.3 关键词通道：FTS5 + BM25

**索引结构**（`internal/index/db.go`）：

```sql
CREATE TABLE entries(filename PK, title, type, tags, summary, body,
                     mandatory, mtime);              -- 原文
CREATE VIRTUAL TABLE entries_fts USING fts5(
    title, tags, summary, body, filename UNINDEXED); -- 切分后文本，独立维护
CREATE TABLE vectors(filename PK, dim, blob);        -- float32 小端
```

FTS 表为**独立内容表**（非 external-content + 触发器），由 Sync 显式
delete+insert 维护——换来的是对切分预处理的完全控制。

**查询构造**（`query.go`）：

```
MATCH 串 = Terms(提问) 每个词元双引号包裹后用 " OR " 连接
SELECT e.filename, e.title, e.type, e.body,
       bm25(entries_fts, 10.0, 8.0, 3.0, 1.0) AS rank
FROM entries_fts f JOIN entries e ON e.filename = f.filename
WHERE entries_fts MATCH ? AND e.mandatory = 0
```

- **列权重 10 / 8 / 3 / 1**（title / tags / summary / body）：标题信号最强，
  tags 次之，正文最弱（长文本噪音多）。
- **BM25 相对旧方案（命中计数 +3/+2/+1）的三处修正**：
  1. **IDF**：稀有词权重高——"sqlite"命中比"配置"命中更值钱；
  2. **词频饱和**：一个词重复出现收益递减，防刷屏；
  3. **长度归一**：长 summary/body 不再天然占优。
- **归一化**：SQLite 的 `bm25()` 返回**负值**（越小越好），取 `kw = -rank`
  后用 `kw/(kw+6)` 压缩到 [0,1)——与余弦同量纲，α/β 才有真实意义。

### 17.4 语义通道：向量余弦

- **写入**：条目向量 = `Embed(标题+摘要+正文)`（OpenAI 兼容接口），float32
  小端 blob 存 `vectors` 表；mtime 未变的条目不重算（增量），缺向量的
  未变化条目在有 key 时补齐（backfill）。
- **查询**：`queryVec` 与全量向量逐条算余弦（万条约 60MB 内存、毫秒级），
  维度不匹配返回 0（换模型未重建时静默降级而不是崩）。
- **成本边界**：hook 路径每次最多为**提问**算 1 次 embedding（5s 超时），
  条目向量只在同步时算。

### 17.5 混合打分与排序

```
score = α · normBM25 + β · cosine        （α、β 默认 1.0，项目可调）
```

过滤与排序（严格确定顺序）：

1. `mandatory = 0`（mandatory 条目已在每会话首次的基础注入中）
2. `score > 0`（零分不注入）
3. 分数降序；同分按标题升序（结果可复现）
4. 截 `top_n`（默认 3）；注入文本再按 `inject.max_tokens` 预算截断

**打分实例**（提问"git 提交规范"，条目《Git 提交规范》tags:[git]）：

- 词元：`git, 提交, 交规, 规范`
- BM25：title 全命中 + tag 命中 → kw≈7.4 → 归一 ≈0.55
- 余弦（假设语义高度相关）≈0.8
- **score = 1.0×0.55 + 1.0×0.8 = 1.35**；对照纯噪音条目 score=0 被过滤

### 17.6 同步算法（热路径性能来源）

`Sync` 每次 hook prompt 都会执行，其设计决定了每次提问的延迟：

```
os.ReadDir(knowledge/)                # 只拿文件名，不读内容
  ├─ DirEntry.Info()                  # Windows 下复用 readdir 数据，零额外系统调用
  ├─ 与 entries 表 filename+mtime 对比
  ├─ 新增/变化 → 仅这些文件 ReadFile+Parse+upsert(+重算向量)
  ├─ 已删除 → 连带清理 entries/fts/vectors
  └─ 有变化才重写 INDEX.md（缺失时必写）
```

- **单事务**：三表写入在一个 tx 内，失败整体回滚，不会出现半同步状态。
- **坏文件容错**：变化文件解析失败 → 跳过该文件（旧索引行保留），其余
  照常提交，提交后返回 `*CorruptEntriesError` 警告（`errors.As` 区分）；
  SQL 失败等致命错误才回滚。
- **复杂度**：热路径 = 1 次目录枚举 + N 次元数据对比（内存），
  与条目正文大小无关。

### 17.7 降级矩阵

| 场景 | 行为 |
|------|------|
| 未配置 embedding key | 纯关键词检索（`queryVec=nil`），一切正常 |
| embedding API 超时/挂掉 | 同步先失败 → `Sync(nil)` 重试 → 关键词检索；注入不缺席 |
| 条目缺向量（未 index） | 该条目语义分为 0，仍可被关键词命中 |
| 切换 embedding 模型未重建 | 维度不匹配余弦为 0；需删 kb.db 后 `ok index` |
| 单个条目文件损坏 | 跳过并保留其旧索引（`CorruptEntriesError` 警告），其余正常 |
| 草稿条目（draft: true） | 不进 FTS 与向量：同步只写入 INDEX.md（标【草稿】），检索与注入排除；批准后正常参与 |
| kb.db 损坏/丢失 | hook 记 ok.log 后 exit 0（fail-open）；`ok index` 可重建 |

### 17.8 实测性能（本机，1 万条目）

| 路径 | 耗时 | 说明 |
|------|------|------|
| 首次全量同步 | 9.8s | 一次性（含 1 万次解析与入库） |
| **hook 热路径** | **36ms** | Open + 增量同步(8ms) + 查询(27ms) |
| 旧方案（逐文件扫描） | ≥1.1s | 每次提问全量读取+解析，已退役 |

热路径由"目录枚举 + 内存 diff"构成，与正文大小无关；万级到 5 万级
预计仍在 100ms 量级。embedding API 调用（200-500ms）是另一笔网络开销，
与检索无关且失败自动降级。

### 17.9 已知局限与演进方向

- **无同义词/查询改写**："删除文件"与"rm"不互相召回 → 可在 Terms 层加
  静态映射表，或查询时并行两路改写
- **二元组歧义**："交规"误配 → 引入更小颗粒度的字典分词（会带依赖）
- **列权重与 k=6 归一常量为硬编码**：数据量大后可按命中率回归调参
- **提问向量无缓存**：连续相似提问重复调 API → 可加短 TTL 缓存
- **无反馈调权**：不记录"哪些条目被注入后真正有用" → 需要埋点，属 v2 议题

---

## 18. 配置参数参考

所有可调参数一览。合并规则：**内置默认 ← 全局 ← 项目**（后者覆盖前者）。

### 18.1 全局配置 `~/.openknowledge/config.toml`

| 参数 | 默认 | 作用与调优 |
|------|------|-----------|
| `embedding.base_url` | `https://api.openai.com/v1` | 任何 OpenAI 兼容端点（OpenAI/硅基流动/Ollama 等） |
| `embedding.api_key` | 空 | 直接存 key（0600）；与 `api_key_env` 二选一，字段优先 |
| `embedding.api_key_env` | 空 | 或指向环境变量名（如 `OPENAI_API_KEY`），不落明文 |
| `embedding.model` | `text-embedding-3-small` | 换模型后必须删 kb.db 再 `ok index` |
| `embedding.timeout_sec` | `5` | 必须小于 hook 配置的 10s，保证 prompt hook 不超时 |
| `inject.max_tokens` | `1500` | 单次注入预算（字符数÷2 估算）；mandatory 多/条目长则调大 |
| `retrieve.alpha` | `1.0` | 关键词分权重。术语精确的场景（错误码、命令名）可调大 |
| `retrieve.beta` | `1.0` | 语义分权重。问法多变的场景可调大（前提是 embedding 质量好） |
| `retrieve.top_n` | `3` | 每次最多注入条数；调大注意挤占 `max_tokens` 预算 |

### 18.2 项目配置 `~/.openknowledge/projects/<名>/config.toml`

可覆盖以上全部参数（`[[enforce]]` 仅项目级）：

| 参数 | 说明 |
|------|------|
| `capture.mode` | 经验沉淀模式：`propose`（默认，AI 主动提议草稿人批准）或 `auto`（Stop hook 周期阻断强制自省）；`ok capture <mode>` 或 GUI 沉淀卡写入 |
| `capture.turn_interval` | auto 模式的自省间隔（Stop 次数，默认 5）；仅项目/全局配置手改 |
| `wiki.stale_commits` | wiki 落后多少 commit 触发 prompt 提示（默认 20，0 = 关闭） |
| `[[enforce]].type` | 规则类型，v1 仅 `changelog_required` |
| `[[enforce]].code_globs` | "算改代码"的 glob 列表。**一律小写**；doublestar 语法，`**/*.go` 可匹配根目录文件 |
| `[[enforce]].changelog_glob` | "算写日志"的 glob，如 `docs/changelogs/**` |
| `[[enforce]].message` | 阻断时输出给 AI 的提示（会进入模型上下文，写清楚要做什么） |

### 18.3 环境变量

| 变量 | 作用 |
|------|------|
| `OK_HOME` | KB 根目录（默认 `~/.openknowledge`）；测试隔离也用它 |
| `KIMI_CODE_HOME` | kimi 配置目录（`ok setup` 写 hooks 时定位 config.toml） |
| `OK_SKILLS_HOME` | 技能安装目录（默认 `~/.agents/skills`） |
| `PI_CODING_AGENT_DIR` | pi 配置根目录（默认 `~/.pi/agent`；`ok setup` 写扩展时定位 extensions/） |
| `api_key_env` 指向的变量 | embedding key 的环境变量通道（如 `OPENAI_API_KEY`） |

### 18.4 hooks 配置（由 `ok setup` 维护）

**kimi**：写入 `~/.kimi-code/config.toml`（`KIMI_CODE_HOME` 优先）的标记块：

| 字段 | 当前值 | 说明 |
|------|--------|------|
| `event` | `UserPromptSubmit` / `PostToolUse` / `Stop` | 三个注入/追踪/强制时机 |
| `matcher` | 仅 PostToolUse 用 `"Write\|Edit"` | 工具名正则过滤 |
| `command` | `"<exe> hook prompt\|post-tool\|stop"` | `ok setup` 烧入绝对路径 |
| `timeout` | `10` / `5` / `5` 秒 | prompt 必须 > `embedding.timeout_sec`（默认 5），否则慢 API 会被 kimi 强杀 |

**pi**：写入 `~/.pi/agent/extensions/openknowledge.ts`（`PI_CODING_AGENT_DIR` 优先）。文件头为头标记（`// openknowledge hooks (managed by ok.exe; do not edit)`）+ `// fingerprint: <模板 sha256 前 12 位>` 行；`HooksInstalled` 要求头标记存在且指纹等于当前模板指纹——模板升级后旧扩展判为"非当前版本"，由 hook 入口 `EnsureHooks` 自愈重写。安装时既有非本工具生成的同名文件先备份为 `.bak-openknowledge`，卸载只删本工具生成的文件。

### 18.5 合并与解析顺序速查

```
配置值：  内置默认  ←  ~/.openknowledge/config.toml  ←  项目 config.toml
API key： 项目 api_key → 全局 api_key → api_key_env 环境变量 → 无(纯关键词)
开关：    ~/.openknowledge/hooks-disabled 存在 = 全静默（ok on 恢复）
```
