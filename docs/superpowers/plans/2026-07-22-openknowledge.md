# OpenKnowledge 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Go 单二进制 CLI `ok`：按项目隔离的 AI 知识库，通过 Kimi Code hooks 完成 SessionStart 注入、UserPromptSubmit 混合检索注入、PostToolUse+Stop 强制检查。

**Architecture:** 单一 Go 二进制，管理子命令（init/add/search/index/list/doctor）+ hook 入口（`ok hook <event>`，stdin 收事件 JSON，exit 0/2 响应）。知识集中存储于 `~/.openknowledge/projects/<name>/`，`registry.toml` 按 cwd 最长前缀匹配路由项目。

**Tech Stack:** Go ≥ 1.22；`github.com/BurntSushi/toml`、`gopkg.in/yaml.v3`、`github.com/bmatcuk/doublestar/v4`；embedding 走 OpenAI 兼容 HTTP API（标准库 net/http）。

**规格文档:** `docs/superpowers/specs/2026-07-22-openknowledge-design.md`

## Global Constraints

- Go 模块名 `openknowledge`，Go 版本 ≥ 1.22。
- 第三方依赖仅允许：`github.com/BurntSushi/toml`、`gopkg.in/yaml.v3`、`github.com/bmatcuk/doublestar/v4`。CLI 用标准库 `flag`，不引 cobra。
- hook stdin 字段名遵循 Kimi/Claude 约定：`hook_event_name`、`session_id`、`cwd`、`tool_name`、`tool_input.file_path`、`prompt`。最终以真实 Kimi Code 验证为准（Task 10 手动验收步骤）。
- hook 路径全面 fail-open：任何内部错误只写 `~/.openknowledge/ok.log`，exit 0。
- exit code 语义：0 = 放行（stdout 追加进上下文）；2 = 阻断（stderr 为原因）。
- 比较用路径一律经 `registry.NormalizePath`（`\`→`/`、全小写、去尾部 `/`）；`[[enforce]]` 的 glob 配置一律小写。
- token 估算：`字符数(rune) ÷ 2`。
- 知识库根目录：`OK_HOME` 环境变量优先，否则 `~/.openknowledge`（测试靠它隔离）。
- 每个 Task 完成标准：`go build ./... && go test ./...` 全绿，然后按步骤提交。

---

### Task 1: 项目脚手架 + registry 项目路由

**Files:**
- Create: `go.mod`（命令生成）
- Create: `.gitignore`
- Create: `internal/registry/registry.go`
- Test: `internal/registry/registry_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `registry.Home() string` — `OK_HOME` 优先，否则 `~/.openknowledge`
  - `registry.DefaultPath() string` — `Home()/registry.toml`
  - `registry.NormalizePath(p string) string`
  - `registry.Project{Name string; Paths []string}`
  - `registry.Registry{Projects []Project}`，`Load(path) (*Registry, error)`，`Save(path) error`，`FindByCwd(cwd) *Project`，`AddProject(name, path) error`

- [ ] **Step 1: 初始化模块与目录**

```bash
cd /d/develop/OpenKnowledge
go mod init openknowledge
go get github.com/BurntSushi/toml@latest
go get gopkg.in/yaml.v3@latest
go get github.com/bmatcuk/doublestar/v4@latest
mkdir -p internal/registry cmd/ok
```

`.gitignore` 内容：

```gitignore
/ok
/ok.exe
/bin/
```

- [ ] **Step 2: 写失败的测试** `internal/registry/registry_test.go`

```go
package registry

import (
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	got := NormalizePath(`D:\develop\OpenKnowledge\`)
	want := "d:/develop/openknowledge"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFindByCwdLongestPrefix(t *testing.T) {
	r := &Registry{Projects: []Project{
		{Name: "root", Paths: []string{`D:\develop`}},
		{Name: "ok", Paths: []string{`D:\develop\OpenKnowledge`}},
	}}
	p := r.FindByCwd(`d:\DEVELOP\OpenKnowledge\docs`)
	if p == nil || p.Name != "ok" {
		t.Fatalf("expected ok, got %+v", p)
	}
	if p := r.FindByCwd(`E:\other`); p != nil {
		t.Fatalf("expected nil, got %+v", p)
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.toml")
	r := &Registry{}
	if err := r.AddProject("ok", `D:\develop\OpenKnowledge`); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != 1 || loaded.Projects[0].Name != "ok" {
		t.Fatalf("unexpected %+v", loaded)
	}
	if err := loaded.AddProject("ok", `E:\x`); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestLoadMissing(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || len(r.Projects) != 0 {
		t.Fatalf("expected empty registry, got %+v err=%v", r, err)
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/registry/`
Expected: 编译失败，`undefined: NormalizePath` 等

- [ ] **Step 4: 实现** `internal/registry/registry.go`

```go
package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Project struct {
	Name  string   `toml:"name"`
	Paths []string `toml:"paths"`
}

type Registry struct {
	Projects []Project `toml:"project"`
}

// Home 返回知识库根目录：OK_HOME 环境变量优先，否则 ~/.openknowledge。
func Home() string {
	if h := os.Getenv("OK_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".openknowledge"
	}
	return filepath.Join(home, ".openknowledge")
}

func DefaultPath() string { return filepath.Join(Home(), "registry.toml") }

// NormalizePath 统一路径用于比较：分隔符转为 "/"，转小写，去掉尾部 "/"。
func NormalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	return strings.ToLower(p)
}

func Load(path string) (*Registry, error) {
	r := &Registry{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	return r, nil
}

func (r *Registry) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(r); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

// FindByCwd 按规范化路径最长前缀匹配项目；未命中返回 nil。
func (r *Registry) FindByCwd(cwd string) *Project {
	ncwd := NormalizePath(cwd)
	var best *Project
	bestLen := -1
	for i := range r.Projects {
		for _, p := range r.Projects[i].Paths {
			np := NormalizePath(p)
			if ncwd == np || strings.HasPrefix(ncwd, np+"/") {
				if len(np) > bestLen {
					bestLen = len(np)
					best = &r.Projects[i]
				}
			}
		}
	}
	return best
}

func (r *Registry) AddProject(name, path string) error {
	for _, p := range r.Projects {
		if p.Name == name {
			return fmt.Errorf("项目 %q 已存在", name)
		}
	}
	r.Projects = append(r.Projects, Project{Name: name, Paths: []string{path}})
	return nil
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/registry/ -v`
Expected: 4 个测试全部 PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum .gitignore internal/registry/
git commit -m "feat: scaffold module and registry project routing"
```

---

### Task 2: entry 知识条目解析

**Files:**
- Create: `internal/entry/entry.go`
- Test: `internal/entry/entry_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `entry.Entry{Title, Type string; Tags []string; Mandatory bool; Summary, Body, Path string}`
  - `entry.Parse(content []byte) (*Entry, error)`（容忍 CRLF 与 BOM）
  - `(e *Entry) Serialize() []byte`
  - `entry.Load(dir string) ([]*Entry, error)`
  - `entry.Slug(title string) string`
  - `(e *Entry) FileName() string`
  - `(e *Entry) EmbedText() string`
  - `entry.ValidType(t string) bool`（rule|pitfall|note|reference）

- [ ] **Step 1: 写失败的测试** `internal/entry/entry_test.go`

```go
package entry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `---
title: 变更日志强制规则
type: rule
tags:
  - changelog
  - workflow
mandatory: true
summary: 每次代码修改必须立即记录变更日志
---

正文内容
`

func TestParse(t *testing.T) {
	e, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "变更日志强制规则" || e.Type != "rule" || !e.Mandatory {
		t.Fatalf("unexpected %+v", e)
	}
	if len(e.Tags) != 2 || e.Tags[0] != "changelog" {
		t.Fatalf("tags %+v", e.Tags)
	}
	if e.Body != "正文内容" {
		t.Fatalf("body %q", e.Body)
	}
}

func TestParseCRLF(t *testing.T) {
	e, err := Parse([]byte(strings.ReplaceAll(sample, "\n", "\r\n")))
	if err != nil || e.Body != "正文内容" {
		t.Fatalf("crlf parse: %v body=%q", err, e.Body)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("no frontmatter")); err == nil {
		t.Fatal("expected error")
	}
	bad := "---\ntitle: x\ntype: bogus\n---\nbody\n"
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected type error")
	}
}

func TestSerializeRoundtrip(t *testing.T) {
	e, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	e2, err := Parse(e.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if e2.Title != e.Title || e2.Body != e.Body || len(e2.Tags) != len(e.Tags) || e2.Mandatory != e.Mandatory {
		t.Fatalf("roundtrip mismatch %+v", e2)
	}
}

func TestSlug(t *testing.T) {
	if got := Slug(`a/b:c d`); got != "abc-d" {
		t.Fatalf("slug %q", got)
	}
	if got := Slug("变更日志 规则"); got != "变更日志-规则" {
		t.Fatalf("slug %q", got)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path == "" || entries[0].FileName() != "b.md" {
		t.Fatalf("unexpected %+v", entries)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/entry/`
Expected: 编译失败，`undefined: Parse` 等

- [ ] **Step 3: 实现** `internal/entry/entry.go`

```go
package entry

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Entry struct {
	Title     string   `yaml:"title"`
	Type      string   `yaml:"type"`
	Tags      []string `yaml:"tags"`
	Mandatory bool     `yaml:"mandatory"`
	Summary   string   `yaml:"summary"`
	Body      string   `yaml:"-"`
	Path      string   `yaml:"-"`
}

var validTypes = map[string]bool{"rule": true, "pitfall": true, "note": true, "reference": true}

func ValidType(t string) bool { return validTypes[t] }

// Parse 解析 "---\n<yaml>\n---\n<body>" 格式的条目文件；容忍 CRLF 与 UTF-8 BOM。
func Parse(content []byte) (*Entry, error) {
	s := strings.TrimPrefix(string(content), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, fmt.Errorf("缺少 frontmatter 起始 ---")
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	delim := "\n---\n"
	if end < 0 && strings.HasSuffix(rest, "\n---") {
		end = len(rest) - len("\n---")
		delim = "\n---"
	}
	if end < 0 {
		return nil, fmt.Errorf("缺少 frontmatter 结束 ---")
	}
	e := &Entry{}
	if err := yaml.Unmarshal([]byte(rest[:end]), e); err != nil {
		return nil, fmt.Errorf("解析 frontmatter: %w", err)
	}
	if e.Title == "" {
		return nil, fmt.Errorf("缺少 title")
	}
	if !ValidType(e.Type) {
		return nil, fmt.Errorf("非法 type %q（rule|pitfall|note|reference）", e.Type)
	}
	e.Body = strings.TrimSpace(rest[end+len(delim):])
	return e, nil
}

func (e *Entry) Serialize() []byte {
	fm, err := yaml.Marshal(e)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n\n")
	buf.WriteString(e.Body)
	buf.WriteString("\n")
	return buf.Bytes()
}

// Load 读取目录下全部 .md 条目，按文件名排序。
func Load(dir string) ([]*Entry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var entries []*Entry
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		e, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		e.Path = m
		entries = append(entries, e)
	}
	return entries, nil
}

// Slug 将标题转为安全文件名（不含扩展名）。
func Slug(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, " ", "-")
	return strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return -1
		}
		return r
	}, title)
}

// FileName 返回条目在磁盘上的文件名。
func (e *Entry) FileName() string {
	if e.Path != "" {
		return filepath.Base(e.Path)
	}
	return Slug(e.Title) + ".md"
}

// EmbedText 是计算 embedding 时使用的文本。
func (e *Entry) EmbedText() string {
	return e.Title + "\n" + e.Summary + "\n" + e.Body
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/entry/ -v`
Expected: 6 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/entry/
git commit -m "feat: knowledge entry frontmatter parsing"
```

---

### Task 3: config 项目配置加载

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `config.Embedding{BaseURL, APIKeyEnv, Model string; TimeoutSec int}`
  - `config.Inject{MaxTokens int}`
  - `config.Retrieve{Alpha, Beta float64; TopN int}`
  - `config.EnforceRule{Type string; CodeGlobs []string; ChangelogGlob, Message string}`
  - `config.Config{Embedding, Inject, Retrieve; Enforce []EnforceRule}`
  - `config.Default() Config`（1500 / top_n=3 / α=β=1.0 / timeout=5）
  - `config.Load(path string) (Config, error)`：文件不存在返回 Default；缺省字段用默认值填充

- [ ] **Step 1: 写失败的测试** `internal/config/config_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inject.MaxTokens != 1500 || cfg.Retrieve.TopN != 3 || cfg.Embedding.TimeoutSec != 5 {
		t.Fatalf("unexpected defaults %+v", cfg)
	}
}

func TestLoadMergesOverDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[retrieve]
top_n = 5

[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.TopN != 5 || cfg.Retrieve.Alpha != 1.0 {
		t.Fatalf("merge failed %+v", cfg.Retrieve)
	}
	if len(cfg.Enforce) != 1 || cfg.Enforce[0].ChangelogGlob != "docs/changelogs/**" {
		t.Fatalf("enforce %+v", cfg.Enforce)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/`
Expected: 编译失败，`undefined: Load`

- [ ] **Step 3: 实现** `internal/config/config.go`

```go
package config

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

type Embedding struct {
	BaseURL    string `toml:"base_url"`
	APIKeyEnv  string `toml:"api_key_env"`
	Model      string `toml:"model"`
	TimeoutSec int    `toml:"timeout_sec"`
}

type Inject struct {
	MaxTokens int `toml:"max_tokens"`
}

type Retrieve struct {
	Alpha float64 `toml:"alpha"`
	Beta  float64 `toml:"beta"`
	TopN  int     `toml:"top_n"`
}

type EnforceRule struct {
	Type          string   `toml:"type"`
	CodeGlobs     []string `toml:"code_globs"`
	ChangelogGlob string   `toml:"changelog_glob"`
	Message       string   `toml:"message"`
}

type Config struct {
	Embedding Embedding     `toml:"embedding"`
	Inject    Inject        `toml:"inject"`
	Retrieve  Retrieve      `toml:"retrieve"`
	Enforce   []EnforceRule `toml:"enforce"`
}

func Default() Config {
	return Config{
		Embedding: Embedding{TimeoutSec: 5},
		Inject:    Inject{MaxTokens: 1500},
		Retrieve:  Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 3},
	}
}

// Load 读取配置；文件不存在返回 Default，缺省字段用默认值填充。
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: 2 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: project config loading with defaults"
```

---

### Task 4: store 存储布局与 INDEX 生成

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `entry.Entry`
- Produces:
  - `store.New(root string) *Store`
  - `KnowledgeDir() / IndexPath() / VectorsPath() / StateDir() / ConfigPath() string`
  - `(s *Store) EnsureDirs() error`
  - `store.IndexContent(entries []*entry.Entry) string`
  - `(s *Store) RebuildIndex(entries []*entry.Entry) error`
  - `store.EstimateTokens(s string) int`（rune 数 ÷ 2）
  - `store.TruncateToBudget(s string, maxTokens int) string`

- [ ] **Step 1: 写失败的测试** `internal/store/store_test.go`

```go
package store

import (
	"os"
	"strings"
	"testing"

	"openknowledge/internal/entry"
)

func TestIndexContent(t *testing.T) {
	entries := []*entry.Entry{
		{Title: "规则A", Type: "rule", Tags: []string{"a", "b"}, Summary: "摘要A"},
	}
	got := IndexContent(entries)
	if !strings.Contains(got, "**规则A** (rule) [a, b] — 摘要A") {
		t.Fatalf("unexpected index %q", got)
	}
}

func TestTruncateToBudget(t *testing.T) {
	s := strings.Repeat("好", 100)
	got := TruncateToBudget(s, 10) // 预算 20 runes
	if !strings.HasSuffix(got, "…(已截断)") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	if got := TruncateToBudget("short", 100); got != "short" {
		t.Fatalf("short text should pass through, got %q", got)
	}
}

func TestRebuildIndex(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if err := s.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	entries := []*entry.Entry{{Title: "A", Type: "note", Summary: "s"}}
	if err := s.RebuildIndex(entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.IndexPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "**A**") {
		t.Fatalf("unexpected %q", data)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/`
Expected: 编译失败，`undefined: New` 等

- [ ] **Step 3: 实现** `internal/store/store.go`

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"openknowledge/internal/entry"
)

type Store struct{ Root string }

func New(root string) *Store { return &Store{Root: root} }

func (s *Store) KnowledgeDir() string { return filepath.Join(s.Root, "knowledge") }
func (s *Store) IndexPath() string    { return filepath.Join(s.Root, "INDEX.md") }
func (s *Store) VectorsPath() string  { return filepath.Join(s.Root, "vectors.json") }
func (s *Store) StateDir() string     { return filepath.Join(s.Root, "state") }
func (s *Store) ConfigPath() string   { return filepath.Join(s.Root, "config.toml") }

func (s *Store) EnsureDirs() error {
	for _, d := range []string{s.KnowledgeDir(), s.StateDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// IndexContent 生成轻量索引文本（标题+类型+tags+摘要）。
func IndexContent(entries []*entry.Entry) string {
	var b strings.Builder
	b.WriteString("# 知识索引\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", e.Title, e.Type, strings.Join(e.Tags, ", "), e.Summary)
	}
	return b.String()
}

func (s *Store) RebuildIndex(entries []*entry.Entry) error {
	return os.WriteFile(s.IndexPath(), []byte(IndexContent(entries)), 0o644)
}

// EstimateTokens 按字符数 ÷ 2 保守估算 token 数。
func EstimateTokens(s string) int { return utf8.RuneCountInString(s) / 2 }

// TruncateToBudget 将文本截断到 token 预算内（按 rune 安全截断）。
func TruncateToBudget(s string, maxTokens int) string {
	maxRunes := maxTokens * 2
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "\n…(已截断)"
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/store/ -v`
Expected: 3 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: store layout and INDEX generation"
```

---

### Task 5: embed 客户端与向量缓存

**Files:**
- Create: `internal/embed/embed.go`
- Create: `internal/embed/vectors.go`
- Test: `internal/embed/embed_test.go`

**Interfaces:**
- Consumes: `entry.Entry`
- Produces:
  - `embed.Client` 接口：`Embed(ctx context.Context, text string) ([]float32, error)`
  - `embed.OpenAIClient{BaseURL, APIKey, Model string; Timeout time.Duration}`（实现 Client，POST `{BaseURL}/embeddings`）
  - `embed.Cosine(a, b []float32) float64`
  - `embed.EntryVector{ModTime int64; Vector []float32}`
  - `embed.VectorSet{Vectors map[string]*EntryVector}`，`LoadVectors(path)`、`Save(path)`、`Update(ctx, Client, entries) error`（按 mtime 增量，清理已删条目）

- [ ] **Step 1: 写失败的测试** `internal/embed/embed_test.go`

```go
package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/entry"
)

func newFakeServer(t *testing.T, vec []float32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	}))
}

func TestOpenAIClientEmbed(t *testing.T) {
	srv := newFakeServer(t, []float32{1, 2, 3})
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: 5 * time.Second}
	vec, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 || vec[0] != 1 {
		t.Fatalf("unexpected %v", vec)
	}
}

func TestCosine(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{1, 0}); got != 1 {
		t.Fatalf("got %v", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); got != 0 {
		t.Fatalf("got %v", got)
	}
	if got := Cosine([]float32{}, []float32{}); got != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestVectorSetUpdate(t *testing.T) {
	srv := newFakeServer(t, []float32{0.5, 0.5})
	defer srv.Close()
	c := &OpenAIClient{BaseURL: srv.URL, Model: "m"}

	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &entry.Entry{Title: "A", Path: p}
	vs := &VectorSet{Vectors: map[string]*EntryVector{}}
	if err := vs.Update(context.Background(), c, []*entry.Entry{e}); err != nil {
		t.Fatal(err)
	}
	if len(vs.Vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vs.Vectors))
	}
	// 再次调用应命中缓存（mtime 未变）
	if err := vs.Update(context.Background(), c, []*entry.Entry{e}); err != nil {
		t.Fatal(err)
	}
	// 条目删除后向量被清理
	if err := vs.Update(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	if len(vs.Vectors) != 0 {
		t.Fatal("expected cleanup")
	}
}

