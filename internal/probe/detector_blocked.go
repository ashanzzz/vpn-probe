package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
)

// PeerProxyCandidate 自有健康候选节点信息
type PeerProxyCandidate struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type CheckHostInitResp struct {
	OK        int                 `json:"ok"`
	RequestID string              `json:"request_id"`
	Nodes     map[string][]string `json:"nodes"`
}

// CheckAbroadPort 通过 Check-Host.net 境外多节点 API 测试海外连通性
func CheckAbroadPort(ctx context.Context, host string, port int) (bool, string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	initURL := fmt.Sprintf("https://check-host.net/check-tcp?host=%s:%d&max_nodes=3", host, port)
	req, err := http.NewRequestWithContext(ctx, "GET", initURL, nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "vpn-probe-health-checker/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("check-host init failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, "", fmt.Errorf("check-host init status %d: %s", resp.StatusCode, string(body))
	}

	var initData CheckHostInitResp
	if err := json.NewDecoder(resp.Body).Decode(&initData); err != nil {
		return false, "", fmt.Errorf("decode check-host init: %w", err)
	}

	if initData.RequestID == "" {
		return false, "", fmt.Errorf("empty check-host request_id")
	}

	// 等待 2.5 秒供境外多节点完成探测
	select {
	case <-ctx.Done():
		return false, "", ctx.Err()
	case <-time.After(2500 * time.Millisecond):
	}

	// 轮询查询结果
	resultURL := fmt.Sprintf("https://check-host.net/check-result/%s", initData.RequestID)
	resReq, err := http.NewRequestWithContext(ctx, "GET", resultURL, nil)
	if err != nil {
		return false, "", err
	}
	resReq.Header.Set("Accept", "application/json")

	resResp, err := client.Do(resReq)
	if err != nil {
		return false, "", fmt.Errorf("check-host poll failed: %w", err)
	}
	defer resResp.Body.Close()

	var rawResult map[string]any
	if err := json.NewDecoder(resResp.Body).Decode(&rawResult); err != nil {
		return false, "", fmt.Errorf("decode check-host result: %w", err)
	}

	successCount := 0
	totalCount := 0
	var detailParts []string

	for nodeName, nodeData := range rawResult {
		totalCount++
		if arr, ok := nodeData.([]any); ok && len(arr) > 0 {
			first := arr[0]
			// Check-Host 标准响应: [{"time": 0.12, "address": "..."}]
			if m, ok := first.(map[string]any); ok {
				if _, hasTime := m["time"]; hasTime {
					successCount++
					detailParts = append(detailParts, fmt.Sprintf("%s: Open", nodeName))
					continue
				}
			}
			// 备用格式: [[1, 0.05]]
			if innerArr, ok := first.([]any); ok && len(innerArr) > 0 {
				if code, ok := innerArr[0].(float64); ok && code == 1 {
					successCount++
					detailParts = append(detailParts, fmt.Sprintf("%s: Open", nodeName))
					continue
				}
			} else if code, ok := first.(float64); ok && code == 1 {
				successCount++
				detailParts = append(detailParts, fmt.Sprintf("%s: Open", nodeName))
				continue
			}
		}
		detailParts = append(detailParts, fmt.Sprintf("%s: Closed", nodeName))
	}

	summary := strings.Join(detailParts, ", ")
	isAbroadReachable := successCount >= 1
	return isAbroadReachable, summary, nil
}

// CheckDomesticTCP 测试国内本地是否能通过 TCP 握手直连目标端口
func CheckDomesticTCP(host string, port int) bool {
	target := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", target, 2500*time.Millisecond)
	if err == nil {
		conn.Close()
		return true
	}
	return false
}

// CheckDomesticPing 使用系统原生 ping 命令测试国内 ICMP 连通性
func CheckDomesticPing(host string) bool {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "2", "-w", "1000", host)
	} else {
		cmd = exec.Command("ping", "-c", "2", "-W", "1", host)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	outStr := strings.ToLower(string(out))
	if strings.Contains(outStr, "ttl=") || strings.Contains(outStr, "bytes from") || strings.Contains(outStr, "0% 丢失") || strings.Contains(outStr, "0% packet loss") {
		return true
	}
	return false
}

