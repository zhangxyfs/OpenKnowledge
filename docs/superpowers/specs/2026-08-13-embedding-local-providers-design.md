# embedding 多服务配置与本地模型支持 设计文档

日期：2026-08-13
状态：已确认（UI 视觉稿经可视化伴侣审阅通过）

## 1. 背景与目标

现状：embedding 仅支持 OpenAI 兼容线上 API（`internal/embed` 单实现
`OpenAIClient`，配置为 `config.toml` 里平铺的 `[embedding]` 字段），语义检索
依赖外网，知识内容要发送到第三方服务。

目标：配置 embedding 时提供三种服务形态，用户可保存**多套配置**并切换其一为
"使用中"：

1. **自定义（OpenAI 兼容）**——即现有线上方式，保留原行为；
2. **Ollama**——本机或局域网 Ollama/LM Studio 等本地服务，免 API key；
3. **内置（llama.cpp）**——ok 自带推理运行时 sidecar + 按需下载 GGUF 模型，
   **完全离线可用，知识不出本机**（安全性动机）。

三种形态对检索链路同构——都是 OpenAI 兼容 `/v1/embeddings` endpoint；内置
= ok 托管的 localhost 服务。架构上与 daemon 进程模型（按需拉起、静默子进程）
一致，不引入 CGO，主构建管线不变。

## 2. 范围

### 包含

- 配置模型重构：`[[embedding.profiles]]` 多配置 + `active` 切换，旧平铺配置
  无损迁移
- GUI：引导页卡片改造（显示当前服务单行摘要 + "配置…"按钮）+ 配置弹窗
  （左列表右表单，仿"模型配置"式交互）
- 内置模式：llama.cpp 官方预编译 `llama-server` 随安装包分发；GGUF 模型按需
  下载（默认国内镜像，可换源）；daemon 托管 sidecar 生命周期
- Ollama 模式：地址可配，模型下拉自动探测已安装模型
- 模型身份管理：kb.db 记录建索引的模型身份，切换模型后语义通道跳过并提示
  重建（修复"换模型后语义分静默归零"的已有隐患）
- `ok index` 全量重建支持批量 embedding
- CLI `ok setup` 引导与 `ok doctor` 适配三形态
- 打包：runtime 进 Windows 安装包（iss）与 Linux deb/tar.gz（nfpm）

### 非目标（本期不做）

- GPU 变体（CUDA/Vulkan 运行时包）——CPU 版足够万级条目规模，体积翻倍不值
- 用户自选清单外任意 GGUF 文件——v1 清单制（钉死 repo/文件/sha256）
- 代用户安装 Ollama 或执行 `ollama pull`——只探测 + 提示命令
- 向量 ANN 索引/独立向量库——万级条目内存余弦已毫秒级
- macOS / arm 平台——与现有发布矩阵一致（win/amd64 + linux/amd64）
- 裸 exe 便携形态的内置模式——runtime 随安装包分发，便携形态下内置类型
  提示"仅安装版可用"（Ollama/自定义不受影响）

## 3. 核心架构决策

**D1 三形态同构**：检索链路只面向 `embed.Client` 接口 + base_url。Ollama 与
内置都只是预设了 base_url 的 OpenAI 兼容端点，链路代码零分叉；差异集中在
配置层与内置模式的进程/模型管理。

**D2 sidecar 独立进程，不引 CGO**：内置运行时 = llama.cpp 官方 release 预编译
二进制（Windows `llama-b*-bin-win-cpu-x64.zip`、Linux `llama-b*-bin-ubuntu-x64.tar.gz`
中的 `llama-server` 及依赖库），版本号在构建脚本中钉死。ok 主进程与交叉编译
管线完全不动，延续"无 CGO、单二进制"形态（sidecar 是独立文件，非链接依赖）。

**D3 daemon 托管 sidecar**：hook 进程短生命周期且有超时预算（默认 10s），
绝不承担拉起/看护职责。常驻 daemon 负责 sidecar 的拉起、崩溃有界重启、空闲
回收；hook/cli 只发 HTTP，连不上立即 fail-open 降级纯 BM25（现状语义不变）。

**D4 模型身份显式化**：kb.db 新增 meta 表记录建索引时的模型身份与维度；查询
时与当前配置不符即跳过语义通道并提示重建索引，取代现有的"维度不等余弦返回 0
静默归零"行为。

