package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/healer"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
)

func TestOrchestratorEvaluationAndQuery(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "custom_groups.json")
	restore := SetCustomGroupsFileForTest(tmpFile)
	defer restore()
	tmpChains := filepath.Join(t.TempDir(), "custom_chains.json")
	defer SetCustomChainsFileForTest(tmpChains)()

	h := healer.NewHealer(nil, nil)
	sch := &Scheduler{
		healer:     h,
		nodeStates: make(map[string]*NodeState),
	}

	// 注入节点状态：
	// 1. 美西线路机 (US, Transit, Clean) - 任何线路机，不限4837
	sch.nodeStates["美西线路机"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "1",
			Name:        "美西洛杉矶专线线路机 — VLESS Reality",
			OriginalURL: "vless://u1@38.175.222.44:36564#US-LAX-Relay",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
			TestedAt:    time.Now(),
		},
	}
	// 2. LZ-US-家宽 (US, Residential, Clean)
	sch.nodeStates["美国家宽01"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "2",
			Name:        "LZ-US-Pavlov-家宽01",
			OriginalURL: "socks5://u:***@1.2.3.4:1001#LZ-US-家宽01",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
			TestedAt:    time.Now(),
		},
	}
	// 3. 东京软银 (JP, Transit, Clean)
	sch.nodeStates["东京软银"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "3",
			Name:        "东京软银 — VLESS Reality Vision",
			OriginalURL: "vless://u3@45.192.242.72:24444#JP-SoftBank",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
			TestedAt:    time.Now(),
		},
	}
	// 4. 送中节点 (US, Transit, Leaked)
	sch.nodeStates["送中测试"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "4",
			Name:        "某送中机房",
			OriginalURL: "vless://u4@9.9.9.9:443#Leaked-CN",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true,
			TestedAt:    time.Now(),
		},
	}

	orc := NewOrchestrator(sch)

	// 1. 测试原子查询：查询美区线路机
	usTransits := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "transit", CleanOnly: true})
	if len(usTransits) != 1 || !strings.Contains(usTransits[0].Name, "美西洛杉矶专线") {
		t.Fatalf("expected 1 clean US transit, got %v", usTransits)
	}

	// 1.1 测试原子查询：查询送中隔离节点 (ChinaLeakOnly)
	leakedAtoms := orc.QueryAtoms(AtomQueryFilter{ChinaLeakOnly: true})
	if len(leakedAtoms) != 1 || leakedAtoms[0].Name != "某送中机房" {
		t.Fatalf("expected 1 leaked atom named '某送中机房', got %v", leakedAtoms)
	}

	// 2. 测试原子查询：查询美国家宽直连
	usResis := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "residential", CleanOnly: true})
	if len(usResis) != 1 || !strings.Contains(usResis[0].Name, "LZ-US") {
		t.Fatalf("expected 1 US residential, got %v", usResis)
	}

	// 3. 测试原子查询：查询线路拉家宽链式中继 (全量网状挂载: 美西专线 + 东京软银 均拉取美国家宽，送中节点物理隔离排除)
	usChains := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "chain"})
	if len(usChains) != 2 || !strings.Contains(usChains[0].Name, "链式") {
		t.Fatalf("expected 2 clean US chains, got %v", usChains)
	}

	// 4. 测试动态公式编排求值：连接 = 美国家宽 + 美国线路机 + 线路家宽机
	bundleFilter := model.GroupFilter{
		Countries:          []string{"US"},
		IncludeTransit:     true, // 美国线路机 (1)
		IncludeResidential: true, // 美国家宽 (1)
		IncludeChain:       true, // 线路家宽机 (2: 美西拉美国家宽 + 东京软银拉美国家宽)
		ExcludeChinaLeak:   true, // 排除送中 (排除送中机房)
	}

	bundleURLs := orc.EvaluateGroup(bundleFilter)
	// 应当恰好返回 4 条连接 (1 线路 + 1 家宽直连 + 2 链式中继：美西与软银分别挂载美国家宽)
	if len(bundleURLs) != 4 {
		t.Fatalf("expected 4 nodes for US bundle formula, got %d: %v", len(bundleURLs), bundleURLs)
	}
	for _, u := range bundleURLs {
		if strings.Contains(u, "Leaked-CN") {
			t.Errorf("leak node must not be in US bundle: %s", u)
		}
	}

	// 5. 测试持久化 Save / Load / Delete
	def := model.CustomGroupDef{
		Name:        "ashan-us-bundle",
		DisplayName: "自建-美国全家桶",
		Enabled:     true,
		Filter:      bundleFilter,
	}

	if err := SaveCustomGroup(def); err != nil {
		t.Fatalf("SaveCustomGroup failed: %v", err)
	}

	loaded, err := LoadCustomGroups()
	if err != nil || len(loaded) != 1 || loaded[0].Name != "ashan-us-bundle" {
		t.Fatalf("LoadCustomGroups failed, got: %v, err: %v", loaded, err)
	}

	if err := DeleteCustomGroup("ashan-us-bundle"); err != nil {
		t.Fatalf("DeleteCustomGroup failed: %v", err)
	}

	afterDel, _ := LoadCustomGroups()
	if len(afterDel) != 0 {
		t.Fatalf("expected 0 groups after delete, got %d", len(afterDel))
	}

	// 6. 测试显式动态拉线路：指定某台线路机拉某台家宽
	bind, err := orc.BindExplicitChain("美西洛杉矶专线线路机 — VLESS Reality", "LZ-US-Pavlov-家宽01")
	if err != nil {
		t.Fatalf("BindExplicitChain failed: %v", err)
	}
	if !strings.Contains(bind.ChainName, "美西洛杉矶专线") || !strings.Contains(bind.ChainName, "LZ-US") {
		t.Errorf("unexpected chain name: %s", bind.ChainName)
	}

	// 检查读取显式定制中继列表
	customChains, err := LoadCustomChains()
	if err != nil || len(customChains) == 0 {
		t.Fatalf("expected custom chains loaded, got: %v, err: %v", customChains, err)
	}

	// 测试解绑
	if err := orc.UnbindExplicitChain(bind.ID); err != nil {
		t.Fatalf("UnbindExplicitChain failed: %v", err)
	}
}

