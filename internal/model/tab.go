package model

// TabType 选项卡类型
type TabType string

const (
	TabTypeAtomic    TabType = "atomic"    // 原子标签 (如 "线路机", "家宽", "外部", "美区", "日区")
	TabTypeComposite TabType = "composite" // 组合节点组选项卡 (可多选包含其他选项卡，实现节点组内嵌套选项卡)
)

// NodeTab 动态选项卡定义 (存储于 SQLite node_tabs 表)
type NodeTab struct {
	ID          string   `json:"id"`                     // 唯一标识 (如 "transit", "residential", "group_transit", "group_self_all")
	Name        string   `json:"name"`                   // 选项卡名称 (如 "线路机", "家宽", "自建-线路机", "自建-全部")
	Type        TabType  `json:"type"`                   // atomic | composite
	Icon        string   `json:"icon,omitempty"`         // 图标 (如 "🚀", "🏠", "☁️", "⚡", "🛡️")
	Description string   `json:"description,omitempty"`  // 详细说明
	Keywords    []string `json:"keywords,omitempty"`     // 匹配关键字/符号 (如 ["线路机", "4837", "IIJ", "SoftBank"], ["家宽", "美宽", "日宽"])
	IncludeTabs []string `json:"include_tabs,omitempty"` // 包含/引用的其他选项卡 ID 列表 (节点组内嵌套选项卡)
	ExcludeTags []string `json:"exclude_tags,omitempty"` // 排除标签/关键字 (如 ["送中", "离线"])
	IsSystem    bool     `json:"is_system"`              // 是否系统内置预设
	OrderIdx    int      `json:"order_idx"`              // 展示顺序
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// SubscriptionProfile 对外订阅 Profile 定义 (存储于 SQLite subscription_profiles 表，如 ashan, Q)
type SubscriptionProfile struct {
	Name            string   `json:"name"`                      // 订阅标识: "ashan", "Ashan", "Q", "q"
	DisplayName     string   `json:"display_name"`              // 界面展示名: "Ashan 全量全家桶", "Q 高速骨干轻量组"
	SelectedTabs    []string `json:"selected_tabs"`             // 多选包含的选项卡 ID 列表
	IncludeExternal bool     `json:"include_external"`          // 是否包含外部商业机场连接 (蜂窝云)
	TargetFormat    string   `json:"target_format,omitempty"`   // 目标格式 (默认 mihomoConfig)
	UpdatedAt       string   `json:"updated_at"`
}

// TabResolveResult 选项卡解析后的节点匹配结果
type TabResolveResult struct {
	TabID        string      `json:"tab_id"`
	TabName      string      `json:"tab_name"`
	MatchedNodes []*AtomNode `json:"matched_nodes"`
	NodeCount    int         `json:"node_count"`
	SubNames     []string    `json:"sub_names"` // 对应的 SubStore 订阅源名称
}
