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
			DisplayName: "自建-台湾家宽",
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

func TestCompileScript_CleanExternalAndNodePools(t *testing.T) {
	script, err := CompileScript(model.DefaultCustomGroups(), nil)
	if err != nil {
		t.Fatalf("CompileScript: %v", err)
	}
	if !strings.Contains(script, "isNodePool") {
		t.Fatalf("expected isNodePool check in script")
	}
	if !strings.Contains(script, "!isSelfName(p)") {
		t.Fatalf("expected !isSelfName(p) check in script to purge self groups from external pools")
	}
	if !strings.Contains(script, "topNames = ['选择代理', '自动选择'") {
		t.Fatalf("expected topNames to place 选择代理 and 自动选择 at topmost")
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
			map[string]any{"name": "UK-Node-1"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "选择代理", "proxies": []any{"外部分组", "自建-线路机"}},
			map[string]any{"name": "手动选择", "proxies": []any{"自动选择", "外部分组", "自建-线路机"}},
			map[string]any{"name": "外部分组", "proxies": []any{"ExternalNode1"}},
			map[string]any{"name": "自动选择", "proxies": []any{"ExternalNode1", "UK-Node-1"}},
			map[string]any{"name": "英国节点", "proxies": []any{"自建-线路机", "自建-美国家宽", "UK-Node-1"}},
			map[string]any{"name": "AI服务", "proxies": []any{"自建-线路机", "自建-美国家宽", "ExternalNode1"}},
			map[string]any{"name": "TikTok", "proxies": []any{"自建-线路机", "ExternalNode1"}},
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

	// 1. Check topmost groups
	if len(processed.Groups) < 2 || processed.Groups[0].Name != "选择代理" || processed.Groups[1].Name != "自动选择" {
		t.Fatalf("expected 选择代理 and 自动选择 at index 0 and 1, got [0]=%s, [1]=%s", processed.Groups[0].Name, processed.Groups[1].Name)
	}

	// 2. Check 选择代理 inner proxies: 自动选择 first, self groups strictly trailing
	entryGroup := processed.Groups[0]
	if len(entryGroup.Proxies) == 0 || entryGroup.Proxies[0] != "自动选择" {
		t.Fatalf("expected '选择代理' first proxy to be '自动选择', got %v", entryGroup.Proxies)
	}

	// 3. Check 英国节点 (pure region pool): MUST NOT contain any self groups
	var ukGroupFound bool
	for _, g := range processed.Groups {
		if g.Name == "英国节点" {
			ukGroupFound = true
			for _, p := range g.Proxies {
				if strings.HasPrefix(p, "自建-") || p == "不送中" {
					t.Fatalf("CRITICAL BUG: '英国节点' node pool still contains self group '%s'!", p)
				}
			}
			if len(g.Proxies) != 1 || g.Proxies[0] != "UK-Node-1" {
				t.Fatalf("expected 英国节点 to only contain UK-Node-1, got: %v", g.Proxies)
			}
		}
	}
	if !ukGroupFound {
		t.Fatalf("expected 英国节点 group in processed groups")
	}

	// 4. Check external app groups (AI服务, TikTok):
	// Unified layout: ['自动选择', '选择代理', ...external members, ...self-built groups at tail]
	for _, g := range processed.Groups {
		if g.Name == "TikTok" || g.Name == "AI服务" {
			if len(g.Proxies) < 3 {
				t.Fatalf("expected external app group '%s' to have at least 3 proxies, got: %v", g.Name, g.Proxies)
			}
			if g.Proxies[0] != "自动选择" {
				t.Fatalf("expected '%s' first proxy to be '自动选择', got: %v", g.Name, g.Proxies[0])
			}
			if g.Proxies[1] != "选择代理" {
				t.Fatalf("expected '%s' second proxy to be '选择代理', got: %v", g.Name, g.Proxies[1])
			}
			// Verify that self-built groups are at the tail
			lastProxy := g.Proxies[len(g.Proxies)-1]
			if !strings.HasPrefix(lastProxy, "自建-") && lastProxy != "不送中" {
				t.Fatalf("expected '%s' tail proxy to be self group, got: %s (all: %v)", g.Name, lastProxy, g.Proxies)
			}
		}
	}

	// 5. Check self groups strictly at bottom
	var seenSelf bool
	for _, g := range processed.Groups {
		isSelf := strings.HasPrefix(g.Name, "自建-") || g.Name == "不送中"
		if isSelf {
			seenSelf = true
		} else if seenSelf {
			t.Fatalf("CRITICAL BUG: non-self group '%s' found AFTER self group! Self groups must be strictly at the end.", g.Name)
		}
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
			if (g.Name == "选择代理" || g.Name == "谷歌服务" || g.Name == "Youtube") && (p == "自建-送中" || (strings.Contains(p, "送中") && !strings.Contains(p, "不送中"))) {
				t.Fatalf("clean group %s illegally contains dirty group %s", g.Name, p)
			}
		}
	}
}

func TestCompileScript_CleanNonLeakGroupPreserved(t *testing.T) {
	// 验证自建-不送中组设置了 add_to_entry 时，不会被 isDirtyLeakName 误判为送中脏组而剔除
	cleanGroup := model.CustomGroupDef{
		Name:        "ashan-clean",
		DisplayName: "自建-不送中",
		Enabled:     true,
		Type:        "url-test",
		Order:       70,
		AddToEntry:  true,
		Filter:      model.GroupFilter{CleanOnly: true},
	}
	leakGroup := model.CustomGroupDef{
		Name:        "ashan-china-leak",
		DisplayName: "自建-送中",
		Enabled:     true,
		Type:        "select",
		Order:       99,
		AddToEntry:  true,
		Filter:      model.GroupFilter{ChinaLeakOnly: true},
	}
	atoms := []model.AtomNode{
		{Name: "Clean-Transit-1", Kind: "transit", Country: "US", ChinaExit: false},
		{Name: "Leaked-Transit-1", Kind: "transit", Country: "US", ChinaExit: true},
	}

	script, err := CompileScript([]model.CustomGroupDef{cleanGroup, leakGroup}, atoms, "ashan")
	if err != nil {
		t.Fatalf("CompileScript failed: %v", err)
	}

	mockCfg := map[string]any{
		"proxies": []any{
			map[string]any{"name": "Clean-Transit-1"},
			map[string]any{"name": "Leaked-Transit-1"},
			map[string]any{"name": "AirportNode1"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "选择代理", "proxies": []any{"自动选择", "AirportNode1"}},
			map[string]any{"name": "自动选择", "proxies": []any{"AirportNode1"}},
			map[string]any{"name": "谷歌服务", "proxies": []any{"AirportNode1"}},
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

	// 1. 验证 选择代理 (entry) 必须包含 自建-不送中，但绝不能包含 自建-送中
	var entryProxies []string
	for _, g := range processed.Groups {
		if g.Name == "选择代理" {
			entryProxies = g.Proxies
			break
		}
	}
	hasClean := false
	hasLeak := false
	for _, p := range entryProxies {
		if p == "自建-不送中" {
			hasClean = true
		}
		if p == "自建-送中" {
			hasLeak = true
		}
	}
	if !hasClean {
		t.Errorf("expected 选择代理 to contain clean group '自建-不送中', got: %v", entryProxies)
	}
	if hasLeak {
		t.Errorf("expected 选择代理 to EXCLUDE dirty leak group '自建-送中', got: %v", entryProxies)
	}
}