func TestLoadVectorsMissing(t *testing.T) {
	vs, err := LoadVectors(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || len(vs.Vectors) != 0 {
		t.Fatalf("got %+v err=%v", vs, err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/embed/`
Expected: 编译失败，`undefined: OpenAIClient` 等

- [ ] **Step 3: 实现** `internal/embed/embed.go`

```go
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

type Client interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type OpenAIClient struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *OpenAIClient) Embed(ctx context.Context, text string) ([]float32, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	body, err := json.Marshal(embedRequest{Model: c.Model, Input: text})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding API %d: %s", resp.StatusCode, msg)
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Data) == 0 || len(er.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding API 返回空向量")
	}
	return er.Data[0].Embedding, nil
}

// Cosine 计算余弦相似度；任一零向量或长度不等返回 0。
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
```

`internal/embed/vectors.go`

```go
package embed

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"openknowledge/internal/entry"
)

type EntryVector struct {
	ModTime int64     `json:"mod_time"`
	Vector  []float32 `json:"vector"`
}

type VectorSet struct {
	Vectors map[string]*EntryVector `json:"vectors"`
}

func LoadVectors(path string) (*VectorSet, error) {
	vs := &VectorSet{Vectors: map[string]*EntryVector{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return vs, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, vs); err != nil {
		return nil, err
	}
	return vs, nil
}

func (vs *VectorSet) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(vs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Update 按文件 mtime 增量更新条目向量，并清理已删除条目的向量。
func (vs *VectorSet) Update(ctx context.Context, c Client, entries []*entry.Entry) error {
	alive := map[string]bool{}
	for _, e := range entries {
		name := e.FileName()
		alive[name] = true
		fi, err := os.Stat(e.Path)
		if err != nil {
			return err
		}
		mtime := fi.ModTime().Unix()
		if v, ok := vs.Vectors[name]; ok && v.ModTime == mtime {
			continue
		}
		vec, err := c.Embed(ctx, e.EmbedText())
		if err != nil {
			return err
		}
		vs.Vectors[name] = &EntryVector{ModTime: mtime, Vector: vec}
	}
	for name := range vs.Vectors {
		if !alive[name] {
			delete(vs.Vectors, name)
		}
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/embed/ -v`
Expected: 4 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/embed/
git commit -m "feat: OpenAI-compatible embedding client and vector cache"
```

---

### Task 6: retrieve 混合检索

**Files:**
- Create: `internal/retrieve/retrieve.go`
- Test: `internal/retrieve/retrieve_test.go`

**Interfaces:**
- Consumes: `entry.Entry`、`embed.VectorSet`、`embed.Cosine`、`config.Retrieve`
- Produces:
  - `retrieve.Scored{Entry *entry.Entry; Score float64}`
  - `retrieve.Terms(s string) []string`（小写拉丁/数字词 ≥2 字符 + CJK 二元组）
  - `retrieve.KeywordScore(query string, e *entry.Entry) float64`（tag +3 / title +2 / summary +1）
  - `retrieve.Rank(entries, query string, queryVec []float32, vs *embed.VectorSet, cfg config.Retrieve) []Scored`（mandatory 排除；queryVec=nil 退化为纯关键词；只留 score>0；分数降序、同分标题升序；截 top_n）

- [ ] **Step 1: 写失败的测试** `internal/retrieve/retrieve_test.go`

```go
package retrieve

import (
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
)

func TestTerms(t *testing.T) {
	got := Terms("Git 提交规范")
	want := map[string]bool{"git": true, "提交": true, "交规": true, "规范": true}
	if len(got) != len(want) {
		t.Fatalf("terms %v", got)
	}
	for _, term := range got {
		if !want[term] {
			t.Fatalf("unexpected term %q in %v", term, got)
		}
	}
}

func TestKeywordScore(t *testing.T) {
	e := &entry.Entry{Title: "Git 提交规范", Tags: []string{"git"}, Summary: "提交信息怎么写"}
	if s := KeywordScore("git 提交 规范", e); s <= 0 {
		t.Fatalf("expected positive, got %v", s)
	}
	if s := KeywordScore("zzz qqq", e); s != 0 {
		t.Fatalf("expected 0, got %v", s)
	}
}

func TestRankHybridAndTopN(t *testing.T) {
	entries := []*entry.Entry{
		{Title: "不相关", Type: "note", Path: "a.md", Summary: "无"},
		{Title: "Git 提交规范", Type: "rule", Tags: []string{"git"}, Path: "b.md", Summary: "提交信息"},
		{Title: "构建命令", Type: "note", Path: "c.md", Summary: "构建"},
		{Title: "强制规则", Type: "rule", Mandatory: true, Path: "d.md", Summary: "git"},
	}
	vs := &embed.VectorSet{Vectors: map[string]*embed.EntryVector{
		"c.md": {ModTime: 1, Vector: []float32{1, 0}},
	}}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 2}
	got := Rank(entries, "git 提交", []float32{1, 0}, vs, cfg)
	if len(got) != 2 {
		t.Fatalf("expected top 2, got %d (%+v)", len(got), got)
	}
	for _, s := range got {
		if s.Entry.Mandatory {
			t.Fatal("mandatory entry must be excluded")
		}
	}
	// 纯关键词退化：queryVec=nil 时仍能按关键词命中
	got = Rank(entries, "git 提交", nil, vs, cfg)
	if len(got) == 0 || got[0].Entry.Title != "Git 提交规范" {
		t.Fatalf("degraded ranking wrong %+v", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/retrieve/`
Expected: 编译失败，`undefined: Terms` 等

- [ ] **Step 3: 实现** `internal/retrieve/retrieve.go`

```go
package retrieve

import (
	"sort"
	"strings"
	"unicode"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
)

type Scored struct {
	Entry *entry.Entry
	Score float64
}

// Terms 将文本切分为检索词：小写拉丁/数字词（≥2 字符）与 CJK 二元组。
func Terms(s string) []string {
	var terms []string
	var latin []rune
	var cjk []rune
	flushLatin := func() {
		if len(latin) >= 2 {
			terms = append(terms, string(latin))
		}
		latin = latin[:0]
	}
	flushCJK := func() {
		if len(cjk) == 1 {
			terms = append(terms, string(cjk))
		}
		for i := 0; i+1 < len(cjk); i++ {
			terms = append(terms, string(cjk[i:i+2]))
		}
		cjk = cjk[:0]
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin = append(latin, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return terms
}

// KeywordScore：tag 命中 +3，title 命中 +2，summary 命中 +1。
func KeywordScore(query string, e *entry.Entry) float64 {
	qt := map[string]bool{}
	for _, t := range Terms(query) {
		qt[t] = true
	}
	var score float64
	for _, tag := range e.Tags {
		for _, t := range Terms(tag) {
			if qt[t] {
				score += 3
			}
		}
	}
	for _, t := range Terms(e.Title) {
		if qt[t] {
			score += 2
		}
	}
	for _, t := range Terms(e.Summary) {
		if qt[t] {
			score += 1
		}
	}
	return score
}

// Rank 混合打分排序：score = alpha·关键词 + beta·语义。queryVec 为 nil 时退化为纯关键词。
// mandatory 条目不参与。仅返回 score > 0 的前 cfg.TopN 条。
func Rank(entries []*entry.Entry, query string, queryVec []float32, vs *embed.VectorSet, cfg config.Retrieve) []Scored {
	var out []Scored
	for _, e := range entries {
		if e.Mandatory {
			continue
		}
		score := cfg.Alpha * KeywordScore(query, e)
		if queryVec != nil && vs != nil {
			if v, ok := vs.Vectors[e.FileName()]; ok {
				score += cfg.Beta * embed.Cosine(queryVec, v.Vector)
			}
		}
		if score > 0 {
			out = append(out, Scored{Entry: e, Score: score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Entry.Title < out[j].Entry.Title
	})
	if cfg.TopN > 0 && len(out) > cfg.TopN {
		out = out[:cfg.TopN]
	}
	return out
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/retrieve/ -v`
Expected: 3 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/retrieve/
git commit -m "feat: hybrid keyword+semantic retrieval"
```

---

### Task 7: state 会话状态 + enforce 规则判定

**Files:**
- Create: `internal/state/state.go`
- Create: `internal/enforce/enforce.go`
- Test: `internal/state/state_test.go`
- Test: `internal/enforce/enforce_test.go`

**Interfaces:**
- Consumes: `config.EnforceRule`
- Produces:
  - `state.Session{SessionID string; Touched, BlockedRules []string}`
  - `state.Load(dir, sessionID string) *Session`（不存在返回空状态；sessionID 净化为安全文件名 `session-<id>.json`）
  - `(s *Session) Save(dir string) error`、`AddTouched(p string)`（去重）、`HasBlocked(ruleType string) bool`、`MarkBlocked(ruleType string)`
  - `state.Clean(dir string, maxAge time.Duration) error`
  - `enforce.EvalChangelog(rule config.EnforceRule, st *state.Session) (block bool, reason string)`

- [ ] **Step 1: 写失败的测试** `internal/state/state_test.go`

```go
package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir, "abc/123") // 含非法字符 → 文件名被净化
	s.AddTouched("a.go")
	s.AddTouched("a.go")
	s.MarkBlocked("changelog_required")
	if len(s.Touched) != 1 {
		t.Fatal("dedupe failed")
	}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	s2 := Load(dir, "abc/123")
	if !s2.HasBlocked("changelog_required") || len(s2.Touched) != 1 {
		t.Fatalf("unexpected %+v", s2)
	}
}

func TestClean(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "session-old.json")
	if err := os.WriteFile(old, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "session-new.json")
	if err := os.WriteFile(fresh, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Clean(dir, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old state should be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh state should remain")
	}
}
```

`internal/enforce/enforce_test.go`

```go
package enforce

import (
	"testing"

	"openknowledge/internal/config"
	"openknowledge/internal/state"
)

var rule = config.EnforceRule{
	Type:          "changelog_required",
	CodeGlobs:     []string{"**/*.go"},
	ChangelogGlob: "docs/changelogs/**",
	Message:       "请补变更日志",
}

func TestBlockWhenCodeTouchedWithoutChangelog(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	block, reason := EvalChangelog(rule, st)
	if !block || reason != "请补变更日志" {
		t.Fatalf("expected block, got %v %q", block, reason)
	}
}

func TestNoBlockWhenChangelogTouched(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	st.AddTouched("docs/changelogs/2026-07-22.md")
	block, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("expected no block")
	}
}

func TestNoBlockWhenNoCodeTouched(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("README.md")
	block, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("expected no block")
	}
}

func TestBlockOnlyOncePerSession(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	st.MarkBlocked("changelog_required")
	block, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("rule already blocked once this session")
	}
}

func TestRootLevelGoFileMatches(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("main.go")
	block, _ := EvalChangelog(rule, st)
	if !block {
		t.Fatal("**/*.go should match root-level main.go")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/state/ ./internal/enforce/`
Expected: 编译失败，`undefined: Load` / `undefined: EvalChangelog`

- [ ] **Step 3: 实现** `internal/state/state.go`

```go
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Session struct {
	SessionID    string   `json:"session_id"`
	Touched      []string `json:"touched"`
	BlockedRules []string `json:"blocked_rules"`
}

func fileName(sessionID string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, sessionID)
	if clean == "" {
		clean = "unknown"
	}
	return "session-" + clean + ".json"
}

// Load 读取会话状态；不存在或损坏时返回空状态。
func Load(dir, sessionID string) *Session {
	s := &Session{SessionID: sessionID}
	data, err := os.ReadFile(filepath.Join(dir, fileName(sessionID)))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	s.SessionID = sessionID
	return s
}

func (s *Session) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fileName(s.SessionID)), data, 0o644)
}

func (s *Session) AddTouched(p string) {
	for _, t := range s.Touched {
		if t == p {
			return
		}
	}
	s.Touched = append(s.Touched, p)
}

func (s *Session) HasBlocked(ruleType string) bool {
	for _, b := range s.BlockedRules {
		if b == ruleType {
			return true
		}
	}
	return false
}

func (s *Session) MarkBlocked(ruleType string) {
	if !s.HasBlocked(ruleType) {
		s.BlockedRules = append(s.BlockedRules, ruleType)
	}
}

// Clean 删除 dir 中 mtime 早于 maxAge 的会话状态文件。
func Clean(dir string, maxAge time.Duration) error {
	matches, err := filepath.Glob(filepath.Join(dir, "session-*.json"))
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
	return nil
}
```

`internal/enforce/enforce.go`

```go
package enforce

