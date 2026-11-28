package vpsmgr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
)

const (
	defaultSingBoxVersion = "1.11.15"
	defaultRealitySNI     = "aws.amazon.com"
)

var forbiddenTransitKeywords = []string{
	"lz-",
	"家宽",
	"动态",
	"residential",
	"resi",
	"global",
}

// ValidateTransitNodeName 校验线路机节点名，严防含家宽关键词导致分类错乱或跳过换端口自愈。
func ValidateTransitNodeName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("节点名称不能为空")
	}
	lower := strings.ToLower(trimmed)
	for _, kw := range forbiddenTransitKeywords {
		if strings.Contains(lower, kw) {
			return fmt.Errorf("线路机节点名 %q 禁止包含家宽保留词 %q", trimmed, kw)
		}
	}
	if model.IsResidentialNode(trimmed, "", "") {
		return fmt.Errorf("线路机节点名 %q 被识别为家宽特征，请更换命名", trimmed)
	}
	return nil
}

// BuildSafeTransitNodeName 根据实例地区与名称生成安全的线路机节点名（如 HK-HKG-NAT）。
func BuildSafeTransitNodeName(inst *VPSInstance) string {
	if inst == nil {
		return "VPS-TRANSIT"
	}
	cc := regionToCountryCode(inst.Region, inst.Name)
	tag := sanitizeNodeTag(inst.Name)
	if tag == "" {
		tag = inst.ID
	}
	if tag == "" {
		tag = "NODE"
	}
	suffix := ""
	if inst.IsNAT || inst.ProviderID == "lazycat" {
		suffix = "-NAT"
	}
	candidate := fmt.Sprintf("%s-%s%s", cc, tag, suffix)
	if ValidateTransitNodeName(candidate) != nil {
		candidate = fmt.Sprintf("%s-ID%s%s", cc, sanitizeNodeTag(inst.ID), suffix)
	}
	return candidate
}

func regionToCountryCode(region, name string) string {
	cc := model.ResolveCountry("", region)
	if cc != "" && cc != "OTHER" {
		return cc
	}
	cc = model.ResolveCountry("", name)
	if cc != "" && cc != "OTHER" {
		return cc
	}
	return "VPS"
}

func sanitizeNodeTag(raw string) string {
	s := strings.TrimSpace(raw)
	for _, noise := range []string{"0.1折", "全球家宽", "固定出口", "lazycat", "LazyCat"} {
		s = strings.ReplaceAll(s, noise, "")
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
		} else if (r == '-' || r == '_' || r == ' ') && b.Len() > 0 && !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 24 {
		out = strings.Trim(out[:24], "-")
	}
	return out
}

