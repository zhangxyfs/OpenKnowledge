# OpenKnowledge 设计文档

日期：2026-07-22
状态：已确认

## 1. 背景与目标

OpenKnowledge 是一个为 AI 编程助手提供的项目知识库系统。知识按项目隔离，通过
Kimi Code 的 hooks 机制自动注入 AI 上下文，解决两类问题：

1. **静态项目约定**：架构说明、代码规范、强制工作流（如"每次代码修改必须立即
   记录变更日志"）等，需要始终在 AI 上下文中在场。
2. **踩坑经验积累**：报错原因、修复方法、注意事项等随使用逐渐积累的知识，需要
   在相关提问时被检索注入。

v1 只支持 Kimi Code，调用方式只用 hooks。

## 2. 范围

### v1 包含

- Go 单二进制 CLI（`ok`），内含 hooks 入口与管理子命令
- 集中式存储 + 项目注册表（按 cwd 路由项目）
- 每会话首次提问注入（mandatory 条目全文 + 轻量索引）
- UserPromptSubmit 混合检索注入（关键词 + 向量语义）
- PostToolUse + Stop 强制检查（v1 规则类型：`changelog_required`）
- OpenAI 兼容 API 的 embedding
- `ok setup` 首次引导（自动写入 hooks 配置、安装 kimi 技能）
- `ok setup` 交互式配置 embedding（写入全局配置，一次问答）
- hooks 全局开关（`ok on` / `ok off`，默认开启，持续到手动切换）

### 非目标（v1 不做）

- 其他 AI 编程工具（Cursor、Claude Code 等）的适配
- hooks 自动从会话中提取经验写入知识库（写入路径以人工维护为主）
- 本地 embedding 模型（会引入 ONNX/Python sidecar，破坏单二进制）
- MCP server 形态
- 多人协作与知识库远程同步

## 3. 总体架构

单一 Go 二进制 `ok`，两类子命令：

- **`ok hook <event>`**：hooks 调用的入口。从 stdin 读取事件 JSON，处理，按
  Kimi Code hooks 约定响应（exit 0 = 放行，stdout 追加进上下文；exit 2 = 阻断，
  stderr 为阻断原因）。
- **管理子命令**：`init / add / search / index / list / doctor`，供人维护知识库。

Kimi Code 全局配置 `~/.kimi-code/config.toml` 中只需配置一份 hooks（见第 8 节），
hook 按事件 stdin 中的 `cwd` 自动路由到对应项目知识库。

## 4. 存储布局

集中存储于用户目录：

```
~/.openknowledge/
├── ok.log                  # 运行日志（hook 错误等）
├── registry.toml           # 项目注册表
├── config.toml             # 全局配置：embedding/inject/retrieve 默认值（项目可覆盖）
├── hooks-disabled          # 全局开关标志文件（存在即关闭）
└── projects/
    └── <项目名>/
        ├── config.toml     # 本项目 KB 配置：embedding、注入预算、强制规则
        ├── knowledge/      # 知识条目，一文件一条，Markdown
        ├── INDEX.md        # 自动生成的轻量索引（标题+摘要+tags）
        ├── vectors.json    # 各条目 embedding 缓存（含文件 mtime）
        └── state/          # 会话运行时状态 session-<session_id>.json
```

**配置合并**：生效配置 = 内置默认值 ← 全局 `~/.openknowledge/config.toml` ←
项目 `config.toml`，后者覆盖前者。全局配置只承载 `[embedding]`/`[inject]`/
`[retrieve]`；`[[enforce]]` 规则仅项目级。

**API key 解析顺序**：项目 `api_key` → 全局 `api_key` → `api_key_env` 指向的
环境变量 → 无（仅关键词检索）。`ok setup` 交互写入的 key 存于全局配置的
`api_key` 字段（文件权限 0600）。

`registry.toml`：

```toml
[[project]]
name = "OpenKnowledge"
paths = ["D:/develop/OpenKnowledge"]
```

