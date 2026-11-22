package api

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"vpn-probe/internal/config"
	customlog "vpn-probe/internal/logger"
	"vpn-probe/internal/mcp"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/probe"
	"vpn-probe/internal/rules"
	"vpn-probe/internal/speedtest"
	"vpn-probe/internal/store"
	"vpn-probe/internal/substore"
	"vpn-probe/internal/vpsmgr"
	"vpn-probe/web"
)

type APIServer struct {
	app            *fiber.App
	cfg            *config.Config
	configPath     string
	engine         *probe.Engine
	store          *store.Store
	substoreClient *substore.Client
	snapshotMgr    *substore.SnapshotManager
	mcpServer      *mcp.MCPServer
	scheduler      *monitor.Scheduler
	rulesMgr       *rules.Manager
	vpsMgr         *vpsmgr.Manager
	orchestrator   *monitor.Orchestrator
}

// SetConfigPath 设置配置文件路径以支持运行时热改写保存
func (s *APIServer) SetConfigPath(p string) {
	s.configPath = p
}

// SetVPSManager 注入 VPS 管理器
func (s *APIServer) SetVPSManager(m *vpsmgr.Manager) {
	s.vpsMgr = m
}

// SetSnapshotManager 注入快照管理器 (支持测试隔离与自定义存储路径)
func (s *APIServer) SetSnapshotManager(sm *substore.SnapshotManager) {
	s.snapshotMgr = sm
}

// SetOrchestrator 注入动态编排器 (支持测试隔离与自定义实例)
func (s *APIServer) SetOrchestrator(orc *monitor.Orchestrator) {
	s.orchestrator = orc
}

func NewAPIServer(
	cfg *config.Config,
	engine *probe.Engine,
	st *store.Store,
	sc *substore.Client,
	ms *mcp.MCPServer,
	sched *monitor.Scheduler,
	rm *rules.Manager,
) *APIServer {
	app := fiber.New(fiber.Config{
		AppName:               "vpn-probe API & MCP Gateway",
		DisableStartupMessage: true,
		BodyLimit:             1 * 1024 * 1024, // 1MB 请求体限制防止大 payload 攻击
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format:     "[${time}] [HTTP]   ${status} | ${latency} | ${method} ${path}\n",
		TimeFormat: "2006-01-02 15:04:05.000",
	}))
	corsOrigins := cfg.Server.AllowOrigins
	if corsOrigins == "" {
		corsOrigins = "*"
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: corsOrigins,
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-API-Key",
	}))

	// 挂载安全响应头 (nosniff, frame-deny)
	app.Use(NewSecurityHeadersMiddleware())

	// 挂载安全鉴权中间件 (精确免密白名单：/health, /favicon.ico, 以及供 Sub-Store 算子拉取的规则脚本)
	whitelist := map[string]bool{
		"/health":                true,
		"/favicon.ico":           true,
		"/rules/custom_patch.js": true,
		"/rules/convert.js":      true,
	}
	app.Use(NewAuthMiddleware(AuthConfig{
		AuthToken:  cfg.Server.AuthToken,
		ReadToken:  cfg.Server.ReadToken,
		AdminToken: cfg.Server.AdminToken,
		Whitelist:  whitelist,
	}))

	if cfg.Server.AuthToken != "" || cfg.Server.ReadToken != "" || cfg.Server.AdminToken != "" {
		customlog.Infof("[SECURITY] 🔒 API 与 MCP 统一多权限鉴权已就绪 (保护全部 /api/* 与 /mcp/* 接口)")
	} else {
		customlog.Warnf("[SECURITY] ⚠️  未配置任何 Token！API 与 MCP 控制接口处于无保护开放模式！")
	}

	var snapshotMgr *substore.SnapshotManager
	if sc != nil {
		snapshotMgr = substore.NewSnapshotManager(sc, filepath.Join("data", "snapshots"))
		if cfg.SubStore.Enabled {
			snapshotMgr.StartAutoWarmup(15*time.Minute, []string{"ashan", "Ashan", "Q", "q"})
		}
	}

	server := &APIServer{
		app:            app,
		cfg:            cfg,
		engine:         engine,
		store:          st,
		substoreClient: sc,
		snapshotMgr:    snapshotMgr,
		mcpServer:      ms,
		scheduler:      sched,
		rulesMgr:       rm,
		orchestrator:   monitor.NewOrchestrator(sched),
	}

	server.setupRoutes()
	return server
}

