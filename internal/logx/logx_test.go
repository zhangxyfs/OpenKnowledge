package logx

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriterPrefixesEachLine(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf)
	fmt.Fprintln(w, "第一行")
	fmt.Fprintln(w, "second line")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	for _, ln := range lines {
		// 每行都以 "2006-01-02 15:04:05 " 形态的时间戳开头
		if len(ln) < len(TimestampFormat)+1 || ln[len(TimestampFormat)] != ' ' {
			t.Fatalf("line missing timestamp prefix: %q", ln)
		}
		ts, err := time.ParseInLocation(TimestampFormat, ln[:len(TimestampFormat)], time.Local)
		if err != nil {
			t.Fatalf("bad timestamp in %q: %v", ln, err)
		}
		if time.Since(ts) > time.Minute {
			t.Fatalf("timestamp not recent: %q", ln)
		}
	}
	if !strings.HasSuffix(lines[0], "第一行") || !strings.HasSuffix(lines[1], "second line") {
		t.Fatalf("content mangled: %q", buf.String())
	}
}

// 一行内容分多次 Write 到达（子进程按块 flush）时只出一个时间戳。
func TestWriterBuffersPartialLine(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf)
	if _, err := w.Write([]byte("part1-")); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("partial line must be buffered, got %q", buf.String())
	}
	if _, err := w.Write([]byte("part2\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.HasSuffix(got, " part1-part2\n") || strings.Count(got, "part") != 2 {
		t.Fatalf("joined line expected, got %q", got)
	}
}

func TestWriterConcurrentLines(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fmt.Fprintf(w, "goroutine-%d\n", i)
		}(i)
	}
	wg.Wait()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 20 {
		t.Fatalf("expected 20 intact lines, got %d: %q", len(lines), buf.String())
	}
	for _, ln := range lines {
		if !strings.HasPrefix(ln[len(TimestampFormat)+1:], "goroutine-") {
			t.Fatalf("interleaved line: %q", ln)
		}
	}
}
