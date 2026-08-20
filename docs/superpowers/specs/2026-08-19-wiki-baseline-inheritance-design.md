# wiki 基线读时继承设计（inherited 态）

- 日期：2026-08-19
- 状态：已批准（用户选定方案 A 读时回退 + 正常 stale 门控；2026-08-19 会话逐点确认）
- 目标版本：待定
- 前置：`2026-08-08-wiki-branch-awareness-design.md`（分期均已发布：一期分支感知 v2.6、二期分支差异条目 v2.7；另有 v2.8 合并谱系 + born 溯源）

## 1. 背景与问题

分支感知一期落地后，游标按分支存储（`state/wiki.json` 的 `cursors` 表），只在 `ok wiki mark`（openknowledge-wiki 技能）执行时归入**当时所在分支**（`internal/cli/cli.go:936`）。由此产生误报式体验：

- 从 master 切出 `feat/x` 后第一次提问，hook 注入提示「当前分支 feat/x 尚无基线，结构描述以 master 为准」（`internal/hook/hook.go:404` 的 `no_cursor` 分支）
- 但此时 master 游标 commit **必然是 HEAD 祖先**——wiki 基线内容对本分支在该时点依然为真，`no_cursor` 把「能证明为真」与「不能证明」混为一档，对新切分支过于保守
- 真正的损失不是那句警告（wiki 内容本来就照常注入），而是**落后追踪缺失**：新分支积累 N 个 commit 后无人提醒 wiki 该更新

经验/pitfall/mandatory 条目无分支维度（分支感知 spec §3），跨分支天然继承，不在本设计范围。分支敏感的只有 wiki 结构条目与游标。

## 2. 决策记录

| 决策点 | 结论 |
|---|---|
| 总体方案 | **A：读时回退**——`CheckStatus` 保持只读，本分支无游标时从游标表找可达游标作为「继承基线」，状态新增 `inherited` |
| stale 门控 | **走正常门控**：inherited 态 `Behind`/`Stale` 照常计算，超 `stale_commits` 阈值即提醒（提醒并入上下文行，见 §4）；接受反向合并导致 behind 虚高（分支感知 spec §7.3 已接受同类口径） |
| 落盘 | **不落盘**：继承只是检测态，`wiki.json` 零变更、零迁移 |
| 触发方式 | 否决 git hook（侵入用户仓库、IDE/worktree 路径漏网）；否决惰性落盘（打破 CheckStatus 只读铁律、污染游标表） |
| 展示层 | **变体 B 状态卡**（原型 `web/prototype-wiki-inherited.html`，2026-08-19 用户选定）：hook 一行带落后数、CLI 键值块、GUI 状态卡 + 动作入口；否决 A 内联徽标（信息不足）与 C 静默派（继承无存在感，违背「让落后可见」的初衷） |

## 3. 检测层（internal/wiki，保持只读）

`CheckStatus` 在 `s.Cursors[branch]` 缺失时的回退查找：

1. 先验**基准分支**游标 commit 是否为 HEAD 祖先（一次 `merge-base` 判定），命中即短路；
2. 不命中再遍历其余游标，取可达且 `rev-list` 距离最近的一条；
3. 找到 → `BranchState = "inherited"`，`LastCommit` 取继承的 commit，`Behind`/`Stale` 照 ok 路径逻辑计算；
4. 找不到（所有游标 commit 均不可达，如分支从游离历史切出）→ 维持现有 `no_cursor`。

`Status` 结构体需加一个字段：`InheritedFrom string`（继承来源分支名，展示层要用；空串 = 非继承态）。`HasWiki` 语义不变（`len(Cursors) > 0`，继承态下已为 true）。

**git 调用预算**：继承命中路径 = symbolic-ref + merge-base（基准短路）+ rev-list = 3 次，与 ok 路径持平；基准不可达才遍历其余游标，上限受游标表大小约束（常态 1-2 条）。

## 4. 注入行为（internal/hook，按变体 B 一行带数字）

- `wikiContextLine` 新增 `inherited` 分支：
  - 未超阈值：`[OpenKnowledge] wiki 基于 master@abc1234；当前分支 feat/x 继承该基线，结构描述以 master 为准。`
  - 超阈值（`Stale`）：合并为一行，带落后数——`[OpenKnowledge] wiki 基于 master@abc1234（当前分支 feat/x 继承，落后 N commit），结构描述以 master 为准，建议更新。`
