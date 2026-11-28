package monitor

import (
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

func TestGetHostGroupKey_HKG3Variants(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"HK-HKG3-NAT", "vless://u@216.236.60.76:15088#HK-HKG3-NAT", "HKG-3"},
		{"HK-HKG-3-NAT", "vless://u@1.2.3.4:12345#HK-HKG-3-NAT", "HKG-3"},
		{"LZ-HKG3", "vless://u@216.236.60.76:15088#LZ-HKG3", "HKG-3"},
	} {
		res := &model.NodeProbeResult{
			Name:        tc.name,
			OriginalURL: tc.url,
		}
		if got := getHostGroupKey(res); got != tc.want {
			t.Errorf("getHostGroupKey(%q, %q) = %q, want %q", tc.name, tc.url, got, tc.want)
		}
	}
}

func TestScheduler_ReloadMonitoredNodes(t *testing.T) {
	cfg := &config.Config{}
	sch := NewScheduler(cfg, nil, nil, nil)
	sch.ReloadMonitoredNodes([]config.MonitoredNode{
		{
			Name:   "HK-HKG3-NAT",
			URL:    "vless://u@216.236.60.76:15088#HK-HKG3-NAT",
			IsSelf: true,
		},
	})

	sch.mu.RLock()
	st, ok := sch.nodeStates["HK-HKG3-NAT"]
	cfgCount := len(sch.cfg.Monitor.Nodes)
	sch.mu.RUnlock()

	if !ok || st == nil || st.LastResult == nil {
		t.Fatalf("expected HK-HKG3-NAT state created after ReloadMonitoredNodes")
	}
	if cfgCount != 1 || st.LastResult.NodeType != "transit" {
		t.Fatalf("unexpected reloaded node state: cfgCount=%d node=%+v", cfgCount, st.LastResult)
	}
}
