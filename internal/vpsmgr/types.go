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
	SSHKey      string `json:"ssh_key,omitempty"`
	ProviderID  string `json:"provider_id"`
	IsNAT       bool   `json:"is_nat"`
	ExecMode    string `json:"exec_mode,omitempty"` // "ssh" | "hub"（hub = 经 :3338 WebSocket PTY 执行）
	NATPorts    []int  `json:"nat_ports,omitempty"`
	Status      string `json:"status"`
	Region      string `json:"region"`
	Category    string `json:"category,omitempty"` // 懒猫云分类：固定出口 | 全球家宽
}

// BackupSnapshot VPS 配置文件或数据库的历史快照
type BackupSnapshot struct {
	ID        string `json:"id"`
	FileName  string `json:"file_name"`
	FilePath  string `json:"file_path"`
	CreatedAt string `json:"created_at"`
	SizeBytes int64  `json:"size_bytes"`
}

// SmokeTestResult 软件升级后内置沙箱真实出网冒烟测试结果
type SmokeTestResult struct {
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latency_ms"`
	TestedAt  string `json:"tested_at"`
	ExitIP    string `json:"exit_ip,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// SoftwareInfo VPS 上运行的代理软件详情与生命周期信息
type SoftwareInfo struct {
	InstanceID     string            `json:"instance_id"`
	InstanceName   string            `json:"instance_name"`
	IP             string            `json:"ip"`
	SoftwareType   string            `json:"software_type"` // "sing-box" | "3x-ui" | "xray" | "none"
	DeployMode     string            `json:"deploy_mode"`   // "docker" | "native" | "none"
	ContainerName  string            `json:"container_name,omitempty"`
	ImageID        string            `json:"image_id,omitempty"`
	ConfigPath     string            `json:"config_path,omitempty"`
	CurrentVersion string            `json:"current_version"`
	LatestVersion  string            `json:"latest_version"`
	ReleaseDate    string            `json:"release_date"`
	HasUpdate      bool              `json:"has_update"`
	ActivePorts    []int             `json:"active_ports"`
	Status         string            `json:"status"` // "running" | "stopped" | "not_installed" | "unreachable"
	ErrorMessage   string            `json:"error_message,omitempty"`
	LastCheckedAt  string            `json:"last_checked_at"`
	Backups        []*BackupSnapshot `json:"backups,omitempty"`
	LastSmokeTest  *SmokeTestResult  `json:"last_smoke_test,omitempty"`
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

// LifecycleActionRequest 容器生命周期操作请求 (start, stop, restart, backup)
type LifecycleActionRequest struct {
	VPSID  string `json:"vps_id"`
	Action string `json:"action"` // "start" | "stop" | "restart" | "backup"
}

// LifecycleRestoreRequest 容器快照还原请求
type LifecycleRestoreRequest struct {
	VPSID      string `json:"vps_id"`
	SnapshotID string `json:"snapshot_id"`
}

// SafeUpgradeRequest 安全升级请求
type SafeUpgradeRequest struct {
	VPSID    string `json:"vps_id"`
	Software string `json:"software,omitempty"`
	NodeName string `json:"node_name,omitempty"`
}

// SafeUpgradeResponse 安全升级响应
type SafeUpgradeResponse struct {
	Success         bool             `json:"success"`
	VPSID           string           `json:"vps_id"`
	IP              string           `json:"ip"`
	SoftwareType    string           `json:"software_type"`
	PreviousVersion string           `json:"previous_version"`
	CurrentVersion  string           `json:"current_version"`
	RolledBack      bool             `json:"rolled_back"`
	RollbackReason  string           `json:"rollback_reason,omitempty"`
	BackupCreated   string           `json:"backup_created,omitempty"`
	SmokeTest       *SmokeTestResult `json:"smoke_test,omitempty"`
	Message         string           `json:"message"`
}

// RegisterInstanceRequest 纳管/注册全新 VPS 或 NAT VPS 请求
type RegisterInstanceRequest struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	SSHPassword string `json:"ssh_password"`
	SSHKey      string `json:"ssh_key,omitempty"`
	ProviderID  string `json:"provider_id,omitempty"`
	IsNAT       bool   `json:"is_nat"`
	ExecMode    string `json:"exec_mode,omitempty"` // "ssh" | "hub"（hub = 经 :3338 WebSocket PTY 执行）
	NATPorts    []int  `json:"nat_ports,omitempty"`
	Region      string `json:"region,omitempty"`
}

// UninstallRequest 彻底卸载指定 VPS 代理容器请求
type UninstallRequest struct {
	VPSID         string `json:"vps_id"`
	CleanSubStore bool   `json:"clean_substore"` // 默认 true
	RemoveData    bool   `json:"remove_data"`    // 默认 true
}

// UninstallResponse 卸载响应
type UninstallResponse struct {
	Success              bool     `json:"success"`
	VPSID                string   `json:"vps_id"`
	IP                   string   `json:"ip"`
	CleanedContainers    []string `json:"cleaned_containers"`
	CleanedDirectories   []string `json:"cleaned_directories"`
	SubStoreRemovedCount int      `json:"substore_removed_count"`
	Message              string   `json:"message"`
}
