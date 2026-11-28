package vpsmgr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
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
	vpsCache       []*SoftwareInfo
	vpsCacheTime   time.Time
	vpsCacheMu     sync.RWMutex
	vpsRefreshing  atomic.Bool
	detector       *Detector
	ghChecker      *GitHubChecker
	healer         *AutoHealer
	backupMgr      *BackupManager
	substoreClient *substore.Client
	configPath        string
	probeFunc         NodeProbeFunc
	probeMu           sync.RWMutex
	onNodeAdded       func(node config.MonitoredNode)
	substoreSmokeFunc func(ctx context.Context) error
}

// SetSubStoreSmokeFunc 注册 Sub-Store 升级后的真实健康校验函数。
func (m *Manager) SetSubStoreSmokeFunc(fn func(ctx context.Context) error) {
	m.probeMu.Lock()
	defer m.probeMu.Unlock()
	m.substoreSmokeFunc = fn
}

func (m *Manager) getSubStoreSmokeFunc() func(ctx context.Context) error {
	m.probeMu.RLock()
	defer m.probeMu.RUnlock()
	return m.substoreSmokeFunc
}

// SetOnNodeAdded 注册新部署线路机的热上线回调（写入 Scheduler 与触发五维同步）。
func (m *Manager) SetOnNodeAdded(fn func(node config.MonitoredNode)) {
	m.hubMu.Lock()
	defer m.hubMu.Unlock()
	m.onNodeAdded = fn
}

// SetOnURLUpdated 注册换端口后的热更新回调。
func (m *Manager) SetOnURLUpdated(fn func(oldURL, newURL string)) {
	if m.healer != nil {
		m.healer.SetOnURLUpdated(fn)
	}
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
	cloned := make([]*VPSInstance, len(insts))
	for idx, inst := range insts {
		clone := *inst
		cloned[idx] = &clone
	}
	m.instances = cloned
	m.instancesMu.Unlock()

	m.vpsCacheMu.Lock()
	m.vpsCache = nil
	m.vpsCacheTime = time.Time{}
	m.vpsCacheMu.Unlock()
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
	m.vpsHubURL = url
	m.vpsHubUser = user
	m.vpsHubPass = pass
	m.hubMu.Unlock()

	if url != "" && m.detector != nil {
		hubClient := NewHubClient(url, user, pass)
		m.detector.SetHubClient(hubClient)
		m.detector.SetHubExecutor(func(ctx context.Context, instanceID, cmd string, timeout time.Duration) (string, error) {
			return hubClient.Exec(ctx, instanceID, cmd, timeout)
		})
	}
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
	if cfg != nil && cfg.SubStore.SelfSubName != "" {
		heal.SetSelfSubName(cfg.SubStore.SelfSubName)
	}

	var vpsHubURL, vpsHubUser, vpsHubPass string
	if cfg != nil {
		vpsHubURL = cfg.VPSHub.URL
		vpsHubUser = cfg.VPSHub.Username
		vpsHubPass = cfg.VPSHub.Password
	}
	if env := os.Getenv("VPS_HUB_URL"); env != "" {
		vpsHubURL = env
	}
	if env := os.Getenv("VPS_HUB_USER"); env != "" {
		vpsHubUser = env
	}
	if env := os.Getenv("VPS_HUB_PASS"); env != "" {
		vpsHubPass = env
	}

	var loadedInstances []*VPSInstance
	if cfg != nil {
		for _, vc := range cfg.VPSInstances {
			sshPort := vc.SSHPort
			if sshPort <= 0 {
				sshPort = 22
			}
			sshUser := vc.SSHUser
			if sshUser == "" {
				sshUser = "root"
			}
			loadedInstances = append(loadedInstances, &VPSInstance{
				ID:          vc.ID,
				Name:        vc.Name,
				IP:          vc.IP,
				SSHPort:     sshPort,
				SSHUser:     sshUser,
				SSHPassword: vc.SSHPassword,
				ProviderID:  vc.ProviderID,
				Status:      vc.Status,
				Region:      vc.Region,
				IsNAT:       vc.IsNAT,
				NATPorts:    append([]int(nil), vc.NATPorts...),
			})
		}
	}

	m := &Manager{
		vpsHubURL:      vpsHubURL,
		vpsHubUser:     vpsHubUser,
		vpsHubPass:     vpsHubPass,
		instances:      loadedInstances,
		detector:       det,
		ghChecker:      gh,
		healer:         heal,
		backupMgr:      NewBackupManager(det),
		substoreClient: sc,
		configPath:     configPath,
	}

	// 若配置了 VPS Hub，注入 WebSocket PTY 执行器并异步同步实例（无任何源码硬编码凭据）
	if vpsHubURL != "" {
		hubClient := NewHubClient(vpsHubURL, vpsHubUser, vpsHubPass)
		det.SetHubClient(hubClient)
		det.SetHubExecutor(func(ctx context.Context, instanceID, cmd string, timeout time.Duration) (string, error) {
			return hubClient.Exec(ctx, instanceID, cmd, timeout)
		})
		go m.refreshFromVPSHub()
	}

	return m
}

