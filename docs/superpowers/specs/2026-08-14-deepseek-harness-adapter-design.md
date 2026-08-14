# DeepSeek Harness 适配器设计（agentx 第十适配器）

- 日期：2026-08-14
- 状态：设计已批准（2026-08-14 评审通过）
- 关联：`docs/ARCHITECTURE.md` §9.2；opencode/pi 适配器先例；DeepSeek Harness 源码（本地 `D:\develop\deepseek-harness1`）

## 1. 背景与目标

OpenKnowledge 的 hooks 集成经 agentx 适配器注册表支持 9 个 agent。DeepSeek Harness（`dsh`，TypeScript/Node，Cordis 插件框架，"一切皆插件"）经对其源码核实：

- **插件系统**：插件 = 导出 `apply(ctx, config)` 的 TS/JS ESM 模块，经 `cordis.patch.yml` 行挂载，`name` 字段**直接接受本地绝对路径**（`docs/user/develop/basic/index.md`），无需 npm 发布；
- **事件总线完备**（`packages/core/agent/src/runtime-types.ts`、`packages/core/tools/src/index.ts`）：`agent/pre-step`（waterfall，可改写/追加消息）、`tools/post-execute`（拿完整 `ToolExecutionResult`，可附 `additionalContexts`）、`agent/turn-stopping`（可 `agent.steer()` 续跑）——三个 ok 核心事件全部有一等对应物，且比官方 `dsh-hooks-claude-code` 桥的信息更完整；
- **技能目录原生扫描 `~/.agents/skills`**（`packages/skill/*`，`<projectRoot>/.agents/skills` → `$DSH_HOME/skills` → `~/.agents/skills` 优先级链）——正是 ok 的共享技能目录，技能分发零新机制；
- 家目录：`$DSH_HOME` 环境变量优先，默认 `~/.dsh`（`packages/util/home-paths`）。

目标：新增 `dsh` 适配器，用户经 GUI 引导 / `ok setup --agent dsh` 获得与 kimi/opencode 等价的三大能力——prompt 注入、post-tool 追踪、stop 沉淀闭环。

## 2. 方案选型

| 方案 | 形态 | 结论 |
|---|---|---|
| A. 官方 `dsh-hooks-claude-code` 桥 | 生成 Claude 方言 `hooks.json` + patch 行挂桥 | 否决：桥有已知缺口（PostToolUse 的 `tool_response` 压平为文本、无输入改写），且多一层间接 |
| **B. 原生插件 + 本地绝对路径挂载** | `go:embed` 插件文件写 `$DSH_HOME/plugins/openknowledge/`，`$DSH_HOME/cordis.patch.yml` 插入挂载行 | **采用**（用户拍板）：直订事件总线功能最全；复用 opencode/pi 的 embed 模板管理模式，无 npm 运营 |
| C. npm bundle 分发 | 插件发 npm，`dsh plugin add` 安装 | 否决（用户拍板）：需发包运营；patch `name` 本支持绝对路径，本地挂载已够用 |

已拍板的关键决策：

1. **路径 B + 本地挂载**；npm bundle 本期不做，GUI 不加额外按钮（用户 2026-08-14 确认）。
2. **插件为薄桥**：知识检索/沉淀/拦截逻辑全在 Go 侧，插件只负责事件订阅、调 `ok.exe hook <event>`、把输出转回 DSH 注入通道——与 opencode/pi 同一模式，不随 DSH preview 变更重写业务逻辑。
3. **只挂家目录级 `$DSH_HOME/cordis.patch.yml`**（所有 profile 共享）；单 profile patch 不做（YAGNI）。

## 3. 总体形态

与 opencode/pi 同构，两个新实现文件（测试另计，见 §8）：

- `internal/agentx/deepharness.go`：实现 Agent 接口 9 方法（`agentx.go:11-21`）+ `init()` 里 `Register`；注册表、CLI flag、GUI 下拉、`/api/status` 全部数据驱动自发现，**不需要改注册表与前端**。
- `internal/agentx/dsh_plugin.js`：go:embed 模板，纯 ESM JS（不依赖 TS 编译步骤，规避宿主加载器差异），首行头标记 + sha256 指纹注释（opencode/pi 同款管理模式）。

配置根：`OK_DSH_HOME` 环境变量优先（测试隔离口）> `DSH_HOME`（官方重定位变量）> 默认 `~/.dsh`。

## 4. Go 适配器方法语义

