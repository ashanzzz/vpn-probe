package vpsmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
)

func TestDiagnoseNode_PortBlocked(t *testing.T) {
	healer := &AutoHealer{}
	res := &model.NodeProbeResult{
		Name:        "Test Node",
		OriginalURL: "vless://uuid@1.2.3.4:12345#Test",
		Online:      false,
		IsSelf:      true,
		Diagnostic: &model.DiagnosticInfo{
			DomesticReachable: false,
			AbroadReachable:   true,
			DomesticPing:      true,
			TargetIP:          "1.2.3.4",
			TargetPort:        12345,
		},
	}

	diag := healer.DiagnoseNode(res)
	if diag.FaultType != "PORT_BLOCKED" {
		t.Fatalf("expected PORT_BLOCKED, got %s", diag.FaultType)
	}
	if !diag.CanAutoHealPort {
		t.Fatalf("expected CanAutoHealPort to be true")
	}
}

func TestDiagnoseNode_ServerBlocked(t *testing.T) {
	healer := &AutoHealer{}
	res := &model.NodeProbeResult{
		Name:        "Test Node",
		OriginalURL: "vless://uuid@1.2.3.4:12345#Test",
		Online:      false,
		IsSelf:      true,
		Diagnostic: &model.DiagnosticInfo{
			DomesticReachable: false,
			AbroadReachable:   true,
			DomesticPing:      false,
			TargetIP:          "1.2.3.4",
			TargetPort:        12345,
		},
	}

	diag := healer.DiagnoseNode(res)
	if diag.FaultType != "SERVER_BLOCKED" {
		t.Fatalf("expected SERVER_BLOCKED, got %s", diag.FaultType)
	}
	if !diag.CanRelayRescue {
		t.Fatalf("expected CanRelayRescue to be true")
	}
}

func TestDiagnoseNode_ProtocolMismatch(t *testing.T) {
	healer := &AutoHealer{}
	res := &model.NodeProbeResult{
		Name:        "Test Node",
		OriginalURL: "vless://uuid@1.2.3.4:12345#Test",
		Online:      false,
		IsSelf:      true,
		Diagnostic: &model.DiagnosticInfo{
			DomesticReachable: true,
			AbroadReachable:   true,
			DomesticPing:      true,
			TargetIP:          "1.2.3.4",
			TargetPort:        12345,
		},
	}

	diag := healer.DiagnoseNode(res)
	if diag.FaultType != "PROTOCOL_MISMATCH" {
		t.Fatalf("expected PROTOCOL_MISMATCH, got %s", diag.FaultType)
	}
	if diag.CanAutoHealPort {
		t.Fatalf("PROTOCOL_MISMATCH must not allow auto-changing port")
	}
}

func TestDiagnoseNode_NetworkCongestion(t *testing.T) {
	healer := &AutoHealer{}
	res := &model.NodeProbeResult{
		Name:        "Test Node Congestion",
		OriginalURL: "vless://uuid@1.2.3.4:12345#Test",
		Online:      false,
		IsSelf:      true,
		Diagnostic: &model.DiagnosticInfo{
			DomesticReachable: true,
			AbroadReachable:   true,
			DomesticPing:      true,
			TargetIP:          "1.2.3.4",
			TargetPort:        12345,
			FaultType:         model.FaultNetworkCongestion,
			TroubleshootTip:   "【网络拥塞】国内外TCP均已握手连通，应用层握手超时",
		},
	}

	diag := healer.DiagnoseNode(res)
	if diag.FaultType != "NETWORK_CONGESTION" {
		t.Fatalf("expected NETWORK_CONGESTION, got %s", diag.FaultType)
	}
	if diag.CanAutoHealPort {
		t.Fatalf("NETWORK_CONGESTION must not allow auto-changing port")
	}
	if diag.CanRelayRescue {
		t.Fatalf("NETWORK_CONGESTION must not trigger relay rescue")
	}
}

func TestGenerateRandomPort(t *testing.T) {
	for i := 0; i < 50; i++ {
		p := GenerateRandomPort()
		if p < 20000 || p > 60000 {
			t.Fatalf("port %d out of bounds [20000, 60000]", p)
		}
	}
}

