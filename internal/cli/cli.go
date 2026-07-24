package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/index"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/setupx"
	"openknowledge/internal/store"
)

const hooksBlock = `
# OpenKnowledge hooks —— 追加到 ~/.kimi-code/config.toml
[[hooks]]
event = "UserPromptSubmit"
command = "ok hook prompt"
timeout = 10

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "ok hook post-tool"
timeout = 5

[[hooks]]
event = "Stop"
command = "ok hook stop"
timeout = 5
`

const defaultProjectConfig = `# OpenKnowledge 项目知识库配置
# [embedding] / [inject] / [retrieve] 缺省继承全局配置 ~/.openknowledge/config.toml。
# 需要按项目覆盖时自行添加对应小节（字段见 ok setup 输出与设计文档）。

# 强制规则（glob 一律小写；同会话同规则只阻断一次）：
# [[enforce]]
# type = "changelog_required"
# code_globs = ["**/*.go"]
# changelog_glob = "docs/changelogs/**"
# message = "本次会话修改了代码但未更新变更日志，请先按规范补齐。"
`

// resolveFromCwd 以进程当前目录解析项目；失败时打印提示。
func resolveFromCwd(stderr io.Writer) (*project.Context, int) {
	pc, err := project.FromCurrentDir()
	if err != nil {
		fmt.Fprintf(stderr, "%v（请先在项目目录运行 ok init <name>）\n", err)
		return nil, 1
	}
	return pc, 0
}

// Init: ok init <name>
func Init(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "用法: ok init [项目名]（缺省取当前目录名）")
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	name := filepath.Base(cwd)
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := reg.AddProject(name, cwd); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	st := store.New(filepath.Join(registry.Home(), "projects", name))
	if err := st.EnsureDirs(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := os.Stat(st.ConfigPath()); os.IsNotExist(err) {
		if err := os.WriteFile(st.ConfigPath(), []byte(defaultProjectConfig), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "已注册项目 %q → %s\n知识库目录: %s\n", name, cwd, st.Root)
	fmt.Fprint(stdout, hooksBlock+"\n")
	fmt.Fprintln(stdout, "或直接运行 ok setup 自动写入 hooks 配置并安装技能（推荐）")
	return 0
}

// Add: ok add --title T --type rule --tags a,b --mandatory [--file body.md]
func Add(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "条目标题（必填）")
	typ := fs.String("type", "note", "rule|pitfall|note|reference")
	tags := fs.String("tags", "", "逗号分隔")
	mandatory := fs.Bool("mandatory", false, "每会话首次提问全文注入")
	file := fs.String("file", "", "正文来源文件；缺省生成模板")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *title == "" || !entry.ValidType(*typ) {
		fmt.Fprintln(stderr, "用法: ok add --title <标题> --type <rule|pitfall|note|reference> [--tags a,b] [--mandatory] [--file 正文.md]")
		return 1
	}
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	body := "TODO: 在此填写正文（frontmatter 中的 summary 也请补充）"
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		body = string(data)
	}
	e := &entry.Entry{Title: *title, Type: *typ, Mandatory: *mandatory, Summary: *title, Body: strings.TrimSpace(body)}
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			e.Tags = append(e.Tags, strings.TrimSpace(t))
		}
	}
	path := filepath.Join(pc.Store.KnowledgeDir(), entry.Slug(*title)+".md")
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(stderr, "条目已存在: %s\n", path)
		return 1
	}
	if err := os.WriteFile(path, e.Serialize(), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "已创建 %s\n", path)
	return afterAdd(pc, stdout, stderr)
}

