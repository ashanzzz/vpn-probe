package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/store"
	"vpn-probe/internal/substore"
)

func TestTabEndpoints_FullLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-api-tabs.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-secret-token",
		},
		SubStore: config.SubStoreConfig{
			Enabled: false,
		},
	}
	subClient := substore.NewClient(&cfg.SubStore)
	scheduler := monitor.NewScheduler(cfg, nil, st, subClient)

	server := NewAPIServer(cfg, nil, st, subClient, nil, scheduler, nil)
	app := server.app

	// 1. GET /api/orchestration/tabs
	req := httptest.NewRequest(http.MethodGet, "/api/orchestration/tabs", nil)
	req.Header.Set("Authorization", "Bearer admin-secret-token")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("GET /api/orchestration/tabs failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var getTabsResp struct {
		Status string           `json:"status"`
		Count  int              `json:"count"`
		Tabs   []*model.NodeTab `json:"tabs"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&getTabsResp)
	if getTabsResp.Count < 9 {
		t.Errorf("expected at least 9 tabs, got %d", getTabsResp.Count)
	}

	// 2. POST /api/orchestration/tabs (新建组合节点组选项卡)
	newTab := model.NodeTab{
		ID:          "custom_us_bundle",
		Name:        "自定义-美区专线与家宽",
		Type:        model.TabTypeComposite,
		Keywords:    []string{"Pavlov", "4837"},
		IncludeTabs: []string{"us"},
	}
	newTabBody, _ := json.Marshal(newTab)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/orchestration/tabs", bytes.NewReader(newTabBody))
	reqPost.Header.Set("Authorization", "Bearer admin-secret-token")
	reqPost.Header.Set("Content-Type", "application/json")
	respPost, err := app.Test(reqPost, 5000)
	if err != nil || respPost.StatusCode != fiber.StatusOK {
		t.Fatalf("POST /api/orchestration/tabs failed, status: %d", respPost.StatusCode)
	}

	// 3. GET /api/orchestration/tabs/:id/preview
	reqPrev := httptest.NewRequest(http.MethodGet, "/api/orchestration/tabs/custom_us_bundle/preview", nil)
	reqPrev.Header.Set("Authorization", "Bearer admin-secret-token")
	respPrev, err := app.Test(reqPrev, 5000)
	if err != nil || respPrev.StatusCode != fiber.StatusOK {
		t.Fatalf("preview failed, status: %d", respPrev.StatusCode)
	}

	// 4. GET /api/substore/profiles
	reqProf := httptest.NewRequest(http.MethodGet, "/api/substore/profiles", nil)
	reqProf.Header.Set("Authorization", "Bearer admin-secret-token")
	respProf, err := app.Test(reqProf, 5000)
	if err != nil || respProf.StatusCode != fiber.StatusOK {
		t.Fatalf("GET /api/substore/profiles failed, status: %d", respProf.StatusCode)
	}
	var profResp struct {
		Status   string                       `json:"status"`
		Profiles []*model.SubscriptionProfile `json:"profiles"`
	}
	_ = json.NewDecoder(respProf.Body).Decode(&profResp)
	if len(profResp.Profiles) < 4 {
		t.Errorf("expected at least 4 profiles, got %d", len(profResp.Profiles))
	}

	// 5. POST /api/substore/profiles/:name (修改 Q 选项卡多选)
	updateQ := model.SubscriptionProfile{
		SelectedTabs:    []string{"external", "group_transit", "custom_us_bundle"},
		IncludeExternal: true,
	}
	updateQBody, _ := json.Marshal(updateQ)
	reqUpdateQ := httptest.NewRequest(http.MethodPost, "/api/substore/profiles/Q", bytes.NewReader(updateQBody))
	reqUpdateQ.Header.Set("Authorization", "Bearer admin-secret-token")
	reqUpdateQ.Header.Set("Content-Type", "application/json")
	respUpdateQ, err := app.Test(reqUpdateQ, 5000)
	if err != nil || respUpdateQ.StatusCode != fiber.StatusOK {
		t.Fatalf("update profile Q failed, status: %d", respUpdateQ.StatusCode)
	}

	// 6. POST /api/substore/profiles/reset
	reqReset := httptest.NewRequest(http.MethodPost, "/api/substore/profiles/reset", nil)
	reqReset.Header.Set("Authorization", "Bearer admin-secret-token")
	respReset, err := app.Test(reqReset, 5000)
	if err != nil || respReset.StatusCode != fiber.StatusOK {
		t.Fatalf("reset profiles failed, status: %d", respReset.StatusCode)
	}

	// 7. DELETE /api/orchestration/tabs/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/orchestration/tabs/custom_us_bundle", nil)
	reqDel.Header.Set("Authorization", "Bearer admin-secret-token")
	respDel, err := app.Test(reqDel, 5000)
	if err != nil || respDel.StatusCode != fiber.StatusOK {
		t.Fatalf("delete tab failed, status: %d", respDel.StatusCode)
	}
}
