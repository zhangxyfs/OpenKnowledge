# embedding 多服务配置与本地模型支持 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** embedding 配置从"单一 OpenAI 兼容线上服务"升级为"多 profile 三形态（openai/ollama/内置 llama.cpp sidecar）+ 使用中切换 + GUI 配置弹窗 + 模型身份管理"。

**Architecture:** 三形态对检索链路同构（都是 OpenAI 兼容 `/v1/embeddings`）；内置形态 = llama.cpp 官方预编译 `llama-server` 作独立 sidecar 进程，daemon 托管其生命周期，主程序零 CGO、构建管线不变。配置层 `[[embedding.profiles]]` + `active`，旧平铺配置自动迁移。kb.db meta 表记录模型身份，切换后语义通道显式跳过并提示重建（替代静默归零）。

**Tech Stack:** Go 1.25（纯标准库+现有依赖，**不新增 go.mod 依赖**）、modernc.org/sqlite、BurntSushi/toml、Inno Setup 7、nfpm、llama.cpp b10405 预编译包。

**Spec:** `docs/superpowers/specs/2026-08-13-embedding-local-providers-design.md`（已批准，commit 82754a4）

## Global Constraints

- 纯 Go、零 CGO、单二进制形态不变；sidecar 是独立分发的 `llama-server` 二进制，非链接依赖
- `go.mod` 不新增任何依赖
- 测试隔离沿用 `OK_HOME`（`registry.Home()` 的唯一环境变量口）；GUI/hook/agentx 测试同理
- fail-open：hook 注入永不因 embedding/sidecar 缺席或失败而缺席；hook 热路径绝不等待 sidecar 冷启动
- 模型清单四条的 repo/file/size/sha256 已钉死（见 Task 2），实现时逐字使用，不得改动
- llama.cpp release 钉死 `b10405`；构建脚本支持 `LLAMA_CPP_BASE_URL` 环境变量覆盖下载源（国内代理）
- commit 信息沿用仓库惯例：中文 conventional commits（参考 `git log --oneline`）
- 每任务结束跑 `go build ./... && go vet ./... && go test ./...` 保持全绿
- Windows 子进程必须静默（无控制台窗口），参照 `internal/daemon/spawn_windows.go` 模式
- 版本号唯一来源：`installer/openknowledge.iss` 的 `#define AppVersion`

## 任务总览

| # | 任务 | 依赖 |
|---|---|---|
| 1 | embed.Client 双路径+批量接口 | — |
| 2 | 内置模型清单（钉死值） | — |
| 3 | 模型下载器（断点续传+校验） | 2 |
| 4 | config profiles 重构 + embedx.Client + 构造点切换 + setupx 持久化 + GUI shim | 1 |
| 5 | embedsidecar 包（状态/spawn/Manager/Reconcile） | 2 |
| 6 | daemon janitor 集成 | 4, 5 |
| 7 | embedx builtin 分支 + CLI setup 三选一 + Doctor builtin | 3, 5, 6 |
| 8 | index meta 表 + Sync 批量重构 + ClearVectors/EmbeddingMeta | 1 |
| 9 | 身份守卫 embedx.QueryVec + ok index 切换重建 | 4, 8 |
| 10 | GUI 后端 API（CRUD/激活/测试/下载） | 3, 4, 7 |
| 11 | GUI 前端（卡片摘要 + 配置弹窗） | 10 |
| 12 | 打包（llama.cpp runtime 进 win/linux 包） | — |
| 13 | 版本 2.14.0 + 更新日志 + 文档同步 | 全部 |

---

### Task 1: embed.Client 双路径 + 批量接口

**Files:**
- Modify: `internal/embed/embed.go`
- Test: `internal/embed/embed_test.go`（新建）
- Modify（机械迁移 `.Embed` 调用点，保持编译绿）:
  - `internal/cli/cli.go:331`（Search）与 `:464`（Doctor）→ `EmbedQuery`
  - `internal/hook/core.go:98`（查询侧）→ `EmbedQuery`
  - `internal/index/sync.go:138,183`（建索引侧）→ `EmbedDocument`
  - `internal/setupx/setupx.go:175`（TestEmbedding）→ `EmbedQuery`

**Interfaces:**
- Produces（后续所有任务依赖）:
  - `embed.Client` interface：`ModelIdentity() string`、`EmbedQuery(ctx, string) ([]float32, error)`、`EmbedDocument(ctx, string) ([]float32, error)`、`EmbedDocuments(ctx, []string) ([][]float32, error)`
  - `embed.OpenAIClient` 新字段：`Identity string`、`QueryPrefix string`、`DocPrefix string`

- [ ] **Step 1: 写失败测试** `internal/embed/embed_test.go`

```go
package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeEmbeddings 返回一个 httptest 服务器：记录最后一次请求体，
// 按 input 顺序返回 [len(text),index] 形式的二维向量（故意乱序 index 以验证重排）。
func fakeEmbeddings(t *testing.T, gotBody *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		b, _ := json.Marshal(req)
		*gotBody = string(b)
		type datum struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		data := make([]datum, len(req.Input))
		for i, text := range req.Input {
			data[len(req.Input)-1-i] = datum{Embedding: []float32{float32(len(text)), float32(i)}, Index: i}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestEmbedDocumentsBatchAndOrder(t *testing.T) {
	var got string
	srv := fakeEmbeddings(t, &got)
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second}
	vecs, err := c.EmbedDocuments(context.Background(), []string{"ab", "cdef"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 2 || vecs[1][0] != 4 {
		t.Fatalf("应按 index 重排回原顺序: %v", vecs)
	}
	var req struct {
		Input []string `json:"input"`
	}
	_ = json.Unmarshal([]byte(got), &req)
	if len(req.Input) != 2 || req.Input[0] != "ab" {
		t.Fatalf("input 应为数组: %s", got)
	}
}

func TestQueryPrefixApplied(t *testing.T) {
	var got string
	srv := fakeEmbeddings(t, &got)
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, Model: "m", QueryPrefix: "Instruct: x\nQuery: ", DocPrefix: "doc: ", Timeout: 5 * time.Second}
	if _, err := c.EmbedQuery(context.Background(), "你好"); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Input []string `json:"input"`
	}
	_ = json.Unmarshal([]byte(got), &req)
	if req.Input[0] != "Instruct: x\nQuery: 你好" {
		t.Fatalf("查询侧应加前缀: %q", req.Input[0])
	}
	if _, err := c.EmbedDocument(context.Background(), "正文"); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(got), &req)
	if req.Input[0] != "doc: 正文" {
		t.Fatalf("文档侧应加文档前缀: %q", req.Input[0])
	}
}

func TestModelIdentity(t *testing.T) {
	c := &OpenAIClient{Identity: "openai:m@h"}
	if c.ModelIdentity() != "openai:m@h" {
		t.Fatal(c.ModelIdentity())
	}
	var zero OpenAIClient
	if zero.ModelIdentity() != "" {
		t.Fatal("空 Identity 应返回空串")
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

Run: `cd D:/develop/OpenKnowledge && go test ./internal/embed/`
Expected: FAIL（`EmbedDocuments`/`EmbedQuery` 未定义）

- [ ] **Step 3: 重写 `internal/embed/embed.go`**（完整替换；保留 Cosine 不动）

```go
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client 是 embedding 服务抽象。查询与建索引是两条路径：
// 指令感知模型（Qwen3-Embedding、nomic）只在对应路径加前缀。
type Client interface {
	// ModelIdentity 返回建索引的模型身份串（写入 kb.db meta，供切换检测）；
	// 空串表示旧式构造（不参与身份判定）。
	ModelIdentity() string
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	EmbedDocument(ctx context.Context, text string) ([]float32, error)
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
}

// OpenAIClient 面向 OpenAI 兼容 /v1/embeddings（线上服务、Ollama、
// 内置 llama-server 三形态共用）。QueryPrefix/DocPrefix 为空即不加前缀。
type OpenAIClient struct {
	BaseURL     string
	APIKey      string
	Model       string
	Timeout     time.Duration
	Identity    string
	QueryPrefix string
	DocPrefix   string
}

func (c *OpenAIClient) ModelIdentity() string { return c.Identity }

