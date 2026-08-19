package config

import "strconv"

// EffectiveDedupTurns 返回生效的冷却轮数：<0 归一为 0（关闭，fail-open 方向）。
func (r Retrieve) EffectiveDedupTurns() int {
	if r.DedupTurns < 0 {
		return 0
	}
	return r.DedupTurns
}

// SetRetrieveDedupTurns 在 config.toml 的 [retrieve] 小节内 upsert 单个键
// dedup_turns（alpha/fusion 等其余键与 [retrieve.gate] 子表原样保留）。
// 供 GUI 引导页冷却轮数配置写盘。
func SetRetrieveDedupTurns(path string, n int) error {
	return upsertTomlKey(path, "retrieve", "dedup_turns", "dedup_turns = "+strconv.Itoa(n))
}
