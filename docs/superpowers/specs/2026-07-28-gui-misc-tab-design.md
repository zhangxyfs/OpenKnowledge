# GUI "其他" Tab 设计：数据导出/导入 + 版本显示 + 最大化修复

日期：2026-07-28
状态：已批准（头脑风暴三节设计逐节确认）
关联：`docs/ARCHITECTURE.md`、`docs/superpowers/specs/2026-07-27-daemon-design.md`

## 1. 背景与目标

三件事：

1. **新功能**：GUI 增加第三个 tab"其他"，内含数据导出/导入与版本号显示。
2. **回归修复**：`ok gui` 打开的页面不再自动最大化。根因已定位：daemon 改造（`d36feab`）后 `ok gui` 开浏览器即退，而 `OpenBrowser` 里 `go maximizeWindowByTitle(...)` 是协程——进程退出协程被杀，最大化永远不执行。实证：窗口标题匹配（"OpenKnowledge 知识库"）、非最大化状态（IsZoomed=False）、手动 ShowWindow 可从用户会话最大化。
3. **版本号落地**：当前版本只存在于 `installer/openknowledge.iss`，程序自身不知道自己的版本。

已确认的需求决策：

- 导出粒度：全部项目 / 按项目，界面可选。
- 导入冲突：同名条目覆盖（`ok add --force` 语义）。
- 方案：daemon HTTP API + 前端 tab 一体，不加 CLI 子命令，不导固定目录。

## 2. 后端

### 新包 `internal/backup`（叶子包，依赖 registry/store/entry + stdlib archive/zip）

- `Export(w io.Writer, project string) error`：zip 写入 `registry.toml`、`projects/<名>/knowledge/*.md`、`projects/<名>/config.toml`；`project` 为 `"all"` 全导，否则只导该项目。zip 内路径正斜杠，与磁盘布局一致。不含 kb.db/向量/state（导入后 Sync 重建）。
- `Import(r io.ReaderAt, size int64) (Report, error)`：上限 32MB。流程：解 zip → 逐项校验 → 注册缺失项目（用包内原路径）→ 条目同名覆盖写盘 → 逐项目 Sync → 返回 `Report{Imported, Skipped int, Projects []string}`。
- 校验规则：只接受 `registry.toml`、`projects/*/knowledge/*.md`、`projects/*/config.toml` 三类路径；任何含 `..`/绝对路径/盘符的条目名整包拒绝（zip-slip 防护）；`.md` 必须过 `entry.Parse`，失败计入 skipped 不中断。
- 纪律：导入只写 knowledge/ 与 registry，kb.db 由 Sync 重建。

### daemon/gui 端点（internal/gui/api.go，全部走现有 withAuth）

- `GET /api/export?project=<名|all>` → `Content-Disposition: attachment; filename=openknowledge-backup-*.zip` 流式下载。
- `POST /api/import`（multipart 字段 `file`）→ JSON 报告。
- `GET /api/status` 响应加 `"app_version": version.Version`。

### 版本号

新建 `internal/version/version.go`：`var Version = "dev"`。`scripts/build-dist.sh` 从 `openknowledge.iss` grep `AppVersion`，加 `-ldflags "-X openknowledge/internal/version.Version=x.y.z"`（保留现有 `-s -w -H windowsgui`）。单一事实源 = .iss；裸 `go build` 显示 `dev`。

### 最大化修复

`internal/gui/server.go` OpenBrowser 两处 `go maximizeWindowByTitle("OpenKnowledge", 10*time.Second)` 去掉 `go` 改同步——窗口出现并最大化后（通常 1~3s）`ok gui` 才退出。注释更新说明根因（`ok gui` 进程即退，协程随进程死亡）。

## 3. 前端"其他"tab

`web/index.html` + `web/app.js` + `web/style.css`，复用现有 tab 切换与卡片样式。三个区块：

**数据导出**：项目下拉框（`全部项目` + 各注册项目，数据来自 `/api/projects`）+ 导出按钮，点击 `window.location = /api/export?project=...&token=...` 原生下载。

**数据导入**：文件选择（accept `.zip`）+ 导入按钮，multipart POST `/api/import`。完成展示报告"导入 N 条，跳过 M 条（格式损坏），涉及项目：A、B"，提示同名已覆盖；失败显示中文原因。成功后刷新管理页数据。

**关于**：`OpenKnowledge v<app_version>`、知识库根目录路径、项目数/条目数（`/api/status` 现有数据）。

YAGNI：导入预览/diff、导出加密、定时备份、按条选择性导入，均不做。

## 4. 错误处理

- 导出：项目不存在 → 404；磁盘读失败 → 500，zip 流已开始后的错误记日志（下载截断，可接受）。
- 导入：非 zip/超 32MB/无有效条目 → 400 + 中文原因；单条 frontmatter 损坏计入 skipped；写盘/Sync 失败 → 500 不回滚（幂等可重导）；zip-slip 整包 400；registry 同名不同路径按 `AddProject` 现有行为报错进 500。
- 版本号无 ldflags 时显示 `dev`，不算错误。

## 5. 测试

全部 `t.TempDir()` + `OK_HOME` 隔离：

- `internal/backup`：导出→导入往返（条目逐字节一致、registry/config 保留）；按项目导出只含该项目；同名覆盖；损坏 .md 计 skipped；zip-slip 整包拒绝；超 32MB 拒绝；未注册项目自动注册。
- `internal/gui`：三端点鉴权（无 token 401）、export Content-Disposition、import multipart 报告 JSON、status 含 app_version。
- `internal/version`：默认值 `dev` 断言。
- 最大化：逻辑不动只动调用点，gui 包现有测试保持绿；OpenBrowser 效果靠手工验收。
- 前端无测试设施，手工验收：tab 切换、导出下载、导入报告、版本显示、窗口最大化。
