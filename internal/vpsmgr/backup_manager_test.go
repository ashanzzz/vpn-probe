package vpsmgr

import (
	"strings"
	"testing"
)

func TestBackupManager_CreateAndPrune(t *testing.T) {
	det := NewDetector(nil)
	var executedCommands []string

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		executedCommands = append(executedCommands, cmd)
		if strings.Contains(cmd, "stat -c") || strings.Contains(cmd, "wc -c") {
			return "2048", nil
		}
		if strings.Contains(cmd, "ls -1t") {
			// 模拟已有 4 个快照，测试自动清理第 4 个
			return "config_20261123_100003.json\nconfig_20261123_100002.json\nconfig_20261123_100001.json\nconfig_20261123_090000.json", nil
		}
		return "", nil
	})

	bm := NewBackupManager(det)
	inst := &VPSInstance{IP: "1.2.3.4"}
	softInfo := &SoftwareInfo{
		SoftwareType:  "sing-box",
		ContainerName: "sb-mgr",
	}

	snap, err := bm.CreateBackup(inst, softInfo)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	if snap == nil {
		t.Fatalf("expected snapshot, got nil")
	}
	if snap.SizeBytes != 2048 {
		t.Errorf("expected size 2048, got %d", snap.SizeBytes)
	}

	// 验证是否执行了清理旧快照命令
	var foundPrune bool
	for _, cmd := range executedCommands {
		if strings.Contains(cmd, "rm -f") && strings.Contains(cmd, "config_20261123_090000.json") {
			foundPrune = true
			break
		}
	}
	if !foundPrune {
		t.Errorf("expected pruning command for oldest snapshot, executed: %v", executedCommands)
	}
}

func TestBackupManager_ListAndRestore(t *testing.T) {
	det := NewDetector(nil)
	var restartCalled bool

	det.SetMockSSH(func(inst *VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "stat -c '%n|%s|%y'") {
			return "/opt/sb-mgr/backups/config_20261123_100000.json|4096|2026-11-23 10:00:00\n/opt/sb-mgr/backups/config_20261123_090000.json|2048|2026-11-23 09:00:00\n", nil
		}
		if strings.Contains(cmd, "docker restart sb-mgr") {
			restartCalled = true
			return "", nil
		}
		return "", nil
	})

	bm := NewBackupManager(det)
	inst := &VPSInstance{IP: "1.2.3.4"}
	softInfo := &SoftwareInfo{
		SoftwareType:  "sing-box",
		ContainerName: "sb-mgr",
	}

	backups, err := bm.ListBackups(inst, softInfo)
	if err != nil {
		t.Fatalf("ListBackups error: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(backups))
	}
	if backups[0].SizeBytes != 4096 {
		t.Errorf("expected first backup size 4096, got %d", backups[0].SizeBytes)
	}

	// 还原快照
	err = bm.RestoreBackup(inst, softInfo, "20261123_100000")
	if err != nil {
		t.Fatalf("RestoreBackup error: %v", err)
	}
	if !restartCalled {
		t.Errorf("expected container restart after restore")
	}
}
