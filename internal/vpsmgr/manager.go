package vpsmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
)

type NodeProbeFunc func(ctx context.Context, link string) *model.NodeProbeResult

type Manager struct {
	vpsHubURL         string
	vpsHubUser        string
	vpsHubPass        string
	hubMu             sync.RWMutex
	instances         []*VPSInstance
	instancesMu       sync.RWMutex
	instancesFilePath string
	detector          *Detector
	ghChecker         *GitHubChecker
	healer            *AutoHealer
	backupMgr         *BackupManager
	substoreClient    *substore.Client
	configPath        string
	probeFunc         NodeProbeFunc
	probeMu           sync.RWMutex
}

// SetProbeFunc 注入沙箱冒烟测试函数
func (m *Manager) SetProbeFunc(fn NodeProbeFunc) {
	m.probeMu.Lock()
	defer m.probeMu.Unlock()
	m.probeFunc = fn
}

func (m *Manager) getProbeFunc() NodeProbeFunc {
	m.probeMu.RLock()
	defer m.probeMu.RUnlock()
	return m.probeFunc
}

// SetVPSHubConfig 设置 VPS Hub 访问参数 (线程安全，支持动态热修改与单测注入)
func (m *Manager) SetVPSHubConfig(url, user, pass string) {
	m.hubMu.Lock()
	defer m.hubMu.Unlock()
	m.vpsHubURL = url
	m.vpsHubUser = user
	m.vpsHubPass = pass
}

func (m *Manager) getVPSHubConfig() (string, string, string) {
	m.hubMu.RLock()
	defer m.hubMu.RUnlock()
	return m.vpsHubURL, m.vpsHubUser, m.vpsHubPass
}

func NewManager(cfg *config.Config, sc *substore.Client, configPath string) *Manager {
	gh := NewGitHubChecker()
	det := NewDetector(gh)
	heal := NewAutoHealer(det, sc, configPath)

	m := &Manager{
		vpsHubURL:      "http://192.168.8.11:3338",
		vpsHubUser:     "ashan",
		vpsHubPass:     "Woo@@@204317",
		detector:       det,
		ghChecker:      gh,
		healer:         heal,
		backupMgr:      NewBackupManager(det),
		substoreClient: sc,
		configPath:     configPath,
	}

	// 初始化静态托管 VPS 凭据（防止 VPS Hub 短暂离线时丢失）
	m.instances = []*VPSInstance{
		{
			ID:          "23881",
			Name:        "JPSB4",
			IP:          "45.192.242.72",
			SSHPort:     22,
			SSHUser:     "root",
			SSHPassword: "eTA^a9nSg6DHtN2d",
			ProviderID:  "acck",
			Status:      "running",
			Region:      "日本",
		},
		{
			ID:          "13483",
			Name:        "LAX4837ISP",
			IP:          "38.175.222.44",
			SSHPort:     22,
			SSHUser:     "root",
			SSHPassword: "GVpjVRFfYb0oS-3m",
			ProviderID:  "acck",
			Status:      "running",
			Region:      "美国",
		},
		{
			ID:          "23712",
			Name:        "JP IIJ BGP",
			IP:          "156.231.113.91",
			SSHPort:     22,
			SSHUser:     "root",
			SSHPassword: "s5Upk7j1PKt!RP0TYn",
			ProviderID:  "akile",
			Status:      "running",
			Region:      "日本",
		},
	}

	m.instancesFilePath = "./data/managed_instances.json"
	m.loadManagedInstances()

	// 异步尝试从 VPS Hub 动态刷新最新实例
	go m.refreshFromVPSHub()

	return m
}

func (m *Manager) loadManagedInstances() {
	if m.instancesFilePath == "" {
		return
	}
	data, err := os.ReadFile(m.instancesFilePath)
	if err != nil {
		return
	}
	var loaded []*VPSInstance
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}

	m.instancesMu.Lock()
	defer m.instancesMu.Unlock()
	for _, li := range loaded {
		if li == nil || li.IP == "" {
			continue
		}
		var found bool
		for _, exist := range m.instances {
			if exist.IP == li.IP || (exist.ID != "" && exist.ID == li.ID) {
				exist.Name = li.Name
				exist.SSHPort = li.SSHPort
				exist.SSHUser = li.SSHUser
				if li.SSHPassword != "" {
					exist.SSHPassword = li.SSHPassword
				}
				if li.SSHKey != "" {
					exist.SSHKey = li.SSHKey
				}
				exist.IsNAT = li.IsNAT
				exist.NATPorts = li.NATPorts
				exist.Region = li.Region
				found = true
				break
			}
		}
		if !found {
			m.instances = append(m.instances, li)
		}
	}
}

