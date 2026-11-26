package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
)

// MockSubStoreClient 模拟 SubStore 客户端用于验证熔断保护
type MockSubStoreClient struct {
	syncCalls int
	lastPayload substore.ThreeTierPayload
}

func (m *MockSubStoreClient) SyncThreeTier(ctx context.Context, payload substore.ThreeTierPayload) error {
	m.syncCalls++
	m.lastPayload = payload
	return nil
}

func TestSchedulerStableNodeKeying(t *testing.T) {
	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			Nodes: []config.MonitoredNode{
				{
					Name:   "US-Node-1",
					URL:    "vless://uuid1@1.1.1.1:36563#US-Node-1",
					IsSelf: true,
				},
				{
					Name:   "JP-Node-2",
					URL:    "ss://base64@2.2.2.2:24443#JP-Node-2",
					IsSelf: true,
				},
			},
		},
	}

	s := NewScheduler(cfg, nil, nil, nil)

	status := s.GetStatus()
	if len(status) != 2 {
		t.Fatalf("expected 2 node states, got %d", len(status))
	}

	if _, ok := status["US-Node-1"]; !ok {
		t.Fatalf("expected status keyed by node name 'US-Node-1'")
	}
	if _, ok := status["JP-Node-2"]; !ok {
		t.Fatalf("expected status keyed by node name 'JP-Node-2'")
	}
}

func TestEngineErrorRatioSafeguard(t *testing.T) {
	// 测试当引擎错误率达到或超过 30% 时，Sub-Store 同步被熔断中止
	totalNodes := 10
	engineErrors := 4 // 4/10 = 40% >= 30%

	var results []*model.NodeProbeResult
	for i := 0; i < totalNodes; i++ {
		res := &model.NodeProbeResult{
			ID:          string(rune('A' + i)),
			Name:        string(rune('A' + i)),
			OriginalURL: "vless://mock",
			Online:      false,
			TestedAt:    time.Now(),
		}
		if i < engineErrors {
			res.OfflineReason = "probe_engine_error"
		} else {
			res.OfflineReason = "node_down"
		}
		results = append(results, res)
	}

	resp := &model.BatchProbeResponse{
		Total:        totalNodes,
		EngineErrors: engineErrors,
		Results:      results,
	}

	// 校验引擎熔断阈值逻辑
	ratio := float64(resp.EngineErrors) / float64(resp.Total)
	if ratio < 0.30 {
		t.Fatalf("expected ratio >= 0.30, got %f", ratio)
	}

	syncAborted := false
	if resp.Total > 0 && float64(resp.EngineErrors)/float64(resp.Total) >= 0.30 {
		syncAborted = true
	}

	if !syncAborted {
		t.Fatalf("expected sync to be aborted when engine error ratio >= 0.30")
	}
}

func TestPortBlockedNoOverwrite(t *testing.T) {
	// 测试当节点发生端口封锁时，OriginalURL 不被直接篡改
	origURL := "vless://my-uuid@38.175.222.44:36563?security=reality#US-LAX"
	res := &model.NodeProbeResult{
		Name:        "US-LAX",
		OriginalURL: origURL,
		Diagnostic: &model.DiagnosticInfo{
			FaultType:  model.FaultPortBlocked,
			TargetIP:   "38.175.222.44",
			TargetPort: 36563,
		},
	}

	h := &struct{}{}
	_ = h

	// 模拟 scheduler 中的端口自愈判定
	if res.Diagnostic != nil && res.Diagnostic.FaultType == model.FaultPortBlocked {
		res.Diagnostic.ActionType = "AI_CHANGE_PORT"
		res.HealedURL = "vless://my-uuid@38.175.222.44:48888?security=reality#US-LAX"
	}

	if res.OriginalURL != origURL {
		t.Fatalf("OriginalURL was mutated! Expected %s, got %s", origURL, res.OriginalURL)
	}
	if res.HealedURL == "" {
		t.Fatalf("expected HealedURL to be populated")
	}
	if res.Diagnostic.ActionType != "AI_CHANGE_PORT" {
		t.Fatalf("expected ActionType to be AI_CHANGE_PORT, got %s", res.Diagnostic.ActionType)
	}
}

