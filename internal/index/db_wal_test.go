package index

import (
	"path/filepath"
	"testing"
)

// Open 必须以 WAL 模式打开库：默认 journal（回滚日志）下读写互斥，daemon 转发、
// 本地兜底与 GUI 并发访问时，注入查询会被同步写事务挡到 busy_timeout 耗尽，
// 该轮注入整体静默跳过。WAL 落库后此测试防回归（DSN 漏带 pragma 时立刻变红）。
func TestOpenEnablesWAL(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.sql.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	// WAL 是库级持久属性：重开同一文件仍应生效（首次 Open 已写进库文件）
	db2, err := Open(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var mode2 string
	if err := db2.sql.QueryRow(`PRAGMA journal_mode`).Scan(&mode2); err != nil {
		t.Fatal(err)
	}
	if mode2 != "wal" {
		t.Fatalf("reopened journal_mode = %q, want wal", mode2)
	}
}
