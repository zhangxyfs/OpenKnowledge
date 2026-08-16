package index

import "time"

// 事件类型：injected（条目被检索注入）/ adopted（本会话注入过的条目被读取=采纳）。
const (
	EventInjected = "injected"
	EventAdopted  = "adopted"
)

// RecordEvents 批量记录条目事件（同一 ts）。文件名一律原始大小写 basename
//（与 entries.filename / Hit.Filename 一致，否则统计对不上）。
func (db *DB) RecordEvents(kind string, filenames []string) error {
	if len(filenames) == 0 {
		return nil
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, f := range filenames {
		if _, err := tx.Exec(`INSERT INTO entry_events(filename, kind, ts) VALUES(?,?,?)`, f, kind, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// FeedbackStat 是一条目在统计窗口内的注入/采纳计数。
type FeedbackStat struct {
	Injections int
	Adoptions  int
}

// FeedbackStats 返回最近 windowDays 天内各条目的注入/采纳计数（30 天事件千级，
// 全表分组毫秒级）。windowDays<=0 按 30。
func (db *DB) FeedbackStats(windowDays int) (map[string]FeedbackStat, error) {
	if windowDays <= 0 {
		windowDays = 30
	}
	since := time.Now().Unix() - int64(windowDays)*86400
	rows, err := db.sql.Query(`SELECT filename, kind, COUNT(*) FROM entry_events WHERE ts >= ? GROUP BY filename, kind`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FeedbackStat{}
	for rows.Next() {
		var name, kind string
		var n int
		if err := rows.Scan(&name, &kind, &n); err != nil {
			return nil, err
		}
		s := out[name]
		switch kind {
		case EventInjected:
			s.Injections = n
		case EventAdopted:
			s.Adoptions = n
		}
		out[name] = s
	}
	return out, rows.Err()
}

// PruneEvents 删除 olderThan（Unix 秒）之前的事件。Sync 时顺带调用（60 天）。
func (db *DB) PruneEvents(olderThan int64) error {
	_, err := db.sql.Exec(`DELETE FROM entry_events WHERE ts < ?`, olderThan)
	return err
}
