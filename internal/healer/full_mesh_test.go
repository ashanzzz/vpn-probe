package healer

import (
	"net/url"
	"strings"
	"testing"

	"vpn-probe/internal/model"
)

func TestBuildFullMeshRegionalChains(t *testing.T) {
	h := NewHealer(nil, nil)

	// 1 线路机 (美西 4837) + 2 线路机 (东京软银, 东京 IIJ)
	transits := []*model.NodeProbeResult{
		{
			Name:        "美西 ACCK 洛杉矶 4837 — VLESS Reality",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
		},
		{
			Name:        "美西 LAX — VLESS WS（CF/OpenResty 443 救活）",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit", // 待命救活节点，应被自动忽略
		},
		{
			Name:        "东京软银 — VLESS Reality Vision",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
		},
		{
			Name:        "东京 IIJ — VLESS Reality",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
		},
	}

	// 3 美国家宽 + 2 日本家宽
	resis := []*model.NodeProbeResult{
		{
			Name:        "LZ-US-Pavlov-家宽01",
			OriginalURL: "socks5://u1:p1@1.2.3.4:1001#LZ-US-Pavlov-家宽01",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
		},
		{
			Name:        "LZ-US-Pavlov-家宽02",
			OriginalURL: "socks5://u2:p2@1.2.3.4:1002#LZ-US-Pavlov-家宽02",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
		},
		{
			Name:        "LZ-US-Pavlov-家宽03",
			OriginalURL: "socks5://u3:p3@1.2.3.4:1003#LZ-US-Pavlov-家宽03",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
		},
		{
			Name:        "LZ-JP-SoftBank-家宽01",
			OriginalURL: "socks5://u4:p4@1.2.3.4:2001#LZ-JP-SoftBank-家宽01",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "residential",
		},
		{
			Name:        "LZ-JP-SoftBank-家宽02",
			OriginalURL: "socks5://u5:p5@1.2.3.4:2002#LZ-JP-SoftBank-家宽02",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "residential",
		},
	}

	chains := h.BuildFullMeshRegionalChains(transits, resis)

	// 验收标准：咱们所有自有中转服务器，全部把所有家宽链式代理
	// 3 活跃中转 (4837, 软银, IIJ) × 5 家宽 (3 US + 2 JP) = 15 条链式中继
	expectedTotal := 15
	if len(chains) != expectedTotal {
		t.Fatalf("expected %d full mesh chains, got %d:\n%v", expectedTotal, len(chains), strings.Join(chains, "\n"))
	}

	// 检查 4837 是否挂载了所有 3 个美国家宽与 2 个日本家宽
	lax4837Chains := 0
	usChains := 0
	for _, c := range chains {
		unescaped, _ := url.QueryUnescape(c)
		if strings.Contains(unescaped, "美西 ACCK 洛杉矶 4837") {
			lax4837Chains++
			if strings.Contains(unescaped, "LZ-US") {
				usChains++
			}
		}
		// 救活待命节点绝不应出现在链式中继中
		if strings.Contains(unescaped, "CF-WS") {
			t.Errorf("standby CF-WS node should not be in chains: %s", c)
		}
	}
	if lax4837Chains != 5 {
		t.Errorf("expected 5 chains with 4837 (all 5 resis), got %d", lax4837Chains)
	}
	if usChains != 3 {
		t.Errorf("expected 3 US chains with 4837, got %d", usChains)
	}

	// 验收标准：节点名（# fragment）洁净度，严禁含有 '+' 编码残留字符
	for _, c := range chains {
		hashIdx := strings.Index(c, "#")
		if hashIdx != -1 {
			fragment := c[hashIdx+1:]
			if strings.Contains(fragment, "+") {
				t.Errorf("chain URL fragment contains '+' residue: %s (full: %s)", fragment, c)
			}
		}
	}
}
