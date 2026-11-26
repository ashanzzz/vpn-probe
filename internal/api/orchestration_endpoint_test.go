package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/healer"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/substore"
)

func TestOrchestrationEndpoints(t *testing.T) {
	ResetAuthTracker()
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "custom_groups.json")
	cleanupCustom := monitor.SetCustomGroupsFileForTest(customFile)
	defer cleanupCustom()

	// 1. Mock SubStore HTTP Server
	mockSubStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
	}))
	defer mockSubStore.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-token-123",
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     mockSubStore.URL,
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	h := healer.NewHealer(nil, nil)
	sch := monitor.NewScheduler(cfg, nil, nil, sc)
	sch.SetHealerForTest(h)

	// 注入节点
	sch.InjectNodeStateForTest("US-Transit", &model.NodeProbeResult{
		ID:          "1",
		Name:        "美西 ACCK 洛杉矶 4837 — VLESS Reality",
		OriginalURL: "vless://u1@38.175.222.44:36564#US-LAX-4837",
		ExitCountry: "US",
		Online:      true,
		NodeType:    "transit",
		TestedAt:    time.Now(),
	})
	sch.InjectNodeStateForTest("US-Resi", &model.NodeProbeResult{
		ID:          "2",
		Name:        "LZ-US-Pavlov-家宽01",
		OriginalURL: "socks5://u:***@1.2.3.4:1001#LZ-US-家宽01",
		ExitCountry: "US",
		Online:      true,
		NodeType:    "residential",
		TestedAt:    time.Now(),
	})
	sch.InjectNodeStateForTest("CN-Leak", &model.NodeProbeResult{
		ID:          "3",
		Name:        "送中测试节点",
		OriginalURL: "vless://u3@9.9.9.9:443#CN-Leak",
		ExitCountry: "CN",
		ChinaExit:   true,
		Online:      true,
		NodeType:    "transit",
		TestedAt:    time.Now(),
	})

	server := NewAPIServer(cfg, nil, nil, sc, nil, sch, nil)

	// 2. 测试 GET /api/orchestration/atoms (原子节点查询)
	t.Run("GetAtoms", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/orchestration/atoms?country=US", nil)
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body struct {
			Status string           `json:"status"`
			Count  int              `json:"count"`
			Atoms  []model.AtomNode `json:"atoms"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Count < 2 {
			t.Errorf("expected at least 2 atoms, got %d", body.Count)
		}
	})

	// 2.1 测试 GET /api/orchestration/atoms?china_leak_only=true
	t.Run("GetAtoms_ChinaLeakOnly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/orchestration/atoms?china_leak_only=true", nil)
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body struct {
			Status string           `json:"status"`
			Count  int              `json:"count"`
			Atoms  []model.AtomNode `json:"atoms"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Count != 1 || body.Atoms[0].Name != "送中测试节点" {
			t.Errorf("expected 1 leak atom named '送中测试节点', got count=%d, atoms=%v", body.Count, body.Atoms)
		}
	})

	// 3. 测试 GET /api/orchestration/categories (全局统计大盘)
	t.Run("GetCategories", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/orchestration/categories", nil)
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	// 4. 测试 POST /api/orchestration/evaluate (实时试算公式: 美国线路机 + 美国家宽 + 线路家宽机)
	t.Run("EvaluateGroup", func(t *testing.T) {
		filter := model.GroupFilter{
			Countries:          []string{"US"},
			IncludeTransit:     true,
			IncludeResidential: true,
			IncludeChain:       true,
			ExcludeChinaLeak:   true,
		}
		fBytes, _ := json.Marshal(filter)
		req := httptest.NewRequest(http.MethodPost, "/api/orchestration/evaluate", bytes.NewReader(fBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body struct {
			Status string   `json:"status"`
			Count  int      `json:"count"`
			Nodes  []string `json:"nodes"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		// 应该匹配 3 条（1 线路 + 1 家宽 + 1 链式中继）
		if body.Count != 3 {
			t.Errorf("expected 3 nodes in US bundle evaluation, got %d", body.Count)
		}
	})

	// 5. 测试 POST /api/orchestration/groups (创建并保存自定义分组)
	t.Run("SaveCustomGroup", func(t *testing.T) {
		def := model.CustomGroupDef{
			Name:        "ashan-us-all",
			DisplayName: "自建-美国全景",
			Enabled:     true,
			Filter: model.GroupFilter{
				Countries:          []string{"US"},
				IncludeTransit:     true,
				IncludeResidential: true,
				IncludeChain:       true,
			},
		}
		dBytes, _ := json.Marshal(def)
		req := httptest.NewRequest(http.MethodPost, "/api/orchestration/groups?sync=false", bytes.NewReader(dBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// 检查读取
		getReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/groups", nil)
		getReq.Header.Set("Authorization", "Bearer admin-token-123")
		getResp, _ := server.app.Test(getReq)
		var getBody struct {
			Groups []model.CustomGroupDef `json:"groups"`
		}
		_ = json.NewDecoder(getResp.Body).Decode(&getBody)
		var found bool
		for _, g := range getBody.Groups {
			if g.Name == "ashan-us-all" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected group 'ashan-us-all' in groups list %v", getBody.Groups)
		}

		// 删除
		delReq := httptest.NewRequest(http.MethodDelete, "/api/orchestration/groups/ashan-us-all", nil)
		delReq.Header.Set("Authorization", "Bearer admin-token-123")
		delResp, _ := server.app.Test(delReq)
		if delResp.StatusCode != http.StatusOK {
			t.Errorf("expected delete 200, got %d", delResp.StatusCode)
		}
	})

	// 6. 测试全量链式中继重建端点
	t.Run("RebuildChainsAndSyncFiveGroups", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/orchestration/chains/rebuild", nil)
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	// 7. 测试显式动态拉线路与解绑 API (满足用户任意线路机拉指定家宽需求)
	t.Run("BindAndUnbindCustomChainAPI", func(t *testing.T) {
		bindBody := map[string]string{
			"transit":     "美西 ACCK 洛杉矶 4837 — VLESS Reality",
			"residential": "LZ-US-Pavlov-家宽01",
		}
		bBytes, _ := json.Marshal(bindBody)
		req := httptest.NewRequest(http.MethodPost, "/api/orchestration/chains/bind", bytes.NewReader(bBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("bind request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// 获取列表
		getReq := httptest.NewRequest(http.MethodGet, "/api/orchestration/chains", nil)
		getReq.Header.Set("Authorization", "Bearer admin-token-123")
		getResp, _ := server.app.Test(getReq)
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", getResp.StatusCode)
		}

		// 解绑
		unbindBody := map[string]string{
			"target": "LZ-US-Pavlov-家宽01",
		}
		uBytes, _ := json.Marshal(unbindBody)
		delReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/chains/unbind", bytes.NewReader(uBytes))
		delReq.Header.Set("Content-Type", "application/json")
		delReq.Header.Set("Authorization", "Bearer admin-token-123")
		delResp, _ := server.app.Test(delReq)
		if delResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", delResp.StatusCode)
		}

		// 测试 RESTful DELETE 路由解绑
		bindReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/chains/bind", bytes.NewReader(bBytes))
		bindReq.Header.Set("Content-Type", "application/json")
		bindReq.Header.Set("Authorization", "Bearer admin-token-123")
		_, _ = server.app.Test(bindReq)

		delRouteReq := httptest.NewRequest(http.MethodDelete, "/api/orchestration/chains/LZ-US-Pavlov-家宽01", nil)
		delRouteReq.Header.Set("Authorization", "Bearer admin-token-123")
		delRouteResp, err := server.app.Test(delRouteReq)
		if err != nil || delRouteResp.StatusCode != http.StatusOK {
			t.Fatalf("expected DELETE route to return 200, got %v (err: %v)", delRouteResp.StatusCode, err)
		}
	})

	// 8. 测试正则合法性强校验 (Evaluate 与 Save)
	t.Run("RegexValidation", func(t *testing.T) {
		// 8.1 EvaluateGroup 拒绝非法正则
		badFilter := model.GroupFilter{
			CustomMatchRegex: "[unclosed-bracket",
		}
		bfBytes, _ := json.Marshal(badFilter)
		evalReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/evaluate", bytes.NewReader(bfBytes))
		evalReq.Header.Set("Content-Type", "application/json")
		evalReq.Header.Set("Authorization", "Bearer admin-token-123")
		evalResp, _ := server.app.Test(evalReq)
		if evalResp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for bad regex in evaluate, got %d", evalResp.StatusCode)
		}

		// 8.2 SaveCustomGroup 拒绝非法正则
		badDef := model.CustomGroupDef{
			Name: "bad-regex-group",
			Filter: model.GroupFilter{
				CustomMatchRegex: "(unclosed-paren",
			},
		}
		bdBytes, _ := json.Marshal(badDef)
		saveReq := httptest.NewRequest(http.MethodPost, "/api/orchestration/groups?sync=false", bytes.NewReader(bdBytes))
		saveReq.Header.Set("Content-Type", "application/json")
		saveReq.Header.Set("Authorization", "Bearer admin-token-123")
		saveResp, _ := server.app.Test(saveReq)
		if saveResp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for bad regex in save group, got %d", saveResp.StatusCode)
		}
	})

	t.Run("AI summary with residential chain nodes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ai/status", nil)
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("ai status request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var res map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&res)
		summary, ok := res["summary"].(map[string]any)
		if !ok {
			t.Fatalf("expected summary in response, got %v", res)
		}
		// Since transit is healthy and resi has fault none (or not failing port), requires_healing_action should be false
		if reqHealing, _ := summary["requires_healing_action"].(bool); reqHealing {
			t.Fatalf("expected requires_healing_action=false, got true")
		}
	})
}

func TestChainAtomsHealthAndLatencyAccuracy(t *testing.T) {
	ResetAuthTracker()
	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-token-123",
		},
	}
	h := healer.NewHealer(nil, nil)
	sch := monitor.NewScheduler(cfg, nil, nil, nil)
	sch.SetHealerForTest(h)

	// 注入在线线路专线
	sch.InjectNodeStateForTest("US-Transit", &model.NodeProbeResult{
		ID:          "transit-1",
		Name:        "美西 ACCK 洛杉矶 4837 — VLESS Reality",
		OriginalURL: "vless://u1@38.175.222.44:36564#US-LAX-4837",
		ExitCountry: "US",
		Online:      true,
		LatencyMs:   120,
		NodeType:    "transit",
		TestedAt:    time.Now(),
	})

	// 注入离线家庭端节点 (FaultResidentialTunnelOffline)
	sch.InjectNodeStateForTest("Offline-Resi", &model.NodeProbeResult{
		ID:          "resi-tunnel-down",
		Name:        "LZ-US-Down-家宽",
		OriginalURL: "socks5://u:***@1.2.3.4:1002#LZ-US-Down-家宽",
		ExitCountry: "US",
		Online:      false,
		NodeType:    "residential",
		Diagnostic: &model.DiagnosticInfo{
			FaultType:       model.FaultResidentialTunnelOffline,
			AbroadReachable: false,
		},
		TestedAt: time.Now(),
	})

	// 注入仅国内直连受阻但海外畅通的家宽节点 (FaultResidentialDirectBlocked)
	sch.InjectNodeStateForTest("Blocked-Resi", &model.NodeProbeResult{
		ID:          "resi-direct-blocked",
		Name:        "LZ-US-DirectBlocked-家宽",
		OriginalURL: "socks5://u:***@1.2.3.4:1003#LZ-US-DirectBlocked-家宽",
		ExitCountry: "US",
		Online:      false,
		NodeType:    "residential",
		Diagnostic: &model.DiagnosticInfo{
			FaultType:       model.FaultResidentialDirectBlocked,
			AbroadReachable: true,
			AbroadRTTMs:     250,
		},
		TestedAt: time.Now(),
	})

	server := NewAPIServer(cfg, nil, nil, nil, nil, sch, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/atoms?country=US&kind=chain", nil)
	req.Header.Set("Authorization", "Bearer admin-token-123")
	resp, err := server.app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (err: %v)", resp.StatusCode, err)
	}

	var body struct {
		Status string           `json:"status"`
		Count  int              `json:"count"`
		Atoms  []model.AtomNode `json:"atoms"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	var foundDownChain, foundBlockedChain bool
	for _, a := range body.Atoms {
		if a.Name == "美西 ACCK 洛杉矶 4837 — VLESS Reality ➔ LZ-US-Down-家宽" {
			foundDownChain = true
			if a.Online {
				t.Errorf("expected chain for tunnel offline residential node to be offline (Online=false), got true")
			}
		}
		if a.Name == "美西 ACCK 洛杉矶 4837 — VLESS Reality ➔ LZ-US-DirectBlocked-家宽" {
			foundBlockedChain = true
			if !a.Online {
				t.Errorf("expected chain for direct-blocked residential node with abroad reachable to be Online=true, got false")
			}
			// Latency should be 120 (transit) + 250 (abroad) = 370
			if a.LatencyMs != 370 {
				t.Errorf("expected combined latency 370ms, got %d", a.LatencyMs)
			}
		}
	}
	if !foundDownChain {
		t.Errorf("expected to find down chain in atoms")
	}
	if !foundBlockedChain {
		t.Errorf("expected to find direct-blocked chain in atoms")
	}
}

func TestPolicyGroupBatchAndDefaultsEndpoints(t *testing.T) {
	ResetAuthTracker()
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "custom_groups.json")
	publishFile := filepath.Join(tmpDir, "publish_jobs.json")
	cleanupCustom := monitor.SetCustomGroupsFileForTest(customFile)
	defer cleanupCustom()

	t.Setenv("VPN_PROBE_PUBLISH_JOBS", publishFile)

	mockSubStore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
	}))
	defer mockSubStore.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-token-123",
		},
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     mockSubStore.URL,
		},
	}

	sc := substore.NewClient(&cfg.SubStore)
	sch := monitor.NewScheduler(cfg, nil, nil, sc)
	server := NewAPIServer(cfg, nil, nil, sc, nil, sch, nil)

	// 1. GET /api/v1/policy-groups/defaults
	t.Run("GetDefaults", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/policy-groups/defaults", nil)
		req.Header.Set("Authorization", "Bearer admin-token-123")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Status string                 `json:"status"`
			Count  int                    `json:"count"`
			Groups []model.CustomGroupDef `json:"groups"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Count != 8 || len(body.Groups) != 8 {
			t.Errorf("expected 8 canonical default groups, got count=%d, len=%d", body.Count, len(body.Groups))
		}
	})

	// 2. PUT /api/v1/policy-groups/batch with empty payload
	t.Run("BatchSave_EmptyPayload_Rejected", func(t *testing.T) {
		emptyCases := []string{
			`{}`,
			`{"groups": []}`,
			`[]`,
		}
		for _, rawJSON := range emptyCases {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/policy-groups/batch", bytes.NewBufferString(rawJSON))
			req.Header.Set("Authorization", "Bearer admin-token-123")
			req.Header.Set("Content-Type", "application/json")
			resp, err := server.app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("payload %q: expected 400 Bad Request, got %d", rawJSON, resp.StatusCode)
			}
		}
	})

	// 2b. PUT /api/v1/policy-groups/batch with malformed payload
	t.Run("BatchSave_MalformedPayload_Rejected", func(t *testing.T) {
		malformedCases := []string{
			`not a valid json`,
			`{"groups": "not-an-array"}`,
			`[{"name": ""}]`,
			`[{"name": "valid", "type": "invalid-type"}]`,
		}
		for _, rawJSON := range malformedCases {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/policy-groups/batch", bytes.NewBufferString(rawJSON))
			req.Header.Set("Authorization", "Bearer admin-token-123")
			req.Header.Set("Content-Type", "application/json")
			resp, err := server.app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("payload %q: expected 400 Bad Request, got %d", rawJSON, resp.StatusCode)
			}
		}
	})

	// 3. PUT /api/v1/policy-groups/batch with duplicate names
	t.Run("BatchSave_DuplicateNames_Rejected", func(t *testing.T) {
		dupJSON := `{"groups": [
			{"name": "group_a", "display_name": "Group A", "enabled": true, "type": "url-test"},
			{"name": "group_a", "display_name": "Group B", "enabled": true, "type": "url-test"}
		]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/policy-groups/batch", bytes.NewBufferString(dupJSON))
		req.Header.Set("Authorization", "Bearer admin-token-123")
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for duplicate names, got %d", resp.StatusCode)
		}
	})

	// 4. PUT /api/v1/policy-groups/batch with duplicate display names
	t.Run("BatchSave_DuplicateDisplayNames_Rejected", func(t *testing.T) {
		dupJSON := `{"groups": [
			{"name": "group_a", "display_name": "Same Name", "enabled": true, "type": "url-test"},
			{"name": "group_b", "display_name": "Same Name", "enabled": true, "type": "url-test"}
		]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/policy-groups/batch", bytes.NewBufferString(dupJSON))
		req.Header.Set("Authorization", "Bearer admin-token-123")
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for duplicate display names, got %d", resp.StatusCode)
		}
	})

	// 5. PUT /api/v1/policy-groups/batch valid save via wrapped object
	t.Run("BatchSave_ValidWrapped_Success", func(t *testing.T) {
		validJSON := `{"groups": [
			{"name": "group_1", "display_name": "Group One", "enabled": true, "type": "url-test", "order": 10},
			{"name": "group_2", "display_name": "Group Two", "enabled": true, "type": "select", "order": 20}
		]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/policy-groups/batch", bytes.NewBufferString(validJSON))
		req.Header.Set("Authorization", "Bearer admin-token-123")
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 202/200, got %d", resp.StatusCode)
		}

		// Verify saved in file
		loaded, err := monitor.LoadCustomGroups()
		if err != nil {
			t.Fatalf("LoadCustomGroups: %v", err)
		}
		if len(loaded) != 2 {
			t.Fatalf("expected 2 groups saved, got %d", len(loaded))
		}
		if loaded[0].Name != "group_1" || loaded[1].Name != "group_2" {
			t.Errorf("unexpected saved groups: %+v", loaded)
		}
	})

	// 6. POST /api/v1/policy-groups/batch valid save via raw array
	t.Run("BatchSave_ValidRawArray_Success", func(t *testing.T) {
		rawArrayJSON := `[
			{"name": "group_x", "display_name": "Group X", "enabled": true, "type": "url-test", "order": 5}
		]`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-groups/batch", bytes.NewBufferString(rawArrayJSON))
		req.Header.Set("Authorization", "Bearer admin-token-123")
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 202/200, got %d", resp.StatusCode)
		}

		loaded, err := monitor.LoadCustomGroups()
		if err != nil {
			t.Fatalf("LoadCustomGroups: %v", err)
		}
		if len(loaded) != 1 || loaded[0].Name != "group_x" {
			t.Fatalf("expected 1 group saved with name group_x, got %+v", loaded)
		}
	})

	// 7. Unit test monitor.SaveCustomGroupsBatch empty slice check
	t.Run("SaveCustomGroupsBatch_EmptyCheck", func(t *testing.T) {
		if err := monitor.SaveCustomGroupsBatch(nil); err == nil {
			t.Errorf("expected error when saving nil batch, got nil")
		}
		if err := monitor.SaveCustomGroupsBatch([]model.CustomGroupDef{}); err == nil {
			t.Errorf("expected error when saving empty slice, got nil")
		}
	})

	// 8. POST /api/v1/policy-groups single upsert returns 400 on duplicate display_name
	t.Run("Upsert_DuplicateDisplayName_Returns400", func(t *testing.T) {
		baseGroup := model.CustomGroupDef{
			Name:        "group_existing",
			DisplayName: "冲突显示名",
			Enabled:     true,
			Type:        "url-test",
		}
		if err := monitor.SaveCustomGroup(baseGroup); err != nil {
			t.Fatalf("setup base group failed: %v", err)
		}

		conflictingJSON := `{"name": "group_another", "display_name": "冲突显示名", "enabled": true, "type": "url-test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-groups", bytes.NewBufferString(conflictingJSON))
		req.Header.Set("Authorization", "Bearer admin-token-123")
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected HTTP 400 for duplicate display_name, got %d", resp.StatusCode)
		}
	})
}
