package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"openknowledge/internal/agentx"
	"openknowledge/internal/registry"
	"openknowledge/internal/setupx"
)

// Setup: ok setup —— 首次引导：写 hooks 配置、装技能、配 embedding、打印引导
func Setup(args []string, in io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseURL := fs.String("embedding-base-url", "", "embedding base_url")
	model := fs.String("embedding-model", "", "embedding model")
	apiKey := fs.String("embedding-key", "", "embedding API key")
	agentID := fs.String("agent", "", "只安装指定 agent 的 hooks（"+strings.ReplaceAll(agentIDs(), " / ", "|")+"）；缺省为全部已检测 agent")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	var targets []agentx.Agent
	if *agentID != "" {
		a, ok := agentx.Find(*agentID)
		if !ok {
			fmt.Fprintf(stderr, "未知 agent %q（可用：%s）\n", *agentID, agentIDs())
			return 1
		}
		if !a.Detect() {
			fmt.Fprintf(stderr, "提示：未检测到 %s，仍将写入其配置\n", a.DisplayName())
		}
		targets = []agentx.Agent{a}
	}
	exe, err := resolveExe()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if code := writeHooks(targets, exe, stdout, stderr); code != 0 {
		return code
	}
	if err := setupx.InstallSkills(exe); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "技能已安装到 %s\n", strings.Join(setupx.SkillDirs(), "；"))
	embeddingSet := false
	fs.Visit(func(f *flag.Flag) {
		if strings.HasPrefix(f.Name, "embedding-") {
			embeddingSet = true
		}
	})
	setupEmbedding(embeddingSet, *baseURL, *model, *apiKey, in, stdout)
	fmt.Fprint(stdout, guideText+"\n")
	return 0
}

// resolveExe 返回当前可执行文件的解析后路径。
func resolveExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// writeHooks 对目标 agent 幂等写入 hooks 集成（targets 为 nil 时取全部已检测
// agent），供 setup 与 init 共用。单 agent 失败不影响其余：失败逐项收集，
// 全部尝试后汇总到 stderr 并返回 1。
func writeHooks(targets []agentx.Agent, exe string, stdout, stderr io.Writer) int {
	if targets == nil {
		targets = agentx.Detected()
	}
	if len(targets) == 0 {
		fmt.Fprintln(stdout, "未检测到支持的 agent（kimi / pi），跳过 hooks 写入")
		return 0
	}
	var failed []string
	for _, a := range targets {
		if err := a.InstallHooks(exe); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.ID(), err))
			continue
		}
		fmt.Fprintf(stdout, "hooks 配置已写入 %s\n", a.HooksTarget())
	}
	if len(failed) > 0 {
		fmt.Fprintf(stderr, "部分 agent hooks 写入失败：\n%s\n", strings.Join(failed, "\n"))
		return 1
	}
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
	if err := setupx.SaveEmbedding(baseURL, model, apiKey); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	fmt.Fprintf(stdout, "embedding 已写入全局配置 %s\n", filepath.Join(registry.Home(), "config.toml"))
	if err := setupx.TestEmbedding(baseURL, model, apiKey); err != nil {
		fmt.Fprintf(stdout, "embedding 连通性验证失败（不影响使用关键词检索）: %v\n", err)
	} else {
		fmt.Fprintln(stdout, "embedding 连通性验证通过")
	}
}

const guideText = `
下一步：
  1. 在需要知识库的项目目录运行 ok init（自动取当前目录名，或在 kimi 中说"初始化知识库"）
  2. 用 ok add 添加知识条目
  3. 新开 kimi 会话即可生效；ok off / ok on 可随时全局开关
`

// agentIDs 返回已注册 agent 的 id 列表（用于报错提示）。
func agentIDs() string {
	ids := make([]string, 0, len(agentx.All()))
	for _, a := range agentx.All() {
		ids = append(ids, a.ID())
	}
	return strings.Join(ids, " / ")
}
