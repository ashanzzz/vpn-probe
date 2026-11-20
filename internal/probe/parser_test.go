package probe

import (
	"testing"
)

func TestParseLink(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		wantType string
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
			link:     "socks5://user1:-n9U8PoHRK7uVb2zxcKpww@167.104.167.151:40981#LZ-JP-SoftBank-家宽",
			wantType: "socks5",
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
		})
	}
}
