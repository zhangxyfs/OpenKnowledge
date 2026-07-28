# GUI "其他" Tab 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** GUI 新增"其他"tab（数据导出/导入 + 版本显示），修复 `ok gui` 打开页面不自动最大化的回归。

**Architecture:** 新叶子包 `internal/backup` 用 stdlib archive/zip 做导出/导入；daemon 侧在 `internal/gui/api.go` 加三个端点（export/import/status 扩展）；版本号 `internal/version` + build-dist.sh ldflags 注入；最大化修复 = OpenBrowser 里协程改同步；前端 web/ 加第三个 tab。

**Tech Stack:** Go 1.25（module `openknowledge`），stdlib archive/zip，BurntSushi/toml，原生 JS（无框架）。

## Global Constraints

- 平台：Windows + Git Bash；测试不得依赖真实 `~/.openknowledge`、`~/.kimi-code`、`~/.agents`。
- 测试隔离：`OK_HOME`、`KIMI_CODE_HOME`、`OK_SKILLS_HOME` 全部 `t.Setenv` → `t.TempDir()`。
- 导入冲突语义：**同名条目覆盖**（ok add --force 语义）。
- 导入上限 32MB；只接受 `registry.toml`、`projects/*/knowledge/*.md`、`projects/*/config.toml` 三类路径；zip-slip（`..`/绝对路径/盘符/反斜杠）整包拒绝。
- 导入只写 knowledge/ 与 registry，kb.db 由 Sync 重建；`.md` 必须过 `entry.Parse`，失败计 skipped 不中断。
- 版本号单一事实源 = `installer/openknowledge.iss` 的 AppVersion；裸 `go build` 显示 `dev`。
- 最大化修复只改调用点（去 `go`），不动 `window_windows.go` 逻辑。
- 每任务结束 `go build ./... && go vet ./... && go test ./...` 全绿后提交，Conventional Commits 英文。
- spec：`docs/superpowers/specs/2026-07-28-gui-misc-tab-design.md`。spec §3"关于"卡片的条目数调整为项目数（status 无条目数聚合，YAGNI）。

---

### Task 1: internal/version + build-dist 注入 + status 扩展

**Files:**
- Create: `internal/version/version.go`、`internal/version/version_test.go`
- Modify: `scripts/build-dist.sh`、`internal/gui/api.go:257-294`（apiStatus）、`internal/gui/api_test.go`

**Interfaces:**
- Consumes: `registry.Home()`（registry.go:23）。
- Produces: `openknowledge/internal/version` 包 `var Version = "dev"`；apiStatus 响应新增 `"app_version"` 与 `"home"` 两个字段（Task 5 前端依赖这两个字段名）。

- [ ] **Step 1: 写失败测试**

`internal/version/version_test.go`：

```go
package version

import "testing"

func TestDefaultIsDev(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("Version = %q, want dev (ldflags 未注入时)", Version)
	}
}
```

`internal/gui/api_test.go` 追加（newEnv/do/testToken 模式见 api_test.go:23-86）：