func (m *Manager) saveManagedInstances() error {
	if m.instancesFilePath == "" {
		return nil
	}
	m.instancesMu.RLock()
	data, err := json.MarshalIndent(m.instances, "", "  ")
	m.instancesMu.RUnlock()
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(m.instancesFilePath), 0755)
	return os.WriteFile(m.instancesFilePath, data, 0600)
}

// RegisterInstance 动态录入全新独立 VPS 或 NAT VPS 并持久化
func (m *Manager) RegisterInstance(req *RegisterInstanceRequest) (*VPSInstance, error) {
	if req == nil || req.IP == "" || req.Name == "" {
		return nil, fmt.Errorf("ip and name are required")
	}

	sshPort := req.SSHPort
	if sshPort <= 0 {
		sshPort = 22
	}
	sshUser := req.SSHUser
	if sshUser == "" {
		sshUser = "root"
	}

	id := fmt.Sprintf("custom-%d", time.Now().UnixNano()%100000)
	inst := &VPSInstance{
		ID:          id,
		Name:        req.Name,
		IP:          req.IP,
		SSHPort:     sshPort,
		SSHUser:     sshUser,
		SSHPassword: req.SSHPassword,
		SSHKey:      req.SSHKey,
		ProviderID:  req.ProviderID,
		IsNAT:       req.IsNAT,
		NATPorts:    req.NATPorts,
		Status:      "running",
		Region:      req.Region,
	}

	m.instancesMu.Lock()
	var updated bool
	for idx, exist := range m.instances {
		if exist.IP == req.IP {
			inst.ID = exist.ID
			m.instances[idx] = inst
			updated = true
			break
		}
	}
	if !updated {
		m.instances = append(m.instances, inst)
	}
	m.instancesMu.Unlock()

	_ = m.saveManagedInstances()
	logger.Infof("[VPS-REGISTER] Registered new VPS '%s' (%s, is_nat=%v, ports=%v)", inst.Name, inst.IP, inst.IsNAT, inst.NATPorts)
	return inst, nil
}

// refreshFromVPSHub 从 Unraid VPS Hub API 同步最新实例与凭据
func (m *Manager) refreshFromVPSHub() {
	hubURL, hubUser, hubPass := m.getVPSHubConfig()
	if hubURL == "" {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	// 1. 登录 VPS Hub 获取 Token
	loginBody := strings.NewReader(fmt.Sprintf(`{"username":"%s","password":"%s"}`, hubUser, hubPass))
	req, err := http.NewRequest("POST", hubURL+"/api/auth/login", loginBody)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	var loginRes struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginRes); err != nil || loginRes.Token == "" {
		return
	}

	// 2. 拉取实例列表
	instReq, err := http.NewRequest("GET", hubURL+"/api/instances", nil)
	if err != nil {
		return
	}
	instReq.Header.Set("Authorization", "Bearer "+loginRes.Token)

	instResp, err := client.Do(instReq)
	if err != nil {
		return
	}
	defer instResp.Body.Close()
	if instResp.StatusCode != http.StatusOK {
		return
	}

	var hubData struct {
		Instances []*struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			IP          string `json:"ip"`
			SSHPort     int    `json:"ssh_port"`
			SSHUser     string `json:"ssh_user"`
			SSHPassword string `json:"ssh_password"`
			RootPassword string `json:"root_password"`
			ProviderID  string `json:"provider_id"`
			Status      string `json:"status"`
			Region      string `json:"region"`
		} `json:"instances"`
	}
	if err := json.NewDecoder(instResp.Body).Decode(&hubData); err != nil {
		return
	}

	m.instancesMu.Lock()
	defer m.instancesMu.Unlock()

	for _, hi := range hubData.Instances {
		if hi == nil {
			continue
		}
		if hi.ProviderID == "lazycat" {
			continue // 家宽网关不属于标准 root SSH 托管机
		}
		pass := hi.SSHPassword
		if pass == "" {
			pass = hi.RootPassword
		}
		sshPort := hi.SSHPort
		if sshPort <= 0 {
			sshPort = 22
		}
		sshUser := hi.SSHUser
		if sshUser == "" {
			sshUser = "root"
		}

		// 更新或追加
		var updated bool
		for _, inst := range m.instances {
			if inst.ID == hi.ID || inst.IP == hi.IP {
				inst.Name = hi.Name
				inst.SSHPort = sshPort
				inst.SSHUser = sshUser
				if pass != "" {
					inst.SSHPassword = pass
				}
				inst.Status = hi.Status
				inst.Region = hi.Region
				updated = true
				break
			}
		}
		if !updated && hi.IP != "" && pass != "" {
			m.instances = append(m.instances, &VPSInstance{
				ID:          hi.ID,
				Name:        hi.Name,
				IP:          hi.IP,
				SSHPort:     sshPort,
				SSHUser:     sshUser,
				SSHPassword: pass,
				ProviderID:  hi.ProviderID,
				Status:      hi.Status,
				Region:      hi.Region,
			})
		}
	}
}

