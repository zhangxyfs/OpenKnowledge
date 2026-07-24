# OpenKnowledge

为 AI 编程助手提供的**项目知识库**——知识按项目隔离，通过 Kimi Code 的 hooks 自动注入 AI 上下文，并能强制执行"改代码必须写变更日志"这类工作流规则。

单二进制 Go CLI（`ok`），零运行时依赖。

## 它能做什么

- **基础注入**：每个会话第一次提问时，自动把项目的强制约定（mandatory 条目）全文 + 知识索引发送给 AI
- **检索注入**：每次提问时按 关键词 + 向量语义 混合检索，把最相关的知识条目注入上下文（如"git 提交规范"）
- **强制检查**：跟踪 AI 改过的文件；回合结束时发现改了代码却没写变更日志，就阻断并要求补齐（同会话只阻断一次）
- **一键引导**：`ok setup` 自动完成 hooks 配置、技能安装、embedding 配置
- **随时开关**：`ok off` 全局停用，`ok on` 恢复
- **Web GUI**：双击 exe 或 `ok gui` 打开浏览器管理界面——可视化维护条目、检索预览、完成首次引导

## 安装

```bash
# 1. 构建（Go ≥ 1.25）
go build -o ok.exe ./cmd/ok        # Windows
go build -o ok ./cmd/ok            # Linux/macOS

# 2. 首次引导（幂等，可重复执行）
./ok.exe setup
```

`ok setup` 会依次：

1. 把 3 条 hook 写入 `~/.kimi-code/config.toml`（标记块幂等，自动备份原配置）
2. 安装 `openknowledge-init / on / off` 三个 kimi 技能到 `~/.agents/skills/`
3. 询问 embedding 配置（base_url / model / API key，可直接粘贴；回车跳过则只用关键词检索），写入全局配置并当场验证连通性

## 快速开始

```bash
# 在需要使用知识库的项目目录里
cd /your/project
ok init                      # 注册项目（自动取目录名）
ok add --title "变更日志强制规则" --type rule --mandatory --file rule.md
ok add --title "Git 提交规范" --type note --tags git --file git.md
ok index                     # 配好 embedding 后同步索引与向量
```

然后**新开一个 Kimi Code 会话**（hooks 在会话启动时加载）即可生效：

- 问 "git 提交规范是什么" → AI 直接引用知识库内容回答
- AI 改了代码没写变更日志 → 回合结束时被阻断提醒

也可以在 kimi 会话里直接说"初始化知识库""关闭知识库 hooks"——对应技能会自动调用。

## Web GUI

**双击 `ok.exe`（无参数运行）或执行 `ok gui`** 即可启动 Web 管理界面：浏览器自动打开 `http://127.0.0.1:<随机端口>/?token=…`，关闭窗口（页面 30 秒无心跳）或点击"关闭服务"即退出。

两个标签页：

- **管理**：项目/条目列表，新建、编辑、删除条目，检索预览，全局开关。首次使用（hooks 未安装）时该页自动隐藏。
- **引导**：一键完成 hooks 配置、技能安装、embedding 配置（等价于 `ok setup` 的图形版），另有「卸载」卡片可移除全部集成（知识库数据保留）。完成后进入管理页。

GUI 需要 `web/` 目录与 `ok.exe` 同级（或当前目录有 `web/`）。用发布构建脚本一次产出两者：

```bash
bash scripts/build-dist.sh   # 产出 dist/ok.exe + dist/web/
```

## 知识条目

每条知识是一个带 frontmatter 的 Markdown 文件（`ok add` 创建，也可手写）：

```markdown
---
title: 变更日志强制规则
type: rule              # rule | pitfall | note | reference
tags: [changelog, workflow]
mandatory: true         # true = 每会话首次提问全文注入
summary: 每次代码修改必须立即记录变更日志
---

正文（Markdown 自由格式）
```

数据存放在 `~/.openknowledge/`（集中存储，不污染项目仓库）。

**草稿流程（AI 提议，人批准）**：AI 可通过 `ok propose` 把会话中沉淀的经验记为**草稿条目**（frontmatter `draft: true`）——草稿不参与检索与注入，只出现在 `ok list` 和 GUI 管理页（带「草稿」徽标）。人确认后用 `ok approve <文件>` 或 GUI 的「采纳」按钮转正。`ok capture propose|auto` 可切换沉淀模式：`propose` 为 AI 主动提议（默认，无轮次限制），`auto` 则由 Stop hook 每隔指定轮次强制 AI 自省提取经验；轮次间隔用 `ok capture interval <n>` 配置（仅 auto 生效）。

