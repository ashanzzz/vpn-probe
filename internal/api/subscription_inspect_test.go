package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/substore"
)

func TestSubscriptionInspectEndpoint(t *testing.T) {
	// Setup test SnapshotManager with mock YAML
	tmpDir := t.TempDir()
	mockYAML := `
proxies:
  - name: "LZ-US-Pavlov-家宽"
    type: socks5
    server: 1.2.3.4
    port: 1080
  - name: "美西 ACCK 洛杉矶 4837 — VLESS Reality"
    type: vless
    server: 2.3.4.5
    port: 443
  - name: "蜂窝云-美区01"
    type: vless
    server: 3.4.5.6
    port: 443

proxy-groups:
  - name: "选择代理"
    type: select
    proxies: ["自建-美国和美国家宽", "外部-节点"]
  - name: "自建-美国和美国家宽"
    type: select
    proxies: ["LZ-US-Pavlov-家宽", "美西 ACCK 洛杉矶 4837 — VLESS Reality"]
  - name: "外部-节点"
    type: select
    proxies: ["蜂窝云-美区01"]

rules:
  - GEOSITE,youtube,自建-美国和美国家宽
  - GEOIP,US,自建-美国和美国家宽
  - MATCH,选择代理
`

	snapDir := filepath.Join(tmpDir, "snapshots")
	_ = os.MkdirAll(snapDir, 0755)
	_ = os.WriteFile(filepath.Join(snapDir, "test-sub.yaml"), []byte(mockYAML), 0644)

	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-auth-token",
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     "http://127.0.0.1:3001",
		},
	}
	server := NewAPIServer(cfg, nil, nil, nil, nil, nil, nil)
	sm := substore.NewSnapshotManager(substore.NewClient(&cfg.SubStore), snapDir)
	server.snapshotMgr = sm

	// 1. Missing name returns 404/400
	reqMissing := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions//inspect", nil)
	reqMissing.Header.Set("Authorization", "Bearer test-auth-token")
	respMissing, _ := server.app.Test(reqMissing)
	if respMissing.StatusCode != http.StatusNotFound && respMissing.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 or 404, got %d", respMissing.StatusCode)
	}

	// 2. Query valid inspect
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/test-sub/inspect", nil)
	req.Header.Set("Authorization", "Bearer test-auth-token")
	resp, err := server.app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out SubscriptionInspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if out.Subscription != "test-sub" {
		t.Errorf("expected subscription 'test-sub', got %s", out.Subscription)
	}
	if out.TotalProxies != 3 {
		t.Errorf("expected 3 proxies, got %d", out.TotalProxies)
	}
	if out.TotalGroups != 3 {
		t.Errorf("expected 3 groups, got %d", out.TotalGroups)
	}
	if len(out.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(out.Rules))
	}

	// Verify group classification
	var foundSelf, foundExt bool
	for _, g := range out.Groups {
		if g.Name == "自建-美国和美国家宽" {
			foundSelf = true
			if g.IsExternal {
				t.Errorf("expected 自建-美国和美国家宽 to have IsExternal=false")
			}
			if g.RuleCount != 2 {
				t.Errorf("expected 2 rules targeting 自建-美国和美国家宽, got %d", g.RuleCount)
			}
		}
		if g.Name == "外部-节点" {
			foundExt = true
			if !g.IsExternal {
				t.Errorf("expected 外部-节点 to have IsExternal=true")
			}
		}
	}
	if !foundSelf || !foundExt {
		t.Errorf("missing expected groups: foundSelf=%v, foundExt=%v", foundSelf, foundExt)
	}
}
