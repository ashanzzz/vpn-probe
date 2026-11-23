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
}
