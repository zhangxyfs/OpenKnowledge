# v2.2 GUI 其他 Tab：数据导出/导入 + 版本显示 + 最大化修复

- **backup 包（`internal/backup`，新叶子包）**：`Export(w, project)` 把
  registry.toml + 各项目 knowledge/*.md + config.toml 打成 zip（`project="all"`
  全导，单项目时 registry 随之过滤）；`Import(r, size)` 解包回知识库——
  32MB 上限（`MaxSize`）、zip-slip 防护（`validName` 拒绝 `..`/绝对路径）、
  只接受 `registry.toml` / `projects/<名>/knowledge/*.md` / `projects/<名>/config.toml`
  三类路径（其余直接 400）、条目 .md 过 `entry.Parse` 失败计 skipped 不阻断、
  同名覆盖、缺失项目自动注册（同名已注册则合并）、最后逐项目 `index.Sync`
  重建索引。返回 `Report{imported, skipped, projects}`，客户端侧错误统一包
  `ErrBadPackage`。
- **导出/导入端点（withAuth，同其余 /api）**：`GET /api/export?project=<名|all>`
  流式返回 zip（Content-Disposition 带日期文件名，项目不存在 404）；
  `POST /api/import` 收 multipart `file` 字段（MaxBytesReader 兜底超限），
  成功返回 Report JSON，`ErrBadPackage` 映射 400。
- **应用版本号（`internal/version`）**：`var Version = "dev"`，`scripts/build-dist.sh`
  用 sed 从 `installer/openknowledge.iss` 的 `#define AppVersion` 提取版本，经
  `-ldflags -X openknowledge/internal/version.Version=` 注入——版本事实源只有
  .iss 一处；裸 `go build` 为 `dev`。`/api/status` 新增 `app_version` 与 `home`
  （KB 根目录）两个字段。
- **GUI 第三个标签页「其他」（`web/`）**：数据导出（项目下拉 + 全部，fetch+blob
  下载 zip）、数据导入（选 zip 上传，展示 imported/skipped/projects 结果）、
  关于卡片（显示 `v<app_version>`、KB 根目录 `home` 与项目数）。样式复用引导页卡片。
- **最大化修复（`internal/gui/server.go`）**：`OpenBrowser` 的
  `maximizeWindowByTitle` 由协程改为同步调用。回归根因：daemon 化后 `ok gui`
  开浏览器即退，最大化协程随进程死亡，窗口不再最大化。
