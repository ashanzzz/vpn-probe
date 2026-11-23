package api

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"sort"
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

// ResetAuthTracker 重置防爆破追踪器 (主要用于测试隔离)
func ResetAuthTracker() {
	globalTracker.mu.Lock()
	defer globalTracker.mu.Unlock()
	globalTracker.failures = make(map[string]*failureRecord)
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
	// 已过期且超过 10 分钟窗口的旧失败记录，在查阅时顺手清理
	if now.Sub(rec.firstFailAt) > 10*time.Minute {
		delete(t.failures, ip)
	}
	return false, 0
}

func (t *ipFailTracker) cleanupLocked(now time.Time) {
	// 防御性内存回收：当记录数超过 100 时，清理已解锁且超过 10 分钟窗口的陈旧记录
	if len(t.failures) > 100 {
		for ip, rec := range t.failures {
			if now.After(rec.lockedUntil) && now.Sub(rec.firstFailAt) > 10*time.Minute {
				delete(t.failures, ip)
			}
		}
		// 若依然超过硬上限 500（例如面临分布式海量扫描），按最早失败时间淘汰超额记录保安全
		if excess := len(t.failures) - 500; excess > 0 {
			type ipTimestamp struct {
				ip string
				t  time.Time
			}
			items := make([]ipTimestamp, 0, len(t.failures))
			for ip, rec := range t.failures {
				items = append(items, ipTimestamp{ip: ip, t: rec.firstFailAt})
			}
			sort.Slice(items, func(i, j int) bool {
				return items[i].t.Before(items[j].t)
			})
			for i := 0; i < excess && i < len(items); i++ {
				delete(t.failures, items[i].ip)
			}
		}
	}
}

func (t *ipFailTracker) recordFailure(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.cleanupLocked(now)

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

		// 规则脚本公开访问免密放行 (/rules/*.js 且只允许 GET 供 Sub-Store 算子拉取)
		if strings.HasPrefix(path, "/rules/") && c.Method() == fiber.MethodGet {
			return c.Next()
		}

		// 静态前端资源免密放行 (只允许 GET/HEAD 访问根路径及静态资产文件)
		if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/mcp/") && !strings.HasPrefix(path, "/rules/") {
			if c.Method() == fiber.MethodGet || c.Method() == fiber.MethodHead {
				return c.Next()
			}
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

		// 4. 提取 Token (支持 Bearer, Basic Auth, X-API-Key 及白名单 URL Query)
		headerToken := ""
		queryToken := ""

		authHeader := c.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				headerToken = parts[1]
			} else if len(parts) == 2 && strings.EqualFold(parts[0], "Basic") {
				if decoded, err := base64.StdEncoding.DecodeString(parts[1]); err == nil {
					userPass := string(decoded)
					if idx := strings.Index(userPass, ":"); idx != -1 {
						pass := userPass[idx+1:]
						user := userPass[:idx]
						if pass != "" {
							headerToken = pass
						} else {
							headerToken = user
						}
					} else {
						headerToken = userPass
					}
				}
			} else {
				headerToken = authHeader
			}
		}
		if headerToken == "" {
			headerToken = c.Get("X-API-Key")
		}
		headerToken = strings.TrimSpace(headerToken)

		queryToken = c.Query("token")
		if queryToken == "" {
			queryToken = c.Query("api_key")
		}
		queryToken = strings.TrimSpace(queryToken)

		// 安全规则：URL Query 传参仅允许在 /api/sub/* 订阅拉取端点及 /rules/* 脚本引用端点使用
		isQueryAllowed := strings.HasPrefix(path, "/api/sub/") || strings.HasPrefix(path, "/rules/")
		if queryToken != "" && !isQueryAllowed {
			customlog.Audit("auth_query_rejected", clientIP, path, "rejected", "Query param ?token= is only permitted for /api/sub/* and /rules/*")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "unauthorized",
				"message": "Query parameter authentication is only allowed for subscription endpoints (/api/sub/*) and rules endpoints (/rules/*). Use Authorization or X-API-Key header.",
			})
		}

		// 5. 恒定时间比对判断角色权限 (优先 Header，Header 未命中且端点允许时回退 Query)
		evaluateRole := func(tok string) Role {
			if tok == "" {
				return RoleNone
			}
			if cfg.AdminToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.AdminToken)) == 1 {
				return RoleAdmin
			}
			if cfg.AuthToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.AuthToken)) == 1 {
				return RoleWrite
			}
			if cfg.ReadToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.ReadToken)) == 1 {
				return RoleRead
			}
			return RoleNone
		}

		matchedRole := evaluateRole(headerToken)
		if matchedRole == RoleNone && isQueryAllowed && queryToken != "" {
			matchedRole = evaluateRole(queryToken)
		}

		if matchedRole == RoleNone {
			justLocked := globalTracker.recordFailure(clientIP)
			if justLocked {
				customlog.Audit("ip_lockout", clientIP, path, "locked", "5 consecutive auth failures, locked for 60s")
			}
			if headerToken == "" && queryToken == "" {
				customlog.Event(customlog.LevelWarn, "auth", "token_missing", fmt.Sprintf("ip=%s path=%s method=%s", clientIP, path, c.Method()))
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error":   "unauthorized",
					"message": "Authentication token required. Use 'Authorization: Bearer ***', 'Authorization: Basic ***', or 'X-API-Key: ***'.",
				})
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
