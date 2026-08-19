package config

import "strconv"

// SetInjectMandatoryMax 在 config.toml 的 [inject] 小节内 upsert 单个键
// mandatory_max_tokens：小节已存在则只替换/追加该键行（max_tokens、
// reinject_turns 与注释原样保留），小节不存在则文件尾追加 [inject] 块。
// 与 SetGate 的整段替换不同——[inject] 是多人共用笔触的小节，不能整段覆盖。
func SetInjectMandatoryMax(path string, n int) error {
	return upsertTomlKey(path, "inject", "mandatory_max_tokens", "mandatory_max_tokens = "+strconv.Itoa(n))
}