// CheckAbroadViaProxy 通过本地健康代理沙箱端口，在海外向目标主机端口发起 TCP 握手
func CheckAbroadViaProxy(ctx context.Context, socksAddr string, targetHost string, targetPort int, timeout time.Duration) (bool, time.Duration, error) {
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return false, 0, fmt.Errorf("create socks5 dialer: %w", err)
	}

	target := net.JoinHostPort(targetHost, fmt.Sprintf("%d", targetPort))
	start := time.Now()

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var conn net.Conn
	if cd, ok := dialer.(proxy.ContextDialer); ok {
		conn, err = cd.DialContext(dialCtx, "tcp", target)
	} else {
		connChan := make(chan net.Conn, 1)
		errChan := make(chan error, 1)
		go func() {
			c, e := dialer.Dial("tcp", target)
			if e != nil {
				errChan <- e
			} else {
				connChan <- c
			}
		}()
		select {
		case <-dialCtx.Done():
			return false, 0, dialCtx.Err()
		case err = <-errChan:
			return false, 0, err
		case conn = <-connChan:
		}
	}

	if err != nil {
		return false, 0, err
	}
	conn.Close()
	return true, time.Since(start), nil
}

// CheckAbroadTiered 实施三级阶梯式海外连通性探测：自有健康节点优先，公共 API 兜底
func CheckAbroadTiered(
	ctx context.Context,
	sm *SandboxManager,
	peers []PeerProxyCandidate,
	targetHost string,
	targetPort int,
	cfg *config.Config,
) (reachable bool, source string, rttMs int, detail string) {
	mode := "auto"
	fallbackPublic := true
	peerConcurrency := 2
	peerTimeout := 3 * time.Second

	if cfg != nil {
		if cfg.Probe.AbroadProbe.Mode != "" {
			mode = cfg.Probe.AbroadProbe.Mode
		}
		fallbackPublic = cfg.Probe.AbroadProbe.FallbackToPublic
		if cfg.Probe.AbroadProbe.PeerConcurrency > 0 {
			peerConcurrency = cfg.Probe.AbroadProbe.PeerConcurrency
		}
		if cfg.Probe.AbroadProbe.PeerTimeoutMs > 0 {
			peerTimeout = time.Duration(cfg.Probe.AbroadProbe.PeerTimeoutMs) * time.Millisecond
		}
	}

	// 1. 若配置为 public_only，直接走公共 API
	if mode == "public_only" {
		ok, dt, err := CheckAbroadPort(ctx, targetHost, targetPort)
		if err != nil {
			return false, "public:check-host.net", 0, fmt.Sprintf("公共API探测异常: %v", err)
		}
		return ok, "public:check-host.net", 0, dt
	}

	// 2. 筛选可用的自有健康节点 (过滤掉目标自身，避免自测自)
	var validPeers []PeerProxyCandidate
	for _, p := range peers {
		if p.Host != targetHost || p.Port != targetPort {
			validPeers = append(validPeers, p)
		}
	}

	// 3. 尝试通过自有健康节点进行探测
	if len(validPeers) > 0 && sm != nil {
		if len(validPeers) > peerConcurrency {
			validPeers = validPeers[:peerConcurrency]
		}

		type peerResult struct {
			peerName string
			ok       bool
			rtt      time.Duration
			err      error
		}

		resCh := make(chan peerResult, len(validPeers))
		var wg sync.WaitGroup

		for _, p := range validPeers {
			wg.Add(1)
			go func(candidate PeerProxyCandidate) {
				defer wg.Done()

				parsed, err := ParseLink(candidate.URL)
				if err != nil {
					resCh <- peerResult{peerName: candidate.Name, ok: false, err: err}
					return
				}

				sbCtx, sbCancel := context.WithTimeout(ctx, peerTimeout+2*time.Second)
				defer sbCancel()

				inst, err := sm.Launch(sbCtx, parsed.RawOutbound)
				if err != nil {
					resCh <- peerResult{peerName: candidate.Name, ok: false, err: err}
					return
				}
				defer inst.Close(sm)

				socksAddr := fmt.Sprintf("127.0.0.1:%d", inst.Port)
				ok, rtt, err := CheckAbroadViaProxy(sbCtx, socksAddr, targetHost, targetPort, peerTimeout)
				resCh <- peerResult{peerName: candidate.Name, ok: ok, rtt: rtt, err: err}
			}(p)
		}

		wg.Wait()
		close(resCh)

		var successPeers []string
		var minRTT time.Duration = time.Hour
		var anySuccess bool

		for r := range resCh {
			if r.ok {
				anySuccess = true
				successPeers = append(successPeers, r.peerName)
				if r.rtt < minRTT {
					minRTT = r.rtt
				}
			}
		}

		if anySuccess {
			source = fmt.Sprintf("peer:%s", strings.Join(successPeers, ","))
			rttMs = int(minRTT.Milliseconds())
			detail = fmt.Sprintf("自有海外节点[%s]直连成功 (RTT: %dms)", strings.Join(successPeers, ","), rttMs)
			return true, source, rttMs, detail
		}

		logger.Warnf("[%s:%d] All %d peer proxy checks failed, checking fallback...", targetHost, targetPort, len(validPeers))
	} else {
		logger.Infof("[%s:%d] No healthy peer proxies available for abroad probe", targetHost, targetPort)
	}

	// 4. 若自有节点探测未成功 (或无可用自有节点)，且允许公共 API 兜底
	if fallbackPublic || mode == "auto" {
		logger.Probe("[%s:%d] Falling back to public Check-Host.net API...", targetHost, targetPort)
		ok, dt, err := CheckAbroadPort(ctx, targetHost, targetPort)
		if err != nil {
			return false, "public:check-host.net", 0, fmt.Sprintf("公共API探测异常: %v", err)
		}
		return ok, "public:check-host.net", 0, dt
	}

	return false, "none", 0, "无可用探针且已禁用公共API"
}

