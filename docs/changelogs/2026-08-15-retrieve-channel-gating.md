# 2026-08-15 检索注入按通道准入：min_score 阈值 + 库规模缩放 + 语义退化提示

- **问题**：UserPromptSubmit 注入的"相关知识"经常与提问无关（如问 deepseek apiKey 却注入经验沉淀机制/Web GUI/多-Agent支持）。根因有二：
  1. 本项目 kb.db 是 ≤2.13 历史索引（向量无模型身份记录），`embedx.QueryVec` 每次拦截语义通道、退化为纯关键词检索——几百条 `prompt embed identity` 告警只写 ok.log，用户不可见；
  2. 关键词通道零阈值（score>0 即注入、top_n 强行凑满），CJK 二元组伪词（"已经验证"切出"经验"）命中标题即可注入。
- **修复（检索机制变更）**：
  - `config.Retrieve` 新增 `min_score`（默认 0.5，≤0 关闭）：**准入按通道独立判定**——关键词通道需归一 BM25 分（未乘 α）≥ 阈值，语义通道需余弦 ≥ 阈值，满足其一才注入；总分只用于排序。同域语料下无关文本余弦基线可达 0.4+，用混合总分做准入会把伪词命中顶过阈值，故必须分通道；
  - `index.MinScoreFloor`：阈值随可检索条目数缩放（n<10 关闭、10→30 线性过渡、n≥30 取配置值）——FTS5 bm25 的 idf 在小库下趋近 0（N=2 时为 0），固定绝对阈值会误伤小库真实命中；
  - 语义通道退化（模型身份缺失/切换）时注入末尾附 `[OpenKnowledge] 语义检索退化：…` 提示（每会话一次，新增 `state.Session.RetrieveWarned`），把原来只进 ok.log 的告警暴露给模型；
  - 数据侧：`ok index` 已重建（"历史向量无模型身份记录，按当前模型全量重建"，embedding：builtin:qwen3-emb-0.6b-q8），语义通道恢复。
- **验证**：用户原话"我已经验证了A 没啥问题，你也可以看看 deepseek apiKey"→ 零注入；
  "多-Agent支持 十个适配器"→ 精确命中 多-Agent支持（agentx）/架构总览/演进历程；
  "帮我写一个 python 爬虫"→ 零注入。
- **测试**：`TestMinScoreFloor`（缩放曲线）/`TestQueryMinScore`（12 条目场景）
  /`TestQueryChannelAdmission`（关键词强命中+向量正交仍准入、语义弱信号不凑数）
  /`TestInjectSemanticDegradeHintOnce`（退化提示每会话一次，httptest 假 embedding）；
  全仓 `go test ./...` 26 包绿。
## 补充：语义门槛改为模型无关（同日追加）

- **问题**：`min_score` 的语义门槛是绝对余弦值，但余弦分布随 embedding 模型漂移。
  同组查询实测（bge-m3 硅基流动 vs qwen3-emb-0.6b 本地）：
  bge-m3 跨域无关噪声高达 0.52、qwen3 仅 0.26；同域闲聊 bge-m3 0.46-0.58。
  固定 0.5 门槛对 bge-m3 会漏噪声，对低对比度自定义模型会误杀真相关。
- **修复**：新增 `index.SemanticFloor`——以本次查询的余弦分布为参照：头部（max）
  相对中位数有显著分离（相对 gap ≥ 0.25）时门槛 = max(绝对下限, median+0.5·gap)，
  无显著头部则语义通道整体不准入（关键词通道兜底）；`min_score` 退化为绝对下限，
  且可按模型调低（低对比度自定义模型）。语义通道改为两遍扫描（先收集分布再准入）。
- **验证**：六场景实测数据（两种模型 × 相关/同域闲聊/跨域无关）全部按期望准入或拒绝；
  qwen3 端到端行为不变（用户原话零注入、相关命中、跨域零注入）。
## 补充 2：min_gap 自救旋钮 + 语义诊断日志 + GUI「日志」页（同日追加）

- **min_gap**：`config.Retrieve` 新增 `min_gap`（默认 0.25）——SemanticFloor 的
  头部显著性阈值可配：低对比度自定义 embedding 模型调低放宽、≤0 关闭 gap 判定
  （仅绝对下限，回到模型相关语义）。四模型压测（+Qwen3-Embedding-8B、
  bge-large-zh-v1.5，硅基流动）12 场景全部按期望准入/拒绝。
- **语义诊断**：`index.QueryEx` 返回 `QueryInfo`（样本数/max/median/relGap）；
  语义通道参与但全部候选被拒时，hook 记 `prompt semantic` 日志、`ok search` 打
  stderr（附 min_gap 调节指引），不再静默。
- **GUI 日志页**：管理/引导/其他之后新增「日志」标签——`GET /api/logs` 回读
  ok.log/daemon.log/embed-sidecar.log 尾部（tail 参数、每文件 ≤256KB），行带 src
  与 semantic 标记；前端 2 秒轮询（仅标签激活）、来源 chips 多选（ok/守护/sidecar）
  +「仅语义」开关 + 文本过滤输入框、上滚暂停贴底；只读。