```go
func TestStatusVersionAndHome(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	code, data := do(t, "GET", srv.URL+"/api/status", testToken, nil)
	if code != 200 {
		t.Fatalf("code=%d body=%s", code, data)
	}
	var s struct {
		AppVersion string `json:"app_version"`
		Home       string `json:"home"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.AppVersion == "" || s.Home == "" {
		t.Fatalf("status missing app_version/home: %s", data)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/version/ ./internal/gui/ -run 'TestDefaultIsDev|TestStatusVersionAndHome' -v`
Expected: version 包不存在编译失败；gui 测试 FAIL（无字段）。

- [ ] **Step 3: 实现**

`internal/version/version.go`：

```go
// Package version 持有构建期注入的应用版本号。
package version

// Version 由 build-dist.sh 经 -ldflags -X 注入（事实源 installer/openknowledge.iss 的 AppVersion）；
// 裸 go build 为 dev。
var Version = "dev"
```

`internal/gui/api.go` apiStatus 的 `writeJSON` map（api.go:288-294）加两行：

```go
		"app_version":         version.Version,
		"home":                registry.Home(),
```

（import 加 `"openknowledge/internal/version"`。）

`scripts/build-dist.sh` 改构建行：

```bash
VERSION=$(sed -n 's/^#define AppVersion "\(.*\)"/\1/p' installer/openknowledge.iss)
go build -ldflags "-s -w -H windowsgui -X openknowledge/internal/version.Version=$VERSION" -o dist/ok.exe ./cmd/ok
```

- [ ] **Step 4: 跑测试确认通过 + 构建脚本验证**

Run: `go test ./internal/version/ ./internal/gui/ -v && bash scripts/build-dist.sh && ./dist/ok.exe doctor`
Expected: 测试 PASS；dist 构建成功（doctor 能跑即可，版本字段由端点测试锁定）。
注意：构建前若 dist/ok.exe 被运行中的 daemon 占用，先 `dist/ok.exe daemon stop`。

- [ ] **Step 5: Commit**

```bash
git add internal/version/ internal/gui/ scripts/build-dist.sh
git commit -m "feat: app version injected via ldflags and exposed in /api/status"
```

---

### Task 2: internal/backup Export

**Files:**
- Create: `internal/backup/backup.go`（包声明 + 常量 + Report + Export；Import 在 Task 3 追加同文件）
- Test: `internal/backup/backup_test.go`

**Interfaces:**
- Consumes: `registry.Load/DefaultPath/Home/Registry/Project`（registry.go:13-43）、`store.New/KnowledgeDir/ConfigPath`（store.go:9-17）、`entry.Parse`（entry.go:30）、`index.Open/Sync/CorruptEntriesError`（db.go:48、sync.go:20-52）。
- Produces（Task 3/4 依赖）：
  - `const MaxSize = 32 << 20`
  - `var ErrBadPackage = errors.New("无效的备份包")`（客户端错误 sentinel，HTTP 层映射 400）
  - `type Report struct { Imported int \`json:"imported"\`; Skipped int \`json:"skipped"\`; Projects []string \`json:"projects"\` }`
  - `func Export(w io.Writer, project string) error`（project="all" 全导；项目不存在返回 `fmt.Errorf("%w: 项目 %q 不存在", ErrBadPackage, project)`）

- [ ] **Step 1: 写失败测试**

`internal/backup/backup_test.go`：

```go
package backup

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openknowledge/internal/registry"
)

// setupHome 建隔离 OK_HOME，注册两个项目并各写条目/config。
func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	reg, _ := registry.Load(registry.DefaultPath())
	for _, name := range []string{"alpha", "beta"} {
		if err := reg.AddProject(name, `D:\src\`+name); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(home, "projects", name, "knowledge")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		md := "---\ntitle: " + name + "条目\ntype: note\ntags: [t]\nsummary: s\ndraft: false\nmandatory: false\n---\n正文" + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "projects", name, "config.toml"), []byte("[wiki]\nstale_commits = 7\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	return home
}

func zipNames(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, rc)
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

func TestExportAll(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "all"); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, buf.Bytes())
	for _, want := range []string{
		"registry.toml",
		"projects/alpha/knowledge/alpha.md",
		"projects/alpha/config.toml",
		"projects/beta/knowledge/beta.md",
		"projects/beta/config.toml",
	} {
		if _, ok := names[want]; !ok {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	if !strings.Contains(names["registry.toml"], "alpha") ||
		!strings.Contains(names["projects/alpha/knowledge/alpha.md"], "正文alpha") {
		t.Fatal("content mismatch")
	}
}

func TestExportSingleProject(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "alpha"); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, buf.Bytes())
	if _, ok := names["projects/alpha/knowledge/alpha.md"]; !ok {
		t.Fatal("alpha missing")
	}
	for n := range names {
		if strings.Contains(n, "beta") {
			t.Fatalf("single export leaked %s", n)
		}
	}
	if strings.Contains(names["registry.toml"], "beta") {
		t.Fatal("filtered registry leaked beta")
	}
}

func TestExportUnknownProject(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	err := Export(&buf, "nope")
	if !errors.Is(err, ErrBadPackage) {
		t.Fatalf("err=%v", err)
	}
}
```

（`io` 与 `errors` import 按使用补齐。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/backup/`
Expected: 编译失败（包不存在）。

- [ ] **Step 3: 实现**

`internal/backup/backup.go`：

```go
// Package backup 提供知识库导出/导入（zip）。叶子工具包。
package backup

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"openknowledge/internal/registry"
)

// MaxSize 是导入包的大小上限。
const MaxSize = 32 << 20

// ErrBadPackage 标记客户端侧的包错误（HTTP 层映射 400）。
var ErrBadPackage = errors.New("无效的备份包")

// Report 是导入结果。
type Report struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Projects []string `json:"projects"`
}