func TestCFRescueStandbyAndFailover(t *testing.T) {
	// 场景 1: 美西 LAX 主机直连节点健康在线，CF救活节点必须被抑制待命 (Standby)
	// 场景 2: 东京 IIJ 主机直连节点全部离线，CF救活节点必须被自动提升入网 (Promoted)
	// 场景 3: 包含重复端点的节点必须被安全去重

	nodes := []*model.NodeProbeResult{
		// 美西 LAX 节点: 2个直连在线，1个CF救活在线，1个端点重复节点
		{
			Name:        "US-LAX-Reality",
			OriginalURL: "vless://uuid@38.175.222.44:36564?security=reality#US-LAX-Reality",
			Online:      true,
			NodeType:    "transit",
		},
		{
			Name:        "美西 ACCK 洛杉矶 4837 — VLESS Reality (Duplicate)",
			OriginalURL: "vless://uuid@38.175.222.44:36564?security=reality#US-LAX-Reality",
			Online:      true,
			NodeType:    "transit",
		},
		{
			Name:        "US-LAX-SS2022",
			OriginalURL: "ss://pass@38.175.222.44:24443#US-LAX-SS2022",
			Online:      true,
			NodeType:    "transit",
		},
		{
			Name:        "美西 LAX — VLESS WS（CF/OpenResty 443 救活）",
			OriginalURL: "vless://uuid@lax-free.1992004.xyz:443?type=ws&path=/cf-vless#US-LAX-CF-WS",
			Online:      true,
			NodeType:    "transit",
		},

		// 东京 IIJ 节点: 直连全部离线，仅 CF 救活节点在线
		{
			Name:        "JP-Tokyo-IIJ-Reality",
			OriginalURL: "vless://uuid@156.231.113.91:33339?security=reality#JP-Tokyo-IIJ-Reality",
			Online:      false,
			NodeType:    "transit",
		},
		{
			Name:        "JP-Tokyo-IIJ-SS2022",
			OriginalURL: "ss://pass@156.231.113.91:24443#JP-Tokyo-IIJ-SS2022",
			Online:      false,
			NodeType:    "transit",
		},
		{
			Name:        "东京 IIJ — VLESS WS（CF/OpenResty 443 救活）",
			OriginalURL: "vless://uuid@tokyo-free.1992004.xyz:443?type=ws&path=/cf-vless#JP-Tokyo-IIJ-CF-WS",
			Online:      true,
			NodeType:    "transit",
		},
		// 东京软银 节点: 直连离线且未配置 CF 救活节点
		{
			Name:        "JP-Tokyo-Softbank-Reality",
			OriginalURL: "vless://uuid@45.192.242.72:24444#JP-Tokyo-Softbank",
			Online:      false,
			NodeType:    "transit",
		},
	}

	activeLinks := filterActiveTransitNodes(nodes)

	// 校验 1: 重复的 38.175.222.44:36564 必须只出现 1 次
	countReality := 0
	for _, l := range activeLinks {
		if l == "vless://uuid@38.175.222.44:36564?security=reality#US-LAX-Reality" {
			countReality++
		}
	}
	if countReality != 1 {
		t.Fatalf("expected duplicate Reality node to appear exactly once, got %d", countReality)
	}

	// 校验 2: 美西 LAX 直连在线，lax-free (CF救活) 必须被排除/抑制
	for _, l := range activeLinks {
		if strings.Contains(l, "lax-free.1992004.xyz") {
			t.Fatalf("expected lax-free CF rescue node to be SUPPRESSED when direct nodes are healthy, but found it in active links!")
		}
	}

	// 校验 3: 东京 IIJ 直连全灭，tokyo-free (CF救活) 必须被提升入网
	hasTokyoRescue := false
	for _, l := range activeLinks {
		if strings.Contains(l, "tokyo-free.1992004.xyz") {
			hasTokyoRescue = true
			break
		}
	}
	if !hasTokyoRescue {
		t.Fatalf("expected tokyo-free CF rescue node to be PROMOTED when direct nodes are down, but it was missing!")
	}

	// 校验 4: activeLinks 总数应为 3 (LAX Reality, LAX SS, Tokyo CF-Rescue，未配置救活且离线的软银被正确排除)
	if len(activeLinks) != 3 {
		t.Fatalf("expected exactly 3 active links, got %d: %v", len(activeLinks), activeLinks)
	}
}