func TestOrchestrator_SyncAllCustomGroups_EmptyNodes(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "custom_groups.json")
	restore := SetCustomGroupsFileForTest(tmpFile)
	defer restore()

	// 注入一个匹配不到任何节点的自定义分组
	def := model.CustomGroupDef{
		Name:        "ashan-fr-empty",
		DisplayName: "自建-法国空分组",
		Enabled:     true,
		Filter: model.GroupFilter{
			Countries: []string{"FR"}, // 无法国节点
		},
	}
	if err := SaveCustomGroup(def); err != nil {
		t.Fatalf("SaveCustomGroup failed: %v", err)
	}

	var syncedName string
	var syncedContent string
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/sub/") {
			mu.Lock()
			syncedName = strings.TrimPrefix(r.URL.Path, "/api/sub/")
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if c, ok := body["content"].(string); ok {
				syncedContent = c
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	subClient := substore.NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     ts.URL,
	})
	subClient.SetHTTPClient(ts.Client())

	sch := &Scheduler{
		healer:         healer.NewHealer(nil, nil),
		nodeStates:     make(map[string]*NodeState),
		substoreClient: subClient,
	}

	orc := NewOrchestrator(sch)
	err := orc.SyncAllCustomGroups(context.Background())
	if err != nil {
		t.Fatalf("SyncAllCustomGroups failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if syncedName != "ashan-fr-empty" {
		t.Errorf("expected synced subscription 'ashan-fr-empty', got %q", syncedName)
	}
	if syncedContent != "" {
		t.Errorf("expected empty string content for empty custom group, got %q", syncedContent)
	}
}

func TestOrchestrator_LoadCustomChains_MalformedJSONReturnsError(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "malformed_chains.json")
	if err := os.WriteFile(tmpFile, []byte("{not valid json"), 0644); err != nil {
		t.Fatalf("failed to write tmp file: %v", err)
	}
	defer SetCustomChainsFileForTest(tmpFile)()

	_, err := LoadCustomChains()
	if err == nil {
		t.Fatal("expected error when loading malformed custom chains JSON, got nil")
	}

	orc := &Orchestrator{}
	err = orc.UnbindExplicitChain("target-1")
	if err == nil {
		t.Fatal("expected error from UnbindExplicitChain on malformed file, got nil")
	}
}