func (c *OpenAIClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.embedBatch(ctx, []string{c.QueryPrefix + text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (c *OpenAIClient) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.embedBatch(ctx, []string{c.DocPrefix + text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (c *OpenAIClient) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = c.DocPrefix + t
	}
	return c.embedBatch(ctx, prefixed)
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (c *OpenAIClient) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	body, err := json.Marshal(embedRequest{Model: c.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding API %d: %s", resp.StatusCode, msg)
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding API 返回 %d 条，期望 %d 条", len(er.Data), len(inputs))
	}
	// 按 index 重排（部分实现不保证顺序）
	sort.Slice(er.Data, func(i, j int) bool { return er.Data[i].Index < er.Data[j].Index })
	out := make([][]float32, len(er.Data))
	for i, d := range er.Data {
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("embedding API 返回空向量")
		}
		out[i] = d.Embedding
	}
	return out, nil
}

// Cosine 计算余弦相似度；任一零向量或长度不等返回 0。
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
```

- [ ] **Step 4: 机械迁移调用点**

四处单行替换：
- `internal/index/sync.go:138` 与 `:183`：`client.Embed(context.Background(), e.EmbedText())` → `client.EmbedDocument(context.Background(), e.EmbedText())`
- `internal/hook/core.go:98`：`client.Embed(context.Background(), promptText)` → `client.EmbedQuery(context.Background(), promptText)`
- `internal/cli/cli.go:331`：`client.Embed(context.Background(), query)` → `client.EmbedQuery(context.Background(), query)`
- `internal/cli/cli.go:464`：`client.Embed(context.Background(), "ping")` → `client.EmbedQuery(context.Background(), "ping")`
- `internal/setupx/setupx.go:175`：`client.Embed(context.Background(), "ping")` → `client.EmbedQuery(context.Background(), "ping")`

注意：`sync.go`/`core.go` 的 `embed.Client` 接口变量现在指向新接口，老测试里若有自绘 fake client 需同步加方法（用 `go build ./...` 找出全部破损点逐一修复）。

- [ ] **Step 5: 跑全量测试**

Run: `cd D:/develop/OpenKnowledge && go build ./... && go test ./internal/embed/ ./internal/index/ ./internal/hook/ ./internal/cli/ ./internal/setupx/ ./internal/gui/`
Expected: 全 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/embed internal/index/sync.go internal/hook/core.go internal/cli/cli.go internal/setupx/setupx.go
git commit -m "refactor(embed): Client 拆查询/文档双路径 + 批量 EmbedDocuments——OpenAIClient 支持前缀与模型身份，为本地模型与指令感知模型铺路"
```

---

### Task 2: 内置模型清单（钉死值）

**Files:**
- Create: `internal/embed/manifest.go`
- Test: `internal/embed/manifest_test.go`

**Interfaces:**
- Produces:
  - `embed.BuiltinModel{ID, Label, Repo, File string; Size int64; SHA256 string; Dim int; Pooling, QueryPrefix, DocPrefix string}`
  - `embed.BuiltinModels []BuiltinModel`（var，测试可追加假条目）
  - `embed.FindBuiltinModel(id string) *BuiltinModel`
  - `embed.MirrorBase(mirror string) string`
  - `(BuiltinModel) URL(mirror string) string`、`InstalledPath(modelsDir string) string`、`Installed(modelsDir string) bool`

- [ ] **Step 1: 写失败测试** `internal/embed/manifest_test.go`

```go
package embed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindBuiltinModel(t *testing.T) {
	m := FindBuiltinModel("qwen3-emb-0.6b-q8")
	if m == nil || m.Dim != 1024 || m.Pooling != "last" {
		t.Fatalf("%+v", m)
	}
	if m.QueryPrefix == "" {
		t.Fatal("qwen3 应有查询指令前缀")
	}
	if FindBuiltinModel("不存在") != nil {
		t.Fatal("未知 id 应返回 nil")
	}
	n := FindBuiltinModel("nomic-embed-q8")
	if n.DocPrefix != "search_document: " || n.QueryPrefix != "search_query: " {
		t.Fatalf("nomic 双前缀: %+v", n)
	}
}

func TestMirrorBaseAndURL(t *testing.T) {
	m := FindBuiltinModel("bge-m3-q4_k_m")
	if got := m.URL(""); got != "https://hf-mirror.com/gpustack/bge-m3-GGUF/resolve/main/bge-m3-Q4_K_M.gguf" {
		t.Fatal(got)
	}
	if got := m.URL("huggingface"); got != "https://huggingface.co/gpustack/bge-m3-GGUF/resolve/main/bge-m3-Q4_K_M.gguf" {
		t.Fatal(got)
	}
	if got := m.URL("http://127.0.0.1:9999/"); got != "http://127.0.0.1:9999/gpustack/bge-m3-GGUF/resolve/main/bge-m3-Q4_K_M.gguf" {
		t.Fatal("自定义镜像应去尾斜杠")
	}
}

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	m := FindBuiltinModel("nomic-embed-q8")
	if m.Installed(dir) {
		t.Fatal("空目录不应判定已安装")
	}
	p := m.InstalledPath(dir)
	if err := os.WriteFile(p, make([]byte, m.Size), 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.Installed(dir) {
		t.Fatal("尺寸一致应判定已安装")
	}
	if filepath.Base(p) != "nomic-embed-q8.gguf" {
		t.Fatal(p)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/embed/ -run 'TestFind|TestMirror|TestInstalled'`
Expected: FAIL（标识符未定义）

- [ ] **Step 3: 实现** `internal/embed/manifest.go`

```go
package embed

import (
	"os"
	"path/filepath"
	"strings"
)

// BuiltinModel 是内置（llama.cpp sidecar）可下载模型的清单条目。
// size/sha256 在引入时钉死（来源 HF API /api/models/<repo>/tree/main 的 lfs.oid），
// 下载后校验，防篡改与截断。
type BuiltinModel struct {
	ID       string // 清单 id，如 "qwen3-emb-0.6b-q8"
	Label    string // GUI/CLI 展示名（含体积/维度/特点）
	Repo     string // HF repo，如 "Qwen/Qwen3-Embedding-0.6B-GGUF"
	File     string // repo 内文件名
	Size     int64
	SHA256   string // 小写 hex
	Dim      int
	Pooling  string // llama-server --pooling 取值：cls|last|mean
	QueryPrefix string // 查询侧前缀（指令感知模型），空=不加
	DocPrefix   string // 文档侧前缀（nomic 系），空=不加
}

// BuiltinModels 内置模型清单（默认第一条）。
// 2026-08-13 钉死；变更新增条目即可，无需改代码。
var BuiltinModels = []BuiltinModel{
	{
		ID: "qwen3-emb-0.6b-q8", Label: "Qwen3-Embedding-0.6B · Q8_0（推荐 · 639MB · 1024 维 · 中文+代码强）",
		Repo: "Qwen/Qwen3-Embedding-0.6B-GGUF", File: "Qwen3-Embedding-0.6B-Q8_0.gguf",
		Size: 639150592, SHA256: "06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439",
		Dim: 1024, Pooling: "last",
		QueryPrefix: "Instruct: 检索与用户输入相关的知识条目\nQuery: ",
	},
	{
		ID: "bge-m3-q4_k_m", Label: "BAAI/bge-m3 · Q4_K_M（下载最小 418MB · 1024 维 · 中英双语）",
		Repo: "gpustack/bge-m3-GGUF", File: "bge-m3-Q4_K_M.gguf",
		Size: 437778496, SHA256: "6d39681b26c61279ac1f82db35a04a05009e94c415b51c858ff571489a82fc06",
		Dim: 1024, Pooling: "cls",
	},
	{
		ID: "bge-m3-q8_0", Label: "BAAI/bge-m3 · Q8_0（605MB · 1024 维 · bge 质量档）",
		Repo: "gpustack/bge-m3-GGUF", File: "bge-m3-Q8_0.gguf",
		Size: 634553760, SHA256: "950f4a8e5e19477a6d3c26d2f162233c20002c601f75e4b002e3239997821167",
		Dim: 1024, Pooling: "cls",
	},
	{
		ID: "nomic-embed-q8", Label: "nomic-embed-text v1.5 · Q8_0（139MB · 768 维 · 英文为主）",
		Repo: "nomic-ai/nomic-embed-text-v1.5-GGUF", File: "nomic-embed-text-v1.5.Q8_0.gguf",
		Size: 146146432, SHA256: "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7",
		Dim: 768, Pooling: "mean",
		QueryPrefix: "search_query: ", DocPrefix: "search_document: ",
	},
}

// FindBuiltinModel 按 id 查清单；未知返回 nil。
func FindBuiltinModel(id string) *BuiltinModel {
	for i := range BuiltinModels {
		if BuiltinModels[i].ID == id {
			return &BuiltinModels[i]
		}
	}
	return nil
}

// MirrorBase 把镜像名解析为下载 base URL：空/hf-mirror=国内镜像，
// huggingface=官方，其余视为自定义 base（去尾斜杠）。
func MirrorBase(mirror string) string {
	switch mirror {
	case "", "hf-mirror":
		return "https://hf-mirror.com"
	case "huggingface":
		return "https://huggingface.co"
	default:
		return strings.TrimRight(mirror, "/")
	}
}

// URL 返回模型文件下载地址（HF resolve 路径约定）。
func (m BuiltinModel) URL(mirror string) string {
	return MirrorBase(mirror) + "/" + m.Repo + "/resolve/main/" + m.File
}

// InstalledPath 返回模型文件落盘路径（<modelsDir>/<id>.gguf）。
func (m BuiltinModel) InstalledPath(modelsDir string) string {
	return filepath.Join(modelsDir, m.ID+".gguf")
}

// Installed 快速判定模型已就绪（存在且尺寸一致；完整 sha256 校验在下载完成时做）。
func (m BuiltinModel) Installed(modelsDir string) bool {
	st, err := os.Stat(m.InstalledPath(modelsDir))
	return err == nil && st.Size() == m.Size
}
```

- [ ] **Step 4: 跑测试 + commit**

Run: `go test ./internal/embed/`
Expected: PASS

```bash
git add internal/embed/manifest.go internal/embed/manifest_test.go
git commit -m "feat(embed): 内置模型清单——qwen3-emb-0.6b（默认）/bge-m3 双档/nomic，repo+size+sha256 钉死，镜像源可配"
```

---

### Task 3: 模型下载器（断点续传 + sha256 校验）

**Files:**
- Create: `internal/embed/download.go`
- Test: `internal/embed/download_test.go`

**Interfaces:**
- Consumes: Task 2 的 `BuiltinModel`/`URL`/`InstalledPath`
- Produces: `embed.Download(ctx context.Context, hc *http.Client, m BuiltinModel, mirror, modelsDir string, progress func(done, total int64)) error`——`.part` 断点续传，完成校验 sha256 后原子改名；取消保留 `.part`；校验失败删 `.part` 报错

- [ ] **Step 1: 写失败测试** `internal/embed/download_test.go`

```go
package embed

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeModelServer 按 Range 提供 content；记录是否收到 Range 头。
func fakeModelServer(content []byte, sawRange *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rg := r.Header.Get("Range")
		if rg == "" {
			w.Write(content)
			return
		}
		*sawRange = true
		var from int
		fmt.Sscanf(rg, "bytes=%d-", &from)
		if from >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(from)+"-"+strconv.Itoa(len(content)-1)+"/"+strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content[from:])
	}))
}

func testModel(content []byte) BuiltinModel {
	sum := sha256.Sum256(content)
	return BuiltinModel{ID: "t-model", Repo: "r/p", File: "m.gguf", Size: int64(len(content)), SHA256: fmt.Sprintf("%x", sum), Dim: 8}
}

func TestDownloadFull(t *testing.T) {
	content := []byte(strings.Repeat("abc123", 1000))
	srv := fakeModelServer(content, new(bool))
	defer srv.Close()
	m := testModel(content)
	dir := t.TempDir()
	var lastDone, lastTotal int64
	err := Download(context.Background(), srv.Client(), m, srv.URL, dir, func(d, t int64) { lastDone, lastTotal = d, t })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(m.InstalledPath(dir))
	if string(got) != string(content) {
		t.Fatal("内容不一致")
	}
	if lastDone != m.Size || lastTotal != m.Size {
		t.Fatalf("进度回调: %d/%d", lastDone, lastTotal)
	}
}

func TestDownloadResume(t *testing.T) {
	content := []byte(strings.Repeat("xyz789", 2000))
	var sawRange bool
	srv := fakeModelServer(content, &sawRange)
	defer srv.Close()
	m := testModel(content)
	dir := t.TempDir()
	// 预置半截 .part
	if err := os.WriteFile(m.InstalledPath(dir)+".part", content[:5000], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), srv.Client(), m, srv.URL, dir, nil); err != nil {
		t.Fatal(err)
	}
	if !sawRange {
		t.Fatal("应带 Range 头续传")
	}
	got, _ := os.ReadFile(m.InstalledPath(dir))
	if string(got) != string(content) {
		t.Fatal("续传后内容不一致")
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	content := []byte("content")
	srv := fakeModelServer(content, new(bool))
	defer srv.Close()
	m := testModel(content)
	m.SHA256 = strings.Repeat("0", 64)
	dir := t.TempDir()
	err := Download(context.Background(), srv.Client(), m, srv.URL, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("应报校验失败: %v", err)
	}
	if _, serr := os.Stat(m.InstalledPath(dir) + ".part"); !os.IsNotExist(serr) {
		t.Fatal("校验失败应删除 .part")
	}
}

func TestDownloadCancelKeepsPart(t *testing.T) {
	content := []byte(strings.Repeat("q", 1<<20))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content[:100])
		w.(http.Flusher).Flush()
		<-r.Context().Done() // 挂住直到客户端取消
	}))
	defer srv.Close()
	m := testModel(content)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_ = Download(ctx, srv.Client(), m, srv.URL, dir, nil)
	st, err := os.Stat(m.InstalledPath(dir) + ".part")
	if err != nil || st.Size() == 0 {
		t.Fatalf("取消应保留 .part 供续传: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, m.ID+".gguf")); !os.IsNotExist(err) {
		t.Fatal("取消不应产生正式文件")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/embed/ -run TestDownload`
Expected: FAIL（`Download` 未定义）

- [ ] **Step 3: 实现** `internal/embed/download.go`

```go
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Download 把模型下载到 modelsDir/<id>.gguf：
// .part 断点续传（Range）→ 写完后整文件 sha256 校验 → 原子改名。
// ctx 取消保留 .part 供下次续传；sha256 不符删 .part 报错（防循环续传坏文件）。
func Download(ctx context.Context, hc *http.Client, m BuiltinModel, mirror, modelsDir string, progress func(done, total int64)) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return err
	}
	dest := m.InstalledPath(modelsDir)
	part := dest + ".part"
	var offset int64
	if st, err := os.Stat(part); err == nil {
		offset = st.Size()
		if offset > m.Size { // 异常残留，重下
			_ = os.Remove(part)
			offset = 0
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL(mirror), nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// .part 比服务端还长（源变了）：删掉重头来
		_ = os.Remove(part)
		return fmt.Errorf("续传偏移越界（416），已清除 %s，请重试", filepath.Base(part))
	case offset > 0 && resp.StatusCode == http.StatusOK:
		// 服务端不认 Range：截断重下
		offset = 0
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
		return fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	flag := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flag, 0o644)
	if err != nil {
		return err
	}
	written := offset
	buf := make([]byte, 256*1024)
	copyErr := func() error {
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					return werr
				}
				written += int64(n)
				if progress != nil {
					progress(written, m.Size)
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					return nil
				}
				return rerr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}()
	_ = f.Close()
	if copyErr != nil {
		return copyErr // .part 保留
	}
	if written != m.Size {
		return fmt.Errorf("下载大小不符：%d，期望 %d（.part 已保留可续传）", written, m.Size)
	}
	sum, err := fileSHA256(part)
	if err != nil {
		return err
	}
	if sum != m.SHA256 {
		_ = os.Remove(part)
		return fmt.Errorf("sha256 校验不符（已删除 %s）", filepath.Base(part))
	}
	return os.Rename(part, dest)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

- [ ] **Step 4: 跑测试 + commit**

Run: `go test ./internal/embed/`
Expected: PASS

```bash
git add internal/embed/download.go internal/embed/download_test.go
git commit -m "feat(embed): 模型下载器——.part 断点续传 + sha256 校验 + 进度回调 + 取消保留现场"
```

---

### Task 4: config profiles 重构 + embedx.Client + 构造点切换 + setupx 持久化 + GUI shim

本任务把"配置模型"整体切到 profiles，所有构造点收口到 `embedx.Client`，GUI 旧前端经 shim 继续可用。改动面大但语义单一：配置形态替换。

**Files:**
- Modify: `internal/config/config.go`（Embedding 重构 + 迁移 + 按名合并）
- Test: `internal/config/config_test.go`
- Create: `internal/embedx/embedx.go` + `internal/embedx/embedx_test.go`
- Modify: `internal/setupx/setupx.go`（SaveEmbedding/TestEmbedding → profile 版三件套）
- Modify: `internal/cli/cli.go:293-305`（embeddingClient 收口）
- Modify: `internal/cli/setup.go:105-140`（setupEmbedding 走 profile；交互菜单在 Task 7）
- Modify: `internal/hook/core.go:34-42`（构造收口）
- Modify: `internal/gui/api.go:327-344`（apiStatus）+ `:702-719`（embeddingClientFor）+ `:1073-1110`（apiSetupEmbedding shim）
- Test: `internal/cli/setup_test.go`、`internal/gui/api_test.go`（跟随断言调整）

**Interfaces:**
- Produces:
  - `config.EmbeddingProfile{Name, Type, BaseURL, Model, APIKey, APIKeyEnv, Mirror string}`；Type ∈ `openai|ollama|builtin`
  - `config.Embedding{Active string; TimeoutSec int; Profiles []EmbeddingProfile; BaseURL/APIKey/APIKeyEnv/Model string}`（后四个为旧版迁移用，`omitempty`）
  - `(Embedding) ActiveProfile() *EmbeddingProfile`；`(EmbeddingProfile) ResolvedAPIKey() string`；`(EmbeddingProfile) ModelIdentity() string`
  - `embedx.Client(cfg config.Config) embed.Client`；`embedx.ClientForProfile(p config.EmbeddingProfile, timeout time.Duration) embed.Client`
  - `setupx.SaveEmbeddingProfile(p config.EmbeddingProfile, activate bool) error`（api_key 空=保留同名旧 key）
  - `setupx.SetActiveEmbedding(name string) error`（空串=停用）
  - `setupx.DeleteEmbeddingProfile(name string) error`（删 active 项时 Active 置空）
  - `setupx.TestEmbeddingProfile(p config.EmbeddingProfile, timeout time.Duration) error`

- [ ] **Step 1: config 失败测试**（追加到 `internal/config/config_test.go`；已有 `ResolvedAPIKey` 用例如引用旧方法需改到 profile 上）

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyEmbeddingMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	old := "[embedding]\nbase_url = \"https://api.siliconflow.cn/v1\"\nmodel = \"BAAI/bge-m3\"\napi_key = \"sk-x\"\ntimeout_sec = 7\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Name != "默认" || p.Type != "openai" || p.BaseURL != "https://api.siliconflow.cn/v1" || p.Model != "BAAI/bge-m3" || p.ResolvedAPIKey() != "sk-x" {
		t.Fatalf("迁移结果: %+v", p)
	}
	if cfg.Embedding.BaseURL != "" || cfg.Embedding.Model != "" || cfg.Embedding.APIKey != "" {
		t.Fatal("迁移后平铺字段应清空")
	}
	if cfg.Embedding.TimeoutSec != 7 {
		t.Fatal("timeout_sec 保留")
	}
	if p.ModelIdentity() != "openai:BAAI/bge-m3@https://api.siliconflow.cn/v1" {
		t.Fatal(p.ModelIdentity())
	}
}

func TestProfilesMergeByName(t *testing.T) {
	dir := t.TempDir()
	global := "[embedding]\nactive = \"a\"\n[[embedding.profiles]]\nname = \"a\"\ntype = \"openai\"\nmodel = \"m1\"\n[[embedding.profiles]]\nname = \"b\"\ntype = \"ollama\"\nbase_url = \"http://localhost:11434\"\nmodel = \"bge-m3\"\n"
	project := "[[embedding.profiles]]\nname = \"a\"\ntype = \"openai\"\nmodel = \"m2\"\n"
	gp := filepath.Join(dir, "g.toml")
	pp := filepath.Join(dir, "p.toml")
	os.WriteFile(gp, []byte(global), 0o600)
	os.WriteFile(pp, []byte(project), 0o600)
	cfg, err := LoadMerged(pp, gp)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Embedding.Profiles) != 2 {
		t.Fatalf("按名合并应为 2 条: %+v", cfg.Embedding.Profiles)
	}
	for _, p := range cfg.Embedding.Profiles {
		if p.Name == "a" && p.Model != "m2" {
			t.Fatal("项目级同名覆盖")
		}
	}
	if cfg.Embedding.Active != "a" {
		t.Fatal("active 继承全局")
	}
}

func TestActiveProfileAndIdentity(t *testing.T) {
	var e Embedding
	if e.ActiveProfile() != nil {
		t.Fatal("空 active 应为 nil")
	}
	b := EmbeddingProfile{Name: "内", Type: "builtin", Model: "qwen3-emb-0.6b-q8"}
	if b.ModelIdentity() != "builtin:qwen3-emb-0.6b-q8" {
		t.Fatal(b.ModelIdentity())
	}
	o := EmbeddingProfile{Name: "o", Type: "ollama", BaseURL: "http://h:11434", Model: "bge-m3"}
	if o.ModelIdentity() != "ollama:bge-m3@http://h:11434" {
		t.Fatal(o.ModelIdentity())
	}
}
```

- [ ] **Step 2: 确认失败**

Run: `go test ./internal/config/`
Expected: FAIL（EmbeddingProfile 未定义）

- [ ] **Step 3: 重写 config.go 的 Embedding 部分**

`internal/config/config.go`：
- 删除旧 `Embedding` struct 与 `(Embedding) ResolvedAPIKey()`，替换为：

```go
// EmbeddingProfile 是一套 embedding 服务配置。Type：openai（OpenAI 兼容线上/自建）、
// ollama（本机或局域网 Ollama，免 key）、builtin（ok 托管 llama.cpp sidecar）。
// openai/ollama 的 Model 是模型名；builtin 的 Model 是 embed.BuiltinModels 清单 id，
// Mirror 是其下载源（hf-mirror|huggingface|自定义 base URL）。
type EmbeddingProfile struct {
	Name      string `toml:"name"`
	Type      string `toml:"type"`
	BaseURL   string `toml:"base_url,omitempty"`
	Model     string `toml:"model,omitempty"`
	APIKey    string `toml:"api_key,omitempty"`
	APIKeyEnv string `toml:"api_key_env,omitempty"`
	Mirror    string `toml:"mirror,omitempty"`
}

// ResolvedAPIKey 返回生效 key：api_key 优先，其次 api_key_env 指向的环境变量。
func (p EmbeddingProfile) ResolvedAPIKey() string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.APIKeyEnv != "" {
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

// ModelIdentity 返回建索引的模型身份串（写入 kb.db meta，切换检测用）。
func (p EmbeddingProfile) ModelIdentity() string {
	switch p.Type {
	case "builtin":
		return "builtin:" + p.Model
	case "ollama":
		return "ollama:" + p.Model + "@" + p.BaseURL
	default:
		return "openai:" + p.Model + "@" + p.BaseURL
	}
}

type Embedding struct {
	Active     string             `toml:"active,omitempty"` // 使用中 profile 名；空=未配置
	TimeoutSec int                `toml:"timeout_sec"`
	Profiles   []EmbeddingProfile `toml:"profiles,omitempty"`
	// 旧版平铺字段（≤v2.13），仅迁移读取；omitempty 保证新写盘不再出现
	BaseURL   string `toml:"base_url,omitempty"`
	APIKey    string `toml:"api_key,omitempty"`
	APIKeyEnv string `toml:"api_key_env,omitempty"`
	Model     string `toml:"model,omitempty"`
}

// ActiveProfile 返回使用中 profile；未配置或 active 悬空返回 nil。
func (e Embedding) ActiveProfile() *EmbeddingProfile {
	if e.Active == "" {
		return nil
	}
	for i := range e.Profiles {
		if e.Profiles[i].Name == e.Active {
			return &e.Profiles[i]
		}
	}
	return nil
}

// migrateLegacy 把 ≤v2.13 的平铺字段迁移为 "默认" openai profile（内存态；
// 下次保存配置时自然落盘）。
func (e *Embedding) migrateLegacy() {
	if e.BaseURL == "" && e.Model == "" && e.APIKey == "" && e.APIKeyEnv == "" {
		return
	}
	if len(e.Profiles) == 0 {
		e.Profiles = []EmbeddingProfile{{
			Name: "默认", Type: "openai",
			BaseURL: e.BaseURL, Model: e.Model, APIKey: e.APIKey, APIKeyEnv: e.APIKeyEnv,
		}}
		if e.Active == "" {
			e.Active = "默认"
		}
	}
	e.BaseURL, e.Model, e.APIKey, e.APIKeyEnv = "", "", "", ""
}
```

- `Load` 在 `toml.Unmarshal` 后加一行 `cfg.Embedding.migrateLegacy()`。
- `LoadMerged` 改为（`toml.Decode` 拿 MetaData；profiles 数组按名合并，其余语义不变）：

```go
// LoadMerged 合并配置：内置默认 ← globalPath ← projectPath，后者覆盖前者。
// profiles 数组例外：toml 对数组整体替换，这里按 name 合并（项目级同名覆盖，
// 不删除全局独有项）。两个文件都可以不存在（视为空）。
func LoadMerged(projectPath, globalPath string) (Config, error) {
	cfg := Default()
	for _, path := range []string{globalPath, projectPath} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, err
		}
		prev := cfg.Embedding.Profiles
		md, err := toml.Decode(string(data), &cfg)
		if err != nil {
			return cfg, fmt.Errorf("解析 %s: %w", path, err)
		}
		if md.IsDefined("embedding", "profiles") && len(prev) > 0 {
			merged := append([]EmbeddingProfile{}, prev...)
			for _, p := range cfg.Embedding.Profiles {
				found := false
				for i := range merged {
					if merged[i].Name == p.Name {
						merged[i] = p
						found = true
						break
					}
				}
				if !found {
					merged = append(merged, p)
				}
			}
			cfg.Embedding.Profiles = merged
		}
	}
	cfg.Embedding.migrateLegacy()
	return cfg, nil
}
```

（原 `Load` 保持 `toml.Unmarshal` 即可，仅追加 migrate 调用。）

- [ ] **Step 4: embedx 包** `internal/embedx/embedx.go`

```go
// Package embedx 按配置构造 embedding 客户端——CLI/hook/GUI 的唯一构造点。
// 三形态同构：openai 直连、ollama 补 /v1、builtin 经 embedsidecar 发现端口
// （Task 7 接入；未就绪一律返回 nil，调用方走纯关键词降级）。
package embedx

import (
	"strings"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

// Client 返回使用中（active）profile 的客户端；未配置/暂不可用返回 nil。
func Client(cfg config.Config) embed.Client {
	p := cfg.Embedding.ActiveProfile()
	if p == nil {
		return nil
	}
	return ClientForProfile(*p, time.Duration(cfg.Embedding.TimeoutSec)*time.Second)
}

// ClientForProfile 构造单个 profile 的客户端。
// openai/ollama 要求 base_url 与 model 非空（key 可空——本地兼容服务常无 key）。
func ClientForProfile(p config.EmbeddingProfile, timeout time.Duration) embed.Client {
	switch p.Type {
	case "ollama":
		if p.BaseURL == "" || p.Model == "" {
			return nil
		}
		return &embed.OpenAIClient{
			BaseURL:  strings.TrimRight(p.BaseURL, "/") + "/v1",
			Model:    p.Model,
			Timeout:  timeout,
			Identity: p.ModelIdentity(),
		}
	case "builtin":
		return nil // Task 7 接入 embedsidecar
	default: // openai
		if p.BaseURL == "" || p.Model == "" {
			return nil
		}
		return &embed.OpenAIClient{
			BaseURL:  p.BaseURL,
			APIKey:   p.ResolvedAPIKey(),
			Model:    p.Model,
			Timeout:  timeout,
			Identity: p.ModelIdentity(),
		}
	}
}
```

- [ ] **Step 5: embedx 测试** `internal/embedx/embedx_test.go`

```go
package embedx

import (
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

func TestClientNilWhenNoActive(t *testing.T) {
	if Client(config.Config{}) != nil {
		t.Fatal("未配置应为 nil")
	}
}

func TestClientOpenAI(t *testing.T) {
	cfg := config.Config{Embedding: config.Embedding{
		Active: "a", TimeoutSec: 5,
		Profiles: []config.EmbeddingProfile{{Name: "a", Type: "openai", BaseURL: "http://h/v1", Model: "m", APIKey: "k"}},
	}}
	c := Client(cfg)
	oc, ok := c.(*embed.OpenAIClient)
	if !ok || oc.BaseURL != "http://h/v1" || oc.APIKey != "k" || oc.Timeout != 5*time.Second {
		t.Fatalf("%+v", oc)
	}
	if c.ModelIdentity() != "openai:m@http://h/v1" {
		t.Fatal(c.ModelIdentity())
	}
}

func TestClientOllamaAppendsV1(t *testing.T) {
	p := config.EmbeddingProfile{Name: "o", Type: "ollama", BaseURL: "http://localhost:11434/", Model: "bge-m3"}
	c := ClientForProfile(p, time.Second)
	oc := c.(*embed.OpenAIClient)
	if oc.BaseURL != "http://localhost:11434/v1" || oc.APIKey != "" {
		t.Fatalf("%+v", oc)
	}
}

func TestClientMissingFieldsNil(t *testing.T) {
	if ClientForProfile(config.EmbeddingProfile{Name: "x", Type: "openai"}, time.Second) != nil {
		t.Fatal("缺 base_url/model 应为 nil")
	}
}
```

- [ ] **Step 6: setupx profile 持久化 + 测试改造**

`internal/setupx/setupx.go`：
- 删除 `SaveEmbedding` 与 `TestEmbedding`，新增（import 增 `openknowledge/internal/embedx`）：

```go
// saveGlobalConfig 把配置写回全局 config.toml（0600）。Save* 系列共用。
func saveGlobalConfig(cfg config.Config) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("全局配置编码失败: %w", err)
	}
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(globalPath, []byte(buf.String()), 0o600); err != nil {
		return fmt.Errorf("全局配置写入失败: %w", err)
	}
	return nil
}

// SaveEmbeddingProfile 保存（同名覆盖）一个 profile 到全局配置；activate 时
// 同时置为使用中。api_key 留空 = 保留同名旧 key（GUI 密文不回传语义）。
func SaveEmbeddingProfile(p config.EmbeddingProfile, activate bool) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败，跳过 embedding: %w", err)
	}
	for i := range cfg.Embedding.Profiles {
		if cfg.Embedding.Profiles[i].Name == p.Name {
			if p.APIKey == "" {
				p.APIKey = cfg.Embedding.Profiles[i].APIKey
			}
			cfg.Embedding.Profiles[i] = p
			if activate {
				cfg.Embedding.Active = p.Name
			}
			return saveGlobalConfig(cfg)
		}
	}
	cfg.Embedding.Profiles = append(cfg.Embedding.Profiles, p)
	if activate {
		cfg.Embedding.Active = p.Name
	}
	return saveGlobalConfig(cfg)
}

// SetActiveEmbedding 切换使用中 profile；name 空串 = 停用（纯关键词检索）。
func SetActiveEmbedding(name string) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	if name != "" {
		found := false
		for _, p := range cfg.Embedding.Profiles {
			if p.Name == name {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("profile 不存在: %s", name)
		}
	}
	cfg.Embedding.Active = name
	return saveGlobalConfig(cfg)
}

// DeleteEmbeddingProfile 删除 profile；删除使用中项时 Active 置空（退回纯关键词）。
func DeleteEmbeddingProfile(name string) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	kept := cfg.Embedding.Profiles[:0]
	for _, p := range cfg.Embedding.Profiles {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	cfg.Embedding.Profiles = kept
	if cfg.Embedding.Active == name {
		cfg.Embedding.Active = ""
	}
	return saveGlobalConfig(cfg)
}

// TestEmbeddingProfile 以 timeout 做 profile 连通性检查（builtin 在 Task 7 扩展）。
func TestEmbeddingProfile(p config.EmbeddingProfile, timeout time.Duration) error {
	c := embedx.ClientForProfile(p, timeout)
	if c == nil {
		return fmt.Errorf("profile 不可用（类型 %s，检查必填项）", p.Type)
	}
	_, err := c.EmbedQuery(context.Background(), "ping")
	return err
}
```

同时把 `SaveHooksTimeout`/`SaveReasonixEnforceMode` 的写盘尾部换成 `saveGlobalConfig(cfg)`（顺手去重，行为不变）。

- [ ] **Step 7: 调用点切换（编译驱动）**

- `internal/cli/cli.go:293-305` 替换为：

```go
// embeddingClient 配置齐全时返回客户端，否则返回 nil（构造收口在 embedx）。
func embeddingClient(pc *project.Context) embed.Client {
	return embedx.Client(pc.Config)
}
```

（import 加 `openknowledge/internal/embedx`；原返回 `*embed.OpenAIClient`，调用处已是 `embed.Client` 语义。）

- `internal/hook/core.go:34-42` 整块替换为一行 `client := embedx.Client(pc.Config)`（import 换 embedx，去掉 time/embed 若不再使用——`time` 仍被他处用则保留）。
- `internal/gui/api.go:702-719` `embeddingClientFor` 函数体替换为：

```go
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return nil
	}
	return embedx.Client(cfg)
```

- `internal/gui/api.go:327-338`（apiStatus embedding 段）替换为：

```go
	embeddingConfigured := false
	embedding := map[string]any{"base_url": "", "model": "", "has_key": false}
	hooksTimeout := 10
	if cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml")); err == nil {
		if p := cfg.Embedding.ActiveProfile(); p != nil {
			embeddingConfigured = true
			embedding["base_url"] = p.BaseURL
			embedding["model"] = p.Model
			embedding["has_key"] = p.ResolvedAPIKey() != ""
		}
		if cfg.Hooks.TimeoutSec > 0 {
			hooksTimeout = cfg.Hooks.TimeoutSec
		}
	}
```

- `internal/gui/api.go:1073-1110` `apiSetupEmbedding`（**shim：保持旧请求形状**，写"默认" openai profile；新端点在 Task 10）替换为：

```go
func (h *Handler) apiSetupEmbedding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
		APIKey  string `json:"api_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.BaseURL == "" {
		req.BaseURL = "https://api.openai.com/v1"
	}
	if req.Model == "" {
		req.Model = "text-embedding-3-small"
	}
	if req.APIKey == "" {
		// 留空 = 保留 active openai profile 已保存的 key
		if cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml")); err == nil {
			if p := cfg.Embedding.ActiveProfile(); p != nil && p.Type == "openai" {
				req.APIKey = p.ResolvedAPIKey()
			}
		}
		if req.APIKey == "" {
			writeErr(w, http.StatusBadRequest, "api_key 不能为空（尚未保存过 key）")
			return
		}
	}
	result := func(err error) {
		resp := map[string]any{"ok": err == nil, "error": ""}
		if err != nil {
			resp["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	}
	p := config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: req.BaseURL, Model: req.Model, APIKey: req.APIKey}
	if err := setupx.SaveEmbeddingProfile(p, true); err != nil {
		result(err)
		return
	}
	result(setupx.TestEmbeddingProfile(p, 10*time.Second))
}
```

- `internal/cli/setup.go:130-139`：Save/Test 两行换成

```go
	p := config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: baseURL, Model: model, APIKey: apiKey}
	if err := setupx.SaveEmbeddingProfile(p, true); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	fmt.Fprintf(stdout, "embedding 已写入全局配置 %s\n", filepath.Join(registry.Home(), "config.toml"))
	if err := setupx.TestEmbeddingProfile(p, 10*time.Second); err != nil {
		fmt.Fprintf(stdout, "embedding 连通性验证失败（不影响使用关键词检索）: %v\n", err)
	} else {
		fmt.Fprintln(stdout, "embedding 连通性验证通过")
	}
```

（import 加 `openknowledge/internal/config` 与 `time`。）

- [ ] **Step 8: 修复全部测试编译与断言**

Run: `go build ./... && go vet ./... && go test ./...`
预期破损点（逐一修复，断言语义等价迁移到 profiles）：
- `internal/config/config_test.go`：旧 `ResolvedAPIKey` 用例改到 `EmbeddingProfile`
- `internal/cli/setup_test.go`：`config.toml` 断言改为 `[[embedding.profiles]]` 形态（`active = "默认"`）
- `internal/gui/api_test.go`：embedding 相关用例（保存后 status 的 `embedding.base_url` 等从 active profile 读取，请求形状不变）

- [ ] **Step 9: 全绿后 commit**

```bash
git add internal/config internal/embedx internal/setupx internal/cli internal/hook internal/gui
git commit -m "feat(config): embedding 多 profile 重构——active+profiles 三形态，旧平铺配置自动迁移；客户端构造收口 embedx，GUI 旧端点 shim 保兼容"
```

---

### Task 5: embedsidecar 包（状态文件 / 静默 spawn / Manager）

**Files:**
- Create: `internal/embedsidecar/sidecar.go`（State/want/Touch）
- Create: `internal/embedsidecar/manager.go`（Manager.Ensure/Stop/Reconcile + RuntimeServerPath + DefaultRuntimeDir）
- Create: `internal/embedsidecar/spawn_windows.go`、`spawn_other.go`
- Test: `internal/embedsidecar/sidecar_test.go`（helper-process 伪装 llama-server）

**Interfaces:**
- Consumes: `embed.BuiltinModel`、`registry.Home()`
- Produces:
  - `embedsidecar.State{PID, Port int; ModelID string; StartedAt, LastUsed time.Time}`、`LoadState() *State`、`(*State) Healthy() bool`、`(*State) BaseURL() string`
  - `RequestStart()`、`ClearWant()`、`WantPending() bool`、`Touch()`
  - `Manager{RuntimeDir, ModelsDir string; HealthTimeout, IdleTimeout time.Duration}`、`Ensure(m embed.BuiltinModel) (*State, error)`、`Stop()`、`Reconcile(desired *embed.BuiltinModel, now time.Time)`
  - `RuntimeServerPath(dir string) (string, error)`、`DefaultRuntimeDir() string`
  - 测试口：`var ServerCommand func(path string, args ...string) *exec.Cmd`（默认 `exec.Command`）

- [ ] **Step 1: 失败测试** `internal/embedsidecar/sidecar_test.go`

```go
package embedsidecar

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/embed"
)