import (
	"github.com/bmatcuk/doublestar/v4"

	"openknowledge/internal/config"
	"openknowledge/internal/state"
)

// EvalChangelog 判定 changelog_required 规则：触碰过 code_globs 且未触碰
// changelog_glob → 阻断（同会话同规则已被阻断过则放行，防死循环）。
func EvalChangelog(rule config.EnforceRule, st *state.Session) (block bool, reason string) {
	if st.HasBlocked(rule.Type) {
		return false, ""
	}
	code := false
	for _, p := range st.Touched {
		if ok, _ := doublestar.Match(rule.ChangelogGlob, p); ok {
			return false, ""
		}
		if !code {
			for _, g := range rule.CodeGlobs {
				if ok, _ := doublestar.Match(g, p); ok {
					code = true
					break
				}
			}
		}
	}
	if !code {
		return false, ""
	}
	return true, rule.Message
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/state/ ./internal/enforce/ -v`
Expected: 7 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/state/ internal/enforce/
git commit -m "feat: session state tracking and changelog enforcement rule"
```

---

### Task 8: project 解析 + hook 四个事件入口

**Files:**
- Create: `internal/project/project.go`
- Create: `internal/hook/hook.go`
- Test: `internal/project/project_test.go`
- Test: `internal/hook/hook_test.go`

**Interfaces:**
- Consumes: 之前全部包的接口
- Produces:
  - `project.Context{Project *registry.Project; Store *store.Store; Config config.Config}`
  - `project.FromCwd(cwd string) (*Context, error)`
  - `hook.Event{HookEventName, SessionID, Cwd, ToolName string; ToolInput json.RawMessage; Prompt string}`
  - `hook.ParseEvent(r io.Reader) (*Event, error)`
  - `(e *Event) FilePath() string`
  - `hook.HandleSessionStart(r io.Reader, w io.Writer) int`
  - `hook.HandlePrompt(r io.Reader, w io.Writer) int`
  - `hook.HandlePostTool(r io.Reader) int`
  - `hook.HandleStop(r io.Reader, stderr io.Writer) int`

- [ ] **Step 1: 写失败的测试** `internal/project/project_test.go`

```go
package project

import (
	"os"
	"path/filepath"
	"testing"

	"openknowledge/internal/registry"
)

func TestFromCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	proj := filepath.Join(home, "work", "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "demo", Paths: []string{proj}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	ctx, err := FromCwd(filepath.Join(proj, "sub", "dir"))
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Project.Name != "demo" {
		t.Fatalf("unexpected project %+v", ctx.Project)
	}
	if ctx.Config.Inject.MaxTokens != 1500 {
		t.Fatalf("expected default config, got %+v", ctx.Config)
	}
	if _, err := FromCwd(filepath.Join(home, "nowhere")); err == nil {
		t.Fatal("expected error for unregistered dir")
	}
}
```

`internal/hook/hook_test.go`

```go
package hook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/registry"
)

// setupProject 在临时 OK_HOME 下注册项目并返回项目目录与 KB 根。
func setupProject(t *testing.T) (projDir, kbRoot string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	projDir = filepath.Join(home, "work", "demo")
	kbRoot = filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(filepath.Join(kbRoot, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(kbRoot, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "demo", Paths: []string{projDir}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	return projDir, kbRoot
}

func writeEntry(t *testing.T, kbRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(kbRoot, "knowledge", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const mandatoryEntry = `---
title: 变更日志强制规则
type: rule
mandatory: true
summary: 改代码必须写日志
---

改完代码先写日志。
`

const gitEntry = `---
title: Git 提交规范
type: note
tags: [git]
summary: 提交信息格式
---

使用 Conventional Commits。
`

func TestSessionStartInjectsMandatoryAndIndex(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "rule.md", mandatoryEntry)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	if err := os.WriteFile(filepath.Join(kbRoot, "INDEX.md"), []byte("# 知识索引\n\n- **Git 提交规范**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"SessionStart","session_id":"s1","cwd":%q}`, projDir)
	var out bytes.Buffer
	if code := HandleSessionStart(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "改完代码先写日志。") || !strings.Contains(got, "知识索引") {
		t.Fatalf("unexpected output %q", got)
	}
	if strings.Contains(got, "Conventional Commits") {
		t.Fatal("non-mandatory body should not be injected at session start")
	}
}

func TestPromptKeywordFallback(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交规范是什么"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "Conventional Commits") {
		t.Fatalf("expected git entry injected, got %q", out.String())
	}
}

func TestPromptUnregisteredProjectSilent(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	in := `{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"/nowhere","prompt":"git"}`
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 || out.Len() != 0 {
		t.Fatalf("expected silent 0, got %d %q", code, out.String())
	}
}

func TestPostToolAndStopEnforcement(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	cfg := `
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	codeFile := filepath.Join(projDir, "main.go")
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s1","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, projDir, codeFile)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("post-tool exit %d", code)
	}
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s1","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 2 {
		t.Fatalf("expected block(2), got %d", code)
	}
	if !strings.Contains(stderr.String(), "请补变更日志") {
		t.Fatalf("missing message %q", stderr.String())
	}
	// 第二次 Stop 放行（防死循环）
	stderr.Reset()
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 0 {
		t.Fatalf("expected pass on second stop, got %d", code)
	}
	// 新会话：触碰代码 + 触碰变更日志 → 放行
	cl := filepath.Join(projDir, "docs", "changelogs", "2026-07-22.md")
	post2 := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s2","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, projDir, codeFile)
	_ = HandlePostTool(strings.NewReader(post2))
	post3 := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s2","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, projDir, cl)
	_ = HandlePostTool(strings.NewReader(post3))
	stderr.Reset()
	stop2 := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s2","cwd":%q}`, projDir)
	if code := HandleStop(strings.NewReader(stop2), &stderr); code != 0 {
		t.Fatalf("expected pass after changelog, got %d (%q)", code, stderr.String())
	}
}

func TestStopWithoutEnforceRulesPass(t *testing.T) {
	projDir, _ := setupProject(t)
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s3","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 0 {
		t.Fatalf("expected 0 without enforce rules, got %d", code)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/project/ ./internal/hook/`
Expected: 编译失败，`undefined: FromCwd` / `undefined: HandleSessionStart` 等

- [ ] **Step 3: 实现** `internal/project/project.go`

```go
package project

import (
	"fmt"
	"os"
	"path/filepath"

	"openknowledge/internal/config"
	"openknowledge/internal/registry"
	"openknowledge/internal/store"
)

type Context struct {
	Project *registry.Project
	Store   *store.Store
	Config  config.Config
}

// FromCwd 按目录解析已注册项目；未注册返回错误。
func FromCwd(cwd string) (*Context, error) {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return nil, err
	}
	p := reg.FindByCwd(cwd)
	if p == nil {
		return nil, fmt.Errorf("目录未注册为知识库项目: %s", cwd)
	}
	st := store.New(filepath.Join(registry.Home(), "projects", p.Name))
	cfg, err := config.Load(st.ConfigPath())
	if err != nil {
		return nil, err
	}
	return &Context{Project: p, Store: st, Config: cfg}, nil
}

// FromCurrentDir 以进程当前目录解析。
func FromCurrentDir() (*Context, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return FromCwd(cwd)
}
```

`internal/hook/hook.go`

```go
package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/enforce"
	"openknowledge/internal/entry"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/state"
	"openknowledge/internal/store"
)

type Event struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Prompt        string          `json:"prompt"`
}

func ParseEvent(r io.Reader) (*Event, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, err
	}
	e := &Event{}
	if err := json.Unmarshal(data, e); err != nil {
		return nil, err
	}
	return e, nil
}

// FilePath 从 tool_input 提取文件路径（Write/Edit 工具）。
func (e *Event) FilePath() string {
	var ti struct {
		FilePath string `json:"file_path"`
	}
	if len(e.ToolInput) > 0 {
		_ = json.Unmarshal(e.ToolInput, &ti)
	}
	return ti.FilePath
}

func logErr(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(registry.Home(), "ok.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("2006-01-02 15:04:05 ")+format+"\n", args...)
}

// HandleSessionStart 注入 mandatory 条目全文 + 索引；顺带清理过期会话状态。
func HandleSessionStart(r io.Reader, w io.Writer) int {
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("session-start parse: %v", err)
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	_ = state.Clean(pc.Store.StateDir(), 7*24*time.Hour)
	entries, err := entry.Load(pc.Store.KnowledgeDir())
	if err != nil {
		logErr("session-start load entries: %v", err)
		return 0
	}
	var b strings.Builder
	for _, e := range entries {
		if !e.Mandatory {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", e.Title, e.Body)
	}
	if idx, err := os.ReadFile(pc.Store.IndexPath()); err == nil {
		b.Write(idx)
	}
	out := store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens)
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(w, out)
	}
	return 0
}

// HandlePrompt 混合检索并注入 top-N 条目；embedding 失败降级为关键词检索。
func HandlePrompt(r io.Reader, w io.Writer) int {
	ev, err := ParseEvent(r)
	if err != nil || strings.TrimSpace(ev.Prompt) == "" {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	entries, err := entry.Load(pc.Store.KnowledgeDir())
	if err != nil {
		logErr("prompt load entries: %v", err)
		return 0
	}
	vs, err := embed.LoadVectors(pc.Store.VectorsPath())
	if err != nil {
		logErr("prompt load vectors: %v", err)
		vs = nil
	}
	var queryVec []float32
	if key := os.Getenv(pc.Config.Embedding.APIKeyEnv); key != "" && pc.Config.Embedding.BaseURL != "" {
		client := &embed.OpenAIClient{
			BaseURL: pc.Config.Embedding.BaseURL,
			APIKey:  key,
			Model:   pc.Config.Embedding.Model,
			Timeout: time.Duration(pc.Config.Embedding.TimeoutSec) * time.Second,
		}
		if vec, err := client.Embed(context.Background(), ev.Prompt); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			queryVec = vec
		}
	}
	ranked := retrieve.Rank(entries, ev.Prompt, queryVec, vs, pc.Config.Retrieve)
	if len(ranked) == 0 {
		return 0
	}
	var b strings.Builder
	for _, s := range ranked {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.Entry.Title, s.Entry.Body)
	}
	fmt.Fprintln(w, store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens))
	return 0
}