func TestOrchestrator_ChainChinaExitInheritance(t *testing.T) {
	tmpChains := filepath.Join(t.TempDir(), "custom_chains.json")
	defer SetCustomChainsFileForTest(tmpChains)()
	tmpGroups := filepath.Join(t.TempDir(), "custom_groups.json")
	defer SetCustomGroupsFileForTest(tmpGroups)()

	sch := &Scheduler{
		healer:     healer.NewHealer(nil, nil),
		nodeStates: make(map[string]*NodeState),
	}

	// 1. 干净线路机与干净家宽
	sch.nodeStates["clean-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "t1",
			Name:        "US-Clean-Transit",
			OriginalURL: "vless://u1@1.1.1.1:443#US-Clean-Transit",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
			LatencyMs:   100,
		},
	}
	sch.nodeStates["clean-resi"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "r1",
			Name:        "LZ-US-Clean-家宽",
			OriginalURL: "vless://u2@2.2.2.2:443#LZ-US-Clean-家宽",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
			LatencyMs:   200,
		},
	}

	// 2. 送中家宽与送中线路机
	sch.nodeStates["leaked-resi"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "r2",
			Name:        "LZ-US-Leaked-家宽",
			OriginalURL: "vless://u3@3.3.3.3:443#LZ-US-Leaked-家宽",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   true,
			LatencyMs:   300,
		},
	}
	sch.nodeStates["leaked-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "t2",
			Name:        "US-Leaked-Transit",
			OriginalURL: "vless://u4@4.4.4.4:443#US-Leaked-Transit",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true,
			LatencyMs:   150,
		},
	}

	// 注入 3 条自定义链式代理
	customChains := []CustomChainBind{
		{
			ID:              "bind-clean",
			TransitName:     "US-Clean-Transit",
			ResidentialName: "LZ-US-Clean-家宽",
			ChainName:       "链式|US-Clean-Transit ➔ LZ-US-Clean-家宽",
			ChainURL:        "vless://u2@2.2.2.2:443?dialerProxy=US-Clean-Transit#链式|US-Clean-Transit ➔ LZ-US-Clean-家宽",
		},
		{
			ID:              "bind-leaked-landing",
			TransitName:     "US-Clean-Transit",
			ResidentialName: "LZ-US-Leaked-家宽",
			ChainName:       "链式|US-Clean-Transit ➔ LZ-US-Leaked-家宽",
			ChainURL:        "vless://u3@3.3.3.3:443?dialerProxy=US-Clean-Transit#链式|US-Clean-Transit ➔ LZ-US-Leaked-家宽",
		},
		{
			ID:              "bind-leaked-relay",
			TransitName:     "US-Leaked-Transit",
			ResidentialName: "LZ-US-Clean-家宽",
			ChainName:       "链式|US-Leaked-Transit ➔ LZ-US-Clean-家宽",
			ChainURL:        "vless://u2@2.2.2.2:443?dialerProxy=US-Leaked-Transit#链式|US-Leaked-Transit ➔ LZ-US-Clean-家宽",
		},
	}
	if err := SaveCustomChains(customChains); err != nil {
		t.Fatalf("failed to save custom chains: %v", err)
	}

	orc := NewOrchestrator(sch)
	atoms := orc.CollectAtoms()

	var cleanChain, leakedLandingChain, leakedRelayChain *model.AtomNode
	for i := range atoms {
		switch atoms[i].Name {
		case "链式|US-Clean-Transit ➔ LZ-US-Clean-家宽":
			cleanChain = &atoms[i]
		case "链式|US-Clean-Transit ➔ LZ-US-Leaked-家宽":
			leakedLandingChain = &atoms[i]
		case "链式|US-Leaked-Transit ➔ LZ-US-Clean-家宽":
			leakedRelayChain = &atoms[i]
		}
	}

	if cleanChain == nil || cleanChain.ChinaExit {
		t.Fatalf("expected cleanChain to have ChinaExit=false, got: %v", cleanChain)
	}
	if leakedLandingChain == nil || !leakedLandingChain.ChinaExit {
		t.Fatalf("expected leakedLandingChain to inherit ChinaExit=true from residential landing, got: %v", leakedLandingChain)
	}
	if leakedRelayChain == nil || !leakedRelayChain.ChinaExit {
		t.Fatalf("expected leakedRelayChain to inherit ChinaExit=true from transit relay, got: %v", leakedRelayChain)
	}

	// 验证 QueryAtoms 过滤机制
	cleanChains := orc.QueryAtoms(AtomQueryFilter{CleanOnly: true, Kind: "chain"})
	if len(cleanChains) != 1 || cleanChains[0].Name != "链式|US-Clean-Transit ➔ LZ-US-Clean-家宽" {
		t.Fatalf("expected only 1 clean chain, got %d: %v", len(cleanChains), cleanChains)
	}

	leakedChains := orc.QueryAtoms(AtomQueryFilter{ChinaLeakOnly: true, Kind: "chain"})
	if len(leakedChains) != 2 {
		t.Fatalf("expected 2 leaked chains, got %d: %v", len(leakedChains), leakedChains)
	}

	// 验证 GetCategoriesSummary 大盘分类隔离
	summary := orc.GetCategoriesSummary()
	cats := summary["categories"].(map[string]any)
	chinaLeaks := cats["china_leaks"].([]model.AtomNode)

	foundLeakedLanding := false
	foundLeakedRelay := false
	for _, a := range chinaLeaks {
		if a.Name == "链式|US-Clean-Transit ➔ LZ-US-Leaked-家宽" {
			foundLeakedLanding = true
		}
		if a.Name == "链式|US-Leaked-Transit ➔ LZ-US-Clean-家宽" {
			foundLeakedRelay = true
		}
	}
	if !foundLeakedLanding || !foundLeakedRelay {
		t.Fatalf("expected both leaked chains in china_leaks isolation box, got: %v", chinaLeaks)
	}
}

