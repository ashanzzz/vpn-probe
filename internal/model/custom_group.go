package model

import "time"

// AtomNode 原子节点项（提供细粒度查询与动态编排）
type AtomNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Protocol  string    `json:"protocol"`
	Country   string    `json:"country"`    // US, JP, SG, HK...
	Kind      string    `json:"kind"`       // "transit" (线路机) | "residential" (家宽机) | "chain" (线路拉家宽链式)
	ChinaExit bool      `json:"china_exit"` // 是否送中
	LatencyMs int       `json:"latency_ms"` // 延迟 (ms)
	Online    bool      `json:"online"`     // 是否在线
	TestedAt  time.Time `json:"tested_at"`
}

// GroupFilter 自定义分组的动态过滤与匹配规则
type GroupFilter struct {
	Countries          []string `json:"countries,omitempty"`           // 匹配的国家列表: 如 ["US"], ["JP"]，空表示不限
	IncludeTransit     bool     `json:"include_transit"`              // 是否包含线路机
	IncludeResidential bool     `json:"include_residential"`          // 是否包含家宽直连
	IncludeChain       bool     `json:"include_chain"`                // 是否包含线路拉家宽链式中继
	ExcludeChinaLeak   bool     `json:"exclude_china_leak"`           // 是否排除送中节点 (默认 true 严格排除)
	CustomMatchRegex   string   `json:"custom_match_regex,omitempty"` // 可选自定义正则匹配
}

// CustomGroupDef 自定义分组定义 (保存在 data/custom_groups.json)
type CustomGroupDef struct {
	Name        string      `json:"name"`         // 英文唯一标识: 如 "ashan-us-bundle"
	DisplayName string      `json:"display_name"` // 界面展示名: 如 "自建-美国全家桶"
	Enabled     bool        `json:"enabled"`
	Filter      GroupFilter `json:"filter"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

// FiveGroupPayload 自建五维策略组数据载荷
type FiveGroupPayload struct {
	TransitNodes   []string `json:"transit_nodes"`   // 自建-线路机 (ashan-transit)
	USResiNodes    []string `json:"us_resi_nodes"`    // 自建-美国和美国家宽 (ashan-us-resi)
	JPResiNodes    []string `json:"jp_resi_nodes"`    // 自建-日本和日本家宽 (ashan-jp-resi)
	OtherNodes     []string `json:"other_nodes"`      // 自建-其他 (ashan-other)
	ChinaLeakNodes []string `json:"china_leak_nodes"` // 自建-送中 (ashan-china-leak)
}
