package model

// TopologyOverviewResponse 全局四阶网络拓扑看板数据
type TopologyOverviewResponse struct {
	GeneratedAt int64                   `json:"generated_at"`
	Summary     TopologySummary         `json:"summary"`     // 统计概览
	Transits    []*TopologyTransitNode  `json:"transits"`    // Stage 2: 自建专线线路机
	NATRelays   []*TopologyNATRelayNode `json:"nat_relays"`  // Stage 3: 多区域 NAT 中转宿主 (JP/US/HK等)
	Landings    []*TopologyLandingNode  `json:"landings"`    // Stage 4: 最终落地家宽节点
	Links       []*TopologyLinkEdge     `json:"links"`       // 跨阶有向连线
}

// TopologySummary 拓扑看板统计指标
type TopologySummary struct {
	TotalLandings   int `json:"total_landings"`   // 家宽落地总数
	TotalNATRelays  int `json:"total_nat_relays"`  // NAT中转机数
	TotalTransits   int `json:"total_transits"`   // 自建专线机数
	TotalPorts      int `json:"total_ports"`      // 映射端口总数
	OnlineLinks     int `json:"online_links"`     // 通畅链路数
	FaultLinks      int `json:"fault_links"`      // 中断/故障链路数
}

// TopologyTransitNode Stage 2: 专线线路机
type TopologyTransitNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Country   string `json:"country"`
	Online    bool   `json:"online"`
	LatencyMs int    `json:"latency_ms"`
}

// TopologyNATRelayNode Stage 3: 多区 NAT 中转服务器 (宿主机)
type TopologyNATRelayNode struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Region   string                 `json:"region"`    // JP, US, HK, SG 等 (支持动态切换)
	PublicIP string                 `json:"public_ip"` // NAT 宿主公网 IP (如 167.104.167.151)
	Online   bool                   `json:"online"`
	Ports    []*TopologyNATPortItem `json:"ports"`     // 该宿主纳管的映射端口池
}

// TopologyNATPortItem 单个 NAT 映射端口状态
type TopologyNATPortItem struct {
	Port        int    `json:"port"`             // 外部映射端口 (如 41360)
	Region      string `json:"region,omitempty"` // 对应家宽落地国家代码 (如 US, JP, TW)
	TargetProto string `json:"proto"`            // vless, ss, trojan
	TargetNode  string `json:"target_node"`      // 对应目标家宽标签
	Online      bool   `json:"online"`
	LatencyMs   int    `json:"latency_ms"`
}

// TopologyLandingNode Stage 4: 最终落地家宽节点
type TopologyLandingNode struct {
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	ExitIP    string `json:"exit_ip"`
	Country   string `json:"country"`
	City      string `json:"city"`
	ISP       string `json:"isp"`
	IPType    string `json:"ip_type"`   // 家宽 / 机房
	Online    bool   `json:"online"`
	ChinaExit bool   `json:"china_exit"`
}

// TopologyLinkEdge 跨阶有向连线
type TopologyLinkEdge struct {
	ID        string `json:"id"`
	SourceID  string `json:"source_id"`
	TargetID  string `json:"target_id"`
	StageFrom int    `json:"stage_from"` // 1: Client, 2: Transit, 3: NAT, 4: Landing
	StageTo   int    `json:"stage_to"`
	Port      int    `json:"port,omitempty"`
	Proto     string `json:"proto,omitempty"`
	LatencyMs int    `json:"latency_ms"`
	Status    string `json:"status"` // "ok", "fault", "warn"
}

// TopologyHopTrace 单节点微观 4 阶路由追踪
type TopologyHopTrace struct {
	HopIndex  int    `json:"hop_index"`
	Title     string `json:"title"` // 客户端 -> 线路机 -> NAT中转(:41360) -> 家宽出口
	NodeHost  string `json:"node_host"`
	NodePort  int    `json:"node_port,omitempty"`
	Region    string `json:"region"`
	LatencyMs int    `json:"latency_ms"`
	Online    bool   `json:"online"`
	ErrorMsg  string `json:"error_msg,omitempty"`
}
