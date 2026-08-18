# 条目 AI 优化 + 模型配置（GUI）设计

日期：2026-08-17 · 状态：待批准 · 目标版本：待定

## 背景与目标

知识条目靠人工/AI 会话沉淀，质量参差（措辞啰嗦、摘要复读标题、重点不突出）。在 GUI 管理页编辑条目时提供「优化」按钮：调用用户自配的大模型，结合项目上下文润色条目。模型服务配置走引导页新卡片管理，跨项目共用。

已确认的决策（用户逐项拍板）：
- 弹窗样式：**变体 C 改版**——卡片列表 + 行内展开编辑；「＋ 添加配置」在标题下方；不使用单选框，使用中卡显示绿色「使用中」徽标、非使用中卡显示「设为使用中」按钮
- 优化结果：**先弹对照预览，确认后回填编辑表单**；`.md` 只在用户点「保存」时经既有 writeEntry 路径落盘——优化本身不碰磁盘（2026-08-17 与用户确认：不多开写盘路径，避免生命周期字段/mtime/索引同步行为分叉）
- 配置存储：全局 `config.toml` 新增 `[llm]` 段（active + profiles），**跨项目共用**
- 系统提示词要求：结合项目功能上下文、最简洁语言、不丢重点；优化时覆盖条目全部信息（标题/摘要/tags/正文）
- 未配置模型时点「优化」：弹提示（含「去配置」跳引导页），不静默失败
- UI 原型：`web/prototype-model-config.html`（已定稿变体 C 改版；**正式打包前删除该文件**，web/ 整目录随包分发）

## 范围

- 引导页新增「模型配置」卡（样式同 embedding 卡：标题 + 状态徽标 + 描述行 + 「配置…」按钮）+ 配置弹窗
- 管理页编辑条目弹窗「关闭」后加「✨ 优化」按钮 + 对照预览弹窗 + 未配置提示弹窗
- 后端：llm profiles 存取、连通性测试、entry optimize 接口
- 明确不做：CLI 不加 optimize 子命令（YAGNI）；优化不自动落盘；批量优化不做

## 配置模型（config 包）

`config.toml` 新增（全局层，`registry.Home()/config.toml`）：

```toml
[llm]
active = "DeepSeek 官方"

[[llm.profiles]]
name = "DeepSeek 官方"
kind = "openai"            # openai | anthropic（仅两种，其余 400）
base_url = "https://api.deepseek.com/v1"
model = "deepseek-chat"
api_key = "sk-..."
```

- `Config.LLM`：`Active string` + `Profiles []LLMProfile`；`Default()` 零值（无配置）
- 写入纪律沿用刚沉淀的教训：**必须走 `setupx.updateGlobalConfig`（锁内 LoadMerged→改→fsx.WriteFile 原子写）**，禁止裸读改写；GUI 的 llm 保存/删除/设 active 全部收口到这一条路径
- api_key 不落日志、不出现在 GET 响应明文（回显掩码 `sk-***`，提交时空值=保持不变）

## 后端

### llmx 包（新，仿 embedx 收口）

- `Client`：按 kind 分两种协议
  - openai：`POST {base_url}/chat/completions`，`Authorization: Bearer`
  - anthropic：`POST {base_url}/v1/messages`，`x-api-key` + `anthropic-version: 2023-06-01`
- 超时：`TimeoutSec<=0` 钳 5s（沿用 v2.18.2 embedx 教训）；默认 30s（生成比 embed 慢）
- `Chat(ctx, system, user) (string, error)` 单轮非流式即可
- `Test(ctx)`：发一条极短 ping（max_tokens=1）验证连通与鉴权

### GUI API（internal/gui/api.go）

- `GET /api/llm` → `{active, profiles:[{name, kind, base_url, model, api_key: "***掩码"}]}`（全局配置，无 project 参数）
- `POST /api/llm/profile`（新增/覆盖保存，按 name 定位）、`POST /api/llm/delete`、`POST /api/llm/active`、`POST /api/llm/test`（表单直传测未保存配置）
- `POST /api/entry/optimize`：入参 `{project, file, title, tags, summary, body}`（表单当前值，不是盘上值——用户可能有未保存改动）
  1. 无 active profile → 409 `{error: "no_llm"}`，前端据此弹「尚未配置模型」
  2. **事实检索（关键步骤，先于优化）**：让优化扎根真实项目内容，防止"越优化越假"——
     - 从条目正文提取文件路径引用（`path` / `path:行号` 形态），到项目目录读对应文件的相关片段（按行号窗口或文件头部，合计截 ~3000 token）
     - 用条目 title+summary+body 截断后作查询，走项目知识库混合检索（复用 `db.QueryExBranch`）取 top 3 相关条目（排除自身），作为旁证事实
     - 加上项目名 + INDEX.md 主列表（截 ~2000 token）作为功能全貌背景
  3. 把「条目表单值 + 事实参照（代码片段/相关条目/INDEX 摘录）」一起交给 llmx.Chat，解析返回 JSON → `{title, tags, summary, body}` 回前端
  4. 不调 writeEntry、不写盘、不记 feedback

