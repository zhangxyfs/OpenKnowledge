# 跨轮注入冷却（dedup_turns）

日期：2026-08-19

同一 session 内已注入的检索条目冷却 N 轮（默认 3）不再重复注入，避免连续同主题对话时同一条目每轮重复消耗注入预算。冷却排除在 top_n 截断之前生效——冷却条目不占名额，排名靠后的新条目得以补位；冷却中条目若被模型按历史轮指针读取，仍正常计入采纳归因。冷却轮次不记 injected 事件，反馈降权统计不受影响。借鉴 OpenViking RecallLedger，轮次语义按 prompt 轮计（门控命中轮也计），时钟自走无停摆。

配置：`[retrieve] dedup_turns = 3`（0 = 关闭，恢复每轮都注入的旧行为）；GUI 引导页新增"跨轮注入冷却"卡片可直接调整（0~99）。

补充（同日跟进）：compaction/PreCompact 压缩事件现在会连同冷却台账一起重置（`ResetBaseInjection` 内清 `InjectedLog`）——压缩后已注入指针同样被摘要/丢弃，冷却中的条目立即恢复可注入，与 mandatory 重注入同语义。Claude PreCompact 与 Reasonix compaction.complete 两条链路共用该逻辑，无压缩事件的宿主不受影响（冷却本就有界自愈）。
