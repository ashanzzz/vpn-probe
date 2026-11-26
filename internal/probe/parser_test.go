package probe

import (
	"testing"
)

func TestParseLink(t *testing.T) {
	tests := []struct {
		name       string
		link       string
		wantType   string
		wantServer string
		wantPort   int
	}{
		{
			name:     "Trojan Link",
			link:     "trojan://mypassword@example.com:443?sni=example.com#MyTrojan",
			wantType: "trojan",
		},
		{
			name:     "VLESS Reality Link",
			link:     "vless://a1b2c3d4-e5f6-7890-abcd-ef1234567890@1.2.3.4:443?security=reality&sni=test.com&pbk=fakekey#MyVLESS",
			wantType: "vless",
		},
		{
			name:     "Shadowsocks Link",
			link:     "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@1.2.3.4:8388#MySS",
			wantType: "shadowsocks",
		},
		{
			name:     "Sing-box Raw JSON",
			link:     `{"type":"vless","tag":"json-node","server":"1.2.3.4","server_port":443,"uuid":"test-uuid"}`,
			wantType: "vless",
		},
		{
			name:     "Socks5 Link",
			link:     "socks5://user1:***@167.104.167.151:40981#LZ-JP-SoftBank-家宽",
			wantType: "socks5",
		},
		{
			name:       "VLESS IPv6 Link",
			link:       "vless://a1b2c3d4-e5f6-7890-abcd-ef1234567890@[2606:4700:4700::1111]:8443?security=reality&sni=test.com#MyVLESS6",
			wantType:   "vless",
			wantServer: "2606:4700:4700::1111",
			wantPort:   8443,
		},
		{
			name:       "Trojan IPv6 Link",
			link:       "trojan://mypassword@[2001:db8::1]:443?sni=example.com#MyTrojan6",
			wantType:   "trojan",
			wantServer: "2001:db8::1",
			wantPort:   443,
		},
		{
			name:       "Shadowsocks IPv6 Link",
			link:       "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@[2001:db8::2]:8388#MySS6",
			wantType:   "shadowsocks",
			wantServer: "2001:db8::2",
			wantPort:   8388,
		},
		{
			name:       "Hysteria2 IPv6 Link",
			link:       "hysteria2://mypassword@[2001:db8::3]:8443?sni=example.com#MyHy2",
			wantType:   "hysteria2",
			wantServer: "2001:db8::3",
			wantPort:   8443,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseLink(tt.link)
			if err != nil {
				t.Fatalf("ParseLink failed: %v", err)
			}
			if parsed.Protocol != tt.wantType {
				t.Errorf("Protocol = %v, want %v", parsed.Protocol, tt.wantType)
			}
			if parsed.RawOutbound == nil {
				t.Errorf("RawOutbound is nil")
			}
			if tt.wantServer != "" {
				if s, ok := parsed.RawOutbound["server"].(string); !ok || s != tt.wantServer {
					t.Errorf("server = %v, want %v", parsed.RawOutbound["server"], tt.wantServer)
				}
			}
			if tt.wantPort != 0 {
				if p, ok := parsed.RawOutbound["server_port"].(int); !ok || p != tt.wantPort {
					t.Errorf("server_port = %v, want %v", parsed.RawOutbound["server_port"], tt.wantPort)
				}
			}
		})
	}
}

func TestParseLink_DialerProxyAndDetour(t *testing.T) {
	link := "vless://813d55b2-fca7-42be-ae66-0bfe14b80f80@167.104.167.151:49040?security=reality&sni=www.apple.com&pbk=abc&sid=def&dialerProxy=US-ACCK-4837#US-ACCK-4837%20%E2%9E%94%20LZ-US-Pavlov-%E5%AE%B6%E5%AE%BD"
	parsed, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink failed: %v", err)
	}
	if parsed.DialerProxy != "US-ACCK-4837" {
		t.Fatalf("expected DialerProxy 'US-ACCK-4837', got %q", parsed.DialerProxy)
	}

	eng := &Engine{
		transitLinks: map[string]string{
			"US-ACCK-4837": "vless://5b84d275-d328-4565-9b47-20e9121fee7e@38.175.222.44:36564?security=reality&sni=aws.amazon.com&pbk=xyz&sid=1234#US-ACCK-4837",
		},
	}
	obs, finalTag, err := eng.buildLaunchOutbounds(parsed)
	if err != nil {
		t.Fatalf("buildLaunchOutbounds failed: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("expected 2 outbounds (landing + transit), got %d", len(obs))
	}
	if finalTag != parsed.Tag {
		t.Fatalf("expected finalTag %q, got %q", parsed.Tag, finalTag)
	}
	detour, _ := obs[0]["detour"].(string)
	transitTag, _ := obs[1]["tag"].(string)
	if detour == "" || detour != transitTag {
		t.Fatalf("expected landing outbound detour %q to match transit tag %q", detour, transitTag)
	}
}
