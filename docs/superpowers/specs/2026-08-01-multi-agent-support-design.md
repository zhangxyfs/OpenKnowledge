# 多 Agent 支持设计（kimiCode + pi）

日期：2026-08-01
状态：已确认

## 1. 背景与目标

OpenKnowledge 目前只支持 kimiCode 一个 AI 编码 agent：hook 注入（`~/.kimi-code/config.toml` 的 TOML `[[hooks]]` 标记块）、技能分发（`~/.agents/skills/`）、GUI 引导页的状态检测与文案全部硬编码 kimi。

目标：

1. 支持第二个 agent：**pi**（`@earendil-works/pi-coding-agent`，项目在 `D:\develop\pi`）。
2. 抽象出一套 **Agent 适配器接口**，未来新增 agent 只需新增一个适配器文件并注册。
3. GUI 引导页增加 **agents 下拉菜单**，hook 配置卡与技能卡随所选 agent 联动。
4. CLI 增加 `--agent` 参数。

非目标：不改变 kimi 侧的任何既有行为（存量用户的 `config.toml` 不受影响）；不实现每 agent 独立技能目录（接口预留，当前共享）。

## 2. 关键调研结论

### 2.1 kimiCode 侧现状（迁移对象）

- hook 写入：`setupx.UpsertHooksBlock` 往 `~/.kimi-code/config.toml` 写入 3 条 `[[hooks]]`（UserPromptSubmit / PostToolUse / Stop），标记块 `# >>> openknowledge hooks >>>` 幂等 upsert，写前备份为 `.bak-openknowledge`。
- 自愈：`EnsureHooksBlock` + `hook.go` 的 `selfHealHooks()`（kimi 有时会清掉标记注释行，入口检测缺失则重写，fail-open）。
- 技能：6 个 SKILL.md 模板（5 个内联 + 1 个 `//go:embed`），`{{EXE}}` 烘焙 ok.exe 绝对路径，写入 `~/.agents/skills/<name>/SKILL.md`。
- 状态检测：`apiStatus` 检查标记块存在 + 硬编码 5 个技能目录（漏 wiki，随本次修复）。

### 2.2 pi 侧机制

- pi **没有声明式 hook 配置**，但有 TypeScript 扩展系统：往 `~/.pi/agent/extensions/` 放 `.ts` 文件即全局加载（不受项目信任门控）。
- 事件映射：`before_agent_start` ≈ UserPromptSubmit；`tool_result` ≈ PostToolUse；`agent_settled` ≈ Stop。
- 扩展内 `pi.exec(cmd, args, {timeout})` 可调外部 exe；`pi.sendMessage()` 可注入用户消息。
- 技能：pi 直接读 `~/.agents/skills/`（与现有共享目录兼容）及 `~/.pi/agent/skills/`。
- 检测：`~/.pi/agent` 目录存在，或 `pi --version` 可用；`PI_CODING_AGENT_DIR` 环境变量可覆盖配置根目录。
- 扩展异常只记 `pi-debug.log` 不打断会话，天然 fail-open。

## 3. 架构：Agent 适配器注册表（方案 A）

新建 `internal/agentx` 包：

```
internal/agentx/
  agentx.go        # Agent 接口 + 注册表（All/Find/Detected）
  kimi.go          # kimi 适配器：现有 setupx 的 hook 逻辑原样迁入
  pi.go            # pi 适配器：写/删 ~/.pi/agent/extensions/openknowledge.ts
  pi_extension.ts  # pi 扩展模板，//go:embed 嵌入，{{EXE}} 烘焙
```

### 3.1 接口

```go
type Agent interface {
    ID() string                  // "kimi" / "pi"，CLI/GUI/API 统一标识
    DisplayName() string         // "Kimi Code" / "Pi"
    Detect() bool                // kimi：KimiHome() 目录存在；pi：~/.pi/agent 存在或 pi --version 可用
    HooksInstalled() bool        // kimi：config.toml 含标记块；pi：扩展文件存在且内容指纹匹配
    InstallHooks(exe string) error
    RemoveHooks() error
    SkillsDir() string           // 当前两者都返回共享 ~/.agents/skills；接口预留每 agent 独立目录
}

func All() []Agent               // 注册表；新增 agent = 新适配器文件 + 在此登记
func Find(id string) (Agent, bool)
func Detected() []Agent
```

### 3.2 kimi 适配器

- `setupx` 中 hook 相关逻辑（`HooksBlockFor`、`UpsertHooksBlock`、`EnsureHooksBlock`、`StripLegacyOKHooks`、`KimiHome` 等）原样迁入 `kimi.go`，行为逐字不变。
- `setupx` 保留：技能模板与 `InstallSkills`/`Uninstall` 中的技能部分、通用路径工具。
- `hook.go` 的 `selfHealHooks()` 改为遍历 `agentx.Detected()` 逐个执行各自的 Ensure 逻辑（kimi 为标记块自愈；pi 为扩展文件缺失/指纹不符时重写）。

### 3.3 pi 适配器

