package setupx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openknowledge/internal/agentx"
	"openknowledge/internal/daemonx"
	"openknowledge/internal/registry"
)

// UninstallResult 汇总卸载各步骤的结果（KB 数据始终保留）。
type UninstallResult struct {
	HooksRemoved     bool // kimi config.toml 中的 hooks 标记块已移除
	SkillsRemoved    int  // 删除的技能目录数
	EmbeddingRemoved bool // 全局配置中的 [embedding] 小节已移除
}

// Uninstall 卸载 OpenKnowledge 的集成部分：hooks 配置、技能、全局 embedding 配置。
// 绝不触碰知识库数据（registry、projects、kb.db、knowledge 条目）。
func Uninstall() (*UninstallResult, error) {
	r := &UninstallResult{}

	// 0. 停止常驻 daemon（不存在则忽略）
	daemonx.StopDaemon()

	// 1. 移除所有已注册 agent 的 hooks 集成
	hooksRemoved := false
	for _, a := range agentx.All() {
		removed, err := a.RemoveHooks()
		if err != nil {
			return r, fmt.Errorf("移除 %s hooks: %w", a.ID(), err)
		}
		hooksRemoved = hooksRemoved || removed
	}
	r.HooksRemoved = hooksRemoved

	// 2. 删除已安装的技能目录（仅 skillTemplates 中登记的）
	for name := range skillTemplates {
		dir := filepath.Join(agentx.SkillsHome(), name)
		if _, err := os.Stat(dir); err == nil {
			if err := os.RemoveAll(dir); err != nil {
				return r, fmt.Errorf("删除技能 %s: %w", name, err)
			}
			r.SkillsRemoved++
		}
	}

	// 3. 移除全局配置中的 [embedding] 小节
	globalPath := filepath.Join(registry.Home(), "config.toml")
	removed, err := RemoveSection(globalPath, "[embedding]")
	if err != nil {
		return r, fmt.Errorf("移除 embedding 配置: %w", err)
	}
	r.EmbeddingRemoved = removed
	return r, nil
}

// RemoveSection 从 toml 文件中删除指定小节（到下一个 [section] 或文件尾），
// 其余内容原样保留；文件因此不再含任何有效内容时删除文件本身。
// 返回是否真的删除了小节。
func RemoveSection(path, section string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == section {
				start = i
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	if start < 0 {
		return false, nil
	}
	out := append([]string{}, lines[:start]...)
	// 去掉紧挨小节前的一个空行（如果小节原本前面有空行且输出非空）
	out = append(out, lines[end:]...)
	var body []string
	for _, l := range out {
		body = append(body, strings.TrimRight(l, " \t"))
	}
	content := strings.TrimSpace(strings.Join(body, "\n"))
	if content == "" {
		if err := os.Remove(path); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.WriteFile(path, []byte(strings.Join(body, "\n")), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
