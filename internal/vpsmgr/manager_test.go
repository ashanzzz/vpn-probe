package vpsmgr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	loginStatus := http.StatusOK
	instancesStatus := http.StatusOK

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			w.WriteHeader(loginStatus)
			if loginStatus == http.StatusOK {
				_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-jwt-token"})
			}
			return
		}
		if r.URL.Path == "/api/instances" {
			w.WriteHeader(instancesStatus)
			if instancesStatus == http.StatusOK {
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
	loginStatus = http.StatusUnauthorized
	m.refreshFromVPSHub()

	// 3. 实例列表拉取失败 (500 Internal Server Error) 验证安全返回
	loginStatus = http.StatusOK
	instancesStatus = http.StatusInternalServerError
	m.refreshFromVPSHub()
}
