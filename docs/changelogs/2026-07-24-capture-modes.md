# v2.1：经验沉淀双模式（草稿全链路 + Stop 自动自省 + GUI 沉淀卡）

## 草稿（draft）全链路

- `ok propose --title … [--type] [--tags] [--summary] [--file|--body]`：AI 面向的
  草稿写入——条目带 `draft: true`（mandatory 恒 false），只同步 INDEX.md
  （标【草稿】），不算向量、不参与检索与注入。
- `ok approve <文件>`：草稿转正（draft=false，其余字段原样保留），同步 INDEX
  并（有 embedding key 时）补算向量；同一秒内 mtime 未变时手动推进一秒，
  防止增量 diff 漏判。
- 检索与注入排除草稿；批准后自动参与（hook 查询前增量同步自然生效）。

## 沉淀模式：propose / auto

- `ok capture` 打印当前模式与 turn_interval；`ok capture propose|auto` 写项目
  `config.toml` 的 `[capture]` 小节（整段替换幂等，注释保留）。
- `propose`（默认）：AI 主动提议草稿，人批准。
- `auto`：Stop hook 在本轮有触碰文件且距上次提醒满 `turn_interval` 个 Stop 时，
  以 exit 2 阻断一次强制 AI 自省，值得沉淀则当场 propose 草稿；与 enforce
  判定同入口、自省优先。

## GUI（Task 22）

- 条目列表/详情 API 增加 `draft` 字段；管理页草稿条目带「草稿」徽标，
  操作列多「采纳」按钮（`POST /api/approve` → 刷新列表）。
- `POST /api/approve {project,file}`：等价 `ok approve`（缺文件/非草稿 400），
  批准后按合并配置带 embedding 客户端同步，失败降级只同步 INDEX。
- `GET /api/capture?project=` 返回 `{mode, turn_interval}`；
  `POST /api/capture {project,mode}` 写项目配置（非法模式 400）。
- 引导页新增「经验沉淀」卡片：显示当前模式与 turn_interval，
  「AI 提议（默认）」「Stop 自动提取」两个按钮一键切换。

## 技能

- 新增 `openknowledge-propose`（何时提议/查重/命令格式/告知"已记为草稿待批准"）
  与 `openknowledge-capture`（查看与切换沉淀模式），`ok setup` / GUI 一键安装，
  技能总数 3 → 5；GUI status 的技能安装检查同步覆盖新技能。
