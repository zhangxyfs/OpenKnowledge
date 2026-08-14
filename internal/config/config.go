package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

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
	// ModelsDir 内置模型下载目录；空=默认 <ok.exe 所在目录>/models（安装版即安装目录下）
	ModelsDir string `toml:"models_dir,omitempty"`
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

type Inject struct {
	MaxTokens int `toml:"max_tokens"`
}

type Retrieve struct {
	Alpha float64 `toml:"alpha"`
	Beta  float64 `toml:"beta"`
	TopN  int     `toml:"top_n"`
}

type EnforceRule struct {
	Type          string   `toml:"type"`
	CodeGlobs     []string `toml:"code_globs"`
	ChangelogGlob string   `toml:"changelog_glob"`
	Message       string   `toml:"message"`
}

// Capture 是知识捕获模式配置：propose（AI 提议草稿，人批准）或
// auto（自动捕获）；TurnInterval 为自动捕获的轮次间隔。
type Capture struct {
	Mode         string `toml:"mode"`
	TurnInterval int    `toml:"turn_interval"`
}

// Wiki 控制 wiki 落后提示。
type Wiki struct {
	StaleCommits int `toml:"stale_commits"` // 落后多少 commit 提示；0 = 关闭
}

// Hooks 控制写入 agent 配置的 hook 超时（秒）。2026-08-04 曾出现 Windows 高负载下
// 5s 超时导致 PostToolUse 整个会话静默丢失，故默认 10 且可在 GUI 引导页调整。
type Hooks struct {
	TimeoutSec int `toml:"timeout_sec"`
}

// Reasonix 控制 reasonix sidecar 的强制检查表达方式。
type Reasonix struct {
	EnforceMode string `toml:"enforce_mode"` // soft|hard|mixed；缺省/非法按 mixed
}

// Provenance 控制分支溯源（born 标签的自动记录）。
type Provenance struct {
	AutoBorn bool `toml:"auto_born"` // 默认 true（见 Default）
}

type Config struct {
	Embedding  Embedding     `toml:"embedding"`
	Inject     Inject        `toml:"inject"`
	Retrieve   Retrieve      `toml:"retrieve"`
	Enforce    []EnforceRule `toml:"enforce"`
	Capture    Capture       `toml:"capture"`
	Wiki       Wiki          `toml:"wiki"`
	Hooks      Hooks         `toml:"hooks"`
	Reasonix   Reasonix      `toml:"reasonix"`
	Provenance Provenance    `toml:"provenance"`
}

func Default() Config {
	return Config{
		Embedding:  Embedding{TimeoutSec: 5},
		Inject:     Inject{MaxTokens: 800},
		Retrieve:   Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 2},
		Capture:    Capture{Mode: "propose", TurnInterval: 5},
		Wiki:       Wiki{StaleCommits: 20},
		Hooks:      Hooks{TimeoutSec: 10},
		Provenance: Provenance{AutoBorn: true},
	}
}

// Load 读取配置；文件不存在返回 Default，缺省字段用默认值填充。
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	cfg.Embedding.migrateLegacy()
	return cfg, nil
}

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
