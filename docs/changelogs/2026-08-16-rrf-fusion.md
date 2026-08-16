# 2026-08-16 RRF 融合：按名次融合替代 α/β 手工加权（fusion 配置 + weighted 回滚档）

- **问题**：融合分 `score = α·kw/(kw+6) + β·cos` 依赖手工归一化——归一化常数 +6 与
  α/β 是分数尺度桥接，换 embedding 模型后余弦分布漂移（bge-m3 噪声基线 0.52 vs
  qwen3 0.26），α/β 平衡即失效。
- **修复**：
  - 融合改 RRF（Reciprocal Rank Fusion，Zep 同款）：对已准入集合
    `score = Σ_channel 1/(rrf_k + rank)`，只看名次不看分数，rrf_k 默认 60；
    双通道同时准入的 hit 两项相加，自然排在单通道命中之前（交叉验证优先）；
  - **准入完全不变**：关键词通道仍按未乘 α 的归一 BM25 ≥ MinScoreFloor 准入，
    语义通道仍按 SemanticFloor（模型无关相对门槛）准入；QueryInfo 诊断保留；
    tie-break（总分降序、标题升序）与 top_n/分支过滤位置不变；
  - 配置 `[retrieve] fusion = "rrf"（默认）| "weighted"（旧行为回滚档）`、
    `rrf_k = 60`；非法值按 rrf（fail-open）；scoreFloor=1e-6 保护仅属
    weighted（RRF 下负余弦本就不进语义名次表）；
  - alpha/beta 仅 weighted 生效：rrf 模式下配置了非默认值时 hook 记 ok.log、
    ok search 打 stderr 提示被忽略；ok search 分数输出改 %.4f（RRF 分值域
    ~0.016-0.033，%.2f 下无法区分）。
- **验证**（真实库双模式标定，`ok search` 新构建 dist/ok.exe，embedding 本地
  qwen3 sidecar 有效——5 查询均完成语义分布采样（40 样本）；本库当前
  SemanticFloor 对 5 个查询均未准入语义候选，故本次实测覆盖的是"关键词单通道 +
  语义诊断"场景，双通道交叉排序路径由单测覆盖）：
  1. "我已经验证了A 没啥问题，你也可以看看 deepseek apiKey"：两模式均注入同一
     1 条（混合检索准入必须按通道独立判定…），集合与排序一致；
  2. "多-Agent支持 十个适配器"：准入集合一致（多-Agent支持（agentx）/agentx
     新适配器…/架构总览 3 条）；排序 rrf 把"agentx 新适配器"排在"架构总览"前，
     weighted 相反（weighted 总分含 β·cos 加成、RRF 只按准入场次）——被提前的恰是
     更直接相关的适配器经验，无真实相关被挤出 top_n，属可接受次序变化；
  3. "帮我写一个 python 爬虫"：两模式均零注入，一致；
  4. "构建命令是什么"：两模式均同一 2 条（构建双路径漂移…/经验沉淀机制），
     集合与排序一致；
  5. "windows 权限位 chmod"：两模式均同一 2 条（Windows 宿主交叉构建 Linux
     包…/安装器与发布），集合与排序一致。
  结论：5/5 场景准入集合完全一致（符合"准入逻辑未动"预期），排序仅 Q2 一处互换
  且方向更合理——spec §9 回归闸门通过，无需调 rrf_k 或回滚。
- **测试**：`TestQueryRRFCrossValidation`（双通道交叉项排前）/
  `TestQueryRRFDefaultFusion`（零值按 rrf）/`TestQueryRRFSingleChannelOrder`
  （单通道与 weighted 同序）/`TestQueryWeightedNegativeCosFloor`（回滚档
  scoreFloor 保护）/`TestQueryRRFNegativeCos`（负余弦不进名次表）/
  `TestFusionConfigDefaultAndOverride`（配置四态）/
  `TestInjectRRFIgnoresAlphaBetaHint`（忽略提示）；既有准入/诊断/分支过滤
  测试在默认 rrf 下全绿；全仓 go test ./... 绿。
- **升级注意**：升级后默认排序行为变化（RRF）；要旧行为显式
  `fusion = "weighted"`。GUI 搜索为纯关键词单通道，次序不变仅分数值变化。
