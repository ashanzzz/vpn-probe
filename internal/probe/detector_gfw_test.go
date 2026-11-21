package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArbitrateGFW(t *testing.T) {
	tests := []struct {
		name          string
		googleSuccess bool
		googleChina   bool
		googleDetail  string
		cfSuccess     bool
		cfChina       bool
		cfLoc         string
		cfIP          string
		cfDetail      string
		wantChina     bool
		wantCertain   bool
		wantReason    string
	}{
		{
			name:          "Dual probe failed (unreachable/timeout)",
			googleSuccess: false,
			googleChina:   false,
			googleDetail:  "unreachable: i/o timeout",
			cfSuccess:     false,
			cfChina:       false,
			cfLoc:         "",
			cfIP:          "",
			cfDetail:      "trace err: dial tcp timeout",
			wantChina:     false,
			wantCertain:   false,
			wantReason:    "unreachable",
		},
		{
			name:          "Both clean overseas node (e.g. Tokyo)",
			googleSuccess: true,
			googleChina:   false,
			googleDetail:  "code 200, loc: ",
			cfSuccess:     true,
			cfChina:       false,
			cfLoc:         "JP",
			cfIP:          "156.231.113.91",
			cfDetail:      "cf loc=JP (ip: 156.231.113.91)",
			wantChina:     false,
			wantCertain:   true,
			wantReason:    "clean",
		},
		{
			name:          "CF loc=CN confirmed SongZhong",
			googleSuccess: true,
			googleChina:   true,
			googleDetail:  "redirected to https://www.google.com.hk",
			cfSuccess:     true,
			cfChina:       true,
			cfLoc:         "CN",
			cfIP:          "110.242.68.66",
			cfDetail:      "cf loc=CN (ip: 110.242.68.66)",
			wantChina:     true,
			wantCertain:   true,
			wantReason:    "cf_loc_cn",
		},
		{
			name:          "Conflict: Google redirected to HK/CN, but CF trace proves clean US/JP exit (CF wins)",
			googleSuccess: true,
			googleChina:   true,
			googleDetail:  "redirected to https://www.google.com.hk",
			cfSuccess:     true,
			cfChina:       false,
			cfLoc:         "US",
			cfIP:          "38.175.222.44",
			cfDetail:      "cf loc=US (ip: 38.175.222.44)",
			wantChina:     false,
			wantCertain:   true,
			wantReason:    "clean",
		},
		{
			name:          "Google failed/timeout, but CF succeeded with clean JP",
			googleSuccess: false,
			googleChina:   false,
			googleDetail:  "unreachable: timeout",
			cfSuccess:     true,
			cfChina:       false,
			cfLoc:         "JP",
			cfIP:          "156.231.113.91",
			cfDetail:      "cf loc=JP (ip: 156.231.113.91)",
			wantChina:     false,
			wantCertain:   true,
			wantReason:    "clean",
		},
		{
			name:          "CF failed, but Google succeeded with redirect to CN",
			googleSuccess: true,
			googleChina:   true,
			googleDetail:  "redirected to https://www.google.cn",
			cfSuccess:     false,
			cfChina:       false,
			cfLoc:         "",
			cfIP:          "",
			cfDetail:      "trace err: connection reset",
			wantChina:     true,
			wantCertain:   true,
			wantReason:    "redirected_cn",
		},
		{
			name:          "CF failed, but Google succeeded with clean 200",
			googleSuccess: true,
			googleChina:   false,
			googleDetail:  "code 200, loc: ",
			cfSuccess:     false,
			cfChina:       false,
			cfLoc:         "",
			cfIP:          "",
			cfDetail:      "trace err: connection reset",
			wantChina:     false,
			wantCertain:   true,
			wantReason:    "clean",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := ArbitrateGFW(tc.googleSuccess, tc.googleChina, tc.googleDetail, tc.cfSuccess, tc.cfChina, tc.cfLoc, tc.cfIP, tc.cfDetail)
			if res.IsChina != tc.wantChina {
				t.Errorf("IsChina got %v, want %v", res.IsChina, tc.wantChina)
			}
			if res.Certain != tc.wantCertain {
				t.Errorf("Certain got %v, want %v", res.Certain, tc.wantCertain)
			}
			if res.Reason != tc.wantReason {
				t.Errorf("Reason got %s, want %s", res.Reason, tc.wantReason)
			}
		})
	}
}

func TestCheckGFW_Blackhole(t *testing.T) {
	// Pointing to an unused local port to simulate blackhole / unreachable connection
	// Issue #6 acceptance criterion: "用一个必然超时的假节点测试，结果应为「不可达」，不是「送中」"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close() // Immediately close so connections fail

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res := CheckGFW(ctx, port, "http://127.0.0.1:1/ncr", "http://127.0.0.1:1/trace", 200*time.Millisecond)
	if res.IsChina {
		t.Errorf("Unreachable blackhole must NOT be classified as China exit, got IsChina=true")
	}
	if res.Certain {
		t.Errorf("Unreachable blackhole must have Certain=false, got true")
	}
	if res.Reason != "unreachable" {
		t.Errorf("Unreachable blackhole must have Reason='unreachable', got %s", res.Reason)
	}
}

func TestParseCloudflareTrace(t *testing.T) {
	raw := `fl=706f36
h=cloudflare.com
ip=156.231.113.91
ts=1732174200.123
visit_scheme=https
uag=curl/8.4.0
colo=NRT
sliver=none
http=http/2
loc=JP
tls=TLSv1.3
sni=plaintext
warp=off
gateway=off
rbi=off
kex=X25519`

	ip, loc := parseCloudflareTrace(strings.NewReader(raw))
	if ip != "156.231.113.91" {
		t.Errorf("expected ip 156.231.113.91, got %s", ip)
	}
	if loc != "JP" {
		t.Errorf("expected loc JP, got %s", loc)
	}
}

func TestMockServers(t *testing.T) {
	// Verify httptest works with custom handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://www.google.com.hk")
		w.WriteHeader(http.StatusFound)
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("failed to GET mock: %v", err)
	}
	resp.Body.Close()
}
