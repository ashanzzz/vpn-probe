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
