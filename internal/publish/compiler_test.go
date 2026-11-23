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
