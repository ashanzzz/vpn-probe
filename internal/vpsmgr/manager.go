package vpsmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
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

// SetInstancesForTest 注入单测实例列表
func (m *Manager) SetInstancesForTest(insts []*VPSInstance) {
	m.instancesMu.Lock()
	defer m.instancesMu.Unlock()
	cloned := make([]*VPSInstance, len(insts))
	for idx, inst := range insts {
		clone := *inst
		cloned[idx] = &clone
	}
	m.instances = cloned
}

// SetDetectorForTest 注入单测探查器
func (m *Manager) SetDetectorForTest(det *Detector) {
	m.detector = det
	m.backupMgr = NewBackupManager(det)
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

		// 更新或追加 (采用 Copy-on-Write 替换指针，避免与其他并发读取的 Goroutine 发生 Data Race)
		var updated bool
		for idx, inst := range m.instances {
			if inst.ID == hi.ID || inst.IP == hi.IP {
				updatedInst := *inst
				updatedInst.Name = hi.Name
				updatedInst.SSHPort = sshPort
				updatedInst.SSHUser = sshUser
				if pass != "" {
					updatedInst.SSHPassword = pass
				}
				updatedInst.Status = hi.Status
				updatedInst.Region = hi.Region
				m.instances[idx] = &updatedInst
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
	for idx, inst := range m.instances {
		clone := *inst
		insts[idx] = &clone
	}
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
			clone := *inst
			matchedInst = &clone
			break
		}
	}
	m.instancesMu.RUnlock()

	if matchedInst == nil {
		return nil, fmt.Errorf("no matching managed VPS instance found for IP %s", targetIP)
	}

	return m.healer.ExecutePortHeal(ctx, matchedInst, targetRes.Name, targetRes.OriginalURL, targetPort)
}

// findInstance 统一根据 ID, IP 或 Name 查找 VPS 实例 (返回独立 Clone 副本以保证跨 Goroutine 读写安全)
func (m *Manager) findInstance(target string) *VPSInstance {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	m.instancesMu.RLock()
	defer m.instancesMu.RUnlock()
	for _, i := range m.instances {
		if i.ID == target || i.IP == target || strings.EqualFold(i.Name, target) || strings.Contains(strings.ToLower(i.Name), strings.ToLower(target)) {
			clone := *i
			return &clone
		}
	}
	return nil
}

