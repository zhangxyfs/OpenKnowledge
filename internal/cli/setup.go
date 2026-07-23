package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/registry"
)

const markerBegin = "# >>> openknowledge hooks >>>"
const markerEnd = "# <<< openknowledge hooks <<<"

// Setup: ok setup —— 首次引导：写 hooks 配置、装技能、配 embedding、打印引导
func Setup(args []string, in io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseURL := fs.String("embedding-base-url", "", "embedding base_url")
	model := fs.String("embedding-model", "", "embedding model")
	apiKey := fs.String("embedding-key", "", "embedding API key")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cfgPath := filepath.Join(kimiHome(), "config.toml")
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = os.WriteFile(cfgPath+".bak-openknowledge", data, 0o644)
	}
	if err := upsertHooksBlock(cfgPath, hooksBlockFor(exe)); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "hooks 配置已写入 %s\n", cfgPath)
	if err := installSkills(exe); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "技能已安装到 %s (openknowledge-init/on/off)\n", skillsHome())
	setupEmbedding(fs.NFlag() > 0, *baseURL, *model, *apiKey, in, stdout)
	fmt.Fprint(stdout, guideText+"\n")
	return 0
}

// setupEmbedding 交互或按 flags 写入全局 embedding 配置并验证连通性。
func setupEmbedding(nonInteractive bool, baseURL, model, apiKey string, in io.Reader, stdout io.Writer) {
	if !nonInteractive {
		fmt.Fprintln(stdout, "\n配置 embedding 语义检索（可选，直接回车跳过）：")
		r := bufio.NewReader(in)
		fmt.Fprintf(stdout, "base_url [https://api.openai.com/v1]: ")
		baseURL, _ = r.ReadString('\n')
		baseURL = strings.TrimSpace(baseURL)
		fmt.Fprintf(stdout, "model [text-embedding-3-small]: ")
		model, _ = r.ReadString('\n')
		model = strings.TrimSpace(model)
		fmt.Fprintf(stdout, "API key（粘贴后回车；留空跳过）: ")
		apiKey, _ = r.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
	}
	if apiKey == "" {
		fmt.Fprintln(stdout, "跳过 embedding 配置（仅关键词检索；之后可重跑 ok setup 配置）")
		return
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	globalPath := filepath.Join(registry.Home(), "config.toml")
	cfg, err := config.LoadMerged("", globalPath)
	if err != nil {
		fmt.Fprintf(stdout, "全局配置读取失败，跳过 embedding: %v\n", err)
		return
	}
	cfg.Embedding.BaseURL = baseURL
	cfg.Embedding.Model = model
	cfg.Embedding.APIKey = apiKey
	cfg.Embedding.APIKeyEnv = ""
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		fmt.Fprintf(stdout, "全局配置编码失败: %v\n", err)
		return
	}
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	if err := os.WriteFile(globalPath, []byte(buf.String()), 0o600); err != nil {
		fmt.Fprintf(stdout, "全局配置写入失败: %v\n", err)
		return
	}
	fmt.Fprintf(stdout, "embedding 已写入全局配置 %s\n", globalPath)
	client := &embed.OpenAIClient{BaseURL: baseURL, APIKey: apiKey, Model: model, Timeout: 10 * time.Second}
	if _, err := client.Embed(context.Background(), "ping"); err != nil {
		fmt.Fprintf(stdout, "embedding 连通性验证失败（不影响使用关键词检索）: %v\n", err)
	} else {
		fmt.Fprintln(stdout, "embedding 连通性验证通过")
	}
}

func kimiHome() string {
	if h := os.Getenv("KIMI_CODE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code")
}

func skillsHome() string {
	if h := os.Getenv("OK_SKILLS_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents", "skills")
}

func hooksBlockFor(exe string) string {
	exe = filepath.ToSlash(exe)
	return fmt.Sprintf(`[[hooks]]
event = "UserPromptSubmit"
command = "%s hook prompt"
timeout = 10

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "%s hook post-tool"
timeout = 5

[[hooks]]
event = "Stop"
command = "%s hook stop"
timeout = 5
`, exe, exe, exe)
}

// upsertHooksBlock 以标记块幂等写入 hooks 配置：已存在标记块则原位替换，否则追加。
func upsertHooksBlock(configPath, block string) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(data)
	wrapped := markerBegin + "\n" + block + markerEnd + "\n"
	i := strings.Index(content, markerBegin)
	j := strings.Index(content, markerEnd)
	var out string
	switch {
	case i >= 0 && j > i:
		tail := strings.TrimPrefix(content[j+len(markerEnd):], "\n")
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
	return os.WriteFile(configPath, []byte(out), 0o644)
}

func installSkills(exe string) error {
	for name, tpl := range skillTemplates {
		dir := filepath.Join(skillsHome(), name)
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

var skillTemplates = map[string]string{
	"openknowledge-init": "---\nname: openknowledge-init\ndescription: 在当前项目目录初始化 OpenKnowledge 知识库（ok init，自动以当前目录名注册，无需用户提供项目名）。当用户要求\"初始化知识库\"或\"把本项目注册到知识库\"时使用。\n---\n\n# openknowledge-init\n\n用 Bash 工具在当前工作目录直接执行（无参数，自动取当前目录名，不要向用户询问项目名）：\n\n    \"{{EXE}}\" init\n\n把输出的知识库路径汇报给用户；若提示重复注册，告知用户该项目已初始化过。\n",
	"openknowledge-on":   "---\nname: openknowledge-on\ndescription: 开启 OpenKnowledge 知识库 hooks 全局开关。当用户要求\"开启知识库\"\"启用知识库 hooks\"时使用。\n---\n\n# openknowledge-on\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" on\n\n把输出汇报给用户。\n",
	"openknowledge-off":  "---\nname: openknowledge-off\ndescription: 关闭 OpenKnowledge 知识库 hooks 全局开关（持续到手动开启）。当用户要求\"关闭知识库\"\"停用知识库 hooks\"时使用。\n---\n\n# openknowledge-off\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" off\n\n把输出汇报给用户，并说明：关闭后所有项目的知识库注入与强制检查都会暂停，直到执行 ok on。\n",
}

const guideText = `
下一步：
  1. 在需要知识库的项目目录运行 ok init（自动取当前目录名，或在 kimi 中说"初始化知识库"）
  2. 用 ok add 添加知识条目
  3. 新开 kimi 会话即可生效；ok off / ok on 可随时全局开关
`
