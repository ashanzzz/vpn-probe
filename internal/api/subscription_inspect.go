package api

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/publish"
)

// ProxyInspectSummary summarizes a single proxy within a group or subscription
type ProxyInspectSummary struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Country      string   `json:"country,omitempty"`
	Kind         string   `json:"kind,omitempty"` // transit | residential | chain | airport
	IsSelfHosted bool     `json:"is_self_hosted"`
	LatencyMs    int      `json:"latency_ms"`
	Online       bool     `json:"online"`
	Tags         []string `json:"tags,omitempty"`
}

// GroupInspectItem represents a policy group with its classification, member nodes, and tags
type GroupInspectItem struct {
	Name        string                `json:"name"`
	DisplayName string                `json:"display_name"`
	Type        string                `json:"type"`
	IsExternal  bool                  `json:"is_external"`
	Category    string                `json:"category"` // external_default | self_backbone | self_resi | self_custom | app_routing
	Tags        []string              `json:"tags"`
	Proxies     []ProxyInspectSummary `json:"proxies"`
	ProxyCount  int                   `json:"proxy_count"`
	RuleCount   int                   `json:"rule_count"`
	CustomDef   *model.CustomGroupDef `json:"custom_def,omitempty"`
}

// RuleInspectItem represents a traffic routing rule
type RuleInspectItem struct {
	Type     string `json:"type"`    // GEOSITE, GEOIP, RULE-SET, MATCH, etc.
	Payload  string `json:"payload"` // e.g. youtube, telegram, US
	Target   string `json:"target"`  // target policy group name
	Provider string `json:"provider,omitempty"`
	Raw      string `json:"raw"`
}

// SubscriptionInspectResponse is the full topology response for a subscription
type SubscriptionInspectResponse struct {
	Status        string             `json:"status"`
	Subscription  string             `json:"subscription"`
	TotalProxies  int                `json:"total_proxies"`
	TotalGroups   int                `json:"total_groups"`
	SelfGroups    int                `json:"self_groups"`
	ExtGroups     int                `json:"ext_groups"`
	Groups        []GroupInspectItem `json:"groups"`
	Rules         []RuleInspectItem  `json:"rules"`
	RuleProviders []string           `json:"rule_providers"`
	RoutingScript string             `json:"routing_script"`
	UpdatedAt     string             `json:"updated_at"`
}

