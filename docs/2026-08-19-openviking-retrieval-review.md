# OpenViking 知识检索调研：对 OpenKnowledge 的借鉴价值

日期：2026-08-19
调研对象：[OpenViking](https://github.com/volcengine/OpenViking) @ commit `d88967aa`（2026-08-19；火山引擎开源，"面向 AI Agent 的上下文数据库"，VLDB 2026 VikingMem 论文实现，Python + Rust + C++ 引擎，主项目 AGPLv3，CLI/examples 为 Apache 2.0）
调研目的：评估其检索设计中哪些可移植到 OpenKnowledge（Go + SQLite + FTS5 + 内存余弦，万条级单用户本地知识库）

> 独立第三方调研，非官方对比，与 OpenViking 项目方无关。文中 `file:line` 引用均基于上述 commit，上游更新后行号可能漂移。

---

## 1. OpenViking 检索机制速览

- **分层摘要**：写入时异步加工为 L0 `.abstract.md`（~100 token）/ L1 `.overview.md`（~2k token）/ L2 原文，目录级摘要由 LLM 自底向上 DAG 生成（`openviking/storage/queuefs/semantic_processor.py:292`，`semantic_dag.py:146`）。
- **目录递归检索**：全局检索只取目录摘要（level 0/1）→ rerank 重排目录 → 优先队列递归下钻，子分与父分线性混合 `final = α·child + (1-α)·parent`（`openviking/retrieve/hierarchical_retriever.py:396,504`）；top-k 连续 3 轮不变即收敛停钻（`:533-556`）。
- **混合检索融合下沉引擎**：dense + sparse logit 按 `sparse_weight` 在向量引擎内部融合（`storage/vectordb_adapters/base.py:248-263`），不是应用层 RRF。
- **hotness 生命周期分**：`freq = sigmoid(log1p(active_count))`，`recency = exp(-(ln2/7d)·age)`，`hotness = freq·recency`，与语义分线性混合（`retrieve/memory_lifecycle.py:19-64`）；使用反馈在 session commit 时回写 `active_count`。
- **上下文装配**：分桶配额 + peer 罚分 + 4 倍超取（`retrieve/context_assembler/gather.py:260,408`）；预算装配"宽度优先落 tier → 剩余预算逐级加深 → spare pass 去掉单条 cap 花光预算"（`budget.py:85-194`）；跨轮冷却台账 RecallLedger 按 `dedup_turns` 轮去重（`ledger.py:52,118`）；LLM digest 重写带强合约校验，无效 URI 的 bullet 直接丢弃（`rewrite.py:36-66`）。
- **查询理解**：IntentAnalyzer 用 LLM 生成多个 TypedQuery 并发检索（`retrieve/intent_analyzer.py`）；查询扩展上限 3 条、5s 超时、失败闭环回原 query（`context_assembler/expansion.py`）。
- **观测**：`retrieve/retrieval_stats.py` 采集 zero-result 率、分数分布、rerank 回退率。

## 2. 与 OpenKnowledge 现状的对照

| 维度 | OpenViking | OpenKnowledge |
|---|---|---|
| 检索单元 | L0/L1/L2 分层 + 目录树 | 单 Markdown 条目，无 chunking（`internal/entry/entry.go:147`） |
| 融合 | 引擎内 dense+sparse 加权 | 应用层 RRF（默认，k=60），weighted 回滚档（`internal/index/query.go:325`） |
| 准入 | rerank 阈值 0.1 + 按排序花预算 | 分通道独立准入 + 按查询分布动态 SemanticFloor（`query.go:40-64`） |
| 向量索引 | C++ 引擎 / 云端 / GPU cuVS | 内存暴力余弦（万条毫秒级，够用） |
| 查询理解 | LLM 意图分析 + 扩展 | 仅分词 + 泛化门控（hook 路径明确不加 LLM） |
| 反馈信号 | active_count → hotness 升权 | entry_events → 采纳降权 ×0.8（只降不升，默认关） |
| 跨轮去重 | RecallLedger 冷却 dedup_turns 轮 | **无**（挂账仅用于采纳归因，见 `internal/hook/core.go:211,294`） |
| 时效 | 半衰期 7 天指数衰减（hotness 内） | 按类型分窗线性衰减 ×0.85~1.0（`internal/index/recency.go:17`） |

## 3. 值得借鉴（按 ROI 排序）

### 3.1 跨轮注入冷却台账（高价值、低成本）⭐

**问题**：OK 目前检索注入每轮独立查询，同一 session 内若多轮 prompt 相似，同一条目会反复注入，反复消耗 800 token 预算。OpenViking 的 RecallLedger 按条目冷却 `dedup_turns` 轮（`ledger.py:52`），且有一条精细规则：以最小 detail（只给 URI 指针）服务的条目**不冷却**。

**OK 落地基础**：`entry_events` 表与 session 状态挂账已存在，冷却判定只需在 `InjectForPrompt`（`internal/hook/core.go:151`）查询后过滤"本 session 最近 N 轮已注入"的条目。注意与采纳归因共存：冷却中的条目若被模型读取仍应记采纳。

### 3.2 hotness 公式作为 feedback v2 升权（中价值）

OK 的反馈目前"只降不升"（`internal/index/feedback.go:17` 注释：加分会自我强化造成条目固化），spec 把 boost 列为 v2 候选。OpenViking 给出了一个经过论文验证的防固化升权形式：

```
freq = sigmoid(log1p(active_count))      # 天然饱和，老条目不会无限累积
hotness = freq · exp(-(ln2/half_life)·age_days)   # 不用的条目热度自动衰减
```

sigmoid(log1p) 的饱和性 + 时间衰减正好回应了"自我强化固化"的担忧：升权有上限且会过期。数据侧 OK 已有 injected/adopted 事件，可直接计算。

### 3.3 "分数带太窄，按排序花预算"的装配策略（中价值，架构性参考）

OpenViking 实测注释（`budget.py:163-166`）：分数聚在 0.38-0.50 窄带，**不设绝对加深阈值**，而是按排序宽度优先落 tier，剩余预算再逐级加深。这与 OK 的 SemanticFloor"按查询分布动态定门槛"是同一结论的独立验证（两边都发现绝对分数阈值不可靠）。

对 OK 的启示在注入侧而非准入侧：当前 OK 注入固定 top_n=2 指针、不强行凑满；若未来做分层详情（摘要 → 全文）或"预算有剩余就多给一条"，应采用 OpenViking 的预算驱动加深而非分数阈值驱动。

### 3.4 检索健康统计（低-中价值）

zero-result 率、语义通道拒绝率、分数分布的持续采集（`retrieval_stats.py`）。OK 已有 `QueryInfo` 暴露 SemanticRejected/RecencyShifted/FeedbackDemoted（`query.go:70-83`），缺的是聚合与展示——可在 GUI 加检索健康页，成本取决于 GUI 排期。

### 3.5 digest 引用的强合约校验（低价值，备用）

LLM 重写摘要时只接受带合法 URI 引用的 bullet，无效引用丢弃（`rewrite.py:36-66`）；模型报 NO_RELEVANT_MEMORY 时整块置空且不写台账。OK 目前 hook 路径无 LLM 重写，若未来在 `ok search`/GUI 加 LLM 摘要可直接借用该合约。

## 4. 明确不适合 OpenKnowledge 的部分

- **目录递归检索 + 分层 DAG 摘要**：OpenViking 的核心创新，但前提是"海量异构资源 + 目录树 + 异步 LLM 加工管线"。OK 是扁平 `knowledge/*.md`、万条级、写入即索引入库，层级下钻收益为零、成本巨大。
- **引擎内混合融合**：OK 用 SQLite + 内存计算，无引擎内融合条件；应用层 RRF 已解决尺度桥接问题（v2.17.0）。
- **hook 路径 LLM 意图分析/查询扩展**：OK 设计文档已明确"不在 hook 路径加 LLM rerank（延迟敏感）"（`docs/superpowers/specs/2026-08-16-retrieval-evolution.md:26`）。如要做，按既定方针只放 `ok search` CLI，OpenViking 的护栏参数可参考：扩展上限 3 条、超时 5s、失败闭环回原 query。
- **多租户/peer 罚分/GPU 索引**：单用户本地场景用不上。

## 5. 互相印证的设计结论（不改，增强信心）

1. **绝对分数阈值不可靠**：OpenViking 实测分数带 0.38-0.50 故不设加深阈值；OK 因余弦基线 0.4+ 改动态 SemanticFloor。两个独立项目得出同一结论。
2. **宁缺毋滥**：OpenViking 查询扩展失败闭环回原 query、无相关记忆整块置空；OK 无显著头部则语义通道整体拒绝（+Inf）。
3. **正文不直接进上下文**：OpenViking resources/skills 默认只给 256 字符摘要防凭据泄露；OK 只注入指针（标题+摘要+路径）由模型自行读文件，天然规避。
4. **退化要可见**：OpenViking rerank 失败回退向量分并统计回退率；OK 语义退化时注入中文提示行（每会话一次，`core.go:274-281`）。

## 6. 行动建议

| 优先级 | 事项 | 落地位置 |
|---|---|---|
| 高 | 跨轮注入冷却（dedup_turns，默认建议 3~5 轮） | `internal/hook/core.go` 检索注入段 + session 状态 |
| 中 | feedback v2 升权采用 sigmoid(log1p(count))·exp 衰减形式 | `internal/index/feedback.go`（等 read 派发接通后） |
| 中 | GUI 检索健康页（zero-result 率、SemanticRejected 聚合） | `internal/gui` |
| 低 | `ok search` 的 LLM 查询扩展（≤3 条、5s 超时、失败闭环） | `internal/cli`（明确不进 hook 路径） |
| 备用 | LLM digest 强合约校验 | 未来 LLM 摘要场景 |
