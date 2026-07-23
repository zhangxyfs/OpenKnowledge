package store

import (
	"os"
	"path/filepath"
	"unicode/utf8"
)

type Store struct{ Root string }

func New(root string) *Store { return &Store{Root: root} }

func (s *Store) KnowledgeDir() string { return filepath.Join(s.Root, "knowledge") }
func (s *Store) IndexPath() string    { return filepath.Join(s.Root, "INDEX.md") }
func (s *Store) KbPath() string       { return filepath.Join(s.Root, "kb.db") }
func (s *Store) StateDir() string     { return filepath.Join(s.Root, "state") }
func (s *Store) ConfigPath() string   { return filepath.Join(s.Root, "config.toml") }

func (s *Store) EnsureDirs() error {
	for _, d := range []string{s.KnowledgeDir(), s.StateDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// EstimateTokens 按字符数 ÷ 2 保守估算 token 数。
func EstimateTokens(s string) int { return utf8.RuneCountInString(s) / 2 }

// TruncateToBudget 将文本截断到 token 预算内（按 rune 安全截断）。
func TruncateToBudget(s string, maxTokens int) string {
	maxRunes := maxTokens * 2
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "\n…(已截断)"
}
