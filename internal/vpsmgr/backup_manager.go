package vpsmgr

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"vpn-probe/internal/logger"
)

const MaxBackupRetention = 3

// BackupManager 处理远程 VPS 代理软件的轻量原子备份与还原
type BackupManager struct {
	detector *Detector
}

func NewBackupManager(detector *Detector) *BackupManager {
	return &BackupManager{detector: detector}
}

// resolveBackupPaths 统一解析指定代理软件与部署模式的配置路径、备份目录与重启指令
func resolveBackupPaths(softInfo *SoftwareInfo) (backupDir, activeFile, fileExt string, isSqlite bool, restartCmd string, err error) {
	if softInfo == nil {
		return "", "", "", false, "", fmt.Errorf("software info is nil")
	}

	if softInfo.DeployMode == "native" {
		if softInfo.SoftwareType == "sing-box" {
			activeFile = "/etc/sing-box/config.json"
			if softInfo.ConfigPath != "" {
				activeFile = softInfo.ConfigPath
			}
			backupDir = path.Join(path.Dir(activeFile), "backups")
			restartCmd = "systemctl restart sing-box 2>/dev/null || service sing-box restart 2>/dev/null"
			return backupDir, activeFile, ".json", false, restartCmd, nil
		} else if softInfo.SoftwareType == "xray" {
			activeFile = "/etc/xray/config.json"
			if softInfo.ConfigPath != "" {
				activeFile = softInfo.ConfigPath
			}
			backupDir = path.Join(path.Dir(activeFile), "backups")
			restartCmd = "systemctl restart xray 2>/dev/null || service xray restart 2>/dev/null"
			return backupDir, activeFile, ".json", false, restartCmd, nil
		} else if softInfo.SoftwareType == "3x-ui" {
			activeFile = "/etc/x-ui/x-ui.db"
			if softInfo.ConfigPath != "" {
				activeFile = softInfo.ConfigPath
			}
			backupDir = path.Join(path.Dir(activeFile), "backups")
			restartCmd = "systemctl restart x-ui 2>/dev/null || service x-ui restart 2>/dev/null"
			return backupDir, activeFile, ".db", true, restartCmd, nil
		}
	}

	if softInfo.SoftwareType == "3x-ui" {
		backupDir = "/etc/x-ui/backups"
		activeFile = "/etc/x-ui/x-ui.db"
		if softInfo.ConfigPath != "" {
			activeFile = softInfo.ConfigPath
			backupDir = path.Join(path.Dir(activeFile), "backups")
		}
		return backupDir, activeFile, ".db", true, "docker restart 3x-ui", nil
	}

	switch softInfo.SoftwareType {
	case "sing-box":
		cName := softInfo.ContainerName
		if cName == "" {
			cName = "sb-mgr"
		}
		backupDir = fmt.Sprintf("/opt/%s/backups", cName)
		activeFile = fmt.Sprintf("/opt/%s/config.json", cName)
		if softInfo.ConfigPath != "" {
			activeFile = softInfo.ConfigPath
			backupDir = path.Join(path.Dir(activeFile), "backups")
		}
		restartCmd = fmt.Sprintf("docker restart %s", cName)
		return backupDir, activeFile, ".json", false, restartCmd, nil

	case "xray":
		cName := softInfo.ContainerName
		if cName == "" {
			cName = "v2ray-agent-docker"
		}
		backupDir = "/etc/v2ray-agent/backups"
		activeFile = "/etc/v2ray-agent/docker/config.json"
		if softInfo.ConfigPath != "" {
			activeFile = softInfo.ConfigPath
			backupDir = path.Join(path.Dir(activeFile), "backups")
		}
		restartCmd = fmt.Sprintf("docker restart %s", cName)
		return backupDir, activeFile, ".json", false, restartCmd, nil

	default:
		return "", "", "", false, "", fmt.Errorf("unsupported software type for backup: %s", softInfo.SoftwareType)
	}
}

// CreateBackup 为指定 VPS 上的代理软件创建轻量原子时间戳快照并滚动保留最近 3 份
func (bm *BackupManager) CreateBackup(inst *VPSInstance, softInfo *SoftwareInfo) (*BackupSnapshot, error) {
	if inst == nil || softInfo == nil {
		return nil, fmt.Errorf("instance or software info is nil")
	}

	backupDir, activeFile, fileExt, isSqlite, _, err := resolveBackupPaths(softInfo)
	if err != nil {
		return nil, err
	}

	ts := time.Now().Format("20060102_150405")
	snapPrefix := "config"
	if softInfo.SoftwareType == "3x-ui" {
		snapPrefix = "x-ui"
	}
	snapFile := fmt.Sprintf("%s/%s_%s%s", backupDir, snapPrefix, ts, fileExt)

	// 1. 创建备份目录
	mkdirCmd := fmt.Sprintf("mkdir -p %s", backupDir)
	if _, err := bm.detector.RunSSH(inst, mkdirCmd); err != nil {
		return nil, fmt.Errorf("create backup dir '%s': %w", backupDir, err)
	}

	// 2. 执行原子热备份
	var copyCmd string
	if isSqlite {
		// 优先使用 sqlite3 在线热备保证事务一致性，若主机无 sqlite3 则使用普通 cp
		copyCmd = fmt.Sprintf(
			"if command -v sqlite3 >/dev/null 2>&1; then sqlite3 %s \".backup '%s'\"; elif docker ps | grep -q ' 3x-ui'; then docker exec 3x-ui sqlite3 %s \".backup '%s'\"; else cp %s %s; fi",
			activeFile, snapFile, activeFile, snapFile, activeFile, snapFile,
		)
	} else {
		copyCmd = fmt.Sprintf("cp %s %s", activeFile, snapFile)
	}

	if out, err := bm.detector.RunSSH(inst, copyCmd); err != nil {
		return nil, fmt.Errorf("execute backup command: %w (output: %s)", err, out)
	}

	// 3. 校验备份文件大小
	sizeCmd := fmt.Sprintf("stat -c %%s %s 2>/dev/null || wc -c < %s 2>/dev/null", snapFile, snapFile)
	sizeOut, err := bm.detector.RunSSH(inst, sizeCmd)
	var sizeBytes int64
	if err == nil {
		sizeBytes, _ = strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)
	}

	snap := &BackupSnapshot{
		ID:        ts,
		FileName:  path.Base(snapFile),
		FilePath:  snapFile,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
		SizeBytes: sizeBytes,
	}

	logger.Infof("[BACKUP] Created snapshot %s on %s (%s, %d bytes)", snap.FileName, inst.IP, softInfo.SoftwareType, sizeBytes)

	// 4. 清理旧快照，仅滚动保留最新 MaxBackupRetention 份
	bm.pruneOldBackups(inst, backupDir)

	return snap, nil
}

