package model

import "testing"

func TestCustomGroupValidate(t *testing.T) {
	g := CustomGroupDef{Name: "self-us", DisplayName: "自建-美国", Filter: GroupFilter{Mode: "filter", Kinds: []string{"residential"}}}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	g.Filter.CustomMatchRegex = "["
	if err := g.Validate(); err == nil {
		t.Fatal("invalid regex should fail")
	}
	g.Filter.CustomMatchRegex = ""
	g.Filter.Mode = "manual"
	if err := g.Validate(); err == nil {
		t.Fatal("manual group needs IDs")
	}
}

func TestDefaultCustomGroups_Ashan(t *testing.T) {
	groups := DefaultCustomGroups("ashan")
	expectedNames := map[string]string{
		"ashan-transit":     "自建-线路机",
		"ashan-us-resi":     "自建-美国家宽",
		"ashan-jp-resi":     "自建-日本家宽",
		"ashan-tw-resi":     "自建-台湾家宽",
		"ashan-other":       "自建-其他",
		"ashan-residential": "自建-家宽分组",
		"ashan-clean":       "自建-不送中",
		"ashan-china-leak":  "自建-送中",
	}

	for _, g := range groups {
		expectedDisplayName, ok := expectedNames[g.Name]
		if !ok {
			t.Errorf("unexpected group name %s", g.Name)
			continue
		}
		if g.DisplayName != expectedDisplayName {
			t.Errorf("group %s: expected DisplayName %q, got %q", g.Name, expectedDisplayName, g.DisplayName)
		}
		if g.Name == "ashan-us-resi" || g.Name == "ashan-jp-resi" || g.Name == "ashan-tw-resi" {
			for _, kind := range g.Filter.Kinds {
				if kind == "transit" {
					t.Errorf("group %s should NOT contain 'transit' kind in kinds filter: %v", g.Name, g.Filter.Kinds)
				}
			}
		}
	}
}
