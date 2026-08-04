# 升级后 GUI 更新日志弹窗 —— 设计文档

日期：2026-08-04
状态：已确认（用户批准）

## 背景与目标

每次升级安装新版本后，用户再次打开 GUI 时应看到更新日志弹窗（修复了什么、更新了什么）。跳级升级（如 2.3.0 → 2.3.2）累计展示所有新版本日志；另提供常驻入口随时回看历史版本。

方案定案 **A：随安装包文件分发**——changelogs 像 `web/` 一样装到 `{app}/changelogs/`，内容仍只在 `docs/changelogs/` 一处维护。

## 内容分发

- `scripts/build-dist.sh`：构建时 `rm -rf dist/changelogs && cp -r docs/changelogs dist/changelogs`（在现有拷贝 web 的相邻位置）。
- `installer/openknowledge.iss`：新增 `Source: "..\dist\changelogs\*"; DestDir: "{app}\changelogs"; Flags: ignoreversion recursesubdirs`。

## 后端（`internal/gui/changelog.go` 新文件）

### 目录定位

`changelogDir(webDir string)`：优先 `filepath.Dir(webDir)/changelogs`（安装态 `{app}/changelogs`）；不存在则回退 `filepath.Dir(webDir)/docs/changelogs`（dev 仓库内运行）。两级都不存在 → 视为无 changelog（不报错）。

### 解析与比较

- 只认 `^\d+\.\d+\.\d+\.md$` 命名的文件（早期日期命名 changelog 不参与）。
- 版本比较：major.minor.patch 数值三元组；非规范版本号（如 `dev`）不参与比较。
- 读出的每个条目：`{version, log}`（log 为 md 文件全文）。

### 状态

`~/.openknowledge/gui.json`（`registry.Home()` 下，0600）：

```json
{"last_seen_version": "2.3.1"}
```

缺失/损坏 → 视为无记录。

### API（注册进 daemon 的 mux，与既有 `api()` 包装一致）

- `GET /api/changelog` →
  ```json
  {"current": "2.3.2",
   "pending": [{"version": "2.3.1", "log": "..."}, {"version": "2.3.2", "log": "..."}],
   "all": [{"version": "2.2.3", "log": "..."}, {"version": "2.3.2", "log": "..."}]}
  ```
  - `pending` = 版本号严格大于 `last_seen_version` 的条目，升序；无记录 → 空（首次不弹历史）；`current == "dev"` → 恒空；降级安装（last_seen 更新）→ 空。
  - `all` = 全部版本含 log（总量 <100KB，一次带全）。
- `POST /api/changelog/seen` → 写 `last_seen_version = current`；`current == "dev"` 时不写文件直接返回 ok。

## 前端（`web/`）

- `index.html`：新增 modal overlay 容器（`#changelog-modal`，默认 hidden）；"其他"标签页新增"更新日志"卡片入口（按钮 + 简介）。
- `app.js`：
  - `refreshStatus` 完成后拉取 `/api/changelog`；`pending` 非空 → 显示 modal，按版本升序累计渲染，标题"新版本 vX.Y.Z 更新内容"（多版时"已更新到 vX.Y.Z"）。
  - 点击"知道了"→ `POST /api/changelog/seen` → 关闭 modal。
  - 常驻入口点击 → 同一 modal 渲染 `all`（标题"更新日志"），仅查看、不影响 seen 状态。
  - 极简 markdown 渲染（约 30 行，零依赖）：`#`/`##` 标题、`-` 列表项、`**粗体**`、`` `行内代码` ``；其余行按段落输出；一律先 HTML 转义再套格式，防注入。
- `style.css`：`.modal-overlay` + `.modal-card` 样式，沿用现有 CSS 变量与卡片圆角/配色。

## 错误处理

| 场景 | 行为 |
|------|------|
| changelogs 目录缺失/为空 | `pending`、`all` 皆空；前端不弹窗，常驻入口显示"暂无更新日志" |
| `gui.json` 缺失/损坏 | 视为无记录，`pending` 为空 |
| `current == "dev"` | `pending` 恒空；POST seen 不写文件返回 ok |
| 降级安装（last_seen 比 current 新） | `pending` 为空 |
| POST seen 写失败 | 500；前端 toast 报错但允许关闭（下次启动再弹，不丢提醒） |

## 测试（`internal/gui/changelog_test.go`，`t.Setenv("OK_HOME", t.TempDir())` 隔离）

- 目录解析：版本号排序（2.10.0 > 2.9.0）、非 `N.N.N.md` 文件被过滤、dev 回退路径（`web/` 兄弟目录无 changelogs 时读 `docs/changelogs`）。
- `GET /api/changelog`：无 gui.json → pending 空；last_seen=2.3.0 且有 2.3.1/2.3.2 → pending 两个升序；降级 → 空；构造 `current=dev` → 空。
- `POST /api/changelog/seen`：写入后再 GET，pending 为空；gui.json 内容与 0600 权限（权限非 Windows 跳过）。

## 手动验收

构建安装包安装后打开 GUI：应弹出 2.3.2 更新日志；手工把 `~/.openknowledge/gui.json` 的 `last_seen_version` 改成 2.3.0 再开 → 应累计弹出 2.3.1+2.3.2；"其他"标签页入口随时可看全部版本。

## 明确不做（YAGNI）

- 不做 markdown 完整渲染（不引第三方库）。
- 不做"不再提示"之外的偏好设置（无每版本粒度开关）。
- 不改早期日期命名 changelog 的归属（它们不映射到版本号，永久不参与）。
- 不做安装器内的"安装完成即显示更新日志"页（只在 GUI 打开时弹）。
