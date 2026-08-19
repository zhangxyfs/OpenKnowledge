package config

import "strconv"

// SetRetrieveDedupTurns 在 config.toml 的 [retrieve] 小节内 upsert 单个键
// dedup_turns（alpha/fusion 等其余键与 [retrieve.gate] 子表原样保留）。
// 供 GUI 引导页冷却轮数配置写盘。
func SetRetrieveDedupTurns(path string, n int) error {
	return upsertTomlKey(path, "retrieve", "dedup_turns", "dedup_turns = "+strconv.Itoa(n))
}
