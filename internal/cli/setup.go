package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	if err := fs.Parse(args); err != nil {
		return 1
	}
	exe, err := resolveExe()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if code := writeHooks(exe, stdout, stderr); code != 0 {
		return code
	}
	if err := setupx.InstallSkills(exe); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "技能已安装到 %s (openknowledge-init/on/off/propose/capture)\n", setupx.SkillsHome())
	setupEmbedding(fs.NFlag() > 0, *baseURL, *model, *apiKey, in, stdout)
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

// writeHooks 备份并幂等写入 kimi hooks 配置（自动清除存量重复的 ok hooks），
// 供 setup 与 init 共用。
func writeHooks(exe string, stdout, stderr io.Writer) int {
	cfgPath := filepath.Join(setupx.KimiHome(), "config.toml")
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = os.WriteFile(cfgPath+".bak-openknowledge", data, 0o644)
	}
	if err := setupx.UpsertHooksBlock(cfgPath, setupx.HooksBlockFor(exe)); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "hooks 配置已写入 %s\n", cfgPath)
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