// pruneOldBackups 清理超出保留数量的旧快照
func (bm *BackupManager) pruneOldBackups(inst *VPSInstance, backupDir string) {
	// 列出文件按时间从新到旧排列
	listCmd := fmt.Sprintf("ls -1t %s 2>/dev/null", backupDir)
	out, err := bm.detector.RunSSH(inst, listCmd)
	if err != nil {
		return
	}

	files := strings.Split(strings.TrimSpace(out), "\n")
	var validFiles []string
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f != "" && (strings.HasSuffix(f, ".json") || strings.HasSuffix(f, ".db") || strings.HasSuffix(f, ".bak")) {
			validFiles = append(validFiles, f)
		}
	}

	if len(validFiles) > MaxBackupRetention {
		for _, stale := range validFiles[MaxBackupRetention:] {
			delCmd := fmt.Sprintf("rm -f %s/%s", backupDir, stale)
			bm.detector.RunSSH(inst, delCmd)
			logger.Infof("[BACKUP] Pruned old snapshot: %s/%s on %s", backupDir, stale, inst.IP)
		}
	}
}

// ListBackups 列出指定 VPS 对应软件当前所有可用快照
func (bm *BackupManager) ListBackups(inst *VPSInstance, softInfo *SoftwareInfo) ([]*BackupSnapshot, error) {
	if inst == nil || softInfo == nil {
		return nil, fmt.Errorf("instance or software info is nil")
	}

	backupDir, _, _, _, _, err := resolveBackupPaths(softInfo)
	if err != nil {
		return []*BackupSnapshot{}, nil
	}

	// 格式输出: %n (filename) | %s (size) | %y (modified time)
	listCmd := fmt.Sprintf("stat -c '%%n|%%s|%%y' %s/* 2>/dev/null", backupDir)
	out, err := bm.detector.RunSSH(inst, listCmd)
	if err != nil || strings.TrimSpace(out) == "" {
		return []*BackupSnapshot{}, nil
	}

	var snapshots []*BackupSnapshot
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		fPath := parts[0]
		fName := path.Base(fPath)
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		modTime := parts[2]
		if len(modTime) >= 19 {
			modTime = modTime[:19]
		}

		// 从文件名中提取完整时间戳 ID (例如 config_20261123_101500.json -> 20261123_101500)
		id := fName
		if idx := strings.LastIndex(fName, "."); idx != -1 {
			id = fName[:idx]
		}
		if idx := strings.Index(id, "_"); idx != -1 {
			id = id[idx+1:]
		}

		snapshots = append(snapshots, &BackupSnapshot{
			ID:        id,
			FileName:  fName,
			FilePath:  fPath,
			CreatedAt: modTime,
			SizeBytes: size,
		})
	}

	// 按时间倒序排序
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt > snapshots[j].CreatedAt
	})

	return snapshots, nil
}

// RestoreBackup 从指定快照还原配置并重启容器
func (bm *BackupManager) RestoreBackup(inst *VPSInstance, softInfo *SoftwareInfo, snapshotID string) error {
	if inst == nil || softInfo == nil {
		return fmt.Errorf("instance or software info is nil")
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return fmt.Errorf("snapshot_id is required")
	}

	backups, err := bm.ListBackups(inst, softInfo)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}

	var targetSnap *BackupSnapshot
	for _, b := range backups {
		if b.ID == snapshotID || b.FileName == snapshotID || strings.Contains(b.FileName, snapshotID) {
			targetSnap = b
			break
		}
	}

	if targetSnap == nil {
		return fmt.Errorf("snapshot '%s' not found on %s", snapshotID, inst.IP)
	}

	_, activeFile, _, _, restartCmd, err := resolveBackupPaths(softInfo)
	if err != nil {
		return err
	}

	// 执行还原与重启
	cmd := fmt.Sprintf("cp %s %s && %s", targetSnap.FilePath, activeFile, restartCmd)
	out, err := bm.detector.RunSSH(inst, cmd)
	if err != nil {
		return fmt.Errorf("restore snapshot failed: %w (output: %s)", err, out)
	}

	logger.Infof("[BACKUP] Successfully restored snapshot %s on %s and restarted container", targetSnap.FileName, inst.IP)
	return nil
}
