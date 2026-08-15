package agentx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/fsx"
	"openknowledge/internal/registry"
)

const MarkerBegin = "# >>> openknowledge hooks >>>"
const MarkerEnd = "# <<< openknowledge hooks <<<"

// KimiHome 返回 kimi-code 配置目录（KIMI_CODE_HOME 优先）。
func KimiHome() string {
	if h := os.Getenv("KIMI_CODE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code")
}

func kimiConfigPath() string { return filepath.Join(KimiHome(), "config.toml") }

// HooksBlockFor 生成指向 exe 的 hooks 配置块；三条 hook 统一使用 timeoutSec 秒超时
// （Windows 上 ok.exe 冷启动 + daemon 转发在高负载下可超过 5s，超时会被 kimi 静默杀死）。
// exe 必须加引号：路径含空格（如 C:/Users/John Doe/）时按空格分词会断裂，hook 永不执行。
func HooksBlockFor(exe string, timeoutSec int) string {
	exe = filepath.ToSlash(exe)
	return fmt.Sprintf(`[[hooks]]
event = "UserPromptSubmit"
command = "\"%s\" hook prompt"
timeout = %d

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "\"%s\" hook post-tool"
timeout = %d

[[hooks]]
event = "Stop"
command = "\"%s\" hook stop"
timeout = %d
`, exe, timeoutSec, exe, timeoutSec, exe, timeoutSec)
}

// HookTimeoutSec 返回写入 hooks 的超时秒数：全局配置 [hooks] timeout_sec，
// 读取失败或未配置时回退 10（与 config.Default 一致）。
func HookTimeoutSec() int {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil || cfg.Hooks.TimeoutSec <= 0 {
		return 10
	}
	return cfg.Hooks.TimeoutSec
}

// okHookCommand 匹配指向 ok hook 的 command 行（如 "ok hook prompt"、
// "\"D:/x/ok.exe\" hook stop"——exe 加引号后值内含转义引号，需一并兼容）。
var okHookCommand = regexp.MustCompile(`(?i)^\s*command\s*=\s*"(?:[^"]|\\")*\bok(?:\.exe)?(?:\\")?\s+hook\s`)

// StripLegacyOKHooks 移除配置中所有指向 ok hook 的无标记 [[hooks]] 表
// （历史遗留的手动粘贴块），其它工具的 hooks 原样保留。
// 注意：被删表连同其后直到下一个 section 头的所有行一起移除（TOML 表所有权范围），
// 调用方需保证不应触碰的区域（如标记块内部）不传入本函数。
func StripLegacyOKHooks(content string) string {
	lines := strings.Split(content, "\n")
	removed := make([]bool, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "[[hooks]]" {
			continue
		}
		j := i + 1
		isOK := false
		for ; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "[") {
				break
			}
			if okHookCommand.MatchString(lines[j]) {
				isOK = true
			}
		}
		if !isOK {
			continue
		}
		for k := i; k < j; k++ {
			removed[k] = true
		}
		// 连同删除紧随其后的空行（块间分隔），避免留下成串空行
		for k := j; k < len(lines) && strings.TrimSpace(lines[k]) == ""; k++ {
			removed[k] = true
		}
		// 连同删除紧邻其前的 OpenKnowledge 注释行（init 曾打印的引导注释）
		if i > 0 && !removed[i-1] {
			t := strings.TrimSpace(lines[i-1])
			if strings.HasPrefix(t, "#") && strings.Contains(strings.ToLower(t), "openknowledge") {
				removed[i-1] = true
			}
		}
	}
	var out []string
	for i, l := range lines {
		if !removed[i] {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// UpsertHooksBlock 以标记块幂等写入 hooks 配置：先清除存量 ok hooks（含无标记的
// 历史遗留块），已存在标记块则原位替换（exe 路径随之更新），否则追加。
func UpsertHooksBlock(configPath, block string) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(data)
	// 存量 ok hooks 只清理标记块之外的区域；块内内容交给原位替换/损坏报错逻辑。
	// 若对整个 content 调用 StripLegacyOKHooks，标记块自身的 ok hook 表会被删掉，
	// 连带着吃掉两个标记行，导致原位替换退化为尾部追加、损坏标记检测失效。
	if i := strings.Index(content, MarkerBegin); i >= 0 {
		if j := strings.Index(content, MarkerEnd); j > i {
			content = StripLegacyOKHooks(content[:i]) + content[i:j+len(MarkerEnd)] + StripLegacyOKHooks(content[j+len(MarkerEnd):])
		} else {
			// 有头无尾：保留原样，交给下面的损坏标记分支报错
			content = StripLegacyOKHooks(content[:i]) + content[i:]
		}
	} else {
		content = StripLegacyOKHooks(content)
	}
	wrapped := MarkerBegin + "\n" + block + MarkerEnd + "\n"
	i := strings.Index(content, MarkerBegin)
	j := strings.Index(content, MarkerEnd)
	var out string
	switch {
	case i >= 0 && j > i:
		tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
		out = content[:i] + wrapped + tail
	case i >= 0:
		return fmt.Errorf("hooks 标记块损坏（缺少结束标记）: %s", configPath)
	default:
		sep := ""
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			sep = "\n"
		}
		out = content + sep + "\n" + wrapped
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(configPath, []byte(out), 0o644)
}

// EnsureHooksBlock hook 入口自检：kimi-code 有时会清掉标记注释行，使标记块丢失
// （孤儿 hook 表仍在、hook 照常运行，但下次 setup 的去重依据没了）。标记块缺失时
// 自动备份并重新 Upsert 修复；标记块存在则不动。调用方按 fail-open 处理返回错误。
func EnsureHooksBlock(configPath, exe string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if strings.Contains(string(data), MarkerBegin) {
		return nil
	}
	_ = os.WriteFile(configPath+".bak-openknowledge", data, 0o644)
	return UpsertHooksBlock(configPath, HooksBlockFor(exe, HookTimeoutSec()))
}

// kimiAgent kimiCode 适配器。
type kimiAgent struct{}

func init() { Register(kimiAgent{}) }

func (kimiAgent) ID() string          { return "kimi" }
func (kimiAgent) DisplayName() string { return "Kimi Code" }
func (kimiAgent) SkillsDir() string   { return SkillsHome() }
func (kimiAgent) HooksTarget() string { return kimiConfigPath() }

func (kimiAgent) Detect() bool {
	info, err := os.Stat(KimiHome())
	return err == nil && info.IsDir()
}

func (kimiAgent) HooksInstalled() bool {
	data, err := os.ReadFile(kimiConfigPath())
	return err == nil && strings.Contains(string(data), MarkerBegin)
}

func (kimiAgent) InstallHooks(exe string) error {
	cfgPath := kimiConfigPath()
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = os.WriteFile(cfgPath+".bak-openknowledge", data, 0o644)
	}
	return UpsertHooksBlock(cfgPath, HooksBlockFor(exe, HookTimeoutSec()))
}

func (kimiAgent) RemoveHooks() (bool, error) {
	cfgPath := kimiConfigPath()
	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	content := string(data)
	orig := content
	i := strings.Index(content, MarkerBegin)
	j := strings.Index(content, MarkerEnd)
	if i >= 0 && j > i {
		tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
		head := strings.TrimRight(content[:i], "\n")
		content = head + "\n" + tail
	}
	content = StripLegacyOKHooks(content)
	if content == orig {
		return false, nil
	}
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("移除 hooks 配置: %w", err)
	}
	return true, nil
}

func (kimiAgent) EnsureHooks(exe string) error { return EnsureHooksBlock(kimiConfigPath(), exe) }
