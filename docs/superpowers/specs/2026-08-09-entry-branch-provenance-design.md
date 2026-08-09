# 条目级分支溯源（provenance）设计

- 日期：2026-08-09
- 状态：已批准（born 记录方式/存量回填/谱系落盘/基准锚定逐项确认）
- 目标版本：v2.8.0
- 前置：分支感知（v2.6）、分支差异条目（v2.7）

## 1. 背景与问题

分支功能现状：**scope（适用范围）**维度已有（`branch:<名>` 标签，仅差异条目用，控制注入过滤），但 **provenance（来源谱系）** 维度缺失——条目模型不记录"这条知识诞生于哪个分支"，导致：

- GUI 管理页分支列在常见场景**整列空白**（只有非基准分支跑 wiki 产生的差异条目才有标签；基准分支生成的共享条目天然无标签）——用户无法知道一条信息属于哪个分支
- 分支合并后，"哪批知识是从 dev 带过来的"无据可查

用户诉求（已确认）：**每条知识体现出生分支（只展示不过滤）+ 合并谱系可表达**，与 scope 标签正交。

## 2. 决策记录

| 决策点 | 结论 |
|---|---|
| born 记录方式 | **写入时自动记录**（`ok add`/`ok propose` 落笔探测当前分支）+ **可配置关闭**（项目级 `[provenance] auto_born`，默认 true） |
| 出生时刻语义 | 以**创建时刻**为准；`ok approve` 草稿转正**不改写** born |
| 历史存量 | 显式一次性回填命令 `ok backfill-born`（预览+确认；只补无 born 的条目，不覆盖） |
| 合并谱系 | **落盘**：`wiki.json` 加 `merges: [{from, to, commit, time}]`，检出即追加去重 |
| born 存储 | **tags 约定**（`born:<分支>`）——零 DB 迁移，与 `branch:` scope 正交；frontmatter 六字段不动 |
| 基准分支判定 | 维持**当前分支锚定**（首次生成时所在分支），**不用 origin/HEAD**（猜测会锚错，如 main-v2 工作线）；`ok wiki mark` 输出明示基准；`ok wiki base` 无参列候选 |

维度正交性强调：`branch:<名>` = "在哪生效"（scope，注入过滤用）；`born:<名>` = "在哪出生"（provenance，展示用，不参与任何过滤/隔离）。

## 3. 数据模型

- **born 标签**：`tags` 含 `born:<分支名>`（首个即出生分支；无 = 未记录/历史未回填）
- **合并谱系**：`wiki.json` 增加可选字段：
  ```json
  "merges": [
    { "from": "dev", "to": "master", "commit": "<并入时 HEAD>", "time": "2026-08-09T…" }
  ]
  ```
  旧文件无此字段 → 零值空数组，无迁移负担
- **配置**：项目 config.toml 加 `[provenance] auto_born = true|false`（默认 true，键缺失即 true）

## 4. 记录时机与行为

| 动作 | 行为 |
|---|---|
| `ok add` / `ok propose` | `auto_born` 开启且当前目录是 git 仓库时，探测当前分支（复用 `wiki.CurrentBranch`）写入 `born:<分支>` 到 tags（用户显式传的 born 标签优先，不覆盖）；探测失败静默不标（fail-open，不阻断建条目） |
| `ok approve` | 不改写 born（出生以创建时刻为准） |
| `ok backfill-born`（新子命令，项目级） | 预览"将按当前分支 `<X>` 回填 N 条无 born 条目"，确认后写入；只补无 born 的条目，不覆盖已有值；非 git 项目报错退出 |
| `ok wiki status` / `ok wiki mark` | 执行时若 `MergedIntoBase` 非空，向 `merges` 追加记录（按 `from+commit` 判重）——谱系随检测自然积累；写失败仅记日志，不影响主输出 |

`ok wiki status` 落盘谱系的理由：status 是谱系检测的天然查询点，merges 属状态维护（非用户数据写），与"不自动删改条目"的写入收敛不冲突。

## 5. GUI 呈现（管理页）

### 5.1 分支上下文（工具条区）

工具条（项目/类型/分支过滤器行）显示：`基准分支: <base_branch> · 当前分支: <项目目录实际 checkout 分支>`。

- 基准分支：读 `wiki.json`；当前分支：daemon 经 `wiki.CurrentBranch(项目路径)` 探测（非 git 显示"—"）
- 随项目切换联动；`基准 ≠ 当前` 时当前分支加警示色（提示"wiki 内容基于另一条分支"）
- API：status 端点扩展或复用现有条目接口附带（实现时取最小 diff，倾向复用 /api/status 加 per-project 字段）

### 5.2 条目行双徽标（分支列）

- born 徽标：`⎇ <born>`（灰蓝色）——每条有 born 的条目都显示，含主分支（`⎇ master`）
- scope 徽标：`⇢ <branch>`（区别色）——仅带 `branch:` scope 标签的条目追加显示
- 无 born 无 scope：空白（历史未回填状态，如实呈现）

### 5.3 分支过滤器

选项 = 条目 born 值 ∪ scope 值聚合（随项目联动）；选中 X → 显示 `born==X` 或 `branch:X` 的条目 + 无 born 无 scope 的老条目；默认"全部"。

### 5.4 合并谱系行

`merges` 非空时工具条下显示一行小字："合并谱系：<from> → <to>（<最新一条日期>）"；点击/悬停查看全部记录（简化起见首版可只显示最新一条+总数）。

### 5.5 provenance 开关

管理页"经验沉淀"卡加一行 checkbox："自动记录出生分支"——读写项目 `[provenance] auto_born`，与该卡其他项目级配置同一保存路径。

## 6. 错误处理与铁律

- fail-open：分支探测失败 = 不写 born（条日照常创建）；merges 读写失败不影响 status/注入主流程；GUI 谱系数据缺失时静默不显示
- 写入收敛：born 只写在创建/显式回填两个口子；approve 不改写；merges 只追加不改历史；谱系清理（如有）必须用户确认
- 零 DB 迁移：tags 约定 + JSON 零值兼容；旧版 ok 读 born 当普通标签展示，不崩

## 7. 测试策略

- cli：add/propose 自动标 born（git 夹具各分支）、显式 born 不被覆盖、approve 不改写、backfill 预览/只补空/非 git 报错、status+mark 谱系追加去重、base 无参列候选
- gui api：分支上下文字段、谱系输出、provenance 开关读写
- 前端：双徽标渲染、过滤器聚合语义（手动验收）
- 回归：无 born 项目行为与现状一致；config 缺 `[provenance]` 节默认 true

## 8. 明确不做（Out of Scope）

- born 参与检索过滤/隔离（那是 scope 的职责，永不合流）
- 合并时自动改标 born（谱系只记录不改归属）
- origin/HEAD 推断基准分支
- 谱系的图形化展示/时间线视图
- provenance 的全局配置（只按项目）

## 9. 验收标准

1. git 项目中 `ok add` 新条目自动带 born 徽标（管理页可见）；`auto_born=false` 后不标
2. `ok backfill-born` 确认后存量条目补上 born，已有 born 的不变；分支列从全空变为有内容
3. dev 合入 master 后执行 `ok wiki status`：谱系行出现 "dev → master"；重复执行不重复记录
4. GUI 工具条显示 `基准分支: master · 当前分支: dev`，不一致时当前分支警示色
5. `ok wiki mark` 输出含基准分支；`ok wiki base` 无参列出候选分支
6. 全量测试绿；无 born/无 merges 的项目行为与 2.7.1 一致
