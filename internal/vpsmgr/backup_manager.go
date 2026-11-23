package vpsmgr

import (
	"fmt"
	"path/filepath"
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

// CreateBackup 为指定 VPS 上的代理软件创建轻量原子时间戳快照并滚动保留最近 3 份
func (bm *BackupManager) CreateBackup(inst *VPSInstance, softInfo *SoftwareInfo) (*BackupSnapshot, error) {
	if inst == nil || softInfo == nil {
		return nil, fmt.Errorf("instance or software info is nil")
	}

	ts := time.Now().Format("20060102_150405")
	var backupDir, activeFile, snapFile string

	switch softInfo.SoftwareType {
	case "sing-box":
		cName := softInfo.ContainerName
		if cName == "" {
			cName = "sb-mgr"
		}
		backupDir = fmt.Sprintf("/opt/%s/backups", cName)
		activeFile = fmt.Sprintf("/opt/%s/config.json", cName)
		snapFile = fmt.Sprintf("%s/config_%s.json", backupDir, ts)

	case "3x-ui":
		backupDir = "/etc/x-ui/backups"
		activeFile = "/etc/x-ui/x-ui.db"
		snapFile = fmt.Sprintf("%s/x-ui_%s.db", backupDir, ts)

	case "xray":
		cName := softInfo.ContainerName
		if cName == "" {
			cName = "v2ray-agent-docker"
		}
		backupDir = "/etc/v2ray-agent/backups"
		activeFile = "/etc/v2ray-agent/docker/config.json"
		snapFile = fmt.Sprintf("%s/config_%s.json", backupDir, ts)

	default:
		return nil, fmt.Errorf("unsupported software type for backup: %s", softInfo.SoftwareType)
	}

	// 1. 创建备份目录
	mkdirCmd := fmt.Sprintf("mkdir -p %s", backupDir)
	if _, err := bm.detector.RunSSH(inst, mkdirCmd); err != nil {
		return nil, fmt.Errorf("create backup dir '%s': %w", backupDir, err)
	}

	// 2. 执行原子热备份
	var copyCmd string
	if softInfo.SoftwareType == "3x-ui" {
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
		FileName:  filepath.Base(snapFile),
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

	var backupDir string
	switch softInfo.SoftwareType {
	case "sing-box":
		cName := softInfo.ContainerName
		if cName == "" {
			cName = "sb-mgr"
		}
		backupDir = fmt.Sprintf("/opt/%s/backups", cName)
	case "3x-ui":
		backupDir = "/etc/x-ui/backups"
	case "xray":
		backupDir = "/etc/v2ray-agent/backups"
	default:
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
		fName := filepath.Base(fPath)
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		modTime := parts[2]
		if len(modTime) >= 19 {
			modTime = modTime[:19]
		}

		// 从文件名中提取 ID
		id := fName
		if idx := strings.LastIndex(fName, "."); idx != -1 {
			id = fName[:idx]
		}
		if partsID := strings.Split(id, "_"); len(partsID) >= 2 {
			id = partsID[len(partsID)-1]
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

	var activeFile string
	var restartCmd string

	switch softInfo.SoftwareType {
	case "sing-box":
		cName := softInfo.ContainerName
		if cName == "" {
			cName = "sb-mgr"
		}
		activeFile = fmt.Sprintf("/opt/%s/config.json", cName)
		restartCmd = fmt.Sprintf("docker restart %s", cName)
	case "3x-ui":
		activeFile = "/etc/x-ui/x-ui.db"
		restartCmd = "docker restart 3x-ui"
	case "xray":
		cName := softInfo.ContainerName
		if cName == "" {
			cName = "v2ray-agent-docker"
		}
		activeFile = "/etc/v2ray-agent/docker/config.json"
		restartCmd = fmt.Sprintf("docker restart %s", cName)
	default:
		return fmt.Errorf("unsupported software type: %s", softInfo.SoftwareType)
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
