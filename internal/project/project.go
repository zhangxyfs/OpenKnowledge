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
	cfg, err := config.LoadMerged(st.ConfigPath(), filepath.Join(registry.Home(), "config.toml"))
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
