// Package logx 提供按行加时间戳的日志 Writer。
package logx

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"
)

// TimestampFormat 与 ok.log 一致的时间格式。
const TimestampFormat = "2006-01-02 15:04:05"

// Writer 给每行输出加时间戳前缀后转发到 w（多 goroutine 安全）。跨多次 Write
// 的无换行尾部片段缓冲到换行出现时一并输出——子进程日志按行 flush，一行内容
// 可能分多次到达，逐片段加前缀会把一行拆成多个时间戳。
type Writer struct {
	mu  sync.Mutex
	w   io.Writer
	buf []byte
}

// New 返回包裹 w 的按行加时间戳 Writer。
func New(w io.Writer) *Writer { return &Writer{w: w} }

func (t *Writer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			t.buf = append(t.buf, p...)
			break
		}
		var line []byte
		if len(t.buf) > 0 {
			t.buf = append(t.buf, p[:i+1]...)
			line = t.buf
		} else {
			line = p[:i+1]
		}
		if _, err := fmt.Fprintf(t.w, "%s %s", time.Now().Format(TimestampFormat), line); err != nil {
			return n, err
		}
		t.buf = t.buf[:0] // 写出后重置；line 曾别名 buf 也已用完
		p = p[i+1:]
	}
	return n, nil
}
