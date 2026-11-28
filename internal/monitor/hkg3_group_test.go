package monitor

import (
	"testing"

	"vpn-probe/internal/model"
)

func TestGetHostGroupKey_HKG3(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"HK-HKG3-NAT", "vless://u@216.236.60.76:30021?security=reality#HK-HKG3-NAT", "HKG-3"},
		{"香港-HKG3-NAT", "vless://u@216.236.60.76:30021#%E9%A6%99%E6%B8%AF-HKG3-NAT", "HKG-3"},
		{"any-name", "vless://u@216.236.60.76:30021#any-name", "HKG-3"},
	}
	for _, tc := range cases {
		res := &model.NodeProbeResult{Name: tc.name, OriginalURL: tc.url}
		if got := getHostGroupKey(res); got != tc.want {
			t.Errorf("getHostGroupKey(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// 香港家宽不得被误并入 HKG-3 线路机分组（只有 host 或 HKG 标识才算）。
func TestGetHostGroupKey_HKResidentialNotHKG3(t *testing.T) {
	res := &model.NodeProbeResult{
		Name:        "🇭🇰 香港-HKT动态家宽",
		OriginalURL: "vless://u@203.0.113.9:443#hk-hkt",
	}
	if got := getHostGroupKey(res); got == "HKG-3" {
		t.Fatalf("HK residential node must not be grouped as HKG-3, got %q", got)
	}
}
