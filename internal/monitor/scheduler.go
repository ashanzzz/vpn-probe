package monitor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/healer"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/probe"
	"vpn-probe/internal/speedtest"
	"vpn-probe/internal/store"
	"vpn-probe/internal/substore"
)

type NodeState struct {
	Config      config.MonitoredNode    `json:"config"`
	LastResult  *model.NodeProbeResult  `json:"last_result,omitempty"`
	LastChecked time.Time               `json:"last_checked"`
}

type Scheduler struct {
	cfg            *config.Config
	engine         *probe.Engine
	store          *store.Store
	substoreClient *substore.Client
	healer         *healer.Healer
	mu             sync.RWMutex
	nodeStates     map[string]*NodeState // node name -> state
	cancel         context.CancelFunc
}

func NewScheduler(
	cfg *config.Config,
	engine *probe.Engine,
	st *store.Store,
	sc *substore.Client,
) *Scheduler {
	s := &Scheduler{
		cfg:            cfg,
		engine:         engine,
		store:          st,
		substoreClient: sc,
		healer:         healer.NewHealer(cfg, sc),
		nodeStates:     make(map[string]*NodeState),
	}

	for _, n := range cfg.Monitor.Nodes {
		s.nodeStates[n.Name] = &NodeState{
			Config: n,
		}
	}

	return s
}

func (s *Scheduler) Start(ctx context.Context) {
	if !s.cfg.Monitor.Enabled {
		logger.Infof("Monitor scheduler is disabled in config")
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// 启动时自动对 SubStore 拓扑进行基线健康审计
	if s.substoreClient != nil && s.cfg.SubStore.Enabled {
		go func() {
			time.Sleep(3 * time.Second)
			audit, err := s.substoreClient.AuditTopology(context.Background())
			if err != nil {
				logger.Warnf("[SUBSTORE] Initial topology audit warning: %v", err)
			} else if !audit.Healthy {
				logger.Warnf("[SUBSTORE] ⚠️ Topology discrepancies detected in SubStore (%d issues found). Use POST /api/substore/reconcile to auto-heal.", len(audit.Discrepancies))
				for _, d := range audit.Discrepancies {
					logger.Warnf("[SUBSTORE]   - %s", d)
				}
			} else {
				logger.Sync("[SUBSTORE] ✅ SubStore topology is healthy and verified.")
			}
		}()
	}

	healthDur, _ := time.ParseDuration(s.cfg.Monitor.HealthCheckInterval)
	if healthDur <= 0 {
		healthDur = 2 * time.Minute
	}

	gfwDur, _ := time.ParseDuration(s.cfg.Monitor.GFWCheckInterval)
	if gfwDur <= 0 {
		gfwDur = 15 * time.Minute
	}

	splitDur, _ := time.ParseDuration(s.cfg.Monitor.SplitCheckInterval)
	if splitDur <= 0 {
		splitDur = 30 * time.Minute
	}

	logger.Infof("Monitor Scheduler started: %d nodes monitored", len(s.cfg.Monitor.Nodes))
	logger.Infof("Intervals: Health/Latency=%s | GFW/送中=%s | Split/分流=%s", healthDur, gfwDur, splitDur)

	// 启动后立即异步跑第一轮巡检
	go s.RunAllChecks(runCtx, false)

	// 定时器 1: 常规在线与延迟巡检
	go func() {
		ticker := time.NewTicker(healthDur)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				logger.Infof("[SCHEDULE] Triggering periodic Health & Latency check...")
				s.RunAllChecks(runCtx, false)
			}
		}
	}()

	// 定时器 2: 扩展分流业务巡检 (包含 ChatGPT/Claude/YouTube 探测)
	go func() {
		ticker := time.NewTicker(splitDur)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				logger.Infof("[SCHEDULE] Triggering periodic Split Targets check...")
				s.RunAllChecks(runCtx, true)
			}
		}
	}()

	// 定时器 3: 15 分钟定时拓扑审计与自愈核对
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if s.substoreClient != nil && s.cfg.SubStore.Enabled {
					logger.Sync("[SCHEDULE] Triggering 15-min periodic SubStore topology reconcile & node refresh...")
					_, _ = s.substoreClient.ReconcileTopology(runCtx)
					_ = s.RefreshNodesFromSubStore(runCtx)
				}
			}
		}
	}()

	// 启动 3 秒后尝试从 SubStore 动态扩展节点列表
	if s.substoreClient != nil && s.cfg.SubStore.Enabled {
		go func() {
			time.Sleep(3 * time.Second)
			_ = s.RefreshNodesFromSubStore(runCtx)
		}()
	}
}

