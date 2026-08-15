package index

import (
	"math"
	"sort"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

// MinScoreFloor 计算生效的最低分数阈值。FTS5 bm25 的 idf 在小语料库下趋近于 0
// （N=2 时单文档词 idf 恰好为 0），固定绝对阈值会误伤小库的真实命中；条目数
// n<10 时关闭阈值（命中即注入，小库注入成本低），n≥40 时取配置值，中间线性过渡。
// minScore<=0 恒返回 0（用户显式关闭，旧语义 score>0 即注入）。
func MinScoreFloor(minScore float64, n int) float64 {
	if minScore <= 0 {
		return 0
	}
	switch {
	case n < 10:
		return 0
	case n >= 30:
		return minScore
	default:
		return minScore * float64(n-10) / 20
	}
}

// SemanticFloor 计算语义通道准入门槛（模型无关）。余弦的绝对分布随 embedding
// 模型变化：实测同一组查询，bge-m3 的跨域噪声可达 0.52、qwen3 仅 0.26，固定绝对
// 阈值要么漏噪声（bge-m3）要么误杀低对比度模型，自定义模型分布完全未知。故以
// 本次查询的余弦分布为参照：头部（max）相对中位数有显著分离（相对 gap ≥ minGap，
// 默认 0.25，BGE/Qwen 四模型 12 场景标定）时启用相对门槛 max(floor, median+0.5·gap)；
// 无显著头部时返回 +Inf——语义通道整体不准入（宁缺毋滥，关键词通道兜底）。
// 低对比度自定义模型可调低 min_gap 放宽；minGap<=0 关闭 gap 判定（仅绝对下限）。
// floor<=0（小库/用户关闭）返回 0 即旧语义（cos>0 即准入）；样本不足（<3）退回
// 绝对下限 floor。
func SemanticFloor(coses []float64, floor, minGap float64) float64 {
	if floor <= 0 {
		return 0
	}
	if len(coses) < 3 {
		return floor
	}
	sorted := append([]float64(nil), coses...)
	sort.Float64s(sorted)
	max := sorted[len(sorted)-1]
	if max <= 0 {
		return floor
	}
	median := sorted[len(sorted)/2]
	if minGap > 0 {
		relGap := (max - median) / max
		if relGap < minGap {
			return math.Inf(1)
		}
	}
	if sem := median + (max-median)*0.5; sem > floor {
		return sem
	}
	return floor
}

// QueryInfo 描述一次 Query 的语义通道诊断（供 hook 日志 / ok search 提示 / GUI
// 日志页展示）。仅当语义通道参与（向量存在）且样本 ≥3 时有意义。
type QueryInfo struct {
	// SemanticRejected：语义通道参与但全部候选被门槛拒绝（无显著头部）。
	SemanticRejected bool
	Coses            int
	MaxCos           float64
	MedianCos        float64
	RelGap           float64
}

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

// Query 混合检索：score = α·归一BM25 + β·余弦（总分仅用于排序）。准入按通道
// 独立判定：关键词通道需归一 BM25 分（未乘 α）≥ floor，语义通道需余弦 ≥
// SemanticFloor(cos 分布, floor)（模型无关相对门槛），满足其一即可注入；
// floor = MinScoreFloor(cfg.MinScore, 库条目数)，随库规模缩放，
// MinScore<=0 维持旧语义（score>0 即注入）。同域语料下无关文本的余弦基线可能
// 高达 0.4+，若用混合总分做准入会把伪词关键词命中顶过阈值，故必须分通道。
// mandatory 与 draft 条目不参与；结果按总分降序、同分标题升序，截 top_n——不强行
// 凑满。terms 与 queryVec 均为空时返回空结果。
func (db *DB) Query(terms []string, queryVec []float32, cfg config.Retrieve) ([]Hit, error) {
	hits, _, err := db.QueryEx(terms, queryVec, cfg)
	return hits, err
}

func (db *DB) QueryEx(terms []string, queryVec []float32, cfg config.Retrieve) ([]Hit, QueryInfo, error) {
	if len(terms) == 0 && len(queryVec) == 0 {
		return nil, QueryInfo{}, nil
	}
	hits := map[string]*Hit{}
	// 通道准入阈值按可检索条目数缩放（Count 失败时关闭阈值，fail-open 不阻断注入）
	floor := 0.0
	if n, err := db.Count(); err == nil {
		floor = MinScoreFloor(cfg.MinScore, n)
	}
	var info QueryInfo

	// 关键词通道：FTS5 BM25。bm25 返回负值（越小越好），取 kw=-rank，
	// 归一化为 kw/(kw+6)。
	if match := buildMatch(terms); match != "" {
		rows, err := db.sql.Query(
			`SELECT e.filename, e.title, e.type, e.summary, e.body, e.tags,
				bm25(entries_fts, 10.0, 8.0, 3.0, 1.0) AS r
			FROM entries_fts JOIN entries e ON e.filename = entries_fts.filename
			WHERE entries_fts MATCH ? AND e.mandatory = 0 AND e.draft = 0`, match)
		if err != nil {
			return nil, QueryInfo{}, err
		}
		for rows.Next() {
			var h Hit
			var tagsStr string
			var rank float64
			if err := rows.Scan(&h.Filename, &h.Title, &h.Type, &h.Summary, &h.Body, &tagsStr, &rank); err != nil {
				_ = rows.Close()
				return nil, QueryInfo{}, err
			}
			h.Tags = splitTags(tagsStr)
			kw := -rank
			// 准入看未乘 α 的归一 BM25 分（用户调 α 不应改变通道准入门槛）
			if floor > 0 && kw/(kw+6) < floor {
				continue
			}
			h.Score = cfg.Alpha * kw / (kw + 6)
			hits[h.Filename] = &h
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, QueryInfo{}, err
		}
		_ = rows.Close()
	}

	// 语义通道：向量全量读入内存算余弦（万条毫秒级），先收集分布再按
	// SemanticFloor（模型无关相对门槛）准入。已有关键词准入的条目只加总分
	// （排序用），不受语义门槛影响。
	if len(queryVec) > 0 {
		rows, err := db.sql.Query(
			`SELECT e.filename, e.title, e.type, e.summary, e.body, e.tags, v.blob
			FROM vectors v JOIN entries e ON e.filename = v.filename
			WHERE e.mandatory = 0 AND e.draft = 0`)
		if err != nil {
			return nil, QueryInfo{}, err
		}
		type cand struct {
			h   Hit
			cos float64
		}
		var cands []cand
		coses := make([]float64, 0, 64)
		for rows.Next() {
			var filename, title, typ, summary, body, tagsStr string
			var blob []byte
			if err := rows.Scan(&filename, &title, &typ, &summary, &body, &tagsStr, &blob); err != nil {
				_ = rows.Close()
				return nil, QueryInfo{}, err
			}
			cos := embed.Cosine(queryVec, decodeVector(blob))
			if cos > 0 {
				coses = append(coses, cos)
			}
			cands = append(cands, cand{Hit{
				Filename: filename, Title: title, Type: typ, Summary: summary, Body: body,
				Tags: splitTags(tagsStr),
			}, cos})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, QueryInfo{}, err
		}
		_ = rows.Close()
		semFloor := SemanticFloor(coses, floor, cfg.MinGap)
		semAdmitted := false
		for _, c := range cands {
			if h, ok := hits[c.h.Filename]; ok {
				h.Score += cfg.Beta * c.cos
			} else if c.cos > 0 && (semFloor == 0 || c.cos >= semFloor) {
				c.h.Score = cfg.Beta * c.cos
				hits[c.h.Filename] = &c.h
				semAdmitted = true
			}
		}
		// 语义诊断：样本足够却无任何语义准入（无显著头部）——供日志/提示/GUI 展示
		if len(coses) >= 3 && !semAdmitted {
			sorted := append([]float64(nil), coses...)
			sort.Float64s(sorted)
			max := sorted[len(sorted)-1]
			median := sorted[len(sorted)/2]
			info = QueryInfo{
				SemanticRejected: true,
				Coses:            len(coses),
				MaxCos:           max,
				MedianCos:        median,
			}
			if max > 0 {
				info.RelGap = (max - median) / max
			}
		}
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
	return out, info, nil
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
