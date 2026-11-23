package vpsmgr

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

func TestManager_SafeUpgrade_Success(t *testing.T) {
	det := NewDetector(nil)
	var pullCalled, runCalled bool

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "docker inspect --format='{{.Image}}'") {
			return "sha256:old-singbox-image-hash", nil
		}
		if strings.Contains(cmd, "docker pull") {
			pullCalled = true
		}
		if strings.Contains(cmd, "docker run -d") {
			runCalled = true
		}
		if strings.Contains(cmd, "State.Running") {
			return "true", nil
		}
		if strings.Contains(cmd, "sing-box version") {
			return "sing-box version 1.12.0", nil
		}
		if strings.Contains(cmd, "stat -c") {
			return "1024", nil
		}
		return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 10 hours", nil
	})

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.detector = det
	m.backupMgr = NewBackupManager(det)

	// 模拟沙箱探针冒烟测试：成功且在线
	m.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		return &model.NodeProbeResult{
			Online:    true,
			LatencyMs: 65,
			ExitIP:    "45.192.242.72",
		}
	})

	req := &SafeUpgradeRequest{
		VPSID: "23881", // JPSB4 (45.192.242.72)
	}
	probeResults := []*model.NodeProbeResult{
		{
			Name:        "JPSB4-Node",
			OriginalURL: "vless://u@45.192.242.72:24444#JPSB4",
		},
	}

	resp, err := m.SafeUpgradeSoftware(context.Background(), req, probeResults)
	if err != nil {
		t.Fatalf("SafeUpgradeSoftware failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected success, got false: %s", resp.Message)
	}
	if resp.RolledBack {
		t.Errorf("expected no rollback, but was rolled back: %s", resp.RollbackReason)
	}
	if !pullCalled || !runCalled {
		t.Errorf("expected pull and run called, pull=%v, run=%v", pullCalled, runCalled)
	}
	if resp.SmokeTest == nil || !resp.SmokeTest.Success {
		t.Errorf("expected smoke test success")
	}
}

func TestManager_SafeUpgrade_SmokeFailure_TriggersRollback(t *testing.T) {
	det := NewDetector(nil)
	var rollbackOldImageCalled bool
	var lastBackupFile string

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "cp") && strings.Contains(cmd, "backups/config_") {
			parts := strings.Fields(cmd)
			for _, p := range parts {
				if strings.Contains(p, "backups/config_") {
					lastBackupFile = p
				}
			}
			return "", nil
		}
		if strings.Contains(cmd, "stat -c '%n|%s|%y'") {
			if lastBackupFile != "" {
				return fmt.Sprintf("%s|1024|2026-11-23 10:15:00", lastBackupFile), nil
			}
			return "/opt/sb-mgr/backups/config_20261123_101500.json|1024|2026-11-23 10:15:00", nil
		}
		if strings.Contains(cmd, "stat -c") {
			return "1024", nil
		}
		if strings.Contains(cmd, "docker inspect --format='{{.Image}}'") {
			return "sha256:old-singbox-image-hash", nil
		}
		if strings.Contains(cmd, "State.Running") {
			return "true", nil
		}
		if strings.Contains(cmd, "sha256:old-singbox-image-hash") {
			rollbackOldImageCalled = true
		}
		return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 10 hours", nil
	})

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.detector = det
	m.backupMgr = NewBackupManager(det)

	// 模拟沙箱探针冒烟测试：失败（离线/握手超时）
	m.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		return &model.NodeProbeResult{
			Online:   false,
			ErrorMsg: "context deadline exceeded: reality handshake failed",
		}
	})

	req := &SafeUpgradeRequest{
		VPSID: "23881",
	}
	probeResults := []*model.NodeProbeResult{
		{
			Name:        "JPSB4-Node",
			OriginalURL: "vless://u@45.192.242.72:24444#JPSB4",
		},
	}

	resp, err := m.SafeUpgradeSoftware(context.Background(), req, probeResults)
	if err != nil {
		t.Fatalf("SafeUpgradeSoftware unexpected error: %v", err)
	}

	if resp.Success {
		t.Fatalf("expected failure, got success")
	}
	if !resp.RolledBack {
		t.Fatalf("expected rolled_back to be true")
	}
	if !strings.Contains(resp.RollbackReason, "reality handshake failed") {
		t.Errorf("expected rollback reason to mention handshake failed, got: %s", resp.RollbackReason)
	}
	if !rollbackOldImageCalled {
		t.Errorf("expected rollback command with old image ID to be executed")
	}
}

func TestManager_ExecuteLifecycleAction(t *testing.T) {
	det := NewDetector(nil)
	var lastAction string

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "docker restart") {
			lastAction = "restart"
		} else if strings.Contains(cmd, "docker stop") {
			lastAction = "stop"
		} else if strings.Contains(cmd, "docker start") {
			lastAction = "start"
		}
		return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 10 hours", nil
	})

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.detector = det

	// Test Restart
	info, err := m.ExecuteLifecycleAction(context.Background(), "23881", "restart")
	if err != nil {
		t.Fatalf("ExecuteLifecycleAction restart failed: %v", err)
	}
	if lastAction != "restart" {
		t.Errorf("expected restart executed, got %s", lastAction)
	}
	if info == nil {
		t.Errorf("expected returned info")
	}

	// Test Stop
	_, err = m.ExecuteLifecycleAction(context.Background(), "23881", "stop")
	if err != nil {
		t.Fatalf("ExecuteLifecycleAction stop failed: %v", err)
	}
	if lastAction != "stop" {
		t.Errorf("expected stop executed, got %s", lastAction)
	}
}