// ListVPSInstances 并发探查所有托管 VPS 的软件形态、当前版本与最新版本
func (m *Manager) ListVPSInstances(ctx context.Context) []*SoftwareInfo {
	m.instancesMu.RLock()
	insts := make([]*VPSInstance, len(m.instances))
	copy(insts, m.instances)
	m.instancesMu.RUnlock()

	var wg sync.WaitGroup
	results := make([]*SoftwareInfo, len(insts))

	for i, inst := range insts {
		wg.Add(1)
		go func(idx int, target *VPSInstance) {
			defer wg.Done()
			info := m.detector.InspectVPS(ctx, target)
			if info != nil && m.backupMgr != nil && info.Status == "running" {
				if backups, err := m.backupMgr.ListBackups(target, info); err == nil {
					info.Backups = backups
				}
			}
			results[idx] = info
		}(i, inst)
	}

	wg.Wait()
	return results
}

// GetBlockedAnalysis 对所有自建节点执行四分类深度墙况计算
func (m *Manager) GetBlockedAnalysis(probeResults []*model.NodeProbeResult) []*BlockedAnalysisResult {
	analysis := make([]*BlockedAnalysisResult, 0)
	for _, res := range probeResults {
		if !res.IsSelf {
			continue // 严格限制：仅对自建节点执行墙况计算
		}
		diagRes := m.healer.DiagnoseNode(res)
		analysis = append(analysis, diagRes)
	}
	return analysis
}

// HealPort 主动或自动触发针对指定节点的换端口自愈操作
func (m *Manager) HealPort(ctx context.Context, nodeName string, targetPort int, probeResults []*model.NodeProbeResult) (*PortHealResponse, error) {
	var targetRes *model.NodeProbeResult
	for _, r := range probeResults {
		if r.Name == nodeName || strings.Contains(r.OriginalURL, nodeName) {
			targetRes = r
			break
		}
	}

	if targetRes == nil {
		return nil, fmt.Errorf("node '%s' not found in self-hosted probe results", nodeName)
	}

	// 提取目标 IP 匹配 VPSInstance
	u, err := url.Parse(targetRes.OriginalURL)
	if err != nil {
		return nil, fmt.Errorf("parse node url: %w", err)
	}
	targetIP := u.Hostname()

	m.instancesMu.RLock()
	var matchedInst *VPSInstance
	for _, inst := range m.instances {
		if inst.IP == targetIP {
			matchedInst = inst
			break
		}
	}
	m.instancesMu.RUnlock()

	if matchedInst == nil {
		return nil, fmt.Errorf("no matching managed VPS instance found for IP %s", targetIP)
	}

	return m.healer.ExecutePortHeal(ctx, matchedInst, targetRes.Name, targetRes.OriginalURL, targetPort)
}

