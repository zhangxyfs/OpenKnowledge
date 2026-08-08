// Package wiki 管理项目 wiki 的游标（state/wiki.json）与 git 落后计数。
// 叶子包：只依赖 stdlib、procx 与外部 git 命令。
package wiki

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/procx"
)

// Cursor 记录上次 wiki 生成到的位置。
type Cursor struct {
	LastCommit  string    `json:"last_commit"`
	GeneratedAt time.Time `json:"generated_at"`
	EntryCount  int       `json:"entry_count"` // 纯展示用
}

// CursorPath 返回游标文件路径（固定文件名，不受 state 目录 session-* GC 影响）。
func CursorPath(stateDir string) string { return filepath.Join(stateDir, "wiki.json") }

// LoadCursor 读游标；不存在或损坏返回 nil。
func LoadCursor(stateDir string) *Cursor {
	data, err := os.ReadFile(CursorPath(stateDir))
	if err != nil {
		return nil
	}
	var c Cursor
	if json.Unmarshal(data, &c) != nil {
		return nil
	}
	return &c
}

// SaveCursor 写游标（必要时创建 state 目录）。
func SaveCursor(stateDir string, c *Cursor) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(CursorPath(stateDir), data, 0o644)
}

// BranchCursor 单分支游标（字段语义同旧 Cursor）。
type BranchCursor struct {
	LastCommit  string    `json:"last_commit"`
	GeneratedAt time.Time `json:"generated_at"`
	EntryCount  int       `json:"entry_count,omitempty"`
}

// State 是 wiki.json 的新格式：基准分支 + 按分支游标表。
// Legacy 承载旧格式（顶层 last_commit）的惰性迁移：LoadState 识别后挂在此处、
// 不落盘；归属判定（git 可达性）由 CheckStatus 完成（见 status.go）。
type State struct {
	BaseBranch string                  `json:"base_branch,omitempty"`
	Cursors    map[string]BranchCursor `json:"cursors,omitempty"`
	Legacy     *BranchCursor           `json:"-"`
}

// LoadState 读 wiki.json：不存在/损坏返回 nil；旧格式（顶层 last_commit 且
// 无 cursors）升级为 State{Legacy}，归属待 CheckStatus 判定。
func LoadState(stateDir string) *State {
	data, err := os.ReadFile(CursorPath(stateDir))
	if err != nil {
		return nil
	}
	var disk struct {
		BaseBranch  string                  `json:"base_branch"`
		Cursors     map[string]BranchCursor `json:"cursors"`
		LastCommit  string                  `json:"last_commit"`
		GeneratedAt time.Time               `json:"generated_at"`
		EntryCount  int                     `json:"entry_count"`
	}
	if json.Unmarshal(data, &disk) != nil {
		return nil
	}
	s := &State{BaseBranch: disk.BaseBranch, Cursors: disk.Cursors}
	if disk.Cursors == nil && disk.LastCommit != "" {
		s.Legacy = &BranchCursor{
			LastCommit:  disk.LastCommit,
			GeneratedAt: disk.GeneratedAt,
			EntryCount:  disk.EntryCount,
		}
	}
	return s
}

// SaveState 以新格式写 wiki.json（Legacy 不落盘）。
func SaveState(stateDir string, s *State) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(CursorPath(stateDir), data, 0o644)
}

// Status 是 ok wiki status 的结果。Behind=-1 表示 git 不可用。
type Status struct {
	HasWiki    bool   `json:"has_wiki"`
	LastCommit string `json:"last_commit,omitempty"`
	Behind     int    `json:"behind"`
	Stale      bool   `json:"stale"`
	Threshold  int    `json:"threshold"`
}

// CheckStatus 计算 wiki 落后状态。srcDir 为项目源码目录（git 仓库）。
// 无游标时 Behind 为全历史 commit 数；游标 commit 失踪/非 git 项目时 Behind=-1。
func CheckStatus(stateDir, srcDir string, threshold int) *Status {
	st := &Status{Behind: -1, Threshold: threshold}
	c := LoadCursor(stateDir)
	if c == nil {
		if n, err := countCommits(srcDir, "HEAD"); err == nil {
			st.Behind = n
			st.Stale = threshold > 0 && n >= threshold
		}
		return st
	}
	st.HasWiki = true
	st.LastCommit = c.LastCommit
	if c.LastCommit == "" {
		return st
	}
	if n, err := countCommits(srcDir, c.LastCommit+"..HEAD"); err == nil {
		st.Behind = n
		st.Stale = threshold > 0 && n >= threshold
	}
	return st
}

// HeadCommit 返回 srcDir 的 HEAD 完整 hash。
func HeadCommit(srcDir string) (string, error) {
	cmd := exec.Command("git", "-C", srcDir, "rev-parse", "HEAD")
	procx.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func countCommits(srcDir, rev string) (int, error) {
	cmd := exec.Command("git", "-C", srcDir, "rev-list", "--count", rev)
	procx.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}
