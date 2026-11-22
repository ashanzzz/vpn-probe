package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/substore"
)

func TestSnapshotEndpoints(t *testing.T) {
	ResetAuthTracker()

	// 1. Mock Sub-Store
	mockClash := `proxies:
  - name: "HK-01"
    type: ss
    server: 1.1.1.1
    port: 8388
`
	subServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/file/Q" {
			w.Header().Set("Content-Type", "text/yaml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(mockClash))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer subServer.Close()

	cfg := config.DefaultConfig()
	cfg.Server.AuthToken = "test-token"
	cfg.SubStore.Enabled = true
	cfg.SubStore.URL = subServer.URL

	sc := substore.NewClient(&cfg.SubStore)
	server := NewAPIServer(cfg, nil, nil, sc, nil, nil, nil)
	// 使用隔离的临时快照目录，避免受全局磁盘快照干扰
	isolatedSM := substore.NewSnapshotManager(sc, t.TempDir())
	server.SetSnapshotManager(isolatedSM)

	// 2. 测试查询快照状态 (空状态)
	req := httptest.NewRequest("GET", "/api/sub/snapshot/status", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := server.app.Test(req)
	if err != nil {
		t.Fatalf("GET /api/sub/snapshot/status failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// 3. 测试通过 Query Token 拉取快照 /api/sub/file/Q?token=test-token
	req = httptest.NewRequest("GET", "/api/sub/file/Q?token=test-token", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	resp, err = server.app.Test(req, 10000)
	if err != nil {
		t.Fatalf("GET /api/sub/file/Q failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != mockClash {
		t.Fatalf("expected mock clash body, got: %s", string(body))
	}

	// 验证关键响应头
	if ct := resp.Header.Get("Content-Type"); ct != "text/yaml; charset=utf-8" {
		t.Fatalf("expected text/yaml content-type, got: %s", ct)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header to be present")
	}

	// 4. 测试 ETag 304 极速协商 (精确 ETag、弱 ETag、通配符 * 及多重 ETag 列表)
	req = httptest.NewRequest("GET", "/api/sub/file/Q?token=test-token", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("If-None-Match", etag)
	resp, err = server.app.Test(req)
	if err != nil {
		t.Fatalf("ETag test failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", resp.StatusCode)
	}

	// 4.1 通配符 * 协商 304
	req = httptest.NewRequest("GET", "/api/sub/file/Q?token=test-token", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("If-None-Match", "*")
	resp, _ = server.app.Test(req)
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 for wildcard ETag, got %d", resp.StatusCode)
	}

	// 4.2 多 ETag 列表协商 304
	req = httptest.NewRequest("GET", "/api/sub/file/Q?token=test-token", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("If-None-Match", "\"old-etag-1\", "+etag+", \"old-etag-2\"")
	resp, _ = server.app.Test(req)
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 for multi-ETag match, got %d", resp.StatusCode)
	}

	// 5. 测试无需 Token 时的鉴权拦截 (防未经授权窃取)
	req = httptest.NewRequest("GET", "/api/sub/file/Q", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	resp, _ = server.app.Test(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for request without token, got %d", resp.StatusCode)
	}
}
