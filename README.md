<p align="center">
  <img src="docs/assets/logo.svg" alt="OpenKnowledge" width="580">
</p>

<p align="center">
  <b>简体中文</b> · <a href="README_EN.md">English</a> · <a href="docs/ARCHITECTURE.md">架构文档</a> · <a href="docs/changelogs/">更新日志</a>
</p>

<p align="center">
  <img alt="version" src="https://img.shields.io/badge/version-2.21.0-2563eb">
  <img alt="go" src="https://img.shields.io/badge/go-%3E%3D1.25-00ADD8">
  <img alt="platform" src="https://img.shields.io/badge/platform-windows%20%7C%20linux-0078d6">
  <a href="LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-green"></a>
</p>

<p align="center">
  为 AI 编程助手提供的<b>项目知识库</b>——知识按项目隔离，通过各 AI 助手的 hooks/扩展自动注入 AI 上下文，<br>
  并能强制执行"改代码必须写变更日志"这类工作流规则。单二进制 Go CLI（<code>ok</code>），零运行时依赖。
</p>

---

## 三分钟上手

### 1. 安装

| 平台 | 方式 |
|------|------|
| Windows | 运行 `OpenKnowledgeSetup-<版本>.exe`（免管理员，装到 `%LOCALAPPDATA%\Programs\OpenKnowledge`，卸载默认保留知识库数据） |
| Linux | `openknowledge_<版本>_linux_amd64.tar.gz` 解压后 `./ok setup`，或 `sudo dpkg -i openknowledge_<版本>_amd64.deb` |

> 安装包约 50MB，内含 llama.cpp CPU runtime（本地 embedding 用）；模型首次启用时按需下载。

### 2. 打开 Web GUI，完成引导配置

**双击 `ok.exe`（或执行 `ok gui`）**，浏览器自动打开管理界面 `http://127.0.0.1:17888`。首次使用会落在**引导页**，按卡片顺序点完即可：

1. **写 hooks**——给你机器上所有已检测的 AI 助手写入集成：Kimi Code、Pi、ZCode、Reasonix、opencode、Claude Code/CodePilot、Codex、Qoder CN（CLI + IDE）、DeepSeek Harness
2. **装技能**——`openknowledge-init / on / off / propose / capture / wiki` 六个技能写入各 agent 的技能目录
3. **配 embedding**——语义检索三选一：线上 OpenAI 兼容服务 / 本机 Ollama（免 key）/ 内置本地模型（完全离线，知识不出本机）；跳过则只用关键词检索，之后随时可补配

<p align="center">
  <img src="docs/assets/gui-guide.png" alt="引导页" width="860"><br>
  <sub>引导页：一键完成 hooks、技能与 embedding 配置</sub>
</p>

完成后进入管理页，日常的条目维护都在这里点点鼠标完成：

<p align="center">
  <img src="docs/assets/gui-manage.png" alt="管理页" width="860"><br>
  <sub>管理页：项目 / 条目列表、检索预览与全局开关</sub>
</p>

| 标签页 | 功能 |
|--------|------|
| **管理** | 项目/条目增删改、检索预览、全局开关；草稿条目带徽标，一键「采纳」转正 |
| **引导** | hooks、技能、embedding 一站配完（等价于命令行 `ok setup`），另有卸载卡片 |
| **其他** | 数据导出/导入（zip 备份恢复）、更新日志、删除项目知识库（三重确认） |
| **日志** | ok / 守护 / embedding 三来源实时日志，多选过滤 |

GUI 由全系统唯一的常驻 daemon 托管，关页面不退进程；`ok daemon stop` 可停止。不用图形界面的话，`ok setup` 是引导页的命令行等价物（幂等，可重复执行），`ok doctor` 可体检配置与 hooks 状态。

### 3. 初始化项目并生成 wiki

打开你的 AI 助手，**在需要知识库的项目目录里新开一个会话**（hooks 在会话启动时加载），然后依次用两个技能：