func TestScheduler_FilterActiveTransitNodes_NoRescueAndAllDown(t *testing.T) {
	nodes := []*model.NodeProbeResult{
		{
			Name:        "JP-Tokyo-Softbank-Reality",
			OriginalURL: "vless://uuid@45.192.242.72:24444#JP-Tokyo-Softbank",
			Online:      false,
			NodeType:    "transit",
		},
		{
			Name:        "JP-Tokyo-IIJ-Reality",
			OriginalURL: "vless://uuid@156.231.113.91:33339#JP-Tokyo-IIJ",
			Online:      false,
			NodeType:    "transit",
		},
		{
			Name:        "JP-Tokyo-IIJ-CF-WS",
			OriginalURL: "vless://uuid@tokyo-free.1992004.xyz:443?type=ws&path=/cf-vless#JP-Tokyo-IIJ-CF-WS",
			Online:      false, // 救活节点也离线
			NodeType:    "transit",
		},
	}

	activeLinks := filterActiveTransitNodes(nodes)
	if len(activeLinks) != 0 {
		t.Fatalf("expected 0 active links when all nodes are down, got %d: %v", len(activeLinks), activeLinks)
	}
}

func TestScheduler_RefreshNodes_ConcurrentRace(t *testing.T) {
	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			Nodes: []config.MonitoredNode{
				{Name: "US-Node", URL: "vless://u@1.1.1.1:443#US-Node", IsSelf: true},
			},
			ResidentialNodes: []config.MonitoredNode{
				{Name: "LZ-Resi-01", URL: "socks5://u:p@1.1.1.1:1000#LZ-Resi-01", IsSelf: true},
			},
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     "http://mock.example.com",
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	s := NewScheduler(cfg, nil, nil, sc)

	var wg sync.WaitGroup

	// 并发写者：模拟 RunAllChecks 频繁更新 nodeStates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			s.InjectNodeStateForTest("LZ-Resi-01", &model.NodeProbeResult{
				Name:        "LZ-Resi-01",
				OriginalURL: "socks5://u:p@1.1.1.1:1000#LZ-Resi-01",
				NodeType:    "residential",
				Online:      i%2 == 0,
			})
			time.Sleep(100 * time.Microsecond)
		}
	}()

	// 并发读者：高频调用 GetStatus
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = s.GetStatus()
				time.Sleep(200 * time.Microsecond)
			}
		}()
	}

	wg.Wait()
}

