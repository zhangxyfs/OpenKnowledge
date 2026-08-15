package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"openknowledge/internal/fsx"
)

type Project struct {
	Name  string   `toml:"name"`
	Paths []string `toml:"paths"`
}

type Registry struct {
	Projects []Project `toml:"project"`
}

// Home 返回知识库根目录：OK_HOME 环境变量优先，否则真实用户目录下的 ~/.openknowledge。
// 真实目录解析对 HOME/USERPROFILE 重定向免疫——CodePilot 等宿主 spawn 子进程时会把
// 它们重定向到 shadow 临时目录做 provider 隔离，跟随重定向会看到空数据根而静默失效。
func Home() string {
	if h := os.Getenv("OK_HOME"); h != "" {
		return h
	}
	if home, err := realProfileDir(); err == nil && home != "" {
		return filepath.Join(home, ".openknowledge")
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
	return fsx.WriteFile(path, []byte(buf.String()), 0o644)
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

// RemoveProject 按名移除项目，返回是否找到；持久化需另调 Save。
func (r *Registry) RemoveProject(name string) bool {
	for i, p := range r.Projects {
		if p.Name == name {
			r.Projects = append(r.Projects[:i], r.Projects[i+1:]...)
			return true
		}
	}
	return false
}

// HooksDisabled 报告 hooks 全局开关是否关闭（标志文件存在）。
func HooksDisabled() bool {
	_, err := os.Stat(filepath.Join(Home(), "hooks-disabled"))
	return err == nil
}
