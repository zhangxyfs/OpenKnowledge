# claude 生态适配器设计（agentx 第五席）

日期：2026-08-12
状态：已获用户确认
路线：方案 C —— 本适配器立即实施 + MCP 设计结论沉淀进知识库（MCP server 本身不实施）

## 背景与实测结论

起因：用户发现部分 AI agent 助手（举例如 CodePilot）"不支持 hook"，问能否用 MCP 实现
OpenKnowledge 的 hook 等效功能。

**CodePilot 实测（2026-08-12，探针法）**：在 `~/.claude/settings.json` 埋入
`UserPromptSubmit` / `Stop` 两个探针 hook（命令向日志文件写标记），在 CodePilot 中
发送一条消息并完成回合——两个探针均触发。**结论：CodePilot 原生执行 Claude Code
用户级 hooks。**

机制链（源码核实）：

1. CodePilot（Electron + Next.js，agent 内核为官方 `@anthropic-ai/claude-agent-sdk`）
   正常查询 `settingSources` 始终含 `'user'`（DB provider 场景甚至只有 `'user'`）→
   加载 `~/.claude/settings.json`（`src/lib/claude-client.ts`）。
2. provider 隔离的 shadow HOME 只剥 `settings.json` 里 `env` 的 `ANTHROPIC_*` 键，
   `hooks` 字段原样继承（`src/lib/claude-home-shadow.ts` 的 `stripAuthEnv`）。
3. CodePilot 自身也在注册 programmatic hooks（PreToolUse 等），SDK hook 管线在主
   查询路径上是活的。

**推论**：这对原版 Claude Code 同样成立。现有四适配器（kimi/zcode/pi/reasonix）缺
Claude 生态一席——一个适配器同时覆盖 Claude Code 本体 + CodePilot + 一切 SDK 兼容宿主。

**hook→MCP 降级映射**（供未来真 hook-less agent 参考，本次不实施）：

| hook 链路 | MCP 复刻可行性 |
|---|---|
| InjectForPrompt（逐 prompt 自动注入） | 只能改为 agent 主动调 search 工具，需指令文件引导（弱保证） |
| TrackTouched（文件追踪） | 无等效物（MCP 无法感知 agent 文件操作） |
| CheckStop（自省/enforce 阻断） | 无 stop 时机，退化为指令文件软约定 |

## 已核实的关键前提

- **协议零成本复用**：`ok hook prompt claude` 输出的 `hookSpecificOutput.
  additionalContext` JSON（`internal/hook/hook.go:85`）与 Stop 阻断的
  `decision:block` JSON（`hook.go:99`）就是 Claude Code 标准协议——hook 侧
  代码一行不改。
- **共享配置安全性**：`project.FromCwd` 失败 → 静默 `return 0`
  （`hook.go:159-162`，post-tool/stop 同构）。hooks 装入全局 settings 后，
  未注册项目目录下使用 Claude Code/CodePilot 完全无副作用（fail-open 铁律）。

## 设计

### 1. 集成形态

新增一个文件 `internal/agentx/claude.go`，`init` 中 `Register`。注册表自动驱动
`ok setup --agent claude`、GUI agents 数组、引导页下拉、hook 自愈遍历——与现有
四适配器同构，零额外接线。

### 2. 配置目标与格式

目标：`~/.claude/settings.json` 的 `hooks` 字段（Claude Code 与 CodePilot 共享
此文件，装一次双宿主生效）。Claude Code 原生结构：

```jsonc
"hooks": {
  "UserPromptSubmit": [{"matcher": "*", "hooks": [
    {"type": "command", "command": "\"D:/.../ok.exe\" hook prompt claude", "timeout": 30}]}],
  "PostToolUse":      [{"matcher": "Write|Edit", "hooks": [ /* hook post-tool claude */ ]}],
  "Stop":             [{"matcher": "*", "hooks": [ /* hook stop claude */ ]}]
}
```

- 命令为 **shell 字符串**（区别于 zcode 的 process+args）：exe 路径用正斜杠 +
  双引号包裹——cmd.exe 与 bash 均可执行（探针实验已验证 cmd 接受正斜杠路径）。
- `timeout` 秒级，复用全局 `HookTimeoutSec()`。
- **ok 条目识别**：命令串包含 `hook prompt claude` / `hook post-tool claude` /
  `hook stop claude` 模式（不看 exe basename——改名/迁移/测试二进制不影响识别，
  与 zcode 同哲学）。
- PostToolUse matcher `Write|Edit` 为未锚定正则，与 zcode 行为一致（Edit 同时
  覆盖 MultiEdit/NotebookEdit）。

### 3. 接口实现要点（agentx.Agent 九方法）

- `ID()`：`"claude"`；`DisplayName()`：体现生态覆盖（Claude Code 及兼容宿主）。
- `Detect()`：`~/.claude` **或** `~/.codepilot` 目录存在（后者覆盖仅装
  CodePilot 的机器）。
- `SkillsDir()`：`~/.claude/skills`（Claude Code 原生加载；CodePilot 的
  skill-discovery 同样扫描）。
- `HooksTarget()`：`~/.claude/settings.json` 展示路径。
- `EnsureHooks()`：与 zcode 同语义——从未安装不复活（尊重用户显式移除）；
  装过但过期（exe 迁移/超时变更/旧格式）则重写。
- **合并写纪律**：strip 只删 ok 条目，第三方 hooks 原样保留；写前
  `.bak-openknowledge` 备份；解析失败不覆盖损坏文件；map 合并写 key 重排代价
  可接受（同 zcode 既有注释）。

### 4. 测试（v2.5.0 踩过的坑，三条铁律）

- 测试隔离口 `OK_CLAUDE_HOME`（仿 `OK_ZCODE_HOME`）。
- **全仓遍历注册表的测试（含 cmd/ok E2E）必须补隔离**，否则真实写入用户
  `~/.claude/settings.json`。
- 适配器单测覆盖：安装/移除/幂等/自愈/第三方条目保留/损坏文件不覆盖。

### 5. MCP 设计结论沉淀

适配器完成后用 `openknowledge-propose` 沉淀一条 reference 草稿，内容：

- MCP server 实现路径：`ok mcp` 子命令、stdio 传输、官方
  `github.com/modelcontextprotocol/go-sdk`、handler 复用 `internal/retrieve` /
  `internal/store`（不经 daemon 网络层）；
- 上文 hook→MCP 降级映射表；
- CodePilot 实测结论（原生 hooks 支持，无需 MCP 兜底）。

定位：未来真 hook-less agent 的预案，**不排期实施**。

### 6. 版本与文档

- 版本：`installer/*.iss` 单一事实源 bump minor（新 agent 支持为 feature），
  `scripts/sync-version.sh` 同步 README/官网徽标。
- `CHANGELOG.md` 新增条目；README（中英）agent 支持表格加一行。

## 范围之外（YAGNI）

- 不动 hook 核心代码（协议已兼容）。
- 不实现 MCP server（仅沉淀结论）。
- 不改 CodePilot 侧任何配置（零配置继承用户级 settings）。