## 4. 配置模型

```toml
[embedding]
active = "内置 bge-m3"      # profile 名；空 = 未配置（纯关键词检索）
timeout_sec = 5             # 沿用，三形态共用

[[embedding.profiles]]
name = "SiliconFlow"
type = "openai"             # openai | ollama | builtin
base_url = "https://api.siliconflow.cn/v1"
model = "BAAI/bge-m3"
api_key = "..."
api_key_env = ""            # 沿用 env 间接引用

[[embedding.profiles]]
name = "Ollama 本机"
type = "ollama"
base_url = "http://localhost:11434"   # 可改局域网地址
model = "bge-m3"

[[embedding.profiles]]
name = "内置 bge-m3"
type = "builtin"
model = "bge-m3-q4_k_m"     # 内置模型清单 id
mirror = "hf-mirror"        # hf-mirror | huggingface | 自定义 base URL
```

- **迁移**：加载时检测到旧平铺字段（`base_url`/`model`/`api_key` 直接挂在
  `[embedding]` 下）且有值 → 自动生成 `type = "openai"` 的 profile（名称
  "默认"）并置为 `active`，写回后旧字段清除；无值则视为未配置。配置仍是
  全局 `~/.openknowledge/config.toml` 与项目级合并（项目级覆盖全局），
  profile 列表按同名覆盖合并。
- **模型身份串**（写入 kb.db meta，用于 D4 比对）：
  `openai:<model>@<base_url>` / `ollama:<model>@<base_url>` /
  `builtin:<model_id>`。

## 5. 内置模式：运行时与模型

**运行时**：构建脚本从钉死的 llama.cpp release 下载对应平台压缩包，解出
`llama-server`（及依赖 dll/so）放入 `dist/runtime/`，安装包落到
`{安装目录}/runtime/`。sidecar 启动参数：`-m <gguf路径> --embeddings
--port <随机空闲端口> --host 127.0.0.1`，pooling 以模型卡为准显式指定
（bge-m3 为 CLS；`/v1/embeddings` 要求 pooling 非 none）。
`/v1/embeddings` 端点已验证 OpenAI 兼容，输出 L2 归一化向量，支持数组
批量输入。

**模型清单**（烘焙进二进制；每条含 id/repo/文件/size/sha256/维度/pooling/
query_prefix/doc_prefix，实现时钉死具体值）：

| id | 模型 | 大小量级 | 维度 | pooling | 说明 |
|---|---|---|---|---|---|
| `qwen3-emb-0.6b-q8` | Qwen3-Embedding-0.6B Q8_0 | ~639MB | 1024 | last | **默认推荐**：官方 GGUF，中文+代码检索强；查询侧加 Instruct 前缀（+1~5%） |
| `bge-m3-q4_k_m` | BAAI/bge-m3 Q4_K_M | ~400MB | 1024 | cls | 下载最小，中英双语成熟 |
| `bge-m3-q8_0` | BAAI/bge-m3 Q8_0 | ~600MB | 1024 | cls | bge 质量档 |
| `nomic-embed-q8` | nomic-embed-text v1.5 | ~150MB | 768 | mean | 最小，英文为主 |

已验证来源：`Qwen/Qwen3-Embedding-0.6B-GGUF`（官方，Q8_0=639MB）、
`gpustack/bge-m3-GGUF`（社区下载量最高）、`ggml-org/bge-m3-Q8_0-GGUF`
（llama.cpp 官方 org）、nomic 官方 GGUF。Qwen3-Embedding 4B/8B 不进内置
清单（CPU 推理超出 hook 超时预算），需要大模型走 Ollama/自定义形态。

**下载**：
- 源：`hf-mirror`（`https://hf-mirror.com`，国内默认）| `huggingface` 官方 |
  自定义 base URL；URL 模板 `<mirror>/<repo>/resolve/main/<file>`。
- 行为：默认下载到 `<安装目录>/models/<id>.gguf.part`（`[embedding] models_dir`
  可改，GUI 弹窗可直接修改/打开文件夹；已有模型文件不随迁），完成后校验
  size + sha256，原子改名；支持断点续传（HTTP Range）；GUI/CLI 可取消，
  `.part` 保留供续传；进度经 GUI API 轮询展示。
