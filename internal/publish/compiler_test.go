package publish

import (
	"strings"
	"testing"
	"vpn-probe/internal/model"
)

func TestCompileScriptOnlyUserGroups(t *testing.T) {
	atoms := []model.AtomNode{
		{ID: "a", Name: "US-Home", Country: "US", Kind: "residential"},
		{ID: "b", Name: "JP-Home", Country: "JP", Kind: "residential"},
	}
	g := model.CustomGroupDef{
		Name:        "self-us",
		DisplayName: "自建-美国",
		Enabled:     true,
		Type:        "select",
		Filter:      model.GroupFilter{Countries: []string{"US"}, Kinds: []string{"residential"}},
	}
	script, err := CompileScript([]model.CustomGroupDef{g}, atoms, "ashan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, `"name":"自建-美国"`) || !strings.Contains(script, "US-Home") || strings.Contains(script, "JP-Home") {
		t.Fatalf("unexpected script: %s", script)
	}

	// For Q: residential nodes must be strictly excluded
	scriptQ, err := CompileScript([]model.CustomGroupDef{g}, atoms, "q")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(scriptQ, "US-Home") {
		t.Fatalf("Q must not contain residential node US-Home: %s", scriptQ)
	}
}

func TestCompileScriptAliasResolver(t *testing.T) {
	atoms := []model.AtomNode{
		{ID: "c1", Name: "链式|JP-Tokyo-IIJ-Reality ➔ LZ-US-Pavlov-家宽", Country: "US", Kind: "chain"},
	}
	g := model.CustomGroupDef{
		Name:        "self-us-chain",
		DisplayName: "自建-美国家宽",
		Enabled:     true,
		Type:        "select",
		Filter:      model.GroupFilter{Countries: []string{"US"}, Kinds: []string{"chain"}},
	}
	script, err := CompileScript([]model.CustomGroupDef{g}, atoms, "ashan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "resolveProxyName") {
		t.Fatalf("expected resolveProxyName in script")
	}
}

func TestCompileScript_Q_StrictRejectionManualMode(t *testing.T) {
	atoms := []model.AtomNode{
		{ID: "t1", Name: "US-LAX-4837", Country: "US", Kind: "transit"},
		{ID: "r1", Name: "LZ-US-Pavlov-家宽", Country: "US", Kind: "residential"},
		{ID: "c1", Name: "链式|US-LAX-4837 ➔ LZ-US-Pavlov-家宽", Country: "US", Kind: "chain"},
	}
	// Manual selection specifying residential and chain along with transit
	g := model.CustomGroupDef{
		Name:        "manual-mixed",
		DisplayName: "手动混合组",
		Enabled:     true,
		Type:        "select",
		Filter: model.GroupFilter{
			Mode:    "manual",
			NodeIDs: []string{"t1", "r1", "c1"},
		},
	}
	// Ashan should have all 3
	scriptAshan, err := CompileScript([]model.CustomGroupDef{g}, atoms, "ashan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scriptAshan, "US-LAX-4837") || !strings.Contains(scriptAshan, "LZ-US-Pavlov-家宽") {
		t.Fatalf("ashan must contain all matched manual nodes: %s", scriptAshan)
	}

	// Q must ONLY contain transit (US-LAX-4837), strictly dropping residential and chain
	scriptQ, err := CompileScript([]model.CustomGroupDef{g}, atoms, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scriptQ, "US-LAX-4837") {
		t.Fatalf("q must contain transit node: %s", scriptQ)
	}
	if strings.Contains(scriptQ, "LZ-US-Pavlov-家宽") {
		t.Fatalf("q must NOT contain residential node even in manual mode: %s", scriptQ)
	}
	if strings.Contains(scriptQ, "链式|US-LAX-4837") {
		t.Fatalf("q must NOT contain chain node even in manual mode: %s", scriptQ)
	}
}

func TestMatchMemberName_Symmetric(t *testing.T) {
	raw := "链式|JP-Tokyo-IIJ-Reality ➔ LZ-US-Pavlov-家宽"
	alias := "IIJ-美宽-Pavlov"

	if !matchMemberName(raw, alias) {
		t.Fatalf("expected matchMemberName(raw, alias) to be true")
	}
	if !matchMemberName(alias, raw) {
		t.Fatalf("expected matchMemberName(alias, raw) to be true (symmetric)")
	}
	if !matchMemberName("DIRECT", "DIRECT") {
		t.Fatalf("expected exact match to be true")
	}
	if matchMemberName(raw, "SB-日宽-SoftBank") {
		t.Fatalf("expected mismatch to return false")
	}
}
