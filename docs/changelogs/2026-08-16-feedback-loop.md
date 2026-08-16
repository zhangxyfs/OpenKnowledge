# 2026-08-16 注入→采纳反馈闭环：持续注入零采纳条目降权（retrieve.feedback）

- **问题**：注入后模型读没读该条目，系统完全不知道——没有反馈闭环，持续
  不相关的条目永远占着 top_n 名额。
- **修复**：
  - 新表 `entry_events(filename, kind, ts)`（kind ∈ injected|adopted，
    索引 (filename, ts)）：Open 的 schema 常量含 CREATE TABLE IF NOT EXISTS，
    老库打开即自动迁移；Sync 顺带 prune 60 天前事件；
  - 数据流：prompt hook 选定 hits 时写 injected 事件 + session.InjectedKnowledge；
    post-tool hook 规范化路径位于知识库目录且 basename 命中本会话注入清单时
    记 session.AdoptedKnowledge（EqualFold 匹配，原始大小写入账）；下一次
    InjectForPrompt 开头把挂账入账 entry_events(adopted) 并清空——
    **post-tool 不开库**（避免每工具调用加 DB 依赖），会话就此结束则挂账
    丢失（统计性信号，可接受）；
  - 归因窗口 = 本会话（只统计"读本会话注入过的条目"）；mandatory 粘性指针
    重读不计入（mandatory 不经检索，天然不在注入清单）；
  - **v1 只降不升**：30 天窗口内 injections ≥ min_injections（默认 4）且
    adoptions == 0 → score ×= demote（默认 0.8）；加分会自我强化造成条目
    固化，降权只修"持续噪声"这一种确定的问题；与时效系数叠乘
    （0.85×0.8=0.68），不设额外下限；
  - 观测：降权命中时 hook 记 ok.log `prompt feedback:` 一行、ok search 打
    stderr（GUI 日志可按"反馈"过滤）；fail-open：事件写失败/统计查询失败
    仅记日志或跳过降权；
  - 配置 `[retrieve.feedback]` enabled（默认 true）/ window_days（30）/
    min_injections（4）/ demote（0.8）；GUI 暴露列为跟进项。
- **测试**：`TestRecordEventsAndStats` / `TestFeedbackStatsWindowAndPrune`
  （窗口截止 + prune 边界）/ `TestSessionAdoptedKnowledge`（去重/落盘/
  清挂账/Update 合并不丢）/ `TestAdoptionLoop`（hook 集成：注入挂账 →
  读知识库文件采纳 → 下轮入账；mandatory 与未注入条目不计；项目内文件
  Touched 回归）/ `TestApplyFeedback`（4 注入 0 采纳触发/有采纳不触发/
  未达次数不触发/关闭/nil stats/demote 非法）/ `TestQueryFeedbackDemote`
  （真实库 ×0.8 倍率 + 有采纳恢复）/ `TestFeedbackConfigDefaultAndOverride`
  （配置四态）；全仓 go test ./... 绿。
