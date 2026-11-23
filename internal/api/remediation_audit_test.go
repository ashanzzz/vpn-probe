package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/store"
	"vpn-probe/internal/vpsmgr"
)

// TestRemediation_AISummaryResidentialAndCongestion 测试住宅家宽链式代理与网络拥塞并存时的文案表现
func TestRemediation_AISummaryResidentialAndCongestion(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "audit-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)

	// 注入 1 个住宅家宽直连受阻节点
	scheduler.InjectNodeStateForTest("Resi-Direct-Blocked", &model.NodeProbeResult{
		Name:        "Resi-Direct-Blocked",
		OriginalURL: "vless://uuid@10.0.0.1:8443#Resi-Direct-Blocked",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "10.0.0.1",
			TargetPort:      8443,
			FaultType:       model.FaultResidentialDirectBlocked,
			StatusText:      "【仅境外通】国内直连受阻",
			ActionType:      "RELAY_RESCUE",
			TroubleshootTip: "国内直连受阻，通过自建专线链式中继",
		},
	})

	// 注入 1 个跨境网络拥塞节点
	scheduler.InjectNodeStateForTest("HK-Congestion", &model.NodeProbeResult{
		Name:        "HK-Congestion",
		OriginalURL: "vless://uuid@10.0.0.2:443#HK-Congestion",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "10.0.0.2",
			TargetPort:      443,
			FaultType:       model.FaultNetworkCongestion,
			StatusText:      "【网络拥塞超时】国内外TCP连通但应用层请求超时",
			ActionType:      "CHECK_NETWORK_ROUTING",
			TroubleshootTip: "【网络拥塞】国内外TCP均握手连通，应用层握手超时",
		},
	})

	// 注入 1 个健康节点
	scheduler.InjectNodeStateForTest("US-Healthy", &model.NodeProbeResult{
		Name:        "US-Healthy",
		OriginalURL: "vless://uuid@10.0.0.3:443#US-Healthy",
		Online:      true,
		StatusText:  "连通",
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)

	req := httptest.NewRequest("GET", "/api/summary", nil)
	req.Header.Set("Authorization", "Bearer audit-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/summary failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	summary := res["summary"].(map[string]any)
	if summary["requires_healing_action"] != false {
		t.Errorf("expected requires_healing_action=false, got %v", summary["requires_healing_action"])
	}
	if int(summary["residential_chain_nodes"].(float64)) != 1 {
		t.Errorf("expected residential_chain_nodes=1, got %v", summary["residential_chain_nodes"])
	}
	if int(summary["congestion_nodes"].(float64)) != 1 {
		t.Errorf("expected congestion_nodes=1, got %v", summary["congestion_nodes"])
	}

	prompt := summary["ai_instruction"].(string)
	if !strings.Contains(prompt, "住宅家宽节点直连受阻") || !strings.Contains(prompt, "链式中继") {
		t.Errorf("expected prompt to mention residential chain, got: %s", prompt)
	}
	if !strings.Contains(prompt, "跨境网络拥塞/抖动超时") {
		t.Errorf("expected prompt to mention network congestion, got: %s", prompt)
	}
	if !strings.Contains(prompt, "无需换端口") {
		t.Errorf("expected prompt to mention '无需换端口', got: %s", prompt)
	}
}

// TestRemediation_AISummaryResidentialAndCongestionWithPortBlocked 测试存在换端口需求时各异常分支同时存在
func TestRemediation_AISummaryResidentialAndCongestionWithPortBlocked(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "audit-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)

	// 1 端口被墙
	scheduler.InjectNodeStateForTest("Port-Blocked-1", &model.NodeProbeResult{
		Name:        "Port-Blocked-1",
		OriginalURL: "vless://uuid@10.0.0.1:443#Port-Blocked-1",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "10.0.0.1",
			TargetPort:      443,
			FaultType:       model.FaultPortBlocked,
			StatusText:      "【端口被墙】",
			ActionType:      "AI_CHANGE_PORT",
			TroubleshootTip: "端口被封锁",
		},
	})

	// 1 家宽直连受阻
	scheduler.InjectNodeStateForTest("Resi-Blocked-1", &model.NodeProbeResult{
		Name:        "Resi-Blocked-1",
		OriginalURL: "vless://uuid@10.0.0.2:443#Resi-Blocked-1",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "10.0.0.2",
			TargetPort:      443,
			FaultType:       model.FaultResidentialDirectBlocked,
			StatusText:      "【仅境外通】",
			ActionType:      "RELAY_RESCUE",
			TroubleshootTip: "家宽直连受阻",
		},
	})

	// 1 网络拥塞
	scheduler.InjectNodeStateForTest("Congestion-1", &model.NodeProbeResult{
		Name:        "Congestion-1",
		OriginalURL: "vless://uuid@10.0.0.3:443#Congestion-1",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "10.0.0.3",
			TargetPort:      443,
			FaultType:       model.FaultNetworkCongestion,
			StatusText:      "【网络拥塞超时】",
			ActionType:      "CHECK_NETWORK_ROUTING",
			TroubleshootTip: "网络拥塞",
		},
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)

	req := httptest.NewRequest("GET", "/api/summary", nil)
	req.Header.Set("Authorization", "Bearer audit-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/summary failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	summary := res["summary"].(map[string]any)
	if summary["requires_healing_action"] != true {
		t.Errorf("expected requires_healing_action=true, got %v", summary["requires_healing_action"])
	}
	if int(summary["port_blocked_nodes"].(float64)) != 1 {
		t.Errorf("expected port_blocked_nodes=1, got %v", summary["port_blocked_nodes"])
	}
	if int(summary["residential_chain_nodes"].(float64)) != 1 {
		t.Errorf("expected residential_chain_nodes=1, got %v", summary["residential_chain_nodes"])
	}
	if int(summary["congestion_nodes"].(float64)) != 1 {
		t.Errorf("expected congestion_nodes=1, got %v", summary["congestion_nodes"])
	}

	prompt := summary["ai_instruction"].(string)
	if !strings.Contains(prompt, "端口被墙需换端口") {
		t.Errorf("expected prompt to contain '端口被墙需换端口', got: %s", prompt)
	}
	if !strings.Contains(prompt, "网络拥塞/超时(无需换端口)") {
		t.Errorf("expected prompt to contain '网络拥塞/超时(无需换端口)', got: %s", prompt)
	}
}

