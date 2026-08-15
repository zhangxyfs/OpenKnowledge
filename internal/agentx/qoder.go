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

// QoderHome 返回 Qoder CN CLI 配置根目录：OK_QODER_HOME（ok 自留测试隔离口，
// OK_CLAUDE_HOME 同款命名）> QODERCN_CONFIG_DIR（Qoder CN CLI 官方重定位环境变量，
// 文档化）> ~/.qoder-cn（终端 CLI 默认用户配置目录）。注意：QoderCN IDE 的 hooks
// 走独立的 ~/.lingma/settings.json（灵码内核，仅 5 事件、Stop 不可阻断），不读本
// 目录——本适配器只覆盖终端 CLI 面。
// bundle 源码（@qodercn-ai/qoderclicn 1.1.20）另有 QODERCN_CLI_HOME / GEMINI_CLI_HOME
// 参与解析（CLI_HOME 作为 ~/.qoder-cn 的父目录）——非文档化且语义嵌套，不接入。
func QoderHome() string {
	if h := os.Getenv("OK_QODER_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("QODERCN_CONFIG_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qoder-cn")
}

func qoderSettingsPath() string { return filepath.Join(QoderHome(), "settings.json") }

// qoderHookEvents 是 ok 接入的 Qoder hook 事件（对应 ok 的三条 hook 链路）。
// Qoder 的 hooks 契约逐字兼容 Claude Code：settings.json 的 hooks 分组、command 类型、
// stdin JSON（hook_event_name/session_id/cwd/tool_name/tool_input）、退出码 0/2、
// stdout JSON（decision/reason/hookSpecificOutput.additionalContext）——输出协议
// Claude JSON（args 末尾 "claude"），hook.go 输出层零改动。PostToolUse 追
// Write|Edit（Qoder 与 Claude 同款写盘工具），不追 Bash——与 claude 对齐。
var qoderHookEvents = []struct {
	event   string // Qoder 事件名（逐字沿用 Claude Code 命名）
	matcher string // 组级 matcher（空或 * 匹配全部，| 多值）
	okHook  string // ok hook 子命令
}{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "*", "stop"},
}

