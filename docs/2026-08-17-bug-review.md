# 全仓 bug 排查报告（2026-08-17）

排查范围：v2.18.0 发布后（57263a7）全量代码审查。构建、`go vet`、全量测试（30 个包）
均通过，以下问题均为测试覆盖不到的逻辑缺口。按严重度排序，每条附文件:行号与推演依据。

## 确认的 bug

### 1. GUI 编辑条目丢失 draft / archived 标记（高）

- 位置：`internal/gui/api.go:644-653`（`writeEntry`）；前端 `web/app.js:392-394`
- `entryRequest` 结构没有 Draft/Archived 字段，`writeEntry` 重建 `entry.Entry` 时两字段
  默认 false。前端每行条目（含草稿行）都渲染"编辑"按钮，走 PUT `/api/entry`。
- 后果：
  - 编辑一条**草稿**会静默转正——未经 approve/采纳流程直接参与检索与注入；
  - 编辑一条**已归档**条目会静默取消归档。
- 写路径里只有 `created` 做了从旧文件继承的处理（`api.go:637-643`），Draft/Archived
  漏了同样的继承。
- 修法建议：更新路径先读旧条目，继承 `Draft`/`Archived`（或 entryRequest 增加字段并由
  前端显式传递）。

### 2. 归档的 mandatory 条目仍被强制全文注入（高）

- 位置：`internal/index/query.go:360-362`（`Mandatory()`）
- SQL 条件 `WHERE mandatory = 1 AND draft = 0` 缺 `archived = 0`。
- 归档的设计语义（见 `rebuildIndex` 注释与 `ok archive` 帮助文本）：不进 INDEX 主列表、
  仍可检索。检索通道放行是有意的，但 mandatory 强制注入这条最强路径漏了：用户归档一条
  过时规则后，它从 INDEX 消失，却仍每会话全文注入——L2 周期重注入、L3 粘性指针也都
  带着它（`internal/hook/core.go:84,118,128`）。
- 无测试锁定该行为；`bd37b9c` 终审只修了 `WikiEntries` 的归档过滤，此处是同类遗漏。
- 修法建议：`Mandatory()` 加 `AND archived = 0`，并补测试。

### 3. GUI 与备份导入的 Sync 未透传 index.max_lines（中）

- 位置：`internal/gui/api.go:208`（`syncIndex`）、`internal/gui/api.go:837,846`
  （`syncApprove`）、`internal/backup/backup.go:246`（`Import`）
- commit 9c8ee57 只给 hook/cli 的调用点补了 `SyncOptions{MaxLines: ...}`，上述四处
  `db.Sync(...)` 均不带选项，一律按默认 50 行渲染。
- 后果：用户配置 `index.max_lines = 20` 时，GUI 里任何一次条目增删改/采纳、或导入备份，
  都会把 INDEX.md 按默认预算重渲染，静默覆盖配置。
- 修法建议：`syncIndex`/`syncApprove` 用 `config.LoadMerged` 取项目配置后透传；
  `backup.Import` 同样按项目取配置（或接受一个取配置的回调）。

### 4. 未变化条目补向量时解析失败会中止整个 Sync（中低）

- 位置：`internal/index/sync.go:170-175`
- changed 路径对损坏条目的口径是跳过并返回 `*CorruptEntriesError`（`sync.go:180-185`），
  但"未变化且缺向量"的补齐路径里 `readEntry` 失败直接 `return rollback(err)`，中止
  整个同步事务。
- 触发场景：条目普遍缺向量（模型切换后 `ClearVectors`、新配 embedding、≤2.13 历史库
  迁移），叠加某文件损坏但 mtime 未变（同秒内写坏——正是已知的秒级 mtime 粒度问题）。
- 影响：hook 路径会降级重试（client=nil 的 Sync）成功，注入不受影响；但 `ok index`
  （`cli.Index` 对非 corrupt 错误无降级重试）会持续失败，直到该文件再次被修改。
- 这与 `Sync` 注释"一个 YAML 笔误不能压制全部注入"的承诺矛盾。现有测试
  `TestSyncDoesNotReadUnchangedFiles` 未覆盖该路径（首次同步已写全向量，`hasVector`
  全为 true，不会走补齐分支）。
