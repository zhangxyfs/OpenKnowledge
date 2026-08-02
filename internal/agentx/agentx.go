// Package agentx 抽象 AI 编码 agent 的 hook 集成：每个 agent 一个适配器，
// 注册表统一驱动 CLI / GUI / hook 自愈；新增 agent = 实现 Agent 并 Register。
package agentx

import (
	"os"
	"path/filepath"
)

// Agent 一个 AI 编码 agent 的集成适配器。
type Agent interface {
	ID() string                   // 稳定标识："kimi" / "pi"，CLI/GUI/API 统一使用
	DisplayName() string          // 展示名："Kimi Code" / "Pi"
	Detect() bool                 // 本机是否已安装该 agent
	HooksInstalled() bool         // hooks 集成是否已安装且为当前版本
	InstallHooks(exe string) error
	RemoveHooks() (bool, error)   // 返回是否真的移除了内容
	EnsureHooks(exe string) error // hook 入口自愈；错误由调用方 fail-open 处理
	HooksTarget() string          // hook 写入目标的展示路径
	SkillsDir() string            // 技能目录（当前均返回共享 SkillsHome）
}

var agents []Agent

// Register 登记 agent（在各适配器文件的 init 中调用）。
func Register(a Agent) { agents = append(agents, a) }

// All 返回全部已注册 agent（注册顺序）。
func All() []Agent { return append([]Agent(nil), agents...) }

// Find 按 id 查找 agent。
func Find(id string) (Agent, bool) {
	for _, a := range agents {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}

// Detected 返回本机已安装的 agent。
func Detected() []Agent {
	var out []Agent
	for _, a := range agents {
		if a.Detect() {
			out = append(out, a)
		}
	}
	return out
}

// SkillsHome 返回共享技能安装目录（OK_SKILLS_HOME 优先）。
func SkillsHome() string {
	if h := os.Getenv("OK_SKILLS_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents", "skills")
}