// TestMain 模式：helper 进程伪装 llama-server（/health + 常驻）。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("OK_HELPER") != "1" {
		return
	}
	port := os.Getenv("OK_HELPER_PORT")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
	os.Exit(0)
}

// setupEnv：OK_HOME 隔离 + ServerCommand 替换为 helper 进程 + 假模型落盘。
func setupEnv(t *testing.T) (*Manager, embed.BuiltinModel) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	ServerCommand = func(path string, args ...string) *exec.Cmd {
		// 从 args 里挖 --port 值传给 helper
		port := ""
		for i, a := range args {
			if a == "--port" && i+1 < len(args) {
				port = args[i+1]
			}
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
		cmd.Env = append(os.Environ(), "OK_HELPER=1", "OK_HELPER_PORT="+port)
		return cmd
	}
	t.Cleanup(func() { ServerCommand = exec.Command })
	model := embed.BuiltinModel{ID: "fake", File: "fake.gguf", Size: 4, Pooling: "cls", Dim: 2}
	modelsDir := filepath.Join(home, "models")
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(model.InstalledPath(modelsDir), []byte("fake"), 0o644)
	rtDir := filepath.Join(home, "runtime")
	os.MkdirAll(rtDir, 0o755)
	os.WriteFile(filepath.Join(rtDir, serverExeName), []byte("x"), 0o755)
	mgr := &Manager{RuntimeDir: rtDir, ModelsDir: modelsDir, HealthTimeout: 10 * time.Second, IdleTimeout: 100 * time.Millisecond}
	t.Cleanup(mgr.Stop)
	return mgr, model
}

func TestEnsureHealthyAndStop(t *testing.T) {
	mgr, model := setupEnv(t)
	st, err := mgr.Ensure(model)
	if err != nil {
		t.Fatal(err)
	}
	if st.Port <= 0 || st.ModelID != "fake" || !st.Healthy() {
		t.Fatalf("%+v", st)
	}
	if LoadState() == nil {
		t.Fatal("state 应已落盘")
	}
	// 幂等：再次 Ensure 复用
	st2, err := mgr.Ensure(model)
	if err != nil || st2.Port != st.Port {
		t.Fatalf("应复用: %v %+v", err, st2)
	}
	mgr.Stop()
	if LoadState() != nil {
		t.Fatal("Stop 后 state 应删除")
	}
}

func TestReconcileLifecycle(t *testing.T) {
	mgr, model := setupEnv(t)
	now := time.Now() // 固定基准时间：Reconcile 的 now 参数化正是为测试确定性
	// 无 want 且 desired 未变 → 不拉起
	mgr.lastDesired = "fake"
	mgr.Reconcile(&model, now)
	if LoadState() != nil {
		t.Fatal("无 want 不应拉起")
	}
	// want → 拉起
	RequestStart()
	mgr.Reconcile(&model, now)
	st := LoadState()
	if st == nil || !st.Healthy() {
		t.Fatal("want 应触发拉起")
	}
	if WantPending() {
		t.Fatal("拉起后 want 应清除")
	}
	// 空闲超时 → 回收
	mgr.Reconcile(&model, now.Add(time.Hour))
	if LoadState() != nil {
		t.Fatal("空闲应回收")
	}
	// desired 消失 → 确保停止
	RequestStart()
	mgr.Reconcile(&model, now)
	mgr.Reconcile(nil, now)
	if LoadState() != nil {
		t.Fatal("desired 消失应停止")
	}
}

func TestWantFlagRoundTrip(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	if WantPending() {
		t.Fatal("初始无 want")
	}
	RequestStart()
	if !WantPending() {
		t.Fatal("want 应存在")
	}
	ClearWant()
	if WantPending() {
		t.Fatal("清除后无 want")
	}
}
```

- [ ] **Step 2: 确认失败**

Run: `go test ./internal/embedsidecar/`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现 sidecar.go**

```go
// Package embedsidecar 管理内置 embedding 推理 sidecar（llama-server）：
// 状态文件发现、want 拉起请求、静默 spawn、空闲回收。daemon 是唯一的
// 拉起/看护主体；hook/cli/gui 只读状态或写 want，绝不等待冷启动。
package embedsidecar

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"openknowledge/internal/registry"
)

// State 是 embed-sidecar.json 的内容：sidecar 发现与身份。
type State struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	ModelID   string    `json:"model_id"`
	StartedAt time.Time `json:"started_at"`
	LastUsed  time.Time `json:"last_used"`
}

func statePath() string { return filepath.Join(registry.Home(), "embed-sidecar.json") }
func wantPath() string  { return filepath.Join(registry.Home(), "embed-sidecar.want") }
func logPath() string   { return filepath.Join(registry.Home(), "embed-sidecar.log") }

// LoadState 读状态文件；不存在/解析失败返回 nil。
func LoadState() *State {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return nil
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

func writeState(s *State) error {
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0o644)
}

// BaseURL 是 sidecar 的 OpenAI 兼容入口。
func (s *State) BaseURL() string { return "http://127.0.0.1:" + strconv.Itoa(s.Port) + "/v1" }

// Healthy 以 800ms 预算探 /health（hook 热路径可接受的快速失败）。
func (s *State) Healthy() bool {
	if s == nil {
		return false
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(s.Port) + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// RequestStart 写 want 标记（幂等）：daemon 下一轮 reconcile 拉起 sidecar。
func RequestStart() {
	_ = os.MkdirAll(registry.Home(), 0o755)
	_ = os.WriteFile(wantPath(), []byte("1"), 0o644)
}

// ClearWant 清除 want 标记。
func ClearWant() { _ = os.Remove(wantPath()) }

// WantPending 报告 want 标记是否存在。
func WantPending() bool {
	_, err := os.Stat(wantPath())
	return err == nil
}

// Touch 更新 last_used（embedding 调用成功后由客户端包装层调用；失败静默）。
func Touch() {
	st := LoadState()
	if st == nil {
		return
	}
	st.LastUsed = time.Now()
	_ = writeState(st)
}
```

- [ ] **Step 4: 实现 manager.go**

```go
package embedsidecar

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"openknowledge/internal/embed"
)

// ServerCommand 是 spawn 接缝：测试替换为 helper 进程。生产即 exec.Command。
var ServerCommand = func(path string, args ...string) *exec.Cmd { return exec.Command(path, args...) }

// serverExeName 随平台（windows=llama-server.exe）。
var serverExeName = map[bool]string{true: "llama-server.exe", false: "llama-server"}[runtime.GOOS == "windows"]

// RuntimeServerPath 返回 <runtimeDir>/llama-server[.exe]；缺失时报错（裸 exe
// 便携形态无 runtime 目录 → 内置模式不可用）。
func RuntimeServerPath(runtimeDir string) (string, error) {
	p := filepath.Join(runtimeDir, serverExeName)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("推理运行时缺失（%s）——内置模式仅安装版可用", p)
	}
	return p, nil
}

// DefaultRuntimeDir 返回 <exe 所在目录>/runtime。
func DefaultRuntimeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "runtime")
}

