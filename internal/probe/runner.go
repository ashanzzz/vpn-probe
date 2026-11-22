package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// TailBuffer 保留最后固定字节的线程安全日志缓冲区
type TailBuffer struct {
	mu      sync.Mutex
	buf     []byte
	maxSize int
}

func NewTailBuffer(maxSize int) *TailBuffer {
	if maxSize <= 0 {
		maxSize = 8192
	}
	return &TailBuffer{
		buf:     make([]byte, 0, maxSize),
		maxSize: maxSize,
	}
}

func (b *TailBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.maxSize {
		cut := len(b.buf) - b.maxSize
		b.buf = b.buf[cut:]
	}
	return len(p), nil
}

func (b *TailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func (b *TailBuffer) RecentLines(n int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	trimmed := strings.TrimSpace(string(b.buf))
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	var cleaned []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			cleaned = append(cleaned, l)
		}
	}
	if len(cleaned) <= n {
		return strings.Join(cleaned, " | ")
	}
	return strings.Join(cleaned[len(cleaned)-n:], " | ")
}

// SandboxInstance 运行中的临时 sing-box 代理沙箱
type SandboxInstance struct {
	ID         string
	Port       int
	ConfigFile string
	Cmd        *exec.Cmd
	LogBuf     *TailBuffer
	Cancel     context.CancelFunc
	mu         sync.Mutex
	closed     bool
}

func (s *SandboxInstance) GetRecentLogs(n int) string {
	if s.LogBuf != nil {
		return s.LogBuf.RecentLines(n)
	}
	return ""
}

// SandboxManager 负责分配端口、生成配置、调度临时 sing-box
type SandboxManager struct {
	singboxBin string
	portStart  int
	portEnd    int
	mu         sync.Mutex
	usedPorts  map[int]bool
}

func NewSandboxManager(binPath string, portStart, portEnd int) *SandboxManager {
	if binPath == "" || binPath == "sing-box" {
		if _, err := exec.LookPath("sing-box"); err != nil {
			if _, err := os.Stat("./bin/sing-box.exe"); err == nil {
				binPath = "./bin/sing-box.exe"
			} else if _, err := os.Stat("./bin/sing-box"); err == nil {
				binPath = "./bin/sing-box"
			}
		}
	}
	if binPath == "" {
		binPath = "sing-box"
	}
	if portStart <= 0 || portEnd <= portStart {
		portStart = 20000
		portEnd = 28000
	}
	return &SandboxManager{
		singboxBin: binPath,
		portStart:  portStart,
		portEnd:    portEnd,
		usedPorts:  make(map[int]bool),
	}
}

// Launch 针对指定的 outbound 配置启动一个隔离的 sing-box 进程
func (m *SandboxManager) Launch(ctx context.Context, outbound map[string]any) (*SandboxInstance, error) {
	port, err := m.allocatePort()
	if err != nil {
		return nil, fmt.Errorf("allocate port: %w", err)
	}

	instanceID := uuid.New().String()[:8]
	tmpDir := os.TempDir()
	confPath := filepath.Join(tmpDir, fmt.Sprintf("vpn-probe-%s-%d.json", instanceID, port))

	// 构建最小化 sing-box 配置 (不配置代理自身 detour 的 DNS，由系统原生直连解析 proxy host，避免域名/CDN 节点陷入死锁)
	tagStr, _ := outbound["tag"].(string)
	config := map[string]any{
		"log": map[string]any{
			"level":    "debug",
			"disabled": false,
		},
		"inbounds": []any{
			map[string]any{
				"type":        "mixed",
				"tag":         "mixed-in",
				"listen":      "127.0.0.1",
				"listen_port": port,
			},
		},
		"outbounds": []any{
			outbound,
			map[string]any{
				"type": "direct",
				"tag":  "direct",
			},
		},
		"route": map[string]any{
			"final": tagStr,
		},
	}

	confBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		m.releasePort(port)
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(confPath, confBytes, 0644); err != nil {
		m.releasePort(port)
		return nil, fmt.Errorf("write config file: %w", err)
	}

	logBuf := NewTailBuffer(8192)
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, m.singboxBin, "run", "-c", confPath)
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf

	if err := cmd.Start(); err != nil {
		cancel()
		os.Remove(confPath)
		m.releasePort(port)
		return nil, fmt.Errorf("start sing-box (%s): %w", m.singboxBin, err)
	}

	inst := &SandboxInstance{
		ID:         instanceID,
		Port:       port,
		ConfigFile: confPath,
		Cmd:        cmd,
		LogBuf:     logBuf,
		Cancel:     cancel,
	}

	// 轮询等待入站端口处于监听状态（最多等待 3 秒）
	if err := waitForPort(cmd, logBuf, port, 3*time.Second); err != nil {
		outMsg := strings.TrimSpace(logBuf.String())
		inst.Close(m)
		if outMsg != "" {
			return nil, fmt.Errorf("wait for proxy port %d ready [sing-box: %s]: %w", port, outMsg, err)
		}
		return nil, fmt.Errorf("wait for proxy port %d ready: %w", port, err)
	}

	return inst, nil
}

func (s *SandboxInstance) Close(m *SandboxManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true

	if s.Cancel != nil {
		s.Cancel()
	}
	if s.Cmd != nil && s.Cmd.Process != nil {
		_ = s.Cmd.Process.Kill()
		_ = s.Cmd.Wait()
	}
	if s.ConfigFile != "" {
		_ = os.Remove(s.ConfigFile)
	}
	if m != nil {
		m.releasePort(s.Port)
	}
}

func (m *SandboxManager) allocatePort() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for tries := 0; tries < 200; tries++ {
		p := m.portStart + rand.Intn(m.portEnd-m.portStart)
		if m.usedPorts[p] {
			continue
		}
		// 校验系统当前是否空闲
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			ln.Close()
			m.usedPorts[p] = true
			return p, nil
		}
	}
	return 0, fmt.Errorf("no available port in range %d-%d", m.portStart, m.portEnd)
}

func (m *SandboxManager) releasePort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.usedPorts, port)
}

func waitForPort(cmd *exec.Cmd, logBuf *TailBuffer, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		if cmd != nil && cmd.Process != nil && cmd.Process.Pid > 0 {
			if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
				outMsg := ""
				if logBuf != nil {
					outMsg = strings.TrimSpace(logBuf.String())
				}
				if outMsg != "" {
					return fmt.Errorf("sing-box process exited prematurely: %s", outMsg)
				}
				return fmt.Errorf("sing-box process exited prematurely: %w", err)
			}
		}
		conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", addr)
}
