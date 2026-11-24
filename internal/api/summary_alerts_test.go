package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
)

func TestSummaryAndAlertsEndpoints(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "secret-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)
	// Inject a healthy node and an offline node
	scheduler.InjectNodeStateForTest("JP-Relay", &model.NodeProbeResult{
		Name:        "JP-Relay",
		OriginalURL: "vless://uuid1@1.2.3.4:443#JP-Relay",
		Online:      true,
		Protocol:    "vless",
		LatencyMs:   45,
		StatusText:  "正常双向互通",
	})
	scheduler.InjectNodeStateForTest("US-Offline", &model.NodeProbeResult{
		Name:        "US-Offline",
		OriginalURL: "socks5://user:secretpass@5.6.7.8:1080#US-Offline",
		Online:      false,
		StatusText:  "【机器关机/离线】",
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "5.6.7.8",
			TargetPort:      1080,
			FaultType:       model.FaultMachineDown,
			StatusText:      "【机器关机/离线】",
			ActionType:      "CHECK_SERVER_AND_PROCESS",
			TroubleshootTip: "机器可能关机，请检查进程状态",
		},
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)

	// 1. 测试未携带 Token 时返回 401
	unauthPaths := []string{
		"/api/summary",
		"/api/v1/summary",
		"/api/alerts",
		"/api/v1/alerts",
	}
	for _, p := range unauthPaths {
		req := httptest.NewRequest("GET", p, nil)
		resp, err := server.app.Test(req, 2000)
		if err != nil {
			t.Fatalf("request %s failed: %v", p, err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated %s, got %d", p, resp.StatusCode)
		}
	}

	// 2. 测试携带 Token 时 /api/summary 与 /api/v1/summary 返回 200 及正确结构
	summaryPaths := []string{"/api/summary", "/api/v1/summary"}
	for _, p := range summaryPaths {
		req := httptest.NewRequest("GET", p, nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		resp, err := server.app.Test(req, 2000)
		if err != nil {
			t.Fatalf("request %s failed: %v", p, err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 for %s, got %d: %s", p, resp.StatusCode, string(body))
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode summary json failed: %v", err)
		}
		if _, ok := body["summary"]; !ok {
			t.Errorf("expected summary field in %s", p)
		}
		if hn, ok := body["healthy_nodes"]; !ok || hn == nil {
			t.Errorf("expected non-nil healthy_nodes slice in %s", p)
		}
		if na, ok := body["nodes_requiring_action"]; !ok || na == nil {
			t.Errorf("expected non-nil nodes_requiring_action slice in %s", p)
		}
	}

	// 3. 测试携带 Token 时 /api/alerts 与 /api/v1/alerts 返回 200 及告警列表
	alertPaths := []string{"/api/alerts", "/api/v1/alerts"}
	for _, p := range alertPaths {
		req := httptest.NewRequest("GET", p, nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		resp, err := server.app.Test(req, 2000)
		if err != nil {
			t.Fatalf("request %s failed: %v", p, err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 for %s, got %d: %s", p, resp.StatusCode, string(body))
		}

		var body struct {
			Total  int              `json:"total"`
			Alerts []map[string]any `json:"alerts"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode alerts json failed: %v", err)
		}
		if body.Alerts == nil {
			t.Errorf("expected non-nil Alerts slice in %s", p)
		}
		if body.Total != 1 {
			t.Errorf("expected 1 alert, got %d", body.Total)
		}
		if len(body.Alerts) != 1 {
			t.Fatalf("expected 1 alert item, got %d", len(body.Alerts))
		}

		item := body.Alerts[0]
		if item["name"] != "US-Offline" {
			t.Errorf("expected alert node name US-Offline, got %v", item["name"])
		}
		if item["fault_type"] != string(model.FaultMachineDown) {
			t.Errorf("expected fault_type %s, got %v", model.FaultMachineDown, item["fault_type"])
		}
		// 校验默认脱敏
		origURL, _ := item["original_url"].(string)
		if origURL == "" || origURL == "socks5://user:secretpass@5.6.7.8:1080#US-Offline" {
			t.Errorf("expected masked URL, got %v", origURL)
		}
	}

	// 4. 测试 reveal=true 时原样返回 URL
	req := httptest.NewRequest("GET", "/api/v1/alerts?reveal=true", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/v1/alerts?reveal=true failed: %v", err)
	}
	var body struct {
		Total  int              `json:"total"`
		Alerts []map[string]any `json:"alerts"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Alerts) > 0 {
		origURL, _ := body.Alerts[0]["original_url"].(string)
		if origURL != "socks5://user:secretpass@5.6.7.8:1080#US-Offline" {
			t.Errorf("expected unmasked URL with reveal=true, got %v", origURL)
		}
	}
}

func TestSummaryFaultClassification(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "secret-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)
	// 1. IP Blocked node
	scheduler.InjectNodeStateForTest("Node-IPBlocked", &model.NodeProbeResult{
		Name:        "Node-IPBlocked",
		OriginalURL: "vless://uuid@1.1.1.1:443#Node-IPBlocked",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:   "1.1.1.1",
			TargetPort: 443,
			FaultType:  model.FaultIPBlocked,
			StatusText: "【IP被墙】",
		},
	})
	// 2. Residential Direct Blocked (normal expected residential state)
	scheduler.InjectNodeStateForTest("Resi-Chained", &model.NodeProbeResult{
		Name:        "Resi-Chained",
		OriginalURL: "vless://uuid@2.2.2.2:443#Resi-Chained",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:   "2.2.2.2",
			TargetPort: 443,
			FaultType:  model.FaultResidentialDirectBlocked,
			StatusText: "【仅境外通】国内直连受阻",
		},
	})
	// 3. Residential Tunnel Offline (fault)
	scheduler.InjectNodeStateForTest("Resi-TunnelDown", &model.NodeProbeResult{
		Name:        "Resi-TunnelDown",
		OriginalURL: "vless://uuid@3.3.3.3:443#Resi-TunnelDown",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:   "3.3.3.3",
			TargetPort: 443,
			FaultType:  model.FaultResidentialTunnelOffline,
			StatusText: "【家宽隧道离线】",
		},
	})
	// 4. Machine Down node
	scheduler.InjectNodeStateForTest("Node-Down", &model.NodeProbeResult{
		Name:        "Node-Down",
		OriginalURL: "vless://uuid@4.4.4.4:443#Node-Down",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:   "4.4.4.4",
			TargetPort: 443,
			FaultType:  model.FaultMachineDown,
			StatusText: "【机器关机/离线】",
		},
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)

	req := httptest.NewRequest("GET", "/api/summary", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/summary failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode summary json failed: %v", err)
	}

	summary := res["summary"].(map[string]any)
	if summary["requires_healing_action"] != true {
		t.Errorf("expected requires_healing_action=true, got %v", summary["requires_healing_action"])
	}
	if int(summary["ip_blocked_nodes"].(float64)) != 1 {
		t.Errorf("expected ip_blocked_nodes=1, got %v", summary["ip_blocked_nodes"])
	}
	if int(summary["residential_chain_nodes"].(float64)) != 1 {
		t.Errorf("expected residential_chain_nodes=1, got %v", summary["residential_chain_nodes"])
	}
	if int(summary["offline_nodes"].(float64)) != 1 {
		t.Errorf("expected offline_nodes=1, got %v", summary["offline_nodes"])
	}
	if int(summary["residential_tunnel_offline_nodes"].(float64)) != 1 {
		t.Errorf("expected residential_tunnel_offline_nodes=1, got %v", summary["residential_tunnel_offline_nodes"])
	}
}

func TestSummaryResidentialTunnelOfflineOnly(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "secret-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)
	scheduler.InjectNodeStateForTest("LZ-US-Verizon-家宽", &model.NodeProbeResult{
		Name:        "LZ-US-Verizon-家宽",
		OriginalURL: "vless://uuid@167.104.167.151:44012#LZ-US-Verizon-家宽",
		Online:      false,
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "167.104.167.151",
			TargetPort:      44012,
			FaultType:       model.FaultResidentialTunnelOffline,
			StatusText:      "【家庭端未就绪或断开】家宽反向隧道未建立或连接被拒",
			ActionType:      "CHECK_RESIDENTIAL_DEVICE",
			TroubleshootTip: "严禁更换端口，请检查家庭端设备反向连接状态！",
		},
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)

	req := httptest.NewRequest("GET", "/api/summary", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/summary failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode summary json failed: %v", err)
	}

	summary := res["summary"].(map[string]any)
	if summary["requires_healing_action"] != false {
		t.Errorf("expected requires_healing_action=false for purely residential tunnel offline, got %v", summary["requires_healing_action"])
	}
	if int(summary["residential_tunnel_offline_nodes"].(float64)) != 1 {
		t.Errorf("expected residential_tunnel_offline_nodes=1, got %v", summary["residential_tunnel_offline_nodes"])
	}
	if int(summary["offline_nodes"].(float64)) != 0 {
		t.Errorf("expected offline_nodes=0, got %v", summary["offline_nodes"])
	}
	instruction, _ := summary["ai_instruction"].(string)
	if instruction == "" || !strings.Contains(instruction, "严禁更换端口") {
		t.Errorf("expected prompt to guide waiting for reconnect without changing port, got %q", instruction)
	}
}

func TestSummaryNetworkCongestion(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "secret-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)
	scheduler.InjectNodeStateForTest("US-Congestion", &model.NodeProbeResult{
		Name:        "US-Congestion",
		OriginalURL: "vless://uuid@1.2.3.4:443#US-Congestion",
		Online:      false,
		StatusText:  "【网络拥塞超时】国内外TCP连通但应用层请求超时",
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "1.2.3.4",
			TargetPort:      443,
			FaultType:       model.FaultNetworkCongestion,
			StatusText:      "【网络拥塞超时】国内外TCP连通但应用层请求超时",
			ActionType:      "CHECK_NETWORK_ROUTING",
			TroubleshootTip: "【网络拥塞】国内外TCP均已握手连通，应用层握手超时",
		},
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)

	req := httptest.NewRequest("GET", "/api/summary", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/summary failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode summary json failed: %v", err)
	}

	summary := res["summary"].(map[string]any)
	if summary["requires_healing_action"] != false {
		t.Errorf("expected requires_healing_action=false for pure congestion, got %v", summary["requires_healing_action"])
	}
	if int(summary["congestion_nodes"].(float64)) != 1 {
		t.Errorf("expected congestion_nodes=1, got %v", summary["congestion_nodes"])
	}
	if int(summary["offline_nodes"].(float64)) != 0 {
		t.Errorf("expected offline_nodes=0, got %v", summary["offline_nodes"])
	}

	actions := res["nodes_requiring_action"].([]any)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action item, got %d", len(actions))
	}
	act := actions[0].(map[string]any)
	if act["fault_type"] != string(model.FaultNetworkCongestion) {
		t.Errorf("expected fault_type=%s, got %v", model.FaultNetworkCongestion, act["fault_type"])
	}
}

func TestSummaryProbeEngineErrorClassification(t *testing.T) {
	ResetAuthTracker()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "secret-token"
	cfg.Monitor.Enabled = true

	scheduler := monitor.NewScheduler(cfg, nil, nil, nil)
	scheduler.InjectNodeStateForTest("US-Sandbox-Err", &model.NodeProbeResult{
		Name:          "US-Sandbox-Err",
		OriginalURL:   "vless://uuid@38.175.222.44:36564#US-Sandbox-Err",
		Online:        false,
		OfflineReason: "probe_engine_error",
		StatusText:    "【引擎异常】本地沙箱端口连接被拒",
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "38.175.222.44",
			TargetPort:      36564,
			FaultType:       model.FaultProbeEngineError,
			StatusText:      "【引擎异常】本地沙箱端口连接被拒",
			ActionType:      "RETRY_PROBE",
			TroubleshootTip: "local sandbox connection refused",
		},
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, scheduler, nil)

	req := httptest.NewRequest("GET", "/api/summary", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := server.app.Test(req, 2000)
	if err != nil {
		t.Fatalf("request /api/summary failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode summary json failed: %v", err)
	}

	summary := res["summary"].(map[string]any)
	if summary["requires_healing_action"] != false {
		t.Errorf("expected requires_healing_action=false for engine error, got %v", summary["requires_healing_action"])
	}
	if int(summary["engine_error_nodes"].(float64)) != 1 {
		t.Errorf("expected engine_error_nodes=1, got %v", summary["engine_error_nodes"])
	}
	if int(summary["offline_nodes"].(float64)) != 0 {
		t.Errorf("expected offline_nodes=0, got %v", summary["offline_nodes"])
	}

	actions := res["nodes_requiring_action"].([]any)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action item, got %d", len(actions))
	}
	act := actions[0].(map[string]any)
	if act["fault_type"] != string(model.FaultProbeEngineError) {
		t.Errorf("expected fault_type=%s, got %v", model.FaultProbeEngineError, act["fault_type"])
	}
	if act["server_ip"] != "38.175.222.44" {
		t.Errorf("expected server_ip=38.175.222.44, got %v", act["server_ip"])
	}
	if int(act["server_port"].(float64)) != 36564 {
		t.Errorf("expected server_port=36564, got %v", act["server_port"])
	}
}
