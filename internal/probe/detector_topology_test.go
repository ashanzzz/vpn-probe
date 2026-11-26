package probe

import (
	"testing"
	"time"

	"vpn-probe/internal/model"
)

func TestDetectRegionFromName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Tokyo Reality", "JP-Tokyo-IIJ-Reality", "JP"},
		{"AKILE IIJ", "JP-AKILE-IIJ", "JP"},
		{"Softbank", "JP--ACCK-SB", "JP"},
		{"ACCK SB", "JP-ACCK-SB", "JP"},
		{"US 4837", "US-LAX-ACCK-4837-Reality", "US"},
		{"ACCK 4837", "US-ACCK-4837", "US"},
		{"US Pavlov", "LZ-US-Pavlov-家宽", "US"},
		{"Taiwan HiNet", "LZ-TW-HiNet-家宽", "TW"},
		{"Singapore StarHub", "LZ-SG-StarHub-家宽", "SG"},
		{"Frankfurt", "DE-Frankfurt-Direct", "DE"},
		{"London", "UK-London-Direct", "UK"},
		{"Unknown fallback", "Random-Node", "FALLBACK"},
		{"Business node not SG", "BUSINESS-NODE-01", "FALLBACK"},
		{"Projekt node not KR", "DE-PROJEKT-01", "DE"},
		{"Infra node not DE", "INFRA-US-01", "US"},
		{"Galaxy node not US", "GALAXY-NODE", "FALLBACK"},
		{"Singapore Airport Token", "SG-SIN-01", "SG"},
		{"Singapore Airport Digit Suffix", "SG-SIN01", "SG"},
		{"Korea Telecom Token", "LZ-KR-KT-家宽", "KR"},
		{"Frankfurt Airport Token", "DE-FRA01-Direct", "DE"},
		{"LAX Token", "US-LAX02", "US"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectRegionFromName(tt.input, "FALLBACK")
			if got != tt.expected {
				t.Errorf("DetectRegionFromName(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildNodeHopTrace(t *testing.T) {
	resDirect := &model.NodeProbeResult{
		Name:        "LZ-TW-HiNet-家宽",
		OriginalURL: "vless://uuid@167.104.167.151:48731?security=reality#LZ-TW-HiNet-家宽",
		Online:      true,
		LatencyMs:   120,
		ExitIP:      "122.118.196.25",
		ExitCountry: "TW",
		ExitCity:    "Taichung",
		ExitIPType:  "家宽",
		TestedAt:    time.Now(),
	}

	traces := BuildNodeHopTrace(resDirect, nil, resDirect.OriginalURL)
	if len(traces) != 3 {
		t.Fatalf("expected 3 hops for direct node, got %d", len(traces))
	}
	if traces[0].Title != "客户端接入" {
		t.Errorf("expected hop 1 title '客户端接入', got %q", traces[0].Title)
	}
	if traces[1].NodePort != 48731 {
		t.Errorf("expected hop 2 port 48731, got %d", traces[1].NodePort)
	}
	if traces[2].Region != "TW" {
		t.Errorf("expected hop 3 region 'TW', got %q", traces[2].Region)
	}

	resChain := &model.NodeProbeResult{
		Name:        "东京软银 ➔ LZ-US-Pavlov-家宽",
		OriginalURL: "vless://uuid@167.104.167.151:49040?security=reality#chain",
		Online:      true,
		LatencyMs:   180,
		ExitIP:      "96.63.208.40",
		ExitCountry: "US",
		ExitCity:    "San Jose",
		ExitIPType:  "家宽",
		TestedAt:    time.Now(),
	}

	chainTraces := BuildNodeHopTrace(resChain, nil, resChain.OriginalURL)
	if len(chainTraces) != 4 {
		t.Fatalf("expected 4 hops for chain node, got %d", len(chainTraces))
	}
	if chainTraces[1].Title != "自建专线线路机" {
		t.Errorf("expected hop 2 title '自建专线线路机', got %q", chainTraces[1].Title)
	}
}

func TestBuildTopologyOverview(t *testing.T) {
	results := []*model.NodeProbeResult{
		{
			Name:        "东京软银 ➔ LZ-US-Pavlov-家宽",
			OriginalURL: "vless://uuid@167.104.167.151:49040?security=reality#chain1",
			Protocol:    "vless",
			Online:      true,
			LatencyMs:   150,
			ExitIP:      "96.63.208.40",
			ExitCountry: "US",
			ExitCity:    "San Jose",
			ExitIPType:  "家宽",
		},
		{
			Name:        "东京软银 ➔ LZ-US-Frontier-家宽",
			OriginalURL: "vless://uuid@167.104.167.151:41360?security=reality#chain2",
			Protocol:    "vless",
			Online:      false,
			LatencyMs:   0,
			ExitIP:      "47.141.117.80",
			ExitCountry: "US",
			ExitCity:    "Los Angeles",
			ExitIPType:  "家宽",
		},
		{
			Name:        "美西4837 ➔ LZ-TW-HiNet-家宽",
			OriginalURL: "vless://uuid@167.104.167.151:48731?security=reality#chain3",
			Protocol:    "vless",
			Online:      true,
			LatencyMs:   110,
			ExitIP:      "122.118.196.25",
			ExitCountry: "TW",
			ExitCity:    "Taichung",
			ExitIPType:  "家宽",
		},
	}

	overview1 := BuildTopologyOverview(results)
	overview2 := BuildTopologyOverview(results)

	// 1. 验证确定性排序 (多次调用顺序完全一致)
	if len(overview1.Transits) != len(overview2.Transits) {
		t.Fatalf("mismatched transits count: %d vs %d", len(overview1.Transits), len(overview2.Transits))
	}
	for i := range overview1.Transits {
		if overview1.Transits[i].ID != overview2.Transits[i].ID {
			t.Errorf("transit non-deterministic order at index %d: %s vs %s", i, overview1.Transits[i].ID, overview2.Transits[i].ID)
		}
	}
	for i := range overview1.Landings {
		if overview1.Landings[i].ID != overview2.Landings[i].ID {
			t.Errorf("landing non-deterministic order at index %d: %s vs %s", i, overview1.Landings[i].ID, overview2.Landings[i].ID)
		}
	}
	for i := range overview1.Links {
		if overview1.Links[i].ID != overview2.Links[i].ID {
			t.Errorf("link non-deterministic order at index %d: %s vs %s", i, overview1.Links[i].ID, overview2.Links[i].ID)
		}
	}

	// 2. 验证指标计算一致性
	totalLinks := overview1.Summary.OnlineLinks + overview1.Summary.FaultLinks
	if totalLinks != len(overview1.Links) {
		t.Errorf("summary links count mismatch: online(%d) + fault(%d) = %d != len(links)=%d",
			overview1.Summary.OnlineLinks, overview1.Summary.FaultLinks, totalLinks, len(overview1.Links))
	}
	if overview1.Summary.TotalTransits != len(overview1.Transits) {
		t.Errorf("summary total transits (%d) != len(transits) (%d)",
			overview1.Summary.TotalTransits, len(overview1.Transits))
	}
	if overview1.Summary.TotalNATRelays != len(overview1.NATRelays) {
		t.Errorf("summary total NAT relays (%d) != len(nat_relays) (%d)",
			overview1.Summary.TotalNATRelays, len(overview1.NATRelays))
	}
	if overview1.Summary.TotalLandings != len(overview1.Landings) {
		t.Errorf("summary total landings (%d) != len(landings) (%d)",
			overview1.Summary.TotalLandings, len(overview1.Landings))
	}
}

func TestBuildTopologyOverview_TransitIsolationAndMultiRegionNAT(t *testing.T) {
	results := []*model.NodeProbeResult{
		{
			Name:        "US-ACCK-4837",
			OriginalURL: "vless://u@38.175.222.44:36564?security=reality#US-ACCK-4837",
			Protocol:    "vless",
			NodeType:    "transit",
			Online:      true,
			LatencyMs:   120,
			ExitCountry: "US",
		},
		{
			Name:        "JP-AKILE-IIJ",
			OriginalURL: "vless://u@156.231.113.91:33339?security=reality#JP-AKILE-IIJ",
			Protocol:    "vless",
			NodeType:    "transit",
			Online:      true,
			LatencyMs:   75,
			ExitCountry: "JP",
		},
		{
			Name:        "US-ACCK-4837 ➔ LZ-US-Pavlov-家宽",
			OriginalURL: "vless://u@167.104.167.151:49040?security=reality&dialerProxy=US-ACCK-4837#US-ACCK-4837%20%E2%9E%94%20LZ-US-Pavlov-%E5%AE%B6%E5%AE%BD",
			Protocol:    "vless",
			NodeType:    "chain",
			Online:      true,
			LatencyMs:   180,
			ExitIP:      "96.63.208.40",
			ExitCountry: "US",
			ExitIPType:  "家宽",
		},
		{
			Name:        "JP-AKILE-IIJ ➔ LZ-JP-SoftBank-家宽",
			OriginalURL: "vless://u@167.104.167.151:40981?security=reality&dialerProxy=JP-AKILE-IIJ#JP-AKILE-IIJ%20%E2%9E%94%20LZ-JP-SoftBank-%E5%AE%B6%E5%AE%BD",
			Protocol:    "vless",
			NodeType:    "chain",
			Online:      true,
			LatencyMs:   140,
			ExitIP:      "126.1.2.3",
			ExitCountry: "JP",
			ExitIPType:  "家宽",
		},
	}

	overview := BuildTopologyOverview(results)
	if len(overview.Transits) != 2 {
		t.Fatalf("expected 2 transit nodes, got %d", len(overview.Transits))
	}
	if len(overview.NATRelays) != 1 {
		t.Fatalf("expected 1 aggregated NAT relay (transit nodes must NOT leak into NATRelays), got %d: %+v", len(overview.NATRelays), overview.NATRelays)
	}
	if overview.NATRelays[0].Region != "MULTI" {
		t.Errorf("expected multi-region NAT relay region 'MULTI', got %q", overview.NATRelays[0].Region)
	}
	if len(overview.NATRelays[0].Ports) != 2 {
		t.Errorf("expected 2 NAT ports on aggregated relay, got %d", len(overview.NATRelays[0].Ports))
	}
	if len(overview.Landings) != 2 {
		t.Errorf("expected 2 landings (transit nodes must NOT leak into Landings), got %d", len(overview.Landings))
	}
}
