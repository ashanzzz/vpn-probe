package api

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestAuthMiddleware(t *testing.T) {
	authToken := "my-write-token"
	readToken := "my-read-token"
	adminToken := "my-admin-token"

	app := fiber.New()
	app.Use(NewSecurityHeadersMiddleware())
	app.Use(NewAuthMiddleware(AuthConfig{
		AuthToken:  authToken,
		ReadToken:  readToken,
		AdminToken: adminToken,
		Whitelist:  map[string]bool{"/health": true},
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})
	app.Get("/api/sub/online", func(c *fiber.Ctx) error {
		return c.SendString("sub-content")
	})
	app.Get("/api/protected", func(c *fiber.Ctx) error {
		return c.SendString("secret-data")
	})
	app.Post("/api/protected", func(c *fiber.Ctx) error {
		return c.SendString("updated")
	})
	app.Get("/api/sensitive", func(c *fiber.Ctx) error {
		if c.QueryBool("reveal", false) {
			return c.SendString("raw-credentials")
		}
		return c.SendString("masked")
	})

	app.Get("/rules/custom_patch.js", func(c *fiber.Ctx) error {
		return c.SendString("// patch js")
	})
	app.Post("/rules/custom_patch.js", func(c *fiber.Ctx) error {
		return c.SendString("// post patch")
	})

	// 1. 测试白名单接口 (免密且返回安全响应头)
	req := httptest.NewRequest("GET", "/health", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected /health to return 200, got %d, err: %v", resp.StatusCode, err)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff header")
	}

	// 2. 测试保护接口无 Token -> 401
	req = httptest.NewRequest("GET", "/api/protected", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected /api/protected without token to return 401, got %d", resp.StatusCode)
	}

	// 3. 测试非订阅端点使用 URL query token -> 401 拦截
	req = httptest.NewRequest("GET", "/api/protected?token="+authToken, nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected query token on non-sub endpoint to be rejected with 401, got %d", resp.StatusCode)
	}

	// 4. 测试订阅端点使用 URL query token -> 200 放行并携带 Cache-Control: no-store
	req = httptest.NewRequest("GET", "/api/sub/online?token="+readToken, nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected /api/sub/online with query token to return 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("expected Cache-Control: no-store on subscription response")
	}

	// 5. 测试 Header Authorization: Bearer <authToken> -> 200
	req = httptest.NewRequest("GET", "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected /api/protected with Bearer token to return 200, got %d", resp.StatusCode)
	}

	// 6. 测试只读 Token 进行 POST 写操作 -> 403 Forbidden
	req = httptest.NewRequest("POST", "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer "+readToken)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected readToken POST to return 403, got %d", resp.StatusCode)
	}

	// 7. 测试只读 Token 请求 reveal=true -> 403 Forbidden
	req = httptest.NewRequest("GET", "/api/sensitive?reveal=true", nil)
	req.Header.Set("Authorization", "Bearer "+readToken)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected readToken reveal=true to return 403, got %d", resp.StatusCode)
	}

	// 8. 测试 Admin Token 请求 reveal=true -> 200 OK
	req = httptest.NewRequest("GET", "/api/sensitive?reveal=true", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected adminToken reveal=true to return 200, got %d", resp.StatusCode)
	}

	// 9. 测试 /rules/*.js 静态规则脚本免密放行 (SubStore 脚本算子直接无 Token GET 引用)
	req = httptest.NewRequest("GET", "/rules/custom_patch.js", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected GET /rules/custom_patch.js without token to return 200, got %d", resp.StatusCode)
	}

	// 10. 测试 /rules/*.js 携带 query token 亦能安全放行
	req = httptest.NewRequest("GET", "/rules/custom_patch.js?token=some_token", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected GET /rules/custom_patch.js?token=... to return 200, got %d", resp.StatusCode)
	}

	// 11. 测试非 GET 访问 /rules/* 无 token 仍被拦截 -> 401
	req = httptest.NewRequest("POST", "/rules/custom_patch.js", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected POST /rules/custom_patch.js without token to return 401, got %d", resp.StatusCode)
	}

	// 12. 测试 HTTP Basic Auth (ashan:writeToken) -> 200 OK
	req = httptest.NewRequest("GET", "/api/protected", nil)
	req.SetBasicAuth("ashan", authToken)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected Basic Auth to return 200, got %d", resp.StatusCode)
	}

	// 13. 测试 HTTP Basic Auth (只传密码形式 :writeToken) -> 200 OK
	req = httptest.NewRequest("GET", "/api/protected", nil)
	req.SetBasicAuth("", authToken)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected password-only Basic Auth to return 200, got %d", resp.StatusCode)
	}

	// 14. 测试 Header 携带其他/未匹配鉴权时，订阅端点回退至合法 Query Token -> 200 OK
	req = httptest.NewRequest("GET", "/api/sub/online?token="+readToken, nil)
	req.SetBasicAuth("other_user", "mismatched_password")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected query token fallback on subscription endpoint to return 200, got %d", resp.StatusCode)
	}

	// 15. 测试 Header / Query Token 携带前后空白字符自动 TrimSpace -> 200 OK
	req = httptest.NewRequest("GET", "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer  "+authToken+"  \n")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected header token with whitespace padding to return 200, got %d", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/api/sub/online?token=%20"+readToken+"%20", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected query token with whitespace padding to return 200, got %d", resp.StatusCode)
	}
}

