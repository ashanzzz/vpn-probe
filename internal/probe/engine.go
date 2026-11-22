package probe

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"vpn-probe/internal/classifier"
	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/speedtest"
)

type Engine struct {
	cfg          *config.Config
	sandboxMgr   *SandboxManager
	peerMu       sync.RWMutex
	healthyPeers map[string]PeerProxyCandidate // key: URL
}

func NewEngine(cfg *config.Config) *Engine {
	sm := NewSandboxManager(cfg.Probe.SingboxBin, cfg.Probe.PortStart, cfg.Probe.PortEnd)
	return &Engine{
		cfg:          cfg,
		sandboxMgr:   sm,
		healthyPeers: make(map[string]PeerProxyCandidate),
	}
}

func (e *Engine) UpdateHealthyPeer(candidate PeerProxyCandidate) {
	e.peerMu.Lock()
	defer e.peerMu.Unlock()
	e.healthyPeers[candidate.URL] = candidate
}

func (e *Engine) RemoveHealthyPeer(url string) {
	e.peerMu.Lock()
	defer e.peerMu.Unlock()
	delete(e.healthyPeers, url)
}

func (e *Engine) GetHealthyPeers() []PeerProxyCandidate {
	e.peerMu.RLock()
	defer e.peerMu.RUnlock()
	var list []PeerProxyCandidate
	for _, p := range e.healthyPeers {
		list = append(list, p)
	}
	return list
}

