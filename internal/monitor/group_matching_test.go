package monitor

import (
	"testing"
	"vpn-probe/internal/model"
)

func TestMatchAtomsManualFilterAndTags(t *testing.T) {
	nodes := []model.AtomNode{{ID: "one", Name: "home US", Country: "US", Kind: "residential", Tags: []string{"home"}, InfraType: model.InfraNATResidential, Online: true}, {ID: "two", Name: "home JP", Country: "JP", Kind: "residential", Tags: []string{"home"}, InfraType: model.InfraVPSResidential, ChinaExit: true, Online: true}}
	filter := model.GroupFilter{Countries: []string{"US"}, Kinds: []string{"residential"}, InfraTypes: []model.InfraType{model.InfraNATResidential}, TagsAny: []string{"home"}, CleanOnly: true}
	out := MatchAtoms(nodes, filter)
	if len(out) != 1 || out[0].ID != "one" {
		t.Fatalf("unexpected matched: %+v", out)
	}
	out = MatchAtoms(nodes, model.GroupFilter{Mode: "manual", NodeIDs: []string{"two"}})
	if len(out) != 1 || out[0].ID != "two" {
		t.Fatalf("bad manual selection: %+v", out)
	}
	out = MatchAtoms(nodes, model.GroupFilter{Mode: "manual", NodeIDs: []string{"two"}, CleanOnly: true})
	if len(out) != 0 {
		t.Fatal("china exit not filtered")
	}

	// Test ChinaLeakOnly
	leakFilter := model.GroupFilter{
		Kinds:         []string{"residential"},
		ChinaLeakOnly: true,
	}
	leakOut := MatchAtoms(nodes, leakFilter)
	if len(leakOut) != 1 || leakOut[0].ID != "two" {
		t.Fatalf("expected only node 'two' for ChinaLeakOnly, got: %+v", leakOut)
	}

	// Verify DefaultCustomGroups ashan-china-leak matches leaked nodes and excludes clean nodes
	defaultGroups := model.DefaultCustomGroups()
	var chinaLeakGroup *model.CustomGroupDef
	for i := range defaultGroups {
		if defaultGroups[i].Name == "ashan-china-leak" {
			chinaLeakGroup = &defaultGroups[i]
			break
		}
	}
	if chinaLeakGroup == nil {
		t.Fatal("missing default group ashan-china-leak")
	}
	matchedLeak := MatchAtoms(nodes, chinaLeakGroup.Filter)
	if len(matchedLeak) != 1 || matchedLeak[0].ID != "two" {
		t.Fatalf("expected ashan-china-leak default group to isolate node 'two', got: %+v", matchedLeak)
	}

	// Test UK and GB synonym normalization in MatchAtoms
	ukNodes := []model.AtomNode{
		{ID: "uk-1", Name: "London-01", Country: "UK", Kind: "residential", Online: true},
		{ID: "gb-1", Name: "London-02", Country: "GB", Kind: "residential", Online: true},
	}
	filterUK := model.GroupFilter{Countries: []string{"UK"}, Kinds: []string{"residential"}}
	matchedUK := MatchAtoms(ukNodes, filterUK)
	if len(matchedUK) != 2 {
		t.Fatalf("expected both UK and GB nodes to match filter with 'UK', got %d", len(matchedUK))
	}
	filterGB := model.GroupFilter{Countries: []string{"GB"}, Kinds: []string{"residential"}}
	matchedGB := MatchAtoms(ukNodes, filterGB)
	if len(matchedGB) != 2 {
		t.Fatalf("expected both UK and GB nodes to match filter with 'GB', got %d", len(matchedGB))
	}
}
