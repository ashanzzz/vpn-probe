package substore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{
			name: "Username with empty password falls back to URL path token",
			cfg: &config.SubStoreConfig{
				URL:      "https://free.1992004.xyz/Woo@@@204317",
				Username: "ashan",
				Password: "",
				Token:    "",
			},
			expectAuth: "Basic YXNoYW46V29vQEBAMjA0MzE3",
			expectKey:  "Woo@@@204317",
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

	// 验证 getHTTPClient 并发读安全与回退默认客户端
	if c.getHTTPClient() != customClient {
		t.Errorf("getHTTPClient did not return custom client")
	}
	c.SetHTTPClient(nil)
	if c.getHTTPClient() == nil {
		t.Errorf("getHTTPClient should return fallback client when nil")
	}
	c.SetHTTPClient(customClient)

	sm := NewSnapshotManager(c, t.TempDir())
	sm.SetHTTPClient(customClient)

	sm.mu.RLock()
	if sm.httpClient != customClient {
		t.Errorf("SetHTTPClient did not set custom client on SnapshotManager")
	}
	sm.mu.RUnlock()
}

func TestClient_SyncThreeTier_EmptyNodes(t *testing.T) {
	patchedSubs := make(map[string]string)
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/sub/") {
			subName := strings.TrimPrefix(r.URL.Path, "/api/sub/")
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			if c, ok := body["content"].(string); ok {
				patchedSubs[subName] = c
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/collection/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     ts.URL,
	}
	c := NewClient(cfg)
	c.SetHTTPClient(ts.Client())

	payload := ThreeTierPayload{
		TransitDirectNodes: []string{},
		ResidentialNodes:   []string{},
		ChainNodes:         []string{},
	}

	err := c.SyncThreeTier(context.Background(), payload)
	if err != nil {
		t.Fatalf("SyncThreeTier failed: %v", err)
	}

	expectedSubs := []string{"ashan-transit-direct", "ashan-resi-direct", "ashan-chain"}
	mu.Lock()
	defer mu.Unlock()
	for _, sub := range expectedSubs {
		content, ok := patchedSubs[sub]
		if !ok {
			t.Errorf("expected subscription %s to be patched, but was not", sub)
			continue
		}
		if content != "" {
			t.Errorf("expected empty string content for %s, got %q", sub, content)
		}
	}
}

func TestClient_DeleteSubscription_Auth(t *testing.T) {
	var receivedAuth string
	var receivedAPIKey string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedAPIKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.SubStoreConfig{
		Enabled:  true,
		URL:      ts.URL,
		Username: "admin",
		Password: "secretpassword",
		Token:    "admin-token",
	}
	c := NewClient(cfg)
	c.SetHTTPClient(ts.Client())

	err := c.DeleteSubscription(context.Background(), "test-sub")
	if err != nil {
		t.Fatalf("DeleteSubscription failed: %v", err)
	}

	if !strings.HasPrefix(receivedAuth, "Basic ") {
		t.Errorf("expected Basic auth header, got %q", receivedAuth)
	}
	if receivedAPIKey != "admin-token" {
		t.Errorf("expected x-api-key header 'admin-token', got %q", receivedAPIKey)
	}
}

