package agentx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
// 命令形态按平台分叉（见 codexCommand）：Windows 用 .cmd 包装文件裸路径，其他平台
// 与 claude 适配器相同（shell 字符串：正斜杠 exe + 双引号）；输出协议 Claude JSON
// （args 末尾 "claude"）——Codex hook 契约逐字兼容 Claude Code
// （hookSpecificOutput.additionalContext 注入、Stop decision:block 阻断），
// hook.go 输出层零改动。PostToolUse 只追 apply_patch（Codex 专用写盘工具，
// 无 Write/Edit），不追 Bash——与 claude 不追 Bash 对齐。
var codexHookEvents = []struct {
	event   string // Codex 事件名（逐字沿用 Claude Code 命名）
	label   string // snake 事件名（信任哈希 identity 与 hooks.state 节 key 用）
	matcher string // 组级 matcher
	okHook  string // ok hook 子命令
	filters bool   // matcher 是否入信任哈希（Codex 仅过滤型事件入：PreToolUse/PostToolUse）
}{
	{"UserPromptSubmit", "user_prompt_submit", "*", "prompt", false},
	{"PostToolUse", "post_tool_use", "apply_patch", "post-tool", true},
	{"Stop", "stop", "*", "stop", false},
}

// codexCommand 生成 hook 命令串，按平台分叉：
//   - Windows：Codex 执行 hooks.json 的 command 时整行外套双引号
//     cmd.exe /C "<整行>"（codex-rs/hooks/src/engine/command_runner.rs），含内嵌
//     引号的命令静默不执行却报 Completed（上游 issue #38168，Open 未修）——故
//     command 用 .cmd 包装文件绝对路径裸串（无引号、反斜杠形态），与 exe 位置
//     解耦：exe 迁移只改包装文件内容，hooks.json 不变，信任哈希永不因此过期。
//   - 其他平台（linux/darwin）：claudeCommand 同款 quoted shell 串
//     （上游 bug 仅 Windows cmd 路径）。
//
// 已知限制：用户名含空格时包装路径带空格，hooks.json 裸串命令仍会被 #38168 外层
// 引号 bug 截断——上游修复前不额外处理。
func codexCommand(exe, okHook string) string {
	if runtime.GOOS == "windows" {
		return codexWrapperPath(okHook)
	}
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// codexWrapperPath 返回 Windows 包装文件路径：<CodexHome>/ok-hook-<okHook>.cmd
// （filepath.Join 在 Windows 出反斜杠形态——hooks.json command 裸串即它）。
func codexWrapperPath(okHook string) string {
	return filepath.Join(CodexHome(), "ok-hook-"+okHook+".cmd")
}

// codexWrapperContent 返回包装文件内容：单行 @"<exe>" hook <okHook> claude（CRLF
// 结尾）。exe 路径在 .cmd 文件内部带引号无妨——#38168 外层引号 bug 只作用于
// hooks.json 的命令行。
func codexWrapperContent(exe, okHook string) string {
	return "@\"" + exe + "\" hook " + okHook + " claude\r\n"
}

// ensureCodexWrappers 确保三个包装文件存在且内容为当前 exe（缺失/过期重写，
// 已当前则不写盘）。仅 Windows 调用。
func ensureCodexWrappers(exe string) error {
	for _, e := range codexHookEvents {
		path := codexWrapperPath(e.okHook)
		want := codexWrapperContent(exe, e.okHook)
		if data, err := os.ReadFile(path); err == nil && string(data) == want {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("写入 codex hook 包装: %w", err)
		}
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			return fmt.Errorf("写入 codex hook 包装: %w", err)
		}
	}
	return nil
}

