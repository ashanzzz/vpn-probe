package vpsmgr

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
)

type AutoHealer struct {
	detector       *Detector
	substoreClient *substore.Client
	configPath     string
	selfSubName    string
	onURLUpdated   func(oldURL, newURL string)
}

func NewAutoHealer(d *Detector, sc *substore.Client, configPath string) *AutoHealer {
	return &AutoHealer{
		detector:       d,
		substoreClient: sc,
		configPath:     configPath,
	}
}

// SetOnURLUpdated 注册节点 URL 热更新回调（同步通知 Scheduler 更新内存状态）。
func (h *AutoHealer) SetOnURLUpdated(fn func(oldURL, newURL string)) {
	h.onURLUpdated = fn
}

// SetSelfSubName 配置主自建订阅名称（避免硬编码遗留名称）
func (h *AutoHealer) SetSelfSubName(name string) {
	if strings.TrimSpace(name) != "" {
		h.selfSubName = strings.TrimSpace(name)
	}
}

func (h *AutoHealer) targetSelfSubs() []string {
	seen := map[string]bool{"ashan-transit": true}
	subs := []string{"ashan-transit"}
	if h.selfSubName != "" {
		if !seen[h.selfSubName] {
			subs = append(subs, h.selfSubName)
		}
	} else {
		// 未显式配置 self_sub_name 时兼容存量 "ashan's vps"（仅更新已存在项，绝不重建已删除订阅）
		subs = append(subs, "ashan's vps")
	}
	return subs
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
	isResi := res.NodeType == "residential" || model.IsResidentialNode(res.Name, res.OriginalURL, res.Protocol)
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

	if res.Diagnostic != nil && res.Diagnostic.FaultType == model.FaultNetworkCongestion {
		result.FaultType = "NETWORK_CONGESTION"
		result.DiagnosticMessage = res.Diagnostic.TroubleshootTip
		if result.DiagnosticMessage == "" {
			result.DiagnosticMessage = "【网络拥塞超时】国内外TCP连通但应用层请求超时，通常为国际网络链路拥塞抖动，机器与端口均正常。"
		}
		result.RecommendedAction = "保持观察等待链路抖动恢复（无需换端口或修改配置）"
		result.CanAutoHealPort = false
		result.CanRelayRescue = false
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

	if model.IsResidentialNode(nodeName, targetURL, "") || IsLazyCatResidential(inst) {
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

	// NAT 固定出口（懒猫云）：只重发布公网映射，宿机 sing-box 配置与进程零改动
	if IsLazyCatTransit(inst) && inst.IsNAT {
		return h.healNATPublicPort(ctx, inst, nodeName, targetURL, oldPort, resp)
	}

	// 选定新端口
	newPort := targetPort
	if newPort <= 0 || newPort == oldPort {
		if inst != nil && inst.IsNAT && len(inst.NATPorts) > 0 {
			// NAT VPS: 严格受限在分配的 nat_ports 端口池中挑选
			for _, candidate := range inst.NATPorts {
				if candidate == oldPort {
					continue
				}
				checkCmd := fmt.Sprintf("ss -tulpn | grep -q ':%d ' && echo 'busy' || echo 'free'", candidate)
				out, err := h.detector.RunSSH(inst, checkCmd)
				if err == nil && strings.TrimSpace(out) == "free" {
					newPort = candidate
					break
				}
			}
			if newPort <= 0 || newPort == oldPort {
				// 如果都检测不到空闲，兜底选第一个非旧端口
				for _, candidate := range inst.NATPorts {
					if candidate != oldPort {
						newPort = candidate
						break
					}
				}
			}
			if newPort <= 0 || newPort == oldPort {
				resp.Message = fmt.Sprintf("NAT VPS %s has no available alternative ports in pool: %v", inst.IP, inst.NATPorts)
				return resp, fmt.Errorf("%s", resp.Message)
			}
			logger.Infof("[AUTO-HEAL] NAT VPS %s selected new port %d from nat_ports pool", inst.IP, newPort)
		} else {
			// 普通独立 IP VPS: 在安全随机范围 [20000, 60000] 生成
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

	// A. 自动配置快照备份 (保留 .bak 与时间戳滚动备份)
	backupCmd := fmt.Sprintf("cp %s %s.bak && cp %s %s.bak.$(date +%%Y%%m%%d%%H%%M%%S) 2>/dev/null || cp %s %s.bak", activeFile, activeFile, activeFile, activeFile, activeFile, activeFile)
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
		// JSON 文本配置 (sing-box / xray)：优先用 python3 精准定位 inbounds 节点更新端口，防误改出站/DNS 同号端口；缺 python3 时安全回退 scoped sed
		pyScript := fmt.Sprintf(`python3 -c 'import json; p="%s"; d=json.load(open(p)); [inb.__setitem__(k,%d) for inb in d.get("inbounds",[]) for k in ("listen_port","port") if inb.get(k)==%d]; json.dump(d,open(p,"w"),indent=2)'`, activeFile, newPort, oldPort)
		var sedFallback string
		if softInfo.SoftwareType == "xray" {
			sedFallback = fmt.Sprintf("sed -i -E 's/\"port\":\\s*%d/\"port\": %d/g' %s", oldPort, newPort, activeFile)
		} else {
			sedFallback = fmt.Sprintf("sed -i -E 's/\"(listen_port|port)\":\\s*%d/\"\\1\": %d/g' %s", oldPort, newPort, activeFile)
		}
		replaceCmd := fmt.Sprintf("if command -v python3 >/dev/null 2>&1; then %s; else %s; fi", pyScript, sedFallback)
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

	// C. 放行新端口、清理旧端口并尽量持久化防火墙规则后平滑重启服务
	fwAndRestartCmd := fmt.Sprintf("iptables -D INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || true; iptables -D INPUT -p udp --dport %d -j ACCEPT 2>/dev/null || true; iptables -I INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || true; iptables -I INPUT -p udp --dport %d -j ACCEPT 2>/dev/null || true; (command -v ufw >/dev/null 2>&1 && ufw allow %d >/dev/null 2>&1 && ufw delete allow %d >/dev/null 2>&1) || true; (command -v netfilter-persistent >/dev/null 2>&1 && netfilter-persistent save >/dev/null 2>&1) || true; %s", oldPort, oldPort, newPort, newPort, newPort, oldPort, restartCmd)
	if out, err := h.detector.RunSSH(inst, fwAndRestartCmd); err != nil {
		resp.Message = fmt.Sprintf("failed to restart service: %v (output: %s)", err, out)
		return resp, err
	}

	// 2. 构造新节点 URL
	u.Host = net.JoinHostPort(host, strconv.Itoa(newPort))
	newURL := u.String()
	resp.NewURL = newURL

	// 3. 同步回写 SubStore、本地 config.yaml 与内存调度器
	h.syncHealedNodeURL(ctx, oldPort, newPort, targetURL, newURL, resp)

	resp.Success = true
	resp.Message = fmt.Sprintf("Successfully healed port %d -> %d on %s", oldPort, newPort, inst.IP)
	logger.Infof("[AUTO-HEAL] %s", resp.Message)
	return resp, nil
}

func (h *AutoHealer) syncHealedNodeURL(ctx context.Context, oldPort, newPort int, oldURL, newURL string, resp *PortHealResponse) {
	if h.substoreClient != nil {
		if syncErr := h.updateSubStoreSubscription(ctx, oldPort, newPort, oldURL, newURL); syncErr == nil {
			resp.SubStoreSync = true
		} else {
			logger.Warnf("[AUTO-HEAL] SubStore sync warning: %v", syncErr)
		}
	}
	if h.configPath != "" {
		if err := h.updateLocalConfigYAML(oldURL, newURL); err == nil {
			resp.ConfigSync = true
		} else {
			logger.Warnf("[AUTO-HEAL] Local config.yaml update warning: %v", err)
		}
	}
	if h.onURLUpdated != nil {
		h.onURLUpdated(oldURL, newURL)
	}
}

// updateSubStoreSubscription 将换端口后的新节点同步至 SubStore 主订阅
func (h *AutoHealer) updateSubStoreSubscription(ctx context.Context, oldPort, newPort int, oldURL, newURL string) error {
	if h.substoreClient == nil {
		return nil
	}
	targetSubs := h.targetSelfSubs()
	var updatedCount int
	var lastErr error

	var oldHost string
	if u, err := url.Parse(oldURL); err == nil {
		oldHost = u.Hostname()
	}

	for _, subName := range targetSubs {
		content, err := h.substoreClient.GetSubscriptionContent(ctx, subName)
		if err != nil {
			lastErr = err
			continue
		}

		lines := strings.Split(content, "\n")
		var updatedLinks []string
		var found bool
		for _, line := range lines {
			lineTrim := strings.TrimSpace(line)
			if lineTrim == "" {
				continue
			}
			isMatch := false
			if lineTrim == oldURL {
				isMatch = true
			} else if oldHost != "" {
				if u, err := url.Parse(lineTrim); err == nil && u.Host != "" {
					if u.Hostname() == oldHost && u.Port() == strconv.Itoa(oldPort) {
						isMatch = true
					}
				} else {
					targetSubstrs := []string{
						fmt.Sprintf("@%s:%d", oldHost, oldPort),
						fmt.Sprintf("//%s:%d", oldHost, oldPort),
					}
					for _, targetSubstr := range targetSubstrs {
						if idx := strings.Index(lineTrim, targetSubstr); idx != -1 {
							rest := lineTrim[idx+len(targetSubstr):]
							if len(rest) == 0 || rest[0] == '/' || rest[0] == '?' || rest[0] == '#' {
								isMatch = true
								break
							}
						}
					}
				}
			}
			if isMatch {
				updatedLinks = append(updatedLinks, newURL)
				found = true
			} else {
				updatedLinks = append(updatedLinks, lineTrim)
			}
		}

		if !found {
			updatedLinks = append(updatedLinks, newURL)
		}

		if err := h.substoreClient.UpdateSubscriptionNodes(ctx, subName, updatedLinks); err == nil {
			updatedCount++
		} else {
			lastErr = err
		}
	}

	if updatedCount == 0 && lastErr != nil {
		return fmt.Errorf("get/update sub content: %w", lastErr)
	}
	return nil
}

// AddNodeToSubStore 将新安装节点追加同步至 SubStore 主订阅
func (h *AutoHealer) AddNodeToSubStore(ctx context.Context, newURL string) error {
	if h.substoreClient == nil || newURL == "" {
		return nil
	}
	targetSubs := h.targetSelfSubs()
	var successCount int
	var lastErr error
	for _, subName := range targetSubs {
		content, err := h.substoreClient.GetSubscriptionContent(ctx, subName)
		if err != nil {
			// 若获取失败，仅主订阅 ashan-transit 尝试创建，避免幽灵历史订阅死灰复燃
			if subName == "ashan-transit" {
				if updateErr := h.substoreClient.UpdateSubscriptionNodes(ctx, subName, []string{newURL}); updateErr == nil {
					successCount++
				} else {
					lastErr = updateErr
				}
			}
			continue
		}

		lines := strings.Split(content, "\n")
		var updatedLinks []string
		var found bool
		for _, line := range lines {
			lineTrim := strings.TrimSpace(line)
			if lineTrim == "" {
				continue
			}
			if lineTrim == newURL {
				found = true
			}
			updatedLinks = append(updatedLinks, lineTrim)
		}
		if !found {
			updatedLinks = append(updatedLinks, newURL)
		}

		if updateErr := h.substoreClient.UpdateSubscriptionNodes(ctx, subName, updatedLinks); updateErr == nil {
			successCount++
		} else {
			lastErr = updateErr
		}
	}
	if successCount == 0 && lastErr != nil {
		return fmt.Errorf("add node to substore: %w", lastErr)
	}
	return nil
}

// RemoveNodeFromSubStore 从 SubStore 订阅中下架属于指定 IP 的所有节点
func (h *AutoHealer) RemoveNodeFromSubStore(ctx context.Context, targetIP string) (int, error) {
	if h.substoreClient == nil || targetIP == "" {
		return 0, nil
	}
	targetSubs := h.targetSelfSubs()
	var totalRemoved int
	for _, subName := range targetSubs {
		content, err := h.substoreClient.GetSubscriptionContent(ctx, subName)
		if err != nil {
			continue
		}

		lines := strings.Split(content, "\n")
		var remainingLinks []string
		var removedCount int

		for _, line := range lines {
			lineTrim := strings.TrimSpace(line)
			if lineTrim == "" {
				continue
			}
			isTarget := false
			if u, err := url.Parse(lineTrim); err == nil && u.Host != "" {
				if u.Hostname() == targetIP {
					isTarget = true
				}
			} else if strings.Contains(lineTrim, "@"+targetIP+":") || strings.Contains(lineTrim, "//"+targetIP+":") || strings.Contains(lineTrim, "@"+targetIP+"?") {
				isTarget = true
			}
			if isTarget {
				removedCount++
			} else {
				remainingLinks = append(remainingLinks, lineTrim)
			}
		}

		if removedCount > 0 {
			if err := h.substoreClient.UpdateSubscriptionNodes(ctx, subName, remainingLinks); err == nil {
				totalRemoved += removedCount
			}
		}
	}

	return totalRemoved, nil
}

// updateLocalConfigYAML 更新本地 config.yaml 中的节点链接
func (h *AutoHealer) updateLocalConfigYAML(oldURL, newURL string) error {
	if h.configPath == "" {
		return nil
	}
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
		bakPath := fmt.Sprintf("%s.bak", h.configPath)
		_ = os.WriteFile(bakPath, data, 0600)
		return atomicWriteFile(h.configPath, []byte(newContent), 0644)
	}

	return nil
}

func atomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-atomic-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)
	defer tmpFile.Close()

	if err := tmpFile.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", filename, err)
	}

	return nil
}