// Manager 托管 sidecar 生命周期。仅 daemon 持有。
type Manager struct {
	RuntimeDir    string
	ModelsDir     string
	HealthTimeout time.Duration // Ensure 就绪等待上限（建议 90s）
	IdleTimeout   time.Duration // 空闲回收阈值（建议 10min）

	mu          sync.Mutex
	cmd         *exec.Cmd
	lastDesired string
	failCount   int
}

// Ensure 保证 model 对应 sidecar 在线（幂等）；返回可用 State。
func (m *Manager) Ensure(model embed.BuiltinModel) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := LoadState(); st != nil && st.ModelID == model.ID && st.Healthy() {
		m.failCount = 0
		return st, nil
	}
	m.stopLocked()
	server, err := RuntimeServerPath(m.RuntimeDir)
	if err != nil {
		return nil, err
	}
	modelPath := model.InstalledPath(m.ModelsDir)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("模型文件缺失: %s", modelPath)
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-m", modelPath,
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
		"--embeddings",
		"--pooling", model.Pooling,
	}
	cmd := ServerCommand(server, args...)
	hideWindow(cmd)
	logF, err := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		cmd.Stdout = logF
		cmd.Stderr = logF
	}
	if err := cmd.Start(); err != nil {
		if logF != nil {
			_ = logF.Close()
		}
		return nil, err
	}
	if logF != nil {
		_ = logF.Close() // 子进程已继承句柄
	}
	m.cmd = cmd
	st := &State{PID: cmd.Process.Pid, Port: port, ModelID: model.ID, StartedAt: time.Now(), LastUsed: time.Now()}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	deadline := time.Now().Add(m.HealthTimeout)
	for {
		if st.Healthy() {
			if err := writeState(st); err != nil {
				return nil, err
			}
			m.failCount = 0
			return st, nil
		}
		select {
		case err := <-waitCh:
			return nil, fmt.Errorf("llama-server 提前退出: %v（日志 %s）", err, logPath())
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("llama-server 就绪超时（%s）", m.HealthTimeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Stop 杀 sidecar 并删状态文件（幂等）。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_, _ = m.cmd.Process.Wait()
		m.cmd = nil
	} else if st := LoadState(); st != nil {
		// 跨进程残留（daemon 重启后 m.cmd 为空）：按 PID 杀
		if p, err := os.FindProcess(st.PID); err == nil {
			_ = p.Kill()
		}
	}
	_ = os.Remove(statePath())
}

// Reconcile 调和一次：desired=期望模型（nil=不需要 sidecar）。
// 拉起条件：desired 就绪 且（激活刚变化 或 want 标记 pending）；
// 停止条件：不需要/未就绪/模型切换/空闲超时。连续 3 次拉起失败进入
// 冷却（直到 desired 变化重试）。
func (m *Manager) Reconcile(desired *embed.BuiltinModel, now time.Time) {
	desiredID := ""
	if desired != nil {
		desiredID = desired.ID
	}
	changed := desiredID != m.lastDesired
	m.lastDesired = desiredID
	if changed {
		m.failCount = 0
	}
	if desiredID == "" || !desired.Installed(m.ModelsDir) {
		if LoadState() != nil {
			m.Stop()
		}
		ClearWant()
		return
	}
	st := LoadState()
	if st != nil && st.ModelID != desiredID {
		m.Stop()
		st = nil
	}
	if st == nil {
		if (changed || WantPending()) && m.failCount < 3 {
			if _, err := m.Ensure(*desired); err != nil {
				m.failCount++
			} else {
				ClearWant()
			}
		}
		return
	}
	if now.Sub(st.LastUsed) > m.IdleTimeout {
		m.Stop()
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
```

- [ ] **Step 5: spawn 平台文件**

`internal/embedsidecar/spawn_windows.go`：

```go
//go:build windows

package embedsidecar

import (
	"os/exec"
	"syscall"
)

// hideWindow 阻止 llama-server 弹出控制台窗口（与 daemon 子进程同策略）。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
```

`internal/embedsidecar/spawn_other.go`：

```go
//go:build !windows

package embedsidecar

import "os/exec"

func hideWindow(*exec.Cmd) {}
```


- [ ] **Step 6: 跑测试 + commit**

Run: `go test ./internal/embedsidecar/ -v`
Expected: PASS（helper 进程模式；Windows 上进程 Kill 后 Wait 返回错误属正常路径）

```bash
git add internal/embedsidecar
git commit -m "feat(embedsidecar): llama-server sidecar 托管——状态文件/want 拉起/静默 spawn/空闲回收/有界重启，仅 daemon 持有 Manager"
```

---

### Task 6: daemon janitor 集成

**Files:**
- Create: `internal/daemon/sidecar.go`
- Modify: `internal/daemon/run.go`（接入 janitor + defer Stop）
- Test: `internal/daemon/sidecar_test.go`

**Interfaces:**
- Consumes: Task 5 的 `Manager`/`Reconcile`；Task 4 的 `ActiveProfile`
- Produces: `desiredBuiltinModel(cfg config.Config) *embed.BuiltinModel`（包内函数）；`var sidecarJanitorInterval = 10 * time.Second`（测试可调）

- [ ] **Step 1: 失败测试** `internal/daemon/sidecar_test.go`

```go
package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
)

func TestDesiredBuiltinModel(t *testing.T) {
	var cfg config.Config
	if desiredBuiltinModel(cfg) != nil {
		t.Fatal("无 active 应 nil")
	}
	cfg.Embedding.Active = "a"
	cfg.Embedding.Profiles = []config.EmbeddingProfile{{Name: "a", Type: "openai", Model: "m", BaseURL: "h"}}
	if desiredBuiltinModel(cfg) != nil {
		t.Fatal("openai 应 nil")
	}
	cfg.Embedding.Profiles[0].Type = "builtin"
	cfg.Embedding.Profiles[0].Model = "qwen3-emb-0.6b-q8"
	m := desiredBuiltinModel(cfg)
	if m == nil || m.ID != "qwen3-emb-0.6b-q8" {
		t.Fatalf("%+v", m)
	}
	cfg.Embedding.Profiles[0].Model = "不存在"
	if desiredBuiltinModel(cfg) != nil {
		t.Fatal("未知清单 id 应 nil")
	}
}

// TestJanitorStartsSidecar：全局配置 active=内置 + 假模型就绪 → janitor 一轮内拉起。
func TestJanitorStartsSidecar(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	// 假模型 + 假 runtime
	model := embed.BuiltinModel{ID: "fake-j", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, model)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	modelsDir := filepath.Join(home, "models")
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(model.InstalledPath(modelsDir), []byte("fake"), 0o644)
	rtDir := filepath.Join(home, "runtime")
	os.MkdirAll(rtDir, 0o755)
	// helper 进程接缝（embedsidecar.ServerCommand 包级 var）
	oldSC := embedsidecar.ServerCommand
	embedsidecar.ServerCommand = helperServerCommand
	t.Cleanup(func() { embedsidecar.ServerCommand = oldSC })
	cfgText := "[embedding]\nactive = \"内\"\n[[embedding.profiles]]\nname = \"内\"\ntype = \"builtin\"\nmodel = \"fake-j\"\n"
	os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfgText), 0o600)

	mgr := &embedsidecar.Manager{RuntimeDir: rtDir, ModelsDir: modelsDir, HealthTimeout: 10 * time.Second, IdleTimeout: time.Hour}
	t.Cleanup(mgr.Stop)
	old := sidecarJanitorInterval
	sidecarJanitorInterval = 50 * time.Millisecond
	t.Cleanup(func() { sidecarJanitorInterval = old })
	go sidecarJanitor(mgr)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := embedsidecar.LoadState(); st != nil && st.Healthy() {
			return // 成功
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("janitor 未在 5s 内拉起 sidecar")
}
```

`helperServerCommand` 与 helper 进程放在 `internal/daemon/sidecar_test.go` 同文件（与 Task 5 同款模式；注意 daemon 包不能 import embedsidecar 的测试文件，故复制一份）：

```go
// TestSidecarHelperProcess 伪装 llama-server（仅 /health）。
func TestSidecarHelperProcess(t *testing.T) {
	if os.Getenv("OK_HELPER") != "1" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	_ = http.ListenAndServe("127.0.0.1:"+os.Getenv("OK_HELPER_PORT"), mux)
	os.Exit(0)
}

// helperServerCommand 从 args 挖 --port 并起 helper 进程。
func helperServerCommand(_ string, args ...string) *exec.Cmd {
	port := ""
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port = args[i+1]
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestSidecarHelperProcess")
	cmd.Env = append(os.Environ(), "OK_HELPER=1", "OK_HELPER_PORT="+port)
	return cmd
}
```

（ServerCommand 恢复约定：所有测试 cleanup 恢复**替换前的旧值**（生产默认是 `exec.Command` 的包装闭包），manager 实现不判 nil。）

- [ ] **Step 2: 确认失败**

Run: `go test ./internal/daemon/ -run 'TestDesired|TestJanitor'`
Expected: FAIL（`desiredBuiltinModel`/`sidecarJanitor` 未定义）

- [ ] **Step 3: 实现** `internal/daemon/sidecar.go`

```go
package daemon

import (
	"path/filepath"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
	"openknowledge/internal/registry"
)

// sidecarJanitorInterval sidecar 调和周期；测试可调小。
var sidecarJanitorInterval = 10 * time.Second

// desiredBuiltinModel 从全局配置解析期望的内置模型；非内置/未知清单 id 返回 nil。
func desiredBuiltinModel(cfg config.Config) *embed.BuiltinModel {
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Type != "builtin" {
		return nil
	}
	return embed.FindBuiltinModel(p.Model)
}

// sidecarJanitor 周期调和 embedding sidecar（active 为内置且模型就绪 → 在线；
// 空闲/切换/停用 → 回收）。配置变更经周期轮询自然生效（GUI/CLI 写全局配置）。
func sidecarJanitor(mgr *embedsidecar.Manager) {
	ticker := time.NewTicker(sidecarJanitorInterval)
	defer ticker.Stop()
	for range ticker.C {
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			continue
		}
		mgr.Reconcile(desiredBuiltinModel(cfg), time.Now())
	}
}
```

- [ ] **Step 4: 接入 `internal/daemon/run.go`**

在 `Run` 函数 `fmt.Fprintf(stdout, "OpenKnowledge daemon: %s\n", ...)` 之前插入：

```go
	// embedding sidecar 托管：active profile 为内置时按需保持 llama-server 在线；
	// daemon 退出时回收（sidecar 绝不留孤儿进程）
	sidecarMgr := &embedsidecar.Manager{
		RuntimeDir:    embedsidecar.DefaultRuntimeDir(),
		ModelsDir:     filepath.Join(registry.Home(), "models"),
		HealthTimeout: 90 * time.Second,
		IdleTimeout:   10 * time.Minute,
	}
	defer sidecarMgr.Stop()
	go sidecarJanitor(sidecarMgr)
```

import 增加 `openknowledge/internal/embedsidecar` 与 `openknowledge/internal/registry`（`filepath` 已有）。

- [ ] **Step 5: 全量测试 + commit**

Run: `go build ./... && go test ./internal/daemon/ ./internal/embedsidecar/`
Expected: PASS

```bash
git add internal/daemon
git commit -m "feat(daemon): embedding sidecar janitor——10s 周期按全局配置调和 llama-server，daemon 退出回收"
```

---

### Task 7: embedx builtin 分支 + setupx Ollama 探测 + CLI setup 三选一 + Doctor builtin

**Files:**
- Modify: `internal/embedx/embedx.go`（builtin 分支 + sidecarClient 包装）
- Test: `internal/embedx/embedx_test.go`
- Modify: `internal/setupx/setupx.go`（TestEmbeddingProfile builtin 分支 + ListOllamaModels）
- Test: `internal/setupx/setupx_test.go`
- Modify: `internal/cli/setup.go`（交互三选一）
- Modify: `internal/cli/cli.go:418-476`（Doctor builtin 细分提示）
- Test: `internal/cli/setup_test.go`

**Interfaces:**
- Consumes: Task 5 `embedsidecar` 全部；Task 3 `embed.Download`；Task 2 `embed.BuiltinModels`
- Produces: `setupx.ListOllamaModels(baseURL string) ([]string, error)`

- [ ] **Step 1: embedx builtin 失败测试**（追加 `internal/embedx/embedx_test.go`）

```go
// 假 llama-server：httptest 同时服务 /health 与 /v1/embeddings
func fakeSidecar(t *testing.T) (port int, closeFn func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[{"embedding":[0.1,0.2],"index":0}]}`)
	})
	srv := httptest.NewServer(mux)
	port = srv.Listener.Addr().(*net.TCPAddr).Port
	return port, srv.Close
}

func TestBuiltinClientViaState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	port, closeFn := fakeSidecar(t)
	defer closeFn()
	m := embed.BuiltinModel{ID: "fake-x", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2, QueryPrefix: "Q:"}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	// 写 state（假装 daemon 已拉起）
	st := map[string]any{"pid": 1, "port": port, "model_id": "fake-x",
		"started_at": time.Now(), "last_used": time.Now()}
	data, _ := json.Marshal(st)
	os.WriteFile(filepath.Join(home, "embed-sidecar.json"), data, 0o644)

	p := config.EmbeddingProfile{Name: "内", Type: "builtin", Model: "fake-x"}
	c := ClientForProfile(p, 3*time.Second)
	if c == nil {
		t.Fatal("state 健康应返回客户端")
	}
	if c.ModelIdentity() != "builtin:fake-x" {
		t.Fatal(c.ModelIdentity())
	}
	if _, err := c.EmbedQuery(context.Background(), "ping"); err != nil {
		t.Fatal(err)
	}
	// Touch 生效：last_used 被刷新（读回不报错即覆盖路径走到）
	if got := embedsidecar.LoadState(); got == nil || got.LastUsed.IsZero() {
		t.Fatal("Touch 未生效")
	}
}

func TestBuiltinClientNotReadyWritesWant(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	m := embed.BuiltinModel{ID: "fake-y", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	p := config.EmbeddingProfile{Name: "内", Type: "builtin", Model: "fake-y"}
	if c := ClientForProfile(p, time.Second); c != nil {
		t.Fatal("无 state 应为 nil（降级）")
	}
	if !embedsidecar.WantPending() {
		t.Fatal("应写 want 请求 daemon 拉起")
	}
}
```

（import：context/encoding/json/fmt/net/http/httptest/os/path/filepath/testing/time + config/embed/embedsidecar。）

- [ ] **Step 2: 确认失败**

Run: `go test ./internal/embedx/`
Expected: FAIL（builtin 分支返回 nil，WantPending 未定义于本包断言路径）

- [ ] **Step 3: embedx builtin 分支实现**

`internal/embedx/embedx.go`：`case "builtin":` 的 `return nil` 替换为：

```go
	case "builtin":
		m := embed.FindBuiltinModel(p.Model)
		if m == nil {
			return nil
		}
		return builtinClient(*m, p, timeout)
```

文件末尾追加（import 增 `context` 与 `openknowledge/internal/embedsidecar`）：

```go
// builtinClient 经 sidecar 状态文件发现端口；未就绪写 want 请求 daemon 拉起
// 并返回 nil（调用方走纯关键词降级，绝不等待冷启动）。
func builtinClient(m embed.BuiltinModel, p config.EmbeddingProfile, timeout time.Duration) embed.Client {
	st := embedsidecar.LoadState()
	if st == nil || st.ModelID != m.ID || !st.Healthy() {
		embedsidecar.RequestStart()
		return nil
	}
	return sidecarClient{&embed.OpenAIClient{
		BaseURL:     st.BaseURL(),
		Model:       m.File,
		Timeout:     timeout,
		Identity:    p.ModelIdentity(),
		QueryPrefix: m.QueryPrefix,
		DocPrefix:   m.DocPrefix,
	}}
}

// sidecarClient 在成功调用后 Touch last_used（daemon 空闲回收依据）。
type sidecarClient struct{ *embed.OpenAIClient }

func (s sidecarClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	v, err := s.OpenAIClient.EmbedQuery(ctx, text)
	if err == nil {
		embedsidecar.Touch()
	}
	return v, err
}

func (s sidecarClient) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	v, err := s.OpenAIClient.EmbedDocument(ctx, text)
	if err == nil {
		embedsidecar.Touch()
	}
	return v, err
}

func (s sidecarClient) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	v, err := s.OpenAIClient.EmbedDocuments(ctx, texts)
	if err == nil {
		embedsidecar.Touch()
	}
	return v, err
}
```

- [ ] **Step 4: setupx 扩展**

`internal/setupx/setupx.go`（import 增 `encoding/json`、`errors`、`net/http`、`openknowledge/internal/embed`、`openknowledge/internal/embedsidecar`）：

`TestEmbeddingProfile` 整体替换为：

```go
// TestEmbeddingProfile 以 timeout 做 profile 连通性检查。
// builtin：检查 runtime/模型文件，sidecar 未就绪时写 want 并返回"启动中"提示性错误。
func TestEmbeddingProfile(p config.EmbeddingProfile, timeout time.Duration) error {
	if p.Type == "builtin" {
		m := embed.FindBuiltinModel(p.Model)
		if m == nil {
			return fmt.Errorf("未知内置模型: %s", p.Model)
		}
		if _, err := embedsidecar.RuntimeServerPath(embedsidecar.DefaultRuntimeDir()); err != nil {
			return err
		}
		if !m.Installed(filepath.Join(registry.Home(), "models")) {
			return errors.New("模型未下载（先在配置弹窗或 ok setup 中下载）")
		}
		c := embedx.ClientForProfile(p, timeout)
		if c == nil {
			return errors.New("sidecar 未就绪——已请求 daemon 拉起，稍后自动生效（数秒到一分钟）")
		}
		_, err := c.EmbedQuery(context.Background(), "ping")
		return err
	}
	c := embedx.ClientForProfile(p, timeout)
	if c == nil {
		return fmt.Errorf("profile 不可用（类型 %s，检查必填项）", p.Type)
	}
	_, err := c.EmbedQuery(context.Background(), "ping")
	return err
}

// ListOllamaModels 探测 Ollama 已安装模型（GET {base}/api/tags，3s 超时）。
func ListOllamaModels(baseURL string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/api/tags"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API %d", resp.StatusCode)
	}
	var tr struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tr.Models))
	for _, m := range tr.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
```

