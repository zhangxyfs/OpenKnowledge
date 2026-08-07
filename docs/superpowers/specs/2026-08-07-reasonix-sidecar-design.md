# Reasonix sidecar 集成设计（Extension Protocol 方案）

- 日期：2026-08-07
- 状态：已批准（用户确认方案 B、SDK vendor、enforce 默认软+硬、GUI 三档开关）
- 目标版本：v2.5.0

## 1. 背景与目标

OpenKnowledge（ok）通过 agentx 注册表支持多 agent（kimi / pi / zcode）：每个 agent 一个适配器，统一驱动 CLI / GUI / hook 自愈。本次接入 **Reasonix**（源码 `D:\develop\DeepSeek-Reasonix`，Go 实现的 DeepSeek-native coding agent）。

与既有 agent 的关键差异：Reasonix 的 `UserPromptSubmit` hook **不把 stdout 注入上下文**（`internal/hook/runner.go:161-169`，仅返回 block/message），stdout 注入只有 `SessionStart` 支持——ok 的核心价值"逐 prompt 检索注入"无法经传统 hook 通道实现。因此采用 **Extension Protocol sidecar**（方案 B）：ok.exe 以插件 runtime 形态住进 Reasonix 事件循环，获得逐 prompt 的拦截与改写能力。

决策记录：

| 决策点 | 结论 |
|---|---|
| 注入通道 | Extension Protocol sidecar（拦截 `input.receive` + `tool.after`），不做 settings.json hook |
| Reasonix Go SDK 引入 | **vendor 快照**进 ok 仓库（自包含构建；手动同步上游） |
| enforce 默认语义 | auto 自省=软提醒（replace 前缀）；changelog_required=硬阻断（block） |
| enforce 表达开关 | GUI 引导页三档：`soft` / `hard` / `mixed`（默认），写 ok 全局配置，即时生效 |
| 插件安装 | ok 直接写 `<reasonixHome>/plugins/openknowledge/` + `plugin-packages.json`（备份+原子写） |
| 技能分发 | 零改动：Reasonix 全局扫描 `~/.agents/skills`，`SkillsDir()` 返回共享 SkillsHome |

## 2. 调研结论（设计依据的 Reasonix 事实）

### 2.1 扩展协议 v1（docs/EXTENSION_PROTOCOL.zh-CN.md、internal/extension/protocol/）

- 传输：严格 JSON-RPC 2.0 over NDJSON（stdin/stdout 每行一个 JSON 对象）；单帧上限 8 MiB
- 生命周期：宿主 exec form（不经 shell）拉起 sidecar → `extension/initialize` → sidecar 应答声明（必须是 manifest 声明子集，否则 `capability_not_declared`）→ `extension/initialized` → 正常服务；关闭：`extension/shutdown`（带超时）→ 关 stdin → 杀进程树
- **`InitializeParams.Session` = `{sessionId, workspaceRoot, generation}`**（`dto_lifecycle.go:30-34`，sessionId/workspaceRoot 均非空）——ok 的 per-session state 与 `project.FromCwd` 直接可用
- 拦截裁决：`continue` / `block`（中止操作 + 用户可见原因）/ `replace`（替换载荷，宿主按点位 DTO 严格重校验）/ `allow`/`deny`（仅 permission.decision）
- 超时：输入/工具/权限点位默认 5s；manifest `timeoutMillis` 可调，上限 60s；`required=false`（默认）的扩展超时/崩溃只告警降级，不影响宿主
- sidecar 环境：继承完整环境 + manifest env + `REASONIX_PLUGIN_ROOT/NAME/VERSION`（`internal/extension/sidecar/process.go:164-179`）
- 稳定性契约：major v1 内只增不改（新增 optional 字段/枚举/方法）；schema hash CI 防漂移

### 2.2 关键点位载荷（internal/extension/dispatch/payloads.go）

| 点位 | 载荷 | ok 的用途 |
|---|---|---|
| `input.receive` | `{text}` | 检索 query 来源 + 注入通道（replace 前缀知识）+ enforce（block/prepend） |
| `tool.after` | `{name, arguments(JSON 字符串), result, isError}` | touched 文件追踪（等价 post-tool 链） |