// ProbeOne 对单个链接进行完整生命周期的测试
func (e *Engine) ProbeOne(ctx context.Context, link string, isSelf bool, checkSplit bool, timeoutSec int) *model.NodeProbeResult {
	if timeoutSec <= 0 {
		timeoutSec = e.cfg.Probe.DefaultTimeout
	}
	timeout := time.Duration(timeoutSec) * time.Second

	result := &model.NodeProbeResult{
		ID:          uuid.New().String(),
		OriginalURL: link,
		IsSelf:      isSelf,
		TestedAt:    time.Now(),
	}

	// 1. 协议解析
	parsed, err := ParseLink(link)
	if err != nil {
		result.Online = false
		result.ErrorMsg = fmt.Sprintf("parse error: %v", err)
		result.Category = classifier.Classify(isSelf, false, false)
		logger.Warnf("Link parse failed: %v", err)
		return result
	}

	result.Protocol = parsed.Protocol
	result.Name = parsed.Tag
	logger.Probe("[%s] Starting probe (protocol: %s, is_self: %v)...", parsed.Tag, parsed.Protocol, isSelf)

	// 2. 启动临时 sing-box 沙箱
	sandboxCtx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	inst, err := e.sandboxMgr.Launch(sandboxCtx, parsed.RawOutbound)
	if err != nil {
		result.Online = false
		result.OfflineReason = "probe_engine_error"
		result.ErrorMsg = fmt.Sprintf("launch proxy sandbox failed: %v", err)
		result.Category = classifier.Classify(isSelf, false, false)
		logger.Errorf("[%s] Sandbox launch failed: %v", parsed.Tag, err)
		return result
	}
	defer func() {
		inst.Close(e.sandboxMgr)
		logger.Debugf("[%s] Sandbox on port %d released", parsed.Tag, inst.Port)
	}()

	logger.Debugf("[%s] Sandbox listening on 127.0.0.1:%d", parsed.Tag, inst.Port)

	// 3. 在线检测与延迟
	online, latency, err := CheckOnline(sandboxCtx, inst.Port, e.cfg.Probe.LatencyCheckURL, timeout)
	result.Online = online
	result.LatencyMs = latency

	serverHost, _ := parsed.RawOutbound["server"].(string)
	serverPort, _ := parsed.RawOutbound["server_port"].(int)

	if !online {
		// 校验是否属于本地临时沙箱进程/端口连接被拒 (探针引擎内部故障，而非远端节点故障)
		// 严格校验是否针对本地环回端口自身的 dial 握手失败，排除远端节点经由 SOCKS 返回的连接拒绝 (如 "unknown error connection refused")
		if err != nil {
			errStr := strings.ToLower(err.Error())
			loopbackDialFail := strings.Contains(errStr, fmt.Sprintf("dial tcp 127.0.0.1:%d", inst.Port)) &&
				(strings.Contains(errStr, "connect: connection refused") || strings.Contains(errStr, "connection refused")) &&
				!strings.Contains(errStr, "unknown error")
			if loopbackDialFail {
				result.OfflineReason = "probe_engine_error"
				result.Category = classifier.Classify(isSelf, false, false)
				result.StatusText = "【引擎异常】本地沙箱端口连接被拒"
				result.ErrorMsg = fmt.Sprintf("local sandbox connection refused: %v", err)
				logger.Errorf("[%s] Local sandbox loopback connection failed on 127.0.0.1:%d: %v", parsed.Tag, inst.Port, err)
				return result
			}
		}

		result.OfflineReason = "node_down"
		result.Category = classifier.Classify(isSelf, false, false)
		e.RemoveHealthyPeer(link)
		sbLog := ""
		if inst != nil {
			sbLog = inst.GetRecentLogs(3)
		}
		if sbLog != "" {
			logger.Probe("[%s] Status: ❌ OFFLINE (domestic error: %v | sing-box: %s)", parsed.Tag, err, sbLog)
		} else {
			logger.Probe("[%s] Status: ❌ OFFLINE (domestic error: %v)", parsed.Tag, err)
		}

		// 提前释放无用的本地临时沙箱进程，避免后续深度诊断期间空占端口与文件句柄
		if inst != nil {
			inst.Close(e.sandboxMgr)
		}

		// 触发深度故障诊断：严格切分「离线」与「被墙」
		if serverHost != "" && serverPort > 0 {
			diagCtx, diagCancel := context.WithTimeout(ctx, 15*time.Second)
			defer diagCancel()
			peers := e.GetHealthyPeers()
			protoDesc := parsed.Protocol
			if parsed.RawOutbound != nil {
				if tlsMap, ok := parsed.RawOutbound["tls"].(map[string]any); ok && tlsMap["reality"] != nil {
					protoDesc = parsed.Protocol + "-reality"
				} else if transMap, ok := parsed.RawOutbound["transport"].(map[string]any); ok && transMap["type"] == "ws" {
					protoDesc = parsed.Protocol + "-ws"
				}
			}
			isResi := strings.Contains(parsed.Tag, "家宽") || strings.Contains(strings.ToLower(parsed.Tag), "resi") || parsed.Protocol == "socks5"
			diag := DiagnoseNode(diagCtx, serverHost, serverPort, protoDesc, false, e.sandboxMgr, peers, e.cfg, isResi, err)
			result.Diagnostic = diag
			result.StatusText = diag.StatusText
			result.ErrorMsg = fmt.Sprintf("离线 (%s)", diag.StatusText)
		} else {
			result.StatusText = "【离线】目标主机配置无效"
			result.ErrorMsg = "offline (invalid server host/port)"
		}

		return result
	}

	result.StatusText = "正常双向互通"
	result.Diagnostic = &model.DiagnosticInfo{
		FaultType:         model.FaultNone,
		StatusText:        "正常双向互通",
		TargetIP:          serverHost,
		TargetPort:        serverPort,
		AbroadReachable:   true,
		DomesticReachable: true,
		DomesticPing:      true,
	}
	logger.Probe("[%s] Status: 🟢 ONLINE (latency: %dms)", parsed.Tag, latency)

	// 4. 送中检测 (Google NCR + CF trace)
	gfwRes := CheckGFW(sandboxCtx, inst.Port, e.cfg.Probe.GoogleNCRURL, e.cfg.Probe.CloudflareTrace, timeout)
	result.ChinaExit = gfwRes.IsChina
	result.ExitIP = gfwRes.ExitIP
	result.ExitCountry = gfwRes.ExitCountry

	if gfwRes.IsChina {
		logger.Probe("[%s] GFW check: 🇨🇳 送中 (exit: %s, %s)", parsed.Tag, gfwRes.ExitCountry, gfwRes.ExitIP)
	} else {
		logger.Probe("[%s] GFW check: 🌐 不送中 (exit: %s, %s)", parsed.Tag, gfwRes.ExitCountry, gfwRes.ExitIP)
		// 优先将不送中的健康海外节点加入探针池
		if serverHost != "" && serverPort > 0 {
			e.UpdateHealthyPeer(PeerProxyCandidate{
				Name: parsed.Tag,
				URL:  link,
				Host: serverHost,
				Port: serverPort,
			})
		}
	}

	// 5. 分类归档
	result.Category = classifier.Classify(isSelf, true, result.ChinaExit)
	logger.Probe("[%s] Classified as: 【%s】", parsed.Tag, result.Category)

	// 6. 拓展分流探测 (可选，仅展示)
	if checkSplit {
		logger.Debugf("[%s] Running split targets probe (ChatGPT/Claude/YouTube/Baidu)...", parsed.Tag)
		result.Split = CheckDetailedSplit(sandboxCtx, inst.Port, timeout)
		logger.Probe("[%s] Split results: ChatGPT=%s, Claude=%s, YouTube=%s, Baidu=%s",
			parsed.Tag, result.Split.ChatGPT, result.Split.Claude, result.Split.YouTube, result.Split.Baidu)
	}

	return result
}