`setupx_test.go` 追加：`TestListOllamaModels`（httptest 返回 `{"models":[{"name":"bge-m3:latest"}]}` 断言解析）与 `TestTestEmbeddingProfileBuiltinNoRuntime`（OK_HOME 隔离 + DefaultRuntimeDir 无 llama-server → 错误含"仅安装版可用"）。

- [ ] **Step 5: CLI setup 三选一**

`internal/cli/setup.go`：`setupEmbedding` 整体替换为（import 增 `context`、`openknowledge/internal/config`、`openknowledge/internal/embed`、`openknowledge/internal/embedsidecar`）：

```go
// setupEmbedding 交互三选一（线上/Ollama/内置）或按 flags 写入（flags 向后兼容，
// 固定写 openai "默认" profile）。
func setupEmbedding(nonInteractive bool, baseURL, model, apiKey string, in io.Reader, stdout io.Writer) {
	if nonInteractive {
		if apiKey == "" {
			fmt.Fprintln(stdout, "跳过 embedding 配置（仅关键词检索；之后可重跑 ok setup 配置）")
			return
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		if model == "" {
			model = "text-embedding-3-small"
		}
		saveAndTestProfile(config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: baseURL, Model: model, APIKey: apiKey}, stdout)
		return
	}
	fmt.Fprintln(stdout, "\n配置 embedding 语义检索（可选，直接回车跳过）：")
	fmt.Fprintln(stdout, "  1) 线上 OpenAI 兼容服务")
	fmt.Fprintln(stdout, "  2) Ollama（本机/局域网，免 key）")
	fmt.Fprintln(stdout, "  3) 内置本地模型（ok 托管，完全离线）")
	r := bufio.NewReader(in)
	fmt.Fprint(stdout, "选择 [1/2/3]: ")
	choice, _ := r.ReadString('\n')
	switch strings.TrimSpace(choice) {
	case "1":
		fmt.Fprintf(stdout, "base_url [https://api.openai.com/v1]: ")
		baseURL, _ = r.ReadString('\n')
		baseURL = strings.TrimSpace(baseURL)
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		fmt.Fprintf(stdout, "model [text-embedding-3-small]: ")
		model, _ = r.ReadString('\n')
		model = strings.TrimSpace(model)
		if model == "" {
			model = "text-embedding-3-small"
		}
		fmt.Fprintf(stdout, "API key（粘贴后回车；留空跳过）: ")
		apiKey, _ = r.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			fmt.Fprintln(stdout, "跳过 embedding 配置（仅关键词检索）")
			return
		}
		saveAndTestProfile(config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: baseURL, Model: model, APIKey: apiKey}, stdout)
	case "2":
		fmt.Fprintf(stdout, "Ollama 地址 [http://localhost:11434]: ")
		base, _ := r.ReadString('\n')
		base = strings.TrimSpace(base)
		if base == "" {
			base = "http://localhost:11434"
		}
		if models, err := setupx.ListOllamaModels(base); err != nil {
			fmt.Fprintf(stdout, "Ollama 探测失败（%v），按手动输入继续\n", err)
		} else if len(models) > 0 {
			fmt.Fprintln(stdout, "已安装模型："+strings.Join(models, "，"))
		}
		fmt.Fprintf(stdout, "模型 [bge-m3]: ")
		m, _ := r.ReadString('\n')
		m = strings.TrimSpace(m)
		if m == "" {
			m = "bge-m3"
		}
		saveAndTestProfile(config.EmbeddingProfile{Name: "Ollama 本机", Type: "ollama", BaseURL: base, Model: m}, stdout)
	case "3":
		setupEmbeddingBuiltin(r, stdout)
	default:
		fmt.Fprintln(stdout, "跳过 embedding 配置（仅关键词检索；之后可重跑 ok setup 配置）")
	}
}

// saveAndTestProfile 保存并激活 profile，然后连通性验证。
func saveAndTestProfile(p config.EmbeddingProfile, stdout io.Writer) {
	if err := setupx.SaveEmbeddingProfile(p, true); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	fmt.Fprintf(stdout, "embedding 已写入全局配置 %s\n", filepath.Join(registry.Home(), "config.toml"))
	if err := setupx.TestEmbeddingProfile(p, 10*time.Second); err != nil {
		fmt.Fprintf(stdout, "embedding 连通性验证：%v\n", err)
	} else {
		fmt.Fprintln(stdout, "embedding 连通性验证通过")
	}
}

// setupEmbeddingBuiltin 内置模型：选档位 → 选镜像 → 按需下载（进度行）→ 激活 + 请求拉起。
func setupEmbeddingBuiltin(r *bufio.Reader, stdout io.Writer) {
	if _, err := embedsidecar.RuntimeServerPath(embedsidecar.DefaultRuntimeDir()); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	fmt.Fprintln(stdout, "可选模型：")
	for i, m := range embed.BuiltinModels {
		fmt.Fprintf(stdout, "  %d) %s\n", i+1, m.Label)
	}
	fmt.Fprint(stdout, "选择 [1]: ")
	sel, _ := r.ReadString('\n')
	idx := 1
	fmt.Sscanf(strings.TrimSpace(sel), "%d", &idx)
	if idx < 1 || idx > len(embed.BuiltinModels) {
		idx = 1
	}
	m := embed.BuiltinModels[idx-1]
	fmt.Fprint(stdout, "下载源 [1=hf-mirror 国内镜像（默认） 2=huggingface 官方]: ")
	ms, _ := r.ReadString('\n')
	mirror := "hf-mirror"
	if strings.TrimSpace(ms) == "2" {
		mirror = "huggingface"
	}
	modelsDir := filepath.Join(registry.Home(), "models")
	if !m.Installed(modelsDir) {
		fmt.Fprintf(stdout, "开始下载 %s …\n", m.File)
		err := embed.Download(context.Background(), nil, m, mirror, modelsDir, func(done, total int64) {
			fmt.Fprintf(stdout, "\r  %d / %d MB", done>>20, total>>20)
		})
		fmt.Fprintln(stdout)
		if err != nil {
			fmt.Fprintf(stdout, "下载失败：%v（重跑 ok setup 可断点续传）\n", err)
			return
		}
	}
	p := config.EmbeddingProfile{Name: "内置 " + m.ID, Type: "builtin", Model: m.ID, Mirror: mirror}
	if err := setupx.SaveEmbeddingProfile(p, true); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	embedsidecar.RequestStart()
	fmt.Fprintln(stdout, "已设为使用中；sidecar 由 daemon 自动拉起（首次数秒到一分钟），期间检索退化为关键词")
}
```

- [ ] **Step 6: setup 测试**

`internal/cli/setup_test.go` 追加（沿用该文件已有的 OK_HOME 隔离惯例；若既有用例断言旧交互提示词如 `base_url [`，按新菜单文案同步调整）：

```go
func TestSetupEmbeddingMenuSkip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	var out strings.Builder
	setupEmbedding(false, "", "", "", strings.NewReader("\n"), &out)
	if !strings.Contains(out.String(), "跳过") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("跳过不应写配置")
	}
}

func TestSetupEmbeddingMenuOpenAI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"embedding":[0.1],"index":0}]}`)
	}))
	defer srv.Close()
	in := "1\n" + srv.URL + "/v1\nm\nsk-x\n" // 选 1 → base_url → model → key
	var out strings.Builder
	setupEmbedding(false, "", "", "", strings.NewReader(in), &out)
	cfg, err := config.LoadMerged("", filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Type != "openai" || p.Model != "m" || p.ResolvedAPIKey() != "sk-x" {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(out.String(), "验证通过") {
		t.Fatal(out.String())
	}
}

func TestSetupEmbeddingMenuBuiltin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	m := embed.BuiltinModel{ID: "fake-cli", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	// 预置已下载模型（跳过真实下载）
	modelsDir := filepath.Join(home, "models")
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(m.InstalledPath(modelsDir), []byte("fake"), 0o644)
	// 假 runtime：测试二进制所在目录建 runtime/llama-server[.exe]
	exe, _ := os.Executable()
	rtDir := filepath.Join(filepath.Dir(exe), "runtime")
	os.MkdirAll(rtDir, 0o755)
	serverName := "llama-server"
	if runtime.GOOS == "windows" {
		serverName = "llama-server.exe"
	}
	os.WriteFile(filepath.Join(rtDir, serverName), []byte("x"), 0o755)
	t.Cleanup(func() { os.RemoveAll(rtDir) })

	idx := len(embed.BuiltinModels) // 假模型在清单末尾
	in := "3\n" + strconv.Itoa(idx) + "\n\n" // 选 3 → 选模型 → 默认镜像
	var out strings.Builder
	setupEmbedding(false, "", "", "", strings.NewReader(in), &out)
	cfg, _ := config.LoadMerged("", filepath.Join(home, "config.toml"))
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Type != "builtin" || p.Model != "fake-cli" || p.Mirror != "hf-mirror" {
		t.Fatalf("%+v", p)
	}
	if !embedsidecar.WantPending() {
		t.Fatal("激活内置应写 want 标记")
	}
}
```

- [ ] **Step 7: Doctor builtin 细分**

`internal/cli/cli.go:459-469` 块替换为：

```go
		client := embeddingClient(pc)
		if client == nil {
			switch prof := pc.Config.Embedding.ActiveProfile(); {
			case prof == nil:
				fmt.Fprintf(stdout, "[%s] 未配置 embedding（仅关键词检索可用）\n", p.Name)
			case prof.Type == "builtin":
				fmt.Fprintf(stdout, "[%s] 内置 embedding sidecar 未就绪（确认 daemon 运行中，或模型未下载）\n", p.Name)
			default:
				fmt.Fprintf(stdout, "[%s] embedding profile 不完整（重跑 ok setup 或在 GUI 配置）\n", p.Name)
			}
			continue
		}
		if _, err := client.EmbedQuery(context.Background(), "ping"); err != nil {
			fmt.Fprintf(stdout, "[%s] embedding 不可用: %v\n", p.Name, err)
			healthy = false
		} else {
			fmt.Fprintf(stdout, "[%s] embedding 正常（%s）\n", p.Name, client.ModelIdentity())
		}
```

- [ ] **Step 8: 全量测试 + commit**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS

```bash
git add internal/embedx internal/setupx internal/cli
git commit -m "feat(setup): 内置/ollama 形态接入——embedx builtin 分支（sidecar 发现+want+Touch）、ok setup 三选一交互、Ollama 模型探测、doctor 细分提示"
```

---

### Task 8: index meta 表 + Sync 批量重构 + 身份防混合

**Files:**
- Modify: `internal/index/db.go`（meta 表 + SetMeta/GetMeta/EmbeddingMeta/ClearVectors）
- Modify: `internal/index/sync.go`（批量收集 + 身份不符阻断向量写）
- Test: `internal/index/index_test.go`（追加）或新建 `internal/index/meta_test.go`

**Interfaces:**
- Produces:
  - `(db *DB) SetMeta(key, value string) error`、`GetMeta(key string) (string, error)`（不存在返回 `"", nil`）
  - `(db *DB) EmbeddingMeta() (model string, dim int, err error)`
  - `(db *DB) ClearVectors() error`（清 vectors 表 + 删 embedding_model/embedding_dim 两条 meta）
  - Sync 新语义：client 身份与 meta 不符 → **跳过全部向量写与 meta 更新**（INDEX/FTS 照常），杜绝混合向量

- [ ] **Step 1: 失败测试** `internal/index/meta_test.go`

```go
package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// batchFake 记录 EmbedDocuments 批次的假客户端（实现 embed.Client）。
type batchFake struct {
	identity string
	batches  []int
}

func (f *batchFake) ModelIdentity() string { return f.identity }
func (f *batchFake) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return f.EmbedDocument(ctx, text)
}
func (f *batchFake) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	v, err := f.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}
func (f *batchFake) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	f.batches = append(f.batches, len(texts))
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t)), 1, 0}
	}
	return out, nil
}

