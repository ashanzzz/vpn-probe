package substore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vpn-probe/internal/config"
)

func TestAuditFileGroups(t *testing.T) {
	sampleYAML := `
proxies:
  - name: 节点1
    type: ss
  - name: 节点2
    type: vless

proxy-groups:
  - name: 代理组A
    type: select
    proxies:
      - 节点1
      - 节点2
      - DIRECT
  - name: 代理组B
    type: select
    proxies:
      - 节点1
      - 不存在的节点
`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Write([]byte(sampleYAML))
	}))
	defer ts.Close()

	client := NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     ts.URL,
	})

	audit, err := client.AuditFileGroups(context.Background(), "test-file")
	if err != nil {
		t.Fatalf("AuditFileGroups failed: %v", err)
	}

	if audit.TotalProxies != 2 {
		t.Errorf("expected 2 proxies, got %d", audit.TotalProxies)
	}
	if audit.Valid {
		t.Errorf("expected Valid=false due to dangling node, got true")
	}
	if len(audit.DanglingReferences["代理组B"]) != 1 || audit.DanglingReferences["代理组B"][0] != "不存在的节点" {
		t.Errorf("expected '不存在的节点' in dangling, got %v", audit.DanglingReferences)
	}
	if len(audit.DanglingReferences["代理组A"]) != 0 {
		t.Errorf("expected 0 dangling in 代理组A, got %v", audit.DanglingReferences["代理组A"])
	}
}

func TestModePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	origModeFile := modeFile
	modeFile = filepath.Join(tmpDir, "substore_mode.json")
	defer func() { modeFile = origModeFile }()

	client := NewClient(&config.SubStoreConfig{
		Enabled: false,
	})

	// Default when file does not exist
	if m := client.GetCurrentMode(); m != ModeExternalAndSelf {
		t.Errorf("expected default ModeExternalAndSelf, got %s", m)
	}

	// Write mock mode
	state := ModeState{
		CurrentMode: ModeSelfOnly,
		UpdatedAt:   "2026-11-21T09:00:00Z",
	}
	data := `{"current_mode": "self", "updated_at": "2026-11-21T09:00:00Z"}`
	_ = os.WriteFile(modeFile, []byte(data), 0644)

	if m := client.GetCurrentMode(); m != ModeSelfOnly {
		t.Errorf("expected ModeSelfOnly, got %s", m)
	}
	_ = state
}

func TestDynamicExternalSourcesAndModes(t *testing.T) {
	tmpDir := t.TempDir()
	restore := SetExternalSourcesFileForTest(filepath.Join(tmpDir, "external_sources.json"))
	defer restore()

	client := NewClient(&config.SubStoreConfig{
		Enabled:         true,
		ExternalSources: []string{"DefaultAirport"},
	})

	// 1. 默认取配置中的 ExternalSources
	sources := client.GetExternalSources()
	if len(sources) != 1 || sources[0] != "DefaultAirport" {
		t.Fatalf("expected ['DefaultAirport'], got %v", sources)
	}

	// 2. 动态设置 external_sources
	newSources := []string{"AirportA", "AirportB"}
	if err := client.SetExternalSources(newSources); err != nil {
		t.Fatalf("SetExternalSources failed: %v", err)
	}

	sources = client.GetExternalSources()
	if len(sources) != 2 || sources[0] != "AirportA" || sources[1] != "AirportB" {
		t.Fatalf("expected ['AirportA', 'AirportB'], got %v", sources)
	}

	// 3. 测试 ModeSelfOnly: 仅自建纯净 4 组订阅，零外部机场，零送中
	selfSubs := client.GetModeSubscriptions(ModeSelfOnly)
	expectedSelf := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other"}
	if len(selfSubs) != len(expectedSelf) {
		t.Fatalf("expected %v, got %v", expectedSelf, selfSubs)
	}
	for i, s := range expectedSelf {
		if selfSubs[i] != s {
			t.Fatalf("at index %d expected %s, got %s", i, s, selfSubs[i])
		}
	}

	// 4. 测试 ModeExternalOnly: 仅外部机场
	extSubs := client.GetModeSubscriptions(ModeExternalOnly)
	if len(extSubs) != 2 || extSubs[0] != "AirportA" || extSubs[1] != "AirportB" {
		t.Fatalf("expected ['AirportA', 'AirportB'], got %v", extSubs)
	}

	// 5. 测试 ModeExternalAndSelf: 外部机场 + 自建 4 组
	allSubs := client.GetModeSubscriptions(ModeExternalAndSelf)
	if len(allSubs) != 6 {
		t.Fatalf("expected 6 subs in external+self, got %d: %v", len(allSubs), allSubs)
	}
	// 验证绝对不能含有废弃的 ashan's vps 或递归的 Q
	for _, s := range allSubs {
		if s == "ashan's vps" || s == "Q" {
			t.Fatalf("forbidden legacy sub '%s' found in allSubs: %v", s, allSubs)
		}
	}
}