- 修法建议：补齐路径的解析失败改为跳过该条目（记入 skipped），与 changed 路径口径
  对齐，并补一条"缺向量 + 损坏未变化条目"的测试。

### 5. `ok add --force` 与 GUI 更新缺同秒 mtime 推进（低）

- 位置：`internal/cli/cli.go:183`（`Add --force` 覆盖已有文件）、`internal/gui/api.go:654`
  （`writeEntry`）
- Approve（CLI `cli.go:736-746` 与 GUI `api.go:887-897`）和 Archive（`cli.go:492-502`）
  都做了"写后 mtime 仍同秒则 `os.Chtimes` 推进一秒"的防护，这两个写点没有。
- 后果：同一秒内对同一文件连续两次写（如快速连续保存），第二次被 Sync 的秒级 mtime
  diff 判为未变化，索引与 INDEX 停留在旧内容，直到文件下次被修改。
- 即知识库已沉淀的 pitfall"Sync 的 mtime 秒级粒度"在新增写点上的遗漏。
- 修法建议：两处补同款 Chtimes 防护（可抽成 fsx 小工具函数消除四处重复）。

## 低危 / 记录在案

### 6. `ok doctor` 对空 Paths 项目会 panic

- 位置：`internal/cli/cli.go:578`（`p.Paths[0]` 无防护）
- registry.toml 可手工编辑；写了 `[[project]] name = "x"` 而无 paths 时 `ok doctor`
  直接 index out of range 崩溃。`guiBornTag`（`api.go:679`）、`apiProjectBranchInfo`
  （`api.go:1141`）都做了 `len(p.Paths) > 0` 判断，Doctor 漏了。
- 关联：`backup/backup.go:186` 导入 paths 为空的项目时会注册成 `Paths: [""]`，
  建议一并处理（空 paths 时跳过注册或要求包内有路径）。

### 7. Archive 用裸 os.WriteFile 写条目（非原子）

- 位置：`internal/cli/cli.go:493`
- 其他所有条目写点都走原子 `fsx.WriteFile`（tmp+fsync+rename），此处裸写，中途崩溃/
  断电会留半截文件。Sync 会把损坏条目跳过（自愈），但期间 INDEX 可能短暂回退。

### 8. wiki 标签子串匹配误判

- 位置：`internal/index/sync.go:349`（`strings.Contains(r.tags, "wiki")`）、
  `internal/index/query.go:389,428,443`（`tags LIKE '%wiki%'`）
- tag 形如 `sewiki`/`nowiki` 会被当作 wiki 条目：不进主列表、进 Wiki 目录。各处口径
  一致，不算行为分叉，但语义上应按 tag 精确匹配（splitTags 后 == "wiki"）。

### 9. WikiCount 不过滤 archived，与 WikiEntries 口径不一致

- 位置：`internal/index/query.go:428`
- `WikiEntries` 已过滤 `archived = 0`，`WikiCount` 没过滤——`ok wiki mark` 显示的
  条数可能多于 Wiki 目录实际条数。

### 10. WithFileLock 释放路径 TOCTOU（窄窗口）

- 位置：`internal/fsx/lock.go:33-35`
- token 校验（ReadFile）与 Remove 之间若被抢占者精确交错（要求临界区超 2s 且时序极窄），
  可能误删新持有者的锁，短暂破坏互斥。注释已声明 fail-open 取舍，记录在案即可。

## 查证后排除的疑点（非 bug）

- `ok index` 无 embedding 时返回码 1 —— `internal/cli/cli_test.go:167,200` 明确断言，
  是设计（提示向量未重建）。
- 归档条目仍可被检索命中 —— `rebuildIndex` 注释明确"仍保留在库可检索"，设计如此。
- daemon 防双执行竞态 —— 端口即锁 + 15s 自省收敛（`daemon/run.go:81-91`），设计自洽。
- SQLite WAL/DSN —— pragma 走 DSN 每连接生效，`journal_mode(WAL)` 持久化，正确。
- config 三层合并（Default ← global ← project）——BurntSushi 只覆盖已定义键，
  数组合并逻辑正确。
- race 检测器未能运行（本机无 gcc，`-race` 依赖 CGO）；相关包人工核查无死锁路径。
