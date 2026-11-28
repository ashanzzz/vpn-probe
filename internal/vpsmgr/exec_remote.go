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

type hubInstanceExecResponse struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
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

// GetJSON 发起带鉴权的 GET；401/403 自动重登一次。
func (c *HubClient) GetJSON(ctx context.Context, path string) ([]byte, error) {
	return c.doJSONRequest(ctx, http.MethodGet, path, nil)
}

// PostJSON 发起带鉴权的 POST；401/403 自动重登一次。
func (c *HubClient) PostJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	return c.doJSONRequest(ctx, http.MethodPost, path, payload)
}

func (c *HubClient) doJSONRequest(ctx context.Context, method, path string, payload any) ([]byte, error) {
	if c.base == "" {
		return nil, errors.New("vps-hub url is empty")
	}
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	do := func(tok string) (*http.Response, error) {
		var bodyReader *bytes.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		req, reqErr := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
		if reqErr != nil {
			return nil, reqErr
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
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

// Exec 在懒猫云指定实例上执行命令（WS-PTY 通道）。
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

// ExecInstance 通过 :3338 统一实例执行通道执行命令（普通主机走 SSH，懒猫云走 WS-PTY，vpn-probe 无需持有密码）。
func (c *HubClient) ExecInstance(ctx context.Context, instanceID, cmd string, timeout time.Duration) (string, error) {
	if instanceID == "" {
		return "", errors.New("instance id is empty")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	raw, err := c.PostJSON(ctx, "/api/instances/"+instanceID+"/exec", map[string]any{
		"command":     cmd,
		"timeout_sec": int(timeout / time.Second),
	})
	if err != nil {
		return "", err
	}
	return ParseHubInstanceExecResponse(raw)
}

// ExecByInstance 按实例类型选择合适的 :3338 执行通道。
func (c *HubClient) ExecByInstance(ctx context.Context, inst *VPSInstance, cmd string, timeout time.Duration) (string, error) {
	if inst == nil {
		return "", errors.New("instance is nil")
	}
	if inst.ProviderID == "lazycat" {
		return c.Exec(ctx, inst.ID, cmd, timeout)
	}
	return c.ExecInstance(ctx, inst.ID, cmd, timeout)
}

// AddLazyCatPort 通过 :3338 为懒猫云 NAT 实例新增端口映射并返回公网端口。
func (c *HubClient) AddLazyCatPort(ctx context.Context, instanceID string, privatePort int, protocol, remark string) (int, error) {
	if protocol == "" {
		protocol = "tcp"
	}
	raw, err := c.PostJSON(ctx, "/api/lazycat/ports", map[string]any{
		"instance_id":  instanceID,
		"private_port": privatePort,
		"protocol":     protocol,
		"remark":       remark,
	})
	if err != nil {
		return 0, err
	}
	var res struct {
		PublicPort int `json:"public_port"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("decode add port response failed: %w", err)
	}
	if res.PublicPort <= 0 {
		return 0, errors.New("vps-hub returned empty public port")
	}
	return res.PublicPort, nil
}

// DeleteLazyCatPort 通过 :3338 删除懒猫云 NAT 端口映射。
func (c *HubClient) DeleteLazyCatPort(ctx context.Context, instanceID string, mappingID int) error {
	_, err := c.doJSONRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/lazycat/ports/%d?instance_id=%s", mappingID, instanceID), nil)
	return err
}

// ParseHubInstanceExecResponse 解析 :3338 /api/instances/{id}/exec 响应。
func ParseHubInstanceExecResponse(body []byte) (string, error) {
	var res hubInstanceExecResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("decode vps-hub instance exec response failed: %w", err)
	}
	combined := res.Stdout
	if strings.TrimSpace(res.Stderr) != "" {
		if combined != "" {
			combined += "\n" + res.Stderr
		} else {
			combined = res.Stderr
		}
	}
	if res.ExitCode != 0 {
		return combined, fmt.Errorf("vps-hub instance exec exit code %d", res.ExitCode)
	}
	return res.Stdout, nil
}

// HubPortMapping :3338 返回的 NAT 端口映射条目。
type HubPortMapping struct {
	ID          int    `json:"id"`
	Protocol    string `json:"protocol"`
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port"`
	Remark      string `json:"remark"`
}

// HubPortsResponse :3338 GET /api/lazycat/ports 响应。
type HubPortsResponse struct {
	PortLimit int              `json:"port_limit"`
	PortCount int              `json:"port_count"`
	Total     int              `json:"total"`
	Ports     []HubPortMapping `json:"ports"`
}

// HubSingBoxHealth :3338 GET /api/lazycat/singbox/health 响应。
type HubSingBoxHealth struct {
	Installed      bool   `json:"Installed"`
	Running        bool   `json:"Running"`
	Version        string `json:"Version"`
	ServiceStatus  string `json:"ServiceStatus"`
	ListeningPorts []int  `json:"ListeningPorts"`
}

// HubSingBoxDeployRequest :3338 POST /api/lazycat/singbox/deploy 请求。
type HubSingBoxDeployRequest struct {
	InstanceID string `json:"instance_id"`
	ListenPort int    `json:"listen_port"`
	SNI        string `json:"sni"`
	Remark     string `json:"remark"`
}

// HubSingBoxDeployResult :3338 POST /api/lazycat/singbox/deploy 响应。
type HubSingBoxDeployResult struct {
	UUID       string           `json:"uuid"`
	PublicKey  string           `json:"public_key"`
	ShortID    string           `json:"short_id"`
	ListenPort int              `json:"listen_port"`
	PublicPort int              `json:"public_port"`
	MappingID  int              `json:"mapping_id"`
	ServerIP   string           `json:"server_ip"`
	VLESSURL   string           `json:"vless_url"`
	Health     HubSingBoxHealth `json:"health"`
	Steps      []string         `json:"steps"`
}

// ListLazyCatPorts 查询懒猫云实例当前 NAT 端口映射与配额。
func (c *HubClient) ListLazyCatPorts(ctx context.Context, instanceID string) (*HubPortsResponse, error) {
	raw, err := c.GetJSON(ctx, "/api/lazycat/ports?instance_id="+instanceID)
	if err != nil {
		return nil, err
	}
	var res HubPortsResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode ports response failed: %w", err)
	}
	return &res, nil
}

// GetLazyCatSingBoxHealth 查询懒猫云实例原生 sing-box 运行状态。
func (c *HubClient) GetLazyCatSingBoxHealth(ctx context.Context, instanceID string) (*HubSingBoxHealth, error) {
	raw, err := c.GetJSON(ctx, "/api/lazycat/singbox/health?instance_id="+instanceID)
	if err != nil {
		return nil, err
	}
	var res HubSingBoxHealth
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode singbox health failed: %w", err)
	}
	return &res, nil
}

// DeployLazyCatSingBox 经 :3338 在懒猫云实例上部署原生 sing-box 并建立 NAT 映射。
func (c *HubClient) DeployLazyCatSingBox(ctx context.Context, req HubSingBoxDeployRequest) (*HubSingBoxDeployResult, error) {
	raw, err := c.PostJSON(ctx, "/api/lazycat/singbox/deploy", req)
	if err != nil {
		return nil, err
	}
	var res HubSingBoxDeployResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode singbox deploy response failed: %w", err)
	}
	if res.VLESSURL == "" && res.PublicPort <= 0 {
		return nil, errors.New("vps-hub singbox deploy returned empty vless_url and public_port")
	}
	return &res, nil
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
	client := d.hubClient
	fn := d.hubExec
	d.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 非懒猫云普通主机优先走统一的 /api/instances/{id}/exec（无需保存密码）；
	// 懒猫云走注入的 hubExec（便于单测注入）或 client.Exec。
	if inst != nil && inst.ProviderID != "lazycat" && client != nil {
		return client.ExecInstance(ctx, inst.ID, cmd, 90*time.Second)
	}
	if fn != nil {
		return fn(ctx, inst.ID, cmd, 90*time.Second)
	}
	if client != nil {
		return client.ExecByInstance(ctx, inst, cmd, 90*time.Second)
	}
	return "", errors.New("vps-hub executor not configured")
}

// AddLazyCatPortViaHub 经 :3338 为懒猫云 NAT 实例新增端口映射。
func (d *Detector) AddLazyCatPortViaHub(ctx context.Context, instanceID string, privatePort int, protocol, remark string) (int, error) {
	d.mu.RLock()
	client := d.hubClient
	d.mu.RUnlock()
	if client == nil {
		return 0, errors.New("vps-hub client not configured")
	}
	return client.AddLazyCatPort(ctx, instanceID, privatePort, protocol, remark)
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

// ListLazyCatPortsViaHub 经 :3338 查询懒猫云 NAT 端口映射清单。
func (d *Detector) ListLazyCatPortsViaHub(ctx context.Context, instanceID string) (*HubPortsResponse, error) {
	d.mu.RLock()
	client := d.hubClient
	d.mu.RUnlock()
	if client == nil {
		return nil, errors.New("vps-hub client not configured")
	}
	return client.ListLazyCatPorts(ctx, instanceID)
}

// GetLazyCatSingBoxHealthViaHub 经 :3338 查询懒猫云原生 sing-box 健康状态。
func (d *Detector) GetLazyCatSingBoxHealthViaHub(ctx context.Context, instanceID string) (*HubSingBoxHealth, error) {
	d.mu.RLock()
	client := d.hubClient
	d.mu.RUnlock()
	if client == nil {
		return nil, errors.New("vps-hub client not configured")
	}
	return client.GetLazyCatSingBoxHealth(ctx, instanceID)
}

// DeployLazyCatSingBoxViaHub 经 :3338 在懒猫云 NAT 机上部署原生 sing-box 并映射公网端口。
func (d *Detector) DeployLazyCatSingBoxViaHub(ctx context.Context, req HubSingBoxDeployRequest) (*HubSingBoxDeployResult, error) {
	d.mu.RLock()
	client := d.hubClient
	d.mu.RUnlock()
	if client == nil {
		return nil, errors.New("vps-hub client not configured")
	}
	return client.DeployLazyCatSingBox(ctx, req)
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