func TestIPLockout(t *testing.T) {
	globalTracker.recordSuccess("0.0.0.0")
	app := fiber.New()
	app.Use(NewAuthMiddleware(AuthConfig{
		AuthToken: "valid-secret-token",
	}))
	app.Get("/api/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// 连续发送 5 次错误 token
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("step %d: expected 401, got %d", i, resp.StatusCode)
		}
	}

	// 第 6 次请求即被临时封禁 429 Too Many Requests
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-secret-token")
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("expected locked IP to return 429, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_FailureTrackerPruning(t *testing.T) {
	ResetAuthTracker()
	now := time.Now()

	// 1. 模拟注入 150 个已过期超过 10 分钟窗口的陈旧扫描 IP
	globalTracker.mu.Lock()
	for i := 1; i <= 150; i++ {
		fakeIP := fmt.Sprintf("192.168.100.%d", i)
		globalTracker.failures[fakeIP] = &failureRecord{
			count:       2,
			firstFailAt: now.Add(-15 * time.Minute),
			lockedUntil: now.Add(-14 * time.Minute),
		}
	}
	if len(globalTracker.failures) != 150 {
		globalTracker.mu.Unlock()
		t.Fatalf("expected 150 failure records, got %d", len(globalTracker.failures))
	}
	globalTracker.mu.Unlock()

	// 2. 触发一次新的失败，触发自动清理
	globalTracker.recordFailure("10.0.0.1")

	globalTracker.mu.Lock()
	remaining := len(globalTracker.failures)
	globalTracker.mu.Unlock()

	if remaining > 10 {
		t.Fatalf("expected expired records to be pruned, remaining: %d", remaining)
	}

	// 3. 验证 isLocked 懒清理：注入一个过期条目，查询时自动移除
	globalTracker.mu.Lock()
	globalTracker.failures["10.0.0.2"] = &failureRecord{
		count:       1,
		firstFailAt: now.Add(-12 * time.Minute),
		lockedUntil: now.Add(-11 * time.Minute),
	}
	globalTracker.mu.Unlock()

	locked, _ := globalTracker.isLocked("10.0.0.2")
	if locked {
		t.Fatal("expected expired IP to not be locked")
	}

	globalTracker.mu.Lock()
	_, exists := globalTracker.failures["10.0.0.2"]
	globalTracker.mu.Unlock()
	if exists {
		t.Fatal("expected expired IP to be pruned upon isLocked call")
	}

	// 4. 验证极端高频扫描下的硬上限 (500 条) 淘汰
	globalTracker.mu.Lock()
	for i := 1; i <= 550; i++ {
		fakeIP := fmt.Sprintf("172.16.%d.%d", i/256, i%256)
		globalTracker.failures[fakeIP] = &failureRecord{
			count:       1,
			firstFailAt: now.Add(time.Duration(-i) * time.Second),
			lockedUntil: now.Add(60 * time.Second),
		}
	}
	globalTracker.mu.Unlock()

	globalTracker.recordFailure("10.99.99.99")

	globalTracker.mu.Lock()
	hardCapRemaining := len(globalTracker.failures)
	globalTracker.mu.Unlock()

	if hardCapRemaining > 505 {
		t.Fatalf("expected hard cap pruning to keep records bounded <= 505, got %d", hardCapRemaining)
	}
}

func TestAuthMiddleware_RevealRequiresAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(NewAuthMiddleware(AuthConfig{
		AuthToken:  "write-tok",
		ReadToken:  "read-tok",
		AdminToken: "admin-tok",
	}))
	app.Get("/api/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	cases := []struct {
		name   string
		token  string
		path   string
		expect int
	}{
		{"read reveal=true denied", "read-tok", "/api/test?reveal=true", 403},
		{"write reveal=true denied", "write-tok", "/api/test?reveal=true", 403},
		{"write reveal=1 denied", "write-tok", "/api/test?reveal=1", 403},
		{"write normal allowed", "write-tok", "/api/test", 200},
		{"admin reveal=true allowed", "admin-tok", "/api/test?reveal=true", 200},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s failed: %v", tc.name, err)
		}
		if resp.StatusCode != tc.expect {
			t.Errorf("%s: expected HTTP %d, got %d", tc.name, tc.expect, resp.StatusCode)
		}
	}
}
