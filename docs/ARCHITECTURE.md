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
| 语言 | **Go**（go.mod 声明 `go 1.23.8`，要求 ≥ 1.22） |
| 构建 | Go 标准工具链，单二进制产出，无 CGO |
| CLI 解析 | 标准库 `flag`（刻意不引 cobra） |
| HTTP | 标准库 `net/http`（embedding API 调用） |
| 测试 | 标准库 `testing` + `net/http/httptest` |

**表格 B — 第三方依赖清单**（全部，仅 3 个，与 `go.mod` 一致）：

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/BurntSushi/toml` | v1.6.0 | registry.toml 与各层 config.toml 解析 |
| `gopkg.in/yaml.v3` | v3.0.1 | 知识条目 frontmatter 解析 |
| `github.com/bmatcuk/doublestar/v4` | v4.10.0 | 强制规则的 `**` glob 匹配 |

> 注：go.mod 中三个依赖仍带 `// indirect` 标记（历史遗留），实际均为直接依赖；`go mod tidy` 可清理。

---

## 3. 模块架构

单 module（`openknowledge`），12 个包，严格单向依赖、无环：

```
┌─────────────────────────────────────────────┐
│ cmd/ok            （二进制入口 + 子命令调度） │
└───────┬───────────────────────┬─────────────┘
        │                       │
┌───────▼────────┐      ┌───────▼───────────────┐
│ internal/cli   │      │ internal/hook         │
│ （人用的命令）  │      │ （kimi hooks 入口）   │
└───────┬────────┘      └───────┬───────────────┘
        │               ┌───────▼────────┐
        │               │ internal/project│（cwd→项目）
        │               └───────┬────────┘
   ┌────▼───────────────────────▼───────────────────┐
   │ 基础层（被上层直接组合）                         │
   │ registry · entry · config · store · embed ·    │
   │ retrieve · state · enforce                     │
   └────────────────────────────────────────────────┘
```

**依赖关系**（→ 表示 import）：

- `cmd/ok` → `cli`、`hook`
- `cli` → `registry`、`entry`、`store`、`embed`、`retrieve`、`project`、`config`
- `hook` → `project`、`registry`、`entry`、`store`、`embed`、`retrieve`、`state`、`enforce`
- `project` → `registry`、`config`、`store`
- `retrieve` → `entry`、`embed`、`config`
- `embed` → `entry`
- `store` → `entry`
- `enforce` → `config`、`state`（+ doublestar）
- `registry` → BurntSushi/toml；`entry` → yaml.v3；`config` → BurntSushi/toml
- `state` → 仅标准库

**分层原则**：`hook` 与 `cli` 是两个互不 import 的应用层；`project` 是它们共享的项目解析层；其余为单一职责的基础包。

---

## 4. 目录结构