**项目路由**：hook 从 stdin 取 `cwd`，对所有注册项目的 `paths` 做最长前缀匹配；
匹配前对路径做规范化（统一分隔符为 `/`，Windows 下大小写不敏感）。匹配不到
任何项目时静默 exit 0（fail-open，不打扰未注册的项目）。

## 5. 知识条目格式

每条知识是一个带 YAML frontmatter 的 Markdown 文件：

```markdown
---
title: 变更日志强制规则
type: rule              # rule | pitfall | note | reference
tags: [changelog, workflow]
mandatory: true         # true 时 SessionStart 全文注入
summary: 每次代码修改必须立即记录变更日志
---
正文（Markdown 自由格式）
```

- `type`：`rule`/`note` 表示静态约定，`pitfall`/`reference` 表示积累的经验与速查。
- `mandatory: true` 的条目在 SessionStart 时全文注入，保证强制约束始终在场。
- `INDEX.md` 由工具从全量条目自动生成（标题 + summary + tags），禁止手改；
  `ok add` 或手动增删条目后自动重建。

## 6. 检索与注入

**注入通道只有 UserPromptSubmit**（实测 SessionStart 的 stdout 不进入上下文，
见附录 A）。注入分两部分：

### 基础注入（每会话首次提问一次）

某会话第一次触发 UserPromptSubmit 时，基础注入 = 全部 `mandatory: true` 条目
全文 + INDEX.md 全文。注入后在 session state 中标记 `base_injected`，后续提问
不再重复（内容已存在于会话历史中）。

### 检索注入（每次提问）

混合检索，每条知识得分 = `α·关键词分 + β·语义分`（α、β 在项目 config.toml 的
`[retrieve]` 节配置，默认 1.0 / 1.0；top-N 同在 `[retrieve]`，键名 `top_n`）：

- **关键词分**：用户提问对 title / tags / summary 的匹配，tags 权重最高。
- **语义分**：调 embedding API 嵌入用户提问，与 vectors.json 中缓存的条目向量
  计算余弦相似度。

取 top-N（默认 3）注入全文；`mandatory` 条目已在上下文中，不重复注入。

### 索引化检索（v1.3，万级条目）

检索路径**不再按次扫描 Markdown 文件**，而是查询预建的 SQLite 索引
（`projects/<名>/kb.db`，纯 Go 的 `modernc.org/sqlite`，无 CGO）：

- **同步**：Markdown 文件仍是唯一真相源；`ok add`/`ok index` 及 hook 查询前
  按 filename+mtime 做增量同步（变化才重写；FTS 由触发器自动维护）。
  旧 `vectors.json` 在首次打开时自动导入并改名为 `.bak`。
- **关键词**：FTS5 虚表（title/tags/summary/body 四列，bm25 列权 10/8/3/1），
  CJK 文本入库与查询同口径二元组预切分；BM25 分归一化 `s/(s+6)` 到 [0,1)。
- **语义**：向量 blob 存库，查询时一次读入内存算余弦（万条约 60MB、毫秒级）。
- **混合**：`α·归一BM25 + β·余弦`，score>0，降序取 top-N，仅 top-N 回库取正文。
- **规模目标**：1 万条目单次查询（不含 embedding API 调用）< 50ms。

约束：

- embedding 请求独立超时（默认 5s），失败或超时**降级为纯关键词检索**。
- 条目向量在 `ok add` 时自动增量更新（按文件 mtime 判断）；hook 路径上从不为
  条目算向量，每次调用只为提问算一次 embedding。
- 注入文本（基础注入 + 检索注入合并）按 `inject.max_tokens` 预算截断，token 按
  "字符数 ÷ 2"保守估算；超出时 mandatory 条目优先，检索结果其次，INDEX 最后。

## 7. 强制检查（PostToolUse + Stop）

