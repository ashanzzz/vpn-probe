package probe

import "testing"

func TestDetectNATRegion_HKG3(t *testing.T) {
	if got := DetectNATRegion("216.236.60.76", ""); got != "HK" {
		t.Fatalf("DetectNATRegion(216.236.60.76) = %q, want HK", got)
	}
	// 既有行为不得回归
	if got := DetectNATRegion("167.104.167.151", ""); got != "JP" {
		t.Fatalf("167.104.x must stay JP, got %q", got)
	}
	if got := DetectNATRegion("45.192.242.72", ""); got != "US" {
		t.Fatalf("45.192.x must stay US, got %q", got)
	}
}

func TestDetectRegionFromName_HKG3(t *testing.T) {
	if got := DetectRegionFromName("HK-HKG3-NAT", ""); got != "HK" {
		t.Fatalf("DetectRegionFromName(HK-HKG3-NAT) = %q, want HK", got)
	}
}