1. **初始化**——输入 `/openknowledge-init`（或直接说"初始化知识库"）。技能会把当前项目注册进知识库，名字自动取目录名，不用填任何参数
2. **生成 wiki**——再说 **"生成项目 wiki"**。wiki 技能扫描代码结构与 git 历史，把项目沉淀成一批 reference 条目（自动建索引与向量）；之后新功能、新模块定稿时说 **"更新 wiki"** 即可增量维护

> 高阶用法：这两个技能背后是 `ok init` 和 wiki 相关 CLI——技能就是命令的会话内封装，日常用自然语言即可，命令行留给脚本与排障（见[常用命令](#常用命令)）。

### 4. 日常使用：在 AI 会话里用技能

装好后基本不用记命令——自然语言会触发对应技能：

| 你说 | 发生的 |
|------|--------|
| "初始化知识库" / "把本项目注册到知识库" | 技能调 `ok init` 注册当前项目 |
| "生成项目 wiki" / "更新 wiki" | wiki 技能全量/增量沉淀项目结构 |
| "开启/关闭知识库" | 技能调 `ok on` / `ok off` 切全局开关 |
| "开启自动提取" / "调整提取频率" | 技能调 `ok capture` 切沉淀模式 |
| （踩坑解决、新需求定稿） | AI 主动问"要不要沉淀进知识库"，同意后 `ok propose` 记草稿，人批准后生效 |

同时 hooks 在后台自动做两件事，无需任何口令：

- **提问即注入**：每次提问按关键词 + 向量混合检索，把相关知识塞进 AI 上下文；首次提问还会带上强制约定全文（如"git 提交规范是什么"直接被引用回答）
- **改代码必写日志**：AI 改了代码没写变更日志，回合结束时被阻断提醒（同会话同规则只阻断一次）

手动维护条目也可以（GUI 管理页等价）：

```bash
ok add --title "Git 提交规范" --type note --tags git --file git.md
ok search 提交规范    # 命令行预览检索效果
```

<details>
<summary>更多截图（其他页）</summary>

<p align="center">
  <img src="docs/assets/gui-misc.png" alt="其他页" width="860"><br>
  <sub>其他页：数据导出 / 导入、更新日志与删除项目知识库</sub>
</p>

</details>

## 知识条目

每条知识是一个带 frontmatter 的 Markdown 文件（`ok add` 创建，也可手写），集中存放在 `~/.openknowledge/`，不污染项目仓库：

```markdown
---
title: 变更日志强制规则
type: rule              # rule | pitfall | note | reference
tags: [changelog, workflow]
mandatory: true         # true = 每会话首次提问全文注入
summary: 每次代码修改必须立即记录变更日志
---

正文（Markdown 自由格式）
```

**草稿流程（AI 提议，人批准）**：`ok propose` 记的草稿条目（`draft: true`）不参与检索与注入，只在 `ok list` 和 GUI 可见；确认后 `ok approve <文件>` 或 GUI「采纳」转正。沉淀模式用 `ok capture propose|auto` 切换：`propose` 为 AI 主动提议（默认）；`auto` 每隔指定轮次强制 AI 自省提取，间隔用 `ok capture interval <n>` 配置。

<p align="center">
  <img src="docs/assets/ai-session.png" alt="AI 会话中的经验沉淀" width="860"><br>
  <sub>实际会话：一次发布完成后，AI 主动沉淀 wiki 条目，并询问是否要把踩坑单独记为 pitfall（propose 模式）</sub>
</p>

## 常用命令

| 命令 | 作用 |
|------|------|
| `ok setup` | 首次引导：hooks 配置 + 技能安装 + embedding 配置 |
| `ok gui` | 启动 Web 管理界面（双击 exe 无参数运行同效） |
| `ok init [名字]` | 注册当前项目，并幂等写入/更新 hooks 配置 |
| `ok add` / `ok list` | 新建条目 / 列出项目与条目 |
| `ok propose` / `ok approve <文件>` | AI 提议草稿 / 批准转正 |
| `ok capture [propose\|auto\|interval <n>]` | 查看/切换沉淀模式与轮次间隔 |
| `ok search <词>` | 命令行预览检索效果 |
| `ok index` | 同步索引与向量（手改条目后执行） |
| `ok doctor` | 体检：配置、embedding 连通性、hooks 状态 |
| `ok on` / `ok off` | 全局开关 |
| `ok daemon [stop]` | 常驻进程管理（开机自启，一般无需手动操作） |

<details>
<summary>更多命令：wiki 游标 / 分支溯源回填</summary>

| 命令 | 作用 |
|------|------|
| `ok wiki status` / `mark` / `base` / `diff` | wiki 状态（JSON）/ 记游标 / 查看或设置基准分支 / 输出分支差异素材（wiki 技能内部使用） |
| `ok backfill-born` | 回填存量条目的 born 分支溯源标签（预览确认后写入，已有值不覆盖） |

</details>

<details>
<summary>配置：全局 / 项目两层 TOML 与 embedding profiles</summary>

生效配置 = 内置默认 ← 全局 `~/.openknowledge/config.toml` ← 项目 `~/.openknowledge/projects/<名>/config.toml`（逐层覆盖）。

```toml
# 全局配置（ok setup 可交互写入；GUI 引导页"配置…"弹窗可管理多套）
[embedding]
active = "默认"                    # 使用中 profile 名；空 = 只用关键词检索

[[embedding.profiles]]             # 形态一：线上/自建 OpenAI 兼容服务
name = "默认"
type = "openai"
base_url = "https://api.openai.com/v1"
api_key = "sk-..."                 # 或用 api_key_env 指向环境变量；无鉴权本地服务可留空
model = "text-embedding-3-small"

# [[embedding.profiles]]           # 形态二：本机/局域网 Ollama（免 key）
# name = "ollama"
# type = "ollama"
# base_url = "http://127.0.0.1:11434"
# model = "bge-m3"

# [[embedding.profiles]]           # 形态三：内置 llama.cpp 本地模型（离线，仅安装版）
# name = "内置"
# type = "builtin"
# model = "qwen3-emb-0.6b-q8"      # 清单内 4 档之一，GUI/CLI 下载后激活
# mirror = "hf-mirror"

# 项目配置：强制规则示例
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]                  # 改了这些 = 改了代码
changelog_glob = "docs/changelogs/**"     # 碰了这些 = 写了日志
message = "本次会话修改了代码但未更新变更日志，请先按规范补齐。"
```

</details>

<details>
<summary>深入：检索算法与工作原理</summary>

**检索**：SQLite + FTS5 混合检索，单文件 `kb.db` 零基础设施——

- 关键词通道：FTS5 + BM25，中文二元组分词；语义通道：embedding + 余弦相似度
- 准入与排序分离：两路独立准入，融合默认按名次（RRF），模型无关无需调参；宁缺毋滥、不强行凑 top_n
- 1 万条知识下单次查询约 30ms；embedding 不可用时自动降级纯关键词，注入永不缺席

**工作原理**（以 Kimi Code 为例，其他 agent 经各自适配器等价触发）：

| Hook | 作用 |
|------|------|
| `UserPromptSubmit` | 首次提问注入 mandatory + 索引；每次提问检索注入 |
| `PostToolUse`（Write/Edit） | 把 AI 改过的文件记入会话状态 |
| `Stop` | 改了代码没写日志 → exit 2 阻断（同会话同规则只阻断一次） |

所有 hook 路径 fail-open：任何内部错误只记日志（`~/.openknowledge/ok.log`），绝不影响正常会话。详见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

</details>

## 开发

```bash
go test ./...    # 25 个包全部测试（零网络，含端到端）
go vet ./...
```

```bash
# 维护者打包发布产物
bash scripts/build-dist.sh        # dist/ok.exe + dist/web/
bash scripts/build-installer.sh   # Windows 安装程序
bash scripts/build-linux.sh       # Linux tar.gz / .deb
```

| 文档 | 位置 |
|------|------|
| 架构文档 | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| 变更日志 | [docs/changelogs/](docs/changelogs/) |
