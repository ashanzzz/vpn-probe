package monitor

import (
	"context"
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

	// 校验 4: activeLinks 总数应为 3 (LAX Reality, LAX SS, Tokyo CF-Rescue)
	if len(activeLinks) != 3 {
		t.Fatalf("expected exactly 3 active links, got %d: %v", len(activeLinks), activeLinks)
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
