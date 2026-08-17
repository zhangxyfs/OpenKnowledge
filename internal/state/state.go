package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/fsx"
)

type Session struct {
	SessionID           string   `json:"session_id"`
	Touched             []string `json:"touched"`
	BlockedRules        []string `json:"blocked_rules"`
	BaseInjected        bool     `json:"base_injected"`
	// InjectCount 本会话 prompt 注入轮次计数（reinject_turns 周期性重注入用）。
	InjectCount         int      `json:"inject_count"`
	StopCount           int      `json:"stop_count"`
	LastExtractReminder int      `json:"last_extract_reminder"`
	WikiNudged          bool     `json:"wiki_nudged"`
	// RetrieveWarned 标记"语义检索退化"提示本会话已给出（每会话最多一条）。
	RetrieveWarned      bool     `json:"retrieve_warned"`
	// MergedChecked 标记 merged 检测本会话已算过（无论结果）：与 WikiNudged 独立——
	// merged 为空时 WikiNudged 不置位，若无此字段基准分支上每次 prompt 都会为每条
	// 非基准游标重付 rev-parse + merge-base 两次 git spawn。
	MergedChecked bool `json:"merged_checked"`
	// InjectedKnowledge 本会话最近一轮注入的检索条目（原始大小写 basename，
	// 供采纳归因）；AdoptedKnowledge 待入账的采纳挂账（post-tool 不开库，
	// 下次 InjectForPrompt 开头入账 entry_events 后清空；会话结束挂账丢失，
	// 统计性信号可接受）。
	InjectedKnowledge []string `json:"injected_knowledge"`
	AdoptedKnowledge  []string `json:"adopted_knowledge"`
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
	if err := json.Unmarshal(data, s); err != nil {
		// 损坏按空状态处理：半解析结果若被当作合法状态回写，损坏会被"洗白"成
		// 错误状态（如 BaseInjected=false 触发 mandatory 重注入）且无从定位；
		// 空状态一次覆盖即自愈
		return &Session{SessionID: sessionID}
	}
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
	return fsx.WriteFile(filepath.Join(dir, fileName(s.SessionID)), data, 0o644)
}

// Update 在跨进程锁内完成会话状态的读-改-写。宿主（如 Claude Code 并行工具调用）
// 会并发派发多个 hook 进程，各自 Load→改→Save 会互相覆盖（丢 Touched、丢防重标记，
// 严重时把 BaseInjected 归零导致 mandatory 重复注入）；Update 在锁内基于最新快照
// 重放 fn 的修改。锁获取超时 fail-open（无锁继续，竞态窗口远小于无锁直写）。
func Update(dir, sessionID string, fn func(*Session)) error {
	return fsx.WithFileLock(filepath.Join(dir, fileName(sessionID)), func() error {
		s := Load(dir, sessionID)
		fn(s)
		return s.Save(dir)
	})
}

func (s *Session) AddTouched(p string) {
	for _, t := range s.Touched {
		if t == p {
			return
		}
	}
	s.Touched = append(s.Touched, p)
}

// AddAdopted 去重追加采纳挂账。
func (s *Session) AddAdopted(name string) {
	for _, v := range s.AdoptedKnowledge {
		if v == name {
			return
		}
	}
	s.AdoptedKnowledge = append(s.AdoptedKnowledge, name)
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