func TestOrchestrator_ChainNameAndURLQuerySafety(t *testing.T) {
	tmpChains := filepath.Join(t.TempDir(), "custom_chains_query.json")
	defer SetCustomChainsFileForTest(tmpChains)()

	sch := &Scheduler{
		healer:     healer.NewHealer(nil, nil),
		nodeStates: make(map[string]*NodeState),
	}

	sch.nodeStates["jp-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "t-jp",
			Name:        "JP-SoftBank",
			OriginalURL: "vless://u@1.1.1.1:443#JP-SoftBank",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			LatencyMs:   120,
		},
	}
	sch.nodeStates["jp-resi"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "r-jp",
			Name:        "LZ-JP-KDDI-家宽",
			OriginalURL: "vless://u@2.2.2.2:443#LZ-JP-KDDI-家宽",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "residential",
			LatencyMs:   180,
		},
	}

	// URL query params 携带 '|' 与 '➔'，验证安全解析不被 query 参数破坏
	complexURL := "vless://uuid@167.104.167.151:20640?encryption=none&pbk=key|split&sni=apple.com&flow=xtls-rprx-vision&dialerProxy=JP-SoftBank#链式|JP-SoftBank ➔ LZ-JP-KDDI-家宽"
	customChains := []CustomChainBind{
		{
			ID:              "bind-complex",
			TransitName:     "JP-SoftBank",
			ResidentialName: "LZ-JP-KDDI-家宽",
			ChainName:       "链式|JP-SoftBank ➔ LZ-JP-KDDI-家宽",
			ChainURL:        complexURL,
		},
	}
	if err := SaveCustomChains(customChains); err != nil {
		t.Fatalf("failed to save custom chains: %v", err)
	}

	orc := NewOrchestrator(sch)
	atoms := orc.CollectAtoms()

	var matched *model.AtomNode
	for i := range atoms {
		if atoms[i].Kind == "chain" {
			matched = &atoms[i]
			break
		}
	}

	if matched == nil {
		t.Fatal("expected chain atom to be created")
	}
	if matched.Name != "链式|JP-SoftBank ➔ LZ-JP-KDDI-家宽" {
		t.Fatalf("expected exact name, got: %s", matched.Name)
	}
	if matched.Country != "JP" {
		t.Fatalf("expected country JP, got: %s", matched.Country)
	}
	if matched.LatencyMs != 300 {
		t.Fatalf("expected latency 300 (120+180), got: %d", matched.LatencyMs)
	}
}

func TestOrchestrator_ChainWithoutTransitDoesNotMatchFirstTransit(t *testing.T) {
	tmpChains := filepath.Join(t.TempDir(), "custom_chains_no_transit.json")
	defer SetCustomChainsFileForTest(tmpChains)()

	sch := &Scheduler{
		healer:     healer.NewHealer(nil, nil),
		nodeStates: make(map[string]*NodeState),
	}

	sch.nodeStates["leaked-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "t-leak",
			Name:        "US-Leaking-Transit",
			OriginalURL: "vless://u@1.1.1.1:443#US-Leaking-Transit",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true,
			LatencyMs:   999,
		},
	}
	sch.nodeStates["clean-resi"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "r-clean",
			Name:        "LZ-US-Clean-家宽",
			OriginalURL: "vless://u@2.2.2.2:443#LZ-US-Clean-家宽",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
			LatencyMs:   50,
		},
	}

	// Custom chain with no transit part (no ➔)
	customChains := []CustomChainBind{
		{
			ID:              "bind-no-transit",
			TransitName:     "",
			ResidentialName: "LZ-US-Clean-家宽",
			ChainName:       "LZ-US-Clean-家宽",
			ChainURL:        "vless://u@2.2.2.2:443#LZ-US-Clean-家宽",
		},
	}
	if err := SaveCustomChains(customChains); err != nil {
		t.Fatalf("failed to save custom chains: %v", err)
	}

	orc := NewOrchestrator(sch)
	atoms := orc.CollectAtoms()

	var matched *model.AtomNode
	for i := range atoms {
		if atoms[i].Kind == "chain" {
			matched = &atoms[i]
			break
		}
	}

	if matched == nil {
		t.Fatal("expected chain atom to be created")
	}
	t.Logf("Chain LatencyMs: %d, ChinaExit: %v", matched.LatencyMs, matched.ChinaExit)
	if matched.ChinaExit {
		t.Errorf("BUG DETECTED: chain with empty transit part inherited ChinaExit from unrelated transit!")
	}
	if matched.LatencyMs != 50 {
		t.Errorf("BUG DETECTED: chain with empty transit part inherited transit latency (%d != 50)", matched.LatencyMs)
	}
}

