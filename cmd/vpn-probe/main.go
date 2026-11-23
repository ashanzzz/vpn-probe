package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"vpn-probe/internal/api"
	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/mcp"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/probe"
	"vpn-probe/internal/rules"
	"vpn-probe/internal/store"
	"vpn-probe/internal/substore"
	"vpn-probe/internal/vpsmgr"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "server":
		runServer(os.Args[2:])
	case "mcp":
		runMCPStdio(os.Args[2:])
	case "probe":
		runProbeCLI(os.Args[2:])
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Print(`vpn-probe: 代理链接检测、送中研判、MCP 与 SubStore 4分组自愈系统

用法:
  vpn-probe server [--config path]        启动 REST API 与远程 MCP SSE 网关
  vpn-probe mcp [--config path]           启动 本地标准输入输出(stdio) MCP Server
  vpn-probe probe [--self] <proxy_link>   命令行快速单测一条链接
`)
}

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "配置文件路径")
	allowInsecure := fs.Bool("allow-insecure", false, "允许以无鉴权免密模式启动 (仅供离线单机测试)")
	_ = fs.Parse(args)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("Load config error: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志系统 (支持控制台 + 本地文件落盘)
	_, _ = logger.Init(cfg.Logging.Level, cfg.Logging.FilePath)
	logger.Infof("=== vpn-probe Server Starting ===")
	logger.Infof("Config loaded from: %s", *configPath)
	logger.Infof("Log file: %s (level: %s)", cfg.Logging.FilePath, cfg.Logging.Level)

	// 生产鉴权硬性校验 (Owner 指令 A.3：未设置密码时拒绝启动)
	insecureEnv := os.Getenv("VPN_PROBE_INSECURE") == "true"
	if cfg.Server.AuthToken == "" && cfg.Server.ReadToken == "" && cfg.Server.AdminToken == "" && !*allowInsecure && !insecureEnv {
		logger.Errorf("FATAL: No authentication token configured (auth_token/read_token/admin_token). Refusing to start in unauthenticated mode.")
		logger.Errorf("Please set server.auth_token in config.yaml or pass environment variable VPN_PROBE_AUTH_TOKEN.")
		os.Exit(1)
	}

	// 探针引擎预检：检查 sing-box 可用性
	bin := cfg.Probe.SingboxBin
	if bin == "" {
		bin = "sing-box"
	}
	checkCmd := exec.Command(bin, "version")
	if out, err := checkCmd.Output(); err != nil {
		logger.Warnf("⚠️  [ENGINE PRE-CHECK] sing-box binary '%s' execution test failed: %v. Probes will report probe_engine_error until installed.", bin, err)
	} else {
		logger.Infof("✅ [ENGINE PRE-CHECK] sing-box binary '%s' verified (%s)", bin, strings.TrimSpace(strings.Split(string(out), "\n")[0]))
	}

	st, err := store.NewStore("./data/vpn-probe.db")
	if err != nil {
		logger.Errorf("Init store error: %v", err)
		os.Exit(1)
	}
	defer st.Close()
	logger.Infof("SQLite database initialized at ./data/vpn-probe.db")

	engine := probe.NewEngine(cfg)
	subClient := substore.NewClient(&cfg.SubStore)
	if cfg.SubStore.Enabled {
		logger.Infof("SubStore sync enabled: %s", cfg.SubStore.URL)
	} else {
		logger.Infof("SubStore sync disabled (can be enabled in config.yaml)")
	}

	vpsMgr := vpsmgr.NewManager(cfg, subClient, *configPath)
	vpsMgr.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		return engine.ProbeOne(ctx, link, true, false, 8)
	})

	sched := monitor.NewScheduler(cfg, engine, st, subClient)
	sched.SetVPSManager(vpsMgr)
	if cfg.Monitor.Enabled {
		sched.Start(context.Background())
	}
	defer sched.Stop()

	rulesMgr, err := rules.NewManager("./data/custom_rules.json")
	if err != nil {
		logger.Warnf("Init custom rules manager: %v", err)
	}

	mcpSrv := mcp.NewMCPServer(cfg, engine, st, subClient)
	apiSrv := api.NewAPIServer(cfg, engine, st, subClient, mcpSrv, sched, rulesMgr)
	apiSrv.SetConfigPath(*configPath)
	apiSrv.SetVPSManager(vpsMgr)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.Infof("REST API listening on http://%s", addr)
	logger.Infof("MCP SSE listening on http://%s/mcp/sse", addr)
	logger.Infof("Health check available at http://%s/health", addr)

	// 注册信号监听，实现生产级容器优雅停机 (SIGINT/SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Infof("Received termination signal %v, initiating graceful shutdown...", sig)
		if sched != nil {
			sched.Stop()
		}
		if err := apiSrv.Shutdown(10 * time.Second); err != nil {
			logger.Warnf("Server graceful shutdown warning: %v", err)
		}
	}()

	if err := apiSrv.Start(addr); err != nil {
		logger.Infof("Server stopped: %v", err)
	}
}

func runMCPStdio(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "配置文件路径")
	_ = fs.Parse(args)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		os.Exit(1)
	}

	// stdio 模式下，日志只输出到文件，不能写 stdout，避免污染 MCP 管道
	_, _ = logger.Init("info", cfg.Logging.FilePath)

	st, err := store.NewStore("./data/vpn-probe.db")
	if err != nil {
		os.Exit(1)
	}
	defer st.Close()

	engine := probe.NewEngine(cfg)
	subClient := substore.NewClient(&cfg.SubStore)
	mcpSrv := mcp.NewMCPServer(cfg, engine, st, subClient)

	if err := server.ServeStdio(mcpSrv.GetServer()); err != nil {
		os.Exit(1)
	}
}

func runProbeCLI(args []string) {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	isSelf := fs.Bool("self", true, "是否作为自有服务")
	checkSplit := fs.Bool("split", true, "是否探测常用分流")
	configPath := fs.String("config", "config.yaml", "配置文件路径")
	_ = fs.Parse(args)

	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Println("请提供待测试的代理链接，例如: vpn-probe probe 'vless://...'")
		return
	}

	link := rest[0]
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("Load config error: %v\n", err)
		os.Exit(1)
	}

	_, _ = logger.Init(cfg.Logging.Level, "")

	engine := probe.NewEngine(cfg)
	res := engine.ProbeOne(context.Background(), link, *isSelf, *checkSplit, 10)

	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
}