// ProbeBatch 并发对多个链接进行批量探测
func (e *Engine) ProbeBatch(ctx context.Context, req *model.BatchProbeRequest) *model.BatchProbeResponse {
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = e.cfg.Probe.MaxConcurrency
	}
	if concurrency <= 0 {
		concurrency = 5
	}
	if concurrency > 30 {
		concurrency = 30
	}

	logger.Infof("Batch probe started: %d links (concurrency=%d, is_self=%v, split=%v)",
		len(req.Links), concurrency, req.IsSelf, req.CheckDetailedSplit)

	results := make([]*model.NodeProbeResult, len(req.Links))
	groupCounts := make(map[model.Category]int)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, link := range req.Links {
		wg.Add(1)
		go func(idx int, targetLink string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := e.ProbeOne(ctx, targetLink, req.IsSelf, req.CheckDetailedSplit, req.TimeoutSeconds)

			mu.Lock()
			results[idx] = res
			groupCounts[res.Category]++
			mu.Unlock()
		}(i, link)
	}

	wg.Wait()

	resp := &model.BatchProbeResponse{
		Total:       len(results),
		Results:     results,
		GroupCounts: groupCounts,
	}

	for _, r := range results {
		if r.OfflineReason == "probe_engine_error" {
			resp.EngineErrors++
		}
		if r.Online {
			resp.Online++
			if r.ChinaExit {
				resp.ChinaExit++
			} else {
				resp.CleanExit++
			}
		} else {
			resp.Offline++
		}
	}

	logger.Event(logger.LevelInfo, "probe", "batch_done",
		fmt.Sprintf("total=%d online=%d offline=%d engine_err=%d", resp.Total, resp.Online, resp.Offline, resp.EngineErrors))

	return resp
}

// SpeedTestOne 对单个节点链接启动沙箱并执行测速
func (e *Engine) SpeedTestOne(ctx context.Context, link string, isStream bool, durationSec int) (*model.SpeedResult, error) {
	parsed, err := ParseLink(link)
	if err != nil {
		return nil, fmt.Errorf("parse link: %w", err)
	}

	timeout := 45 * time.Second
	if isStream && durationSec > 0 {
		timeout = time.Duration(durationSec+15) * time.Second
	}

	sandboxCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inst, err := e.sandboxMgr.Launch(sandboxCtx, parsed.RawOutbound)
	if err != nil {
		return nil, fmt.Errorf("launch sandbox: %w", err)
	}
	defer inst.Close(e.sandboxMgr)

	// 首先探测是否在线与基础延迟
	online, latency, err := CheckOnline(sandboxCtx, inst.Port, e.cfg.Probe.LatencyCheckURL, 8*time.Second)
	if !online {
		return &model.SpeedResult{
			Method:   "failed",
			TestedAt: time.Now(),
			Score:    0,
		}, fmt.Errorf("node is offline or unreachable: %v", err)
	}

	if isStream {
		return speedtest.TestStream30S(sandboxCtx, inst.Port, "", durationSec, latency)
	}
	return speedtest.TestVPS10M(sandboxCtx, inst.Port, "", latency)
}
