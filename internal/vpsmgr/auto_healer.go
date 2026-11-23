package vpsmgr

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
	"gopkg.in/yaml.v3"
)

type AutoHealer struct {
	detector       *Detector
	substoreClient *substore.Client
	configPath     string
}

func NewAutoHealer(d *Detector, sc *substore.Client, configPath string) *AutoHealer {
	return &AutoHealer{
		detector:       d,
		substoreClient: sc,
		configPath:     configPath,
	}
}

// GenerateRandomPort 在 [20000, 60000] 之间生成安全随机高位端口
func GenerateRandomPort() int {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return 20000 + r.Intn(40001)
}

// DiagnoseNode 对单个节点的探测结果执行深度四分类诊断
func (h *AutoHealer) DiagnoseNode(res *model.NodeProbeResult) *BlockedAnalysisResult {
	result := &BlockedAnalysisResult{
		NodeName:    res.Name,
		DomesticTCP: false,
		AbroadTCP:   false,
	}

	u, err := url.Parse(res.OriginalURL)
	if err == nil {
		result.Protocol = u.Scheme
		result.ServerIP = u.Hostname()
		if p := u.Port(); p != "" {
			result.ServerPort, _ = strconv.Atoi(p)
		}
	}

	// 提取诊断特征
	diag := res.Diagnostic
	if diag != nil {
		result.DomesticTCP = diag.DomesticReachable
		result.AbroadTCP = diag.AbroadReachable
		result.DomesticPing = diag.DomesticPing
		result.AbroadRTTMs = int64(diag.AbroadRTTMs)
		result.AbroadSource = diag.AbroadSource
		if diag.TargetIP != "" {
			result.ServerIP = diag.TargetIP
		}
		if diag.TargetPort > 0 {
			result.ServerPort = diag.TargetPort
		}
	}

	// 判定分类
	if res.Online {
		result.FaultType = "HEALTHY"
		result.DiagnosticMessage = "节点双向连通且代理握手成功，状态健康。"
		result.RecommendedAction = "无需处理"
		return result
	}

	// 住宅家宽节点专属故障模型与安全防护（家宽物理上均为中转+家庭隧道链式代理，严禁触发换端口自愈）
	isResi := res.NodeType == "residential" || strings.Contains(res.Name, "家宽")
	if isResi {
		result.CanAutoHealPort = false
		result.CanRelayRescue = false
		if result.AbroadTCP {
			result.FaultType = "RESIDENTIAL_DIRECT_BLOCKED"
			result.DiagnosticMessage = fmt.Sprintf("【仅境外通】住宅家宽目标 %s:%d 境外链路畅通，国内直连受阻。严禁更换端口，需通过自建专线链式中继！", result.ServerIP, result.ServerPort)
			result.RecommendedAction = "通过自建专线中转接入（链式代理）"
			return result
		} else if result.DomesticPing {
			result.FaultType = "RESIDENTIAL_TUNNEL_OFFLINE"
			result.DiagnosticMessage = fmt.Sprintf("【家庭端离线】家宽中转机 %s 在线(Ping通)，但端口 %d 全球无响应。家庭设备离线或隧道未就绪，严禁换端口！", result.ServerIP, result.ServerPort)
			result.RecommendedAction = "检查家庭端设备或穿透隧道"
			return result
		} else {
			result.FaultType = "TRANSIT_RELAY_DOWN"
			result.DiagnosticMessage = fmt.Sprintf("【中转离线】家宽中转服务器 %s 全球不可达，中转机宕机或网络中断。", result.ServerIP)
			result.RecommendedAction = "检查中转服务器运行状态"
			return result
		}
	}

	if result.DomesticTCP && result.AbroadTCP {
		result.FaultType = "PROTOCOL_MISMATCH"
		result.DiagnosticMessage = "【配置/密钥错误】境内与境外TCP均可握手，端口未被墙！代理协议握手失败（Reality公钥/短ID不匹配或flow未配置）。"
		result.RecommendedAction = "严禁换端口！自动通过SSH读取VPS服务端配置校准客户端链接。"
		return result
	}

	if !result.DomesticTCP && result.AbroadTCP {
		if result.DomesticPing {
			result.FaultType = "PORT_BLOCKED"
			result.DiagnosticMessage = "【端口被墙】境内TCP超时，境外对端TCP存活，境内ICMP Ping存活。确认仅该服务端口被GFW阻断。"
			result.RecommendedAction = "全自动更换端口自愈：在20000-60000范围分配新端口并热更新。"
			result.CanAutoHealPort = true
			return result
		} else {
			result.FaultType = "SERVER_BLOCKED"
			result.DiagnosticMessage = "【整机/IP被墙】境内TCP超时且ICMP Ping 100%丢包，境外对端可正常连通。整个VPS IP被阻断。"
			result.RecommendedAction = "切勿换端口！自动借道海外健康节点中继扶持或启用Cloudflare 443 CDN反代救活。"
			result.CanRelayRescue = true
			return result
		}
	}

	// 境外国内均不通
	result.FaultType = "MACHINE_DOWN"
	result.DiagnosticMessage = "【机器离线/服务停止】全球对端与国内均无法握手该端口，服务进程退出或机器停机。"
	result.RecommendedAction = "通过SSH重启容器服务或检查VPS运行状态。"
	return result
}

