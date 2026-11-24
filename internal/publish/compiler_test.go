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

	if !MatchMemberName(raw, alias) {
		t.Fatalf("expected MatchMemberName(raw, alias) to be true")
	}
	if !MatchMemberName(alias, raw) {
		t.Fatalf("expected MatchMemberName(alias, raw) to be true (symmetric)")
	}
	if !MatchMemberName("DIRECT", "DIRECT") {
		t.Fatalf("expected MatchMemberName(DIRECT, DIRECT) to be true")
	}
	if MatchMemberName(raw, "SB-日宽-SoftBank") {
		t.Fatalf("expected mismatch to return false")
	}
}

func TestMatchMemberName_UnicodePrefixAndChineseTransit(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		alias    string
		expected bool
	}{
		{
			name:     "Flag emoji with Chinese Softbank",
			raw:      "🇯🇵 链式|东京软银 ➔ LZ-JP-SoftBank-家宽",
			alias:    "SB-日宽-SoftBank",
			expected: true,
		},
		{
			name:     "Flag emoji with Chinese LAX 4837",
			raw:      "🇺🇸 链式|洛杉矶4837 ➔ LZ-US-Pavlov-家宽",
			alias:    "LAX-美宽-Pavlov",
			expected: true,
		},
		{
			name:     "Chinese West US transit name",
			raw:      "链式|美西洛杉矶专线线路机 — VLESS Reality ➔ LZ-US-Pavlov-家宽01",
			alias:    "LAX-美宽-Pavlov",
			expected: true,
		},
		{
			name:     "Direct node with flag emoji vs plain",
			raw:      "🇯🇵 JP-Tokyo-Softbank-Reality-Vision",
			alias:    "JP-Tokyo-Softbank-Reality-Vision",
			expected: true,
		},
		{
			name:     "Direct node with flag emoji vs lowercase plain",
			raw:      "🇯🇵 JP-Tokyo-Softbank-Reality-Vision",
			alias:    "jp-tokyo-softbank-reality-vision",
			expected: true,
		},
		{
			name:     "Direct node lowercase emoji vs uppercase plain",
			raw:      "🇯🇵 jp-tokyo-softbank-reality-vision",
			alias:    "JP-Tokyo-Softbank-Reality-Vision",
			expected: true,
		},
		{
			name:     "Case-insensitive alias matching",
			raw:      "链式|JP-Tokyo-Softbank-Reality-Vision ➔ LZ-JP-SoftBank-家宽",
			alias:    "sb-日宽-softbank",
			expected: true,
		},
		{
			name:     "Case-insensitive lowercase transit and resi chain matching",
			raw:      "链式|jp-tokyo-iij-reality ➔ lz-us-pavlov-家宽",
			alias:    "IIJ-美宽-Pavlov",
			expected: true,
		},
		{
			name:     "Unrelated mismatch",
			raw:      "链式|JP-Tokyo-IIJ-Reality ➔ LZ-US-Pavlov-家宽",
			alias:    "SB-日宽-SoftBank",
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotForward := MatchMemberName(tc.raw, tc.alias)
			if gotForward != tc.expected {
				t.Errorf("MatchMemberName(%q, %q) = %v; want %v", tc.raw, tc.alias, gotForward, tc.expected)
			}
			gotBackward := MatchMemberName(tc.alias, tc.raw)
			if gotBackward != tc.expected {
				t.Errorf("MatchMemberName(%q, %q) = %v; want %v", tc.alias, tc.raw, gotBackward, tc.expected)
			}
		})
	}
}

func TestCompileScript_EmptyProxiesFallbackInScript(t *testing.T) {
	script, err := CompileScript([]model.CustomGroupDef{
		{
			Name:        "test-group",
			DisplayName: "Test Group",
			Enabled:     true,
			Type:        "select",
		},
	}, nil)
	if err != nil {
		t.Fatalf("CompileScript: %v", err)
	}
	if !strings.Contains(script, "g.proxies.length === 0") || !strings.Contains(script, "['DIRECT']") {
		t.Fatalf("expected script to contain defensive empty proxies fallback to DIRECT")
	}
}

func TestCompileScript_PreservesUserGroupsInOtherProxyGroups(t *testing.T) {
	script, err := CompileScript([]model.CustomGroupDef{
		{
			Name:        "ashan-tw-resi",
			DisplayName: "自建-台湾家宽分组",
			Enabled:     true,
			Type:        "select",
			AddToEntry:  true,
		},
	}, nil)
	if err != nil {
		t.Fatalf("CompileScript: %v", err)
	}
	// Verify that the script does not strip vpnDefined from all groups' proxies
	if strings.Contains(script, "g.proxies = g.proxies.filter(n => !vpnDefined.has(n));") {
		t.Fatalf("script should not unconditionally purge user-defined groups from all existing policy groups")
	}
	if !strings.Contains(script, "allowedAdditions") {
		t.Fatalf("expected script to contain allowedAdditions logic for entry group")
	}
}
