package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcp_sdk "github.com/mark3labs/mcp-go/mcp"
	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/probe"
	"vpn-probe/internal/store"
	"vpn-probe/internal/substore"
)

func TestMCPServer_ToolRegistration(t *testing.T) {
	cfg := &config.Config{}
	ms := NewMCPServer(cfg, nil, nil, nil)
	s := ms.GetServer()
	if s == nil {
		t.Fatalf("expected non-nil server")
	}

	// 验证所有 9 个工具均已成功注册
	expectedTools := []string{
		"probe_and_classify_links",
		"get_latest_probe_results",
		"sync_substore_now",
		"list_port_blocked_nodes",
		"get_ai_status_summary",
		"vpn_speedtest_node",
		"substore_sync_fivegroups",
		"substore_sync_threetier",
		"substore_purge_legacy",
	}

	// 通过 server.ListTools 验证
	tools := s.ListTools()
	for _, exp := range expectedTools {
		if _, ok := tools[exp]; !ok {
			t.Errorf("expected tool %q to be registered, but it was not", exp)
		}
	}
}

func TestMCPServer_NilSafety(t *testing.T) {
	cfg := &config.Config{}
	ms := NewMCPServer(cfg, nil, nil, nil)
	ctx := context.Background()

	// 1. handleProbeLinks with nil engine
	res, err := ms.handleProbeLinks(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError == false {
		t.Errorf("expected error result on nil engine")
	}

	// 2. handleGetHistory with nil store
	res, err = ms.handleGetHistory(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError == false {
		t.Errorf("expected error result on nil store")
	}

	// 3. handleListPortBlocked with nil store
	res, err = ms.handleListPortBlocked(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError == false {
		t.Errorf("expected error result on nil store")
	}

	// 4. handleGetAISummary with nil store
	res, err = ms.handleGetAISummary(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError == false {
		t.Errorf("expected error result on nil store")
	}

	// 5. handleSpeedtestNode with nil engine
	res, err = ms.handleSpeedtestNode(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError == false {
		t.Errorf("expected error result on nil engine")
	}

	// 6. handlePurgeLegacySubs with disabled substore
	res, err = ms.handlePurgeLegacySubs(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError == false {
		t.Errorf("expected error result on disabled substore")
	}
}

func TestMCPServer_EmptySliceNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	cfg := &config.Config{}
	ms := NewMCPServer(cfg, nil, st, nil)
	ctx := context.Background()

	// 1. 验证 get_latest_probe_results 为空时返回 [] 而非 null
	res, err := ms.handleGetHistory(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handleGetHistory failed: %v", err)
	}
	text := res.Content[0].(mcp_sdk.TextContent).Text
	if strings.TrimSpace(text) != "[]" {
		t.Errorf("expected '[]' for empty history, got: %q", text)
	}

	// 2. 验证 list_port_blocked_nodes 为空时返回 [] 而非 null
	res, err = ms.handleListPortBlocked(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handleListPortBlocked failed: %v", err)
	}
	text = res.Content[0].(mcp_sdk.TextContent).Text
	if strings.TrimSpace(text) != "[]" {
		t.Errorf("expected '[]' for empty blocked list, got: %q", text)
	}

	// 3. 验证 get_ai_status_summary 为空时 healthy_nodes 与 nodes_requiring_action 为 [] 而非 null
	res, err = ms.handleGetAISummary(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handleGetAISummary failed: %v", err)
	}
	text = res.Content[0].(mcp_sdk.TextContent).Text
	var summaryMap map[string]any
	if err := json.Unmarshal([]byte(text), &summaryMap); err != nil {
		t.Fatalf("unmarshal ai summary failed: %v", err)
	}

	hn, ok := summaryMap["healthy_nodes"].([]any)
	if !ok || hn == nil {
		t.Errorf("expected non-nil healthy_nodes array, got: %v", summaryMap["healthy_nodes"])
	}
	na, ok := summaryMap["nodes_requiring_action"].([]any)
	if !ok || na == nil {
		t.Errorf("expected non-nil nodes_requiring_action array, got: %v", summaryMap["nodes_requiring_action"])
	}
}

func TestMCPServer_AISummaryAndPortBlockedWithRecords(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	now := time.Now()

	// 写入一条健康节点
	_ = st.SaveResult(ctx, &model.NodeProbeResult{
		ID:          "p-online",
		OriginalURL: "vless://u1@1.1.1.1:443#OnlineNode",
		Name:        "OnlineNode",
		Protocol:    "vless",
		IsSelf:      true,
		Online:      true,
		LatencyMs:   150,
		ChinaExit:   false,
		ExitIP:      "1.1.1.1",
		ExitCountry: "JP",
		Category:    model.CategorySelfOnlineClean,
		StatusText:  "正常在线",
		TestedAt:    now,
	})

	// 写入一条端口被墙节点
	_ = st.SaveResult(ctx, &model.NodeProbeResult{
		ID:          "p-blocked",
		OriginalURL: "vless://u2@2.2.2.2:443#BlockedNode",
		Name:        "BlockedNode",
		Protocol:    "vless",
		IsSelf:      true,
		Online:      false,
		StatusText:  "端口被墙",
		TestedAt:    now.Add(time.Second),
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:        "2.2.2.2",
			TargetPort:      443,
			FaultType:       model.FaultPortBlocked,
			StatusText:      "端口被墙需换端口",
			ActionType:      "CHANGE_PORT",
			TroubleshootTip: "更换端口",
			AbroadSource:    "Tokyo",
			AbroadRTTMs:     30,
		},
	})

	cfg := &config.Config{}
	ms := NewMCPServer(cfg, nil, st, nil)

	// 1. 验证 list_port_blocked_nodes
	res, err := ms.handleListPortBlocked(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handleListPortBlocked failed: %v", err)
	}
	text := res.Content[0].(mcp_sdk.TextContent).Text
	var blockedList []map[string]any
	if err := json.Unmarshal([]byte(text), &blockedList); err != nil {
		t.Fatalf("unmarshal blockedList failed: %v", err)
	}
	if len(blockedList) != 1 {
		t.Fatalf("expected 1 blocked node, got %d", len(blockedList))
	}
	if blockedList[0]["target_ip"] != "2.2.2.2" {
		t.Errorf("expected target_ip 2.2.2.2, got %v", blockedList[0]["target_ip"])
	}

	// 2. 验证 get_ai_status_summary
	res, err = ms.handleGetAISummary(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handleGetAISummary failed: %v", err)
	}
	text = res.Content[0].(mcp_sdk.TextContent).Text
	var summaryMap map[string]any
	if err := json.Unmarshal([]byte(text), &summaryMap); err != nil {
		t.Fatalf("unmarshal summaryMap failed: %v", err)
	}
	hn := summaryMap["healthy_nodes"].([]any)
	na := summaryMap["nodes_requiring_action"].([]any)
	if len(hn) != 1 || len(na) != 1 {
		t.Fatalf("expected 1 healthy and 1 action node, got %d and %d", len(hn), len(na))
	}
}

func TestMCPServer_PurgeLegacyAndFiveGroups(t *testing.T) {
	deletedSubs := make(map[string]bool)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 4 {
				deletedSubs[parts[len(parts)-1]] = true
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/subs":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "success",
					"data": []map[string]any{
						{"name": "ashan-transit"},
					},
				})
				return
			case "/api/collections":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "success",
					"data": []map[string]any{
						{"name": "ashan-master", "subscriptions": []string{"ashan-transit"}},
					},
				})
				return
			case "/api/files":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "success",
					"data": []map[string]any{
						{"name": "ashan", "source": "ashan-master"},
					},
				})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/sub/") {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"name":    "ashan-transit",
					"content": "vless://u1@1.2.3.4:443#T1\n",
				})
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	subCfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     ts.URL,
	}
	sc := substore.NewClient(subCfg)
	sc.SetHTTPClient(ts.Client())

	cfg := &config.Config{
		SubStore: *subCfg,
	}
	ms := NewMCPServer(cfg, nil, nil, sc)
	ctx := context.Background()

	// 1. 测试 MCP handlePurgeLegacySubs
	res, err := ms.handlePurgeLegacySubs(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handlePurgeLegacySubs failed: %v, res: %+v", err, res)
	}
	text := res.Content[0].(mcp_sdk.TextContent).Text
	var purgeRes map[string]any
	if err := json.Unmarshal([]byte(text), &purgeRes); err != nil {
		t.Fatalf("unmarshal purge result failed: %v", err)
	}
	if purgeRes["status"] != "ok" {
		t.Errorf("expected status ok, got %v", purgeRes["status"])
	}
	for _, exp := range substore.LegacyThreeTierSubNames {
		if !deletedSubs[exp] {
			t.Errorf("expected legacy sub %q to be purged via MCP", exp)
		}
	}

	// 2. 测试 MCP handleSyncFiveGroups
	res, err = ms.handleSyncFiveGroups(ctx, mcp_sdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handleSyncFiveGroups failed: %v, res: %+v", err, res)
	}
}

func TestMCPServer_ProbeLinks(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	cfg := &config.Config{}
	engine := probe.NewEngine(cfg)
	ms := NewMCPServer(cfg, engine, st, nil)

	ctx := context.Background()

	// 1. 参数校验：空链接报错
	req := mcp_sdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"links": []any{},
	}
	res, err := ms.handleProbeLinks(ctx, req)
	if err != nil || !res.IsError {
		t.Errorf("expected error on empty links")
	}

	// 2. 正常链接探测 (解析失败快速返回离线，无需真实沙箱)
	req.Params.Arguments = map[string]any{
		"links":           []any{"invalid://foo#MockNode"},
		"is_self":         true,
		"sync_substore":   false,
		"timeout_seconds": 1,
	}
	res, err = ms.handleProbeLinks(ctx, req)
	if err != nil || res.IsError {
		t.Fatalf("handleProbeLinks failed: %v", err)
	}

	// 验证结果已存入 store
	records, err := st.GetRecentResults(ctx, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record saved in store, got %d (err: %v)", len(records), err)
	}
}
