package healer

import (
	"net/url"
	"strings"
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

func TestHealPortBlocked_IPv4AndIPv6(t *testing.T) {
	h := NewHealer(nil, nil)

	// IPv4 node
	resV4 := &model.NodeProbeResult{
		Name:        "US-LAX-Reality",
		OriginalURL: "vless://uuid@1.2.3.4:443?security=reality#US-LAX-Reality",
		Diagnostic: &model.DiagnosticInfo{
			FaultType:  model.FaultPortBlocked,
			TargetIP:   "1.2.3.4",
			TargetPort: 443,
		},
	}
	newV4, err := h.HealPortBlocked(nil, resV4)
	if err != nil {
		t.Fatalf("HealPortBlocked IPv4 failed: %v", err)
	}
	uV4, err := url.Parse(newV4)
	if err != nil {
		t.Fatalf("parse healed IPv4 url failed: %v", err)
	}
	if uV4.Hostname() != "1.2.3.4" || uV4.Port() == "443" || uV4.Port() == "" {
		t.Errorf("expected new port in healed IPv4 url, got %s", newV4)
	}

	// IPv6 node
	resV6 := &model.NodeProbeResult{
		Name:        "JP-Tokyo-IPv6",
		OriginalURL: "vless://uuid@[2001:db8::1]:443?security=reality#JP-Tokyo-IPv6",
		Diagnostic: &model.DiagnosticInfo{
			FaultType:  model.FaultPortBlocked,
			TargetIP:   "2001:db8::1",
			TargetPort: 443,
		},
	}
	newV6, err := h.HealPortBlocked(nil, resV6)
	if err != nil {
		t.Fatalf("HealPortBlocked IPv6 failed: %v", err)
	}
	uV6, err := url.Parse(newV6)
	if err != nil {
		t.Fatalf("parse healed IPv6 url failed: %v", err)
	}
	if uV6.Hostname() != "2001:db8::1" || uV6.Port() == "443" || uV6.Port() == "" {
		t.Errorf("expected new port in healed IPv6 url, got %s", newV6)
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

func TestBuildSmartChains_ExcludeChinaExitAndMachineDown(t *testing.T) {
	h := NewHealer(nil, nil)

	transits := []*model.NodeProbeResult{
		{
			Name:      "JP-Transit-Leaking",
			Online:    true,
			ChinaExit: true, // 送中，必须被过滤
			ExitCountry: "JP",
		},
		{
			Name:      "JP-Transit-Clean",
			Online:    true,
			ChinaExit: false, // 正常洁净节点
			ExitCountry: "JP",
		},
	}

	resis := []*model.NodeProbeResult{
		{
			Name:        "LZ-JP-Dead-Machine",
			OriginalURL: "socks5://u:p@167.104.167.151:10001#LZ-JP-Dead-Machine",
			Online:      false,
			Diagnostic: &model.DiagnosticInfo{
				FaultType: model.FaultMachineDown, // 宕机，必须被跳过
			},
		},
		{
			Name:        "LZ-JP-Working",
			OriginalURL: "socks5://u:p@167.104.167.151:10002#LZ-JP-Working",
			Online:      true,
			ExitCountry: "JP",
		},
	}

	chains := h.BuildSmartChains(transits, resis)
	if len(chains) != 1 {
		t.Fatalf("expected 1 chain, got %d: %v", len(chains), chains)
	}

	// 必须配对洁净中继，绝不包含送中中继
	if strings.Contains(chains[0], "JP-Transit-Leaking") {
		t.Errorf("chain must not pair with China-leaking transit: %s", chains[0])
	}
	if !strings.Contains(chains[0], "JP-Transit-Clean") {
		t.Errorf("chain should pair with clean transit: %s", chains[0])
	}
}

func TestHealIPBlocked_AvoidChinaExit(t *testing.T) {
	h := NewHealer(nil, nil)

	blockedNode := &model.NodeProbeResult{
		Name:        "US-Blocked-Node",
		OriginalURL: "vless://u@1.2.3.4:443#US-Blocked-Node",
		ExitIP:      "1.2.3.4",
		Online:      false,
	}

	relays := []*model.NodeProbeResult{
		{
			Name:      "Relay-Leaking",
			ExitIP:    "5.6.7.8",
			Online:    true,
			ChinaExit: true, // 送中，不能优先选
		},
		{
			Name:      "Relay-Clean",
			ExitIP:    "9.10.11.12",
			Online:    true,
			ChinaExit: false, // 洁净
		},
	}

	healedURL := h.HealIPBlocked(blockedNode, relays)
	if strings.Contains(healedURL, "Relay-Leaking") {
		t.Errorf("healed URL should not use China-leaking relay: %s", healedURL)
	}
	if !strings.Contains(healedURL, "Relay-Clean") {
		t.Errorf("healed URL should select clean relay: %s", healedURL)
	}
}

func TestBuildSmartChains_ExcludeResiChinaExit(t *testing.T) {
	h := NewHealer(nil, nil)
	transits := []*model.NodeProbeResult{
		{Name: "Transit-1", Online: true, ExitCountry: "US"},
	}
	resis := []*model.NodeProbeResult{
		{Name: "Resi-Normal", Online: true, ExitCountry: "US", OriginalURL: "socks5://u:p@1.1.1.1:10001#Resi-Normal"},
		{Name: "Resi-Leaking", Online: true, ExitCountry: "US", ChinaExit: true, OriginalURL: "socks5://u:p@1.1.1.1:10002#Resi-Leaking"},
	}
	chains := h.BuildSmartChains(transits, resis)
	if len(chains) != 1 {
		t.Fatalf("expected 1 chain (excluding ChinaExit resi), got %d: %v", len(chains), chains)
	}
	if strings.Contains(chains[0], "Resi-Leaking") {
		t.Errorf("chain should not include ChinaExit resi: %s", chains[0])
	}
}

func TestHealIPBlocked_FragmentQuestionMark(t *testing.T) {
	h := NewHealer(nil, nil)
	blockedNode := &model.NodeProbeResult{
		Name:        "US-Node",
		OriginalURL: "vless://u@1.2.3.4:443#US-Node?query=1", // ? only in hash fragment
		ExitIP:      "1.2.3.4",
		Online:      false,
	}
	relays := []*model.NodeProbeResult{
		{Name: "Relay-Clean", ExitIP: "9.10.11.12", Online: true, ChinaExit: false},
	}
	healedURL := h.HealIPBlocked(blockedNode, relays)
	// base has no ?, so separator must be ? not &
	if !strings.Contains(healedURL, "443?dialerProxy=") {
		t.Errorf("expected 443?dialerProxy= in healed URL, got %s", healedURL)
	}
}

func TestCleanDialerProxyParam_NoDuplicate(t *testing.T) {
	raw := "vless://u@1.2.3.4:443?security=reality&dialerProxy=OldRelay&sni=example.com"
	cleaned := cleanDialerProxyParam(raw)
	if strings.Contains(cleaned, "dialerProxy") {
		t.Errorf("expected dialerProxy to be removed, got: %s", cleaned)
	}
	if !strings.Contains(cleaned, "security=reality") || !strings.Contains(cleaned, "sni=example.com") {
		t.Errorf("expected other params preserved, got: %s", cleaned)
	}

	// Test kebab-case and case-insensitive dialer-proxy
	kebabRaw := "vless://u@1.2.3.4:443?security=reality&dialer-proxy=OldRelay&sni=example.com"
	kebabCleaned := cleanDialerProxyParam(kebabRaw)
	if strings.Contains(strings.ToLower(kebabCleaned), "dialer-proxy") {
		t.Errorf("expected dialer-proxy to be removed, got: %s", kebabCleaned)
	}

	upperRaw := "vless://u@1.2.3.4:443?security=reality&DIALERPROXY=OldRelay&sni=example.com"
	upperCleaned := cleanDialerProxyParam(upperRaw)
	if strings.Contains(strings.ToLower(upperCleaned), "dialerproxy") {
		t.Errorf("expected DIALERPROXY to be removed, got: %s", upperCleaned)
	}

	h := NewHealer(nil, nil)
	res := &model.NodeProbeResult{
		Name:        "US-Test",
		OriginalURL: "vless://u@1.2.3.4:443?security=reality&dialerProxy=ExistingRelay#US-Test",
		ExitIP:      "1.2.3.4",
		Online:      false,
	}
	relays := []*model.NodeProbeResult{
		{Name: "Relay-Clean", ExitIP: "9.10.11.12", Online: true, ChinaExit: false},
	}
	healed := h.HealIPBlocked(res, relays)
	count := strings.Count(healed, "dialerProxy=")
	if count != 1 {
		t.Errorf("expected exactly 1 dialerProxy parameter, got %d in %s", count, healed)
	}
}