// UpsertLocalConfigNode 将新部署或更新的线路机节点安全写入 config.yaml 的 monitor.nodes。
func (h *AutoHealer) UpsertLocalConfigNode(name, nodeURL string) error {
	if h.configPath == "" || strings.TrimSpace(nodeURL) == "" {
		return nil
	}
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("unmarshal yaml: %w", err)
	}
	if doc == nil {
		doc = make(map[string]any)
	}
	monMap, ok := doc["monitor"].(map[string]any)
	if !ok || monMap == nil {
		monMap = make(map[string]any)
		doc["monitor"] = monMap
	}

	var existing []any
	if rawNodes, ok := monMap["nodes"].([]any); ok {
		existing = rawNodes
	}

	var replaced bool
	newHost, _, _ := parseHostPortFromURL(nodeURL)
	for idx, item := range existing {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemName, _ := m["name"].(string)
		itemURL, _ := m["url"].(string)
		itemHost, _, _ := parseHostPortFromURL(itemURL)
		if (name != "" && itemName == name) || (newHost != "" && itemHost == newHost) {
			m["name"] = name
			m["url"] = nodeURL
			m["is_self"] = true
			existing[idx] = m
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, map[string]any{
			"name":    name,
			"url":     nodeURL,
			"is_self": true,
		})
	}
	monMap["nodes"] = existing

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}
	bakPath := fmt.Sprintf("%s.bak", h.configPath)
	_ = os.WriteFile(bakPath, data, 0600)
	return atomicWriteFile(h.configPath, out, 0644)
}