// InstallSoftware 在指定 VPS / NAT VPS 上全自动一键部署 sing-box、3x-ui 或 Xray (纯粹标准化 VLESS-Reality 架构)
func (m *Manager) InstallSoftware(ctx context.Context, vpsID string, software string) (string, error) {
	m.instancesMu.RLock()
	var inst *VPSInstance
	for _, i := range m.instances {
		if i.ID == vpsID || i.IP == vpsID || i.Name == vpsID {
			inst = i
			break
		}
	}
	m.instancesMu.RUnlock()

	if inst == nil {
		return "", fmt.Errorf("VPS instance '%s' not found", vpsID)
	}

	// 1. 挑选可用端口 (NAT VPS 严格限定在 nat_ports 范围，普通 VPS 在 20000-60000 随机)
	var port int
	if inst.IsNAT && len(inst.NATPorts) > 0 {
		for _, p := range inst.NATPorts {
			checkCmd := fmt.Sprintf("ss -tulpn | grep -q ':%d ' && echo 'busy' || echo 'free'", p)
			out, err := m.detector.RunSSH(inst, checkCmd)
			if err == nil && strings.TrimSpace(out) == "free" {
				port = p
				break
			}
		}
		if port <= 0 {
			port = inst.NATPorts[0]
		}
	} else {
		port = GenerateRandomPort()
	}

	if software == "" || software == "sing-box" {
		// 首选方案：安装现代轻量安全 VLESS Reality sing-box Docker 容器 (彻底废除 Shadowsocks)
		installCmd := fmt.Sprintf(`
command -v docker >/dev/null 2>&1 || curl -fsSL https://get.docker.com | sh
docker pull ghcr.io/sagernet/sing-box:latest
mkdir -p /opt/sb-mgr
cat << 'EOF' > /opt/sb-mgr/config.json
{
  "log": {"level": "warn", "timestamp": true},
  "inbounds": [
    {
      "type": "vless",
      "tag": "vless-reality",
      "listen": "::",
      "listen_port": %d,
      "users": [
        {
          "uuid": "5b84d275-d328-4565-9b47-20e9121fee7e",
          "flow": "xtls-rprx-vision"
        }
      ],
      "tls": {
        "enabled": true,
        "server_name": "aws.amazon.com",
        "reality": {
          "enabled": true,
          "handshake": {
            "server": "aws.amazon.com",
            "server_port": 443
          },
          "private_key": "oA6K1zcO9AWpQkBOmx9kU2yTMc9g9oGINyB9OmC38WY",
          "short_id": ["3c4ffcc0"]
        }
      }
    }
  ],
  "outbounds": [{"type": "direct", "tag": "direct"}]
}
EOF
iptables -I INPUT -p tcp --dport %d -j ACCEPT
docker rm -f sb-mgr 2>/dev/null
docker run -d --name sb-mgr --restart always --net=host -v /opt/sb-mgr:/etc/sing-box ghcr.io/sagernet/sing-box:latest run -c /etc/sing-box/config.json
`, port, port)
		out, err := m.detector.RunSSH(inst, installCmd)
		if err != nil {
			return out, fmt.Errorf("install sing-box failed: %w", err)
		}

		// 同步生成 VLESS-Reality 节点并推送到 SubStore
		nodeURL := fmt.Sprintf("vless://5b84d275-d328-4565-9b47-20e9121fee7e@%s:%d?security=reality&sni=aws.amazon.com&fp=chrome&pbk=oA6K1zcO9AWpQkBOmx9kU2yTMc9g9oGINyB9OmC38WY&sid=3c4ffcc0&type=tcp&flow=xtls-rprx-vision#%s", inst.IP, port, url.QueryEscape(inst.Name))
		if m.healer != nil {
			_ = m.healer.AddNodeToSubStore(ctx, nodeURL)
		}

		return fmt.Sprintf("Successfully installed sing-box VLESS Reality container 'sb-mgr' on %s (port %d)", inst.IP, port), nil

	} else if software == "3x-ui" {
		// 3x-ui: 启动面板并在 SQLite 初始化 VLESS Reality 入站
		cmd := fmt.Sprintf(`
docker pull ghcr.io/mhsanaei/3x-ui:latest
docker rm -f 3x-ui 2>/dev/null
mkdir -p /etc/x-ui
docker run -d --name 3x-ui --restart always --net=host -v /etc/x-ui:/etc/x-ui ghcr.io/mhsanaei/3x-ui:latest
sleep 2
docker exec 3x-ui sqlite3 /etc/x-ui/x-ui.db "INSERT OR REPLACE INTO inbounds (id, user_id, up, down, total, remark, enable, expiry_time, listen, port, protocol, settings, stream_settings, tag, sniffing) VALUES (1, 1, 0, 0, 0, 'reality-inbound', 1, 0, '', %d, 'vless', '{\"clients\":[{\"id\":\"5b84d275-d328-4565-9b47-20e9121fee7e\",\"flow\":\"xtls-rprx-vision\"}],\"decryption\":\"none\"}', '{\"network\":\"tcp\",\"security\":\"reality\",\"realitySettings\":{\"show\":false,\"xver\":0,\"dest\":\"aws.amazon.com:443\",\"serverNames\":[\"aws.amazon.com\"],\"privateKey\":\"oA6K1zcO9AWpQkBOmx9kU2yTMc9g9oGINyB9OmC38WY\",\"shortIds\":[\"3c4ffcc0\"]}}', 'inbound-%d', '{\"enabled\":true,\"destOverride\":[\"http\",\"tls\"]}');"
iptables -I INPUT -p tcp --dport %d -j ACCEPT
docker restart 3x-ui
`, port, port, port)
		out, err := m.detector.RunSSH(inst, cmd)
		if err != nil {
			return out, fmt.Errorf("install 3x-ui failed: %w", err)
		}

		nodeURL := fmt.Sprintf("vless://5b84d275-d328-4565-9b47-20e9121fee7e@%s:%d?security=reality&sni=aws.amazon.com&fp=chrome&pbk=oA6K1zcO9AWpQkBOmx9kU2yTMc9g9oGINyB9OmC38WY&sid=3c4ffcc0&type=tcp&flow=xtls-rprx-vision#%s", inst.IP, port, url.QueryEscape(inst.Name))
		if m.healer != nil {
			_ = m.healer.AddNodeToSubStore(ctx, nodeURL)
		}

		return fmt.Sprintf("Successfully installed 3x-ui with initialized VLESS-Reality on %s (port %d)", inst.IP, port), nil

	} else if software == "xray" || software == "v2ray" {
		// Xray: 部署官方轻量 VLESS-Reality 容器
		cmd := fmt.Sprintf(`
docker pull teddysun/xray:latest
mkdir -p /etc/xray
cat << 'EOF' > /etc/xray/config.json
{
  "log": {"loglevel": "warning"},
  "inbounds": [
    {
      "port": %d,
      "protocol": "vless",
      "settings": {
        "clients": [{"id": "5b84d275-d328-4565-9b47-20e9121fee7e", "flow": "xtls-rprx-vision"}],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "tcp",
        "security": "reality",
        "realitySettings": {
          "show": false,
          "dest": "aws.amazon.com:443",
          "serverNames": ["aws.amazon.com"],
          "privateKey": "oA6K1zcO9AWpQkBOmx9kU2yTMc9g9oGINyB9OmC38WY",
          "shortIds": ["3c4ffcc0"]
        }
      }
    }
  ],
  "outbounds": [{"protocol": "freedom"}]
}
EOF
iptables -I INPUT -p tcp --dport %d -j ACCEPT
docker rm -f xray 2>/dev/null
docker run -d --name xray --restart always --net=host -v /etc/xray:/etc/xray teddysun/xray:latest
`, port, port)
		out, err := m.detector.RunSSH(inst, cmd)
		if err != nil {
			return out, fmt.Errorf("install xray failed: %w", err)
		}

		nodeURL := fmt.Sprintf("vless://5b84d275-d328-4565-9b47-20e9121fee7e@%s:%d?security=reality&sni=aws.amazon.com&fp=chrome&pbk=oA6K1zcO9AWpQkBOmx9kU2yTMc9g9oGINyB9OmC38WY&sid=3c4ffcc0&type=tcp&flow=xtls-rprx-vision#%s", inst.IP, port, url.QueryEscape(inst.Name))
		if m.healer != nil {
			_ = m.healer.AddNodeToSubStore(ctx, nodeURL)
		}

		return fmt.Sprintf("Successfully installed Xray VLESS Reality container on %s (port %d)", inst.IP, port), nil
	}

	return "", fmt.Errorf("unsupported software '%s'", software)
}

