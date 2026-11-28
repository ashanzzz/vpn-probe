package speedtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vpn-probe/internal/model"
)

func TestValidateAndProbeSelfHostedEndpoint(t *testing.T) {
	if err := ValidateSelfHostedURL("https://speed.cloudflare.com/__down"); err == nil {
		t.Fatalf("expected public speedtest host to be blocked")
	}
	if err := ValidateSelfHostedURL("not-a-url"); err == nil {
		t.Fatalf("expected invalid url to fail")
	}

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("0123456789abcdef"))
	}))
	defer okSrv.Close()

	code, n, err := ProbeSelfHostedEndpoint(context.Background(), okSrv.URL+"/10m.bin")
	if err != nil || code != http.StatusPartialContent || n == 0 {
		t.Fatalf("expected probe success, got code=%d n=%d err=%v", code, n, err)
	}

	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer authSrv.Close()

	code, _, err = ProbeSelfHostedEndpoint(context.Background(), authSrv.URL+"/10m.bin")
	if err == nil || code != http.StatusUnauthorized || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected HTTP 401 failure, got code=%d err=%v", code, err)
	}
}

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