// ExecutePortHeal 执行安全换端口自愈操作（备份、修改、sing-box check预检、回滚、iptables放行、更新SubStore与本地配置）
func (h *AutoHealer) ExecutePortHeal(ctx context.Context, inst *VPSInstance, nodeName string, targetURL string, targetPort int) (*PortHealResponse, error) {
	resp := &PortHealResponse{
		Success:  false,
		NodeName: nodeName,
	}

	if strings.Contains(nodeName, "家宽") || (inst != nil && inst.ProviderID == "lazycat") {
		resp.Message = "住宅家宽节点严禁执行换端口操作：家宽物理上为链式代理，请核查中转机或家庭端连通性"
		return resp, fmt.Errorf("port heal is forbidden on residential nodes")
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		resp.Message = fmt.Sprintf("invalid node url: %v", err)
		return resp, err
	}

	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		resp.Message = "node url does not contain port"
		return resp, fmt.Errorf("invalid host format: %s", u.Host)
	}
	oldPort, _ := strconv.Atoi(portStr)
	resp.OldPort = oldPort

	// 选定新端口
	newPort := targetPort
	if newPort <= 0 || newPort == oldPort {
		for i := 0; i < 5; i++ {
			candidate := GenerateRandomPort()
			// 探查 VPS 上是否被占用
			checkCmd := fmt.Sprintf("ss -tulpn | grep -q ':%d ' && echo 'busy' || echo 'free'", candidate)
			out, err := h.detector.RunSSH(inst, checkCmd)
			if err == nil && strings.TrimSpace(out) == "free" {
				newPort = candidate
				break
			}
		}
		if newPort <= 0 || newPort == oldPort {
			newPort = GenerateRandomPort()
		}
	}
	resp.NewPort = newPort

	logger.Infof("[AUTO-HEAL] Starting port heal for node '%s' on VPS %s: %d -> %d", nodeName, inst.IP, oldPort, newPort)

	// 探查当前运行的代理容器/服务
	softInfo := h.detector.InspectVPS(ctx, inst)
	if softInfo.Status == "unreachable" {
		resp.Message = fmt.Sprintf("VPS %s is unreachable via SSH", inst.IP)
		return resp, fmt.Errorf("%s", resp.Message)
	}

	// 1. 根据软件形态与部署模式解析文件路径并安全执行换端口
	_, activeFile, _, isSqlite, restartCmd, err := resolveBackupPaths(softInfo)
	if err != nil {
		resp.Message = fmt.Sprintf("resolve proxy paths: %v", err)
		return resp, err
	}

	// A. 自动配置快照备份
	backupCmd := fmt.Sprintf("cp %s %s.bak", activeFile, activeFile)
	if _, err := h.detector.RunSSH(inst, backupCmd); err != nil {
		resp.Message = fmt.Sprintf("failed to backup config: %v", err)
		return resp, err
	}

	// B. 端口替换与语法预检
	if isSqlite {
		// 3x-ui 数据库热更新 (优先使用容器或宿主机 sqlite3)
		var dbCmd string
		if softInfo.DeployMode == "docker" {
			cName := softInfo.ContainerName
			if cName == "" {
				cName = "3x-ui"
			}
			dbCmd = fmt.Sprintf("docker exec %s sqlite3 /etc/x-ui/x-ui.db \"UPDATE inbounds SET port=%d WHERE port=%d;\" 2>/dev/null || sqlite3 %s \"UPDATE inbounds SET port=%d WHERE port=%d;\"", cName, newPort, oldPort, activeFile, newPort, oldPort)
		} else {
			dbCmd = fmt.Sprintf("sqlite3 %s \"UPDATE inbounds SET port=%d WHERE port=%d;\"", activeFile, newPort, oldPort)
		}
		if _, err := h.detector.RunSSH(inst, dbCmd); err != nil {
			// 回滚
			h.detector.RunSSH(inst, fmt.Sprintf("cp %s.bak %s", activeFile, activeFile))
			resp.Message = fmt.Sprintf("failed to update 3x-ui inbound port: %v", err)
			return resp, err
		}
	} else {
		// JSON 文本配置 (sing-box / xray)
		// sing-box 使用 "listen_port": <int>，xray 入站使用 "port": <int>
		var replaceCmd string
		if softInfo.SoftwareType == "xray" {
			replaceCmd = fmt.Sprintf("sed -i -E 's/\"port\":\\s*%d/\"port\": %d/g' %s", oldPort, newPort, activeFile)
		} else {
			replaceCmd = fmt.Sprintf("sed -i -E 's/\"(listen_port|port)\":\\s*%d/\"\\1\": %d/g' %s", oldPort, newPort, activeFile)
		}
		if _, err := h.detector.RunSSH(inst, replaceCmd); err != nil {
			h.detector.RunSSH(inst, fmt.Sprintf("cp %s.bak %s", activeFile, activeFile))
			resp.Message = fmt.Sprintf("failed to modify port in config: %v", err)
			return resp, err
		}

		// 强校验预检 (Preflight Check，真实检验语法状态，失败触发原子回滚)
		var checkCmd string
		if softInfo.DeployMode == "docker" {
			cName := softInfo.ContainerName
			if cName == "" {
				cName = "sb-mgr"
			}
			if softInfo.SoftwareType == "xray" {
				checkCmd = fmt.Sprintf("docker exec %s xray -test -config /etc/xray/config.json", cName)
			} else {
				checkCmd = fmt.Sprintf("docker exec %s sing-box check -c /etc/sing-box/config.json", cName)
			}
		} else {
			if softInfo.SoftwareType == "xray" {
				checkCmd = fmt.Sprintf("xray -test -config %s", activeFile)
			} else {
				checkCmd = fmt.Sprintf("sing-box check -c %s", activeFile)
			}
		}

		checkOut, err := h.detector.RunSSH(inst, checkCmd)
		if err != nil {
			logger.Errorf("[AUTO-HEAL] Preflight check failed on %s: %s, rolling back!", inst.IP, checkOut)
			h.detector.RunSSH(inst, fmt.Sprintf("cp %s.bak %s", activeFile, activeFile))
			resp.Message = fmt.Sprintf("preflight check failed: %v, rolled back", err)
			return resp, fmt.Errorf("%s", resp.Message)
		}
	}

	// C. 防火墙放行新端口并安全平滑重启服务
	fwAndRestartCmd := fmt.Sprintf("iptables -I INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null; iptables -I INPUT -p udp --dport %d -j ACCEPT 2>/dev/null; %s", newPort, newPort, restartCmd)
	if out, err := h.detector.RunSSH(inst, fwAndRestartCmd); err != nil {
		resp.Message = fmt.Sprintf("failed to restart service: %v (output: %s)", err, out)
		return resp, err
	}

	// 2. 构造新节点 URL
	u.Host = net.JoinHostPort(host, strconv.Itoa(newPort))
	newURL := u.String()
	resp.NewURL = newURL

	// 3. 同步回写 SubStore 订阅
	if h.substoreClient != nil {
		syncErr := h.updateSubStoreSubscription(ctx, oldPort, newPort, targetURL, newURL)
		if syncErr == nil {
			resp.SubStoreSync = true
		} else {
			logger.Warnf("[AUTO-HEAL] SubStore sync warning: %v", syncErr)
		}
	}

	// 4. 同步更新本地 config.yaml
	if h.configPath != "" {
		if err := h.updateLocalConfigYAML(targetURL, newURL); err == nil {
			resp.ConfigSync = true
		} else {
			logger.Warnf("[AUTO-HEAL] Local config.yaml update warning: %v", err)
		}
	}

	resp.Success = true
	resp.Message = fmt.Sprintf("Successfully healed port %d -> %d on %s", oldPort, newPort, inst.IP)
	logger.Infof("[AUTO-HEAL] %s", resp.Message)
	return resp, nil
}