// DeployVLESS 在无代理（或需重装 VLESS）的正常流量机上部署 VLESS+Reality，做真机冒烟，并自动写入监控与 Sub-Store。
func (m *Manager) DeployVLESS(ctx context.Context, req *DeployVLESSRequest) (*DeployVLESSResponse, error) {
	if req == nil || strings.TrimSpace(req.VPSID) == "" {
		return nil, errors.New("vps_id 必填")
	}
	inst := m.findInstance(req.VPSID)
	if inst == nil {
		return nil, fmt.Errorf("未找到托管实例 %q", req.VPSID)
	}
	if err := ensureTransitManageable(inst); err != nil {
		return nil, err
	}

	nodeName := strings.TrimSpace(req.NodeName)
	if nodeName == "" {
		nodeName = BuildSafeTransitNodeName(inst)
	}
	if err := ValidateTransitNodeName(nodeName); err != nil {
		return nil, err
	}

	sni := strings.TrimSpace(req.SNI)
	if sni == "" {
		sni = defaultRealitySNI
	}

	resp := &DeployVLESSResponse{
		VPSID:    inst.ID,
		IP:       inst.IP,
		NodeName: nodeName,
	}

	var err error
	if inst.ProviderID == "lazycat" && (inst.IsNAT || NeedsHub(inst)) {
		err = m.deployVLESSViaHub(ctx, inst, req.ListenPort, sni, nodeName, resp)
	} else {
		err = m.deployVLESSViaSSH(ctx, inst, req.ListenPort, sni, nodeName, resp)
	}
	if err != nil {
		errMsg := formatVPSHubError(inst, err)
		if resp.Message == "" {
			resp.Message = errMsg
		}
		return resp, errors.New(errMsg)
	}

	// 真实出网冒烟校验（NAT 端口映射不通时自动切换一次映射重试）
	probeFn := m.getProbeFunc()
	if probeFn != nil && !req.SkipSmoke {
		smokeRes := probeFn(ctx, resp.VLESSURL)
		resp.SmokeTest = smokeResultFromProbe(smokeRes)

		if !resp.SmokeTest.Success && inst.ProviderID == "lazycat" && inst.IsNAT && resp.ListenPort > 0 && resp.PublicPort > 0 {
			logger.Warnf("[VLESS-DEPLOY] %s (%s:%d) 首次冒烟失败，尝试重发布 NAT 公网端口...", nodeName, inst.IP, resp.PublicPort)
			if newPub, newMapID, repErr := m.detector.RepublishNATPortViaHub(ctx, inst.ID, resp.ListenPort, resp.PublicPort, nodeName); repErr == nil && newPub > 0 {
				resp.RetriedNAT = true
				resp.PublicPort = newPub
				if newMapID > 0 {
					resp.MappingID = newMapID
				}
				resp.VLESSURL = replaceURLPort(resp.VLESSURL, newPub)
				retryProbe := probeFn(ctx, resp.VLESSURL)
				resp.SmokeTest = smokeResultFromProbe(retryProbe)
			} else if repErr != nil {
				logger.Warnf("[VLESS-DEPLOY] %s 重发布 NAT 端口失败: %v", nodeName, repErr)
			}
		}

		if !resp.SmokeTest.Success {
			resp.Success = false
			resp.Message = fmt.Sprintf("VLESS 已部署到 %s:%d，但握手冒烟未通过: %s", inst.IP, resp.PublicPort, resp.SmokeTest.Error)
			return resp, errors.New(resp.Message)
		}
	}

	// 自动闭环上线：写入 config.yaml -> 热注入调度器 -> 写入 Sub-Store
	m.onboardDeployedTransitNode(ctx, nodeName, resp)

	resp.Success = true
	resp.Message = fmt.Sprintf("已部署 VLESS Reality 线路机 %s (%s:%d，模式: %s)", nodeName, inst.IP, resp.PublicPort, resp.DeployMode)
	m.RefreshVPSInstancesAsync()
	return resp, nil
}

// deployVLESSViaHub 由 vpn-probe 直接掌控 VLESS 安装、Reality 密钥生成与链接创建，
// 仅调用 :3338 提供的模拟 SSH 命令通道与 NAT 端口映射 API。
func (m *Manager) deployVLESSViaHub(ctx context.Context, inst *VPSInstance, requestedPort int, sni, nodeName string, resp *DeployVLESSResponse) error {
	listenPort := requestedPort
	if listenPort <= 0 {
		picked, err := m.pickLazyCatListenPort(ctx, inst)
		if err != nil {
			return err
		}
		listenPort = picked
	}

	archOut, err := m.detector.RunSSH(inst, "uname -m")
	if err != nil {
		return fmt.Errorf("探测懒猫云实例架构失败: %w", err)
	}
	arch := "x86_64"
	if strings.Contains(archOut, "aarch64") || strings.Contains(archOut, "arm64") {
		arch = "aarch64"
	}

	newUUID := generateRandomUUID()
	newSID := generateShortID()
	installScript, err := buildNativeSingBoxInstallCmd(listenPort, newUUID, newSID, sni, arch)
	if err != nil {
		return err
	}
	out, err := m.detector.RunSSH(inst, installScript)
	if err != nil {
		return fmt.Errorf("经 :3338 安装原生 sing-box 失败: %w (output: %s)", err, truncateStr(out, 300))
	}

	pubKey := extractOutputKV(out, "PUBLIC_KEY")
	if pubKey == "" {
		return fmt.Errorf("原生 sing-box 未返回 Reality 公钥 (output: %s)", truncateStr(out, 300))
	}
	if actualUUID := extractOutputKV(out, "UUID"); actualUUID != "" {
		newUUID = actualUUID
	}

	pubPort, mapID, err := m.ensureLazyCatNATPort(ctx, inst.ID, listenPort, nodeName)
	if err != nil {
		return fmt.Errorf("申请或复用 NAT 端口映射失败: %w", err)
	}

	resp.DeployMode = "hub-native"
	resp.ListenPort = listenPort
	resp.PublicPort = pubPort
	resp.MappingID = mapID
	resp.VLESSURL = buildVLESSLink(newUUID, inst.IP, pubPort, sni, pubKey, newSID, nodeName)
	return nil
}

