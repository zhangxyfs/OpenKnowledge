package daemon

import (
	"path/filepath"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/embedsidecar"
	"openknowledge/internal/registry"
)

// sidecarJanitorInterval sidecar 调和周期；测试可调小。
var sidecarJanitorInterval = 10 * time.Second

// desiredBuiltinModel 从全局配置解析期望的内置模型；非内置/未知清单 id 返回 nil。
func desiredBuiltinModel(cfg config.Config) *embed.BuiltinModel {
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Type != "builtin" {
		return nil
	}
	return embed.FindBuiltinModel(p.Model)
}

// sidecarJanitor 周期调和 embedding sidecar（active 为内置且模型就绪 → 在线；
// 空闲/切换/停用 → 回收）。配置变更经周期轮询自然生效（GUI/CLI 写全局配置）。
func sidecarJanitor(mgr *embedsidecar.Manager) {
	ticker := time.NewTicker(sidecarJanitorInterval)
	defer ticker.Stop()
	for range ticker.C {
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			continue
		}
		mgr.Reconcile(desiredBuiltinModel(cfg), time.Now())
	}
}
