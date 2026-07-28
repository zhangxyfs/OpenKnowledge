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