- **安装** = 写 `~/.pi/agent/extensions/openknowledge.ts`（`PI_CODING_AGENT_DIR` 优先）。文件头带标记注释与内容指纹（模板渲染后 sha256 前 12 位）；`HooksInstalled()` = 文件存在且指纹匹配当前渲染结果（升级覆盖由此驱动）。写前若已存在非本工具生成的同名文件，备份为 `.bak-openknowledge`。
- **卸载** = 删除该文件（仅当指纹/标记确认为本工具生成）。
- `Detect()`：`~/.pi/agent` 目录存在，或 `pi --version` 可执行。

### 3.4 pi 扩展模板逻辑

| pi 事件 | 对应 ok 命令 | 扩展行为 |
|---|---|---|
| `before_agent_start` | `ok.exe hook prompt` | stdin 喂 `{"prompt": event.prompt, "cwd": ctx.cwd, "session_id": ctx.sessionId}`；stdout 非空则 `return { message: { customType: "openknowledge", content: stdout, display: false } }` |
| `tool_result` | `ok.exe hook post-tool` | 仅 write/edit 类工具触发，stdin 喂 `{"tool_input": {"path": ...}, "cwd": ...}`；fire-and-forget |
| `agent_settled` | `ok.exe hook stop` | stdin 喂 `{"cwd": ..., "session_id": ...}`；stdout 非空时 `pi.sendMessage()` 把自省提示注入会话，驱动 agent 完成 propose 自省 |

- **handler 零改动**：扩展把 pi 事件翻译成 kimi 形态的 stdin JSON，`internal/hook` 三个 handler、fail-open、daemon 转发全部复用。
- `pi.exec` timeout：prompt 10s、其余 5s，与 kimi 的 hook timeout 对齐。
- 已知语义差异：kimi 的 Stop hook 用 exit 2 阻断强制自省；pi 的 `agent_settled` 无法阻断，改用 `sendMessage` 注入提示达到对等效果。写入文档。

## 4. CLI

- `ok setup [--agent <id>]`：缺省时对 `Detected()` 的全部 agent 装 hook + 共享目录装技能（保持"一键全好"）；指定时只处理该 agent。未知 id 报错并列出可用 id。
- `ok init` 内部沿用同一路径，行为不变。
- `ok uninstall`：遍历注册表 `RemoveHooks()` + 删共享技能目录，语义扩展到所有 agent。
- 输出按 agent 分行展示成功/失败。

## 5. GUI 引导页联动

### 5.1 API（`internal/gui/api.go`）

- `GET /api/status`：`hooksInstalled: bool` 改为 `agents: [{id, name, detected, hooksInstalled}]`；`skillsInstalled` 保留（共享），并修复漏检 wiki 技能的 bug（改为按技能注册表动态检查 6 个）。
- `POST /api/setup/hooks`：请求体加 `{"agent": "<id>"}`；缺省时对全部已检测 agent 执行（兼容旧前端）。

### 5.2 前端（`web/index.html`、`web/app.js`）

- 引导页顶部新增 Agent 下拉（`<select>`），选项由 `agents` 数组渲染；未检测到的 agent 置灰并标注"未安装"。
- **hooks 卡联动**：徽标显示选中 agent 的 hook 状态；按钮文案"把 hook 写入 \<agent 名\> 配置"；点击提交当前选中 agent。
- **技能卡联动**：技能为共享目录，徽标显示共享安装状态；文案说明哪些 agent 可读（为将来每 agent 独立目录预留 UI 形态）。
- 全局开关、经验沉淀、卸载卡为全局项，不随下拉变化，视觉上与 agent 相关卡分组区分。
- 选中项存 `localStorage`，刷新保持。

## 6. 错误处理

- 保持两条铁律：fail-open（任何 agent 的 hook 安装/运行失败只记 ok.log，exit 0）；写入收敛（仅 setup/uninstall 路径写 agent 配置文件）。
- 单 agent 失败不影响其他 agent：遍历注册表时逐个收集错误，最后汇总报告。
- pi 配置目录不存在（未装 pi）→ `Detect()` 为 false，setup 跳过并提示，不算错误。
- kimi 侧既有防御（有头无尾标记块报错、备份）原样保留。

## 7. 测试

- `agentx`：kimi 适配器在临时 HOME 下做 InstallHooks/HooksInstalled/RemoveHooks 往返（现有 setupx 相关测试迁移）；pi 适配器在临时 `PI_CODING_AGENT_DIR` 下验证写文件、指纹识别、非本工具文件不删。
- 注册表：`Find`/`Detected` 行为。
- `pi_extension.ts`：模板渲染后无残留 `{{EXE}}`；TS 语法冒烟检查。
- CLI：`--agent` 参数解析、未知 id 报错。
- GUI：手动验证下拉联动（项目无前端测试设施）。

## 8. 文档

- `docs/ARCHITECTURE.md`：5.11 gui API 表、6.4 首次引导、9.1 hooks 章节、新增"多 agent 抽象"小节、18.3 环境变量（`PI_CODING_AGENT_DIR`）。
- README 提及支持 pi。

## 9. 未来扩展

- 新 agent：实现 `Agent` 接口 + 注册表登记一行；若 hook 机制不是配置文件而是其他形态，适配器内部自行处理（如 pi 的 TS 扩展）。
- 每 agent 独立技能目录：`SkillsDir()` 已预留，届时 GUI 技能卡改为按 agent 安装/检测。
- pi 的 npm 包分发形态（`pi.extensions` 清单 + `pi install`）可作为后续升级路径。
