# OpenKnowledge 使用帮助

OpenKnowledge 是 AI 编码助手的**本地知识库**：把项目经验、规约、结构文档存下来，在你下次提问时自动注入给 AI——换一次会话、换一个工具，知识都还在。

## 30 秒上手

1. 安装后运行 `ok setup`（或在 GUI 引导页选你的 agent 点"安装"）
2. 到你的项目目录运行 `ok init`（自动用目录名注册，无需起名字）
3. 正常和 AI 对话即可——相关知识会随你的提问自动出现在上下文里

## 怎么调用

### 在 AI 会话里（最常用）

- **知识注入是全自动的**：每次你提问，相关知识摘要自动进入上下文，无需任何操作
- **斜杠技能**（各 agent 均支持，也可用自然语言，如"初始化知识库""把项目沉淀成 wiki"）：

- `/openknowledge-init`——初始化当前项目（等价 `ok init`）
- `/openknowledge-propose`——把本次会话的经验提议为草稿条目（待你批准）
- `/openknowledge-wiki`——生成/增量更新项目 wiki（结构文档）
- `/openknowledge-capture`——查看/切换经验沉淀模式与轮次间隔
- `/openknowledge-on` / `/openknowledge-off`——全局开启/关闭知识库 hooks

- **生效时机**：kimi / pi / zcode 的 hook 配置在**新开会话**时加载；reasonix 以插件形式安装，新会话生效（会话中 `/reload` 可重载）

### GUI（`ok gui` 或托盘双击）

- **管理**：条目增删改查、草稿"采纳"、按项目/类型/分支过滤、搜索
- **引导**：安装/重装各 agent 集成、hook 超时、enforce 三档（reasonix）、embedding 语义检索
- **其他**：数据导出/导入（zip 备份）、更新日志、本帮助

### CLI 速查

- `ok init`——注册当前项目（自动取目录名）
- `ok setup [--agent <id>]`——写 hooks/插件 + 装技能 + 配 embedding（交互向导）
- `ok add --title T --type note`——直接添加条目（`--tags/--summary/--mandatory/--force/--file`）
- `ok propose` / `ok approve`——沉淀草稿 / 采纳草稿
- `ok backfill-born`——按当前分支回填存量条目的 born 标签（预览确认，已有值不覆盖）
- `ok list` / `ok search <词>`——列条目 / 检索
- `ok capture`——查看或设置沉淀模式（propose/auto、轮次间隔）
- `ok wiki status / mark / base / diff`——wiki 状态 / 记游标 / 基准分支 / 分支结构差异
- `ok doctor`——体检：逐 agent 的 hooks 安装状态
- `ok on` / `ok off`——全局开关（off 后所有注入与检查暂停）
- `ok gui`——打开管理界面

### daemon 与托盘

daemon 常驻后台、按需自动拉起，无需手动管理；托盘图标右键看版本/退出，双击聚焦 GUI 窗口。

## 怎么配置

### 全局配置 `~/.openknowledge/config.toml`

- `[hooks] timeout_sec`（默认 10）——hook 超时秒数，过短在高负载下会被宿主静默杀死。改法：GUI 引导页"hook 超时"卡（保存后自动重写所有 agent），或手改后重跑 `ok setup`
- `[embedding]`（语义检索，可选；不配则纯关键词检索，照样可用）——三套服务配置（profile）存一处、`active` 指定"使用中"，三种形态任选：
  - **自定义（openai）**：任何 OpenAI 兼容服务（线上或自建）。填 `base_url` / `model` / `api_key`（key 可留空适配无鉴权本地服务，也可用 `api_key_env` 指向环境变量）
  - **Ollama**：先安装 Ollama 并 `ollama pull bge-m3`（或其他 embedding 模型），然后填 `base_url`（默认 `http://127.0.0.1:11434`，也可是局域网地址）与模型名，免 key；GUI/CLI 会自动探测已拉取的模型列表
  - **内置（builtin，仅安装版）**：ok 托管的 llama.cpp 本地模型，**完全离线、知识不出本机**。选模型（4 档，默认 Qwen3-Embedding-0.6B 约 639MB，各档体积约 146MB–639MB）→ 选镜像（默认 hf-mirror 国内镜像）→ 下载 → 设为使用中。首次拉起需数秒到一分钟，期间自动降级为关键词检索、就绪后自动恢复语义检索；**切换模型后跑一次 `ok index` 重建向量**（会提示）。模型目录默认 `<安装目录>/models`，弹窗"模型目录"行可改（`[embedding] models_dir`；已有模型文件不随迁，状态按新目录判定）并可直接"打开文件夹"
  - 改法：GUI 引导页 embedding 卡"配置…"弹窗（左列表右表单，显式"设为使用中"），或 `ok setup` 交互向导（三选一，带下载进度与连通性验证）；`timeout_sec` 默认 5 秒