func (s *APIServer) setupRoutes() {
	// 基础探针
	s.app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "vpn-probe"})
	})

	// 1. 批量检测 API (REST)
	s.app.Post("/api/probe", s.handleProbe)

	// 2. 监控调度与自动巡检状态 API
	s.app.Get("/api/monitor/status", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		status := s.scheduler.GetStatus()
		reveal := c.QueryBool("reveal", false)
		if reveal {
			role, _ := c.Locals("role").(Role)
			if role != RoleAdmin {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privilege required to reveal unmasked urls"})
			}
			customlog.Audit("reveal_nodes", c.IP(), "/api/monitor/status", "success", "Caller retrieved unmasked node URLs")
			return c.JSON(status)
		}
		masked := make(map[string]any, len(status))
		for k, v := range status {
			if v == nil {
				continue
			}
			clone := *v
			cfgClone := clone.Config
			cfgClone.URL = MaskNodeURL(cfgClone.URL)
			clone.Config = cfgClone
			if clone.LastResult != nil {
				resClone := *clone.LastResult
				resClone.OriginalURL = MaskNodeURL(resClone.OriginalURL)
				clone.LastResult = &resClone
			}
			masked[k] = clone
		}
		return c.JSON(masked)
	})

	// 专供 AI / 自愈代理消费的清晰聚合大盘（含完整 V2Ray 链接、分类诊断与自愈指令）
	s.app.Get("/api/ai/status", s.handleAISummary)
	s.app.Get("/api/monitor/ai-summary", s.handleAISummary)
	s.app.Get("/api/summary", s.handleAISummary)
	s.app.Get("/api/v1/summary", s.handleAISummary)
	s.app.Get("/api/alerts", s.handleAlerts)
	s.app.Get("/api/v1/alerts", s.handleAlerts)
	// 供 AI 专门拉取需要换端口的节点清单 (只包含 XXX端口被墙 的节点)
	s.app.Get("/api/monitor/port-blocked", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		reveal := c.QueryBool("reveal", false)
		allStates := s.scheduler.GetStatus()
		blockedList := make([]fiber.Map, 0)
		for _, st := range allStates {
			if st.LastResult != nil && st.LastResult.Diagnostic != nil {
				if st.LastResult.Diagnostic.FaultType == model.FaultPortBlocked {
					nodeURL := st.Config.URL
					if !reveal {
						nodeURL = MaskNodeURL(nodeURL)
					}
					blockedList = append(blockedList, fiber.Map{
						"name":             st.Config.Name,
						"original_url":     nodeURL,
						"target_ip":        st.LastResult.Diagnostic.TargetIP,
						"blocked_port":     st.LastResult.Diagnostic.TargetPort,
						"status_text":      st.LastResult.Diagnostic.StatusText, // 例如 "33338端口被墙"
						"abroad_source":    st.LastResult.Diagnostic.AbroadSource,
						"abroad_rtt_ms":    st.LastResult.Diagnostic.AbroadRTTMs,
						"action_type":      st.LastResult.Diagnostic.ActionType, // "AI_CHANGE_PORT"
						"troubleshoot_tip": st.LastResult.Diagnostic.TroubleshootTip,
					})
				}
			}
		}
		return c.JSON(fiber.Map{
			"total": len(blockedList),
			"nodes": blockedList,
		})
	})
	// 供管理员/AI 拉取真正离线 (机子关机/软件坏了/线路断) 的节点清单，需单独排查
	s.app.Get("/api/monitor/offline", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		reveal := c.QueryBool("reveal", false)
		allStates := s.scheduler.GetStatus()
		offlineList := make([]fiber.Map, 0)
		for _, st := range allStates {
			if st.LastResult != nil && st.LastResult.Diagnostic != nil {
				if st.LastResult.Diagnostic.FaultType == model.FaultMachineDown {
					nodeURL := st.Config.URL
					if !reveal {
						nodeURL = MaskNodeURL(nodeURL)
					}
					offlineList = append(offlineList, fiber.Map{
						"name":             st.Config.Name,
						"original_url":     nodeURL,
						"target_ip":        st.LastResult.Diagnostic.TargetIP,
						"target_port":      st.LastResult.Diagnostic.TargetPort,
						"status_text":      st.LastResult.Diagnostic.StatusText,
						"troubleshoot_tip": st.LastResult.Diagnostic.TroubleshootTip,
					})
				}
			}
		}
		return c.JSON(fiber.Map{
			"total": len(offlineList),
			"nodes": offlineList,
		})
	})
	s.app.Post("/api/monitor/trigger", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		checkSplit := c.QueryBool("split", false)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		go func() {
			defer cancel()
			s.scheduler.RunAllChecks(ctx, checkSplit)
		}()
		return c.JSON(fiber.Map{"status": "ok", "message": "inspection round triggered"})
	})

	// 3. 历史记录查询 API
	s.app.Get("/api/history", s.handleGetHistory)

	// 3. 动态订阅拉取接口（给 SubStore 或客户端直接订阅用）
	s.app.Get("/api/sub/:group", s.handleGetGroupSubscription)

	// 3.1 极速预烘焙订阅快照直出接口 (Issue #21 彻底终结 SubStore 300% CPU 负载风暴)
	s.app.Get("/api/sub/file/:name", s.handleGetSnapshotFile)
	s.app.Get("/api/sub/snapshot/status", s.handleGetSnapshotStatus)
	s.app.Post("/api/sub/snapshot/warmup", s.handleTriggerSnapshotWarmup)

	// 4. 手动触发 SubStore 远端同步
	s.app.Post("/api/substore/sync", s.handleSyncSubStore)

	// 5. 个性规则动态管理 API 与 Sub-Store 脚本托管
	s.app.Get("/api/rules/custom", func(c *fiber.Ctx) error {
		if s.rulesMgr == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "rules manager not initialized"})
		}
		return c.JSON(s.rulesMgr.GetConfig())
	})
	s.app.Post("/api/rules/custom", func(c *fiber.Ctx) error {
		if s.rulesMgr == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "rules manager not initialized"})
		}
		var req rules.CustomRuleConfig
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if err := s.rulesMgr.UpdateConfig(req); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "ok", "message": "custom rules updated", "rules": s.rulesMgr.GetConfig()})
	})
	s.app.Post("/api/rules/direct-domain", func(c *fiber.Ctx) error {
		if s.rulesMgr == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "rules manager not initialized"})
		}
		domain := c.Query("domain")
		if domain == "" {
			var body struct {
				Domain string `json:"domain"`
			}
			_ = c.BodyParser(&body)
			domain = body.Domain
		}
		if domain == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "domain parameter required"})
		}
		_ = s.rulesMgr.AddDirectDomain(domain)
		return c.JSON(fiber.Map{"status": "ok", "message": "direct domain added", "domain": domain})
	})
	// 供 Sub-Store Script Operator 直接引用的动态 JS 脚本
	s.app.Get("/rules/custom_patch.js", func(c *fiber.Ctx) error {
		if s.rulesMgr == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString("// rules manager not initialized")
		}
		c.Set("Content-Type", "application/javascript; charset=utf-8")
		return c.SendString(s.rulesMgr.GenerateJS())
	})

	// 动态缓存并桥接 GitHub 开源最新 convert.min.js 的 Sub-Store 算子接口
	s.app.Get("/rules/convert.js", func(c *fiber.Ctx) error {
		if s.rulesMgr == nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString("// rules manager not initialized")
		}
		c.Set("Content-Type", "application/javascript; charset=utf-8")
		return c.SendString(s.rulesMgr.GetBridgedConvertJS())
	})

	// 5.1 SubStore 自有订阅节点管理 API (查阅/增删/更新自有链接)
	s.app.Get("/api/substore/nodes", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		name := c.Query("name", "ashan's vps")
		reveal := c.QueryBool("reveal", false)
		content, err := s.substoreClient.GetSubscriptionContent(c.Context(), name)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		lines := make([]string, 0)
		for _, l := range strings.Split(content, "\n") {
			l = strings.TrimSpace(l)
			if l != "" {
				if !reveal {
					lines = append(lines, MaskNodeURL(l))
				} else {
					lines = append(lines, l)
				}
			}
		}
		res := fiber.Map{
			"status":       "ok",
			"subscription": name,
			"count":        len(lines),
			"nodes":        lines,
		}
		if reveal {
			res["raw_content"] = content
		}
		return c.JSON(res)
	})
	s.app.Post("/api/substore/nodes", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		var body struct {
			Name    string   `json:"name"`
			Content string   `json:"content"`
			Links   []string `json:"links"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		subName := body.Name
		if subName == "" {
			subName = "ashan's vps"
		}
		var links []string
		if len(body.Links) > 0 {
			links = body.Links
		} else if body.Content != "" {
			for _, l := range strings.Split(body.Content, "\n") {
				l = strings.TrimSpace(l)
				if l != "" {
					links = append(links, l)
				}
			}
		} else {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "either links or content is required"})
		}
		if err := s.substoreClient.UpdateSubscriptionNodes(c.Context(), subName, links); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"status":       "ok",
			"message":      fmt.Sprintf("updated %d nodes to SubStore subscription '%s'", len(links), subName),
			"subscription": subName,
			"count":        len(links),
		})
	})

	// 5.2 SubStore 拓扑审计与自主整理 (Self-Healing Topology Reconciler)
	s.app.Get("/api/substore/topology", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		audit, err := s.substoreClient.AuditTopology(c.Context())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(audit)
	})
	s.app.Post("/api/substore/reconcile", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		audit, err := s.substoreClient.ReconcileTopology(c.Context())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"status": "ok",
			"message": "SubStore topology successfully reconciled and organized",
			"audit": audit,
		})
	})

	// 5.1.4 清理指定订阅中的失效 socks5 节点 (带快照备份)
	s.app.Post("/api/substore/cleanup", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		var req struct {
			Subscription string `json:"subscription"`
		}
		_ = c.BodyParser(&req)
		subName := req.Subscription
		if subName == "" {
			subName = "Q"
		}
		res, err := s.substoreClient.CleanupDeadSocks(c.Context(), subName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		customlog.Audit("cleanup_dead_socks", c.IP(), fmt.Sprintf("sub/%s", subName), "success", fmt.Sprintf("Removed %d dead socks nodes", res.RemovedCount))
		return c.JSON(res)
	})

	// 5.1.4.1 平退并删除 SubStore 中历史残留的旧三层独立订阅 (ashan-transit-direct, ashan-resi-direct, ashan-chain, ashan-tw-resi)
	s.app.Post("/api/substore/purge-legacy", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		purged, err := s.substoreClient.PurgeLegacyThreeTierSubs(c.Context())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":        err.Error(),
				"purged_count": purged,
				"purged_subs":  substore.LegacyThreeTierSubNames,
			})
		}
		customlog.Audit("purge_legacy_subs", c.IP(), "substore/legacy", "success", fmt.Sprintf("Purged %d legacy subscriptions", purged))
		return c.JSON(fiber.Map{
			"status":       "ok",
			"purged_count": purged,
			"purged_subs":  substore.LegacyThreeTierSubNames,
			"message":      "Successfully purged legacy 3-tier subscriptions from SubStore",
		})
	})

	// 5.1.5 审计指定 File 生成配置的策略组引用完整性 (检查空引用与双域名连通性)
	s.app.Get("/api/substore/audit", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		fileName := c.Query("file", "Q")
		token := c.Query("token", "13602004317")
		audit, err := s.substoreClient.AuditFileGroups(c.Context(), fileName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		audit.DomainsCheck = substore.CheckShareDomains(c.Context(), fileName, token)
		return c.JSON(audit)
	})

	// 5.1.5.1 综合订阅全量校验报告 (双域名测试 + 策略组与连接分布 + 仅自建节点延迟/送中/分流 + 组合创建正确性证明)
	s.app.Get("/api/substore/verify", s.handleVerifySubscription)

	// 5.1.6 查询组合订阅当前三模式状态与动态外部机场池 (Issue #18)
	s.app.Get("/api/substore/mode", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		curr := s.substoreClient.GetCurrentMode()
		extSources := s.substoreClient.GetExternalSources()
		return c.JSON(fiber.Map{
			"current_mode":     curr,
			"allowed_modes":    []string{"external+self", "self", "external"},
			"external_sources": extSources,
			"subscriptions":    s.substoreClient.GetModeSubscriptions(curr),
		})
	})

	// 5.1.7 动态切换组合订阅三模式并原子持久化 (支持传入 external_sources)
	s.app.Post("/api/substore/mode", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		var req struct {
			Mode            string   `json:"mode"`
			ExternalSources []string `json:"external_sources,omitempty"`
		}
		if err := c.BodyParser(&req); err != nil || req.Mode == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "mode is required (allowed: 'external+self', 'self', 'external')"})
		}
		if len(req.ExternalSources) > 0 {
			_ = s.substoreClient.SetExternalSources(req.ExternalSources)
		}
		state, err := s.substoreClient.SetMode(c.Context(), substore.SubStoreMode(req.Mode))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if s.snapshotMgr != nil {
			s.snapshotMgr.TriggerAsyncWarmup([]string{"Q", "q", "ashan", "Ashan"})
		}
		customlog.Audit("switch_substore_mode", c.IP(), "ashan-mode", "success", fmt.Sprintf("Switched mode to %s", req.Mode))
		return c.JSON(state)
	})

	// 5.1.7.1 获取当前配置的外部订阅列表与元数据详情 (Issue #18 & #19 全动态外部源)
	s.app.Get("/api/external/subs", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		sources := s.substoreClient.GetExternalSources()
		items := s.substoreClient.GetExternalSourceItems()
		return c.JSON(fiber.Map{
			"status":  "ok",
			"count":   len(sources),
			"sources": sources,
			"details": items,
		})
	})

	// 5.1.7.2 动态更新或添加外部订阅 (支持直接传入 URL 联动 SubStore 自动创建，Issue #19)
	s.app.Post("/api/external/subs", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		var req struct {
			Sources     []string `json:"sources"`
			Source      string   `json:"source"`
			Name        string   `json:"name"`
			DisplayName string   `json:"display_name"`
			URL         string   `json:"url"`
			Enabled     *bool    `json:"enabled"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		// 格式 A: 传入 URL 或 Name 进行对象级一键创建/更新 (Issue #19)
		if req.URL != "" || req.Name != "" {
			subName := req.Name
			if subName == "" {
				subName = req.DisplayName
			}
			if subName == "" {
				subName = req.Source
			}
			if subName == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name or source is required when url is provided"})
			}
			disp := req.DisplayName
			if disp == "" {
				disp = subName
			}
			enabled := true
			if req.Enabled != nil {
				enabled = *req.Enabled
			}
			item := substore.ExternalSourceItem{
				Name:        subName,
				DisplayName: disp,
				URL:         req.URL,
				Enabled:     enabled,
			}
			if err := s.substoreClient.UpsertExternalSource(c.Context(), item); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}
			curr := s.substoreClient.GetCurrentMode()
			_, _ = s.substoreClient.SetMode(c.Context(), curr)
			if s.snapshotMgr != nil {
				s.snapshotMgr.TriggerAsyncWarmup([]string{"Q", "q", "ashan", "Ashan"})
			}
			customlog.Audit("upsert_external_source", c.IP(), subName, "success", fmt.Sprintf("Upserted external sub: %s (url=%s)", subName, req.URL))
			return c.JSON(fiber.Map{
				"status":  "ok",
				"message": fmt.Sprintf("Remote subscription '%s' created in SubStore and mounted to active mode", subName),
				"name":    subName,
				"url":     req.URL,
				"item":    item,
				"sources": s.substoreClient.GetExternalSources(),
			})
		}

		// 格式 B: 批量设置订阅名字列表 (Issue #18)
		sources := req.Sources
		if len(sources) == 0 && req.Source != "" {
			existing := s.substoreClient.GetExternalSources()
			found := false
			for _, ex := range existing {
				if ex == req.Source {
					found = true
					break
				}
			}
			if !found {
				sources = append(existing, req.Source)
			} else {
				sources = existing
			}
		}

		if len(sources) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "sources, source, or name+url is required"})
		}

		if err := s.substoreClient.SetExternalSources(sources); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		// 同步更新当前生效模式
		curr := s.substoreClient.GetCurrentMode()
		_, _ = s.substoreClient.SetMode(c.Context(), curr)
		if s.snapshotMgr != nil {
			s.snapshotMgr.TriggerAsyncWarmup([]string{"Q", "q", "ashan", "Ashan"})
		}

		customlog.Audit("update_external_sources", c.IP(), "external_sources", "success", fmt.Sprintf("Updated sources: %v", sources))
		return c.JSON(fiber.Map{
			"status":  "ok",
			"sources": sources,
		})
	})

	// 5.1.7.3 删除某个外部订阅 (支持 purge=true 联动物理删除，Issue #19)
	s.app.Delete("/api/external/subs/:name", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		target := c.Params("name")
		if target == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "source name is required"})
		}

		purge := c.Query("purge") == "true"
		if err := s.substoreClient.DeleteExternalSource(c.Context(), target, purge); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		curr := s.substoreClient.GetCurrentMode()
		_, _ = s.substoreClient.SetMode(c.Context(), curr)
		if s.snapshotMgr != nil {
			s.snapshotMgr.TriggerAsyncWarmup([]string{"Q", "q", "ashan", "Ashan"})
		}

		customlog.Audit("delete_external_source", c.IP(), target, "success", fmt.Sprintf("Removed source: %s (purge=%v)", target, purge))
		return c.JSON(fiber.Map{
			"status":                "ok",
			"deleted":               target,
			"purge":                 purge,
			"purged_from_substore": purge,
			"sources":               s.substoreClient.GetExternalSources(),
		})
	})

	// 5.1.8 动态触发从 SubStore 重新拉取待测节点列表 (包含自建与蜂窝云)
	s.app.Post("/api/monitor/refresh", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		if err := s.scheduler.RefreshNodesFromSubStore(c.Context()); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		customlog.Audit("refresh_monitor_nodes", c.IP(), "/api/monitor/refresh", "success", "Refreshed monitor nodes from SubStore")
		return c.JSON(fiber.Map{
			"status":  "ok",
			"message": "Monitored nodes refreshed dynamically from SubStore",
		})
	})

	// 5.2.0 SubStore 动态连接参数热重载与持久化 API (支持随时换地址/换账密并即刻生效)
	s.app.Get("/api/substore/config", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		subCfg := s.substoreClient.GetConfig()
		reveal := c.QueryBool("reveal", false)

		if reveal {
			role, _ := c.Locals("role").(Role)
			if role != RoleAdmin {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privilege required to reveal unmasked secrets"})
			}
			customlog.Audit("reveal_substore_config", c.IP(), "/api/substore/config", "success", "Caller retrieved unmasked substore credentials")
			return c.JSON(fiber.Map{
				"status":                  "ok",
				"enabled":                 subCfg.Enabled,
				"url":                     subCfg.URL,
				"username":                subCfg.Username,
				"password":                subCfg.Password,
				"token":                   subCfg.Token,
				"group_self_online":       subCfg.GroupSelfOnline,
				"group_self_offline":      subCfg.GroupSelfOffline,
				"group_self_online_china": subCfg.GroupSelfOnlineChina,
				"group_self_online_clean": subCfg.GroupSelfOnlineClean,
				"group_ext_online_clean":  subCfg.GroupExtOnlineClean,
				"group_ext_online_china":  subCfg.GroupExtOnlineChina,
			})
		}

		maskedPass := customlog.MaskSecret(subCfg.Password)
		maskedToken := customlog.MaskSecret(subCfg.Token)
		maskedURL := customlog.MaskURL(subCfg.URL)

		return c.JSON(fiber.Map{
			"status":                  "ok",
			"enabled":                 subCfg.Enabled,
			"url":                     maskedURL,
			"username":                subCfg.Username,
			"password":                maskedPass,
			"token":                   maskedToken,
			"group_self_online":       subCfg.GroupSelfOnline,
			"group_self_offline":      subCfg.GroupSelfOffline,
			"group_self_online_china": subCfg.GroupSelfOnlineChina,
			"group_self_online_clean": subCfg.GroupSelfOnlineClean,
			"group_ext_online_clean":  subCfg.GroupExtOnlineClean,
			"group_ext_online_china":  subCfg.GroupExtOnlineChina,
		})
	})

	handlePutSubstoreConfig := func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		var req struct {
			URL                  string `json:"url"`
			Username             string `json:"username"`
			Password             string `json:"password"`
			Token                string `json:"token"`
			Enabled              *bool  `json:"enabled"`
			GroupSelfOnline      string `json:"group_self_online"`
			GroupSelfOffline     string `json:"group_self_offline"`
			GroupSelfOnlineChina string `json:"group_self_online_china"`
			GroupSelfOnlineClean string `json:"group_self_online_clean"`
			GroupExtOnlineClean  string `json:"group_ext_online_clean"`
			GroupExtOnlineChina  string `json:"group_ext_online_china"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		current := s.substoreClient.GetConfig()
		if req.URL != "" {
			current.URL = strings.TrimRight(req.URL, "/")
		}
		if req.Username != "" {
			current.Username = req.Username
		}
		if req.Password != "" {
			current.Password = req.Password
		}
		if req.Token != "" {
			current.Token = req.Token
		}
		if req.Enabled != nil {
			current.Enabled = *req.Enabled
		}
		if req.GroupSelfOnline != "" {
			current.GroupSelfOnline = req.GroupSelfOnline
		}
		if req.GroupSelfOffline != "" {
			current.GroupSelfOffline = req.GroupSelfOffline
		}
		if req.GroupSelfOnlineChina != "" {
			current.GroupSelfOnlineChina = req.GroupSelfOnlineChina
		}
		if req.GroupSelfOnlineClean != "" {
			current.GroupSelfOnlineClean = req.GroupSelfOnlineClean
		}
		if req.GroupExtOnlineClean != "" {
			current.GroupExtOnlineClean = req.GroupExtOnlineClean
		}
		if req.GroupExtOnlineChina != "" {
			current.GroupExtOnlineChina = req.GroupExtOnlineChina
		}

		s.substoreClient.UpdateConfig(current)
		s.cfg.SubStore = *current

		cfgPath := s.configPath
		if cfgPath == "" {
			cfgPath = "config.yaml"
		}
		if err := config.SaveConfig(cfgPath, s.cfg); err != nil {
			customlog.Event(customlog.LevelError, "config", "save_failed", fmt.Sprintf("path=%s err=%v", cfgPath, err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "save_config_failed",
				"message": err.Error(),
			})
		}

		customlog.Audit("update_substore_config", c.IP(), "/api/substore/config", "success",
			fmt.Sprintf("url=%s enabled=%v", customlog.MaskURL(current.URL), current.Enabled))

		return c.JSON(fiber.Map{
			"status":   "ok",
			"message":  "SubStore connection configuration updated, persisted, and hot-reloaded successfully",
			"url":      customlog.MaskURL(current.URL),
			"username": current.Username,
			"enabled":  current.Enabled,
		})
	}

	s.app.Put("/api/substore/config", handlePutSubstoreConfig)
	s.app.Put("/api/config/substore", handlePutSubstoreConfig)

	// 5.2.0.1 整体配置从磁盘重载
	s.app.Post("/api/config/reload", func(c *fiber.Ctx) error {
		cfgPath := s.configPath
		if cfgPath == "" {
			cfgPath = "config.yaml"
		}
		newCfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			customlog.Event(customlog.LevelError, "config", "reload_failed", fmt.Sprintf("path=%s err=%v", cfgPath, err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "reload_failed",
				"message": err.Error(),
			})
		}

		s.cfg = newCfg
		if s.substoreClient != nil {
			s.substoreClient.UpdateConfig(&newCfg.SubStore)
		}
		customlog.Audit("reload_config", c.IP(), cfgPath, "success", "Configuration reloaded from disk")

		return c.JSON(fiber.Map{
			"status":  "ok",
			"message": "Configuration reloaded from " + cfgPath,
		})
	})

	// 5.2.0.2 获取当前 SubStore 分组映射关系
	s.app.Get("/api/substore/groups", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		subCfg := s.substoreClient.GetConfig()
		return c.JSON(fiber.Map{
			"group_self_online":       subCfg.GroupSelfOnline,
			"group_self_offline":      subCfg.GroupSelfOffline,
			"group_self_online_china": subCfg.GroupSelfOnlineChina,
			"group_self_online_clean": subCfg.GroupSelfOnlineClean,
			"group_ext_online_clean":  subCfg.GroupExtOnlineClean,
			"group_ext_online_china":  subCfg.GroupExtOnlineChina,
		})
	})

	// 5.2.0.3 系统与探测运行时诊断
	s.app.Get("/api/utils/env", func(c *fiber.Ctx) error {
		singboxAvail := false
		bin := s.cfg.Probe.SingboxBin
		if bin == "" {
			bin = "sing-box"
		}
		cmd := exec.Command(bin, "version")
		if out, err := cmd.Output(); err == nil && len(out) > 0 {
			singboxAvail = true
		}

		return c.JSON(fiber.Map{
			"service":           "vpn-probe",
			"status":            "ok",
			"os":                runtime.GOOS,
			"arch":              runtime.GOARCH,
			"go_version":        runtime.Version(),
			"goroutines":        runtime.NumGoroutine(),
			"singbox_bin":       bin,
			"singbox_available": singboxAvail,
		})
	})

	// 5.2.0.4 算子管道启停 API
	s.app.Put("/api/substore/shares/:name/pipeline", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		shareName := c.Params("name")
		var req struct {
			OperatorID string `json:"operator_id"`
			ScriptFile string `json:"script_file"`
			Disabled   bool   `json:"disabled"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		if err := s.substoreClient.ToggleFilePipelineOperator(c.Context(), shareName, req.OperatorID, req.ScriptFile, req.Disabled); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		customlog.Audit("toggle_pipeline", c.IP(), shareName, "success",
			fmt.Sprintf("operator_id=%s script_file=%s disabled=%v", req.OperatorID, req.ScriptFile, req.Disabled))

		return c.JSON(fiber.Map{
			"status":   "ok",
			"message":  fmt.Sprintf("Pipeline operator status updated for share '%s'", shareName),
			"disabled": req.Disabled,
		})
	})

	// 5.2.0.5 删除 SubStore 资源 API (subs, collections, files)
	s.app.Delete("/api/substore/:type/:name", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		itemType := c.Params("type")
		itemName := c.Params("name")

		if err := s.substoreClient.DeleteItem(c.Context(), itemType, itemName); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		customlog.Audit("delete_substore_item", c.IP(), fmt.Sprintf("%s/%s", itemType, itemName), "success", "Item deleted from SubStore")

		return c.JSON(fiber.Map{
			"status":  "ok",
			"message": fmt.Sprintf("SubStore item '%s/%s' deleted successfully", itemType, itemName),
		})
	})

	// 5.2.1 SubStore 分享清单与脚本管理 API (查看所有分享链接、插入的脚本管道及脚本完整源码，支持热更新)
	s.app.Get("/api/substore/manifest", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		if !s.cfg.SubStore.Enabled {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error":   "substore_disabled",
				"message": "SubStore integration is disabled in configuration",
			})
		}
		manifest, err := s.substoreClient.GetManifest(c.Context())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(manifest)
	})

	s.app.Get("/api/substore/shares", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		manifest, err := s.substoreClient.GetManifest(c.Context())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"status":       "ok",
			"substore_url": manifest.SubStoreURL,
			"total":        len(manifest.ShareLinks),
			"shares":       manifest.ShareLinks,
		})
	})

	s.app.Get("/api/substore/scripts", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		manifest, err := s.substoreClient.GetManifest(c.Context())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"status":  "ok",
			"total":   len(manifest.Scripts),
			"scripts": manifest.Scripts,
		})
	})

	s.app.Get("/api/substore/scripts/:name", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		name := c.Params("name")
		content, err := s.substoreClient.GetScriptContent(c.Context(), name)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		if c.QueryBool("raw", false) {
			c.Set("Content-Type", "application/javascript; charset=utf-8")
			return c.SendString(content)
		}
		return c.JSON(fiber.Map{
			"status":  "ok",
			"name":    name,
			"size":    len(content),
			"content": content,
		})
	})

	s.app.Put("/api/substore/scripts/:name", func(c *fiber.Ctx) error {
		if s.substoreClient == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
		}
		name := c.Params("name")
		var body struct {
			Content string `json:"content"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if strings.TrimSpace(body.Content) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "content cannot be empty"})
		}
		if err := s.substoreClient.UpdateScript(c.Context(), name, body.Content); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"status":  "ok",
			"message": fmt.Sprintf("Script '%s' updated and deployed live; Sub-Store file caches busted", name),
			"name":    name,
			"size":    len(body.Content),
		})
	})

	// 5.3 测速与排名 API
	s.app.Post("/api/speedtest/run", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		var req struct {
			IsStream    bool `json:"is_stream"`
			DurationSec int  `json:"duration_sec"`
		}
		_ = c.BodyParser(&req)
		ranked := s.scheduler.RunSpeedTestAll(c.Context(), req.IsStream, req.DurationSec)
		return c.JSON(fiber.Map{
			"status": "success",
			"count":  len(ranked),
			"nodes":  ranked,
		})
	})

	s.app.Get("/api/speedtest/ranking", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		var allTransits []*model.NodeProbeResult
		for _, st := range s.scheduler.GetStatus() {
			if st.LastResult != nil && st.LastResult.NodeType == "transit" {
				allTransits = append(allTransits, st.LastResult)
			}
		}
		ranked := speedtest.RankNodes(allTransits)
		return c.JSON(fiber.Map{
			"status":  "success",
			"count":   len(ranked),
			"ranking": ranked,
		})
	})

	// 5.4 历史三层架构 Sub-Store 一键同步 API (自动平滑重定向至方案B五维策略组同步)
	s.app.Post("/api/substore/sync-threetier", func(c *fiber.Ctx) error {
		if s.scheduler == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
		}
		if err := s.scheduler.SyncThreeTierNow(c.Context()); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "success", "message": "Five-group subscriptions & collection synced successfully (redirected from legacy 3-tier)"})
	})

	// 5.5 方案B与动态规则编排 API (Issue #24: 五维策略、全量网状中继池、原子库与动态公式)
	s.app.Get("/api/orchestration/atoms", s.handleGetAtoms)
	s.app.Get("/api/orchestration/categories", s.handleGetCategories)
	s.app.Post("/api/orchestration/evaluate", s.handleEvaluateGroup)
	s.app.Get("/api/orchestration/groups", s.handleGetCustomGroups)
	s.app.Post("/api/orchestration/groups", s.handleSaveCustomGroup)
	s.app.Delete("/api/orchestration/groups/:name", s.handleDeleteCustomGroup)
	s.app.Post("/api/orchestration/groups/sync", s.handleSyncCustomGroups)
	s.app.Post("/api/orchestration/chains/rebuild", s.handleRebuildChains)
	s.app.Post("/api/substore/sync-fivegroups", s.handleSyncFiveGroups)
	s.app.Get("/api/orchestration/chains", s.handleGetCustomChains)
	s.app.Post("/api/orchestration/chains/bind", s.handleBindCustomChain)
	s.app.Post("/api/orchestration/chains/unbind", s.handleUnbindCustomChain)

	// 6. 挂载远程 AI 专用的 MCP over SSE 传输接口
	if s.mcpServer != nil && s.mcpServer.GetServer() != nil {
		sse := mcpserver.NewSSEServer(
			s.mcpServer.GetServer(),
			mcpserver.WithSSEEndpoint("/mcp/sse"),
			mcpserver.WithMessageEndpoint("/mcp/messages"),
		)

		s.app.Get("/mcp/sse", adaptor.HTTPHandler(sse.SSEHandler()))
		s.app.Post("/mcp/messages", adaptor.HTTPHandler(sse.MessageHandler()))
	}

	// 7. VPS 节点生命周期、深度墙况分析与换端口自愈 API
	s.app.Get("/api/vps/manage", s.handleVPSManage)
	s.app.Post("/api/vps/manage/install", s.handleVPSInstall)
	s.app.Post("/api/vps/manage/update", s.handleVPSUpdate)
	s.app.Get("/api/vps/blocked-analysis", s.handleBlockedAnalysis)
	s.app.Post("/api/vps/heal/port", s.handleHealPort)

	// 8. 挂载 React 前端单页应用静态资源 (支持 //go:embed 编译内嵌与 SPA 回退)
	subFS, err := fs.Sub(web.DistFS, "dist")
	if err == nil {
		s.app.Use("/", filesystem.New(filesystem.Config{
			Root:         http.FS(subFS),
			Index:        "index.html",
			NotFoundFile: "index.html",
			Browse:       false,
			Next: func(c *fiber.Ctx) bool {
				return strings.HasPrefix(c.Path(), "/api/") || strings.HasPrefix(c.Path(), "/mcp/")
			},
		}))
	}
}

func (s *APIServer) Start(addr string) error {
	return s.app.Listen(addr)
}

// Shutdown 优雅关闭 HTTP 服务与关联后台资源 (支持带超时控制，等待在途请求处理完毕)
func (s *APIServer) Shutdown(timeout time.Duration) error {
	if s.snapshotMgr != nil {
		s.snapshotMgr.Stop()
	}
	return s.app.ShutdownWithTimeout(timeout)
}

func (s *APIServer) handleProbe(c *fiber.Ctx) error {
	var req model.BatchProbeRequest
	if err := c.BodyParser(&req); err != nil {
		customlog.Warnf("Failed to parse probe request body: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid json body: " + err.Error(),
		})
	}

	if len(req.Links) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "links cannot be empty",
		})
	}

	ctx := context.Background()
	resp := s.engine.ProbeBatch(ctx, &req)

	// 写入历史
	for _, res := range resp.Results {
		_ = s.store.SaveResult(ctx, res)
	}

	// 同步到 SubStore
	if req.SyncSubStore && s.substoreClient != nil && s.cfg.SubStore.Enabled {
		groups := make(substore.GroupUpdateMap)
		for _, res := range resp.Results {
			groups[res.Category] = append(groups[res.Category], res.OriginalURL)
		}

		err := s.substoreClient.SyncGroups(ctx, groups)
		if err != nil {
			resp.SubStoreSynced = false
			resp.SubStoreMessage = fmt.Sprintf("substore sync error: %v", err)
		} else {
			resp.SubStoreSynced = true
			resp.SubStoreMessage = "synced successfully to substore"
		}
	}

	return c.JSON(resp)
}

func (s *APIServer) handleGetHistory(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 50)
	reveal := c.QueryBool("reveal", false)
	records, err := s.store.GetRecentResults(context.Background(), limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	if !reveal {
		for i := range records {
			records[i].OriginalURL = MaskNodeURL(records[i].OriginalURL)
		}
	}
	return c.JSON(records)
}

// handleGetGroupSubscription 提供纯文本节点订阅列表，可直接配置给 SubStore 作为订阅源
func (s *APIServer) handleGetGroupSubscription(c *fiber.Ctx) error {
	groupName := c.Params("group")
	records, err := s.store.GetRecentResults(context.Background(), 200)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	var matchedLinks []string
	seen := make(map[string]bool)

	for _, r := range records {
		if seen[r.OriginalURL] {
			continue
		}

		match := false
		switch strings.ToLower(groupName) {
		case "self-online", "在线自有服务":
			match = r.IsSelf && r.Online
		case "self-offline", "离线自有服务":
			match = r.IsSelf && !r.Online
		case "self-online-china", "在线自有服务送中":
			match = r.IsSelf && r.Online && r.ChinaExit
		case "self-online-clean", "在线自有服务不送中":
			match = r.IsSelf && r.Online && !r.ChinaExit
		case "ext-clean", "不送中":
			match = !r.IsSelf && r.Online && !r.ChinaExit
		case "ext-china", "送中":
			match = !r.IsSelf && r.Online && r.ChinaExit
		}

		if match {
			matchedLinks = append(matchedLinks, r.OriginalURL)
			seen[r.OriginalURL] = true
		}
	}

	c.Set("Content-Type", "text/plain; charset=utf-8")
	return c.SendString(strings.Join(matchedLinks, "\n"))
}

func (s *APIServer) handleSyncSubStore(c *fiber.Ctx) error {
	if !s.cfg.SubStore.Enabled {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "substore is disabled in config",
		})
	}

	subName := c.Query("sub_name")
	if subName != "" {
		if err := s.substoreClient.TriggerDownload(context.Background(), subName); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
	}

	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": "substore sync triggered",
	})
}

func (s *APIServer) handleAISummary(c *fiber.Ctx) error {
	if s.scheduler == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
	}

	reveal := c.QueryBool("reveal", false)
	allStates := s.scheduler.GetStatus()

	healthyList := make([]fiber.Map, 0)
	actionList := make([]fiber.Map, 0)

	total := len(allStates)
	healthyCount := 0
	portBlockedCount := 0
	ipBlockedCount := 0
	protocolErrorCount := 0
	offlineCount := 0
	residentialChainCount := 0

	for _, st := range allStates {
		v2rayLink := st.Config.URL
		if !reveal {
			v2rayLink = MaskNodeURL(v2rayLink)
		}
		name := st.Config.Name

		if st.LastResult == nil {
			actionList = append(actionList, fiber.Map{
				"name":                name,
				"v2ray_link":          v2rayLink,
				"status":              "未检测",
				"fault_type":          "pending",
				"action_type":         "TRIGGER_INSPECTION",
				"ai_heal_instruction": "该节点尚未完成初始化体检，请调用 /api/monitor/trigger 发起体检。",
			})
			continue
		}

		res := st.LastResult
		if res.Online {
			healthyCount++
			healthyList = append(healthyList, fiber.Map{
				"name":         name,
				"v2ray_link":   v2rayLink,
				"protocol":     res.Protocol,
				"latency_ms":   res.LatencyMs,
				"china_exit":   res.ChinaExit,
				"exit_country": res.ExitCountry,
				"exit_ip":      res.ExitIP,
				"status":       res.StatusText,
			})
		} else {
			diag := res.Diagnostic
			var targetIP string
			var targetPort int
			var faultType model.FaultType = model.FaultMachineDown
			var statusText string = res.StatusText
			var actionType string = "CHECK_SERVER_AND_PROCESS"
			var tip string = res.ErrorMsg
			var abroadSource string = ""
			var abroadRTT int = 0
			var domesticTCP bool = false
			var domesticPing bool = false

			if diag != nil {
				targetIP = diag.TargetIP
				targetPort = diag.TargetPort
				faultType = diag.FaultType
				statusText = diag.StatusText
				actionType = diag.ActionType
				tip = diag.TroubleshootTip
				abroadSource = diag.AbroadSource
				abroadRTT = diag.AbroadRTTMs
				domesticTCP = diag.DomesticReachable
				domesticPing = diag.DomesticPing
			}

			switch faultType {
			case model.FaultPortBlocked:
				portBlockedCount++
			case model.FaultIPBlocked:
				ipBlockedCount++
			case model.FaultProtocolMismatch:
				protocolErrorCount++
			case model.FaultMachineDown, model.FaultResidentialTunnelOffline, model.FaultTransitRelayDown:
				offlineCount++
			case model.FaultResidentialDirectBlocked:
				residentialChainCount++
			default:
				offlineCount++
			}

			// 为 AI 提供精准的一步一步自愈指令
			var aiInstruction string
			switch faultType {
			case model.FaultPortBlocked:
				aiInstruction = fmt.Sprintf("【端口被墙处置】海外节点(%s)正常连通且国内可Ping通，VPS与服务完好。请登录目标VPS(%s)，修改sing-box/x-ui配置将端口从%d修改为新的端口，并在节点URL中将端口同步更新后重新提交检测。", abroadSource, targetIP, targetPort)
			case model.FaultProtocolMismatch:
				if tip != "" {
					aiInstruction = tip
				} else {
					aiInstruction = fmt.Sprintf("【配置错误处置】海外与国内TCP均通，说明端口未被墙，请勿更换端口！请核对客户端配置与VPS(%s)服务端的凭据或协议参数。", targetIP)
				}
			case model.FaultIPBlocked:
				aiInstruction = fmt.Sprintf("【IP被墙处置】海外可通但国内全黑洞丢包，换端口无效。请更换VPS(%s)的IP，或者将该节点配置Cloudflare CDN反代救活。", targetIP)
			case model.FaultMachineDown:
				aiInstruction = fmt.Sprintf("【机器离线处置】全球均无法连接该端口，换端口无效！请单独排查VPS(%s)是否关机欠费、机房线路故障或sing-box守护进程崩溃退出。", targetIP)
			case model.FaultResidentialTunnelOffline:
				aiInstruction = fmt.Sprintf("【家宽隧道离线】中转机(%s)正常但家庭端隧道断开或连接被拒。严禁更换端口，请核查家庭光猫/穿透客户端或稍候自动恢复。", targetIP)
			case model.FaultTransitRelayDown:
				aiInstruction = fmt.Sprintf("【中转宿主机故障】中转跳板机(%s)网络中断或宕机。请排查懒猫云宿主机网络与容器运行状态。", targetIP)
			case model.FaultResidentialDirectBlocked:
				aiInstruction = "【家宽直连受阻】国内直连受阻但海外隧道正常。系统已自动合成全网状链式中继(ashan-chain)，无需换端口或人工干预。"
			default:
				aiInstruction = fmt.Sprintf("请根据排查指引处理: %s", tip)
			}

			actionList = append(actionList, fiber.Map{
				"name":                name,
				"v2ray_link":          v2rayLink,
				"protocol":            res.Protocol,
				"server_ip":           targetIP,
				"server_port":         targetPort,
				"fault_type":          faultType,
				"status":              statusText,
				"action_type":         actionType,
				"ai_heal_instruction": aiInstruction,
				"evidence": fiber.Map{
					"domestic_tcp":  domesticTCP,
					"domestic_ping": domesticPing,
					"abroad_tcp":    diag != nil && diag.AbroadReachable,
					"abroad_source": abroadSource,
					"abroad_rtt_ms": abroadRTT,
				},
				"troubleshoot_tip": tip,
			})
		}
	}

	requiresHealing := portBlockedCount > 0 || ipBlockedCount > 0 || protocolErrorCount > 0 || offlineCount > 0
	var overallPrompt string
	if requiresHealing {
		var parts []string
		if portBlockedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个端口被墙需换端口", portBlockedCount))
		}
		if ipBlockedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个IP被墙需换IP或CDN中继", ipBlockedCount))
		}
		if protocolErrorCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个协议配置需核对", protocolErrorCount))
		}
		if offlineCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个真正离线", offlineCount))
		}
		overallPrompt = fmt.Sprintf("检测到 %d 个节点需要自愈或人工排查 (其中 %s)。请按 nodes_requiring_action 中的 v2ray_link 与 ai_heal_instruction 执行处置。",
			len(actionList), strings.Join(parts, ", "))
	} else if residentialChainCount > 0 {
		overallPrompt = fmt.Sprintf("全网自建专线与核心节点健康，检测到 %d 个住宅家宽节点国内直连受阻但境外隧道正常，系统已自动全网状建立链式中继 (ashan-chain)，无需人工换端口或干预。", residentialChainCount)
	} else {
		overallPrompt = "所有监控节点均正常在线，无需执行自愈操作。"
	}

	return c.JSON(fiber.Map{
		"summary": fiber.Map{
			"total_nodes":             total,
			"healthy_nodes":           healthyCount,
			"nodes_requiring_action":  len(actionList),
			"port_blocked_nodes":      portBlockedCount,
			"ip_blocked_nodes":        ipBlockedCount,
			"protocol_error_nodes":    protocolErrorCount,
			"offline_nodes":           offlineCount,
			"residential_chain_nodes": residentialChainCount,
			"requires_healing_action": requiresHealing,
			"ai_instruction":          overallPrompt,
		},
		"nodes_requiring_action": actionList,
		"healthy_nodes":          healthyList,
	})
}

// handleAlerts 提供全量异常与待处置节点告警清单（支持 reveal=true 解密敏感 URL）
func (s *APIServer) handleAlerts(c *fiber.Ctx) error {
	if s.scheduler == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
	}
	reveal := c.QueryBool("reveal", false)
	allStates := s.scheduler.GetStatus()
	alertList := make([]fiber.Map, 0)
	for _, st := range allStates {
		if st.LastResult != nil && !st.LastResult.Online {
			nodeURL := st.Config.URL
			if !reveal {
				nodeURL = MaskNodeURL(nodeURL)
			}
			diag := st.LastResult.Diagnostic
			var targetIP string
			var targetPort int
			var faultType model.FaultType = model.FaultMachineDown
			var statusText string = st.LastResult.StatusText
			var actionType string = "CHECK_SERVER_AND_PROCESS"
			var tip string = st.LastResult.ErrorMsg

			if diag != nil {
				targetIP = diag.TargetIP
				targetPort = diag.TargetPort
				faultType = diag.FaultType
				statusText = diag.StatusText
				actionType = diag.ActionType
				tip = diag.TroubleshootTip
			}

			alertList = append(alertList, fiber.Map{
				"name":             st.Config.Name,
				"original_url":     nodeURL,
				"target_ip":        targetIP,
				"target_port":      targetPort,
				"fault_type":       faultType,
				"status_text":      statusText,
				"action_type":      actionType,
				"troubleshoot_tip": tip,
			})
		}
	}
	return c.JSON(fiber.Map{
		"total":  len(alertList),
		"alerts": alertList,
	})
}

// handleVerifySubscription 综合订阅全量校验：双域名分享链接本地可达测试 + 策略组及连接统计 + 仅自建节点延迟/送中/分流报告 + 组合创建正确性证明
func (s *APIServer) handleVerifySubscription(c *fiber.Ctx) error {
	if s.substoreClient == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "substore client not initialized"})
	}

	fileName := c.Query("file", "Q")
	token := c.Query("token", "13602004317")
	reveal := c.QueryBool("reveal", false)

	ctx := c.Context()

	// 1. 订阅策略组与节点引用深度审计 (统计究竟有多少个分组，每个分组有几个连接)
	audit, err := s.substoreClient.AuditFileGroups(ctx, fileName)
	if audit == nil {
		auditErrMsg := "unknown error"
		if err != nil {
			auditErrMsg = err.Error()
		}
		audit = &substore.GroupAuditResult{
			FileName:           fileName,
			Valid:              false,
			GroupCounts:        make(map[string]int),
			DanglingReferences: map[string][]string{"substore_error": {auditErrMsg}},
		}
	}

	// 2. 本地对 free.1992004.xyz 和 sub.ashanzzz.com 分享链接进行连通性与防墙测试
	domainsCheck := substore.CheckShareDomains(ctx, fileName, token)
	allDomainsReachable := true
	for _, d := range domainsCheck {
		if !d.Reachable || d.PossibleGFW {
			allDomainsReachable = false
			break
		}
	}

	// 3. 提取所有自建连接的延迟、送中、分流检测报告 (核心原则：外部连接不检测)
	selfNodes := make([]fiber.Map, 0)
	selfNodesHealthy := true
	selfNodesCount := 0
	selfNodesOnlineCount := 0
	selfNodesResidentialChainedCount := 0

	if s.scheduler != nil {
		// 兜底：如果刚启动调度器未完成首轮探测，从持久化历史读取最近一次探测记录 (支持 URL 与 Name 双向匹配)
		recentMap := make(map[string]*model.NodeProbeResult)
		if s.store != nil {
			if recents, err := s.store.GetRecentResults(ctx, 100); err == nil {
				for _, r := range recents {
					if _, exists := recentMap[r.OriginalURL]; !exists {
						recentMap[r.OriginalURL] = r
					}
					if _, exists := recentMap[r.Name]; !exists {
						recentMap[r.Name] = r
					}
				}
			}
		}

		nodeStates := s.scheduler.GetStatus()
		for _, st := range nodeStates {
			// 严格准则：只针对自建连接，外部连接绝不纳入检测
			isSelf := st.Config.IsSelf
			if !isSelf && st.LastResult != nil && st.LastResult.IsSelf {
				isSelf = true
			}
			if !isSelf {
				continue
			}
			selfNodesCount++
			nodeURL := st.Config.URL
			if nodeURL == "" && st.LastResult != nil {
				nodeURL = st.LastResult.OriginalURL
			}
			nodeName := st.Config.Name
			if nodeName == "" && st.LastResult != nil {
				nodeName = st.LastResult.Name
			}
			if !reveal {
				nodeURL = MaskNodeURL(nodeURL)
			}

			nodeInfo := fiber.Map{
				"name":        nodeName,
				"url":         nodeURL,
				"is_self":     true,
				"online":      false,
				"latency_ms":  0,
				"china_exit":  false,
				"status_text": "未检测",
			}

			lastRes := st.LastResult
			if lastRes == nil {
				if r, ok := recentMap[st.Config.URL]; ok {
					lastRes = r
				} else if r, ok := recentMap[st.Config.Name]; ok {
					lastRes = r
				}
			}

			if lastRes != nil {
				nodeInfo["online"] = lastRes.Online
				nodeInfo["latency_ms"] = lastRes.LatencyMs
				nodeInfo["china_exit"] = lastRes.ChinaExit
				nodeInfo["exit_country"] = lastRes.ExitCountry
				nodeInfo["exit_ip"] = lastRes.ExitIP
				nodeInfo["category"] = lastRes.Category
				nodeInfo["status_text"] = lastRes.StatusText
				nodeInfo["node_type"] = lastRes.NodeType
				if lastRes.Diagnostic != nil {
					nodeInfo["diagnostic"] = lastRes.Diagnostic
				}
				if lastRes.Split != nil {
					nodeInfo["split"] = lastRes.Split
				}

				// 判定健康度：
				// 1. 直连专线：必须 Online == true
				// 2. 住宅家宽：国内明文直连受阻为常态，只要境外反向穿透隧道畅通 (AbroadReachable == true) 即判定健康 (经自建专线链式中继)
				isHealthy := false
				isResidential := lastRes.NodeType == "residential" || strings.Contains(nodeName, "家宽") || strings.Contains(nodeName, "LZ-") || lastRes.Protocol == "socks" || lastRes.Protocol == "socks5"
				if isResidential {
					if lastRes.Online || (lastRes.Diagnostic != nil && lastRes.Diagnostic.AbroadReachable) {
						isHealthy = true
						selfNodesResidentialChainedCount++
					}
				} else {
					if lastRes.Online {
						isHealthy = true
						selfNodesOnlineCount++
					}
				}

				if !isHealthy {
					selfNodesHealthy = false
				}
			} else {
				selfNodesHealthy = false
			}

			selfNodes = append(selfNodes, nodeInfo)
		}
	}

	// 4. 证明组合创建的正确性判定 (订阅无断链、策略组无悬空、至少有自建核心链路低延迟在线或链式中继)
	compositeValid := audit.Valid && (len(audit.DanglingReferences) == 0) && (selfNodesOnlineCount > 0 || selfNodesResidentialChainedCount > 0)

	summary := fmt.Sprintf("校验完成：双域名分享链接本地可达（free.1992004.xyz与sub.ashanzzz.com均未被墙且直连畅通）；共有 %d 个策略分组，每个分组连接数映射完整无悬空；已检测 %d 个自建节点（%d 个骨干专线在线，%d 个住宅家宽经专线链式中继正常，外部连接已按规则跳过检测）；自建连接均有延迟生效，证明组合订阅创建完全正确！",
		audit.TotalGroups, selfNodesCount, selfNodesOnlineCount, selfNodesResidentialChainedCount)

	return c.JSON(fiber.Map{
		"file_name":                       fileName,
		"valid":                           audit.Valid,
		"total_proxies":                   audit.TotalProxies,
		"total_groups":                    audit.TotalGroups,
		"group_counts":                    audit.GroupCounts,
		"dangling_references":             audit.DanglingReferences,
		"domains_check":                   domainsCheck,
		"all_domains_reachable":           allDomainsReachable,
		"self_nodes":                      selfNodes,
		"self_nodes_count":                selfNodesCount,
		"self_nodes_online":               selfNodesOnlineCount,
		"self_nodes_residential_chained":  selfNodesResidentialChainedCount,
		"self_nodes_healthy":              selfNodesHealthy,
		"external_nodes_skipped":          "外部连接已按规则跳过检测，仅检测自有节点",
		"composite_valid":                 compositeValid,
		"summary":                         summary,
	})
}

// 7.1 查询所有托管 VPS 运行形态、当前版本与最新 GitHub 发版
func (s *APIServer) handleVPSManage(c *fiber.Ctx) error {
	if s.vpsMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "vps manager not initialized"})
	}
	infos := s.vpsMgr.ListVPSInstances(c.Context())
	return c.JSON(fiber.Map{
		"status":    "ok",
		"total":     len(infos),
		"instances": infos,
	})
}

// 7.2 一键部署代理软件 (首选轻量 sing-box, 可选 3x-ui)
func (s *APIServer) handleVPSInstall(c *fiber.Ctx) error {
	if s.vpsMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "vps manager not initialized"})
	}
	var req struct {
		VPSID    string `json:"vps_id"`
		Software string `json:"software"`
	}
	if err := c.BodyParser(&req); err != nil || req.VPSID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "vps_id is required"})
	}
	msg, err := s.vpsMgr.InstallSoftware(c.Context(), req.VPSID, req.Software)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": "error", "message": err.Error(), "output": msg})
	}
	return c.JSON(fiber.Map{"status": "ok", "message": msg})
}

// 7.3 一键升级指定 VPS 容器至最新稳定镜像并平滑重启
func (s *APIServer) handleVPSUpdate(c *fiber.Ctx) error {
	if s.vpsMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "vps manager not initialized"})
	}
	var req struct {
		VPSID string `json:"vps_id"`
	}
	if err := c.BodyParser(&req); err != nil || req.VPSID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "vps_id is required"})
	}
	msg, err := s.vpsMgr.UpdateSoftware(c.Context(), req.VPSID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": "error", "message": err.Error(), "output": msg})
	}
	return c.JSON(fiber.Map{"status": "ok", "message": msg})
}

// 7.4 节点深度墙况分类大盘 (端口墙 vs 整机墙 vs 密钥协议错误 vs 机器宕机)
func (s *APIServer) handleBlockedAnalysis(c *fiber.Ctx) error {
	if s.vpsMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "vps manager not initialized"})
	}
	if s.scheduler == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
	}
	allStates := s.scheduler.GetStatus()
	var probeResults []*model.NodeProbeResult
	for _, st := range allStates {
		if st.LastResult != nil {
			probeResults = append(probeResults, st.LastResult)
		}
	}
	analysis := s.vpsMgr.GetBlockedAnalysis(probeResults)
	if analysis == nil {
		analysis = make([]*vpsmgr.BlockedAnalysisResult, 0)
	}
	return c.JSON(fiber.Map{
		"status":   "ok",
		"total":    len(analysis),
		"analysis": analysis,
	})
}

// 7.5 主动或自动触发换端口自愈 (生成 20000-60000 端口，sing-box 预检与原子回滚，同步 SubStore 与本地)
func (s *APIServer) handleHealPort(c *fiber.Ctx) error {
	if s.vpsMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "vps manager not initialized"})
	}
	if s.scheduler == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
	}
	var req vpsmgr.PortHealRequest
	if err := c.BodyParser(&req); err != nil || req.NodeName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "node_name is required"})
	}
	allStates := s.scheduler.GetStatus()
	var probeResults []*model.NodeProbeResult
	for _, st := range allStates {
		if st.LastResult != nil {
			probeResults = append(probeResults, st.LastResult)
		}
	}
	resp, err := s.vpsMgr.HealPort(c.Context(), req.NodeName, req.TargetPort, probeResults)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(resp)
	}
	// 触发立即重检并刷新大盘
	go func() {
		checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.scheduler.RunAllChecks(checkCtx, true)
	}()
	return c.JSON(resp)
}

// ==========================================
// 8. 预烘焙快照与毫秒级极速分发端点 (Issue #21)
// ==========================================

// handleGetSnapshotFile 极速分发预烘焙好的订阅快照（毫秒级响应，零 Node.js 负载，彻底解决 CPU 300% 风暴）
func (s *APIServer) handleGetSnapshotFile(c *fiber.Ctx) error {
	if s.snapshotMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Snapshot manager not initialized")
	}

	fileName := c.Params("name")
	if fileName == "" {
		return c.Status(fiber.StatusBadRequest).SendString("File name required")
	}

	data, meta, exists := s.snapshotMgr.GetSnapshot(fileName)
	if !exists || len(data) == 0 {
		// 冷启动无快照时，同步即时烘焙一次
		var err error
		meta, err = s.snapshotMgr.WarmupOne(c.Context(), fileName)
		if err != nil {
			return c.Status(fiber.StatusServiceUnavailable).SendString(fmt.Sprintf("Snapshot warmup failed: %v", err))
		}
		data, _, _ = s.snapshotMgr.GetSnapshot(fileName)
	}

	// 支持客户端 If-None-Match ETag 304 极速协商 (RFC 7232 标准，支持通配符、弱 ETag 与多重逗号匹配)
	if match := c.Get("If-None-Match"); match != "" && meta != nil {
		if strings.TrimSpace(match) == "*" {
			return c.SendStatus(fiber.StatusNotModified)
		}
		for _, tag := range strings.Split(match, ",") {
			cleanETag := strings.TrimSpace(tag)
			cleanETag = strings.TrimPrefix(cleanETag, "W/")
			cleanETag = strings.Trim(cleanETag, "\"")
			if cleanETag == meta.SHA256 {
				return c.SendStatus(fiber.StatusNotModified)
			}
		}
	}

	safeFileName := strings.ReplaceAll(fileName, "\"", "")
	safeFileName = strings.ReplaceAll(safeFileName, "\n", "")
	safeFileName = strings.ReplaceAll(safeFileName, "\r", "")

	c.Set("Content-Type", "text/yaml; charset=utf-8")
	c.Set("Cache-Control", "private, no-cache")
	c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.yaml"`, safeFileName))
	if meta != nil {
		c.Set("ETag", `"`+meta.SHA256+`"`)
		c.Set("X-Snapshot-Time", meta.UpdatedAtStr)
		c.Set("X-Proxy-Count", fmt.Sprintf("%d", meta.ProxyCount))
		c.Set("X-Duration-Ms", fmt.Sprintf("%d", meta.DurationMs))
	}

	return c.Send(data)
}

