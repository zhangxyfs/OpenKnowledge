package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Session struct {
	SessionID    string   `json:"session_id"`
	Touched      []string `json:"touched"`
	BlockedRules []string `json:"blocked_rules"`
}

func fileName(sessionID string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, sessionID)
	if clean == "" {
		clean = "unknown"
	}
	return "session-" + clean + ".json"
}

// Load 读取会话状态；不存在或损坏时返回空状态。
func Load(dir, sessionID string) *Session {
	s := &Session{SessionID: sessionID}
	data, err := os.ReadFile(filepath.Join(dir, fileName(sessionID)))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	s.SessionID = sessionID
	return s
}

func (s *Session) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fileName(s.SessionID)), data, 0o644)
}

func (s *Session) AddTouched(p string) {
	for _, t := range s.Touched {
		if t == p {
			return
		}
	}
	s.Touched = append(s.Touched, p)
}

func (s *Session) HasBlocked(ruleType string) bool {
	for _, b := range s.BlockedRules {
		if b == ruleType {
			return true
		}
	}
	return false
}

func (s *Session) MarkBlocked(ruleType string) {
	if !s.HasBlocked(ruleType) {
		s.BlockedRules = append(s.BlockedRules, ruleType)
	}
}

// Clean 删除 dir 中 mtime 早于 maxAge 的会话状态文件。
func Clean(dir string, maxAge time.Duration) error {
	matches, err := filepath.Glob(filepath.Join(dir, "session-*.json"))
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
	return nil
}
