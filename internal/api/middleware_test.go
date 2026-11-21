package api

import (
	"net/http/httptest"
	"testing"

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