func writeEntries(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("---\ntitle: 条目%d\ntype: note\n---\n正文%d", i, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("e%03d.md", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncBatchAndMeta(t *testing.T) {
	dir := t.TempDir()
	writeEntries(t, dir, 70)
	db, err := Open(filepath.Join(dir, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f := &batchFake{identity: "openai:m@h"}
	if err := db.Sync(dir, f); err != nil {
		t.Fatal(err)
	}
	if len(f.batches) != 3 || f.batches[0] != 32 || f.batches[2] != 6 {
		t.Fatalf("应按 32 分批: %v", f.batches)
	}
	model, dim, err := db.EmbeddingMeta()
	if err != nil || model != "openai:m@h" || dim != 3 {
		t.Fatalf("meta: %s %d %v", model, dim, err)
	}
}

func TestSyncIdentityMismatchSkipsVectors(t *testing.T) {
	dir := t.TempDir()
	writeEntries(t, dir, 3)
	db, _ := Open(filepath.Join(dir, "kb.db"))
	defer db.Close()
	f1 := &batchFake{identity: "openai:m1@h"}
	if err := db.Sync(dir, f1); err != nil {
		t.Fatal(err)
	}
	// 换一个身份的 client 同步：向量与 meta 都应保持旧模型的
	f2 := &batchFake{identity: "builtin:qwen3-emb-0.6b-q8"}
	if err := db.Sync(dir, f2); err != nil {
		t.Fatal(err)
	}
	if len(f2.batches) != 0 {
		t.Fatal("身份不符不应算向量")
	}
	model, _, _ := db.EmbeddingMeta()
	if model != "openai:m1@h" {
		t.Fatal("meta 不应被覆盖")
	}
	// ClearVectors 后全量重建
	if err := db.ClearVectors(); err != nil {
		t.Fatal(err)
	}
	if err := db.Sync(dir, f2); err != nil {
		t.Fatal(err)
	}
	if len(f2.batches) != 1 || f2.batches[0] != 3 {
		t.Fatalf("清向量后应全量重建: %v", f2.batches)
	}
	model, _, _ = db.EmbeddingMeta()
	if model != "builtin:qwen3-emb-0.6b-q8" {
		t.Fatal("重建后 meta 应更新")
	}
}
```

- [ ] **Step 2: 确认失败**

Run: `go test ./internal/index/ -run 'TestSyncBatch|TestSyncIdentity'`
Expected: FAIL（EmbeddingMeta/ClearVectors 未定义）

- [ ] **Step 3: db.go 增加 meta**

schema 常量追加：

```sql
CREATE TABLE IF NOT EXISTS meta(
  key TEXT PRIMARY KEY, value TEXT NOT NULL
);
```

db.go 追加（import 增 `strconv`；`errors`/`database/sql` 已有）：

```go
// SetMeta 写 kb 级元数据（embedding 模型身份等）。
func (db *DB) SetMeta(key, value string) error {
	_, err := db.sql.Exec(
		`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

// GetMeta 读元数据；不存在返回 ("", nil)。
func (db *DB) GetMeta(key string) (string, error) {
	var v string
	err := db.sql.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// EmbeddingMeta 返回建索引的模型身份与维度；未记录返回 ("", 0, nil)。
func (db *DB) EmbeddingMeta() (string, int, error) {
	model, err := db.GetMeta("embedding_model")
	if err != nil {
		return "", 0, err
	}
	ds, err := db.GetMeta("embedding_dim")
	if err != nil {
		return "", 0, err
	}
	dim := 0
	if ds != "" {
		dim, _ = strconv.Atoi(ds)
	}
	return model, dim, nil
}

// ClearVectors 清空向量表并复位 embedding 身份 meta（模型切换后的全量重建前置）。
func (db *DB) ClearVectors() error {
	if _, err := db.sql.Exec(`DELETE FROM vectors`); err != nil {
		return err
	}
	_, err := db.sql.Exec(`DELETE FROM meta WHERE key IN ('embedding_model','embedding_dim')`)
	return err
}
```

- [ ] **Step 4: sync.go 批量重构 + 身份防混合**

改动点（其余逻辑不变）：

a) 函数开头（`alive := ...` 之前）插入身份闸：

```go
	// 模型身份闸：client 身份与索引 meta 不符时跳过全部向量写（INDEX/FTS 照常），
	// 杜绝新旧模型向量混合；由 ok index 显式 ClearVectors 后全量重建。
	embedBlocked := client == nil
	if client != nil && client.ModelIdentity() != "" {
		if m, _, err := db.EmbeddingMeta(); err == nil && m != "" && m != client.ModelIdentity() {
			embedBlocked = true
		}
	}
```

b) "未变化条目补向量"分支（现 `sync.go:131-147`）与"变化条目"分支（:182-191）中的向量直写删除，改为收集：

```go
	type pendingEmbed struct{ name, text string }
	var pending []pendingEmbed
```

未变化分支内：
```go
		if old, ok := existing[name]; ok && old == mtime {
			// 未变化条目不读不解析；仅在缺向量且可算向量时收集补齐
			if !embedBlocked && !hasVector[name] {
				e, err := readEntry(f.path)
				if err != nil {
					return rollback(err)
				}
				pending = append(pending, pendingEmbed{name, e.EmbedText()})
			}
			continue
		}
```

变化分支把 `if client != nil { vec... }` 块替换为：
```go
		if !embedBlocked {
			pending = append(pending, pendingEmbed{name, e.EmbedText()})
		}
```

c) 多余条目删除循环之后、`tx.Commit()` 之前，批量算向量：

```go
	const embedBatchSize = 32
	vecDim := 0
	for i := 0; i < len(pending); i += embedBatchSize {
		j := i + embedBatchSize
		if j > len(pending) {
			j = len(pending)
		}
		texts := make([]string, 0, j-i)
		for _, p := range pending[i:j] {
			texts = append(texts, p.text)
		}
		vecs, err := client.EmbedDocuments(context.Background(), texts)
		if err != nil {
			return rollback(err)
		}
		for k, vec := range vecs {
			vecDim = len(vec)
			if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
				pending[i+k].name, len(vec), encodeVector(vec)); err != nil {
				return rollback(err)
			}
		}
	}
```

d) `tx.Commit()` 之后刷新 meta（身份非空才算数，防止测试直连 client 覆盖真实身份）：

```go
	if !embedBlocked && client != nil && vecDim > 0 && client.ModelIdentity() != "" {
		if err := db.SetMeta("embedding_model", client.ModelIdentity()); err != nil {
			return err
		}
		if err := db.SetMeta("embedding_dim", strconv.Itoa(vecDim)); err != nil {
			return err
		}
	}
```

（sync.go import 增 `strconv`。）

- [ ] **Step 5: 全量测试 + commit**

Run: `go build ./... && go test ./internal/index/ ./internal/hook/ ./internal/cli/ ./internal/gui/`
Expected: PASS（注意既有 fake client 需实现新接口全部方法）

```bash
git add internal/index
git commit -m "feat(index): meta 表记录 embedding 模型身份+维度——Sync 批量算向量（32/批），身份不符阻断向量写杜绝混合模型向量"
```

---

### Task 9: 身份守卫 embedx.QueryVec + ok index 切换重建

**Files:**
- Modify: `internal/embedx/embedx.go`（新增 QueryVec）
- Test: `internal/embedx/queryvec_test.go`
- Modify: `internal/cli/cli.go:329-336`（Search）+ `:355-391`（Index）
- Modify: `internal/hook/core.go:96-103`

**Interfaces:**
- Consumes: Task 8 的 `EmbeddingMeta/ClearVectors`；Task 4 的构造点
- Produces: `embedx.QueryVec(db *index.DB, client embed.Client, queryVec []float32) (usable []float32, warn string)`

- [ ] **Step 1: 失败测试** `internal/embedx/queryvec_test.go`

```go
package embedx

import (
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/embed"
	"openknowledge/internal/index"
)

func openTestDB(t *testing.T) *index.DB {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestQueryVecGuard(t *testing.T) {
	db := openTestDB(t)
	c := &embed.OpenAIClient{Identity: "openai:m1@h"}
	vec := []float32{1, 2, 3}

	// 无 meta 记录 → 放行
	got, warn := QueryVec(db, c, vec)
	if warn != "" || len(got) != 3 {
		t.Fatal("无记录应放行")
	}
	// 身份一致 → 放行
	db.SetMeta("embedding_model", "openai:m1@h")
	db.SetMeta("embedding_dim", "3")
	got, warn = QueryVec(db, c, vec)
	if warn != "" || len(got) != 3 {
		t.Fatal("一致应放行")
	}
	// 身份不符 → 拦截 + 提示
	got, warn = QueryVec(db, &embed.OpenAIClient{Identity: "builtin:qwen3-emb-0.6b-q8"}, vec)
	if got != nil || !strings.Contains(warn, "ok index") {
		t.Fatalf("应拦截: %v %q", got, warn)
	}
	// 维度不符 → 拦截
	got, warn = QueryVec(db, c, []float32{1, 2})
	if got != nil || warn == "" {
		t.Fatal("维度不符应拦截")
	}
	// 旧式客户端（Identity 空）→ 放行
	got, warn = QueryVec(db, &embed.OpenAIClient{}, vec)
	if warn != "" || len(got) != 3 {
		t.Fatal("空身份应放行")
	}
}
```

- [ ] **Step 2: 确认失败**

Run: `go test ./internal/embedx/ -run TestQueryVecGuard`
Expected: FAIL（QueryVec 未定义）

- [ ] **Step 3: 实现 QueryVec**（`internal/embedx/embedx.go` 追加；import 增 `fmt` 与 `openknowledge/internal/index`）

```go
// QueryVec 判定 queryVec 能否进入语义通道：索引的模型身份与当前客户端不符
// （或维度不符）时返回 nil + 中文提示（调用方决定展示层级：CLI stderr / hook 日志）。
// 无 meta 记录（从未算过向量）或旧式客户端（身份空）不拦截。
func QueryVec(db *index.DB, client embed.Client, queryVec []float32) ([]float32, string) {
	if client == nil || len(queryVec) == 0 || client.ModelIdentity() == "" {
		return queryVec, ""
	}
	model, dim, err := db.EmbeddingMeta()
	if err != nil || model == "" {
		return queryVec, ""
	}
	if model != client.ModelIdentity() || (dim > 0 && dim != len(queryVec)) {
		return nil, fmt.Sprintf(
			"embedding 模型已切换（索引=%s，当前=%s），本次退化为关键词检索；运行 ok index 重建后恢复",
			model, client.ModelIdentity())
	}
	return queryVec, ""
}
```

- [ ] **Step 4: 接入三处**

`internal/cli/cli.go` Search（:329-336 块替换）：

```go
	var queryVec []float32
	if client := embeddingClient(pc); client != nil {
		if vec, err := client.EmbedQuery(context.Background(), query); err != nil {
			fmt.Fprintf(stderr, "embedding 失败，降级为关键词检索: %v\n", err)
		} else {
			var warn string
			queryVec, warn = embedx.QueryVec(db, client, vec)
			if warn != "" {
				fmt.Fprintln(stderr, warn)
			}
		}
	}
```

`internal/hook/core.go`（:96-103 块替换）：

```go
	var queryVec []float32
	if client != nil {
		if vec, err := client.EmbedQuery(context.Background(), promptText); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			var warn string
			queryVec, warn = embedx.QueryVec(db, client, vec)
			if warn != "" {
				logErr("prompt embed identity: %s", warn)
			}
		}
	}
```

`internal/cli/cli.go` Index（`:366-369` client 构造之后、`db.Sync` 之前插入；结尾输出调整）：

```go
	if client != nil && client.ModelIdentity() != "" {
		if m, _, err := db.EmbeddingMeta(); err == nil && m != "" && m != client.ModelIdentity() {
			fmt.Fprintf(stdout, "embedding 模型已切换（%s → %s），重建全部向量…\n", m, client.ModelIdentity())
			if err := db.ClearVectors(); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
	}
```

成功输出的 `INDEX 已重建；索引共 %d 条` 改为：

```go
	fmt.Fprintf(stdout, "INDEX 已重建；索引共 %d 条（embedding：%s）\n", n, client.ModelIdentity())
```

（client==nil 分支文案不变。）

- [ ] **Step 5: 全量测试 + commit**

Run: `go build ./... && go test ./internal/embedx/ ./internal/cli/ ./internal/hook/ ./internal/index/`
Expected: PASS

```bash
git add internal/embedx internal/cli internal/hook
git commit -m "feat(retrieve): 模型身份守卫——身份/维度不符语义通道显式跳过并提示 ok index，替代维度不等静默归零；ok index 切换模型自动清向量重建"
```

---

### Task 10: GUI 后端 API（profiles CRUD / 激活 / 测试 / 下载）

**Files:**
- Create: `internal/gui/embedding.go`（新端点全部在此；api.go 已 1130 行，不再塞）
- Modify: `internal/gui/api.go`（Handler 加下载管理字段；路由替换；apiStatus embedding 段改新结构；删除旧 `apiSetupEmbedding`）
- Test: `internal/gui/embedding_test.go`

**Interfaces:**
- Consumes: Task 3 `embed.Download`、Task 4 `setupx` 三件套、Task 7 `TestEmbeddingProfile`/`ListOllamaModels`、Task 5 `embedsidecar`
- Produces（前端 Task 11 依赖的响应形状）:
  - `GET /api/setup/embedding` → `{active, runtime_available, builtin_models:[{id,label,size,dim,downloaded}], download:{model_id,state,done,total,error}, profiles:[{name,type,base_url,model,has_key,mirror,downloaded}]}`
  - `POST /api/setup/embedding/profile` body `{name,type,base_url,model,api_key,mirror}`（api_key 空=保留旧值）
  - `POST /api/setup/embedding/active` body `{name}`（builtin 未下载 → 400）
  - `DELETE /api/setup/embedding/profile` body `{name}`
  - `POST /api/setup/embedding/test` body `{name}` → `{ok,error}`
  - `POST /api/setup/embedding/download` body `{model_id,mirror}`；`POST /api/setup/embedding/download/cancel` body `{model_id}`
  - `GET /api/setup/embedding/ollama-models?base_url=` → `{models:[...]}` 或 `{error}`

- [ ] **Step 1: 失败测试** `internal/gui/embedding_test.go`

```go
package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("OK_HOME", t.TempDir())
	return NewHandler(t.TempDir(), "tok", nil)
}

func embGet(t *testing.T, h *Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/setup/embedding", nil)
	req.Header.Set("X-Ok-Token", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func embPost(t *testing.T, h *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("X-Ok-Token", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestProfileSaveActivateDeleteCycle(t *testing.T) {
	h := newTestHandler(t)
	w := embPost(t, h, "/api/setup/embedding/profile",
		`{"name":"a","type":"openai","base_url":"http://h/v1","model":"m","api_key":"k"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	st := embGet(t, h)
	profiles := st["profiles"].([]any)
	if len(profiles) != 1 || profiles[0].(map[string]any)["has_key"] != true {
		t.Fatalf("%v", profiles)
	}
	if st["active"] != "" {
		t.Fatal("保存不自动激活")
	}
	embPost(t, h, "/api/setup/embedding/active", `{"name":"a"}`)
	if st := embGet(t, h); st["active"] != "a" {
		t.Fatal("激活失败")
	}
	// key 留空保留
	embPost(t, h, "/api/setup/embedding/profile", `{"name":"a","type":"openai","base_url":"http://h/v1","model":"m2"}`)
	cfg, _ := config.LoadMerged("", filepath.Join(os.Getenv("OK_HOME"), "config.toml"))
	if cfg.Embedding.Profiles[0].ResolvedAPIKey() != "k" || cfg.Embedding.Profiles[0].Model != "m2" {
		t.Fatal("key 应保留、model 应更新")
	}
	req := httptest.NewRequest("DELETE", "/api/setup/embedding/profile", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("X-Ok-Token", "tok")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	st = embGet(t, h)
	if st["active"] != "" || len(st["profiles"].([]any)) != 0 {
		t.Fatal("删除使用中 profile 应清空 active")
	}
}

func TestActivateBuiltinRequiresDownload(t *testing.T) {
	h := newTestHandler(t)
	m := embed.BuiltinModel{ID: "fake-g", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	embPost(t, h, "/api/setup/embedding/profile", `{"name":"内","type":"builtin","model":"fake-g","mirror":"hf-mirror"}`)
	w := embPost(t, h, "/api/setup/embedding/active", `{"name":"内"}`)
	if w.Code != 400 {
		t.Fatal("未下载应 400")
	}
	// 落盘模型后可激活
	os.MkdirAll(filepath.Join(os.Getenv("OK_HOME"), "models"), 0o755)
	os.WriteFile(m.InstalledPath(filepath.Join(os.Getenv("OK_HOME"), "models")), []byte("fake"), 0o644)
	if w := embPost(t, h, "/api/setup/embedding/active", `{"name":"内"}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
}

func TestDownloadLifecycle(t *testing.T) {
	h := newTestHandler(t)
	content := []byte("0123456789")
	sum := "84d89877f0d4041efb6bf91a16f0248f2fd573e6af05c19f96bedb9f882f7882" // sha256("0123456789")
	m := embed.BuiltinModel{ID: "fake-dl", Repo: "r/p", File: "m.gguf", Size: int64(len(content)), SHA256: sum, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, m)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()
	w := embPost(t, h, "/api/setup/embedding/download", `{"model_id":"fake-dl","mirror":"`+srv.URL+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := embGet(t, h)["download"].(map[string]any)
		if st["state"] == "done" {
			break
		}
		if st["state"] == "error" {
			t.Fatal(st["error"])
		}
		if time.Now().After(deadline) {
			t.Fatal("下载超时未完成")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !m.Installed(filepath.Join(os.Getenv("OK_HOME"), "models")) {
		t.Fatal("模型应已落盘")
	}
}

func TestOllamaModelsProxy(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Write([]byte(`{"models":[{"name":"bge-m3:latest"},{"name":"nomic-embed-text:latest"}]}`))
	}))
	defer srv.Close()
	req := httptest.NewRequest("GET", "/api/setup/embedding/ollama-models?base_url="+srv.URL, nil)
	req.Header.Set("X-Ok-Token", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out["models"].([]any)) != 2 {
		t.Fatalf("%v", out)
	}
}
```

（注意 `config` import 只为断言；`OK_HOME` 隔离遵循仓库 gui 测试惯例——若本仓库 api_test.go 已有 home 隔离 helper，复用它。）

- [ ] **Step 2: 确认失败**

Run: `go test ./internal/gui/ -run 'TestProfile|TestActivate|TestDownload|TestOllama'`
Expected: FAIL（路由 404）

- [ ] **Step 3: 实现 `internal/gui/embedding.go`**

```go
package gui

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
	"openknowledge/internal/registry"
	"openknowledge/internal/setupx"
)

// dlJob 是一个模型下载任务的状态（GUI 轮询展示）。
type dlJob struct {
	ModelID string `json:"model_id"`
	State   string `json:"state"` // downloading|done|error
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Err     string `json:"error"`
	cancel  context.CancelFunc
	mu      sync.Mutex
}

// dlSnapshot 返回当前下载任务快照（优先 downloading；无任务返回零值）。
// 逐字段拷贝避免复制 sync.Mutex（go vet copylocks）。
func (h *Handler) dlSnapshot() *dlJob {
	h.dlMu.Lock()
	defer h.dlMu.Unlock()
	var pick *dlJob
	for _, j := range h.dl {
		pick = j
		if j.State == "downloading" {
			break
		}
	}
	if pick == nil {
		return &dlJob{}
	}
	pick.mu.Lock()
	defer pick.mu.Unlock()
	return &dlJob{ModelID: pick.ModelID, State: pick.State, Done: pick.Done, Total: pick.Total, Err: pick.Err}
}

// apiEmbeddingGet：弹窗全量状态。
func (h *Handler) apiEmbeddingGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	modelsDir := filepath.Join(registry.Home(), "models")
	builtinModels := make([]map[string]any, 0, len(embed.BuiltinModels))
	for _, m := range embed.BuiltinModels {
		builtinModels = append(builtinModels, map[string]any{
			"id": m.ID, "label": m.Label, "size": m.Size, "dim": m.Dim,
			"downloaded": m.Installed(modelsDir),
		})
	}
	profiles := make([]map[string]any, 0, len(cfg.Embedding.Profiles))
	for _, p := range cfg.Embedding.Profiles {
		item := map[string]any{
			"name": p.Name, "type": p.Type, "base_url": p.BaseURL, "model": p.Model,
			"has_key": p.ResolvedAPIKey() != "", "mirror": p.Mirror,
		}
		if p.Type == "builtin" {
			if m := embed.FindBuiltinModel(p.Model); m != nil {
				item["downloaded"] = m.Installed(modelsDir)
				item["dim"] = m.Dim
			}
		}
		profiles = append(profiles, item)
	}
	_, rtErr := embedsidecar.RuntimeServerPath(embedsidecar.DefaultRuntimeDir())
	writeJSON(w, http.StatusOK, map[string]any{
		"active":             cfg.Embedding.Active,
		"runtime_available":  rtErr == nil,
		"builtin_models":     builtinModels,
		"download":           h.dlSnapshot(),
		"profiles":           profiles,
	})
}

// apiEmbeddingSave：新增/覆盖保存（不自动激活；api_key 空=保留旧值）。
func (h *Handler) apiEmbeddingSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
		APIKey  string `json:"api_key"`
		Mirror  string `json:"mirror"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "名称不能为空")
		return
	}
	switch req.Type {
	case "openai", "ollama":
		if req.BaseURL == "" || req.Model == "" {
			writeErr(w, http.StatusBadRequest, "base_url 与 model 不能为空")
			return
		}
	case "builtin":
		if embed.FindBuiltinModel(req.Model) == nil {
			writeErr(w, http.StatusBadRequest, "未知内置模型: "+req.Model)
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "type 必须是 openai|ollama|builtin")
		return
	}
	p := config.EmbeddingProfile{
		Name: req.Name, Type: req.Type, BaseURL: req.BaseURL,
		Model: req.Model, APIKey: req.APIKey, Mirror: req.Mirror,
	}
	if err := setupx.SaveEmbeddingProfile(p, false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiEmbeddingActive：设为使用中（builtin 要求模型已下载；成功写 want 预热）。
func (h *Handler) apiEmbeddingActive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != "" {
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		var target *config.EmbeddingProfile
		for i := range cfg.Embedding.Profiles {
			if cfg.Embedding.Profiles[i].Name == req.Name {
				target = &cfg.Embedding.Profiles[i]
			}
		}
		if target == nil {
			writeErr(w, http.StatusBadRequest, "profile 不存在: "+req.Name)
			return
		}
		if target.Type == "builtin" {
			m := embed.FindBuiltinModel(target.Model)
			if m == nil || !m.Installed(filepath.Join(registry.Home(), "models")) {
				writeErr(w, http.StatusBadRequest, "模型未下载，请先下载")
				return
			}
		}
	}
	if err := setupx.SetActiveEmbedding(req.Name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Name != "" {
		embedsidecar.RequestStart() // 非内置也无害：janitor 会忽略并清除
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiEmbeddingDelete：删除 profile（使用中项被删则退回未配置）。
func (h *Handler) apiEmbeddingDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := setupx.DeleteEmbeddingProfile(req.Name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiEmbeddingTest：对指定已保存 profile 做连通性/就绪检查。
func (h *Handler) apiEmbeddingTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var target *config.EmbeddingProfile
	for i := range cfg.Embedding.Profiles {
		if cfg.Embedding.Profiles[i].Name == req.Name {
			target = &cfg.Embedding.Profiles[i]
		}
	}
	if target == nil {
		writeErr(w, http.StatusBadRequest, "profile 不存在: "+req.Name)
		return
	}
	resp := map[string]any{"ok": true, "error": ""}
	if err := setupx.TestEmbeddingProfile(*target, 10*time.Second); err != nil {
		resp["ok"] = false
		resp["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// apiEmbeddingDownload：后台下载内置模型（单任务；重复调用幂等）。
func (h *Handler) apiEmbeddingDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelID string `json:"model_id"`
		Mirror  string `json:"mirror"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	m := embed.FindBuiltinModel(req.ModelID)
	if m == nil {
		writeErr(w, http.StatusBadRequest, "未知内置模型: "+req.ModelID)
		return
	}
	modelsDir := filepath.Join(registry.Home(), "models")
	if m.Installed(modelsDir) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "done"})
		return
	}
	h.dlMu.Lock()
	if _, running := h.dl[req.ModelID]; running {
		h.dlMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "downloading"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &dlJob{ModelID: req.ModelID, State: "downloading", Total: m.Size, cancel: cancel}
	h.dl[req.ModelID] = job
	h.dlMu.Unlock()
	go func() {
		err := embed.Download(ctx, nil, *m, req.Mirror, modelsDir, func(done, total int64) {
			job.mu.Lock()
			job.Done = done
			job.Total = total
			job.mu.Unlock()
		})
		h.dlMu.Lock()
		defer h.dlMu.Unlock()
		job.mu.Lock()
		defer job.mu.Unlock()
		switch {
		case err == nil:
			job.State = "done"
			job.Done = job.Total
		case ctx.Err() != nil:
			delete(h.dl, req.ModelID) // 取消：清空状态（.part 保留可续传）
		default:
			job.State = "error"
			job.Err = err.Error()
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "downloading"})
}

