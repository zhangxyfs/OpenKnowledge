# 知识索引膨胀治理（方案 A：预算感知精选索引）设计

日期：2026-08-16
状态：已确认（用户选定方案 A 为主干）

## 背景与问题

`INDEX.md` 由 `internal/index/sync.go` 的 `rebuildIndex` 每次同步从 entries 表全量重写，每条目一行 `- **标题** (类型) [tags] — 摘要`，按 filename 排序、无上限。会话首次注入把整个 INDEX 塞进 prompt（`internal/hook/core.go:93`），外层仅按 `Inject.MaxTokens`（默认 800 token）截断（`core.go:205`）。

后果：

1. 条目增多后索引超出注入预算，尾部条目被静默截断，等于白沉淀；
2. 每次会话首次注入 token 开销随条目数线性增长；
3. 大量条目摘要复读标题（渲染层原样照写），行体积虚高。

本设计在索引渲染层做预算感知治理，hook 注入层零改动。

## 方案概览（已确认的四件事）

1. 摘要去重（渲染层兜底 + 源头约定）；
2. 价值排序 + 溢出折叠（核心）；
3. 生命周期归档（手动归档 + 候选报告，不做默认自动归档）；
4. 配置新增 `[index] max_lines`。

明确不做：分级懒加载索引（方案 B）、纯检索化（方案 C）、默认自动归档。

## ① 摘要去重

在 `rebuildIndex` 渲染行时做冗余判定，满足任一即省略摘要：

- 规范化（去首尾空白、去末尾标点）后 summary == title；
- 规范化后 summary 以 title 为前缀；
- title 与 summary 的共有前缀长度 ≥ summary 长度的 80%。

省略后行格式退化为 `- **标题** (类型) [tags]`。

兜底放在渲染层，存量条目无需回填。同时修改 `ok add` 与 openknowledge-propose / openknowledge-wiki 技能的提示词，新增"摘要不得复读标题，应补充标题之外的线索"约定，防止新增重复。

## ② 价值排序 + 溢出折叠

### 排序

- 排序信号复用现有 `internal/index/events.go` 的 `FeedbackStats(windowDays)`（30 天窗口内各条目的注入/采纳计数），不新建统计。
- 主键：`采纳×2 + 注入×1` 降序；平局按 `updated` 降序。
- 草稿条目（`draft != 0`）固定沉底，保留【草稿】前缀。
- 无事件数据（新库/新装）时自然退化为按 `updated` 倒序，零回归。
- wiki 主目录节、分支差异节维持现有按序输出，不参与重排（已是带链接的指针形态，天然轻量）。

### 溢出折叠

- 新增配置 `[index] max_lines`（TOML 键 `index.max_lines`，默认 50，<=0 按 50）。只约束主列表行数，不含 Wiki 目录节与分支差异节。
- 超出预算的条目不再被外层 token 截断静默丢掉，而是在主列表末尾折叠成一行：

  ```
  - 另有 N 条未列出（tags 分布：agentx×12, hooks×8, …），可用关键词/向量检索命中
  ```

  tags 分布取被折叠条目的 tag 计数降序前 5 个；被折叠条目无 tag 时省略括号段。
- 外层 `Inject.MaxTokens` 截断逻辑与 mandatory 保护保持不变。

## ③ 生命周期归档

- 条目 frontmatter 新增 `archived: true` 标记。归档条目保留在库中（检索可命中），但不进 INDEX 主列表；`rebuildIndex` 渲染时跳过。
- 新增 `ok archive <file...>` 子命令：给指定条目写入 `archived: true` 并触发同步；`ok archive --undo <file...>` 移除标记。
- `ok index` 输出末尾附"归档候选"报告：创建超过 90 天且 30 天窗口内零注入零采纳的条目列表（仅提示，不动数据）。
- 不做默认自动归档：静默移动知识风险太大，由人根据报告决定。

## ④ 错误处理与兼容

- 配置缺键全部走默认值（`max_lines=50`）；`archived` 缺键视为 false。
- `FeedbackStats` 查询失败时退化为 `updated` 倒序并记日志，不阻断 Sync。
- 归档候选统计失败只省略报告，不影响 `ok index` 主输出。
- 既有 `draft`、`branch:`、wiki 目录逻辑语义不变。

## ⑤ 测试

- `internal/index` 单测：
  - 去重三条规则各自命中与不命中；
  - 排序：有事件数据按加权降序、平局按 updated、无事件退化 updated 倒序、草稿沉底；
  - 折叠行：N、tags 分布前 5、无 tag 省略括号、未超限不折叠；
  - `archived: true` 条目不进 INDEX 但检索可命中。
- `internal/hook` 集成测试：构造超预算索引，断言注入文本含折叠行且不含被折叠条目标题。
- 更新 `index_test.go` 中依赖 filename 排序的旧断言。
- `internal/cli`：`ok archive` / `--undo` 写标记并触发同步的测试。

## 改动范围

- `internal/index/sync.go`：rebuildIndex 渲染层（去重、排序、折叠、归档过滤）；
- `internal/index/events.go` 或新文件：排序所需的窗口统计查询（复用 FeedbackStats）；
- `internal/config/config.go`：`[index]` 段与 `max_lines` 键；
- `internal/cli/`：`ok archive` 子命令、`ok index` 归档候选报告；
- `internal/entry/entry.go`：`archived` frontmatter 解析；
- 技能/提示词：`ok add`、openknowledge-propose、openknowledge-wiki 的摘要约定；
- hook 注入层（`internal/hook/core.go`）：零改动。
