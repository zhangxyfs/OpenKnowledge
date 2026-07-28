# hook 注入瘦身设计

日期：2026-07-28　状态：已批准（用户选择"全都要"：去重 + 降默认预算 + 检索摘要化）

## 背景与问题

当前每次 prompt hook 注入内容庞大，以本项目实测为例：

- **INDEX.md 重复**：wiki 条目同时出现在主列表（`- **标题** (type) [tags] — 摘要`）和"Wiki 目录"节（`- [标题](文件) — 摘要`），12 条 wiki 条目摘要出现两次
- **检索注入全文**：每次 prompt 检索 top-N（默认 3）命中条目，注入**完整正文**（`hook.go:184` `## 标题\n\n正文`），两篇长条目就上千 token
- **预算偏松**：`inject.max_tokens` 默认 1500（约 3000 字符），单次注入轻松超过大多数提问本身的长度

目标：大幅压缩注入体积，同时保留"发现能力"——AI 知道有哪些知识、需要时能自取全文。

## 方案（三件套）

### 1. INDEX.md 去重

`rebuildIndex`（`internal/index/sync.go:226`）主列表**排除 wiki 标签条目**（过滤口径与 `WikiEntries()` 一致：tags 含 `wiki`）；wiki 条目只出现在"Wiki 目录"节（保留链接+摘要，AI 可据此 Read 全文）。

- 无 wiki 条目时整节省略（现有行为不变）
- 非 wiki 条目行格式不变，向后兼容
- 技能文档 `openknowledge-wiki/SKILL.md` 中"summary 会出现在 INDEX.md 的 Wiki 目录里"的表述不受影响（仍成立）

### 2. 降默认值

- `inject.max_tokens`：1500 → **800**
- `retrieve.top_n`：3 → **2**

只改 `internal/config/config.go` 默认值；用户全局/项目配置里已显式写的值继续生效（覆盖链不变）。

### 3. 检索命中注入摘要而非全文

`HandlePrompt` 的检索命中段改为紧凑列表：

```
## 相关知识（需要全文时读取对应文件）
- **标题** (type) — 摘要（`<knowledge目录绝对路径>/<filename>`）
```

- mandatory 条目维持现状：每会话首次注入**全文**（它们是"必须遵守"类规则，摘要不足以约束行为）
- 摘要来自 frontmatter `summary` 字段；空摘要退化为只有标题行
- 给出文件绝对路径，AI 需要全文时用 Read 自取（条目本就是 markdown 文件，wiki 条目同理）
- `index.Query` 的 `Hit` 结构增加 `Summary` 字段（FTS 与向量两路 SELECT 均加 `e.summary`；Filename/Title/Type 字段已存在）

## 体积估算（本项目实测基线）

| 项 | 现状 | 瘦身后 |
|---|---|---|
| 首次注入（INDEX 部分） | 主列表 12 条 wiki 摘要 + Wiki 目录 12 条 ≈ 24 行 | 主列表 0 条 wiki + Wiki 目录 12 条 ≈ 12 行 |
| 每次检索注入 | top3 全文（两篇长文即 ~1200 字符以上） | top2 × 一行摘要 ≈ 2 行 |
| 预算兜底 | 1500 token | 800 token |

常规 prompt 注入从"数百~上千 token"降到"几十~一百 token"量级。

## 影响面与兼容

- `internal/config/config_test.go`、`internal/project/project_test.go` 中 1500/3 的断言同步更新
- `internal/index/wiki_test.go` 主列表断言更新（wiki 条目不再进主列表）
- hook 注入格式变化对 AI 消费者透明（纯文本，无协议约束）
- fail-open、预算截断（`TruncateToBudget`）、wiki nudge 追加等链路不变

## 测试

- rebuildIndex：含 wiki 条目时主列表无 wiki 行、Wiki 目录节保留链接；无 wiki 条目时输出与旧版逐字节一致
- config 默认值断言 800/2；项目覆盖链不变
- HandlePrompt：命中条目注入摘要行（含路径），不注入正文；mandatory 仍全文；空摘要降级
- 实测：kimi 会话里观察 hook_result 体积

## 明确不做

- 不改 embedding/检索打分逻辑
- 不动 mandatory 全文注入语义
- 不做"按 token 动态选条目数"之类的自适应策略