// refreshFromVPSHub 从 Unraid VPS Hub API 同步最新实例与凭据
func (m *Manager) refreshFromVPSHub() {
	hubURL, hubUser, hubPass := m.getVPSHubConfig()
	if hubURL == "" {
		return
	}
	client := &http.Client{Timeout: 12 * time.Second}
	defer client.CloseIdleConnections()

	// 1. 登录 VPS Hub 获取 Token
	loginPayload, _ := json.Marshal(map[string]string{
		"username": hubUser,
		"password": hubPass,
	})
	req, err := http.NewRequest("POST", strings.TrimRight(hubURL, "/")+"/api/auth/login", strings.NewReader(string(loginPayload)))
	if err != nil {
		logger.Warnf("[VPS-HUB] Build login request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logger.Warnf("[VPS-HUB] Login failed (%s): %v", hubURL, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Warnf("[VPS-HUB] Login returned HTTP %d", resp.StatusCode)
		return
	}

	var loginRes struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginRes); err != nil || loginRes.Token == "" {
		logger.Warnf("[VPS-HUB] Login decode empty token: %v", err)
		return
	}

	// 2. 拉取实例列表
	instReq, err := http.NewRequest("GET", strings.TrimRight(hubURL, "/")+"/api/instances", nil)
	if err != nil {
		return
	}
	instReq.Header.Set("Authorization", "Bearer "+loginRes.Token)

	instResp, err := client.Do(instReq)
	if err != nil {
		logger.Warnf("[VPS-HUB] Fetch /api/instances failed: %v", err)
		return
	}
	defer instResp.Body.Close()
	if instResp.StatusCode != http.StatusOK {
		logger.Warnf("[VPS-HUB] Fetch /api/instances HTTP %d", instResp.StatusCode)
		return
	}

	var hubData struct {
		Instances []*struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			IP           string `json:"ip"`
			SSHPort      int    `json:"ssh_port"`
			SSHUser      string `json:"ssh_user"`
			SSHPassword  string `json:"ssh_password"`
			RootPassword string `json:"root_password"`
			ProviderID   string `json:"provider_id"`
			Status       string `json:"status"`
			Region       string `json:"region"`
			Category     string `json:"category"`
			PortLimit    int    `json:"port_limit"`
			PortCount    int    `json:"port_count"`
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
		if hi.ProviderID == "lazycat" && hi.Category != "固定出口" {
			continue // 全球家宽网关不属于可托管线路机；固定出口（如 HKG-3）必须纳入
		}
		pass := hi.SSHPassword
		if pass == "" {
			pass = hi.RootPassword
		}
		sshPort := hi.SSHPort
		if sshPort <= 0 && hi.ProviderID != "lazycat" {
			sshPort = 22
		}
		sshUser := hi.SSHUser
		if sshUser == "" {
			sshUser = "root"
		}

		// 更新或追加 (采用 Copy-on-Write 替换指针，避免与其他并发读取的 Goroutine 发生 Data Race)
		// 只要实例来自 VPS Hub（:3338），全部统一设为 hub 执行模式，vpn-probe 无需持有任何主机 SSH 密码。
		var updated bool
		for idx, inst := range m.instances {
			if (hi.ID != "" && inst.ID == hi.ID) || (hi.IP != "" && inst.IP == hi.IP) {
				updatedInst := *inst
				if hi.ID != "" {
					updatedInst.ID = hi.ID
				}
				updatedInst.Name = hi.Name
				updatedInst.SSHPort = sshPort
				updatedInst.SSHUser = sshUser
				if pass != "" {
					updatedInst.SSHPassword = pass
				}
				updatedInst.ProviderID = hi.ProviderID
				updatedInst.Status = hi.Status
				updatedInst.Region = hi.Region
				updatedInst.Category = hi.Category
				updatedInst.PortLimit = hi.PortLimit
				updatedInst.PortCount = hi.PortCount
				updatedInst.IsNAT = hi.ProviderID == "lazycat"
				updatedInst.ExecMode = "hub"
				m.instances[idx] = &updatedInst
				updated = true
				break
			}
		}
		if !updated && hi.IP != "" && hi.ID != "" {
			m.instances = append(m.instances, &VPSInstance{
				ID:          hi.ID,
				Name:        hi.Name,
				IP:          hi.IP,
				SSHPort:     sshPort,
				SSHUser:     sshUser,
				SSHPassword: pass,
				ProviderID:  hi.ProviderID,
				IsNAT:       hi.ProviderID == "lazycat",
				ExecMode:    "hub",
				Status:      hi.Status,
				Region:      hi.Region,
				Category:    hi.Category,
				PortLimit:   hi.PortLimit,
				PortCount:   hi.PortCount,
			})
		}
	}
}

