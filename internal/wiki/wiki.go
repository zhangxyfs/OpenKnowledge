// Package wiki 管理项目 wiki 的游标（state/wiki.json）与 git 落后计数。
// 叶子包：只依赖 stdlib、procx/fsx 与外部 git 命令。
package wiki

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/fsx"
	"openknowledge/internal/procx"
)

// CursorPath 返回游标文件路径（固定文件名，不受 state 目录 session-* GC 影响）。
func CursorPath(stateDir string) string { return filepath.Join(stateDir, "wiki.json") }

// BranchCursor 单分支游标（字段语义同旧 Cursor）。
type BranchCursor struct {
	LastCommit  string    `json:"last_commit"`
	GeneratedAt time.Time `json:"generated_at"`
	EntryCount  int       `json:"entry_count,omitempty"`
}

// MergeRecord 是一条合并谱系：from 分支于 time 被并入 to（检出时 HEAD 为 commit）。
type MergeRecord struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	Commit string    `json:"commit"`
	Time   time.Time `json:"time"`
}

// State 是 wiki.json 的新格式：基准分支 + 按分支游标表 + 合并谱系。
// Legacy 承载旧格式（顶层 last_commit）的惰性迁移：LoadState 识别后挂在此处、
// 不落盘；归属判定（git 可达性）由 CheckStatus 完成（见 status.go）。
type State struct {
	BaseBranch string                  `json:"base_branch,omitempty"`
	Cursors    map[string]BranchCursor `json:"cursors,omitempty"`
	Merges     []MergeRecord           `json:"merges,omitempty"`
	Legacy     *BranchCursor           `json:"-"`
}

// AppendMerge 追加合并谱系（from+commit 判重）；返回是否实际新增。
func (s *State) AppendMerge(from, to, commit string, t time.Time) bool {
	for _, m := range s.Merges {
		if m.From == from && m.Commit == commit {
			return false
		}
	}
	s.Merges = append(s.Merges, MergeRecord{From: from, To: to, Commit: commit, Time: t})
	return true
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
		Merges      []MergeRecord           `json:"merges"`
		LastCommit  *string                 `json:"last_commit"` // 键存在即旧格式（含空值：非 git mark 的时间戳游标）
		GeneratedAt time.Time               `json:"generated_at"`
		EntryCount  int                     `json:"entry_count"`
	}
	if json.Unmarshal(data, &disk) != nil {
		return nil
	}
	s := &State{BaseBranch: disk.BaseBranch, Cursors: disk.Cursors, Merges: disk.Merges}
	if disk.Cursors == nil && disk.LastCommit != nil {
		s.Legacy = &BranchCursor{
			LastCommit:  *disk.LastCommit,
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
	return fsx.WriteFile(CursorPath(stateDir), data, 0o644)
}

// Status 是 ok wiki status 的结果。Behind=-1 表示 git 不可用或状态无法计算。
// BranchState 分支状态："ok"（正常）、"no_cursor"（本分支无基线）、"diverged"
// （游标与本分支分叉）、"gone"（游标 commit 被改写）、"legacy_orphan"（旧格式
// 游标归属不可判）；非 git 项目与旧行为路径为空串。
type Status struct {
	HasWiki     bool   `json:"has_wiki"`
	LastCommit  string `json:"last_commit,omitempty"`
	Behind      int    `json:"behind"`
	Stale       bool   `json:"stale"`
	Threshold   int    `json:"threshold"`
	Branch      string `json:"branch,omitempty"`
	BaseBranch  string `json:"base_branch,omitempty"`
	BranchState string `json:"branch_state,omitempty"`
	MergeBase   string `json:"merge_base,omitempty"`
}

// CheckStatus 计算 wiki 状态（只读 git 与游标文件，绝不写盘；迁移落盘只发生
// 在 mark/base 写入路径）。srcDir 为项目源码目录。非 git 项目行为同旧版。
func CheckStatus(stateDir, srcDir string, threshold int) *Status {
	st := &Status{Behind: -1, Threshold: threshold}
	s := LoadState(stateDir)
	branch := CurrentBranch(srcDir)
	st.Branch = branch
	if s == nil || (s.Legacy == nil && len(s.Cursors) == 0) {
		// 无 wiki 游标：现状路径（非 git 时 Behind=-1）。含空 cursors 状态文件
		// （如仅由 ok wiki base 落盘的 {"base_branch":"dev"}），否则 no-wiki 提示
		// 会因 HasWiki=false、Behind=-1 直返而被永久抑制。
		if s != nil {
			st.BaseBranch = s.BaseBranch
		}
		if n, err := countCommits(srcDir, "HEAD"); err == nil {
			st.Behind = n
			st.Stale = threshold > 0 && n >= threshold
		}
		return st
	}
	st.BaseBranch = s.BaseBranch
	// 旧格式惰性迁移判定（不写盘）
	if s.Legacy != nil {
		lc := s.Legacy.LastCommit
		switch {
		case branch == "" || !commitExists(srcDir, lc):
			// git 不可判：保持旧行为（直接用 legacy 算，失败则 Behind=-1）
			st.HasWiki = true
			st.LastCommit = lc
			if lc == "" {
				// 空游标（非 git mark 的时间戳游标）：旧版提前返回，不算 behind
				return st
			}
			if n, err := countCommits(srcDir, lc+"..HEAD"); err == nil {
				st.Behind = n
				st.Stale = threshold > 0 && n >= threshold
			}
			return st
		case isAncestor(srcDir, lc, "HEAD"):
			// 可达 → 视同归入当前分支（内存升级，不落盘）
			st.HasWiki = true
			st.LastCommit = lc
			st.BranchState = "ok"
			if s.BaseBranch == "" {
				st.BaseBranch = branch
			}
			if n, err := countCommits(srcDir, lc+"..HEAD"); err == nil {
				st.Behind = n
				st.Stale = threshold > 0 && n >= threshold
			}
			return st
		default:
			// 存在但不可达：报疑，不归入任何分支
			st.HasWiki = true
			st.BranchState = "legacy_orphan"
			return st
		}
	}
	st.HasWiki = len(s.Cursors) > 0
	cur, ok := s.Cursors[branch]
	if !ok {
		// 本分支无基线：展示基准分支游标供提示使用
		if bc, ok2 := s.Cursors[s.BaseBranch]; ok2 {
			st.LastCommit = bc.LastCommit
		}
		if st.HasWiki {
			st.BranchState = "no_cursor"
		}
		return st
	}
	st.LastCommit = cur.LastCommit
	lc := cur.LastCommit
	if lc == "" {
		return st // 非 git mark 的时间戳游标：无 behind 可算，同旧行为
	}
	// 收敛口径：rev-list 对"存在但非祖先"的游标也会成功，不能隐含可达性；
	// 用 merge-base 一次判别三态（ok 路径 git 调用 4→3：symbolic-ref + merge-base + rev-list）。
	mb := mergeBase(srcDir, lc, "HEAD")
	if mb == "" {
		if !commitExists(srcDir, lc) {
			st.BranchState = "gone"
		} else {
			st.BranchState = "diverged" // 无共同祖先
		}
		return st
	}
	if mb == lc {
		st.BranchState = "ok"
		if n, err := countCommits(srcDir, lc+"..HEAD"); err == nil {
			st.Behind = n
			st.Stale = threshold > 0 && n >= threshold
		}
		return st
	}
	st.BranchState = "diverged"
	st.MergeBase = mb
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
