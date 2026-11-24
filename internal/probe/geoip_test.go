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
