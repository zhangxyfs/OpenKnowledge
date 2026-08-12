package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ClaudeHome 返回 Claude 生态配置根目录（OK_CLAUDE_HOME 优先——ok 自留测试隔离口，
// OK_ZCODE_HOME 同款），否则 ~/.claude。CodePilot 等 claude-agent-sdk 兼容宿主经
// settingSources:['user'] 同样加载该目录的 settings.json（hooks 字段 shadow 原样继承）。
func ClaudeHome() string {
	if h := os.Getenv("OK_CLAUDE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func claudeSettingsPath() string { return filepath.Join(ClaudeHome(), "settings.json") }

// codepilotHome 仅用于 Detect：只装 CodePilot 的机器可能还没有 ~/.claude。
// OK_CODEPILOT_HOME 为测试隔离口；CLAUDE_GUI_DATA_DIR 是 CodePilot 官方覆盖。
func codepilotHome() string {
	if h := os.Getenv("OK_CODEPILOT_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("CLAUDE_GUI_DATA_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codepilot")
}

// claudeHookEvents 是 ok 接入的 Claude Code hook 事件（对应 ok 的三条 hook 链路）。
// 命令为 shell 字符串：正斜杠 exe + 双引号（cmd.exe 与 bash 均可执行——探针实测
// cmd 接受正斜杠路径）。输出协议 Claude JSON（args 末尾 "claude"）：注入走
// hookSpecificOutput.additionalContext，Stop 阻断走 decision:block（hook.go 现成）。
var claudeHookEvents = []struct {
	event   string // Claude Code 事件名
	matcher string // 组级 matcher（UserPromptSubmit/Stop 用 "*"，实测可用形态）
	okHook  string // ok hook 子命令
}{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "*", "stop"},
}

// claudeCommand 生成 hook 命令串。
func claudeCommand(exe, okHook string) string {
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// isOKClaudeHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串以
// " hook <prompt|post-tool|stop> claude" 结尾。不看 exe basename——改名/迁移/
// 测试二进制都不影响识别。
func isOKClaudeHook(h map[string]any) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range claudeHookEvents {
		if strings.HasSuffix(cmd, " hook "+e.okHook+" claude") {
			return true
		}
	}
	return false
}

// claudeAgent Claude 生态适配器：hook 集成 = 合并写 ~/.claude/settings.json 的
// hooks 字段（Claude Code 与 CodePilot 共享此文件，装一次多宿主生效）；
// 技能目录 ~/.claude/skills（CodePilot 的 skill-discovery 同样扫描）。
type claudeAgent struct{}

func init() { Register(claudeAgent{}) }

func (claudeAgent) ID() string          { return "claude" }
func (claudeAgent) DisplayName() string { return "Claude Code（含 CodePilot 等兼容宿主）" }
func (claudeAgent) HooksTarget() string { return claudeSettingsPath() }
func (claudeAgent) SkillsDir() string   { return filepath.Join(ClaudeHome(), "skills") }

func (claudeAgent) Detect() bool {
	for _, dir := range []string{ClaudeHome(), codepilotHome()} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// 接口占位（Task 2 补全真实行为）：骨架阶段零值实现——不注册则编译不过，
// 零值语义（未安装/无需自愈/无可移除）对未安装链路安全。
func (claudeAgent) RemoveHooks() (bool, error) { return false, nil }
func (claudeAgent) EnsureHooks(exe string) error {
	return nil
}
func (claudeAgent) HooksInstalled() bool { return false }

// claudeOKGroup 生成一个事件的 ok hook 组：type=command shell 串，
// timeout 秒级 = 全局 [hooks] timeout_sec。
func claudeOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": claudeCommand(exe, okHook),
		"timeout": HookTimeoutSec(),
	}
	return map[string]any{"matcher": matcher, "hooks": []any{hook}}
}

// loadClaudeSettings 读 settings.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadClaudeSettings() (map[string]any, error) {
	data, err := os.ReadFile(claudeSettingsPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("claude settings.json 解析失败: %w", err)
	}
	return cfg, nil
}

// claudeEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func claudeEventsOf(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	return events
}

// claudeEventsEdit 取 hooks 事件表供写入：缺失时创建（Claude Code 无 enabled 开关）。
func claudeEventsEdit(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		cfg["hooks"] = events
	}
	return events
}

// stripOKClaudeHooks 移除事件表里所有 ok 自有 hook（组内 hooks 被删空时整组移除，
// 事件数组空了删事件键），返回是否有改动。第三方条目原样保留。
func stripOKClaudeHooks(events map[string]any) bool {
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
				if hm, _ := h.(map[string]any); hm != nil && isOKClaudeHook(hm) {
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

// hasOKClaudeHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKClaudeHook(events map[string]any) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKClaudeHook(hm) {
					return true
				}
			}
		}
	}
	return false
}

// claudeHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态
// （command=exe、matcher 与 timeout 正确）。
func claudeHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range claudeHookEvents {
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
				if hm == nil || !isOKClaudeHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeout"].(float64)
				if cmd == claudeCommand(exe, e.okHook) && timeout == wantTimeout {
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

// writeClaudeSettings 备份后写回 settings.json（MarshalIndent，未知字段保留）。
func writeClaudeSettings(cfg map[string]any) error {
	path := claudeSettingsPath()
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

func (claudeAgent) InstallHooks(exe string) error {
	cfg, err := loadClaudeSettings()
	if err != nil {
		return err
	}
	events := claudeEventsEdit(cfg)
	stripOKClaudeHooks(events)
	for _, e := range claudeHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, claudeOKGroup(exe, e.matcher, e.okHook))
	}
	return writeClaudeSettings(cfg)
}