// updateSubStoreSubscription 将换端口后的新节点同步至 SubStore 主订阅
func (h *AutoHealer) updateSubStoreSubscription(ctx context.Context, oldPort, newPort int, oldURL, newURL string) error {
	content, err := h.substoreClient.GetSubscriptionContent(ctx, "ashan's vps")
	if err != nil {
		return fmt.Errorf("get sub content: %w", err)
	}

	lines := strings.Split(content, "\n")
	var updatedLinks []string
	var found bool
	for _, line := range lines {
		lineTrim := strings.TrimSpace(line)
		if lineTrim == "" {
			continue
		}
		if lineTrim == oldURL || strings.Contains(lineTrim, fmt.Sprintf(":%d", oldPort)) {
			updatedLinks = append(updatedLinks, newURL)
			found = true
		} else {
			updatedLinks = append(updatedLinks, lineTrim)
		}
	}

	if !found {
		updatedLinks = append(updatedLinks, newURL)
	}

	return h.substoreClient.UpdateSubscriptionNodes(ctx, "ashan's vps", updatedLinks)
}

// updateLocalConfigYAML 更新本地 config.yaml 中的节点链接
func (h *AutoHealer) updateLocalConfigYAML(oldURL, newURL string) error {
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("unmarshal yaml: %w", err)
	}

	// 文本级安全替换，保留所有注释与缩进
	content := string(data)
	if strings.Contains(content, oldURL) {
		newContent := strings.Replace(content, oldURL, newURL, 1)
		return os.WriteFile(h.configPath, []byte(newContent), 0644)
	}

	return nil
}
