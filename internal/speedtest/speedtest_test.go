package speedtest

import (
	"testing"

	"vpn-probe/internal/model"
)

func TestCalculateScore(t *testing.T) {
	res := &model.SpeedResult{
		SpeedMbps: 60.0,
		AvgMbps:   60.0,
		PeakMbps:  80.0,
		TTFBMs:    150,
	}
	score := CalculateScore(res, 90, true)
	// speed: 0.5 * 60 = 30
	// latency: 0.3 * (100 - 90/3 = 70) = 21
	// stability: 0.2 * 100 = 20
	// total = 30 + 21 + 20 = 71.0
	if score < 70.0 || score > 72.0 {
		t.Fatalf("expected score ~71.0, got %.1f", score)
	}

	offlineScore := CalculateScore(res, 90, false)
	if offlineScore != 0.0 {
		t.Fatalf("expected 0.0 for offline, got %.1f", offlineScore)
	}
}

func TestRankNodes(t *testing.T) {
	nodes := []*model.NodeProbeResult{
		{Name: "NodeA", Online: true, Score: 50.0, LatencyMs: 150},
		{Name: "NodeB", Online: false, Score: 90.0, LatencyMs: 50},
		{Name: "NodeC", Online: true, Score: 85.0, LatencyMs: 90},
	}

	sorted := RankNodes(nodes)
	if sorted[0].Name != "NodeC" {
		t.Fatalf("expected first to be NodeC (score 85), got %s", sorted[0].Name)
	}
	if sorted[1].Name != "NodeA" {
		t.Fatalf("expected second to be NodeA (score 50), got %s", sorted[1].Name)
	}
	if sorted[2].Name != "NodeB" {
		t.Fatalf("expected third to be NodeB (offline), got %s", sorted[2].Name)
	}
}