func TestExtractVersion(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"sing-box version 1.14.4", "1.14.4"},
		{"sing-box version v1.14.4-rc.1", "1.14.4-rc.1"},
		{"Xray 26.11.9 (Xray, Penetrates Everything.)", "26.11.9"},
		{"xray-core version v26.11.9", "26.11.9"},
	}
	for _, tc := range cases {
		v := extractVersion(tc.input)
		if v != tc.expected {
			t.Errorf("extractVersion(%q) = %q; expected %q", tc.input, v, tc.expected)
		}
	}
}

func TestDiagnoseNode_ResidentialSafety(t *testing.T) {
	healer := &AutoHealer{}

	// Case 1: 家宽节点国内超时，境外可通，Ping可通 -> 必须为 RESIDENTIAL_DIRECT_BLOCKED，严禁换端口
	res := &model.NodeProbeResult{
		Name:        "LZ-TW-HiNet-家宽",
		OriginalURL: "vless://uuid@167.104.167.151:48731#LZ-TW-HiNet-家宽",
		Online:      false,
		NodeType:    "residential",
		IsSelf:      true,
		Diagnostic: &model.DiagnosticInfo{
			DomesticReachable: false,
			AbroadReachable:   true,
			DomesticPing:      true,
			TargetIP:          "167.104.167.151",
			TargetPort:        48731,
		},
	}

	diag := healer.DiagnoseNode(res)
	if diag.FaultType != "RESIDENTIAL_DIRECT_BLOCKED" {
		t.Fatalf("expected RESIDENTIAL_DIRECT_BLOCKED, got %s", diag.FaultType)
	}
	if diag.CanAutoHealPort {
		t.Fatalf("residential node must NEVER allow CanAutoHealPort")
	}
	if diag.CanRelayRescue {
		t.Fatalf("residential node must not trigger server relay rescue")
	}

	// Case 2: 家宽节点国内外端口均不通，中转机Ping通 -> 必须为 RESIDENTIAL_TUNNEL_OFFLINE
	res.Diagnostic.AbroadReachable = false
	diag2 := healer.DiagnoseNode(res)
	if diag2.FaultType != "RESIDENTIAL_TUNNEL_OFFLINE" {
		t.Fatalf("expected RESIDENTIAL_TUNNEL_OFFLINE, got %s", diag2.FaultType)
	}
	if diag2.CanAutoHealPort {
		t.Fatalf("residential tunnel offline must not allow CanAutoHealPort")
	}

	// Case 3: 家宽节点国内外端口均不通，中转机Ping不通 -> 必须为 TRANSIT_RELAY_DOWN
	res.Diagnostic.DomesticPing = false
	diag3 := healer.DiagnoseNode(res)
	if diag3.FaultType != "TRANSIT_RELAY_DOWN" {
		t.Fatalf("expected TRANSIT_RELAY_DOWN, got %s", diag3.FaultType)
	}
	if diag3.CanAutoHealPort {
		t.Fatalf("transit relay down must not allow CanAutoHealPort")
	}
}

func TestExecutePortHeal_ResidentialForbidden(t *testing.T) {
	healer := &AutoHealer{}
	resp, err := healer.ExecutePortHeal(nil, nil, "LZ-US-Pavlov-家宽", "vless://uuid@167.104.167.151:49040#Pavlov", 0)
	if err == nil {
		t.Fatalf("expected ExecutePortHeal on residential node to return error, got nil")
	}
	if resp == nil || resp.Success {
		t.Fatalf("expected resp.Success to be false")
	}
}

