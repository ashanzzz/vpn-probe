package model

import (
	"testing"
)

func TestResolveCountry(t *testing.T) {
	tests := []struct {
		name        string
		exitCountry string
		nodeName    string
		expected    string
	}{
		{
			name:        "Explicit ExitCountry takes precedence",
			exitCountry: "JP",
			nodeName:    "Some-Random-Name",
			expected:    "JP",
		},
		{
			name:        "Arrow chain takes target side",
			exitCountry: "",
			nodeName:    "链式|US-LAX-4837 ➔ LZ-JP-SoftBank-家宽",
			expected:    "JP",
		},
		{
			name:        "Compact chain US broadband (SB-美宽-Pavlov)",
			exitCountry: "",
			nodeName:    "SB-美宽-Pavlov",
			expected:    "US",
		},
		{
			name:        "Compact chain JP broadband with US prefix (LAX-日宽-SoftBank)",
			exitCountry: "",
			nodeName:    "LAX-日宽-SoftBank",
			expected:    "JP",
		},
		{
			name:        "Compact chain TW broadband (SB-台宽-HiNet)",
			exitCountry: "",
			nodeName:    "SB-台宽-HiNet",
			expected:    "TW",
		},
		{
			name:        "Compact chain KR broadband (SB-韩宽-KT)",
			exitCountry: "",
			nodeName:    "SB-韩宽-KT",
			expected:    "KR",
		},
		{
			name:        "Compact chain SG broadband (LAX-新宽-StarHub)",
			exitCountry: "",
			nodeName:    "LAX-新宽-StarHub",
			expected:    "SG",
		},
		{
			name:        "Standard US direct",
			exitCountry: "",
			nodeName:    "US-LAX-ACCK-4837-Reality",
			expected:    "US",
		},
		{
			name:        "Standard JP direct",
			exitCountry: "",
			nodeName:    "JP-Tokyo-Softbank-Reality-Vision",
			expected:    "JP",
		},
		{
			name:        "Explicit GB ExitCountry normalizes to UK",
			exitCountry: "GB",
			nodeName:    "Some-Random-Name",
			expected:    "UK",
		},
		{
			name:        "UK node with London",
			exitCountry: "",
			nodeName:    "UK-London-Direct",
			expected:    "UK",
		},
		{
			name:        "Airport code LAX with digit suffix",
			exitCountry: "",
			nodeName:    "US-LAX02",
			expected:    "US",
		},
		{
			name:        "Airport code FRA with digit suffix",
			exitCountry: "",
			nodeName:    "DE-FRA01-Direct",
			expected:    "DE",
		},
		{
			name:        "Airport code SIN with digit suffix",
			exitCountry: "",
			nodeName:    "SG-SIN01",
			expected:    "SG",
		},
		{
			name:        "Underscore delimiter",
			exitCountry: "",
			nodeName:    "US_LAX_01",
			expected:    "US",
		},
		{
			name:        "Bracket delimiter",
			exitCountry: "",
			nodeName:    "[US] Direct-01",
			expected:    "US",
		},
		{
			name:        "Space delimiter",
			exitCountry: "",
			nodeName:    "US Direct 01",
			expected:    "US",
		},
		{
			name:        "Collision prevention BUSINESS",
			exitCountry: "",
			nodeName:    "BUSINESS-NODE-01",
			expected:    "OTHER",
		},
		{
			name:        "Collision prevention GALAXY",
			exitCountry: "",
			nodeName:    "GALAXY-NODE-01",
			expected:    "OTHER",
		},
		{
			name:        "Collision prevention PROJEKT",
			exitCountry: "",
			nodeName:    "DE-PROJEKT-01",
			expected:    "DE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveCountry(tc.exitCountry, tc.nodeName)
			if got != tc.expected {
				t.Errorf("ResolveCountry(%q, %q) = %q; want %q", tc.exitCountry, tc.nodeName, got, tc.expected)
			}
		})
	}
}
