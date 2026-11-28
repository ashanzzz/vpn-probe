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
	execMode := inst.ExecMode
	if execMode == "" {
		if NeedsHub(inst) {
			execMode = "hub"
		} else {
			execMode = "ssh"
		}
	}
	info := &SoftwareInfo{
		InstanceID:    inst.ID,
		InstanceName:  inst.Name,
		IP:            inst.IP,
		ProviderID:    inst.ProviderID,
		Category:      inst.Category,
		ExecMode:      execMode,
		IsNAT:         inst.IsNAT,
		PortLimit:     inst.PortLimit,
		PortCount:     inst.PortCount,
		SoftwareType:  "none",
		DeployMode:    "none",
		Status:        "stopped",
		LastCheckedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	// 1. 测试 SSH 连通性并获取容器列表
	out, err := d.RunSSH(inst, "docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' 2>/dev/null; which sing-box xray 2>/dev/null; ss -tulpn 2>/dev/null || netstat -tlnp 2>/dev/null")
	if err != nil && len(out) == 0 {
		info.Status = "unreachable"
		info.ErrorMessage = err.Error()
		return info
	}

	// 2. 解析 Docker 容器（支持同主机多组件共存：sing-box / 3x-ui / xray / sub-store）
	type detectedSpec struct {
		appType       string
		deployMode    string
		containerName string
		configPath    string
		repoName      string
		verCmd        string
	}
	var specs []*detectedSpec
	seenType := make(map[string]bool)

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

		if strings.Contains(cImage, "sing-box") || strings.HasPrefix(cName, "sb-") || cName == "sing-box" {
			seenType["sing-box"] = true
			specs = append(specs, &detectedSpec{
				appType:       "sing-box",
				deployMode:    "docker",
				containerName: cName,
				configPath:    fmt.Sprintf("/opt/%s/config.json", cName),
				repoName:      "SagerNet/sing-box",
				verCmd:        fmt.Sprintf("docker exec %s sing-box version 2>/dev/null", cName),
			})
		} else if strings.Contains(cImage, "3x-ui") || cName == "3x-ui" {
			seenType["3x-ui"] = true
			specs = append(specs, &detectedSpec{
				appType:       "3x-ui",
				deployMode:    "docker",
				containerName: cName,
				configPath:    "/etc/x-ui/x-ui.db",
				repoName:      "MHSanaei/3x-ui",
				verCmd:        fmt.Sprintf("docker exec %s /app/x-ui -v 2>/dev/null || docker exec %s /app/bin/xray-linux-amd64 version 2>/dev/null", cName, cName),
			})
		} else if strings.Contains(cImage, "xray") || cName == "v2ray-agent-docker" {
			seenType["xray"] = true
			specs = append(specs, &detectedSpec{
				appType:       "xray",
				deployMode:    "docker",
				containerName: cName,
				configPath:    "/etc/v2ray-agent/docker/config.json",
				repoName:      "XTLS/Xray-core",
				verCmd:        fmt.Sprintf("docker exec %s xray version 2>/dev/null", cName),
			})
		} else if strings.Contains(cImage, "sub-store") || cName == "sub-store" {
			seenType["sub-store"] = true
			specs = append(specs, &detectedSpec{
				appType:       "sub-store",
				deployMode:    "docker",
				containerName: cName,
				configPath:    "/opt/1panel/apps/sub-store/sub-store.json",
				repoName:      "sub-store-org/Sub-Store",
				verCmd:        fmt.Sprintf("docker exec %s node -e \"try{console.log(require('/opt/app/package.json').version)}catch(e){try{console.log(require('/app/package.json').version)}catch(e2){}}\" 2>/dev/null || true", cName),
			})
		}
	}

	// 检查 native 二进制（仅当同类型无 Docker 容器时补充，兼容混合主机）
	if !seenType["sing-box"] && strings.Contains(out, "/sing-box") {
		specs = append(specs, &detectedSpec{
			appType:    "sing-box",
			deployMode: "native",
			configPath: "/etc/sing-box/config.json",
			repoName:   "SagerNet/sing-box",
			verCmd:     "sing-box version 2>/dev/null",
		})
	}
	if !seenType["xray"] && strings.Contains(out, "/xray") {
		specs = append(specs, &detectedSpec{
			appType:    "xray",
			deployMode: "native",
			configPath: "/etc/xray/config.json",
			repoName:   "XTLS/Xray-core",
			verCmd:     "xray version 2>/dev/null",
		})
	}

	if len(specs) == 0 {
		info.SoftwareType = "none"
		info.DeployMode = "none"
		info.Status = "not_installed"
		return info
	}

	// 3. 选择主展示软件: sing-box > 3x-ui > xray > sub-store
	priority := map[string]int{"sing-box": 1, "3x-ui": 2, "xray": 3, "sub-store": 4}
	primarySpec := specs[0]
	for _, sp := range specs[1:] {
		if priority[sp.appType] < priority[primarySpec.appType] {
			primarySpec = sp
		}
	}

	info.SoftwareType = primarySpec.appType
	info.DeployMode = primarySpec.deployMode
	info.ContainerName = primarySpec.containerName
	info.ConfigPath = primarySpec.configPath
	if primarySpec.deployMode == "docker" {
		info.Status = "running"
	} else {
		info.Status = "installed_stopped"
	}

	// 4. 读取主软件运行版本（保持单命令兼容性，不破坏旧测试 mock）
	if primarySpec.verCmd != "" {
		if verOut, err := d.RunSSH(inst, primarySpec.verCmd); err == nil {
			info.CurrentVersion = extractVersion(verOut)
		}
	}

	// 5. 提取监听端口
	info.ActivePorts = extractPorts(out)

	// 5.1 native 部署必须校验真实存活（服务状态 + 端口监听），不能仅凭 which 结果判定 running
	if info.DeployMode == "native" && info.SoftwareType != "none" {
		svcCmd := fmt.Sprintf("(rc-service %s status 2>&1 | head -n1 || true; systemctl is-active %s 2>/dev/null || true)", info.SoftwareType, info.SoftwareType)
		if svcOut, _ := d.RunSSH(inst, svcCmd); serviceRunningFromStatus(svcOut) {
			info.Status = "running"
		}
	}

	// 6. 对比 GitHub 最新版本（限制 3s 子超时，避免 GitHub 网络抖动拖垮整体探查）
	fetchLatest := func(repo string) *ReleaseInfo {
		if repo == "" || d.ghChecker == nil {
			return nil
		}
		ghCtx, ghCancel := context.WithTimeout(ctx, 3*time.Second)
		rel, err := d.ghChecker.GetLatestRelease(ghCtx, repo)
		ghCancel()
		if err != nil {
			logger.Warnf("[VPSMGR-DETECTOR] Failed to fetch latest release for %s: %v", repo, err)
			return nil
		}
		return rel
	}

	if rel := fetchLatest(primarySpec.repoName); rel != nil {
		info.LatestVersion = rel.Version
		info.ReleaseDate = rel.PublishedAt
		if info.CurrentVersion != "" && info.LatestVersion != "" {
			info.HasUpdate = (normalizeVer(info.CurrentVersion) != normalizeVer(info.LatestVersion))
		}
	}

	// 7. 组装全部共存组件清单（若存在多个组件，一次性合并脚本抓取其余组件版本）
	var extraCmds []string
	extraIdxMap := make(map[int]*ManagedAppInfo)
	info.ManagedApps = make([]*ManagedAppInfo, 0, len(specs))
	for idx, sp := range specs {
		app := &ManagedAppInfo{
			AppType:       sp.appType,
			DeployMode:    sp.deployMode,
			ContainerName: sp.containerName,
			ConfigPath:    sp.configPath,
			Status:        "running",
		}
		if sp.deployMode == "native" {
			app.Status = "installed_stopped"
			if sp == primarySpec {
				app.Status = info.Status
			}
		}
		if sp == primarySpec {
			app.CurrentVersion = info.CurrentVersion
			app.LatestVersion = info.LatestVersion
			app.HasUpdate = info.HasUpdate
		} else if sp.verCmd != "" {
			extraCmds = append(extraCmds, fmt.Sprintf("echo \"APP_VER_%d:$( (%s) | head -n1 )\"", idx, sp.verCmd))
			extraIdxMap[idx] = app
		}
		info.ManagedApps = append(info.ManagedApps, app)
	}

	if len(extraCmds) > 0 {
		if extraOut, err := d.RunSSH(inst, strings.Join(extraCmds, "; ")); err == nil {
			for idx, app := range extraIdxMap {
				prefix := fmt.Sprintf("APP_VER_%d:", idx)
				for _, line := range strings.Split(extraOut, "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, prefix) {
						val := strings.TrimSpace(strings.TrimPrefix(line, prefix))
						if val != "" {
							app.CurrentVersion = extractVersion(val)
						}
						break
					}
				}
			}
		}
	}
	for idx, sp := range specs {
		if sp == primarySpec {
			continue
		}
		app := info.ManagedApps[idx]
		if rel := fetchLatest(sp.repoName); rel != nil {
			app.LatestVersion = rel.Version
			if app.CurrentVersion != "" && app.LatestVersion != "" {
				app.HasUpdate = (normalizeVer(app.CurrentVersion) != normalizeVer(app.LatestVersion))
			}
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
