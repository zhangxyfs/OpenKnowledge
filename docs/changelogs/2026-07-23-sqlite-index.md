# 检索换线到 SQLite 索引（kb.db），退役文件扫描路径

v1.3 架构变更：检索从"逐文件扫描 Markdown + vectors.json 向量缓存"切换为
Task 14 交付的 SQLite+FTS5 索引库 `kb.db`（`internal/index`）。

- `hook.HandlePrompt`：打开 kb.db → **查询前增量同步**（`Sync`，按 filename+mtime，
  仅为变化条目重算向量，无 key 时跳过）→ 基础注入改自 `Mandatory()` + INDEX.md →
  检索改走 `Query`（FTS5 BM25 + 余弦混合，α/β/top_n 语义不变）。全程 fail-open：
  打开/同步/查询失败只记 ok.log 后 exit 0。手工编辑条目后下次提问自动生效，
  无需手动 `ok index`。
- `ok add`：后处理改为 `index.Open` + `Sync`（embedding 失败降级为只同步 INDEX，
  向量稍后 `ok index` 补齐——`Sync` 会为缺向量的未变化条目补算）。
- `ok search`：改走 `index.Query`，输出格式不变（`%.2f\ttitle (filename)`）。
- `ok index`：改为增量 `Sync` + `Count()` 打印条目数；保留"无 key 时 INDEX 已重建、
  退出码 1"的语义。
- 退役代码：删除 `retrieve.Rank/KeywordScore/Scored`（`retrieve` 只剩 `Terms`）、
  删除 `internal/embed/vectors.go`（VectorSet/LoadVectors）、`store.VectorsPath/
  RebuildIndex/IndexContent` 及对应测试。
- 迁移：旧版 `vectors.json` 首次打开 kb.db 时自动导入 vectors 表并改名为
  `vectors.json.bak`（迁移解析已内联进 index 包，不再依赖 embed.VectorSet）。
- 新增 `store.KbPath()`；go.mod 升至 go 1.25、新增 modernc.org/sqlite 依赖（Task 14）。
- 集成测试新增断言：手改条目文件（INDEX 过期）后，下一次 `hook prompt` 命中新内容
  且 INDEX.md 被重建（验证查询前增量同步）。