```
OpenKnowledge/
├── go.mod / go.sum                # 模块定义（3 个第三方依赖）
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
│   │   ├── store.go               #   目录路径、INDEX.md 生成、token 预算截断
│   │   └── store_test.go
│   ├── embed/                     # 向量
│   │   ├── embed.go               #   Client 接口、OpenAI 兼容客户端、Cosine
│   │   ├── vectors.go             #   vectors.json 缓存（mtime 增量更新）
│   │   └── embed_test.go
│   ├── retrieve/                  # ★ 混合检索
│   │   ├── retrieve.go            #   Terms（CJK 二元组）、KeywordScore、Rank
│   │   └── retrieve_test.go
│   ├── state/                     # 会话状态
│   │   ├── state.go               #   Session（触碰文件/已阻断规则/已基础注入）、Clean
│   │   └── state_test.go
│   ├── enforce/                   # 强制规则
│   │   ├── enforce.go             #   changelog_required 判定（doublestar 匹配）
│   │   └── enforce_test.go
│   ├── project/                   # 项目解析（hook 与 cli 共享）
│   │   ├── project.go             #   Context{Project,Store,Config}、FromCwd
│   │   └── project_test.go
│   ├── hook/                      # ★ hooks 事件处理
│   │   ├── hook.go                #   Event 解析、HandlePrompt/HandlePostTool/HandleStop
│   │   └── hook_test.go
│   └── cli/                       # 管理命令
│       ├── cli.go                 #   Init/Add/Search/Index/List/Doctor
│       ├── setup.go               #   Setup 引导、hooks 块幂等写入、技能安装、embedding 配置
│       ├── toggle.go              #   On/Off 全局开关
│       └── *_test.go
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

双加载策略是手维护场景的关键设计：

- `Load(dir)` — 严格模式，任何文件解析失败即整体报错（CLI 用，错误要暴露给用户）
- `LoadTolerant(dir)` — 宽容模式，坏文件跳过并收集错误（hook 用，一个 YAML 笔误不能瘫痪全部注入）

### 5.3 config — 三层配置合并（95 行）

```go
func LoadMerged(projectPath, globalPath string) (Config, error)
```

生效配置 = **内置默认 ← 全局 `~/.openknowledge/config.toml` ← 项目 `config.toml`**，后者覆盖前者（TOML 依次解码到同一 struct 实现）。`Enforce` 规则只有项目级。API key 解析收敛在一处：

```go
func (e Embedding) ResolvedAPIKey() string  // api_key 字段 > api_key_env 环境变量 > ""
```

### 5.4 store — KB 存储布局（57 行）

纯路径计算层：`KnowledgeDir()/IndexPath()/VectorsPath()/StateDir()/ConfigPath()`。另两个职责：`IndexContent` 生成轻量索引（标题+类型+tags+摘要，机器生成禁止手改）；`TruncateToBudget` 按"字符数(rune) ÷ 2"保守估算 token 并截断注入文本。

### 5.5 embed — 向量客户端与缓存（embed.go 90 行 + vectors.go 74 行）

- `Client` 接口隔离 HTTP 细节，测试用 `httptest` fake server，不碰真实网络
- `OpenAIClient` 实现 OpenAI 兼容协议：`POST {base_url}/embeddings`，`Authorization: Bearer <key>`，带 context 超时
- `VectorSet` 是 `vectors.json` 缓存：`Update` 按**文件 mtime** 增量重算、清理已删条目；hook 路径上从不为条目算向量（只为提问算一次），这是 hook 低延迟的关键

### 5.6 retrieve — 混合检索（111 行）

```
score = α·关键词分 + β·余弦语义分
```

- `Terms`：自研分词——小写拉丁/数字词（≥2 字符）+ **CJK 二元组**（`unicode.Han`），解决中文无空格分词问题
- `KeywordScore`：tag 命中 +3 / title +2 / summary +1
- `Rank`：mandatory 条目不参与（已在基础注入中）；只留 score>0；分数降序、同分标题升序；截 top_n；`queryVec=nil` 时自动退化为纯关键词（embedding 失败降级路径）

### 5.7 state — 会话状态（96 行）

`Session{SessionID, Touched, BlockedRules, BaseInjected}` 持久化到 `state/session-<净化id>.json`（sessionID 净化为安全文件名，防路径穿越）。三个职责：记录触碰文件（enforce 的证据）、阻断记忆（同会话同规则只阻断一次，防死循环）、基础注入标记（每会话只注入一次）。`Clean` 清理 7 天前的状态文件。

### 5.8 enforce — 强制规则判定（34 行）

v1 仅 `changelog_required`：触碰文件中存在匹配 `code_globs` 的 且 不存在匹配 `changelog_glob` 的 → 阻断并返回用户配置的 message。用 doublestar 做 `**` glob 匹配（`**/*.go` 可匹配根目录文件）。刻意**不理解**变更日志的细则——细则写在知识条目里由注入教给 AI，hook 只做机械检查。

### 5.9 hook — hooks 事件处理（238 行，全项目最大文件）

三个 handler 共享同一套防御结构：**第一行检查全局开关 → 解析事件 → 路由项目 → 各自逻辑 → 任何错误只记 ok.log 并 exit 0**。

- `Event` 的 `Prompt` 是 `json.RawMessage`，`PromptText()` 兼容两种真实载荷形态（字符串 / `[{"type":"text","text":"..."}]` 数组）
- `FilePath()` 取 `tool_input.path`（kimi 实际字段），兼容 `file_path`
- `HandlePrompt`：每会话首次提问先做基础注入（mandatory 全文 + INDEX.md，标记 `BaseInjected` 且仅当内容非空才置位），然后每次提问做混合检索注入；embedding 失败降级纯关键词
- `HandlePostTool`：记录触碰文件（经 `relativize` 转项目相对、小写、`/` 分隔）
- `HandleStop`：评估 enforce 规则，命中即 `MarkBlocked` → 保存状态 → stderr 输出 message → **exit 2**（全项目唯一非零出口）

### 5.10 cli — 管理命令（cli.go 348 行 + setup.go 210 行 + toggle.go 38 行）

- `cli.go`：`Init`（项目名缺省取目录基名）、`Add`（重复条目拒绝；后接 INDEX 重建 + 向量增量）、`Search`（检索预览）、`Index`（INDEX + 向量全量重建）、`List`、`Doctor`（注册表/配置/embedding 连通性/hooks 安装状态/开关状态）
- `setup.go`：见第 6.4 节
- `toggle.go`：`On`/`Off` 即删除/创建 `~/.openknowledge/hooks-disabled` 标志文件

---

## 6. 核心业务架构

### 6.1 注入链路（知识 → AI 上下文）

```
用户发消息
  → kimi 触发 UserPromptSubmit
  → 执行 "ok.exe hook prompt"，stdin 喂事件 JSON
  → ok：开关检查 → 项目路由 → LoadTolerant 加载条目
  → 首次提问？→ 输出 mandatory 全文 + INDEX.md（置 BaseInjected）
  → 每次提问 → embed 提问(失败降级) → Rank 混合打分 → top-N 全文
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
ok init [名字]   → registry 注册 + KB 骨架（项目名缺省取目录基名）
ok add --title … → 写条目 → 重建 INDEX.md → 有 key 则增量更新向量
手工编辑条目     → ok index 重新同步 INDEX + 向量
ok search <词>   → 命令行预览检索效果（调试注入质量）
```

### 6.4 首次引导（ok setup）

```
ok setup
  → os.Executable 取自身绝对路径（hooks 命令不依赖 PATH）
  → 备份 ~/.kimi-code/config.toml → 标记块幂等写入 3 条 hook
  → 安装 openknowledge-init/on/off 三个技能到 ~/.agents/skills/（烧入 exe 路径）
  → 交互（或 flags）收集 embedding base_url/model/API key
      → 写全局 ~/.openknowledge/config.toml（0600）→ 立即连通性验证
  → 打印引导
```

幂等性由"标记块原位替换"保证：重复执行只更新不重复追加；标记损坏（有头无尾）时报错拒绝修改，不破坏用户配置。

---

## 7. 数据流与事件流

### 7.1 一次提问的完整时序

```
用户        kimi                ok.exe hook prompt         存储
 │ 提问      │                       │                      │
 │──────────►│ UserPromptSubmit     │                      │
 │           │────stdin JSON───────►│                      │
 │           │                      │──读 registry.toml────►│
 │           │                      │──LoadTolerant────────►│
 │           │                      │──(首次)读 INDEX.md────►│
 │           │                      │──(可选)embedding API──►│ (OpenAI 兼容服务)
 │           │                      │──Rank/截断             │
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
    ├── INDEX.md            # 机器生成的轻量索引（标题+类型+tags+摘要）
    ├── vectors.json        # 条目向量缓存：{文件名: {mod_time, vector}}
    └── state/session-*.json # 会话状态（Touched/BlockedRules/BaseInjected）
```

**写入纪律**：INDEX.md 与 vectors.json 由工具维护，不手改；knowledge/ 是人工维护区；config.toml 项目级手写（模板含注释示例）。

**一致性策略**：全部 JSON/TOML 读取对"文件不存在"宽容（视为空）；损坏文件按层处理——entry 在 hook 路径跳过单文件、CLI 路径整体报错；state 损坏回退空状态（最坏情况是重复阻断一次，fail-safe 方向正确）。

---

## 9. 外部集成

### 9.1 Kimi Code hooks（0.28.1 实测校准）

| 事件 | ok 子命令 | 载荷关键字段（实测） | 作用 |
|------|-----------|---------------------|------|
| `UserPromptSubmit` | `ok hook prompt` | `prompt` 是**内容块数组** `[{"type":"text","text":"…"}]` | stdout 追加进上下文 |
| `PostToolUse`（matcher `Write\|Edit`） | `ok hook post-tool` | `tool_input.path`（**不是** `file_path`） | 记录触碰文件 |
| `Stop` | `ok hook stop` | — | exit 2 阻断，stderr 为原因 |

关键实测结论（记录在规格附录 A）：**SessionStart 的 stdout 不进入上下文**（观察型事件），因此基础注入放在首次 UserPromptSubmit；Windows 上 hook 命令由系统 shell 执行，`sh -c` 不可用，绝对路径 exe 可用。

### 9.2 OpenAI 兼容 embedding API

- 端点：`POST {base_url}/embeddings`，请求 `{model, input}`，响应 `{data:[{embedding}]}`
- key 解析：`api_key` 字段（全局/项目）→ `api_key_env` 环境变量 → 无（纯关键词）
- 超时：客户端 `timeout_sec`（默认 5s）< hook 配置的 10s 上限，保证任何情况下 hook 不会拖累会话

---

## 10. 性能与可靠性策略

| 优化项 | 位置 | 说明 |
|--------|------|------|
| **单二进制 + 进程内无状态** | 全项目 | hook 冷启动 ~10ms；无 daemon、无 IPC |
| **mtime 增量向量缓存** | `embed/vectors.go` | hook 路径从不为条目算向量，每次调用最多为提问算 1 次 embedding |
| **embedding 失败降级** | `hook.HandlePrompt` | 超时/失败自动退化为纯关键词检索，注入永不缺席 |
| **token 预算截断** | `store.TruncateToBudget` | 注入文本按 `inject.max_tokens` 截断（字符数÷2 保守估算） |
| **全面 fail-open** | 所有 hook handler | 任何内部错误 → ok.log + exit 0；`main.runHook` 还有 panic-recover 兜底 |
| **宽容加载** | `entry.LoadTolerant` | 一个坏条目不拖垮全部注入 |
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
              │           ┌───┴─────────────────────┐
              ▼           ▼                         │
          ┌───┴────────┐  │                         │
          │  project   │◄─┘                         │
          └───┬────────┘  │                         │
   ┌──────────┼───────────┼──────────┐              │
   ▼          ▼           ▼          ▼              ▼
┌──────┐  ┌──────┐   ┌───────┐  ┌────────┐   ┌──────────┐
│registry│  │config│   │ store │  │retrieve│   │ enforce  │
└──┬───┘  └──┬───┘   └───┬───┘  └───┬────┘   └────┬─────┘
   │         │           │          │             │
   ▼         │           ▼          ▼             ▼
[toml]      │        ┌───────┐  ┌────────┐   ┌───────┐
            └───────►│ entry │◄─┤ embed  │   │ state │
                     └───────┘  └────────┘   └───────┘
                       │            │
                       ▼            ▼
                   [yaml.v3]   (vectors.json)
                                  │
                              [doublestar] ◄── enforce 也使用
```

第三方库：`toml`（registry/config）、`yaml.v3`（entry）、`doublestar`（enforce）。

---

## 12. 构建配置与命令

### 12.1 构建

```bash
go build -o ok.exe ./cmd/ok   # Windows
go build -o ok ./cmd/ok       # Linux/macOS
```

无构建标签、无代码生成、无资源嵌入；`go.mod` 声明 `go 1.23.8`（≥ 1.22 即可编译）。

### 12.2 常用开发命令

```bash
go test ./...          # 全部测试（12 包）
go vet ./...           # 静态检查
go build ./...         # 编译检查
```

### 12.3 依赖解析失败排查

本机 `proxy.golang.org` 不可达时：`GOPROXY=https://goproxy.cn,direct go get ...`

---

## 13. CLI 命令面

| 命令 | 作用 | 关键行为 |
|------|------|----------|
| `ok setup` | 首次引导 | 写 hooks 配置（标记块幂等）+ 装 3 个 kimi 技能 + 交互配 embedding + 连通性验证 |
| `ok init [名字]` | 注册当前项目 | 名字缺省取目录基名；建 KB 骨架；打印 hooks 提示 |
| `ok add --title …` | 新建条目 | `--type/--tags/--mandatory/--file`；自动重建 INDEX + 增量向量 |
| `ok search <词>` | 检索预览 | 命令行输出打分排序（调试用） |
| `ok index` | 全量重建 | INDEX.md + vectors.json（无 key 时 INDEX 仍重建，向量跳过） |
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

- **单元测试**（每包一个 `*_test.go`，共 11 个文件）：registry 路由、entry 解析（含 CRLF/BOM）、config 三层合并、store 截断、embed（httptest fake server）、retrieve 打分、state 持久化、enforce 全分支、project 解析、hook 三入口、cli 各命令、setup 幂等写入
- **端到端测试**（`cmd/ok/integration_test.go`）：`TestMain` 编译真实二进制，驱动完整流程——init → add → 首次提问基础注入 → 二次提问不重复 → enforce 阻断一次后放行 → 未注册目录静默 → 开关 off/on
- **隔离保证**：`OK_HOME` + `KIMI_CODE_HOME` + `OK_SKILLS_HOME` 指向 `t.TempDir()`，`OPENAI_API_KEY` 置空，全程零网络

运行：`go test ./... -v`（12 包全绿）；`go vet ./...` 干净。

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
- 语义分是否为 0：embedding 未配置或失败（`ok doctor` 验证连通性）；vectors.json 是否过期（`ok index` 重建）

### 15.3 语义检索不生效

检查：
- 全局 `~/.openknowledge/config.toml` 的 `[embedding]` 是否有 `api_key`
- 项目 config.toml 是否覆盖了全局（项目级配置优先级最高，旧模板可能有写死的 embedding 段）
- 切换过 embedding 模型后必须 `ok index` 全量重建（旧向量维度不匹配会静默归零）

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

1. **清理 go.mod 标记**：执行 `go mod tidy` 去掉三个依赖上过期的 `// indirect` 标记。
2. **防御性钳制**：`config.Load` 对 `max_tokens < 0`、`top_n < 0` 做钳制（当前手改配置为负数时 `TruncateToBudget` 会 panic——hook 路径虽有 recover 兜底，CLI 路径没有）。
3. **VectorSet nil map 防御**：`VectorSet.Update` 开头加 `if vs.Vectors == nil { vs.Vectors = make(...) }`，防手改 `{"vectors":null}` 后 panic。
4. **写盘原子化**：vectors.json / state / registry 改为临时文件 + rename，避免崩溃半截文件。
5. **ok.log 治理**：当前只增不减且会写入 embedding 错误响应体（≤512B），建议只记状态码并加大小滚动。
6. **embedding 模型漂移检测**：`ok doctor` 对比 vectors.json 向量维度与当前模型维度，不一致时提示 `ok index`。
7. **Doctor 校验 enforce glob**：用 doublestar 预编译用户配置的 glob，格式错误提前暴露（当前 malformed glob 静默不生效）。
8. **CRLF 归一**：仓库在 Windows 下全量 CRLF，`gofmt -l` 全报未格式化；建议加 `.gitattributes`（`* text=auto eol=lf`）统一为 LF。
9. **v2 候选方向**（当前为非目标，勿提前实现）：hooks 自动沉淀经验、其他 AI 工具适配、本地 embedding、知识库远程同步。
