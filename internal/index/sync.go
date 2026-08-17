package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/fsx"
	"openknowledge/internal/retrieve"
)

// ftsText 将原文切分为空格分隔的词元文本（复用 retrieve.Terms），
// 供 FTS 表入库；MATCH 查询使用同样的切分保证词元一致。
func ftsText(s string) string { return strings.Join(retrieve.Terms(s), " ") }

// CorruptEntriesError 表示同步已完成，但有文件因解析失败被跳过。
type CorruptEntriesError struct{ Files []string }

func (e *CorruptEntriesError) Error() string {
	return fmt.Sprintf("跳过 %d 个损坏条目: %s", len(e.Files), strings.Join(e.Files, ", "))
}

// readEntry 读取并解析单个条目文件；仅当 diff 判定条目变化
// （或未变化条目需要补向量）时才被调用。
func readEntry(path string) (*entry.Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	e, err := entry.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	e.Path = path
	return e, nil
}

// SyncOptions 控制 Sync 重建 INDEX.md 的渲染预算；零值走默认（MaxLines=50）。
type SyncOptions struct {
	MaxLines int // 主列表最大行数，<=0 按 50
}

// Sync 将 dir（knowledge 目录）下的 Markdown 条目增量同步进索引库：
// 先用 os.ReadDir 枚举文件名+mtime（不读文件内容），与 entries 表按
// filename+mtime 对比；仅新增/变化的条目才 read+parse 并 upsert
// （entries 存原文，entries_fts 存 ftsText 切分文本），client!=nil 时
// 收集变化条目与缺向量的未变化条目（只读这些文件）的 EmbedText，
// 提交前按 32 条一批调 EmbedDocuments 批量算向量写入 vectors 表；
// 提交后若 client 身份非空且确有向量写入，则刷新 meta 表的
// embedding_model/embedding_dim。client 身份与 meta 记录不符时
// 跳过全部向量写与 meta 更新（INDEX/FTS 照常），杜绝新旧模型向量
// 混合——需调用方显式 ClearVectors 后再同步以全量重建。
// 库中多余的 filename 删除。变化条目解析失败时跳过该文件（已索引旧行
// 保留，无旧行则缺席），其余条目照常提交——一个 YAML 笔误不能压制全部
// 注入；提交成功后若有跳过，返回 *CorruptEntriesError 警告（调用方用
// errors.As 区分）。SQL 失败、目录不可读、INDEX.md 写入失败等致命错误
// 仍中止并回滚。diff 非空或 INDEX.md 缺失时重建 <dir>/../INDEX.md，
// 无变化的纯热路径不做任何写盘。
func (db *DB) Sync(dir string, client embed.Client, opts ...SyncOptions) error {
	o := SyncOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	// Windows 上 DirEntry.Info 复用 readdir 数据，无额外系统调用
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type diskFile struct {
		name  string
		path  string
		mtime int64
	}
	var disk []diskFile
	for _, de := range dirents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			return err
		}
		disk = append(disk, diskFile{de.Name(), filepath.Join(dir, de.Name()), info.ModTime().Unix()})
	}

	existing := map[string]int64{}
	rows, err := db.sql.Query(`SELECT filename, mtime FROM entries`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var mtime int64
		if err := rows.Scan(&name, &mtime); err != nil {
			_ = rows.Close()
			return err
		}
		existing[name] = mtime
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	hasVector := map[string]bool{}
	vrows, err := db.sql.Query(`SELECT filename FROM vectors`)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var name string
		if err := vrows.Scan(&name); err != nil {
			_ = vrows.Close()
			return err
		}
		hasVector[name] = true
	}
	if err := vrows.Err(); err != nil {
		_ = vrows.Close()
		return err
	}
	_ = vrows.Close()

	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	// 模型身份闸：client 身份与索引 meta 不符时跳过全部向量写（INDEX/FTS 照常），
	// 杜绝新旧模型向量混合；由 ok index 显式 ClearVectors 后全量重建。
	// meta 空但有向量 = ≤2.13 历史库（向量身份不明），同样阻断待重建。
	embedBlocked := client == nil
	if client != nil && client.ModelIdentity() != "" {
		m, _, err := db.EmbeddingMeta()
		if err == nil {
			switch {
			case m != "" && m != client.ModelIdentity():
				embedBlocked = true // 身份不符
			case m == "":
				if hv, herr := db.HasVectors(); herr == nil && hv {
					embedBlocked = true // 历史向量无身份记录（≤2.13 库），阻断待 ok index 重建
				}
			}
		}
	}
	type pendingEmbed struct{ name, text string }
	var pending []pendingEmbed

	alive := map[string]bool{}
	changed := false
	var skipped []string
	for _, f := range disk {
		name := f.name
		alive[name] = true
		mtime := f.mtime
		if old, ok := existing[name]; ok && old == mtime {
			// 未变化条目不读不解析；仅在缺向量且可算向量时收集补齐
			if !embedBlocked && !hasVector[name] {
				e, err := readEntry(f.path)
				if err != nil {
					// 与 changed 路径同口径：损坏条目跳过、记入告警，不中止整轮
					// 同步（同秒写坏 mtime 未变的场景会走到这里——"一个 YAML 笔误
					// 不能压制全部注入"）
					skipped = append(skipped, name)
					continue
				}
				pending = append(pending, pendingEmbed{name, e.EmbedText()})
			}
			continue
		}
		changed = true
		e, err := readEntry(f.path)
		if err != nil {
			// 损坏条目跳过：已索引旧行保留（新文件则缺席），其余条目照常提交；
			// mtime 未入库，下次同步会重试并在修复后自动追上
			skipped = append(skipped, name)
			continue
		}
		tags := strings.Join(e.Tags, ", ")
		mandatory := 0
		if e.Mandatory {
			mandatory = 1
		}
		draft := 0
		if e.Draft {
			draft = 1
		}
		archived := 0
		if e.Archived {
			archived = 1
		}
		if _, err := tx.Exec(`INSERT INTO entries(filename,title,type,tags,summary,body,mandatory,draft,archived,mtime)
			VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(filename) DO UPDATE SET
			title=excluded.title, type=excluded.type, tags=excluded.tags,
			summary=excluded.summary, body=excluded.body,
			mandatory=excluded.mandatory, draft=excluded.draft,
			archived=excluded.archived, mtime=excluded.mtime`,
			name, e.Title, e.Type, tags, e.Summary, e.Body, mandatory, draft, archived, mtime); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`DELETE FROM entries_fts WHERE filename=?`, name); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`INSERT INTO entries_fts(title,tags,summary,body,filename) VALUES(?,?,?,?,?)`,
			ftsText(e.Title), ftsText(strings.Join(e.Tags, " ")), ftsText(e.Summary), ftsText(e.Body), name); err != nil {
			return rollback(err)
		}
		if !embedBlocked {
			pending = append(pending, pendingEmbed{name, e.EmbedText()})
		}
	}
	for name := range existing {
		if !alive[name] {
			changed = true
			for _, q := range []string{
				`DELETE FROM entries WHERE filename=?`,
				`DELETE FROM entries_fts WHERE filename=?`,
				`DELETE FROM vectors WHERE filename=?`,
			} {
				if _, err := tx.Exec(q, name); err != nil {
					return rollback(err)
				}
			}
		}
	}
	const embedBatchSize = 32
	vecDim := 0
	for i := 0; i < len(pending); i += embedBatchSize {
		j := i + embedBatchSize
		if j > len(pending) {
			j = len(pending)
		}
		texts := make([]string, 0, j-i)
		for _, p := range pending[i:j] {
			texts = append(texts, p.text)
		}
		vecs, err := client.EmbedDocuments(context.Background(), texts)
		if err != nil {
			return rollback(err)
		}
		for k, vec := range vecs {
			vecDim = len(vec)
			if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
				pending[i+k].name, len(vec), encodeVector(vec)); err != nil {
				return rollback(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if !embedBlocked && client != nil && vecDim > 0 && client.ModelIdentity() != "" {
		if err := db.SetMeta("embedding_model", client.ModelIdentity()); err != nil {
			return err
		}
		if err := db.SetMeta("embedding_dim", strconv.Itoa(vecDim)); err != nil {
			return err
		}
	}
	// 顺带 prune 60 天前的条目事件（统计性数据，失败不阻断 Sync）
	_ = db.PruneEvents(time.Now().Unix() - 60*86400)
	// diff 为空时跳过重写（hook 热路径除上方事件 prune 外零写盘）；INDEX.md 缺失时总是重建
	if !changed {
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "INDEX.md")); err == nil {
			// 补齐路径（未变化但缺向量）的跳过告警不能被热路径吞掉
			if len(skipped) > 0 {
				return &CorruptEntriesError{Files: skipped}
			}
			return nil
		}
	}
	if err := db.rebuildIndex(dir, o.MaxLines); err != nil {
		return err
	}
	if len(skipped) > 0 {
		return &CorruptEntriesError{Files: skipped}
	}
	return nil
}

