package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net"
	"os"
	"testing"
	"time"

	"openknowledge/internal/daemonx"
)

// stubSpawn 替换 SpawnDetached，返回调用次数。
func stubSpawn(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	old := SpawnDetached
	SpawnDetached = func(exe, logPath string) error { *calls++; return nil }
	t.Cleanup(func() { SpawnDetached = old })
	return calls
}

func fakeDaemon(t *testing.T, fingerprint string, hr HookResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ok-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/health" {
			json.NewEncoder(w).Encode(map[string]string{"fingerprint": fingerprint})
			return
		}
		json.NewEncoder(w).Encode(hr)
	}))
}

func saveInfo(t *testing.T, port int, fp string) {
	t.Helper()
	if err := daemonx.Save(&daemonx.Info{PID: os.Getpid(), Port: port, Token: "tok", Fingerprint: fp}); err != nil {
		t.Fatal(err)
	}
}

func TestForwardHookOK(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	fp, err := daemonx.ExeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeDaemon(t, fp, HookResponse{Stdout: "注入内容", Code: 0})
	defer srv.Close()
	saveInfo(t, srv.Listener.Addr().(*net.TCPAddr).Port, fp)
	var out, errOut bytes.Buffer
	handled, code := ForwardHook("prompt", "", []byte(`{}`), &out, &errOut)
	if !handled || code != 0 || out.String() != "注入内容" {
		t.Fatalf("handled=%v code=%d out=%q", handled, code, out.String())
	}
}

func TestForwardHookStaleDaemonFallsBack(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	calls := stubSpawn(t)
	saveInfo(t, 1, "fp") // 端口 1 必然不通
	var out bytes.Buffer
	handled, _ := ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if handled {
		t.Fatal("unreachable daemon should not handle")
	}
	if *calls != 1 {
		t.Fatalf("expected 1 spawn, got %d", *calls)
	}
	// 15s 防抖：第二次不再 spawn
	handled, _ = ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if handled || *calls != 1 {
		t.Fatalf("debounce broken: handled=%v calls=%d", handled, *calls)
	}
}

func TestForwardHookVersionMismatch(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	calls := stubSpawn(t)
	srv := fakeDaemon(t, "old-fingerprint", HookResponse{})
	defer srv.Close()
	saveInfo(t, srv.Listener.Addr().(*net.TCPAddr).Port, "old-fingerprint")
	var out bytes.Buffer
	handled, _ := ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if handled {
		t.Fatal("version mismatch should not handle")
	}
	if *calls != 1 {
		t.Fatalf("expected respawn, got %d", *calls)
	}
}

func TestForwardHookTimeout(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	fp, _ := daemonx.ExeFingerprint()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			json.NewEncoder(w).Encode(map[string]string{"fingerprint": fp})
			return
		}
		time.Sleep(12 * time.Second)
	}))
	defer slow.Close()
	saveInfo(t, slow.Listener.Addr().(*net.TCPAddr).Port, fp)
	var out bytes.Buffer
	start := time.Now()
	handled, code := ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if handled || code != 0 {
		t.Fatalf("timeout should fail-open: handled=%v code=%d", handled, code)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("forward should time out at 9s")
	}
}
