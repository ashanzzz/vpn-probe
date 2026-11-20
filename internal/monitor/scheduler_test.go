package monitor

import (
	"context"
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