// EvaluateFault 根据四维探测结果、代理协议类型与节点角色计算故障分类与诊断指引
func EvaluateFault(
	host string,
	port int,
	protocol string,
	domesticTCPOK bool,
	abroadOK bool,
	pingOK bool,
	abroadSource string,
	isResidential ...bool,
) (faultType model.FaultType, statusText string, actionType string, tip string) {
	isResi := len(isResidential) > 0 && isResidential[0]

	// 1. 住宅家宽节点专属故障模型 (Issue #26: 根除“端口被墙/换端口/认证失败”误判，识别家庭穿透隧道与宿主机状态)
	// 家宽节点物理上为境外中转+反向家庭隧道，国内直连受阻为常态；优先判定家宽链路健康度，严禁被误判为凭据认证失败或盲目换端口
	if isResi {
		if abroadOK {
			// 海外探针正常 (穿透隧道在海外连通)，但国内直连受阻 (无论国内TCP SYN是否握手成功)
			faultType = model.FaultResidentialDirectBlocked
			statusText = "【仅境外通】国内直连受阻/需经自建专线链式中继"
			actionType = "CHAIN_VIA_TRANSIT"
			tip = fmt.Sprintf("【家宽网络特征】住宅家宽目标 %s:%d 海外隧道正常(%s)，仅国内直连受阻。严禁更换端口（换端口无效），请通过自建专线中转（链式代理）接入使用！", host, port, abroadSource)
		} else if !abroadOK && !domesticTCPOK {
			if pingOK {
				// 中转宿主机 Ping 通，但国内外端口都不通 -> 家庭端穿透掉线/设备关机
				faultType = model.FaultResidentialTunnelOffline
				statusText = "【家宽穿透掉线】家庭设备离线或反向穿透隧道中断"
				actionType = "CHECK_RESIDENTIAL_DEVICE_OR_TUNNEL"
				tip = fmt.Sprintf("【家宽穿透掉线】中转宿主机 %s 在线(Ping通)，但端口 %d 全球不通。通常为本地家庭端光猫重拨、断电或反向穿透(Rathole/FRP)服务中断，严禁换端口，请检查家庭端设备！", host, port)
			} else {
				// 中转宿主机连 Ping 都不通 -> 宿主机离线
				faultType = model.FaultTransitRelayDown
				statusText = "【中转离线】家宽中转宿主机宕机或网络中断"
				actionType = "CHECK_TRANSIT_RELAY_HOST"
				tip = fmt.Sprintf("【中转宿主宕机】家宽中转宿主机 %s 丢包不通，请检查日本中转宿主机或云服务商网络！", host)
			}
		} else {
			faultType = model.FaultResidentialTunnelOffline
			statusText = "【家宽异常】家庭设备或穿透服务异常"
			actionType = "CHECK_RESIDENTIAL_DEVICE_OR_TUNNEL"
			tip = fmt.Sprintf("目标 %s:%d 状态异常，请核查家庭穿透隧道与设备。", host, port)
		}
		return
	}

	// 2. 协议认证失败判断 (仅针对自建专线/VPS线路机：境内外TCP端口握手均畅通，但应用层握手被服务端拒绝)
	if domesticTCPOK && abroadOK {
		faultType = model.FaultProtocolMismatch
		switch strings.ToLower(protocol) {
		case "socks", "socks5":
			statusText = "【认证失败】SOCKS5账号密码错误或服务器设置了源IP白名单"
			actionType = "CHECK_SOCKS_AUTH_OR_WHITELIST"
			tip = fmt.Sprintf("【非被墙】目标 %s:%d 在境内与海外(%s)均能成功建立TCP握手，但SOCKS5代理握手失败。端口未被墙，通常为SOCKS5用户名密码不匹配，或服务器配置了源IP白名单限制！", host, port, abroadSource)
		case "vless-ws", "vmess-ws":
			statusText = "【配置错误】WebSocket路径(path)、Host反代域名或UUID不匹配"
			actionType = "CHECK_WS_PATH_OR_UUID"
			tip = fmt.Sprintf("【非被墙】目标 %s:%d 在境内与海外(%s)均能成功建立TCP握手，但WebSocket握手失败。端口未被墙，通常是WS Path路径错误、反代Host未匹配或UUID与服务端不符！", host, port, abroadSource)
		case "vless", "vless-reality":
			statusText = "【配置错误】Reality公钥(pbk)、ShortID校验失败或缺少flow参数"
			actionType = "CHECK_REALITY_KEYS"
			tip = fmt.Sprintf("【非被墙】目标 %s:%d 在境内与海外(%s)均能成功建立TCP握手，但sing-box代理协商失败(reality verification failed)。说明端口未被墙，机器好着，纯属客户端配置的Reality公钥(pbk)、ShortID与服务端不一致，或未匹配flow参数！", host, port, abroadSource)
		case "shadowsocks":
			statusText = "【配置错误】Shadowsocks密码或加密算法不匹配"
			actionType = "CHECK_SS_PASSWORD"
			tip = fmt.Sprintf("【非被墙】目标 %s:%d 在境内与海外(%s)均能成功建立TCP握手，但Shadowsocks代理握手失败，请核对密码与加密算法！", host, port, abroadSource)
		default:
			protoUpper := strings.ToUpper(protocol)
			if protoUpper == "" {
				protoUpper = "代理"
			}
			statusText = fmt.Sprintf("【配置错误】%s 协议握手或认证失败", protoUpper)
			actionType = "CHECK_PROXY_CREDENTIALS"
			tip = fmt.Sprintf("【非被墙】目标 %s:%d 在境内与海外(%s)均能成功建立TCP握手，但代理握手失败，请核对凭据配置！", host, port, abroadSource)
		}
		return
	}

	// 3. 自建 VPS 基础设施被墙与离线诊断模型
	if abroadOK && !domesticTCPOK {
		// 海外通、国内TCP不通 -> 确认为 GFW 拦截
		if pingOK {
			faultType = model.FaultPortBlocked
			statusText = fmt.Sprintf("%d端口被墙", port)
			actionType = "AI_CHANGE_PORT"
			tip = fmt.Sprintf("【%d端口被墙】海外探针(%s)正常连通且国内可Ping通，VPS与sing-box正常在线，仅%d端口被GFW拦截封锁。可指示AI直接登录服务器更换端口。", port, abroadSource, port)
		} else {
			faultType = model.FaultIPBlocked
			statusText = "IP被墙"
			actionType = "CHANGE_IP_OR_CDN"
			tip = fmt.Sprintf("【IP被墙】海外探针(%s)可正常握手，但国内全黑洞丢包，机器与服务完好，需更换IP或配置CDN反代救活。", abroadSource)
		}
	} else {
		// 海外也不通、国内也不通 -> 真正离线
		faultType = model.FaultMachineDown
		statusText = "【离线】机子关机/软件崩溃/线路中断"
		actionType = "CHECK_SERVER_AND_PROCESS"
		tip = fmt.Sprintf("【真正离线】全球海外探针(%s)与国内均无法连接该端口，非GFW封锁。换端口无效！请单独排查：1. VPS是否关机或欠费；2. 机房宿主断网；3. sing-box进程崩溃退出。", abroadSource)
	}
	return
}

