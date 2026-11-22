package vpsmgr

import (
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
