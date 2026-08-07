package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ZcodeHome 返回 ZCode 配置根目录（OK_ZCODE_HOME 优先——ok 自留的测试隔离口，
// ZCode 官方未文档化配置目录环境变量），否则 ~/.zcode。
// 见 https://zcode.z.ai/cn/docs/hooks 与 /cn/docs/skill。
func ZcodeHome() string {
	if h := os.Getenv("OK_ZCODE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zcode")
}

func zcodeConfigPath() string { return filepath.Join(ZcodeHome(), "cli", "config.json") }

// zcodeHookEvents 是 ok 接入的 ZCode hook 事件（对应 ok 的三条 hook 链路）。
// 输出协议用 Claude 风格 JSON（args 末尾的 "claude"）：ZCode 只把以 { 开头的
// 合法 JSON stdout 解析为协议结果，纯文本 stdout 不进模型上下文。
var zcodeHookEvents = []struct {
	event   string // ZCode 事件名
	matcher string // 空 = 不过滤
	okHook  string // ok hook 子命令
}{
	{"UserPromptSubmit", "", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "", "stop"},
}

// zcodeAgent ZCode 适配器：hook 集成 = 合并写 ~/.zcode/cli/config.json；
// 技能目录独立于共享 SkillsHome（ZCode 不自动读 ~/.agents/skills）。
type zcodeAgent struct{}

func init() { Register(zcodeAgent{}) }

func (zcodeAgent) ID() string          { return "zcode" }
func (zcodeAgent) DisplayName() string { return "ZCode" }
func (zcodeAgent) HooksTarget() string { return zcodeConfigPath() }
func (zcodeAgent) SkillsDir() string   { return filepath.Join(ZcodeHome(), "skills") }

func (zcodeAgent) Detect() bool {
	info, err := os.Stat(ZcodeHome())
	return err == nil && info.IsDir()
}

// isOKZcodeHook 判定一条 ZCode hook 条目是否 ok 生成：args 形如 ["hook", "<事件>", ...]，
// 事件为 ok 的三条链路之一。不看 command 的 basename——测试二进制、exe 改名/迁移都不影响识别。
func isOKZcodeHook(h map[string]any) bool {
	args, _ := h["args"].([]any)
	if len(args) < 2 || args[0] != "hook" {
		return false
	}
	switch args[1] {
	case "prompt", "post-tool", "stop":
		return true
	}
	return false
}

// zcodeOKGroup 生成一个事件的 ok hook 组：process 直 exec（不过 shell），
// 三条事件统一 timeoutMs = 全局 [hooks] timeout_sec × 1000。
func zcodeOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":      "process",
		"command":   filepath.ToSlash(exe),
		"args":      []any{"hook", okHook, "claude"},
		"timeoutMs": HookTimeoutSec() * 1000,
	}
	g := map[string]any{"hooks": []any{hook}}
	if matcher != "" {
		g["matcher"] = matcher
	}
	return g
}

// loadZcodeConfig 读 config.json；文件不存在返回空对象，解析失败报错（不覆盖损坏文件）。
// 用 map[string]any 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadZcodeConfig() (map[string]any, error) {
	data, err := os.ReadFile(zcodeConfigPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("zcode config.json 解析失败: %w", err)
	}
	return cfg, nil
}

// zcodeEventsOf 取 hooks.events（只读视图），不做任何创建。
func zcodeEventsOf(cfg map[string]any) map[string]any {
	hooks, _ := cfg["hooks"].(map[string]any)
	events, _ := hooks["events"].(map[string]any)
	return events
}

// zcodeEventsEdit 取 hooks.events 供写入：缺失时创建并把 hooks.enabled 置 true
// （ZCode 要求显式开启，否则整份 hooks 配置不生效）。
func zcodeEventsEdit(cfg map[string]any) map[string]any {
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		cfg["hooks"] = hooks
	}
	hooks["enabled"] = true
	events, _ := hooks["events"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		hooks["events"] = events
	}
	return events
}

// stripOKZcodeHooks 移除 events 里所有 ok 自有 hook（组内 hooks 被删空时整组移除，
// 事件数组空了删事件键），返回是否有改动。
func stripOKZcodeHooks(events map[string]any) bool {
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
				if hm, _ := h.(map[string]any); hm != nil && isOKZcodeHook(hm) {
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

// hasOKZcodeHook 报告 events 里是否存在任何 ok 自有 hook。
func hasOKZcodeHook(events map[string]any) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKZcodeHook(hm) {
					return true
				}
			}
		}
	}
	return false
}

// zcodeHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态
// （command=exe、args 含 claude、matcher 与 timeoutMs 正确）。
func zcodeHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec() * 1000)
	wantExe := filepath.ToSlash(exe)
	for _, e := range zcodeHookEvents {
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
				if hm == nil || !isOKZcodeHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeoutMs"].(float64)
				args, _ := hm["args"].([]any)
				if cmd == wantExe && timeout == wantTimeout &&
					len(args) == 3 && args[1] == e.okHook && args[2] == "claude" {
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

// writeZcodeConfig 备份后写回 config.json（MarshalIndent，未知字段保留）。
func writeZcodeConfig(cfg map[string]any) error {
	path := zcodeConfigPath()
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

func (zcodeAgent) InstallHooks(exe string) error {
	cfg, err := loadZcodeConfig()
	if err != nil {
		return err
	}
	events := zcodeEventsEdit(cfg)
	stripOKZcodeHooks(events)
	for _, e := range zcodeHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, zcodeOKGroup(exe, e.matcher, e.okHook))
	}
	return writeZcodeConfig(cfg)
}

func (zcodeAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(zcodeConfigPath()); os.IsNotExist(err) {
		return false, nil
	}
	cfg, err := loadZcodeConfig()
	if err != nil {
		return false, err
	}
	events := zcodeEventsOf(cfg)
	if events == nil || !stripOKZcodeHooks(events) {
		return false, nil
	}
	if err := writeZcodeConfig(cfg); err != nil {
		return false, fmt.Errorf("移除 zcode hooks: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：config 存在、曾安装过 ok hooks 且内容过期（exe 迁移、超时
// 变更、旧格式）时重写；从未安装（无任何 ok 条目）则 no-op——zcode 没有 kimi
// "标记注释被清"的已知行为，用户显式移除的集成不复活。
func (zcodeAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(zcodeConfigPath()); err != nil {
		return nil
	}
	cfg, err := loadZcodeConfig()
	if err != nil {
		return err
	}
	events := zcodeEventsOf(cfg)
	if events == nil || !hasOKZcodeHook(events) || zcodeHooksCurrent(events, exe) {
		return nil
	}
	events = zcodeEventsEdit(cfg)
	stripOKZcodeHooks(events)
	for _, e := range zcodeHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, zcodeOKGroup(exe, e.matcher, e.okHook))
	}
	return writeZcodeConfig(cfg)
}

func (zcodeAgent) HooksInstalled() bool {
	cfg, err := loadZcodeConfig()
	if err != nil {
		return false
	}
	events := zcodeEventsOf(cfg)
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
	return zcodeHooksCurrent(events, exe)
}