// RefreshNodesFromSubStore 从 SubStore 动态同步待监控的节点 (包含自建 VPS 与蜂窝云/机场节点)
func (s *Scheduler) RefreshNodesFromSubStore(ctx context.Context) error {
	if s.substoreClient == nil || !s.cfg.SubStore.Enabled {
		return nil
	}

	subsToPull := []string{"ashan's vps", "蜂窝云"}
	var dynamicNodes []config.MonitoredNode

	for _, subName := range subsToPull {
		content, err := s.substoreClient.GetSubscriptionContent(ctx, subName)
		if err != nil {
			logger.Warnf("[MONITOR] Dynamic pull from SubStore sub '%s' failed: %v (falling back to static)", subName, err)
			continue
		}

		lines := strings.Split(content, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			parsed, parseErr := probe.ParseLink(trimmed)
			if parseErr != nil {
				continue
			}
			dynamicNodes = append(dynamicNodes, config.MonitoredNode{
				Name: parsed.Tag,
				URL:  trimmed,
			})
		}
	}

	if len(dynamicNodes) == 0 {
		logger.Warnf("[MONITOR] Dynamic pull returned 0 nodes, preserving existing %d nodes", len(s.nodeStates))
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, n := range dynamicNodes {
		if _, exists := s.nodeStates[n.Name]; !exists {
			s.nodeStates[n.Name] = &NodeState{Config: n}
		} else {
			s.nodeStates[n.Name].Config.URL = n.URL
		}
	}

	logger.Sync("[MONITOR] ✅ Dynamically synchronized %d monitor nodes from SubStore (total monitored: %d)", len(dynamicNodes), len(s.nodeStates))
	return nil
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// RunAllChecks 对已配置的监控节点全量执行检测并分类落盘
func (s *Scheduler) RunAllChecks(ctx context.Context, checkSplit bool) *model.BatchProbeResponse {
	s.mu.RLock()
	var links []string
	for _, state := range s.nodeStates {
		links = append(links, state.Config.URL)
		// 预热上一次已知健康的海外节点作为候选探针
		if state.LastResult != nil && state.LastResult.Online && !state.LastResult.ChinaExit {
			if state.LastResult.Diagnostic != nil {
				s.engine.UpdateHealthyPeer(probe.PeerProxyCandidate{
					Name: state.LastResult.Name,
					URL:  state.Config.URL,
					Host: state.LastResult.Diagnostic.TargetIP,
					Port: state.LastResult.Diagnostic.TargetPort,
				})
			}
		}
	}
	s.mu.RUnlock()

	if len(links) == 0 {
		return nil
	}

	req := &model.BatchProbeRequest{
		Links:              links,
		IsSelf:             true,
		SyncSubStore:       s.cfg.Monitor.AutoSyncSubStore,
		CheckDetailedSplit: checkSplit,
		TimeoutSeconds:     s.cfg.Probe.DefaultTimeout,
	}

	resp := s.engine.ProbeBatch(ctx, req)

	// 1. 结构化分类与自愈编排 (区分线路机、家宽机)
	var transitNodes []*model.NodeProbeResult
	var resiNodes []*model.NodeProbeResult
	var healthyTransits []*model.NodeProbeResult

	for _, res := range resp.Results {
		isResi := strings.Contains(res.Name, "家宽") || strings.Contains(res.Name, "动态") || strings.Contains(res.Name, "LZ-") || res.Protocol == "socks" || res.Protocol == "socks5"
		if isResi {
			res.NodeType = "residential"
			resiNodes = append(resiNodes, res)
		} else {
			res.NodeType = "transit"
			transitNodes = append(transitNodes, res)
			if res.Online {
				healthyTransits = append(healthyTransits, res)
				// 若尚未测速，先依延迟赋予基础评分
				if res.Score <= 0 {
					res.Score = speedtest.CalculateScore(&model.SpeedResult{SpeedMbps: 50}, res.LatencyMs, true)
				}
			}
		}

		// 自愈判定 1: 端口被墙 (设置诊断动作供具备权限的 VPS 执行方/MCP/AI处置，不单方面静默覆盖原生产URL)
		if res.Diagnostic != nil && res.Diagnostic.FaultType == model.FaultPortBlocked {
			res.Diagnostic.ActionType = "AI_CHANGE_PORT"
			res.StatusText = fmt.Sprintf("端口%d被墙(待自愈换端口)", res.Diagnostic.TargetPort)
			if healedURL, err := s.healer.HealPortBlocked(ctx, res); err == nil && healedURL != "" {
				res.HealedURL = healedURL
			}
		}

		// 自愈判定 2: 整机 IP 被墙借道扶持
		if res.Diagnostic != nil && res.Diagnostic.FaultType == model.FaultIPBlocked && !isResi {
			healedURL := s.healer.HealIPBlocked(res, healthyTransits)
			res.HealedURL = healedURL
			res.StatusText = "整墙借道扶持"
		}
	}

	// 2. 对线路机按综合得分排序
	rankedTransits := speedtest.RankNodes(transitNodes)

	// 3. 组装智能链式代理
	chainLinks := s.healer.BuildSmartChains(rankedTransits, resiNodes)

	// 4. 更新状态与持久化 (以稳定的 Node.Name 作为 Key，杜绝 URL 变动导致的 Key 丢失)
	s.mu.Lock()
	for _, res := range resp.Results {
		if state, ok := s.nodeStates[res.Name]; ok {
			state.LastResult = res
			state.LastChecked = time.Now()
		}
		_ = s.store.SaveResult(ctx, res)
	}
	s.mu.Unlock()

	// 5. 三层独立订阅与总集合原子推送至 Sub-Store (带引擎故障熔断保护)
	if s.substoreClient != nil && s.cfg.SubStore.Enabled {
		total := len(resp.Results)
		if total > 0 && float64(resp.EngineErrors)/float64(total) >= 0.30 {
			ratio := float64(resp.EngineErrors) / float64(total)
			logger.Event(logger.LevelWarn, "substore", "sync_skipped",
				fmt.Sprintf("reason=engine_error_ratio ratio=%.2f threshold=0.30 total=%d engine_err=%d",
					ratio, total, resp.EngineErrors))
		} else {
			var transitLinks, resiLinks []string
			for _, t := range rankedTransits {
				if t.Online {
					transitLinks = append(transitLinks, t.OriginalURL)
				}
			}
			for _, r := range resiNodes {
				if r.Online {
					resiLinks = append(resiLinks, r.OriginalURL)
				}
			}

			err := s.substoreClient.SyncThreeTier(ctx, substore.ThreeTierPayload{
				TransitDirectNodes: transitLinks,
				ResidentialNodes:   resiLinks,
				ChainNodes:         chainLinks,
			})
			if err != nil {
				logger.Event(logger.LevelWarn, "substore", "sync_failed", fmt.Sprintf("error=%v", err))
			} else {
				logger.Event(logger.LevelInfo, "substore", "sync_done",
					fmt.Sprintf("transit=%d resi=%d chains=%d", len(transitLinks), len(resiLinks), len(chainLinks)))
			}
		}
	}

	return resp
}

// RunSpeedTestAll 对所有在线线路机执行测速并按得分重排
func (s *Scheduler) RunSpeedTestAll(ctx context.Context, isStream bool, durationSec int) []*model.NodeProbeResult {
	s.mu.RLock()
	var targets []*NodeState
	for _, state := range s.nodeStates {
		if state.LastResult != nil && state.LastResult.Online && state.LastResult.NodeType == "transit" {
			targets = append(targets, state)
		}
	}
	s.mu.RUnlock()

	if len(targets) == 0 {
		return nil
	}

	logger.Infof("[SPEEDTEST] Launching speed tests for %d active transit nodes...", len(targets))
	for _, t := range targets {
		res, err := s.engine.SpeedTestOne(ctx, t.LastResult.OriginalURL, isStream, durationSec)
		if err == nil && res != nil {
			t.LastResult.Speed = res
			t.LastResult.Score = res.Score
			_ = s.store.SaveResult(ctx, t.LastResult)
		} else {
			logger.Warnf("[SPEEDTEST] Speed test for %s failed: %v", t.LastResult.Name, err)
		}
	}

	// 重新聚合排序并同步 Sub-Store
	var allTransits []*model.NodeProbeResult
	var allResis []*model.NodeProbeResult
	s.mu.RLock()
	for _, state := range s.nodeStates {
		if state.LastResult != nil {
			if state.LastResult.NodeType == "transit" {
				allTransits = append(allTransits, state.LastResult)
			} else {
				allResis = append(allResis, state.LastResult)
			}
		}
	}
	s.mu.RUnlock()

	ranked := speedtest.RankNodes(allTransits)
	chains := s.healer.BuildSmartChains(ranked, allResis)

	if s.substoreClient != nil && s.cfg.SubStore.Enabled {
		var transitLinks, resiLinks []string
		for _, t := range ranked {
			if t.Online {
				transitLinks = append(transitLinks, t.OriginalURL)
			}
		}
		for _, r := range allResis {
			if r.Online {
				resiLinks = append(resiLinks, r.OriginalURL)
			}
		}
		_ = s.substoreClient.SyncThreeTier(ctx, substore.ThreeTierPayload{
			TransitDirectNodes: transitLinks,
			ResidentialNodes:   resiLinks,
			ChainNodes:         chains,
		})
	}

	return ranked
}

// SyncThreeTierNow 立即执行一次三层订阅同步
func (s *Scheduler) SyncThreeTierNow(ctx context.Context) error {
	if s.substoreClient == nil || !s.cfg.SubStore.Enabled {
		return fmt.Errorf("substore integration is not enabled")
	}

	var allTransits []*model.NodeProbeResult
	var allResis []*model.NodeProbeResult
	s.mu.RLock()
	for _, state := range s.nodeStates {
		if state.LastResult != nil {
			if state.LastResult.NodeType == "transit" {
				allTransits = append(allTransits, state.LastResult)
			} else {
				allResis = append(allResis, state.LastResult)
			}
		}
	}
	s.mu.RUnlock()

	ranked := speedtest.RankNodes(allTransits)
	chains := s.healer.BuildSmartChains(ranked, allResis)

	var transitLinks, resiLinks []string
	for _, t := range ranked {
		if t.Online {
			transitLinks = append(transitLinks, t.OriginalURL)
		}
	}
	for _, r := range allResis {
		if r.Online {
			resiLinks = append(resiLinks, r.OriginalURL)
		}
	}

	return s.substoreClient.SyncThreeTier(ctx, substore.ThreeTierPayload{
		TransitDirectNodes: transitLinks,
		ResidentialNodes:   resiLinks,
		ChainNodes:         chains,
	})
}

// GetStatus 返回当前所有监控节点的实时健康度与分组
func (s *Scheduler) GetStatus() map[string]*NodeState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copyMap := make(map[string]*NodeState)
	for k, v := range s.nodeStates {
		copyMap[k] = v
	}
	return copyMap
}
