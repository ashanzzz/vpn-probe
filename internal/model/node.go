package model

import (
	"strings"
	"time"
)

// Category 分类标签定义
type Category string

const (
	CategorySelfOnline        Category = "在线自有服务"
	CategorySelfOffline       Category = "离线自有服务"
	CategorySelfOnlineChina   Category = "在线自有服务送中"
	CategorySelfOnlineClean   Category = "在线自有服务不送中"
	CategoryExtOnlineClean    Category = "不送中"
	CategoryExtOnlineChina    Category = "送中"
	CategoryExtOffline        Category = "离线"
)

// FaultType 离线根因与被墙类型枚举
type FaultType string

const (
	FaultNone                      FaultType = "none"                       // 正常
	FaultPortBlocked               FaultType = "port_blocked"               // 端口被墙 (机器软件正常，国内阻断)
	FaultIPBlocked                 FaultType = "ip_blocked"                 // IP被墙 (机器软件正常，整机阻断)
	FaultProtocolMismatch          FaultType = "protocol_mismatch"          // 协议认证失败 (端口通但UUID/Reality验证失败)
	FaultMachineDown               FaultType = "machine_offline"            // 真正离线 (全球不通: 机子关机/软件坏了/线路断)
	FaultResidentialTunnelOffline  FaultType = "residential_tunnel_offline"  // 家宽穿透隧道掉线/家庭设备离线 (Issue #26)
	FaultTransitRelayDown          FaultType = "transit_relay_down"          // 家宽中转宿主机宕机/网络中断 (Issue #26)
	FaultResidentialDirectBlocked  FaultType = "residential_direct_blocked"  // 家宽国内直连受阻(境外隧道正常，需中继) (Issue #26)
)

// DiagnosticInfo 深度故障与被墙诊断信息
type DiagnosticInfo struct {
	FaultType         FaultType `json:"fault_type"`
	StatusText        string    `json:"status_text"`        // "33338端口被墙" | "IP被墙" | "【离线】机子关机/软件崩溃/线路中断"
	TargetIP          string    `json:"target_ip"`
	TargetPort        int       `json:"target_port"`
	AbroadReachable   bool      `json:"abroad_reachable"`             // 海外探针是否畅通
	AbroadSource      string    `json:"abroad_source,omitempty"`      // 探针来源 (如 "peer:JP-Tokyo-Softbank-SS2022" 或 "public:check-host.net")
	AbroadRTTMs       int       `json:"abroad_rtt_ms,omitempty"`      // 海外探测耗时 (毫秒)
	DomesticReachable bool      `json:"domestic_reachable"`           // 国内是否连通
	DomesticPing      bool      `json:"domestic_ping"`                // 国内是否可 Ping 通
	ActionType        string    `json:"action_type"`                  // AI_CHANGE_PORT | AI_RESTART_SERVICE | CHANGE_IP
	TroubleshootTip   string    `json:"troubleshoot_tip"`             // 排查与处置指引
}

// SplitInfo 拓展分流探测结果（供人类/AI展示）
type SplitInfo struct {
	ChatGPT  string `json:"chatgpt,omitempty"`   // e.g. "ok (200)", "blocked (403)", "timeout"
	Claude   string `json:"claude,omitempty"`    // e.g. "ok (200)", "blocked (403)", "timeout"
	YouTube  string `json:"youtube,omitempty"`   // e.g. "ok (204)", "timeout"
	Baidu    string `json:"baidu,omitempty"`     // 国内连通性 / 直连参考
}

// SpeedResult 真实测速结果 (VPS 30MB 互测或 30s 流媒体测试)
type SpeedResult struct {
	Method      string    `json:"method"`                 // "vps_30m" | "stream_30s"
	TestedAt    time.Time `json:"tested_at"`
	DurationSec float64   `json:"duration_sec"`           // 耗时(秒)
	TotalBytes  int64     `json:"total_bytes"`            // 传输字节数
	SpeedMBps   float64   `json:"speed_mb_s"`             // 传输速率 (MB/s)
	SpeedMbps   float64   `json:"speed_mbps"`             // 传输速率 (Mbps)
	PeakMbps    float64   `json:"peak_mbps,omitempty"`     // 峰值速率 (Mbps)
	AvgMbps     float64   `json:"avg_mbps,omitempty"`      // 平均速率 (Mbps)
	TTFBMs      int       `json:"ttfb_ms,omitempty"`       // 首包/首字节延迟 (毫秒)
	Score       float64   `json:"score"`                  // 综合加权评分 (0~100)
}

// NodeProbeResult 单个节点的探测结果
type NodeProbeResult struct {
	ID          string          `json:"id"`
	OriginalURL string          `json:"original_url"`
	Protocol    string          `json:"protocol"`      // vmess, vless, ss, trojan, singbox, socks5
	Name        string          `json:"name"`          // 节点备注名
	NodeType    string          `json:"node_type,omitempty"` // "transit" | "residential" | "cf_ws"
	IsSelf      bool            `json:"is_self"`       // 是否为自有服务
	Online      bool            `json:"online"`        // 是否在线
	LatencyMs   int             `json:"latency_ms"`    // 延迟 (毫秒)
	ChinaExit   bool            `json:"china_exit"`    // 是否送中
	ExitIP      string          `json:"exit_ip"`       // 出口 IP
	ExitCountry string          `json:"exit_country"`  // 出口国家 (CN, US, JP...)
	Category    Category        `json:"category"`      // 最终判定类别
	StatusText  string          `json:"status_text,omitempty"` // 规范化展示文案 ("33338端口被墙" / "【离线】机子关机/软件崩溃/线路中断")
	ErrorMsg      string          `json:"error_msg,omitempty"`
	OfflineReason string          `json:"offline_reason,omitempty"` // "probe_engine_error" | "node_down"
	HealedURL     string          `json:"healed_url,omitempty"`     // 自愈生成的安全候选 URL (不直接覆盖 OriginalURL)
	Diagnostic    *DiagnosticInfo `json:"diagnostic,omitempty"`
	Split       *SplitInfo      `json:"split,omitempty"`
	Speed       *SpeedResult    `json:"speed,omitempty"`
	Score       float64         `json:"score,omitempty"` // 综合评分 (由测速+延迟+稳定性计算)
	TestedAt    time.Time       `json:"tested_at"`
}

