package vpsmgr

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"vpn-probe/internal/logger"
)

// SSHRunnerFunc 自定义 SSH 命令执行器 (支持单测 Mock 与沙箱隔离)
type SSHRunnerFunc func(inst *VPSInstance, cmd string) (string, error)

type Detector struct {
	ghChecker *GitHubChecker
	sshRunner SSHRunnerFunc
	hubExec   HubExecutor
	hubClient *HubClient
	mu        sync.RWMutex
}

func NewDetector(gh *GitHubChecker) *Detector {
	return &Detector{
		ghChecker: gh,
	}
}

// SetSSHRunner 注入自定义 SSH 执行器 (单测/Mock 专用)
func (d *Detector) SetSSHRunner(runner SSHRunnerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sshRunner = runner
}

func (d *Detector) getSSHRunner() SSHRunnerFunc {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.sshRunner
}

// RunSSH 执行远程命令并返回标准输出。
// 懒猫云 NAT 实例（无公网 22 映射）自动改走 :3338 WebSocket PTY 通道。
func (d *Detector) RunSSH(inst *VPSInstance, cmd string) (string, error) {
	if NeedsHub(inst) {
		return d.RunViaHub(inst, cmd)
	}
	if runner := d.getSSHRunner(); runner != nil {
		return runner(inst, cmd)
	}
	return d.runSSHDirect(inst, cmd)
}

