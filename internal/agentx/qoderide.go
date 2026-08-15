package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"openknowledge/internal/fsx"
)

// LingmaHome 返回 Qoder CN IDE（通义灵码内核）配置根目录：OK_QODER_IDE_HOME
// （ok 自留测试隔离口，OK_CLAUDE_HOME 同款命名）> ~/.lingma。注意与 qoder 适配器
// （终端 CLI）是两套：CLI 读 ~/.qoder-cn/settings.json，IDE 读 ~/.lingma/settings.json
// （官方 IDE hooks 文档 docs.qoder.cn/user-guide/hooks 核实；IDE 无目录重定位环境变量）。
func LingmaHome() string {
	if h := os.Getenv("OK_QODER_IDE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lingma")
}

func lingmaSettingsPath() string { return filepath.Join(LingmaHome(), "settings.json") }

// lingmaHookEvents 是 ok 接入的 Qoder CN IDE hook 事件（对应 ok 的三条 hook 链路）。
// IDE hooks 契约与 CLI 同构（settings.json hooks 分组、command 类型、stdin JSON、
// 退出码 0/2、stdout JSON continue/stopReason/suppressOutput/hookSpecificOutput），
// 但能力降级（官方文档核实）：仅 5 事件、**Stop 与 PostToolUse 不可阻断**（enforce/
// auto 自省在 IDE 上不走通——Stop 的 decision:block 输出被忽略，静默放行）、无
// hooksConfig.enabled 门、改配置需重启 IDE 生效（无热加载）。输出协议复用 Claude
// JSON（args 末尾 "claude"）：注入 hookSpecificOutput.additionalContext（IDE 文档
// 的 UserPromptSubmit 场景明列"自动注入上下文"），hook.go 输出层零改动。
// PostToolUse 追 Write|Edit（IDE 工具名双套——原生 run_in_terminal/create_file/
// search_replace 与兼容名 Bash/Write/Edit 运行时映射，matcher 两套都认；无空格形态
// 在"| 拆分"与"正则"两种匹配语义下均正确）。
var lingmaHookEvents = []struct {
	event   string // IDE 事件名
	matcher string // 组级 matcher（不填或 * 匹配全部，| 多值）
	okHook  string // ok hook 子命令
}{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "*", "stop"},
}

// lingmaCommand 生成 hook 命令串，按平台分叉：
//   - Windows：IDE 的 command 执行模型未文档化（官方示例为 chmod +x 的脚本路径，
//     Unix 形态），quoted 命令串在 cmd /s 剥引号语义下会静默不执行（qoder CLI 实测
//     同源问题）——故沿用 .cmd 包装文件绝对路径裸串：cmd 外壳执行可跑，且与 exe
//     位置解耦（迁移自愈只改包装内容，settings.json 逐字节不动）。
//   - 其他平台：quoted shell 串（脚本路径语义，quoted 无碍）。
func lingmaCommand(exe, okHook string) string {
	if runtime.GOOS == "windows" {
		return lingmaWrapperPath(okHook)
	}
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// lingmaWrapperPath 返回 Windows 包装文件路径：<LingmaHome>/ok-hook-<okHook>.cmd
// （与 qoder CLI 的包装同名不同目录——~/.lingma vs ~/.qoder-cn，无冲突）。
func lingmaWrapperPath(okHook string) string {
	return filepath.Join(LingmaHome(), "ok-hook-"+okHook+".cmd")
}

// lingmaWrapperContent 返回包装文件内容：单行 @"<exe>" hook <okHook> claude（CRLF 结尾）。
func lingmaWrapperContent(exe, okHook string) string {
	return "@\"" + exe + "\" hook " + okHook + " claude\r\n"
}

// ensureLingmaWrappers 确保三个包装文件存在且内容为当前 exe（缺失/过期重写，
// 已当前则不写盘）。仅 Windows 调用。
func ensureLingmaWrappers(exe string) error {
	for _, e := range lingmaHookEvents {
		path := lingmaWrapperPath(e.okHook)
		want := lingmaWrapperContent(exe, e.okHook)
		if data, err := os.ReadFile(path); err == nil && string(data) == want {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("写入 qoder ide hook 包装: %w", err)
		}
		if err := fsx.WriteFile(path, []byte(want), 0o644); err != nil {
			return fmt.Errorf("写入 qoder ide hook 包装: %w", err)
		}
	}
	return nil
}

// removeLingmaWrappers 删除三个包装文件，返回是否有删除。仅删内容确为 ok 生成的
// （含 " hook <okHook> claude"）——防误删用户同名文件。仅 Windows 调用。
func removeLingmaWrappers() bool {
	removed := false
	for _, e := range lingmaHookEvents {
		path := lingmaWrapperPath(e.okHook)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), " hook "+e.okHook+" claude") &&
			os.Remove(path) == nil {
			removed = true
		}
	}
	return removed
}

// isOKQoderIdeHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串形态匹配
// （Windows 认包装文件裸路径，/ 与 \ 分隔、大小写不敏感；quoted 后缀 " hook
// <prompt|post-tool|stop> claude" 两平台都认）。不看 exe basename。
func isOKQoderIdeHook(h map[string]any) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range lingmaHookEvents {
		if runtime.GOOS == "windows" {
			for _, sep := range []string{"/", "\\"} {
				if hasSuffixFold(cmd, sep+"ok-hook-"+e.okHook+".cmd") {
					return true
				}
			}
		}
		if strings.HasSuffix(cmd, " hook "+e.okHook+" claude") {
			return true
		}
	}
	return false
}

// qoderIdeAgent Qoder CN IDE（通义灵码内核）适配器：hook 集成 = 合并写
// ~/.lingma/settings.json 的 hooks 分组（IDE 无 enabled 门）；技能目录
// ~/.lingma/skills（官方 IDE 文档核实，SKILL.md 结构与 Claude 一致，共享模板
// 零适配）。Stop 不可阻断属 IDE 当前版本限制——注入与触碰追踪可用，enforce 降级
// （详见 lingmaHookEvents 注释）。
type qoderIdeAgent struct{}

func init() { Register(qoderIdeAgent{}) }

func (qoderIdeAgent) ID() string          { return "qoder-ide" }
func (qoderIdeAgent) DisplayName() string { return "Qoder CN IDE（灵码内核）" }

// HooksTarget 展示路径返回 settings.json；Windows 另在同目录维护 ok-hook-*.cmd
// 包装文件（settings.json command 指向它们，见 lingmaCommand）。
func (qoderIdeAgent) HooksTarget() string { return lingmaSettingsPath() }
func (qoderIdeAgent) SkillsDir() string   { return filepath.Join(LingmaHome(), "skills") }

func (qoderIdeAgent) Detect() bool {
	info, err := os.Stat(LingmaHome())
	return err == nil && info.IsDir()
}

// lingmaOKGroup 生成一个事件的 ok hook 组：type=command shell 串，
// timeout 秒级 = 全局 HookTimeoutSec()（IDE 默认 30 秒，写入值按 ok 全局配置）。
func lingmaOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": lingmaCommand(exe, okHook),
		"timeout": HookTimeoutSec(),
	}
	return map[string]any{"matcher": matcher, "hooks": []any{hook}}
}

// loadLingmaSettings 读 settings.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadLingmaSettings() (map[string]any, error) {
	data, err := os.ReadFile(lingmaSettingsPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("lingma settings.json 解析失败: %w", err)
	}
	return cfg, nil
}

// lingmaEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func lingmaEventsOf(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	return events
}

// lingmaEventsEdit 取 hooks 事件表供写入：缺失时创建。
func lingmaEventsEdit(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		cfg["hooks"] = events
	}
	return events
}

// stripOKQoderIdeHooks 移除事件表里所有 ok 自有 hook（组内 hooks 被删空时整组移除，
// 事件数组空了删事件键），返回是否有改动。第三方条目原样保留。
func stripOKQoderIdeHooks(events map[string]any) bool {
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
				if hm, _ := h.(map[string]any); hm != nil && isOKQoderIdeHook(hm) {
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

// hasOKQoderIdeHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKQoderIdeHook(events map[string]any) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKQoderIdeHook(hm) {
					return true
				}
			}
		}
	}
	return false
}

// lingmaHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态（command=当前命令、
// matcher 与 timeout 正确）；Windows 另要求包装文件内容为当前 exe。
func lingmaHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range lingmaHookEvents {
		if runtime.GOOS == "windows" {
			data, err := os.ReadFile(lingmaWrapperPath(e.okHook))
			if err != nil || string(data) != lingmaWrapperContent(exe, e.okHook) {
				return false
			}
		}
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
				if hm == nil || !isOKQoderIdeHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeout"].(float64)
				if cmd == lingmaCommand(exe, e.okHook) && timeout == wantTimeout {
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

// writeLingmaSettings 备份后写回 settings.json（MarshalIndent，未知字段保留）。
func writeLingmaSettings(cfg map[string]any) error {
	path := lingmaSettingsPath()
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
	return fsx.WriteFile(path, append(data, '\n'), 0o644)
}

func (qoderIdeAgent) InstallHooks(exe string) error {
	if runtime.GOOS == "windows" {
		// 先写 3 个包装文件（当前 exe）——settings.json command 指向它们。
		if err := ensureLingmaWrappers(exe); err != nil {
			return err
		}
	}
	cfg, err := loadLingmaSettings()
	if err != nil {
		return err
	}
	events := lingmaEventsEdit(cfg)
	stripOKQoderIdeHooks(events)
	for _, e := range lingmaHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, lingmaOKGroup(exe, e.matcher, e.okHook))
	}
	return writeLingmaSettings(cfg)
}

// RemoveHooks 移除 settings.json 里 ok 的 hooks 条目；Windows 另删 3 个包装文件
// （仅删内容确为 ok 生成的，防误删用户同名文件）。
func (qoderIdeAgent) RemoveHooks() (bool, error) {
	wrappersRemoved := false
	if runtime.GOOS == "windows" {
		wrappersRemoved = removeLingmaWrappers()
	}
	if _, err := os.Stat(lingmaSettingsPath()); os.IsNotExist(err) {
		return wrappersRemoved, nil
	}
	cfg, err := loadLingmaSettings()
	if err != nil {
		return false, err
	}
	events := lingmaEventsOf(cfg)
	if events == nil || !hasOKQoderIdeHook(events) {
		return wrappersRemoved, nil
	}
	stripOKQoderIdeHooks(events)
	if err := writeLingmaSettings(cfg); err != nil {
		return false, fmt.Errorf("移除 qoder ide hooks: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：settings 存在、曾安装过 ok hooks 且内容过期（exe 迁移、超时
// 变更）时重写；从未安装（无任何 ok 条目）则 no-op——用户显式移除的集成不复活。
// Windows 先重写过期/缺失的包装文件（exe 迁移只动包装内容）。
func (qoderIdeAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(lingmaSettingsPath()); err != nil {
		return nil
	}
	cfg, err := loadLingmaSettings()
	if err != nil {
		return err
	}
	events := lingmaEventsOf(cfg)
	if events == nil || !hasOKQoderIdeHook(events) {
		return nil // 从未安装（无任何 ok 条目）不复活——含用户显式移除
	}
	if runtime.GOOS == "windows" {
		if err := ensureLingmaWrappers(exe); err != nil {
			return err
		}
	}
	if lingmaHooksCurrent(events, exe) {
		return nil
	}
	events = lingmaEventsEdit(cfg)
	stripOKQoderIdeHooks(events)
	for _, e := range lingmaHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, lingmaOKGroup(exe, e.matcher, e.okHook))
	}
	return writeLingmaSettings(cfg)
}

func (qoderIdeAgent) HooksInstalled() bool {
	cfg, err := loadLingmaSettings()
	if err != nil {
		return false
	}
	events := lingmaEventsOf(cfg)
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
	return lingmaHooksCurrent(events, exe)
}
