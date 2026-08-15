package store

import (
	"os"
	"path/filepath"
	"unicode"
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

// isCJK 判定 token 密度接近 1 token/字的字符（汉字/假名/谚文等）。
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// EstimateTokens 保守估算 token 数：CJK 按约 1 token/字计，其余（拉丁/数字/符号/
// 空白）按约 4 字符/token 计。旧实现统一按 2 字符/token，对以中文为主的知识条目
// 低估约 2 倍——默认预算 800 实际可塞进 1000+ 真实 token，与"保守"语义相反。
func EstimateTokens(s string) int {
	cjk, rest := 0, 0
	for _, r := range s {
		if isCJK(r) {
			cjk++
		} else {
			rest++
		}
	}
	return cjk + (rest+3)/4
}

const truncateMarker = "\n…(已截断)"

// TruncateToBudget 将文本按密度截到 token 预算内（按 rune 安全截断）：CJK 每字
// 扣 1 预算，其余每 4 字符扣 1。截断标记自身的成本预先扣除，保证结果含标记不
// 超预算。maxTokens 为负（配置笔误）钳为 0，避免负数下标切片 panic。
func TruncateToBudget(s string, maxTokens int) string {
	if maxTokens < 0 {
		maxTokens = 0
	}
	if EstimateTokens(s) <= maxTokens {
		return s
	}
	budget := maxTokens - EstimateTokens(truncateMarker)
	if budget < 0 {
		budget = 0
	}
	runes := []rune(s)
	cjk, rest := 0, 0
	for i, r := range runes {
		if isCJK(r) {
			cjk++
		} else {
			rest++
		}
		if cjk+rest/4 >= budget {
			return string(runes[:i]) + truncateMarker
		}
	}
	return string(runes) + truncateMarker
}
