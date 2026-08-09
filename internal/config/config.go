package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Embedding struct {
	BaseURL    string `toml:"base_url"`
	APIKey     string `toml:"api_key"`
	APIKeyEnv  string `toml:"api_key_env"`
	Model      string `toml:"model"`
	TimeoutSec int    `toml:"timeout_sec"`
}

// ResolvedAPIKey 返回生效的 embedding API key：api_key 字段优先，其次 api_key_env 环境变量。
func (e Embedding) ResolvedAPIKey() string {
	if e.APIKey != "" {
		return e.APIKey
	}
	if e.APIKeyEnv != "" {
		return os.Getenv(e.APIKeyEnv)
	}
	return ""
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
	return cfg, nil
}

// LoadMerged 合并配置：内置默认 ← globalPath ← projectPath，后者覆盖前者。
// 两个文件都可以不存在（视为空）。
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
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("解析 %s: %w", path, err)
		}
	}
	return cfg, nil
}