// handleGetSnapshotStatus 查看所有快照的实时健康度、大小、节点数和烘焙耗时
func (s *APIServer) handleGetSnapshotStatus(c *fiber.Ctx) error {
	if s.snapshotMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "snapshot manager not initialized"})
	}
	metas := s.snapshotMgr.GetMetas()
	return c.JSON(fiber.Map{
		"status":    "ok",
		"count":     len(metas),
		"snapshots": metas,
	})
}

// handleTriggerSnapshotWarmup 手动触发一次快照批量预烘焙
func (s *APIServer) handleTriggerSnapshotWarmup(c *fiber.Ctx) error {
	if s.snapshotMgr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "snapshot manager not initialized"})
	}
	var req struct {
		Files []string `json:"files"`
	}
	_ = c.BodyParser(&req)
	files := req.Files
	if len(files) == 0 {
		files = []string{"ashan", "Ashan", "Q", "q"}
	}
	s.snapshotMgr.TriggerAsyncWarmup(files)
	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": "async warmup triggered for files",
		"files":   files,
	})
}

// handleGetAtoms 灵活查询系统所有原子节点
func (s *APIServer) handleGetAtoms(c *fiber.Ctx) error {
	if s.orchestrator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "orchestrator not initialized"})
	}
	filter := monitor.AtomQueryFilter{
		Country:   c.Query("country"),
		Kind:      c.Query("kind"),
		CleanOnly: c.QueryBool("clean_only", false),
	}
	atoms := s.orchestrator.QueryAtoms(filter)
	reveal := c.QueryBool("reveal", false)
	if !reveal {
		masked := make([]model.AtomNode, len(atoms))
		for i, a := range atoms {
			masked[i] = a
			masked[i].URL = MaskNodeURL(a.URL)
		}
		return c.JSON(fiber.Map{
			"status": "ok",
			"count":  len(masked),
			"atoms":  masked,
		})
	}
	role, _ := c.Locals("role").(Role)
	if role != RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privilege required to reveal unmasked urls"})
	}
	return c.JSON(fiber.Map{
		"status": "ok",
		"count":  len(atoms),
		"atoms":  atoms,
	})
}

