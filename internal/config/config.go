package config

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

type Embedding struct {
	BaseURL    string `toml:"base_url"`
	APIKeyEnv  string `toml:"api_key_env"`
	Model      string `toml:"model"`
	TimeoutSec int    `toml:"timeout_sec"`
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

type Config struct {
	Embedding Embedding     `toml:"embedding"`
	Inject    Inject        `toml:"inject"`
	Retrieve  Retrieve      `toml:"retrieve"`
	Enforce   []EnforceRule `toml:"enforce"`
}

func Default() Config {
	return Config{
		Embedding: Embedding{TimeoutSec: 5},
		Inject:    Inject{MaxTokens: 1500},
		Retrieve:  Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 3},
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
