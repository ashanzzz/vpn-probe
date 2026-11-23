package vpsmgr

import (
	"context"
	"os"
	"strings"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

func TestManager_NATVPS_PortHealConstraint(t *testing.T) {
	det := NewDetector(nil)

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		// 模拟 30001 已被占用，30002 空闲
		if strings.Contains(cmd, ":30001 ") {
			return "busy", nil
		}
		if strings.Contains(cmd, ":30002 ") {
			return "free", nil
		}
		if strings.Contains(cmd, "docker exec") || strings.Contains(cmd, "docker restart") {
			return "", nil
		}
		return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 5 hours", nil
	})

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.detector = det
	m.healer = NewAutoHealer(det, nil, "")

	// 注册一台 NAT VPS
	natInst, err := m.RegisterInstance(&RegisterInstanceRequest{
		Name:     "HK-NAT-01",
		IP:       "103.1.2.3",
		IsNAT:    true,
		NATPorts: []int{30001, 30002, 30003},
	})
	if err != nil {
		t.Fatalf("RegisterInstance failed: %v", err)
	}

	probeResults := []*model.NodeProbeResult{
		{
			Name:        "HK-NAT-Node",
			OriginalURL: "vless://u@103.1.2.3:30001#HK-NAT",
			IsSelf:      true,
			Online:      false,
			Diagnostic: &model.DiagnosticInfo{
				FaultType:         model.FaultPortBlocked,
				DomesticReachable: false,
				AbroadReachable:   true,
				DomesticPing:      true,
			},
		},
	}

	resp, err := m.HealPort(context.Background(), "HK-NAT-Node", 0, probeResults)
	if err != nil {
		t.Fatalf("HealPort failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected heal success, got false: %s", resp.Message)
	}
	// 验证新端口是否是从 NATPorts 中挑选出的空闲端口 30002
	if resp.NewPort != 30002 {
		t.Errorf("expected NAT VPS to select port 30002, got %d", resp.NewPort)
	}
	if !natInst.IsNAT {
		t.Errorf("expected is_nat to be true")
	}
}

func TestManager_InstallMultiCoreReality(t *testing.T) {
	det := NewDetector(nil)
	var executedCmds []string

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		executedCmds = append(executedCmds, cmd)
		return "", nil
	})

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.detector = det

	inst, _ := m.RegisterInstance(&RegisterInstanceRequest{
		Name: "JP-Test",
		IP:   "45.192.242.72",
	})

	// 1. 安装 sing-box Reality
	msg, err := m.InstallSoftware(context.Background(), inst.ID, "sing-box")
	if err != nil {
		t.Fatalf("Install sing-box failed: %v", err)
	}
	if !strings.Contains(msg, "Successfully installed sing-box VLESS Reality") {
		t.Errorf("unexpected msg: %s", msg)
	}

	// 2. 安装 3x-ui Reality
	msgUI, err := m.InstallSoftware(context.Background(), inst.ID, "3x-ui")
	if err != nil {
		t.Fatalf("Install 3x-ui failed: %v", err)
	}
	if !strings.Contains(msgUI, "Successfully installed 3x-ui") {
		t.Errorf("unexpected msg: %s", msgUI)
	}

	// 3. 安装 Xray Reality
	msgXray, err := m.InstallSoftware(context.Background(), inst.ID, "xray")
	if err != nil {
		t.Fatalf("Install xray failed: %v", err)
	}
	if !strings.Contains(msgXray, "Successfully installed Xray VLESS Reality") {
		t.Errorf("unexpected msg: %s", msgXray)
	}
}

func TestManager_UninstallSoftware(t *testing.T) {
	det := NewDetector(nil)
	var removedContainers, removedDirs bool

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "docker rm -f") {
			removedContainers = true
		}
		if strings.Contains(cmd, "rm -rf") {
			removedDirs = true
		}
		return "", nil
	})

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.detector = det

	inst, _ := m.RegisterInstance(&RegisterInstanceRequest{
		Name: "US-LAX-Test",
		IP:   "38.175.222.44",
	})

	req := &UninstallRequest{
		VPSID:         inst.ID,
		CleanSubStore: false,
		RemoveData:    true,
	}

	resp, err := m.UninstallSoftware(context.Background(), req)
	if err != nil {
		t.Fatalf("UninstallSoftware failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success")
	}
	if !removedContainers || !removedDirs {
		t.Errorf("expected containers and dirs removed, c=%v, d=%v", removedContainers, removedDirs)
	}

	// 清理临时测试持久化文件
	_ = os.Remove("./data/managed_instances.json")
}
