# 2026-08-15 稳健性批次：会话状态并发竞态 + 检索准入补漏 + 统一原子写 + 日志时间戳

- **问题（会话状态竞态，最严重）**：宿主（如 Claude Code 并行工具调用）会并发派发多个
  hook 进程，各自对同一 `state/session-*.json` 做无锁的 Load→改→Save：后写者覆盖前写者
  （丢 Touched → changelog 规则漏拦）；`Save` 是裸 `os.WriteFile`（O_TRUNC），崩溃/并发读
  留下半截 JSON，而 `Load` 用 `_ = json.Unmarshal` 吞错当空状态 → `BaseInjected` 归零 →
  mandatory 全文 + INDEX **重复注入**。rxext sidecar 早已为同一竞态加了进程内互斥，多进程
  hook 路径却一直裸奔。
- **修复（state）**：新增 `state.Update`——O_EXCL 锁文件跨进程互斥（持有者 token 防误删、
  30s mtime 过期抢占死锁、5s 获取超时 fail-open），锁内重 Load 最新快照、重放修改、原子
  落盘。hook 全部 8 处状态写入点（TrackTouched/CheckStop/MarkBlocked/注入计数/
  WikiNudged/MergedChecked/RetrieveWarned/ResetBaseInjection）改造完毕，rxext 同步。
- **修复（检索准入两处缺陷）**：
  1. 负余弦否决——关键词通道先准入、语义通道随后 `score += β·cos`（cos 可为负），末尾
     `score > 0` 过滤把已过关键词门槛的条目静默丢弃，语义通道获得单方面否决权（与
     v2.16.0 "准入按通道独立"的设计意图直接矛盾）。修法：关键词准入条目总分设下限
     （1e-6）保住注入资格，语义分只影响排序；
  2. top_n 截断先于分支过滤——其他分支的条目白白挤占名额、被过滤后本分支条目无补位，
     注入条数稳定少于 top_n。新增 `QueryExBranch`：过滤下推进查询、先过滤后截断。
- **修复（落盘原子性）**：新增 `internal/fsx`（同目录随机 tmp + fsync + rename），INDEX.md、
  registry.toml、config.toml、六个宿主 settings 写入、备份导入、条目写入（cli/gui）、
  wiki 游标、sidecar 状态统一收口——崩溃/断电不再留半截文件，宿主也读不到截断的 JSON。
- **修复（注入预算）**：token 估算旧按 runes/2，中文实际约 1 token/字，预算系统性低估
  约 2 倍（默认 800 实际可塞 1000+ 真实 token）。改为按密度计（CJK 1/字、拉丁 1/4 字符），
  截断按密度扫描且预留截断标记成本；`max_tokens` 配负不再 panic（钳 0）。
- **修复（kimi）**：hook 命令 exe 加引号——路径含空格（`C:/Users/John Doe/`）时按空格
  分词断裂、hook 永不执行且无报错；识别正则同步兼容新旧两种格式（存量块照常自愈替换）。
- **新功能（日志时间戳）**：新增 `internal/logx`（按行加时间戳 Writer，格式与 ok.log
  一致）。`daemon.log`（ok daemon 入口包裹 stdout/stderr，含 http.Server ErrorLog 显式
  指向）与 `embed-sidecar.log`（llama-server 输出按行加时间戳 + 启动/退出分隔标记）全行
  带时间节点；半行跨多次 Write 缓冲到换行、并发写安全。
- **说明**：`MinScoreFloor` 注释 n≥40 笔误修正为 n≥30（代码行为未变）；`go build`/
  `go vet` 干净，全仓 `go test ./...` 27 包绿；新增测试覆盖负余弦保留关键词命中、分支
  过滤先于截断（含补位与空分支）、Update 字段合并与锁释放、logx 半行缓冲与并发行完整性。
