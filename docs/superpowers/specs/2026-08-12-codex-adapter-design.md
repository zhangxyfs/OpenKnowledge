# codex 适配器设计（agentx 第七席）

日期：2026-08-12
状态：已获用户确认（post-tool 方案、写入位置、三处默认均经选择定稿）
路线：hooks.json 合并写 + 补丁头解析全量对齐 Claude 档位

## 背景与官方文档核实结论

用户需求：hooks 功能兼容 Codex——引导 Agent 列表加 Codex、制作 hook 适配器、
自动生成对应技能。

**官方文档核实（2026-08-12，learn.chatgpt.com/docs/hooks 与 /docs/build-skills，
用户代理环境 WebFetch 直读）**：

1. **hook 契约 Claude 兼容**：Codex hooks 逐字采用 Claude Code 事件名与输入输出
   协议——stdin 一个 JSON（`session_id`/`transcript_path`/`cwd`/
   `hook_event_name`/`tool_name`/`tool_input` 等，Codex 扩展 `model`/`turn_id`）；
   注入走 `hookSpecificOutput.additionalContext`（UserPromptSubmit 等事件适用）；
   Stop 阻断走 `decision:"block"` + `reason`（语义与 Claude 相同：reason 作为新
   提示让 Codex 继续跑）。
2. **配置位置**：用户层 `~/.codex/hooks.json` **或** `~/.codex/config.toml`
   inline `[hooks]` 表（同层混用两机制会合并并启动告警，官方建议每层只用一种）；
   另有仓库级 `<repo>/.codex/hooks.json`。
3. **技能目录**：Codex 原生扫描 USER 作用域 `$HOME/.agents/skills`——与 ok 的
   共享技能目录 `agentx.SkillsHome()` **就是同一个目录**（opencode 同款零适配）。
   `~/.codex/skills` 不存在。SKILL.md frontmatter 要求 `name`+`description`，
   与 ok 现有模板格式兼容。
4. **信任门**：非受管 hooks 按内容哈希记录信任，首次运行提示用户审查（`/hooks`
   命令管理）；内容变更（重装/自愈重写）哈希变化会再次询问。一次性自动化可用
   `--dangerously-bypass-hook-trust`；`[features] hooks=false` 可整体关闭。
5. **写盘工具**：Codex 编辑文件走 `apply_patch`（`tool_input.command` 载补丁
   文本），无 Claude 式 Write/Edit；shell 工具在 hooks 接口暴露名为 `Bash`。
   handler 仅 `type:"command"` 被执行（`prompt`/`agent` 类型解析但跳过）。

**推论**：现有六适配器（kimi/pi/zcode/reasonix/opencode/claude）之后，Codex 是
协议适配成本最低的一席——输出协议零改动、技能分发零改动、GUI 零改动，唯一新
逻辑在 hook 输入侧（apply_patch 补丁头解析）。

## 已核实的关键前提

- **输出协议零成本复用**：`ok hook prompt claude` 的 `hookSpecificOutput.
  additionalContext` JSON（`internal/hook/hook.go:85`）与 Stop 的
  `decision:block` JSON（`hook.go:99`）即 Codex 标准协议——hook 命令第三参数
  继续用 `"claude"`，`hook.go` 输出层、`cmd/ok/main.go` format 分发、daemon
  `?format=` 透传全部零改动。
- **技能分发零接线**：`setupx.InstallSkills` 写入目标是全部已检测 agent 的
  `SkillsDir()` 去重并集（`internal/setupx/setupx.go:35-80`）；codex 适配器
  `SkillsDir()` 返回共享 `SkillsHome()` 后，6 个 openknowledge-* 技能自动覆盖
  Codex——"自动生成对应技能"无需任何新代码。
- **GUI/CLI 零接线**：引导页下拉由 `state.status.agents` 动态填充
  （`web/app.js:607-678`），`ok setup --agent` 名单由 `agentIDs()` 动态生成
  （`internal/cli/setup.go:24,149-156`）——注册即出现。
