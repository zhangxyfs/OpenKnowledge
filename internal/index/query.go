package index

import (
	"sort"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

// Hit 是一条检索命中，携带注入所需的正文与摘要。
type Hit struct {
	Filename string
	Title    string
	Type     string
	Summary  string
	Body     string
	Tags     []string // 供注入层按分支过滤
	Score    float64
}

// buildMatch 构造 FTS5 MATCH 串：每个词元双引号包裹（防注入）后以 OR 连接。
func buildMatch(terms []string) string {
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		if t == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// Query 混合检索：score = α·归一BM25 + β·余弦。mandatory 与 draft 条目不参与；
// 仅返回 score>0 的命中，按分数降序、同分标题升序，截 top_n。
// terms 与 queryVec 均为空时返回空结果。
func (db *DB) Query(terms []string, queryVec []float32, cfg config.Retrieve) ([]Hit, error) {
	if len(terms) == 0 && len(queryVec) == 0 {
		return nil, nil
	}
	hits := map[string]*Hit{}

	// 关键词通道：FTS5 BM25。bm25 返回负值（越小越好），取 kw=-rank，
	// 归一化为 kw/(kw+6)。
	if match := buildMatch(terms); match != "" {
		rows, err := db.sql.Query(
			`SELECT e.filename, e.title, e.type, e.summary, e.body, e.tags,
				bm25(entries_fts, 10.0, 8.0, 3.0, 1.0) AS r
			FROM entries_fts JOIN entries e ON e.filename = entries_fts.filename
			WHERE entries_fts MATCH ? AND e.mandatory = 0 AND e.draft = 0`, match)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h Hit
			var tagsStr string
			var rank float64
			if err := rows.Scan(&h.Filename, &h.Title, &h.Type, &h.Summary, &h.Body, &tagsStr, &rank); err != nil {
				_ = rows.Close()
				return nil, err
			}
			h.Tags = splitTags(tagsStr)
			kw := -rank
			h.Score = cfg.Alpha * kw / (kw + 6)
			hits[h.Filename] = &h
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	// 语义通道：向量全量读入内存算余弦（万条毫秒级）。
	if len(queryVec) > 0 {
		rows, err := db.sql.Query(
			`SELECT e.filename, e.title, e.type, e.summary, e.body, e.tags, v.blob
			FROM vectors v JOIN entries e ON e.filename = v.filename
			WHERE e.mandatory = 0 AND e.draft = 0`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var filename, title, typ, summary, body, tagsStr string
			var blob []byte
			if err := rows.Scan(&filename, &title, &typ, &summary, &body, &tagsStr, &blob); err != nil {
				_ = rows.Close()
				return nil, err
			}
			cos := embed.Cosine(queryVec, decodeVector(blob))
			if h, ok := hits[filename]; ok {
				h.Score += cfg.Beta * cos
			} else if cos > 0 {
				hits[filename] = &Hit{
					Filename: filename, Title: title, Type: typ, Summary: summary, Body: body,
					Tags:  splitTags(tagsStr),
					Score: cfg.Beta * cos,
				}
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.Score > 0 {
			out = append(out, *h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Title < out[j].Title
	})
	if cfg.TopN > 0 && len(out) > cfg.TopN {
		out = out[:cfg.TopN]
	}
	return out, nil
}

// Mandatory 返回全部 mandatory 条目（用于基础注入）；草稿即使带 mandatory 标记也不注入。
func (db *DB) Mandatory() ([]Hit, error) {
	rows, err := db.sql.Query(
		`SELECT filename, title, type, body FROM entries WHERE mandatory = 1 AND draft = 0 ORDER BY filename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Filename, &h.Title, &h.Type, &h.Body); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// WikiEntry 是 Wiki 目录的一行。
type WikiEntry struct {
	Title    string
	Filename string
	Summary  string
	Branch   string
}

// WikiEntries 返回打 wiki 标签的已转正条目（按 title 排序）。
func (db *DB) WikiEntries() ([]WikiEntry, error) {
	rows, err := db.sql.Query(`SELECT title, filename, summary, tags FROM entries WHERE draft = 0 AND tags LIKE '%wiki%' ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WikiEntry
	for rows.Next() {
		var e WikiEntry
		var tagsStr string
		if err := rows.Scan(&e.Title, &e.Filename, &e.Summary, &tagsStr); err != nil {
			return nil, err
		}
		e.Branch = BranchOf(splitTags(tagsStr))
		out = append(out, e)
	}
	return out, rows.Err()
}

// HasBranchWiki 报告指定分支是否存在已转正的差异条目（wiki 标签且 branch 精确匹配）。
// 空分支（非 git/未知）直接 false：无分支 wiki 条目不是任何分支的差异条目。
func (db *DB) HasBranchWiki(branch string) (bool, error) {
	if branch == "" {
		return false, nil
	}
	entries, err := db.WikiEntries()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Branch == branch {
			return true, nil
		}
	}
	return false, nil
}

// WikiCount 返回 wiki 条目数（ok wiki mark 展示用）。
func (db *DB) WikiCount() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM entries WHERE draft = 0 AND tags LIKE '%wiki%'`).Scan(&n)
	return n, err
}

// HasWikiMatch 报告检索词是否有 wiki 条目（draft=0 且 tags 含 wiki）覆盖。
// 仅看 FTS 关键词、不看向量——兜底启发式，供 ok search 输出提示；terms 为空返回 true。
func (db *DB) HasWikiMatch(terms []string) (bool, error) {
	match := buildMatch(terms)
	if match == "" {
		return true, nil
	}
	var exists bool
	err := db.sql.QueryRow(
		`SELECT EXISTS(
			SELECT 1 FROM entries_fts JOIN entries e ON e.filename = entries_fts.filename
			WHERE entries_fts MATCH ? AND e.draft = 0 AND e.tags LIKE '%wiki%'
		)`, match).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
