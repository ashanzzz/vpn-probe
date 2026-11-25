package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig        `yaml:"server"`
	Logging      LogConfig           `yaml:"logging"`
	Monitor      MonitorConfig       `yaml:"monitor"`
	SubStore     SubStoreConfig      `yaml:"substore"`
	Probe        ProbeConfig         `yaml:"probe"`
	SpeedTest    SpeedTestConfig     `yaml:"speedtest"`
	VPSHub       VPSHubConfig        `yaml:"vps_hub,omitempty"`
	VPSInstances []VPSInstanceConfig `yaml:"vps_instances,omitempty"`
}

type VPSHubConfig struct {
	URL      string `yaml:"url" json:"url"`
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
}

type VPSInstanceConfig struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	IP          string `yaml:"ip" json:"ip"`
	SSHPort     int    `yaml:"ssh_port" json:"ssh_port"`
	SSHUser     string `yaml:"ssh_user" json:"ssh_user"`
	SSHPassword string `yaml:"ssh_password" json:"ssh_password"`
	ProviderID  string `yaml:"provider_id,omitempty" json:"provider_id,omitempty"`
	Status      string `yaml:"status,omitempty" json:"status,omitempty"`
	Region      string `yaml:"region,omitempty" json:"region,omitempty"`
	IsNAT       bool   `yaml:"is_nat,omitempty" json:"is_nat,omitempty"`
	NATPorts    []int  `yaml:"nat_ports,omitempty" json:"nat_ports,omitempty"`
}

type SpeedTestConfig struct {
	SelfHostedURL10M string `yaml:"self_hosted_url_10m" json:"self_hosted_url_10m"`
	SelfHostedURL30M string `yaml:"self_hosted_url_30m" json:"self_hosted_url_30m"`
}

type MonitoredNode struct {
	Name   string `yaml:"name" json:"name"`
	URL    string `yaml:"url" json:"url"`
	IsSelf bool   `yaml:"is_self" json:"is_self"`
}

type MonitorConfig struct {
	Enabled              bool            `yaml:"enabled"`
	HealthCheckInterval  string          `yaml:"health_check_interval"`  // e.g. "2m" (常规在线与延迟)
	GFWCheckInterval     string          `yaml:"gfw_check_interval"`     // e.g. "15m" (送中检测)
	SplitCheckInterval   string          `yaml:"split_check_interval"`   // e.g. "30m" (ChatGPT/Claude分流探测)
	AutoSyncSubStore     bool            `yaml:"auto_sync_substore"`
	Nodes                []MonitoredNode `yaml:"nodes"`
	ResidentialNodes     []MonitoredNode `yaml:"residential_nodes,omitempty" json:"residential_nodes,omitempty"`
}

type LogConfig struct {
	Level    string `yaml:"level"`     // debug, info, warn, error
	FilePath string `yaml:"file_path"` // 本地落盘路径，如 "./data/vpn-probe.log"
}

type ServerConfig struct {
	Port         int    `yaml:"port"`
	Host         string `yaml:"host"`
	AuthToken    string `yaml:"auth_token"`               // 默认主 Token (写/操作权限)
	ReadToken    string `yaml:"read_token,omitempty"`     // 只读订阅拉取 Token
	AdminToken   string `yaml:"admin_token,omitempty"`    // 管理员超级权限 Token (为空时回退至 AuthToken)
	AllowOrigins string `yaml:"allow_origins,omitempty"`
}