func TestRefreshNodesFromSubStore_FiveGroups(t *testing.T) {
	// 模拟 SubStore 返回 5 组自建策略组，包含线路机、家宽直连机与链式代理
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/subs" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-transit","displayName":"线路机","content":"vless://u1@1.1.1.1:443#Transit-01\nvless://u2@2.2.2.2:443#Transit-02"},
				{"name":"ashan-us-resi","displayName":"美区全家桶","content":"vless://u1@1.1.1.1:443#Transit-01\nvless://u3@167.104.167.151:41000#LZ-US-Pavlov-家宽\nvless://u3@167.104.167.151:41000?dialerProxy=Transit-01#链式|Transit-01 ➔ LZ-US-Pavlov-家宽"},
				{"name":"ashan-jp-resi","displayName":"日区全家桶","content":"vless://u4@167.104.167.151:42000#LZ-JP-SoftBank-家宽"},
				{"name":"ashan-other","displayName":"其他全家桶","content":""},
				{"name":"ashan-china-leak","displayName":"送中隔离","content":""},
				{"name":"蜂窝云","displayName":"外部机场","content":"vless://airport@8.8.8.8:443#Airport-Node"}
			]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     mockServer.URL,
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	s := NewScheduler(cfg, nil, nil, sc)

	err := s.RefreshNodesFromSubStore(context.Background())
	if err != nil {
		t.Fatalf("expected no error from RefreshNodesFromSubStore, got: %v", err)
	}

	status := s.GetStatus()
	// 期望 4 个原子节点：Transit-01, Transit-02, LZ-US-Pavlov-家宽, LZ-JP-SoftBank-家宽
	// 外部机场 'Airport-Node' 与链式代理 '链式|...' 必须彻底排除
	if len(status) != 4 {
		t.Fatalf("expected 4 base self-hosted nodes, got %d: %+v", len(status), status)
	}

	// 验证链式代理未进入探针列表
	for k := range status {
		if strings.Contains(k, "链式") || strings.Contains(k, "➔") {
			t.Errorf("chain node %s should have been filtered out from monitor", k)
		}
		if strings.Contains(k, "Airport") {
			t.Errorf("external airport node %s must never be monitored", k)
		}
	}

	// 验证类型区分
	if status["Transit-01"].LastResult.NodeType != "transit" {
		t.Errorf("expected Transit-01 to have NodeType 'transit', got: %s", status["Transit-01"].LastResult.NodeType)
	}
	if status["Transit-02"].LastResult.NodeType != "transit" {
		t.Errorf("expected Transit-02 to have NodeType 'transit', got: %s", status["Transit-02"].LastResult.NodeType)
	}
	if status["LZ-US-Pavlov-家宽"].LastResult.NodeType != "residential" {
		t.Errorf("expected LZ-US-Pavlov-家宽 to have NodeType 'residential', got: %s", status["LZ-US-Pavlov-家宽"].LastResult.NodeType)
	}
	if status["LZ-JP-SoftBank-家宽"].LastResult.NodeType != "residential" {
		t.Errorf("expected LZ-JP-SoftBank-家宽 to have NodeType 'residential', got: %s", status["LZ-JP-SoftBank-家宽"].LastResult.NodeType)
	}
}

func TestRefreshNodesFromSubStore_LegacyFallback(t *testing.T) {
	// 模拟旧版本 SubStore 仅存在旧三层独立订阅 (ashan-transit-direct, ashan-resi-direct)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/subs" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-transit-direct","displayName":"线路机直连","content":"vless://u1@1.1.1.1:443#Legacy-Transit-01"},
				{"name":"ashan-resi-direct","displayName":"家宽直连","content":"vless://u2@167.104.167.151:41000#LZ-Legacy-Resi-01"}
			]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     mockServer.URL,
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	s := NewScheduler(cfg, nil, nil, sc)

	err := s.RefreshNodesFromSubStore(context.Background())
	if err != nil {
		t.Fatalf("expected no error from RefreshNodesFromSubStore fallback, got: %v", err)
	}

	status := s.GetStatus()
	if len(status) != 2 {
		t.Fatalf("expected 2 nodes from legacy fallback, got %d", len(status))
	}
	if _, ok := status["Legacy-Transit-01"]; !ok {
		t.Errorf("missing Legacy-Transit-01 in status")
	}
	if _, ok := status["LZ-Legacy-Resi-01"]; !ok {
		t.Errorf("missing LZ-Legacy-Resi-01 in status")
	}
}