- **去重**：inherited 且 Stale 时 `wikiNudge` 的 stale 分支跳过（提醒已并入上下文行，避免同语义两行）；未超阈值不涉及 nudge。`no_cursor` 路径行为不变。
- `no_cursor` 文案保留，仅服务「所有游标不可达」的真无基线场景。

## 5. CLI 与 GUI 展示（展示层定稿：变体 B 状态卡，原型 `web/prototype-wiki-inherited.html` 2026-08-19 用户选定）

- `ok wiki status`：`inherited` 用键值块单列继承来源——
  `基线: 继承自 master@127cc01（2026-08-12 生成，15 条目）`，落后行在超阈值时附「→ 建议在本分支运行 /openknowledge-wiki」。
- GUI 分支上下文从一行文字升级为**状态卡**（`apiProjectBranchInfo` + `renderBranchInfo`）：
  - 端点增加 `branch_state`、`behind`、`last_commit`、`generated_at`、`entry_count` 透传（调 `CheckStatus`；本地 daemon 页面加载频次低，git 调用可接受；失败 fail-open 不显示卡片，退回现状一行文字）。
  - 工具栏摘要：当前分支 + 超阈值时加 `wiki 落后 N` 徽标（`badge-inherit` 中性绿色，无基线/分叉维持 `branch-warn` 橙）。
  - 卡片内容：基准分支 / 当前分支 / 基线（继承自 <分支>@<short>）/ wiki 生成时间·条目数 / 落后 N commit（阈值 M）。
  - 动作入口两键：「在本分支更新 wiki」点击复制引导文案（提示在 agent 会话运行 `/openknowledge-wiki`，一期不在 GUI 直接触发生成）；「查看分支差异」跳转/提示 `ok wiki diff`。

## 6. 与分支差异条目（v2.7 已落地）的边界

`inherited` 不落盘，`Cursors[branch]` 依旧不存在：差异条目的 diff 起点（`ok wiki diff` → `DiffSummary(cwd, base)` 走 merge-base）本就不依赖本分支游标；用户在非基准分支跑 openknowledge-wiki 技能时，技能侧仍按「无本分支游标」对待——先生成本分支差异条目、再 `ok wiki mark` 落真游标，此后本分支脱离 inherited 进入 ok 路径。「继承的基线」与「本分支真生成过」始终可区分，差异条目既有语义不受影响。

## 7. 错误处理与铁律

- fail-open：git 不可用/命令失败/状态损坏 → 不回退、不提醒，行为同现状
- 只读：`CheckStatus` 绝不写盘的铁律不变；继承不产生任何落盘
- 写入收敛：游标写入仍只有 `ok wiki mark` 一个入口

## 8. 测试策略

- wiki 包：master 有游标 + 切新分支 → `inherited`、`InheritedFrom` 与 behind 正确；基准游标不可达、其他游标可达 → 取最近者；全部不可达 → `no_cursor`；基准短路不产生多余 git 调用
- hook 包：`inherited` 未超阈值/超阈值两档文案；inherited + Stale 时 wikiNudge 不重复提醒；每会话一次去重
- CLI：`ok wiki status` 的 inherited 键值块输出
- GUI：branch-info 端点新增字段透传；状态卡渲染与 CheckStatus 失败时的 fail-open 回退
- 回归：基准分支与 `ok`/`diverged`/`gone`/`legacy_orphan` 路径逐字节不变（现有测试全绿）

## 9. 明确不做（Out of Scope）

- 游标落盘继承（方案 B）、git hook 切分支自动 mark（方案 C）
- inherited 态的独立 stale 阈值（已决策走正常门控）
- 经验/mandatory/enforce 条目的分支维度（本无分支维度，天然继承）
- 多 worktree 并发、远程跟踪状态（沿分支感知 spec §12）

## 10. 验收标准

1. 从基准分支切出新分支首次提问：注入显示「继承该基线」而非「尚无基线」
2. 新分支提交数超 `stale_commits`：上下文行带「落后 N commit，建议更新」，且无第二行重复提醒
3. 分支从游离历史切出（无游标可达）：仍为「尚无基线」
4. 基准分支上注入与现状逐字节一致
5. `wiki.json` 在整个继承路径中零写入
6. GUI 状态卡正确展示继承来源与落后数；CheckStatus 失败时回退现状一行文字
7. 全量测试绿
