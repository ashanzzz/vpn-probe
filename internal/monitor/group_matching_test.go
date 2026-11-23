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
}
