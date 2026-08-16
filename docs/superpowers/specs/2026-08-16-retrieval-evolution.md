# 检索策略进化设计：RRF 融合 / 时效信号 / 采纳反馈闭环 / 泛化门控

日期：2026-08-16
状态：草案（待确认）

## 1. 背景与动机

现行检索（`internal/index/query.go`）已是 BM25F + 向量余弦的混合检索，且踩坑后确立了
"准入按通道独立判定"的铁律。对标 Reasonix / CodePilot / openhanako 与
Mem0 / Zep / Letta 后，确认差距不在召回算法本身，而在四点（按 ROI 排序）：

1. **融合方式脆弱**：`score = α·kw/(kw+6) + β·cos` 依赖手工归一化，换 embedding
   模型后余弦分布漂移，α/β 平衡即失效。
2. **零时效信号**：两年前的过时 pitfall 与上周的 rule 同分竞争 top_n=2 的名额。
3. **无反馈闭环**：注入后模型读没读该条目，系统完全不知道——而这恰是 hook 型
   系统独有、三家第三方都不具备的信号源（PostTool 链路可见文件读取）。
4. **无门控**："继续"/"好的"这类泛化 prompt 也跑检索 + embedding，白烧 token
   与一次网络往返。

四条共用原则：**不动分通道准入语义**（既有坑不再踩）、**fail-open**（任何新环节
出错仅记 ok.log，不阻断注入）、**各自独立发布**（每个特性一个 minor，可单独回滚）。

## 2. 非目标

- 不引入时序知识图谱（Zep 路线，小库过度工程；如需"取代"语义另议 `superseded_by` 字段）
- 不在 hook 路径加 LLM rerank（延迟敏感；如需要只放 `ok search` CLI）
- 不删除/削弱向量语义通道（openhanako 删向量依赖 LLM 常驻摘要，与本库人在环模式不同）
- GUI 表单只给特性④门控做（用户最可能想临时关它、且短语表天然要维护）；
  其余三个特性的配置键（fusion/rrf_k/recency.*/feedback.*）v1 仅 config.toml，
  GUI 暴露列为跟进项

## 3. 特性①：RRF 融合

### 3.1 现状与问题

`queryAll` 中关键词通道分 `kw/(kw+6)`（kw=-bm25 rank）、语义通道分 cos，按
`α·kw归一 + β·cos` 加总排序。归一化常数 +6 与 α/β 是手工标定的分数尺度桥接，
embedding 模型更换（bge-m3 噪声基线 0.52 vs qwen3 0.26）后尺度假设即破坏。

### 3.2 方案

- **准入完全不变**：关键词通道仍按未乘 α 的归一 BM25 ≥ `MinScoreFloor` 准入；
  语义通道仍按 `SemanticFloor`（模型无关相对门槛）准入。`QueryInfo` 诊断保留。
- **融合改为 RRF**（Reciprocal Rank Fusion，Zep 同款，只看名次不看分数）：
  对已准入集合，`score(h) = Σ_channel 1/(rrf_k + rank_c)`，`rank_c` 为该通道内
  名次（从 1 起），`rrf_k` 默认 60。单通道准入的 hit 只有一项。
- 双通道同时准入的 hit 两项相加，自然排在单通道命中之前——免费获得
  "交叉验证优先"语义。
- 排序 tie-break 不变（总分降序、标题升序）；top_n 截断与分支过滤位置不变。
- `scoreFloor = 1e-6` 保护逻辑仅属 weighted 模式；RRF 下负余弦的条目根本不会
  通过语义准入，无需保护。

### 3.3 配置与回滚

```toml
[retrieve]
fusion = "rrf"      # rrf（默认）| weighted（旧行为回滚档）
rrf_k = 60
# alpha / beta 仅 weighted 模式生效；rrf 模式下配置了非默认值时记一行 ok.log 提示被忽略
```

`fusion` 缺省为 rrf，升级后行为变化写入 changelog；要旧行为显式 `fusion = "weighted"`。

### 3.4 改动点

- `internal/index/query.go`：queryAll 尾部融合段重构为"通道各自产出名次列表 → RRF 汇合"；
  weighted 路径保留原逻辑