- **fail-open 铁律继承**：`project.FromCwd` 失败静默 `return 0`
  （`hook.go:159-162`），hooks 装入全局 hooks.json 后未注册项目无副作用。

## 设计

### 1. 集成形态

新增一个文件 `internal/agentx/codex.go`，`init` 中 `Register`。注册表自动驱动
`ok setup --agent codex`、GUI agents 数组、引导页下拉、hook 自愈遍历、doctor、
setupx 技能分发与卸载——与现有六适配器同构，零额外接线。

### 2. 配置目标与格式

目标：`~/.codex/hooks.json`（用户层，独立 JSON 文件——官方建议每层一种机制；
不动用户 config.toml；不使用仓库级——ok 是用户级一次性安装模型）。结构：

```jsonc
{
  "hooks": {
    "UserPromptSubmit": [{"matcher": "*", "hooks": [
      {"type": "command", "command": "\"D:/.../ok.exe\" hook prompt claude", "timeout": 30}]}],
    "PostToolUse":      [{"matcher": "apply_patch", "hooks": [ /* hook post-tool claude */ ]}],
    "Stop":             [{"matcher": "*", "hooks": [ /* hook stop claude */ ]}]
  }
}
```

- 命令为 **shell 字符串**，形态与 claude 适配器相同：`strconv.Quote` +
  正斜杠 exe（`claude.go:53-55` 同款；Codex 官方示例亦为 shell 字符串）。
- `timeout` 秒级（Codex 默认 600），复用全局 `HookTimeoutSec()`。
- **ok 条目识别**：命令串以 ` hook <prompt|post-tool|stop> claude` 结尾判定
  （不看 exe basename——改名/迁移/测试二进制不影响识别）。
- PostToolUse matcher `apply_patch`：只追专用写盘工具，不追 `Bash`（与 claude
  不追 Bash 对齐——Bash 九成调用不写文件，且 shell 命令串无法可靠解析写盘
  目标）。
- **探针待验证项**（实施期真机实测，仿 claude 适配器探针流程）：
  1. UserPromptSubmit/Stop 的组级 matcher 形态（`"*"` 或省略——官方称 matcher
     仅部分事件支持过滤）；
  2. `apply_patch` 在 hooks 接口的精确 tool 名；
  3. Windows 下 Codex hook 命令的 shell 执行形态（cmd 接受正斜杠路径与否）。

### 3. CodexHome 优先级

```
OK_CODEX_HOME（ok 自留测试隔离口，OK_CLAUDE_HOME 同款命名）
  > CODEX_HOME（Codex CLI 官方重定位环境变量）
  > ~/.codex（os.UserHomeDir() 跟随重定向——与各适配器 XxxHome 一致，
    刻意不免疫 shadow HOME：自愈最坏写 shadow 副本，真实配置无风险）
```

`Detect()`：`CodexHome()` 目录存在（Codex CLI 与 IDE 扩展共享此目录）。

### 4. hook 包补丁头解析（唯一的新逻辑）

- 新增 `Event.PatchPaths()`：从 `tool_input.command`（兼容字符串与数组两种
  JSON 形态）按行扫描，提取 apply_patch 头标记后的路径：
  `*** Add File:` / `*** Update File:` / `*** Delete File:` / `*** Move to:`
  （move 语义下 Update 与 Move to 两个路径都算触碰）。
- `HandlePostTool`（`hook.go:175`）改为多路径：现有 `FilePath()`（path /
  file_path，kimi/claude/zcode 行为不变）与 `PatchPaths()` 合并去重，逐条调
  `TrackTouched`——`TrackTouched` 本身签名与逻辑不动。
- **相对路径处理**：补丁头路径相对 cwd，先 `filepath.Join(ev.Cwd, p)` 再进
  `relativize()`，否则项目前缀匹配不上全部 skip。
- 收益：Codex 上 auto 自省提醒（`core.go:194` 要求 `len(st.Touched)>0`）与
  enforce `changelog_required` 规则（按触碰路径匹配）与 Claude 完全同档。

