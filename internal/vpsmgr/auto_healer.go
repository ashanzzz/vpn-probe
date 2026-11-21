package vpsmgr

import (
	"context"
	"fmt"
	"math/rand"
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
		hostParts := strings.Split(u.Host, ":")
		if len(hostParts) == 2 {
			result.ServerIP = hostParts[0]
			result.ServerPort, _ = strconv.Atoi(hostParts[1])
		} else {
			result.ServerIP = u.Host
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

	u, err := url.Parse(targetURL)
	if err != nil {
		resp.Message = fmt.Sprintf("invalid node url: %v", err)
		return resp, err
	}

	hostParts := strings.Split(u.Host, ":")
	if len(hostParts) != 2 {
		resp.Message = "node url does not contain port"
		return resp, fmt.Errorf("invalid host format: %s", u.Host)
	}
	oldPort, _ := strconv.Atoi(hostParts[1])
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

	// 1. 处理 sing-box 服务更新
	if softInfo.SoftwareType == "sing-box" {
		containerName := softInfo.ContainerName
		if containerName == "" {
			containerName = "sb-lax"
		}
		cfgHostPath := fmt.Sprintf("/opt/%s/config.json", containerName)

		// A. 自动备份
		backupCmd := fmt.Sprintf("cp %s %s.bak", cfgHostPath, cfgHostPath)
		if _, err := h.detector.RunSSH(inst, backupCmd); err != nil {
			resp.Message = fmt.Sprintf("failed to backup config: %v", err)
			return resp, err
		}

		// B. 替换端口
		replaceCmd := fmt.Sprintf("sed -i 's/\"listen_port\": %d/\"listen_port\": %d/g' %s", oldPort, newPort, cfgHostPath)
		if _, err := h.detector.RunSSH(inst, replaceCmd); err != nil {
			// 回滚
			h.detector.RunSSH(inst, fmt.Sprintf("cp %s.bak %s", cfgHostPath, cfgHostPath))
			resp.Message = fmt.Sprintf("failed to modify port in config: %v", err)
			return resp, err
		}

		// C. 强校验：sing-box check 预检
		checkCmd := fmt.Sprintf("docker exec %s sing-box check -c /etc/sing-box/config.json", containerName)
		checkOut, err := h.detector.RunSSH(inst, checkCmd)
		if err != nil {
			// 预检失败，原子回滚！
			logger.Errorf("[AUTO-HEAL] sing-box check failed on %s: %s, rolling back!", inst.IP, checkOut)
			h.detector.RunSSH(inst, fmt.Sprintf("cp %s.bak %s", cfgHostPath, cfgHostPath))
			resp.Message = fmt.Sprintf("sing-box check failed: %v, rolled back", err)
			return resp, fmt.Errorf("%s", resp.Message)
		}

		// D. 防火墙放行新端口并重启
		fwCmd := fmt.Sprintf("iptables -I INPUT -p tcp --dport %d -j ACCEPT; iptables -I INPUT -p udp --dport %d -j ACCEPT; docker restart %s", newPort, newPort, containerName)
		if _, err := h.detector.RunSSH(inst, fwCmd); err != nil {
			resp.Message = fmt.Sprintf("failed to restart sing-box container: %v", err)
			return resp, err
		}
	} else if softInfo.SoftwareType == "3x-ui" {
		// 3x-ui 数据库热更新
		dbCmd := fmt.Sprintf("docker exec 3x-ui sqlite3 /etc/x-ui/x-ui.db \"UPDATE inbounds SET port=%d WHERE port=%d;\" && docker restart 3x-ui", newPort, oldPort)
		if _, err := h.detector.RunSSH(inst, dbCmd); err != nil {
			resp.Message = fmt.Sprintf("failed to update 3x-ui inbound port: %v", err)
			return resp, err
		}
	}

	// 2. 构造新节点 URL
	u.Host = fmt.Sprintf("%s:%d", hostParts[0], newPort)
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
