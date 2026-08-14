package embedsidecar

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
)

// ServerCommand 是 spawn 接缝：测试替换为 helper 进程。生产即 exec.Command。
var ServerCommand = func(path string, args ...string) *exec.Cmd { return exec.Command(path, args...) }

// serverExeName 随平台（windows=llama-server.exe）。
var serverExeName = map[bool]string{true: "llama-server.exe", false: "llama-server"}[runtime.GOOS == "windows"]

// RuntimeServerPath 返回 <runtimeDir>/llama-server[.exe]；缺失时报错（裸 exe
// 便携形态无 runtime 目录 → 内置模式不可用）。
func RuntimeServerPath(runtimeDir string) (string, error) {
	p := filepath.Join(runtimeDir, serverExeName)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("推理运行时缺失（%s）——内置模式仅安装版可用", p)
	}
	return p, nil
}

// DefaultRuntimeDir 返回 <exe 所在目录>/runtime。
func DefaultRuntimeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "runtime")
}

// DefaultModelsDir 返回 <exe 所在目录>/models（安装版即安装目录下）。
func DefaultModelsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "models")
}

// ModelsDir 解析生效的模型目录：配置优先，空则默认。
func ModelsDir(cfg config.Config) string {
	if cfg.Embedding.ModelsDir != "" {
		return cfg.Embedding.ModelsDir
	}
	return DefaultModelsDir()
}

// Manager 托管 sidecar 生命周期。仅 daemon 持有。
type Manager struct {
	RuntimeDir    string
	ModelsDir     string
	HealthTimeout time.Duration // Ensure 就绪等待上限（建议 90s）
	IdleTimeout   time.Duration // 空闲回收阈值（建议 10min）

	mu              sync.Mutex
	cmd             *exec.Cmd
	lastDesired     string
	failCount       int
	unhealthyStreak int
}

// Ensure 保证 model 对应 sidecar 在线（幂等）；返回可用 State。
func (m *Manager) Ensure(model embed.BuiltinModel) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := LoadState(); st != nil && st.ModelID == model.ID && st.Healthy() {
		m.failCount = 0
		return st, nil
	}
	m.stopLocked()
	server, err := RuntimeServerPath(m.RuntimeDir)
	if err != nil {
		return nil, err
	}
	modelPath := model.InstalledPath(m.ModelsDir)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("模型文件缺失: %s", modelPath)
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-m", modelPath,
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
		"--embeddings",
		"--pooling", model.Pooling,
	}
	cmd := ServerCommand(server, args...)
	hideWindow(cmd)
	logF, err := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		cmd.Stdout = logF
		cmd.Stderr = logF
	}
	if err := cmd.Start(); err != nil {
		if logF != nil {
			_ = logF.Close()
		}
		return nil, err
	}
	if logF != nil {
		_ = logF.Close() // 子进程已继承句柄
	}
	m.cmd = cmd
	st := &State{PID: cmd.Process.Pid, Port: port, ModelID: model.ID, StartedAt: time.Now(), LastUsed: time.Now()}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	deadline := time.Now().Add(m.HealthTimeout)
	for {
		if st.Healthy() {
			if err := writeState(st); err != nil {
				return nil, err
			}
			m.failCount = 0
			return st, nil
		}
		select {
		case err := <-waitCh:
			return nil, fmt.Errorf("llama-server 提前退出: %v（日志 %s）", err, logPath())
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("llama-server 就绪超时（%s）", m.HealthTimeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Stop 杀 sidecar 并删状态文件（幂等）。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_, _ = m.cmd.Process.Wait()
		m.cmd = nil
	} else if st := LoadState(); st != nil {
		// 跨进程残留（daemon 重启后 m.cmd 为空）：按 PID 杀
		if p, err := os.FindProcess(st.PID); err == nil {
			_ = p.Kill()
		}
	}
	_ = os.Remove(statePath())
}

// Reconcile 调和一次：desired=期望模型（nil=不需要 sidecar）。
// 拉起条件：desired 就绪 且（激活刚变化 或 want 标记 pending）；
// 停止条件：不需要/未就绪/模型切换/空闲超时/连续两轮不健康（进程崩溃）。
// 连续 3 次拉起失败进入冷却（直到 desired 变化重试）。
func (m *Manager) Reconcile(desired *embed.BuiltinModel, now time.Time) {
	desiredID := ""
	if desired != nil {
		desiredID = desired.ID
	}
	changed := desiredID != m.lastDesired
	m.lastDesired = desiredID
	if changed {
		m.failCount = 0
	}
	if desiredID == "" || !desired.Installed(m.ModelsDir) {
		m.Stop()
		ClearWant()
		return
	}
	st := LoadState()
	if st != nil && st.ModelID != desiredID {
		m.Stop()
		st = nil
	}
	if st == nil {
		if (changed || WantPending()) && m.failCount < 3 {
			if _, err := m.Ensure(*desired); err != nil {
				m.failCount++
			} else {
				ClearWant()
			}
		}
		return
	}
	if !st.Healthy() {
		m.unhealthyStreak++
		if m.unhealthyStreak >= 2 {
			m.Stop() // 连续两轮（≥10s）不健康判定死亡；want/激活门控下轮自然重拉
			return
		}
	} else {
		m.unhealthyStreak = 0
	}
	if now.Sub(st.LastUsed) > m.IdleTimeout {
		m.Stop()
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