// DiagnoseNode 深度故障诊断：严格切分「协议认证错误」、「端口被墙」、「IP被墙」、「真正离线」与「家宽穿透模型」
func DiagnoseNode(
	ctx context.Context,
	host string,
	port int,
	protocol string,
	domesticProxyReachable bool,
	sm *SandboxManager,
	peers []PeerProxyCandidate,
	cfg *config.Config,
	isResidential ...bool,
) *model.DiagnosticInfo {
	diag := &model.DiagnosticInfo{
		TargetIP:          host,
		TargetPort:        port,
		DomesticReachable: domesticProxyReachable,
	}

	logger.Probe("[%s:%d] Starting deep diagnosis (Tiered Abroad Probe + Domestic TCP + Domestic Ping)...", host, port)

	// 1. 境外探针 (自有节点优先 -> 公共 Check-Host 兜底)
	abroadOK, abroadSource, abroadRTT, abroadDetail := CheckAbroadTiered(ctx, sm, peers, host, port, cfg)
	diag.AbroadReachable = abroadOK
	diag.AbroadSource = abroadSource
	diag.AbroadRTTMs = abroadRTT
	logger.Probe("[%s:%d] Abroad probe result: reachable=%v [source: %s, rtt: %dms] (%s)",
		host, port, abroadOK, abroadSource, abroadRTT, abroadDetail)

	// 2. 境内本地 TCP 连通测试
	domesticTCPOK := CheckDomesticTCP(host, port)
	diag.DomesticReachable = domesticTCPOK
	logger.Probe("[%s:%d] Domestic TCP connect result: open=%v", host, port, domesticTCPOK)

	// 3. 境内 ICMP Ping
	pingOK := CheckDomesticPing(host)
	diag.DomesticPing = pingOK
	logger.Probe("[%s] Domestic ping result: pingable=%v", host, pingOK)

	// 4. 严格四维判定矩阵 (包含住宅家宽角色感知)
	diag.FaultType, diag.StatusText, diag.ActionType, diag.TroubleshootTip = EvaluateFault(
		host, port, protocol, domesticTCPOK, abroadOK, pingOK, abroadSource, isResidential...,
	)

	logger.Probe("[%s:%d] Final diagnosis: %s (Action: %s)", host, port, diag.StatusText, diag.ActionType)
	return diag
}
