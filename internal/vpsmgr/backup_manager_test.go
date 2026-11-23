package vpsmgr

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

// mockSSHFS 模拟远端主机文件系统状态与执行环境
type mockSSHFS struct {
	mu       sync.Mutex
	files    map[string]string // path -> content
	commands []string
}

func newMockSSHFS() *mockSSHFS {
	return &mockSSHFS{
		files: map[string]string{
			"/opt/sb-mgr/config.json": `{"log":{"level":"info"}}`,
			"/etc/x-ui/x-ui.db":       "sqlite-dummy-db-content",
			"/etc/sing-box/config.json": `{"log":{"level":"warn"}}`,
		},
		commands: make([]string, 0),
	}
}

func (m *mockSSHFS) run(inst *VPSInstance, cmd string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = append(m.commands, cmd)

	// 1. mkdir
	if strings.HasPrefix(cmd, "mkdir -p") {
		return "", nil
	}

	// 2. stat -c %s (size)
	if strings.Contains(cmd, "stat -c %s") || strings.Contains(cmd, "wc -c") {
		return "1024", nil
	}

	// 3. cp
	if strings.HasPrefix(cmd, "cp ") {
		parts := strings.Fields(cmd)
		if len(parts) >= 3 {
			src := parts[1]
			dst := parts[2]
			if content, ok := m.files[src]; ok {
				m.files[dst] = content
			} else {
				m.files[dst] = "copied-content"
			}
		}
		return "", nil
	}

	// 4. sqlite backup
	if strings.Contains(cmd, "sqlite3") && strings.Contains(cmd, ".backup") {
		return "", nil
	}

	// 5. stat -c '%n|%s|%y' list backups
	if strings.Contains(cmd, "stat -c '%n|%s|%y'") {
		var lines []string
		for p := range m.files {
			if strings.Contains(p, "/backups/") {
				lines = append(lines, fmt.Sprintf("%s|1024|2026-11-23 10:15:00", p))
			}
		}
		return strings.Join(lines, "\n"), nil
	}

	// 6. ls -1t
	if strings.HasPrefix(cmd, "ls -1t") {
		var names []string
		for p := range m.files {
			if strings.Contains(p, "/backups/") {
				parts := strings.Split(p, "/")
				names = append(names, parts[len(parts)-1])
			}
		}
		return strings.Join(names, "\n"), nil
	}

	// 7. rm -f
	if strings.HasPrefix(cmd, "rm -f") {
		parts := strings.Fields(cmd)
		if len(parts) >= 2 {
			delete(m.files, parts[1])
		}
		return "", nil
	}

	// 8. docker / systemctl
	if strings.HasPrefix(cmd, "docker ") || strings.Contains(cmd, "systemctl") {
		return "ok", nil
	}

	return "", nil
}

func TestBackupManager_CreateAndListBackups(t *testing.T) {
	fs := newMockSSHFS()
	detector := NewDetector(nil)
	detector.SetSSHRunner(fs.run)
	bm := NewBackupManager(detector)

	inst := &VPSInstance{
		ID:   "vps-tokyo",
		Name: "东京软银",
		IP:   "45.192.242.72",
	}

	// 1. Docker sing-box 备份
	softDocker := &SoftwareInfo{
		SoftwareType:  "sing-box",
		DeployMode:    "docker",
		ContainerName: "sb-mgr",
		ConfigPath:    "/opt/sb-mgr/config.json",
	}

	snap, err := bm.CreateBackup(inst, softDocker)
	if err != nil {
		t.Fatalf("CreateBackup docker sing-box failed: %v", err)
	}

	if snap == nil || snap.FileName == "" {
		t.Fatalf("expected non-empty snapshot, got nil")
	}

	// 验证 ID 格式与时间戳一致 (非截断的 150405)
	if len(snap.ID) < 15 || !strings.Contains(snap.ID, "_") {
		t.Errorf("expected full timestamp ID (YYYYMMDD_HHMMSS), got: %s", snap.ID)
	}

	// 2. 列出快照
	snaps, err := bm.ListBackups(inst, softDocker)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatalf("expected at least 1 backup in list, got 0")
	}

	// 验证 ListBackups 提取的 ID 与 CreateBackup 的 ID 保持一致，无截断
	listSnap := snaps[0]
	if !strings.Contains(snap.FileName, listSnap.ID) {
		t.Errorf("ListBackups snapshot ID '%s' mismatch with filename '%s'", listSnap.ID, snap.FileName)
	}

	// 3. 原生 Native sing-box 备份
	softNative := &SoftwareInfo{
		SoftwareType: "sing-box",
		DeployMode:   "native",
		ConfigPath:   "/etc/sing-box/config.json",
	}

	snapNative, err := bm.CreateBackup(inst, softNative)
	if err != nil {
		t.Fatalf("CreateBackup native sing-box failed: %v", err)
	}
	if !strings.Contains(snapNative.FilePath, "/etc/sing-box/backups") {
		t.Errorf("expected native backup path in /etc/sing-box/backups, got: %s", snapNative.FilePath)
	}
}