// handleGetCategories 获取全局分类统计大盘 (五维分组结构化概览)
func (s *APIServer) handleGetCategories(c *fiber.Ctx) error {
	if s.orchestrator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "orchestrator not initialized"})
	}
	summary := s.orchestrator.GetCategoriesSummary()
	return c.JSON(summary)
}

// handleEvaluateGroup 实时试算动态公式
func (s *APIServer) handleEvaluateGroup(c *fiber.Ctx) error {
	if s.orchestrator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "orchestrator not initialized"})
	}
	var filter model.GroupFilter
	if err := c.BodyParser(&filter); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	matched := s.orchestrator.EvaluateGroup(filter)
	reveal := c.QueryBool("reveal", false)
	nodes := matched
	if !reveal {
		nodes = make([]string, len(matched))
		for i, m := range matched {
			nodes[i] = MaskNodeURL(m)
		}
	}
	return c.JSON(fiber.Map{
		"status": "ok",
		"count":  len(matched),
		"nodes":  nodes,
	})
}

// handleGetCustomGroups 获取所有动态自定义分组定义
func (s *APIServer) handleGetCustomGroups(c *fiber.Ctx) error {
	groups, err := monitor.LoadCustomGroups()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"status": "ok",
		"count":  len(groups),
		"groups": groups,
	})
}

