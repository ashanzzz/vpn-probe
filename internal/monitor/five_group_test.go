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
