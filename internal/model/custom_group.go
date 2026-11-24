package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// InfraType describes the actual hosting network, independent of the proxy role.
type InfraType string

const (
	InfraNATResidential InfraType = "nat_resi"
	InfraVPSResidential InfraType = "vps_resi"
	InfraNATLine        InfraType = "nat_line"
	InfraVPSLine        InfraType = "vps_line"
	InfraCommercial     InfraType = "commercial"
)

// NodeMetadata is intentionally user-maintained: a port or ASN does not prove NAT/ISP ownership.
type NodeMetadata struct {
	InfraType InfraType `json:"infra_type,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
}

// AtomNode is a probe result with editable, non-secret classification metadata.
type AtomNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Protocol  string    `json:"protocol"`
	Country   string    `json:"country"`
	Kind      string    `json:"kind"`
	InfraType InfraType `json:"infra_type,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	ChinaExit bool      `json:"china_exit"`
	LatencyMs int       `json:"latency_ms"`
	Online    bool      `json:"online"`
	TestedAt  time.Time `json:"tested_at"`
}

// GroupFilter supports the former three-kind API as well as a single canonical new contract.
// mode = manual selects stable node IDs; mode = filter matches user-chosen rules.
type GroupFilter struct {
	Mode               string      `json:"mode,omitempty"`
	NodeIDs            []string    `json:"node_ids,omitempty"`
	Countries          []string    `json:"countries,omitempty"`
	Kinds              []string    `json:"kinds,omitempty"`
	InfraTypes         []InfraType `json:"infra_types,omitempty"`
	TagsAny            []string    `json:"tags_any,omitempty"`
	TagsAll            []string    `json:"tags_all,omitempty"`
	ExcludeTags        []string    `json:"exclude_tags,omitempty"`
	Keywords           []string    `json:"keywords,omitempty"`
	Exclude            []string    `json:"exclude,omitempty"`
	OnlineOnly         bool        `json:"online_only,omitempty"`
	CleanOnly          bool        `json:"clean_only,omitempty"`
	ChinaLeakOnly      bool        `json:"china_leak_only,omitempty"`
	LeakOnly           bool        `json:"leak_only,omitempty"`
	IncludeTransit     bool        `json:"include_transit"`
	IncludeResidential bool        `json:"include_residential"`
	IncludeChain       bool        `json:"include_chain"`
	ExcludeChinaLeak   bool        `json:"exclude_china_leak"`
	CustomMatchRegex   string      `json:"custom_match_regex,omitempty"`
}

// CustomGroupDef is a USER-created visible Clash group, not a Sub-Store source pool.
type CustomGroupDef struct {
	Name        string      `json:"name"` // immutable stable ID; used by old APIs
	DisplayName string      `json:"display_name"`
	Enabled     bool        `json:"enabled"`
	Type        string      `json:"type,omitempty"` // select | url-test | fallback | load-balance
	Order       int         `json:"order,omitempty"`
	AddToEntry  bool        `json:"add_to_entry,omitempty"`
	Filter      GroupFilter `json:"filter"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

func (g CustomGroupDef) Validate() error {
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`).MatchString(g.Name) {
		return fmt.Errorf("group ID must be 1-64 letters/digits/_/- and start with a letter")
	}
	if strings.TrimSpace(g.DisplayName) == "" || len([]rune(g.DisplayName)) > 80 {
		return fmt.Errorf("display_name must be 1-80 characters")
	}
	switch g.Type {
	case "", "select", "url-test", "fallback", "load-balance":
	default:
		return fmt.Errorf("unsupported group type %q", g.Type)
	}
	f := g.Filter
	if f.Mode != "" && f.Mode != "filter" && f.Mode != "manual" {
		return fmt.Errorf("mode must be filter or manual")
	}
	if f.Mode == "manual" && len(f.NodeIDs) == 0 {
		return fmt.Errorf("manual selection needs node_ids")
	}
	for _, k := range f.Kinds {
		if k != "transit" && k != "residential" && k != "chain" {
			return fmt.Errorf("unsupported kind %q", k)
		}
	}
	for _, it := range f.InfraTypes {
		switch it {
		case InfraNATResidential, InfraVPSResidential, InfraNATLine, InfraVPSLine, InfraCommercial:
		default:
			return fmt.Errorf("unsupported infra_type %q", it)
		}
	}
	if f.CustomMatchRegex != "" {
		if _, err := regexp.Compile(f.CustomMatchRegex); err != nil {
			return fmt.Errorf("invalid custom_match_regex: %w", err)
		}
	}
	return nil
}

// Existing internal pools remain independent from user-visible custom groups.
type FiveGroupPayload struct {
	TransitNodes   []string `json:"transit_nodes"`
	USResiNodes    []string `json:"us_resi_nodes"`
	JPResiNodes    []string `json:"jp_resi_nodes"`
	OtherNodes     []string `json:"other_nodes"`
	ChinaLeakNodes []string `json:"china_leak_nodes"`
}

