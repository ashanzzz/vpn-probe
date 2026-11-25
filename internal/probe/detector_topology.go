package probe

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"vpn-probe/internal/model"
)

// BuildNodeHopTrace 解析单个节点的四阶微观路由链路
func BuildNodeHopTrace(result *model.NodeProbeResult, parsed *ParsedNode, rawURL string) []*model.TopologyHopTrace {
	var traces []*model.TopologyHopTrace

	// 1. Hop 1: 客户端 / 探针发起端
	traces = append(traces, &model.TopologyHopTrace{
		HopIndex:  1,
		Title:     "客户端接入",
		NodeHost:  "本地客户端 / 探针宿主",
		Region:    "LOCAL",
		Online:    true,
		LatencyMs: 0,
	})

	u, err := url.Parse(rawURL)
	var serverHost string
	var serverPort int
	if err == nil {
		serverHost = u.Hostname()
		if p, perr := strconv.Atoi(u.Port()); perr == nil {
			serverPort = p
		}
	}
	if serverHost == "" && parsed != nil && parsed.RawOutbound != nil {
		serverHost, _ = parsed.RawOutbound["server"].(string)
		serverPort, _ = parsed.RawOutbound["server_port"].(int)
	}

	rawTag := result.Name
	isChain := strings.Contains(rawTag, "➔") || strings.Contains(rawTag, "链式") || (u != nil && u.Query().Get("dialerProxy") != "")

	var transitName string
	var landingTag string = rawTag
	if isChain {
		if idx := strings.Index(rawTag, "➔"); idx != -1 {
			transitName = strings.TrimSpace(rawTag[:idx])
			landingTag = strings.TrimSpace(rawTag[idx+len("➔"):])
		} else if u != nil && u.Query().Get("dialerProxy") != "" {
			transitName = u.Query().Get("dialerProxy")
		} else {
			transitName = "自建前置专线线路机"
		}
		transitName = strings.TrimPrefix(transitName, "链式|")
		transitName = strings.TrimSpace(transitName)
	}

	// 2. Hop 2 (如果为链式代理): 自建专线线路机
	if isChain {
		transitLatency := result.LatencyMs / 3
		if transitLatency < 10 && result.LatencyMs > 0 {
			transitLatency = 15
		}
		traces = append(traces, &model.TopologyHopTrace{
			HopIndex:  2,
			Title:     "自建专线线路机",
			NodeHost:  transitName,
			Region:    DetectRegionFromName(transitName, "TRANSIT"),
			Online:    result.Online,
			LatencyMs: transitLatency,
		})
	}

	// 3. Hop 3: 懒猫云 / 外部 NAT 中转宿主机与端口
	natRegion := DetectNATRegion(serverHost, landingTag)
	natTitle := fmt.Sprintf("NAT 中转机 (%s:%d)", serverHost, serverPort)
	if serverPort == 0 {
		natTitle = fmt.Sprintf("NAT 中转宿主 (%s)", serverHost)
	}
	natLatency := result.LatencyMs
	if isChain && result.LatencyMs > 20 {
		natLatency = (result.LatencyMs * 2) / 3
	}

	traces = append(traces, &model.TopologyHopTrace{
		HopIndex:  len(traces) + 1,
		Title:     natTitle,
		NodeHost:  serverHost,
		NodePort:  serverPort,
		Region:    natRegion,
		Online:    result.Online,
		LatencyMs: natLatency,
	})

	// 4. Hop 4: 物理家宽出口 IP (落地出口)
	exitTitle := "真实家宽落地出口"
	if result.ExitIPType != "" {
		exitTitle = fmt.Sprintf("落地出口 [%s]", result.ExitIPType)
	}
	exitHost := result.ExitIP
	if exitHost == "" {
		exitHost = "出口探测中..."
	}
	if result.ExitCity != "" {
		exitHost = fmt.Sprintf("%s (%s, %s)", result.ExitIP, result.ExitCity, result.ExitCountry)
	}

	traces = append(traces, &model.TopologyHopTrace{
		HopIndex:  len(traces) + 1,
		Title:     exitTitle,
		NodeHost:  exitHost,
		Region:    result.ExitCountry,
		Online:    result.Online,
		LatencyMs: result.LatencyMs,
	})

	return traces
}

// hasRegionToken 判断节点名称中是否包含作为独立词元出现的区域代号 (委托给 model.HasRegionToken)
func hasRegionToken(upper, code string) bool {
	return model.HasRegionToken(upper, code)
}

// DetectRegionFromName 根据节点名推导区域代码 (JP, US, HK, TW, SG, KR, DE, UK...)
func DetectRegionFromName(name, fallback string) string {
	reg := model.ResolveCountry("", name)
	if reg != "OTHER" && reg != "" {
		return reg
	}
	return fallback
}