// InstallSoftware 在指定 VPS 上全自动一键部署 sing-box 或 3x-ui
func (m *Manager) InstallSoftware(ctx context.Context, vpsID string, software string) (string, error) {
	inst := m.findInstance(vpsID)
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

// UpdateSoftware 升级指定 VPS 上的代理软件至最新版本或镜像并平滑重启（Docker 模式下执行容器重建事务，Native 模式下触发脚本更新）
func (m *Manager) UpdateSoftware(ctx context.Context, vpsID string) (string, error) {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return "", fmt.Errorf("VPS instance '%s' not found", vpsID)
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	if softInfo == nil || softInfo.SoftwareType == "none" || softInfo.Status == "not_installed" {
		return "", fmt.Errorf("instance %s does not have manageable proxy software installed", inst.IP)
	}

	var updateCmd string
	if softInfo.DeployMode == "docker" {
		cName := softInfo.ContainerName
		if cName == "" {
			if softInfo.SoftwareType == "3x-ui" {
				cName = "3x-ui"
			} else {
				cName = "sb-mgr"
			}
		}

		if softInfo.SoftwareType == "3x-ui" {
			cfgDir := "/etc/x-ui"
			if softInfo.ConfigPath != "" {
				cfgDir = path.Dir(softInfo.ConfigPath)
			}
			updateCmd = fmt.Sprintf("docker pull ghcr.io/mhsanaei/3x-ui:latest && "+
				"docker rm -f %s-old 2>/dev/null; docker rename %s %s-old && docker stop %s-old && "+
				"(docker run -d --name %s --restart always --net=host -v %s:/etc/x-ui ghcr.io/mhsanaei/3x-ui:latest || "+
				"(docker rm -f %s 2>/dev/null; docker rename %s-old %s; docker start %s; exit 1)) && "+
				"docker rm -f %s-old",
				cName, cName, cName, cName,
				cName, cfgDir,
				cName, cName, cName, cName,
				cName)
		} else if softInfo.SoftwareType == "xray" {
			cfgDir := "/etc/v2ray-agent/docker"
			if softInfo.ConfigPath != "" {
				cfgDir = path.Dir(softInfo.ConfigPath)
			}
			updateCmd = fmt.Sprintf("docker pull ghcr.io/xtls/xray-core:latest && "+
				"docker rm -f %s-old 2>/dev/null; docker rename %s %s-old && docker stop %s-old && "+
				"(docker run -d --name %s --restart always --net=host -v %s:/etc/xray ghcr.io/xtls/xray-core:latest run -c /etc/xray/config.json || "+
				"(docker rm -f %s 2>/dev/null; docker rename %s-old %s; docker start %s; exit 1)) && "+
				"docker rm -f %s-old",
				cName, cName, cName, cName,
				cName, cfgDir,
				cName, cName, cName, cName,
				cName)
		} else {
			// 默认 sing-box
			cfgDir := fmt.Sprintf("/opt/%s", cName)
			if softInfo.ConfigPath != "" {
				cfgDir = path.Dir(softInfo.ConfigPath)
			}
			updateCmd = fmt.Sprintf("docker pull ghcr.io/sagernet/sing-box:latest && "+
				"docker rm -f %s-old 2>/dev/null; docker rename %s %s-old && docker stop %s-old && "+
				"(docker run -d --name %s --restart always --net=host -v %s:/etc/sing-box ghcr.io/sagernet/sing-box:latest run -c /etc/sing-box/config.json || "+
				"(docker rm -f %s 2>/dev/null; docker rename %s-old %s; docker start %s; exit 1)) && "+
				"docker rm -f %s-old",
				cName, cName, cName, cName,
				cName, cfgDir,
				cName, cName, cName, cName,
				cName)
		}
	} else if softInfo.DeployMode == "native" {
		if softInfo.SoftwareType == "sing-box" {
			updateCmd = "bash -c \"$(curl -fsSL https://sing-box.app/deb-install.sh)\" 2>/dev/null || " +
				"bash -c \"$(curl -fsSL https://raw.githubusercontent.com/SagerNet/sing-box/main/install.sh)\" 2>/dev/null; " +
				"systemctl restart sing-box 2>/dev/null || service sing-box restart 2>/dev/null"
		} else if softInfo.SoftwareType == "3x-ui" {
			updateCmd = "x-ui update 2>/dev/null || bash <(curl -Ls https://raw.githubusercontent.com/mhsanaei/3x-ui/master/install.sh) 2>/dev/null; " +
				"systemctl restart x-ui 2>/dev/null || service x-ui restart 2>/dev/null"
		} else if softInfo.SoftwareType == "xray" {
			updateCmd = "bash -c \"$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)\" @ install 2>/dev/null; " +
				"systemctl restart xray 2>/dev/null || service xray restart 2>/dev/null"
		} else {
			return "", fmt.Errorf("unsupported native software type: %s", softInfo.SoftwareType)
		}
	} else {
		return "", fmt.Errorf("instance %s does not run manageable software (detected: %s/%s)", inst.IP, softInfo.SoftwareType, softInfo.DeployMode)
	}

	out, err := m.detector.RunSSH(inst, updateCmd)
	if err != nil {
		return out, fmt.Errorf("update software on %s failed: %w (output: %s)", inst.IP, err, out)
	}

	return fmt.Sprintf("Successfully updated %s (%s) on %s to latest", softInfo.SoftwareType, softInfo.DeployMode, inst.IP), nil
}

// CreateBackup 为指定 VPS 创建配置快照
func (m *Manager) CreateBackup(ctx context.Context, vpsID string) (*BackupSnapshot, error) {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return nil, fmt.Errorf("VPS instance '%s' not found", vpsID)
	}
	softInfo := m.detector.InspectVPS(ctx, inst)
	return m.backupMgr.CreateBackup(inst, softInfo)
}

// ListBackups 列出指定 VPS 当前所有历史快照
func (m *Manager) ListBackups(ctx context.Context, vpsID string) ([]*BackupSnapshot, error) {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return nil, fmt.Errorf("VPS instance '%s' not found", vpsID)
	}
	softInfo := m.detector.InspectVPS(ctx, inst)
	return m.backupMgr.ListBackups(inst, softInfo)
}

// RestoreBackup 从指定快照还原配置
func (m *Manager) RestoreBackup(ctx context.Context, vpsID string, snapshotID string) error {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return fmt.Errorf("VPS instance '%s' not found", vpsID)
	}
	softInfo := m.detector.InspectVPS(ctx, inst)
	return m.backupMgr.RestoreBackup(inst, softInfo, snapshotID)
}

