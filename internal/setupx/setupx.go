// Package setupx 提供 ok setup / on / off 的核心逻辑，供 CLI 与 GUI 共享。
package setupx

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"openknowledge/internal/agentx"
	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
	"openknowledge/internal/embedx"
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

// SkillDirs 返回技能安装目标目录并集：全部已检测 agent 的 SkillsDir() 去重
// （kimi/pi/reasonix/opencode 共享 SkillsHome，zcode 是独立的 ~/.zcode/skills）；无已检测 agent
// 时回退共享 SkillsHome（保持原语义）。
func SkillDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	for _, a := range agentx.Detected() {
		d := a.SkillsDir()
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		dirs = append(dirs, agentx.SkillsHome())
	}
	return dirs
}

// AllSkillDirs 返回全部已注册 agent 的技能目录并集（卸载清理用，不问是否检测到）。
func AllSkillDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	for _, a := range agentx.All() {
		d := a.SkillsDir()
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// InstallSkills 把技能模板（烘焙 exe 路径）写入 SkillDirs() 的每个目录。
func InstallSkills(exe string) error {
	for _, home := range SkillDirs() {
		for name, tpl := range skillTemplates {
			dir := filepath.Join(home, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			content := strings.ReplaceAll(tpl, "{{EXE}}", filepath.ToSlash(exe))
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// saveGlobalConfig 把配置写回全局 config.toml（0600）。Save* 系列共用。
func saveGlobalConfig(cfg config.Config) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
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

// SaveEmbeddingProfile 保存（同名覆盖）一个 profile 到全局配置；activate 时
// 同时置为使用中。api_key/api_key_env 留空 = 保留同名旧值（GUI 密文不回传语义）。
func SaveEmbeddingProfile(p config.EmbeddingProfile, activate bool) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败，跳过 embedding: %w", err)
	}
	for i := range cfg.Embedding.Profiles {
		if cfg.Embedding.Profiles[i].Name == p.Name {
			if p.APIKey == "" {
				p.APIKey = cfg.Embedding.Profiles[i].APIKey
			}
			if p.APIKeyEnv == "" {
				p.APIKeyEnv = cfg.Embedding.Profiles[i].APIKeyEnv
			}
			cfg.Embedding.Profiles[i] = p
			if activate {
				cfg.Embedding.Active = p.Name
			}
			return saveGlobalConfig(cfg)
		}
	}
	cfg.Embedding.Profiles = append(cfg.Embedding.Profiles, p)
	if activate {
		cfg.Embedding.Active = p.Name
	}
	return saveGlobalConfig(cfg)
}

// SetActiveEmbedding 切换使用中 profile；name 空串 = 停用（纯关键词检索）。
func SetActiveEmbedding(name string) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	if name != "" {
		found := false
		for _, p := range cfg.Embedding.Profiles {
			if p.Name == name {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("profile 不存在: %s", name)
		}
	}
	cfg.Embedding.Active = name
	return saveGlobalConfig(cfg)
}

// DeleteEmbeddingProfile 删除 profile；删除使用中项时 Active 置空（退回纯关键词）。
func DeleteEmbeddingProfile(name string) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	kept := cfg.Embedding.Profiles[:0]
	for _, p := range cfg.Embedding.Profiles {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	cfg.Embedding.Profiles = kept
	if cfg.Embedding.Active == name {
		cfg.Embedding.Active = ""
	}
	return saveGlobalConfig(cfg)
}

// SaveEmbeddingModelsDir 把内置模型目录写入全局配置 [embedding] models_dir；
// 空串 = 恢复默认（<ok.exe 所在目录>/models）。调用方负责校验/创建目录。
func SaveEmbeddingModelsDir(path string) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	cfg.Embedding.ModelsDir = path
	return saveGlobalConfig(cfg)
}

// TestEmbeddingProfile 以 timeout 做 profile 连通性检查。
// builtin：检查 runtime/模型文件，sidecar 未就绪时写 want 并返回"启动中"提示性错误。
func TestEmbeddingProfile(p config.EmbeddingProfile, timeout time.Duration) error {
	if p.Type == "builtin" {
		m := embed.FindBuiltinModel(p.Model)
		if m == nil {
			return fmt.Errorf("未知内置模型: %s", p.Model)
		}
		if _, err := embedsidecar.RuntimeServerPath(embedsidecar.DefaultRuntimeDir()); err != nil {
			return err
		}
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			return fmt.Errorf("全局配置读取失败: %w", err)
		}
		if !m.Installed(embedsidecar.ModelsDir(cfg)) {
			return errors.New("模型未下载（先在配置弹窗或 ok setup 中下载）")
		}
		c := embedx.ClientForProfile(p, timeout)
		if c == nil {
			return errors.New("sidecar 未就绪——已请求 daemon 拉起，稍后自动生效（数秒到一分钟）")
		}
		_, err = c.EmbedQuery(context.Background(), "ping")
		return err
	}
	c := embedx.ClientForProfile(p, timeout)
	if c == nil {
		return fmt.Errorf("profile 不可用（类型 %s，检查必填项）", p.Type)
	}
	_, err := c.EmbedQuery(context.Background(), "ping")
	return err
}

// ListOllamaModels 探测 Ollama 已安装模型（GET {base}/api/tags，3s 超时）。
func ListOllamaModels(baseURL string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/api/tags"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API %d", resp.StatusCode)
	}
	var tr struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tr.Models))
	for _, m := range tr.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