// HandlePostTool 记录触碰的文件（相对项目根、小写、"/" 分隔）。
func HandlePostTool(r io.Reader) int {
	ev, err := ParseEvent(r)
	if err != nil {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	rel := relativize(pc, ev.FilePath())
	if rel == "" {
		return 0
	}
	st := state.Load(pc.Store.StateDir(), ev.SessionID)
	st.AddTouched(rel)
	if err := st.Save(pc.Store.StateDir()); err != nil {
		logErr("post-tool save state: %v", err)
	}
	return 0
}

// relativize 将绝对路径转为相对项目根的路径；无法转换时返回 ""。
func relativize(pc *project.Context, abs string) string {
	if abs == "" {
		return ""
	}
	normAbs := registry.NormalizePath(abs)
	for _, root := range pc.Project.Paths {
		nr := registry.NormalizePath(root)
		if strings.HasPrefix(normAbs, nr+"/") {
			return normAbs[len(nr)+1:]
		}
	}
	return ""
}

// HandleStop 评估 enforce 规则，需要时以 exit 2 阻断。
func HandleStop(r io.Reader, stderr io.Writer) int {
	ev, err := ParseEvent(r)
	if err != nil {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	if len(pc.Config.Enforce) == 0 {
		return 0
	}
	st := state.Load(pc.Store.StateDir(), ev.SessionID)
	for _, rule := range pc.Config.Enforce {
		if rule.Type != "changelog_required" {
			continue
		}
		if block, reason := enforce.EvalChangelog(rule, st); block {
			st.MarkBlocked(rule.Type)
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("stop save state: %v", err)
			}
			fmt.Fprintln(stderr, reason)
			return 2
		}
	}
	return 0
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/project/ ./internal/hook/ -v`
Expected: 6 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/ internal/hook/
git commit -m "feat: hook event handlers for session-start/prompt/post-tool/stop"
```

---

### Task 9: CLI 子命令 + main 调度

**Files:**
- Create: `internal/cli/cli.go`
- Create: `cmd/ok/main.go`
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: 之前全部包的接口
- Produces:
  - `cli.Init/Add/Search/Index/List/Doctor(args []string, stdout, stderr io.Writer) int`
  - `main`：`ok <init|add|search|index|list|doctor|hook <event>>`，hook 路径 recover 后 exit 0

- [ ] **Step 1: 写失败的测试** `internal/cli/cli_test.go`

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/entry"
)

// chdir 切换工作目录并在结束时还原。
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestInitAddSearchList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("OPENAI_API_KEY", "") // 防止真实网络调用，保证测试离线
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer

	// init
	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "[[hooks]]") {
		t.Fatalf("init should print hooks block, got %q", out.String())
	}

	// add（无 embedding key → 提示跳过向量，仍成功）
	body := filepath.Join(proj, "body.md")
	if err := os.WriteFile(body, []byte("使用 Conventional Commits。"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	code := Add([]string{"--title", "Git 提交规范", "--type", "note", "--tags", "git", "--file", body}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("add code=%d err=%q", code, errBuf.String())
	}
	kb := filepath.Join(home, "projects", "demo")
	entries, err := entry.Load(filepath.Join(kb, "knowledge"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %+v err=%v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(kb, "INDEX.md")); err != nil || !strings.Contains(string(data), "Git 提交规范") {
		t.Fatalf("INDEX not rebuilt: %v %q", err, data)
	}

	// 重复 add 同名条目 → 失败
	out.Reset()
	if code := Add([]string{"--title", "Git 提交规范", "--type", "note"}, &out, &errBuf); code == 0 {
		t.Fatal("expected duplicate add to fail")
	}

	// search（无 embedding key → 纯关键词）
	out.Reset()
	if code := Search([]string{"git", "提交"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Git 提交规范") {
		t.Fatalf("search should hit, got %q", out.String())
	}

	// list
	out.Reset()
	if code := List(nil, &out, &errBuf); code != 0 {
		t.Fatalf("list code=%d", code)
	}
	if !strings.Contains(out.String(), "demo") || !strings.Contains(out.String(), "Git 提交规范") {
		t.Fatalf("list output %q", out.String())
	}
}

func TestAddOutsideProjectFails(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	chdir(t, t.TempDir())
	var out, errBuf bytes.Buffer
	if code := Add([]string{"--title", "X", "--type", "note"}, &out, &errBuf); code == 0 {
		t.Fatal("expected failure outside registered project")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/cli/`
Expected: 编译失败，`undefined: Init` 等

- [ ] **Step 3: 实现** `internal/cli/cli.go`

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	"openknowledge/internal/retrieve"
	"openknowledge/internal/store"
)

const hooksBlock = `
# OpenKnowledge hooks —— 追加到 ~/.kimi-code/config.toml
[[hooks]]
event = "SessionStart"
command = "ok hook session-start"
timeout = 10

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
[embedding]
base_url = "https://api.openai.com/v1"
api_key_env = "OPENAI_API_KEY"
model = "text-embedding-3-small"
timeout_sec = 5

[inject]
max_tokens = 1500

[retrieve]
alpha = 1.0
beta = 1.0
top_n = 3

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
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "用法: ok init <项目名>")
		return 1
	}
	name := fs.Arg(0)
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
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
	fmt.Fprintln(stdout, hooksBlock)
	return 0
}

// Add: ok add --title T --type rule --tags a,b --mandatory [--file body.md]
func Add(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "条目标题（必填）")
	typ := fs.String("type", "note", "rule|pitfall|note|reference")
	tags := fs.String("tags", "", "逗号分隔")
	mandatory := fs.Bool("mandatory", false, "SessionStart 全文注入")
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

// afterAdd 重建 INDEX 并（有 API key 时）增量更新向量。
func afterAdd(pc *project.Context, stdout, stderr io.Writer) int {
	entries, err := entry.Load(pc.Store.KnowledgeDir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := pc.Store.RebuildIndex(entries); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	client := embeddingClient(pc)
	if client == nil {
		fmt.Fprintln(stdout, "未配置 embedding API key，跳过向量更新（稍后运行 ok index）")
		return 0
	}
	vs, err := embed.LoadVectors(pc.Store.VectorsPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := vs.Update(context.Background(), client, entries); err != nil {
		fmt.Fprintf(stderr, "向量更新失败（可稍后 ok index 重试）: %v\n", err)
		return 0
	}
	if err := vs.Save(pc.Store.VectorsPath()); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "INDEX 与向量已更新")
	return 0
}

// embeddingClient 配置齐全时返回客户端，否则返回 nil。
func embeddingClient(pc *project.Context) *embed.OpenAIClient {
	key := os.Getenv(pc.Config.Embedding.APIKeyEnv)
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
	entries, err := entry.Load(pc.Store.KnowledgeDir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	vs, _ := embed.LoadVectors(pc.Store.VectorsPath())
	var queryVec []float32
	if client := embeddingClient(pc); client != nil {
		if vec, err := client.Embed(context.Background(), query); err != nil {
			fmt.Fprintf(stderr, "embedding 失败，降级为关键词检索: %v\n", err)
		} else {
			queryVec = vec
		}
	}
	for _, s := range retrieve.Rank(entries, query, queryVec, vs, pc.Config.Retrieve) {
		fmt.Fprintf(stdout, "%.2f\t%s (%s)\n", s.Score, s.Entry.Title, s.Entry.FileName())
	}
	return 0
}

// Index: ok index —— 全量重建向量
func Index(args []string, stdout, stderr io.Writer) int {
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	client := embeddingClient(pc)
	if client == nil {
		fmt.Fprintln(stderr, "未配置 embedding API key，无法重建向量")
		return 1
	}
	entries, err := entry.Load(pc.Store.KnowledgeDir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	vs := &embed.VectorSet{Vectors: map[string]*embed.EntryVector{}}
	if err := vs.Update(context.Background(), client, entries); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := vs.Save(pc.Store.VectorsPath()); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "已重建 %d 条向量\n", len(vs.Vectors))
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
```

`cmd/ok/main.go`

```go
package main

import (
	"fmt"
	"os"

	"openknowledge/internal/cli"
	"openknowledge/internal/hook"
)

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	if len(argv) < 2 {
		usage()
		return 1
	}
	switch argv[1] {
	case "hook":
		return runHook(argv[2:])
	case "init":
		return cli.Init(argv[2:], os.Stdout, os.Stderr)
	case "add":
		return cli.Add(argv[2:], os.Stdout, os.Stderr)
	case "search":
		return cli.Search(argv[2:], os.Stdout, os.Stderr)
	case "index":
		return cli.Index(argv[2:], os.Stdout, os.Stderr)
	case "list":
		return cli.List(argv[2:], os.Stdout, os.Stderr)
	case "doctor":
		return cli.Doctor(argv[2:], os.Stdout, os.Stderr)
	default:
		usage()
		return 1
	}
}

// runHook hook 路径全面 fail-open：panic 也只放行。
func runHook(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = 0
		}
	}()
	if len(args) < 1 {
		return 0
	}
	switch args[0] {
	case "session-start":
		return hook.HandleSessionStart(os.Stdin, os.Stdout)
	case "prompt":
		return hook.HandlePrompt(os.Stdin, os.Stdout)
	case "post-tool":
		return hook.HandlePostTool(os.Stdin)
	case "stop":
		return hook.HandleStop(os.Stdin, os.Stderr)
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: ok <init|add|search|index|list|doctor|hook> ...")
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go build ./... && go test ./internal/cli/ -v`
Expected: 编译成功，2 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ cmd/ok/
git commit -m "feat: CLI subcommands and main dispatch"
```

---

### Task 10: 端到端集成测试与验收

**Files:**
- Create: `cmd/ok/integration_test.go`

**Interfaces:**
- Consumes: 编译后的 `ok` 二进制全部行为
- Produces: 无（终端验收）

- [ ] **Step 1: 写集成测试** `cmd/ok/integration_test.go`

```go
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ok-bin")
	if err != nil {
		panic(err)
	}
	name := "ok"
	if runtime.GOOS == "windows" {
		name = "ok.exe"
	}
	binPath = filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build failed: %v\n%s", err, out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// runOK 以隔离的 OK_HOME 运行二进制，返回 stdout、stderr 与退出码。
func runOK(t *testing.T, home, cwd, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "OK_HOME="+home, "OPENAI_API_KEY=") // 清空 key 保证测试离线
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return so.String(), se.String(), code
}

func TestEndToEnd(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	// init：注册项目并打印 hooks 块
	stdout, _, code := runOK(t, home, proj, "", "init", "demo")
	if code != 0 || !strings.Contains(stdout, "已注册项目") || !strings.Contains(stdout, "[[hooks]]") {
		t.Fatalf("init failed: code=%d out=%q", code, stdout)
	}

	// add 普通条目（无 embedding key → 跳过向量但成功）
	body := filepath.Join(proj, "body.md")
	if err := os.WriteFile(body, []byte("使用 Conventional Commits。"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = runOK(t, home, proj, "", "add", "--title", "Git 提交规范", "--type", "note", "--tags", "git", "--file", body)
	if code != 0 {
		t.Fatalf("add failed: code=%d out=%q", code, stdout)
	}

	// add mandatory 条目
	mb := filepath.Join(proj, "mb.md")
	if err := os.WriteFile(mb, []byte("改完代码先写日志。"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code = runOK(t, home, proj, "", "add", "--title", "变更日志强制规则", "--type", "rule", "--mandatory", "--file", mb); code != 0 {
		t.Fatalf("add mandatory failed: code=%d", code)
	}

	// session-start：注入 mandatory 全文 + 索引，不含非 mandatory 正文
	ev := fmt.Sprintf(`{"hook_event_name":"SessionStart","session_id":"s1","cwd":%q}`, proj)
	stdout, _, code = runOK(t, home, proj, ev, "hook", "session-start")
	if code != 0 || !strings.Contains(stdout, "改完代码先写日志。") || !strings.Contains(stdout, "知识索引") {
		t.Fatalf("session-start: code=%d out=%q", code, stdout)
	}
	if strings.Contains(stdout, "Conventional Commits") {
		t.Fatalf("non-mandatory body leaked: %q", stdout)
	}

	// prompt：关键词命中注入
	ev = fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交规范"}`, proj)
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "Conventional Commits") {
		t.Fatalf("prompt: code=%d out=%q", code, stdout)
	}

	// 配置强制规则
	cfgPath := filepath.Join(home, "projects", "demo", "config.toml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg = append(cfg, []byte("\n[[enforce]]\ntype = \"changelog_required\"\ncode_globs = [\"**/*.go\"]\nchangelog_glob = \"docs/changelogs/**\"\nmessage = \"请补变更日志\"\n")...)
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	// post-tool 触碰代码 → stop 阻断一次 → 第二次放行
	codeFile := filepath.Join(proj, "main.go")
	ev = fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s9","cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, proj, codeFile)
	if _, _, code = runOK(t, home, proj, ev, "hook", "post-tool"); code != 0 {
		t.Fatalf("post-tool: code=%d", code)
	}
	ev = fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s9","cwd":%q}`, proj)
	_, stderr, code := runOK(t, home, proj, ev, "hook", "stop")
	if code != 2 || !strings.Contains(stderr, "请补变更日志") {
		t.Fatalf("stop should block: code=%d err=%q", code, stderr)
	}
	if _, _, code = runOK(t, home, proj, ev, "hook", "stop"); code != 0 {
		t.Fatalf("second stop should pass: code=%d", code)
	}

	// 未注册目录：所有 hook 静默放行
	ev = fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git"}`, filepath.Join(home, "nowhere"))
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || stdout != "" {
		t.Fatalf("unregistered should be silent: code=%d out=%q", code, stdout)
	}

	// list / doctor 冒烟
	stdout, _, code = runOK(t, home, proj, "", "list")
	if code != 0 || !strings.Contains(stdout, "demo") {
		t.Fatalf("list: code=%d out=%q", code, stdout)
	}
	stdout, _, _ = runOK(t, home, proj, "", "doctor")
	if !strings.Contains(stdout, "demo") {
		t.Fatalf("doctor: out=%q", stdout)
	}
}
```

- [ ] **Step 2: 运行全部测试确认通过**

Run: `go build ./... && go test ./... -v`
Expected: 所有包 PASS，包括 `TestEndToEnd`

- [ ] **Step 3: Commit**

```bash
git add cmd/ok/integration_test.go
git commit -m "test: end-to-end integration test via compiled binary"
```

- [ ] **Step 4: 手动验收（真实 Kimi Code，执行者人工完成）**

1. `go build -o ok.exe ./cmd/ok`，把 `ok.exe` 放到 PATH（如 `%USERPROFILE%\bin`）。
2. 把 `ok init` 打印的 hooks 块追加到 `~/.kimi-code/config.toml`。
3. **验证 stdin 字段名**（Global Constraints 中的 `prompt` / `tool_input.file_path` 是按 Kimi/Claude 约定假设的）：临时把 prompt hook 改为 `command = "sh -c 'cat > /tmp/ok-prompt.json'"`，在 Kimi Code 中发一条消息，检查 `/tmp/ok-prompt.json` 里提问文本的实际字段名；同样用 Write 工具触发一次 PostToolUse 验证 `file_path`。若字段名不同，改 `internal/hook/hook.go` 中 `Event` 结构体的 json tag 并回归 `go test ./...`。
4. 恢复正式 hooks 配置，新开 Kimi Code 会话验证：启动时注入 mandatory + 索引；提问相关关键词时注入条目；修改 `.go` 文件后结束回合被阻断并提示补变更日志。

---

## 验收修正（2026-07-22 真实 Kimi Code 0.28.1 实测后）

真实验收发现三处与载荷假设不符（详见规格附录 A）：

1. `UserPromptSubmit` 的 `prompt` 是内容块数组 `[{"type":"text","text":"..."}]`，
   不是字符串 —— 原 `Event.Prompt string` 解析失败，`HandlePrompt` 静默 exit 0。
2. SessionStart 的 stdout **不进入上下文**（标记注入实验：UserPromptSubmit 标记
   1 命中，SessionStart 标记 0 命中）—— SessionStart 注入通道不存在。
3. `PostToolUse` 的文件路径字段是 `tool_input.path`，不是 `file_path`。

### Task 11: 实测修正 —— prompt 数组、path 字段、基础注入迁移

**Files:**
- Modify: `internal/hook/hook.go`（Event 结构、PromptText/FilePath、HandlePrompt 重写、删 HandleSessionStart）
- Modify: `internal/state/state.go`（Session 增加字段）
- Modify: `cmd/ok/main.go`（删 session-start 分支）
- Modify: `internal/cli/cli.go`（hooksBlock 删 SessionStart 条目）
- Test: `internal/hook/hook_test.go`
- Test: `cmd/ok/integration_test.go`

**Interfaces:**
- Consumes: 现有全部包接口
- Produces:
  - `Event.Prompt` 改为 `json.RawMessage`；新增 `(e *Event) PromptText() string`
  - `(e *Event) FilePath() string`：`tool_input.path` 优先，兼容 `file_path`
  - `state.Session` 新增字段 `BaseInjected bool`（json: `base_injected`）
  - `hook.HandleSessionStart` 删除；`HandlePrompt` 承担基础注入 + 检索注入

- [ ] **Step 1: 改测试（先失败）**

`internal/hook/hook_test.go` 调整：

1. 所有 prompt 载荷改为真实数组形态，例如：
   `"prompt":[{"type":"text","text":"git 提交规范是什么"}]`
2. 所有 PostToolUse 载荷的 `"file_path"` 改为 `"path"`。
3. 删除 `TestSessionStartInjectsMandatoryAndIndex`，替换为：

```go
func TestFirstPromptInjectsBaseOnce(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "rule.md", mandatoryEntry)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	if err := os.WriteFile(filepath.Join(kbRoot, "INDEX.md"), []byte("# 知识索引\n\n- **Git 提交规范**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkPrompt := func(text string) string {
		return fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":%q}]}`, projDir, text)
	}
	var out bytes.Buffer
	// 首次提问：基础注入（mandatory 全文 + 索引）+ 检索命中
	if code := HandlePrompt(strings.NewReader(mkPrompt("git 提交规范是什么")), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "改完代码先写日志。") || !strings.Contains(got, "知识索引") {
		t.Fatalf("first prompt missing base injection: %q", got)
	}
	if !strings.Contains(got, "Conventional Commits") {
		t.Fatalf("first prompt missing retrieval: %q", got)
	}
	// 第二次提问（同会话）：不再重复基础注入，检索仍生效
	out.Reset()
	if code := HandlePrompt(strings.NewReader(mkPrompt("git 提交规范是什么")), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got = out.String()
	if strings.Contains(got, "改完代码先写日志。") || strings.Contains(got, "知识索引") {
		t.Fatalf("base injection repeated: %q", got)
	}
	if !strings.Contains(got, "Conventional Commits") {
		t.Fatalf("retrieval lost on second prompt: %q", got)
	}
}

func TestPromptStringFormCompat(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "git.md", gitEntry)
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":"git 提交"}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "Conventional Commits") {
		t.Fatalf("string prompt form broken: %q", out.String())
	}
}
```

`cmd/ok/integration_test.go` 调整：
- 删去 `hook session-start` 调用段；改为：首次 `hook prompt` 断言同时含
  mandatory 正文（"改完代码先写日志。"）、知识索引与检索命中；第二次
  `hook prompt` 断言不含 mandatory 正文与"知识索引"但仍含检索命中。
- PostToolUse 载荷 `"file_path"` 改 `"path"`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/hook/ ./cmd/ok/`
Expected: FAIL（当前实现解析数组 prompt 失败；无基础注入）

