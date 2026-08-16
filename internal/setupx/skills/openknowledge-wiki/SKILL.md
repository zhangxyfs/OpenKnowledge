---
name: openknowledge-wiki
description: 为已存在的项目生成或增量更新 OpenKnowledge 项目 wiki——扫描代码结构与 git 历史，经 ok add 录入 reference 条目（tags 含 wiki），条目随混合检索注入，目录固定进 INDEX.md。当用户要求"生成项目 wiki""初始化 wiki""更新 wiki""把项目沉淀成 wiki"，或新功能/新模块/重要架构变化定稿需要沉淀进项目 wiki 时使用。
---

# openknowledge-wiki

把当前项目沉淀成 wiki：架构总览 + 模块条目 + 演进里程碑。全部条目用 Bash 调 `"{{EXE}}"` 写入，直接转正（非草稿），用户在 GUI 可随时修订。

## 第 0 步：状态检查

```bash
"{{EXE}}" wiki status
```

输出 JSON：`has_wiki`（是否生成过）、`last_commit`（游标）、`behind`（落后 commit 数，-1 = 非 git 项目）、`stale`、`threshold`，以及分支字段 `branch`（当前分支）、`base_branch`（基准分支）、`branch_state`（ok/no_cursor/diverged/gone/legacy_orphan）、`merged_branches`（已并入基准的分支，可选）。

**先判分支，再判新旧：**

- `merged_branches` 非空 → 告诉用户这些分支已并入基准、差异条目已失效，**经用户确认后**删除对应条目（并可用 `{{EXE}} wiki mark` 刷新游标）。然后继续下面的分支判断。
- `branch` 与 `base_branch` 均非空且**不相等** → 走【分支差异流程】（不得重写全量条目——那会污染基准分支视角）
- 否则（在基准分支上）：`has_wiki:false` → 走【全量流程】；`behind > 0` → 走【增量流程】；`behind = 0` → 告诉用户 wiki 已是最新，结束

## 分支差异流程

为非基准分支生成/更新**差异条目**——只记本分支与基准的结构差异，不重写全量条目：

1. 取素材：`"{{EXE}}" wiki diff`（分叉点以来的目录/文件/Top 变更摘要），必要时再 `git log --oneline <分叉点>..HEAD` 补充
2. 把差异消化成条目：标题 `原条目名（<分支> 分支差异）`（无对应主条目时自定主题名），tags 为 `wiki,branch:<分支名>`，正文写"与基准分支的结构差异是什么/为什么"，300 字内
3. 同名已存在用 `add --force` 覆盖更新；不再适用的差异条目经用户确认后删除
4. `"{{EXE}}" wiki mark` 记本分支游标，汇报变更摘要

## 全量流程

1. **盘点**：读目录结构（跳过 `.git`、`node_modules`、`vendor`、`dist`、`target` 等产物目录）、README、构建文件（go.mod / package.json / pom.xml 等）。文件数 > 500 时先向用户报告规模，按顶层目录分层扫描，不要试图读完全部代码。
2. **读关键代码**：每个顶层模块读入口文件与核心类型，理解职责与模块间依赖。
3. **消化历史**（`behind:-1` 时跳过此步）：
   ```bash
   git log --oneline | head -200
   git log --reverse --oneline | head -20   # 项目起源
   ```
   识别里程碑：版本号变更、大重构、目录结构突变、重要功能引入。只提炼转折点，不逐 commit 叙述。
4. **写条目**（规范见下）：第一条固定是"架构总览"，然后每模块/每主题一条，最后一条"演进历程"记里程碑。
5. **推进游标**：
   ```bash
   "{{EXE}}" wiki mark
   ```
6. **汇报**：列出写入的条目清单（标题 + 一句话），告诉用户可在 GUI 修订。

## 增量流程

1. 取游标范围内的变更：
   ```bash
   git log --oneline <last_commit>..HEAD
   git diff --stat <last_commit>..HEAD
   ```
2. 只消化这段变更：受影响模块的旧条目用同名 `add --force` 重写覆盖；新主题新增条目；"演进历程"条目追加新里程碑后重写（同样带 `--force`）。
3. `"{{EXE}}" wiki mark` 推进游标，汇报变更摘要。

## 条目规范（每条都必须遵守）

- 正文先写入临时 .md 文件，再执行：
  ```bash
  "{{EXE}}" add --title <标题> --type reference --tags wiki,<主题tag> --summary <一句话> --file <正文.md>
  ```
- `type` 固定 `reference`；tags 第一个固定 `wiki`，第二个是主题（如 `架构`、`daemon`、`安装器`）。
- `summary` 必填一句话——它会出现在 INDEX.md 的 Wiki 目录里；写标题之外的检索线索，不要复读标题。
- 一个主题一条目，正文 300~800 字；标题即文件名（同名覆盖需 `--force`，增量流程命令已带）。
- 内容写"为什么"和"怎么协作"，不抄代码；关键文件用 `path:行号` 引用。

## 护栏

- 不读 `.env`、密钥、证书等敏感文件。
- 不用向用户追问项目名——`wiki status` 与 `add` 自动按当前目录路由项目。
- 全部失败可重入：重复执行本技能不会产生重复条目（同名覆盖需 `--force`，增量流程命令已带）。