// apiEmbeddingDownloadCancel：取消下载（保留 .part 供续传）。
func (h *Handler) apiEmbeddingDownloadCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelID string `json:"model_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	h.dlMu.Lock()
	if job, ok := h.dl[req.ModelID]; ok && job.cancel != nil {
		job.cancel()
	}
	h.dlMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiOllamaModels：代理探测 Ollama 已安装模型（前端跨域受限，经后端转发）。
func (h *Handler) apiOllamaModels(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base_url")
	if base == "" {
		base = "http://localhost:11434"
	}
	models, err := setupx.ListOllamaModels(base)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}
```

- [ ] **Step 4: api.go 配套改动**

1. `Handler` struct 加字段 `dlMu sync.Mutex; dl map[string]*dlJob`；`NewHandler` 初始化 `dl: map[string]*dlJob{}`。
2. 路由表：删 `api("POST /api/setup/embedding", h.apiSetupEmbedding)`，新增：

```go
		api("GET /api/setup/embedding", h.apiEmbeddingGet)
		api("POST /api/setup/embedding/profile", h.apiEmbeddingSave)
		api("DELETE /api/setup/embedding/profile", h.apiEmbeddingDelete)
		api("POST /api/setup/embedding/active", h.apiEmbeddingActive)
		api("POST /api/setup/embedding/test", h.apiEmbeddingTest)
		api("POST /api/setup/embedding/download", h.apiEmbeddingDownload)
		api("POST /api/setup/embedding/download/cancel", h.apiEmbeddingDownloadCancel)
		api("GET /api/setup/embedding/ollama-models", h.apiOllamaModels)
```

3. 删除旧 `apiSetupEmbedding` 函数（:1073-1110 附近）。
4. apiStatus embedding 段（:327-338）替换为：

```go
	embeddingConfigured := false
	embedding := map[string]any{"configured": false}
	hooksTimeout := 10
	if cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml")); err == nil {
		if p := cfg.Embedding.ActiveProfile(); p != nil {
			embeddingConfigured = true
			embedding = map[string]any{
				"configured": true, "name": p.Name, "type": p.Type,
				"model": p.Model, "base_url": p.BaseURL,
			}
			if p.Type == "builtin" {
				if m := embed.FindBuiltinModel(p.Model); m != nil {
					embedding["model_label"] = m.Label
					embedding["dim"] = m.Dim
				}
			}
		}
		if cfg.Hooks.TimeoutSec > 0 {
			hooksTimeout = cfg.Hooks.TimeoutSec
		}
	}
```

5. 修正既有 `api_test.go` 中断言旧 `embedding.base_url/has_key` 形状的用例（改为读 `GET /api/setup/embedding` 的 profiles）。

- [ ] **Step 5: 全量测试 + commit**

Run: `go build ./... && go vet ./... && go test ./internal/gui/`
Expected: PASS

```bash
git add internal/gui
git commit -m "feat(gui): embedding 多服务管理 API——profiles CRUD/激活/测试/Ollama 探测代理/内置模型后台下载（单任务+进度+取消），apiStatus 输出使用中摘要"
```

---

### Task 11: GUI 前端（卡片摘要 + 配置弹窗）

按已确认视觉稿（`.superpowers/brainstorm/` 留档）：卡片单行摘要 + "配置…"按钮；弹窗左列表右表单，显式"设为使用中"。

**Files:**
- Modify: `web/index.html`（卡片改造 :131-143 + 弹窗 markup）
- Modify: `web/style.css`（弹窗样式，沿用既有 token）
- Modify: `web/app.js`（renderGuide embedding 段 + 弹窗模块）

**Interfaces:**
- Consumes: Task 10 全部端点形状；`/api/status` 的 `embedding:{configured,name,type,model,base_url,model_label,dim}`

- [ ] **Step 1: index.html 卡片改造**

`:131-143` 的 embedding 卡片整体替换为：

```html
        <div class="card card-wide">
          <div class="card-head"><h3>embedding 语义检索</h3><span id="badge-embedding" class="badge badge-off">未配置</span></div>
          <p id="emb-current" class="card-desc muted">未启用（仅关键词检索）</p>
          <div class="card-actions"><button id="btn-embedding-config" type="button" class="btn">配置…</button></div>
        </div>
```

changelog-modal（:260-268）之后插入弹窗：

```html
  <div id="emb-modal" class="modal hidden">
    <div class="modal-box emb-box">
      <div class="emb-title">embedding 服务配置
        <button id="emb-close" type="button" class="emb-x" aria-label="关闭">×</button>
      </div>
      <div class="emb-main">
        <div class="emb-side">
          <div id="emb-list"></div>
          <button id="emb-add" type="button" class="btn">＋ 添加</button>
        </div>
        <div class="emb-form">
          <label class="emb-row">名称 <input id="emb-f-name" type="text" autocomplete="off"></label>
          <label class="emb-row">类型
            <select id="emb-f-type">
              <option value="builtin">内置本地模型（ok 托管 · 无需联网）</option>
              <option value="ollama">Ollama（本机/局域网服务）</option>
              <option value="openai">自定义（OpenAI 兼容服务）</option>
            </select>
          </label>
          <div id="emb-fs-builtin">
            <label class="emb-row">模型 <select id="emb-f-bi-model"></select></label>
            <label class="emb-row">下载源
              <select id="emb-f-bi-mirror">
                <option value="hf-mirror">hf-mirror 镜像（国内推荐）</option>
                <option value="huggingface">huggingface 官方</option>
              </select>
            </label>
            <div id="emb-bi-status" class="emb-status muted">…</div>
          </div>
          <div id="emb-fs-ollama" class="hidden">
            <label class="emb-row">地址 <input id="emb-f-ol-url" type="text" placeholder="http://localhost:11434"></label>
            <label class="emb-row" id="emb-ol-sel-row">模型 <select id="emb-f-ol-model"></select></label>
            <label class="emb-row hidden" id="emb-ol-input-row">模型（手动输入） <input id="emb-f-ol-model-text" type="text" placeholder="bge-m3"></label>
            <p class="emb-note muted" id="emb-ol-hint"></p>
          </div>
          <div id="emb-fs-openai" class="hidden">
            <label class="emb-row">base_url <input id="emb-f-base-url" type="text" placeholder="https://api.openai.com/v1"></label>
            <label class="emb-row">model <input id="emb-f-model" type="text" placeholder="text-embedding-3-small"></label>
            <label class="emb-row">api_key <input id="emb-f-api-key" type="password" autocomplete="off"></label>
            <p class="emb-note muted">注意：DeepSeek 无 embedding 接口，会报 404。</p>
          </div>
          <p id="emb-form-msg" class="emb-note"></p>
          <div class="emb-actions">
            <button id="emb-save" type="button" class="btn btn-primary">保存</button>
            <button id="emb-activate" type="button" class="btn">设为使用中</button>
            <button id="emb-test" type="button" class="btn">测试</button>
            <span style="flex:1"></span>
            <button id="emb-delete" type="button" class="btn btn-danger">删除</button>
          </div>
        </div>
      </div>
    </div>
  </div>
```

- [ ] **Step 2: style.css 追加**（文件末尾；token 全部复用 :root 变量）

```css
/* ---------- embedding 配置弹窗 ---------- */

.emb-box { max-width: 780px; padding: 0; overflow: hidden; }
.emb-title {
  display: flex; justify-content: space-between; align-items: center;
  padding: 12px 20px; font-size: 15px; font-weight: 600;
}
.emb-x { background: none; border: none; font-size: 18px; color: var(--muted); cursor: pointer; }
.emb-main { display: flex; border-top: 1px solid var(--border); min-height: 420px; }
.emb-side {
  width: 186px; flex: none; border-right: 1px solid var(--border);
  padding: 10px; display: flex; flex-direction: column; gap: 6px; background: #fafbfc;
}
.emb-item {
  display: flex; gap: 8px; align-items: center; padding: 8px 10px;
  border-radius: 6px; border: 1px solid transparent; cursor: pointer;
}
.emb-item:hover { border-color: var(--border); }
.emb-item.active { background: #eff6ff; border-color: #bfdbfe; }
.emb-item-name { font-size: 13px; font-weight: 600; line-height: 1.3; }
.emb-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--ok); flex: none; }
.emb-dot.off { visibility: hidden; }
#emb-add { margin-top: auto; width: 100%; }
.emb-tag { background: #e0e7ff; color: #3730a3; border-radius: 4px; padding: 1px 6px; font-size: 12px; }
.emb-tag.local { background: #dcfce7; color: #166534; }
.emb-form { flex: 1; padding: 14px 18px; min-width: 0; }
.emb-row { display: block; font-size: 12px; color: var(--muted); margin-bottom: 10px; }
.emb-row input, .emb-row select { display: block; width: 100%; margin-top: 3px; }
.emb-note { font-size: 12px; margin: 6px 0; }
.emb-status { background: #f0f9ff; border: 1px solid #bae6fd; border-radius: 6px; padding: 8px 12px; font-size: 13px; }
.emb-prog { height: 8px; background: #e5e7eb; border-radius: 999px; overflow: hidden; margin: 6px 0 4px; }
.emb-prog i { display: block; height: 100%; background: var(--primary); border-radius: 999px; }
.emb-actions { display: flex; gap: 8px; margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--border); align-items: center; }
#emb-form-msg { color: var(--muted); }
#emb-form-msg.err { color: var(--danger); }
#emb-form-msg.ok { color: var(--ok); }
```

- [ ] **Step 3: app.js — renderGuide 段替换 + 弹窗模块**

a) `renderGuide` 中（:653 与 :656-660）替换为：

```js
    var emb = s.embedding || {};
    setBadge("badge-embedding", !!emb.configured, "已配置", "未配置");
    var ec = $("emb-current");
    if (emb.configured) {
      var tag = emb.type === "builtin" ? "内置" : emb.type === "ollama" ? "Ollama" : "自定义";
      var parts = ["[" + tag + "] " + emb.name];
      parts.push(emb.type === "builtin" ? (emb.model_label || emb.model) : (emb.model + " @ " + (emb.base_url || "")));
      if (emb.type === "builtin") parts.push("本地运行，无需联网");
      ec.textContent = parts.join(" · ");
    } else {
      ec.textContent = "未启用（仅关键词检索）";
    }
```

并删除对 `emb-base-url/emb-model/emb-api-key` 的引用（元素已删）。

b) 文件末尾追加弹窗模块（沿用 `$`/`api` helper 与既有代码风格）：

```js
  // ---------- embedding 配置弹窗 ----------

  var embState = { data: null, sel: -1, pollTimer: null };

  function embOpen() {
    $("emb-modal").classList.remove("hidden");
    embRefresh();
  }
  function embClose() {
    $("emb-modal").classList.add("hidden");
    if (embState.pollTimer) { clearTimeout(embState.pollTimer); embState.pollTimer = null; }
  }
  function embRefresh(keepSel) {
    api("/api/setup/embedding").then(function (d) {
      embState.data = d;
      if (!keepSel) embState.sel = d.active ? d.profiles.findIndex(function (p) { return p.name === d.active; }) : -1;
      if (embState.sel < 0 && d.profiles.length > 0) embState.sel = 0;
      embRenderList();
      embRenderForm();
      embSchedulePoll();
    });
  }
  function embRenderList() {
    var d = embState.data, box = $("emb-list");
    box.innerHTML = "";
    d.profiles.forEach(function (p, i) {
      var item = document.createElement("div");
      item.className = "emb-item" + (i === embState.sel ? " active" : "");
      var dot = document.createElement("span");
      dot.className = "emb-dot" + (p.name === d.active ? "" : " off");
      dot.title = "使用中";
      var right = document.createElement("div");
      var nm = document.createElement("div");
      nm.className = "emb-item-name";
      nm.textContent = p.name;
      var tag = document.createElement("span");
      tag.className = "emb-tag" + (p.type === "builtin" ? " local" : "");
      tag.textContent = p.type === "builtin" ? "内置" : p.type === "ollama" ? "Ollama" : "自定义";
      right.appendChild(nm); right.appendChild(tag);
      item.appendChild(dot); item.appendChild(right);
      item.onclick = function () { embState.sel = i; embRenderList(); embRenderForm(); };
      box.appendChild(item);
    });
  }
  function embCur() {
    var d = embState.data;
    return embState.sel >= 0 && d && d.profiles[embState.sel] ? d.profiles[embState.sel] : null;
  }
  function embRenderForm() {
    var p = embCur(), d = embState.data;
    $("emb-f-name").value = p ? p.name : "";
    $("emb-f-type").value = p ? p.type : "builtin";
    // 内置模型下拉
    var biSel = $("emb-f-bi-model");
    biSel.innerHTML = "";
    (d.builtin_models || []).forEach(function (m) {
      var o = document.createElement("option");
      o.value = m.id; o.textContent = m.label + (m.downloaded ? "（已下载）" : "");
      biSel.appendChild(o);
    });
    if (p && p.type === "builtin") biSel.value = p.model;
    $("emb-f-bi-mirror").value = (p && p.mirror) || "hf-mirror";
    $("emb-f-ol-url").value = p && p.type === "ollama" ? p.base_url : "http://localhost:11434";
    $("emb-f-base-url").value = p && p.type === "openai" ? p.base_url : "";
    $("emb-f-model").value = p && p.type === "openai" ? p.model : "";
    $("emb-f-api-key").value = "";
    $("emb-f-api-key").placeholder = p && p.has_key ? "已保存（留空保持不变）" : "api_key";
    embTypeSwitch();
    embRenderBiStatus();
  }
  function embTypeSwitch() {
    var t = $("emb-f-type").value;
    $("emb-fs-builtin").classList.toggle("hidden", t !== "builtin");
    $("emb-fs-ollama").classList.toggle("hidden", t !== "ollama");
    $("emb-fs-openai").classList.toggle("hidden", t !== "openai");
    if (t === "ollama") embLoadOllamaModels();
  }
  function embLoadOllamaModels() {
    var base = $("emb-f-ol-url").value.trim() || "http://localhost:11434";
    var p = embCur();
    api("/api/setup/embedding/ollama-models?base_url=" + encodeURIComponent(base)).then(function (r) {
      var sel = $("emb-f-ol-model");
      sel.innerHTML = "";
      var ok = r.models && r.models.length > 0;
      $("emb-ol-sel-row").classList.toggle("hidden", !ok);
      $("emb-ol-input-row").classList.toggle("hidden", ok);
      if (ok) {
        r.models.forEach(function (m) {
          var o = document.createElement("option"); o.value = m; o.textContent = m; sel.appendChild(o);
        });
        if (p && p.type === "ollama") sel.value = p.model;
        $("emb-ol-hint").textContent = "已探测到 " + r.models.length + " 个已安装模型";
      } else {
        if (p && p.type === "ollama") $("emb-f-ol-model-text").value = p.model;
        $("emb-ol-hint").textContent = "未能探测模型列表（" + (r.error || "无模型") + "），手动输入；未安装请先 ollama pull bge-m3";
      }
    });
  }
  function embRenderBiStatus() {
    var d = embState.data, box = $("emb-bi-status");
    var id = $("emb-f-bi-model").value;
    var m = (d.builtin_models || []).filter(function (x) { return x.id === id; })[0];
    if (!m) { box.textContent = "…"; return; }
    var dl = d.download || {};
    if (dl.state === "downloading" && dl.model_id === id) {
      var pct = dl.total ? Math.floor(dl.done * 100 / dl.total) : 0;
      box.innerHTML = "";
      box.appendChild(document.createTextNode("正在下载 — " + fmtMB(dl.done) + " / " + fmtMB(dl.total)));
      var bar = document.createElement("div"); bar.className = "emb-prog";
      var fill = document.createElement("i"); fill.style.width = pct + "%";
      bar.appendChild(fill); box.appendChild(bar);
      var tip = document.createElement("span"); tip.textContent = pct + "%";
      box.appendChild(tip);
      var cancel = document.createElement("button");
      cancel.className = "btn"; cancel.textContent = "取消"; cancel.style.marginLeft = "8px";
      cancel.onclick = function () {
        api("/api/setup/embedding/download/cancel", { method: "POST", body: { model_id: id } })
          .then(function () { embRefresh(true); });
      };
      box.appendChild(cancel);
      return;
    }
    if (m.downloaded) {
      box.textContent = "✓ 模型已就绪（" + m.dim + " 维），sidecar 按需拉起、空闲自动退出";
      return;
    }
    box.innerHTML = "";
    if (!d.runtime_available) {
      box.textContent = "⚠ 推理运行时缺失——内置模式仅安装版可用（裸 exe 形态请用 Ollama/自定义）";
      return;
    }
    var btn = document.createElement("button");
    btn.className = "btn"; btn.textContent = "下载模型（" + fmtMB(m.size) + "）";
    btn.onclick = function () {
      api("/api/setup/embedding/download", { method: "POST", body: { model_id: id, mirror: $("emb-f-bi-mirror").value } })
        .then(function () { embRefresh(true); });
    };
    box.appendChild(btn);
    if (dl.state === "error" && dl.model_id === id) {
      var err = document.createElement("span");
      err.style.color = "var(--danger)";
      err.textContent = "  上次下载失败：" + dl.error;
      box.appendChild(err);
    }
  }
  function fmtMB(n) { return (n / 1048576).toFixed(0) + " MB"; }
  function embSchedulePoll() {
    if (embState.pollTimer) clearTimeout(embState.pollTimer);
    var dl = embState.data && embState.data.download;
    if (dl && dl.state === "downloading") {
      embState.pollTimer = setTimeout(function () { embRefresh(true); }, 1000);
    }
  }
  function embMsg(text, cls) {
    var el = $("emb-form-msg");
    el.textContent = text || "";
    el.className = "emb-note" + (cls ? " " + cls : "");
  }
  function embCollect() {
    var t = $("emb-f-type").value;
    var body = { name: $("emb-f-name").value.trim(), type: t };
    if (t === "openai") {
      body.base_url = $("emb-f-base-url").value.trim();
      body.model = $("emb-f-model").value.trim();
      body.api_key = $("emb-f-api-key").value;
    } else if (t === "ollama") {
      body.base_url = $("emb-f-ol-url").value.trim() || "http://localhost:11434";
      var selOk = !$("emb-ol-sel-row").classList.contains("hidden");
      body.model = selOk ? $("emb-f-ol-model").value : $("emb-f-ol-model-text").value.trim();
    } else {
      body.model = $("emb-f-bi-model").value;
      body.mirror = $("emb-f-bi-mirror").value;
    }
    return body;
  }

  $("btn-embedding-config").onclick = embOpen;
  $("emb-close").onclick = embClose;
  $("emb-f-type").onchange = embTypeSwitch;
  $("emb-f-bi-model").onchange = embRenderBiStatus;
  $("emb-f-ol-url").onchange = embLoadOllamaModels;
  $("emb-add").onclick = function () { embState.sel = -1; embRenderList(); embRenderForm(); $("emb-f-name").focus(); };
  $("emb-save").onclick = function () {
    var body = embCollect();
    if (!body.name) { embMsg("名称不能为空", "err"); return; }
    api("/api/setup/embedding/profile", { method: "POST", body: body }).then(function () {
      embMsg("已保存", "ok");
      embRefresh(true);
      refreshStatus();
    }).catch(function (e) { embMsg(e.message || String(e), "err"); });
  };
  $("emb-activate").onclick = function () {
    var body = embCollect();
    if (!body.name) { embMsg("名称不能为空", "err"); return; }
    api("/api/setup/embedding/profile", { method: "POST", body: body }).then(function () {
      return api("/api/setup/embedding/active", { method: "POST", body: { name: body.name } });
    }).then(function () {
      embMsg("已设为使用中", "ok");
      embRefresh(true);
      refreshStatus();
    }).catch(function (e) { embMsg(e.message || String(e), "err"); });
  };
  $("emb-test").onclick = function () {
    var p = embCur();
    if (!p) { embMsg("先保存再测试", "err"); return; }
    embMsg("测试中…");
    api("/api/setup/embedding/test", { method: "POST", body: { name: p.name } }).then(function (r) {
      embMsg(r.ok ? "✓ 可用" : "✗ " + (r.error || "失败"), r.ok ? "ok" : "err");
    }).catch(function (e) { embMsg(e.message || String(e), "err"); });
  };
  $("emb-delete").onclick = function () {
    var p = embCur();
    if (!p) return;
    if (!confirm("删除配置「" + p.name + "」？" + (embState.data.active === p.name ? "它是使用中项，删除后退回关键词检索。" : ""))) return;
    api("/api/setup/embedding/profile", { method: "DELETE", body: { name: p.name } }).then(function () {
      embState.sel = -1;
      embRefresh();
      refreshStatus();
    }).catch(function (e) { embMsg(e.message || String(e), "err"); });
  };
```

