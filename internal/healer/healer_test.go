package healer

import (
	"testing"

	"vpn-probe/internal/model"
)

func TestGenerateRandomHighPort(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := GenerateRandomHighPort()
		if p < 20000 || p > 50000 {
			t.Fatalf("port %d out of range [20000, 50000]", p)
		}
	}
}

func TestBuildSmartChains(t *testing.T) {
	h := NewHealer(nil, nil)

	transits := []*model.NodeProbeResult{
		{
			Name:   "东京软银-直连",
			Online: true,
			Diagnostic: &model.DiagnosticInfo{
				TargetIP: "45.192.242.72",
			},
		},
		{
			Name:   "美西4837-直连",
			Online: true,
			Diagnostic: &model.DiagnosticInfo{
				TargetIP: "38.175.222.44",
			},
		},
	}

	resis := []*model.NodeProbeResult{
		{
			Name:        "LZ-JP-SoftBank-家宽",
			OriginalURL: "socks5://user1:pass1@167.104.167.151:40981#LZ-JP-SoftBank-家宽",
			Online:      true,
		},
		{
			Name:        "LZ-US-Pavlov-家宽",
			OriginalURL: "socks5://user5:pass5@167.104.167.151:49040#LZ-US-Pavlov-家宽",
			Online:      true,
		},
	}

	chains := h.BuildSmartChains(transits, resis)
	if len(chains) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(chains))
	}

	// 验证日本家宽是否配对东京软银
	if !contains(chains[0], "dialerProxy=%E4%B8%9C%E4%BA%AC%E8%BD%AF%E9%93%B6-%E7%9B%B4%E8%BF%9E") &&
		!contains(chains[0], "dialerProxy=东京软银-直连") {
		t.Errorf("expected chain[0] to pair with 东京软银, got: %s", chains[0])
	}

	// 验证美国家宽是否配对美西4837
	if !contains(chains[1], "dialerProxy=%E7%BE%8E%E8%A5%BF4837-%E7%9B%B4%E8%BF%9E") &&
		!contains(chains[1], "dialerProxy=美西4837-直连") {
		t.Errorf("expected chain[1] to pair with 美西4837, got: %s", chains[1])
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
