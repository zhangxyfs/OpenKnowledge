package agentx

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed pi_extension.ts
var piExtensionTemplate string

// piExtensionMarker 本工具生成的扩展文件头标记（RemoveHooks 据此识别归属）。
const piExtensionMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"

// PiHome 返回 pi 配置根目录（PI_CODING_AGENT_DIR 优先）。
func PiHome() string {
	if h := os.Getenv("PI_CODING_AGENT_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}

// piAgent Pi 适配器：hook 集成 = 写 TS 扩展到 ~/.pi/agent/extensions/。
type piAgent struct{}

func init() { Register(piAgent{}) }

func (piAgent) ID() string          { return "pi" }
func (piAgent) DisplayName() string { return "Pi" }
func (piAgent) SkillsDir() string   { return SkillsHome() }
func (piAgent) HooksTarget() string { return piExtensionPath() }

func piExtensionPath() string { return filepath.Join(PiHome(), "extensions", "openknowledge.ts") }

func (piAgent) Detect() bool {
	info, err := os.Stat(PiHome())
	return err == nil && info.IsDir()
}

// piTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func piTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(piExtensionTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderPiExtension 渲染扩展：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderPiExtension(exe string) string {
	body := strings.ReplaceAll(piExtensionTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return piExtensionMarker + "\n// fingerprint: " + piTemplateFingerprint() + "\n" + body
}

func (piAgent) HooksInstalled() bool {
	data, err := os.ReadFile(piExtensionPath())
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, piExtensionMarker) &&
		strings.Contains(content, "// fingerprint: "+piTemplateFingerprint())
}

func (piAgent) InstallHooks(exe string) error {
	path := piExtensionPath()
	if data, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(data), piExtensionMarker) {
			if err := os.WriteFile(path+".bak-openknowledge", data, 0o644); err != nil {
				return fmt.Errorf("备份既有扩展失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取既有扩展失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(renderPiExtension(exe)), 0o644)
}

func (piAgent) RemoveHooks() (bool, error) {
	path := piExtensionPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(data), piExtensionMarker) {
		return false, nil // 非本工具生成，不删
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("删除 pi 扩展: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：文件存在且为本工具生成、但内容过期（模板升级或 exe 迁移）
// 时重写；文件不存在时为 no-op（pi 无扩展即不会触发 hook，无需修复）。
func (piAgent) EnsureHooks(exe string) error {
	path := piExtensionPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if !strings.Contains(string(data), piExtensionMarker) {
		return nil
	}
	rendered := renderPiExtension(exe)
	if string(data) == rendered {
		return nil
	}
	return os.WriteFile(path, []byte(rendered), 0o644)
}