func TestSaveCustomGroup_DuplicateDisplayNameLaterInSlice(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := tmpDir + "/custom_groups.json"
	revert := SetCustomGroupsFileForTest(customPath)
	defer revert()

	// 保存两个分组 g1 (Alpha) 与 g2 (Beta)
	g1 := model.CustomGroupDef{
		Name:        "g1",
		DisplayName: "Alpha",
		Enabled:     true,
		Order:       10,
	}
	g2 := model.CustomGroupDef{
		Name:        "g2",
		DisplayName: "Beta",
		Enabled:     true,
		Order:       20,
	}
	if err := SaveCustomGroup(g1); err != nil {
		t.Fatalf("save g1: %v", err)
	}
	if err := SaveCustomGroup(g2); err != nil {
		t.Fatalf("save g2: %v", err)
	}

	// 试图将位于索引 0 的 g1 的 DisplayName 改为排在其后的 g2 的 "Beta"
	g1Updated := model.CustomGroupDef{
		Name:        "g1",
		DisplayName: "Beta", // 冲突！
		Enabled:     true,
		Order:       10,
	}
	err := SaveCustomGroup(g1Updated)
	if err == nil {
		t.Fatalf("expected duplicate display_name error when updating g1 to match g2's display_name, but got nil")
	}
	if !strings.Contains(err.Error(), "duplicate display_name") {
		t.Fatalf("expected error containing 'duplicate display_name', got: %v", err)
	}
}

func TestUnbindExplicitChain_EmptyTargetSafety(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := tmpDir + "/custom_chains.json"
	revert := SetCustomChainsFileForTest(customPath)
	defer revert()

	chains := []CustomChainBind{
		{
			ID:              "bind-1",
			TransitName:     "TransitA",
			ResidentialName: "ResiA",
			ChainName:       "链式|TransitA ➔ ResiA",
		},
	}
	if err := SaveCustomChains(chains); err != nil {
		t.Fatalf("save custom chains: %v", err)
	}

	orc := NewOrchestrator(nil)

	// 验证空 target / 纯空格不会误删除所有链
	if err := orc.UnbindExplicitChain(""); err == nil {
		t.Fatalf("expected error for empty target, got nil")
	}
	if err := orc.UnbindExplicitChain("   "); err == nil {
		t.Fatalf("expected error for whitespace target, got nil")
	}

	// 验证链依然完好保存
	saved, err := LoadCustomChains()
	if err != nil {
		t.Fatalf("load custom chains: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 chain retained, got %d", len(saved))
	}
}

func TestBindExplicitChain_ExactMatchPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := tmpDir + "/custom_chains.json"
	revert := SetCustomChainsFileForTest(customPath)
	defer revert()

	sch := &Scheduler{
		nodeStates: make(map[string]*NodeState),
	}
	// 注入包含前缀模糊子串的两个节点：一个全字匹配，一个较长包含匹配
	sch.nodeStates["jp-tokyo-partial"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "jp-tokyo-extended",
			Name:        "JP-Tokyo-IIJ-Extended",
			OriginalURL: "vless://u1@1.1.1.1:443#JP-Tokyo-IIJ-Extended",
			NodeType:    "transit",
			Online:      true,
		},
	}
	sch.nodeStates["jp-tokyo-exact"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "jp-tokyo",
			Name:        "JP-Tokyo",
			OriginalURL: "vless://u2@2.2.2.2:443#JP-Tokyo",
			NodeType:    "transit",
			Online:      true,
		},
	}
	sch.nodeStates["resi-exact"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "resi-1",
			Name:        "LZ-US-Pavlov-家宽",
			OriginalURL: "vless://u3@3.3.3.3:443#LZ-US-Pavlov-家宽",
			NodeType:    "residential",
			Online:      true,
		},
	}

	orc := NewOrchestrator(sch)

	// 验证入参非空
	if _, err := orc.BindExplicitChain("", "LZ-US-Pavlov-家宽"); err == nil {
		t.Fatalf("expected error for empty transitName")
	}

	// 验证精确匹配优先
	bind, err := orc.BindExplicitChain("JP-Tokyo", "LZ-US-Pavlov-家宽")
	if err != nil {
		t.Fatalf("bind explicit chain: %v", err)
	}
	if bind.TransitName != "JP-Tokyo" {
		t.Fatalf("expected exact match 'JP-Tokyo', got '%s'", bind.TransitName)
	}
}