- [ ] **Step 3: 实现**

`internal/state/state.go` — Session 增加字段（其余不动）：

```go
type Session struct {
	SessionID    string   `json:"session_id"`
	Touched      []string `json:"touched"`
	BlockedRules []string `json:"blocked_rules"`
	BaseInjected bool     `json:"base_injected"`
}
```

`internal/hook/hook.go` — Event 与 FilePath/PromptText：

```go
type Event struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Prompt        json.RawMessage `json:"prompt"`
}

// PromptText 提取提问文本：兼容字符串与内容块数组 [{"type":"text","text":"..."}]。
func (e *Event) PromptText() string {
	if len(e.Prompt) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(e.Prompt, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(e.Prompt, &parts); err != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// FilePath 从 tool_input 提取文件路径：kimi 用 path，兼容 file_path。
func (e *Event) FilePath() string {
	var ti struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if len(e.ToolInput) > 0 {
		_ = json.Unmarshal(e.ToolInput, &ti)
	}
	if ti.Path != "" {
		return ti.Path
	}
	return ti.FilePath
}
```

删除 `HandleSessionStart` 整个函数。`HandlePrompt` 重写为：

```go
// HandlePrompt 基础注入（每会话首次：mandatory 全文 + 索引）+ 检索注入（每次）。
// embedding 失败降级为关键词检索；任何内部错误 fail-open。
func HandlePrompt(r io.Reader, w io.Writer) int {
	ev, err := ParseEvent(r)
	if err != nil {
		logErr("prompt parse: %v", err)
		return 0
	}
	promptText := ev.PromptText()
	if strings.TrimSpace(promptText) == "" {
		return 0
	}
	pc, err := project.FromCwd(ev.Cwd)
	if err != nil {
		return 0
	}
	entries, errs := entry.LoadTolerant(pc.Store.KnowledgeDir())
	for _, e := range errs {
		logErr("prompt skip bad entry: %v", e)
	}
	st := state.Load(pc.Store.StateDir(), ev.SessionID)
	var b strings.Builder
	if !st.BaseInjected {
		_ = state.Clean(pc.Store.StateDir(), 7*24*time.Hour)
		base := b.Len()
		for _, e := range entries {
			if !e.Mandatory {
				continue
			}
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", e.Title, e.Body)
		}
		if idx, err := os.ReadFile(pc.Store.IndexPath()); err == nil {
			b.Write(idx)
		}
		if b.Len() > base {
			st.BaseInjected = true
			if err := st.Save(pc.Store.StateDir()); err != nil {
				logErr("prompt save state: %v", err)
			}
		}
	}
	vs, err := embed.LoadVectors(pc.Store.VectorsPath())
	if err != nil {
		logErr("prompt load vectors: %v", err)
		vs = nil
	}
	var queryVec []float32
	if key := os.Getenv(pc.Config.Embedding.APIKeyEnv); key != "" && pc.Config.Embedding.BaseURL != "" {
		client := &embed.OpenAIClient{
			BaseURL: pc.Config.Embedding.BaseURL,
			APIKey:  key,
			Model:   pc.Config.Embedding.Model,
			Timeout: time.Duration(pc.Config.Embedding.TimeoutSec) * time.Second,
		}
		if vec, err := client.Embed(context.Background(), promptText); err != nil {
			logErr("prompt embed: %v", err)
		} else {
			queryVec = vec
		}
	}
	ranked := retrieve.Rank(entries, promptText, queryVec, vs, pc.Config.Retrieve)
	for _, s := range ranked {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.Entry.Title, s.Entry.Body)
	}
	out := store.TruncateToBudget(b.String(), pc.Config.Inject.MaxTokens)
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(w, out)
	}
	return 0
}
```