// PerformLifecycleAction 处理容器或服务的生命周期操作 (start, stop, restart, backup)
func (m *Manager) PerformLifecycleAction(ctx context.Context, req *LifecycleActionRequest) (string, error) {
	if req == nil || req.VPSID == "" {
		return "", fmt.Errorf("vps_id is required")
	}

	inst := m.findInstance(req.VPSID)
	if inst == nil {
		return "", fmt.Errorf("VPS instance '%s' not found", req.VPSID)
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	action := strings.ToLower(strings.TrimSpace(req.Action))

	switch action {
	case "backup":
		snap, err := m.backupMgr.CreateBackup(inst, softInfo)
		if err != nil {
			return "", fmt.Errorf("backup failed: %w", err)
		}
		return fmt.Sprintf("Created snapshot %s (%d bytes)", snap.FileName, snap.SizeBytes), nil

	case "start", "stop", "restart":
		var cmd string
		if softInfo.DeployMode == "native" {
			svc := softInfo.SoftwareType
			if svc == "3x-ui" {
				svc = "x-ui"
			}
			cmd = fmt.Sprintf("systemctl %s %s 2>/dev/null || service %s %s 2>/dev/null", action, svc, svc, action)
		} else {
			cName := softInfo.ContainerName
			if cName == "" {
				if softInfo.SoftwareType == "3x-ui" {
					cName = "3x-ui"
				} else {
					cName = "sb-mgr"
				}
			}
			cmd = fmt.Sprintf("docker %s %s", action, cName)
		}

		out, err := m.detector.RunSSH(inst, cmd)
		if err != nil {
			return out, fmt.Errorf("execute lifecycle action '%s' on %s failed: %w", action, inst.IP, err)
		}
		return fmt.Sprintf("Successfully executed '%s' on %s", action, inst.IP), nil

	default:
		return "", fmt.Errorf("unsupported lifecycle action: %s (allowed: start, stop, restart, backup)", req.Action)
	}
}

// PerformSafeUpgrade 安全升级：升级前自动创建配置快照，升级后执行真实出网冒烟测试，若冒烟失败则原子回滚！
func (m *Manager) PerformSafeUpgrade(ctx context.Context, req *SafeUpgradeRequest) (*SafeUpgradeResponse, error) {
	if req == nil || req.VPSID == "" {
		return nil, fmt.Errorf("vps_id is required")
	}

	inst := m.findInstance(req.VPSID)
	if inst == nil {
		return nil, fmt.Errorf("VPS instance '%s' not found", req.VPSID)
	}

	softInfo := m.detector.InspectVPS(ctx, inst)
	prevVer := softInfo.CurrentVersion

	resp := &SafeUpgradeResponse{
		VPSID:           inst.ID,
		IP:              inst.IP,
		SoftwareType:    softInfo.SoftwareType,
		PreviousVersion: prevVer,
	}

	// 1. 升级前自动执行快照备份
	snap, err := m.backupMgr.CreateBackup(inst, softInfo)
	if err != nil {
		logger.Warnf("[SAFE-UPGRADE] Pre-upgrade backup failed on %s: %v, continuing with caution", inst.IP, err)
	} else {
		resp.BackupCreated = snap.FileName
	}

	// 2. 执行升级更新
	msg, updateErr := m.UpdateSoftware(ctx, req.VPSID)
	if updateErr != nil {
		resp.Success = false
		resp.Message = fmt.Sprintf("Update command failed: %v (output: %s)", updateErr, msg)
		return resp, updateErr
	}

	// 3. 升级后重新探查版本
	postInfo := m.detector.InspectVPS(ctx, inst)
	resp.CurrentVersion = postInfo.CurrentVersion

	// 4. 执行出网冒烟测试 (Smoke Test)
	probeFn := m.getProbeFunc()
	nodeTarget := req.NodeName
	if nodeTarget == "" {
		nodeTarget = inst.Name
	}
	if probeFn != nil && nodeTarget != "" {
		smokeRes := probeFn(ctx, nodeTarget)
		smoke := &SmokeTestResult{
			Success:  smokeRes != nil && smokeRes.Online,
			TestedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
		if smokeRes != nil {
			smoke.LatencyMs = int64(smokeRes.LatencyMs)
			smoke.ExitIP = smokeRes.ExitIP
			if !smokeRes.Online {
				smoke.Error = "Smoke test probe offline or handshake failed"
				if smokeRes.Diagnostic != nil {
					smoke.Error = smokeRes.Diagnostic.StatusText
				}
			}
		} else {
			smoke.Error = "Smoke test returned nil result"
		}
		resp.SmokeTest = smoke

		// 5. 若冒烟失败，执行自动故障回滚或报告失败
		if !smoke.Success {
			resp.Success = false
			if snap != nil {
				logger.Warnf("[SAFE-UPGRADE] ⚠️ Smoke test failed after upgrade on %s! Initiating automatic rollback to snapshot %s...", inst.IP, snap.FileName)
				rollbackErr := m.backupMgr.RestoreBackup(inst, softInfo, snap.ID)
				if rollbackErr != nil {
					resp.RolledBack = false
					resp.RollbackReason = fmt.Sprintf("Smoke test failed (%s), but rollback also failed: %v", smoke.Error, rollbackErr)
					resp.Message = "Upgrade failed smoke test; automatic rollback failed!"
				} else {
					resp.RolledBack = true
					resp.RollbackReason = fmt.Sprintf("Smoke test failed: %s. Successfully rolled back to %s.", smoke.Error, snap.FileName)
					resp.Message = "Upgrade rolled back automatically due to failed smoke test."
				}
			} else {
				resp.RolledBack = false
				resp.RollbackReason = fmt.Sprintf("Smoke test failed: %s (no pre-upgrade backup snapshot available)", smoke.Error)
				resp.Message = "Upgrade failed smoke test and no snapshot was available for rollback!"
			}
			return resp, nil
		}
	}

	resp.Success = true
	resp.Message = fmt.Sprintf("Successfully upgraded %s from %s to %s", inst.IP, prevVer, resp.CurrentVersion)
	return resp, nil
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
