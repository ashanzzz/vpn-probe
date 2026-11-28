package store

import "testing"

func containsKeyword(list []string, kw string) bool {
	for _, v := range list {
		if v == kw {
			return true
		}
	}
	return false
}

func TestTransitTabKeywords_IncludesHongKong(t *testing.T) {
	kws := transitTabKeywords()
	for _, want := range []string{"HK", "HKG", "香港"} {
		if !containsKeyword(kws, want) {
			t.Errorf("transit tab keywords missing %q: %v", want, kws)
		}
	}
}

func TestOtherRegionTabKeywords_IncludesHongKong(t *testing.T) {
	kws := otherRegionTabKeywords()
	for _, want := range []string{"HK", "HKG", "香港"} {
		if !containsKeyword(kws, want) {
			t.Errorf("other_region tab keywords missing %q: %v", want, kws)
		}
	}
}
