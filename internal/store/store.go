package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"openknowledge/internal/entry"
)

type Store struct{ Root string }

func New(root string) *Store { return &Store{Root: root} }

func (s *Store) KnowledgeDir() string { return filepath.Join(s.Root, "knowledge") }
func (s *Store) IndexPath() string    { return filepath.Join(s.Root, "INDEX.md") }
func (s *Store) VectorsPath() string  { return filepath.Join(s.Root, "vectors.json") }
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

// IndexContent 生成轻量索引文本（标题+类型+tags+摘要）。
func IndexContent(entries []*entry.Entry) string {
	var b strings.Builder
	b.WriteString("# 知识索引\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", e.Title, e.Type, strings.Join(e.Tags, ", "), e.Summary)
	}
	return b.String()
}

func (s *Store) RebuildIndex(entries []*entry.Entry) error {
	return os.WriteFile(s.IndexPath(), []byte(IndexContent(entries)), 0o644)
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
