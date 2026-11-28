package model

import "testing"

func TestResolveCountry_HKG3(t *testing.T) {
	cases := []string{"HK-HKG3-NAT", "🇭🇰 香港-HKG3-NAT", "HKG-3"}
	for _, name := range cases {
		if got := ResolveCountry("", name); got != "HK" {
			t.Errorf("ResolveCountry(%q) = %q, want HK", name, got)
		}
	}
}

// 命名硬规则守护：名字里带 LZ- 会被判为家宽，所以 HKG-3 节点不得使用 LZ- 前缀。
func TestIsResidentialNode_HKG3NamingRule(t *testing.T) {
	if IsResidentialNode("HK-HKG3-NAT", "vless://u@216.236.60.76:30021#HK-HKG3-NAT", "vless") {
		t.Fatal("HK-HKG3-NAT must be treated as transit, not residential")
	}
	if !IsResidentialNode("LZ-HKG-3", "vless://u@216.236.60.76:30021#LZ-HKG-3", "vless") {
		t.Fatal("LZ- prefix is expected to be classified residential (documents why naming is forbidden)")
	}
	for _, bad := range []string{"HK-动态家宽", "HK-residential"} {
		if !IsResidentialNode(bad, "vless://u@1.1.1.1:443#x", "vless") {
			t.Errorf("%q should be classified residential", bad)
		}
	}
}
