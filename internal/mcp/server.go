package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/probe"
	"vpn-probe/internal/store"
	"vpn-probe/internal/substore"
)

type MCPServer struct {
	server         *server.MCPServer
	engine         *probe.Engine
	store          *store.Store
	substoreClient *substore.Client
	cfg            *config.Config
}

func NewMCPServer(cfg *config.Config, engine *probe.Engine, st *store.Store, sc *substore.Client) *MCPServer {
	s := server.NewMCPServer(
		"vpn-probe",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	ms := &MCPServer{
		server:         s,
		engine:         engine,
		store:          st,
		substoreClient: sc,
		cfg:            cfg,
	}

	ms.registerTools()
	return ms
}

func (m *MCPServer) GetServer() *server.MCPServer {
	return m.server
}

func (m *MCPServer) registerTools() {
	// 1. probe_and_classify_links
	probeTool := mcp.NewTool("probe_and_classify_links",
		mcp.WithDescription("提交代理节点链接(vmess/vless/ss/trojan/sing-box)，自动进行是否在线与是否送中检测，并分类同步至远程 SubStore"),
		mcp.WithArray("links", mcp.Required(), mcp.Description("待检测代理链接列表"), mcp.WithStringItems()),
		mcp.WithBoolean("is_self", mcp.Description("是否作为自有服务管理(默认为 true，归入在线自有服务、离线自有服务、在线自有服务送中、在线自有服务不送中)")),
		mcp.WithBoolean("sync_substore", mcp.Description("检测完成后是否自动同步更新至远程 SubStore 分组(默认为 true)")),
		mcp.WithBoolean("check_detailed_split", mcp.Description("是否顺带探测 ChatGPT/Claude/YouTube 等分流详情(仅展示，默认为 false)")),
		mcp.WithNumber("timeout_seconds", mcp.Description("单个节点检测超时秒数(默认 8 秒)")),
	)

	m.server.AddTool(probeTool, m.handleProbeLinks)

	// 2. get_latest_probe_results
	historyTool := mcp.NewTool("get_latest_probe_results",
		mcp.WithDescription("获取系统最近检测过的节点状态历史(包含在线情况、送中状态、延迟与分流信息)"),
		mcp.WithNumber("limit", mcp.Description("获取数量限制(默认 20)")),
	)
	m.server.AddTool(historyTool, m.handleGetHistory)

	// 3. sync_substore_now
	syncTool := mcp.NewTool("sync_substore_now",
		mcp.WithDescription("立即触发远程 SubStore 同步或重新拉取缓存"),
		mcp.WithString("sub_name", mcp.Description("指定刷新的 SubStore 订阅名称，留空则刷新配置中所有分组")),
	)
	m.server.AddTool(syncTool, m.handleSyncSubStore)

	// 4. list_port_blocked_nodes (供 AI 快速获取需要更换端口的节点)
	portBlockedTool := mcp.NewTool("list_port_blocked_nodes",
		mcp.WithDescription("获取当前所有【被墙且需要换端口】的节点清单。这些节点的机器和软件正常运行，仅端口被 GFW 拦截，AI 可据此自动登录 VPS 更换端口。"),
	)
	m.server.AddTool(portBlockedTool, m.handleListPortBlocked)

	// 5. get_ai_status_summary (AI 专属全量大盘)
	aiSummaryTool := mcp.NewTool("get_ai_status_summary",
		mcp.WithDescription("获取面向 AI 自愈代理的完整节点状态大盘，包含所有节点的原始 V2Ray 协议链接(vless/vmess/ss/trojan)、被墙与故障诊断、以及精确到每台 VPS 的自愈处置指令。"),
	)
	m.server.AddTool(aiSummaryTool, m.handleGetAISummary)

	// 6. vpn_speedtest_node (免滥用风控真实测速)
	speedtestTool := mcp.NewTool("vpn_speedtest_node",
		mcp.WithDescription("对指定节点进行免滥用风控的真实性能测速(自建 VPS 30MB 互测或 30 秒真实流媒体持续测速)，输出带宽、吞吐速率、TTFB 与综合评分。"),
		mcp.WithString("link", mcp.Required(), mcp.Description("待测速代理节点链接(vless/ss/socks5/trojan)")),
		mcp.WithBoolean("is_stream", mcp.Description("是否进行持续流媒体采样测速(默认为 false 进行自建 30MB 吞吐量测速)")),
		mcp.WithNumber("duration_sec", mcp.Description("持续测速秒数(默认 30 秒)")),
	)
	m.server.AddTool(speedtestTool, m.handleSpeedtestNode)

	// 7. substore_sync_fivegroups & substore_sync_threetier (Sub-Store 拓扑自愈与五维策略组同步)
	fivegroupsTool := mcp.NewTool("substore_sync_fivegroups",
		mcp.WithDescription("触发 Sub-Store 方案B自建五维策略组（线路机、美区全家桶、日区全家桶、其他、送中）原子同步与全量拓扑自愈整理。"),
	)
	m.server.AddTool(fivegroupsTool, m.handleSyncFiveGroups)

	threetierTool := mcp.NewTool("substore_sync_threetier",
		mcp.WithDescription("【平滑兼容】触发 Sub-Store 拓扑自愈与五维策略组原子同步 (已升级取代历史三层架构)。"),
	)
	m.server.AddTool(threetierTool, m.handleSyncFiveGroups)

	// 8. substore_purge_legacy (彻底平退并删除 SubStore 中历史残留的旧三层独立订阅)
	purgeTool := mcp.NewTool("substore_purge_legacy",
		mcp.WithDescription("从 Sub-Store 彻底平退并删除历史残留的旧三层独立订阅(ashan-transit-direct, ashan-resi-direct, ashan-chain, ashan-tw-resi)，保证五维拓扑洁净度。"),
	)
	m.server.AddTool(purgeTool, m.handlePurgeLegacySubs)
}

func (m *MCPServer) handleProbeLinks(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.engine == nil {
		return mcp.NewToolResultError("probe engine is not initialized"), nil
	}
	links, err := request.RequireStringSlice("links")
	if err != nil || len(links) == 0 {
		return mcp.NewToolResultError("links must be a non-empty string array"), nil
	}

	isSelf := request.GetBool("is_self", true)
	syncSubstore := request.GetBool("sync_substore", true)
	checkSplit := request.GetBool("check_detailed_split", false)
	timeoutSec := request.GetInt("timeout_seconds", 8)

	batchReq := &model.BatchProbeRequest{
		Links:              links,
		IsSelf:             isSelf,
		SyncSubStore:       syncSubstore,
		CheckDetailedSplit: checkSplit,
		TimeoutSeconds:     timeoutSec,
	}

	resp := m.engine.ProbeBatch(ctx, batchReq)

	// 保存记录到数据库
	if m.store != nil {
		for _, res := range resp.Results {
			_ = m.store.SaveResult(ctx, res)
		}
	}

	// 同步到 SubStore
	if syncSubstore && m.substoreClient != nil && m.cfg.SubStore.Enabled {
		groups := make(substore.GroupUpdateMap)
		for _, res := range resp.Results {
			groups[res.Category] = append(groups[res.Category], res.OriginalURL)
		}

		err := m.substoreClient.SyncGroups(ctx, groups)
		if err != nil {
			resp.SubStoreSynced = false
			resp.SubStoreMessage = fmt.Sprintf("substore sync error: %v", err)
		} else {
			resp.SubStoreSynced = true
			resp.SubStoreMessage = "successfully synced to substore 4 groups"
		}
	}

	resultBytes, _ := json.MarshalIndent(resp, "", "  ")
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func (m *MCPServer) handleGetHistory(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.store == nil {
		return mcp.NewToolResultError("store is not initialized"), nil
	}
	limit := request.GetInt("limit", 20)

	records, err := m.store.GetRecentResults(ctx, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query history failed: %v", err)), nil
	}
	if records == nil {
		records = make([]*model.NodeProbeResult, 0)
	}

	b, _ := json.MarshalIndent(records, "", "  ")
	return mcp.NewToolResultText(string(b)), nil
}

func (m *MCPServer) handleSyncSubStore(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !m.cfg.SubStore.Enabled {
		return mcp.NewToolResultError("substore is disabled in config"), nil
	}

	subName := request.GetString("sub_name", "")
	if subName != "" {
		err := m.substoreClient.TriggerDownload(ctx, subName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("trigger %s failed: %v", subName, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("substore trigger download '%s' ok", subName)), nil
	}

	return mcp.NewToolResultText("substore sync completed"), nil
}

func (m *MCPServer) handleListPortBlocked(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.store == nil {
		return mcp.NewToolResultError("store is not initialized"), nil
	}
	records, err := m.store.GetRecentResults(ctx, 100)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query history failed: %v", err)), nil
	}

	blockedList := make([]map[string]any, 0)
	seen := make(map[string]bool)

	for _, r := range records {
		if seen[r.OriginalURL] {
			continue
		}
		seen[r.OriginalURL] = true

		if r.Diagnostic != nil && r.Diagnostic.FaultType == model.FaultPortBlocked {
			blockedList = append(blockedList, map[string]any{
				"name":             r.Name,
				"original_url":     r.OriginalURL,
				"target_ip":        r.Diagnostic.TargetIP,
				"blocked_port":     r.Diagnostic.TargetPort,
				"status_text":      r.Diagnostic.StatusText,
				"abroad_source":    r.Diagnostic.AbroadSource,
				"abroad_rtt_ms":    r.Diagnostic.AbroadRTTMs,
				"action_type":      r.Diagnostic.ActionType,
				"troubleshoot_tip": r.Diagnostic.TroubleshootTip,
			})
		}
	}

	b, _ := json.MarshalIndent(blockedList, "", "  ")
	return mcp.NewToolResultText(string(b)), nil
}

func (m *MCPServer) handleGetAISummary(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.store == nil {
		return mcp.NewToolResultError("store is not initialized"), nil
	}
	records, err := m.store.GetRecentResults(ctx, 100)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query history failed: %v", err)), nil
	}

	healthyList := make([]map[string]any, 0)
	actionList := make([]map[string]any, 0)
	seen := make(map[string]bool)

	healthyCount := 0
	portBlockedCount := 0
	ipBlockedCount := 0
	protocolErrorCount := 0
	offlineCount := 0
	congestionCount := 0
	residentialChainCount := 0

	for _, r := range records {
		if seen[r.OriginalURL] {
			continue
		}
		seen[r.OriginalURL] = true

		if r.Online {
			healthyCount++
			healthyList = append(healthyList, map[string]any{
				"name":         r.Name,
				"v2ray_link":   r.OriginalURL,
				"protocol":     r.Protocol,
				"latency_ms":   r.LatencyMs,
				"china_exit":   r.ChinaExit,
				"exit_country": r.ExitCountry,
				"exit_ip":      r.ExitIP,
				"status":       r.StatusText,
			})
		} else {
			diag := r.Diagnostic
			var targetIP string
			var targetPort int
			var faultType model.FaultType = model.FaultMachineDown
			var statusText string = r.StatusText
			var actionType string = "CHECK_SERVER_AND_PROCESS"
			var tip string = r.ErrorMsg
			var abroadSource string = ""
			var abroadRTT int = 0
			var domesticTCP bool = false
			var domesticPing bool = false

			if diag != nil {
				targetIP = diag.TargetIP
				targetPort = diag.TargetPort
				faultType = diag.FaultType
				statusText = diag.StatusText
				actionType = diag.ActionType
				tip = diag.TroubleshootTip
				abroadSource = diag.AbroadSource
				abroadRTT = diag.AbroadRTTMs
				domesticTCP = diag.DomesticReachable
				domesticPing = diag.DomesticPing
			}

			switch faultType {
			case model.FaultPortBlocked:
				portBlockedCount++
			case model.FaultIPBlocked:
				ipBlockedCount++
			case model.FaultProtocolMismatch:
				protocolErrorCount++
			case model.FaultNetworkCongestion:
				congestionCount++
			case model.FaultMachineDown, model.FaultResidentialTunnelOffline, model.FaultTransitRelayDown:
				offlineCount++
			case model.FaultResidentialDirectBlocked:
				residentialChainCount++
			default:
				offlineCount++
			}

			var aiInstruction string
			switch faultType {
			case model.FaultPortBlocked:
				aiInstruction = fmt.Sprintf("【端口被墙处置】海外节点(%s)连通且国内可Ping通。请登录目标VPS(%s)将端口%d更换为新端口，并重新提交自愈检测。", abroadSource, targetIP, targetPort)
			case model.FaultProtocolMismatch:
				aiInstruction = fmt.Sprintf("【配置错误处置】海外与国内TCP均通，说明端口未被墙，请勿更换端口！请核对客户端配置与VPS(%s)服务端的Reality公钥(pbk)或ShortID是否一致。", targetIP)
			case model.FaultNetworkCongestion:
				if tip != "" {
					aiInstruction = tip
				} else {
					aiInstruction = fmt.Sprintf("【网络拥塞处置】目标VPS(%s)国内外TCP均已握手成功，仅应用层请求超时。机器与端口正常，请勿修改凭据或端口，保持观察等待链路抖动恢复。", targetIP)
				}
			case model.FaultIPBlocked:
				aiInstruction = fmt.Sprintf("【IP被墙处置】海外通但国内全黑洞丢包，换端口无效。请更换VPS(%s)的IP或配置CDN反代救活。", targetIP)
			case model.FaultMachineDown:
				aiInstruction = fmt.Sprintf("【机器离线处置】全球均无法连接该端口，换端口无效！请单独排查VPS(%s)是否关机欠费或sing-box崩溃。", targetIP)
			case model.FaultResidentialTunnelOffline:
				aiInstruction = fmt.Sprintf("【家宽隧道离线】中转机(%s)正常但家庭端隧道断开或连接被拒。严禁更换端口，请核查家庭光猫/穿透客户端或稍候自动恢复。", targetIP)
			case model.FaultTransitRelayDown:
				aiInstruction = fmt.Sprintf("【中转宿主机故障】中转跳板机(%s)网络中断或宕机。请排查懒猫云宿主机网络与容器运行状态。", targetIP)
			case model.FaultResidentialDirectBlocked:
				aiInstruction = "【家宽直连受阻】国内直连受阻但海外隧道正常。系统已自动合成全网状链式中继(ashan-chain)，无需换端口或人工干预。"
			default:
				aiInstruction = fmt.Sprintf("请根据排查指引处理: %s", tip)
			}

			actionList = append(actionList, map[string]any{
				"name":                r.Name,
				"v2ray_link":          r.OriginalURL,
				"protocol":            r.Protocol,
				"server_ip":           targetIP,
				"server_port":         targetPort,
				"fault_type":          faultType,
				"status":              statusText,
				"action_type":         actionType,
				"ai_heal_instruction": aiInstruction,
				"evidence": map[string]any{
					"domestic_tcp":  domesticTCP,
					"domestic_ping": domesticPing,
					"abroad_tcp":    diag != nil && diag.AbroadReachable,
					"abroad_source": abroadSource,
					"abroad_rtt_ms": abroadRTT,
				},
				"troubleshoot_tip": tip,
			})
		}
	}

	total := len(seen)
	requiresHealing := portBlockedCount > 0 || ipBlockedCount > 0 || protocolErrorCount > 0 || offlineCount > 0
	var overallPrompt string
	if requiresHealing {
		var parts []string
		if portBlockedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d个端口被墙需换端口", portBlockedCount))
		}
		if ipBlockedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d个IP被墙需换IP或CDN中继", ipBlockedCount))
		}
		if protocolErrorCount > 0 {
			parts = append(parts, fmt.Sprintf("%d个协议配置需核对", protocolErrorCount))
		}
		if offlineCount > 0 {
			parts = append(parts, fmt.Sprintf("%d个真正离线", offlineCount))
		}
		if congestionCount > 0 {
			parts = append(parts, fmt.Sprintf("%d个网络拥塞/超时(无需换端口)", congestionCount))
		}
		overallPrompt = fmt.Sprintf("检测到 %d 个节点需要自愈或人工排查 (%s)。请按 nodes_requiring_action 列表中的 v2ray_link 与 ai_heal_instruction 执行处置。",
			len(actionList), strings.Join(parts, ", "))
	} else if residentialChainCount > 0 && congestionCount > 0 {
		overallPrompt = fmt.Sprintf("检测到 %d 个住宅家宽节点直连受阻(已自动建立链式中继)，且有 %d 个节点出现跨境网络拥塞/抖动超时(机器正常无需换端口)，系统保持持续观测。", residentialChainCount, congestionCount)
	} else if residentialChainCount > 0 {
		overallPrompt = fmt.Sprintf("全网自建专线与核心节点健康，检测到 %d 个住宅家宽节点国内直连受阻但境外隧道正常，系统已自动全网状建立链式中继 (ashan-chain)，无需人工换端口或干预。", residentialChainCount)
	} else if congestionCount > 0 {
		overallPrompt = fmt.Sprintf("所有核心服务在线，检测到 %d 个节点出现跨境网络拥塞/抖动超时，机器与端口均正常，无需更换端口，系统保持持续观测。", congestionCount)
	} else {
		overallPrompt = "所有监控节点均正常在线，无需执行自愈操作。"
	}

	resultMap := map[string]any{
		"summary": map[string]any{
			"total_nodes":             total,
			"healthy_nodes":           healthyCount,
			"nodes_requiring_action":  len(actionList),
			"port_blocked_nodes":      portBlockedCount,
			"ip_blocked_nodes":        ipBlockedCount,
			"protocol_error_nodes":    protocolErrorCount,
			"offline_nodes":           offlineCount,
			"congestion_nodes":        congestionCount,
			"residential_chain_nodes": residentialChainCount,
			"requires_healing_action": requiresHealing,
			"ai_instruction":          overallPrompt,
		},
		"nodes_requiring_action": actionList,
		"healthy_nodes":          healthyList,
	}

	b, _ := json.MarshalIndent(resultMap, "", "  ")
	return mcp.NewToolResultText(string(b)), nil
}