func (m *Manager) ensureLazyCatNATPort(ctx context.Context, instanceID string, privatePort int, remark string) (publicPort, mappingID int, err error) {
	if portsResp, listErr := m.detector.ListLazyCatPortsViaHub(ctx, instanceID); listErr == nil && portsResp != nil {
		for _, pm := range portsResp.Ports {
			if pm.PrivatePort == privatePort && pm.PublicPort > 0 && (pm.Protocol == "" || strings.EqualFold(pm.Protocol, "tcp")) {
				return pm.PublicPort, pm.ID, nil
			}
		}
	}
	pubPort, addErr := m.detector.AddLazyCatPortViaHub(ctx, instanceID, privatePort, "tcp", remark)
	if addErr != nil {
		return 0, 0, addErr
	}
	// 反查新建映射 ID
	if portsResp, listErr := m.detector.ListLazyCatPortsViaHub(ctx, instanceID); listErr == nil && portsResp != nil {
		for _, pm := range portsResp.Ports {
			if pm.PrivatePort == privatePort && pm.PublicPort == pubPort {
				return pubPort, pm.ID, nil
			}
		}
	}
	return pubPort, 0, nil
}

func (m *Manager) pickLazyCatListenPort(ctx context.Context, inst *VPSInstance) (int, error) {
	portsResp, portErr := m.detector.ListLazyCatPortsViaHub(ctx, inst.ID)
	listening := make(map[int]bool)
	if out, err := m.detector.RunSSH(inst, "ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null || true"); err == nil {
		for _, p := range extractPorts(out) {
			listening[p] = true
		}
	}

	usedMappings := make(map[int]bool)
	if portErr == nil && portsResp != nil {
		// 1. 若已有映射且私网端口无其他进程占用（或本机已有 sing-box 在监听），优先复用以节省配额
		for _, pm := range portsResp.Ports {
			if pm.PrivatePort > 0 {
				usedMappings[pm.PrivatePort] = true
			}
			if pm.PublicPort > 0 {
				usedMappings[pm.PublicPort] = true
			}
			if !strings.EqualFold(pm.Protocol, "tcp") && pm.Protocol != "" {
				continue
			}
			if pm.PrivatePort <= 1024 || pm.PrivatePort == 22 {
				continue
			}
			if !listening[pm.PrivatePort] {
				return pm.PrivatePort, nil
			}
			// 若已有进程在监听，检查是否为 sing-box 自身
			if sbOut, sbErr := m.detector.RunSSH(inst, "which sing-box >/dev/null 2>&1 && echo HAS_SB"); sbErr == nil && strings.Contains(sbOut, "HAS_SB") {
				return pm.PrivatePort, nil
			}
		}
		usedCount := portsResp.PortCount
		if portsResp.Total > usedCount {
			usedCount = portsResp.Total
		}
		if len(portsResp.Ports) > usedCount {
			usedCount = len(portsResp.Ports)
		}
		if portsResp.PortLimit > 0 && usedCount >= portsResp.PortLimit {
			return 0, fmt.Errorf("懒猫云实例 %s NAT 端口配额已满 (%d/%d)，请先释放无用映射", inst.ID, usedCount, portsResp.PortLimit)
		}
	}

	for i := 0; i < 20; i++ {
		cand := GenerateRandomPort()
		if cand > 40000 {
			cand = 20000 + (cand % 20000)
		}
		if !usedMappings[cand] && !listening[cand] {
			return cand, nil
		}
	}
	return 30021, nil
}