// BatchProbeRequest 批量探测请求
type BatchProbeRequest struct {
	Links              []string `json:"links"`
	IsSelf             bool     `json:"is_self"`
	SyncSubStore       bool     `json:"sync_substore"`
	CheckDetailedSplit bool     `json:"check_detailed_split"`
	TimeoutSeconds     int      `json:"timeout_seconds,omitempty"`
	Concurrency        int      `json:"concurrency,omitempty"`
}

// BatchProbeResponse 批量探测响应
type BatchProbeResponse struct {
	Total     int                 `json:"total"`
	Online       int                 `json:"online"`
	Offline      int                 `json:"offline"`
	EngineErrors int                 `json:"engine_errors,omitempty"`
	ChinaExit int                 `json:"china_exit"`
	CleanExit int                 `json:"clean_exit"`
	Results   []*NodeProbeResult  `json:"results"`
	GroupCounts map[Category]int  `json:"group_counts"`
	SubStoreSynced bool           `json:"substore_synced"`
	SubStoreMessage string        `json:"substore_message,omitempty"`
}

func matchCountryToken(s, code string) bool {
	return strings.HasPrefix(s, code+"-") ||
		strings.Contains(s, "-"+code+"-") ||
		strings.HasSuffix(s, "-"+code) ||
		s == code
}

// ResolveCountry 通用解析节点的所属国家/地区 ISO 二位大写代码 (如 US, JP, HK, SG, DE)
// 严禁绑定具体机房名或运营商名 (4837 仅为美区线路机举例，非硬编码标识)，严格依据真实探测出口或规范国别标签
func ResolveCountry(exitCountry, nodeName string) string {
	c := strings.ToUpper(strings.TrimSpace(exitCountry))
	if c != "" && len(c) == 2 {
		return c
	}

	// 若为链式中继 (如 "链式|线路机 ➔ 家宽机")，其出口国家必须以落地的目标家宽机为准
	if idx := strings.LastIndex(nodeName, "➔"); idx != -1 {
		nodeName = nodeName[idx+len("➔"):]
	} else if idx := strings.LastIndex(nodeName, "%E2%9E%94"); idx != -1 {
		nodeName = nodeName[idx+len("%E2%9E%94"):]
	}

	upper := strings.ToUpper(nodeName)

	// 针对紧凑型链式代理命名 [前置]-[落地区域]-[运营商] (如 SB-美宽-Pavlov, LAX-日宽-SoftBank, SB-台宽-HiNet)，
	// 必须优先以落地家宽区域标识为准，彻底防止前置机前缀 (如 LAX/美西) 误导国别解析
	if strings.Contains(upper, "美宽") {
		return "US"
	}
	if strings.Contains(upper, "日宽") {
		return "JP"
	}
	if strings.Contains(upper, "台宽") {
		return "TW"
	}
	if strings.Contains(upper, "韩宽") {
		return "KR"
	}
	if strings.Contains(upper, "新宽") {
		return "SG"
	}

	switch {
	case matchCountryToken(upper, "US") || strings.Contains(upper, "美国") || strings.Contains(upper, "美西") || strings.Contains(upper, "美东") || strings.Contains(upper, "USA") || strings.Contains(upper, "UNITED STATES"):
		return "US"
	case matchCountryToken(upper, "JP") || strings.Contains(upper, "日本") || strings.Contains(upper, "东京") || strings.Contains(upper, "大阪") || strings.Contains(upper, "JAPAN"):
		return "JP"
	case matchCountryToken(upper, "HK") || strings.Contains(upper, "香港") || strings.Contains(upper, "HONG KONG"):
		return "HK"
	case matchCountryToken(upper, "SG") || strings.Contains(upper, "新加坡") || strings.Contains(upper, "SINGAPORE"):
		return "SG"
	case matchCountryToken(upper, "TW") || strings.Contains(upper, "台湾") || strings.Contains(upper, "TAIWAN"):
		return "TW"
	case matchCountryToken(upper, "KR") || strings.Contains(upper, "韩国") || strings.Contains(upper, "首尔") || strings.Contains(upper, "KOREA"):
		return "KR"
	case matchCountryToken(upper, "DE") || strings.Contains(upper, "德国") || strings.Contains(upper, "GERMANY") || strings.Contains(upper, "法兰克福"):
		return "DE"
	case matchCountryToken(upper, "GB") || matchCountryToken(upper, "UK") || strings.Contains(upper, "英国") || strings.Contains(upper, "伦敦"):
		return "GB"
	default:
		return "OTHER"
	}
}
