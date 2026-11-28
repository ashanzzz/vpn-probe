package vpsmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 通过 :3338（personal-vps-monitor）控制面执行远程操作。
//
// 适用场景：懒猫云 NAT 实例未映射公网 22 端口（ssh_port=0），无法直接 ssh.Dial。

// HubExecutor 实例命令执行器抽象（便于单测注入）。
type HubExecutor func(ctx context.Context, instanceID, cmd string, timeout time.Duration) (string, error)

type hubLoginResponse struct {
	Token string `json:"token"`
}

type hubExecResponse struct {
	Status   string `json:"status"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error"`
}

// HubClient 带 token 缓存的 :3338 控制面客户端。
type HubClient struct {
	base     string
	username string
	password string
	http     *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewHubClient 构造客户端。baseURL 形如 http://192.168.8.11:3338。
func NewHubClient(baseURL, username, password string) *HubClient {
	return &HubClient{
		base:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		username: username,
		password: password,
		http:     &http.Client{Timeout: 150 * time.Second},
	}
}

func (c *HubClient) login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}
	payload, _ := json.Marshal(map[string]string{"username": c.username, "password": c.password})
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/auth/login", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("vps-hub login failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vps-hub login http %d", resp.StatusCode)
	}
	var lr hubLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil || lr.Token == "" {
		return "", errors.New("vps-hub login returned empty token")
	}
	c.token = lr.Token
	c.tokenExp = time.Now().Add(30 * time.Minute)
	return c.token, nil
}

func (c *HubClient) invalidate() {
	c.mu.Lock()
	c.token = ""
	c.tokenExp = time.Time{}
	c.mu.Unlock()
}

// PostJSON 发起带鉴权的 POST；401/403 自动重登一次。
func (c *HubClient) PostJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	if c.base == "" {
		return nil, errors.New("vps-hub url is empty")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	do := func(tok string) (*http.Response, error) {
		req, reqErr := http.NewRequestWithContext(ctx, "POST", c.base+path, bytes.NewReader(body))
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		return c.http.Do(req)
	}

	tok, err := c.login(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := do(tok)
	if err != nil {
		return nil, fmt.Errorf("vps-hub %s request failed: %w", path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		c.invalidate()
		if tok, err = c.login(ctx); err != nil {
			return nil, err
		}
		if resp, err = do(tok); err != nil {
			return nil, fmt.Errorf("vps-hub %s retry failed: %w", path, err)
		}
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("read vps-hub response failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vps-hub %s http %d: %s", path, resp.StatusCode, truncateStr(buf.String(), 200))
	}
	return buf.Bytes(), nil
}

// Exec 在指定实例上执行命令。
func (c *HubClient) Exec(ctx context.Context, instanceID, cmd string, timeout time.Duration) (string, error) {
	if instanceID == "" {
		return "", errors.New("instance id is empty")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	raw, err := c.PostJSON(ctx, "/api/lazycat/terminal/exec", map[string]any{
		"instance_id": instanceID,
		"command":     cmd,
		"timeout_sec": int(timeout / time.Second),
	})
	if err != nil {
		return "", err
	}
	return ParseHubExecResponse(raw)
}

// RepublishNATPort 重发布 NAT 公网端口（私网端口不变，宿机进程不动）。
func (c *HubClient) RepublishNATPort(ctx context.Context, instanceID string, privatePort, oldPublicPort int, remark string) (newPublicPort, newMappingID int, err error) {
	raw, err := c.PostJSON(ctx, "/api/lazycat/ports/republish", map[string]any{
		"instance_id":     instanceID,
		"private_port":    privatePort,
		"old_public_port": oldPublicPort,
		"remark":          remark,
	})
	if err != nil {
		return 0, 0, err
	}
	var res struct {
		NewPublicPort int `json:"new_public_port"`
		NewMappingID  int `json:"new_mapping_id"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, 0, fmt.Errorf("decode republish response failed: %w", err)
	}
	if res.NewPublicPort <= 0 {
		return 0, 0, errors.New("vps-hub returned empty public port")
	}
	return res.NewPublicPort, res.NewMappingID, nil
}

// ParseHubExecResponse 解析 :3338 exec 响应；非零退出码保留 output 供诊断。
func ParseHubExecResponse(body []byte) (string, error) {
	var res hubExecResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("decode vps-hub response failed: %w", err)
	}
	if res.Error != "" {
		return res.Output, fmt.Errorf("vps-hub exec error: %s", res.Error)
	}
	if res.ExitCode != 0 {
		return res.Output, fmt.Errorf("vps-hub exec exit code %d", res.ExitCode)
	}
	return res.Output, nil
}

// NewHubExecutor 构造执行器闭包。
func NewHubExecutor(baseURL, username, password string) HubExecutor {
	client := NewHubClient(baseURL, username, password)
	return func(ctx context.Context, instanceID, cmd string, timeout time.Duration) (string, error) {
		return client.Exec(ctx, instanceID, cmd, timeout)
	}
}

// SetHubExecutor 注入 hub 执行器（由 Manager 在初始化时调用）。
func (d *Detector) SetHubExecutor(fn HubExecutor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hubExec = fn
}

// SetHubClient 注入 hub 客户端（用于端口重发布等非命令操作）。
func (d *Detector) SetHubClient(c *HubClient) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hubClient = c
}

// RunViaHub 经 :3338 执行命令。
func (d *Detector) RunViaHub(inst *VPSInstance, cmd string) (string, error) {
	d.mu.RLock()
	fn := d.hubExec
	d.mu.RUnlock()
	if fn == nil {
		return "", errors.New("vps-hub executor not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return fn(ctx, inst.ID, cmd, 90*time.Second)
}

// RepublishNATPortViaHub 经 :3338 重发布 NAT 公网端口。
func (d *Detector) RepublishNATPortViaHub(ctx context.Context, instanceID string, privatePort, oldPublicPort int, remark string) (int, int, error) {
	d.mu.RLock()
	client := d.hubClient
	d.mu.RUnlock()
	if client == nil {
		return 0, 0, errors.New("vps-hub client not configured")
	}
	return client.RepublishNATPort(ctx, instanceID, privatePort, oldPublicPort, remark)
}

// NeedsHub 判断该实例是否必须走 hub 通道。
func NeedsHub(inst *VPSInstance) bool {
	if inst == nil {
		return false
	}
	if inst.ExecMode == "hub" {
		return true
	}
	return inst.ProviderID == "lazycat" && inst.SSHPort <= 0
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
