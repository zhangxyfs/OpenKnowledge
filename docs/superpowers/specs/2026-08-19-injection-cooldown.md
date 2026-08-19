# 跨轮注入冷却（dedup_turns）：同 session 检索条目冷却 N 轮不重复注入

日期：2026-08-19
状态：草案（待确认）
来源：OpenViking 检索调研（`docs/2026-08-19-openviking-retrieval-review.md` §3.1，对标其 RecallLedger）

## 1. 背景与动机

检索注入（`InjectForPrompt` 的"## 相关知识"段）每轮 prompt 独立查询，无任何跨轮
记忆。实际使用中用户的连续几轮 prompt 往往围绕同一主题，同一条目会以相同名次
反复命中、反复注入，每轮都白白消耗 `[inject] max_tokens`（默认 800）预算。

现状佐证：session 状态里的 `InjectedKnowledge` 只存"最近一轮"的注入清单，且仅供
采纳归因（`TrackTouched`）使用，不用于注入决策本身——冷却机制是真实缺口。

OpenViking 的对应解法是 RecallLedger：条目注入后按 `dedup_turns` 轮冷却，冷却期
内不再注入；另有一条"以最小 detail（仅 URI）服务的条目不冷却"的精细规则。OK 的
检索注入统一是指针（标题+摘要+路径），没有 detail 分级，该规则不适用，只取
"按轮冷却"主干。

收益：省 token（直接）、给新条目让出 top_n 名额（间接提升召回多样性）、减少
注入文本的跨轮冗余（降低 lost-middle 噪声）。

## 2. 非目标

- **不动 mandatory 粘性指针**：L3 指针每轮仅几十 token，是防压缩沉没的兜底机制，
  冷却它会破坏既有语义
- **不动基础注入 / INDEX / wiki nudge**：各有独立的每会话一次或按轮次机制
- **不做条目更新提前解冻**（mtime 变化解除冷却）：v1 不感知，冷却到期自然回来
- **不做跨 session 冷却**：session 状态文件天然隔离，新会话重新开始
- fusion/rrf_k/recency/feedback 的 GUI 暴露仍列为跟进项（沿用检索进化 spec §9
  的既有决议）；**`dedup_turns` 例外，v1 即在 GUI 引导页可配**（见 §3.6）
- **hotness 升权**：feedback v2 候选，另案（见调研文档 §3.2）

## 3. 方案

### 3.1 冷却语义

- session 状态新增**prompt 轮次计数器**与**注入台账**（条目 basename → 最后注入
  的轮次号）。每轮 prompt 计数器 +1。
- 检索命中后、渲染前：条目距上次注入 ≤ `dedup_turns` 轮 → 本轮跳过；否则正常
  注入并刷新台账。
- **每个 prompt 轮都计冷却轮次，门控命中轮也不例外**。理由：语义最简单
  （"每 prompt 一轮"，无特例）；且门控轮（"继续"/"好的"）本身是内容空窗，让
  冷却在空窗期流逝，下一轮真实提问时条目可回归，行为合理。
- 冷却跳过的轮次**不记** `EventInjected`（未注入不记）——反馈降权的
  "注入 ≥4 次且采纳 0"统计不被冷却轮污染。
- 新 session 无台账，天然无冷却；session 状态文件 7 天清理沿用既有 `state.Clean`。

**设计裁决的外部印证（OpenViking 实证，2026-08-19 代码核实）**：

1. **轮次语义**：OpenViking 台账的"轮"按消息条数计（`_resolve_turn` 用
   `total_message_count`），对同时推 user+assistant 消息的宿主，其默认值 5 实际
   仅 ≈ 1-2 个真实对话轮（`docs/zh/agent-integrations/16-capability-reference.md`
   三点说明之②）。本 spec 按 prompt 轮计时，语义符合直觉；默认 3 个 prompt 轮与
   OpenViking 默认 5 消息的实际效果同量级，取值有外部参照支持。
2. **时钟必须自走**：OpenViking 在 `autoCapture=0` 且 `autoRecall=1` 时消息数
   恒 0，台账时钟停摆，已发正文的 URI 在 session 内永久冷却（同文档之③）——
   这是把冷却时钟绑在外部计数器上的坑。本 spec 的时钟是 `InjectForPrompt` 内
   自增的 prompt 轮计数器，每轮必走、无停摆模式。
3. **台账防膨胀**：OpenViking 台账只保留 `dedup_turns × 4` 轮内的记录
   （`ledger.py:147`）。本 spec 依赖既有 session 文件 7 天清理 + 单 session
   注入条目数有限，不单独做台账裁剪；若未来发现长 session 台账膨胀，可参照
   该倍数窗口补裁。

