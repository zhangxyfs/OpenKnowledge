package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/agentx"
	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/index"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/store"
	"openknowledge/internal/wiki"
)

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
	// 幂等写入 hooks 配置（已存在则覆盖 exe 路径并去重）；失败不阻断注册结果
	if exe, err := resolveExe(); err != nil {
		fmt.Fprintf(stderr, "hooks 配置写入失败（可运行 ok setup 重试）: %v\n", err)
	} else if code := writeHooks(nil, exe, stdout, stderr); code != 0 {
		fmt.Fprintln(stderr, "hooks 配置写入失败（可运行 ok setup 重试）")
	}
	return 0
}

// Add: ok add --title T --type rule --tags a,b --summary S --mandatory [--file body.md] [--force]
func Add(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "条目标题（必填）")
	typ := fs.String("type", "note", "rule|pitfall|note|reference")
	tags := fs.String("tags", "", "逗号分隔")
	summary := fs.String("summary", "", "一句话摘要（缺省取标题）")
	mandatory := fs.Bool("mandatory", false, "每会话首次提问全文注入")
	file := fs.String("file", "", "正文来源文件；缺省生成模板")
	force := fs.Bool("force", false, "覆盖已存在的同名条目")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *title == "" || !entry.ValidType(*typ) {
		fmt.Fprintln(stderr, "用法: ok add --title <标题> --type <rule|pitfall|note|reference> [--tags a,b] [--summary 摘要] [--mandatory] [--file 正文.md] [--force]")
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
	sum := *summary
	if sum == "" {
		sum = *title
	}
	e := &entry.Entry{Title: *title, Type: *typ, Mandatory: *mandatory, Summary: sum, Body: strings.TrimSpace(body)}
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			e.Tags = append(e.Tags, strings.TrimSpace(t))
		}
	}
	path := filepath.Join(pc.Store.KnowledgeDir(), entry.Slug(*title)+".md")
	if _, err := os.Stat(path); err == nil && !*force {
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
	terms := retrieve.Terms(query)
	hits, err := db.Query(terms, queryVec, pc.Config.Retrieve)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, h := range hits {
		fmt.Fprintf(stdout, "%.2f\t%s (%s)\n", h.Score, h.Title, h.Filename)
	}
	// wiki 覆盖兜底提示：无 wiki 条目命中该主题时，提示可经 openknowledge-wiki 补充。
	// fail-open：检查失败不提示，search 主输出格式不变。
	if covered, err := db.HasWikiMatch(terms); err == nil && !covered {
		fmt.Fprintln(stdout, "提示：该主题暂无 wiki 条目覆盖；若内容属于新功能/新模块，建议用 openknowledge-wiki 技能补充 wiki。")
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
	detectedAny := false
	for _, a := range agentx.All() {
		if !a.Detect() {
			continue
		}
		detectedAny = true
		if a.HooksInstalled() {
			fmt.Fprintf(stdout, "[%s] hooks 已安装\n", a.ID())
		} else {
			fmt.Fprintf(stdout, "[%s] hooks 未安装（运行 ok setup）\n", a.ID())
			healthy = false
		}
	}
	if !detectedAny {
		fmt.Fprintln(stdout, "未检测到支持的 agent（kimi / pi / zcode）")
		healthy = false
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

// Propose: ok propose --title T [--type note] [--tags a,b] [--summary S] [--file f | --body text]
// AI 面向的草稿写入：条目带 draft:true（mandatory 恒为 false），只同步 INDEX
// 不算向量，待 ok approve 批准后才参与检索。
func Propose(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("propose", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "条目标题（必填）")
	typ := fs.String("type", "note", "rule|pitfall|note|reference")
	tags := fs.String("tags", "", "逗号分隔")
	summary := fs.String("summary", "", "一句话摘要（缺省取标题）")
	file := fs.String("file", "", "正文来源文件（与 --body 二选一）")
	body := fs.String("body", "", "内联正文（与 --file 二选一）")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *title == "" || !entry.ValidType(*typ) || (*file != "" && *body != "") {
		fmt.Fprintln(stderr, "用法: ok propose --title <标题> [--type <rule|pitfall|note|reference>] [--tags a,b] [--summary 摘要] [--file 正文.md | --body 正文]")
		return 1
	}
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	content := "TODO: 在此填写正文（frontmatter 中的 summary 也请补充）"
	switch {
	case *file != "":
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		content = string(data)
	case *body != "":
		content = *body
	}
	sum := *summary
	if sum == "" {
		sum = *title
	}
	e := &entry.Entry{Title: *title, Type: *typ, Draft: true, Summary: sum, Body: strings.TrimSpace(content)}
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
	fmt.Fprintf(stdout, "已创建 %s（已记为草稿，待批准）\n", path)
	// 草稿只同步 INDEX，不算向量（nil client）
	db, err := index.Open(pc.Store.KbPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	if err := db.Sync(pc.Store.KnowledgeDir(), nil); err != nil {
		var corrupt *index.CorruptEntriesError
		if errors.As(err, &corrupt) {
			// 损坏条目已跳过、INDEX 已重建：警告到 stderr，成功流程继续
			fmt.Fprintln(stderr, err)
		} else {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, "INDEX 已更新（草稿不参与检索与向量）")
	return 0
}

// Approve: ok approve <file> —— 将草稿条目转正（draft=false，其余字段原样保留），
// 同步 INDEX 并（有 API key 时）计算向量。
func Approve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "用法: ok approve <条目文件>")
		return 1
	}
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	path := fs.Arg(0)
	if _, err := os.Stat(path); err != nil {
		// 裸文件名按 knowledge 目录解析
		path = filepath.Join(pc.Store.KnowledgeDir(), filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "条目不存在: %v\n", err)
		return 1
	}
	e, err := entry.Parse(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !e.Draft {
		fmt.Fprintf(stderr, "不是草稿条目: %s\n", path)
		return 1
	}
	e.Draft = false
	// Sync 的 diff 按秒级 mtime 判断变化；propose 后同一秒内 approve 会被误判为
	// 未变化而跳过重建，此时手动把 mtime 推进一秒。
	oldInfo, statErr := os.Stat(path)
	if err := os.WriteFile(path, e.Serialize(), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if statErr == nil {
		if newInfo, err := os.Stat(path); err == nil && newInfo.ModTime().Unix() == oldInfo.ModTime().Unix() {
			t := oldInfo.ModTime().Add(time.Second)
			_ = os.Chtimes(path, t, t)
		}
	}
	fmt.Fprintf(stdout, "已批准 %s\n", path)
	return afterAdd(pc, stdout, stderr)
}

// CaptureCmd: ok capture —— 打印当前捕获模式与 turn_interval；
// ok capture propose|auto 设置模式；ok capture interval <n> 设置轮次间隔（auto 模式生效）。
func CaptureCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	cfgPath := pc.Store.ConfigPath()
	header := defaultProjectConfig + "\n"
	switch fs.Arg(0) {
	case "":
		fmt.Fprintf(stdout, "capture 模式: %s（turn_interval=%d）\n", pc.Config.Capture.Mode, pc.Config.Capture.TurnInterval)
		return 0
	case "propose", "auto":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "用法: ok capture [propose|auto|interval <n>]")
			return 1
		}
		if err := config.SetCapture(cfgPath, fs.Arg(0), pc.Config.Capture.TurnInterval, header); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "capture 模式已设为 %s（%s）\n", fs.Arg(0), cfgPath)
		return 0
	case "interval":
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, "用法: ok capture interval <n>（n ≥ 1，auto 模式下每 n 回合自省一次）")
			return 1
		}
		n, err := strconv.Atoi(fs.Arg(1))
		if err != nil || n < 1 {
			fmt.Fprintln(stderr, "interval 必须是 ≥1 的整数")
			return 1
		}
		if err := config.SetCapture(cfgPath, pc.Config.Capture.Mode, n, header); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "turn_interval 已设为 %d（%s）\n", n, cfgPath)
		return 0
	default:
		fmt.Fprintln(stderr, "用法: ok capture [propose|auto|interval <n>]")
		return 1
	}
}