func TestExecutePortHeal_DockerSingbox_Success(t *testing.T) {
	var executedCmds []string
	det := NewDetector(nil)
	det.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		executedCmds = append(executedCmds, cmd)
		if strings.Contains(cmd, "docker ps") {
			return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 5 hours\n", nil
		}
		if strings.Contains(cmd, "echo 'busy' || echo 'free'") {
			return "free\n", nil
		}
		return "", nil
	})

	healer := NewAutoHealer(det, nil, "")
	inst := &VPSInstance{
		ID:   "vps-docker",
		Name: "Docker VPS",
		IP:   "1.2.3.4",
	}

	ctx := context.Background()
	resp, err := healer.ExecutePortHeal(ctx, inst, "Self-Node-1", "vless://uuid@1.2.3.4:30000#Self-Node-1", 45000)
	if err != nil {
		t.Fatalf("ExecutePortHeal failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected resp.Success to be true, got false")
	}
	if resp.NewPort != 45000 {
		t.Errorf("expected new port 45000, got: %d", resp.NewPort)
	}

	// 检查备份指令、sed 端口替换、语法预检与防火墙放行
	var foundBackup, foundSed, foundCheck, foundRestart bool
	for _, cmd := range executedCmds {
		if strings.Contains(cmd, "cp /opt/sb-mgr/config.json /opt/sb-mgr/config.json.bak") {
			foundBackup = true
		}
		if strings.Contains(cmd, `sed -i -E 's/"(listen_port|port)":\s*30000`) {
			foundSed = true
		}
		if strings.Contains(cmd, "docker exec sb-mgr sing-box check -c /etc/sing-box/config.json") {
			foundCheck = true
		}
		if strings.Contains(cmd, "iptables -I INPUT -p tcp --dport 45000 -j ACCEPT") && strings.Contains(cmd, "docker restart sb-mgr") {
			foundRestart = true
		}
	}
	if !foundBackup || !foundSed || !foundCheck || !foundRestart {
		t.Fatalf("missing expected step: backup=%v sed=%v check=%v restart=%v. Commands: %v",
			foundBackup, foundSed, foundCheck, foundRestart, executedCmds)
	}
}

func TestExecutePortHeal_NativeSingbox_Success(t *testing.T) {
	var executedCmds []string
	det := NewDetector(nil)
	det.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		executedCmds = append(executedCmds, cmd)
		if strings.Contains(cmd, "docker ps") {
			return "/usr/bin/sing-box\n", nil
		}
		if strings.Contains(cmd, "echo 'busy' || echo 'free'") {
			return "free\n", nil
		}
		return "", nil
	})

	healer := NewAutoHealer(det, nil, "")
	inst := &VPSInstance{
		ID:   "vps-native",
		Name: "Native VPS",
		IP:   "5.6.7.8",
	}

	ctx := context.Background()
	resp, err := healer.ExecutePortHeal(ctx, inst, "Self-Native-1", "vless://uuid@5.6.7.8:20000#Self-Native-1", 33333)
	if err != nil {
		t.Fatalf("ExecutePortHeal native failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected resp.Success to be true, got false")
	}

	var foundBackup, foundCheck, foundSystemctl bool
	for _, cmd := range executedCmds {
		if strings.Contains(cmd, "cp /etc/sing-box/config.json /etc/sing-box/config.json.bak") {
			foundBackup = true
		}
		if strings.Contains(cmd, "sing-box check -c /etc/sing-box/config.json") {
			foundCheck = true
		}
		if strings.Contains(cmd, "systemctl restart sing-box") {
			foundSystemctl = true
		}
	}
	if !foundBackup || !foundCheck || !foundSystemctl {
		t.Fatalf("missing expected native step: backup=%v check=%v systemctl=%v. Commands: %v",
			foundBackup, foundCheck, foundSystemctl, executedCmds)
	}
}

