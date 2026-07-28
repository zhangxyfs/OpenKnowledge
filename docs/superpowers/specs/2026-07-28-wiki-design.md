# Wiki 功能设计：init 后项目 wiki 生成与增量更新

日期：2026-07-28
状态：已批准（头脑风暴六节设计逐节确认）
关联：`docs/ARCHITECTURE.md`、`docs/superpowers/specs/2026-07-27-daemon-design.md`

## 1. 背景与目标

`ok init` 目前只做注册与骨架，对"项目早已存在"的场景没有引导。目标：让知识库能为已存在的项目生成一份详细 wiki（架构 + 模块 + git 历史里程碑叙事），并在后续开发中增量更新。

已确认的方向决策：

- **技能驱动**：ok.exe 不引入生成式 LLM 调用（全库目前只有 embedding 接口）。扫描、理解、写作全部由 kimi 会话中的 `openknowledge-wiki` 技能驱动 AI 完成；CLI 只提供数据搬运（游标、commit 计数、目录生成）。
- **不塞进 `ok init`**：init 保持秒级、离线、无副作用；wiki 由独立命令/技能触发。
- 内容范围：架构 + 模块 + 历史叙事（git 历史只提炼重大决策/转折点，不逐 commit 消化）。
- wiki 条目**直接转正**（draft=false）并以 tag 标记为自动生成；**正常参与混合检索**，同时 INDEX.md 里固定注入一份 Wiki 目录。
- 增量更新触发：手动 `ok wiki` + hook 轻提示（每会话一次），daemon 不自动扫描。

## 2. 架构总览

四个组件：

1. **`internal/wiki`（新叶子包）**：游标读写、`git rev-list --count` 落后计数、Wiki 目录 Markdown 生成。只依赖 store/registry/stdlib + 外部 git 命令，可独立单测。
2. **CLI 接线**（`internal/cli` + `cmd/ok`）：新增 `ok wiki status` / `ok wiki mark` 子命令。
3. **INDEX.md 目录注入**：Sync 重建 INDEX.md 时追加"Wiki 目录"一节，复用现有基础注入通道（每会话首次注入 INDEX.md 全文），不动检索逻辑。
4. **`openknowledge-wiki` 技能**：全量扫描与增量更新的全部智能，条目经 `ok add` 写入，最后调 `ok wiki mark` 推进游标。

数据流：prompt hook 末尾内联调 `wiki status`，落后超阈值在注入文本末尾附一句提示（每会话一次）→ AI 或用户触发技能 → 技能写条目 → `ok wiki mark` → 提示消失。

## 3. CLI 子命令

`wiki` 进 usage 帮助（非隐藏命令）。

### `ok wiki status`

输出 JSON 一行（机器消费）：

```json
{"project":"OpenKnowledge","has_wiki":true,"last_commit":"4f876df...","behind":37,"stale":true,"threshold":20}
```

- 无游标：`has_wiki:false`，`behind` 为全历史 commit 数。
- 非 git 项目或 git 不可用：`has_wiki` 照报，`behind:-1`，不报错。
- 退出码恒 0（fail-open）。
- 性能要求：只做一次 `git rev-list --count`，不开 kb.db；`has_wiki` 看游标文件是否存在。

### `ok wiki mark [commit]`

- 写游标；省略 commit 时取 `git rev-parse HEAD`。
- 非 git 项目允许只写时间戳。
- 成功输出"已记录 wiki 游标 <短hash>"。

### 配置

全局/项目 config.toml 加 `[wiki] stale_commits = 20`（默认 20，0 = 关闭提示），并入现有 config 覆盖链。

## 4. 游标与 Wiki 目录

### 游标文件

`projects/<名>/state/wiki.json`（与 session state 同目录但不受 7 天 GC 影响——固定文件名，GC 只清 `session-*`）：

```json
{"last_commit":"4f876df123...","generated_at":"2026-07-27T23:40:00+08:00","entry_count":18}
```

`entry_count` 纯展示用，不参与逻辑。

### Wiki 目录生成

Sync 重建 INDEX.md 时末尾追加：

```markdown
## Wiki 目录

- [架构总览](architecture.md) — 单 daemon 承载 GUI/hook/CLI 的三层结构
- [daemon 进程模型](daemon-process.md) — 端口锁单实例、指纹切换、孤儿自检
```

