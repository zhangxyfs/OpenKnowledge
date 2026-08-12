package agentx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CodexHome 返回 Codex 配置根目录：OK_CODEX_HOME（ok 自留测试隔离口，
// OK_CLAUDE_HOME 同款命名）> CODEX_HOME（Codex CLI 官方重定位环境变量）>
// ~/.codex。os.UserHomeDir() 跟随环境重定向，与各适配器 XxxHome 一致——
// hook 子进程在 shadow HOME 下自愈最坏只写 shadow 副本，真实配置无风险。
func CodexHome() string {
	if h := os.Getenv("OK_CODEX_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func codexHooksPath() string { return filepath.Join(CodexHome(), "hooks.json") }

// codexHookEvents 是 ok 接入的 Codex hook 事件（对应 ok 的三条 hook 链路）。
// 命令形态与 claude 适配器相同：shell 字符串（正斜杠 exe + 双引号），输出协议
// Claude JSON（args 末尾 "claude"）——Codex hook 契约逐字兼容 Claude Code
// （hookSpecificOutput.additionalContext 注入、Stop decision:block 阻断），
// hook.go 输出层零改动。PostToolUse 只追 apply_patch（Codex 专用写盘工具，
// 无 Write/Edit），不追 Bash——与 claude 不追 Bash 对齐。
var codexHookEvents = []struct {
	event   string // Codex 事件名（逐字沿用 Claude Code 命名）
	matcher string // 组级 matcher
	okHook  string // ok hook 子命令
}{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "apply_patch", "post-tool"},
	{"Stop", "*", "stop"},
}

// codexCommand 生成 hook 命令串（claudeCommand 同款形态）。
func codexCommand(exe, okHook string) string {
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// isOKCodexHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串以
// " hook <prompt|post-tool|stop> claude" 结尾。不看 exe basename——改名/迁移/
// 测试二进制都不影响识别。
func isOKCodexHook(h map[string]any) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range codexHookEvents {
		if strings.HasSuffix(cmd, " hook "+e.okHook+" claude") {
			return true
		}
	}
	return false
}

// codexAgent Codex 适配器：hook 集成 = 合并写用户层 ~/.codex/hooks.json
// （官方建议每层一种机制，不动 config.toml）；技能目录共享 SkillsHome——
// Codex 原生扫描 USER 作用域 ~/.agents/skills（opencode 同款零适配）。
type codexAgent struct{}

func init() { Register(codexAgent{}) }

func (codexAgent) ID() string          { return "codex" }
func (codexAgent) DisplayName() string { return "Codex" }
func (codexAgent) HooksTarget() string { return codexHooksPath() }
func (codexAgent) SkillsDir() string   { return SkillsHome() }

func (codexAgent) Detect() bool {
	info, err := os.Stat(CodexHome())
	return err == nil && info.IsDir()
}

// 以下四个方法为 Task 3 行走骨架（walking skeleton），Task 4 填充真实合并写实现。
func (codexAgent) InstallHooks(exe string) error { return nil }
func (codexAgent) RemoveHooks() (bool, error)    { return false, nil }
func (codexAgent) EnsureHooks(exe string) error  { return nil }
func (codexAgent) HooksInstalled() bool          { return false }