func TestOrchestrator_CollectAtoms_LandingEmojiFallbackInheritance(t *testing.T) {
	tmpChains := filepath.Join(t.TempDir(), "custom_chains.json")
	defer SetCustomChainsFileForTest(tmpChains)()
	tmpGroups := filepath.Join(t.TempDir(), "custom_groups.json")
	defer SetCustomGroupsFileForTest(tmpGroups)()

	sch := &Scheduler{
		healer:     healer.NewHealer(nil, nil),
		nodeStates: make(map[string]*NodeState),
	}
	sch.nodeStates["transit-1"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "transit-1",
			Name:        "🇯🇵 JP-Tokyo-Softbank", // 带 emoji
			OriginalURL: "vless://u1@1.1.1.1:443#JP-Tokyo-Softbank",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
	}
	sch.nodeStates["resi-1"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "resi-1",
			Name:        "🇺🇸 LZ-US-Pavlov-家宽", // 带 emoji
			OriginalURL: "socks5://u:***@167.104.167.151:1001#LZ-US-Pavlov-家宽",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   true, // 落地送中！
		},
	}

	customChains := []CustomChainBind{
		{
			ID:              "bind-emoji-fallback",
			TransitName:     "JP-Tokyo-Softbank", // 无 emoji
			ResidentialName: "LZ-US-Pavlov-家宽",  // 无 emoji
			ChainName:       "链式|JP-Tokyo-Softbank ➔ LZ-US-Pavlov-家宽",
			ChainURL:        "vless://u1@1.1.1.1:443?dialerProxy=JP-Tokyo-Softbank#链式|JP-Tokyo-Softbank ➔ LZ-US-Pavlov-家宽",
		},
	}
	if err := SaveCustomChains(customChains); err != nil {
		t.Fatalf("failed to save custom chains: %v", err)
	}

	orc := NewOrchestrator(sch)
	atoms := orc.CollectAtoms()

	// 查找 chain 节点
	var chainAtom *model.AtomNode
	for i := range atoms {
		if atoms[i].Kind == "chain" && atoms[i].Name == "链式|JP-Tokyo-Softbank ➔ LZ-US-Pavlov-家宽" {
			chainAtom = &atoms[i]
			break
		}
	}

	if chainAtom == nil {
		t.Fatalf("expected chain atom to be generated, got none in %d atoms", len(atoms))
	}

	// 验证链式节点通过 fallback 模糊比对成功继承了送中状态
	if !chainAtom.ChinaExit {
		t.Fatalf("expected chain atom to inherit ChinaExit=true from residential landing with emoji prefix, got false")
	}
}

func TestUnbindExplicitChain_SubstringCollisionSafety(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := tmpDir + "/custom_chains.json"
	revert := SetCustomChainsFileForTest(customPath)
	defer revert()

	chains := []CustomChainBind{
		{
			ID:              "bind-us-1",
			TransitName:     "US-LAX-4837",
			ResidentialName: "LZ-US-Pavlov-家宽",
			ChainName:       "链式|US-LAX-4837 ➔ LZ-US-Pavlov-家宽",
		},
		{
			ID:              "bind-jp-1",
			TransitName:     "JP-Tokyo-IIJ",
			ResidentialName: "LZ-JP-KDDI-家宽",
			ChainName:       "链式|JP-Tokyo-IIJ ➔ LZ-JP-KDDI-家宽",
		},
	}
	if err := SaveCustomChains(chains); err != nil {
		t.Fatalf("save custom chains: %v", err)
	}

	orc := NewOrchestrator(nil)

	// 通用子串不能误杀任何绑定
	riskySubstrings := []string{"US", "家宽", "Reality", "➔", "链式", "4837", "LAX", "Tokyo"}
	for _, sub := range riskySubstrings {
		if err := orc.UnbindExplicitChain(sub); err == nil {
			t.Fatalf("expected error when unbinding with general substring %q, got nil (potential collision wipeout)", sub)
		}
	}

	saved, err := LoadCustomChains()
	if err != nil || len(saved) != 2 {
		t.Fatalf("expected both chains retained after failed substring unbinds, got %d (err: %v)", len(saved), err)
	}

	// 精确组合解绑：只解绑 bind-us-1
	target := "US-LAX-4837 ➔ LZ-US-Pavlov-家宽"
	if err := orc.UnbindExplicitChain(target); err != nil {
		t.Fatalf("expected exact unbind to succeed, got %v", err)
	}

	saved, err = LoadCustomChains()
	if err != nil || len(saved) != 1 || saved[0].ID != "bind-jp-1" {
		t.Fatalf("expected only bind-jp-1 to remain, got %v (err: %v)", saved, err)
	}

	// 精确 ID 解绑剩余项
	if err := orc.UnbindExplicitChain("bind-jp-1"); err != nil {
		t.Fatalf("expected unbind by ID to succeed, got %v", err)
	}

	saved, err = LoadCustomChains()
	if err != nil || len(saved) != 0 {
		t.Fatalf("expected 0 chains remaining, got %d", len(saved))
	}
}

