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

## 第二轮排查（v2.18.1 修复后补充，2026-08-17）

覆盖上轮未细读区域：wiki 包、cli/setup、embed、setupx、embedsidecar、rxext、agentx 公共层。
全量测试仍绿。新发现按严重度排序：

### 11. saveGlobalConfig 整档重编码丢注释/未知键 + 并发写无锁 + 非原子写（中）

- 位置：`internal/setupx/setupx.go:88-101`（`saveGlobalConfig`）
- 三层问题：
  a) `LoadMerged("", globalPath)` 读入的是 Default+global 合并结果，`toml.NewEncoder`
     整档重写——用户写在全局 config.toml 里的**注释全部丢失**，未知键（手写/未来版本
     的高级键）被**静默删除**。同文件里 SetCapture/SetGate/setProvenanceAutoBorn 都是
     注释保留的小节重写，embedding/hooks-timeout/reasonix 的 Save* 却走整档重编码。
  b) 读-改-写无跨请求互斥：GUI 并发两个 Save*（如保存 profile 的同时切换 active）
     后写者覆盖先写者。registry/state 的同类竞态 a86bb27 已用 fsx.WithFileLock 修过，
     全局 config 漏网。
  c) `os.WriteFile` 非原子——正是 fsx.WriteFile 注释点名"曾被这样写坏"的模式，
     崩溃留半截文件会让 LoadMerged 失败、GUI/CLI 报错。
- 修法建议：改 fsx.WriteFile 原子写；要么包一层文件锁（与 registry.Update 同款），
  要么按小节重写保留注释（工作量更大）。

### 12. timeout_sec=0 时 embedding 调用无任何超时（低中）

- 位置：`internal/embedx/embedx.go:24`、`internal/embed/embed.go:81-85`
- Default 是 5s，但用户显式写 `timeout_sec = 0` 后 `OpenAIClient.Timeout=0` →
  `embedBatch` 不设 deadline。`ClientForIndex` 有 `sec<120 → 120` 钳制（索引路径安全），
  但 `embedx.Client`（hook 检索 / `ok search` / `ok doctor` 的 ping）路径会无限挂起，
  直到 TCP 自身放弃；`ok doctor`/`ok search` 无宿主超时兜底，可长时间卡死。
- 修法建议：`embedx.Client` 对 `TimeoutSec<=0` 钳回默认 5s（fail-open 与其余默认值一致）。

### 13. sidecar 跨进程 kill 无身份校验（低）

- 位置：`internal/embedsidecar/manager.go:177-182`（`stopLocked` else 分支）
- daemon 重启后 `m.cmd` 为空，凭状态文件 PID 杀进程；若 sidecar 已死且 Windows
  复用了该 PID，会误杀无关进程。杀前既不查 `Healthy()` 也不校验进程映像名。
- 修法建议：kill 前先 `st.Healthy()`（不健康才杀），或校验进程可执行文件名。

### 14. wiki 旧格式游标指向已改写 commit 时报告含糊（低）

- 位置：`internal/wiki/wiki.go:139-151`
- `branch != "" && !commitExists(srcDir, lc)` 落进"git 不可判"分支：HasWiki=true、
  Behind=-1、无 BranchState——不像新格式路径那样报 "gone"，nudge 永不触发。
  仅影响 legacy wiki.json + 历史改写场景。
- 修法建议：该分支内再判 `!commitExists` → 报 `legacy_orphan`。

### 15. 零散记录（可不修）

- `ok setup` 交互输 API key 终端回显明文（`cli/setup.go:148-150`）；常规做法
  golang.org/x/term.ReadPassword，CLI 工具常见妥协。
- rxext `onInput` 全程持 mu（含 embed 网络调用与 SQLite 写），tool.after 排队其后
  （`rxext/serve.go:61`）——注释声明的设计取舍，embed 慢时会拖迟触摸记录。
- `DiffSummary` 的 rename（R 状态）不计入目录增删统计——展示层选择。
- 上轮 nit 未变：`HasWikiMatch` SQL LIKE 对 ASCII 大小写不敏感，与 Go 侧精确匹配
  不一致（仅影响 ok search 提示行，非回归）。

### 第二轮排除的疑点

- agentx 各 XxxHome 用 `os.UserHomeDir()` 跟随 HOME 重定向——`codex.go:19` 注释明确
  有意（与宿主自身解析一致）；`registry.Home` 免疫重定向是数据根的另一套要求，设计正确。
- `Manager.Reconcile` 无锁访问 lastDesired/failCount——单 janitor goroutine 独占，安全。
- `Ensure` 中 `Process.Wait` 与 `cmd.Wait` 并发——一方拿状态一方拿 error，无害。
- `embed.Download` 续传/416/sha256/原子改名链路正确。
- `enforce.EvalChangelog`、`config.SetCapture` 小节重写逻辑正确。

## 第三轮排查（全量补查，2026-08-17）

