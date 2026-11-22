package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/substore"
)

func TestVerifyEndpoint_ResidentialAware(t *testing.T) {
	ResetAuthTracker()
	cleanupDomains := substore.SetShareDomainsForTest(nil)
	defer cleanupDomains()

	// 1. Mock SubStore Server: 返回合法订阅与策略组
	mockSubStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": []map[string]any{
				{
					"name":    "ashan",
					"content": "vless://transit@1.1.1.1:443#Transit\nsocks5://user:pass@167.104.167.151:40981#Resi01",
				},
			},
		})
	}))
	defer mockSubStore.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-token-123",
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     mockSubStore.URL,
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	sch := monitor.NewScheduler(cfg, nil, nil, sc)

	// 注入 1 个在线骨干专线节点 + 1 个国内阻断但海外隧道畅通的住宅家宽节点
	now := time.Now()
	transitResult := &model.NodeProbeResult{
		ID:          "transit-1",
		Name:        "JP-Tokyo-Transit",
		OriginalURL: "vless://transit@1.1.1.1:443#Transit",
		NodeType:    "transit",
		IsSelf:      true,
		Online:      true,
		LatencyMs:   100,
		TestedAt:    now,
	}

	resiResult := &model.NodeProbeResult{
		ID:          "resi-1",
		Name:        "LZ-JP-Resi-01",
		OriginalURL: "socks5://user:pass@167.104.167.151:40981#Resi01",
		NodeType:    "residential",
		IsSelf:      true,
		Online:      false, // 国内直连被墙，但海外隧道通
		LatencyMs:   0,
		TestedAt:    now,
		Diagnostic: &model.DiagnosticInfo{
			FaultType:       model.FaultResidentialDirectBlocked,
			StatusText:      "【仅境外通】国内直连受阻/需经自建专线链式中继",
			AbroadReachable: true,
			ActionType:      "CHAIN_VIA_TRANSIT",
		},
	}

	sch.InjectNodeStateForTest("JP-Tokyo-Transit", transitResult)
	sch.InjectNodeStateForTest("LZ-JP-Resi-01", resiResult)

	server := NewAPIServer(cfg, nil, nil, sc, nil, sch, nil)
	app := server.app

	// 请求 /api/substore/verify?file=ashan
	req := httptest.NewRequest("GET", "/api/substore/verify?file=ashan", nil)
	req.Header.Set("Authorization", "Bearer admin-token-123")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// 验证自建节点健康判定：住宅家宽境外隧道正常时，必须判定整体自建节点健康！
	healthy, _ := res["self_nodes_healthy"].(bool)
	if !healthy {
		t.Errorf("expected self_nodes_healthy to be true for healthy chained residential nodes, got false")
	}

	chainedCount, _ := res["self_nodes_residential_chained"].(float64)
	if chainedCount != 1 {
		t.Errorf("expected self_nodes_residential_chained == 1, got %v", chainedCount)
	}

	onlineCount, _ := res["self_nodes_online"].(float64)
	if onlineCount != 1 {
		t.Errorf("expected self_nodes_online == 1, got %v", onlineCount)
	}

	// 模拟家宽节点彻底故障 (机器离线且海外隧道断)
	resiResult.Diagnostic.AbroadReachable = false
	resiResult.Diagnostic.FaultType = model.FaultMachineDown

	req2 := httptest.NewRequest("GET", "/api/substore/verify?file=ashan", nil)
	req2.Header.Set("Authorization", "Bearer admin-token-123")
	resp2, _ := app.Test(req2, 5000)
	var res2 map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&res2)

	healthy2, _ := res2["self_nodes_healthy"].(bool)
	if healthy2 {
		t.Errorf("expected self_nodes_healthy to be false when residential node tunnel is down, got true")
	}
}
