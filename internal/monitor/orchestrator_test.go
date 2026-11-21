package monitor

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vpn-probe/internal/healer"
	"vpn-probe/internal/model"
)

func TestOrchestratorEvaluationAndQuery(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "custom_groups.json")
	restore := SetCustomGroupsFileForTest(tmpFile)
	defer restore()

	h := healer.NewHealer(nil, nil)
	sch := &Scheduler{
		healer:     h,
		nodeStates: make(map[string]*NodeState),
	}

	// 注入节点状态：
	// 1. 美西4837 (US, Transit, Clean)
	sch.nodeStates["美西4837"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "1",
			Name:        "美西 ACCK 洛杉矶 4837 — VLESS Reality",
			OriginalURL: "vless://u1@38.175.222.44:36564#US-LAX-ACCK-4837",
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
			OriginalURL: "socks5://u:p@1.2.3.4:1001#LZ-US-家宽01",
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
	if len(usTransits) != 1 || !strings.Contains(usTransits[0].Name, "4837") {
		t.Fatalf("expected 1 clean US transit, got %v", usTransits)
	}

	// 2. 测试原子查询：查询美国家宽直连
	usResis := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "residential", CleanOnly: true})
	if len(usResis) != 1 || !strings.Contains(usResis[0].Name, "LZ-US") {
		t.Fatalf("expected 1 US residential, got %v", usResis)
	}

	// 3. 测试原子查询：查询线路拉家宽链式中继 (方案B网状挂载)
	usChains := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "chain"})
	if len(usChains) != 1 || !strings.Contains(usChains[0].Name, "链式") {
		t.Fatalf("expected 1 US chain, got %v", usChains)
	}

	// 4. 测试动态公式编排求值：连接 = 美国家宽 + 美国线路机 + 线路家宽机
	bundleFilter := model.GroupFilter{
		Countries:          []string{"US"},
		IncludeTransit:     true, // 美国线路机
		IncludeResidential: true, // 美国家宽
		IncludeChain:       true, // 线路家宽机
		ExcludeChinaLeak:   true, // 排除送中
	}

	bundleURLs := orc.EvaluateGroup(bundleFilter)
	// 应当恰好返回 3 条连接 (1 线路 + 1 家宽直连 + 1 链式中继)
	if len(bundleURLs) != 3 {
		t.Fatalf("expected 3 nodes for US bundle formula, got %d: %v", len(bundleURLs), bundleURLs)
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
}