func TestExecutePortHeal_Xray_PortReplacementAndRollback(t *testing.T) {
	var executedCmds []string
	det := NewDetector(nil)
	det.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		executedCmds = append(executedCmds, cmd)
		if strings.Contains(cmd, "docker ps") {
			return "/usr/bin/xray\n", nil
		}
		if strings.Contains(cmd, "echo 'busy' || echo 'free'") {
			return "free\n", nil
		}
		if strings.Contains(cmd, "xray -test") {
			return "syntax error on line 42", fmt.Errorf("exit status 1")
		}
		return "", nil
	})

	healer := NewAutoHealer(det, nil, "")
	inst := &VPSInstance{
		ID:   "vps-xray",
		Name: "Xray VPS",
		IP:   "9.9.9.9",
	}

	ctx := context.Background()
	resp, err := healer.ExecutePortHeal(ctx, inst, "Self-Xray-1", "vless://uuid@9.9.9.9:10000#Self-Xray-1", 20000)
	if err == nil {
		t.Fatalf("expected ExecutePortHeal to fail due to preflight check, got nil error")
	}
	if resp == nil || resp.Success {
		t.Fatalf("expected resp.Success to be false")
	}

	// 验证包含针对 xray 的 "port" 正则替换，并在检测失败时执行了 cp .bak 还原
	var foundXraySed, foundRollback bool
	for _, cmd := range executedCmds {
		if strings.Contains(cmd, `"port":\s*10000`) {
			foundXraySed = true
		}
		if strings.Contains(cmd, "cp /etc/xray/config.json.bak /etc/xray/config.json") {
			foundRollback = true
		}
	}
	if !foundXraySed {
		t.Errorf("expected xray sed command targeting 'port': %v", executedCmds)
	}
	if !foundRollback {
		t.Errorf("expected atomic rollback cp .bak: %v", executedCmds)
	}
}

