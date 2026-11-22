package vpsmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestGitHubChecker_GetLatestRelease(t *testing.T) {
	statusCode := http.StatusOK
	callCount := 0
	lastAuthHeader := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		lastAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(statusCode)
		if statusCode == http.StatusOK {
			resp := ghReleaseResponse{
				TagName:     "v1.12.0",
				Name:        "Release v1.12.0",
				PublishedAt: "2026-11-20T10:00:00Z",
				HTMLURL:     "https://github.com/SagerNet/sing-box/releases/tag/v1.12.0",
			}
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			_, _ = w.Write([]byte(`{"message": "API rate limit exceeded"}`))
		}
	}))
	defer server.Close()

	os.Setenv("GITHUB_PAT", "ghp_test_mock_token_123")
	defer os.Unsetenv("GITHUB_PAT")

	checker := NewGitHubChecker()
	checker.SetBaseURLForTest(server.URL)

	ctx := context.Background()

	// 1. 成功拉取与 Token 头注入校验
	info, err := checker.GetLatestRelease(ctx, "SagerNet/sing-box")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if info.Version != "1.12.0" || info.TagName != "v1.12.0" {
		t.Errorf("unexpected version info: %+v", info)
	}
	if lastAuthHeader != "token ghp_test_mock_token_123" {
		t.Errorf("expected Authorization 'token ghp_test_mock_token_123', got: %s", lastAuthHeader)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// 2. 缓存命中校验 (不重复调用)
	infoCached, err := checker.GetLatestRelease(ctx, "SagerNet/sing-box")
	if err != nil {
		t.Fatalf("expected cached success, got: %v", err)
	}
	if infoCached.Version != "1.12.0" {
		t.Errorf("expected cached version 1.12.0, got: %s", infoCached.Version)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 call due to cache, got %d", callCount)
	}

	// 3. 上游限流 403 降级回退至旧缓存测试 (缓存过期强制触发)
	checker.cacheMu.Lock()
	if c, ok := checker.cache["SagerNet/sing-box"]; ok {
		// 设置较久时间使 TTL 判定失效，但缓存对象存在
		c.CachedAt = c.CachedAt.Add(-3 * 24 * 3600 * 1e9)
	}
	checker.cacheMu.Unlock()

	statusCode = http.StatusForbidden
	infoFallback, err := checker.GetLatestRelease(ctx, "SagerNet/sing-box")
	if err != nil {
		t.Fatalf("expected fallback to cached version on 403, got error: %v", err)
	}
	if infoFallback.Version != "1.12.0" {
		t.Errorf("expected fallback version 1.12.0, got: %s", infoFallback.Version)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}

	// 4. 全新仓库无缓存且 403 时报错
	_, err = checker.GetLatestRelease(ctx, "NonExistent/repo")
	if err == nil {
		t.Fatalf("expected error on 403 without cache, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 error, got: %v", err)
	}
}
