package vpsmgr

import "time"

// VPSInstance VPS 节点基础配置
type VPSInstance struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	IP          string `json:"ip"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	SSHPassword string `json:"ssh_password"`
	ProviderID  string `json:"provider_id"`
	Status      string `json:"status"`
	Region      string `json:"region"`
}

// SoftwareInfo VPS 上运行的代理软件详情与生命周期信息
type SoftwareInfo struct {
	InstanceID     string   `json:"instance_id"`
	InstanceName   string   `json:"instance_name"`
	IP             string   `json:"ip"`
	SoftwareType   string   `json:"software_type"` // "sing-box" | "3x-ui" | "xray" | "none"
	DeployMode     string   `json:"deploy_mode"`   // "docker" | "native" | "none"
	ContainerName  string   `json:"container_name,omitempty"`
	ConfigPath     string   `json:"config_path,omitempty"`
	CurrentVersion string   `json:"current_version"`
	LatestVersion  string   `json:"latest_version"`
	ReleaseDate    string   `json:"release_date"`
	HasUpdate      bool     `json:"has_update"`
	ActivePorts    []int    `json:"active_ports"`
	Status         string   `json:"status"` // "running" | "stopped" | "not_installed" | "unreachable"
	ErrorMessage   string   `json:"error_message,omitempty"`
	LastCheckedAt  string   `json:"last_checked_at"`
}

// ReleaseInfo 上游 GitHub Release 信息
type ReleaseInfo struct {
	Repo        string    `json:"repo"`
	TagName     string    `json:"tag_name"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	PublishedAt string    `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	CachedAt    time.Time `json:"cached_at"`
}

// BlockedAnalysisResult 节点墙况深度计算与分类诊断结果
type BlockedAnalysisResult struct {
	NodeName          string `json:"node_name"`
	ServerIP          string `json:"server_ip"`
	ServerPort        int    `json:"server_port"`
	Protocol          string `json:"protocol"`
	FaultType         string `json:"fault_type"` // "PORT_BLOCKED", "SERVER_BLOCKED", "PROTOCOL_MISMATCH", "MACHINE_DOWN", "HEALTHY"
	DomesticTCP       bool   `json:"domestic_tcp"`
	AbroadTCP         bool   `json:"abroad_tcp"`
	DomesticPing      bool   `json:"domestic_ping"`
	AbroadRTTMs       int64  `json:"abroad_rtt_ms"`
	AbroadSource      string `json:"abroad_source"`
	DiagnosticMessage string `json:"diagnostic_message"`
	RecommendedAction string `json:"recommended_action"`
	CanAutoHealPort   bool   `json:"can_auto_heal_port"`
	CanRelayRescue    bool   `json:"can_relay_rescue"`
}

// PortHealRequest 换端口请求体
type PortHealRequest struct {
	NodeName   string `json:"node_name"`
	TargetPort int    `json:"target_port,omitempty"` // 为 0 则自动在 20000-60000 生成安全随机高位端口
}

// PortHealResponse 换端口自愈响应
type PortHealResponse struct {
	Success      bool   `json:"success"`
	NodeName     string `json:"node_name"`
	OldPort      int    `json:"old_port"`
	NewPort      int    `json:"new_port"`
	NewURL       string `json:"new_url"`
	SubStoreSync bool   `json:"substore_synced"`
	ConfigSync   bool   `json:"config_synced"`
	Message      string `json:"message"`
}
