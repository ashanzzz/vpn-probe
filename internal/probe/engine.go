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
	splitCache   map[string]*splitCacheEntry
	splitCacheMu sync.RWMutex
}

type splitCacheEntry struct {
	ExitIP   string
	Split    *model.SplitInfo
	CachedAt time.Time
}

func NewEngine(cfg *config.Config) *Engine {
	sm := NewSandboxManager(cfg.Probe.SingboxBin, cfg.Probe.PortStart, cfg.Probe.PortEnd)
	return &Engine{
		cfg:          cfg,
		sandboxMgr:   sm,
		healthyPeers: make(map[string]PeerProxyCandidate),
		splitCache:   make(map[string]*splitCacheEntry),
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

// ProbeOne 对单个链接进行完整生命周期的测试 (保持向后兼容)
func (e *Engine) ProbeOne(ctx context.Context, link string, isSelf bool, checkSplit bool, timeoutSec int) *model.NodeProbeResult {
	return e.ProbeOneAdvanced(ctx, link, isSelf, checkSplit, false, timeoutSec)
}

// ProbeOneAdvanced 对单个链接进行完整生命周期的测试 (支持强制刷新穿透缓存)
func (e *Engine) ProbeOneAdvanced(ctx context.Context, link string, isSelf bool, checkSplit bool, forceRefresh bool, timeoutSec int) *model.NodeProbeResult {
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

	var serverHost string
	var serverPort int
	if parsed.RawOutbound != nil {
		serverHost, _ = parsed.RawOutbound["server"].(string)
		serverPort, _ = parsed.RawOutbound["server_port"].(int)
	}

	// 2. 启动临时 sing-box 沙箱并执行在线状态与延迟检测 (支持本地沙箱启动或环回端口瞬时连接失败自动重试，规避瞬时端口复用竞争)
	var inst *SandboxInstance
	var activeSandboxCancel context.CancelFunc
	var online bool
	var latency int
	var checkErr error

	defer func() {
		if activeSandboxCancel != nil {
			activeSandboxCancel()
		}
		if inst != nil {
			inst.Close(e.sandboxMgr)
			logger.Debugf("[%s] Sandbox on port %d released", parsed.Tag, inst.Port)
		}
	}()

	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			logger.Warnf("[%s] Transient probe engine failure encountered, retrying with fresh sandbox (attempt %d)...", parsed.Tag, attempt+1)
			time.Sleep(150 * time.Millisecond)
		}

		sCtx, sCancel := context.WithTimeout(ctx, timeout+5*time.Second)
		launchInst, launchErr := e.sandboxMgr.Launch(sCtx, parsed.RawOutbound)
		if launchErr != nil {
			sCancel()
			if attempt < 1 {
				continue
			}
			result.Online = false
			result.OfflineReason = "probe_engine_error"
			result.StatusText = "【引擎异常】启动沙箱失败"
			result.ErrorMsg = fmt.Sprintf("launch proxy sandbox failed: %v", launchErr)
			result.Category = classifier.Classify(isSelf, false, false)
			result.Diagnostic = &model.DiagnosticInfo{
				FaultType:       model.FaultProbeEngineError,
				StatusText:      result.StatusText,
				TargetIP:        serverHost,
				TargetPort:      serverPort,
				ActionType:      "RETRY_PROBE",
				TroubleshootTip: result.ErrorMsg,
			}
			logger.Errorf("[%s] Sandbox launch failed: %v", parsed.Tag, launchErr)
			return result
		}

		logger.Debugf("[%s] Sandbox listening on 127.0.0.1:%d (attempt %d)", parsed.Tag, launchInst.Port, attempt+1)

		online, latency, checkErr = CheckOnline(sCtx, launchInst.Port, e.cfg.Probe.LatencyCheckURL, timeout)
		if !online && checkErr != nil {
			errStr := strings.ToLower(checkErr.Error())
			loopbackDialFail := strings.Contains(errStr, fmt.Sprintf("dial tcp 127.0.0.1:%d", launchInst.Port)) &&
				(strings.Contains(errStr, "connect: connection refused") || strings.Contains(errStr, "connection refused")) &&
				!strings.Contains(errStr, "unknown error")

			if loopbackDialFail {
				var sbLog string
				if launchInst.LogBuf != nil {
					sbLog = strings.TrimSpace(launchInst.GetRecentLogs(3))
				}
				launchInst.Close(e.sandboxMgr)
				sCancel()

				if attempt < 1 {
					continue
				}

				result.Online = false
				result.LatencyMs = 0
				result.OfflineReason = "probe_engine_error"
				result.Category = classifier.Classify(isSelf, false, false)
				result.StatusText = "【引擎异常】本地沙箱端口连接被拒"
				errMsg := fmt.Sprintf("local sandbox connection refused: %v", checkErr)
				if sbLog != "" {
					errMsg = fmt.Sprintf("%s | sing-box: %s", errMsg, sbLog)
				}
				result.ErrorMsg = errMsg
				result.Diagnostic = &model.DiagnosticInfo{
					FaultType:       model.FaultProbeEngineError,
					StatusText:      result.StatusText,
					TargetIP:        serverHost,
					TargetPort:      serverPort,
					ActionType:      "RETRY_PROBE",
					TroubleshootTip: result.ErrorMsg,
				}
				logger.Errorf("[%s] Local sandbox loopback connection failed on 127.0.0.1:%d: %v", parsed.Tag, launchInst.Port, checkErr)
				return result
			}
		}

		inst = launchInst
		activeSandboxCancel = sCancel
		break
	}

	result.Online = online
	result.LatencyMs = latency

	if !online {
		result.OfflineReason = "node_down"
		result.Category = classifier.Classify(isSelf, false, false)
		e.RemoveHealthyPeer(link)
		sbLog := ""
		if inst != nil {
			sbLog = inst.GetRecentLogs(3)
		}
		if sbLog != "" {
			logger.Probe("[%s] Status: [OFFLINE] (domestic error: %v | sing-box: %s)", parsed.Tag, checkErr, sbLog)
		} else {
			logger.Probe("[%s] Status: [OFFLINE] (domestic error: %v)", parsed.Tag, checkErr)
		}

		// 提前释放无用的本地临时沙箱进程，避免后续深度诊断期间空占端口与文件句柄
		if inst != nil {
			inst.Close(e.sandboxMgr)
			inst = nil
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
			isResi := model.IsResidentialNode(parsed.Tag, link, parsed.Protocol)
			diagErr := checkErr
			if sbLog != "" {
				diagErr = fmt.Errorf("%w | sing-box: %s", checkErr, sbLog)
			}
			diag := DiagnoseNode(diagCtx, serverHost, serverPort, protoDesc, false, e.sandboxMgr, peers, e.cfg, isResi, diagErr)
			result.Diagnostic = diag
			result.StatusText = diag.StatusText
			result.ErrorMsg = fmt.Sprintf("离线 (%s)", diag.StatusText)
		} else {
			result.StatusText = "【离线】目标主机配置无效"
			result.ErrorMsg = "offline (invalid server host/port)"
		}

		result.TopologyTrace = BuildNodeHopTrace(result, parsed, link)
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
	logger.Probe("[%s] Status: [ONLINE] (latency: %dms)", parsed.Tag, latency)

	// 4. 送中检测 (Google NCR + CF trace)
	gfwCtx, gfwCancel := context.WithTimeout(ctx, timeout)
	defer gfwCancel()
	gfwRes := CheckGFW(gfwCtx, inst.Port, e.cfg.Probe.GoogleNCRURL, e.cfg.Probe.CloudflareTrace, timeout)
	result.ChinaExit = gfwRes.IsChina
	result.ExitIP = gfwRes.ExitIP
	result.ExitCountry = gfwRes.ExitCountry
	result.ExitCity = gfwRes.ExitCity
	result.ExitISP = gfwRes.ExitISP
	isResi := model.IsResidentialNode(parsed.Tag, link, parsed.Protocol)
	if isResi || gfwRes.ExitIPType == "" {
		result.ExitIPType = DetermineIPType(result.ExitIP, result.ExitISP, "", isResi)
	} else {
		result.ExitIPType = gfwRes.ExitIPType
	}

	// 若强制刷新 (forceRefresh)，穿透并清除当前落地 IP 的本地数据库与内存缓存，重新在线查询 Geo 属性
	if forceRefresh && result.ExitIP != "" {
		InvalidateIPGeoCache(ctx, result.ExitIP)
		freshGeo := LookupGeo(ctx, nil, result.ExitIP, "")
		if freshGeo.City != "" {
			result.ExitCity = freshGeo.City
			result.ExitCountry = freshGeo.CountryCode
			result.ExitISP = freshGeo.ISP
			result.ExitIPType = DetermineIPType(freshGeo.IP, freshGeo.ISP, freshGeo.Org, isResi)
		}
	}

	if gfwRes.IsChina {
		logger.Probe("[%s] GFW check: [送中] (exit: %s, %s, %s)", parsed.Tag, gfwRes.ExitCountry, gfwRes.ExitIP, result.ExitIPType)
	} else {
		logger.Probe("[%s] GFW check: [不送中] (exit: %s, %s, %s)", parsed.Tag, gfwRes.ExitCountry, gfwRes.ExitIP, result.ExitIPType)
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

	// 6. 拓展分流探测 (带 24 小时智能继承与 forceRefresh 穿透)
	if checkSplit {
		cacheKey := parsed.Tag
		if result.ExitIP != "" {
			cacheKey = parsed.Tag + "@" + result.ExitIP
		}

		var inherited bool
		if !forceRefresh {
			e.splitCacheMu.RLock()
			cached, exists := e.splitCache[cacheKey]
			e.splitCacheMu.RUnlock()

			if exists && cached != nil && cached.ExitIP == result.ExitIP && time.Since(cached.CachedAt) < 24*time.Hour && cached.Split != nil {
				result.Split = cached.Split
				inherited = true
				logger.Probe("[%s] Split results inherited from cache (ExitIP=%s, cached %s ago)",
					parsed.Tag, result.ExitIP, time.Since(cached.CachedAt).Round(time.Minute))
			}
		}

		if !inherited {
			if forceRefresh {
				e.splitCacheMu.Lock()
				delete(e.splitCache, cacheKey)
				e.splitCacheMu.Unlock()
			}

			logger.Debugf("[%s] Running live split targets probe (ChatGPT/Claude/Gemini/YouTube/Baidu)...", parsed.Tag)
			splitCtx, splitCancel := context.WithTimeout(ctx, timeout)
			defer splitCancel()
			result.Split = CheckDetailedSplit(splitCtx, inst.Port, timeout)

			if result.Split != nil {
				e.splitCacheMu.Lock()
				e.splitCache[cacheKey] = &splitCacheEntry{
					ExitIP:   result.ExitIP,
					Split:    result.Split,
					CachedAt: time.Now(),
				}
				e.splitCacheMu.Unlock()
			}
		}

		if result.Split != nil {
			logger.Probe("[%s] Split results: ChatGPT=%s, Claude=%s, Gemini=%s, YouTube=%s, Baidu=%s",
				parsed.Tag, result.Split.ChatGPT, result.Split.Claude, result.Split.Gemini, result.Split.YouTube, result.Split.Baidu)
		}
	}

	// 7. 构建微观 4 阶路由追踪链路 (客户端 -> 线路机 -> NAT中转 -> 家宽出口)
	result.TopologyTrace = BuildNodeHopTrace(result, parsed, link)

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

	logger.Infof("Batch probe started: %d links (concurrency=%d, is_self=%v, split=%v, force_refresh=%v)",
		len(req.Links), concurrency, req.IsSelf, req.CheckDetailedSplit, req.ForceRefresh)

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

			// 微秒级轻度错峰（0~60ms），规避数十个临时进程在同一毫秒内并发 fork/exec 导致的系统 CPU 调度颠簸与 I/O 争抢
			if idx > 0 {
				stagger := time.Duration((idx % 4) * 20) * time.Millisecond
				time.Sleep(stagger)
			}

			res := e.ProbeOneAdvanced(ctx, targetLink, req.IsSelf, req.CheckDetailedSplit, req.ForceRefresh, req.TimeoutSeconds)

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