func (m *Manager) deployVLESSViaSSH(ctx context.Context, inst *VPSInstance, requestedPort int, sni, nodeName string, resp *DeployVLESSResponse) error {
	listenPort := requestedPort
	if listenPort <= 0 {
		if inst.IsNAT && len(inst.NATPorts) > 0 {
			listenPort = inst.NATPorts[0]
		} else {
			listenPort = GenerateRandomPort()
		}
	}

	probeEnvCmd := "command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && echo HAS_DOCKER || echo NO_DOCKER; uname -m"
	envOut, err := m.detector.RunSSH(inst, probeEnvCmd)
	if err != nil {
		return fmt.Errorf("探测主机环境失败: %w", err)
	}

	hasDocker := strings.Contains(envOut, "HAS_DOCKER")
	arch := "x86_64"
	if strings.Contains(envOut, "aarch64") || strings.Contains(envOut, "arm64") {
		arch = "aarch64"
	}

	newUUID := generateRandomUUID()
	newSID := generateShortID()

	var out string
	if hasDocker {
		resp.DeployMode = "ssh-docker"
		cmd := buildDockerSingBoxInstallCmd(listenPort, newUUID, newSID, sni)
		out, err = m.detector.RunSSH(inst, cmd)
	} else {
		resp.DeployMode = "ssh-native"
		cmd, buildErr := buildNativeSingBoxInstallCmd(listenPort, newUUID, newSID, sni, arch)
		if buildErr != nil {
			return buildErr
		}
		out, err = m.detector.RunSSH(inst, cmd)
	}
	if err != nil {
		return fmt.Errorf("部署 sing-box (%s) 失败: %w (output: %s)", resp.DeployMode, err, truncateStr(out, 300))
	}

	pubKey := extractOutputKV(out, "PUBLIC_KEY")
	if pubKey == "" {
		return fmt.Errorf("部署未返回 Reality 公钥 (output: %s)", truncateStr(out, 300))
	}
	actualUUID := extractOutputKV(out, "UUID")
	if actualUUID != "" {
		newUUID = actualUUID
	}

	resp.ListenPort = listenPort
	resp.PublicPort = listenPort
	resp.VLESSURL = buildVLESSLink(newUUID, inst.IP, listenPort, sni, pubKey, newSID, nodeName)
	return nil
}

func buildDockerSingBoxInstallCmd(port int, uuid, sid, sni string) string {
	return fmt.Sprintf(`
set -e
docker pull ghcr.io/sagernet/sing-box:latest
KEYS=$(docker run --rm ghcr.io/sagernet/sing-box:latest generate reality-keypair)
PRIV_KEY=$(echo "$KEYS" | awk '/PrivateKey/{print $2}')
PUB_KEY=$(echo "$KEYS" | awk '/PublicKey/{print $2}')
if [ -z "$PRIV_KEY" ] || [ -z "$PUB_KEY" ]; then echo "failed to generate reality keypair" >&2; exit 1; fi
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
      "users": [{"uuid": "%s", "flow": "xtls-rprx-vision"}],
      "tls": {
        "enabled": true,
        "server_name": "%s",
        "reality": {
          "enabled": true,
          "handshake": {"server": "%s", "server_port": 443},
          "private_key": "${PRIV_KEY}",
          "short_id": ["%s"]
        }
      }
    }
  ],
  "outbounds": [{"type": "direct", "tag": "direct"}]
}
EOF
docker run --rm -v /opt/sb-mgr:/etc/sing-box ghcr.io/sagernet/sing-box:latest check -c /etc/sing-box/config.json
iptables -I INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || true
(command -v ufw >/dev/null 2>&1 && ufw allow %d/tcp >/dev/null 2>&1) || true
docker rm -f sb-mgr 2>/dev/null || true
docker run -d --name sb-mgr --restart always --net=host -v /opt/sb-mgr:/etc/sing-box ghcr.io/sagernet/sing-box:latest run -c /etc/sing-box/config.json
echo "UUID=%s"
echo "PUBLIC_KEY=${PUB_KEY}"
`, port, uuid, sni, sni, sid, port, port, uuid)
}

