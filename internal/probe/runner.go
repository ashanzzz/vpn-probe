package probe

import (
	"bytes"
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
	"time"

	"github.com/google/uuid"
)

// SandboxInstance 运行中的临时 sing-box 代理沙箱
type SandboxInstance struct {
	ID         string
	Port       int
	ConfigFile string
	Cmd        *exec.Cmd
	Stderr     *bytes.Buffer
	Cancel     context.CancelFunc
	mu         sync.Mutex
	closed     bool
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
		portStart = 30000
		portEnd = 40000
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

	// 构建最小化 sing-box 配置
	tagStr, _ := outbound["tag"].(string)
	config := map[string]any{
		"log": map[string]any{
			"level":    "debug",
			"disabled": false,
		},
		"dns": map[string]any{
			"servers": []any{
				map[string]any{
					"tag":    "remote-dns",
					"type":   "udp",
					"server": "8.8.8.8",
					"detour": tagStr,
				},
			},
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

	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, m.singboxBin, "run", "-c", confPath)
	cmd.Stdout = nil
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

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
		Stderr:     &stderrBuf,
		Cancel:     cancel,
	}

	// 轮询等待入站端口处于监听状态（最多等待 3 秒）
	if err := waitForPort(port, 3*time.Second); err != nil {
		stderrMsg := strings.TrimSpace(stderrBuf.String())
		inst.Close(m)
		if stderrMsg != "" {
			return nil, fmt.Errorf("wait for proxy port %d ready [sing-box: %s]: %w", port, stderrMsg, err)
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

func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", addr)
}
