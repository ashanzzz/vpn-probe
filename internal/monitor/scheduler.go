package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"vpn-probe/internal/config"
	"vpn-probe/internal/healer"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/probe"
	"vpn-probe/internal/speedtest"
	"vpn-probe/internal/store"
	"vpn-probe/internal/substore"
	"vpn-probe/internal/vpsmgr"
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
	vpsMgr         *vpsmgr.Manager
	mu             sync.RWMutex
	checkMu        sync.Mutex            // 保证 RunAllChecks 串行执行，防止沙箱探针与系统资源竞争
	speedMu        sync.Mutex            // 保证 RunSpeedTestAll 串行执行，避免并发测速带宽互相挤占
	syncMu         sync.Mutex            // 保证五维策略组向 SubStore 同步串行化
	nodeStates     map[string]*NodeState // node name -> state
	cancel         context.CancelFunc
}

func (s *Scheduler) SetVPSManager(m *vpsmgr.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vpsMgr = m
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
		if n.IsSelf {
			nodeName := n.Name
			if parsed, err := probe.ParseLink(n.URL); err == nil && parsed.Tag != "" {
				nodeName = parsed.Tag
			}
			s.nodeStates[nodeName] = &NodeState{
				Config: n,
				LastResult: &model.NodeProbeResult{
					ID:          nodeName,
					Name:        nodeName,
					OriginalURL: n.URL,
					NodeType:    "transit",
					Online:      true,
					ExitCountry: model.ResolveCountry("", nodeName),
				},
			}
		}
	}

	// 载入家宽节点 (优先从 config.yaml 的 residential_nodes，其次从 data/residential_nodes.json)
	var resiNodes []config.MonitoredNode
	resiNodes = append(resiNodes, cfg.Monitor.ResidentialNodes...)
	if len(resiNodes) == 0 {
		if data, err := os.ReadFile("./data/residential_nodes.json"); err == nil {
			var loaded []config.MonitoredNode
			if err := json.Unmarshal(data, &loaded); err == nil {
				resiNodes = append(resiNodes, loaded...)
			}
		}
	}

	for _, r := range resiNodes {
		if r.IsSelf || r.Name != "" {
			r.IsSelf = true
			nodeName := r.Name
			if parsed, err := probe.ParseLink(r.URL); err == nil && parsed.Tag != "" {
				nodeName = parsed.Tag
			}
			s.nodeStates[nodeName] = &NodeState{
				Config: r,
				LastResult: &model.NodeProbeResult{
					ID:          nodeName,
					Name:        nodeName,
					OriginalURL: r.URL,
					NodeType:    "residential",
					Online:      true,
					ExitCountry: model.ResolveCountry("", nodeName),
				},
			}
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
			select {
			case <-time.After(3 * time.Second):
			case <-runCtx.Done():
				return
			}
			audit, err := s.substoreClient.AuditTopology(runCtx)
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

	// 定时器 4: 每日凌晨 03:00 巡检 VPS 代理软件版本与 GitHub 上游更新
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		var lastCheckedDay int = -1
		for {
			select {
			case <-runCtx.Done():
				return
			case now := <-ticker.C:
				if now.Hour() == 3 && now.Day() != lastCheckedDay {
					s.mu.RLock()
					mgr := s.vpsMgr
					s.mu.RUnlock()
					if mgr != nil {
						lastCheckedDay = now.Day()
						logger.Infof("[SCHEDULE] 03:00 Daily Software Lifecycle & GitHub Release check triggered...")
						_ = mgr.DailyCheckUpdates(runCtx)
					}
				}
			}
		}
	}()

	// 启动 3 秒后尝试从 SubStore 动态扩展节点列表
	if s.substoreClient != nil && s.cfg.SubStore.Enabled {
		go func() {
			select {
			case <-time.After(3 * time.Second):
			case <-runCtx.Done():
				return
			}
			_ = s.RefreshNodesFromSubStore(runCtx)
		}()
	}
}