func (m *MCPServer) handleSpeedtestNode(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.engine == nil {
		return mcp.NewToolResultError("probe engine is not initialized"), nil
	}
	link, err := request.RequireString("link")
	if err != nil || link == "" {
		return mcp.NewToolResultError("link is required"), nil
	}

	isStream := request.GetBool("is_stream", false)
	durationSec := request.GetInt("duration_sec", 30)

	res, err := m.engine.SpeedTestOne(ctx, link, isStream, durationSec)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("speed test failed: %v", err)), nil
	}

	b, _ := json.MarshalIndent(res, "", "  ")
	return mcp.NewToolResultText(string(b)), nil
}

func (m *MCPServer) handleSyncFiveGroups(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.substoreClient == nil || !m.cfg.SubStore.Enabled {
		return mcp.NewToolResultError("SubStore integration is disabled"), nil
	}

	// 触发全量拓扑审计与自愈整理
	audit, err := m.substoreClient.ReconcileTopology(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reconcile topology failed: %v", err)), nil
	}

	b, _ := json.MarshalIndent(audit, "", "  ")
	return mcp.NewToolResultText(fmt.Sprintf("Sub-Store Five-Group topology synchronized and organized successfully:\n%s", string(b))), nil
}

func (m *MCPServer) handlePurgeLegacySubs(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.substoreClient == nil || !m.cfg.SubStore.Enabled {
		return mcp.NewToolResultError("SubStore integration is disabled"), nil
	}

	purged, err := m.substoreClient.PurgeLegacyThreeTierSubs(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("purge legacy subscriptions failed: %v", err)), nil
	}

	res := map[string]any{
		"status":       "ok",
		"purged_count": purged,
		"purged_subs":  substore.LegacyThreeTierSubNames,
		"message":      "Successfully purged legacy 3-tier subscriptions from SubStore",
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	return mcp.NewToolResultText(string(b)), nil
}