`cmd/ok/main.go` — 删除 `case "session-start"` 分支。

`internal/cli/cli.go` — hooksBlock 删除 SessionStart 的 `[[hooks]]` 条目（保留
UserPromptSubmit / PostToolUse / Stop 三条）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go build ./... && go test ./... -v`
Expected: 全部 PASS（含更新后的集成测试）

- [ ] **Step 5: Commit**

```bash
git add internal/hook/ internal/state/ cmd/ok/ internal/cli/
git commit -m "fix: parse real kimi hook payloads and move base injection to first prompt"
```

**验收（控制器执行，非本任务）**：更新 `~/.kimi-code/config.toml` 为三条 hook，
真实 kimi 会话复验：首次提问上下文含 mandatory+索引；PostToolUse 记录生效；
改代码后 Stop 阻断。

---

## v1.1 特性（2026-07-22 用户追加需求）

### Task 12: ok setup 首次引导 + kimi 技能安装 + hooks 全局开关

**Files:**
- Create: `internal/cli/setup.go`（Setup/kimiHome/skillsHome/hooksBlockFor/upsertHooksBlock/installSkills/skillTemplates/guideText）
- Create: `internal/cli/toggle.go`（On/Off/disabledFlagPath）
- Modify: `internal/registry/registry.go`（加 `HooksDisabled()`）
- Modify: `internal/hook/hook.go`（三个 handler 顶部加开关检查）
- Modify: `internal/cli/cli.go`（Doctor 增加 hooks 安装/开关状态报告；Init 末尾提示 ok setup）
- Modify: `cmd/ok/main.go`（setup/on/off 分支 + usage）
- Create: `docs/changelogs/2026-07-22-v1.1-setup-toggle.md`（强制变更日志规则要求）
- Test: `internal/cli/setup_test.go`、`internal/cli/toggle_test.go`、`internal/hook/hook_test.go`（追加）、`cmd/ok/integration_test.go`（追加开关流程）

**Interfaces:**
- Consumes: 现有全部包接口
- Produces:
  - `cli.Setup(args []string, stdout, stderr io.Writer) int`
  - `cli.On(args []string, stdout, stderr io.Writer) int` / `cli.Off(args []string, stdout, stderr io.Writer) int`
  - `registry.HooksDisabled() bool` — `~/.openknowledge/hooks-disabled` 存在即 true
  - cli 内部：`kimiHome()`（KIMI_CODE_HOME 优先）、`skillsHome()`（OK_SKILLS_HOME 优先，默认 ~/.agents/skills）、`upsertHooksBlock(configPath, block string) error`、`hooksBlockFor(exe string) string`、`installSkills(exe string) error`、常量 `markerBegin`/`markerEnd`

- [ ] **Step 1: 写失败的测试**

`internal/cli/setup_test.go`

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertHooksBlockAppendAndReplace(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("default_model = \"kimi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := upsertHooksBlock(cfg, "BLOCK_V1\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if !strings.Contains(got, "default_model") || !strings.Contains(got, "BLOCK_V1") {
		t.Fatalf("append failed: %q", got)
	}
	if err := upsertHooksBlock(cfg, "BLOCK_V2\n"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(cfg)
	got = string(data)
	if strings.Contains(got, "BLOCK_V1") || !strings.Contains(got, "BLOCK_V2") {
		t.Fatalf("replace failed: %q", got)
	}
	if strings.Count(got, markerBegin) != 1 {
		t.Fatalf("duplicate marker block: %q", got)
	}
}

func TestUpsertHooksBlockNewFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := upsertHooksBlock(cfg, "BLOCK\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), "BLOCK") {
		t.Fatalf("unexpected %q", data)
	}
}

func TestUpsertHooksBlockCorruptMarker(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(markerBegin+"\nno end"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := upsertHooksBlock(cfg, "X\n"); err == nil {
		t.Fatal("expected corrupt marker error")
	}
}

func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	if err := installSkills(`D:\bin\ok.exe`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openknowledge-init", "openknowledge-on", "openknowledge-off"} {
		data, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(string(data), "D:/bin/ok.exe") {
			t.Fatalf("%s missing baked exe path: %q", name, data)
		}
	}
}
```

`internal/cli/toggle_test.go`

```go
package cli

import (
	"bytes"
	"testing"

	"openknowledge/internal/registry"
)

func TestOffOnToggle(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	if registry.HooksDisabled() {
		t.Fatal("default should be enabled")
	}
	if code := Off(nil, &out, &errBuf); code != 0 {
		t.Fatalf("off code=%d err=%q", code, errBuf.String())
	}
	if !registry.HooksDisabled() {
		t.Fatal("expected disabled after ok off")
	}
	if code := On(nil, &out, &errBuf); code != 0 {
		t.Fatalf("on code=%d", code)
	}
	if registry.HooksDisabled() {
		t.Fatal("expected enabled after ok on")
	}
	// On 幂等（无标志文件也成功）
	if code := On(nil, &out, &errBuf); code != 0 {
		t.Fatalf("on idempotent code=%d", code)
	}
}
```

`internal/hook/hook_test.go` 追加

```go
func TestHooksDisabledStopsAll(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	cfg := `
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]
changelog_glob = "docs/changelogs/**"
message = "请补变更日志"
`
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// 关闭全局开关
	if err := os.WriteFile(filepath.Join(registry.Home(), "hooks-disabled"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交"}]}`, projDir)
	var out bytes.Buffer
	if code := HandlePrompt(strings.NewReader(in), &out); code != 0 || out.Len() != 0 {
		t.Fatalf("disabled prompt: code=%d out=%q", code, out.String())
	}
	codeFile := filepath.Join(projDir, "main.go")
	post := fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":"s9","cwd":%q,"tool_name":"Write","tool_input":{"path":%q}}`, projDir, codeFile)
	if code := HandlePostTool(strings.NewReader(post)); code != 0 {
		t.Fatalf("disabled post-tool exit %d", code)
	}
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"s9","cwd":%q}`, projDir)
	var stderr bytes.Buffer
	if code := HandleStop(strings.NewReader(stop), &stderr); code != 0 {
		t.Fatalf("disabled stop should pass, got %d (%q)", code, stderr.String())
	}
}
```

（hook_test.go 需要给 imports 加 `"openknowledge/internal/registry"`。）

`cmd/ok/integration_test.go` 在 TestEndToEnd 末尾（list/doctor 冒烟之后）追加开关流程：

```go
	// 全局开关：off 后 prompt 无输出，on 后恢复
	if _, _, code = runOK(t, home, proj, "", "off"); code != 0 {
		t.Fatalf("off: code=%d", code)
	}
	ev = fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":%q,"prompt":[{"type":"text","text":"git 提交规范"}]}`, proj)
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || stdout != "" {
		t.Fatalf("disabled prompt should be silent: code=%d out=%q", code, stdout)
	}
	if _, _, code = runOK(t, home, proj, "", "on"); code != 0 {
		t.Fatalf("on: code=%d", code)
	}
	stdout, _, code = runOK(t, home, proj, ev, "hook", "prompt")
	if code != 0 || !strings.Contains(stdout, "Conventional Commits") {
		t.Fatalf("re-enabled prompt: code=%d out=%q", code, stdout)
	}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/cli/ ./internal/hook/ ./cmd/ok/`
Expected: 编译失败，`undefined: upsertHooksBlock` / `Off` / `registry.HooksDisabled` 等

- [ ] **Step 3: 实现**

`internal/registry/registry.go` 追加：

```go
// HooksDisabled 报告 hooks 全局开关是否关闭（标志文件存在）。
func HooksDisabled() bool {
	_, err := os.Stat(filepath.Join(Home(), "hooks-disabled"))
	return err == nil
}
```

`internal/cli/toggle.go`

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/registry"
)

func disabledFlagPath() string { return filepath.Join(registry.Home(), "hooks-disabled") }

// Off: ok off —— 关闭 hooks 全局开关（持续到 ok on）
func Off(args []string, stdout, stderr io.Writer) int {
	content := fmt.Sprintf("disabled at %s\nrun `ok on` to re-enable\n", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(disabledFlagPath(), []byte(content), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "hooks 已全局关闭（ok on 重新开启）")
	return 0
}

// On: ok on —— 开启 hooks 全局开关
func On(args []string, stdout, stderr io.Writer) int {
	if err := os.Remove(disabledFlagPath()); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "hooks 已开启")
	return 0
}
```

`internal/cli/setup.go`

```go
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const markerBegin = "# >>> openknowledge hooks >>>"
const markerEnd = "# <<< openknowledge hooks <<<"

// Setup: ok setup —— 首次引导：写入 hooks 配置、安装技能、打印引导
func Setup(args []string, stdout, stderr io.Writer) int {
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
	fmt.Fprintln(stdout, guideText)
	return 0
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
	"openknowledge-init": "---\nname: openknowledge-init\ndescription: 在当前项目目录初始化 OpenKnowledge 知识库（ok init）。当用户要求\"初始化知识库\"或\"把本项目注册到知识库\"时使用。\n---\n\n# openknowledge-init\n\n用 Bash 工具在当前工作目录执行（<目录名> 用当前目录的基名）：\n\n    \"{{EXE}}\" init <目录名>\n\n把输出的知识库路径汇报给用户；若提示重复注册，告知用户该项目已初始化过。\n",
	"openknowledge-on":   "---\nname: openknowledge-on\ndescription: 开启 OpenKnowledge 知识库 hooks 全局开关。当用户要求\"开启知识库\"\"启用知识库 hooks\"时使用。\n---\n\n# openknowledge-on\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" on\n\n把输出汇报给用户。\n",
	"openknowledge-off":  "---\nname: openknowledge-off\ndescription: 关闭 OpenKnowledge 知识库 hooks 全局开关（持续到手动开启）。当用户要求\"关闭知识库\"\"停用知识库 hooks\"时使用。\n---\n\n# openknowledge-off\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" off\n\n把输出汇报给用户，并说明：关闭后所有项目的知识库注入与强制检查都会暂停，直到执行 ok on。\n",
}

const guideText = `
下一步：
  1. 在需要知识库的项目目录运行 ok init <项目名>（或在 kimi 中说"初始化知识库"）
  2. 用 ok add 添加知识条目
  3. 新开 kimi 会话即可生效；ok off / ok on 可随时全局开关