// RefreshNodesFromSubStore 从 SubStore 动态同步待监控的自建节点 (核心原则：外部连接绝不检测，仅检测自有服务)
func (s *Scheduler) RefreshNodesFromSubStore(ctx context.Context) error {
	if s.substoreClient == nil || !s.cfg.SubStore.Enabled {
		return nil
	}

	subs, err := s.substoreClient.GetSubscriptions(ctx)
	if err != nil {
		logger.Warnf("[MONITOR] Dynamic pull from SubStore failed: %v (falling back to static)", err)
		return err
	}

	subMap := make(map[string]string)
	for _, sub := range subs {
		subMap[sub.Name] = sub.Content
	}

	// 方案B五维自建策略组优先：自建线路机 (ashan-transit) 与 自建全家桶 (ashan-us-resi, ashan-jp-resi, ashan-other, ashan-china-leak)
	fiveGroups := []string{
		"ashan-transit",
		"ashan-us-resi",
		"ashan-jp-resi",
		"ashan-other",
		"ashan-china-leak",
	}

	var targetSubs []string
	hasFiveGroup := false
	for _, name := range fiveGroups {
		if _, exists := subMap[name]; exists {
			targetSubs = append(targetSubs, name)
			hasFiveGroup = true
		}
	}

	// 若未发现任何 5 组订阅，尝试向后兼容回退旧三层独立订阅
	if !hasFiveGroup {
		legacySubs := []string{"ashan-transit-direct", "ashan-resi-direct"}
		for _, name := range legacySubs {
			if _, exists := subMap[name]; exists {
				targetSubs = append(targetSubs, name)
			}
		}
	}

	if len(targetSubs) == 0 {
		s.mu.RLock()
		existingCount := len(s.nodeStates)
		s.mu.RUnlock()
		logger.Warnf("[MONITOR] SubStore has no self-hosted subscriptions, preserving existing %d nodes", existingCount)
		return nil
	}

	var dynamicNodes []config.MonitoredNode
	seenEndpoints := make(map[string]bool)

	for _, subName := range targetSubs {
		content := subMap[subName]
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}

			// 彻底过滤链式中继（链式代理为运行时中转池动态组装，不作为基础原子直连探针）
			lower := strings.ToLower(trimmed)
			if strings.Contains(lower, "dialerproxy=") || strings.Contains(lower, "dialer-proxy=") ||
				strings.Contains(trimmed, "➔") || strings.Contains(trimmed, "链式") {
				continue
			}

			parsed, parseErr := probe.ParseLink(trimmed)
			if parseErr != nil {
				continue
			}

			ep := parsed.Tag
			if h, p, s := parseEndpoint(trimmed); h != "" {
				ep = fmt.Sprintf("%s:%d:%s", h, p, s)
			} else {
				ep = trimmed
			}
			if seenEndpoints[ep] {
				continue
			}
			seenEndpoints[ep] = true

			dynamicNodes = append(dynamicNodes, config.MonitoredNode{
				Name:   parsed.Tag,
				URL:    trimmed,
				IsSelf: true,
			})
		}
	}

	if len(dynamicNodes) == 0 {
		s.mu.RLock()
		existingCount := len(s.nodeStates)
		s.mu.RUnlock()
		logger.Warnf("[MONITOR] Dynamic pull returned 0 nodes, preserving existing %d nodes", existingCount)
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	getEndpoint := func(rawURL string) string {
		h, p, s := parseEndpoint(rawURL)
		if h != "" {
			return fmt.Sprintf("%s:%d:%s", h, p, s)
		}
		return rawURL
	}

	validSelfSet := make(map[string]bool)
	for _, n := range dynamicNodes {
		ep := getEndpoint(n.URL)
		matchedKey := ""
		if _, ok := s.nodeStates[n.Name]; ok {
			matchedKey = n.Name
		} else {
			for key, state := range s.nodeStates {
				if getEndpoint(state.Config.URL) == ep {
					matchedKey = key
					break
				}
			}
		}

		isResi := model.IsResidentialNode(n.Name, n.URL, "")
		nodeType := "transit"
		if isResi {
			nodeType = "residential"
		}

		if matchedKey != "" {
			validSelfSet[matchedKey] = true
			s.nodeStates[matchedKey].Config.URL = n.URL
			s.nodeStates[matchedKey].Config.Name = n.Name
			s.nodeStates[matchedKey].Config.IsSelf = true
			if s.nodeStates[matchedKey].LastResult != nil {
				s.nodeStates[matchedKey].LastResult.Name = n.Name
				s.nodeStates[matchedKey].LastResult.OriginalURL = n.URL
				s.nodeStates[matchedKey].LastResult.NodeType = nodeType
			}
		} else {
			validSelfSet[n.Name] = true
			s.nodeStates[n.Name] = &NodeState{
				Config: n,
				LastResult: &model.NodeProbeResult{
					Name:        n.Name,
					OriginalURL: n.URL,
					NodeType:    nodeType,
					Online:      true,
					ExitCountry: model.ResolveCountry("", n.Name),
				},
			}
		}
	}

	// 静态配置文件中显式标记为 is_self 的也保留 (含线路机与家宽机)
	for _, n := range s.cfg.Monitor.Nodes {
		if n.IsSelf {
			validSelfSet[n.Name] = true
		}
	}
	for _, n := range s.cfg.Monitor.ResidentialNodes {
		if n.IsSelf {
			validSelfSet[n.Name] = true
		}
	}

	// 彻底清理掉非自建的外部节点 (外部连接不检测)
	removedCount := 0
	for name, state := range s.nodeStates {
		if !validSelfSet[name] || !state.Config.IsSelf {
			delete(s.nodeStates, name)
			removedCount++
		}
	}

	if removedCount > 0 {
		logger.Infof("[MONITOR] Pruned %d external/non-self nodes from monitor. Only self-hosted nodes remain.", removedCount)
	}

	logger.Sync("[MONITOR] ✅ Dynamically synchronized %d self-hosted monitor nodes (total monitored: %d)", len(dynamicNodes), len(s.nodeStates))
	return nil
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// RunAllChecks 对已配置的监控节点全量执行检测并分类落盘 (仅探测自建节点)
func (s *Scheduler) RunAllChecks(ctx context.Context, checkSplit bool) *model.BatchProbeResponse {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	s.mu.RLock()
	var links []string
	for _, state := range s.nodeStates {
		if !state.Config.IsSelf {
			continue
		}
		links = append(links, state.Config.URL)
		// 预热上一次已知健康的海外节点作为候选探针
		if state.LastResult != nil && state.LastResult.Online && !state.LastResult.ChinaExit {
			if s.engine != nil && state.LastResult.Diagnostic != nil {
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

	if s.engine == nil {
		logger.Warnf("[MONITOR] Probe engine is nil, skipping checks")
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
	if resp == nil {
		return nil
	}

	// 1. 结构化分类与自愈编排 (区分线路机、家宽机)
	var transitNodes []*model.NodeProbeResult
	var resiNodes []*model.NodeProbeResult
	var healthyTransits []*model.NodeProbeResult

	for _, res := range resp.Results {
		isResi := model.IsResidentialNode(res.Name, res.OriginalURL, res.Protocol)
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
		if res.Diagnostic != nil && res.Diagnostic.FaultType == model.FaultPortBlocked && !isResi {
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

	// 4. 更新状态与持久化 (支持以 Node.Name 或 OriginalURL 精准匹配)
	s.mu.Lock()
	for _, res := range resp.Results {
		updated := false
		if state, ok := s.nodeStates[res.Name]; ok {
			state.LastResult = res
			state.LastChecked = time.Now()
			updated = true
		}
		if !updated {
			for _, state := range s.nodeStates {
				if state.Config.URL == res.OriginalURL {
					state.LastResult = res
					state.LastChecked = time.Now()
					break
				}
			}
		}
	}
	s.mu.Unlock()

	if s.store != nil {
		for _, res := range resp.Results {
			_ = s.store.SaveResult(ctx, res)
		}
	}

	// 5. 三层独立订阅与总集合原子推送至 Sub-Store (带引擎故障熔断保护)
	if s.substoreClient != nil && s.cfg.SubStore.Enabled {
		total := len(resp.Results)
		if total > 0 && float64(resp.EngineErrors)/float64(total) >= 0.30 {
			ratio := float64(resp.EngineErrors) / float64(total)
			logger.Event(logger.LevelWarn, "substore", "sync_skipped",
				fmt.Sprintf("reason=engine_error_ratio ratio=%.2f threshold=0.30 total=%d engine_err=%d",
					ratio, total, resp.EngineErrors))
		} else {
			// 方案B：全量区域网状池化中继 (美西4837全量挂载美国家宽)
			uniqueResis := deduplicateNodeResults(resiNodes)
			fullMeshChains := s.healer.BuildFullMeshRegionalChains(rankedTransits, uniqueResis)
			if len(fullMeshChains) > 0 {
				chainLinks = fullMeshChains
			}
			chainLinks = mergeCustomChains(chainLinks)

			// 方案B：五维智能策略组分类 (线路机、美区全家桶、日区全家桶、其他、送中隔离)
			fiveGroups := ClassifyIntoFiveGroups(rankedTransits, uniqueResis, chainLinks)
			s.syncMu.Lock()
			err := s.substoreClient.SyncFiveGroups(ctx, fiveGroups)
			s.syncMu.Unlock()
			if err != nil {
				logger.Event(logger.LevelWarn, "substore", "sync_failed", fmt.Sprintf("error=%v", err))
			} else {
				logger.Event(logger.LevelInfo, "substore", "sync_done",
					fmt.Sprintf("transit=%d resi=%d chains=%d five_groups=synced", len(fiveGroups.TransitNodes), len(uniqueResis), len(chainLinks)))
			}
		}
	}

	return resp
}

// RunSpeedTestAll 对所有在线线路机执行测速并按得分重排
func (s *Scheduler) RunSpeedTestAll(ctx context.Context, isStream bool, durationSec int) []*model.NodeProbeResult {
	s.speedMu.Lock()
	defer s.speedMu.Unlock()

	if s.engine == nil {
		logger.Warnf("[SPEEDTEST] Probe engine is nil, skipping speed test")
		return nil
	}

	type targetInfo struct {
		key         string
		name        string
		originalURL string
	}

	s.mu.RLock()
	var targets []targetInfo
	for k, state := range s.nodeStates {
		if state.LastResult != nil && state.LastResult.Online && state.LastResult.NodeType == "transit" {
			targets = append(targets, targetInfo{
				key:         k,
				name:        state.LastResult.Name,
				originalURL: state.LastResult.OriginalURL,
			})
		}
	}
	s.mu.RUnlock()

	if len(targets) == 0 {
		return nil
	}

	logger.Infof("[SPEEDTEST] Launching speed tests for %d active transit nodes...", len(targets))
	for _, t := range targets {
		res, err := s.engine.SpeedTestOne(ctx, t.originalURL, isStream, durationSec)
		if err == nil && res != nil {
			var toSave *model.NodeProbeResult
			s.mu.Lock()
			if state, ok := s.nodeStates[t.key]; ok && state.LastResult != nil {
				cloned := *state.LastResult
				cloned.Speed = res
				cloned.Score = res.Score
				state.LastResult = &cloned
				toSave = &cloned
			}
			s.mu.Unlock()
			if toSave != nil && s.store != nil {
				_ = s.store.SaveResult(ctx, toSave)
			}
		} else {
			logger.Warnf("[SPEEDTEST] Speed test for %s failed: %v", t.name, err)
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
	uniqueResis := deduplicateNodeResults(allResis)
	chains := s.healer.BuildSmartChains(ranked, uniqueResis)

	if s.substoreClient != nil && s.cfg.SubStore.Enabled {
		fullMeshChains := s.healer.BuildFullMeshRegionalChains(ranked, uniqueResis)
		if len(fullMeshChains) == 0 {
			fullMeshChains = chains
		}
		fullMeshChains = mergeCustomChains(fullMeshChains)
		fiveGroups := ClassifyIntoFiveGroups(ranked, uniqueResis, fullMeshChains)
		s.syncMu.Lock()
		err := s.substoreClient.SyncFiveGroups(ctx, fiveGroups)
		s.syncMu.Unlock()
		if err != nil {
			logger.Event(logger.LevelWarn, "substore", "sync_failed", fmt.Sprintf("speedtest sync error=%v", err))
		}
	}

	return ranked
}

// SyncThreeTierNow 已废弃旧三层架构，自动平滑重定向至方案B五维策略组全量同步 (防旧订阅死灰复燃)
func (s *Scheduler) SyncThreeTierNow(ctx context.Context) error {
	logger.Sync("[SCHEDULER] 🔄 SyncThreeTierNow invoked; redirecting to SyncFiveGroupsNow to ensure Plan B 5-group integrity")
	return s.SyncFiveGroupsNow(ctx)
}

// SyncFiveGroupsNow 立即执行一次自建五维策略组全量同步 (方案B全网状中继池)
func (s *Scheduler) SyncFiveGroupsNow(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

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
	uniqueResis := deduplicateNodeResults(allResis)
	fullMeshChains := s.healer.BuildFullMeshRegionalChains(ranked, uniqueResis)
	fullMeshChains = mergeCustomChains(fullMeshChains)

	fiveGroups := ClassifyIntoFiveGroups(ranked, uniqueResis, fullMeshChains)
	return s.substoreClient.SyncFiveGroups(ctx, fiveGroups)
}

// mergeCustomChains 合并全网状中继与用户显式定制的动态链式绑定，按 URL 去重
func mergeCustomChains(baseChains []string) []string {
	customChains, _ := LoadCustomChains()
	if len(customChains) == 0 {
		return baseChains
	}
	chainSet := make(map[string]bool, len(baseChains)+len(customChains))
	var merged []string
	for _, ch := range baseChains {
		if !chainSet[ch] {
			chainSet[ch] = true
			merged = append(merged, ch)
		}
	}
	for _, cc := range customChains {
		if cc.ChainURL != "" && !chainSet[cc.ChainURL] {
			chainSet[cc.ChainURL] = true
			merged = append(merged, cc.ChainURL)
		}
	}
	return merged
}

// filterActiveTransitNodes 对线路机节点进行去重、主机分组与灾备热备联动过滤 (CF救活原则: 直连健康时热备隐藏，直连全军覆没时自动提升)
func filterActiveTransitNodes(transits []*model.NodeProbeResult) []string {
	uniqueTransits := deduplicateNodeResults(transits)

	type HostGroup struct {
		HostID      string
		DirectNodes []*model.NodeProbeResult
		RescueNodes []*model.NodeProbeResult
	}
	groups := make(map[string]*HostGroup)
	var groupOrder []string

	for _, t := range uniqueTransits {
		gid := getHostGroupKey(t)
		if _, ok := groups[gid]; !ok {
			groups[gid] = &HostGroup{HostID: gid}
			groupOrder = append(groupOrder, gid)
		}
		if isCFRescueNode(t) {
			groups[gid].RescueNodes = append(groups[gid].RescueNodes, t)
		} else {
			groups[gid].DirectNodes = append(groups[gid].DirectNodes, t)
		}
	}

	var activeTransitLinks []string
	for _, gid := range groupOrder {
		grp := groups[gid]
		hasDirectOnline := false
		for _, d := range grp.DirectNodes {
			if d.Online {
				hasDirectOnline = true
				activeTransitLinks = append(activeTransitLinks, d.OriginalURL)
			}
		}

		if hasDirectOnline {
			// 主力直连节点健康！CF 救活节点保持在待命池 (Standby)，从活跃推送订阅中剔除
			for _, r := range grp.RescueNodes {
				logger.Sync("[RESCUE-STANDBY] 🛡️ VPS主机 [%s] 主力直连节点健康在线，CF救活应急节点 [%s] 保持待命 (已从活跃订阅隐藏)", gid, r.Name)
			}
		} else {
			// 🚨 主力直连节点全部离线/被墙！自动激活/提升 CF 救活节点塞入订阅
			promoted := 0
			for _, r := range grp.RescueNodes {
				if r.Online {
					activeTransitLinks = append(activeTransitLinks, r.OriginalURL)
					promoted++
					logger.Sync("[RESCUE-ACTIVE] 🚨 VPS主机 [%s] 直连全部离线！自动激活并提升 CF救活应急节点 [%s] 进入活跃订阅！", gid, r.Name)
				}
			}
			if promoted == 0 {
				if len(grp.RescueNodes) == 0 {
					logger.Warnf("[TRANSIT-OFFLINE] ⚠️ VPS主机 [%s] 主力直连节点离线且未配置CF救活备用节点！", gid)
				} else {
					logger.Warnf("[RESCUE-FAILED] ⚠️ VPS主机 [%s] 直连与CF救活节点均不可用！", gid)
				}
			}
		}
	}
	return activeTransitLinks
}

// parseEndpoint 从代理链接中安全提取 (Host, Port, Scheme)
func parseEndpoint(rawURL string) (string, int, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, ""
	}
	host := u.Hostname()
	portStr := u.Port()
	port := 0
	if p, err := strconv.Atoi(portStr); err == nil {
		port = p
	}
	return host, port, u.Scheme
}

// deduplicateNodeResults 按网络端点 (Server:Port:Scheme) 彻底剔除重复节点
func deduplicateNodeResults(nodes []*model.NodeProbeResult) []*model.NodeProbeResult {
	seen := make(map[string]bool)
	var unique []*model.NodeProbeResult
	for _, n := range nodes {
		if n == nil {
			continue
		}
		h, p, s := parseEndpoint(n.OriginalURL)
		endpoint := ""
		if h != "" {
			endpoint = fmt.Sprintf("%s:%d:%s", h, p, s)
		} else {
			endpoint = n.OriginalURL
		}
		if seen[endpoint] {
			continue
		}
		seen[endpoint] = true
		unique = append(unique, n)
	}
	return unique
}

// isCFRescueNode 判定是否为 Cloudflare CDN WS 救活逃生备用节点
func isCFRescueNode(res *model.NodeProbeResult) bool {
	if res == nil {
		return false
	}
	name := strings.ToLower(res.Name)
	url := strings.ToLower(res.OriginalURL)
	return strings.Contains(name, "cf救活") ||
		strings.Contains(name, "救活") ||
		strings.Contains(name, "cf-ws") ||
		strings.Contains(name, "cf/openresty") ||
		strings.Contains(url, "-free.1992004.xyz") ||
		strings.Contains(url, "cf-vless") ||
		strings.Contains(url, "%2fcf-")
}

// getHostGroupKey 根据节点配置、Tag 或出口域名/IP归属 VPS 主机分组
func getHostGroupKey(res *model.NodeProbeResult) string {
	if res == nil {
		return "unknown"
	}
	name := res.Name
	host, _, _ := parseEndpoint(res.OriginalURL)
	if host == "" && res.Diagnostic != nil {
		host = res.Diagnostic.TargetIP
	}

	// 1. 美西 LAX 主机 (38.175.222.44 / lax-free.1992004.xyz)
	if strings.Contains(host, "lax-free") || strings.Contains(name, "US-LAX") || strings.Contains(name, "洛杉矶") || strings.Contains(name, "美西") || host == "38.175.222.44" {
		return "US-LAX"
	}
	// 2. 东京 IIJ 主机 (156.231.113.91 / tokyo-free.1992004.xyz)
	if strings.Contains(host, "tokyo-free") || strings.Contains(name, "JP-Tokyo-IIJ") || strings.Contains(name, "东京 IIJ") || strings.Contains(name, "东京IIJ") || host == "156.231.113.91" {
		return "JP-Tokyo-IIJ"
	}
	// 3. 东京软银 主机 (45.192.242.72)
	if strings.Contains(name, "JP-Tokyo-Softbank") || strings.Contains(name, "JP--ACCK-SB") || strings.Contains(name, "ACCK-SB") || strings.Contains(name, "东京软银") || host == "45.192.242.72" {
		return "JP-Tokyo-Softbank"
	}

	if host != "" {
		return host
	}
	return name
}

// GetStatus 返回当前所有监控节点的实时健康度与分组 (返回结构体浅拷贝，防止并发读写冲突)
func (s *Scheduler) GetStatus() map[string]*NodeState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copyMap := make(map[string]*NodeState, len(s.nodeStates))
	for k, v := range s.nodeStates {
		if v == nil {
			continue
		}
		st := *v
		copyMap[k] = &st
	}
	return copyMap
}

// ClassifyIntoFiveGroups 将自建线路机、家宽机与链式代理编排为五维自建策略组 (方案B)
func ClassifyIntoFiveGroups(transits []*model.NodeProbeResult, resis []*model.NodeProbeResult, chains []string) model.FiveGroupPayload {
	var payload model.FiveGroupPayload

	// 1. 过滤线路机（去重并按 CF 救活原则隐藏待命节点）
	activeTransitLinks := filterActiveTransitNodes(transits)
	activeTransitSet := make(map[string]bool)
	for _, l := range activeTransitLinks {
		activeTransitSet[l] = true
	}

	// 2. 遍历所有自建节点，严格做送中隔离
	leakURLs := make(map[string]bool)

	for _, t := range transits {
		if t != nil && t.Online && t.ChinaExit {
			leakURLs[t.OriginalURL] = true
			payload.ChinaLeakNodes = append(payload.ChinaLeakNodes, t.OriginalURL)
		}
	}
	for _, r := range resis {
		if r != nil && r.Online && r.ChinaExit {
			leakURLs[r.OriginalURL] = true
			payload.ChinaLeakNodes = append(payload.ChinaLeakNodes, r.OriginalURL)
		}
	}

	// 3. 自建-线路机 (ashan-transit)：所有活跃、未送中的线路机直连
	for _, l := range activeTransitLinks {
		if !leakURLs[l] {
			payload.TransitNodes = append(payload.TransitNodes, l)
		}
	}

	// 4. 地域划分：美区 (US) 与 日区 (JP) 以及 其他 (Other)
	// 4.1 线路机严格隔离于家宽组外（线路机专属于 ashan-transit 自建-线路机，严禁混入美日家宽组，彻底杜绝 Clash 出现 duplicate proxy 错误）

	// 4.2 家宽直连归类 (自建-美国家宽 / 自建-日本家宽)
	uniqueResis := deduplicateNodeResults(resis)
	for _, r := range uniqueResis {
		if r == nil || leakURLs[r.OriginalURL] {
			continue
		}
		if !r.Online && r.Diagnostic != nil && r.Diagnostic.FaultType == model.FaultMachineDown {
			continue
		}
		country := model.ResolveCountry(r.ExitCountry, r.Name)
		switch country {
		case "US":
			payload.USResiNodes = append(payload.USResiNodes, r.OriginalURL)
		case "JP":
			payload.JPResiNodes = append(payload.JPResiNodes, r.OriginalURL)
		default:
			payload.OtherNodes = append(payload.OtherNodes, r.OriginalURL)
		}
	}

	// 4.3 链式中继归类 (依落地家宽端国别分类到对应的美区/日区/其他全家桶，严格做双向送中隔离)
	for _, ch := range chains {
		chainName := ch
		if hashIdx := strings.Index(ch, "#"); hashIdx != -1 {
			chainName = ch[hashIdx+1:]
		}
		if uName, err := url.QueryUnescape(chainName); err == nil {
			chainName = uName
		}

		// 优先从标签名中解析中继与落地目标，避免 URL 参数/密码中的特殊符号干扰
		chainLabel := chainName
		if pipeIdx := strings.Index(chainLabel, "|"); pipeIdx != -1 {
			chainLabel = strings.TrimSpace(chainLabel[pipeIdx+1:])
		}
		transitPart := ""
		targetPart := chainLabel
		if arrowIdx := strings.Index(chainLabel, "➔"); arrowIdx != -1 {
			transitPart = strings.TrimSpace(chainLabel[:arrowIdx])
			targetPart = strings.TrimSpace(chainLabel[arrowIdx+len("➔"):])
		}

		// 检查中继专线或落地家宽是否有任一端送中 (严格匹配节点名称与 emoji 变体，防短 ID / 包含串误判)
		chainLeaked := false
		if transitPart != "" {
			for _, t := range transits {
				if t == nil || !t.ChinaExit || strings.TrimSpace(t.Name) == "" {
					continue
				}
				if MatchNodeName(transitPart, t.Name) {
					chainLeaked = true
					break
				}
			}
		}
		if !chainLeaked && targetPart != "" {
			for _, r := range resis {
				if r == nil || !r.ChinaExit || strings.TrimSpace(r.Name) == "" {
					continue
				}
				if MatchNodeName(targetPart, r.Name) {
					chainLeaked = true
					break
				}
			}
		}

		if chainLeaked {
			payload.ChinaLeakNodes = append(payload.ChinaLeakNodes, ch)
			continue
		}

		country := model.ResolveCountry("", targetPart)
		switch country {
		case "US":
			payload.USResiNodes = append(payload.USResiNodes, ch)
		case "JP":
			payload.JPResiNodes = append(payload.JPResiNodes, ch)
		default:
			payload.OtherNodes = append(payload.OtherNodes, ch)
		}
	}

	return payload
}

// SetHealerForTest 测试钩子
func (s *Scheduler) SetHealerForTest(h *healer.Healer) {
	s.healer = h
}

// InjectNodeStateForTest 测试注入节点状态钩子
func (s *Scheduler) InjectNodeStateForTest(key string, res *model.NodeProbeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	isSelf := true
	origURL := ""
	if res != nil {
		isSelf = res.IsSelf
		origURL = res.OriginalURL
	}
	s.nodeStates[key] = &NodeState{
		Config:     config.MonitoredNode{Name: key, URL: origURL, IsSelf: isSelf},
		LastResult: res,
	}
}

// MatchNodeName 精确匹配节点名称，安全兼容带有国旗/emoji 前缀的变体，杜绝子串碰撞误杀
func MatchNodeName(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b || strings.EqualFold(a, b) {
		return true
	}
	lowerA := strings.ToLower(a)
	lowerB := strings.ToLower(b)
	if strings.HasSuffix(lowerA, lowerB) {
		prefix := strings.TrimSpace(lowerA[:len(lowerA)-len(lowerB)])
		if isEmojiOrSymbolPrefix(prefix) {
			return true
		}
	}
	if strings.HasSuffix(lowerB, lowerA) {
		prefix := strings.TrimSpace(lowerB[:len(lowerB)-len(lowerA)])
		if isEmojiOrSymbolPrefix(prefix) {
			return true
		}
	}
	return false
}

func isEmojiOrSymbolPrefix(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return true
	}
	for _, r := range prefix {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