// qoderCommand 生成 hook 命令串，按平台分叉：
//   - Windows：Qoder 执行 command 型 hook 时走 cmd.exe /d /s /c "<整行>"
//     （bundle 源码 hooks 派发层实证：cmd /s 会把命令行首尾引号剥掉，带内嵌引号的
//     quoted 命令串会被剥坏静默不执行——codex #38168 同源问题），故 command 用
//     .cmd 包装文件绝对路径裸串（无引号、反斜杠形态），与 exe 位置解耦：exe 迁移
//     只改包装文件内容，settings.json 不变，无需信任/哈希刷新。
//   - 其他平台（linux/darwin）：claudeCommand 同款 quoted shell 串（Qoder 走
//     sh -lc，quoted 形态无碍）。
//
// 已知限制：用户名含空格时包装路径带空格，cmd /s 仍会截断——上游修复前不额外处理。
func qoderCommand(exe, okHook string) string {
	if runtime.GOOS == "windows" {
		return qoderWrapperPath(okHook)
	}
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// qoderWrapperPath 返回 Windows 包装文件路径：<QoderHome>/ok-hook-<okHook>.cmd
// （filepath.Join 在 Windows 出反斜杠形态——settings.json command 裸串即它）。
func qoderWrapperPath(okHook string) string {
	return filepath.Join(QoderHome(), "ok-hook-"+okHook+".cmd")
}

// qoderWrapperContent 返回包装文件内容：单行 @"<exe>" hook <okHook> claude（CRLF
// 结尾）。exe 路径在 .cmd 文件内部带引号无妨——cmd /s 剥引号只作用于 settings.json
// 的命令行。
func qoderWrapperContent(exe, okHook string) string {
	return "@\"" + exe + "\" hook " + okHook + " claude\r\n"
}

// ensureQoderWrappers 确保三个包装文件存在且内容为当前 exe（缺失/过期重写，
// 已当前则不写盘）。仅 Windows 调用。
func ensureQoderWrappers(exe string) error {
	for _, e := range qoderHookEvents {
		path := qoderWrapperPath(e.okHook)
		want := qoderWrapperContent(exe, e.okHook)
		if data, err := os.ReadFile(path); err == nil && string(data) == want {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("写入 qoder hook 包装: %w", err)
		}
		if err := fsx.WriteFile(path, []byte(want), 0o644); err != nil {
			return fmt.Errorf("写入 qoder hook 包装: %w", err)
		}
	}
	return nil
}

// removeQoderWrappers 删除三个包装文件，返回是否有删除。仅删内容确为 ok 生成的
// （含 " hook <okHook> claude"）——防误删用户同名文件。仅 Windows 调用。
func removeQoderWrappers() bool {
	removed := false
	for _, e := range qoderHookEvents {
		path := qoderWrapperPath(e.okHook)
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

// isOKQoderHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串形态匹配——
// Windows 认包装文件裸路径（trim 后以 / 或 \ 分隔的 ok-hook-<okHook>.cmd 结尾，
// 大小写不敏感，hasSuffixFold 语意）与 quoted 后缀 " hook <prompt|post-tool|stop>
// claude" 两种形态；其他平台只认 quoted 后缀。不看 exe basename——改名/迁移/测试
// 二进制都不影响识别。
func isOKQoderHook(h map[string]any) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range qoderHookEvents {
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

// qoderAgent Qoder CN CLI 适配器：hook 集成 = 合并写 ~/.qoder-cn/settings.json 的
// hooks 分组 + 顶层 hooksConfig.enabled 开关（零成本开关：Qoder 默认关闭 hooks
// 派发——见 qoderEnableHooksConfig）；技能目录 ~/.qoder-cn/skills（bundle 源码
// getUserSkillsDir = join(配置目录, "skills")，SKILL.md 格式与 Claude 逐字一致，
// 现有共享模板零适配）。
type qoderAgent struct{}

func init() { Register(qoderAgent{}) }

func (qoderAgent) ID() string          { return "qoder" }
func (qoderAgent) DisplayName() string { return "Qoder CN CLI" }

// HooksTarget 展示路径返回 settings.json；Windows 另在同目录维护 ok-hook-*.cmd
// 包装文件（settings.json command 指向它们，见 qoderCommand）。
func (qoderAgent) HooksTarget() string { return qoderSettingsPath() }
func (qoderAgent) SkillsDir() string   { return filepath.Join(QoderHome(), "skills") }

func (qoderAgent) Detect() bool {
	info, err := os.Stat(QoderHome())
	return err == nil && info.IsDir()
}

// qoderOKGroup 生成一个事件的 ok hook 组：type=command shell 串，
// timeout 秒级 = 全局 HookTimeoutSec()。
func qoderOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": qoderCommand(exe, okHook),
		"timeout": HookTimeoutSec(),
	}
	return map[string]any{"matcher": matcher, "hooks": []any{hook}}
}

// loadQoderSettings 读 settings.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadQoderSettings() (map[string]any, error) {
	data, err := os.ReadFile(qoderSettingsPath())
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("qoder settings.json 解析失败: %w", err)
	}
	return cfg, nil
}

// qoderEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func qoderEventsOf(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	return events
}

// qoderEventsEdit 取 hooks 事件表供写入：缺失时创建。
func qoderEventsEdit(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		cfg["hooks"] = events
	}
	return events
}

// stripOKQoderHooks 移除事件表里所有 ok 自有 hook（组内 hooks 被删空时整组移除，
// 事件数组空了删事件键），返回是否有改动。第三方条目原样保留。
func stripOKQoderHooks(events map[string]any) bool {
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
				if hm, _ := h.(map[string]any); hm != nil && isOKQoderHook(hm) {
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

// hasOKQoderHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKQoderHook(events map[string]any) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKQoderHook(hm) {
					return true
				}
			}
		}
	}
	return false
}

// qoderHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态（command=当前命令、
// matcher 与 timeout 正确）；Windows 另要求包装文件内容为当前 exe（exe 迁移后包装
// 过期 = 集成失效，需自愈重写）。
func qoderHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range qoderHookEvents {
		if runtime.GOOS == "windows" {
			data, err := os.ReadFile(qoderWrapperPath(e.okHook))
			if err != nil || string(data) != qoderWrapperContent(exe, e.okHook) {
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
				if hm == nil || !isOKQoderHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeout"].(float64)
				if cmd == qoderCommand(exe, e.okHook) && timeout == wantTimeout {
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

// qoderHooksConfigEnabled 报告 settings 顶层 hooksConfig 的 enabled 是否为真。
// Qoder 对 hooksConfig.enabled 未显式设置时默认关闭——hooks 装好也静默不派发
// （bundle 源码实证：enableHooks = !disableAllHooks && hooksConfig.enabled；
// settings schema 默认 hooksConfig = {} → enabled 未定义 → false，与 codex 的
// codex_hooks 特性开关同款教训）。disableAllHooks 是用户全局 kill switch，
// ok 不读取不修改——只认 hooksConfig。
func qoderHooksConfigEnabled(cfg map[string]any) bool {
	hc, _ := cfg["hooksConfig"].(map[string]any)
	if hc == nil {
		return false
	}
	enabled, _ := hc["enabled"].(bool)
	return enabled
}

// qoderEnableHooksConfig 在 settings 顶层写入/合并 hooksConfig 并把 enabled 置 true，
// 保留 hooksConfig 其余键（如 notifications），返回是否有改动。
func qoderEnableHooksConfig(cfg map[string]any) bool {
	hc, _ := cfg["hooksConfig"].(map[string]any)
	if hc == nil {
		cfg["hooksConfig"] = map[string]any{"enabled": true}
		return true
	}
	enabled, _ := hc["enabled"].(bool)
	if enabled {
		return false
	}
	hc["enabled"] = true
	return true
}

// writeQoderSettings 备份后写回 settings.json（MarshalIndent，未知字段保留）。
func writeQoderSettings(cfg map[string]any) error {
	path := qoderSettingsPath()
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

func (qoderAgent) InstallHooks(exe string) error {
	if runtime.GOOS == "windows" {
		// 先写 3 个包装文件（当前 exe）——settings.json command 指向它们。
		if err := ensureQoderWrappers(exe); err != nil {
			return err
		}
	}
	cfg, err := loadQoderSettings()
	if err != nil {
		return err
	}
	events := qoderEventsEdit(cfg)
	stripOKQoderHooks(events)
	for _, e := range qoderHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, qoderOKGroup(exe, e.matcher, e.okHook))
	}
	// hooksConfig.enabled 默认关闭（装了也静默不派发）——安装时一并开启。
	qoderEnableHooksConfig(cfg)
	return writeQoderSettings(cfg)
}

// RemoveHooks 移除 settings.json 里 ok 的 hooks 条目；Windows 另删 3 个包装文件
// （仅删内容确为 ok 生成的，防误删用户同名文件）。hooksConfig.enabled 开关单独
// 存在无副作用（只是允许 Qoder 派发 hooks；关掉会连带停掉用户的第三方 hooks），
// 不随移除关闭。
func (qoderAgent) RemoveHooks() (bool, error) {
	wrappersRemoved := false
	if runtime.GOOS == "windows" {
		wrappersRemoved = removeQoderWrappers()
	}
	if _, err := os.Stat(qoderSettingsPath()); os.IsNotExist(err) {
		return wrappersRemoved, nil
	}
	cfg, err := loadQoderSettings()
	if err != nil {
		return false, err
	}
	events := qoderEventsOf(cfg)
	if events == nil || !hasOKQoderHook(events) {
		return wrappersRemoved, nil
	}
	stripOKQoderHooks(events)
	if err := writeQoderSettings(cfg); err != nil {
		return false, fmt.Errorf("移除 qoder hooks: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：settings 存在、曾安装过 ok hooks 且内容过期（exe 迁移、超时
// 变更）或 hooksConfig.enabled 被关时重写/重开；从未安装（无任何 ok 条目）则
// no-op——用户显式移除的集成不复活。Windows 先重写过期/缺失的包装文件（exe 迁移
// 只动包装内容）——包装刷新后 settings.json 命令（包装路径）往往仍当前，无需重写。
func (qoderAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(qoderSettingsPath()); err != nil {
		return nil
	}
	cfg, err := loadQoderSettings()
	if err != nil {
		return err
	}
	events := qoderEventsOf(cfg)
	if events == nil || !hasOKQoderHook(events) {
		return nil // 从未安装（无任何 ok 条目）不复活——含用户显式移除
	}
	if runtime.GOOS == "windows" {
		if err := ensureQoderWrappers(exe); err != nil {
			return err
		}
	}
	if qoderHooksCurrent(events, exe) && qoderHooksConfigEnabled(cfg) {
		return nil
	}
	events = qoderEventsEdit(cfg)
	stripOKQoderHooks(events)
	for _, e := range qoderHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, qoderOKGroup(exe, e.matcher, e.okHook))
	}
	qoderEnableHooksConfig(cfg)
	return writeQoderSettings(cfg)
}

func (qoderAgent) HooksInstalled() bool {
	cfg, err := loadQoderSettings()
	if err != nil {
		return false
	}
	events := qoderEventsOf(cfg)
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
	// hooksConfig.enabled 关闭/缺失 = 集成失效（hooks 静默不派发），视为未安装。
	return qoderHooksCurrent(events, exe) && qoderHooksConfigEnabled(cfg)
}