## 常用命令

| 命令 | 作用 |
|------|------|
| `ok setup` | 首次引导：hooks 配置 + 技能安装 + embedding 配置 |
| `ok gui` | 启动 Web 管理界面（双击 exe 无参数运行同效） |
| `ok init [名字]` | 注册当前项目（名字缺省取目录名） |
| `ok add` | 新建知识条目（自动重建索引与向量） |
| `ok propose` | AI 提议草稿条目（不参与检索，待人批准） |
| `ok approve <文件>` | 批准草稿转正（同步索引与向量） |
| `ok capture [propose\|auto\|interval <n>]` | 查看/切换经验沉淀模式，配置轮次间隔 |
| `ok search <词>` | 命令行预览检索效果 |
| `ok index` | 同步索引与向量（手改条目后执行；删除 kb.db 后执行 = 全量重建） |
| `ok list` | 列出项目与条目 |
| `ok doctor` | 体检：配置、embedding 连通性、hooks 状态 |
| `ok on` / `ok off` | 全局开关（默认开启） |

## 配置

生效配置 = 内置默认 ← 全局 `~/.openknowledge/config.toml` ← 项目 `~/.openknowledge/projects/<名>/config.toml`。

```toml
# 全局配置（ok setup 可交互写入）
[embedding]
base_url = "https://api.openai.com/v1"   # 任何 OpenAI 兼容服务
api_key = "sk-..."                        # 或用 api_key_env 指向环境变量
model = "text-embedding-3-small"

# 项目配置：强制规则示例
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]                  # 改了这些 = 改了代码
changelog_glob = "docs/changelogs/**"     # 碰了这些 = 写了日志
message = "本次会话修改了代码但未更新变更日志，请先按规范补齐。"
```

## 检索算法

**用什么**：SQLite + FTS5 的混合检索——

- **关键词通道**：FTS5 全文索引 + BM25 打分（稀有词更值钱、长文不占优），按 标题/标签/摘要/正文 分权重；中文用二元组分词，零依赖
- **语义通道**：OpenAI 兼容 embedding + 余弦相似度，召回"问法不同但意思相近"的条目
- 两路分数归一化后加权混合（α/β 可调），取 top-3 注入
- **草稿条目（`ok propose` 产生）不进索引的检索通道**：FTS 与向量都排除，批准后自动参与

**为什么这么选**：关键词和语义互补是检索质量的基本盘；用 SQLite（纯 Go 移植，无 CGO）承载索引而不是引入向量数据库/搜索服务，是为了守住"单二进制、零基础设施"的部署形态——一个 `kb.db` 文件就是全部。

**能达到什么**：1 万条知识条目下单次查询约 30ms（含索引同步的热路径约 36ms）；embedding 服务不可用时自动降级为纯关键词检索，注入永不缺席。详见 [ARCHITECTURE 第 17 章](docs/ARCHITECTURE.md#17-检索算法实现深度)。

## 工作原理（简版）

Kimi Code 在三个时机调用 `ok`：

| hook | 何时执行 | ok 内部放行条件 | 作用 |
|------|----------|----------------|------|
| `UserPromptSubmit` | 每条用户消息、模型调用之前 | 全局开关开 + 目录已注册 | 首次提问注入 mandatory+索引；每次提问检索注入 |
| `PostToolUse`（matcher `Write\|Edit`） | AI 用 Write/Edit 成功改完文件后（失败不触发） | 开关 + 注册 + 能解析出项目相对路径 | 把改动文件记入会话状态 |
| `Stop` | AI 每个回合即将结束时（Esc 中断不触发） | 开关 + 注册 + 配了 `[[enforce]]` + 满足规则条件 | 改了代码没写日志 → exit 2 阻断（同会话同规则只阻断一次） |

所有 hook 路径 fail-open：任何内部错误只记日志（`~/.openknowledge/ok.log`），绝不影响正常会话。详见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

## 开发

```bash
go test ./...    # 15 个包全部测试（零网络，含端到端）
go vet ./...
```

- 设计文档：`docs/superpowers/specs/2026-07-22-openknowledge-design.md`
- 实施计划：`docs/superpowers/plans/2026-07-22-openknowledge.md`
- 变更日志：`docs/changelogs/`