// DetectNATRegion 识别 NAT 宿主所在区域 (支持 JP / US / HK / SG 等动态区域切换)
func DetectNATRegion(host, tag string) string {
	if reg := DetectRegionFromName(tag, ""); reg != "" {
		return reg
	}
	// 懒猫云常见中转机 IP 启发式判定
	if strings.HasPrefix(host, "167.104.") || strings.HasPrefix(host, "156.231.") {
		return "JP"
	}
	if strings.HasPrefix(host, "45.192.") || strings.HasPrefix(host, "47.141.") {
		return "US"
	}
	return "JP" // 默认中转区
}

// BuildTopologyOverview 将全量探测结果与节点清单聚合成四阶拓扑视图模型
func BuildTopologyOverview(results []*model.NodeProbeResult) *model.TopologyOverviewResponse {
	resp := &model.TopologyOverviewResponse{
		GeneratedAt: time.Now().Unix(),
		Transits:    make([]*model.TopologyTransitNode, 0),
		NATRelays:   make([]*model.TopologyNATRelayNode, 0),
		Landings:    make([]*model.TopologyLandingNode, 0),
		Links:       make([]*model.TopologyLinkEdge, 0),
	}

	transitMap := make(map[string]*model.TopologyTransitNode)
	natMap := make(map[string]*model.TopologyNATRelayNode) // key: publicIP
	landingMap := make(map[string]*model.TopologyLandingNode)
	linkMap := make(map[string]bool)

	summary := model.TopologySummary{}

	for _, res := range results {
		if res == nil {
			continue
		}

		u, _ := url.Parse(res.OriginalURL)
		var serverHost string
		var serverPort int
		if u != nil {
			serverHost = u.Hostname()
			if p, perr := strconv.Atoi(u.Port()); perr == nil {
				serverPort = p
			}
		}

		rawTag := res.Name
		isChain := strings.Contains(rawTag, "➔") || strings.Contains(rawTag, "链式") || (u != nil && u.Query().Get("dialerProxy") != "")

		var transitName string
		var landingTag string = rawTag
		if isChain {
			if idx := strings.Index(rawTag, "➔"); idx != -1 {
				transitName = strings.TrimSpace(rawTag[:idx])
				landingTag = strings.TrimSpace(rawTag[idx+len("➔"):])
			} else if u != nil && u.Query().Get("dialerProxy") != "" {
				transitName = u.Query().Get("dialerProxy")
			}
			transitName = strings.TrimPrefix(transitName, "链式|")
			transitName = strings.TrimSpace(transitName)
		}

		// Stage 2: 专线线路机
		if isChain && transitName != "" {
			if _, exists := transitMap[transitName]; !exists {
				tNode := &model.TopologyTransitNode{
					ID:        "transit-" + transitName,
					Name:      transitName,
					Host:      transitName,
					Country:   DetectRegionFromName(transitName, "US"),
					Online:    res.Online,
					LatencyMs: res.LatencyMs / 3,
				}
				transitMap[transitName] = tNode
			} else if res.Online {
				transitMap[transitName].Online = true
			}
		}

		// Stage 3: 多区 NAT 中转服务器
		natKey := serverHost
		if natKey == "" {
			natKey = "unknown-nat"
		}
		natRelay, exists := natMap[natKey]
		if !exists {
			natRelay = &model.TopologyNATRelayNode{
				ID:       "nat-" + natKey,
				Name:     fmt.Sprintf("懒猫云 NAT 中转 (%s)", DetectNATRegion(natKey, landingTag)),
				Region:   DetectNATRegion(natKey, landingTag),
				PublicIP: natKey,
				Online:   res.Online,
				Ports:    make([]*model.TopologyNATPortItem, 0),
			}
			natMap[natKey] = natRelay
		} else if res.Online {
			natRelay.Online = true
		}

		// 记录该 NAT 宿主的映射端口
		if serverPort > 0 {
			var portItem *model.TopologyNATPortItem
			for _, p := range natRelay.Ports {
				if p.Port == serverPort {
					portItem = p
					break
				}
			}
			if portItem == nil {
				portItem = &model.TopologyNATPortItem{
					Port:        serverPort,
					TargetProto: res.Protocol,
					TargetNode:  landingTag,
					Online:      res.Online,
					LatencyMs:   res.LatencyMs,
				}
				natRelay.Ports = append(natRelay.Ports, portItem)
				summary.TotalPorts++
			} else if res.Online {
				portItem.Online = true
			}
		}

		// Stage 4: 最终落地家宽
		exitKey := res.ExitIP
		if exitKey == "" {
			exitKey = landingTag
		}
		if _, exists := landingMap[exitKey]; !exists {
			ipType := res.ExitIPType
			if ipType == "" {
				ipType = "家宽"
			}
			lNode := &model.TopologyLandingNode{
				ID:        "landing-" + exitKey,
				Tag:       landingTag,
				ExitIP:    res.ExitIP,
				Country:   res.ExitCountry,
				City:      res.ExitCity,
				ISP:       res.ExitISP,
				IPType:    ipType,
				Online:    res.Online,
				ChinaExit: res.ChinaExit,
			}
			landingMap[exitKey] = lNode
			summary.TotalLandings++
		} else if res.Online {
			landingMap[exitKey].Online = true
		}

		// 生成跨阶连线 Links
		linkStatus := "ok"
		if !res.Online {
			linkStatus = "fault"
		}

		if isChain && transitName != "" {
			// Link: Client (1) -> Transit (2)
			c2tID := "link-client-transit-" + transitName
			if !linkMap[c2tID] {
				resp.Links = append(resp.Links, &model.TopologyLinkEdge{
					ID:        c2tID,
					SourceID:  "client",
					TargetID:  "transit-" + transitName,
					StageFrom: 1,
					StageTo:   2,
					LatencyMs: res.LatencyMs / 3,
					Status:    linkStatus,
				})
				linkMap[c2tID] = true
			}

			// Link: Transit (2) -> NAT (3)
			t2nID := fmt.Sprintf("link-transit-%s-nat-%s-%d", transitName, natKey, serverPort)
			if !linkMap[t2nID] {
				resp.Links = append(resp.Links, &model.TopologyLinkEdge{
					ID:        t2nID,
					SourceID:  "transit-" + transitName,
					TargetID:  "nat-" + natKey,
					StageFrom: 2,
					StageTo:   3,
					Port:      serverPort,
					Proto:     res.Protocol,
					LatencyMs: (res.LatencyMs * 2) / 3,
					Status:    linkStatus,
				})
				linkMap[t2nID] = true
			}
		} else {
			// Link: Client (1) -> NAT (3)
			c2nID := fmt.Sprintf("link-client-nat-%s-%d", natKey, serverPort)
			if !linkMap[c2nID] {
				resp.Links = append(resp.Links, &model.TopologyLinkEdge{
					ID:        c2nID,
					SourceID:  "client",
					TargetID:  "nat-" + natKey,
					StageFrom: 1,
					StageTo:   3,
					Port:      serverPort,
					Proto:     res.Protocol,
					LatencyMs: res.LatencyMs / 2,
					Status:    linkStatus,
				})
				linkMap[c2nID] = true
			}
		}

		// Link: NAT (3) -> Landing (4)
		n2lID := fmt.Sprintf("link-nat-%s-%d-landing-%s", natKey, serverPort, exitKey)
		if !linkMap[n2lID] {
			resp.Links = append(resp.Links, &model.TopologyLinkEdge{
				ID:        n2lID,
				SourceID:  "nat-" + natKey,
				TargetID:  "landing-" + exitKey,
				StageFrom: 3,
				StageTo:   4,
				Port:      serverPort,
				Proto:     res.Protocol,
				LatencyMs: res.LatencyMs,
				Status:    linkStatus,
			})
			linkMap[n2lID] = true
		}
	}

	for _, t := range transitMap {
		resp.Transits = append(resp.Transits, t)
	}
	for _, n := range natMap {
		resp.NATRelays = append(resp.NATRelays, n)
	}
	for _, l := range landingMap {
		resp.Landings = append(resp.Landings, l)
	}

	// 稳定排序: 杜绝 Go map 随机遍历导致的 API 响应抖动与前端重渲闪烁
	sort.Slice(resp.Transits, func(i, j int) bool { return resp.Transits[i].ID < resp.Transits[j].ID })
	sort.Slice(resp.NATRelays, func(i, j int) bool { return resp.NATRelays[i].ID < resp.NATRelays[j].ID })
	for _, n := range resp.NATRelays {
		sort.Slice(n.Ports, func(i, j int) bool { return n.Ports[i].Port < n.Ports[j].Port })
	}
	sort.Slice(resp.Landings, func(i, j int) bool { return resp.Landings[i].ID < resp.Landings[j].ID })
	sort.Slice(resp.Links, func(i, j int) bool { return resp.Links[i].ID < resp.Links[j].ID })

	// 精准统计链路状态指标 (基于真实去重后的跨阶连线)
	summary.OnlineLinks = 0
	summary.FaultLinks = 0
	for _, l := range resp.Links {
		if l.Status == "ok" {
			summary.OnlineLinks++
		} else {
			summary.FaultLinks++
		}
	}

	summary.TotalTransits = len(resp.Transits)
	summary.TotalNATRelays = len(resp.NATRelays)
	resp.Summary = summary

	return resp
}