// ListVPSInstances 优先返回短期缓存，避免前端/接口因逐台 SSH 阻塞超时；缓存过期时带 5s 有界超时刷新并后台异步更新
func (m *Manager) ListVPSInstances(ctx context.Context) []*SoftwareInfo {
	m.vpsCacheMu.RLock()
	hasCache := len(m.vpsCache) > 0
	cacheAge := time.Since(m.vpsCacheTime)
	if hasCache && cacheAge < 90*time.Second {
		cached := cloneSoftwareInfos(m.vpsCache)
		m.vpsCacheMu.RUnlock()
		if cacheAge > 30*time.Second {
			m.RefreshVPSInstancesAsync()
		}
		return cached
	}
	m.vpsCacheMu.RUnlock()

	return m.RefreshVPSInstances(ctx)
}

// RefreshVPSInstancesAsync 异步触发后台 VPS 状态刷新（防重入）
func (m *Manager) RefreshVPSInstancesAsync() {
	if !m.vpsRefreshing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer m.vpsRefreshing.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		m.inspectAndCacheVPS(ctx)
	}()
}

// RefreshVPSInstances 同步执行有界超时(<=25s) VPS 状态探查并更新缓存
func (m *Manager) RefreshVPSInstances(ctx context.Context) []*SoftwareInfo {
	if hubURL, _, _ := m.getVPSHubConfig(); hubURL != "" {
		m.refreshFromVPSHub()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	boundedCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	return m.inspectAndCacheVPS(boundedCtx)
}

func (m *Manager) inspectAndCacheVPS(ctx context.Context) []*SoftwareInfo {
	m.instancesMu.RLock()
	insts := make([]*VPSInstance, len(m.instances))
	for idx, inst := range m.instances {
		clone := *inst
		insts[idx] = &clone
	}
	m.instancesMu.RUnlock()

	if len(insts) == 0 {
		return []*SoftwareInfo{}
	}

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

	m.vpsCacheMu.Lock()
	m.vpsCache = cloneSoftwareInfos(results)
	m.vpsCacheTime = time.Now()
	m.vpsCacheMu.Unlock()

	return results
}

func cloneSoftwareInfos(in []*SoftwareInfo) []*SoftwareInfo {
	out := make([]*SoftwareInfo, len(in))
	for i, item := range in {
		if item == nil {
			continue
		}
		c := *item
		if item.ActivePorts != nil {
			c.ActivePorts = append([]int(nil), item.ActivePorts...)
		}
		if item.ManagedApps != nil {
			c.ManagedApps = make([]*ManagedAppInfo, len(item.ManagedApps))
			for j, app := range item.ManagedApps {
				if app == nil {
					continue
				}
				ac := *app
				c.ManagedApps[j] = &ac
			}
		}
		out[i] = &c
	}
	return out
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

func generateRandomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func generateShortID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// InstallSoftware 在指定 VPS 上全自动一键部署 sing-box 或 3x-ui（懒猫云 NAT 机走 :3338 原生部署，普通主机支持 Docker/原生自动适配）
func (m *Manager) InstallSoftware(ctx context.Context, vpsID string, software string) (string, error) {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return "", fmt.Errorf("VPS instance '%s' not found", vpsID)
	}

	if software == "" || software == "sing-box" {
		if NeedsHub(inst) {
			res, err := m.DeployVLESS(ctx, &DeployVLESSRequest{VPSID: inst.ID})
			if err != nil {
				return "", err
			}
			return res.Message, nil
		}
		// 非懒猫云普通主机：兼容原有 Docker / 无 Docker 原生回退逻辑
		newUUID := generateRandomUUID()
		newSID := generateShortID()
		installPort := 36564
		if inst.IsNAT && len(inst.NATPorts) > 0 {
			installPort = inst.NATPorts[0]
		}
		if envOut, envErr := m.detector.RunSSH(inst, "command -v docker >/dev/null 2>&1 && echo HAS_DOCKER || echo NO_DOCKER; uname -m"); envErr == nil && strings.Contains(envOut, "NO_DOCKER") {
			arch := "x86_64"
			if strings.Contains(envOut, "aarch64") || strings.Contains(envOut, "arm64") {
				arch = "aarch64"
			}
			nativeCmd, buildErr := buildNativeSingBoxInstallCmd(installPort, newUUID, newSID, defaultRealitySNI, arch)
			if buildErr != nil {
				return "", buildErr
			}
			out, err := m.detector.RunSSH(inst, nativeCmd)
			if err != nil {
				return out, fmt.Errorf("install native sing-box failed: %w", err)
			}
			if pubKey := extractOutputKV(out, "PUBLIC_KEY"); pubKey != "" {
				nodeName := BuildSafeTransitNodeName(inst)
				vlessURL := buildVLESSLink(newUUID, inst.IP, installPort, defaultRealitySNI, pubKey, newSID, nodeName)
				m.onboardDeployedTransitNode(ctx, nodeName, &DeployVLESSResponse{VPSID: inst.ID, IP: inst.IP, NodeName: nodeName, ListenPort: installPort, PublicPort: installPort, VLESSURL: vlessURL})
			}
			return fmt.Sprintf("Successfully installed native sing-box VLESS Reality on %s (port %d)\n%s", inst.IP, installPort, strings.TrimSpace(out)), nil
		}
		installCmd := fmt.Sprintf(`
command -v docker >/dev/null 2>&1 || curl -fsSL https://get.docker.com | sh
docker pull ghcr.io/sagernet/sing-box:latest
KEYS=$(docker run --rm ghcr.io/sagernet/sing-box:latest generate reality-keypair)
PRIV_KEY=$(echo "$KEYS" | awk '/PrivateKey/{print $2}')
PUB_KEY=$(echo "$KEYS" | awk '/PublicKey/{print $2}')
if [ -z "$PRIV_KEY" ]; then echo "failed to generate reality keypair" >&2; exit 1; fi
mkdir -p /opt/sb-mgr
cat << EOF > /opt/sb-mgr/config.json
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
          "uuid": "%s",
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
          "private_key": "${PRIV_KEY}",
          "short_id": ["%s"]
        }
      }
    }
  ],
  "outbounds": [{"type": "direct", "tag": "direct"}]
}
EOF
iptables -I INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || true
docker rm -f sb-mgr 2>/dev/null
docker run -d --name sb-mgr --restart always --net=host -v /opt/sb-mgr:/etc/sing-box ghcr.io/sagernet/sing-box:latest run -c /etc/sing-box/config.json
echo "PUBLIC_KEY=${PUB_KEY}"
`, installPort, newUUID, newSID, installPort)

		out, err := m.detector.RunSSH(inst, installCmd)
		if err != nil {
			return out, fmt.Errorf("install sing-box failed: %w", err)
		}
		return fmt.Sprintf("Successfully installed sing-box VLESS Reality container 'sb-mgr' on %s (port %d, uuid=%s, sid=%s)\n%s", inst.IP, installPort, newUUID, newSID, strings.TrimSpace(out)), nil
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

// resolveTargetSoftInfo 当同主机存在多个共存组件（如 23712 上同时有 sing-box、3x-ui、sub-store）时，
// 按指定的 software / containerName 定位目标组件并返回独立 SoftInfo 视图。
func resolveTargetSoftInfo(softInfo *SoftwareInfo, software, containerName string) *SoftwareInfo {
	if softInfo == nil {
		return nil
	}
	sw := strings.ToLower(strings.TrimSpace(software))
	cn := strings.TrimSpace(containerName)
	if sw == "" && cn == "" {
		return softInfo
	}
	for _, app := range softInfo.ManagedApps {
		if app == nil {
			continue
		}
		if (cn != "" && app.ContainerName == cn) || (sw != "" && strings.EqualFold(app.AppType, sw)) {
			focused := *softInfo
			focused.SoftwareType = app.AppType
			focused.DeployMode = app.DeployMode
			focused.ContainerName = app.ContainerName
			focused.ConfigPath = app.ConfigPath
			focused.CurrentVersion = app.CurrentVersion
			focused.LatestVersion = app.LatestVersion
			focused.HasUpdate = app.HasUpdate
			if app.Status != "" {
				focused.Status = app.Status
			}
			return &focused
		}
	}
	return softInfo
}

// UpdateSoftware 升级指定 VPS 主软件至最新版本或镜像并平滑重启
func (m *Manager) UpdateSoftware(ctx context.Context, vpsID string) (string, error) {
	return m.UpdateTargetSoftware(ctx, vpsID, "", "")
}

// UpdateTargetSoftware 升级指定 VPS 上某个具体组件（sing-box / 3x-ui / xray / sub-store）
func (m *Manager) UpdateTargetSoftware(ctx context.Context, vpsID, software, containerName string) (string, error) {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return "", fmt.Errorf("VPS instance '%s' not found", vpsID)
	}

	baseInfo := m.detector.InspectVPS(ctx, inst)
	softInfo := resolveTargetSoftInfo(baseInfo, software, containerName)
	if softInfo == nil || softInfo.SoftwareType == "none" || softInfo.Status == "not_installed" {
		return "", fmt.Errorf("instance %s does not have manageable proxy software installed", inst.IP)
	}

	var updateCmd string
	if softInfo.DeployMode == "docker" {
		cName := softInfo.ContainerName
		if cName == "" {
			switch softInfo.SoftwareType {
			case "3x-ui":
				cName = "3x-ui"
			case "sub-store":
				cName = "sub-store"
			default:
				cName = "sb-mgr"
			}
		}

		if softInfo.SoftwareType == "sub-store" {
			updateCmd = fmt.Sprintf(`set -e
COMPOSE_FILE=$(docker inspect %s --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' 2>/dev/null || true)
COMPOSE_SVC=$(docker inspect %s --format '{{index .Config.Labels "com.docker.compose.service"}}' 2>/dev/null || true)
if [ -n "$COMPOSE_FILE" ] && [ -f "$COMPOSE_FILE" ] && [ -n "$COMPOSE_SVC" ]; then
  docker compose -f "$COMPOSE_FILE" pull "$COMPOSE_SVC" && docker compose -f "$COMPOSE_FILE" up -d "$COMPOSE_SVC"
else
  BIND_FLAGS=$(docker inspect %s --format '{{range .Mounts}}{{if eq .Type "bind"}} -v {{.Source}}:{{.Destination}}{{end}}{{end}}' 2>/dev/null || true)
  PORT_FLAGS=$(docker inspect %s --format '{{range $p, $conf := .HostConfig.PortBindings}}{{if $conf}} -p {{(index $conf 0).HostPort}}:{{$p}}{{end}}{{end}}' 2>/dev/null || true)
  NET_MODE=$(docker inspect %s --format '{{.HostConfig.NetworkMode}}' 2>/dev/null || echo "bridge")
  NET_FLAG="--network=${NET_MODE}"
  docker pull xream/sub-store:latest && docker rm -f %s-old 2>/dev/null || true
  docker rename %s %s-old && docker stop %s-old
  (docker run -d --name %s --restart always ${NET_FLAG} ${PORT_FLAGS} ${BIND_FLAGS} xream/sub-store:latest || (docker rm -f %s 2>/dev/null; docker rename %s-old %s; docker start %s; exit 1))
  docker rm -f %s-old 2>/dev/null || true
fi`, cName, cName, cName, cName, cName, cName, cName, cName, cName, cName, cName, cName, cName, cName, cName)
		} else if softInfo.SoftwareType == "3x-ui" {
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
			updateCmd = "if command -v rc-service >/dev/null 2>&1; then " +
				"ARCH=$(uname -m); DL_ARCH=amd64; [ \"$ARCH\" = \"aarch64\" ] && DL_ARCH=arm64; " +
				"VER=$(wget -qO- https://api.github.com/repos/SagerNet/sing-box/releases/latest | sed -n 's/.*\"tag_name\": *\"v\\([^\"]*\\)\".*/\\1/p' | head -n1); " +
				"[ -n \"$VER\" ] || VER=1.11.15; " +
				"rm -rf /tmp/sb-upd && mkdir -p /tmp/sb-upd && wget -q -O /tmp/sb-upd/sb.tar.gz \"https://github.com/SagerNet/sing-box/releases/download/v${VER}/sing-box-${VER}-linux-${DL_ARCH}.tar.gz\" && " +
				"tar -xzf /tmp/sb-upd/sb.tar.gz -C /tmp/sb-upd && cp \"$(find /tmp/sb-upd -type f -name sing-box | head -n1)\" /usr/local/bin/sing-box && chmod +x /usr/local/bin/sing-box && rm -rf /tmp/sb-upd && " +
				"rc-service sing-box restart; " +
				"else " +
				"bash -c \"$(curl -fsSL https://sing-box.app/deb-install.sh)\" 2>/dev/null || " +
				"bash -c \"$(curl -fsSL https://raw.githubusercontent.com/SagerNet/sing-box/main/install.sh)\" 2>/dev/null; " +
				"systemctl restart sing-box 2>/dev/null || service sing-box restart 2>/dev/null; fi"
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
func (m *Manager) CreateBackup(ctx context.Context, vpsID string, target ...string) (*BackupSnapshot, error) {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return nil, fmt.Errorf("VPS instance '%s' not found", vpsID)
	}
	sw, cn := extractTargetArgs(target)
	softInfo := resolveTargetSoftInfo(m.detector.InspectVPS(ctx, inst), sw, cn)
	return m.backupMgr.CreateBackup(inst, softInfo)
}

// ListBackups 列出指定 VPS 当前所有历史快照
func (m *Manager) ListBackups(ctx context.Context, vpsID string, target ...string) ([]*BackupSnapshot, error) {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return nil, fmt.Errorf("VPS instance '%s' not found", vpsID)
	}
	sw, cn := extractTargetArgs(target)
	softInfo := resolveTargetSoftInfo(m.detector.InspectVPS(ctx, inst), sw, cn)
	return m.backupMgr.ListBackups(inst, softInfo)
}

// RestoreBackup 从指定快照还原配置
func (m *Manager) RestoreBackup(ctx context.Context, vpsID string, snapshotID string, target ...string) error {
	inst := m.findInstance(vpsID)
	if inst == nil {
		return fmt.Errorf("VPS instance '%s' not found", vpsID)
	}
	sw, cn := extractTargetArgs(target)
	softInfo := resolveTargetSoftInfo(m.detector.InspectVPS(ctx, inst), sw, cn)
	return m.backupMgr.RestoreBackup(inst, softInfo, snapshotID)
}

func extractTargetArgs(args []string) (software, container string) {
	if len(args) > 0 {
		software = args[0]
	}
	if len(args) > 1 {
		container = args[1]
	}
	return software, container
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

	softInfo := resolveTargetSoftInfo(m.detector.InspectVPS(ctx, inst), req.Software, req.ContainerName)
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
			cmd = fmt.Sprintf("rc-service %s %s 2>/dev/null || systemctl %s %s 2>/dev/null || service %s %s 2>/dev/null", svc, action, action, svc, svc, action)
		} else {
			cName := softInfo.ContainerName
			if cName == "" {
				switch softInfo.SoftwareType {
				case "3x-ui":
					cName = "3x-ui"
				case "sub-store":
					cName = "sub-store"
				default:
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

// PerformSafeUpgrade 安全升级：升级前自动创建配置快照，升级后执行真实出网/Sub-Store冒烟测试，若冒烟失败则原子回滚！
func (m *Manager) PerformSafeUpgrade(ctx context.Context, req *SafeUpgradeRequest) (*SafeUpgradeResponse, error) {
	if req == nil || req.VPSID == "" {
		return nil, fmt.Errorf("vps_id is required")
	}

	inst := m.findInstance(req.VPSID)
	if inst == nil {
		return nil, fmt.Errorf("VPS instance '%s' not found", req.VPSID)
	}

	softInfo := resolveTargetSoftInfo(m.detector.InspectVPS(ctx, inst), req.Software, req.ContainerName)
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
	msg, updateErr := m.UpdateTargetSoftware(ctx, req.VPSID, req.Software, req.ContainerName)
	if updateErr != nil {
		resp.Success = false
		resp.Message = fmt.Sprintf("Update command failed: %v (output: %s)", updateErr, msg)
		return resp, updateErr
	}

	// 3. 升级后重新探查版本
	postInfo := resolveTargetSoftInfo(m.detector.InspectVPS(ctx, inst), req.Software, req.ContainerName)
	resp.CurrentVersion = postInfo.CurrentVersion

	// 4. 执行冒烟测试 (Sub-Store 校验 API 可用性；代理组件校验真实出网)
	if softInfo.SoftwareType == "sub-store" {
		if ssSmoke := m.getSubStoreSmokeFunc(); ssSmoke != nil {
			start := time.Now()
			ssErr := ssSmoke(ctx)
			smoke := &SmokeTestResult{
				Success:   ssErr == nil,
				LatencyMs: time.Since(start).Milliseconds(),
				TestedAt:  time.Now().Format("2006-01-02 15:04:05"),
				ExitIP:    "Sub-Store API",
			}
			if ssErr != nil {
				smoke.Error = ssErr.Error()
			}
			resp.SmokeTest = smoke
			if !smoke.Success {
				resp.Success = false
				if snap != nil {
					if rbErr := m.backupMgr.RestoreBackup(inst, softInfo, snap.ID); rbErr == nil {
						resp.RolledBack = true
						resp.RollbackReason = fmt.Sprintf("Sub-Store smoke check failed (%s), rolled back to %s", smoke.Error, snap.FileName)
					} else {
						resp.RollbackReason = fmt.Sprintf("Sub-Store smoke check failed (%s), rollback failed: %v", smoke.Error, rbErr)
					}
				}
				resp.Message = resp.RollbackReason
				return resp, nil
			}
		}
		resp.Success = true
		resp.Message = fmt.Sprintf("Successfully upgraded Sub-Store on %s (%s -> %s)", inst.IP, prevVer, resp.CurrentVersion)
		return resp, nil
	}

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

// execModeFor 决定远程命令通道：无公网 SSH 映射的懒猫云 NAT 实例走 :3338 WebSocket PTY。
func execModeFor(providerID string, sshPort int) string {
	if providerID == "lazycat" && sshPort <= 0 {
		return "hub"
	}
	return "ssh"
}