- `internal/config/config.go`：Retrieve 增 `Fusion string`、`RrfK int`，Default 填充
- 受益面：`ok search`、GUI 搜索（`internal/gui/api.go`）、hook 注入共用 queryAll，自动生效

## 4. 特性②：时效信号

### 4.1 方案

- 信号源：`entries.mtime`（sync 已维护，无 schema 变更）。**编辑条目即刷新新鲜度**
  视为 feature：被持续维护的条目保持新鲜。
- 作用位置：融合分**之后**乘系数，**不参与准入**——陈旧不等于无关，陈旧条目仍可
  准入注入，只在近似同分时让位。
- 系数函数：age ≤ fresh_days → 1.0；age ≥ stale_days → floor（默认 0.85）；
  中间线性过渡。按条目类型分窗（天）：

  | type | fresh | stale | 理由 |
  |---|---|---|---|
  | pitfall | 90 | 365 | 坑随依赖版本老化最快 |
  | note | 60 | 180 | 笔记最短命 |
  | rule | 180 | 730 | 规约缓慢演化 |
  | reference | 180 | 730 | 文档类长寿 |

- RRF 分数尺度下相邻名次差 ≈ 1/(k+r)²（r=1 时 ≈ 2.7e-4），0.85 系数足以翻转
  相邻名次而不可能翻越大分差——正好是"温和决胜"语义。

### 4.2 配置

```toml
[retrieve.recency]
enabled = true
floor = 0.85
[retrieve.recency.windows]        # [fresh_days, stale_days]，全零 = 该类型不衰减
rule = [180, 730]
pitfall = [90, 365]
note = [60, 180]
reference = [180, 730]
```

### 4.3 改动点

- `internal/index/recency.go`（新）：`Factor(typ string, mtime, now int64, cfg) float64` 纯函数
- `queryAll` 排序前 `h.Score *= recency.Factor(...)`；Hit 已携带 Type，mtime 需加进两条 SELECT
- 观测：系数 <1 且因此改变注入集合时记 ok.log 一行（GUI 日志可按"时效"过滤）

## 5. 特性③：注入→采纳反馈闭环

### 5.1 数据流

```
prompt hook (InjectForPrompt)                post-tool hook (TrackTouched)
┌─────────────────────────────┐              ┌──────────────────────────────┐
│ 选定 hits                    │              │ 路径规范化                    │
│ ├─ 写 entry_events(injected) │              │ ├─ 项目内 → 原 TrackTouched   │
│ └─ session.InjectedKnowledge │              │ └─ 知识库目录内且 basename ∈  │
│    = 本轮注入 filenames ◄────┼──────────────┼── session.InjectedKnowledge   │
│ 开头先入账：                  │              │    → session.AdoptedKnowledge │
│ session.AdoptedKnowledge ────┼──→ entry_events(adopted)，清空挂账          │
└─────────────────────────────┘              └──────────────────────────────┘
```

### 5.2 关键决策

- **新表** `entry_events(filename TEXT, kind TEXT, ts INTEGER)`，索引 `(filename, ts)`；
  kind ∈ injected | adopted。`Open` 的 schema 常量含 `CREATE TABLE IF NOT EXISTS`，
  老库打开即自动迁移，无需 ALTER。Sync 时顺带 prune 60 天前事件。
- **采纳捕获不在 post-tool 开库**：post-tool 每工具调用都触发，当前只碰 session 状态
  文件，给它加 DB 依赖是新故障面 + 延迟。采纳先挂账在 session 状态，下一次
  InjectForPrompt（反正要开库）开头入账。会话就此结束则挂账丢失——统计性信号，
  可接受。
- **归因窗口 = 本会话**：只统计"读本会话注入过的条目"。知识库目录在
  `~/.openknowledge/` 下，现行 `relativize` 会判其"不在项目路径内"而跳过——
  故在 `TrackTouched` 中新增平行分支：规范化路径位于 `pc.Store.KnowledgeDir()`
  且 basename ∈ `session.InjectedKnowledge` 才记 `AdoptedKnowledge`（去重）。
  mandatory 粘性指针的重读**不计入**（mandatory 不经检索，非本特性对象）。