- **PostToolUse**（matcher: `Write|Edit`）：从 `tool_input.path` 取文件路径，
  把本会话触碰的文件路径追加到 `state/session-<session_id>.json`。
- **Stop**：评估项目 config.toml 中的 `[[enforce]]` 规则。v1 支持一种类型：

```toml
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go", "src/**"]        # 触碰这些算"改了代码"
changelog_glob = "docs/changelogs/**"     # 触碰这些算"写了日志"
message = "本次会话修改了代码但未更新变更日志，请先按规范补齐。"
```

判定逻辑：会话触碰文件中存在匹配 `code_globs` 的，且不存在匹配
`changelog_glob` 的 → exit 2 阻断，stderr 输出 `message`，模型被要求继续执行。

防死循环：同一会话同一规则只阻断一次（记入 session state）；模型补写日志后
PostToolUse 会记录，下次 Stop 自然放行。无 enforce 配置时 Stop 直接 exit 0。

设计边界：变更日志的目录结构、INDEX 更新细则等属于知识条目正文内容，由注入
教会 AI；hook 只机械检查"日志文件有没有被碰过"，不理解细则，保持检查逻辑
简单可靠。

会话状态文件按 `session_id` 隔离，记录触碰文件、已阻断规则与 `base_injected`
标记；超过 7 天的状态文件在首次提问注入时清理。

## 8. CLI 命令面与 hooks 配置

| 命令 | 作用 |
|---|---|
| `ok init [name]` | 在当前目录注册项目（写 registry），创建 KB 骨架；name 缺省取当前目录基名。并打印 hooks 配置提示 |
| `ok add` | 按模板新建知识条目（flags: `--title --type --tags --mandatory --file`；`--file` 指定正文来源文件，不带时生成模板文件供手动编辑），自动重建 INDEX、增量更新向量 |
| `ok search <query>` | 命令行跑一遍混合检索，预览注入效果（调试用） |
| `ok index` | 全量重建 vectors.json |
| `ok list` | 列出项目与条目 |
| `ok doctor` | 检查注册表、配置、embedding API 连通性、hooks 安装状态、开关状态 |
| `ok setup` | 首次引导：备份并以标记块幂等写入 hooks 配置到 `~/.kimi-code/config.toml`（命令用自身 exe 绝对路径），安装 kimi 技能到 `~/.agents/skills/`（openknowledge-init / on / off），打印使用引导 |
| `ok on` / `ok off` | hooks 全局开关：删除/创建 `~/.openknowledge/hooks-disabled` 标志文件 |
| `ok hook prompt` | UserPromptSubmit 入口（基础注入 + 检索注入） |
| `ok hook stop` | Stop 入口 |
| `ok hook post-tool` | PostToolUse 入口 |

hooks 配置块（全局一份，由 `ok init` 打印供用户追加）：

```toml
[[hooks]]
event = "UserPromptSubmit"
command = "ok hook prompt"
timeout = 10

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "ok hook post-tool"
timeout = 5

[[hooks]]
event = "Stop"
command = "ok hook stop"
timeout = 5
```

项目 config.toml 中的 embedding 配置（全部可选，缺省继承全局配置）：

```toml
[embedding]
base_url = "https://api.openai.com/v1"
api_key = "sk-..."               # 直接存 key（0600）；与 api_key_env 二选一
api_key_env = "OPENAI_API_KEY"   # 或指向环境变量，不写明文
model = "text-embedding-3-small"
timeout_sec = 5

[inject]
max_tokens = 1500        # 每次注入的预算上限（基础注入与检索注入合并计算）

[retrieve]
alpha = 1.0              # 关键词分权重
beta = 1.0               # 语义分权重
top_n = 3                # UserPromptSubmit 注入条数
```

### 8.1 首次引导与全局开关

**`ok setup`** 做的事（全部幂等，可重复执行）：

