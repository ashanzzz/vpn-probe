package api

import (
	"strings"
	"testing"
)

func TestMaskNodeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		omits    []string
	}{
		{
			name:  "SOCKS5 with user and pass",
			input: "socks5://user1:realSecretPass123@167.104.167.151:40981#LZ-JP-SoftBank-家宽",
			contains: []string{
				"socks5://user1:******@167.104.167.151:40981",
				"#LZ-JP-SoftBank-家宽",
			},
			omits: []string{"realSecretPass123"},
		},
		{
			name:  "SOCKS5 with pass only",
			input: "socks5://singleSecretPass456@167.104.167.151:40981#LZ-JP-SoftBank-家宽",
			contains: []string{
				"socks5://******@167.104.167.151:40981",
				"#LZ-JP-SoftBank-家宽",
			},
			omits: []string{"singleSecretPass456"},
		},
		{
			name:  "Hysteria2 with password",
			input: "hysteria2://myHy2SecretPassword@38.175.222.44:36565?sni=hy2.example.com#Hy2-Node",
			contains: []string{
				"hysteria2://******@38.175.222.44:36565",
				"#Hy2-Node",
			},
			omits: []string{"myHy2SecretPassword"},
		},
		{
			name:  "VLESS Reality with UUID, pbk, sid",
			input: "vless://5b84d275-d328-4565-9b47-20e9121fee7e@38.175.222.44:36563?encryption=none&security=reality&sni=aws.amazon.com&fp=chrome&type=tcp&pbk=Djl4lJnk4RIOWfywQJxvCRwhGyZdUdSk9Xquv-35TzI&sid=3c4ffcc0#US-LAX-ACCK-4837-Reality",
			contains: []string{
				"vless://5b84****ee7e@38.175.222.44:36563",
				"pbk=Djl4****",
				"sid=****",
				"sni=aws.amazon.com",
				"#US-LAX-ACCK-4837-Reality",
			},
			omits: []string{
				"-d328-4565-9b47-",
				"Djl4lJnk4RIOWfywQJxvCRwhGyZdUdSk9Xquv-35TzI",
				"3c4ffcc0",
			},
		},
		{
			name:  "Shadowsocks 2022 base64",
			input: "ss://MjAyMi1ibGFrZTMtYWVzLTEyOC1nY206MDZEc2ZxMXhLRHhYL0FJelRITW1tUT09@45.192.242.72:24443#JP-Tokyo-Softbank-SS2022",
			contains: []string{
				"ss://******@45.192.242.72:24443",
				"#JP-Tokyo-Softbank-SS2022",
			},
			omits: []string{"MjAyMi1ibGFrZTMtYWVz"},
		},
		{
			name:  "Trojan with password",
			input: "trojan://secretpassword123@example.com:443#trojan-node",
			contains: []string{
				"trojan://******@example.com:443",
				"#trojan-node",
			},
			omits: []string{"secretpassword123"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := MaskNodeURL(tc.input)
			for _, c := range tc.contains {
				if !strings.Contains(output, c) {
					t.Errorf("expected output to contain %q, but got %q", c, output)
				}
			}
			for _, o := range tc.omits {
				if strings.Contains(output, o) {
					t.Errorf("expected output to NOT contain %q, but got %q", o, output)
				}
			}
		})
	}
}
