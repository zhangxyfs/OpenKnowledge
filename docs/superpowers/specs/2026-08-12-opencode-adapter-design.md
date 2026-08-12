# opencode 适配器设计（agentx 第五适配器）

- 日期：2026-08-12
- 状态：设计已批准（2026-08-12 评审通过）
- 关联：`docs/ARCHITECTURE.md` §9.2；知识库《多-Agent支持（agentx）》；opencode 源码（本地 `D:\develop\opencode`）

## 1. 背景与目标

OpenKnowledge 的 hooks 集成经 agentx 适配器注册表支持多 agent（kimi/pi/zcode 配置合并写，reasonix sidecar 插件包）。经对 opencode 源码核实：

- opencode **没有** Claude 式 hooks 配置字段（配置 schema 中无 `hooks`），zcode 式 JSON 合并写路线不存在；
- opencode 有**插件系统**：`{plugin,plugins}/*.{ts,js}` 单文件直接 `import()` 加载（`packages/opencode/src/config/plugin.ts:21-29`、`plugin/loader.ts:136-145`），插件返回 hooks 对象（`packages/plugin/src/index.ts:222-335`）；
- opencode **原生扫描 `~/.agents/skills/**/SKILL.md`**（`packages/opencode/src/skill/index.ts:190-194`），frontmatter 只强制 `name`（`skill/index.ts:53-59`）——正是 ok 的共享技能目录，技能分发零新机制。

目标：新增 opencode 适配器，用户经 GUI 引导 / `ok setup --agent opencode` 获得与 kimi/pi 等价的三大能力——prompt 注入、post-tool 追踪、stop 沉淀闭环（nudge 形态）。

## 2. 方案选型

| 方案 | 形态 | 结论 |
|---|---|---|
| **A. pi 式 go:embed TS 插件模板** | `opencode.go` + 内嵌 `opencode_plugin.ts`，渲染 exe 路径写全局插件目录，sha256 指纹管幂等/自愈 | **采用**：与 pi 完全同构；Go 侧零新机制（纯文本协议复用，`internal/hook/` 不动）；技能零改动 |
| B. npm 插件包 + `opencode.json` 登记 | 插件发 npm，InstallHooks 合并写 `plugin` 数组 | 否决：需发包运营、离线不可用，违背"单 exe 本地工具"定位 |
| C. 外部 SSE 订阅事件流 | ok daemon 订阅 opencode server 的 SSE | 否决：只读通道做不了 prompt 注入（核心价值丢一半），且依赖 server 常驻与端口发现 |

已拍板的关键决策：

1. **prompt 注入用 `chat.message` + synthetic text part**：每个用户 prompt 恰好触发一次；备选的 `experimental.chat.system.transform` 挂在每次 LLM 请求上（含工具循环），会把 `ok hook prompt` 重复调用 N 次，否决。
2. **Stop 闭环 = pi 式补发 nudge**：`session.idle` 事件无法像 Claude stop-block 那样"拒绝停止并塞回 reason"，由插件用 SDK client 给会话补发自省消息（用户已确认此方案）。
3. **插件只装全局 `~/.config/opencode/plugins/`**：与 pi 的全局安装一致；项目级 `.opencode/plugins/` 不做（YAGNI）。

## 3. 总体形态

与 pi 同构，两个新实现文件（测试文件另计，见 §8）：

- `internal/agentx/opencode.go`：实现 Agent 接口 9 方法（`agentx.go:11-21`）+ `init()` 里 `Register`；注册表、CLI flag、GUI 下拉、`/api/status` 全部数据驱动自发现，**不需要改注册表与前端**。
- `internal/agentx/opencode_plugin.ts`：go:embed 模板，首行头标记 + sha256 指纹注释（pi 同款管理模式）。

配置根：`OK_OPENCODE_HOME` 环境变量优先（测试隔离口，`OK_ZCODE_HOME` 同款模式），默认 `~/.config/opencode`。

## 4. Go 适配器方法语义

| 方法 | 语义 |
|---|---|
| `ID()` | `"opencode"`（CLI flag / GUI API / localStorage 统一） |
| `DisplayName()` | `"opencode"`（跟随官方品牌小写） |
| `Detect()` | 配置根目录存在（与 kimi/pi/zcode 探测配置根一致） |
| `HooksTarget()` | `<root>/plugins/openknowledge.ts`（回显用） |
| `InstallHooks(exe)` | 渲染模板（`{{EXE}}` 占位替换为 ok 绝对路径）→ 整写目标文件（自有新文件整写，无需备份/合并）；幂等 |
| `HooksInstalled()` | 文件存在 + 头标记匹配 + 指纹匹配（指纹 = 模板内容 sha256 前 12 位，pi 同款；exe 迁移由 `EnsureHooks` 渲染全文比对兜底重写） |
| `RemoveHooks()` | 仅当头标记确认本工具生成才删除；返回是否实际删除 |
| `EnsureHooks(exe)` | 曾安装且指纹过期才重写；显式移除不复活；fail-open（错误仅记日志） |
| `SkillsDir()` | 返回共享 `SkillsHome()`（`agentx.go:53-59`，`OK_SKILLS_HOME` 优先，默认 `~/.agents/skills`） |