func TestClient_PurgeLegacyThreeTierSubs(t *testing.T) {
	deletedSubs := make(map[string]bool)
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodDelete {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 4 {
				deletedSubs[parts[len(parts)-1]] = true
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cfg := &config.SubStoreConfig{
		Enabled:  true,
		URL:      ts.URL,
		Username: "ashan",
		Password: "password",
	}
	c := NewClient(cfg)
	c.SetHTTPClient(ts.Client())

	count, err := c.PurgeLegacyThreeTierSubs(context.Background())
	if err != nil {
		t.Fatalf("PurgeLegacyThreeTierSubs failed: %v", err)
	}
	if count != len(LegacyThreeTierSubNames) {
		t.Errorf("expected %d purged subs, got %d", len(LegacyThreeTierSubNames), count)
	}
	for _, expected := range LegacyThreeTierSubNames {
		if !deletedSubs[expected] {
			t.Errorf("expected sub %q to be deleted, but it was not", expected)
		}
	}
}

func TestClient_PurgeLegacyThreeTierSubs_ResourceNotFoundOn500(t *testing.T) {
	// 模拟 Sub-Store 在资源不存在时返回 500 内部错误，但附带 ResourceNotFoundError
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"failed","error":{"code":"RESOURCE_NOT_FOUND","type":"ResourceNotFoundError","message":"Subscription does not exist!"}}`))
	}))
	defer ts.Close()

	cfg := &config.SubStoreConfig{
		Enabled:  true,
		URL:      ts.URL,
		Username: "ashan",
		Password: "password",
	}
	c := NewClient(cfg)
	c.SetHTTPClient(ts.Client())

	count, err := c.PurgeLegacyThreeTierSubs(context.Background())
	if err != nil {
		t.Fatalf("expected PurgeLegacyThreeTierSubs to succeed idempotently on 500 ResourceNotFoundError, got: %v", err)
	}
	if count != len(LegacyThreeTierSubNames) {
		t.Errorf("expected count %d, got %d", len(LegacyThreeTierSubNames), count)
	}
}

func TestResolveFallbackURL(t *testing.T) {
	cases := []struct {
		name     string
		cfg      *config.SubStoreConfig
		expected string
	}{
		{
			name:     "nil config",
			cfg:      nil,
			expected: "http://156.231.113.91:3001",
		},
		{
			name: "token explicitly set",
			cfg: &config.SubStoreConfig{
				URL:   "https://free.1992004.xyz",
				Token: "mysecret",
			},
			expected: "http://156.231.113.91:3001/mysecret",
		},
		{
			name: "token with leading slash",
			cfg: &config.SubStoreConfig{
				URL:   "https://free.1992004.xyz",
				Token: "/mysecret/",
			},
			expected: "http://156.231.113.91:3001/mysecret",
		},
		{
			name: "token empty but url path contains token (production pattern)",
			cfg: &config.SubStoreConfig{
				URL:   "https://free.1992004.xyz/Woo@@@204317",
				Token: "",
			},
			expected: "http://156.231.113.91:3001/Woo@@@204317",
		},
		{
			name: "token empty and url path is root",
			cfg: &config.SubStoreConfig{
				URL:   "https://free.1992004.xyz/",
				Token: "",
			},
			expected: "http://156.231.113.91:3001",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveFallbackURL(tc.cfg)
			if got != tc.expected {
				t.Errorf("ResolveFallbackURL() = %q; expected %q", got, tc.expected)
			}
		})
	}
}

func TestClient_SyncFiveGroups_SuccessAndErrorPropagation(t *testing.T) {
	// 1. 成功分支：所有订阅和合集均成功更新
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer successServer.Close()

	cfgSuccess := &config.SubStoreConfig{
		Enabled: true,
		URL:     successServer.URL,
	}
	cSuccess := NewClient(cfgSuccess)
	cSuccess.SetHTTPClient(successServer.Client())

	payload := model.FiveGroupPayload{
		TransitNodes: []string{"vless://transit-1"},
		USResiNodes:  []string{"vless://us-1"},
		JPResiNodes:  []string{"vless://jp-1"},
		OtherNodes:   []string{"vless://other-1"},
	}

	err := cSuccess.SyncFiveGroups(context.Background(), payload)
	if err != nil {
		t.Fatalf("expected SyncFiveGroups to succeed, got error: %v", err)
	}

	// 2. 失败分支：部分订阅更新返回 500 错误，验证错误被正确收集与传播，而非静默吞咽
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "ashan-us-resi") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"database disk full"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer failServer.Close()

	cfgFail := &config.SubStoreConfig{
		Enabled: true,
		URL:     failServer.URL,
	}
	cFail := NewClient(cfgFail)
	cFail.SetHTTPClient(failServer.Client())

	failErr := cFail.SyncFiveGroups(context.Background(), payload)
	if failErr == nil {
		t.Fatalf("expected SyncFiveGroups to return error on failed subscription patch, got nil")
	}
	if !strings.Contains(failErr.Error(), "ashan-us-resi") {
		t.Errorf("expected error message to mention 'ashan-us-resi', got: %v", failErr)
	}
}

func TestClient_UpdateSubscription_RetryOn5xx(t *testing.T) {
	// 模拟第一次返回 502 Bad Gateway，第二次返回 200 OK
	attempts := 0
	var mu sync.Mutex

	retryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "test-retry-sub") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()

		if current == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"bad gateway"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer retryServer.Close()

	cfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     retryServer.URL,
	}
	c := NewClient(cfg)
	c.SetHTTPClient(retryServer.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.UpdateSubscriptionNodes(ctx, "test-retry-sub", []string{"vless://node1"})
	if err != nil {
		t.Fatalf("expected retry on 502 to succeed on second attempt, got: %v", err)
	}

	mu.Lock()
	totalAttempts := attempts
	mu.Unlock()

	if totalAttempts != 2 {
		t.Errorf("expected exactly 2 attempts, got %d", totalAttempts)
	}
}