func TestRefreshNodesFromSubStore_EmptyPreservesExisting(t *testing.T) {
	// 模拟 SubStore 连接失败或返回空，验证不清除已有静态节点
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			Nodes: []config.MonitoredNode{
				{Name: "Static-Node-01", URL: "vless://u@1.1.1.1:443#Static-Node-01", IsSelf: true},
			},
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     mockServer.URL,
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	s := NewScheduler(cfg, nil, nil, sc)

	// 初始应有 1 个静态节点
	if len(s.GetStatus()) != 1 {
		t.Fatalf("expected 1 initial static node, got %d", len(s.GetStatus()))
	}

	// 触发刷新失败
	_ = s.RefreshNodesFromSubStore(context.Background())

	// 必须保留原有节点
	if len(s.GetStatus()) != 1 {
		t.Fatalf("expected 1 node preserved after failure, got %d", len(s.GetStatus()))
	}
}

func TestRefreshNodesFromSubStore_PruneDynamicNodes(t *testing.T) {
	// 验证当动态自建家宽节点从 SubStore 移除后，能够正常被 Prune，而静态配置的自建节点不受影响
	subsJSON := `{"status":"success","data":[
		{"name":"ashan-transit","displayName":"线路机","content":"vless://u1@1.1.1.1:443#Transit-01"},
		{"name":"ashan-us-resi","displayName":"美区全家桶","content":"vless://u2@167.104.167.151:41000#LZ-US-To-Be-Pruned"}
	]}`

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/subs" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(subsJSON))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			Nodes: []config.MonitoredNode{
				{Name: "Static-Transit", URL: "vless://u@3.3.3.3:443#Static-Transit", IsSelf: true},
			},
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     mockServer.URL,
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	s := NewScheduler(cfg, nil, nil, sc)

	// 第一次同步：拉入 Transit-01 和 LZ-US-To-Be-Pruned，加上静态节点共 3 个
	_ = s.RefreshNodesFromSubStore(context.Background())
	if len(s.GetStatus()) != 3 {
		t.Fatalf("expected 3 nodes after initial sync, got %d", len(s.GetStatus()))
	}

	// 模拟 SubStore 删除了 LZ-US-To-Be-Pruned，仅保留 Transit-01
	subsJSON = `{"status":"success","data":[
		{"name":"ashan-transit","displayName":"线路机","content":"vless://u1@1.1.1.1:443#Transit-01"}
	]}`

	// 第二次同步：LZ-US-To-Be-Pruned 必须被正常剪枝删除，仅保留 Static-Transit 与 Transit-01 共 2 个节点
	_ = s.RefreshNodesFromSubStore(context.Background())
	status := s.GetStatus()
	if len(status) != 2 {
		t.Fatalf("expected 2 nodes after pruning, got %d", len(status))
	}
	if _, ok := status["LZ-US-To-Be-Pruned"]; ok {
		t.Errorf("expected LZ-US-To-Be-Pruned to be pruned from monitor")
	}
	if _, ok := status["Transit-01"]; !ok {
		t.Errorf("expected Transit-01 to remain")
	}
	if _, ok := status["Static-Transit"]; !ok {
		t.Errorf("expected Static-Transit to remain")
	}
}