func TestBackupManager_PruneOldBackups(t *testing.T) {
	fs := newMockSSHFS()
	// 预注入 5 份快照
	fs.files["/opt/sb-mgr/backups/config_20261123_100100.json"] = "snap1"
	fs.files["/opt/sb-mgr/backups/config_20261123_100200.json"] = "snap2"
	fs.files["/opt/sb-mgr/backups/config_20261123_100300.json"] = "snap3"
	fs.files["/opt/sb-mgr/backups/config_20261123_100400.json"] = "snap4"
	fs.files["/opt/sb-mgr/backups/config_20261123_100500.json"] = "snap5"

	detector := NewDetector(nil)
	detector.SetSSHRunner(fs.run)
	bm := NewBackupManager(detector)

	inst := &VPSInstance{IP: "45.192.242.72"}
	bm.pruneOldBackups(inst, "/opt/sb-mgr/backups")

	// 验证执行了 rm -f 指令
	var rmCount int
	for _, c := range fs.commands {
		if strings.HasPrefix(c, "rm -f") {
			rmCount++
		}
	}
	if rmCount == 0 {
		t.Errorf("expected prune to remove stale backups, but rm -f was not called")
	}
}

func TestBackupManager_RestoreBackup(t *testing.T) {
	fs := newMockSSHFS()
	fs.files["/opt/sb-mgr/backups/config_20261123_101500.json"] = "valid-backup-content"

	detector := NewDetector(nil)
	detector.SetSSHRunner(fs.run)
	bm := NewBackupManager(detector)

	inst := &VPSInstance{IP: "45.192.242.72"}
	softInfo := &SoftwareInfo{
		SoftwareType:  "sing-box",
		DeployMode:    "docker",
		ContainerName: "sb-mgr",
		ConfigPath:    "/opt/sb-mgr/config.json",
	}

	// 1. 还原存在快照
	err := bm.RestoreBackup(inst, softInfo, "20261123_101500")
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	// 验证执行了 cp 和 docker restart
	var foundRestore bool
	for _, c := range fs.commands {
		if strings.Contains(c, "cp ") && strings.Contains(c, "docker restart sb-mgr") {
			foundRestore = true
			break
		}
	}
	if !foundRestore {
		t.Errorf("expected restore command with docker restart, commands run: %v", fs.commands)
	}

	// 2. 还原不存在快照应报错
	err = bm.RestoreBackup(inst, softInfo, "non_existent_snap")
	if err == nil {
		t.Fatalf("expected error for non-existent snapshot, got nil")
	}
}

func TestManager_LifecycleAndSafeUpgrade(t *testing.T) {
	fs := newMockSSHFS()
	detector := NewDetector(nil)
	detector.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "sing-box version") {
			return "sing-box version 1.10.0", nil
		}
		if strings.Contains(cmd, "docker ps") {
			return "sb-mgr|ghcr.io/sagernet/sing-box:latest|Up 2 hours\n", nil
		}
		return fs.run(inst, cmd)
	})

	cfg := &config.Config{}
	m := NewManager(cfg, nil, "")
	m.detector = detector
	m.backupMgr = NewBackupManager(detector)

	inst := &VPSInstance{
		ID:   "vps-lax",
		Name: "美西 ACCK",
		IP:   "38.175.222.44",
	}
	m.instances = []*VPSInstance{inst}

	ctx := context.Background()

	// 1. 执行 Lifecycle Action: restart
	res, err := m.PerformLifecycleAction(ctx, &LifecycleActionRequest{
		VPSID:  "vps-lax",
		Action: "restart",
	})
	if err != nil {
		t.Fatalf("PerformLifecycleAction restart failed: %v", err)
	}
	if !strings.Contains(res, "Successfully") {
		t.Errorf("expected success message, got: %s", res)
	}

	// 2. 执行 Lifecycle Action: backup
	res, err = m.PerformLifecycleAction(ctx, &LifecycleActionRequest{
		VPSID:  "vps-lax",
		Action: "backup",
	})
	if err != nil {
		t.Fatalf("PerformLifecycleAction backup failed: %v", err)
	}
	if !strings.Contains(res, "Created snapshot") {
		t.Errorf("expected snapshot created message, got: %s", res)
	}

	// 3. 安全升级测试 - 冒烟测试成功分支
	m.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		return &model.NodeProbeResult{
			Online:    true,
			LatencyMs: 120,
			ExitIP:    "38.175.222.44",
		}
	})

	upResp, err := m.PerformSafeUpgrade(ctx, &SafeUpgradeRequest{
		VPSID:    "vps-lax",
		NodeName: "美西 ACCK 洛杉矶 4837",
	})
	if err != nil {
		t.Fatalf("PerformSafeUpgrade failed: %v", err)
	}
	if !upResp.Success || upResp.RolledBack {
		t.Fatalf("expected upgrade success without rollback, got: %+v", upResp)
	}
	if upResp.SmokeTest == nil || !upResp.SmokeTest.Success {
		t.Errorf("expected successful smoke test result")
	}

	// 4. 安全升级测试 - 冒烟测试失败自动回滚分支
	m.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		return &model.NodeProbeResult{
			Online:    false,
			Diagnostic: &model.DiagnosticInfo{
				StatusText: "Connection refused on port",
			},
		}
	})

	upRespFail, err := m.PerformSafeUpgrade(ctx, &SafeUpgradeRequest{
		VPSID:    "vps-lax",
		NodeName: "美西 ACCK 洛杉矶 4837",
	})
	if err != nil {
		t.Fatalf("PerformSafeUpgrade should handle rollback gracefully without unhandled error, got: %v", err)
	}
	if upRespFail.Success {
		t.Fatalf("expected upgrade to report failure when smoke test fails")
	}
	if !upRespFail.RolledBack {
		t.Fatalf("expected automatic rollback to be triggered on smoke failure")
	}
	if !strings.Contains(upRespFail.RollbackReason, "Smoke test failed") {
		t.Errorf("expected rollback reason to indicate smoke failure, got: %s", upRespFail.RollbackReason)
	}
}
