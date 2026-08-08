# wiki 分支差异条目设计（二期）

- 日期：2026-08-08
- 状态：已批准（检索过滤、生成机制、GUI 布局逐项确认；skill 变更方式已对齐）
- 目标版本：v2.7.0
- 前置：`docs/superpowers/specs/2026-08-08-wiki-branch-awareness-design.md`（一期，已随 v2.6.0 交付）

## 1. 背景与范围

一期交付了分支感知**检测与提示**（游标按分支记录、五态检测、注入上下文行），但内容层不一致没有治本：wiki 条目仍是全项目共享一份，且存在两个一期遗留：

- **写侧无防护**：在非基准分支上跑 openknowledge-wiki 技能会用该分支结构重写共享全量条目（交叉污染）
- **热路径性能**：每次 prompt 的 git 子进程从 1 次升到 4-5 次（Windows 约 150-300ms）

二期主体是**分支差异条目**：长期并行分支只维护与基准分支的结构 delta，配合检索过滤与 GUI 呈现；两条遗留一并收口。

## 2. 决策记录

| 决策点 | 结论 |
|---|---|
| 其他分支差异条目命中时 | **按当前分支过滤**（注入视角硬过滤，不误导 agent） |
| 差异条目生成 | **技能提炼 + ok 供素材**（新命令 `ok wiki diff` 输出结构变化摘要；拒绝启发式自动生成） |
| 写侧防呆 | 内化进技能流程（非基准分支只写差异条目），不给 `ok add` 加分支强制 |
| GUI 管理页 | 默认显示全部 + 分支徽标 + 分支过滤器（人看视角 ≠ 注入视角） |
| 分支列位置 | 时间列之后、标题之前 |
| 表格布局 | 内容列区横向可滑；操作列 sticky 固定右侧 |
| skill 是否变 | **变**——SKILL.md 流程指引更新，随 ok.exe 内嵌分发，`ok setup` 覆盖更新 |

## 3. 数据模型

- 差异条目 = 普通 reference 条目：tags 含 `wiki` + `branch:<分支名>`；标题带后缀"（<分支> 分支差异）"；正文只记该分支与基准的结构差异（数百字级）
- 无 `branch:` 标签的条目 = 基准共享条目（含一期前全部存量），对所有分支可见
- 条目文件、frontmatter 六字段、kb.db 索引结构均不变——分支维度只落在 tags 约定上

## 4. 检索注入过滤（agent 视角）

- `InjectForPrompt` 在 `db.Query` 命中后过滤：条目 tags 含 `branch:X` 且 X ≠ 当前分支 → 丢弃。当前分支取一期已算的 `wiki.Status.Branch`（零额外 git 调用）
- **INDEX.md 双段**：Wiki 目录分"主目录 + 分支差异（<分支>）"小节；**基础注入裁剪**——读入 INDEX 后按当前分支只保留本分支的差异小节，其余分支小节不进注入（防止过滤被 INDEX 绕过）
- 过滤失效（非 git/分支未知/Branch==""）时**不过滤**：宁多勿漏，与一期"报疑"哲学一致
- 无差异条目的项目：注入与一期逐字节一致（零回归）

## 5. `ok wiki diff`（新命令，供素材）

输出当前分支相对 merge-base（与基准分支的分叉点）的结构变化摘要，供技能消化：

- 增删的目录/包（按顶层目录聚类）
- 增删文件清单（按扩展名聚类计数 + 关键文件列出）
- 变更量 Top-N 文件（diff --stat 摘要）
- 纯文本输出，可直接贴进技能上下文；非 git/无基准/无分叉时输出空摘要并说明（fail-open）

## 6. 技能流程改造（openknowledge-wiki SKILL.md）

第 0 步 status 判读增加分支维度：

| status | 技能行为 |
|---|---|
| 基准分支（branch == base_branch） | 现有全量/增量流程不变 |
| 非基准分支（no_cursor / diverged / ok 但非基准） | **差异流程**：`ok wiki diff` 取素材 → 只写/更新本分支差异条目（`add --force` 同名覆盖）→ `ok wiki mark` 记本分支游标；**不重写全量条目** |
| status 含 `merged_branches` | 提示用户确认后删除对应差异条目 |

