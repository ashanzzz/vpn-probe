package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/healer"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
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
  - name: "LAX-US-Pavlov-家宽"
    type: vless
    server: 2.3.4.5
    port: 443

proxy-groups:
  - name: "选择代理"
    type: select
    proxies: ["自建-美国和美国家宽", "外部-节点"]
  - name: "自建-美国和美国家宽"
    type: select
    proxies: ["LZ-US-Pavlov-家宽", "美西 ACCK 洛杉矶 4837 — VLESS Reality", "LAX-US-Pavlov-家宽"]
  - name: "外部-节点"
    type: select
    proxies: ["蜂窝云-美区01"]

rules:
  - GEOSITE,youtube,自建-美国和美国家宽
  - GEOIP,US,自建-美国和美国家宽
  - MATCH,选择代理

rule-providers:
  provider_b:
    type: http
  provider_a:
    type: http
  provider_c:
    type: http
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
	sc := substore.NewClient(&cfg.SubStore)
	sch := monitor.NewScheduler(cfg, nil, nil, sc)
	h := healer.NewHealer(nil, nil)
	sch.SetHealerForTest(h)

	sch.InjectNodeStateForTest("US-Transit", &model.NodeProbeResult{
		ID:          "1",
		Name:        "美西 ACCK 洛杉矶 4837 — VLESS Reality",
		OriginalURL: "vless://u1@38.175.222.44:36564#US-LAX-4837",
		ExitCountry: "US",
		Online:      true,
		NodeType:    "transit",
		LatencyMs:   120,
		TestedAt:    time.Now(),
	})
	sch.InjectNodeStateForTest("US-Resi", &model.NodeProbeResult{
		ID:          "2",
		Name:        "LZ-US-Pavlov-家宽",
		OriginalURL: "socks5://u:***@1.2.3.4:1001#LZ-US-Pavlov-家宽",
		ExitCountry: "US",
		Online:      true,
		NodeType:    "residential",
		LatencyMs:   250,
		TestedAt:    time.Now(),
	})

	server := NewAPIServer(cfg, nil, nil, sc, nil, sch, nil)
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
	if out.TotalProxies != 4 {
		t.Errorf("expected 4 proxies, got %d", out.TotalProxies)
	}
	if out.TotalGroups != 3 {
		t.Errorf("expected 3 groups, got %d", out.TotalGroups)
	}
	if len(out.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(out.Rules))
	}

	// Verify rule-providers deterministic sorting
	if len(out.RuleProviders) != 3 || out.RuleProviders[0] != "provider_a" || out.RuleProviders[1] != "provider_b" || out.RuleProviders[2] != "provider_c" {
		t.Errorf("expected sorted rule-providers [provider_a, provider_b, provider_c], got %v", out.RuleProviders)
	}

	// Verify group classification and proxy alias resolution
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
			// Verify alias matching for chain node LAX-US-Pavlov-家宽
			for _, p := range g.Proxies {
				if p.Name == "LAX-US-Pavlov-家宽" {
					if p.Kind != "chain" {
						t.Errorf("expected LAX-US-Pavlov-家宽 to be resolved as chain kind, got %q", p.Kind)
					}
					if !p.IsSelfHosted {
						t.Errorf("expected LAX-US-Pavlov-家宽 to have IsSelfHosted=true")
					}
				}
			}
		}
		if g.Name == "外部-节点" {
			foundExt = true
			if !g.IsExternal {
				t.Errorf("expected 外部-节点 to have IsExternal=true")
			}
			// Verify fallback raw type resolution for airport proxies
			for _, p := range g.Proxies {
				if p.Name == "蜂窝云-美区01" {
					if p.Type != "vless" {
						t.Errorf("expected 蜂窝云-美区01 to preserve raw type 'vless', got %q", p.Type)
					}
					if p.Kind != "airport" {
						t.Errorf("expected 蜂窝云-美区01 to be airport kind, got %q", p.Kind)
					}
				}
			}
		}
	}
	if !foundSelf || !foundExt {
		t.Errorf("missing expected groups: foundSelf=%v, foundExt=%v", foundSelf, foundExt)
	}
}
