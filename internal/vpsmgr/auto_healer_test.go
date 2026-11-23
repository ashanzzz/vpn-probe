package vpsmgr

import (
	"context"
	"strings"
	"testing"

	"vpn-probe/internal/model"
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
		if strings.Contains(cmd, "sed -i 's/\"listen_port\": 30000/\"listen_port\": 45000/g' /opt/sb-mgr/config.json") {
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
