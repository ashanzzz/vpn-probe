package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/monitor"
)

func TestMonitorResidentialEndpoints(t *testing.T) {
	ResetAuthTracker()

	var failUpstream atomic.Bool
	egressSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failUpstream.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"nodes": [
				{
					"id": 5,
					"name": "美国-Pavlov动态家宽",
					"flag_emoji": "🇺🇸",
					"mapped": true,
					"vless_url": "vless://813d55b2-fca7-42be-ae66-0bfe14b80f80@167.104.167.151:49040?encryption=none&security=reality&pbk=secretpbk&sid=9b4d2b10#🇺🇸 美国-Pavlov动态家宽"
				}
			]
		}`))
	}))
	defer egressSrv.Close()

	cachePath := filepath.Join(t.TempDir(), "residential_nodes.json")
	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken:  "write-tok",
			ReadToken:  "read-tok",
			AdminToken: "admin-tok",
		},
		Monitor: config.MonitorConfig{
			Enabled: true,
			ResidentialSource: config.ResidentialSourceConfig{
				Enabled:   true,
				URL:       egressSrv.URL,
				CachePath: cachePath,
			},
		},
	}

	sched := monitor.NewScheduler(cfg, nil, nil, nil)
	server := NewAPIServer(cfg, nil, nil, nil, nil, sched, nil)
	app := server.app

	// 1. Read token cannot POST refresh
	reqForbid := httptest.NewRequest(http.MethodPost, "/api/monitor/residential/refresh", nil)
	reqForbid.Header.Set("Authorization", "Bearer read-tok")
	respForbid, err := app.Test(reqForbid, 5000)
	if err != nil {
		t.Fatalf("app.Test forbid: %v", err)
	}
	if respForbid.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for read token POST, got %d", respForbid.StatusCode)
	}

	// 2. Write token triggers refresh -> 200 changed=true
	reqRefresh := httptest.NewRequest(http.MethodPost, "/api/monitor/residential/refresh", nil)
	reqRefresh.Header.Set("Authorization", "Bearer write-tok")
	respRefresh, err := app.Test(reqRefresh, 5000)
	if err != nil {
		t.Fatalf("app.Test refresh: %v", err)
	}
	if respRefresh.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on refresh, got %d", respRefresh.StatusCode)
	}
	var refreshBody map[string]any
	if err := json.NewDecoder(respRefresh.Body).Decode(&refreshBody); err != nil {
		t.Fatalf("decode refresh body: %v", err)
	}
	if changed, _ := refreshBody["changed"].(bool); !changed {
		t.Fatalf("expected changed=true, got %+v", refreshBody)
	}

	// 3. Status masks URLs for read token and denies reveal=true
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/monitor/residential/status", nil)
	reqStatus.Header.Set("Authorization", "Bearer read-tok")
	respStatus, err := app.Test(reqStatus, 5000)
	if err != nil {
		t.Fatalf("app.Test status: %v", err)
	}
	if respStatus.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on status, got %d", respStatus.StatusCode)
	}
	var statusBody struct {
		Nodes []config.MonitoredNode `json:"nodes"`
	}
	if err := json.NewDecoder(respStatus.Body).Decode(&statusBody); err != nil {
		t.Fatalf("decode status body: %v", err)
	}
	if len(statusBody.Nodes) != 1 || strings.Contains(statusBody.Nodes[0].URL, "813d55b2-fca7-42be-ae66-0bfe14b80f80") {
		t.Fatalf("expected masked URL in status response, got %+v", statusBody.Nodes)
	}

	reqRevealDeny := httptest.NewRequest(http.MethodGet, "/api/monitor/residential/status?reveal=true", nil)
	reqRevealDeny.Header.Set("Authorization", "Bearer read-tok")
	respRevealDeny, _ := app.Test(reqRevealDeny, 5000)
	if respRevealDeny.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for read token reveal=true, got %d", respRevealDeny.StatusCode)
	}

	// 4. Admin token can reveal=true
	reqRevealAdmin := httptest.NewRequest(http.MethodGet, "/api/monitor/residential/status?reveal=true", nil)
	reqRevealAdmin.Header.Set("Authorization", "Bearer admin-tok")
	respRevealAdmin, _ := app.Test(reqRevealAdmin, 5000)
	if respRevealAdmin.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for admin reveal=true, got %d", respRevealAdmin.StatusCode)
	}
	var adminBody struct {
		Nodes []config.MonitoredNode `json:"nodes"`
	}
	_ = json.NewDecoder(respRevealAdmin.Body).Decode(&adminBody)
	if len(adminBody.Nodes) != 1 || !strings.Contains(adminBody.Nodes[0].URL, "813d55b2-fca7-42be-ae66-0bfe14b80f80") {
		t.Fatalf("expected unmasked URL for admin reveal=true, got %+v", adminBody.Nodes)
	}

	// 5. Upstream failure returns 502 fallback while preserving cache
	failUpstream.Store(true)
	reqFail := httptest.NewRequest(http.MethodPost, "/api/monitor/residential/refresh", nil)
	reqFail.Header.Set("Authorization", "Bearer write-tok")
	respFail, _ := app.Test(reqFail, 5000)
	if respFail.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream failure fallback, got %d", respFail.StatusCode)
	}
}