// DefaultCustomGroups returns canonical baseline system policy groups for ashan or q.
func DefaultCustomGroups(profile ...string) []CustomGroupDef {
	if len(profile) > 0 && strings.EqualFold(profile[0], "q") {
		return []CustomGroupDef{
			{
				Name:        "q-transit",
				DisplayName: "自建-线路机",
				Enabled:     true,
				Type:        "url-test",
				Order:       10,
				AddToEntry:  true,
				Filter: GroupFilter{
					Mode:      "filter",
					Kinds:     []string{"transit"},
					CleanOnly: false,
				},
			},
			{
				Name:        "q-us-line",
				DisplayName: "自建-美国线路",
				Enabled:     true,
				Type:        "url-test",
				Order:       20,
				AddToEntry:  true,
				Filter: GroupFilter{
					Mode:      "filter",
					Countries: []string{"US"},
					Kinds:     []string{"transit"},
					CleanOnly: true,
				},
			},
			{
				Name:        "q-jp-line",
				DisplayName: "自建-日本线路",
				Enabled:     true,
				Type:        "url-test",
				Order:       30,
				AddToEntry:  true,
				Filter: GroupFilter{
					Mode:      "filter",
					Countries: []string{"JP"},
					Kinds:     []string{"transit"},
					CleanOnly: true,
				},
			},
			{
				Name:        "q-clean",
				DisplayName: "自建-不送中",
				Enabled:     true,
				Type:        "url-test",
				Order:       40,
				AddToEntry:  true,
				Filter: GroupFilter{
					Mode:      "filter",
					Kinds:     []string{"transit"},
					CleanOnly: true,
				},
			},
			{
				Name:        "q-china-leak",
				DisplayName: "自建-送中",
				Enabled:     true,
				Type:        "url-test",
				Order:       50,
				AddToEntry:  false,
				Filter: GroupFilter{
					Mode:          "filter",
					Kinds:         []string{"transit"},
					ChinaLeakOnly: true,
				},
			},
		}
	}

	return []CustomGroupDef{
		{
			Name:        "ashan-transit",
			DisplayName: "自建-线路机",
			Enabled:     true,
			Type:        "url-test",
			Order:       10,
			AddToEntry:  true,
			Filter: GroupFilter{
				Mode:      "filter",
				Kinds:     []string{"transit"},
				CleanOnly: false,
			},
		},
		{
			Name:        "ashan-us-resi",
			DisplayName: "自建-美国家宽",
			Enabled:     true,
			Type:        "url-test",
			Order:       20,
			AddToEntry:  true,
			Filter: GroupFilter{
				Mode:      "filter",
				Countries: []string{"US"},
				Kinds:     []string{"residential", "chain"},
				CleanOnly: true,
			},
		},
		{
			Name:        "ashan-jp-resi",
			DisplayName: "自建-日本家宽",
			Enabled:     true,
			Type:        "url-test",
			Order:       30,
			AddToEntry:  true,
			Filter: GroupFilter{
				Mode:      "filter",
				Countries: []string{"JP"},
				Kinds:     []string{"residential", "chain"},
				CleanOnly: true,
			},
		},
		{
			Name:        "ashan-tw-resi",
			DisplayName: "自建-台湾家宽",
			Enabled:     true,
			Type:        "url-test",
			Order:       40,
			AddToEntry:  true,
			Filter: GroupFilter{
				Mode:      "filter",
				Countries: []string{"TW"},
				Kinds:     []string{"residential", "chain"},
				CleanOnly: true,
			},
		},
		{
			Name:        "ashan-other",
			DisplayName: "自建-其他",
			Enabled:     true,
			Type:        "url-test",
			Order:       50,
			AddToEntry:  true,
			Filter: GroupFilter{
				Mode:      "filter",
				Countries: []string{"SG", "KR", "UK", "DE", "OTHER"},
				Kinds:     []string{"residential", "chain"},
				CleanOnly: true,
			},
		},
		{
			Name:        "ashan-residential",
			DisplayName: "自建-家宽分组",
			Enabled:     true,
			Type:        "url-test",
			Order:       60,
			AddToEntry:  true,
			Filter: GroupFilter{
				Mode:      "filter",
				Kinds:     []string{"residential", "chain"},
				CleanOnly: true,
			},
		},
		{
			Name:        "ashan-clean",
			DisplayName: "自建-不送中",
			Enabled:     true,
			Type:        "url-test",
			Order:       70,
			AddToEntry:  true,
			Filter: GroupFilter{
				Mode:      "filter",
				Kinds:     []string{"transit", "residential", "chain"},
				CleanOnly: true,
			},
		},
		{
			Name:        "ashan-china-leak",
			DisplayName: "自建-送中",
			Enabled:     true,
			Type:        "url-test",
			Order:       80,
			AddToEntry:  false,
			Filter: GroupFilter{
				Mode:          "filter",
				Kinds:         []string{"transit", "residential", "chain"},
				ChinaLeakOnly: true,
			},
		},
	}
}
