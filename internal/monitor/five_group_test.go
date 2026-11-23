package monitor

import (
	"strings"
	"testing"

	"vpn-probe/internal/model"
)

func TestClassifyIntoFiveGroups(t *testing.T) {
	// 1. 模拟各类线路机
	transits := []*model.NodeProbeResult{
		{
			Name:        "美西 ACCK 洛杉矶 4837 — VLESS Reality",
			OriginalURL: "vless://u1@38.175.222.44:36564#US-LAX-ACCK-4837-Reality",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
		{
			Name:        "美西 LAX — VLESS WS（CF/OpenResty 443 救活）",
			OriginalURL: "vless://u2@lax-free.1992004.xyz:443#US-LAX-CF-WS",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false, // 待命救活节点，直连存活时不应进入活跃线路
		},
		{
			Name:        "东京软银 — VLESS Reality Vision",
			OriginalURL: "vless://u3@45.192.242.72:24444#JP-Tokyo-Softbank-Reality-Vision",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
		{
			Name:        "东京 IIJ — VLESS Reality",
			OriginalURL: "vless://u4@156.231.113.91:33339#JP-Tokyo-IIJ-Reality",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
		{
			Name:        "新加坡自建 — VLESS Reality",
			OriginalURL: "vless://u5@1.2.3.4:443#SG-Direct",
			ExitCountry: "SG",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
		{
			Name:        "某送中测试自建机",
			OriginalURL: "vless://u6@5.6.7.8:443#Leaked-CN",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true, // 送中节点！必须隔离！
		},
	}

	// 2. 模拟家宽机
	resis := []*model.NodeProbeResult{
		{
			Name:        "LZ-US-Pavlov-家宽01",
			OriginalURL: "socks5://u:p@1.2.3.4:1001#LZ-US-Pavlov-家宽01",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
		},
		{
			Name:        "LZ-JP-SoftBank-家宽01",
			OriginalURL: "socks5://u:p@1.2.3.4:2001#LZ-JP-SoftBank-家宽01",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
		},
	}

	// 3. 模拟链式中继代理 (美区 + 日区)
	chains := []string{
		"socks5://u:p@1.2.3.4:1001?dialerProxy=美西+ACCK+洛杉矶+4837#链式|美西4837 ➔ LZ-US-Pavlov-家宽01",
		"socks5://u:p@1.2.3.4:2001?dialerProxy=东京软银#链式|东京软银 ➔ LZ-JP-SoftBank-家宽01",
	}

	payload := ClassifyIntoFiveGroups(transits, resis, chains)

	// 1. 验证送中隔离
	if len(payload.ChinaLeakNodes) != 1 || !strings.Contains(payload.ChinaLeakNodes[0], "Leaked-CN") {
		t.Fatalf("expected 1 leak node in ChinaLeakNodes, got: %v", payload.ChinaLeakNodes)
	}

	// 2. 送中节点绝不应出现在任何干净组
	for _, n := range append(payload.TransitNodes, append(payload.USResiNodes, payload.JPResiNodes...)...) {
		if strings.Contains(n, "Leaked-CN") {
			t.Fatalf("china leak node leaked into clean groups: %s", n)
		}
	}

	// 3. 验证线路机 (美西4837, 东京软银, 东京IIJ, 新加坡自建) 共 4 个，CF-WS 待命被隐藏
	if len(payload.TransitNodes) != 4 {
		t.Errorf("expected 4 transit nodes, got %d: %v", len(payload.TransitNodes), payload.TransitNodes)
	}
	for _, n := range payload.TransitNodes {
		if strings.Contains(n, "CF-WS") {
			t.Errorf("standby CF-WS node should be hidden from transit nodes: %s", n)
		}
	}

	// 4. 验证美区全家桶 (美西4837线路 + LZ-US家宽直连 + 链式美西拉家宽)
	if len(payload.USResiNodes) != 3 {
		t.Errorf("expected 3 US nodes (transit + resi + chain), got %d: %v", len(payload.USResiNodes), payload.USResiNodes)
	}

	// 5. 验证日区全家桶 (东京软银 + 东京IIJ + LZ-JP家宽直连 + 链式软银拉家宽)
	if len(payload.JPResiNodes) != 4 {
		t.Errorf("expected 4 JP nodes, got %d: %v", len(payload.JPResiNodes), payload.JPResiNodes)
	}

	// 6. 验证其他组 (新加坡自建)
	if len(payload.OtherNodes) != 1 || !strings.Contains(payload.OtherNodes[0], "SG-Direct") {
		t.Errorf("expected Singapore node in OtherNodes, got: %v", payload.OtherNodes)
	}
}

func TestMergeCustomChains(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := tmpDir + "/custom_chains.json"
	revert := SetCustomChainsFileForTest(customPath)
	defer revert()

	base := []string{
		"socks5://u:p@1.1.1.1:1001#链式|4837 ➔ LZ-US-Frontier",
		"socks5://u:p@1.1.1.1:1002#链式|4837 ➔ LZ-JP-SoftBank",
	}

	// 1. 无自定义链式时原样返回
	merged := mergeCustomChains(base)
	if len(merged) != 2 {
		t.Fatalf("expected 2 chains when custom empty, got %d", len(merged))
	}

	// 2. 写入自定义链式 (一条重复，一条全新)
	customChains := []CustomChainBind{
		{
			ID:          "c1",
			ChainURL:    "socks5://u:p@1.1.1.1:1001#链式|4837 ➔ LZ-US-Frontier", // 重复
			TransitName: "4837",
		},
		{
			ID:          "c2",
			ChainURL:    "socks5://u:p@1.1.1.1:1003#链式|IIJ ➔ LZ-SG-StarHub", // 全新
			TransitName: "IIJ",
		},
	}
	if err := SaveCustomChains(customChains); err != nil {
		t.Fatalf("save custom chains: %v", err)
	}

	merged = mergeCustomChains(base)
	if len(merged) != 3 {
		t.Fatalf("expected 3 chains after merging without duplicate, got %d: %v", len(merged), merged)
	}

	foundNew := false
	for _, ch := range merged {
		if strings.Contains(ch, "LZ-SG-StarHub") {
			foundNew = true
			break
		}
	}
	if !foundNew {
		t.Fatalf("expected merged custom chain 'LZ-SG-StarHub' to be present")
	}
}

func TestClassifyIntoFiveGroups_ChainChinaExitQuarantine(t *testing.T) {
	// 验证：当链式节点的中继或落地家宽端任一端送中，该链必须被送中隔离箱拦截，严禁漏入干净分组
	transits := []*model.NodeProbeResult{
		{
			Name:        "US-Clean-Transit",
			OriginalURL: "vless://u1@1.1.1.1:443#US-Clean-Transit",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
		{
			Name:        "US-Leaked-Transit",
			OriginalURL: "vless://u2@2.2.2.2:443#US-Leaked-Transit",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true, // 送中中继
		},
	}
	resis := []*model.NodeProbeResult{
		{
			Name:        "LZ-US-Clean-家宽",
			OriginalURL: "socks5://u:***@167.104.167.151:1001#LZ-US-Clean-家宽",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
		},
		{
			Name:        "LZ-US-Leaked-家宽",
			OriginalURL: "socks5://u:***@167.104.167.151:1002#LZ-US-Leaked-家宽",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   true, // 送中落地家宽
		},
	}

	chains := []string{
		// 1. 全干净链式
		"socks5://u:***@167.104.167.151:1001?dialerProxy=US-Clean-Transit#链式|US-Clean-Transit ➔ LZ-US-Clean-家宽",
		// 2. 落地送中链式
		"socks5://u:***@167.104.167.151:1002?dialerProxy=US-Clean-Transit#链式|US-Clean-Transit ➔ LZ-US-Leaked-家宽",
		// 3. 中继送中链式
		"socks5://u:***@167.104.167.151:1001?dialerProxy=US-Leaked-Transit#链式|US-Leaked-Transit ➔ LZ-US-Clean-家宽",
	}

	payload := ClassifyIntoFiveGroups(transits, resis, chains)

	// 验证 ChinaLeakNodes 应收拢：1 个送中线路机 + 1 个送中家宽 + 2 个送中链式 = 共 4 个送中项
	if len(payload.ChinaLeakNodes) != 4 {
		t.Fatalf("expected 4 nodes in ChinaLeakNodes, got %d: %v", len(payload.ChinaLeakNodes), payload.ChinaLeakNodes)
	}

	// 验证干净美区组只能包含干净节点，绝不能包含送中链
	for _, n := range payload.USResiNodes {
		if strings.Contains(n, "Leaked") {
			t.Fatalf("leaking chain contaminated USResiNodes: %s", n)
		}
	}
	// 验证唯一存活的干净链式进入了 USResiNodes
	foundCleanChain := false
	for _, n := range payload.USResiNodes {
		if strings.Contains(n, "US-Clean-Transit ➔ LZ-US-Clean-家宽") {
			foundCleanChain = true
			break
		}
	}
	if !foundCleanChain {
		t.Fatalf("expected clean chain to be present in USResiNodes")
	}
}

func TestClassifyIntoFiveGroups_ChainURLParameterSafety(t *testing.T) {
	transits := []*model.NodeProbeResult{
		{
			Name:        "JP-SoftBank",
			OriginalURL: "vless://u1@45.192.242.72:24444#JP-SoftBank",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
	}
	resis := []*model.NodeProbeResult{
		{
			Name:        "LZ-JP-KDDI-家宽",
			OriginalURL: "socks5://u:***@167.104.167.151:20640#LZ-JP-KDDI-家宽",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
		},
	}

	// URL query 参数中包含特殊字符 | 与复杂的 Reality 参数
	chains := []string{
		"vless://uuid@167.104.167.151:20640?encryption=none&pbk=key|split&sni=apple.com&dialerProxy=JP-SoftBank#链式|JP-SoftBank ➔ LZ-JP-KDDI-家宽",
	}

	payload := ClassifyIntoFiveGroups(transits, resis, chains)

	if len(payload.JPResiNodes) != 3 { // 1 transit + 1 resi + 1 chain
		t.Fatalf("expected 3 nodes in JPResiNodes, got %d: %v", len(payload.JPResiNodes), payload.JPResiNodes)
	}
	foundChain := false
	for _, n := range payload.JPResiNodes {
		if strings.Contains(n, "JP-SoftBank ➔ LZ-JP-KDDI-家宽") {
			foundChain = true
			break
		}
	}
	if !foundChain {
		t.Fatalf("expected complex URL chain to be safely resolved into JPResiNodes")
	}
}

func TestClassifyIntoFiveGroups_NilAndEmptyNameEdgeCases(t *testing.T) {
	// 验证：包含 nil 节点、包含空 Name 送中节点、包含短名称送中节点时，不会引发 panic 或误判隔离
	transits := []*model.NodeProbeResult{
		nil, // nil 节点防护
		{
			ID:          "1", // 短 ID 不得污染名字包含 "1" 的干净节点
			Name:        "",  // 空 Name 送中节点防护
			OriginalURL: "vless://u1@1.1.1.1:443#EmptyName",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true,
		},
		{
			ID:          "leak-transit",
			Name:        "US-Leaked-Transit-Extra",
			OriginalURL: "vless://u2@2.2.2.2:443#US-Leaked-Transit-Extra",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true,
		},
		{
			ID:          "clean-transit",
			Name:        "Transit", // 名字为 "Transit"，恰为送中节点的子串，但不得被误杀
			OriginalURL: "vless://u3@3.3.3.3:443#Transit",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
		},
	}
	resis := []*model.NodeProbeResult{
		nil, // nil 节点防护
		{
			ID:          "resi-01",
			Name:        "LZ-US-Resi-01", // 含有 "1"，不得被 ID="1" 误杀
			OriginalURL: "socks5://u:***@167.104.167.151:1001#LZ-US-Resi-01",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
		},
	}

	chains := []string{
		"socks5://u:***@167.104.167.151:1001?dialerProxy=Transit#链式|Transit ➔ LZ-US-Resi-01",
	}

	payload := ClassifyIntoFiveGroups(transits, resis, chains)

	// 验证干净链进入 USResiNodes，没有发生误杀
	foundCleanChain := false
	for _, n := range payload.USResiNodes {
		if strings.Contains(n, "Transit ➔ LZ-US-Resi-01") {
			foundCleanChain = true
			break
		}
	}
	if !foundCleanChain {
		t.Fatalf("clean chain was falsely quarantined due to substring collision or empty name! USResiNodes: %v, ChinaLeakNodes: %v", payload.USResiNodes, payload.ChinaLeakNodes)
	}
}
