package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/vpsmgr"
)

func TestVPSAPIExtensions(t *testing.T) {
	ResetAuthTracker()

	cfg := &config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-vps-token",
		},
	}

	det := vpsmgr.NewDetector(nil)
	det.SetSSHRunner(func(inst *vpsmgr.VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "sing-box version") {
			return "sing-box version 1.12.0", nil
		}
		if strings.Contains(cmd, "docker ps") {
			return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 10 hours\n", nil
		}
		if strings.Contains(cmd, "stat -c '%n|%s|%y'") {
			return "/opt/sb-mgr/backups/config_20261123_101500.json|2048|2026-11-23 10:15:00", nil
		}
		if strings.Contains(cmd, "stat -c %s") {
			return "2048", nil
		}
		return "ok", nil
	})

	vm := vpsmgr.NewManager(cfg, nil, "")
	vm.SetDetectorForTest(det)

	inst := &vpsmgr.VPSInstance{
		ID:   "vps-01",
		Name: "东京软银",
		IP:   "45.192.242.72",
	}
	vm.SetInstancesForTest([]*vpsmgr.VPSInstance{inst})

	server := NewAPIServer(cfg, nil, nil, nil, nil, nil, nil)
	server.SetVPSManager(vm)
	app := server.app

	// 1. handleVPSInstall 支持 node_name 与 software_type
	installBody := map[string]string{
		"node_name":     "东京软银",
		"software_type": "sing-box",
	}
	rawInstall, _ := json.Marshal(installBody)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/manage/install", bytes.NewReader(rawInstall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-vps-token")
	resp, err := app.Test(req, 5000)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handleVPSInstall failed, status=%d, err=%v", resp.StatusCode, err)
	}

	// 2. handleVPSUpdate 支持 node_name
	updateBody := map[string]string{
		"node_name": "东京软银",
	}
	rawUpdate, _ := json.Marshal(updateBody)
	req = httptest.NewRequest(http.MethodPost, "/api/vps/manage/update", bytes.NewReader(rawUpdate))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-vps-token")
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handleVPSUpdate failed, status=%d, err=%v", resp.StatusCode, err)
	}

	// 3. handleVPSLifecycle: restart
	lifeBody := map[string]string{
		"vps_id": "vps-01",
		"action": "restart",
	}
	rawLife, _ := json.Marshal(lifeBody)
	req = httptest.NewRequest(http.MethodPost, "/api/vps/lifecycle", bytes.NewReader(rawLife))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-vps-token")
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handleVPSLifecycle restart failed, status=%d, err=%v", resp.StatusCode, err)
	}

	// 4. handleVPSListBackups
	req = httptest.NewRequest(http.MethodGet, "/api/vps/backups/vps-01", nil)
	req.Header.Set("Authorization", "Bearer test-vps-token")
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handleVPSListBackups failed, status=%d, err=%v", resp.StatusCode, err)
	}
	var backupResp struct {
		Status  string                   `json:"status"`
		Total   int                      `json:"total"`
		Backups []*vpsmgr.BackupSnapshot `json:"backups"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&backupResp)
	if backupResp.Total == 0 || len(backupResp.Backups) == 0 {
		t.Fatalf("expected at least 1 backup in response, got %d", backupResp.Total)
	}

	// 5. handleVPSRestore
	restoreBody := map[string]string{
		"vps_id":      "vps-01",
		"snapshot_id": "20261123_101500",
	}
	rawRestore, _ := json.Marshal(restoreBody)
	req = httptest.NewRequest(http.MethodPost, "/api/vps/restore", bytes.NewReader(rawRestore))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-vps-token")
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handleVPSRestore failed, status=%d, err=%v", resp.StatusCode, err)
	}

	// 6. handleVPSSafeUpgrade with smoke probe
	vm.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		return &model.NodeProbeResult{
			Online:    true,
			LatencyMs: 95,
			ExitIP:    "45.192.242.72",
		}
	})

	safeUpgradeBody := map[string]string{
		"vps_id":    "vps-01",
		"node_name": "东京软银",
	}
	rawSafe, _ := json.Marshal(safeUpgradeBody)
	req = httptest.NewRequest(http.MethodPost, "/api/vps/upgrade/safe", bytes.NewReader(rawSafe))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-vps-token")
	resp, err = app.Test(req, 5000)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handleVPSSafeUpgrade failed, status=%d, err=%v", resp.StatusCode, err)
	}
	var upgradeResp vpsmgr.SafeUpgradeResponse
	_ = json.NewDecoder(resp.Body).Decode(&upgradeResp)
	if !upgradeResp.Success || upgradeResp.RolledBack {
		t.Fatalf("expected successful safe upgrade, got: %+v", upgradeResp)
	}
}
