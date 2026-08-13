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
	ID          string // 清单 id，如 "qwen3-emb-0.6b-q8"
	Label       string // GUI/CLI 展示名（含体积/维度/特点）
	Repo        string // HF repo，如 "Qwen/Qwen3-Embedding-0.6B-GGUF"
	File        string // repo 内文件名
	Size        int64
	SHA256      string // 小写 hex
	Dim         int
	Pooling     string // llama-server --pooling 取值：cls|last|mean
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