type SubStoreConfig struct {
	Enabled  bool   `yaml:"enabled"`
	URL      string `yaml:"url"`      // e.g. "https://sub.example.com"
	Username string `yaml:"username"` // SubStore HTTP Basic Auth Username
	Password    string `yaml:"password"`     // SubStore HTTP Basic Auth Password
	Token       string `yaml:"token"`        // SubStore API Token
	FallbackURL string `yaml:"fallback_url"` // 可选直连兜底端点 (默认 http://156.231.113.91:3001)

	// 动态全周期解耦架构字段 (Issue #18)
	ExternalSources []string `yaml:"external_sources"` // 动态外部机场订阅列表 (如 ["蜂窝云"])
	Domains         []string `yaml:"domains"`          // 域名列表
	ShareToken      string   `yaml:"share_token"`      // 分享 Token (由 config.yaml / VPN_PROBE_SHARE_TOKEN 注入)
	SelfSubName     string   `yaml:"self_sub_name,omitempty" json:"self_sub_name,omitempty"` // 主自建订阅名称 (默认 "ashan's vps")

	// 四大分组对应的 SubStore 订阅或集合名称
	GroupSelfOnline      string `yaml:"group_self_online"`       // 在线自有服务
	GroupSelfOffline     string `yaml:"group_self_offline"`      // 离线自有服务
	GroupSelfOnlineChina string `yaml:"group_self_online_china"` // 在线自有服务送中
	GroupSelfOnlineClean string `yaml:"group_self_online_clean"` // 在线自有服务不送中
	// 外部节点分组（可选）
	GroupExtOnlineClean string `yaml:"group_ext_online_clean"` // 不送中
	GroupExtOnlineChina string `yaml:"group_ext_online_china"` // 送中
}

type ProbeConfig struct {
	SingboxBin      string            `yaml:"singbox_bin"`       // sing-box 可执行路径，留空默认从 PATH 找
	PortStart       int               `yaml:"port_start"`        // 随机端口起始，默认 20000 (避开 Linux 临时端口 32768-60999)
	PortEnd         int               `yaml:"port_end"`          // 随机端口结束，默认 28000
	DefaultTimeout  int               `yaml:"default_timeout"`   // 默认超时(秒)，如 8
	MaxConcurrency  int               `yaml:"max_concurrency"`   // 最大并发测试数，如 10
	GoogleNCRURL    string            `yaml:"google_ncr_url"`    // https://www.google.com/ncr
	CloudflareTrace string            `yaml:"cloudflare_trace"`  // https://cloudflare.com/cdn-cgi/trace
	LatencyCheckURL string            `yaml:"latency_check_url"` // https://www.gstatic.com/generate_204
	AbroadProbe     AbroadProbeConfig `yaml:"abroad_probe"`      // 海外探针配置
}

