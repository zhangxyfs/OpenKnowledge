package embed

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeModelServer 按 Range 提供 content；记录是否收到 Range 头。
func fakeModelServer(content []byte, sawRange *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rg := r.Header.Get("Range")
		if rg == "" {
			w.Write(content)
			return
		}
		*sawRange = true
		var from int
		fmt.Sscanf(rg, "bytes=%d-", &from)
		if from >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(from)+"-"+strconv.Itoa(len(content)-1)+"/"+strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content[from:])
	}))
}

func testModel(content []byte) BuiltinModel {
	sum := sha256.Sum256(content)
	return BuiltinModel{ID: "t-model", Repo: "r/p", File: "m.gguf", Size: int64(len(content)), SHA256: fmt.Sprintf("%x", sum), Dim: 8}
}

func TestDownloadFull(t *testing.T) {
	content := []byte(strings.Repeat("abc123", 1000))
	srv := fakeModelServer(content, new(bool))
	defer srv.Close()
	m := testModel(content)
	dir := t.TempDir()
	var lastDone, lastTotal int64
	err := Download(context.Background(), srv.Client(), m, srv.URL, dir, func(d, t int64) { lastDone, lastTotal = d, t })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(m.InstalledPath(dir))
	if string(got) != string(content) {
		t.Fatal("内容不一致")
	}
	if lastDone != m.Size || lastTotal != m.Size {
		t.Fatalf("进度回调: %d/%d", lastDone, lastTotal)
	}
}

func TestDownloadResume(t *testing.T) {
	content := []byte(strings.Repeat("xyz789", 2000))
	var sawRange bool
	srv := fakeModelServer(content, &sawRange)
	defer srv.Close()
	m := testModel(content)
	dir := t.TempDir()
	// 预置半截 .part
	if err := os.WriteFile(m.InstalledPath(dir)+".part", content[:5000], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), srv.Client(), m, srv.URL, dir, nil); err != nil {
		t.Fatal(err)
	}
	if !sawRange {
		t.Fatal("应带 Range 头续传")
	}
	got, _ := os.ReadFile(m.InstalledPath(dir))
	if string(got) != string(content) {
		t.Fatal("续传后内容不一致")
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	content := []byte("content")
	srv := fakeModelServer(content, new(bool))
	defer srv.Close()
	m := testModel(content)
	m.SHA256 = strings.Repeat("0", 64)
	dir := t.TempDir()
	err := Download(context.Background(), srv.Client(), m, srv.URL, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("应报校验失败: %v", err)
	}
	if _, serr := os.Stat(m.InstalledPath(dir) + ".part"); !os.IsNotExist(serr) {
		t.Fatal("校验失败应删除 .part")
	}
}

func TestDownloadCancelKeepsPart(t *testing.T) {
	content := []byte(strings.Repeat("q", 1<<20))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content[:100])
		w.(http.Flusher).Flush()
		<-r.Context().Done() // 挂住直到客户端取消
	}))
	defer srv.Close()
	m := testModel(content)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_ = Download(ctx, srv.Client(), m, srv.URL, dir, nil)
	st, err := os.Stat(m.InstalledPath(dir) + ".part")
	if err != nil || st.Size() == 0 {
		t.Fatalf("取消应保留 .part 供续传: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, m.ID+".gguf")); !os.IsNotExist(err) {
		t.Fatal("取消不应产生正式文件")
	}
}