func (d *Detector) runSSHDirect(inst *VPSInstance, cmd string) (string, error) {
	config := &ssh.ClientConfig{
		User: inst.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(inst.SSHPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         7 * time.Second,
	}

	addr := net.JoinHostPort(inst.IP, strconv.Itoa(inst.SSHPort))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("ssh dial to %s: %w", addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(cmd); err != nil {
		return stdout.String(), fmt.Errorf("ssh run cmd '%s': %w, stderr: %s", cmd, err, stderr.String())
	}

	return stdout.String(), nil
}

// InspectVPS 探查单个 VPS 节点的软件形态、版本与生命周期
func (d *Detector) InspectVPS(ctx context.Context, inst *VPSInstance) *SoftwareInfo {
	info := &SoftwareInfo{
		InstanceID:    inst.ID,
		InstanceName:  inst.Name,
		IP:            inst.IP,
		SoftwareType:  "none",
		DeployMode:    "none",
		Status:        "stopped",
		LastCheckedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	// 1. 测试 SSH 连通性并获取容器列表
	out, err := d.RunSSH(inst, "docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' 2>/dev/null; which sing-box xray 2>/dev/null; ss -tulpn 2>/dev/null")
	if err != nil && len(out) == 0 {
		info.Status = "unreachable"
		info.ErrorMessage = err.Error()
		return info
	}

	// 2. 解析 Docker 容器
	var hasSingboxContainer bool
	var has3xuiContainer bool
	var hasXrayContainer bool
	var targetContainer string

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "|") {
			continue // 仅解析带管道符的 docker ps 输出行，避免被下方的 ss/netstat 进程行干扰
		}
		parts := strings.Split(line, "|")
		cName := parts[0]
		cImage := ""
		if len(parts) > 1 {
			cImage = parts[1]
		}

		if strings.Contains(cImage, "sing-box") || strings.HasPrefix(cName, "sb-") {
			hasSingboxContainer = true
			targetContainer = cName
		} else if strings.Contains(cImage, "3x-ui") || cName == "3x-ui" {
			has3xuiContainer = true
			if targetContainer == "" {
				targetContainer = "3x-ui"
			}
		} else if strings.Contains(cImage, "xray") || cName == "v2ray-agent-docker" {
			hasXrayContainer = true
			if targetContainer == "" {
				targetContainer = cName
			}
		}
	}

	// 3. 判断优先级: sing-box (现代首选) > 3x-ui > xray > native
	var verCmd string
	var repoName string

	if hasSingboxContainer {
		info.SoftwareType = "sing-box"
		info.DeployMode = "docker"
		info.ContainerName = targetContainer
		info.ConfigPath = fmt.Sprintf("/opt/%s/config.json", targetContainer)
		info.Status = "running"
		verCmd = fmt.Sprintf("docker exec %s sing-box version 2>/dev/null", targetContainer)
		repoName = "SagerNet/sing-box"
	} else if has3xuiContainer {
		info.SoftwareType = "3x-ui"
		info.DeployMode = "docker"
		info.ContainerName = "3x-ui"
		info.ConfigPath = "/etc/x-ui/x-ui.db"
		info.Status = "running"
		verCmd = "docker exec 3x-ui /app/bin/xray-linux-amd64 version 2>/dev/null"
		repoName = "MHSanaei/3x-ui"
	} else if hasXrayContainer {
		info.SoftwareType = "xray"
		info.DeployMode = "docker"
		info.ContainerName = targetContainer
		info.ConfigPath = "/etc/v2ray-agent/docker/config.json"
		info.Status = "running"
		verCmd = fmt.Sprintf("docker exec %s xray version 2>/dev/null", targetContainer)
		repoName = "XTLS/Xray-core"
	} else {
		// 检查 native 二进制
		if strings.Contains(out, "/sing-box") {
			info.SoftwareType = "sing-box"
			info.DeployMode = "native"
			info.ConfigPath = "/etc/sing-box/config.json"
			verCmd = "sing-box version 2>/dev/null"
			repoName = "SagerNet/sing-box"
			info.Status = "installed_stopped"
		} else if strings.Contains(out, "/xray") {
			info.SoftwareType = "xray"
			info.DeployMode = "native"
			info.ConfigPath = "/etc/xray/config.json"
			verCmd = "xray version 2>/dev/null"
			repoName = "XTLS/Xray-core"
			info.Status = "installed_stopped"
		} else {
			info.SoftwareType = "none"
			info.DeployMode = "none"
			info.Status = "not_installed"
			return info
		}
	}

	// 4. 读取运行版本
	if verCmd != "" {
		if verOut, err := d.RunSSH(inst, verCmd); err == nil {
			info.CurrentVersion = extractVersion(verOut)
		}
	}

	// 5. 提取监听端口
	info.ActivePorts = extractPorts(out)

	// 5.1 native 部署必须校验真实存活（服务状态 + 端口监听），不能仅凭 which 结果判定 running
	if info.DeployMode == "native" && info.SoftwareType != "none" {
		svcCmd := fmt.Sprintf("rc-service %s status 2>&1 | head -n1; systemctl is-active %s 2>/dev/null", info.SoftwareType, info.SoftwareType)
		if svcOut, err := d.RunSSH(inst, svcCmd); err == nil && serviceRunningFromStatus(svcOut) {
			info.Status = "running"
		}
	}

	// 6. 对比 GitHub 最新版本
	if repoName != "" && d.ghChecker != nil {
		if rel, err := d.ghChecker.GetLatestRelease(ctx, repoName); err == nil && rel != nil {
			info.LatestVersion = rel.Version
			info.ReleaseDate = rel.PublishedAt
			if info.CurrentVersion != "" && info.LatestVersion != "" {
				info.HasUpdate = (normalizeVer(info.CurrentVersion) != normalizeVer(info.LatestVersion))
			}
		} else {
			logger.Warnf("[VPSMGR-DETECTOR] Failed to fetch latest release for %s: %v", repoName, err)
		}
	}

	return info
}

var verRegex = regexp.MustCompile(`(?:version|Xray)\s+([vV]?\d+\.\d+\.\d+(?:-[\w.]+)?)|(\d+\.\d+\.\d+)`)

func extractVersion(output string) string {
	matches := verRegex.FindStringSubmatch(output)
	if len(matches) > 1 && matches[1] != "" {
		return strings.TrimPrefix(strings.TrimPrefix(matches[1], "v"), "V")
	}
	if len(matches) > 2 && matches[2] != "" {
		return matches[2]
	}
	// 简单按行首寻找数字版本
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.Count(f, ".") >= 2 && (strings.HasPrefix(f, "v") || (f[0] >= '0' && f[0] <= '9')) {
				return strings.TrimPrefix(f, "v")
			}
		}
	}
	return "unknown"
}

var portRegex = regexp.MustCompile(`:(\d{2,5})\s+`)

func extractPorts(output string) []int {
	portMap := make(map[int]bool)
	matches := portRegex.FindAllStringSubmatch(output, -1)
	for _, m := range matches {
		if len(m) > 1 {
			if p, err := strconv.Atoi(m[1]); err == nil {
				if p > 1024 && p < 65535 && p != 22 { // 排除常用非代理端口
					portMap[p] = true
				}
			}
		}
	}
	ports := make([]int, 0, len(portMap))
	for p := range portMap {
		ports = append(ports, p)
	}
	return ports
}

func normalizeVer(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// serviceRunningFromStatus 精确判定服务是否运行中。
// 注意必须整行匹配 "active"，否则 systemctl 的 "inactive" 会被 Contains 误判为运行。
func serviceRunningFromStatus(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if l == "active" || strings.Contains(l, "status: started") || strings.HasSuffix(l, "started") {
			return true
		}
	}
	return false
}
