package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
)

// InitTabTables 初始化选项卡与订阅 Profile 数据表及默认预设
func (s *Store) InitTabTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS node_tabs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		icon TEXT,
		description TEXT,
		keywords_json TEXT,
		include_tabs_json TEXT,
		exclude_tags_json TEXT,
		is_system INTEGER DEFAULT 0,
		order_idx INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS subscription_profiles (
		name TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		selected_tabs_json TEXT NOT NULL,
		include_external INTEGER DEFAULT 1,
		target_format TEXT DEFAULT 'mihomoConfig',
		updated_at DATETIME
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("create tab tables: %w", err)
	}

	return s.seedDefaultTabsAndProfiles()
}

// seedDefaultTabsAndProfiles 写入开箱即用的原子选项卡、组合节点组与默认 Q/ashan 订阅配置
func (s *Store) seedDefaultTabsAndProfiles() error {
	ctx := context.Background()
	now := time.Now().Format("2006-01-02 15:04:05")

	defaultTabs := []*model.NodeTab{
		// 1. 基础原子选项卡 (Atomic Tabs)
		{
			ID:          "transit",
			Name:        "线路机",
			Type:        model.TabTypeAtomic,
			Icon:        "🚀",
			Description: "自建骨干专线线路机 (美西4837, 东京IIJ, 东京软银, 香港HKG)",
			Keywords:    transitTabKeywords(),
			IsSystem:    true,
			OrderIdx:    1,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "residential",
			Name:        "家宽",
			Type:        model.TabTypeAtomic,
			Icon:        "🏠",
			Description: "自建全球原生家宽落地直连与链式中继 (美家宽, 日家宽, 亚太家宽)",
			Keywords:    []string{"家宽", "美宽", "日宽", "台宽", "韩宽", "新宽", "LZ-", "Pavlov", "Frontier", "Verizon", "BIGLOBE", "KDDI", "GLBB", "HiNet", "KT", "StarHub"},
			IsSystem:    true,
			OrderIdx:    2,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "external",
			Name:        "外部机场",
			Type:        model.TabTypeAtomic,
			Icon:        "☁️",
			Description: "外部商业机场订阅池 (蜂窝云等外部订阅)",
			Keywords:    []string{"蜂窝云", "外部", "机场"},
			IsSystem:    true,
			OrderIdx:    3,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "us",
			Name:        "美区",
			Type:        model.TabTypeAtomic,
			Icon:        "🇺🇸",
			Description: "美区所有节点 (专线直连 + 美国家宽落地与中继)",
			Keywords:    []string{"US", "美", "LAX", "Pavlov", "Frontier", "Verizon"},
			IsSystem:    true,
			OrderIdx:    4,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "jp",
			Name:        "日区",
			Type:        model.TabTypeAtomic,
			Icon:        "🇯🇵",
			Description: "日区所有节点 (专线直连 + 日本家宽落地与中继)",
			Keywords:    []string{"JP", "日", "Tokyo", "IIJ", "SoftBank", "BIGLOBE", "KDDI", "GLBB"},
			IsSystem:    true,
			OrderIdx:    5,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "other_region",
			Name:        "其他区",
			Type:        model.TabTypeAtomic,
			Icon:        "🌏",
			Description: "台湾/新加坡/韩国/香港等亚太落地与中继",
			Keywords:    otherRegionTabKeywords(),
			IsSystem:    true,
			OrderIdx:    6,
			CreatedAt:   now,
			UpdatedAt:   now,
		},

		// 2. 组合节点组选项卡 (Composite Tabs: 节点组内可嵌套包含选项卡)
		{
			ID:          "group_transit",
			Name:        "自建-线路机",
			Type:        model.TabTypeComposite,
			Icon:        "⚡",
			Description: "纯骨干专线节点组，无任何家宽，跑大流量低延迟",
			IncludeTabs: []string{"transit"},
			IsSystem:    true,
			OrderIdx:    10,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "group_residential",
			Name:        "自建-家宽机",
			Type:        model.TabTypeComposite,
			Icon:        "🏡",
			Description: "全量自建家宽落地与链式中继，解锁流媒体与高风控",
			IncludeTabs: []string{"residential"},
			IsSystem:    true,
			OrderIdx:    11,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "group_self_all",
			Name:        "自建-全部",
			Type:        model.TabTypeComposite,
			Icon:        "🛡️",
			Description: "自建全家桶: 嵌套包含自建-线路机组与自建-家宽机组",
			IncludeTabs: []string{"group_transit", "group_residential"},
			IsSystem:    true,
			OrderIdx:    12,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	for _, tab := range defaultTabs {
		var exists int
		_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM node_tabs WHERE id = ?", tab.ID).Scan(&exists)
		if exists == 0 {
			if err := s.SaveTab(ctx, tab); err != nil {
				logger.Errorf("Failed to seed default tab '%s': %v", tab.ID, err)
			}
		}
	}

	// 3. 默认对外订阅配置 (Q 标准: 外部 + 自建线路机; ashan 标准: 外部 + 自建全部)
	defaultProfiles := []*model.SubscriptionProfile{
		{
			Name:            "Q",
			DisplayName:     "Q 高速骨干轻量组 (外部+自建线路)",
			SelectedTabs:    []string{"external", "group_transit"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "q",
			DisplayName:     "q 高速骨干轻量组 (小写别名)",
			SelectedTabs:    []string{"external", "group_transit"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "ashan",
			DisplayName:     "Ashan 全量全家桶 (外部+自建全部)",
			SelectedTabs:    []string{"external", "group_self_all"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "Ashan",
			DisplayName:     "Ashan 全量全家桶 (大写别名)",
			SelectedTabs:    []string{"external", "group_self_all"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "business",
			DisplayName:     "business 纯自建商业直出 (仅自建零机场)",
			SelectedTabs:    []string{"group_self_all"},
			IncludeExternal: false,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "Business",
			DisplayName:     "Business 纯自建商业直出 (大写别名)",
			SelectedTabs:    []string{"group_self_all"},
			IncludeExternal: false,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
	}

	for _, p := range defaultProfiles {
		var exists int
		_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM subscription_profiles WHERE name = ?", p.Name).Scan(&exists)
		if exists == 0 {
			if err := s.SaveProfile(ctx, p); err != nil {
				logger.Errorf("Failed to seed default profile '%s': %v", p.Name, err)
			}
		}
	}

	return nil
}

// GetAllTabs 查询所有已注册的选项卡
func (s *Store) GetAllTabs(ctx context.Context) ([]*model.NodeTab, error) {
	query := `
	SELECT id, name, type, icon, description, keywords_json, include_tabs_json, exclude_tags_json, is_system, order_idx, created_at, updated_at
	FROM node_tabs ORDER BY order_idx ASC, id ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tabs []*model.NodeTab
	for rows.Next() {
		var t model.NodeTab
		var isSysInt int
		var kwJSON, incJSON, excJSON sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Icon, &t.Description, &kwJSON, &incJSON, &excJSON, &isSysInt, &t.OrderIdx, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.IsSystem = isSysInt == 1
		if kwJSON.Valid && kwJSON.String != "" {
			_ = json.Unmarshal([]byte(kwJSON.String), &t.Keywords)
		}
		if incJSON.Valid && incJSON.String != "" {
			_ = json.Unmarshal([]byte(incJSON.String), &t.IncludeTabs)
		}
		if excJSON.Valid && excJSON.String != "" {
			_ = json.Unmarshal([]byte(excJSON.String), &t.ExcludeTags)
		}
		tabs = append(tabs, &t)
	}
	return tabs, nil
}

// GetTab 查询单个选项卡
func (s *Store) GetTab(ctx context.Context, id string) (*model.NodeTab, error) {
	query := `
	SELECT id, name, type, icon, description, keywords_json, include_tabs_json, exclude_tags_json, is_system, order_idx, created_at, updated_at
	FROM node_tabs WHERE id = ?
	`
	var t model.NodeTab
	var isSysInt int
	var kwJSON, incJSON, excJSON sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(&t.ID, &t.Name, &t.Type, &t.Icon, &t.Description, &kwJSON, &incJSON, &excJSON, &isSysInt, &t.OrderIdx, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.IsSystem = isSysInt == 1
	if kwJSON.Valid && kwJSON.String != "" {
		_ = json.Unmarshal([]byte(kwJSON.String), &t.Keywords)
	}
	if incJSON.Valid && incJSON.String != "" {
		_ = json.Unmarshal([]byte(incJSON.String), &t.IncludeTabs)
	}
	if excJSON.Valid && excJSON.String != "" {
		_ = json.Unmarshal([]byte(excJSON.String), &t.ExcludeTags)
	}
	return &t, nil
}

// SaveTab 保存或更新一个选项卡
func (s *Store) SaveTab(ctx context.Context, tab *model.NodeTab) error {
	if tab.ID == "" {
		return fmt.Errorf("tab id cannot be empty")
	}
	if tab.Name == "" {
		return fmt.Errorf("tab name cannot be empty")
	}
	if tab.Type == "" {
		tab.Type = model.TabTypeComposite
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if tab.CreatedAt == "" {
		tab.CreatedAt = now
	}
	tab.UpdatedAt = now

	kwJSON, _ := json.Marshal(tab.Keywords)
	incJSON, _ := json.Marshal(tab.IncludeTabs)
	excJSON, _ := json.Marshal(tab.ExcludeTags)
	isSysInt := 0
	if tab.IsSystem {
		isSysInt = 1
	}

	query := `
	INSERT INTO node_tabs (id, name, type, icon, description, keywords_json, include_tabs_json, exclude_tags_json, is_system, order_idx, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name = excluded.name,
		type = excluded.type,
		icon = excluded.icon,
		description = excluded.description,
		keywords_json = excluded.keywords_json,
		include_tabs_json = excluded.include_tabs_json,
		exclude_tags_json = excluded.exclude_tags_json,
		order_idx = excluded.order_idx,
		updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, query,
		tab.ID, tab.Name, string(tab.Type), tab.Icon, tab.Description,
		string(kwJSON), string(incJSON), string(excJSON),
		isSysInt, tab.OrderIdx, tab.CreatedAt, tab.UpdatedAt,
	)
	return err
}

// DeleteTab 删除一个非系统选项卡
func (s *Store) DeleteTab(ctx context.Context, id string) error {
	tab, err := s.GetTab(ctx, id)
	if err != nil {
		return err
	}
	if tab.IsSystem {
		return fmt.Errorf("cannot delete system tab '%s'", id)
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM node_tabs WHERE id = ?", id)
	return err
}

// GetAllProfiles 查询所有订阅 Profile
func (s *Store) GetAllProfiles(ctx context.Context) ([]*model.SubscriptionProfile, error) {
	query := `
	SELECT name, display_name, selected_tabs_json, include_external, target_format, updated_at
	FROM subscription_profiles ORDER BY name ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []*model.SubscriptionProfile
	for rows.Next() {
		var p model.SubscriptionProfile
		var extInt int
		var tabsJSON sql.NullString
		if err := rows.Scan(&p.Name, &p.DisplayName, &tabsJSON, &extInt, &p.TargetFormat, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.IncludeExternal = extInt == 1
		if tabsJSON.Valid && tabsJSON.String != "" {
			_ = json.Unmarshal([]byte(tabsJSON.String), &p.SelectedTabs)
		}
		profiles = append(profiles, &p)
	}
	return profiles, nil
}

// GetProfile 查询指定名称的订阅 Profile
func (s *Store) GetProfile(ctx context.Context, name string) (*model.SubscriptionProfile, error) {
	query := `
	SELECT name, display_name, selected_tabs_json, include_external, target_format, updated_at
	FROM subscription_profiles WHERE name = ?
	`
	var p model.SubscriptionProfile
	var extInt int
	var tabsJSON sql.NullString
	err := s.db.QueryRowContext(ctx, query, name).Scan(&p.Name, &p.DisplayName, &tabsJSON, &extInt, &p.TargetFormat, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.IncludeExternal = extInt == 1
	if tabsJSON.Valid && tabsJSON.String != "" {
		_ = json.Unmarshal([]byte(tabsJSON.String), &p.SelectedTabs)
	}
	return &p, nil
}

// SaveProfile 保存或更新一个订阅 Profile
func (s *Store) SaveProfile(ctx context.Context, p *model.SubscriptionProfile) error {
	if p.Name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	p.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	tabsJSON, _ := json.Marshal(p.SelectedTabs)
	extInt := 0
	if p.IncludeExternal {
		extInt = 1
	}

	query := `
	INSERT INTO subscription_profiles (name, display_name, selected_tabs_json, include_external, target_format, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		display_name = excluded.display_name,
		selected_tabs_json = excluded.selected_tabs_json,
		include_external = excluded.include_external,
		target_format = excluded.target_format,
		updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, query,
		p.Name, p.DisplayName, string(tabsJSON), extInt, p.TargetFormat, p.UpdatedAt,
	)
	return err
}

// ResetProfiles 重置 Q 与 ashan 订阅为标准预设
func (s *Store) ResetProfiles(ctx context.Context) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	standards := []*model.SubscriptionProfile{
		{
			Name:            "Q",
			DisplayName:     "Q 高速骨干轻量组 (外部+自建线路)",
			SelectedTabs:    []string{"external", "group_transit"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "q",
			DisplayName:     "q 高速骨干轻量组 (小写别名)",
			SelectedTabs:    []string{"external", "group_transit"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "ashan",
			DisplayName:     "Ashan 全量全家桶 (外部+自建全部)",
			SelectedTabs:    []string{"external", "group_self_all"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "Ashan",
			DisplayName:     "Ashan 全量全家桶 (大写别名)",
			SelectedTabs:    []string{"external", "group_self_all"},
			IncludeExternal: true,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "business",
			DisplayName:     "business 纯自建商业直出 (仅自建零机场)",
			SelectedTabs:    []string{"group_self_all"},
			IncludeExternal: false,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
		{
			Name:            "Business",
			DisplayName:     "Business 纯自建商业直出 (大写别名)",
			SelectedTabs:    []string{"group_self_all"},
			IncludeExternal: false,
			TargetFormat:    "mihomoConfig",
			UpdatedAt:       now,
		},
	}

	for _, p := range standards {
		if err := s.SaveProfile(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// RecursivelyResolveTab 递归解析选项卡（支持节点组嵌套包含选项卡），返回展开的所有关键词与引用的 SubStore 子订阅源
func (s *Store) RecursivelyResolveTab(ctx context.Context, tabID string, visited map[string]bool) (keywords []string, subNames []string, err error) {
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[tabID] {
		// 循环引用防御
		return nil, nil, nil
	}
	visited[tabID] = true

	tab, err := s.GetTab(ctx, tabID)
	if err != nil {
		return nil, nil, err
	}

	// 映射 SubStore 常用基础订阅
	switch tab.ID {
	case "transit", "group_transit":
		subNames = append(subNames, "ashan-transit")
	case "residential", "group_residential":
		subNames = append(subNames, "ashan-us-resi", "ashan-jp-resi", "ashan-other")
	case "external":
		subNames = append(subNames, "蜂窝云")
	case "us":
		subNames = append(subNames, "ashan-us-resi")
	case "jp":
		subNames = append(subNames, "ashan-jp-resi")
	case "other_region":
		subNames = append(subNames, "ashan-other")
	}

	keywords = append(keywords, tab.Keywords...)

	// 递归展开嵌套选项卡
	for _, incTabID := range tab.IncludeTabs {
		childKeywords, childSubs, childErr := s.RecursivelyResolveTab(ctx, incTabID, visited)
		if childErr == nil {
			keywords = append(keywords, childKeywords...)
			subNames = append(subNames, childSubs...)
		}
	}

	return deduplicateStrings(keywords), deduplicateStrings(subNames), nil
}

func deduplicateStrings(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			out = append(out, trimmed)
		}
	}
	return out
}

// transitTabKeywords 线路机选项卡关键字（香港 HKG-3 必须在此，否则新线路机不会出现在线路机 Tab）。
func transitTabKeywords() []string {
	return []string{"线路机", "4837", "IIJ", "SoftBank", "JPSB", "LAX", "HK", "HKG", "香港"}
}

// otherRegionTabKeywords 其他区选项卡关键字。
func otherRegionTabKeywords() []string {
	return []string{"TW", "台", "HiNet", "KR", "韩", "KT", "SG", "新", "StarHub", "HK", "HKG", "香港"}
}