### 优化 prompt（系统提示词要点）

- 角色：OpenKnowledge 知识库条目编辑。解释条目模型：title（检索命中第一印象）、type（rule/pitfall/note/reference，**不允许改**）、tags（检索过滤维度，允许增删）、mandatory（**不允许改**）、summary（一句话进 INDEX/Wiki 目录，不复读标题）、body（正文）
- 任务：**先通读给定的事实参照（项目代码片段 + 相关条目 + INDEX 摘录），再据实优化**条目的 title/tags/summary/body
- 事实纪律（替代"不新增原文没有的事实"的朴素禁令——那条会让优化退化成文字游戏）：
  - 事实以「原文 + 事实参照」的并集为准，**参照优先**：原文与项目实际内容冲突（接口改名、路径迁移、版本过时）时按实际修正
  - 允许补充事实参照里存在的细节（真实的函数名、路径、参数），**不得添加原文与参照都没有的事实**
  - 参照未覆盖的原文内容保持原意，不得凭空"完善"
- 表达硬约束：最简洁语言；不丢技术重点（命令、路径、版本号、阈值、因果链）；保留中文；frontmatter 语义不变
- 输出：仅 JSON 对象 `{"title","tags","summary","body"}`，无 Markdown 围栏、无解释；解析失败按 502 返回原始输出片段供排障

## 前端（web/）

### 引导页：模型配置卡

- 位置：与 embedding/门控等同区（卡片网格自然落位）；徽标：有 active=「已配置」绿 / 无=「未配置」
- 描述行：当前使用中配置摘要（`名称 · 模型 · 类型`），无配置时「未启用（优化功能不可用）」

### 模型配置弹窗（变体 C 改版，样式类沿用 emb-* + 新增 llm-*）

- 标题行：「模型配置」+ ×；标题正下方「＋ 添加配置」（虚线边）
- 每个 profile 一张卡：绿点（使用中）+ 名称 + 类型徽标（openai 蓝紫 / anthropic 粉）+「使用中」绿徽标 **或**「设为使用中」按钮 +「编辑/收起」
- 展开表单：名称 / 类型（二选一下拉）/ base_url / 模型 / api_key（password，掩码回显，留空=不变）；按钮：保存（主色）、测试（结果显示行内 ok/err）、删除（右侧红色，confirm 确认；删除 active 卡→ active 置空）
- api_key 掩码提交语义：值为掩码或空 → 后端保留原 key

### 管理页：编辑弹窗「✨ 优化」

- 位置：「关闭」右侧，紫边样式（原型已定）
- **悬停浮窗**：鼠标停留显示优化方式说明，复用 `#summary-tip` 单例浮窗机制（跟随、滚动即收），文案：
  「结合项目真实代码与相关条目据实润色标题/标签/摘要/正文（类型与 mandatory 不动）；先出对照预览，确认回填后点保存才生效。」
- 流程：点击 → 收集表单当前值 → `POST /api/entry/optimize`（按钮 loading「优化中…」防重击）→ 成功弹对照预览
- **对照预览弹窗**：逐字段（标题/tags/摘要/正文）分块上下对照——旧值灰底带「原」标、新值白底绿边带「优化后」标；标题行右侧注明本次依据（真实代码片段 + 相关条目 N 条 + INDEX 摘录）；按钮「回填表单」（主色）/「放弃」，按钮区注明"回填只改表单，点保存才写入 .md"；回填只写表单控件，保存仍走原提交
- 409 no_llm → 弹「尚未配置模型」（去配置 → 关弹窗切引导 tab；知道了 → 关闭）
- 其他失败 → 错误横幅（复用 showError）

## 测试

- config：LLM 段 LoadMerged 默认值、updateGlobalConfig 路径写入后回读
- llmx：两种协议的请求头/body 构造（httptest 假服务）、超时钳制、错误码透传
- gui：`/api/llm` round-trip（掩码不回明文、空 key 保留原值）、delete active 置空、optimize 无配置 409、optimize 成功路径（httptest 假 LLM 返回 JSON）不落盘（断言 .md mtime 不变）
- 前端逻辑无单测框架，手测清单：卡布局、弹窗交互、对照预览回填、未配置提示跳转

## 风险与取舍

- 模型返回非 JSON / 围栏包裹：预处理剥围栏 + 严格解析，失败 502 带片段
- 事实检索三步的预算：代码片段 ~3000 token、INDEX ~2000 token、相关条目 top 3（各截 500）；条目正文不截（优化对象本体）。单轮总输入量级 ~8-10k token，常规模型上下文均够
- api_key 安全：仅 https 出站；配置文件 0600（fsx.WriteFile 已保证）；GET 掩码
- 原型文件 `web/prototype-model-config.html` 打包前删除（写进发布检查单）