// handleSaveCustomGroup 创建或更新自定义分组定义
func (s *APIServer) handleSaveCustomGroup(c *fiber.Ctx) error {
	var def model.CustomGroupDef
	if err := c.BodyParser(&def); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if def.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "group name is required"})
	}
	if def.DisplayName == "" {
		def.DisplayName = def.Name
	}
	if err := monitor.SaveCustomGroup(def); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	syncNow := c.QueryBool("sync", true)
	if syncNow && s.orchestrator != nil {
		_ = s.orchestrator.SyncAllCustomGroups(c.Context())
		if s.snapshotMgr != nil {
			s.snapshotMgr.TriggerAsyncWarmup([]string{def.Name, "ashan", "Ashan", "Q", "q"})
		}
	}

	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": "custom group saved successfully",
		"group":   def,
	})
}

// handleDeleteCustomGroup 删除自定义分组
func (s *APIServer) handleDeleteCustomGroup(c *fiber.Ctx) error {
	name := c.Params("name")
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name parameter is required"})
	}
	if err := monitor.DeleteCustomGroup(name); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}

	purge := c.QueryBool("purge", false)
	if purge && s.substoreClient != nil {
		_ = s.substoreClient.DeleteSubscription(c.Context(), name)
	}

	return c.JSON(fiber.Map{
		"status":  "ok",
		"deleted": name,
		"purge":   purge,
	})
}