- **生效规则（v1 只降不升）**：30 天窗口内 `injections ≥ min_injections（默认 4）`
  且 `adoptions == 0` → `score ×= demote（默认 0.8）`。不做加分：加分会自我强化
  造成条目固化，降权只修"持续噪声"这一种确定的问题。boost 列为 v2 候选。
- 查询侧开销：一条 `SELECT filename, kind, COUNT(*) FROM entry_events
  WHERE ts >= ? GROUP BY filename, kind`（30 天事件千级），组 map 后排序前应用。
- 与特性②的系数叠乘（0.85 × 0.8 = 0.68），不设额外下限——两信号同时命中说明
  该条目确实该让位。

### 5.3 配置

```toml
[retrieve.feedback]
enabled = true
window_days = 30
min_injections = 4
demote = 0.8
```

### 5.4 改动点

- `internal/index/db.go`：schema + `PruneEvents`；`internal/index/events.go`（新）：
  RecordEvents / FeedbackStats
- `internal/state/state.go`：Session 增 `InjectedKnowledge` / `AdoptedKnowledge`
- `internal/hook/core.go`：InjectForPrompt 入账 + 注入事件记录；TrackTouched 知识库分支
- 观测：降权命中时 ok.log 一行（"反馈"过滤标记）；fail-open：事件写失败仅 logErr

## 6. 特性④：泛化 prompt 门控

### 6.1 方案

- 位置：`InjectForPrompt` 中 **EmbedQuery 之前**——门控命中连 embedding 调用都省，
  每轮 prompt 省一次网络往返（embedding timeout 5s 的场景收益更明显）。
- 规则（宁窄勿宽，只拦高置信泛化；误拦的代价 > 误放的代价）：
  1. 规范化（trim / 去标点 / 小写 / 折叠空白）后**精确命中**内置短语表：
     继续、继续吧、好的、好、嗯、对、是的、行、可以、收到、谢谢、
     ok、okay、yes、no、thanks、continue、go、go on、next、done
     （约 20 条，多语言常用确认词）
  2. `retrieve.Terms(prompt)` 为空（纯标点/emoji/空白——此时 queryAll 本就返回
     空，门控只是省掉 embed 调用）
  3. **不设长度阈值**：两字的"构建"是合法查询，长度启发式误杀率高，不引入。
- 被门控时：跳过检索注入段（无"相关知识"段）；mandatory / INDEX / wiki nudge 等
  其余注入逻辑不受影响。记 ok.log 一行（"门控"过滤标记）。

### 6.2 配置

```toml
[retrieve.gate]
enabled = true
extra_phrases = []    # 用户自定义短语（内置表之外的追加层）
```

- **分层模型**：内置短语表编译进二进制（随版本演进），用户在 GUI 维护的是
  `extra_phrases` 追加层；两层取并集生效。内置表不物化进 config.toml——否则
  用户碰过列表后，新版本新增的内置短语就到不了老用户。
- 作用域与 retrieve 其他键一致：`LoadMerged` 全局 ← 项目 decode-over，
  项目文件缺键即继承。布尔三态沿用 `auto_born` 先例：文件层"缺省=继承"
  天然成立，API 层用 `*bool`（null=不变）。

### 6.3 GUI：引导页门控卡片 + 短语管理弹窗

**卡片**（引导页，完全镜像"经验沉淀卡片"模式）：

- 状态行：`门控：启用（内置 21 条 + 自定义 3 条，项目 X）` / 无项目时提示先 ok init
- 启用 checkbox：勾选变更即保存（同 auto-born 的 change-即存交互）
- "管理短语表"按钮 → 打开弹窗

**弹窗**（`gate-modal`，复用既有 modal 的 hidden-class 显隐模式）：

- 表格三列：`短语 | 来源 | 操作`
  - 内置行：来源=内置，只读（无操作）——v1 不支持逐条停用内置词（跟进项）
  - 自定义行：来源=自定义，操作=编辑 / 删除
- 顶部输入框 + "添加"按钮；编辑为行内输入框，Enter 保存 / Esc 取消
- 保存语义：**前端提交整个 extra 列表全量替换**（幂等，简单），服务端校验后落盘
- 服务端校验：逐条 trim + 折叠连续空白；按规范化形去重（与内置重复的直接丢弃）；
  单条 ≤ 64 字符、总数 ≤ 200 条（防止 config 被刷爆）；非法即 400