### 5. 接口实现要点（agentx.Agent 九方法）

- `ID()`：`"codex"`；`DisplayName()`：`"Codex"`。
- `HooksTarget()`：`~/.codex/hooks.json` 展示路径。
- `SkillsDir()`：`agentx.SkillsHome()`（共享 `~/.agents/skills`）。
- `InstallHooks()`：加载 → 剥离 ok 旧条目 → 追加三事件组 → 备份写回。
- `RemoveHooks()`：后缀识别剥离；空组连带移除、空事件键删除；第三方条目
  原样保留。
- `EnsureHooks()`：与 claude 同语义——从未安装不复活（尊重用户显式移除）；
  装过但过期（exe 迁移/超时变更）则重写。
- `HooksInstalled()`：三事件均为当前期望形态（command=exe、matcher 与
  timeout 正确）。

### 6. 合并写纪律（claude 同款铁律）

- 写前 `.bak-openknowledge` 备份；解析失败报错**不覆盖**损坏文件；
- map 合并写保留未知字段（key 重排代价可接受，同 claude 既有注释）；
- 全部路径 fail-open。

### 7. 信任门 UX（仅文档，不做 GUI 特判）

Codex 首次运行会提示审查/信任新 hooks（哈希记账；exe 迁移自愈重写后哈希变化
会再次询问）。**不做** reasonix 式 GUI 专属卡片（`renderGuide` 唯一 id 特判
保持单例）——信任提示是 Codex 对所有 hooks 的标准行为，写进 README/官网/
ARCHITECTURE 说明即可。

### 8. 测试（三条铁律沿用）

- 测试隔离口 `OK_CODEX_HOME`。
- **全仓遍历注册表的 12 个测试文件必须补隔离**（eb98f8c 清单照抄：
  cmd/ok daemon+integration、agentx opencode_test、cli 三件、daemon server、
  gui api、hook、rxext serve、setupx 两件）——知识库已沉淀坑，违反则真实写入
  用户 `~/.codex/hooks.json`。
- `codex_test.go`：安装/移除/幂等/自愈（exe 迁移）/第三方条目保留/损坏文件
  不覆盖/CodexHome 优先级/三事件形态断言（镜像 `claude_test.go`）。
- hook 包：`PatchPaths` 单测（多文件、move 双路径、delete、字符串/数组形态、
  非补丁输入为空）+ `HandlePostTool` 相对路径 join cwd 用例。
- 真机探针：§2 三项待验证 + 信任流程 + 注入可见 + stop 阻断生效；探针时导出
  全部 agent home 隔离变量（已沉淀坑）。

### 9. 版本与文档

- 版本：新 agent = minor bump → **2.12.0**；`installer/openknowledge.iss`
  AppVersion 单一事实源 + `scripts/sync-version.sh` 同步 README/官网徽标
  （知识库已沉淀：徽标漂移坑）。
- `docs/changelogs/2.12.0.md`、`docs/ARCHITECTURE.md` §9.2（适配器表加行 +
  接口注释 + 注册表说明）、README 中英（多 Agent 行 + setup 描述）。
- 官网：`site/index.html` 功能卡、`site/docs.html` setup 枚举与引导卡、
  `site/changelog.html` 新区块、`site/assets/site.js` 英文字典键。
- 收尾按约定更新 agentx wiki 条目；探针若踩新坑走 openknowledge-propose。

## 范围之外（YAGNI）

- **不接 SessionStart**：现有六适配器零接入（全仓核实）——注入挂逐条消息是
  因为知识库会变/项目会切，双挂则首条消息重复注入。Codex 不特例。
- **不追 Bash 工具**（§2 已述，与全 agent 对齐）。
- **不做 GUI 信任提示卡**（§7 已述）。
- **不写 config.toml inline / 仓库级 hooks**（官方建议每层一种机制；ok 是
  用户级安装模型）。
- 不动 hook 输出层、main.go format 分发、daemon（协议已兼容）。
- Codex PermissionRequest/SubagentStart 等其余事件不接入（ok 无对应链路）。
