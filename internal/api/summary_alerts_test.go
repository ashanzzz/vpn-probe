package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		if _, ok := body["healthy_nodes"]; !ok {
			t.Errorf("expected healthy_nodes field in %s", p)
		}
		if _, ok := body["nodes_requiring_action"]; !ok {
			t.Errorf("expected nodes_requiring_action field in %s", p)
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