**API**（镜像 capture 的 get/set 对）：

```
GET  /api/gate?project=X
  → { "enabled": true, "builtin": ["继续","好的",...], "extra": ["..."] }
POST /api/gate   body: { "project": "X", "enabled": <bool|null>, "extra": <[]string|null> }
  → enabled / extra 任一为 null 表示该字段不变；成功返回同 GET 的最新视图
```

**落盘**：新增 `config.SetGate`（或 setupx helper），按既有小节重写算法
（SetCapture / setProvenanceAutoBorn 同款：存在则整段替换、不存在则文件尾追加、
其余内容含注释原样保留）写 `[retrieve.gate]` 子表。实现注意：既有 helper 处理的是
顶级小节，子表小节的匹配串是 `[retrieve.gate]`，边界判定（下一个 `[` 开头行）
逻辑可原样复用。

### 6.4 改动点

- `internal/retrieve/gate.go`（新）：`Gated(prompt string, extra []string) bool` 纯函数
  + 内置短语表常量
- `internal/hook/core.go`：InjectForPrompt 前置判定（EmbedQuery 之前）
- `internal/config/config.go`：Retrieve 增 Gate 子结构；`config.SetGate` 小节重写
- `internal/gui/api.go`：apiGateGet / apiGateSet + 路由注册
- `dist/web/`：引导页卡片（index.html）+ gate-modal + app.js 交互（三文件同步改）

## 7. 测试与标定

- 单测：RRF 名次构造（双通道交叉项排前）、recency.Factor 边界（fresh/stale 两端 +
  线性中点）、反馈降权（注入 4 次 0 采纳触发 / 有采纳不触发 / 窗口外不计）、
  门控短语与 Terms 为空两分支
- hook 集成测试：模拟 post-tool 读知识库文件 → 下一轮 prompt 后 entry_events 有
  adopted 行；知识库目录外路径不记分支回归（既有 TrackTouched 行为不变）
- GUI API 测试（沿用 api_test.go 模式）：GET 返回内置+extra 并集视图；
  POST 全量替换 extra 的校验（去重/超长/超量/与内置冲突）；enabled 的 null=不变
- 标定：复用 SemanticFloor 标定的 12 场景 × BGE/Qwen 四模型 bench
  （`.superpowers/sdd/bench`），对比 weighted vs RRF 的注入集合差异，确认无回归；
  准入逻辑未动，预期差异仅在排序
- 配置合并测试：新增四个配置块的缺省填充与项目级覆盖（沿用 config_test 模式）

## 8. 发布计划

建议顺序（每个特性一个 minor，各自 changelog，可独立回滚）：

| 版本 | 特性 | 理由 |
|---|---|---|
| v2.16.0 | ④ 门控（含引导页卡片 + 短语管理弹窗） | 检索侧最小、纯收益（省 embed 调用）；GUI 部分复用 capture 卡片/modal 全套既有模式，风险可控 |
| v2.17.0 | ① RRF | 核心排序变更，weighted 回滚档护航 |
| v2.18.0 | ② 时效 | 叠在 RRF 之上，纯查询侧 |
| v2.19.0 | ③ 反馈闭环 | 最大（schema + 跨 hook 链路），最后做 |

每版例行：`docs/changelogs` + `dist/changelogs` 双写、`scripts/sync-version.sh`
同步徽标、pre-push 钩子兜底（既有坑，见知识库发布条目）。

## 9. 风险与开放问题

- RRF 改变既有排序行为：bench 若发现某场景明显退化，回滚档 + min_gap/rrf_k 调参
  兜底；严重则该版本不发。
- mtime 语义：批量重格式化/迁移条目会整体刷新新鲜度——可接受，文档注明。
- 采纳归因只认"本会话注入过"，模型凭记忆（非注入）主动读条目不统计——v1 明确
  限制，避免噪声入账。
- 门控短语表的多语言维护：先中英常用确认词，用户在 GUI 弹窗里自助补充；不追求
  完备（宁窄勿宽）。内置词逐条停用（disabled_builtin 层）列为跟进项——先观察
  是否有内置词误伤的真实反馈再做。
- GUI 表单是否暴露其余新配置块（fusion/rrf_k/recency/feedback）：跟进项，视使用反馈再排期。
