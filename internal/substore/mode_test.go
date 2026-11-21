package substore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
