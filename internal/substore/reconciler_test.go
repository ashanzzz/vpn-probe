package substore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vpn-probe/internal/config"
)

func TestAuditTopology(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/subs":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan's vps","displayName":"Ashan VPS","source":"local","content":"vless://1\nvless://2"},
				{"name":"Q","displayName":"Q VPS","source":"local","content":"vless://3"},
				{"name":"蜂窝云","displayName":"蜂窝云","source":"remote","url":"https://example.com"}
			]}`))
		case "/api/collections":
			w.WriteHeader(http.StatusOK)
			// 模拟错配：ashan-纯净 混入 蜂窝云，Q-汇总 漏掉 Q
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-纯净","displayName":"Ashan 纯自建VPS","subscriptions":["ashan's vps","蜂窝云"]},
				{"name":"Q-汇总","displayName":"Q 组合订阅","subscriptions":["蜂窝云"]}
			]}`))
		case "/api/files":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan","displayName":"ashan","sourceName":"ashan-纯净"},
				{"name":"Q","displayName":"Q","sourceName":"Q-汇总"}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	cfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     mockServer.URL,
	}
	client := NewClient(cfg)

	audit, err := client.AuditTopology(context.Background())
	if err != nil {
		t.Fatalf("AuditTopology failed: %v", err)
	}

	if audit.Healthy {
		t.Errorf("Expected topology to be unhealthy due to discrepancies, got healthy")
	}

	if len(audit.Discrepancies) == 0 {
		t.Errorf("Expected discrepancies to be detected, got 0")
	}

	hasAshanIssue := false
	hasQIssue := false
	hasOrphanIssue := false

	for _, d := range audit.Discrepancies {
		if contains(d, "ashan-纯净") {
			hasAshanIssue = true
		}
		if contains(d, "Q-汇总") {
			hasQIssue = true
		}
		if contains(d, "孤儿状态") {
			hasOrphanIssue = true
		}
	}

	if !hasAshanIssue {
		t.Errorf("Expected discrepancy for ashan-纯净 pollution, none found")
	}
	if !hasQIssue {
		t.Errorf("Expected discrepancy for Q-汇总 missing Q, none found")
	}
	if !hasOrphanIssue {
		t.Errorf("Expected discrepancy for orphaned Q subscription, none found")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestReconcileTopology_FileBindings(t *testing.T) {
	updatedFiles := make(map[string]map[string]any)
	updatedCollections := make(map[string]map[string]any)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/subs":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-transit","displayName":"Transit","source":"local","content":"vless://1"},
				{"name":"ashan-us-resi","displayName":"US Resi","source":"local","content":"socks5://2"},
				{"name":"ashan-jp-resi","displayName":"JP Resi","source":"local","content":"socks5://3"},
				{"name":"ashan-other","displayName":"Other","source":"local","content":"socks5://4"},
				{"name":"ashan-china-leak","displayName":"Leak","source":"local","content":"vless://5"},
				{"name":"蜂窝云","displayName":"蜂窝云","source":"remote","url":"https://example.com"}
			]}`))
		case r.URL.Path == "/api/collections":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-纯净","displayName":"Ashan 纯自建专线","subscriptions":["ashan-transit","ashan-us-resi","ashan-jp-resi","ashan-other"]},
				{"name":"Q-汇总","displayName":"Q 组合订阅","subscriptions":["ashan-transit","ashan-us-resi","ashan-jp-resi","ashan-other","ashan-china-leak","蜂窝云"]}
			]}`))
		case r.URL.Path == "/api/files":
			w.WriteHeader(http.StatusOK)
			// 模拟文件错配：ashan/Ashan 错误绑定了 Q-汇总，q 错误绑定了 ashan-纯净
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan","displayName":"ashan","sourceName":"Q-汇总"},
				{"name":"Ashan","displayName":"Ashan","sourceName":"Q-汇总"},
				{"name":"Q","displayName":"Q","sourceName":"Q-汇总"},
				{"name":"q","displayName":"q","sourceName":"ashan-纯净"}
			]}`))
		case strings.HasPrefix(r.URL.Path, "/api/file/"):
			fName := strings.TrimPrefix(r.URL.Path, "/api/file/")
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			updatedFiles[fName] = payload
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
		case strings.HasPrefix(r.URL.Path, "/api/collection/"):
			cName := strings.TrimPrefix(r.URL.Path, "/api/collection/")
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			updatedCollections[cName] = payload
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	cfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     mockServer.URL,
	}
	client := NewClient(cfg)

	audit, err := client.ReconcileTopology(context.Background())
	if err != nil {
		t.Fatalf("ReconcileTopology failed: %v", err)
	}

	// 验证 ashan 和 Ashan 是否被自动修复并绑定回 ashan-纯净
	if updatedFiles["ashan"]["sourceName"] != "ashan-纯净" {
		t.Errorf("Expected ashan to be reconciled to ashan-纯净, got %v", updatedFiles["ashan"])
	}
	if updatedFiles["Ashan"]["sourceName"] != "ashan-纯净" {
		t.Errorf("Expected Ashan to be reconciled to ashan-纯净, got %v", updatedFiles["Ashan"])
	}
	// 验证 q 是否被自动修复并绑定回 Q-汇总
	if updatedFiles["q"]["sourceName"] != "Q-汇总" {
		t.Errorf("Expected q to be reconciled to Q-汇总, got %v", updatedFiles["q"])
	}

	if len(audit.ActionsTaken) < 3 {
		t.Errorf("Expected at least 3 actions taken, got %d: %v", len(audit.ActionsTaken), audit.ActionsTaken)
	}
}

func TestAuditTopology_FiveGroupQSummary(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/subs":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-transit","displayName":"Transit","source":"local","content":"vless://1"},
				{"name":"ashan-us-resi","displayName":"US Resi","source":"local","content":"socks5://2"},
				{"name":"ashan-jp-resi","displayName":"JP Resi","source":"local","content":"socks5://3"},
				{"name":"ashan-other","displayName":"Other","source":"local","content":"socks5://4"},
				{"name":"ashan-china-leak","displayName":"Leak","source":"local","content":"vless://5"},
				{"name":"蜂窝云","displayName":"蜂窝云","source":"remote","url":"https://example.com"}
			]}`))
		case "/api/collections":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-纯净","displayName":"Ashan 纯自建专线","subscriptions":["ashan-transit","ashan-us-resi","ashan-jp-resi","ashan-other"]},
				{"name":"Q-汇总","displayName":"Q 组合订阅","subscriptions":["ashan-transit","ashan-us-resi","ashan-jp-resi","ashan-other","ashan-china-leak","蜂窝云"]}
			]}`))
		case "/api/files":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan","displayName":"ashan","sourceName":"ashan-纯净"},
				{"name":"Ashan","displayName":"Ashan","sourceName":"ashan-纯净"},
				{"name":"Q","displayName":"Q","sourceName":"Q-汇总"},
				{"name":"q","displayName":"q","sourceName":"Q-汇总"}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	cfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     mockServer.URL,
	}
	client := NewClient(cfg)

	audit, err := client.AuditTopology(context.Background())
	if err != nil {
		t.Fatalf("AuditTopology failed: %v", err)
	}

	if !audit.Healthy {
		t.Errorf("Expected topology with five-group Q-汇总 to be healthy, got discrepancies: %v", audit.Discrepancies)
	}
	if len(audit.Discrepancies) != 0 {
		t.Errorf("Expected 0 discrepancies, got %d: %v", len(audit.Discrepancies), audit.Discrepancies)
	}
}

func TestDeleteItem_IdempotentOn404(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/api/sub/existing-sub":
			w.WriteHeader(http.StatusOK)
		case "/api/sub/non-existing-sub":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status":"error","message":"subscription not found"}`))
		case "/api/sub/broken-sub":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"status":"error","message":"database crash"}`))
		case "/api/sub/notfound-500-sub":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"status":"failed","error":{"code":"RESOURCE_NOT_FOUND","type":"ResourceNotFoundError","message":"Subscription does not exist!"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	client := NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     mockServer.URL,
	})

	// 1. 200 OK should succeed
	if err := client.DeleteItem(context.Background(), "subs", "existing-sub"); err != nil {
		t.Fatalf("expected nil for 200 OK delete, got: %v", err)
	}

	// 2. 404 Not Found should be treated as success (idempotent delete)
	if err := client.DeleteItem(context.Background(), "subs", "non-existing-sub"); err != nil {
		t.Fatalf("expected nil (idempotent) for 404 Not Found delete, got: %v", err)
	}

	// 3. 500 with ResourceNotFoundError should also be treated as success (idempotent delete)
	if err := client.DeleteItem(context.Background(), "subs", "notfound-500-sub"); err != nil {
		t.Fatalf("expected nil (idempotent) for 500 ResourceNotFoundError delete, got: %v", err)
	}

	// 4. 500 Internal Server Error with general crash should still return an error
	if err := client.DeleteItem(context.Background(), "subs", "broken-sub"); err == nil {
		t.Fatalf("expected error for 500 error, got nil")
	}
}