错误纪律与现有四适配器一致：收集式错误聚合，单 agent 失败不影响其余。

## 5. TS 插件模板

导出形态：旧式命名导出（`export const OpenKnowledgePlugin = async (ctx) => ({...})`，走 `plugin/index.ts:97-110` 的 `getLegacyPlugins`），免 package.json/tsconfig。

三钩子均 try/catch 包裹 + 子进程 `.nothrow()` + timeout，**插件自身异常不得拖垮 opencode 宿主**（与 Go 侧 fail-open 同纪律）。

### 5.1 prompt 注入 —— `chat.message`

- 依据：签名 `packages/plugin/src/index.ts:234-243`；触发点 `session/prompt.ts:999-1009`，`output.parts` 按引用传入且 hook 返回后继续处理并持久化（`prompt.ts:1011-1047`）。
- 行为：从 `output.parts` 提取用户文本 → spawn `<exe> hook prompt`（stdin 喂 Claude 风格 JSON：`hook_event_name=UserPromptSubmit`、`session_id=input.sessionID`、`cwd=ctx.directory`、`prompt=<提取文本>`）→ stdout 非空则 push part：`{ id: <唯一>, messageID: output.message.id, sessionID: input.sessionID, type: "text", text: <stdout>, synthetic: true }`（synthetic 使其不在 UI 当用户输入渲染）。
- 每个用户 prompt 恰好触发一次，插件侧不去重（是否注入、注入什么由 ok 的 `InjectForPrompt` 决定，与 kimi/zcode 一致）。

### 5.2 post-tool 追踪 —— `tool.execute.after`

- 依据：触发点 `session/tools.ts:121`。
- 行为：仅当工具名属于写盘类（确切清单实现时对照 opencode 源码工具注册表核实，预期 write/edit 类）→ spawn `<exe> hook post-tool`（stdin：`hook_event_name=PostToolUse`、`session_id`、`cwd`、`tool_name`、`tool_input`）→ 忽略输出，任何失败静默。

### 5.3 stop 闭环 —— `event` 监听 `session.idle`

- 依据：event 钩子接线 `plugin/index.ts:253-260`（按 directory 过滤后转发 `{ id, type, properties }`）。
- 行为：spawn `<exe> hook stop`（stdin：`hook_event_name=Stop`、`session_id`、`cwd`）→ **exit code 2 时取 stderr 原文**（纯文本协议阻断语义，`internal/hook/hook.go:109-116`；stderr 即 ok 的 stop reason）→ 用 `PluginInput.client`（opencode SDK client）给该 session 补发 reason 作为用户消息，驱动当场自省沉淀。
- 防循环（按 pi 定稿）：pi 扩展插件侧**不设计数器**——防重完全依赖 ok 侧 `CheckStop` 的幂等语义（auto 软提醒按 `LastExtractReminder`/轮次间隔节流，enforce 硬阻断 `MarkBlocked` 每会话每规则一次）；opencode 插件保持一致。

### 5.4 子进程调用约定

- stdin 一律 Claude 风格 snake_case JSON（ok 侧 `hook.ParseEvent`，`hook.go:28-38`）。
- 输出协议 = 纯文本 format（args 末尾**不带** `claude`）：注入写 stdout；阻断 = stderr + exit 2。
- 每条 spawn 带 timeout（实现时对齐 pi 模板），超时/非零退出（stop 的 exit 2 除外）一律静默吞掉，可选 `client.app.log` 记录。

## 6. 回调链路

插件 → `ok hook <event>` → 优先 HTTP 转发常驻 daemon（`cmd/ok/main.go:96` → `internal/daemon/client.go:77-107`）→ daemon 不在则本地兜底并后台拉起。`internal/hook/`、`internal/daemon/` 零改动。

## 7. 技能分发

零新机制：`SkillsDir()` 返回共享 `SkillsHome()`，`InstallSkills`（`internal/setupx/setupx.go:66-80`）按已检测 agent 并集分发时自动覆盖；opencode 原生扫描该目录，模型经内置 `skill` 工具按需加载（`tool/skill.ts:12-70`），现有 6 个技能模板天然合法。

## 8. 测试计划