### 3.2 冷却排除不占 top_n 名额（关键决策)

排除动作放在**检索内部、top_n 截断之前**（与分支过滤同位置），而不是在 hook 层
对截断后的 hits 做事后过滤。否则 top_n=2 且两条都在冷却时，本轮注入为空——
第 3 名的新条目明明存在却被名额浪费挡住。冷却中的条目其指针仍在本 session 的
近期对话历史里，模型可据此读文件；把名额让给新条目是纯收益。

实现上检索查询入口（`QueryExBranch` 链路）新增排除集合入参（basename 集合），
由 hook 层从 session 台账计算后传入；`ok search` CLI 与 GUI 搜索调用点传空集合，
行为不变。

### 3.3 采纳归因窗口扩展

`TrackTouched` 目前只把"本会话最近一轮注入"的条目计入采纳。引入冷却后会出现
新场景：条目第 1 轮注入、第 2 轮冷却中（未再注入），但模型按历史里的指针读了
文件——按旧语义这一轮读取不归因。把归因窗口从"最近一轮注入"扩为
**"最近一轮注入 ∪ 冷却窗口内条目"**，冷却中的读取照常挂账。

### 3.4 fail-open

状态文件损坏、台账读取失败、锁超时等任何异常：按无冷却处理（回到旧行为，
每轮正常注入），仅记 ok.log。冷却纯是省 token 的优化，永不阻断注入。

### 3.5 观测

- 冷却生效跳过条目时记 ok.log（`冷却跳过（a.md、b.md）`），GUI 日志页可按
  "冷却"过滤——与门控/语义/时效/反馈各行的既有模式一致。
- `QueryInfo` 增加冷却跳过清单，供 `ok search` 诊断输出。

### 3.6 配置与默认值

```toml
[retrieve]
dedup_turns = 3   # 同 session 内检索条目冷却轮数；0 = 关闭（旧行为，每轮都注入）
```

默认 **3**、默认开启。与 feedback 默认关的取舍不同：feedback 依赖尚未接通的宿主
read 派发（信号恒零），冷却是纯机械机制、无外部依赖，只省 token 不降低召回上限，
开箱即用。用户设 0 即完整回到旧行为，可独立回滚。

**GUI 引导页可配（v1 即做）**：dedup_turns 是用户最可能想按习惯调整的检索旋钮
（与泛化门控同类——检索进化 spec 正是因此只为门控做了 GUI），v1 在引导页提供
数值输入 + 保存按钮。遵循引导页既有交互约定：**输入/勾选 + 保存按钮两段式，不做
change 即存**（即存模式误触无反悔）。取值校验 0~99 的整数（0=关闭），非法 400。
写路径新增 `config.SetRetrieveDedupTurns`：在 `[retrieve]` 小节内**单键 upsert**，
其余键保留——与 `SetInjectMandatoryMax` 同款；不能像 `SetGate` 那样整段替换，
`[retrieve]` 是含 alpha/beta/top_n/min_gap/fusion 等多人共用笔触的小节。

### 3.7 与 reinject_turns 的区别（防混淆）

两者方向相反、互不干扰：`reinject_turns` 是 mandatory 全文的**周期性重注入**
（防上下文压缩把硬约束摘要掉）；`dedup_turns` 是检索条目的**周期性抑制**
（防重复注入浪费预算）。一个管"别忘了"，一个管"别啰嗦"。

## 4. 用户故事

1. 作为与 agent 连续对话的开发者，我希望同一知识条目不在相邻几轮里反复注入，
   以便每轮的注入预算花在新的相关信息上。
2. 作为开发者，我希望围绕同一主题追问时，模型仍能依据前几轮历史中的指针读取
   该条目文件，以便冷却不影响任务连续性。
3. 作为开发者，我希望冷却期过后条目能自然回归注入，以便长会话后期话题回旋时
   重新获得提示。
4. 作为开发者，我希望冷却中的高分条目不占用 top_n 名额，以便看到排名靠后的
   新条目。
5. 作为开发者，我希望说"继续"/"好的"这类泛化 prompt 也正常消耗冷却轮次，
   以便冷却语义简单可预期。
6. 作为开发者，我希望冷却中条目的文件读取仍被记为采纳，以便反馈闭环统计
   不失真。
7. 作为开发者，我希望冷却轮次不产生注入事件，以便"持续注入但从未采纳"的
   降权判定不被冷却扭曲。
8. 作为开发者，我希望能配置冷却轮数或完全关闭（dedup_turns=0），以便按
   个人习惯调整。
