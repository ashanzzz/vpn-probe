package probe

import (
	"testing"
)

func TestDetermineIPType(t *testing.T) {
	tests := []struct {
		name              string
		ip                string
		isp               string
		org               string
		isResidentialNode bool
		expected          string
	}{
		{
			name:     "Cloudflare WARP IP prefix",
			ip:       "104.28.19.4",
			isp:      "Cloudflare, Inc.",
			expected: "机房",
		},
		{
			name:     "Frontier Residential IP",
			ip:       "47.141.117.80",
			isp:      "Frontier Communications of America, Inc.",
			expected: "家宽",
		},
		{
			name:     "Softbank Residential / Telecom IP",
			ip:       "156.231.113.91",
			isp:      "Softbank Corp.",
			expected: "家宽",
		},
		{
			name:     "AWS Hosting",
			ip:       "54.214.12.3",
			isp:      "Amazon.com, Inc.",
			org:      "AWS EC2",
			expected: "机房",
		},
		{
			name:     "DigitalOcean Hosting",
			ip:       "167.172.10.5",
			isp:      "DigitalOcean, LLC",
			expected: "机房",
		},
		{
			name:     "Biglobe Residential IP",
			ip:       "133.202.199.234",
			isp:      "Biglobe",
			org:      "Biglobe",
			expected: "家宽",
		},
		{
			name:     "GLBB Japan Residential IP",
			ip:       "202.12.245.91",
			isp:      "GLBB Japan KK",
			expected: "家宽",
		},
		{
			name:     "Pavlov Media US Residential IP",
			ip:       "96.63.208.40",
			isp:      "Pavlov Media",
			expected: "家宽",
		},
		{
			name:     "Korea Telecom KT Residential IP",
			ip:       "115.14.193.210",
			isp:      "KT",
			expected: "家宽",
		},
		{
			name:     "StarHub Singapore Residential IP",
			ip:       "202.156.27.163",
			isp:      "StarHub Ltd",
			expected: "家宽",
		},
		{
			name:     "Singtel Singapore Residential IP",
			ip:       "116.86.10.20",
			isp:      "Singtel",
			expected: "家宽",
		},
		{
			name:              "Unknown IP on residential node",
			ip:                "192.0.2.1",
			isResidentialNode: true,
			expected:          "家宽",
		},
		{
			name:              "Unknown IP default",
			ip:                "192.0.2.1",
			isResidentialNode: false,
			expected:          "机房",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineIPType(tt.ip, tt.isp, tt.org, tt.isResidentialNode)
			if got != tt.expected {
				t.Errorf("DetermineIPType(%q, %q, %q, %v) = %q; want %q",
					tt.ip, tt.isp, tt.org, tt.isResidentialNode, got, tt.expected)
			}
		})
	}
}