func TestGetCategoriesSummary_CustomGroupsConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	customGroupsPath := tmpDir + "/custom_groups.json"
	revert := SetCustomGroupsFileForTest(customGroupsPath)
	defer revert()

	defs := []model.CustomGroupDef{
		{
			Name:        "group-us",
			DisplayName: "测试-美国组",
			Enabled:     true,
			Type:        "url-test",
			Filter: model.GroupFilter{
				Mode:      "filter",
				Countries: []string{"US"},
				Kinds:     []string{"transit"},
			},
		},
	}
	if err := SaveCustomGroupsBatch(defs); err != nil {
		t.Fatalf("save custom groups: %v", err)
	}

	sch := &Scheduler{
		nodeStates: make(map[string]*NodeState),
	}
	sch.nodeStates["n1"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "1",
			Name:        "US-Node-1",
			OriginalURL: "vless://u1@38.175.222.44:36564#US-Node-1",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			TestedAt:    time.Now(),
		},
	}

	orc := NewOrchestrator(sch)
	summary := orc.GetCategoriesSummary()
	if summary["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", summary["status"])
	}

	customGroupsSummary, ok := summary["custom_groups"].([]map[string]any)
	if !ok || len(customGroupsSummary) != 1 {
		t.Fatalf("expected 1 custom group in summary, got %v", summary["custom_groups"])
	}
	if customGroupsSummary[0]["count"] != 1 {
		t.Fatalf("expected custom group count to be 1, got %v", customGroupsSummary[0]["count"])
	}
}

func TestOrchestrator_CollectAtoms_RetainsOfflineTransitAsOfflineAtom(t *testing.T) {
	tmpChains := filepath.Join(t.TempDir(), "custom_chains_offline_transit.json")
	defer SetCustomChainsFileForTest(tmpChains)()

	sch := &Scheduler{
		healer:     healer.NewHealer(nil, nil),
		nodeStates: make(map[string]*NodeState),
	}

	// 1. 在线直连线路机
	sch.nodeStates["online-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "transit-online",
			Name:        "JP-Tokyo-Online",
			OriginalURL: "vless://u1@1.1.1.1:443#JP-Tokyo-Online",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			LatencyMs:   50,
			TestedAt:    time.Now(),
		},
	}
	// 2. 离线直连线路机 (主力专线掉线)
	sch.nodeStates["offline-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "transit-offline",
			Name:        "US-LAX-Offline",
			OriginalURL: "vless://u2@2.2.2.2:443#US-LAX-Offline",
			ExitCountry: "US",
			Online:      false,
			NodeType:    "transit",
			LatencyMs:   0,
			TestedAt:    time.Now(),
		},
	}

	orc := NewOrchestrator(sch)
	atoms := orc.CollectAtoms()

	var foundOnline, foundOffline *model.AtomNode
	for i := range atoms {
		if atoms[i].ID == "transit-online" {
			foundOnline = &atoms[i]
		}
		if atoms[i].ID == "transit-offline" {
			foundOffline = &atoms[i]
		}
	}

	if foundOnline == nil || !foundOnline.Online {
		t.Fatalf("expected online transit atom to be present with Online=true, got %+v", foundOnline)
	}
	if foundOffline == nil {
		t.Fatalf("expected offline transit atom to be retained in atom inventory")
	}
	if foundOffline.Online {
		t.Errorf("expected offline transit atom to have Online=false, got %+v", foundOffline)
	}
}

