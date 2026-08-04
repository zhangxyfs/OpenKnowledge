# 经验沉淀分流"新需求"到 wiki —— 设计文档

日期：2026-08-04
状态：已确认（用户批准）

## 背景与目标

现状的经验沉淀（`ok propose`）只覆盖"经验型"内容：踩坑、隐藏约定、非显而易见问题的解法。当会话产出的是"结构型"内容（新功能、新模块、重要架构/流程变化）时，没有机制引导 AI 把它沉淀为 wiki 条目——wiki 只能靠用户手动调用 openknowledge-wiki 技能或落后 nudge 提醒。

目标：经验沉淀环节识别"新需求型"内容，引导 AI 建议补 wiki 条目（而非、或同时写经验草稿）。两个判断场景都要覆盖：

1. AI 准备 propose 时的分类自查（这是新功能还是坑？）
2. `ok search` 查重发现该主题无任何 wiki 条目覆盖（知识空白）

方案定案 **A + B 结合**：技能文案给判断力，CLI 一行提示给兜底。

## 组件 1：propose 技能文案

文件：`internal/setupx/skills/openknowledge-propose/SKILL.md`（go:embed 进 ok.exe，经 `ok setup` / GUI"安装技能"分发到 `~/.agents/skills/`）。

新增"先分类"指引（放在"何时提议"之前）：

- **经验型**（踩坑、隐藏约定、非显而易见问题的解法）→ 走本技能 `ok propose` 记草稿；
- **结构型**（新功能、新模块、新子系统、重要架构/流程变化）→ 不是草稿，建议改用 openknowledge-wiki 技能新增/更新 wiki 条目（同名 `add --force` 重写）；
- **两者兼有** → 都记：wiki 条目记"是什么/怎么协作"，草稿记"坑"。

查重指引扩展（修改"何时不要提议"的查重条目）：

- `ok search` 查重时，若输出末尾出现 wiki 覆盖提示行（组件 2），且内容属于结构型，告诉用户"这是知识空白，建议补 wiki"。

## 组件 2：`ok search` wiki 覆盖提示

### index 层

`internal/index` 新增：

```go
// HasWikiMatch 报告检索词是否有 wiki 条目（draft=0 且 tags 含 wiki）覆盖。
// 仅看 FTS 关键词、不看向量——兜底启发式，误报无害；terms 为空返回 true。
func (db *DB) HasWikiMatch(terms []string) (bool, error)
```

实现：`entries_fts` MATCH（复用 `buildMatch`，空串时不查库直接返回 true）JOIN `entries` ON filename，过滤 `draft = 0 AND tags LIKE '%wiki%'`，`SELECT EXISTS(...)`。

### CLI 层

`internal/cli/cli.go` 的 `Search`：输出 hits 之后调用 `HasWikiMatch`；返回 false 时向 stdout 追加一行：

```
提示：该主题暂无 wiki 条目覆盖；若内容属于新功能/新模块，建议用 openknowledge-wiki 技能补充 wiki。
```

- 检查出错（MATCH 异常等）静默不提示——fail-open，search 主输出格式不变。
- 项目尚无 wiki / 全新主题时提示行总会出现：可接受，文案中"若内容属于…"把最终判断权留给 AI。

## 数据流

```
AI 会话（解决新需求）
  → openknowledge-propose 技能"先分类"
    → 经验型 → ok propose（草稿，待人批准）
    → 结构型 → ok search 查重
                 → 无 wiki 覆盖提示行 → openknowledge-wiki 技能
                   → ok add --type reference --tags wiki,<主题>（直接转正）
                   → INDEX.md Wiki 目录自动更新
```

## 错误处理

| 场景 | 行为 |
|------|------|
| `HasWikiMatch` 查询失败 | 不输出提示行（fail-open） |
| 查询词经 `retrieve.Terms` 后为空 | 不输出提示行 |
| 知识库无 wiki 条目 | 提示行正常出现（符合预期） |
| 技能未重新分发 | 旧技能行为不变，仅 CLI 提示生效（降级可接受） |

## 测试

沿用现有测试隔离模式（`t.Setenv("OK_HOME", t.TempDir())`）：

- `internal/index`：`HasWikiMatch` —— 有 wiki 条目命中 / 无 wiki 条目 / wiki 条目为 draft 不计入 / 空 terms 返回 true。
- `internal/cli`：`Search` —— 知识库含 wiki 覆盖时输出无提示行；无覆盖时输出含提示行。

## 分发与生效

- 技能文案：随 `ok setup` 或 GUI 引导页"安装技能"重新分发到 `~/.agents/skills/openknowledge-propose/`。
- CLI 提示：随 ok.exe 升级生效（下个版本 2.3.2 或并入 2.3.1，发版时定）。

## 明确不做（YAGNI）

- 不改 auto 模式 Stop hook 自省文案（propose 默认模式覆盖不到，且自省本来就会引导 AI 走技能）。
- 不做向量语义层面的 wiki 覆盖判断（启发式兜底，关键词够用）。
- 不自动写 wiki——只"建议"，写条目始终由 AI 走 openknowledge-wiki 技能完成。