- 写文件工具名：`write_file` / `edit_file` / `multi_edit` / `notebook_edit`，参数键 `path`（`internal/hook/hook.go:610-617` 的映射表佐证）——ok 现有 `FilePath()` 已兼容 `path`
- 不选 `context.prepare`：载荷是整个 messages 数组（每回合 MiB 级搬运 + content ref 分页），过重；`input.receive` 轻量且语义正对"用户提交 prompt 时注入"
- 不选 `session.start`：其 replace 只能改 `SessionPayload{sessionPath, phase}`，无法注入上下文；会话首次注入逻辑放 `input.receive` 首回合（与 ok 现有 HandlePrompt 语义一致）

### 2.3 插件安装与信任门（docs/EXTENSIONS.zh-CN.md、internal/pluginpkg/、internal/extension/sidecar/process.go）

- **只有通过插件安装流程（写入 `<reasonixHome>/plugin-packages.json`）的插件才能启动 sidecar**；项目配置永远无法声明 runtime——信任门即"在该文件里"
- `State` 结构简单：`{version, plugins:[{name, source, root, version, description, manifestKind, enabled, commit}]}`（`pluginpkg.go:241-255`）；写锁是进程内的（`pluginpkg.go:313` 注释明示非跨进程锁），ok 外部写入用备份 + temp+rename 原子写，不劣于宿主自身
- `runtime.command` 必须解析为**绝对路径**（exec form，`resolveRuntimeCommand` process.go:144-161），**允许指向插件根之外**——直接指向 ok.exe 安装路径即可，无需拷贝二进制；ok 升级替换 ok.exe 后，下一个 sidecar generation 自动用新版本
- manifest v1 `runtime` 块字段：`command / args / env / required / priority(-1000..1000) / timeoutMillis / intercepts / replaces / capabilities`（`manifest_v1.go:34-52`；capabilities 合法值 `interceptors|strategies|providers|ui`）

### 2.4 其他事实

- Reasonix home：`REASONIX_HOME` > Windows `os.UserConfigDir()/reasonix`（回退 `~/AppData/Roaming/reasonix`）/ 其他 `~/.reasonix`（`internal/config/paths.go:47-67`）
- skills 发现：全局扫描 `<reasonixHome>/skills/` + `~/(.reasonix|.agents|.agent|.claude)/skills`（`internal/skill/skill.go:452-496`，非隔离模式）→ ok 共享 SkillsHome（`~/.agents/skills`）直接被发现
- Reasonix Go SDK：`sdk/go`，module `github.com/esengine/DeepSeek-Reasonix/sdk/go`，约 2700 行（sdk.go / wire.go / types_ext.go / types_generated.go），仅标准库依赖，已处理传输/握手/序号/content ref/关闭；官方文档明示版本化 tag 发布前从源码使用

## 3. 总体架构

```
Reasonix 宿主（CLI/Desktop）
   │ exec form 拉起；NDJSON/JSON-RPC over stdio
   ▼
ok.exe extension-serve（新子命令 = sidecar 模式，internal/rxext）
   ├─ initialize    ← 握手获得 sessionId + workspaceRoot + generation
   ├─ input.receive → 逐 prompt 检索注入（replace）+ enforce 评估（block / prepend）
   └─ tool.after    → touched 文件追踪（post-tool 链等价物）

安装面（ok setup --agent reasonix / GUI / 自愈）：
   agentx.reasonixAgent
   ├─ 写 <reasonixHome>/plugins/openknowledge/reasonix-plugin.json（manifest v1 + runtime 块）
   ├─ 合并写 <reasonixHome>/plugin-packages.json（信任门登记）
   └─ SkillsDir() = 共享 SkillsHome（setupx 现有分发零改动）
```

## 4. 组件设计

### 4.1 `internal/rxext`（新包：sidecar 实现）

**vendor SDK**：拷贝 `DeepSeek-Reasonix/sdk/go` 为 `internal/rxext/sdk/`（包名调整为 ok 内部包），源头部注明上游 commit 与同步方式；仅标准库依赖，无额外 go.mod 变化。

**`ok extension-serve` 子命令**：调用 SDK `Serve`，注册：

