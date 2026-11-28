package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/speedtest"
)

func TestSpeedtestConfigPersistenceAndVerification(t *testing.T) {
	ResetAuthTracker()
	tmpDir := t.TempDir()
	persistPath := filepath.Join(tmpDir, "speedtest_config.json")
	t.Setenv("VPN_PROBE_SPEEDTEST_CONFIG", persistPath)
	speedtest.SetSelfHostedSpeedURLsExact("", "")
	t.Cleanup(func() { speedtest.SetSelfHostedSpeedURLsExact("", "") })

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("speedtest-payload-ok"))
	}))
	defer okSrv.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{AuthToken: "admin-token-123"},
	}
	server := NewAPIServer(cfg, nil, nil, nil, nil, nil, nil)

	// 1. 拒绝公网测速网站
	badBody, _ := json.Marshal(map[string]any{
		"self_hosted_url_10m": "https://speed.cloudflare.com/__down",
	})
	badReq := httptest.NewRequest(http.MethodPost, "/api/speedtest/config", bytes.NewReader(badBody))
	badReq.Header.Set("Authorization", "Bearer admin-token-123")
	badReq.Header.Set("Content-Type", "application/json")
	badResp, err := server.app.Test(badReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for public speedtest URL, got %d", badResp.StatusCode)
	}

	// 2. 预检 + 保存自建 URL 并持久化
	goodBody, _ := json.Marshal(map[string]any{
		"self_hosted_url_10m": okSrv.URL + "/10m.bin",
		"self_hosted_url_30m": okSrv.URL + "/30m.bin",
	})
	goodReq := httptest.NewRequest(http.MethodPost, "/api/speedtest/config", bytes.NewReader(goodBody))
	goodReq.Header.Set("Authorization", "Bearer admin-token-123")
	goodReq.Header.Set("Content-Type", "application/json")
	goodResp, err := server.app.Test(goodReq)
	if err != nil {
		t.Fatalf("save request failed: %v", err)
	}
	if goodResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 on save, got %d", goodResp.StatusCode)
	}

	// 3. 模拟重启清空内存后重建 APIServer，确认从 speedtest_config.json 自动恢复
	speedtest.SetSelfHostedSpeedURLsExact("", "")
	cfgRestart := &config.Config{
		Server: config.ServerConfig{AuthToken: "admin-token-123"},
	}
	_ = NewAPIServer(cfgRestart, nil, nil, nil, nil, nil, nil)
	got10, got30 := speedtest.GetSelfHostedSpeedURLs()
	if got10 != okSrv.URL+"/10m.bin" || got30 != okSrv.URL+"/30m.bin" {
		t.Fatalf("expected restored URLs after restart, got 10m=%q 30m=%q", got10, got30)
	}
}