func TestUpsertAndDeleteExternalSource(t *testing.T) {
	tmpDir := t.TempDir()
	restore := SetExternalSourcesFileForTest(filepath.Join(tmpDir, "external_sources.json"))
	defer restore()

	client := NewClient(&config.SubStoreConfig{
		Enabled: false,
	})

	// 1. 动态新增外部源 (URL 级纳管，Issue #19)
	item := ExternalSourceItem{
		Name:        "TestAirport",
		DisplayName: "测试主力机场",
		URL:         "https://test.airport.com/clash",
		Enabled:     true,
	}

	if err := client.UpsertExternalSource(context.Background(), item); err != nil {
		t.Fatalf("UpsertExternalSource failed: %v", err)
	}

	sources := client.GetExternalSources()
	if len(sources) != 1 || sources[0] != "TestAirport" {
		t.Fatalf("expected ['TestAirport'], got %v", sources)
	}

	items := client.GetExternalSourceItems()
	if len(items) != 1 || items[0].URL != "https://test.airport.com/clash" {
		t.Fatalf("expected URL 'https://test.airport.com/clash', got %+v", items)
	}

	// 2. 测试禁用
	item.Enabled = false
	if err := client.UpsertExternalSource(context.Background(), item); err != nil {
		t.Fatalf("UpsertExternalSource disable failed: %v", err)
	}
	sources = client.GetExternalSources()
	if len(sources) != 0 {
		t.Fatalf("expected 0 active sources when disabled, got %v", sources)
	}

	// 3. 测试删除
	if err := client.DeleteExternalSource(context.Background(), "TestAirport", false); err != nil {
		t.Fatalf("DeleteExternalSource failed: %v", err)
	}
	items = client.GetExternalSourceItems()
	if len(items) != 0 {
		t.Fatalf("expected 0 items after deletion, got %v", items)
	}
}

func TestCleanupDeadSocks_PathEscapeAndAtomicWrite(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/api/subs" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[{"name":"test sub with spaces","content":"socks5://1.2.3.4:1080\nvless://user@5.6.7.8:443"}]}`))
			return
		}
		if r.Method == "PATCH" {
			requestedPath = r.RequestURI
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     server.URL,
	})

	res, err := client.CleanupDeadSocks(context.Background(), "test sub with spaces")
	if err != nil {
		t.Fatalf("CleanupDeadSocks failed: %v", err)
	}
	if res.RemovedCount != 1 || res.RemainingCount != 1 {
		t.Errorf("expected 1 removed and 1 remaining, got %+v", res)
	}
	if !strings.Contains(requestedPath, "test%20sub%20with%20spaces") {
		t.Errorf("expected URL path to be escaped with test%%20sub%%20with%%20spaces, got %s", requestedPath)
	}
	if _, err := os.Stat(res.BackupPath); err != nil {
		t.Errorf("expected backup file to exist at %s: %v", res.BackupPath, err)
	}
	defer os.Remove(res.BackupPath)
}
