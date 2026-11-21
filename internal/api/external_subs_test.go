package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/substore"
)

func TestExternalSubsAPI(t *testing.T) {
	tmpDir := t.TempDir()
	extFile := filepath.Join(tmpDir, "external_sources.json")
	_ = os.WriteFile(extFile, []byte(`{"sources": ["蜂窝云"]}`), 0644)

	// Mock SubStore HTTP Server
	mockSubStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/subs" && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": []map[string]any{
					{
						"name":        "蜂窝云",
						"displayName": "蜂窝云",
						"source":      "remote",
						"url":         "https://fwcloud.example.com",
					},
				},
			})
			return
		}
		if r.URL.Path == "/api/subs" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
			return
		}
		if r.Method == http.MethodPatch {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
			return
		}
		if r.Method == http.MethodDelete {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
			return
		}
		if r.URL.Path == "/api/collections" {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
	}))
	defer mockSubStore.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-token",
		},
		SubStore: config.SubStoreConfig{
			Enabled:         true,
			URL:             mockSubStore.URL,
			ExternalSources: []string{"蜂窝云"},
		},
	}

	subClient := substore.NewClient(&cfg.SubStore)
	server := NewAPIServer(cfg, nil, nil, subClient, nil, nil, nil)

	// 1. 测试 GET /api/external/subs (Issue #19: 丰富元数据与 details)
	getReq := httptest.NewRequest(http.MethodGet, "/api/external/subs", nil)
	getReq.Header.Set("Authorization", "Bearer test-token")
	getResp, err := server.app.Test(getReq)
	if err != nil {
		t.Fatalf("GET /api/external/subs failed: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}

	var getResult struct {
		Status  string   `json:"status"`
		Count   int      `json:"count"`
		Sources []string `json:"sources"`
		Details []struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			URL         string `json:"url"`
			SourceType  string `json:"source_type"`
		} `json:"details"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&getResult); err != nil {
		t.Fatalf("decode GET response failed: %v", err)
	}
	if getResult.Count < 1 || getResult.Sources[0] != "蜂窝云" {
		t.Fatalf("expected sources to contain '蜂窝云', got %v", getResult.Sources)
	}
	if len(getResult.Details) < 1 || getResult.Details[0].Name != "蜂窝云" {
		t.Fatalf("expected details to contain '蜂窝云', got %v", getResult.Details)
	}

	// 2. 测试 POST /api/external/subs (Issue #19: URL 级一键纳管)
	postPayload := map[string]any{
		"name":         "airport_alpha",
		"display_name": "主力极速机场-A",
		"url":          "https://api.airport-a.com/link/token?clash=1",
	}
	bodyBytes, _ := json.Marshal(postPayload)
	postReq := httptest.NewRequest(http.MethodPost, "/api/external/subs", bytes.NewReader(bodyBytes))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Authorization", "Bearer test-token")

	postResp, err := server.app.Test(postReq)
	if err != nil {
		t.Fatalf("POST /api/external/subs failed: %v", err)
	}
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", postResp.StatusCode)
	}

	var postResult struct {
		Status  string   `json:"status"`
		Name    string   `json:"name"`
		URL     string   `json:"url"`
		Sources []string `json:"sources"`
	}
	if err := json.NewDecoder(postResp.Body).Decode(&postResult); err != nil {
		t.Fatalf("decode POST response failed: %v", err)
	}
	if postResult.Name != "airport_alpha" {
		t.Fatalf("expected created name 'airport_alpha', got %s", postResult.Name)
	}

	// 3. 测试 DELETE /api/external/subs/:name?purge=true
	delReq := httptest.NewRequest(http.MethodDelete, "/api/external/subs/airport_alpha?purge=true", nil)
	delReq.Header.Set("Authorization", "Bearer test-token")

	delResp, err := server.app.Test(delReq)
	if err != nil {
		t.Fatalf("DELETE /api/external/subs failed: %v", err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", delResp.StatusCode)
	}

	var delResult struct {
		Status             string   `json:"status"`
		Sources            []string `json:"sources"`
		PurgedFromSubStore bool     `json:"purged_from_substore"`
	}
	if err := json.NewDecoder(delResp.Body).Decode(&delResult); err != nil {
		t.Fatalf("decode DELETE response failed: %v", err)
	}
	if !delResult.PurgedFromSubStore {
		t.Fatalf("expected PurgedFromSubStore=true")
	}
	for _, s := range delResult.Sources {
		if s == "airport_alpha" {
			t.Fatalf("expected 'airport_alpha' to be removed from sources: %v", delResult.Sources)
		}
	}
}