应"不得抽查、全查"要求，把前两轮未逐行过的剩余源码全部读完：
agentx 全部 10 个适配器（kimi/pi/opencode/claude/zcode/reasonix/deepharness/qoder/qoderide/codex，
含 codex 信任哈希公式与 TOML 行级手术）、rxext SDK（sdk.go/wire.go/types_ext.go 通读，
types_generated.go 为生成镜像扫描）、tray（Win32 消息循环）、gui 窗口/浏览器层、
embed manifest/download、embedsidecar spawn、setupx autostart、logx/procx/version/
console、web/app.js 全文 1522 行（含与 index.html 的元素 ID 交叉核验，无缺失）、
发布脚本全套（build.py/publish-release.py/sync-version.sh/build-dist/build-installer/
build-linux/verify-deb）。

**结论：无新增中高危问题。** 第二轮的 #11（saveGlobalConfig）与 #12（timeout_sec=0）
仍是当前最值得修的两处。本轮新记录如下（均为低危）：

### 16. agentx 插件/技能类小文件写路径非原子（低）

- 位置：`pi.go:80`、`opencode.go:94`、`deepharness.go:127`、`reasonix.go:172`（manifest）、
  `setupx/setupx.go:79`（SKILL.md）、`setupx/autostart_unix.go:22`
- 主配置文件（kimi/claude/zcode/qoder/codex 的 settings/hooks）都已走 fsx.WriteFile
  原子写，但 TS 插件、JS 插件、manifest、SKILL.md、desktop 文件用裸 os.WriteFile。
  中途崩溃留半截文件——影响低（下次 setup/自愈幂等重写覆盖），与 fsx 注释"曾被这样
  写坏"教训同类，可顺手统一。`.bak-openknowledge` 备份文件裸写无妨（不怕半截）。

### 17. UpsertHooksBlock 双标记块残留（低，防御性）

- 位置：`internal/agentx/kimi.go:138-139`
- 手工粘贴等原因导致文件中出现两个完整标记块时，第一个被原位替换，第二个连同其
  hooks 原样残留 → 同一 hook 重复执行（双注入/双计数）。现有去重逻辑只认标记块外
  的 legacy 表。防御性缺口，正常安装/自愈路径不会产生双块。

### 18. publish-release.py 对 GitHub 用了错误的分页参数（低，当前无影响）

- 位置：`scripts/publish-release.py:92`（`?limit=100`）
- GitHub 的分页参数是 `per_page`，`limit` 被忽略（默认 30 条/页）；Gitea 认 `limit`。
  本项目每 release 只传 3 个产物，无实际影响。顺手改为按 host 分叉即可。

### 19. GUI 无归档入口（功能缺口，非 bug）

- v2.18.0 的 ok archive 只有 CLI；GUI 条目行无"归档"操作，entrySummaryJSON 不含
  archived 字段（gui/api.go summaryOf），界面无法展示/操作归档状态。后端编辑路径
  已正确继承该标记（f9b0ad6），此处仅是功能待补。

### 20. 零散 nit（可不修）

- `sidecar.State.Healthy()`（embedsidecar/sidecar.go:59）只探端口不验身份：状态文件
  stale 且端口被无关本地服务占用时，builtinClient 会拿到假 BaseURL（与 #13 的 kill
  侧同类，读侧）。触发条件极窄。
- `scripts/build.py:73` 的嵌套条件表达式（`if Path(winres).exists() if not
  shutil.which(...) else True:`）功能正确但可读性差。
- `embed/download.go`：manifest Size 钉错时 written>m.Size 每轮全量重下（清单受控，
  理论场景）。
- setup 交互输 API key 终端回显（cli/setup.go:148）——见第二轮 #15，维持记录。

### 第三轮排除的疑点

- agentx 各适配器合并写策略（strip→append、备份、按内容识别归属、codex 信任哈希
  双向验证、Windows .cmd 包装解耦 exe 路径）——逐行核对无误，与知识库已沉淀的
  各适配器坑一一对应且有测试。
- rxext SDK（vendored）：握手屏障、有界队列、panic 恢复、content-ref SHA-256 校验
  链路完整；types_generated.go 是生成镜像（DO NOT EDIT），扫描无异常。
- tray Win32 消息循环（LockOSThread、WM_QUIT、NIM_DELETE 清理、菜单收起 WM_NULL
  惯例）正确。
- web/app.js：esc() 全覆盖无 XSS 面、搜索竞态有 seq 防护、心跳版本防抖、401 自动
  刷新防循环、删除项目三重确认；与 index.html 的元素 ID 交叉核验零缺失。
- 发布脚本：iss 单一事实源提取、winres 四段式、tar --mode 钉权限、ar 归档 2 字节
  对齐解析均正确（已知坑都有对应防御）。

## 覆盖声明

三轮合计：internal/ 与 cmd/ 下全部 86 个 Go 源文件（不含 _test.go，测试以运行结果
为准）、web/app.js 全文、web/index.html 结构核验、rxext SDK（generated 扫描）、
scripts/ 全部 7 个发布脚本。未逐行审阅的仅剩 _test.go 文件与 site/（官网静态页）、
installer/nfpm.yaml（配置）、web/index.html 的静态排版——均非运行时逻辑。