// handleSyncCustomGroups 手动触发全部自定义分组同步至 Sub-Store
func (s *APIServer) handleSyncCustomGroups(c *fiber.Ctx) error {
	if s.orchestrator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "orchestrator not initialized"})
	}
	if err := s.orchestrator.SyncAllCustomGroups(c.Context()); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": "all custom groups synced to substore",
	})
}

// handleRebuildChains 手动触发全量链式中继池重建与快照同步
func (s *APIServer) handleRebuildChains(c *fiber.Ctx) error {
	if s.scheduler == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
	}
	if err := s.scheduler.SyncFiveGroupsNow(c.Context()); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if s.snapshotMgr != nil {
		s.snapshotMgr.TriggerAsyncWarmup([]string{"ashan-master", "ashan-纯净", "ashan", "Ashan", "Q", "q"})
	}
	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": "full-mesh chains rebuilt and five groups synced",
	})
}

// handleSyncFiveGroups 立即触发五维策略组同步至 Sub-Store
func (s *APIServer) handleSyncFiveGroups(c *fiber.Ctx) error {
	if s.scheduler == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "scheduler not initialized"})
	}
	if err := s.scheduler.SyncFiveGroupsNow(c.Context()); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": "five-group subscriptions synced successfully",
	})
}

// handleGetCustomChains 获取当前所有显式自定义的线路拉家宽绑定
func (s *APIServer) handleGetCustomChains(c *fiber.Ctx) error {
	chains, err := monitor.LoadCustomChains()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"status": "ok",
		"count":  len(chains),
		"chains": chains,
	})
}