9. 作为 GUI 用户，我希望在引导页用"输入数值 + 保存按钮"调整冷却轮数，
   以便不用手编 config.toml；误触时能反悔（不保存即不生效）。
10. 作为开发者，我希望新会话不受上一个会话冷却状态影响，以便每个任务干净起步。
10. 作为开发者，我希望状态文件损坏时注入照常工作，以便冷却机制永远不成为
    故障点。
12. 作为项目维护者，我希望冷却跳过在 ok.log 可见且 GUI 日志页可过滤，以便
    排查"为什么这条目没注入"类问题。
13. 作为 CLI 用户，我希望 `ok search` 结果不受冷却影响（诊断工具看到全量结果），
    以便区分"检索没召回"与"召回但被冷却"。
14. 作为 GUI 用户，我希望搜索 API 不受冷却影响，以便 GUI 里的检索行为保持稳定。

## 5. 改动点

- **internal/state**：Session 增加 prompt 轮次计数器与注入台账字段（JSON 序列化，
  旧状态文件缺字段按零值自愈）；沿用 `state.Update` 锁内读-改-写 + 原子落盘，
  不新增锁往返（读台账搭采纳入账的既有 Update，写台账搭注入挂账的既有 Update）。
- **internal/index**：检索查询入口增加排除集合入参，在分支过滤之后、top_n 截断
  之前生效；`QueryInfo` 增加冷却跳过清单。`ok search`/GUI 调用点传空集合。
- **internal/hook**：`InjectForPrompt` 检索段渲染前按台账过滤（经排除集合下推），
  注入后刷新台账；`TrackTouched` 归因窗口扩为"最近一轮 ∪ 冷却窗口内"。
- **internal/config**：`Retrieve` 增加 `DedupTurns`，默认 3；非法值（<0）按 0
  处理（关闭，fail-open 方向）。新增 `SetRetrieveDedupTurns` 写路径：`[retrieve]`
  小节内单键 upsert（沿用全局配置"锁内原子写"约定）。
- **internal/gui**：引导页新增冷却轮数配置项（数值输入 + 保存按钮两段式，
  0=关闭，校验 0~99）；新增/复用 GET/POST API 读写该项。日志页"冷却"过滤
  关键字自然生效。

## 6. 测试决策

**不新增测试接缝。** 沿用两处既有接缝：

1. **hook 核心层**（主）：`internal/hook/core_test.go` 直接调 `InjectForPrompt` /
   `TrackTouched` 的夹具模式（setupProject/writeEntry + 同 sessionID 多轮调用）。
   直接前例：`TestInjectForPromptBaseAndRetrieve` 已做"同 session 第 1/2 轮注入
   差异"断言，`TestReinjectTurnsPeriodic` 已覆盖轮次计数语义。
2. **index 层**（辅）：`internal/index` 查询测试验证排除集合的位置语义
   （分支过滤后、top_n 截断前）。前例：v2.18.1 回归测试文件。
3. **GUI/config 层**（辅）：引导页配置 API 的读写往返与非法值 400。
   前例：`internal/gui/api_test.go` 的 gate/inject 配置用例与
   `SetInjectMandatoryMax` 的单键 upsert 测试（验证其余键保留）。

**只断言外部行为**（注入文本内容、事件表记录、采纳入账），不断言台账内部结构。

测试用例：

1. 同 session 同查询连续 N 轮：第 1 轮注入，第 2~N 轮该条目缺席（"相关知识"段
   不出现或其路径不出现），第 N+1 轮恢复注入。
2. 冷却不占名额：两条可命中条目，一条冷却中 → 本轮注入的是另一条（新条目）。
3. `dedup_turns=0`：每轮都注入（旧行为回归保护）。
4. 门控命中轮消耗冷却轮次（门控轮后冷却计数与连续普通轮一致）。
5. 冷却中条目被模型读取 → `TrackTouched` 照常挂账、下一轮入账 `entry_events`。
6. 冷却轮次不记 `EventInjected`（FeedbackStats 注入计数不膨胀）。
7. 换新 sessionID → 无冷却，第 1 轮即注入。
8. 状态文件写坏 → 注入照常（fail-open），且不 panic。
9. index 层：排除集合在 top_n 截断前生效（排除第 1 名后第 3 名能进 top_n=2）。
10. GUI 配置 API：写入 5 → 读回 5；写入 0 → 读回 0（关闭）；写入 -1/100/非整数
    → 400；写入后 config.toml 的 `[retrieve]` 其余键（alpha/fusion 等）原样保留。

## 7. 发布策略

单独一个 minor 发布，可独立回滚（沿用检索进化四特性的发布原则）。升级后默认
开启（dedup_turns=3），用户配置 0 恢复旧行为。