type AbroadProbeConfig struct {
	Mode             string `yaml:"mode"`               // "auto" (自有节点优先，公共兜底), "peer_only" (仅自有), "public_only" (仅公共)
	FallbackToPublic bool   `yaml:"fallback_to_public"` // 自有不可用时是否回退公共API (如 check-host.net)
	PeerConcurrency  int    `yaml:"peer_concurrency"`   // 并发选用自有健康节点数，默认 2
	PeerTimeoutMs    int    `yaml:"peer_timeout_ms"`    // 自有节点拨号超时(毫秒)，默认 3000
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:      8080,
			Host:      "0.0.0.0",
			AuthToken: "",
		},
		Logging: LogConfig{
			Level:    "info",
			FilePath: "./data/vpn-probe.log",
		},
		Monitor: MonitorConfig{
			Enabled:             true,
			HealthCheckInterval: "2m",
			GFWCheckInterval:    "15m",
			SplitCheckInterval:  "30m",
			AutoSyncSubStore:    true,
			Nodes:               nil,
		},
		SubStore: SubStoreConfig{
			Enabled:              false,
			URL:                  "http://127.0.0.1:2999",
			Token:                "",
			ExternalSources:      []string{},
			Domains:              []string{},
			ShareToken:           "",
			SelfSubName:          "ashan's vps",
			GroupSelfOnline:      "在线自有服务",
			GroupSelfOffline:     "离线自有服务",
			GroupSelfOnlineChina: "在线自有服务送中",
			GroupSelfOnlineClean: "在线自有服务不送中",
			GroupExtOnlineClean:  "不送中",
			GroupExtOnlineChina:  "送中",
		},
		Probe: ProbeConfig{
			SingboxBin:      "sing-box",
			PortStart:       20000,
			PortEnd:         28000,
			DefaultTimeout:  8,
			MaxConcurrency:  10,
			GoogleNCRURL:    "https://www.google.com/ncr",
			CloudflareTrace: "https://cloudflare.com/cdn-cgi/trace",
			LatencyCheckURL: "https://www.gstatic.com/generate_204",
			AbroadProbe: AbroadProbeConfig{
				Mode:             "auto",
				FallbackToPublic: true,
				PeerConcurrency:  2,
				PeerTimeoutMs:    3000,
			},
		},
		SpeedTest: SpeedTestConfig{
			SelfHostedURL10M: "",
			SelfHostedURL30M: "",
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		}
	}

	// 环境变量覆盖 (优先级: 环境变量 > 配置文件)
	if env := os.Getenv("VPN_PROBE_AUTH_TOKEN"); env != "" {
		cfg.Server.AuthToken = env
	}
	if env := os.Getenv("VPN_PROBE_READ_TOKEN"); env != "" {
		cfg.Server.ReadToken = env
	}
	if env := os.Getenv("VPN_PROBE_ADMIN_TOKEN"); env != "" {
		cfg.Server.AdminToken = env
	}
	if env := os.Getenv("VPN_PROBE_SUBSTORE_URL"); env != "" {
		cfg.SubStore.URL = env
	}
	if env := os.Getenv("VPN_PROBE_SUBSTORE_USERNAME"); env != "" {
		cfg.SubStore.Username = env
	}
	if env := os.Getenv("VPN_PROBE_SUBSTORE_PASSWORD"); env != "" {
		cfg.SubStore.Password = env
	}
	if env := os.Getenv("VPN_PROBE_SUBSTORE_TOKEN"); env != "" {
		cfg.SubStore.Token = env
	}
	if env := os.Getenv("VPN_PROBE_SHARE_TOKEN"); env != "" {
		cfg.SubStore.ShareToken = env
	}
	if env := os.Getenv("VPS_HUB_URL"); env != "" {
		cfg.VPSHub.URL = env
	}
	if env := os.Getenv("VPS_HUB_USER"); env != "" {
		cfg.VPSHub.Username = env
	}
	if env := os.Getenv("VPS_HUB_PASS"); env != "" {
		cfg.VPSHub.Password = env
	}

	return cfg, nil
}

// SaveConfig 将配置写回持久化文件 (供动态修改 SubStore、节点与监控参数使用)
// 包含安全防护：
// 1. 检查目标文件是否被 Git 跟踪，如果是则拒绝写入，防止凭据意外进入版本控制
// 2. 写入前保留 .bak 备份
// 3. 原子写入：临时文件 + 刷新落盘 + Rename，模式设为 0600
func SaveConfig(path string, cfg *Config) error {
	if path == "" {
		path = "config.yaml"
	}

	// 安全校验：拒绝写入已被 Git 跟踪的文件
	absPath, err := filepath.Abs(path)
	if err == nil {
		dir := filepath.Dir(absPath)
		cmd := exec.Command("git", "ls-files", "--error-unmatch", filepath.Base(absPath))
		cmd.Dir = dir
		if err := cmd.Run(); err == nil {
			// 返回 0 说明该文件已被 Git 跟踪，拒绝序列化写回
			return fmt.Errorf("security refusal: target file '%s' is tracked by git; untrack it first with 'git rm --cached' to protect credentials", path)
		}
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// 备份现有配置
	if _, err := os.Stat(path); err == nil {
		bakPath := path + ".bak"
		_ = os.WriteFile(bakPath, data, 0600)
	}

	// 原子写入临时文件
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create tmp config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write tmp config: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync tmp config: %w", err)
	}
	f.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename config: %w", err)
	}

	return nil
}