| 方法 | 语义 |
|---|---|
| `ID()` | `"dsh"`（与其 CLI 同名；CLI flag / GUI API 统一） |
| `DisplayName()` | `"DeepSeek Harness"` |
| `Detect()` | 配置根目录存在（与其他适配器一致） |
| `HooksTarget()` | `<home>/plugins/openknowledge/index.js`（回显用） |
| `InstallHooks(exe)` | ① 渲染模板（`{{EXE}}` 占位替换为 ok 绝对路径）整写插件文件（自有新文件整写）；② 往 `$DSH_HOME/cordis.patch.yml` 幂等插入挂载行 `- insert: [{ id: ok-hooks, name: '<file:// URL 指向插件绝对路径>' }]`（无 config——exe 烘焙进 JS；file:// URL 系 Task 6 实测修正）——先剥离自有条目再追加、写前 `.bak-openknowledge` 备份、保留第三方行；文件不存在则建最小骨架 |
| `HooksInstalled()` | 插件文件存在 + 头标记 + 指纹匹配，且 patch 行存在（exe 迁移由 `EnsureHooks` 渲染全文比对兜底重写） |
| `RemoveHooks()` | 删自家插件文件 + 从 patch 剥离自家行（头标记/id `ok-hooks` 双重确认，绝不动第三方）；返回是否实际移除 |
| `EnsureHooks(exe)` | 曾安装且指纹/patch 行过期才重写；显式移除不复活；fail-open（错误仅记日志） |
| `SkillsDir()` | 返回共享 `SkillsHome()`（`agentx.go:53-58`，`OK_SKILLS_HOME` 优先，默认 `~/.agents/skills`） |

`cordis.patch.yml` 的 YAML 编辑纪律：该文件是 patch 行列表，用**文本标记块**（`# >>> openknowledge` / `# <<< openknowledge`）包住自家行做幂等管理（与 kimi TOML 标记块同款思路），不引入 YAML 库做结构化改写，避免破坏用户手写格式与注释。

错误纪律与现有适配器一致：收集式错误聚合，单 agent 失败不影响其余。

## 5. JS 插件模板

导出形态：`export const name = 'openknowledge'` + `export const inject = ['agent', 'tools']`（实际服务名实现时对照 cordis 服务表核实）+ `export function apply(ctx, config)`。

三事件均 try/catch 包裹 + `node:child_process.execFile` 直 exec（无 shell 层，天然免疫 Windows pwsh 引号问题）+ 手动 timeout + kill，**插件自身异常不得拖垮 DSH 宿主**（与 Go 侧 fail-open 同纪律）。

### 5.1 prompt 注入 —— `agent/pre-step`

- 依据：`packages/core/agent/src/runtime-types.ts:231`（waterfall，返回 `PreStepDecision`）。
- 行为：从 `payload.messages` 提取最新用户文本 → execFile `<exe> hook prompt`（stdin 喂 Claude 风格 JSON：`hook_event_name=UserPromptSubmit`、`session_id`、`cwd`、`prompt`）→ stdout 非空则返回 `{ kind: 'enter', messages: [...原消息, 注入消息] }`；空则 `{ kind: 'enter', messages: 原样 }`。
- 插件侧不去重（是否注入由 ok 的 `InjectForPrompt` 决定，与其他 agent 一致）。

### 5.2 post-tool 追踪 —— `tools/post-execute`

- 依据：`packages/core/tools/src/index.ts:175`（waterfall，拿完整 `ToolExecutionResult`——比官方桥不被压平）。
- 行为：仅当工具名属于写盘类（确切清单实现时对照 DSH 工具注册表核实）→ execFile `<exe> hook post-tool`（stdin：`hook_event_name=PostToolUse`、`session_id`、`cwd`、`tool_name`、`tool_input`）→ 返回 `{ kind: 'accept' }` 原样放行，任何失败静默。

### 5.3 stop 闭环 —— `agent/turn-stopping`

- 依据：`runtime-types.ts:278`（serial；可 `agent.steer()` 强制再来一步）。
- 行为：execFile `<exe> hook stop`（stdin：`hook_event_name=Stop`、`session_id`、`cwd`）→ **exit code 2 时取 stderr 原文**（纯文本协议阻断语义，`internal/hook/hook.go`），调 `agent.steer()` 把 reason 作为自省消息续跑。
- **风险点**：`steer()` 的确切签名需实现期对照源码核实；若不可用则降级为仅记日志不阻断（enforce 模式降级，有 qoder-ide Stop 不可阻断的先例）。
- 防循环：插件侧不设计数器，防重完全依赖 ok 侧 `CheckStop` 幂等语义（与 pi/opencode 定稿一致）。

### 5.4 子进程调用约定

- stdin 一律 Claude 风格 snake_case JSON（ok 侧 `hook.ParseEvent`）。
- 输出协议 = 纯文本 format（args 末尾**不带** `claude`）：注入写 stdout；阻断 = stderr + exit 2。
- 每条 execFile 带 timeout（对齐 pi 模板），超时/非零退出（stop 的 exit 2 除外）一律静默吞掉。

## 6. 回调链路

插件 → `ok hook <event>` → 优先 HTTP 转发常驻 daemon（`cmd/ok/main.go` → `internal/daemon/client.go`）→ daemon 不在则本地兜底。`internal/hook/`、`internal/daemon/` 零改动。

## 7. 技能分发

零新机制：`SkillsDir()` 返回共享 `SkillsHome()`，`InstallSkills`（`internal/setupx/setupx.go`）按已检测 agent 并集分发时自动覆盖；DSH 原生扫描 `~/.agents/skills`（`docs/subsystems/skills.md`），现有 6 个技能模板天然合法。

