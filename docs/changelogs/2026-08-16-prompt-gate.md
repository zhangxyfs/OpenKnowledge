# 2026-08-16 泛化 prompt 门控：[retrieve.gate] + 引导页卡片与短语管理

- **问题**："继续"/"好的"/emoji 这类无信息量 prompt 也跑完整检索 + embedding，
  每轮白烧 token 与一次网络往返（embedding timeout 5s 的场景更明显）。
- **修复**：
  - `retrieve.Gated` 纯函数：归一化（小写/标点忽略/空白折叠）后精确命中短语表，
    或 `Terms` 提取为空（纯标点/emoji/单字符）→ 判定泛化；宁窄勿宽，不设长度阈值；
  - 分层短语表：内置 21 条中英常用确认词编译进二进制（随版本演进，不物化进
    config.toml），用户在 GUI 维护 `extra_phrases` 追加层，两层取并集；
  - `InjectForPrompt` 在 EmbedQuery 之前短路：门控命中连 embed 调用都省；
    mandatory/INDEX/wiki nudge 等其余注入逻辑不受影响；命中记 ok.log
    `prompt gate` 一行（GUI 日志可按"门控"过滤）；
  - 配置 `[retrieve.gate]`（enabled 默认 true / extra_phrases），`config.SetGate`
    小节重写（SetCapture 同款整段替换算法）；
  - GUI：引导页"泛化门控"卡片（启用 checkbox change 即存、内置/自定义条数状态行）
    + 短语管理弹窗（内置只读、自定义增删改、全量替换语义、服务端校验去重，
    单条 ≤64 字符 / 总数 ≤200）。
- **验证**：门控开启时 "好的" 零检索零 embed（ok.log 有门控行）；extra 登记查询词后
  同 prompt 跳过检索段、关 enabled 即恢复；GUI 弹窗增删改与去重生效。
- **测试**：`TestGated*`（内置/空 Terms/正常 prompt/extra 四分支）、
  `TestGateConfigDefaultAndOverride`（默认→全局→项目→缺键继承四态）、
  `TestSetGateAppendAndReplace`/`TestSetGateMissingFile`（追加/替换/边界/缺失文件）、
  `TestInjectGateSkipsRetrieval`（门控跳检索段 + mandatory 不受影响 + 关闭对照）、
  `TestGateRoundTrip`（API 默认视图/落盘/清洗去重/400 校验/null=不变/替换不追加）。