func TestAutoHealer_UpdateLocalConfigYAML_AtomicAndBackup(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `server:
  port: 8080
monitor:
  nodes:
    - name: US-Test
      url: "vless://u@1.2.3.4:22222#US-Test"
`
	if err := os.WriteFile(confPath, []byte(initialYAML), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	healer := &AutoHealer{configPath: confPath}
	oldURL := "vless://u@1.2.3.4:22222#US-Test"
	newURL := "vless://u@1.2.3.4:33333#US-Test"

	if err := healer.updateLocalConfigYAML(oldURL, newURL); err != nil {
		t.Fatalf("updateLocalConfigYAML failed: %v", err)
	}

	// 验证内容已替换
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if !strings.Contains(string(data), newURL) || strings.Contains(string(data), oldURL) {
		t.Errorf("config.yaml not updated properly: %s", string(data))
	}

	// 验证 .bak 备份已生成且包含 oldURL
	bakData, err := os.ReadFile(confPath + ".bak")
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if !strings.Contains(string(bakData), oldURL) {
		t.Errorf("backup file does not contain oldURL: %s", string(bakData))
	}
}

func TestAutoHealer_UpdateSubStoreSubscription_TransitAndLegacy(t *testing.T) {
	updatedSubs := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/api/subs" {
			// 返回包含 ashan-transit 与 ashan's vps 的订阅列表
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-transit","content":"vless://u@1.2.3.4:20000#Transit-1"},
				{"name":"ashan's vps","content":"vless://u@1.2.3.4:20000#Legacy-1"}
			]}`))
			return
		}
		if r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/api/sub/") {
			subName := strings.TrimPrefix(r.URL.Path, "/api/sub/")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			updatedSubs[subName] = "updated"
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	sc := substore.NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     server.URL,
	})

	healer := &AutoHealer{substoreClient: sc}
	err := healer.updateSubStoreSubscription(context.Background(), 20000, 30000, "vless://u@1.2.3.4:20000#Transit-1", "vless://u@1.2.3.4:30000#Transit-1")
	if err != nil {
		t.Fatalf("updateSubStoreSubscription failed: %v", err)
	}

	if _, ok := updatedSubs["ashan-transit"]; !ok {
		t.Errorf("expected ashan-transit to be updated, got: %v", updatedSubs)
	}
	if _, ok := updatedSubs["ashan's vps"]; !ok {
		t.Errorf("expected ashan's vps to be updated, got: %v", updatedSubs)
	}
}

func TestAutoHealer_UpdateSubStoreSubscription_DoNotOverwriteOtherNodesOnSamePort(t *testing.T) {
	var finalContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/api/subs" {
			w.WriteHeader(http.StatusOK)
			// Contains two nodes on port 443, plus another node on 1.2.3.4:4430!
			w.Write([]byte(`{"status":"success","data":[
				{"name":"ashan-transit","content":"vless://u@1.2.3.4:443#NodeA\nvless://u@5.6.7.8:443#NodeB\nvless://u@1.2.3.4:4430#NodeC"}
			]}`))
			return
		}
		if r.Method == "PATCH" && r.URL.Path == "/api/sub/ashan-transit" {
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			finalContent = body.Content
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	sc := substore.NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     server.URL,
	})

	healer := &AutoHealer{substoreClient: sc}
	oldURL := "vless://u@1.2.3.4:443#NodeA"
	newURL := "vless://u@1.2.3.4:34567#NodeA"

	err := healer.updateSubStoreSubscription(context.Background(), 443, 34567, oldURL, newURL)
	if err != nil {
		t.Fatalf("updateSubStoreSubscription failed: %v", err)
	}

	// 验证 5.6.7.8:443 绝未被覆盖，1.2.3.4:443 成功替换为 34567，1.2.3.4:4430 绝未被覆盖
	if !strings.Contains(finalContent, "vless://u@5.6.7.8:443#NodeB") {
		t.Errorf("other node on 443 was overwritten! Content: %s", finalContent)
	}
	if !strings.Contains(finalContent, "vless://u@1.2.3.4:4430#NodeC") {
		t.Errorf("node on 4430 was overwritten! Content: %s", finalContent)
	}
	if !strings.Contains(finalContent, newURL) {
		t.Errorf("target node was not updated! Content: %s", finalContent)
	}
}

func TestAutoHealer_RemoveNodeFromSubStore_PreciseHostMatch(t *testing.T) {
	updatedContents := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/api/subs" {
			w.WriteHeader(http.StatusOK)
			// Contains target IP 1.2.3.4, prefix-sharing IP 1.2.3.45, and tag-referencing IP 9.9.9.9#Tag-1.2.3.4
			content := "vless://u@1.2.3.4:443#TargetNode\n" +
				"vless://u@1.2.3.45:443#PrefixNode\n" +
				"vless://u@9.9.9.9:443#Relay-1.2.3.4"
			w.Write([]byte(fmt.Sprintf(`{"status":"success","data":[{"name":"ashan-transit","content":%q}]}`, content)))
			return
		}
		if r.Method == "PATCH" && r.URL.Path == "/api/sub/ashan-transit" {
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			updatedContents["ashan-transit"] = body.Content
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	sc := substore.NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     server.URL,
	})

	healer := &AutoHealer{substoreClient: sc}
	removed, err := healer.RemoveNodeFromSubStore(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatalf("RemoveNodeFromSubStore failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}

	final := updatedContents["ashan-transit"]
	if strings.Contains(final, "TargetNode") {
		t.Errorf("expected TargetNode to be removed, got: %s", final)
	}
	if !strings.Contains(final, "1.2.3.45:443#PrefixNode") {
		t.Errorf("PrefixNode was mistakenly removed! Content: %s", final)
	}
	if !strings.Contains(final, "9.9.9.9:443#Relay-1.2.3.4") {
		t.Errorf("Relay with tag was mistakenly removed! Content: %s", final)
	}
}

func TestAutoHealer_AddNodeToSubStore_PreventGhostSubCreation(t *testing.T) {
	createdSubs := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/api/subs" {
			// SubStore has NO subscriptions currently (both 404/empty)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[]}`))
			return
		}
		if r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/api/sub/") {
			subName := strings.TrimPrefix(r.URL.Path, "/api/sub/")
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			createdSubs[subName] = body.Content
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	sc := substore.NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     server.URL,
	})

	healer := &AutoHealer{substoreClient: sc}
	newNode := "vless://u@1.2.3.4:443#NewTransit"
	err := healer.AddNodeToSubStore(context.Background(), newNode)
	if err != nil {
		t.Fatalf("AddNodeToSubStore failed: %v", err)
	}

	// 仅 ashan-transit 允许缺省创建
	if _, ok := createdSubs["ashan-transit"]; !ok {
		t.Errorf("expected ashan-transit to be created, got: %v", createdSubs)
	}
	// 历史废弃订阅 ashan's vps 绝不允许死灰复燃
	if _, ok := createdSubs["ashan's vps"]; ok {
		t.Errorf("ghost subscription ashan's vps must NOT be recreated! got: %v", createdSubs)
	}
}