## 8. 测试计划

- 新建 `internal/agentx/deepharness_test.go`：`t.Setenv("OK_DSH_HOME", ...)` + `OK_HOME` 双隔离（参照 `zcode_test.go`）；覆盖 Detect / InstallHooks 幂等（插件文件 + patch 行各重跑一次）/ HooksInstalled 指纹与 patch 行判定 / InstallPreservesForeign（patch 文件预置第三方行 + 未知注释保留 + `.bak-openknowledge` 备份）/ RemoveHooks 往返（只删自家行）/ EnsureHooks（无配置 no-op、过期重写、显式移除不复活）/ SkillsDir。
- `internal/gui/api_test.go`：硬编码 agent 计数 9→10。
- **全仓遍历注册表的测试补 `OK_DSH_HOME` 隔离**：`gui/api_test.go` 的 `newEnv`、`setupx/setupx_test.go`、`setupx/uninstall_test.go`——知识库 pitfall（agentx 新适配器须补注册表遍历测试隔离）前置消化。
- JS 模板不做运行时单测（与 pi/opencode 同款，靠指纹管理）。

## 9. 实现期必须实测的风险项（实测结果回写本节与知识库）

**计划编制阶段已核实（2026-08-14，对照 deepseek-harness1 源码）:**

1. ~~**沙箱继承**~~ → **设计消解**：插件在 DSH 宿主进程内运行，用 `node:child_process.execFile` 直拉子进程，不经 `ctx.shell` 沙箱执行器，`workspace-write` 沙箱不适用于插件子进程。
2. ~~**`agent.steer()` 签名**~~ → 已确认：`steer(message: UserMessage): void`（`runtime-types.ts:133`）；阻断式 Stop 的官方桥译法即 `agent.steer(...)`（`hooks-claude-code/src/index.ts:270-277`）。
3. **patch 行绝对路径挂载语法** → **已实证**：`dsh --profile web --dump-config` / `--profile headless --dump-config` 均解析出 `id: ok-hooks` + `name: file:///...` 行。**重要修正**：vendored cordis loader 把 `name` 直接交给 Node ESM `import()`（`vendor/loader/src/config/tree.ts:155-159`），Windows 盘符绝对路径（`D:/...`）报 `ERR_UNSUPPORTED_ESM_URL_SCHEME`——已改为 `file:///` URL 挂载（commit c53bc09）。
4. **DSH 是否直接加载 `.js` ESM 插件文件** → **已实证**：无 package.json 的单文件 `.js` 可加载；`import('file:///.../index.js')` 直接返回 `{ name:'openknowledge', apply:fn }`，`dsh web` 启动 20s 无 import 错误。
5. DSH 为 developer preview，事件 API 可能破坏性变更——插件订阅处做存在性守卫，升级报错不炸宿主。

**端到端会话级验证（2026-08-14，keyless 真实 DSH 会话）:** 插件经真实会话触发 `tools/post-execute`（`tool=Write/Edit`）与 prompt 事件，均到达 `ok.exe` 并落 `~/.openknowledge/ok.log`（`22:02:48 post-tool skip: tool=Write ...`、`22:06:57 prompt embed identity ...` 等）；`post-tool skip` 因路径在注册项目之外属正确语义。`agent/turn-stopping`（stop 闭环）因无真实 key 未在无头会话中实证，留待用户真实 DSH 会话终验。

另核实：写盘工具名 `write`/`edit`、参数键 `file_path`（`tool-fs/src/write.ts:51`），与 Claude 方言一致；`UserMessage` 为纯数据（`message.ts:192`），插件用 `node:crypto` 自造，无需依赖 `@deepseek-ai/*` 包。

## 10. 文案与文档点位

| 位置 | 改动 |
|---|---|
| `internal/cli/setup.go` | "未检测到支持的 agent"名单（如为静态文案）加 dsh |
| `README.md` / `README_EN.md` | 多 agent 表与 setup 说明加 dsh |
| `docs/ARCHITECTURE.md` §9.2 | 适配器对比表加行 + dsh 适配器段落；§18.3 环境变量表加 `OK_DSH_HOME` |
| `web/help.md` | agent 列表加 dsh（如需） |

不需要改（已验证数据驱动自发现）：注册表本体、`ok setup --agent` flag、`ok init` 联动、`ok doctor` 遍历、`/api/status` agents 数组、`/api/setup/hooks`、前端 agent 下拉、技能安装/卸载遍历、自愈遍历、daemon 转发、`installer/`。

## 11. 明确不做（YAGNI）

- npm bundle 发布与 GUI 第二按钮（用户明确暂缓）
- 单 profile 级 patch 写入
- `agent/session-start` 注入（detached 语义，可能赶不上首个请求；ok 现有三事件已覆盖核心价值）
- PreToolUse 拦截（`tools/pre-execute` 的 allow/deny/ask）
- DSH 专属 GUI 卡片（reasonix 式 per-agent 扩展 UI）
