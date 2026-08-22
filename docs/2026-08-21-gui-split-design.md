# GUI 分离方案：ok / okd / OkManager 三进程架构

日期：2026-08-21 · 分支：`feat/gui-split` · 状态：已实施

UI 原型：`web/prototype-manager-v2.html`（功能面以此为准）

## 1. 背景与目标

现状：`cmd/ok` 单 exe 内嵌一切——CLI、hook 入口、daemon（`ok daemon` 子命令）、GUI 服务（`ok gui` 子命令）。GUI 与 CLI 耦合，daemon 寄生在 CLI 二进制里，没有独立的配置中心入口。

目标：拆成三个进程，各管一摊，单一写者：

- **ok.exe**（console 子系统）：CLI + hook 入口；hook 事件转发 okd（本地 fallback 例外），CLI 写操作直接写文件（多写者 + mtime sync，见 §4.1）
- **okd.exe**（windowsgui 子系统）：唯一写者。管理 API + 托盘 + sidecar 托管 + Web 静态页分发
- **OkManager.exe**（windowsgui 子系统）：配置中心入口，薄启动器，不含业务逻辑

"GUI 子系统"仅指 PE 头标记（不分配控制台、启动不弹黑窗），okd 没有窗口，唯一可见存在是托盘图标。

## 2. 职责矩阵

| | ok.exe | okd.exe | OkManager.exe |
|---|---|---|---|
| 子系统 | console | windowsgui | windowsgui |
| CLI 子命令 | 全部 | 无 | 无 |
| hook 入口 | 是（转发 + 本地 fallback） | 否 | 否 |
| 数据读写 | 直接写文件（多写者，mtime sync 兜底） | sidecar/API/托盘唯一持有者 | 否（走 HTTP API） |
| 管理 API | 无 | 是（`internal/gui/api.go` 平移） | 无 |
| Web 静态页分发 | 无 | 是（exe 旁 `web/`，实时读盘 + no-cache） | 无 |
| 托盘 | 无 | 是 | 无 |
| sidecar 托管 | 无 | 是（`internal/daemon/sidecar.go` 平移） | 无 |
| 浏览器开窗 | 无 | 无 | 是（`--app=` 模式） |
| 驻留 | 否 | 是 | 否（开窗后即退出） |

## 3. 进程间契约（跨语言共享，改动需同步全部消费方）

消费方：ok.exe（Go）、OkManager.exe（Go，未来可能 C++ 重写）。

- **实例凭证**：`~/.openknowledge/daemon.json`（`internal/daemonx`，原子写入、0600），字段 `port` / `token` / `pid` / `fingerprint` / `started_at`；首选端口 17888，被占回退随机端口并写回
- **健康检查**：`GET http://127.0.0.1:<port>/…`，请求头 `X-Ok-Token: <token>`
- **管理 API**：现有 `internal/gui/api.go` 全部端点，token 认证保留
- **拉起参数**：okd 无参数启动即服务（日志写 `daemon.log`，按行带时间戳——现有行为保留）

契约已落成文档：`docs/2026-08-21-okd-contract.md`（跨语言消费方——如未来 C++ 版 OkManager——的唯一契约依据，改动需同步全部消费方）。

## 4. 各 exe 设计

### 4.1 ok.exe（console）

- CLI 写操作子命令（add/propose/approve/index/archive/on/off/capture…）**保持直接写本地文件，不转发 okd**——条目/配置是普通文件，多写者一致性已由各消费方按需 mtime 增量 `db.Sync` 保证（`internal/hook/core.go:35-41`、`internal/gui/api.go:636`、`internal/cli/cli.go:259-297`），转发只会增加 daemon 存活依赖与延迟。"唯一写者"仅指 okd 独占 sidecar 托管 / 管理 API / 托盘
- `daemon`、`gui` 子命令保留在 ok.exe：`daemon`（隐藏弃用，spawn 回退目标）与 `daemon stop`（安装器在用的客户端操作）；生产 daemon 入口为 `okd.exe`
- **hook 本地 fallback 是有意保留的例外**：okd 不可达时 hook 本地直接写，不得卡死 agent 会话；okd 恢复后收编 fallback 写入（现有逻辑，不动）。此例外要写进注释和本文档，防止以后被当成"违反单写者"误删

### 4.2 okd.exe（windowsgui）

