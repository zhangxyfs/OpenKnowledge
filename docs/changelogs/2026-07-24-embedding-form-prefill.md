# embedding 表单回填 + key 留空保留

- 问题：GUI 保存 embedding 后重开页面表单为空，看起来像"保存不上"（实际已落盘）。
- `GET /api/status` 新增 `embedding: {base_url, model, has_key}`（不回显 key 本体）。
- `POST /api/setup/embedding`：`api_key` 留空表示保留已保存的 key（从未保存过才报 400）。
- 前端：状态加载时回填 base_url/model；key 输入框占位符显示"已保存（留空保持不变）"。
- 附带发现并恢复：web/ 源文件曾被误删（git 恢复，dist/web 未受影响）。
