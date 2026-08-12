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

func codexConfigPath() string { return filepath.Join(CodexHome(), "config.toml") }

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

// codexAgent Codex 适配器：hook 集成 = 合并写用户层 ~/.codex/hooks.json 并确保
// config.toml [features] codex_hooks = true（0.118 起 hooks 是 under-development
// 特性、默认关闭，不开则 hooks.json 装好也静默不派发）；技能目录共享 SkillsHome——
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

// codexHooksFlagOn 报告 config.toml 文本的 [features] 段是否含 codex_hooks = true。
// 行级解析（不做 TOML 全量往返——其余内容逐字节保留）；只认布尔值 true；
// 注释行（# 开头）内的同名键不算。
func codexHooksFlagOn(text string) bool {
	inFeatures := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "[features]":
			inFeatures = true
		case strings.HasPrefix(trimmed, "["):
			inFeatures = false // 下一个段标题——features 段结束
		case inFeatures && !strings.HasPrefix(trimmed, "#") && strings.HasPrefix(trimmed, "codex_hooks"):
			if idx := strings.Index(trimmed, "="); idx >= 0 {
				return strings.TrimSpace(trimmed[idx+1:]) == "true"
			}
		}
	}
	return false
}

// codexEnableHooksFlag 返回开启后的文本与是否有改动：
// [features] 段内已有 codex_hooks 键 → 整行替换为 "codex_hooks = true"（已 true 则不变动）；
// 段存在但无此键 → 段标题行之后插入 "codex_hooks = true"；
// 无 [features] 段 → 文末追加 "[features]\ncodex_hooks = true\n"（追加前确保原文末有换行）。
func codexEnableHooksFlag(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	inFeatures := false
	featuresHeader := -1
	keyLine := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[features]" {
			if featuresHeader < 0 {
				featuresHeader = i
			}
			inFeatures = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inFeatures = false
			continue
		}
		if inFeatures && !strings.HasPrefix(trimmed, "#") && strings.HasPrefix(trimmed, "codex_hooks") {
			if idx := strings.Index(trimmed, "="); idx >= 0 {
				if strings.TrimSpace(trimmed[idx+1:]) == "true" {
					return text, false // 已开启——原文返回，逐字节不动
				}
				keyLine = i
				break
			}
		}
	}
	if keyLine >= 0 {
		lines[keyLine] = "codex_hooks = true"
		return strings.Join(lines, "\n"), true
	}
	if featuresHeader >= 0 {
		lines = append(lines, "")
		copy(lines[featuresHeader+2:], lines[featuresHeader+1:])
		lines[featuresHeader+1] = "codex_hooks = true"
		return strings.Join(lines, "\n"), true
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + "[features]\ncodex_hooks = true\n", true
}

// ensureCodexHooksFeature 确保 config.toml [features] 含 codex_hooks = true：
// Codex hooks 是 under-development 特性、默认关闭——不开则全部 hooks 静默不派发。
// 行级手术编辑，其余内容逐字节保留；写前 .bak-openknowledge 备份；返回是否改动。
func ensureCodexHooksFeature() (bool, error) {
	data, err := os.ReadFile(codexConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	text, changed := codexEnableHooksFlag(string(data))
	if !changed {
		return false, nil
	}
	path := codexConfigPath()
	if len(data) > 0 {
		_ = os.WriteFile(path+".bak-openknowledge", data, 0o644)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return false, fmt.Errorf("开启 codex_hooks 特性: %w", err)
	}
	return true, nil
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
	if err := writeCodexHooks(cfg); err != nil {
		return err
	}
	// Codex 0.118 起 hooks 为 under-development 特性、默认关闭——必须同步开启
	// config.toml [features] codex_hooks，否则 hooks.json 装好也静默不派发。
	if _, err := ensureCodexHooksFeature(); err != nil {
		return err
	}
	return nil
}

// RemoveHooks 只移除 hooks.json 里的 ok 条目；config.toml 的 codex_hooks
// 特性开关单独存在无副作用（只是允许 Codex 派发 hooks），不随移除关闭。
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
// 不复活。codex_hooks 特性开关被关/缺失同样视为过期（曾安装过才走到这）：补开。
func (codexAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(codexHooksPath()); err != nil {
		return nil
	}
	cfg, err := loadCodexHooks()
	if err != nil {
		return err
	}
	events := codexEventsOf(cfg)
	if events == nil || !hasOKCodexHook(events) {
		return nil // 从未安装（无任何 ok 条目）不复活——含用户显式移除
	}
	if !codexHooksCurrent(events, exe) {
		events = codexEventsEdit(cfg)
		stripOKCodexHooks(events)
		for _, e := range codexHookEvents {
			groups, _ := events[e.event].([]any)
			events[e.event] = append(groups, codexOKGroup(exe, e.matcher, e.okHook))
		}
		if err := writeCodexHooks(cfg); err != nil {
			return err
		}
	}
	// codex_hooks 特性开关被关/缺失视为过期（曾安装过才走到这）：补开。
	_, err = ensureCodexHooksFeature()
	return err
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
	if !codexHooksCurrent(events, exe) {
		return false
	}
	// 特性开关关闭/缺失 = 集成失效（hooks 静默不派发），视为未安装。
	data, err := os.ReadFile(codexConfigPath())
	if err != nil || !codexHooksFlagOn(string(data)) {
		return false
	}
	return true
}