func parseHostPortFromURL(raw string) (string, int, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", 0, false
	}
	p, _ := strconv.Atoi(u.Port())
	return u.Hostname(), p, true
}

// healNATPublicPort 通过 :3338 控制面重发布 NAT 公网端口。
// 私网端口与宿机进程保持不变，仅切换公网映射；失败由 RepublishNATPort 内部回滚，并同步回写 SubStore 与 config.yaml。
func (h *AutoHealer) healNATPublicPort(ctx context.Context, inst *VPSInstance, nodeName, targetURL string, oldPublicPort int, resp *PortHealResponse) (*PortHealResponse, error) {
	privatePort := oldPublicPort
	if portsResp, err := h.detector.ListLazyCatPortsViaHub(ctx, inst.ID); err == nil && portsResp != nil {
		for _, m := range portsResp.Ports {
			if m.PublicPort == oldPublicPort && m.PrivatePort > 0 {
				privatePort = m.PrivatePort
				break
			}
		}
	}

	newPub, newID, err := h.detector.RepublishNATPortViaHub(ctx, inst.ID, privatePort, oldPublicPort, "vless-heal")
	if err != nil {
		resp.Message = fmt.Sprintf("NAT 端口重发布失败: %v", err)
		return resp, err
	}
	newURL := replaceURLPort(targetURL, newPub)
	resp.NewPort = newPub
	resp.NewURL = newURL
	h.syncHealedNodeURL(ctx, oldPublicPort, newPub, targetURL, newURL, resp)
	resp.Success = true
	resp.Message = fmt.Sprintf("NAT 公网端口已切换 %d -> %d（mapping=%d，私网端口 %d 与宿机配置未改动）", oldPublicPort, newPub, newID, privatePort)
	logger.Infof("[AUTO-HEAL] %s: %s", nodeName, resp.Message)
	return resp, nil
}

// replaceURLPort 仅替换 URL 中 authority 的端口，保留 UUID、参数与 fragment。
func replaceURLPort(raw string, newPort int) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	port := strconv.Itoa(newPort)
	if u.Port() != "" {
		u.Host = net.JoinHostPort(u.Hostname(), port)
	} else {
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	return u.String()
}
