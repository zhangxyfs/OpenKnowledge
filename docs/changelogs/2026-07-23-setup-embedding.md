# ok setup 交互式 embedding 配置 + 全局配置合并

- 新增全局配置 ~/.openknowledge/config.toml：内置默认 ← 全局 ← 项目，后者覆盖前者。
- embedding 新增 api_key 字段（0600 落盘），ResolvedAPIKey 优先取字段、其次环境变量。
- ok setup 增加 embedding 交互配置（base_url/model/API key，回车跳过），支持
  --embedding-base-url/--embedding-model/--embedding-key 非交互 flags，写完即验连通性。
- ok init 的项目配置模板不再预置 embedding/inject/retrieve，缺省全部继承全局。
