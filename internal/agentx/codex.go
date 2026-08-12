package agentx

import (
	"encoding/json"
	"fmt"
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

// codexOKGroup 生成一个事件的 ok hook 组：type=command shell 串，
// timeout 秒级 = 全局 HookTimeoutSec()。
func codexOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": codexCommand(exe, okHook),
		"timeout": HookTimeoutSec(),
	}
	return map[string]any{"matcher": matcher, "hooks": []any{hook}}
}

// loadCodexHooks 读 hooks.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadCodexHooks() (map[string]any, error) {
	data, err := os.ReadFile(codexHooksPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("codex hooks.json 解析失败: %w", err)
	}
	return cfg, nil
}

// codexEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func codexEventsOf(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	return events
}

// codexEventsEdit 取 hooks 事件表供写入：缺失时创建。
func codexEventsEdit(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		cfg["hooks"] = events
	}
	return events
}

// stripOKCodexHooks 移除事件表里所有 ok 自有 hook（组内 hooks 被删空时整组移除，
// 事件数组空了删事件键），返回是否有改动。第三方条目原样保留。
func stripOKCodexHooks(events map[string]any) bool {
	changed := false
	for name, v := range events {
		groups, _ := v.([]any)
		if groups == nil {
			continue
		}
		kept := groups[:0]
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			if gm == nil || hooks == nil {
				kept = append(kept, g)
				continue
			}
			var keptHooks []any
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKCodexHook(hm) {
					changed = true
					continue
				}
				keptHooks = append(keptHooks, h)
			}
			if len(keptHooks) == 0 {
				changed = true // 整组都是 ok 的，连组移除
				continue
			}
			gm["hooks"] = keptHooks
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(events, name)
		} else {
			events[name] = kept
		}
	}
	return changed
}

// hasOKCodexHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKCodexHook(events map[string]any) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKCodexHook(hm) {
					return true
				}
			}
		}
	}
	return false
}

// codexHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态
// （command=exe、matcher 与 timeout 正确）。
func codexHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		found := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if gm == nil {
				continue
			}
			matcher, _ := gm["matcher"].(string)
			if matcher != e.matcher {
				continue
			}
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				hm, _ := h.(map[string]any)
				if hm == nil || !isOKCodexHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeout"].(float64)
				if cmd == codexCommand(exe, e.okHook) && timeout == wantTimeout {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// writeCodexHooks 备份后写回 hooks.json（MarshalIndent，未知字段保留）。
func writeCodexHooks(cfg map[string]any) error {
	path := codexHooksPath()
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak-openknowledge", data, 0o644)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (codexAgent) InstallHooks(exe string) error {
	cfg, err := loadCodexHooks()
	if err != nil {
		return err
	}
	events := codexEventsEdit(cfg)
	stripOKCodexHooks(events)
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, codexOKGroup(exe, e.matcher, e.okHook))
	}
	return writeCodexHooks(cfg)
}

func (codexAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(codexHooksPath()); os.IsNotExist(err) {
		return false, nil
	}
	cfg, err := loadCodexHooks()
	if err != nil {
		return false, err
	}
	events := codexEventsOf(cfg)
	if events == nil || !stripOKCodexHooks(events) {
		return false, nil
	}
	if err := writeCodexHooks(cfg); err != nil {
		return false, fmt.Errorf("移除 codex hooks: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：hooks.json 存在、曾安装过 ok hooks 且内容过期（exe 迁移、
// 超时变更）时重写；从未安装（无任何 ok 条目）则 no-op——用户显式移除的集成
// 不复活。
func (codexAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(codexHooksPath()); err != nil {
		return nil
	}
	cfg, err := loadCodexHooks()
	if err != nil {
		return err
	}
	events := codexEventsOf(cfg)
	if events == nil || !hasOKCodexHook(events) || codexHooksCurrent(events, exe) {
		return nil
	}
	events = codexEventsEdit(cfg)
	stripOKCodexHooks(events)
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, codexOKGroup(exe, e.matcher, e.okHook))
	}
	return writeCodexHooks(cfg)
}

func (codexAgent) HooksInstalled() bool {
	cfg, err := loadCodexHooks()
	if err != nil {
		return false
	}
	events := codexEventsOf(cfg)
	if events == nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return codexHooksCurrent(events, exe)
}
