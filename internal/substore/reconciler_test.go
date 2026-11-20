package substore

import (
	"context"
	"net/http"
	"net/http/httptest"
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
