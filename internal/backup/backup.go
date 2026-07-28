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