func TestOrchestrator_ChainLiveness_OfflineOrMissingRelay(t *testing.T) {
	tmpChains := filepath.Join(t.TempDir(), "custom_chains_liveness.json")
	defer SetCustomChainsFileForTest(tmpChains)()

	sch := &Scheduler{
		healer:     healer.NewHealer(nil, nil),
		nodeStates: make(map[string]*NodeState),
	}

	sch.nodeStates["online-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "t-online",
			Name:        "JP-Tokyo-Online",
			OriginalURL: "vless://u1@1.1.1.1:443#JP-Tokyo-Online",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			LatencyMs:   50,
		},
	}
	sch.nodeStates["offline-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "t-offline",
			Name:        "US-LAX-Offline",
			OriginalURL: "vless://u2@2.2.2.2:443#US-LAX-Offline",
			ExitCountry: "US",
			Online:      false,
			NodeType:    "transit",
			LatencyMs:   0,
		},
	}
	sch.nodeStates["online-resi"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "r-online",
			Name:        "LZ-US-Pavlov-家宽",
			OriginalURL: "socks5://u3:p@3.3.3.3:10001#LZ-US-Pavlov-家宽",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			LatencyMs:   120,
		},
	}

	customChains := []CustomChainBind{
		{
			ID:              "chain-with-offline-transit",
			TransitName:     "US-LAX-Offline",
			ResidentialName: "LZ-US-Pavlov-家宽",
			ChainName:       "链式|US-LAX-Offline ➔ LZ-US-Pavlov-家宽",
			ChainURL:        "socks5://u3:p@3.3.3.3:10001?dialerProxy=US-LAX-Offline#链式|US-LAX-Offline ➔ LZ-US-Pavlov-家宽",
		},
		{
			ID:              "chain-with-unknown-transit",
			TransitName:     "Unknown-Transit-X",
			ResidentialName: "LZ-US-Pavlov-家宽",
			ChainName:       "链式|Unknown-Transit-X ➔ LZ-US-Pavlov-家宽",
			ChainURL:        "socks5://u3:p@3.3.3.3:10001?dialerProxy=Unknown-Transit-X#链式|Unknown-Transit-X ➔ LZ-US-Pavlov-家宽",
		},
		{
			ID:              "chain-with-unknown-target",
			TransitName:     "JP-Tokyo-Online",
			ResidentialName: "Non-Existent-Resi",
			ChainName:       "链式|JP-Tokyo-Online ➔ Non-Existent-Resi",
			ChainURL:        "socks5://u4:p@4.4.4.4:10001?dialerProxy=JP-Tokyo-Online#链式|JP-Tokyo-Online ➔ Non-Existent-Resi",
		},
	}
	if err := SaveCustomChains(customChains); err != nil {
		t.Fatalf("failed to save custom chains: %v", err)
	}

	orc := NewOrchestrator(sch)
	atoms := orc.CollectAtoms()

	atomMap := make(map[string]model.AtomNode)
	for _, a := range atoms {
		atomMap[a.Name] = a
	}

	// 1. 中转专线掉线的链式代理必须判定为离线
	if c1, ok := atomMap["链式|US-LAX-Offline ➔ LZ-US-Pavlov-家宽"]; !ok || c1.Online {
		t.Errorf("expected chain with offline transit relay to be Online=false, got: %+v", c1)
	}

	// 2. 中转专线完全不存在的链式代理必须判定为离线
	if c2, ok := atomMap["链式|Unknown-Transit-X ➔ LZ-US-Pavlov-家宽"]; !ok || c2.Online {
		t.Errorf("expected chain with unknown transit relay to be Online=false, got: %+v", c2)
	}

	// 3. 落地家宽完全不存在的链式代理必须判定为离线
	if c3, ok := atomMap["链式|JP-Tokyo-Online ➔ Non-Existent-Resi"]; !ok || c3.Online {
		t.Errorf("expected chain with non-existent target to be Online=false, got: %+v", c3)
	}
}

func TestOrchestrator_ChainCountry_TransitFallbackWhenTargetHasNoCountry(t *testing.T) {
	tmpChains := filepath.Join(t.TempDir(), "custom_chains_fallback.json")
	defer SetCustomChainsFileForTest(tmpChains)()

	sch := &Scheduler{
		healer:     healer.NewHealer(nil, nil),
		nodeStates: make(map[string]*NodeState),
	}
	sch.nodeStates["jp-transit"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "jp-t",
			Name:        "东京软银-专线",
			OriginalURL: "vless://u1@1.1.1.1:443#东京软银-专线",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
		},
	}
	sch.nodeStates["resi-no-country"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "resi-1",
			Name:        "原生家宽-01",
			OriginalURL: "socks5://u2:p@2.2.2.2:10001#原生家宽-01",
			ExitCountry: "",
			Online:      true,
			NodeType:    "residential",
		},
	}

	customChains := []CustomChainBind{
		{
			ID:              "fallback-chain",
			TransitName:     "东京软银-专线",
			ResidentialName: "原生家宽-01",
			ChainName:       "链式|东京软银-专线 ➔ 原生家宽-01",
			ChainURL:        "socks5://u2:p@2.2.2.2:10001?dialerProxy=东京软银-专线#链式|东京软银-专线 ➔ 原生家宽-01",
		},
	}
	if err := SaveCustomChains(customChains); err != nil {
		t.Fatalf("failed to save custom chains: %v", err)
	}

	orc := NewOrchestrator(sch)
	atoms := orc.CollectAtoms()

	var fallbackAtom *model.AtomNode
	for i := range atoms {
		if atoms[i].Name == "链式|东京软银-专线 ➔ 原生家宽-01" {
			fallbackAtom = &atoms[i]
			break
		}
	}

	if fallbackAtom == nil {
		t.Fatalf("expected chain atom to be present")
	}
	if fallbackAtom.Country != "JP" {
		t.Errorf("expected chain country to fallback to JP from transit, got %q", fallbackAtom.Country)
	}
}