// removeCodexWrappers 删除三个包装文件，返回是否有删除。仅删内容确为 ok 生成的
// （含 " hook <okHook> claude"）——防误删用户同名文件。仅 Windows 调用。
func removeCodexWrappers() bool {
	removed := false
	for _, e := range codexHookEvents {
		path := codexWrapperPath(e.okHook)
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

// isOKCodexHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串形态匹配——
// Windows 认包装文件裸路径（trim 后以 / 或 \ 分隔的 ok-hook-<okHook>.cmd 结尾，
// 大小写不敏感，Equalfold 语意——cmd.exe 路径语义）；其他平台认后缀
// " hook <prompt|post-tool|stop> claude"。不看 exe basename——改名/迁移/测试
// 二进制都不影响识别。
func isOKCodexHook(h map[string]any) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range codexHookEvents {
		if runtime.GOOS == "windows" {
			for _, sep := range []string{"/", "\\"} {
				if hasSuffixFold(cmd, sep+"ok-hook-"+e.okHook+".cmd") {
					return true
				}
			}
			continue
		}
		if strings.HasSuffix(cmd, " hook "+e.okHook+" claude") {
			return true
		}
	}
	return false
}

// hasSuffixFold 报告 s 是否以 suffix 结尾（大小写不敏感，Equalfold 语意）。
func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

// codexAgent Codex 适配器：hook 集成 = 合并写用户层 ~/.codex/hooks.json 并确保
// config.toml [features] codex_hooks = true（0.118 起 hooks 是 under-development
// 特性、默认关闭，不开则 hooks.json 装好也静默不派发）；技能目录共享 SkillsHome——
// Codex 原生扫描 USER 作用域 ~/.agents/skills（opencode 同款零适配）。
type codexAgent struct{}

func init() { Register(codexAgent{}) }

func (codexAgent) ID() string          { return "codex" }
func (codexAgent) DisplayName() string { return "Codex" }

// HooksTarget 展示路径返回 hooks.json；Windows 另在同目录维护 ok-hook-*.cmd
// 包装文件（hooks.json command 指向它们，见 codexCommand）。
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
// （command=当前命令、matcher 与 timeout 正确）；Windows 另要求包装文件内容为
// 当前 exe（exe 迁移后包装过期 = 集成失效，需自愈重写）。
func codexHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range codexHookEvents {
		if runtime.GOOS == "windows" {
			data, err := os.ReadFile(codexWrapperPath(e.okHook))
			if err != nil || string(data) != codexWrapperContent(exe, e.okHook) {
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

// isSectionHeader 判定 trim 后的行是否指定段头：以 header 开头，且其后剩余为空
// 或以 "#" 开头（允许尾注释——"[features] # 我的特性" 漏判会导致 enable 时文末
// 追加重复 [features] 表，TOML 硬错误）；"[features.xxx]" 子表前缀是 "[features."
// 而非 "[features]"，前缀规则天然排除。
func isSectionHeader(trimmed, header string) bool {
	if !strings.HasPrefix(trimmed, header) {
		return false
	}
	rest := strings.TrimSpace(trimmed[len(header):])
	return rest == "" || strings.HasPrefix(rest, "#")
}

// isFeaturesHeader 判定 trim 后的行是否 [features] 段头。
func isFeaturesHeader(trimmed string) bool {
	return isSectionHeader(trimmed, "[features]")
}

// tomlKeyValue 判定 trim 后的行是否指定键行：键名紧跟 [ \t]*=（词边界——
// "codex_hooks_extra = ..." 不命中 codex_hooks）。命中返回 "=" 后 trim 过的值文本。
func tomlKeyValue(trimmed, key string) (string, bool) {
	if !strings.HasPrefix(trimmed, key) {
		return "", false
	}
	rest := strings.TrimLeft(trimmed[len(key):], " \t")
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	return strings.TrimSpace(rest[1:]), true
}

// codexHooksKeyValue 判定 trim 后的行是否 codex_hooks 键行。
func codexHooksKeyValue(trimmed string) (string, bool) {
	return tomlKeyValue(trimmed, "codex_hooks")
}

// findTomlSection 定位 lines 中 header 节（isSectionHeader 判定，容忍尾注释），
// 返回节头行号与节末行号（下一节头行号；无则 len(lines)）。未找到返回 (-1, -1)。
func findTomlSection(lines []string, header string) (int, int) {
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") {
			continue
		}
		if start < 0 {
			if isSectionHeader(trimmed, header) {
				start = i
			}
			continue
		}
		return start, i
	}
	if start < 0 {
		return -1, -1
	}
	return start, len(lines)
}

// codexHooksFlagOn 报告 config.toml 文本的 [features] 段是否含 codex_hooks = true。
// 行级解析（不做 TOML 全量往返——其余内容逐字节保留）；只认布尔值 true；
// 注释行（# 开头）内的同名键不算。
func codexHooksFlagOn(text string) bool {
	inFeatures := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case isFeaturesHeader(trimmed):
			inFeatures = true
		case strings.HasPrefix(trimmed, "["):
			inFeatures = false // 下一个段标题——features 段结束
		case inFeatures && !strings.HasPrefix(trimmed, "#"):
			if val, ok := codexHooksKeyValue(trimmed); ok {
				return val == "true"
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
		if isFeaturesHeader(trimmed) {
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
		if inFeatures && !strings.HasPrefix(trimmed, "#") {
			if val, ok := codexHooksKeyValue(trimmed); ok {
				if val == "true" {
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

// ensureCodexHooksConfig 单次读写应用两组行级手术：codex_hooks 特性开关（0.118 起
// hooks 为 under-development、默认关闭，不开则装好也静默不派发）+ ok 信任记录
// （hooks.state trusted_hash/enabled）。合并为一次读、一次备份、一次写——同一安装/
// 自愈若拆两次写盘，第二次的备份会覆盖第一次的 .bak-openknowledge、丢掉操作前原文。
// 行级手术编辑，其余内容逐字节保留；两组都无改动则不写盘。
func ensureCodexHooksConfig(entries [][2]string) error {
	data, err := os.ReadFile(codexConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("写入 codex hooks 配置: %w", err)
	}
	text := string(data)
	text, c1 := codexEnableHooksFlag(text)
	text, c2 := codexTrustEdits(text, entries)
	if !c1 && !c2 {
		return nil
	}
	return writeCodexConfig(data, text, "写入 codex hooks 配置")
}

// writeCodexConfig 行级手术写盘纪律（ensureCodexHooksConfig / removeCodexTrust 共
// 用）：原文非空时先写 .bak-openknowledge 备份，再整文件写回。
func writeCodexConfig(orig []byte, text, op string) error {
	path := codexConfigPath()
	if len(orig) > 0 {
		_ = os.WriteFile(path+".bak-openknowledge", orig, 0o644)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// ---------- hooks.state 信任记录（Codex 信任门）----------
//
// Codex 对每条 hooks.json hook 计算内容哈希并与 config.toml
// [hooks.state.'<hooks.json路径>:<label>:<组索引>:<hook索引>'] 节的 trusted_hash
// 比对：不一致（Modified）或无记录（Untrusted）时静默跳过全部 hooks、无任何提示。
// hooks.json 任何内容变化（重装/自愈重写）都会使信任过期——因此 ok 在
// 安装/自愈写 hooks.json 后必须同步重算并写入信任记录。
// （Windows 上 exe 迁移不再触发：command=包装路径与 exe 解耦，只改包装文件内容，
// 哈希输入不变——见 codexCommand。）
// 哈希公式经 Codex 源码（hooks/src/engine/discovery.rs hook_hash →
// config/src/fingerprint.rs version_for_toml）与真实信任记录逐位双向验证：
// trusted_hash = "sha256:" + sha256hex(canonicalJSON(identity))，
// identity = {"event_name":label,"hooks":[{async,command,timeout,type}]}
// （matcher 仅过滤型事件 PostToolUse 入哈希，UserPromptSubmit/Stop 不入）。

// codexTrustHash 按 Codex 公式计算一条 hook 的信任哈希。canonicalJSON =
// sorted-keys compact JSON：Go map 的 encoding/json 默认即 sorted+compact，
// 但须关 HTML 转义（serde_json 不转义 <>&），非 ASCII 直出 UTF-8 两侧一致。
func codexTrustHash(label string, filters bool, matcher, command string, timeoutSec int) string {
	hook := map[string]any{
		"async":   false,
		"command": command,
		"timeout": timeoutSec,
		"type":    "command",
	}
	identity := map[string]any{
		"event_name": label,
		"hooks":      []any{hook},
	}
	if filters {
		identity["matcher"] = matcher
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(identity) // map[string]any 序列化不会失败
	sum := sha256.Sum256([]byte(strings.TrimSuffix(buf.String(), "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// codexTrustHeader 返回信任记录节头文本（key 含 Windows 反斜杠路径，单引号
// literal 原样落盘）。
func codexTrustHeader(key string) string { return "[hooks.state.'" + key + "']" }

// codexTrustKeys 返回事件表里 ok 组对应的 hooks.state 节 key：ok 组在该事件数组中
// 的实际下标（安装语义"先剥离再追加"⇒ ok 组恒为最后一组，第三方组存在时索引顺移），
// hook 索引恒 0。一个事件存在多个 ok 组时逐组各出一 key（防御——正常安装恒单组）。
func codexTrustKeys(events map[string]any) []string {
	var keys []string
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		for i, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKCodexHook(hm) {
					keys = append(keys, fmt.Sprintf("%s:%s:%d:0", codexHooksPath(), e.label, i))
					break
				}
			}
		}
	}
	return keys
}

// codexTrustEntries 按 hooks.json 当前内容计算 ok 三条信任记录：节 key → trusted_hash。
// command/timeout 以 codexOKGroup 实际写入值为准（codexCommand(exe, okHook) 与
// HookTimeoutSec()）；某事件无 ok 组（未安装）时该事件跳过。
func codexTrustEntries(events map[string]any, exe string) [][2]string {
	var entries [][2]string
	for _, e := range codexHookEvents {
		groups, _ := events[e.event].([]any)
		idx := -1
		for i, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOKCodexHook(hm) {
					idx = i
				}
			}
		}
		if idx < 0 {
			continue
		}
		key := fmt.Sprintf("%s:%s:%d:0", codexHooksPath(), e.label, idx)
		entries = append(entries, [2]string{
			key,
			codexTrustHash(e.label, e.filters, e.matcher, codexCommand(exe, e.okHook), HookTimeoutSec()),
		})
	}
	return entries
}

// codexTrustEdits 行级手术写入信任记录：节存在→替换 trusted_hash 行、enabled = true
// 缺失则补（紧跟 trusted_hash 行后，enabled = false 视同过期纠正为 true）；节不存在→
// 文末追加三行块（节头+两行）。节内其他行与第三方 hooks.state 节逐字节保留。
// 全部已是最新返回 (原文, false)。
func codexTrustEdits(text string, entries [][2]string) (string, bool) {
	changed := false
	for _, e := range entries {
		var c bool
		text, c = upsertCodexTrustSection(text, e[0], e[1])
		changed = changed || c
	}
	return text, changed
}

// upsertCodexTrustSection 确保 text 含 key 的信任节且内容最新，返回新文本与是否改动。
func upsertCodexTrustSection(text, key, hash string) (string, bool) {
	header := codexTrustHeader(key)
	hashLine := `trusted_hash = "` + hash + `"`
	lines := strings.Split(text, "\n")
	secStart, secEnd := findTomlSection(lines, header)
	if secStart < 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return text + header + "\n" + hashLine + "\nenabled = true\n", true
	}
	// 节内定位 trusted_hash / enabled 键行（注释行跳过）。
	hashIdx, enabledIdx := -1, -1
	for i := secStart + 1; i < secEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if _, ok := tomlKeyValue(trimmed, "trusted_hash"); ok && hashIdx < 0 {
			hashIdx = i
		}
		if _, ok := tomlKeyValue(trimmed, "enabled"); ok && enabledIdx < 0 {
			enabledIdx = i
		}
	}
	changed := false
	if hashIdx >= 0 {
		if lines[hashIdx] != hashLine {
			lines[hashIdx] = hashLine
			changed = true
		}
	} else {
		lines = append(lines, "")
		copy(lines[secStart+2:], lines[secStart+1:])
		lines[secStart+1] = hashLine
		if enabledIdx > secStart {
			enabledIdx++
		}
		hashIdx = secStart + 1
		changed = true
	}
	if enabledIdx >= 0 {
		if lines[enabledIdx] != "enabled = true" {
			lines[enabledIdx] = "enabled = true"
			changed = true
		}
	} else {
		lines = append(lines, "")
		copy(lines[hashIdx+2:], lines[hashIdx+1:])
		lines[hashIdx+1] = "enabled = true"
		changed = true
	}
	return strings.Join(lines, "\n"), changed
}

// codexTrustConsistent 报告 text 中 entries 各节的 trusted_hash 与 enabled 是否与
// 期望一致：任一节缺失、哈希不符或 enabled 非 true → false（对应 Codex 侧
// Modified/Untrusted/禁用——hooks 静默不派发）。
func codexTrustConsistent(text string, entries [][2]string) bool {
	if len(entries) == 0 {
		return false
	}
	lines := strings.Split(text, "\n")
	for _, e := range entries {
		secStart, secEnd := findTomlSection(lines, codexTrustHeader(e[0]))
		if secStart < 0 {
			return false
		}
		hashOK, enabledOK := false, false
		for i := secStart + 1; i < secEnd; i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if val, ok := tomlKeyValue(trimmed, "trusted_hash"); ok && val == `"`+e[1]+`"` {
				hashOK = true
			}
			if val, ok := tomlKeyValue(trimmed, "enabled"); ok && val == "true" {
				enabledOK = true
			}
		}
		if !hashOK || !enabledOK {
			return false
		}
	}
	return true
}

// codexTrustRemoveEdits 整节移除 keys 对应的 hooks.state 节（节头到下一节头/文末），
// 第三方节与其余内容逐字节保留。无匹配节返回 (原文, false)。
func codexTrustRemoveEdits(text string, keys []string) (string, bool) {
	if len(keys) == 0 {
		return text, false
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	dropping := false
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			dropping = false
			for _, k := range keys {
				if isSectionHeader(trimmed, codexTrustHeader(k)) {
					dropping = true
					changed = true
					break
				}
			}
		}
		if !dropping {
			kept = append(kept, line)
		}
	}
	if !changed {
		return text, false
	}
	return strings.Join(kept, "\n"), true
}

// removeCodexTrust 整节移除 ok 的 hooks.state 信任节（卸载用）；无匹配节不写盘。
func removeCodexTrust(keys []string) error {
	data, err := os.ReadFile(codexConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("移除 codex 信任记录: %w", err)
	}
	text, changed := codexTrustRemoveEdits(string(data), keys)
	if !changed {
		return nil
	}
	return writeCodexConfig(data, text, "移除 codex 信任记录")
}

func (codexAgent) InstallHooks(exe string) error {
	cfg, err := loadCodexHooks()
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// 先写 3 个包装文件（当前 exe）——hooks.json command 指向它们。
		if err := ensureCodexWrappers(exe); err != nil {
			return err
		}
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
	// 同步落 config.toml 两项集成保障（单次读写）：codex_hooks 特性开关（0.118 起
	// 默认关闭，不开则装好也静默不派发）+ hooks.state 信任记录（hooks.json 内容
	// 变化 → 信任哈希过期 → Codex 静默跳过全部 hooks）。
	return ensureCodexHooksConfig(codexTrustEntries(events, exe))
}

// RemoveHooks 移除 hooks.json 里的 ok 条目并连带清理 config.toml 里 ok 的
// hooks.state 信任节（第三方节保留）；Windows 另删 3 个包装文件（仅删内容确为
// ok 生成的，防误删用户同名文件）；config.toml 的 codex_hooks 特性开关单独
// 存在无副作用（只是允许 Codex 派发 hooks），不随移除关闭。
func (codexAgent) RemoveHooks() (bool, error) {
	wrappersRemoved := false
	if runtime.GOOS == "windows" {
		wrappersRemoved = removeCodexWrappers()
	}
	if _, err := os.Stat(codexHooksPath()); os.IsNotExist(err) {
		return wrappersRemoved, nil
	}
	cfg, err := loadCodexHooks()
	if err != nil {
		return false, err
	}
	events := codexEventsOf(cfg)
	if events == nil || !hasOKCodexHook(events) {
		return wrappersRemoved, nil
	}
	trustKeys := codexTrustKeys(events) // 剥离前取 ok 组实际索引
	stripOKCodexHooks(events)
	if err := writeCodexHooks(cfg); err != nil {
		return false, fmt.Errorf("移除 codex hooks: %w", err)
	}
	if err := removeCodexTrust(trustKeys); err != nil {
		return false, err
	}
	return true, nil
}

// EnsureHooks 自愈：hooks.json 存在、曾安装过 ok hooks 且内容过期（exe 迁移、
// 超时变更）时重写；从未安装（无任何 ok 条目）则 no-op——用户显式移除的集成
// 不复活。codex_hooks 特性开关被关/缺失、hooks.state 信任记录过期/缺失同样视为
// 过期（曾安装过才走到这）：按当前 hooks.json 内容重算补写。
// Windows 先重写过期/缺失的包装文件（exe 迁移只动包装内容）——包装刷新后
// hooks.json 命令（包装路径）往往仍当前，无需重写，信任哈希随之不变。
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
	if runtime.GOOS == "windows" {
		if err := ensureCodexWrappers(exe); err != nil {
			return err
		}
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
	// codex_hooks 特性开关被关/缺失、hooks.state 信任记录过期/缺失均视为过期形态
	// （曾安装过才走到这）：无论 hooks.json 本轮是否重写，都按当前内容重算补写
	// （hooks.json 内容任何变化 → 信任哈希过期 → Codex 静默跳过全部 hooks）。
	return ensureCodexHooksConfig(codexTrustEntries(events, exe))
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
	// 信任记录过期/缺失 = Codex 侧 Modified/Untrusted 静默跳过全部 hooks，视为未安装。
	return codexTrustConsistent(string(data), codexTrustEntries(events, exe))
}