- 新建 `internal/agentx/opencode_test.go`：`t.Setenv("OK_OPENCODE_HOME", ...)` + `OK_HOME` 双隔离（参照 `zcode_test.go:12-18`）；覆盖 Detect / InstallHooks 幂等 / HooksInstalled 指纹判定 / RemoveHooks 只删自家文件 / EnsureHooks 过期重写 + 显式移除不复活。
- `internal/gui/api_test.go:150`：硬编码 agent 计数 4→5。
- **全仓遍历注册表的测试补 `OK_OPENCODE_HOME` 隔离**（含 cmd/ok E2E、`setupx_test.go`、`uninstall_test.go`）——知识库 pitfall（v2.5.0 遍历测试真实写入用户配置）前置消化。
- TS 模板不做运行时单测（与 pi 同款，靠指纹管理）。

## 9. 文案与文档点位

| 位置 | 改动 |
|---|---|
| `internal/cli/setup.go:87` | "未检测到支持的 agent"名单加 opencode |
| `internal/cli/cli.go:441` | doctor 名单加 opencode |
| `internal/cli/setup.go:142-147` + `web/index.html:159-161` | 引导"下一步"文案泛化，不写死 kimi（顺带还债） |
| `README.md:43,94-95,228` / `README_EN.md:44,95-96,229` | 多 agent 表与 setup 说明加 opencode |
| `docs/ARCHITECTURE.md` §9.2 | 注入形态表加行 + opencode 适配器段落 |
| `site/assets/site.js:84-86,165-174` | 官网 agent 列表文案 |

不需要改（已验证数据驱动自发现）：注册表本体、`ok setup --agent` flag 与报错、`ok init` 联动、`ok doctor` 遍历、`/api/status` agents 数组、`/api/setup/hooks`、前端 agent 下拉、技能安装/卸载遍历、自愈遍历、daemon 转发、`installer/`。

## 10. 明确不做（YAGNI）

- 项目级 `.opencode/plugins/` 安装
- npm 插件包发布
- `permission.ask` 自动批准
- opencode 专属 GUI 卡片（reasonix 式 per-agent 扩展 UI）
- `experimental.chat.system.transform` / `messages.transform` 注入通道

## 11. 实现事实核实结论（2026-08-12 计划编制时已全部核实，以本地 `D:\develop\opencode` 源码为据）

1. **Windows 全局配置目录** = `~/.config/opencode`（xdg-basedir 拼法，无 win32 特判）；`XDG_CONFIG_HOME` 参与，`OPENCODE_CONFIG_DIR` 最高优先（`packages/core/src/global.ts:10-14,64`；`cli/cmd/uninstall.ts:238` 佐证）。适配器解析序定稿：`OK_OPENCODE_HOME`（测试口）> `OPENCODE_CONFIG_DIR` > `XDG_CONFIG_HOME/opencode` > `~/.config/opencode`。
2. **写盘工具 id**：`write`（参数 `filePath`）、`edit`（参数 `filePath`）、`apply_patch`（参数 `patchText`）；gpt 系新模型（含 `gpt-` 且非 `oss`/`gpt-4`）只给 `apply_patch`，其余模型只给 `write`+`edit`（`tool/registry.ts:292-295` 互斥）——post-tool 追踪必须同时覆盖 apply_patch。patch 文件标记：`*** Add File:` / `*** Update File:` / `*** Delete File:`（`patch/index.ts:76-87,212`）。
3. **SDK 发消息**：`client.session.promptAsync({ path: { id }, body: { parts: [{ type: "text", text }] } })`——"start if needed and return immediately"，不等回复（`sdk/js/src/gen/sdk.gen.ts:639-646`；body 结构 `types.gen.ts:2683-2705` 的 `SessionPromptAsyncData`）。同步版 `client.session.prompt` 会等完整回合，event 钩子里禁用。
4. **`session.idle` properties** = `{ sessionID: string }`（`types.gen.ts:475-480`；桥接层把内部 `data` 映射为 `properties`，`event-v2-bridge.ts:43`）。
5. **pi 防循环**：`pi_extension.ts` 插件侧无计数——防重靠 ok 侧 `CheckStop` 幂等语义；opencode 同样不设计数器（见 §5.3）。
6. **text part 结构**：`part.type === "text"`、`part.text: string`；注入 part 需自带 `id`/`messageID`/`sessionID`/`type`/`text`，`synthetic: true` 不在 UI 当用户输入渲染（`types.gen.ts:160-175`）。`UserMessage` 上无用户输入文本字段，文本只能从 `output.parts` 提取。
7. **子进程写法**：`Bun.spawn([exe, ...args], { stdin: "pipe", stdout: "pipe", stderr: "pipe" })` → `proc.stdin.write(json)` + `proc.stdin.end()` → `await Promise.all([proc.exited, new Response(proc.stdout).text(), new Response(proc.stderr).text()])`（仓内实例 `test/lib/cli-process.ts:397-404`）。**Bun.spawn 无内建 timeout**，须手动 `setTimeout` + `proc.kill()`。