// Export 把 registry 与项目条目/config 写入 zip。project 为 "all" 全导，否则只导该项目
// （registry.toml 随之过滤）。
func Export(w io.Writer, project string) error {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return err
	}
	var projects []registry.Project
	for _, p := range reg.Projects {
		if project == "all" || p.Name == project {
			projects = append(projects, p)
		}
	}
	if project != "all" && len(projects) == 0 {
		return fmt.Errorf("%w: 项目 %q 不存在", ErrBadPackage, project)
	}

	zw := zip.NewWriter(w)
	regData, err := toml.Marshal(registry.Registry{Projects: projects})
	if err != nil {
		return err
	}
	if err := addBytes(zw, "registry.toml", regData); err != nil {
		return err
	}
	for _, p := range projects {
		root := filepath.Join(registry.Home(), "projects", p.Name)
		mds, err := filepath.Glob(filepath.Join(root, "knowledge", "*.md"))
		if err != nil {
			return err
		}
		for _, md := range mds {
			if err := addFile(zw, md, "projects/"+p.Name+"/knowledge/"+filepath.Base(md)); err != nil {
				return err
			}
		}
		cfg := filepath.Join(root, "config.toml")
		if _, err := os.Stat(cfg); err == nil {
			if err := addFile(zw, cfg, "projects/"+p.Name+"/config.toml"); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

func addFile(zw *zip.Writer, src, name string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return addBytes(zw, name, data)
}

func addBytes(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/backup/ -v`
Expected: 3 个测试 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/backup/
git commit -m "feat: backup package with zip export of registry and entries"
```

---

### Task 3: internal/backup Import

**Files:**
- Modify: `internal/backup/backup.go`（追加 Import 与辅助）
- Test: `internal/backup/backup_test.go`（追加）

**Interfaces:**
- Consumes: Task 2 的全部产物 + `entry.Parse`（entry.go:30）、`store.New`（store.go:11）、`index.Open/Sync/CorruptEntriesError`。
- Produces: `func Import(r io.ReaderAt, size int64) (*Report, error)`（Task 4 端点依赖此签名；ErrBadPackage 包裹的错误 → 400）。

- [ ] **Step 1: 写失败测试**

```go
// 往返：导出 → 改环境（删条目+清空 registry）→ 导入 → 断言还原
func TestImportRoundTrip(t *testing.T) {
	home := setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "all"); err != nil {
		t.Fatal(err)
	}
	// 破坏现场：删 alpha 条目、换全新 registry
	if err := os.Remove(filepath.Join(home, "projects", "alpha", "knowledge", "alpha.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	rep, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 2 || rep.Skipped != 0 || len(rep.Projects) != 2 {
		t.Fatalf("report: %+v", rep)
	}
	data, err := os.ReadFile(filepath.Join(home, "projects", "alpha", "knowledge", "alpha.md"))
	if err != nil || !strings.Contains(string(data), "正文alpha") {
		t.Fatalf("entry not restored: %v", err)
	}
	reg, _ := registry.Load(registry.DefaultPath())
	if len(reg.Projects) != 2 {
		t.Fatalf("registry not restored: %+v", reg.Projects)
	}
	// config 也还原
	if _, err := os.Stat(filepath.Join(home, "projects", "beta", "config.toml")); err != nil {
		t.Fatal("config not restored")
	}
}

// 同名覆盖：改条目内容后导入旧包，内容被旧包覆盖
func TestImportOverwrites(t *testing.T) {
	home := setupHome(t)
	var buf bytes.Buffer
	if err := Export(&buf, "all"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, "projects", "alpha", "knowledge", "alpha.md")
	os.WriteFile(p, []byte("---\ntitle: 新版\ntype: note\ntags: []\nsummary: x\ndraft: false\nmandatory: false\n---\n新正文\n"), 0o644)
	if _, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "正文alpha") {
		t.Fatal("overwrite failed")
	}
}

// 损坏 .md 计 skipped，不中断
func TestImportSkipsCorrupt(t *testing.T) {
	setupHome(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("registry.toml")
	w.Write([]byte("[[project]]\nname = \"alpha\"\npaths = [\"D:/src/alpha\"]\n"))
	w2, _ := zw.Create("projects/alpha/knowledge/bad.md")
	w2.Write([]byte("这不是 frontmatter"))
	w3, _ := zw.Create("projects/alpha/knowledge/good.md")
	w3.Write([]byte("---\ntitle: 好\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n"))
	zw.Close()
	rep, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 1 || rep.Skipped != 1 {
		t.Fatalf("report: %+v", rep)
	}
}

// zip-slip 与非法文件整包拒绝
func TestImportRejectsBadNames(t *testing.T) {
	setupHome(t)
	mk := func(name string) *bytes.Buffer {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("registry.toml")
		w.Write([]byte("[[project]]\nname=\"a\"\npaths=[\"x\"]\n"))
		w2, _ := zw.Create(name)
		w2.Write([]byte("x"))
		zw.Close()
		return &buf
	}
	for _, name := range []string{
		"projects/a/knowledge/../../evil.md",
		"/abs.md",
		"C:/evil.md",
		"projects\\a\\knowledge\\x.md",
		"random.txt",
	} {
		buf := mk(name)
		if err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len())); !errors.Is(err, ErrBadPackage) {
			t.Fatalf("%s: expected ErrBadPackage", name)
		}
	}
}

func TestImportTooBigAndMissingRegistry(t *testing.T) {
	setupHome(t)
	if _, err := Import(bytes.NewReader(nil), MaxSize+1); !errors.Is(err, ErrBadPackage) {
		t.Fatal("size limit")
	}
	// 无 registry.toml 的合法 zip
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("projects/a/knowledge/x.md")
	w.Write([]byte("---\ntitle: x\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\nb\n"))
	zw.Close()
	if _, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len())); !errors.Is(err, ErrBadPackage) {
		t.Fatal("missing registry.toml")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/backup/ -run TestImport -v`
Expected: 编译失败（Import 未定义）。

- [ ] **Step 3: 实现**

`backup.go` 追加（import 加 `"bytes"`（不需要——直接用 f.Open 读）、`"strings"`、`"openknowledge/internal/entry"`、`"openknowledge/internal/index"`、`"openknowledge/internal/store"`）：

```go
// Import 解包并写入知识库：注册缺失项目（同名已注册则合并进现有项目）、
// 条目同名覆盖、config 覆盖，最后逐项目 Sync 重建索引。
func Import(r io.ReaderAt, size int64) (*Report, error) {
	if size > MaxSize {
		return nil, fmt.Errorf("%w: 超过 32MB 上限", ErrBadPackage)
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: 不是有效的 zip", ErrBadPackage)
	}

	type item struct{ project, file string; data []byte }
	var regData []byte
	var entries []item
	var configs []item
	for _, f := range zr.File {
		if !validName(f.Name) {
			return nil, fmt.Errorf("%w: 非法路径 %q", ErrBadPackage, f.Name)
		}
		parts := strings.Split(f.Name, "/")
		switch {
		case f.Name == "registry.toml":
			if regData, err = readZipFile(f); err != nil {
				return nil, err
			}
		case len(parts) == 4 && parts[0] == "projects" && parts[2] == "knowledge" && strings.HasSuffix(parts[3], ".md"):
			data, err := readZipFile(f)
			if err != nil {
				return nil, err
			}
			entries = append(entries, item{parts[1], parts[3], data})
		case len(parts) == 3 && parts[0] == "projects" && parts[2] == "config.toml":
			data, err := readZipFile(f)
			if err != nil {
				return nil, err
			}
			configs = append(configs, item{parts[1], parts[2], data})
		default:
			return nil, fmt.Errorf("%w: 不允许的文件 %q", ErrBadPackage, f.Name)
		}
	}
	if regData == nil {
		return nil, fmt.Errorf("%w: 包内缺少 registry.toml", ErrBadPackage)
	}
	var zreg registry.Registry
	if err := toml.Unmarshal(regData, &zreg); err != nil {
		return nil, fmt.Errorf("%w: registry.toml 损坏", ErrBadPackage)
	}
	if len(entries) == 0 && len(configs) == 0 {
		return nil, fmt.Errorf("%w: 包内无有效条目", ErrBadPackage)
	}

	// 注册缺失项目（同名已注册则跳过——条目按项目名合并进现有目录）
	local, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, p := range local.Projects {
		known[p.Name] = true
	}
	changed := false
	for _, p := range zreg.Projects {
		if known[p.Name] {
			continue
		}
		path := ""
		if len(p.Paths) > 0 {
			path = p.Paths[0]
		}
		if err := local.AddProject(p.Name, path); err != nil {
			return nil, err
		}
		known[p.Name] = true
		changed = true
	}
	if changed {
		if err := local.Save(registry.DefaultPath()); err != nil {
			return nil, err
		}
	}

	rep := &Report{}
	seen := map[string]bool{}
	for _, it := range entries {
		if _, err := entry.Parse(it.data); err != nil {
			rep.Skipped++
			continue
		}
		dir := filepath.Join(registry.Home(), "projects", it.project, "knowledge")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, it.file), it.data, 0o644); err != nil {
			return nil, err
		}
		rep.Imported++
		seen[it.project] = true
	}
	for _, it := range configs {
		root := filepath.Join(registry.Home(), "projects", it.project)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(root, "config.toml"), it.data, 0o644); err != nil {
			return nil, err
		}
		seen[it.project] = true
	}
	for name := range seen {
		rep.Projects = append(rep.Projects, name)
	}
	sort.Strings(rep.Projects)

	// 逐项目重建索引（损坏条目告警不视为失败）
	for _, name := range rep.Projects {
		st := store.New(filepath.Join(registry.Home(), "projects", name))
		db, err := index.Open(st.KbPath())
		if err != nil {
			return nil, err
		}
		syncErr := db.Sync(st.KnowledgeDir(), nil)
		db.Close()
		var ce *index.CorruptEntriesError
		if syncErr != nil && !errors.As(syncErr, &ce) {
			return nil, syncErr
		}
	}
	return rep, nil
}

// validName 拒绝 zip-slip：绝对路径、盘符、反斜杠、.. 或空段。
func validName(name string) bool {
	if strings.HasPrefix(name, "/") || strings.ContainsRune(name, ':') || strings.ContainsRune(name, '\\') {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == ".." {
			return false
		}
	}
	return true
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
```

（`"sort"` import 补齐。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/backup/ -v`
Expected: 全部 PASS（含 Task 2 的 3 个）。

- [ ] **Step 5: Commit**

```bash
git add internal/backup/
git commit -m "feat: backup import with overwrite, zip-slip guard and index rebuild"
```

---

### Task 4: gui 导出/导入端点 + 最大化修复

**Files:**
- Modify: `internal/gui/api.go`（路由注册 api.go:44-64、apiStatus 之后追加两个端点函数）、`internal/gui/server.go:15-25`
- Test: `internal/gui/api_test.go`（追加）

**Interfaces:**
- Consumes: Task 2/3 的 `backup.Export/Import/MaxSize/ErrBadPackage/Report`。
- Produces（Task 5 前端依赖）：
  - `GET /api/export?project=<名|all>` → 200 zip 下载（`Content-Disposition: attachment; filename="openknowledge-backup-<project>-<yyyymmdd>.zip"`）；项目不存在 404；无 token 401。
  - `POST /api/import`（multipart 字段 `file`）→ 200 Report JSON；ErrBadPackage → 400；无 token 401。

- [ ] **Step 1: 写失败测试**

```go
func TestExportImportEndpoints(t *testing.T) {
	h, okHome, _ := newEnv(t)
	mkProject(t, okHome, "alpha", `D:\src\alpha`)
	// 写一条条目
	kdir := filepath.Join(okHome, "projects", "alpha", "knowledge")
	os.WriteFile(filepath.Join(kdir, "a.md"), []byte("---\ntitle: A\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n"), 0o644)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 鉴权
	if code, _ := do(t, "GET", srv.URL+"/api/export?project=all", "wrong", nil); code != 401 {
		t.Fatal("export auth")
	}
	if code, _ := do(t, "POST", srv.URL+"/api/import", "wrong", nil); code != 401 {
		t.Fatal("import auth")
	}
	// 导出
	code, data := do(t, "GET", srv.URL+"/api/export?project=all", testToken, nil)
	if code != 200 {
		t.Fatalf("export code=%d", code)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) == 0 {
		t.Fatalf("not a zip: %v", err)
	}
	// 项目不存在
	if code, _ := do(t, "GET", srv.URL+"/api/export?project=nope", testToken, nil); code != 404 {
		t.Fatal("export 404")
	}
	// 删掉条目后用刚导出的包导入
	os.Remove(filepath.Join(kdir, "a.md"))
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "backup.zip")
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/import", &body)
	req.Header.Set("X-Ok-Token", testToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rdata, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("import code=%d body=%s", res.StatusCode, rdata)
	}
	var rep struct {
		Imported int `json:"imported"`
	}
	json.Unmarshal(rdata, &rep)
	if rep.Imported != 1 {
		t.Fatalf("report: %s", rdata)
	}
	if _, err := os.Stat(filepath.Join(kdir, "a.md")); err != nil {
		t.Fatal("entry not restored via endpoint")
	}
}
```

（`mkProject` 的实际签名以 api_test.go:44-56 为准对齐；import 所需包：archive/zip、bytes、encoding/json、io、mime/multipart、net/http、os、path/filepath。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestExportImportEndpoints -v`
Expected: FAIL（404/未实现）。

- [ ] **Step 3: 实现**

`api.go` 路由注册块（api.go:63 之后）加：

```go
	api("GET /api/export", h.apiExport)
	api("POST /api/import", h.apiImport)
```

`api.go` 追加（imports 加 archive/zip 不需要——加 `"bytes"`、`"errors"`、`"io"`、`"time"`、`"openknowledge/internal/backup"`）：

```go
// apiExport 导出知识库 zip（project 缺省 all）。
func (h *Handler) apiExport(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = "all"
	}
	if project != "all" {
		reg, err := registry.Load(registry.DefaultPath())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		found := false
		for _, p := range reg.Projects {
			if p.Name == project {
				found = true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, "项目不存在: "+project)
			return
		}
	}
	filename := "openknowledge-backup-" + project + "-" + time.Now().Format("20060102") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if err := backup.Export(w, project); err != nil {
		log.Printf("export %s: %v", project, err) // 响应头已发，只能记日志
	}
}

// apiImport 导入知识库 zip（multipart 字段 file）。
func (h *Handler) apiImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxSize+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少 file 字段或超过大小上限")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取上传失败或超过 32MB 上限")
		return
	}
	rep, err := backup.Import(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if errors.Is(err, backup.ErrBadPackage) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
```

`server.go` 最大化修复——OpenBrowser 两处去掉 `go`，并更新包注释与函数注释：

```go
// OpenBrowser 以最大化窗口打开 Edge/Chrome 应用模式；失败退回默认浏览器（不保证最大化）。
// 注：cmd start 会吞 --start-maximized 参数，Edge 单实例时 Start-Process 的
// -WindowStyle 也会被既有进程吞掉，只能在窗口出现后事后 ShowWindow 兜底。
// maximizeWindowByTitle 必须同步调用：ok gui 开浏览器即退（daemon 托管生命周期），
// 协程会随进程退出而被杀——v2.1 的"不自动最大化"回归正源于此。
func OpenBrowser(url string) {
	for _, browser := range []string{"msedge", "chrome"} {
		ps := fmt.Sprintf("Start-Process %s -ArgumentList '--app=%s' -WindowStyle Maximized", browser, url)
		if err := exec.Command("powershell", "-NoProfile", "-Command", ps).Run(); err == nil {
			maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
			return
		}
	}
	_ = exec.Command("cmd", "/c", "start", url).Run()
	maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -v`
Expected: 全部 PASS（含既有）。

- [ ] **Step 5: Commit（两个）**

```bash
git add internal/gui/api.go internal/gui/api_test.go
git commit -m "feat: /api/export and /api/import endpoints for knowledge backup"
git add internal/gui/server.go
git commit -m "fix: maximize browser synchronously (ok gui exits immediately, goroutine died with it)"
```

---

### Task 5: 前端"其他"tab

**Files:**
- Modify: `web/index.html`、`web/app.js`、`web/style.css`

**Interfaces:**
- Consumes: Task 1 的 status 字段 `app_version`/`home`；Task 4 的两个端点；现有 `switchTab`（app.js:63-70）、`refreshStatus`（app.js:74-92）、`renderProjectSelect`（app.js:94-108）、`loadEntries`（app.js:146-152）、`state.lastVersion`（app.js:131-134）。
- Produces: 无（纯前端）。

- [ ] **Step 1: index.html 加 tab 与面板**

tab 按钮（index.html:19 附近，tab-guide 之后）：

```html
      <button id="tab-misc" type="button" class="tab">其他</button>
```

面板（page-guide section 之后，卡片样式类照搬 page-guide 内现有卡片——先读 index.html:69 起的引导页结构对齐类名）：

```html
    <section id="page-misc" class="page hidden">
      <div class="card">
        <h2>数据导出</h2>
        <div class="form-row">
          <select id="misc-export-project"></select>
          <button id="btn-export" type="button">导出</button>
        </div>
        <p class="hint">导出 registry 与条目（不含索引，导入时自动重建）。</p>
      </div>
      <div class="card">
        <h2>数据导入</h2>
        <div class="form-row">
          <input id="misc-import-file" type="file" accept=".zip">
          <button id="btn-import" type="button">导入</button>
        </div>
        <p id="misc-import-result" class="hint"></p>
      </div>
      <div class="card">
        <h2>关于</h2>
        <p id="misc-version" class="hint"></p>
        <p id="misc-home" class="hint"></p>
        <p id="misc-project-count" class="hint"></p>
      </div>
    </section>
```

- [ ] **Step 2: app.js 接线**

`switchTab`（app.js:63-70）改为三 tab：

```js
function switchTab(name) {
  $("tab-manage").classList.toggle("active", name === "manage");
  $("tab-guide").classList.toggle("active", name === "guide");
  $("tab-misc").classList.toggle("active", name === "misc");
  $("page-manage").classList.toggle("hidden", name !== "manage");
  $("page-guide").classList.toggle("hidden", name !== "guide");
  $("page-misc").classList.toggle("hidden", name !== "misc");
}
```

tab 点击绑定（找到现有 `$("tab-manage").addEventListener(...)` 处，照样加 `$("tab-misc")`）。

`renderProjectSelect`（app.js:94-108）末尾追加填充导出下拉（不动现有逻辑）：

```js
  var exp = $("misc-export-project");
  if (exp) {
    exp.innerHTML = "";
    var all = document.createElement("option");
    all.value = "all";
    all.textContent = "全部项目";
    exp.appendChild(all);
    (projects || []).forEach(function (p) {
      var o = document.createElement("option");
      o.value = p.name;
      o.textContent = p.name;
      exp.appendChild(o);
    });
  }
```

（`renderProjectSelect` 的形参名以实际为准；若它拿不到 projects 就在 `refreshStatus` 里用 `s.projects` 做同样填充。）

`refreshStatus`（app.js:74-92）里追加"关于"填充：

```js
  $("misc-version").textContent = "OpenKnowledge v" + (s.app_version || "dev");
  $("misc-home").textContent = "知识库目录：" + (s.home || "");
  $("misc-project-count").textContent = "已注册项目：" + ((s.projects || []).length) + " 个";
```

导出/导入绑定（文件尾部初始化区）：

```js
$("btn-export").addEventListener("click", async function () {
  var p = $("misc-export-project").value;
  var res = await fetch("/api/export?project=" + encodeURIComponent(p), {
    headers: { "X-Ok-Token": TOKEN },
  });
  if (!res.ok) {
    var e = await res.json().catch(function () { return {}; });
    alert("导出失败：" + (e.error || res.status));
    return;
  }
  var blob = await res.blob();
  var a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = "openknowledge-backup-" + p + ".zip";
  a.click();
  URL.revokeObjectURL(a.href);
});

$("btn-import").addEventListener("click", async function () {
  var out = $("misc-import-result");
  var f = $("misc-import-file").files[0];
  if (!f) {
    out.textContent = "请先选择 zip 文件";
    return;
  }
  var fd = new FormData();
  fd.append("file", f);
  var res = await fetch("/api/import", { method: "POST", headers: { "X-Ok-Token": TOKEN }, body: fd });
  var data = await res.json().catch(function () { return {}; });
  if (!res.ok) {
    out.textContent = "导入失败：" + (data.error || res.status);
    return;
  }
  out.textContent = "导入 " + data.imported + " 条，跳过 " + data.skipped +
    " 条（格式损坏），涉及项目：" + (data.projects || []).join("、") + "；同名条目已覆盖";
  state.lastVersion = 0;
  refreshStatus();
  loadEntries();
});
```

style.css：优先复用现有卡片/表单类（`.card`/`.form-row`/`.hint` 若不存在则照搬引导页实际类名）；原则上零新增样式，确有缺口再补最小量。

- [ ] **Step 3: 构建并手工自验**

Run: `bash scripts/build-dist.sh`（daemon 占用时先 `dist/ok.exe daemon stop`），然后 `./dist/ok.exe gui`：
Expected: 窗口自动最大化；第三个 tab"其他"可见；导出全部项目得到 zip；删掉一条目后导入恢复；版本显示 `OpenKnowledge v2.2.0`。

- [ ] **Step 4: Commit**

```bash
git add web/ dist/
git commit -m "feat: misc tab with export/import and version display"
```

（dist/ 是否入库以仓库现状为准——若 dist/ 被 .gitignore 忽略则只提交 web/。）

---

### Task 6: 文档 + 全量验证

**Files:**
- Create: `docs/changelogs/2026-07-28-gui-misc-tab.md`
- Modify: `README.md`、`docs/ARCHITECTURE.md`

- [ ] **Step 1: changelog**

仿 `docs/changelogs/2026-07-23-web-gui.md` 格式新建 `docs/changelogs/2026-07-28-gui-misc-tab.md`，标题 `# v2.2 GUI 其他 Tab：数据导出/导入 + 版本显示 + 最大化修复`，要点：`internal/backup`（32MB 上限、zip-slip 防护、同名覆盖、Sync 重建索引）、`/api/export|/api/import`、`/api/status` 加 `app_version`/`home`、`internal/version` + ldflags 注入（事实源 .iss）、OpenBrowser 同步最大化（回归根因：ok gui 即退杀协程）。

- [ ] **Step 2: README + ARCHITECTURE**

- README：GUI 特性列表加"其他 tab：数据导出/导入、版本显示"。
- ARCHITECTURE：§5 核心组件补 `internal/backup` 与 `internal/version` 两行；§9/§13 附近补 `/api/export`、`/api/import` 端点说明；§12 构建配置补 ldflags 版本注入；§6 OpenBrowser 描述改"同步最大化"。按各节现有行文风格简短补，不重写。

- [ ] **Step 3: 全量验证**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿。

- [ ] **Step 4: Commit**

```bash
git add docs/ README.md
git commit -m "docs: misc tab changelog and architecture updates"
```

---

## Self-Review 记录

- spec 覆盖：§2 backup 包→Task 2/3；§2 端点→Task 4；§2 版本号→Task 1；§2 最大化→Task 4；§3 前端→Task 5；§4 错误处理→各任务校验分支；§5 测试→各 Task Step 1。全覆盖。
- spec 微调记录：§3"关于"卡片条目数→项目数（status 无聚合，Global Constraints 已注明）；导出 filename 前端写死 `.zip` 名（不解析 Content-Disposition，简化）。
- 类型一致性：`Export(w io.Writer, project string) error`、`Import(r io.ReaderAt, size int64) (*Report, error)`、`ErrBadPackage`、`MaxSize`、Report 字段（imported/skipped/projects）在 Task 2/3/4 间拼写一致；status 字段 `app_version`/`home` 在 Task 1/5 间一致。
- 安全核对：withAuth 只认 `X-Ok-Token` 头，前端导出用 fetch+blob（不泄露 token 到 URL 历史），与现状一致。