分发：SKILL.md 内嵌 ok.exe（setupx 技能模板），随 2.7.0 发布，`ok setup` 覆盖写各 agent 技能目录。

## 7. "已并入"检测与清理

- 基准分支上 `CheckStatus` 扩展：对 cursors 表每条分支，`git merge-base --is-ancestor <branch> HEAD` 成立且 kb 中存在该分支的差异条目 → `Status.MergedBranches []string`；分支已删除的静默跳过
- `ok wiki status` 输出 `merged_branches`；wikiNudge 增加变体（每会话一次）："dev 已并入 master，其差异条目已失效，建议用 openknowledge-wiki 技能清理"
- 清理只经技能确认执行；ok 不自动删任何条目（写入收敛）

## 8. GUI 管理页分支支持

管理页是"人看 KB"视角，与注入过滤分开——默认显示全部：

- **分支列**：位于**时间列之后、标题之前**；渲染条目的 `branch:X` 标签值为徽标（如 `⎇ dev`）；无标签 = 共享条目留空
- **表格布局**：内容列区（时间→摘要）`overflow-x: auto` 横向可滑；**操作列 `position: sticky; right: 0` 固定**——表头与数据格同步 sticky + 不透明背景遮盖滑动内容，长分支名撑宽表格时操作按钮始终可见
- **分支过滤器**：顶部过滤条"项目、类型"旁加分支下拉；选项 = 当前项目条目的 `branch:` 标签集合（纯前端从 `/api/entries` 的 tags 聚合，**API 零改动**）+ "全部"；切换项目时重新聚合（联动）
- **过滤语义**：选中 `dev` → 共享条目 + `branch:dev` 条目（与注入视角一致）；默认"全部"不过滤

## 9. 性能收敛（一期遗留收口）

`CheckStatus` 的 ok 路径先跑 `countCommits(<cursor>..HEAD)`：成功即隐含"commit 存在且可达"，省掉 `rev-parse --verify` 与 `merge-base --is-ancestor` 两次 spawn；失败再细分 gone/diverged。热路径从 4-5 次 git 调用降回 2 次（symbolic-ref + rev-list）。状态机五态语义不变。

## 10. 错误处理与铁律

- fail-open：git 失败/非 git/状态损坏 → 不过滤、不提示、注入主流程不受影响
- 写入收敛：已并入检测只读；差异条目删除必须用户在技能流程中确认；INDEX 由同步派生（不手改）
- 并发：检索过滤只读；INDEX 重写沿用现有 index 同步路径

## 11. 测试策略

- index/hook：tags 过滤三类（当前分支/其他分支/无标签）、INDEX 注入裁剪（多分支小节只留当前）、过滤失效回退、零差异项目零回归
- wiki 包：`wiki diff` 输出（临时仓库构造分叉）、merged_branches 检测（真实 merge / 分支已删跳过）、CheckStatus 收敛后五态语义不变
- cli：`ok wiki diff` 非 git/无分叉/正常三态
- gui：管理页条目带分支徽标（前端渲染单测无框架——以 api_test 验证 entries 带 tags 为前提，前端逻辑手动验收）
- e2e 手动验收：见 §13

## 12. 明确不做（Out of Scope）

- 差异条目的启发式自动生成、自动删除；基准分支迁移的差异关系反转
- `ok add` 的分支强制约束（防呆只到技能流程层）
- 远程分支同步状态；多 worktree 并发
- 注入视角的分支过滤做成可配置开关（过滤恒开；若未来有需求再加配置）

## 13. 验收标准

1. dev 分支有差异条目时：dev 上注入命中它，master 上注入与检索完全不含它（含 INDEX 小节）
2. 无差异条目项目：注入与一期逐字节一致
3. 非基准分支跑 openknowledge-wiki 技能：只写差异条目，全量条目零改动；mark 记本分支游标
4. dev 合入 master 后：status 报 `merged_branches:["dev"]`，注入 nudge 提示清理；确认后条目删除、提示消失
5. GUI 管理页：分支列在时间后；操作列横向滑动时固定可见；分支过滤器随项目联动
6. 热路径 git 调用 ≤2 次（ok 路径）；全量测试绿