- 新建 `cmd/okd`，平移 `internal/daemon`（server/client/sidecar/spawn）+ `internal/gui`（api/browser/window）
- 托盘：驻留、退出入口、打开配置中心入口
- 单实例：`daemon.json` + 健康检查保证（现有机制）
- 编译：`-ldflags "-H windowsgui"`；日志全量落 `daemon.log`，不依赖控制台 stdout

### 4.3 OkManager.exe（windowsgui）

薄启动器，四步：

1. 读 `daemon.json` + 健康检查探测 okd；没在跑就 `CreateProcess` 拉起，轮询等就绪
2. 拼 `http://127.0.0.1:<port>/?token=…`
3. 找 Edge/Chrome，`--app=` 模式开窗（复用 `internal/gui/browser_windows.go`）
4. 退出（不驻留；驻留是 okd 的事）

先 Go 实现（复用 daemonx + gui/browser，几十行，管线不动）。将来若安装包体积成为问题，可换 C++（约 200~300 行 Win32，100~300KB）——消费的契约不变，替换对其他两个 exe 透明。

## 5. 可用性：okd 挂掉时的降级矩阵

| 能力 | okd 挂时表现 |
|---|---|
| hook 检索注入/沉淀/强制检查 | 正常（本地 fallback） |
| CLI 全部命令 | 正常（首命令自动拉起 okd） |
| 配置中心 | 重开即恢复（启动器拉起 okd） |
| 语义检索（embed sidecar） | 降级为纯关键词检索，okd 重启后自动恢复 |
| 托盘 | 消失，okd 重启后恢复 |

原则：okd 崩溃 = 功能降级，不是停摆。任何一层都不得因 okd 不可达而永久失败。

## 6. 配置中心功能面

以 `web/prototype-manager-v2.html` 为准，左右两栏：左侧菜单（管理/引导/设置/日志/其他），右侧对应页。替换现有 `web/` 四标签 SPA。

- **管理**：项目树 + 条目详情（markdown 解析）；条目增删改、草稿批准与 CLI 对齐
- **引导**：agent 接入卡片（未检测到不显示，安装/卸载）
- **设置**：全局开关 / 语义检索（embedding）/ 模型配置（LLM）/ Hook 超时 / 跨轮注入冷却 / 经验沉淀 / 泛化门控 / 规则配置；范式=弹窗确定生效、总闸即存、简单输入行内保存（改回原值自动变灰）
- **日志**：三来源（ok/daemon/sidecar）实时查看器，2s 轮询，只读
- **其他**：数据导出/导入、更新日志、使用帮助、删除项目知识库（双重确认）、关于

已确立的交互范式（原型迭代中定稿）：卡片=标题行→详情行→控件行；hover 样式必须排除 `:disabled`；整页重渲保持各滚动容器位置；刷新经 `location.hash` 恢复当前菜单。

## 7. 实施步骤（按依赖序）

1. `cmd/okd`：搬 daemon+gui，加托盘，`-H windowsgui`，跑通独立驻留
2. ok.exe 标注：daemon/gui 子命令保留（daemon 隐藏弃用，作 spawn 回退；daemon stop 为安装器在用的客户端操作）；hook fallback 保留并补注释
3. `cmd/okmanager`：Go 薄启动器
4. 配置中心：按原型重写 `web/`（功能面与 CLI 对齐）
5. 构建/安装包：scripts/build-dist.sh 出三 exe；installer 加快捷方式（指向 OkManager.exe）；版本号/winres 同步
6. 契约文档：`daemon.json` + HTTP API + 健康检查，落 `docs/`

每步独立可验证、可回滚；2 之前 1 必须能独立跑。

## 8. 待定问题

1. ~~okd 开机自启策略：注册表 Run 键，还是维持"首次用到时懒拉起"（现状）？~~ **已定**：维持安装即注册自启（Run 键目标改为 okd.exe），懒拉起作为 hook/CLI 路径的既有兜底
2. OkManager 是否/何时换 C++：先 Go，体积敏感时再换
3. okd 异常退出是否要在下次打开配置中心时提示（有 daemon.log 可查，提示可后置）

## 9. 主要影响面

- 新增：`cmd/okd/`、`cmd/okmanager/`
- 大改：`cmd/ok/main.go`（子命令分发）
- 平移：`internal/daemon/`、`internal/gui/`（import 路径调整）
- 重写：`web/`（按 `web/prototype-manager-v2.html`）
- 构建：`scripts/build-dist.sh`、`installer/openknowledge.iss`、`installer/nfpm.yaml`