- 数据源：kb.db `entries` 表，`tags LIKE '%wiki%'` 且 `draft=0`，按 title 排序；描述取 `summary`（无则留空）。
- **不加新 type、不动 frontmatter 结构**，类别区分全靠 tag。
- 无 wiki 条目时整节省略，INDEX.md 与现状逐字节一致（不破坏现有黄金断言）。
- 生成逻辑在 `internal/wiki` 导出，Sync 调用点只加几行。

## 5. hook 落后提示

在 prompt hook 注入流程末尾追加（daemon `/api/hook/prompt` 端点内）。

触发条件（全部满足）：

- 全局开关 on 且项目已注册；
- `wiki.stale_commits > 0`；
- `wiki status` 判定 `stale:true`（落后 ≥ 阈值，或从未生成且全历史 ≥ 阈值）；
- 本会话未提示过：session state 加 `WikiNudged bool`，与 `BaseInjected` 同生命周期，每会话最多一次。

提示文案（附加在注入文本最后，固定放行）：

- 从未生成：`[OpenKnowledge] 本项目还没有 wiki，建议用 openknowledge-wiki 技能生成项目 wiki（含架构、模块与演进历史）。`
- 落后：`[OpenKnowledge] wiki 已落后 37 个 commit，建议用 openknowledge-wiki 技能增量更新。`

性能：内联调用（非 fork），一次 `git rev-list --count` 毫秒级；git 失败静默跳过。

不做：不在 Stop hook 提示；不自动触发扫描。

## 6. 技能：openknowledge-wiki

### 分发

- 仓库新增 `skills/openknowledge-wiki/SKILL.md` 独立文件；setupx 用 `go:embed` 读入并注册进 `skillTemplates` 同一张表，`InstallSkills` 烘焙 `{{EXE}}` 写到 `~/.agents/skills/openknowledge-wiki/SKILL.md`。
- 安装器只打包 ok.exe，技能天然随安装分发，不动 .iss；卸载按 SkillsHome 清理，自动覆盖。
- 现有 5 个技能保持内联不动（最小 diff）。
- README "5 个技能"改 6 个，ARCHITECTURE 相应小节同步。

### 技能内容

触发词："生成项目 wiki""初始化 wiki""更新 wiki"。

全量流程：

1. `ok wiki status` 确认状态；
2. 盘点项目（目录树、README、go.mod/构建文件，跳过 vendor/node_modules 等）；
3. 按模块读关键代码；
4. `git log --oneline` 全量 + 里程碑识别（版本号变更、大重构 commit、目录结构突变）；
5. 按条目规范逐条 `ok add`；
6. `ok wiki mark`；
7. 向用户汇报条目清单。

增量流程：`wiki status` 拿游标 → `git log <cursor>..HEAD` + `git diff --stat` → 只消化该段变更 → 受影响旧条目 `ok add` 重写（同名覆盖）或新增 → `ok wiki mark`。

条目规范（写死在技能里）：

- `type: reference`；tags 必含 `wiki` + 主题 tag；`summary` 一句话必填（进目录用）；
- 一个主题一条目，正文 300~800 字；架构总览固定第一条；
- 直接转正（draft=false），提醒用户可在 GUI 修订。

护栏：不读 `.env`/密钥文件；超大仓库（>500 文件）先向用户报规模再分层扫描。

## 7. 错误处理

全部 fail-open，贴合现有 hook 纪律：

- 非 git 项目 / git 不存在：`status` 报 `behind:-1`，hook 不提示；`mark` 只写时间戳；技能退化掉 git 步骤。
- 游标文件损坏：当作无游标（`has_wiki:false`），不报错。
- 游标 commit 已不在历史（rebase/amend）：`rev-list` 失败 → `behind:-1`，不提示；下次 `mark` 自愈。
- wiki 增量更新重写旧条目靠 `ok add` 同名覆盖的现有行为。

## 8. 测试

全部 OK_HOME/git 隔离，不碰真实环境：

- `internal/wiki`：游标读写往返、损坏游标、临时 git 仓库构造 N 个 commit 验证 behind 计数、commit 失踪场景。
- INDEX.md 目录：有/无 wiki 条目两种黄金输出、draft 不出现、按 title 排序。
- hook 提示：达阈值提示一次、同会话第二次不提示、`stale_commits=0` 关闭、非 git 项目静默。
- CLI：`status` JSON 字段、`mark` 省略/显式 commit、usage 含 wiki。
- 技能 embed：`InstallSkills` 能写出、`{{EXE}}` 替换生效（照抄现有技能测试模式）。
- 端到端：init → 写两条 wiki 条目 → mark → 再造 commit → status 显示落后 → hook 提示出现一次。