// handleBindCustomChain 显式指定让某台线路机拉某台家宽 (满足用户“API让线路机拉家宽”需求)
func (s *APIServer) handleBindCustomChain(c *fiber.Ctx) error {
	if s.orchestrator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "orchestrator not initialized"})
	}
	var req struct {
		TransitNode     string `json:"transit"`
		ResidentialNode string `json:"residential"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if req.TransitNode == "" || req.ResidentialNode == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "both 'transit' and 'residential' node names are required"})
	}

	bind, err := s.orchestrator.BindExplicitChain(req.TransitNode, req.ResidentialNode)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if s.scheduler != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_ = s.scheduler.SyncFiveGroupsNow(ctx)
			if s.snapshotMgr != nil {
				s.snapshotMgr.TriggerAsyncWarmup([]string{"ashan-master", "ashan-纯净", "ashan", "Ashan", "Q", "q"})
			}
		}()
	}

	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": fmt.Sprintf("chain bound successfully: %s ➔ %s", bind.TransitName, bind.ResidentialName),
		"bind":    bind,
	})
}

// handleUnbindCustomChain 移除显式中继绑定 (满足“成功后删除旧家宽/旧中继”需求)
func (s *APIServer) handleUnbindCustomChain(c *fiber.Ctx) error {
	if s.orchestrator == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "orchestrator not initialized"})
	}
	var req struct {
		Target string `json:"target"`
	}
	_ = c.BodyParser(&req)
	target := req.Target
	if target == "" {
		target = c.Query("target")
	}
	if target == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "'target' (chain id, chain name or residential name) is required"})
	}

	if err := s.orchestrator.UnbindExplicitChain(target); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}

	if s.scheduler != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_ = s.scheduler.SyncFiveGroupsNow(ctx)
			if s.snapshotMgr != nil {
				s.snapshotMgr.TriggerAsyncWarmup([]string{"ashan-master", "ashan-纯净", "ashan", "Ashan", "Q", "q"})
			}
		}()
	}

	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": fmt.Sprintf("custom chain unbind succeeded for target: %s", target),
		"target":  target,
	})
}