- 下载中/未下载的内置 profile 可保存，但"设为使用中"与"测试"要求模型就绪。

## 6. sidecar 生命周期（daemon 托管）

- **状态文件**：`~/.openknowledge/embed-sidecar.json`（port / pid / model_id /
  started_at / last_used），hook/cli 读它发现端口。
- **拉起**：daemon 检测到 active profile 为 builtin 且模型就绪 → 拉起
  sidecar，等待 `/health` 就绪后写状态文件。切换 active、改模型、删除
  profile → daemon 停止或重启 sidecar。
- **使用**：hook/cli/gui 的 embedding 调用一律 `http://127.0.0.1:<port>/v1`；
  成功调用后更新状态文件 `last_used`。
- **空闲回收**：daemon 周期检查（≤30s 粒度），`last_used` 超过 10 分钟
  → 杀 sidecar、撤状态文件；hook 发现状态文件缺失或连接拒绝 → 写 `want`
  标记并立即走 BM25 降级，daemon 见到 `want` 重新拉起。冷启动（数百毫秒到
  数秒）只影响首次查询的语义分，注入永不缺席。
- **崩溃**：daemon 有界重启（连续 3 次失败则放弃并标记，等下次 `want`）。
- **退出**：daemon 退出时杀 sidecar；sidecar 日志写 `~/.openknowledge/`
  下独立文件，Windows 沿用静默子进程模式。

## 7. 检索链路改动

- **client 构造点（4 处）**：`cli.go embeddingClient` / `cli.go Doctor` /
  `hook/core.go InjectForPrompt` / `gui/api.go embeddingClientFor` 改为按
  active profile 构造：openai/ollama 直连 base_url；builtin 读 sidecar 状态
  文件得 localhost 端口（无状态文件 = 未就绪 → nil client 走降级）。"未配置"
  判定从 `key == ""` 改为 `active == ""`。
- **模型身份**：kb.db 新增 `meta(key TEXT PRIMARY KEY, value TEXT)`，键
  `embedding_model` / `embedding_dim`。`Sync` 写向量时刷新；`Query` 比对当前
  profile 身份与 `len(queryVec)`：不符 → 跳过语义通道（纯 BM25），CLI
  stderr 提示"embedding 模型已切换，请运行 ok index 重建"，hook 仅 logErr。
- **批量**：`embed.Client` 增加批量能力（`/v1/embeddings` 数组 input，三形态
  都支持），`ok index` 全量重建分批（如 32 条/批）显著提速内置/本地模型；
  增量 sync 仍单条。
- **查询/文档双路径**：接口区分 EmbedQuery（检索侧）与 EmbedDocument（建
  索引侧）。清单中 `query_prefix`/`doc_prefix` 非空的模型在对应侧自动套
  前缀（Qwen3-Embedding 查询侧 `Instruct: <检索指令>\nQuery: `；nomic 双侧
  `search_query: `/`search_document: `；bge-m3 均不加）。pooling 按清单值
  传给 sidecar 启动参数（bge-m3=cls，qwen3=last，nomic=mean）。
- 检索打分、分词、排除 mandatory/draft 等逻辑**不变**。

## 8. GUI 设计（视觉稿已确认）

**引导页卡片**（单行摘要方案 A）：
- 卡头：`embedding 语义检索` + 徽标（已配置绿 / 未配置灰）
- 卡体：单行摘要——类型徽标（内置绿 / Ollama 蓝紫 / 自定义蓝紫）+ 名称 +
  关键参数（如 `BAAI/bge-m3 Q4 · 1024 维 · 本地运行，无需联网`）；未配置时
  显示"未启用（仅关键词检索）"
- 操作：`配置…` 按钮打开弹窗；原三个输入框撤下

**配置弹窗**（左列表右表单，显式"设为使用中"方案 A）：
- 左侧列表：每项 = 使用中绿点（仅 active 项显示）+ 名称 + 类型小徽标；
  底部 `＋ 添加`
- 右侧表单随类型切换：
  - openai：名称 / base_url / model / api_key（密文，留空保持不变）
  - ollama：名称 / 地址 / 模型（下拉探测 `/api/tags`，未安装提示
    `ollama pull <model>`）
  - builtin：名称 / 模型档位下拉 / 下载源下拉 / 模型文件状态区（未下载
    →"下载"按钮；下载中→进度条+取消；已下载→"✓ 已就绪"）
