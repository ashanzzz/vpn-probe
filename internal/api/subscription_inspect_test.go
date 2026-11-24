package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
  - name: "链式|东京软银 ➔ 未知家宽01"
    type: socks5
    server: 2.3.4.5
    port: 1080
  - name: "JP--ACCK-SB"
    type: vless
    server: 4.5.6.7
    port: 443

proxy-groups:
  - name: "选择代理"
    type: select
    proxies: ["自建-美国家宽", "外部-节点"]
  - name: "自建-美国家宽"
    type: select
    proxies: ["LZ-US-Pavlov-家宽", "美西 ACCK 洛杉矶 4837 — VLESS Reality", "LAX-US-Pavlov-家宽", "链式|东京软银 ➔ 未知家宽01"]
  - name: "自建-不送中"
    type: select
    proxies: ["JP--ACCK-SB"]
  - name: "自建-其他"
    type: select
    proxies: ["LZ-US-Pavlov-家宽"]
  - name: "外部-节点"
    type: select
    proxies: ["蜂窝云-美区01"]

rules:
  - GEOSITE,youtube,自建-美国家宽
  - GEOIP,US,自建-美国家宽
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
	if out.TotalProxies != 6 {
		t.Errorf("expected 6 proxies, got %d", out.TotalProxies)
	}
	if out.TotalGroups != 5 {
		t.Errorf("expected 5 groups, got %d", out.TotalGroups)
	}
	if len(out.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(out.Rules))
	}

	// Verify rule-providers deterministic sorting
	if len(out.RuleProviders) != 3 || out.RuleProviders[0] != "provider_a" || out.RuleProviders[1] != "provider_b" || out.RuleProviders[2] != "provider_c" {
		t.Errorf("expected sorted rule-providers [provider_a, provider_b, provider_c], got %v", out.RuleProviders)
	}

	// Verify group classification and proxy alias resolution
	var foundSelf, foundExt, foundOther, foundClean bool
	for _, g := range out.Groups {
		if g.Name == "自建-不送中" {
			foundClean = true
			if g.IsExternal {
				t.Errorf("expected 自建-不送中 to have IsExternal=false")
			}
			if g.Category != "self_clean" {
				t.Errorf("expected 自建-不送中 to have Category='self_clean', got %q", g.Category)
			}
			hasTag := false
			for _, tag := range g.Tags {
				if tag == "自建·防送中直通" {
					hasTag = true
					break
				}
			}
			if !hasTag {
				t.Errorf("expected 自建-不送中 to have tag '自建·防送中直通', got %v", g.Tags)
			}
			for _, p := range g.Proxies {
				if p.Name == "JP--ACCK-SB" {
					if p.Kind != "transit" {
						t.Errorf("expected JP--ACCK-SB to fallback to transit kind, got %q", p.Kind)
					}
					if !p.IsSelfHosted {
						t.Errorf("expected JP--ACCK-SB to have IsSelfHosted=true")
					}
				}
			}
		}
		if g.Name == "自建-其他" {
			foundOther = true
			if g.IsExternal {
				t.Errorf("expected 自建-其他 to have IsExternal=false")
			}
			if g.Category != "self_resi" {
				t.Errorf("expected 自建-其他 to have Category='self_resi', got %q", g.Category)
			}
			hasTag := false
			for _, tag := range g.Tags {
				if tag == "自建·其他家宽" {
					hasTag = true
					break
				}
			}
			if !hasTag {
				t.Errorf("expected 自建-其他 to have tag '自建·其他家宽', got %v", g.Tags)
			}
		}
		if g.Name == "自建-美国家宽" {
			foundSelf = true
			if g.IsExternal {
				t.Errorf("expected 自建-美国家宽 to have IsExternal=false")
			}
			if g.RuleCount != 2 {
				t.Errorf("expected 2 rules targeting 自建-美国家宽, got %d", g.RuleCount)
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
					if p.Country != "US" {
						t.Errorf("expected LAX-US-Pavlov-家宽 country to be 'US', got %q", p.Country)
					}
				}
				if p.Name == "链式|东京软银 ➔ 未知家宽01" {
					if p.Kind != "chain" {
						t.Errorf("expected 链式|东京软银 ➔ 未知家宽01 to fallback to chain kind, got %q", p.Kind)
					}
					if !p.IsSelfHosted {
						t.Errorf("expected 链式|东京软银 ➔ 未知家宽01 to have IsSelfHosted=true")
					}
					if p.Country != "JP" {
						t.Errorf("expected 链式|东京软银 ➔ 未知家宽01 country to be 'JP', got %q", p.Country)
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
	if !foundSelf || !foundExt || !foundOther || !foundClean {
		t.Errorf("missing expected groups: foundSelf=%v, foundExt=%v, foundOther=%v, foundClean=%v", foundSelf, foundExt, foundOther, foundClean)
	}
	if out.RoutingScript == "" {
		t.Errorf("expected non-empty RoutingScript in inspection response")
	}

	// 3. Test route aliases (/api/subscription/:name/inspect, /api/subscriptions/:name/inspect, /api/v1/subscription/:name/inspect)
	aliases := []string{
		"/api/subscription/test-sub/inspect",
		"/api/subscriptions/test-sub/inspect",
		"/api/v1/subscription/test-sub/inspect",
	}
	for _, aliasPath := range aliases {
		reqAlias := httptest.NewRequest(http.MethodGet, aliasPath, nil)
		reqAlias.Header.Set("Authorization", "Bearer test-auth-token")
		respAlias, err := server.app.Test(reqAlias)
		if err != nil {
			t.Fatalf("alias request %s failed: %v", aliasPath, err)
		}
		if respAlias.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for alias %s, got %d", aliasPath, respAlias.StatusCode)
		}
	}
}

func TestSubscriptionInspectEndpoint_BusinessProfile(t *testing.T) {
	tmpDir := t.TempDir()
	mockYAML := `
proxies:
  - name: "美西 ACCK 洛杉矶 4837 — VLESS Reality"
    type: vless
    server: 38.175.222.44
    port: 36564
  - name: "LZ-US-Pavlov-家宽"
    type: socks5
    server: 167.104.167.151
    port: 49040
  - name: "LZ-JP-KDDI-家宽"
    type: socks5
    server: 167.104.167.151
    port: 20640

proxy-groups:
  - name: "选择代理"
    type: select
    proxies: ["自建-线路机", "自建-美国家宽", "自建-日本家宽", "自建-不送中"]
  - name: "自建-线路机"
    type: select
    proxies: ["美西 ACCK 洛杉矶 4837 — VLESS Reality"]
  - name: "自建-美国家宽"
    type: select
    proxies: ["LZ-US-Pavlov-家宽"]
  - name: "自建-日本家宽"
    type: select
    proxies: ["LZ-JP-KDDI-家宽"]
  - name: "自建-不送中"
    type: url-test
    proxies: ["美西 ACCK 洛杉矶 4837 — VLESS Reality", "LZ-US-Pavlov-家宽", "LZ-JP-KDDI-家宽"]

rules:
  - MATCH,选择代理
`

	snapDir := filepath.Join(tmpDir, "snapshots")
	_ = os.MkdirAll(snapDir, 0755)
	_ = os.WriteFile(filepath.Join(snapDir, "business.yaml"), []byte(mockYAML), 0644)

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
	server := NewAPIServer(cfg, nil, nil, sc, nil, sch, nil)
	server.snapshotMgr = substore.NewSnapshotManager(sc, snapDir)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/business/inspect", nil)
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
		t.Fatalf("decode failed: %v", err)
	}

	if out.Subscription != "business" {
		t.Errorf("expected subscription 'business', got %s", out.Subscription)
	}
	if out.TotalProxies != 3 {
		t.Errorf("expected 3 proxies, got %d", out.TotalProxies)
	}
	if out.TotalGroups != 5 {
		t.Errorf("expected 5 groups, got %d", out.TotalGroups)
	}

	// Verify that self-hosted groups in business profile are classified correctly
	for _, g := range out.Groups {
		if strings.HasPrefix(g.Name, "自建-") {
			if g.IsExternal {
				t.Errorf("expected %s to have IsExternal=false", g.Name)
			}
			if g.Category == "external_default" {
				t.Errorf("expected %s to have self_ category, got %s", g.Name, g.Category)
			}
		}
	}
}

func TestDeleteSubStoreItem_InvalidTypeReturns400(t *testing.T) {
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
	server := NewAPIServer(cfg, nil, nil, sc, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/substore/invalid-type/some-name", nil)
	req.Header.Set("Authorization", "Bearer test-auth-token")
	resp, err := server.app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for invalid item type, got %d", resp.StatusCode)
	}
}