`
```

`internal/hook/hook.go`：在 `HandlePrompt`、`HandlePostTool`、`HandleStop` 三个函数
体的第一行（ParseEvent 之前）加：

```go
	if registry.HooksDisabled() {
		return 0
	}
```

`internal/cli/cli.go` 修改：
- `Doctor` 在注册表项目循环之前加：

```go
	if data, err := os.ReadFile(filepath.Join(kimiHome(), "config.toml")); err != nil || !strings.Contains(string(data), markerBegin) {
		fmt.Fprintln(stdout, "hooks 未安装（运行 ok setup）")
		healthy = false
	} else {
		fmt.Fprintln(stdout, "hooks 已安装")
	}
	if registry.HooksDisabled() {
		fmt.Fprintln(stdout, "hooks 当前为关闭状态（ok on 开启）")
	}
```

- `Init` 末尾打印 hooksBlock 之后追加一行：

```go
	fmt.Fprintln(stdout, "或直接运行 ok setup 自动写入 hooks 配置并安装技能（推荐）")
```

`cmd/ok/main.go`：switch 加三个 case，usage 文本同步：

```go
	case "setup":
		return cli.Setup(argv[2:], os.Stdout, os.Stderr)
	case "on":
		return cli.On(argv[2:], os.Stdout, os.Stderr)
	case "off":
		return cli.Off(argv[2:], os.Stdout, os.Stderr)
```

`fmt.Fprintln(os.Stderr, "用法: ok <setup|init|add|search|index|list|doctor|on|off|hook> ...")`

`docs/changelogs/2026-07-22-v1.1-setup-toggle.md`（强制变更日志规则要求，内容）：

```markdown
# v1.1：ok setup 首次引导 + kimi 技能 + hooks 全局开关

- 新增 `ok setup`：以标记块幂等写入 hooks 配置（命令用自身 exe 绝对路径），
  备份原配置，安装 openknowledge-init/on/off 三个用户技能到 ~/.agents/skills/。
- 新增 `ok on` / `ok off`：hooks 全局开关（~/.openknowledge/hooks-disabled
  标志文件），默认开启，关闭持续到手动恢复。
- 三个 hook 入口在处理前检查全局开关；ok doctor 增加 hooks 安装与开关状态报告。
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go build ./... && go test ./... -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ cmd/ docs/changelogs/
git commit -m "feat: first-run setup wizard, kimi skills install, and global hooks toggle"
```

---

## v1.2 特性（2026-07-23 用户追加需求）

### Task 13: 全局配置合并 + ok setup 交互式 embedding 配置

**Files:**
- Modify: `internal/config/config.go`（Embedding 加 APIKey；加 LoadMerged 与 ResolvedAPIKey）
- Modify: `internal/project/project.go`（FromCwd 改用 LoadMerged）
- Modify: `internal/hook/hook.go`（HandlePrompt 用 ResolvedAPIKey）
- Modify: `internal/cli/cli.go`（embeddingClient 用 ResolvedAPIKey；defaultProjectConfig 模板改为注释引导）
- Modify: `internal/cli/setup.go`（Setup 加 embedding 交互/flags；签名加 stdin）
- Modify: `cmd/ok/main.go`（Setup 调用传 os.Stdin）
- Create: `docs/changelogs/2026-07-23-setup-embedding.md`
- Test: `internal/config/config_test.go`（追加）、`internal/cli/setup_test.go`（追加）

**Interfaces:**
- Consumes: 现有全部包接口
- Produces:
  - `config.Embedding` 新增字段 `APIKey string \`toml:"api_key"\``
  - `config.LoadMerged(projectPath, globalPath string) (Config, error)` — Default ← global ← project
  - `(e Embedding) ResolvedAPIKey() string` — api_key 优先，其次 api_key_env 环境变量
  - `cli.Setup(args []string, in io.Reader, stdout, stderr io.Writer) int`（签名变更）

- [ ] **Step 1: 写失败的测试**

`internal/config/config_test.go` 追加：

```go
func TestLoadMergedPrecedence(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	if err := os.WriteFile(global, []byte("[retrieve]\ntop_n = 5\n[embedding]\nbase_url = \"https://g.example.com/v1\"\napi_key = \"gk\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("[retrieve]\ntop_n = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.TopN != 9 {
		t.Fatalf("project should override global, got %d", cfg.Retrieve.TopN)
	}
	if cfg.Embedding.BaseURL != "https://g.example.com/v1" || cfg.Embedding.APIKey != "gk" {
		t.Fatalf("global embedding should apply, got %+v", cfg.Embedding)
	}
	if cfg.Inject.MaxTokens != 1500 {
		t.Fatalf("builtin default lost, got %+v", cfg.Inject)
	}
}

func TestLoadMergedMissingFiles(t *testing.T) {
	cfg, err := LoadMerged(filepath.Join(t.TempDir(), "a.toml"), filepath.Join(t.TempDir(), "b.toml"))
	if err != nil || cfg.Retrieve.TopN != 3 {
		t.Fatalf("missing files should yield defaults, got %+v err=%v", cfg, err)
	}
}

func TestResolvedAPIKey(t *testing.T) {
	t.Setenv("OK_TEST_KEY", "envkey")
	if got := (Embedding{APIKey: "direct", APIKeyEnv: "OK_TEST_KEY"}).ResolvedAPIKey(); got != "direct" {
		t.Fatalf("direct key should win, got %q", got)
	}
	if got := (Embedding{APIKeyEnv: "OK_TEST_KEY"}).ResolvedAPIKey(); got != "envkey" {
		t.Fatalf("env fallback failed, got %q", got)
	}
	if got := (Embedding{}).ResolvedAPIKey(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
```

`internal/cli/setup_test.go` 追加：

```go
func TestSetupWithEmbeddingFlags(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "kimi"))
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	code := Setup([]string{"--embedding-base-url", "https://g.example.com/v1", "--embedding-model", "m1", "--embedding-key", "sk-test"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("OK_HOME"), "config.toml"))
	if err != nil {
		t.Fatalf("global config not written: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `api_key = "sk-test"`) || !strings.Contains(got, `base_url = "https://g.example.com/v1"`) || !strings.Contains(got, `model = "m1"`) {
		t.Fatalf("global config wrong: %q", got)
	}
}

func TestSetupInteractiveSkipKeepsGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "kimi"))
	t.Setenv("OK_SKILLS_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	// 三行全回车 → 跳过 embedding 配置，且不得创建/破坏全局配置
	code := Setup(nil, strings.NewReader("\n\n\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("setup code=%d err=%q", code, errBuf.String())
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("global config should not be created when skipped")
	}
}

func TestInitTemplateHasNoActiveEmbedding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init(nil, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	cfg, err := config.Load(filepath.Join(home, "projects", "demo", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Embedding.BaseURL != "" || cfg.Embedding.APIKey != "" || cfg.Embedding.APIKeyEnv != "" {
		t.Fatalf("project template should leave embedding empty for global inheritance, got %+v", cfg.Embedding)
	}
}
```

（setup_test.go 需要给 imports 加 `"bytes"`；TestInitTemplateHasNoActiveEmbedding 放在 cli_test.go 则需要加 `"openknowledge/internal/config"` import——任选其一文件放置，对应补 import。）

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/ ./internal/cli/`
Expected: 编译失败，`undefined: LoadMerged` / `ResolvedAPIKey` / Setup 参数不匹配等

- [ ] **Step 3: 实现**

`internal/config/config.go` — Embedding 加字段；追加 LoadMerged 与 ResolvedAPIKey（需加 `os` import，已有）：

```go
type Embedding struct {
	BaseURL    string `toml:"base_url"`
	APIKey     string `toml:"api_key"`
	APIKeyEnv  string `toml:"api_key_env"`
	Model      string `toml:"model"`
	TimeoutSec int    `toml:"timeout_sec"`
}

// ResolvedAPIKey 返回生效的 embedding API key：api_key 字段优先，其次 api_key_env 环境变量。
func (e Embedding) ResolvedAPIKey() string {
	if e.APIKey != "" {
		return e.APIKey
	}
	if e.APIKeyEnv != "" {
		return os.Getenv(e.APIKeyEnv)
	}
	return ""
}

// LoadMerged 合并配置：内置默认 ← globalPath ← projectPath，后者覆盖前者。
// 两个文件都可以不存在（视为空）。
func LoadMerged(projectPath, globalPath string) (Config, error) {
	cfg := Default()
	for _, path := range []string{globalPath, projectPath} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, err
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("解析 %s: %w", path, err)
		}
	}
	return cfg, nil
}
```

（config.go 需加 `"fmt"` import；保留现有 `Load` 不动。）

`internal/project/project.go` — FromCwd 中：

```go
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
```

`internal/hook/hook.go` — HandlePrompt 中 key 判断改为：

```go
	if key := pc.Config.Embedding.ResolvedAPIKey(); key != "" && pc.Config.Embedding.BaseURL != "" {
```

`internal/cli/cli.go`：
- `embeddingClient` 中 `key := os.Getenv(pc.Config.Embedding.APIKeyEnv)` 改为
  `key := pc.Config.Embedding.ResolvedAPIKey()`
- `defaultProjectConfig` 整体替换为：

```go
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
```

`internal/cli/setup.go` — Setup 换签名并加 embedding 步骤（追加到文件，需加
`"bufio"`、`"strings"`（已有）、`"openknowledge/internal/config"`、
`"openknowledge/internal/embed"`、`"openknowledge/internal/registry"`、
`"github.com/BurntSushi/toml"`、`"context"`、`"time"` imports）：

```go
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
	fmt.Fprintln(stdout, guideText)
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
```

`cmd/ok/main.go` — Setup 调用改为：

```go
	case "setup":
		return cli.Setup(argv[2:], os.Stdin, os.Stdout, os.Stderr)
```

`docs/changelogs/2026-07-23-setup-embedding.md`：

```markdown
# ok setup 交互式 embedding 配置 + 全局配置合并

- 新增全局配置 ~/.openknowledge/config.toml：内置默认 ← 全局 ← 项目，后者覆盖前者。
- embedding 新增 api_key 字段（0600 落盘），ResolvedAPIKey 优先取字段、其次环境变量。
- ok setup 增加 embedding 交互配置（base_url/model/API key，回车跳过），支持
  --embedding-base-url/--embedding-model/--embedding-key 非交互 flags，写完即验连通性。
- ok init 的项目配置模板不再预置 embedding/inject/retrieve，缺省全部继承全局。
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go build ./... && go test ./... -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ cmd/ docs/changelogs/
git commit -m "feat: global config merge and interactive embedding setup"
```