// handleSubscriptionInspect provides dynamic inspection of policy groups, routing rules, and scripts
func (s *APIServer) handleSubscriptionInspect(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Params("name"))
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "subscription name is required"})
	}

	var data []byte
	var updatedAt string

	// 1. First attempt to read from snapshot manager memory cache
	if s.snapshotMgr != nil {
		raw, meta, exists := s.snapshotMgr.GetSnapshot(name)
		if exists && len(raw) > 0 {
			data = raw
			if meta != nil && !meta.UpdatedAt.IsZero() {
				updatedAt = meta.UpdatedAt.Format(time.RFC3339)
			}
		} else {
			// Try WarmupOne
			m, err := s.snapshotMgr.WarmupOne(c.Context(), name)
			if err == nil {
				data, _, _ = s.snapshotMgr.GetSnapshot(name)
				if m != nil && !m.UpdatedAt.IsZero() {
					updatedAt = m.UpdatedAt.Format(time.RFC3339)
				}
			}
		}
	}

	// 2. Fallback to SubStore direct render if snapshot manager didn't have it
	if len(data) == 0 && s.substoreClient != nil {
		fresh, err := s.substoreClient.RenderFileFresh(c.Context(), name)
		if err == nil && len(fresh) > 0 {
			data = fresh
			updatedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}

	if len(data) == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": fmt.Sprintf("subscription content for %q not available", name),
		})
	}

	// 3. Parse Clash YAML
	var rawCfg struct {
		Proxies []struct {
			Name   string `yaml:"name"`
			Type   string `yaml:"type"`
			Server string `yaml:"server"`
			Port   int    `yaml:"port"`
		} `yaml:"proxies"`
		ProxyGroups []struct {
			Name    string   `yaml:"name"`
			Type    string   `yaml:"type"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Rules         []string       `yaml:"rules"`
		RuleProviders map[string]any `yaml:"rule-providers"`
	}

	if err := yaml.Unmarshal(data, &rawCfg); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("failed to parse YAML for %s: %v", name, err),
		})
	}

	rawProxyTypeMap := make(map[string]string, len(rawCfg.Proxies))
	for _, p := range rawCfg.Proxies {
		if p.Name != "" {
			rawProxyTypeMap[p.Name] = p.Type
		}
	}

	// 4. Gather atoms from orchestrator to annotate proxy nodes
	var atomMap = make(map[string]model.AtomNode)
	var atoms []model.AtomNode
	if s.orchestrator != nil {
		atoms = s.orchestrator.CollectAtoms()
		for _, a := range atoms {
			atomMap[a.Name] = a
		}
	}

	// Load custom groups for identifying user-defined self-hosted groups
	prof := "ashan"
	if strings.EqualFold(name, "q") {
		prof = "q"
	}
	customGroups, _ := monitor.LoadCustomGroups(prof)
	customMap := make(map[string]model.CustomGroupDef)
	for _, cg := range customGroups {
		if cg.Name != "" {
			customMap[cg.Name] = cg
		}
		if cg.DisplayName != "" {
			customMap[cg.DisplayName] = cg
		}
	}

	// 5. Build parsed rules and calculate rule counts per target policy group
	parsedRules := make([]RuleInspectItem, 0, len(rawCfg.Rules))
	ruleCountMap := make(map[string]int)
	for _, rStr := range rawCfg.Rules {
		rStr = strings.TrimSpace(rStr)
		if rStr == "" || strings.HasPrefix(rStr, "#") {
			continue
		}
		parts := strings.Split(rStr, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}

		item := RuleInspectItem{Raw: rStr}
		if len(parts) >= 3 {
			item.Type = parts[0]
			item.Payload = parts[1]
			item.Target = parts[2]
			if len(parts) >= 4 && strings.EqualFold(parts[0], "RULE-SET") {
				item.Provider = parts[1]
			}
			ruleCountMap[item.Target]++
		} else if len(parts) == 2 {
			item.Type = parts[0]
			item.Target = parts[1]
			ruleCountMap[item.Target]++
		}
		parsedRules = append(parsedRules, item)
	}

	// 6. Inspect proxy-groups and classify into external vs self-hosted
	var groupsOut []GroupInspectItem
	selfGroupCount := 0
	extGroupCount := 0

	for _, g := range rawCfg.ProxyGroups {
		gName := g.Name
		isExt := true
		category := "external_default"
		tags := []string{}

		// Check if it's a self-hosted custom group or baseline self group
		cg, isCustom := customMap[gName]
		if strings.HasPrefix(gName, "自建-") || strings.HasPrefix(gName, "自建·") || gName == "不送中" {
			isExt = false
			if strings.Contains(gName, "线路") {
				category = "self_backbone"
				tags = append(tags, "自建·纯骨干")
			} else if strings.Contains(gName, "其他") {
				category = "self_resi"
				tags = append(tags, "自建·其他家宽")
			} else if strings.Contains(gName, "家宽") {
				category = "self_resi"
				tags = append(tags, "自建·家宽全家桶")
			} else if strings.Contains(gName, "不送中") || gName == "不送中" {
				category = "self_clean"
				tags = append(tags, "自建·防送中直通")
			} else if strings.Contains(gName, "送中") {
				category = "self_china_leak"
				tags = append(tags, "自建·送中隔离")
			} else {
				category = "self_custom"
				tags = append(tags, "自建·内部组")
			}
			if isCustom {
				if cg.Filter.Countries != nil && len(cg.Filter.Countries) > 0 {
					tags = append(tags, strings.Join(cg.Filter.Countries, "/"))
				}
				if cg.Filter.CleanOnly {
					tags = append(tags, "干净出口")
				}
			}
		} else if isCustom {
			isExt = false
			category = "self_custom"
			tags = append(tags, "自建·可配置")
			if cg.Filter.Countries != nil && len(cg.Filter.Countries) > 0 {
				tags = append(tags, strings.Join(cg.Filter.Countries, "/"))
			}
			if cg.Filter.CleanOnly {
				tags = append(tags, "干净出口")
			}
		} else {
			// External groups
			if strings.Contains(gName, "选择代理") || strings.Contains(gName, "手动选择") {
				category = "entry_selector"
				tags = append(tags, "全局入口")
			} else if strings.Contains(gName, "节点") || strings.Contains(gName, "外部") || strings.Contains(gName, "蜂窝云") {
				category = "external_airport"
				tags = append(tags, "外部机场")
			} else {
				category = "app_routing"
				tags = append(tags, "应用分流")
			}
			tags = append(tags, "外部默认·只读")
		}

		if isExt {
			extGroupCount++
		} else {
			selfGroupCount++
		}

		// Annotate member proxies
		proxiesOut := make([]ProxyInspectSummary, 0, len(g.Proxies))
		for _, pName := range g.Proxies {
			psum := ProxyInspectSummary{
				Name:         pName,
				IsSelfHosted: false,
			}
			if atom, ok := atomMap[pName]; ok {
				psum.Type = atom.Protocol
				psum.Country = atom.Country
				psum.Kind = atom.Kind
				psum.IsSelfHosted = true
				psum.LatencyMs = atom.LatencyMs
				psum.Online = atom.Online
				psum.Tags = atom.Tags
			} else {
				// 二次尝试：使用对称别名匹配器检索链式中继与别名原子节点
				matched := false
				for _, atom := range atoms {
					if publish.MatchMemberName(atom.Name, pName) {
						psum.Type = atom.Protocol
						psum.Country = atom.Country
						psum.Kind = atom.Kind
						psum.IsSelfHosted = true
						psum.LatencyMs = atom.LatencyMs
						psum.Online = atom.Online
						psum.Tags = atom.Tags
						matched = true
						break
					}
				}
				if !matched {
					if strings.Contains(pName, "链式") || strings.Contains(pName, "➔") {
						psum.Kind = "chain"
						psum.IsSelfHosted = true
						psum.Online = true
					} else if strings.Contains(pName, "家宽") {
						psum.Kind = "residential"
						psum.IsSelfHosted = true
						psum.Online = true
					} else if strings.Contains(pName, "IIJ") || strings.Contains(pName, "软银") || strings.Contains(pName, "Softbank") || strings.Contains(pName, "SoftBank") || strings.Contains(pName, "4837") || strings.Contains(pName, "LAX") || strings.Contains(pName, "洛杉矶") || strings.Contains(pName, "美西") || strings.Contains(pName, "ACCK") || strings.Contains(pName, "-SB") || strings.Contains(pName, "SB-") {
						psum.Kind = "transit"
						psum.IsSelfHosted = true
						psum.Online = true
					} else {
						psum.Kind = "airport"
					}
				}
			}
			if psum.Type == "" {
				psum.Type = rawProxyTypeMap[pName]
			}
			proxiesOut = append(proxiesOut, psum)
		}

		dispName := gName
		if isCustom && cg.DisplayName != "" {
			dispName = cg.DisplayName
		}

		item := GroupInspectItem{
			Name:        gName,
			DisplayName: dispName,
			Type:        g.Type,
			IsExternal:  isExt,
			Category:    category,
			Tags:        tags,
			Proxies:     proxiesOut,
			ProxyCount:  len(g.Proxies),
			RuleCount:   ruleCountMap[gName],
		}
		if isCustom {
			item.CustomDef = &cg
		}
		groupsOut = append(groupsOut, item)
	}

	// 7. Rule provider names
	ruleProviders := make([]string, 0, len(rawCfg.RuleProviders))
	for p := range rawCfg.RuleProviders {
		ruleProviders = append(ruleProviders, p)
	}
	sort.Strings(ruleProviders)

	// 8. Generate the real JavaScript Script Operator source code driving this file
	var script string
	if len(customGroups) > 0 {
		script, _ = publish.CompileScript(customGroups, atoms, strings.ToLower(name))
	}

	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	resp := SubscriptionInspectResponse{
		Status:        "ok",
		Subscription:  name,
		TotalProxies:  len(rawCfg.Proxies),
		TotalGroups:   len(rawCfg.ProxyGroups),
		SelfGroups:    selfGroupCount,
		ExtGroups:     extGroupCount,
		Groups:        groupsOut,
		Rules:         parsedRules,
		RuleProviders: ruleProviders,
		RoutingScript: script,
		UpdatedAt:     updatedAt,
	}

	return c.JSON(resp)
}
