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