func buildNativeSingBoxInstallCmd(port int, uuid, sid, sni, arch string) (string, error) {
	dlArch := "amd64"
	if arch == "aarch64" || arch == "arm64" {
		dlArch = "arm64"
	}
	dlURL := fmt.Sprintf("https://github.com/SagerNet/sing-box/releases/download/v%s/sing-box-%s-linux-%s.tar.gz",
		defaultSingBoxVersion, defaultSingBoxVersion, dlArch)

	cfgTemplate := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"inbounds": []map[string]any{{
			"type":        "vless",
			"tag":         "vless-reality",
			"listen":      "0.0.0.0",
			"listen_port": port,
			"users":       []map[string]any{{"uuid": uuid, "flow": "xtls-rprx-vision"}},
			"tls": map[string]any{
				"enabled":     true,
				"server_name": sni,
				"reality": map[string]any{
					"enabled":     true,
					"handshake":   map[string]any{"server": sni, "server_port": 443},
					"private_key": "__PRIV_KEY__",
					"short_id":    []string{sid},
				},
			},
		}},
		"outbounds": []map[string]any{{"type": "direct", "tag": "direct"}},
	}
	rawCfg, err := json.MarshalIndent(cfgTemplate, "", "  ")
	if err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString(rawCfg)

	script := fmt.Sprintf(`
set -eu
mkdir -p /etc/sing-box /var/log
if [ ! -x /usr/local/bin/sing-box ]; then
  rm -rf /tmp/sb-dl && mkdir -p /tmp/sb-dl
  if command -v curl >/dev/null 2>&1; then curl -fsSL -o /tmp/sb-dl/sb.tar.gz '%s'; else wget -q -O /tmp/sb-dl/sb.tar.gz '%s'; fi
  tar -xzf /tmp/sb-dl/sb.tar.gz -C /tmp/sb-dl
  SB_BIN="$(find /tmp/sb-dl -type f -name sing-box | head -n1)"
  [ -n "$SB_BIN" ] || exit 1
  cp "$SB_BIN" /usr/local/bin/sing-box && chmod +x /usr/local/bin/sing-box
  rm -rf /tmp/sb-dl
fi
KEYS="$(/usr/local/bin/sing-box generate reality-keypair)"
PRIV_KEY="$(echo "$KEYS" | awk '/PrivateKey/{print $2}')"
PUB_KEY="$(echo "$KEYS" | awk '/PublicKey/{print $2}')"
[ -n "$PRIV_KEY" ] && [ -n "$PUB_KEY" ] || exit 1
printf '%%s' '%s' | base64 -d > /etc/sing-box/config.json.new
sed -i "s|__PRIV_KEY__|$PRIV_KEY|g" /etc/sing-box/config.json.new
/usr/local/bin/sing-box check -c /etc/sing-box/config.json.new
mv /etc/sing-box/config.json.new /etc/sing-box/config.json
iptables -I INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || true
(command -v ufw >/dev/null 2>&1 && ufw allow %d/tcp >/dev/null 2>&1) || true
if command -v rc-service >/dev/null 2>&1; then
  cat > /etc/init.d/sing-box <<'INITEOF'
#!/sbin/openrc-run
name="sing-box"
command="/usr/local/bin/sing-box"
command_args="run -c /etc/sing-box/config.json"
command_background="yes"
pidfile="/run/sing-box.pid"
depend() {
  need net
  after firewall
}
INITEOF
  chmod +x /etc/init.d/sing-box
  rc-update add sing-box default >/dev/null 2>&1 || true
  rc-service sing-box restart >/dev/null 2>&1 || rc-service sing-box start
elif command -v systemctl >/dev/null 2>&1; then
  cat > /etc/systemd/system/sing-box.service <<'UNITEOF'
[Unit]
Description=sing-box service
After=network.target

[Service]
ExecStart=/usr/local/bin/sing-box run -c /etc/sing-box/config.json
Restart=on-failure

[Install]
WantedBy=multi-user.target
UNITEOF
  systemctl daemon-reload
  systemctl enable --now sing-box
  systemctl restart sing-box
fi
echo "UUID=%s"
echo "PUBLIC_KEY=${PUB_KEY}"
`, dlURL, dlURL, b64, port, port, uuid)
	return script, nil
}