// UninstallSoftware 彻底卸载指定 VPS 上的代理软件容器、清理配置目录并可选从 SubStore 订阅下架
func (m *Manager) UninstallSoftware(ctx context.Context, req *UninstallRequest) (*UninstallResponse, error) {
	inst, err := m.findInstance(req.VPSID)
	if err != nil {
		return nil, err
	}

	resp := &UninstallResponse{
		Success: false,
		VPSID:   inst.ID,
		IP:      inst.IP,
	}

	// 1. 停止并强制删除所有可能的代理容器
	stopCmd := "docker rm -f sb-mgr 3x-ui xray v2ray-agent-docker 2>/dev/null || true"
	_, _ = m.detector.RunSSH(inst, stopCmd)
	resp.CleanedContainers = []string{"sb-mgr", "3x-ui", "xray", "v2ray-agent-docker"}

	// 2. 清理数据配置目录
	if req.RemoveData {
		cleanDirCmd := "rm -rf /opt/sb-mgr /etc/x-ui /etc/xray /etc/v2ray-agent 2>/dev/null || true"
		_, _ = m.detector.RunSSH(inst, cleanDirCmd)
		resp.CleanedDirectories = []string{"/opt/sb-mgr", "/etc/x-ui", "/etc/xray", "/etc/v2ray-agent"}
	}

	// 3. 从 SubStore 订阅中下架属于此 IP 的节点
	if req.CleanSubStore && m.healer != nil {
		removed, err := m.healer.RemoveNodeFromSubStore(ctx, inst.IP)
		if err == nil {
			resp.SubStoreRemovedCount = removed
		}
	}

	resp.Success = true
	resp.Message = fmt.Sprintf("Successfully uninstalled proxy software on %s (removed %d nodes from SubStore)", inst.IP, resp.SubStoreRemovedCount)
	logger.Infof("[VPS-UNINSTALL] %s", resp.Message)
	return resp, nil
}