// afterAdd 增量同步索引库（重建 INDEX.md；有 API key 时为变化条目重算向量）。
func afterAdd(pc *project.Context, stdout, stderr io.Writer) int {
	db, err := index.Open(pc.Store.KbPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	var client embed.Client
	if c := embeddingClient(pc); c != nil {
		client = c
	}
	if err := db.Sync(pc.Store.KnowledgeDir(), client); err != nil {
		var corrupt *index.CorruptEntriesError
		switch {
		case errors.As(err, &corrupt):
			// 损坏条目已跳过、INDEX 已重建：警告到 stderr，成功流程继续
			fmt.Fprintln(stderr, err)
		case client == nil:
			fmt.Fprintln(stderr, err)
			return 1
		default:
			// embedding 失败：降级为只同步 INDEX，向量稍后 ok index 补齐
			if err2 := db.Sync(pc.Store.KnowledgeDir(), nil); err2 != nil {
				fmt.Fprintln(stderr, err2)
				if !errors.As(err2, &corrupt) {
					return 1
				}
			}
			fmt.Fprintf(stdout, "INDEX 已更新；向量更新失败（可稍后 ok index 重试）: %v\n", err)
			return 0
		}
	}
	if client == nil {
		fmt.Fprintln(stdout, "INDEX 已更新；未配置 embedding API key，向量跳过（稍后运行 ok index）")
		return 0
	}
	fmt.Fprintln(stdout, "INDEX 与向量已更新")
	return 0
}

// embeddingClient 配置齐全时返回客户端，否则返回 nil。
func embeddingClient(pc *project.Context) *embed.OpenAIClient {
	key := pc.Config.Embedding.ResolvedAPIKey()
	if key == "" || pc.Config.Embedding.BaseURL == "" {
		return nil
	}
	return &embed.OpenAIClient{
		BaseURL: pc.Config.Embedding.BaseURL,
		APIKey:  key,
		Model:   pc.Config.Embedding.Model,
		Timeout: time.Duration(pc.Config.Embedding.TimeoutSec) * time.Second,
	}
}

// Search: ok search <查询>
func Search(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "用法: ok search <查询>")
		return 1
	}
	query := strings.Join(fs.Args(), " ")
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	db, err := index.Open(pc.Store.KbPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	var queryVec []float32
	if client := embeddingClient(pc); client != nil {
		if vec, err := client.Embed(context.Background(), query); err != nil {
			fmt.Fprintf(stderr, "embedding 失败，降级为关键词检索: %v\n", err)
		} else {
			queryVec = vec
		}
	}
	hits, err := db.Query(retrieve.Terms(query), queryVec, pc.Config.Retrieve)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, h := range hits {
		fmt.Fprintf(stdout, "%.2f\t%s (%s)\n", h.Score, h.Title, h.Filename)
	}
	return 0
}

// Index: ok index —— 增量同步索引库（INDEX.md + 条目与向量）
func Index(args []string, stdout, stderr io.Writer) int {
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	db, err := index.Open(pc.Store.KbPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	var client embed.Client
	if c := embeddingClient(pc); c != nil {
		client = c
	}
	if err := db.Sync(pc.Store.KnowledgeDir(), client); err != nil {
		var corrupt *index.CorruptEntriesError
		if errors.As(err, &corrupt) {
			// 损坏条目已跳过、INDEX 已重建：警告到 stderr，成功流程继续
			fmt.Fprintln(stderr, err)
		} else {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	n, err := db.Count()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if client == nil {
		fmt.Fprintf(stderr, "INDEX 已重建（%d 条）；未配置 embedding API key，跳过向量重建\n", n)
		return 1
	}
	fmt.Fprintf(stdout, "INDEX 已重建；索引共 %d 条\n", n)
	return 0
}

// List: ok list —— 列出项目与条目（* 表示 mandatory）
func List(args []string, stdout, stderr io.Writer) int {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, p := range reg.Projects {
		fmt.Fprintf(stdout, "%s → %s\n", p.Name, strings.Join(p.Paths, ", "))
		st := store.New(filepath.Join(registry.Home(), "projects", p.Name))
		entries, err := entry.Load(st.KnowledgeDir())
		if err != nil {
			continue
		}
		for _, e := range entries {
			mark := "  "
			if e.Mandatory {
				mark = "* "
			}
			fmt.Fprintf(stdout, "  %s%s (%s)\n", mark, e.Title, e.Type)
		}
	}
	return 0
}

// Doctor: ok doctor —— 检查注册表、配置与 embedding 连通性
func Doctor(args []string, stdout, stderr io.Writer) int {
	healthy := true
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		fmt.Fprintf(stderr, "注册表读取失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "注册表: %d 个项目\n", len(reg.Projects))
	if data, err := os.ReadFile(filepath.Join(setupx.KimiHome(), "config.toml")); err != nil || !strings.Contains(string(data), setupx.MarkerBegin) {
		fmt.Fprintln(stdout, "hooks 未安装（运行 ok setup）")
		healthy = false
	} else {
		fmt.Fprintln(stdout, "hooks 已安装")
	}
	if registry.HooksDisabled() {
		fmt.Fprintln(stdout, "hooks 当前为关闭状态（ok on 开启）")
	}
	for _, p := range reg.Projects {
		st := store.New(filepath.Join(registry.Home(), "projects", p.Name))
		if _, err := os.Stat(st.KnowledgeDir()); err != nil {
			fmt.Fprintf(stdout, "[%s] knowledge 目录缺失\n", p.Name)
			healthy = false
		}
		pc, err := project.FromCwd(p.Paths[0])
		if err != nil {
			fmt.Fprintf(stdout, "[%s] %v\n", p.Name, err)
			healthy = false
			continue
		}
		client := embeddingClient(pc)
		if client == nil {
			fmt.Fprintf(stdout, "[%s] 未配置 embedding（仅关键词检索可用）\n", p.Name)
			continue
		}
		if _, err := client.Embed(context.Background(), "ping"); err != nil {
			fmt.Fprintf(stdout, "[%s] embedding API 不可用: %v\n", p.Name, err)
			healthy = false
		} else {
			fmt.Fprintf(stdout, "[%s] embedding API 正常\n", p.Name)
		}
	}
	if !healthy {
		return 1
	}
	fmt.Fprintln(stdout, "一切正常")
	return 0
}