- `Initialize`：记录 `Session.SessionID / WorkspaceRoot / Generation`；应答 `Subscriptions: ["input.receive", "tool.after"]`（manifest 子集）
- `input.receive` 拦截器（热路径，fail-open）：
  1. 解析 `{text}` 为 query；`project.FromCwd(workspaceRoot)` 取项目上下文
  2. kb.db 增量同步（复用现有逻辑；embedding 失败降级纯 BM25）
  3. 注入组装（复用重构后的检索核心）：会话首次 = mandatory 全文 + INDEX 摘要行；每次 = 混合检索（预算 800 字符 / 2 条）
  4. enforce 评估（复用现有 enforce 包 + per-session state）：
     - `changelog_required` 命中 → 按配置 `block` 或 prepend（见 4.4 三档表）
     - auto 自省提醒命中 → 按配置 prepend 或 block
     - 判定为 `block` 时直接返回 `Block{原因}`，不再注入（用户修正后重发输入时自然获得注入）
  5. 产出 `Replace{text: "<ok-context>…</ok-context>\n\n" + 原文}`：知识注入与软提醒合并为**一个** `<ok-context>` 块后前缀；无任何注入/检查结果时 `Continue`
  6. **任何内部错误 → `Continue`**（fail-open 铁律），错误写 `ok.log`
- `tool.after` 拦截器：`name ∈ {write_file, edit_file, multi_edit, notebook_edit}` 且 `!isError` → 从 `arguments`（JSON 字符串）取 `path` 记 touched（复用 post-tool 核心）→ 一律 `Continue`

**per-session state**：复用 `state.Load(dir, sessionId)`，sessionId 来自 initialize；一个 Reasonix 会话对应一个 sidecar 进程（initialize 携带单会话上下文），进程内缓存 project context 与 state。

### 4.2 hook 包重构（唯一改动现有代码的部分）

把 `HandlePrompt` / `HandlePostTool` 的核心从"读 stdin 事件 + 写 stdout 协议"中抽出为纯函数：

- `InjectForPrompt(pc, sessionID, promptText) (context string, err error)`：同步索引 + 首次基础注入 + 检索注入
- `TrackTouched(pc, sessionID, toolName, argsJSON)`：touched 追踪 + 日志
- enforce/auto 自省评估抽为可复用函数（返回命中结果与文案，不绑定输出协议）

约束：**只抽函数，不改行为**；kimi/zcode/pi 的 `ok hook *` 子命令保持现有 stdin/stdout 契约，现有测试必须全绿。

### 4.3 `internal/agentx/reasonix.go`（适配器）

- `ReasonixHome()`：`OK_REASONIX_HOME`（测试口）> `REASONIX_HOME` > Windows `os.UserConfigDir()/reasonix`（回退 `~/AppData/Roaming/reasonix`）/ 其他 `~/.reasonix`
- `Detect()`：home 目录存在
- `InstallHooks(exe)`（实际是安装插件包）：
  1. 写 `<home>/plugins/openknowledge/reasonix-plugin.json`：
     ```json
     {
       "apiVersion": "reasonix.io/plugin/v1",
       "name": "openknowledge",
       "version": "<ok 版本>",
       "description": "OpenKnowledge 知识库 sidecar：逐 prompt 检索注入与经验沉淀",
       "contributes": {},
       "runtime": {
         "command": "<exe 绝对路径>",
         "args": ["extension-serve"],
         "required": false,
         "priority": 0,
         "timeoutMillis": <HookTimeoutSec()*1000>,
         "intercepts": ["input.receive", "tool.after"],
         "capabilities": ["interceptors"]
       }
     }
     ```
  2. 合并写 `<home>/plugin-packages.json`：读出现有 State（不存在则新建；解析失败报错不覆盖）→ 追加/更新 `openknowledge` 条目（`root` 指插件目录、`manifestKind`、`enabled: true`、`version`）→ `.bak-openknowledge` 备份 + temp+rename 原子写
- `RemoveHooks()`：移除 State 中 openknowledge 条目 + 删插件目录；返回是否真有改动
- `EnsureHooks(exe)`：State 条目缺失不复活（用户显式移除不重建）；存在但内容过期（exe 迁移、版本/超时变化）时重写两个文件
- `HooksInstalled()`：State 条目存在且 manifest 的 runtime.command/args/timeoutMillis 与当前期望一致
- `SkillsDir()`：共享 `SkillsHome()`；`HooksTarget()`：插件目录展示路径

### 4.4 GUI 三档开关（引导页）

- 配置键：ok 全局配置 `~/.openknowledge/config.toml` 新增 `[reasonix] enforce_mode = "soft" | "hard" | "mixed"`（默认 `mixed`，键缺失即默认）——与 `[hooks] timeout_sec` 同一文件、同一读写路径
- 语义表：

| 档位 | auto 自省 | changelog_required |
|---|---|---|
| `soft` 全软提示 | replace 前缀提醒 | replace 前缀提醒（无视则每条输入重复提醒） |
| `hard` 全硬阻断 | block（中止该次输入，原因用户可见） | block |
| `mixed`（默认） | replace 前缀提醒 | block |

