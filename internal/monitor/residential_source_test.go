package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/substore"
)

func TestRefreshResidentialNodes_JSONSyncChangeDetectionAndFallback(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "residential_nodes.json")

	var mode atomic.Int32
	mode.Store(1) // 1=initial 2 nodes, 2=updated 2 nodes (port changed + new node), 3=500 error

	var gotBearer string
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBearer = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-API-Key")
		if gotBearer != "Bearer test-read-token" || gotAPIKey != "test-read-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		switch mode.Load() {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"instance_id": "11433",
				"host_public_ip": "167.104.167.151",
				"total_nodes": 2,
				"mapped_nodes": 2,
				"exported_nodes": 2,
				"nodes": [
					{
						"id": 5,
						"priority": 1,
						"name": "美国-Pavlov动态家宽",
						"country_code": "US",
						"flag_emoji": "🇺🇸",
						"public_port": 49040,
						"mapped": true,
						"host_ip": "167.104.167.151",
						"sni": "www.apple.com",
						"uuid": "813d55b2-fca7-42be-ae66-0bfe14b80f80",
						"public_key": "pubk1",
						"short_id": "9b4d2b10",
						"vless_url": "vless://813d55b2-fca7-42be-ae66-0bfe14b80f80@167.104.167.151:49040?encryption=none&flow=xtls-rprx-vision&fp=chrome&pbk=pubk1&security=reality&sid=9b4d2b10&sni=www.apple.com&type=tcp#%F0%9F%87%BA%F0%9F%87%B8%20%E7%BE%8E%E5%9B%BD-Pavlov%E5%8A%A8%E6%80%81%E5%AE%B6%E5%AE%BD"
					},
					{
						"id": 1,
						"priority": 2,
						"name": "日本-SoftBank动态",
						"country_code": "JP",
						"flag_emoji": "🇯🇵",
						"public_port": 40981,
						"mapped": true,
						"host_ip": "167.104.167.151",
						"sni": "www.apple.com",
						"uuid": "f44f2805-932b-44f5-a01b-4d3ee2ce99e5",
						"public_key": "pubk1",
						"short_id": "9b4d2b10",
						"vless_url": "vless://f44f2805-932b-44f5-a01b-4d3ee2ce99e5@167.104.167.151:40981?encryption=none&flow=xtls-rprx-vision&fp=chrome&pbk=pubk1&security=reality&sid=9b4d2b10&sni=www.apple.com&type=tcp#%F0%9F%87%AF%F0%9F%87%B5%20%E6%97%A5%E6%9C%AC-SoftBank%E5%8A%A8%E6%80%81"
					}
				]
			}`))
		case 2:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"instance_id": "11433",
				"nodes": [
					{
						"id": 5,
						"priority": 1,
						"name": "美国-Pavlov动态家宽",
						"flag_emoji": "🇺🇸",
						"mapped": true,
						"vless_url": "vless://813d55b2-fca7-42be-ae66-0bfe14b80f80@167.104.167.151:55555?encryption=none&security=reality#🇺🇸 美国-Pavlov动态家宽"
					},
					{
						"id": 9,
						"priority": 3,
						"name": "新加坡-StarHub动态家宽",
						"flag_emoji": "🇸🇬",
						"mapped": true,
						"vless_url": "vless://2e37b4c0-b8c8-4058-a85e-c3a682f1488a@167.104.167.151:56190?encryption=none&security=reality#🇸🇬 新加坡-StarHub动态家宽"
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`upstream down`))
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			Nodes: []config.MonitoredNode{
				{Name: "US-ACCK-4837", URL: "vless://u@38.175.222.44:36564#US-ACCK-4837", IsSelf: true},
			},
			ResidentialSource: config.ResidentialSourceConfig{
				Enabled:         true,
				URL:             srv.URL + "/api/lazycat/egress-subscriptions?instance_id=11433&format=json&only_mapped=true",
				Token:           "test-read-token",
				RefreshInterval: "5m",
				CachePath:       cachePath,
			},
		},
	}

	s := NewScheduler(cfg, nil, nil, nil)

	// 1. Initial sync updates cache and nodeStates
	changed, err := s.RefreshResidentialNodes(context.Background())
	if err != nil {
		t.Fatalf("initial refresh failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on initial sync")
	}

	cachedBytes1, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("expected cache file to be written: %v", err)
	}
	var cached1 []config.MonitoredNode
	if err := json.Unmarshal(cachedBytes1, &cached1); err != nil || len(cached1) != 2 {
		t.Fatalf("expected 2 cached nodes, got %d (err=%v)", len(cached1), err)
	}
	if cached1[0].Name != "🇺🇸 美国-Pavlov动态家宽" {
		t.Fatalf("expected decoded tag name, got %q", cached1[0].Name)
	}

	st := s.GetStatus()
	if len(st) != 3 { // 1 transit + 2 residential
		t.Fatalf("expected 3 monitored nodes, got %d", len(st))
	}
	if st["🇯🇵 日本-SoftBank动态"].LastResult.NodeType != "residential" {
		t.Fatalf("expected NodeType residential")
	}

	// 2. Unchanged response skips cache rewrite
	infoBefore, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	changed, err = s.RefreshResidentialNodes(context.Background())
	if err != nil {
		t.Fatalf("second refresh failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false when upstream unchanged")
	}
	infoAfter, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache after: %v", err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Fatalf("expected cache file not to be rewritten when unchanged")
	}

	// 3. Upstream changed: port changed on Pavlov, SoftBank removed, StarHub added
	mode.Store(2)
	changed, err = s.RefreshResidentialNodes(context.Background())
	if err != nil {
		t.Fatalf("third refresh failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on upstream update")
	}
	st = s.GetStatus()
	if len(st) != 3 { // US-ACCK-4837 + Pavlov + StarHub
		t.Fatalf("expected 3 nodes after pruning removed SoftBank and adding StarHub, got %d: %+v", len(st), st)
	}
	if _, ok := st["🇯🇵 日本-SoftBank动态"]; ok {
		t.Fatalf("expected removed residential node SoftBank to be pruned")
	}
	if _, ok := st["🇸🇬 新加坡-StarHub动态家宽"]; !ok {
		t.Fatalf("expected new residential node StarHub to be added")
	}
	if !strings.Contains(st["🇺🇸 美国-Pavlov动态家宽"].Config.URL, ":55555") {
		t.Fatalf("expected Pavlov URL to be updated to port 55555, got %s", st["🇺🇸 美国-Pavlov动态家宽"].Config.URL)
	}
	if _, ok := st["US-ACCK-4837"]; !ok {
		t.Fatalf("expected transit node US-ACCK-4837 to remain untouched")
	}

	// 4. Upstream error falls back and preserves local cache
	mode.Store(3)
	changed, err = s.RefreshResidentialNodes(context.Background())
	if err == nil {
		t.Fatalf("expected error when upstream returns 500")
	}
	if changed {
		t.Fatalf("expected changed=false on upstream failure")
	}
	syncStatus := s.GetResidentialSyncStatus()
	if !syncStatus.UsingCacheFallback || syncStatus.NodeCount != 2 || syncStatus.LastError == "" {
		t.Fatalf("unexpected syncStatus on fallback: %+v", syncStatus)
	}
	if len(s.GetStatus()) != 3 {
		t.Fatalf("expected 3 nodes preserved during upstream failure")
	}

	// 5. Restarting scheduler with failed upstream loads from residential_nodes.json cache
	sRestarted := NewScheduler(cfg, nil, nil, nil)
	stRestarted := sRestarted.GetStatus()
	if len(stRestarted) != 3 {
		t.Fatalf("expected 3 nodes restored from cache on restart, got %d", len(stRestarted))
	}
	if !strings.Contains(stRestarted["🇺🇸 美国-Pavlov动态家宽"].Config.URL, ":55555") {
		t.Fatalf("expected cached port 55555 on restart")
	}
}

func TestRefreshResidentialNodes_PlainTextURIAndSubStorePreservation(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "residential_nodes.json")

	egressSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("vless://u1@167.104.167.151:49040?encryption=none&security=reality#🇺🇸 美国-Pavlov动态家宽\n"))
	}))
	defer egressSrv.Close()

	// SubStore only returns transit node (has stale/different copy of residential or no residential yet)
	subSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":[
			{"name":"ashan-transit","content":"vless://t1@38.175.222.44:36564#US-ACCK-4837"},
			{"name":"ashan-us-resi","content":"vless://old@167.104.167.151:11111#🇺🇸 美国-Pavlov动态家宽"}
		]}`))
	}))
	defer subSrv.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			ResidentialSource: config.ResidentialSourceConfig{
				Enabled:   true,
				URL:       egressSrv.URL,
				CachePath: cachePath,
			},
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     subSrv.URL,
		},
	}

	s := NewScheduler(cfg, nil, nil, substore.NewClient(&cfg.SubStore))
	changed, err := s.RefreshResidentialNodes(context.Background())
	if err != nil || !changed {
		t.Fatalf("expected plain URI refresh success (changed=%v, err=%v)", changed, err)
	}

	if err := s.RefreshNodesFromSubStore(context.Background()); err != nil {
		t.Fatalf("RefreshNodesFromSubStore failed: %v", err)
	}

	st := s.GetStatus()
	if len(st) != 2 {
		t.Fatalf("expected 2 nodes (transit + dynamic residential), got %d", len(st))
	}
	if !strings.Contains(st["🇺🇸 美国-Pavlov动态家宽"].Config.URL, ":49040") {
		t.Fatalf("expected dynamic residential URL (:49040) not to be overwritten by stale SubStore (:11111), got %s",
			st["🇺🇸 美国-Pavlov动态家宽"].Config.URL)
	}
}
