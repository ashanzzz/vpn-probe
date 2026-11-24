package publish

import (
	"encoding/json"
	"os/exec"
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

func TestCompileScript_AppGroupCleanInject(t *testing.T) {
	script, err := CompileScript(model.DefaultCustomGroups(), nil)
	if err != nil {
		t.Fatalf("CompileScript: %v", err)
	}
	if !strings.Contains(script, "appGroupCleanInject") {
		t.Fatalf("expected appGroupCleanInject in script")
	}
	if !strings.Contains(script, "'Youtube'") || !strings.Contains(script, "'YouTube'") {
		t.Fatalf("expected both 'Youtube' and 'YouTube' keys in appGroupCleanInject")
	}
	if !strings.Contains(script, "toLowerCase") {
		t.Fatalf("expected toLowerCase case-insensitive matching in appGroupCleanInject")
	}
	if !strings.Contains(script, "!finalProxies.includes(c)") {
		t.Fatalf("expected proxy deduplication in appGroupCleanInject")
	}
	if !strings.Contains(script, "finalProxies = ['选择代理']") {
		t.Fatalf("expected 选择代理 placed first in finalProxies")
	}
	if !strings.Contains(script, "'外部分组'") || !strings.Contains(script, "entryMembers.has(g.name)") {
		t.Fatalf("expected nonAppGroupNames and entryMembers check to prevent circular proxy-group loops")
	}
}

func TestCompileScript_VpnEntryAndGroupsDefensiveFallback(t *testing.T) {
	script, err := CompileScript(model.DefaultCustomGroups(), nil)
	if err != nil {
		t.Fatalf("CompileScript: %v", err)
	}
	if !strings.Contains(script, "!Array.isArray(vpnEntry.proxies) || vpnEntry.proxies.length === 0") {
		t.Fatalf("expected defensive fallback for vpnEntry.proxies")
	}
	if !strings.Contains(script, "!Array.isArray(g.proxies) || g.proxies.length === 0") {
		t.Fatalf("expected final defensive loop over all groups")
	}
}

func TestNodeProcessConfigAcyclic(t *testing.T) {
	groups := model.DefaultCustomGroups()
	atoms := []model.AtomNode{
		{Name: "US-LAX-ACCK-4837-Reality", Kind: "transit", Country: "US"},
		{Name: "JP-Tokyo-IIJ-Reality", Kind: "transit", Country: "JP"},
		{Name: "JP-Tokyo-Softbank-Reality-Vision", Kind: "transit", Country: "JP"},
	}
	script, err := CompileScript(groups, atoms, "ashan")
	if err != nil {
		t.Fatalf("CompileScript: %v", err)
	}

	mockCfg := map[string]any{
		"proxies": []any{
			map[string]any{"name": "🇺🇸 US-LAX-ACCK-4837-Reality"},
			map[string]any{"name": "🇯🇵 JP-Tokyo-IIJ-Reality"},
			map[string]any{"name": "ExternalNode1"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "选择代理", "proxies": []any{"自动选择", "外部分组", "自建-线路机"}},
			map[string]any{"name": "手动选择", "proxies": []any{"自动选择", "外部分组", "自建-线路机"}},
			map[string]any{"name": "外部分组", "proxies": []any{"ExternalNode1"}},
			map[string]any{"name": "AI服务", "proxies": []any{"选择代理", "自建-线路机"}},
			map[string]any{"name": "金融服务", "proxies": []any{"选择代理", "自建-线路机"}},
		},
	}
	cfgBytes, _ := json.Marshal(mockCfg)

	jsCode := script + `
const input = ` + string(cfgBytes) + `;
const processed = processConfig(input);
console.log(JSON.stringify(processed));
`
	cmd := exec.Command("node", "-e", jsCode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node execution failed: %v, out: %s", err, string(out))
	}

	var processed struct {
		Groups []struct {
			Name    string   `json:"name"`
			Proxies []string `json:"proxies"`
		} `json:"proxy-groups"`
	}
	if err := json.Unmarshal(out, &processed); err != nil {
		t.Fatalf("unmarshal processed: %v", err)
	}

	for _, g := range processed.Groups {
		if g.Name == "外部分组" {
			for _, p := range g.Proxies {
				if p == "选择代理" {
					t.Fatalf("CRITICAL BUG: '外部分组' still contains '选择代理'!")
				}
			}
		}
	}
}

func TestNodeProcessConfigSanitizeUnsupportedAndLeak(t *testing.T) {
	groups := model.DefaultCustomGroups()
	atoms := []model.AtomNode{
		{Name: "US-LAX-ACCK-4837-Reality", Kind: "transit", Country: "US"},
		{Name: "JP-Tokyo-IIJ-Reality", Kind: "transit", Country: "JP"},
	}
	script, err := CompileScript(groups, atoms, "ashan")
	if err != nil {
		t.Fatalf("CompileScript: %v", err)
	}

	mockCfg := map[string]any{
		"proxies": []any{
			map[string]any{"name": "Valid-Node-1", "type": "vless", "server": "1.1.1.1"},
			map[string]any{"name": "Valid-Node-1", "type": "vless", "server": "1.1.1.1"}, // duplicate by name
			map[string]any{"name": "Bad-AnyTLS", "type": "anytls", "server": "2.2.2.2"},   // unsupported protocol
			map[string]any{"name": "到期时间: 2026-12", "type": "vmess", "server": "3.3.3.3"}, // informational
		},
		"proxy-groups": []any{
			map[string]any{"name": "选择代理", "proxies": []any{"自动选择", "Valid-Node-1", "Bad-AnyTLS", "自建-送中"}},
			map[string]any{"name": "谷歌服务", "proxies": []any{"选择代理", "Valid-Node-1", "Bad-AnyTLS", "自建-送中"}},
			map[string]any{"name": "Youtube", "proxies": []any{"选择代理", "Valid-Node-1", "自建-送中"}},
		},
	}
	cfgBytes, _ := json.Marshal(mockCfg)

	jsCode := script + `
const input = ` + string(cfgBytes) + `;
const processed = processConfig(input);
console.log(JSON.stringify(processed));
`
	cmd := exec.Command("node", "-e", jsCode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node execution failed: %v, out: %s", err, string(out))
	}

	var processed struct {
		Proxies []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"proxies"`
		Groups []struct {
			Name    string   `json:"name"`
			Proxies []string `json:"proxies"`
		} `json:"proxy-groups"`
	}
	if err := json.Unmarshal(out, &processed); err != nil {
		t.Fatalf("unmarshal processed: %v", err)
	}

	// 1. Assert proxies sanitization (no anytls, no informational, no duplicate names)
	if len(processed.Proxies) != 1 {
		t.Fatalf("expected exactly 1 sanitized proxy, got %d: %+v", len(processed.Proxies), processed.Proxies)
	}
	if processed.Proxies[0].Name != "Valid-Node-1" {
		t.Fatalf("expected Valid-Node-1, got %s", processed.Proxies[0].Name)
	}

	// 2. Assert clean groups do NOT contain Bad-AnyTLS or 自建-送中
	for _, g := range processed.Groups {
		for _, p := range g.Proxies {
			if p == "Bad-AnyTLS" {
				t.Fatalf("group %s still contains purged node Bad-AnyTLS", g.Name)
			}
			if (g.Name == "选择代理" || g.Name == "谷歌服务" || g.Name == "Youtube") && (p == "自建-送中" || strings.Contains(p, "送中")) {
				t.Fatalf("clean group %s illegally contains dirty group %s", g.Name, p)
			}
		}
	}
}