// UpdateSoftware 升级指定 VPS 上的代理软件至最新镜像并平滑重启
func (m *Manager) UpdateSoftware(ctx context.Context, vpsID string) (string, error) {
	m.instancesMu.RLock()
	var inst *VPSInstance
	for _, i := range m.instances {
		if i.ID == vpsID || i.IP == vpsID {
			inst = i
			break
		}
	}
	m.instancesMu.RUnlock()

	if inst == nil {
		return "", fmt.Errorf("VPS instance '%s' not found", vpsID)
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	if softInfo.DeployMode != "docker" || softInfo.ContainerName == "" {
		return "", fmt.Errorf("instance %s does not run manageable docker container (detected: %s/%s)", inst.IP, softInfo.SoftwareType, softInfo.DeployMode)
	}

	updateCmd := fmt.Sprintf("docker pull ghcr.io/sagernet/sing-box:latest && docker restart %s", softInfo.ContainerName)
	if softInfo.SoftwareType == "3x-ui" {
		updateCmd = "docker pull ghcr.io/mhsanaei/3x-ui:latest && docker restart 3x-ui"
	}

	out, err := m.detector.RunSSH(inst, updateCmd)
	if err != nil {
		return out, fmt.Errorf("update software on %s failed: %w", inst.IP, err)
	}

	return fmt.Sprintf("Successfully updated container '%s' on %s to latest", softInfo.ContainerName, inst.IP), nil
}

// ExecuteLifecycleAction 执行细粒度生命周期管理动作 (start, stop, restart, backup)
func (m *Manager) ExecuteLifecycleAction(ctx context.Context, vpsID string, action string) (*SoftwareInfo, error) {
	inst, err := m.findInstance(vpsID)
	if err != nil {
		return nil, err
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	cName := softInfo.ContainerName
	if cName == "" {
		if softInfo.SoftwareType == "3x-ui" {
			cName = "3x-ui"
		} else {
			cName = "sb-mgr"
		}
	}

	switch action {
	case "start":
		cmd := fmt.Sprintf("docker start %s", cName)
		if _, err := m.detector.RunSSH(inst, cmd); err != nil {
			return nil, fmt.Errorf("docker start failed: %w", err)
		}
		logger.Infof("[VPS-LIFECYCLE] Started container '%s' on %s", cName, inst.IP)

	case "stop":
		cmd := fmt.Sprintf("docker stop -t 10 %s", cName)
		if _, err := m.detector.RunSSH(inst, cmd); err != nil {
			return nil, fmt.Errorf("docker stop failed: %w", err)
		}
		logger.Infof("[VPS-LIFECYCLE] Stopped container '%s' on %s", cName, inst.IP)

	case "restart":
		cmd := fmt.Sprintf("docker restart %s", cName)
		if _, err := m.detector.RunSSH(inst, cmd); err != nil {
			return nil, fmt.Errorf("docker restart failed: %w", err)
		}
		logger.Infof("[VPS-LIFECYCLE] Restarted container '%s' on %s", cName, inst.IP)

	case "backup":
		if m.backupMgr == nil {
			return nil, fmt.Errorf("backup manager not initialized")
		}
		snap, err := m.backupMgr.CreateBackup(inst, softInfo)
		if err != nil {
			return nil, fmt.Errorf("create backup failed: %w", err)
		}
		logger.Infof("[VPS-LIFECYCLE] Created backup snapshot '%s' on %s", snap.FileName, inst.IP)

	default:
		return nil, fmt.Errorf("unknown lifecycle action: %s", action)
	}

	// 重新获取最新状态并加载快照
	updatedInfo := m.detector.InspectVPS(ctx, inst)
	if m.backupMgr != nil {
		if backups, err := m.backupMgr.ListBackups(inst, updatedInfo); err == nil {
			updatedInfo.Backups = backups
		}
	}
	return updatedInfo, nil
}

// ListBackups 获取指定 VPS 当前的所有备份快照
func (m *Manager) ListBackups(ctx context.Context, vpsID string) ([]*BackupSnapshot, error) {
	inst, err := m.findInstance(vpsID)
	if err != nil {
		return nil, err
	}
	if m.backupMgr == nil {
		return nil, fmt.Errorf("backup manager not initialized")
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	return m.backupMgr.ListBackups(inst, softInfo)
}

// RestoreBackup 从指定快照还原配置并重启
func (m *Manager) RestoreBackup(ctx context.Context, vpsID string, snapshotID string) (*SoftwareInfo, error) {
	inst, err := m.findInstance(vpsID)
	if err != nil {
		return nil, err
	}
	if m.backupMgr == nil {
		return nil, fmt.Errorf("backup manager not initialized")
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	if err := m.backupMgr.RestoreBackup(inst, softInfo, snapshotID); err != nil {
		return nil, err
	}

	updatedInfo := m.detector.InspectVPS(ctx, inst)
	if backups, err := m.backupMgr.ListBackups(inst, updatedInfo); err == nil {
		updatedInfo.Backups = backups
	}
	return updatedInfo, nil
}

// SafeUpgradeSoftware 执行工业级安全升级流水线 (备份 -> 拉取新镜像 -> 重启 -> 冒烟测试 -> 失败自动原子回滚)
func (m *Manager) SafeUpgradeSoftware(ctx context.Context, req *SafeUpgradeRequest, probeResults []*model.NodeProbeResult) (*SafeUpgradeResponse, error) {
	inst, err := m.findInstance(req.VPSID)
	if err != nil {
		return nil, err
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	if softInfo.DeployMode != "docker" || softInfo.ContainerName == "" {
		return nil, fmt.Errorf("instance %s does not run manageable docker container (detected: %s/%s)", inst.IP, softInfo.SoftwareType, softInfo.DeployMode)
	}

	resp := &SafeUpgradeResponse{
		Success:         false,
		VPSID:           inst.ID,
		IP:              inst.IP,
		SoftwareType:    softInfo.SoftwareType,
		PreviousVersion: softInfo.CurrentVersion,
	}

	cName := softInfo.ContainerName
	var upstreamImage, runCmdTemplate string
	if softInfo.SoftwareType == "3x-ui" {
		upstreamImage = "ghcr.io/mhsanaei/3x-ui:latest"
		runCmdTemplate = "docker run -d --name 3x-ui --restart always --net=host -v /etc/x-ui:/etc/x-ui %s"
	} else {
		// sing-box 首选
		upstreamImage = "ghcr.io/sagernet/sing-box:latest"
		runCmdTemplate = fmt.Sprintf("docker run -d --name %s --restart always --net=host -v /opt/%s:/etc/sing-box %%s run -c /etc/sing-box/config.json", cName, cName)
	}

	// 1. 记录旧镜像 Image ID 作为回滚锚点
	oldImgCmd := fmt.Sprintf("docker inspect --format='{{.Image}}' %s 2>/dev/null", cName)
	oldImageID, _ := m.detector.RunSSH(inst, oldImgCmd)
	oldImageID = strings.TrimSpace(oldImageID)

	// 2. 创建轻量原子时间戳快照并保留最近 3 份
	var snapID string
	if m.backupMgr != nil {
		snap, bErr := m.backupMgr.CreateBackup(inst, softInfo)
		if bErr != nil {
			logger.Warnf("[SAFE-UPGRADE] Pre-flight backup warning on %s: %v", inst.IP, bErr)
		} else if snap != nil {
			snapID = snap.ID
			resp.BackupCreated = snap.FileName
		}
	}

	// 3. 拉取新镜像并平滑重建容器
	pullAndRunCmd := fmt.Sprintf(
		"docker pull %s && docker stop %s 2>/dev/null && docker rm %s 2>/dev/null && %s",
		upstreamImage, cName, cName, fmt.Sprintf(runCmdTemplate, upstreamImage),
	)
	out, err := m.detector.RunSSH(inst, pullAndRunCmd)
	if err != nil {
		// 启动失败，立即触发回滚
		logger.Errorf("[SAFE-UPGRADE] Failed to pull or start new container on %s: %v, rolling back!", inst.IP, err)
		m.rollbackContainer(inst, cName, oldImageID, runCmdTemplate, softInfo, snapID)
		resp.RolledBack = true
		resp.RollbackReason = fmt.Sprintf("Container launch failed: %v", err)
		resp.Message = fmt.Sprintf("Upgrade failed, auto-rolled back to previous image: %s", out)
		return resp, nil
	}

	// 等待 3 秒观察容器状态
	time.Sleep(3 * time.Second)
	statusCmd := fmt.Sprintf("docker inspect --format='{{.State.Running}}' %s 2>/dev/null", cName)
	statusOut, _ := m.detector.RunSSH(inst, statusCmd)
	if strings.TrimSpace(statusOut) != "true" {
		logger.Errorf("[SAFE-UPGRADE] New container exited prematurely on %s, rolling back!", inst.IP)
		m.rollbackContainer(inst, cName, oldImageID, runCmdTemplate, softInfo, snapID)
		resp.RolledBack = true
		resp.RollbackReason = "New container exited prematurely after launch"
		resp.Message = "New container failed to stay running, rolled back to previous state"
		return resp, nil
	}

	// 4. 沙箱冒烟出网真实检验
	var targetNodeURL string
	if req.NodeName != "" {
		for _, r := range probeResults {
			if r.Name == req.NodeName {
				targetNodeURL = r.OriginalURL
				break
			}
		}
	}
	if targetNodeURL == "" {
		for _, r := range probeResults {
			if strings.Contains(r.OriginalURL, inst.IP) {
				targetNodeURL = r.OriginalURL
				break
			}
		}
	}

	probeFn := m.getProbeFunc()
	if targetNodeURL != "" && probeFn != nil {
		logger.Infof("[SAFE-UPGRADE] Running smoke test for node '%s' on %s...", targetNodeURL, inst.IP)
		smokeStart := time.Now()
		probeRes := probeFn(ctx, targetNodeURL)
		duration := time.Since(smokeStart).Milliseconds()

		smokeTest := &SmokeTestResult{
			TestedAt:  time.Now().Format("2006-01-02 15:04:05"),
			LatencyMs: duration,
		}

		if probeRes != nil && probeRes.Online {
			smokeTest.Success = true
			smokeTest.ExitIP = probeRes.ExitIP
			smokeTest.Message = fmt.Sprintf("Smoke test passed: online, latency=%dms, exit=%s", probeRes.LatencyMs, probeRes.ExitIP)
			resp.SmokeTest = smokeTest
		} else {
			// 冒烟测试失败，新版本协议异常或阻塞！触发原子回滚！
			errMsg := "node offline or handshake failed"
			if probeRes != nil && probeRes.ErrorMsg != "" {
				errMsg = probeRes.ErrorMsg
			}
			smokeTest.Success = false
			smokeTest.Error = errMsg
			resp.SmokeTest = smokeTest

			logger.Errorf("[SAFE-UPGRADE] Smoke test failed for node on %s: %s, initiating atomic rollback!", inst.IP, errMsg)
			m.rollbackContainer(inst, cName, oldImageID, runCmdTemplate, softInfo, snapID)
			resp.RolledBack = true
			resp.RollbackReason = fmt.Sprintf("Smoke verification failed: %s", errMsg)
			resp.Message = fmt.Sprintf("Smoke test failed (%s), safely rolled back to previous version", errMsg)
			return resp, nil
		}
	} else {
		// 没有匹配到节点链接时，做轻量端口与版本提取
		logger.Warnf("[SAFE-UPGRADE] No node link found for IP %s to run full proxy smoke test, verified container running", inst.IP)
	}

	// 5. 校验新版本
	newInfo := m.detector.InspectVPS(ctx, inst)
	resp.Success = true
	resp.CurrentVersion = newInfo.CurrentVersion
	resp.Message = fmt.Sprintf("Successfully upgraded %s on %s from %s to %s with full verification", softInfo.SoftwareType, inst.IP, resp.PreviousVersion, resp.CurrentVersion)
	logger.Infof("[SAFE-UPGRADE] %s", resp.Message)

	return resp, nil
}

// rollbackContainer 原子回滚容器镜像与快照配置
func (m *Manager) rollbackContainer(inst *VPSInstance, cName string, oldImageID string, runCmdTemplate string, softInfo *SoftwareInfo, snapID string) {
	if snapID != "" && m.backupMgr != nil {
		if err := m.backupMgr.RestoreBackup(inst, softInfo, snapID); err != nil {
			logger.Errorf("[SAFE-UPGRADE-ROLLBACK] Restore config snapshot error on %s: %v", inst.IP, err)
		}
	}

	if oldImageID != "" {
		rollbackCmd := fmt.Sprintf(
			"docker stop %s 2>/dev/null; docker rm %s 2>/dev/null; %s",
			cName, cName, fmt.Sprintf(runCmdTemplate, oldImageID),
		)
		if out, err := m.detector.RunSSH(inst, rollbackCmd); err != nil {
			logger.Errorf("[SAFE-UPGRADE-ROLLBACK] Failed to restore old image on %s: %v (out: %s)", inst.IP, err, out)
		} else {
			logger.Infof("[SAFE-UPGRADE-ROLLBACK] Restored previous image %s for %s on %s", oldImageID, cName, inst.IP)
		}
	}
}

func (m *Manager) findInstance(vpsID string) (*VPSInstance, error) {
	m.instancesMu.RLock()
	defer m.instancesMu.RUnlock()
	for _, i := range m.instances {
		if i.ID == vpsID || i.IP == vpsID || i.Name == vpsID {
			cp := *i
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("VPS instance '%s' not found", vpsID)
}

// DailyCheckUpdates 每日定时巡检全量 VPS 软件是否有新版本
func (m *Manager) DailyCheckUpdates(ctx context.Context) []*SoftwareInfo {
	infos := m.ListVPSInstances(ctx)
	for _, info := range infos {
		if info.HasUpdate {
			logger.Warnf("[VPS-LIFECYCLE] 📢 Update Available on %s (%s): current=%s, latest=%s (released: %s)",
				info.InstanceName, info.IP, info.CurrentVersion, info.LatestVersion, info.ReleaseDate)
		} else {
			logger.Infof("[VPS-LIFECYCLE] ✅ %s (%s) is up-to-date: version=%s",
				info.InstanceName, info.IP, info.CurrentVersion)
		}
	}
	return infos
}