// dedupSummary 摘要与标题冗余（规范化后相同/标题复读摘要主干/共有前缀≥摘要 80%）
// 时返回空串——渲染层兜底，存量"摘要复读标题"的条目无需回填。
// "标题复读摘要主干"判据：规范化后标题是摘要前缀，且标题长度≥摘要 40%——
// 仅首字偶然相同（如标题"短"与摘要"短甲……"）不算复读，摘要保留。
func dedupSummary(title, summary string) string {
	norm := func(s string) string {
		return strings.TrimRight(strings.TrimSpace(s), "。．.：:，,；;、 ")
	}
	t, s := norm(title), norm(summary)
	if s == "" || t == "" {
		return summary
	}
	tr, sr := []rune(t), []rune(s)
	n := 0
	for n < len(tr) && n < len(sr) && tr[n] == sr[n] {
		n++
	}
	if s == t {
		return ""
	}
	// 标题是摘要前缀且覆盖摘要主干（≥40%）：尾巴只是补充说明，省略摘要
	if n == len(tr) && float64(n) >= 0.4*float64(len(sr)) {
		return ""
	}
	if float64(n) >= 0.8*float64(len(sr)) {
		return ""
	}
	return summary
}

// indexRow 是 rebuildIndex 主列表渲染用的条目视图。
type indexRow struct {
	filename, title, typ, tags, summary string
	draft, weight                       int
	mtime                               int64
}