// SaveHooksTimeout 把 hooks 超时（秒）写入全局配置 [hooks] timeout_sec；
// 下次写入/自愈 hooks 块（含 GUI 引导页安装）时生效。
func SaveHooksTimeout(sec int) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	cfg.Hooks.TimeoutSec = sec
	return saveGlobalConfig(cfg)
}

// ReasonixEnforceMode 返回 reasonix sidecar 的强制检查表达方式：
// 全局配置 [reasonix] enforce_mode（soft|hard|mixed），缺省/非法按 mixed。
func ReasonixEnforceMode() string {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return "mixed"
	}
	switch cfg.Reasonix.EnforceMode {
	case "soft", "hard":
		return cfg.Reasonix.EnforceMode
	default:
		return "mixed"
	}
}

// SaveReasonixEnforceMode 校验并写入全局配置 [reasonix] enforce_mode；
// sidecar 每条输入实时读配置，即时生效。
func SaveReasonixEnforceMode(mode string) error {
	switch mode {
	case "soft", "hard", "mixed":
	default:
		return fmt.Errorf("enforce_mode 必须是 soft|hard|mixed: %q", mode)
	}
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		return fmt.Errorf("全局配置读取失败: %w", err)
	}
	cfg.Reasonix.EnforceMode = mode
	return saveGlobalConfig(cfg)
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
	"openknowledge-propose": "---\nname: openknowledge-propose\ndescription: 把本次会话中沉淀的经验作为草稿条目提议进 OpenKnowledge 知识库（ok propose，待人批准）。当解决了一个非显而易见的问题、踩到坑、发现项目隐藏约定，或新需求/新功能/架构变化定稿需要沉淀时使用。\n---\n\n# openknowledge-propose\n\n## 先分类：经验型还是结构型\n\n- **经验型**（踩坑、隐藏约定、非显而易见问题的解法）→ 走本技能 ok propose 记草稿。\n- **结构型**（新功能、新模块、新子系统、重要架构/流程变化）→ 不是草稿，改用 openknowledge-wiki 技能新增/更新 wiki 条目（同名 add --force 重写）。\n- **两者兼有** → 都记：wiki 条目记\"是什么/怎么协作\"，草稿记\"坑\"。\n\n## 何时提议\n\n- 解决了一个非显而易见的问题（排查过程值得复用）\n- 踩到坑（环境、依赖、工具链的隐性陷阱）\n- 发现了项目的隐藏约定（代码里没有写明但必须遵守的规则）\n\n## 何时不要提议\n\n- 日常例行操作（常规增删改、跑测试、格式化）\n- 知识库已有的内容——先用 Bash 执行 `\"{{EXE}}\" search <关键词>` 查重，确认没有同主题条目再提议；若输出末尾出现\"暂无 wiki 条目覆盖\"提示行且内容属于结构型，告诉用户这是知识空白、建议用 openknowledge-wiki 技能补 wiki\n\n## 命令\n\n先把正文写入一个临时 Markdown 文件，再用 Bash 工具执行：\n\n    \"{{EXE}}\" propose --title <标题> --type pitfall --tags <逗号分隔> --file <正文.md>\n\n正文很短时可直接用 `--body`（与 `--file` 二选一）：\n\n    \"{{EXE}}\" propose --title <标题> --type note --body <正文>\n\ntype 取值：rule（规则）| pitfall（踩坑）| note（笔记）| reference（参考资料）。\n\n提议成功后告诉用户：\"已记为草稿，待批准\"（草稿不参与检索注入，用户在 GUI 点\"采纳\"或执行 `ok approve` 后转正）。\n",
	"openknowledge-capture": "---\nname: openknowledge-capture\ndescription: 查看或切换 OpenKnowledge 知识库的经验沉淀模式与轮次间隔（ok capture propose|auto|interval）。当用户要求\"切换沉淀模式\"\"开启自动提取\"\"关闭自动提取\"\"调整提取频率\"时使用。\n---\n\n# openknowledge-capture\n\n查看当前模式与轮次间隔，用 Bash 工具执行：\n\n    \"{{EXE}}\" capture\n\n切换模式，用 Bash 工具执行（二选一）：\n\n    \"{{EXE}}\" capture propose\n    \"{{EXE}}\" capture auto\n\n设置轮次间隔（n ≥ 1，仅 auto 模式生效），用 Bash 工具执行：\n\n    \"{{EXE}}\" capture interval <n>\n\n## 两种模式\n\n- **propose（默认）**：AI 主动提议——AI 觉得值得记录时用 ok propose 记为草稿条目，无轮次限制，由人批准后转正入库。\n- **auto（Stop 自动提取）**：每 turn_interval 轮对话结束时，Stop hook 阻断一次并强制 AI 自省本轮是否有值得沉淀的经验，有则当场 propose 草稿。\n\n把切换结果汇报给用户。\n",
}