func buildVLESSLink(uuid, ip string, port int, sni, pbk, sid, remark string) string {
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("security", "reality")
	q.Set("sni", sni)
	// 使用 ios 指纹避免 sing-box 1.14.6+ Chrome X25519MLKEM768 超大 ClientHello 在 NAT/旧核心上触发 Reality 握手失败
	q.Set("fp", "ios")
	q.Set("pbk", pbk)
	q.Set("sid", sid)
	q.Set("type", "tcp")
	q.Set("flow", "xtls-rprx-vision")
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuid, ip, port, q.Encode(), url.PathEscape(remark))
}

func normalizeVLESSFragment(rawURL, nodeName string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Fragment = nodeName
	return u.String()
}

var kvLineRegex = regexp.MustCompile(`(?m)^([A-Z_]+)=([^\s\r\n]+)$`)

func extractOutputKV(out, key string) string {
	for _, m := range kvLineRegex.FindAllStringSubmatch(strings.ReplaceAll(out, "\r", ""), -1) {
		if len(m) == 3 && m[1] == key {
			return strings.TrimSpace(m[2])
		}
	}
	return ""
}

func smokeResultFromProbe(res *model.NodeProbeResult) *SmokeTestResult {
	st := &SmokeTestResult{
		Success:  res != nil && res.Online,
		TestedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if res == nil {
		st.Error = "probe returned nil"
		return st
	}
	st.LatencyMs = int64(res.LatencyMs)
	st.ExitIP = res.ExitIP
	if !res.Online {
		st.Error = "handshake failed or offline"
		if res.Diagnostic != nil && res.Diagnostic.StatusText != "" {
			st.Error = res.Diagnostic.StatusText
		}
	}
	return st
}

func (m *Manager) onboardDeployedTransitNode(ctx context.Context, nodeName string, resp *DeployVLESSResponse) {
	if resp == nil || resp.VLESSURL == "" {
		return
	}
	if m.healer != nil && m.configPath != "" {
		if err := m.healer.UpsertLocalConfigNode(nodeName, resp.VLESSURL); err == nil {
			resp.ConfigSynced = true
		} else {
			logger.Warnf("[VLESS-DEPLOY] config.yaml 写入失败: %v", err)
		}
	}

	m.hubMu.RLock()
	onAdded := m.onNodeAdded
	m.hubMu.RUnlock()
	if onAdded != nil {
		onAdded(config.MonitoredNode{
			Name:   nodeName,
			URL:    resp.VLESSURL,
			IsSelf: true,
		})
		resp.MonitorSynced = true
	}

	if m.healer != nil && m.substoreClient != nil {
		if err := m.healer.AddNodeToSubStore(ctx, resp.VLESSURL); err == nil {
			resp.SubStoreSynced = true
		} else {
			logger.Warnf("[VLESS-DEPLOY] Sub-Store 写入失败: %v", err)
		}
	}
}