// rebuildIndex 从 entries 表重写 <dir>/../INDEX.md。主列表按价值排序
// （30 天窗口 采纳×2+注入×1 降序，平局按 mtime 降序再按 filename 升序；
// 草稿沉底），超过 maxLines 的尾部折叠为一行可检索提示；archived 条目
// 不进主列表（仍保留在库可检索）。wiki 目录节/分支差异节维持原有输出。
func (db *DB) rebuildIndex(dir string, maxLines int) error {
	if maxLines <= 0 {
		maxLines = 50
	}
	rows, err := db.sql.Query(`SELECT filename, title, type, tags, summary, draft, archived, mtime FROM entries`)
	if err != nil {
		return err
	}
	// FeedbackStats 失败静默降级（与 PruneEvents 一致）：权重全零退回 mtime/filename 序
	stats, _ := db.FeedbackStats(30)
	var main, drafts []indexRow
	for rows.Next() {
		var r indexRow
		var archived int
		if err := rows.Scan(&r.filename, &r.title, &r.typ, &r.tags, &r.summary, &r.draft, &archived, &r.mtime); err != nil {
			_ = rows.Close()
			return err
		}
		if archived != 0 {
			continue
		}
		// 已转正的 wiki 条目只进 Wiki 目录节（带链接），主列表不重复
		if r.draft == 0 && hasWikiTag(r.tags) {
			continue
		}
		// 带 branch: 标签的条目（无论类型）不进全分支共享的主列表：
		// branch 标签语义=分支专属——wiki 差异条目已在下方差异节，
		// 非 wiki 分支条目仍可按分支检索命中，只是不进共享目录
		if BranchOf(splitTags(r.tags)) != "" {
			continue
		}
		s := stats[r.filename]
		r.weight = 2*s.Adoptions + s.Injections
		if r.draft != 0 {
			drafts = append(drafts, r)
		} else {
			main = append(main, r)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	byValue := func(rs []indexRow) {
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].weight != rs[j].weight {
				return rs[i].weight > rs[j].weight
			}
			if rs[i].mtime != rs[j].mtime {
				return rs[i].mtime > rs[j].mtime
			}
			return rs[i].filename < rs[j].filename
		})
	}
	byValue(main)
	byValue(drafts)
	ordered := append(main, drafts...)

	var b strings.Builder
	b.WriteString("# 知识索引\n\n")
	shown := ordered
	var folded []indexRow
	if len(ordered) > maxLines {
		shown, folded = ordered[:maxLines], ordered[maxLines:]
	}
	for _, r := range shown {
		title := r.title
		if r.draft != 0 {
			title = "【草稿】" + title
		}
		if sum := dedupSummary(r.title, r.summary); sum != "" {
			fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, r.typ, r.tags, sum)
		} else {
			fmt.Fprintf(&b, "- **%s** (%s) [%s]\n", title, r.typ, r.tags)
		}
	}
	if len(folded) > 0 {
		writeFoldedLine(&b, folded)
	}
	if wikiEntries, err := db.WikiEntries(); err == nil && len(wikiEntries) > 0 {
		writeWikiLine := func(b *strings.Builder, we WikiEntry) {
			if we.Summary != "" {
				fmt.Fprintf(b, "- [%s](%s) — %s\n", we.Title, we.Filename, we.Summary)
			} else {
				fmt.Fprintf(b, "- [%s](%s)\n", we.Title, we.Filename)
			}
		}
		b.WriteString("\n## Wiki 目录\n\n")
		branches := map[string][]WikiEntry{}
		for _, we := range wikiEntries {
			if we.Branch == "" {
				writeWikiLine(&b, we)
			} else {
				branches[we.Branch] = append(branches[we.Branch], we)
			}
		}
		names := make([]string, 0, len(branches))
		for n := range branches {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "\n## 分支差异（%s）\n\n", n)
			for _, we := range branches[n] {
				writeWikiLine(&b, we)
			}
		}
	}
	return fsx.WriteFile(filepath.Join(filepath.Dir(dir), "INDEX.md"), []byte(b.String()), 0o644)
}

// writeFoldedLine 渲染溢出折叠行：条数 + 被折叠条目 tags 计数降序前 5。
func writeFoldedLine(b *strings.Builder, folded []indexRow) {
	counts := map[string]int{}
	for _, r := range folded {
		for _, tg := range splitTags(r.tags) {
			counts[tg]++
		}
	}
	if len(counts) == 0 {
		fmt.Fprintf(b, "- 另有 %d 条未列出，可用关键词/向量检索命中\n", len(folded))
		return
	}
	type kv struct {
		tag string
		n   int
	}
	pairs := make([]kv, 0, len(counts))
	for tg, n := range counts {
		pairs = append(pairs, kv{tg, n})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}
		return pairs[i].tag < pairs[j].tag
	})
	if len(pairs) > 5 {
		pairs = pairs[:5]
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = fmt.Sprintf("%s×%d", p.tag, p.n)
	}
	fmt.Fprintf(b, "- 另有 %d 条未列出（tags 分布：%s），可用关键词/向量检索命中\n", len(folded), strings.Join(parts, ", "))
}