- `internal/gui/api.go`：配置读写接口增加 `rx_enforce_mode` 字段（随 hook 超时卡同路径）
- 引导页前端：agent 下拉选中 `reasonix` 时条件渲染三选一 radio（全软提示 / 全硬阻断 / 软+硬·默认），选择即时保存
- **即时生效**：sidecar 每条输入实时装载配置，无需重装插件或重载 Reasonix 扩展
- kimi/zcode/pi 不受影响（仅 reasonix 选中时显示；sidecar 是唯一消费者）

### 4.5 CLI 路由

- `main.go` 注册 `extension-serve` 子命令（归入现有命令表，隐藏性质同 `hook` 内部子命令）
- `ok setup --agent reasonix` 走注册表现有路径，无需新代码

## 5. 错误处理与 fail-open

- 拦截器内部任何错误 → `Continue`，绝不阻断用户输入；错误写 `~/.openknowledge/ok.log`
- sidecar 崩溃/超时：`required=false` → 宿主告警降级，Reasonix 正常使用；宿主在空闲 reload 时自动重启 sidecar
- `plugin-packages.json` 解析失败：报错且不覆盖（同 zcode config.json 策略）
- 配置读取失败（`enforce_mode` 非法值）：按 `mixed` 默认处理
- 状态（touched/state.json）写失败：仅记日志，不影响注入

## 6. 性能与 prompt 缓存

- `input.receive` 位于回合热路径（协议默认 5s；ok 在 manifest 写 `timeoutMillis = HookTimeoutSec()*1000`，默认 10s）：本地 BM25 检索 <1s；embedding 走现有 `Embedding.TimeoutSec`，超时降级纯 BM25
- 注入内容包 `<ok-context>` 标签、置于**用户输入文本前部**（该消息本身处于上下文尾部）→ 不破坏历史消息前缀缓存（Reasonix 文档"动态数据应尽量留在当前回合尾部"的合规做法）
- 检索预算沿用现有 800 字符 / 2 条上限

## 7. 测试策略

- `internal/rxext`：拦截器单测——注入（首次/检索/去重）、replace 载荷形状、block/prepend 三档分支、fail-open（注入失败/配置损坏/索引损坏）、tool.after touched 追踪与 isError 过滤；参考 vendored SDK 的 `fakehost_test.go` 搭 fake host 做 initialize/intercept 回路测试
- `internal/agentx/reasonix_test.go`：照 `zcode_test.go` 模式——安装/移除/自愈/current 判定、State 未知插件条目保留、损坏 JSON 不覆盖、exe 迁移重写
- `internal/gui/api_test.go`：`rx_enforce_mode` 读写与非法值处理
- 重构回归：现有 kimi/zcode/pi hook 测试必须全绿（行为不变）
- 真机验收：`ok setup --agent reasonix` 后在 Reasonix 中验证——注入出现、改文件后 changelog 阻断、GUI 三档切换即时生效、`/reload` 后 sidecar 正常重生

## 8. 明确不做（Out of Scope）

- 不写 Reasonix `settings.json` hook（SessionStart/PostToolUse/Stop）——sidecar 全覆盖，避免双通道重复注入
- 不拦截 `context.prepare` / `system_prompt.build` / `provider.*`（重载荷/无需求）
- 不做 Reasonix 插件的 `reasonix plugin install` CLI 调用（直接写 State，与 ok 其他适配器"直接写配置"哲学一致）
- 不做插件包形式的 skills 分发（共享 SkillsHome 已够用）
- 桌面端（Wails）专项适配——sidecar 由宿主统一拉起，CLI/Desktop 天然同效

## 9. 验收标准

1. `ok setup --agent reasonix` 后，Reasonix 新会话首条输入即见到知识注入（mandatory + 索引 + 检索）
2. 改了代码未更 CHANGELOG 且配置了 `changelog_required`：下一条输入被 block（mixed/hard）或前缀提醒（soft）
3. GUI 引导页选中 reasonix 出现三档开关，切换后下一条输入即按新档位生效
4. `ok setup --agent reasonix --remove`（或卸载流程）后 State 与插件目录清理干净，Reasonix 无残留报错
5. 全部现有测试 + 新增测试绿；`go build` 产出的安装包装/卸不影响 Reasonix 配置（卸载前清理插件登记）
