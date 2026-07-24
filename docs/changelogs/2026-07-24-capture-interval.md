# capture 轮次间隔可配置 + 写配置逻辑收敛

- `ok capture interval <n>`（n ≥ 1）：设置 auto 模式的自省轮次间隔，模式保持不变。
- 语义明确：propose 模式无轮次概念（AI 自主判断是否记录）；turn_interval 仅
  auto 模式生效。GUI 沉淀卡与技能说明同步表达。
- `config.SetCapture` 抽取到 config 包，cli 与 gui 共用（消除此前的两份重复实现）。
- GUI 沉淀卡新增轮次间隔输入（1~100）与模式语义说明；`/api/capture` POST 支持
  `turn_interval` 字段（0 表示保持不变）。
- `openknowledge-capture` 技能补充 interval 用法。