- 换模型警示条：身份与当前索引不符时显示"需重建索引"
- 底部按钮：`保存`（主）/ `设为使用中` / `测试` / 右侧 `删除`（danger）
- 沿用现有 `.modal` / `.modal-box` 视觉体系与令牌鉴权

**API 面**（细节实现计划定）：`GET /api/setup/embedding`（列表+active+各
profile 状态/下载进度）、`POST .../profiles`（保存）、`POST .../activate`、
`POST .../test`、`DELETE .../profile`、`POST .../builtin/download`（开始/
取消）。key 类秘密只写不读（沿用 `has_key` 语义）。

## 9. CLI 改动

- `ok setup`：embedding 步骤改三选一菜单（线上 OpenAI 兼容 / Ollama /
  内置）；Ollama 自动探测 `localhost:11434/api/tags` 列出模型；内置选择档位
  + 镜像并立即触发下载（显示进度）。保留 `--embedding-*` flags（作用于
  openai 类型，向后兼容）。
- `ok doctor`：按 active profile 检查——openai/ollama 连通性；builtin 检查
  runtime 存在、模型文件校验、sidecar 起停冒烟。
- `ok index`：批量重建；结束报告"索引模型身份：xxx"。

## 10. 打包分发

- `scripts/build.py`：新增 runtime 准备步骤（下载钉死版本的 llama.cpp
  win-cpu-x64 压缩包 → 解出 llama-server.exe + 依赖 dll → `dist/runtime/`）；
  `installer/openknowledge.iss` 把 `runtime/` 打入 `{app}/runtime/`。
- `scripts/build-linux.sh` + `installer/nfpm.yaml`：ubuntu-x64 tar.gz →
  llama-server + so → `/usr/lib/openknowledge/runtime/`（可执行位）。
- 体积：安装包从 ~10MB 增至 ~50MB 级（runtime 约 30–40MB）；模型不随包，
  首次启用内置时下载。
- 免安装 `ok.exe` 单文件形态照常构建，内置类型检测不到 runtime 时给出
  "仅安装版可用"提示。

## 11. 错误与降级

| 场景 | 行为 |
|---|---|
| 未配置（active 空） | 纯 BM25（现状） |
| openai/ollama 连接失败/超时 | fail-open 降级纯 BM25（现状） |
| builtin 模型未下载 | profile 可保存不可激活；GUI/CLI 明确提示去下载 |
| sidecar 未运行/冷启动中 | hook/cli 立即降级 + 写 want 标记，daemon 异步拉起 |
| sidecar 崩溃 | daemon 有界重启 ×3，期间降级 |
| 下载失败/校验不符 | 保留 .part 可重试；不激活 |
| 模型身份与索引不符 | 跳过语义通道 + 提示 `ok index`（新行为，替代静默归零） |
| 删除"使用中"的 profile | 允许删除，`active` 置空退回纯 BM25，GUI/CLI 明确提示 |
| hook 超时预算 | embedding timeout_sec=5s 不变，绝不等待 sidecar 拉起 |

## 12. 测试策略

- 单测：配置迁移（旧平铺→profiles）、模型身份串与维度判定、下载器
  （httptest 模拟源：断点续传/校验失败/取消）、sidecar 管理（假二进制
  脚本模拟就绪/崩溃/空闲）、GUI API（httptest）、检索跳过逻辑
- 测试隔离沿用现有惯例（OK_*_HOME 环境变量隔离，含注册表遍历用例）
- E2E 手动冒烟：安装版 → 内置 bge-m3 Q4 下载 → 激活 → `ok add`/`ok search`
  语义命中 → 断网验证离线可用 → 切 Ollama/线上验证切换与重建提示
- 跨平台：Windows 安装包 + Linux deb 各跑一遍 sidecar 起停

## 13. 文档同步

- `docs/ARCHITECTURE.md`：第 17 章检索（三形态、sidecar 架构、meta 身份）、
  17.7 降级矩阵更新、打包章节加 runtime
- `README.md`：功能清单 + 安装包体积变化说明
- `web/help.md`：三形态配置指引（含 Ollama 准备步骤）
- `CHANGELOG.md`：按项目惯例记录