- `[reasonix] enforce_mode`（默认 mixed）——reasonix 强制检查表达：soft 全软提示 / hard 全硬阻断 / mixed 软+硬。改法：GUI 引导页三档卡（仅选中 reasonix 时显示），**即时生效**

### 项目配置（知识库根目录 `config.toml`）

- `[capture] mode`（默认 propose）——沉淀模式：propose=AI 自主判断 / auto=到间隔自动提醒。改法：GUI 管理页"经验沉淀"卡，或 `ok capture`
- `[capture] turn_interval`（默认 5）——auto 模式的提醒间隔（回合数）。改法：同上
- `[provenance] auto_born`（默认 true）——新建条目自动记录出生分支（born 标签）。改法：GUI 管理页"经验沉淀"卡 checkbox，或手改文件
- `[enforce]`（默认空）——强制检查规则（如 changelog_required：改了代码必须更新 CHANGELOG，否则阻断）。改法：手改文件
- `[wiki] stale_commits`（默认 20）——wiki 落后多少 commit 开始提醒；0 = 关闭提醒。改法：手改文件
- `[retrieve] top_n`（默认 2）——每次注入最多检索命中条数。改法：手改文件
- `[inject] max_tokens`（默认 800）——注入预算（超出截断）。改法：手改文件

### 条目级控制（frontmatter）

- `mandatory: true` → 每会话首次提问**全文注入**（规约类内容用）
- `draft: true` → 草稿，不参与检索注入；GUI 点"采纳"或 `ok approve` 转正
- `tags` 含 `branch:<分支名>` → 该条目只在对应分支注入（2.7+，长期并行分支用）
- `tags` 含 `born:<分支名>` → 出生分支溯源（2.8+，自动记录，只展示不过滤）

## 常见问题

- **注入没出现**：①`ok doctor` 看 hooks 是否安装；②agent 必须是**新开会话**（hook 配置在会话启动时加载）；③看日志 `~/.openknowledge/ok.log`
- **想临时停用**：`ok off`（全部链路暂停），`ok on` 恢复
- **数据都在哪**：`~/.openknowledge/`——`registry.toml`（项目注册表）、`projects/<项目>/knowledge/*.md`（条目真源）、`kb.db`（索引，删了会自动重建）、`state/`（会话状态）
- **备份/迁移**：其他页"数据导出"存 zip → 另一台机器"数据导入"（索引自动重建）
- **切了 git 分支**：wiki 是分支感知的（2.6+）——注入会提示"wiki 基于 <基准分支>"；长期并行分支用 `/openknowledge-wiki` 在该分支生成差异条目（2.7+），互不影响
- **卸载**：Windows"应用与功能"里卸载 OpenKnowledge——会清理 hooks/插件登记/技能/embedding 配置并停 daemon；知识库数据保留在 `~/.openknowledge/` 可手动删
- **已知限制**：用 2.7.1 及更早版本执行过 `ok wiki mark/base` 会丢弃 merges 合并谱系（不崩，但谱系清空）；升级后用 `ok wiki status` 可重新检出积累

## 更多

- 架构与实现细节：仓库 `docs/ARCHITECTURE.md`
- 各版本更新内容：本页"更新日志"卡
