package api

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	customlog "vpn-probe/internal/logger"
)

type Role string

const (
	RoleNone  Role = ""
	RoleRead  Role = "read"
	RoleWrite Role = "write"
	RoleAdmin Role = "admin"
)

// AuthConfig 鉴权中间件配置
type AuthConfig struct {
	AuthToken  string          // 主鉴权 Token (拥有写/控制权限)
	ReadToken  string          // 只读 Token (仅允许拉取订阅、查看健康与状态)
	AdminToken string          // 管理员 Token (用于高危操作、查看明文)
	Whitelist  map[string]bool // 精确免密白名单 (例如: {"/health": true})
}

// IP 鉴权失败追踪器
type ipFailTracker struct {
	mu       sync.Mutex
	failures map[string]*failureRecord
}

type failureRecord struct {
	count       int
	firstFailAt time.Time
	lockedUntil time.Time
}

var globalTracker = &ipFailTracker{
	failures: make(map[string]*failureRecord),
}

func (t *ipFailTracker) isLocked(ip string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec, ok := t.failures[ip]
	if !ok {
		return false, 0
	}
	now := time.Now()
	if rec.lockedUntil.After(now) {
		return true, rec.lockedUntil.Sub(now)
	}
	return false, 0
}

func (t *ipFailTracker) recordFailure(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	rec, ok := t.failures[ip]
	if !ok || time.Since(rec.firstFailAt) > 60*time.Second {
		t.failures[ip] = &failureRecord{
			count:       1,
			firstFailAt: now,
		}
		return false
	}

	rec.count++
	if rec.count >= 5 {
		rec.lockedUntil = now.Add(60 * time.Second)
		return true
	}
	return false
}

func (t *ipFailTracker) recordSuccess(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, ip)
}

// NewSecurityHeadersMiddleware 注入基础安全响应头
func NewSecurityHeadersMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		return c.Next()
	}
}

// NewAuthMiddleware 返回全局鉴权中间件
func NewAuthMiddleware(cfg AuthConfig) fiber.Handler {
	// 如果 AdminToken 为空，默认使用 AuthToken
	if cfg.AdminToken == "" {
		cfg.AdminToken = cfg.AuthToken
	}

	return func(c *fiber.Ctx) error {
		path := c.Path()
		clientIP := c.IP()

		// 1. 检查精确免密白名单
		if cfg.Whitelist != nil && cfg.Whitelist[path] {
			return c.Next()
		}

		// 2. 检查 IP 是否被临时锁定
		if locked, remaining := globalTracker.isLocked(clientIP); locked {
			customlog.Event(customlog.LevelWarn, "auth", "ip_locked",
				fmt.Sprintf("ip=%s remaining_sec=%d", clientIP, int(remaining.Seconds())))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":   "too_many_requests",
				"message": fmt.Sprintf("Too many failed auth attempts. Try again in %d seconds.", int(remaining.Seconds())),
			})
		}

		// 3. 生产模式空密码检查 (若全部 Token 均未配置，直接拦截拒绝裸奔)
		if cfg.AuthToken == "" && cfg.ReadToken == "" && cfg.AdminToken == "" {
			customlog.Event(customlog.LevelError, "auth", "no_token_configured", "Server has no tokens configured; rejecting request")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "unauthorized",
				"message": "Server authentication not configured. Set VPN_PROBE_AUTH_TOKEN.",
			})
		}

		// 4. 提取 Token
		headerToken := ""
		queryToken := ""

		authHeader := c.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				headerToken = parts[1]
			} else {
				headerToken = authHeader
			}
		}
		if headerToken == "" {
			headerToken = c.Get("X-API-Key")
		}

		queryToken = c.Query("token")
		if queryToken == "" {
			queryToken = c.Query("api_key")
		}

		// 安全规则：URL Query 传参仅允许在 /api/sub/* 订阅拉取端点使用
		if queryToken != "" && !strings.HasPrefix(path, "/api/sub/") {
			customlog.Audit("auth_query_rejected", clientIP, path, "rejected", "Query param ?token= is only permitted for /api/sub/*")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "unauthorized",
				"message": "Query parameter authentication is only allowed for subscription endpoints (/api/sub/*). Use Authorization or X-API-Key header.",
			})
		}

		providedToken := headerToken
		if providedToken == "" {
			providedToken = queryToken
		}

		if providedToken == "" {
			justLocked := globalTracker.recordFailure(clientIP)
			if justLocked {
				customlog.Audit("ip_lockout", clientIP, path, "locked", "5 consecutive auth failures, locked for 60s")
			}
			customlog.Event(customlog.LevelWarn, "auth", "token_missing", fmt.Sprintf("ip=%s path=%s method=%s", clientIP, path, c.Method()))
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "unauthorized",
				"message": "Authentication token required. Use 'Authorization: Bearer <token>' or 'X-API-Key: <token>'.",
			})
		}

		// 5. 恒定时间比对判断角色权限
		var matchedRole Role = RoleNone

		if cfg.AdminToken != "" && subtle.ConstantTimeCompare([]byte(providedToken), []byte(cfg.AdminToken)) == 1 {
			matchedRole = RoleAdmin
		} else if cfg.AuthToken != "" && subtle.ConstantTimeCompare([]byte(providedToken), []byte(cfg.AuthToken)) == 1 {
			matchedRole = RoleWrite
		} else if cfg.ReadToken != "" && subtle.ConstantTimeCompare([]byte(providedToken), []byte(cfg.ReadToken)) == 1 {
			matchedRole = RoleRead
		}

		if matchedRole == RoleNone {
			justLocked := globalTracker.recordFailure(clientIP)
			if justLocked {
				customlog.Audit("ip_lockout", clientIP, path, "locked", "5 consecutive auth failures, locked for 60s")
			}
			customlog.Event(customlog.LevelWarn, "auth", "token_invalid", fmt.Sprintf("ip=%s path=%s method=%s", clientIP, path, c.Method()))
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "unauthorized",
				"message": "Invalid authentication token.",
			})
		}

		// 验证成功，清除失败记录
		globalTracker.recordSuccess(clientIP)
		c.Locals("role", matchedRole)

		// 6. 角色鉴权拦截
		if matchedRole == RoleRead {
			// 只读 Token 仅允许 GET/HEAD
			if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
				customlog.Audit("rbac_violation", clientIP, path, "denied", "read_token attempted write method: "+c.Method())
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":   "forbidden",
					"message": "Read-only token is not permitted to perform write operations.",
				})
			}
			// 只读 Token 禁止访问 MCP 路径
			if strings.HasPrefix(path, "/mcp") {
				customlog.Audit("rbac_violation", clientIP, path, "denied", "read_token attempted MCP execution")
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":   "forbidden",
					"message": "Read-only token cannot access MCP tools.",
				})
			}
			// 只读 Token 禁止使用 reveal=true
			if c.QueryBool("reveal", false) {
				customlog.Audit("rbac_violation", clientIP, path, "denied", "read_token attempted reveal=true")
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":   "forbidden",
					"message": "Admin privilege required to reveal unmasked secrets.",
				})
			}
		}

		// 7. 敏感接口增加 Cache-Control: no-store
		if strings.HasPrefix(path, "/api/sub/") || c.QueryBool("reveal", false) {
			c.Set("Cache-Control", "no-store")
		}

		return c.Next()
	}
}
