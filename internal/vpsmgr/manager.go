package vpsmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	vpsHubURL      string
	vpsHubUser     string
	vpsHubPass     string
	hubMu          sync.RWMutex
	instances      []*VPSInstance
	instancesMu    sync.RWMutex
	detector       *Detector
	ghChecker      *GitHubChecker
	healer         *AutoHealer
	backupMgr      *BackupManager
	substoreClient *substore.Client
	configPath     string
	probeFunc      NodeProbeFunc
	probeMu        sync.RWMutex
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

	// 异步尝试从 VPS Hub 动态刷新最新实例
	go m.refreshFromVPSHub()

	return m
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
			results[idx] = m.detector.InspectVPS(ctx, target)
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

// InstallSoftware 在指定 VPS 上全自动一键部署 sing-box 或 3x-ui
func (m *Manager) InstallSoftware(ctx context.Context, vpsID string, software string) (string, error) {
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

	if software == "" || software == "sing-box" {
		// 首选方案：安装现代轻量安全 VLESS Reality sing-box Docker 容器 (彻底废除 Shadowsocks)
		installCmd := `
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
      "listen_port": 36564,
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
iptables -I INPUT -p tcp --dport 36564 -j ACCEPT
docker rm -f sb-mgr 2>/dev/null
docker run -d --name sb-mgr --restart always --net=host -v /opt/sb-mgr:/etc/sing-box ghcr.io/sagernet/sing-box:latest run -c /etc/sing-box/config.json
`
		out, err := m.detector.RunSSH(inst, installCmd)
		if err != nil {
			return out, fmt.Errorf("install sing-box failed: %w", err)
		}
		return fmt.Sprintf("Successfully installed sing-box VLESS Reality container 'sb-mgr' on %s (port 36564)", inst.IP), nil
	} else if software == "3x-ui" {
		cmd := "docker pull ghcr.io/mhsanaei/3x-ui:latest && docker run -d --name 3x-ui --restart always --net=host -v /etc/x-ui:/etc/x-ui ghcr.io/mhsanaei/3x-ui:latest"
		out, err := m.detector.RunSSH(inst, cmd)
		if err != nil {
			return out, fmt.Errorf("install 3x-ui failed: %w", err)
		}
		return fmt.Sprintf("Successfully installed 3x-ui on %s", inst.IP), nil
	}

	return "", fmt.Errorf("unsupported software '%s'", software)
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