// TestRemediation_BlockedAnalysisContract 测试 /api/vps/blocked-analysis 返回契约（analysis与results共存）
func TestRemediation_BlockedAnalysisContract(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "audit-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)

	// 注入自建节点：网络拥塞
	scheduler.InjectNodeStateForTest("Self-Congestion", &model.NodeProbeResult{
		Name:        "Self-Congestion",
		OriginalURL: "vless://uuid@10.0.0.10:443#Self-Congestion",
		Online:      false,
		IsSelf:      true,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:          "10.0.0.10",
			TargetPort:        443,
			DomesticReachable: true,
			AbroadReachable:   true,
			DomesticPing:      true,
			FaultType:         model.FaultNetworkCongestion,
			TroubleshootTip:   "【网络拥塞】链路抖动超时",
		},
	})

	// 注入自建节点：端口被墙
	scheduler.InjectNodeStateForTest("Self-PortBlocked", &model.NodeProbeResult{
		Name:        "Self-PortBlocked",
		OriginalURL: "vless://uuid@10.0.0.11:443#Self-PortBlocked",
		Online:      false,
		IsSelf:      true,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:          "10.0.0.11",
			TargetPort:        443,
			DomesticReachable: false,
			AbroadReachable:   true,
			DomesticPing:      true,
			FaultType:         model.FaultPortBlocked,
		},
	})

	vpsMgr := vpsmgr.NewManager(cfg, nil, "")
	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)
	server.SetVPSManager(vpsMgr)

	req := httptest.NewRequest("GET", "/api/vps/blocked-analysis", nil)
	req.Header.Set("Authorization", "Bearer audit-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/vps/blocked-analysis failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if res["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", res["status"])
	}
	if int(res["total"].(float64)) != 2 {
		t.Errorf("expected total=2, got %v", res["total"])
	}
	if int(res["count"].(float64)) != 2 {
		t.Errorf("expected count=2, got %v", res["count"])
	}

	// 核心验证：前端 client.ts 使用 results，VPSCenter.tsx 兼容 analysis
	// 验证两者皆存在且非空、长度一致
	resultsArr, okResults := res["results"].([]any)
	analysisArr, okAnalysis := res["analysis"].([]any)
	if !okResults || len(resultsArr) != 2 {
		t.Fatalf("expected results array of len 2, got %v", res["results"])
	}
	if !okAnalysis || len(analysisArr) != 2 {
		t.Fatalf("expected analysis array of len 2, got %v", res["analysis"])
	}

	// 深入验证分析项中的故障诊断类型与自愈标志位
	for _, rawItem := range resultsArr {
		item := rawItem.(map[string]any)
		nodeName := item["node_name"].(string)
		faultType := item["fault_type"].(string)
		canHeal := item["can_auto_heal_port"].(bool)
		canRescue := item["can_relay_rescue"].(bool)

		if nodeName == "Self-Congestion" {
			if faultType != "NETWORK_CONGESTION" {
				t.Errorf("expected Self-Congestion to have fault_type=NETWORK_CONGESTION, got %s", faultType)
			}
			if canHeal {
				t.Errorf("expected Self-Congestion can_auto_heal_port=false, got true")
			}
			if canRescue {
				t.Errorf("expected Self-Congestion can_relay_rescue=false, got true")
			}
		} else if nodeName == "Self-PortBlocked" {
			if faultType != "PORT_BLOCKED" {
				t.Errorf("expected Self-PortBlocked to have fault_type=PORT_BLOCKED, got %s", faultType)
			}
			if !canHeal {
				t.Errorf("expected Self-PortBlocked can_auto_heal_port=true, got false")
			}
		}
	}
}