1. 取自身 exe 绝对路径（`os.Executable`），hooks command 不再需要手动填路径。
2. 备份 `~/.kimi-code/config.toml` 到 `config.toml.bak-openknowledge`，然后以
   标记块 `# >>> openknowledge hooks >>>` / `# <<< openknowledge hooks <<<`
   写入三条 hook；已存在标记块则原位替换，否则追加到文件末尾；文件不存在则
   新建。kimi 主目录优先取 `KIMI_CODE_HOME` 环境变量。
3. 安装三个用户技能到 `~/.agents/skills/`（可用 `OK_SKILLS_HOME` 覆盖）：
   `openknowledge-init`（在项目目录执行 `ok init`）、`openknowledge-on`、
   `openknowledge-off`。技能内容里烧入 exe 绝对路径，不依赖 PATH。
4. **交互式配置 embedding**（语义检索）：询问 base_url / model / API key
   （可直接粘贴；留空跳过且不破坏已有全局配置），写入全局
   `~/.openknowledge/config.toml`（0600），并立即做一次连通性验证。
   非交互场景可用 flags：`--embedding-base-url` / `--embedding-model` /
   `--embedding-key`（任一 flag 存在则跳过提问）。
5. 打印使用引导（init → add → 新会话生效）。

**全局开关**：`ok off` 创建 `~/.openknowledge/hooks-disabled` 标志文件，
`ok on` 删除之。三个 hook 入口在处理任何逻辑前先检查该文件，存在即静默
exit 0（HandleStop 也放行）。默认状态为开启（文件不存在），关闭持续到
手动 `ok on`，不分会话。`ok doctor` 报告开关状态与 hooks 安装状态。

## 9. 错误处理

- **全面 fail-open**：hook 路径上任何内部错误（配置缺失、API 故障、文件损坏）
  只记日志到 `~/.openknowledge/ok.log`，exit 0，绝不影响正常会话。
- embedding 失败降级为纯关键词检索（见第 6 节）。
- Stop 阻断有防死循环（见第 7 节）。

## 10. 测试策略

- Go 单元测试：存储读写、frontmatter 解析、混合检索打分、enforce 规则判定、
  项目路由（最长前缀匹配）。
- 集成测试：用编译好的二进制喂 stdin JSON 模拟三种 hook 事件，断言 exit code
  与 stdout 内容（表驱动）。
- embedding API 抽象为接口，测试用 fake 实现，不碰真实网络。

## 11. 技术决策摘要

| 决策点 | 结论 |
|---|---|
| 技术栈 | Go 单二进制（hook 启动快、零运行时依赖） |
| 存储 | 集中存储 `~/.openknowledge/` + registry 项目映射 |
| 注入时机 | UserPromptSubmit 单通道（首次提问基础注入 + 每次检索注入）+ Stop 强制检查 |
| 检索 | 关键词 + 向量语义混合，embedding 走 OpenAI 兼容 API |
| 知识写入 | 人工维护 Markdown 为主，`ok add` 辅助 |
| 失败策略 | 全面 fail-open |

## 附录 A：Kimi Code hooks 实测行为（0.28.1 实测验证）

以下来自对真实 Kimi Code 0.28.1 的 hook 载荷抓取与标记注入实验：

- `UserPromptSubmit` 载荷的 `prompt` 是内容块数组：
  `"prompt":[{"type":"text","text":"..."}]`，不是字符串。解析需兼容数组
  （拼接 text 块）与字符串两种形态。
- `SessionStart` 的 stdout **不进入上下文**（观察型事件，fire-and-forget）；
  其载荷含额外字段 `source`（`startup`/`resume`）。因此基础注入只能走
  UserPromptSubmit。
- `PostToolUse` 载荷中文件路径字段是 `tool_input.path`（不是 Claude Code 的
  `file_path`）；解析时 `path` 优先、兼容 `file_path`。
- hook 命令在 Windows 上由系统 shell 执行，`sh -c` 不可用；直接的可执行文件
  路径（如 `D:/path/ok.exe hook prompt`）可用。

