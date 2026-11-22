package substore

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

func TestClient_ConcurrentConfigUpdate(t *testing.T) {
	initialCfg := &config.SubStoreConfig{
		Enabled:         true,
		URL:             "http://initial.example.com",
		GroupSelfOnline: "InitialGroup",
		Token:           "init-token",
	}

	client := NewClient(initialCfg)

	var wg sync.WaitGroup
	workers := 20
	iterations := 200

	// 1. 并发读者
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cfg := client.GetConfig()
				if cfg == nil {
					t.Errorf("worker %d got nil config", id)
					return
				}
				_ = client.getSubscriptionNameForCategory(model.CategorySelfOnline)
				_ = cfg.URL
			}
		}(i)
	}

	// 2. 并发写者 (频繁动态热重载配置)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				newCfg := &config.SubStoreConfig{
					Enabled:         true,
					URL:             fmt.Sprintf("http://update-%d-%d.example.com", id, j),
					GroupSelfOnline: fmt.Sprintf("Group-%d-%d", id, j),
					Token:           fmt.Sprintf("token-%d-%d", id, j),
				}
				client.UpdateConfig(newCfg)
			}
		}(i)
	}

	wg.Wait()

	finalCfg := client.GetConfig()
	if finalCfg == nil {
		t.Fatal("expected non-nil final config")
	}
}

func TestClient_SetAuth(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *config.SubStoreConfig
		expectAuth    string
		expectKey     string
		expectNoAuth  bool
	}{
		{
			name: "Basic Auth only",
			cfg: &config.SubStoreConfig{
				Username: "ashan",
				Password: "password123",
			},
			expectAuth: "Basic YXNoYW46cGFzc3dvcmQxMjM=",
			expectKey:  "",
		},
		{
			name: "Token only",
			cfg: &config.SubStoreConfig{
				Token: "my-secret-token",
			},
			expectAuth: "Bearer my-secret-token",
			expectKey:  "my-secret-token",
		},
		{
			name: "Both Basic Auth and Token",
			cfg: &config.SubStoreConfig{
				Username: "ashan",
				Password: "password123",
				Token:    "my-secret-token",
			},
			expectAuth: "Basic YXNoYW46cGFzc3dvcmQxMjM=",
			expectKey:  "my-secret-token",
		},
		{
			name: "Username with empty password but valid token uses token",
			cfg: &config.SubStoreConfig{
				Username: "ashan",
				Password: "",
				Token:    "fallback-token",
			},
			expectAuth: "Bearer fallback-token",
			expectKey:  "fallback-token",
		},
		{
			name: "Empty credentials",
			cfg: &config.SubStoreConfig{
				Username: "",
				Password: "",
				Token:    "",
			},
			expectNoAuth: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(tc.cfg)
			req, _ := http.NewRequest("GET", "http://example.com/api", nil)
			c.setAuth(req)

			authHeader := req.Header.Get("Authorization")
			apiKeyHeader := req.Header.Get("x-api-key")

			if tc.expectNoAuth {
				if authHeader != "" || apiKeyHeader != "" {
					t.Errorf("expected no auth headers, got auth=%q key=%q", authHeader, apiKeyHeader)
				}
				return
			}

			if authHeader != tc.expectAuth {
				t.Errorf("expected Authorization %q, got %q", tc.expectAuth, authHeader)
			}
			if apiKeyHeader != tc.expectKey {
				t.Errorf("expected x-api-key %q, got %q", tc.expectKey, apiKeyHeader)
			}
		})
	}
}

func TestIsLocalOrTestEndpoint(t *testing.T) {
	localCases := []string{
		"http://127.0.0.1:8080",
		"http://127.0.0.1:3000/api",
		"http://localhost:3001",
		"http://[::1]:3001",
		"http://test.example.com",
		"https://example.com/api/file",
	}
	for _, u := range localCases {
		if !isLocalOrTestEndpoint(u) {
			t.Errorf("expected %s to be recognized as local/test endpoint", u)
		}
	}

	remoteCases := []string{
		"https://free.1992004.xyz",
		"https://sub.ashanzzz.com/share",
		"http://156.231.113.91:3001",
	}
	for _, u := range remoteCases {
		if isLocalOrTestEndpoint(u) {
			t.Errorf("expected %s to be recognized as remote non-test endpoint", u)
		}
	}
}

func TestClientAndSnapshot_SetHTTPClient(t *testing.T) {
	c := NewClient(&config.SubStoreConfig{Enabled: true})
	customClient := &http.Client{Timeout: 5 * time.Second}
	c.SetHTTPClient(customClient)

	c.mu.RLock()
	if c.httpClient != customClient {
		t.Errorf("SetHTTPClient did not set custom client on Client")
	}
	c.mu.RUnlock()

	sm := NewSnapshotManager(c, t.TempDir())
	sm.SetHTTPClient(customClient)

	sm.mu.RLock()
	if sm.httpClient != customClient {
		t.Errorf("SetHTTPClient did not set custom client on SnapshotManager")
	}
	sm.mu.RUnlock()
}