// TestRemediation_StoreAndHistoryCongestion 验证持久化层与历史查询对 FaultNetworkCongestion 的正确处理
func TestRemediation_StoreAndHistoryCongestion(t *testing.T) {
	tmpDB := t.TempDir() + "/test-history.db"
	st, err := store.NewStore(tmpDB)
	if err != nil {
		t.Fatalf("create store failed: %v", err)
	}
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	item := &model.NodeProbeResult{
		ID:          "store-cong-1",
		Name:        "Test-Congestion-Node",
		OriginalURL: "vless://uuid@1.1.1.1:443#Test-Congestion-Node",
		Online:      false,
		LatencyMs:   9999,
		StatusText:  "【网络拥塞超时】",
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:          "1.1.1.1",
			TargetPort:        443,
			DomesticReachable: true,
			AbroadReachable:   true,
			DomesticPing:      true,
			FaultType:         model.FaultNetworkCongestion,
			TroubleshootTip:   "应用层握手超时",
		},
	}

	if err := st.SaveResult(ctx, item); err != nil {
		t.Fatalf("save probe result failed: %v", err)
	}

	records, err := st.GetRecentResults(ctx, 10)
	if err != nil {
		t.Fatalf("get recent results failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Diagnostic == nil {
		t.Fatalf("expected diagnostic info not nil")
	}
	if records[0].Diagnostic.FaultType != model.FaultNetworkCongestion {
		t.Errorf("expected fault_type=%s, got %s", model.FaultNetworkCongestion, records[0].Diagnostic.FaultType)
	}
}
