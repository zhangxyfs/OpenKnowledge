package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/agentx"
	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
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
	if err := setupx.WriteAutostart(exe); err != nil {
		fmt.Fprintf(stderr, "警告：登录自启写入失败: %v\n", err)
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
		fmt.Fprintf(stdout, "未检测到支持的 agent（%s），跳过 hooks 写入\n", agentIDs())
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

// setupEmbedding 交互三选一（线上/Ollama/内置）或按 flags 写入（flags 向后兼容，
// 固定写 openai "默认" profile）。
func setupEmbedding(nonInteractive bool, baseURL, model, apiKey string, in io.Reader, stdout io.Writer) {
	if nonInteractive {
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
		saveAndTestProfile(config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: baseURL, Model: model, APIKey: apiKey}, stdout)
		return
	}
	fmt.Fprintln(stdout, "\n配置 embedding 语义检索（可选，直接回车跳过）：")
	fmt.Fprintln(stdout, "  1) 线上 OpenAI 兼容服务")
	fmt.Fprintln(stdout, "  2) Ollama（本机/局域网，免 key）")
	fmt.Fprintln(stdout, "  3) 内置本地模型（ok 托管，完全离线）")
	r := bufio.NewReader(in)
	fmt.Fprint(stdout, "选择 [1/2/3]: ")
	choice, _ := r.ReadString('\n')
	switch strings.TrimSpace(choice) {
	case "1":
		fmt.Fprintf(stdout, "base_url [https://api.openai.com/v1]: ")
		baseURL, _ = r.ReadString('\n')
		baseURL = strings.TrimSpace(baseURL)
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		fmt.Fprintf(stdout, "model [text-embedding-3-small]: ")
		model, _ = r.ReadString('\n')
		model = strings.TrimSpace(model)
		if model == "" {
			model = "text-embedding-3-small"
		}
		fmt.Fprintf(stdout, "API key（粘贴后回车；留空跳过）: ")
		apiKey, _ = r.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			fmt.Fprintln(stdout, "跳过 embedding 配置（仅关键词检索）")
			return
		}
		saveAndTestProfile(config.EmbeddingProfile{Name: "默认", Type: "openai", BaseURL: baseURL, Model: model, APIKey: apiKey}, stdout)
	case "2":
		fmt.Fprintf(stdout, "Ollama 地址 [http://localhost:11434]: ")
		base, _ := r.ReadString('\n')
		base = strings.TrimSpace(base)
		if base == "" {
			base = "http://localhost:11434"
		}
		if models, err := setupx.ListOllamaModels(base); err != nil {
			fmt.Fprintf(stdout, "Ollama 探测失败（%v），按手动输入继续\n", err)
		} else if len(models) > 0 {
			fmt.Fprintln(stdout, "已安装模型："+strings.Join(models, "，"))
		}
		fmt.Fprintf(stdout, "模型 [bge-m3]: ")
		m, _ := r.ReadString('\n')
		m = strings.TrimSpace(m)
		if m == "" {
			m = "bge-m3"
		}
		saveAndTestProfile(config.EmbeddingProfile{Name: "Ollama 本机", Type: "ollama", BaseURL: base, Model: m}, stdout)
	case "3":
		setupEmbeddingBuiltin(r, stdout)
	default:
		fmt.Fprintln(stdout, "跳过 embedding 配置（仅关键词检索；之后可重跑 ok setup 配置）")
	}
}

// saveAndTestProfile 保存并激活 profile，然后连通性验证。
func saveAndTestProfile(p config.EmbeddingProfile, stdout io.Writer) {
	if err := setupx.SaveEmbeddingProfile(p, true); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	fmt.Fprintf(stdout, "embedding 已写入全局配置 %s\n", filepath.Join(registry.Home(), "config.toml"))
	if err := setupx.TestEmbeddingProfile(p, 10*time.Second); err != nil {
		fmt.Fprintf(stdout, "embedding 连通性验证：%v\n", err)
	} else {
		fmt.Fprintln(stdout, "embedding 连通性验证通过")
	}
}

// setupEmbeddingBuiltin 内置模型：选档位 → 选镜像 → 按需下载（进度行）→ 激活 + 请求拉起。
func setupEmbeddingBuiltin(r *bufio.Reader, stdout io.Writer) {
	if _, err := embedsidecar.RuntimeServerPath(embedsidecar.DefaultRuntimeDir()); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	fmt.Fprintln(stdout, "可选模型：")
	for i, m := range embed.BuiltinModels {
		fmt.Fprintf(stdout, "  %d) %s\n", i+1, m.Label)
	}
	fmt.Fprint(stdout, "选择 [1]: ")
	sel, _ := r.ReadString('\n')
	idx := 1
	fmt.Sscanf(strings.TrimSpace(sel), "%d", &idx)
	if idx < 1 || idx > len(embed.BuiltinModels) {
		idx = 1
	}
	m := embed.BuiltinModels[idx-1]
	fmt.Fprint(stdout, "下载源 [1=hf-mirror 国内镜像（默认） 2=huggingface 官方]: ")
	ms, _ := r.ReadString('\n')
	mirror := "hf-mirror"
	if strings.TrimSpace(ms) == "2" {
		mirror = "huggingface"
	}
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		fmt.Fprintf(stdout, "全局配置读取失败：%v\n", err)
		return
	}
	modelsDir := embedsidecar.ModelsDir(cfg)
	if !m.Installed(modelsDir) {
		fmt.Fprintf(stdout, "开始下载 %s …\n", m.File)
		err := embed.Download(context.Background(), nil, m, mirror, modelsDir, func(done, total int64) {
			fmt.Fprintf(stdout, "\r  %d / %d MB", done>>20, total>>20)
		})
		fmt.Fprintln(stdout)
		if err != nil {
			fmt.Fprintf(stdout, "下载失败：%v（重跑 ok setup 可断点续传）\n", err)
			return
		}
	}
	p := config.EmbeddingProfile{Name: "内置 " + m.ID, Type: "builtin", Model: m.ID, Mirror: mirror}
	if err := setupx.SaveEmbeddingProfile(p, true); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return
	}
	embedsidecar.RequestStart()
	fmt.Fprintln(stdout, "已设为使用中；sidecar 由 daemon 自动拉起（首次数秒到一分钟），期间检索退化为关键词")
}

const guideText = `
下一步：
  1. 在需要知识库的项目目录运行 ok init（自动取当前目录名，或在 agent 会话中说"初始化知识库"）
  2. 用 ok add 添加知识条目
  3. 新开 agent 会话即可生效；ok off / ok on 可随时全局开关
`

// agentIDs 返回已注册 agent 的 id 列表（用于报错提示）。
func agentIDs() string {
	ids := make([]string, 0, len(agentx.All()))
	for _, a := range agentx.All() {
		ids = append(ids, a.ID())
	}
	return strings.Join(ids, " / ")
}