// WikiCmd 处理 ok wiki status|mark|base|diff。
func WikiCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("wiki", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	cwd, _ := os.Getwd()

	switch fs.Arg(0) {
	case "status":
		st := wiki.CheckStatus(pc.Store.StateDir(), cwd, pc.Config.Wiki.StaleCommits)
		out := map[string]any{
			"project":   pc.Project.Name,
			"has_wiki":  st.HasWiki,
			"behind":    st.Behind,
			"stale":     st.Stale,
			"threshold": st.Threshold,
		}
		if st.LastCommit != "" {
			out["last_commit"] = st.LastCommit
		}
		if st.Branch != "" {
			out["branch"] = st.Branch
		}
		if st.BaseBranch != "" {
			out["base_branch"] = st.BaseBranch
		}
		if st.BranchState != "" {
			out["branch_state"] = st.BranchState
		}
		if st.MergeBase != "" {
			out["merge_base"] = st.MergeBase
		}
		// 已并入检测：仅在基准分支上、存在"tip 已并入且有差异条目"的分支时输出
		if st.Branch != "" && st.Branch == st.BaseBranch && st.BaseBranch != "" {
			if s := wiki.LoadState(pc.Store.StateDir()); s != nil {
				if db, err := index.Open(pc.Store.KbPath()); err == nil {
					merged := wiki.MergedIntoBase(s, cwd, func(b string) bool {
						ok, _ := db.HasBranchWiki(b)
						return ok
					})
					db.Close()
					if len(merged) > 0 {
						out["merged_branches"] = merged
					}
				}
			}
		}
		_ = json.NewEncoder(stdout).Encode(out)
		return 0
	case "mark":
		commit := fs.Arg(1)
		if commit == "" {
			commit, _ = wiki.HeadCommit(cwd) // 非 git 项目留空，只写时间戳
		}
		count := 0
		if db, err := index.Open(pc.Store.KbPath()); err == nil {
			if err := db.Sync(pc.Store.KnowledgeDir(), nil); err == nil {
				count, _ = db.WikiCount()
			}
			db.Close()
		}
		branch := wiki.CurrentBranch(cwd) // 非 git 为 ""：游标挂在 "" 键下（与旧单游标等价）
		s := wiki.LoadState(pc.Store.StateDir())
		if s == nil {
			s = &wiki.State{}
		}
		if s.Cursors == nil {
			s.Cursors = map[string]wiki.BranchCursor{}
		}
		// mark 即用户显式表态：游标归入当前分支，旧格式 Legacy 在此收敛（不落盘）
		s.Cursors[branch] = wiki.BranchCursor{LastCommit: commit, GeneratedAt: time.Now(), EntryCount: count}
		if s.BaseBranch == "" {
			s.BaseBranch = branch
		}
		if err := wiki.SaveState(pc.Store.StateDir(), s); err != nil {
			fmt.Fprintln(stderr, "写游标失败:", err)
			return 1
		}
		short := commit
		if len(short) > 7 {
			short = short[:7]
		}
		if short == "" {
			short = "(无 git)"
		}
		fmt.Fprintf(stdout, "已记录 wiki 游标 %s（%d 条 wiki 条目）\n", short, count)
		return 0
	case "base":
		s := wiki.LoadState(pc.Store.StateDir())
		name := fs.Arg(1)
		if name == "" {
			base := ""
			if s != nil {
				base = s.BaseBranch
			}
			if base == "" {
				fmt.Fprintln(stdout, "(未设置基准分支)")
			} else {
				fmt.Fprintln(stdout, base)
			}
			return 0
		}
		if s == nil {
			s = &wiki.State{}
		}
		// 旧格式文件首次写操作若是 base：先按 git 可达性把 legacy 游标归入当前
		// 分支（与 CheckStatus 同语义），绝不弄丢 last_commit（spec §4）；
		// 归属不可判时拒绝写盘，引导用户先在生成 wiki 的分支上 mark。
		if s.Legacy != nil && !wiki.AttributeLegacy(s, cwd) {
			short := s.Legacy.LastCommit
			if len(short) > 7 {
				short = short[:7]
			}
			if short == "" {
				short = "(空)"
			}
			fmt.Fprintf(stderr, "旧 wiki 游标（%s）与当前分支分叉，为避免丢失未写入；请先在生成 wiki 的分支上运行 ok wiki mark，或先切回该分支再设置基准。\n", short)
			return 1
		}
		s.BaseBranch = name
		if err := wiki.SaveState(pc.Store.StateDir(), s); err != nil {
			fmt.Fprintln(stderr, "写基准分支失败:", err)
			return 1
		}
		fmt.Fprintf(stdout, "基准分支已设为 %s\n", name)
		return 0
	case "diff":
		s := wiki.LoadState(pc.Store.StateDir())
		base := ""
		if s != nil {
			base = s.BaseBranch
		}
		out, err := wiki.DiffSummary(cwd, base)
		if err != nil {
			fmt.Fprintln(stderr, "diff 计算失败:", err)
			return 1
		}
		if out == "" {
			fmt.Fprintln(stdout, "无法计算分叉点（非 git / 未设基准分支 / 无共同祖先）")
			return 0
		}
		fmt.Fprint(stdout, out)
		return 0
	default:
		fmt.Fprintln(stderr, "用法: ok wiki <status|mark [commit]|base [分支名]|diff>")
		return 2
	}
}
