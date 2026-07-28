// Package backup 提供知识库导出/导入（zip）。叶子工具包。
package backup

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"openknowledge/internal/entry"
	"openknowledge/internal/index"
	"openknowledge/internal/registry"
	"openknowledge/internal/store"
)

// MaxSize 是导入包的大小上限。
const MaxSize = 32 << 20

// maxDecompressed 是导入包解压后的总大小上限（防 zip bomb）。包级变量以便测试调小。
var maxDecompressed int64 = 256 << 20

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

	type item struct {
		project, file string
		data          []byte
	}
	var regData []byte
	var entries []item
	var configs []item
	budget := maxDecompressed
	for _, f := range zr.File {
		if !validName(f.Name) {
			return nil, fmt.Errorf("%w: 非法路径 %q", ErrBadPackage, f.Name)
		}
		parts := strings.Split(f.Name, "/")
		switch {
		case f.Name == "registry.toml":
			if regData, err = readZipFile(f, &budget); err != nil {
				return nil, err
			}
		case len(parts) == 4 && parts[0] == "projects" && parts[2] == "knowledge" && strings.HasSuffix(parts[3], ".md"):
			data, err := readZipFile(f, &budget)
			if err != nil {
				return nil, err
			}
			entries = append(entries, item{parts[1], parts[3], data})
		case len(parts) == 3 && parts[0] == "projects" && parts[2] == "config.toml":
			data, err := readZipFile(f, &budget)
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

func readZipFile(f *zip.File, budget *int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, *budget+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > *budget {
		return nil, fmt.Errorf("%w: 解压后超过 256MB 上限", ErrBadPackage)
	}
	*budget -= int64(len(data))
	return data, nil
}
