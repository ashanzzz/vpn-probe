package vpsmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

func TestManager_GetBlockedAnalysis(t *testing.T) {
	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")

	// 1. 空输入验证返回非 nil 的空切片
	emptyRes := m.GetBlockedAnalysis(nil)
	if emptyRes == nil {
		t.Fatalf("expected non-nil empty slice, got nil")
	}
	if len(emptyRes) != 0 {
		t.Fatalf("expected 0 items, got %d", len(emptyRes))
	}

	// 2. 外部节点 (IsSelf=false) 严格过滤跳过
	extNode := &model.NodeProbeResult{
		Name:        "External-Airport-01",
		OriginalURL: "vless://u@1.2.3.4:443#External",
		IsSelf:      false,
	}
	resExt := m.GetBlockedAnalysis([]*model.NodeProbeResult{extNode})
	if len(resExt) != 0 {
		t.Fatalf("expected 0 items for non-self node, got %d", len(resExt))
	}

	// 3. 自建节点正常诊断分析
	selfNode := &model.NodeProbeResult{
		Name:        "JP-Tokyo-Softbank",
		OriginalURL: "vless://u@45.192.242.72:24444#JP-Softbank",
		IsSelf:      true,
		Online:      true,
		Diagnostic: &model.DiagnosticInfo{
			FaultType: model.FaultNone,
		},
	}
	resSelf := m.GetBlockedAnalysis([]*model.NodeProbeResult{selfNode})
	if len(resSelf) != 1 {
		t.Fatalf("expected 1 analysis result, got %d", len(resSelf))
	}
	if resSelf[0].NodeName != "JP-Tokyo-Softbank" {
		t.Errorf("expected NodeName JP-Tokyo-Softbank, got: %s", resSelf[0].NodeName)
	}
}

func TestManager_RefreshFromVPSHub(t *testing.T) {
	var loginStatus atomic.Int32
	var instancesStatus atomic.Int32
	loginStatus.Store(http.StatusOK)
	instancesStatus.Store(http.StatusOK)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			st := int(loginStatus.Load())
			w.WriteHeader(st)
			if st == http.StatusOK {
				_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-jwt-token"})
			}
			return
		}
		if r.URL.Path == "/api/instances" {
			st := int(instancesStatus.Load())
			w.WriteHeader(st)
			if st == http.StatusOK {
				data := map[string]any{
					"instances": []map[string]any{
						{
							"id":       "inst-1",
							"name":     "jp-sb",
							"ip":       "45.192.242.72",
							"ssh_port": 22,
							"ssh_user": "root",
						},
					},
				}
				_ = json.NewEncoder(w).Encode(data)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.SetVPSHubConfig(server.URL, "admin", "admin")

	// 1. 成功从 Hub 同步 (更新 45.192.242.72 的名称为 jp-sb)
	m.refreshFromVPSHub()
	m.instancesMu.RLock()
	var updatedName string
	for _, inst := range m.instances {
		if inst.IP == "45.192.242.72" {
			updatedName = inst.Name
			break
		}
	}
	m.instancesMu.RUnlock()
	if updatedName != "jp-sb" {
		t.Fatalf("expected instance name 'jp-sb' after refresh, got: %s", updatedName)
	}

	// 2. 登录失败 (401 Unauthorized) 验证安全返回且不崩溃、不泄漏
	loginStatus.Store(http.StatusUnauthorized)
	m.refreshFromVPSHub()

	// 3. 实例列表拉取失败 (500 Internal Server Error) 验证安全返回
	loginStatus.Store(http.StatusOK)
	instancesStatus.Store(http.StatusInternalServerError)
	m.refreshFromVPSHub()
}

func TestManager_UpdateSoftware_Docker(t *testing.T) {
	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")

	var executedCmds []string
	det := NewDetector(nil)
	det.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		executedCmds = append(executedCmds, cmd)
		if strings.Contains(cmd, "docker ps") {
			return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 10 hours\n", nil
		}
		return "", nil
	})
	m.SetDetectorForTest(det)

	inst := &VPSInstance{
		ID:   "vps-test",
		Name: "Test VPS",
		IP:   "1.2.3.4",
	}
	m.SetInstancesForTest([]*VPSInstance{inst})

	ctx := context.Background()
	msg, err := m.UpdateSoftware(ctx, "vps-test")
	if err != nil {
		t.Fatalf("UpdateSoftware failed: %v", err)
	}
	if !strings.Contains(msg, "Successfully updated") {
		t.Errorf("unexpected message: %s", msg)
	}

	// 验证包含容器重建事务：pull, rename, stop, run 与 rm -f old
	var foundRebuild bool
	for _, cmd := range executedCmds {
		if strings.Contains(cmd, "docker rename sb-mgr sb-mgr-old") &&
			strings.Contains(cmd, "docker run -d --name sb-mgr") &&
			strings.Contains(cmd, "docker rm -f sb-mgr-old") {
			foundRebuild = true
			break
		}
	}
	if !foundRebuild {
		t.Fatalf("expected docker container rebuild transaction command, executed: %v", executedCmds)
	}
}

func TestManager_UpdateSoftware_Native(t *testing.T) {
	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")

	var executedCmds []string
	det := NewDetector(nil)
	det.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		executedCmds = append(executedCmds, cmd)
		if strings.Contains(cmd, "docker ps") {
			return "/usr/bin/sing-box\n", nil
		}
		return "", nil
	})
	m.SetDetectorForTest(det)

	inst := &VPSInstance{
		ID:   "vps-native",
		Name: "Native VPS",
		IP:   "5.6.7.8",
	}
	m.SetInstancesForTest([]*VPSInstance{inst})

	ctx := context.Background()
	msg, err := m.UpdateSoftware(ctx, "vps-native")
	if err != nil {
		t.Fatalf("UpdateSoftware native failed: %v", err)
	}
	if !strings.Contains(msg, "Successfully updated sing-box (native)") {
		t.Errorf("unexpected message: %s", msg)
	}

	var foundRestart bool
	for _, cmd := range executedCmds {
		if strings.Contains(cmd, "systemctl restart sing-box") {
			foundRestart = true
			break
		}
	}
	if !foundRestart {
		t.Fatalf("expected native service restart in commands: %v", executedCmds)
	}
}