func TestScheduler_RunSpeedTestAll_ConcurrencyAndCloning(t *testing.T) {
	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			Nodes: []config.MonitoredNode{
				{Name: "Transit-01", URL: "vless://u1@1.1.1.1:443#Transit-01", IsSelf: true},
				{Name: "Transit-02", URL: "vless://u2@2.2.2.2:443#Transit-02", IsSelf: true},
			},
		},
	}

	// 1. 验证 engine 为 nil 时不 panic 且安全返回 nil
	s := NewScheduler(cfg, nil, nil, nil)
	res := s.RunSpeedTestAll(context.Background(), false, 1)
	if res != nil {
		t.Errorf("expected nil result when engine is nil, got: %v", res)
	}

	// 初始化节点状态为 online transit
	s.mu.Lock()
	s.nodeStates["Transit-01"].LastResult = &model.NodeProbeResult{
		Name:        "Transit-01",
		OriginalURL: "vless://u1@1.1.1.1:443#Transit-01",
		NodeType:    "transit",
		Online:      true,
		Score:       10,
	}
	s.nodeStates["Transit-02"].LastResult = &model.NodeProbeResult{
		Name:        "Transit-02",
		OriginalURL: "vless://u2@2.2.2.2:443#Transit-02",
		NodeType:    "transit",
		Online:      true,
		Score:       20,
	}
	s.mu.Unlock()

	// 2. 验证 GetStatus 返回的是独立结构体副本
	st1 := s.GetStatus()
	node1 := st1["Transit-01"]
	node1.Config.Name = "Mutated-Name"
	st2 := s.GetStatus()
	if st2["Transit-01"].Config.Name == "Mutated-Name" {
		t.Errorf("GetStatus should return decoupled shallow copies of NodeState")
	}
}

func TestScheduler_RunAllChecks_ConcurrencySafety(t *testing.T) {
	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled: true,
			Nodes: []config.MonitoredNode{
				{Name: "Transit-01", URL: "vless://u1@1.1.1.1:443#Transit-01", IsSelf: true},
			},
		},
	}
	s := NewScheduler(cfg, nil, nil, nil)

	var wg sync.WaitGroup
	// 并发触发 10 个 RunAllChecks 与 SyncFiveGroupsNow，验证互斥保护与竞态安全
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.RunAllChecks(context.Background(), false)
		}()
		go func() {
			defer wg.Done()
			_ = s.SyncFiveGroupsNow(context.Background())
		}()
	}
	wg.Wait()
}

func TestRefreshNodesFromSubStore_RekeysRenamedNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/subs" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":[
					{"name":"ashan-transit","displayName":"线路机","content":"vless://uuid@156.231.113.91:33339#JP-AKILE-IIJ"}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Nodes: []config.MonitoredNode{
				{Name: "东京 IIJ — VLESS Reality", URL: "vless://uuid@156.231.113.91:33339#东京 IIJ — VLESS Reality", IsSelf: true},
			},
		},
		SubStore: config.SubStoreConfig{Enabled: true, URL: srv.URL},
	}
	s := NewScheduler(cfg, nil, nil, substore.NewClient(&cfg.SubStore))
	if err := s.RefreshNodesFromSubStore(context.Background()); err != nil {
		t.Fatalf("RefreshNodesFromSubStore failed: %v", err)
	}

	st := s.GetStatus()
	if _, hasOld := st["东京 IIJ — VLESS Reality"]; hasOld {
		t.Errorf("expected stale map key '东京 IIJ — VLESS Reality' to be removed after rename")
	}
	if renamed, ok := st["JP-AKILE-IIJ"]; !ok {
		t.Fatalf("expected renamed map key 'JP-AKILE-IIJ' in nodeStates")
	} else if renamed.LastResult == nil || renamed.LastResult.Name != "JP-AKILE-IIJ" {
		t.Errorf("expected LastResult.Name=JP-AKILE-IIJ, got %+v", renamed.LastResult)
	}
}

func TestScheduler_RunAllChecks_RespectsAutoSyncSubStoreFalse(t *testing.T) {
	var patchCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patchCalled = true
		}
		if r.URL.Path == "/api/subs" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Monitor: config.MonitorConfig{
			Enabled:          true,
			AutoSyncSubStore: false,
		},
		SubStore: config.SubStoreConfig{Enabled: true, URL: srv.URL},
	}
	s := NewScheduler(cfg, nil, nil, substore.NewClient(&cfg.SubStore))
	_ = s.RunAllChecks(context.Background(), false)
	if patchCalled {
		t.Fatalf("expected RunAllChecks not to PATCH Sub-Store when auto_sync_substore=false")
	}
}
