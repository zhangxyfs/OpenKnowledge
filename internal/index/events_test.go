package index

import (
	"path/filepath"
	"testing"
	"time"
)

func openEventsDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRecordEventsAndStats(t *testing.T) {
	db := openEventsDB(t)
	// a.md：4 注入 0 采纳；b.md：3 注入 1 采纳
	if err := db.RecordEvents(EventInjected, []string{"a.md", "a.md", "b.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventInjected, []string{"a.md", "b.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventInjected, []string{"a.md", "b.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventAdopted, []string{"b.md"}); err != nil {
		t.Fatal(err)
	}
	stats, err := db.FeedbackStats(30)
	if err != nil {
		t.Fatal(err)
	}
	if s := stats["a.md"]; s.Injections != 4 || s.Adoptions != 0 {
		t.Fatalf("a.md stats 错: %+v", s)
	}
	if s := stats["b.md"]; s.Injections != 3 || s.Adoptions != 1 {
		t.Fatalf("b.md stats 错: %+v", s)
	}
	// 空列表 no-op
	if err := db.RecordEvents(EventInjected, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackStatsWindowAndPrune(t *testing.T) {
	db := openEventsDB(t)
	now := time.Now().Unix()
	old := now - 40*86400 // 40 天前，超出 30 天窗口
	// 直接 SQL 造一条旧事件（RecordEvents 只有 now）
	if _, err := db.sql.Exec(`INSERT INTO entry_events(filename, kind, ts) VALUES('old.md', 'injected', ?)`, old); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventInjected, []string{"new.md"}); err != nil {
		t.Fatal(err)
	}
	stats, err := db.FeedbackStats(30)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stats["old.md"]; ok {
		t.Fatal("窗口外事件不应统计")
	}
	if stats["new.md"].Injections != 1 {
		t.Fatalf("new.md 应统计: %+v", stats)
	}
	// PruneEvents 删 60 天前：40 天前的保留，61 天前的删除
	veryOld := now - 61*86400
	if _, err := db.sql.Exec(`INSERT INTO entry_events(filename, kind, ts) VALUES('ancient.md', 'injected', ?)`, veryOld); err != nil {
		t.Fatal(err)
	}
	if err := db.PruneEvents(now - 60*86400); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM entry_events WHERE filename = 'ancient.md'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("61 天前事件应被 prune")
	}
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM entry_events WHERE filename = 'old.md'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("40 天前事件应保留")
	}
}