（`api(path, opts)` 是 fetch 风格 helper：method 经 opts.method、对象 body 自动 JSON 化；错误以 `Error(message=data.error)` reject。`refreshStatus()` 是既有全局状态刷新函数。）

- [ ] **Step 4: 手动验证**

```bash
cd D:/develop/OpenKnowledge && python scripts/build.py --skip-installer --skip-winres
./dist/ok.exe gui
```

引导页 → embedding 卡片"配置…"：
1. 添加 openai profile（指向任意 OpenAI 兼容端点）→ 保存 → 设为使用中 → 卡片摘要刷新
2. 添加 ollama profile（无 Ollama 时提示探测失败但可手动输入保存）
3. 添加内置 profile → 状态区显示下载按钮（不实际下载，留 Task 13 E2E）
4. 删除使用中 profile → 卡片回到"未启用"

- [ ] **Step 5: commit**

```bash
git add web/
git commit -m "feat(gui): embedding 卡片显示使用中服务 + 配置弹窗——左列表右表单三形态、内置模型下载进度、显式设为使用中"
```

---

### Task 12: 打包（llama.cpp runtime 进 win/linux 包）

**Files:**
- Modify: `scripts/build.py`（runtime 准备步骤）
- Modify: `scripts/build-linux.sh`（runtime 下载 + 打包权限处理）
- Modify: `installer/openknowledge.iss`（Files 段加 runtime）
- Modify: `installer/nfpm.yaml`（contents 加 runtime）

**Interfaces:**
- Consumes: Task 5 `DefaultRuntimeDir()`（`<exe目录>/runtime`）与 `registry.Home()/models`
- 钉死：llama.cpp `b10405`；资产 `llama-b10405-bin-win-cpu-x64.zip`、`llama-b10405-bin-ubuntu-x64.tar.gz`；`LLAMA_CPP_BASE_URL` 环境变量可覆盖下载 base（国内代理/镜像）

- [ ] **Step 1: build.py 加 runtime 步骤**

文件头部 LDFLAGS 后加常量：

```python
LLAMA_TAG = "b10405"
LLAMA_BASE_DEFAULT = "https://github.com/ggml-org/llama.cpp/releases/download"
```

`main()` 第 2 步（拷贝 changelogs 之后）插入调用 `prepare_runtime()`，函数定义：

```python
def prepare_runtime():
    """下载 llama.cpp 预编译 llama-server（win cpu x64）到 dist/runtime/。

    内置 embedding sidecar 的运行时；版本钉死 LLAMA_TAG。
    LLAMA_CPP_BASE_URL 可覆盖下载源（国内代理/镜像）。
    """
    import zipfile
    dest = ROOT / "dist" / "runtime"
    if (dest / "llama-server.exe").exists():
        print("runtime 已存在，跳过下载（删除 dist/runtime 可强制刷新）")
        return
    base = os.environ.get("LLAMA_CPP_BASE_URL", LLAMA_BASE_DEFAULT)
    url = f"{base}/{LLAMA_TAG}/llama-{LLAMA_TAG}-bin-win-cpu-x64.zip"
    zip_path = ROOT / "dist" / "llama-win.zip"
    run(["curl", "-fSL", "-o", str(zip_path), url])
    dest.mkdir(exist_ok=True)
    with zipfile.ZipFile(zip_path) as z:
        z.extractall(dest)
    zip_path.unlink()
    if not (dest / "llama-server.exe").exists():
        sys.exit("runtime 解包后缺 llama-server.exe（llama.cpp 资产布局变化？）")
    print(f"runtime 就绪: {dest}（llama.cpp {LLAMA_TAG}）")
```

（`--skip-runtime` flag 可选：不需要时不加，保持脚本简单。）

- [ ] **Step 2: iss 加 runtime**

`installer/openknowledge.iss` Files 段（:38 后）加：

```iss
Source: "..\dist\runtime\*"; DestDir: "{app}\runtime"; Flags: ignoreversion recursesubdirs
```

- [ ] **Step 3: build-linux.sh 加 runtime**

`cp -r docs/changelogs "$STAGE/changelogs"` 之后插入：

```bash
# llama.cpp 运行时（llama-server，ubuntu x64 CPU 版）——内置 embedding sidecar
LLAMA_TAG=b10405
LLAMA_BASE=${LLAMA_CPP_BASE_URL:-https://github.com/ggml-org/llama.cpp/releases/download}
mkdir -p "$STAGE/runtime"
if [ ! -f "$STAGE/runtime/llama-server" ]; then
  curl -fSL -o dist/llama-linux.tar.gz "$LLAMA_BASE/$LLAMA_TAG/llama-$LLAMA_TAG-bin-ubuntu-x64.tar.gz"
  tar -xzf dist/llama-linux.tar.gz -C "$STAGE/runtime"
  rm -f dist/llama-linux.tar.gz
  chmod 0755 "$STAGE/runtime/llama-server" 2>/dev/null || true
fi
```

`cp -r "$STAGE/ok" "$STAGE/web" "$STAGE/changelogs" "dist/$PKG/"` 改为：

```bash
cp -r "$STAGE/ok" "$STAGE/web" "$STAGE/changelogs" "$STAGE/runtime" "dist/$PKG/"
```

tar 权限双保险段改为（llama-server 与 ok 同待遇）：

```bash
TAR="installer/output/$PKG.tar"
tar -cf "$TAR" -C dist --exclude="$PKG/ok" --exclude="$PKG/runtime/llama-server" "$PKG"
tar -rf "$TAR" -C dist --mode=0755 "$PKG/ok" "$PKG/runtime/llama-server"
gzip -f "$TAR"
```

- [ ] **Step 4: nfpm.yaml 加 runtime**

contents 列表追加：

```yaml
  - src: dist/linux-amd64/runtime/
    dst: /usr/lib/openknowledge/runtime/
  - src: dist/linux-amd64/runtime/llama-server
    dst: /usr/lib/openknowledge/runtime/llama-server
    file_info:
      mode: 0755
```

- [ ] **Step 5: 验证 + commit**

```bash
cd D:/develop/OpenKnowledge
python scripts/build.py --skip-installer --skip-winres   # dist/runtime/llama-server.exe 存在
bash -n scripts/build-linux.sh                            # 语法检查
LLAMA_CPP_BASE_URL=<同上> bash scripts/build-linux.sh     # 可选：完整 linux 包
```

Expected: `dist/runtime/llama-server.exe` + 若干 `ggml*.dll` 存在；iss 语法可由完整 `python scripts/build.py` 产出安装包验证（体积约 +30–40MB）。

```bash
git add scripts/build.py scripts/build-linux.sh installer/openknowledge.iss installer/nfpm.yaml
git commit -m "feat(build): llama.cpp b10405 runtime 随包分发（win cpu x64 + ubuntu x64）——内置 embedding sidecar 运行时，LLAMA_CPP_BASE_URL 可换源"
```

---

### Task 13: 版本 2.14.0 + 更新日志 + 文档同步 + E2E 冒烟

**Files:**
- Modify: `installer/openknowledge.iss`（`#define AppVersion "2.13.0"` → `"2.14.0"`）
- Create: `docs/changelogs/2.14.0.md`
- Modify: `docs/ARCHITECTURE.md`（第 17 章 embedding/检索 + 17.7 降级矩阵 + 打包段）
- Modify: `README.md`（功能清单）
- Modify: `web/help.md`（embedding 配置段落）

- [ ] **Step 1: 版本号**

iss 第 5 行 `#define AppVersion "2.13.0"` → `"2.14.0"`。

- [ ] **Step 2: `docs/changelogs/2.14.0.md`**

```markdown
# v2.14.0 embedding 多服务与本地模型：Ollama/内置 llama.cpp、GUI 配置弹窗、模型身份管理

- **embedding 多 profile（`[[embedding.profiles]]` + active）**：可保存多套服务
  配置并切换"使用中"；旧平铺配置自动迁移为"默认"openai profile，无需手动干预。
- **三种服务形态**：
  - 自定义（OpenAI 兼容，即原线上方式，key 可留空适配无鉴权本地服务）；
  - Ollama（本机/局域网，免 key，模型列表自动探测 `/api/tags`）；
  - 内置（ok 托管 llama.cpp `llama-server` sidecar，**完全离线、知识不出本机**）。
- **内置模型清单（4 档，钉死 repo/size/sha256）**：默认 Qwen3-Embedding-0.6B
  Q8_0（639MB，中文+代码强）；bge-m3 Q4_K_M/Q8_0；nomic-embed-text v1.5。
  默认 hf-mirror 国内镜像，断点续传 + sha256 校验；查询/文档双路径前缀
  （qwen3 查询侧 Instruct 前缀、nomic search_query/search_document）。
- **sidecar 生命周期（daemon 托管）**：active=内置且模型就绪时拉起；hook/cli
  只读状态文件、未就绪写 want 标记并立即降级纯 BM25（绝不等待冷启动）；
  空闲 10 分钟回收；崩溃有界重启 ×3；daemon 退出回收。runtime 随安装包分发
  （llama.cpp b10405 CPU 版，win+linux）。
- **模型身份管理（kb.db meta）**：索引记录建向量时的模型身份与维度；身份不符
  时语义通道显式跳过并提示 `ok index`（替代以往维度不等静默归零）；Sync 遇
  身份不符阻断向量写，杜绝混合模型向量；`ok index` 检测切换自动清向量全量
  重建（批量 32/批）。
- **GUI**：引导页 embedding 卡片显示使用中服务单行摘要；"配置…"弹窗左列表
  右表单（内置/ollama/自定义三形态表单 + 下载进度 + 显式"设为使用中"）。
- **CLI**：`ok setup` embedding 步骤三选一（线上/Ollama/内置，含模型选择与
  下载进度）；`--embedding-*` flags 向后兼容；`ok doctor` 细分内置形态提示。
```

- [ ] **Step 3: ARCHITECTURE.md 更新**

先读 `docs/ARCHITECTURE.md` 第 17 章（检索）与 17.7（降级矩阵）及打包章节，按以下要点改写（保持原文结构与编号风格）：

1. embedding 一节：单一 OpenAIClient → 三形态 profiles + embedx 唯一构造点；`embed.Client` 接口（查询/文档双路径 + 批量）；内置形态 sidecar 架构（daemon 托管、状态文件、want 拉起、空闲回收、有界重启）；模型清单钉死与镜像源。
2. kb.db 表清单加 `meta(key,value)`；说明 embedding_model/embedding_dim 身份语义与"身份不符阻断向量写"。
3. 17.7 降级矩阵按 spec §11 新表替换（含"模型身份不符 → 跳过+提示""删除使用中 profile → active 置空"两行）。
4. 打包一节：runtime 目录（llama-server）随 win 安装包与 linux tar/deb 分发；安装包体积约 50MB 级；模型不随包、首次启用下载。
5. 文末/相关处把"本地 embedding 模型（非目标）"之类过时表述删除或改写（v1 原始设计的非目标已被本版本实现）。

- [ ] **Step 4: README.md**

功能/亮点清单中加一条（中文，与现有条目同风格）：

```markdown
- **语义检索三种形态**：线上 OpenAI 兼容 / 本机 Ollama / 内置 llama.cpp 本地模型（离线可用，知识不出本机），GUI 弹窗统一管理
```

- [ ] **Step 5: web/help.md**

找到 embedding 配置段落，重写为三形态指引：openai（base_url/model/key）；Ollama（安装→`ollama pull bge-m3`→地址可局域网）；内置（选模型→选镜像→下载→设为使用中；仅安装版；约 400–640MB；首次拉起数秒到一分钟，期间自动降级关键词检索；切换模型后 `ok index` 重建）。

- [ ] **Step 6: E2E 冒烟（人工，按 spec §12）**

```bash
python scripts/build.py   # 出安装包
```
1. 安装 → GUI 配置弹窗选内置 qwen3-emb-0.6b-q8（hf-mirror）→ 下载 → 设为使用中
2. `ok add` 加条目 → `ok search` 语义命中（分数含余弦分量）
3. 断网（或停用网卡）→ `ok search` 仍语义命中（离线验证）
4. 切换为 Ollama/线上 profile → `ok search` 提示身份切换 → `ok index` 重建 → 恢复
5. `ok doctor` 全绿；删除使用中 profile → 退回纯关键词
6. （可选）Linux：`scripts/build-linux.sh` 产物装 deb 重复 1–5

- [ ] **Step 7: 全量测试 + commit**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全 PASS

```bash
git add installer/openknowledge.iss docs/changelogs/2.14.0.md docs/ARCHITECTURE.md README.md web/help.md
git commit -m "docs: v2.14.0 embedding 多服务与本地模型——更新日志/架构文档§17+17.7+打包/README/help"
```

---

## 自审记录（计划作者）

- **spec 覆盖**：§3 架构决策→Task 1/4/5/6；§4 配置模型+迁移+按名合并→Task 4；§5 清单+下载→Task 2/3；§6 sidecar 生命周期→Task 5/6；§7 链路（双路径/批量/身份）→Task 1/8/9；§8 GUI→Task 10/11；§9 CLI→Task 7；§10 打包→Task 12；§11 降级→分散各任务 + Task 13 文档矩阵；§12 测试→每任务 TDD + Task 13 E2E；§13 文档→Task 13。
- **非目标守门**：GPU 变体/清单外 GGUF/代装 Ollama/ANN/mac-arm 均未引入任务。
- **类型一致性**：`embed.Client` 四方法签名贯穿 Task 1/5/7/8；`EmbeddingProfile` 字段名贯穿 Task 4/7/10/11；`Manager.Reconcile(desired *embed.BuiltinModel, now time.Time)` 在 Task 5 定义、Task 6 调用一致；前端 `api(path, opts)` 为 fetch 风格（method/body 经 opts），Task 11 全部调用点已按此签名。



