// Package setupx 提供 ok setup / on / off 的核心逻辑，供 CLI 与 GUI 共享。
package setupx

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"openknowledge/internal/agentx"
	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/registry"
)

// SkillNames 返回登记的技能名（供状态检测遍历）。
func SkillNames() []string {
	names := make([]string, 0, len(skillTemplates))
	for name := range skillTemplates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InstallSkills 把技能模板（烘焙 exe 路径）写入 SkillsHome。
func InstallSkills(exe string) error {
	for name, tpl := range skillTemplates {
		dir := filepath.Join(agentx.SkillsHome(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		content := strings.ReplaceAll(tpl, "{{EXE}}", filepath.ToSlash(exe))
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// SaveEmbedding 把 embedding 配置写入全局配置（0600）：
// LoadMerged → 设置字段 → 清空 APIKeyEnv → 编码 → 写入。
func SaveEmbedding(baseURL, model, apiKey string) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败，跳过 embedding: %w", err)
	}
	cfg.Embedding.BaseURL = baseURL
	cfg.Embedding.Model = model
	cfg.Embedding.APIKey = apiKey
	cfg.Embedding.APIKeyEnv = ""
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("全局配置编码失败: %w", err)
	}
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(globalPath, []byte(buf.String()), 0o600); err != nil {
		return fmt.Errorf("全局配置写入失败: %w", err)
	}
	return nil
}

// TestEmbedding 以 10s 超时做 embedding 连通性检查。
func TestEmbedding(baseURL, model, apiKey string) error {
	client := &embed.OpenAIClient{BaseURL: baseURL, APIKey: apiKey, Model: model, Timeout: 10 * time.Second}
	_, err := client.Embed(context.Background(), "ping")
	return err
}

// DisabledFlagPath 返回 hooks 全局关闭标志文件路径。
func DisabledFlagPath() string { return filepath.Join(registry.Home(), "hooks-disabled") }

// Disable 写入关闭标志文件，全局关闭 hooks（持续到 Enable）。
func Disable() error {
	content := fmt.Sprintf("disabled at %s\nrun `ok on` to re-enable\n", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(DisabledFlagPath(), []byte(content), 0o644)
}

// Enable 删除关闭标志文件，开启 hooks（幂等）。
func Enable() error {
	if err := os.Remove(DisabledFlagPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

//go:embed skills/openknowledge-wiki/SKILL.md
var wikiSkillTemplate string

func init() {
	skillTemplates["openknowledge-wiki"] = wikiSkillTemplate
}

var skillTemplates = map[string]string{
	"openknowledge-init": "---\nname: openknowledge-init\ndescription: 在当前项目目录初始化 OpenKnowledge 知识库（ok init，自动以当前目录名注册，无需用户提供项目名）。当用户要求\"初始化知识库\"或\"把本项目注册到知识库\"时使用。\n---\n\n# openknowledge-init\n\n用 Bash 工具在当前工作目录直接执行（无参数，自动取当前目录名，不要向用户询问项目名）：\n\n    \"{{EXE}}\" init\n\n把输出的知识库路径汇报给用户；若提示重复注册，告知用户该项目已初始化过。\n",
	"openknowledge-on":   "---\nname: openknowledge-on\ndescription: 开启 OpenKnowledge 知识库 hooks 全局开关。当用户要求\"开启知识库\"\"启用知识库 hooks\"时使用。\n---\n\n# openknowledge-on\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" on\n\n把输出汇报给用户。\n",
	"openknowledge-off":  "---\nname: openknowledge-off\ndescription: 关闭 OpenKnowledge 知识库 hooks 全局开关（持续到手动开启）。当用户要求\"关闭知识库\"\"停用知识库 hooks\"时使用。\n---\n\n# openknowledge-off\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" off\n\n把输出汇报给用户，并说明：关闭后所有项目的知识库注入与强制检查都会暂停，直到执行 ok on。\n",
	"openknowledge-propose": "---\nname: openknowledge-propose\ndescription: 把本次会话中沉淀的经验作为草稿条目提议进 OpenKnowledge 知识库（ok propose，待人批准）。当解决了一个非显而易见的问题、踩到坑、或发现项目隐藏约定时使用。\n---\n\n# openknowledge-propose\n\n## 先分类：经验型还是结构型\n\n- **经验型**（踩坑、隐藏约定、非显而易见问题的解法）→ 走本技能 ok propose 记草稿。\n- **结构型**（新功能、新模块、新子系统、重要架构/流程变化）→ 不是草稿，改用 openknowledge-wiki 技能新增/更新 wiki 条目（同名 add --force 重写）。\n- **两者兼有** → 都记：wiki 条目记\"是什么/怎么协作\"，草稿记\"坑\"。\n\n## 何时提议\n\n- 解决了一个非显而易见的问题（排查过程值得复用）\n- 踩到坑（环境、依赖、工具链的隐性陷阱）\n- 发现了项目的隐藏约定（代码里没有写明但必须遵守的规则）\n\n## 何时不要提议\n\n- 日常例行操作（常规增删改、跑测试、格式化）\n- 知识库已有的内容——先用 Bash 执行 `\"{{EXE}}\" search <关键词>` 查重，确认没有同主题条目再提议；若输出末尾出现\"暂无 wiki 条目覆盖\"提示行且内容属于结构型，告诉用户这是知识空白、建议用 openknowledge-wiki 技能补 wiki\n\n## 命令\n\n先把正文写入一个临时 Markdown 文件，再用 Bash 工具执行：\n\n    \"{{EXE}}\" propose --title <标题> --type pitfall --tags <逗号分隔> --file <正文.md>\n\n正文很短时可直接用 `--body`（与 `--file` 二选一）：\n\n    \"{{EXE}}\" propose --title <标题> --type note --body <正文>\n\ntype 取值：rule（规则）| pitfall（踩坑）| note（笔记）| reference（参考资料）。\n\n提议成功后告诉用户：\"已记为草稿，待批准\"（草稿不参与检索注入，用户在 GUI 点\"采纳\"或执行 `ok approve` 后转正）。\n",
	"openknowledge-capture": "---\nname: openknowledge-capture\ndescription: 查看或切换 OpenKnowledge 知识库的经验沉淀模式与轮次间隔（ok capture propose|auto|interval）。当用户要求\"切换沉淀模式\"\"开启自动提取\"\"关闭自动提取\"\"调整提取频率\"时使用。\n---\n\n# openknowledge-capture\n\n查看当前模式与轮次间隔，用 Bash 工具执行：\n\n    \"{{EXE}}\" capture\n\n切换模式，用 Bash 工具执行（二选一）：\n\n    \"{{EXE}}\" capture propose\n    \"{{EXE}}\" capture auto\n\n设置轮次间隔（n ≥ 1，仅 auto 模式生效），用 Bash 工具执行：\n\n    \"{{EXE}}\" capture interval <n>\n\n## 两种模式\n\n- **propose（默认）**：AI 主动提议——AI 觉得值得记录时用 ok propose 记为草稿条目，无轮次限制，由人批准后转正入库。\n- **auto（Stop 自动提取）**：每 turn_interval 轮对话结束时，Stop hook 阻断一次并强制 AI 自省本轮是否有值得沉淀的经验，有则当场 propose 草稿。\n\n把切换结果汇报给用户。\n",
}
