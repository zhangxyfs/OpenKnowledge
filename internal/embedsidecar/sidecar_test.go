package embedsidecar

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/embed"
)

// TestMain 模式：helper 进程伪装 llama-server（/health + 常驻）。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("OK_HELPER") != "1" {
		return
	}
	port := os.Getenv("OK_HELPER_PORT")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
	os.Exit(0)
}

// setupEnv：OK_HOME 隔离 + ServerCommand 替换为 helper 进程 + 假模型落盘。
func setupEnv(t *testing.T) (*Manager, embed.BuiltinModel) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	old := ServerCommand
	ServerCommand = func(path string, args ...string) *exec.Cmd {
		// 从 args 里挖 --port 值传给 helper
		port := ""
		for i, a := range args {
			if a == "--port" && i+1 < len(args) {
				port = args[i+1]
			}
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
		cmd.Env = append(os.Environ(), "OK_HELPER=1", "OK_HELPER_PORT="+port)
		return cmd
	}
	t.Cleanup(func() { ServerCommand = old })
	model := embed.BuiltinModel{ID: "fake", File: "fake.gguf", Size: 4, Pooling: "cls", Dim: 2}
	modelsDir := filepath.Join(home, "models")
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(model.InstalledPath(modelsDir), []byte("fake"), 0o644)
	rtDir := filepath.Join(home, "runtime")
	os.MkdirAll(rtDir, 0o755)
	os.WriteFile(filepath.Join(rtDir, serverExeName), []byte("x"), 0o755)
	mgr := &Manager{RuntimeDir: rtDir, ModelsDir: modelsDir, HealthTimeout: 10 * time.Second, IdleTimeout: 100 * time.Millisecond}
	t.Cleanup(mgr.Stop)
	return mgr, model
}

func TestEnsureHealthyAndStop(t *testing.T) {
	mgr, model := setupEnv(t)
	st, err := mgr.Ensure(model)
	if err != nil {
		t.Fatal(err)
	}
	if st.Port <= 0 || st.ModelID != "fake" || !st.Healthy() {
		t.Fatalf("%+v", st)
	}
	if LoadState() == nil {
		t.Fatal("state 应已落盘")
	}
	// 幂等：再次 Ensure 复用
	st2, err := mgr.Ensure(model)
	if err != nil || st2.Port != st.Port {
		t.Fatalf("应复用: %v %+v", err, st2)
	}
	mgr.Stop()
	if LoadState() != nil {
		t.Fatal("Stop 后 state 应删除")
	}
}

func TestReconcileLifecycle(t *testing.T) {
	mgr, model := setupEnv(t)
	now := time.Now() // 固定基准时间：Reconcile 的 now 参数化正是为测试确定性
	// 无 want 且 desired 未变 → 不拉起
	mgr.lastDesired = "fake"
	mgr.Reconcile(&model, now)
	if LoadState() != nil {
		t.Fatal("无 want 不应拉起")
	}
	// want → 拉起
	RequestStart()
	mgr.Reconcile(&model, now)
	st := LoadState()
	if st == nil || !st.Healthy() {
		t.Fatal("want 应触发拉起")
	}
	if WantPending() {
		t.Fatal("拉起后 want 应清除")
	}
	// 空闲超时 → 回收
	mgr.Reconcile(&model, now.Add(time.Hour))
	if LoadState() != nil {
		t.Fatal("空闲应回收")
	}
	// desired 消失 → 确保停止
	RequestStart()
	mgr.Reconcile(&model, now)
	mgr.Reconcile(nil, now)
	if LoadState() != nil {
		t.Fatal("desired 消失应停止")
	}
}

func TestWantFlagRoundTrip(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	if WantPending() {
		t.Fatal("初始无 want")
	}
	RequestStart()
	if !WantPending() {
		t.Fatal("want 应存在")
	}
	ClearWant()
	if WantPending() {
		t.Fatal("清除后无 want")
	}
}
