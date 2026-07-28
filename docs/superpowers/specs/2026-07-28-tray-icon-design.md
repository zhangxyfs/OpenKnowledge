# 系统托盘（daemon 内嵌）设计

日期：2026-07-28　状态：已批准（用户确认方案 A + 菜单式弹窗，参照 ZeroTier 托盘样式）

## 背景与目标

daemon 常驻后台但无任何可见入口，用户无法感知其状态，也无法方便地打开 GUI 或停止服务。目标：

- daemon 运行时，右下角系统托盘显示 OpenKnowledge 图标
- **单击**图标：弹出菜单（TrackPopupMenu，ZeroTier 风格）——灰化版本号项 + 分隔线 + "退出"
- **双击**图标：打开 GUI；GUI 窗口有且只有一个——已打开则聚焦既有窗口，不新开
- "退出"：停止 daemon 整体（托盘消失、服务停止）；后续 hook 触发或登录自启会按需拉起（现有机制，无需新增）

## 方案

托盘内嵌 daemon 进程（方案 A）。daemon 是 `-H windowsgui` 无窗口常驻进程，天然适合托管 Win32 消息循环；不引入第二个进程，现有单实例、exe 指纹换版、孤儿自检逻辑零改动。弹窗用系统菜单 `TrackPopupMenu`（方案 A2），不做自绘窗口。

## 组件

### `internal/tray` 新包

```
tray_windows.go   // Win32 实现
tray_other.go     // 空 stub（非 Windows 编译兼容）
```

接口：

```go
// Run 创建托盘图标并跑消息循环，阻塞直至 ctx 取消或 onQuit 被调用方触发退出。
// version 用于 tooltip 与菜单版本项；openGUI 由 daemon 注入（打开/聚焦 GUI）。
// onQuit 由菜单"退出"项触发，daemon 注入关停逻辑（与 /api/shutdown 同路径）。
func Run(ctx context.Context, version string, openGUI func(), onQuit func())
```

daemon.Run 内以 goroutine 调用，所有 panic 由 tray 内部 defer/recover 隔离——托盘崩溃不影响 daemon 服务（fail-open 铁律）。

### 图标与菜单

- 图标：`LoadIcon` 读取 exe 自身模块的 RT_GROUP_ICON（logo 已内嵌），不新增资源文件
- tooltip：`OpenKnowledge v<version>`（版本来自 `internal/version`）
- 菜单（单击 `WM_LBUTTONUP` 时 `TrackPopupMenu`，`TPM_RETURNCMD | TPM_NONOTIFY | TPM_RIGHTBUTTON`）：
  1. `OpenKnowledge v<version>`（`MF_GRAYED`，不可点）
  2. 分隔线
  3. `退出` → 调 onQuit
- 菜单弹出前先 `SetForegroundWindow` 自身消息窗口，保证点击他处菜单正常消失（Win32 标准做法）

### GUI 单窗口

- `gui.OpenBrowser` 增加返回值：新窗口 hwnd（快照-diff 逻辑已在 `maximizeWindowByTitle` 内，顺手返回；未等到新窗口返回 0）
- tray 记录最近一次打开的 hwnd；双击（`WM_LBUTTONDBLCLK`）时：
  - `IsWindow(hwnd)` 为真 → `ShowWindow(SW_RESTORE)` + `SetForegroundWindow`（前台锁被拒时 `AttachThreadInput` 附加到前台线程再设置，兜底 `BringWindowToTop`）
  - 否则 → 调 openGUI（即 daemon 现有 OpenBrowser 链路）并更新记录
- token 含在 URL 里，同一 daemon 生命周期内 URL 稳定，聚焦复用无鉴权问题

### 生命周期与清理

- daemon 任何退出路径（/api/shutdown、信号、孤儿自检、菜单退出）→ ctx 取消 → tray 消息循环退出 → `Shell_NotifyIconW(NIM_DELETE)` 删除图标
- 托盘随 daemon 消失后，下次任意 ok 调用拉起新 daemon 时图标自动回来（语义同现有 daemon 拉起机制）

## 错误处理

- 所有 Win32 调用失败仅记 stderr 日志，不影响 daemon 主服务
- tray goroutine 顶层 defer/recover；消息循环内每次回调也 recover，避免单次消息处理异常杀死循环

## 测试

- 纯逻辑可单测：菜单项构造（版本文案、项序）、hwnd 复用判定（IsWindow 结果分支）——以注入函数变量的方式测试
- Win32 集成部分实测验收：
  1. `ok daemon` 启动 → 托盘出现图标，tooltip 版本正确
  2. 单击 → 菜单弹出，版本项灰化不可点，点击他处菜单消失
  3. 双击 → 打开 GUI 并最大化；再双击 → 聚焦同一窗口不新开
  4. 菜单"退出" → daemon 停止、图标消失；`ok gui` 后图标随新 daemon 回来
  5. 登录自启（HKCU Run）场景图标正常出现

## 明确不做（YAGNI）

- 菜单里不加"打开 GUI"项（双击已覆盖）
- 不做自绘弹窗/气泡通知
- 不做托盘右键菜单与单击菜单的区分（单击即菜单）
- 非 Windows 平台不实现托盘
