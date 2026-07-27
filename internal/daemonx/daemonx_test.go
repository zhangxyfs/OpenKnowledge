package daemonx

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRemove(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	if _, err := Load(); err == nil {
		t.Fatal("missing file should error")
	}
	info := &Info{PID: 123, Port: 17888, Token: "tok", Fingerprint: "fp", StartedAt: time.Now().Format(time.RFC3339)}
	if err := Save(info); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || *got != *info {
		t.Fatalf("roundtrip %+v err=%v", got, err)
	}
	// Windows 的 Go 权限模型仅支持只读位，0600 无法在 Windows 上通过 Mode().Perm() 验证，
	// 故权限断言仅在类 Unix 平台生效（Save 仍以 0600 请求权限）。
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(Path()); fi.Mode().Perm() != 0o600 {
			t.Fatalf("perm = %v", fi.Mode())
		}
	}
	// 损坏文件 → error
	if err := os.WriteFile(Path(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("corrupt file should error")
	}
	if err := Remove(); err != nil {
		t.Fatal(err)
	}
	if err := Remove(); err != nil { // 幂等
		t.Fatal(err)
	}
}

func TestExeFingerprint(t *testing.T) {
	fp, err := ExeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(fp, "|")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "0" {
		t.Fatalf("bad fingerprint %q", fp)
	}
}

func TestHealthyAndShutdown(t *testing.T) {
	shutdown := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ok-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/shutdown" {
			shutdown = true
		}
		w.Write([]byte(`{"fingerprint":"fp"}`))
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	info := &Info{Port: port, Token: "tok"}
	if !info.Healthy(srv.Client()) {
		t.Fatal("should be healthy")
	}
	info.Token = "wrong"
	if info.Healthy(srv.Client()) {
		t.Fatal("wrong token should be unhealthy")
	}
	info.Token = "tok"
	info.Shutdown(srv.Client())
	if !shutdown {
		t.Fatal("shutdown not called")
	}
	// 端口不通 → unhealthy
	if (&Info{Port: 1, Token: "tok"}).Healthy(&http.Client{Timeout: 100 * time.Millisecond}) {
		t.Fatal("closed port should be unhealthy")
	}
	if got := info.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatalf("URL %q", got)
	}
}
