package healer

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"strconv"
	"strings"

	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
)

type Healer struct {
	cfg            *config.Config
	substoreClient *substore.Client
}

func NewHealer(cfg *config.Config, sc *substore.Client) *Healer {
	return &Healer{
		cfg:            cfg,
		substoreClient: sc,
	}
}

// GenerateRandomHighPort 生成在 [20000, 50000] 之间的安全随机高位端口
func GenerateRandomHighPort() int {
	return 20000 + rand.Intn(30000)
}

// CleanDialerProxyParam 剥离 URL 基础部分中已有的 dialerProxy / dialer-proxy 查询参数，防止链式或中继追加时产生重复参数
func CleanDialerProxyParam(rawURL string) string {
	lowerURL := strings.ToLower(rawURL)
	if !strings.Contains(lowerURL, "dialerproxy") && !strings.Contains(lowerURL, "dialer-proxy") {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	modified := false
	for k := range q {
		lowerK := strings.ToLower(k)
		if lowerK == "dialerproxy" || lowerK == "dialer-proxy" {
			q.Del(k)
			modified = true
		}
	}
	if modified {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func cleanDialerProxyParam(rawURL string) string {
	return CleanDialerProxyParam(rawURL)
}

// HealPortBlocked 处理端口被墙换端口自愈逻辑 (生成候选端口，另存 healed_url，不单方面直接覆盖原 URL)
func (h *Healer) HealPortBlocked(ctx context.Context, res *model.NodeProbeResult) (string, error) {
	if res.Diagnostic == nil || res.Diagnostic.FaultType != model.FaultPortBlocked || res.NodeType == "residential" || strings.Contains(res.Name, "家宽") {
		return res.OriginalURL, nil
	}

	oldPort := res.Diagnostic.TargetPort
	targetIP := res.Diagnostic.TargetIP
	newPort := GenerateRandomHighPort()

	logger.Event(logger.LevelInfo, "healer", "heal_proposed",
		fmt.Sprintf("node=%s target=%s:%d fault=port_blocked action=change_port proposed_port=%d",
			res.Name, targetIP, oldPort, newPort))

	// 构建新 URL
	u, err := url.Parse(res.OriginalURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	host := u.Hostname()
	if host != "" {
		u.Host = net.JoinHostPort(host, strconv.Itoa(newPort))
	}

	newURL := u.String()
	res.HealedURL = newURL
	if res.Diagnostic != nil {
		res.Diagnostic.ActionType = "AI_CHANGE_PORT"
	}

	return newURL, nil
}

// HealIPBlocked 处理整机 IP 被墙借道救活模式
func (h *Healer) HealIPBlocked(res *model.NodeProbeResult, healthyRelays []*model.NodeProbeResult) string {
	if len(healthyRelays) == 0 {
		logger.Warnf("[HEALER] ⚠️ IP %s is blocked, but no healthy transit relays available for mutual relay fallback!", res.ExitIP)
		return res.OriginalURL
	}

	// 优选与当前被墙节点不同的健康且不送中的中继
	var chosenRelay *model.NodeProbeResult
	for _, r := range healthyRelays {
		if r != nil && r.ExitIP != res.ExitIP && r.Online && !r.ChinaExit {
			chosenRelay = r
			break
		}
	}
	if chosenRelay == nil {
		for _, r := range healthyRelays {
			if r != nil && r.Online && !r.ChinaExit {
				chosenRelay = r
				break
			}
		}
	}
	if chosenRelay == nil {
		chosenRelay = healthyRelays[0]
	}

	// 截取 # 后的备注名
	origURL := res.OriginalURL
	hashIdx := strings.Index(origURL, "#")
	base := origURL
	nameTag := res.Name
	if hashIdx != -1 {
		base = origURL[:hashIdx]
		nameTag = origURL[hashIdx+1:]
	}
	base = cleanDialerProxyParam(base)

	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}

	fallbackName := fmt.Sprintf("%s-借道%s扶持", nameTag, chosenRelay.Name)
	newURL := fmt.Sprintf("%s%sdialerProxy=%s#%s", base, separator, url.QueryEscape(chosenRelay.Name), fallbackName)

	logger.Sync("[HEALER] 🛡️ Mutual Relay Activated: IP %s blocked -> Downstream injected with dialer-proxy '%s'",
		res.ExitIP, chosenRelay.Name)

	return newURL
}

// BuildSmartChains 根据地理亲和性将自建线路机与懒猫云家宽组装为智能链式代理 (通用国别亲和，无任何机房/运营商硬编码)
func (h *Healer) BuildSmartChains(transitNodes []*model.NodeProbeResult, resiNodes []*model.NodeProbeResult) []string {
	var chains []string
	if len(resiNodes) == 0 || len(transitNodes) == 0 {
		return chains
	}

	// 1. 按国别汇聚所有活跃线路机
	regionalTransits := make(map[string][]*model.NodeProbeResult)
	var activeTransits []*model.NodeProbeResult

	for _, t := range transitNodes {
		if t == nil || !t.Online || t.ChinaExit {
			continue
		}
		if strings.Contains(t.Name, "CF-WS") || strings.Contains(t.Name, "救活") || strings.Contains(t.Name, "Standby") {
			continue
		}
		c := model.ResolveCountry(t.ExitCountry, t.Name)
		regionalTransits[c] = append(regionalTransits[c], t)
		activeTransits = append(activeTransits, t)
	}

	if len(activeTransits) == 0 {
		return chains
	}

	defaultRelay := activeTransits[0]

	for _, r := range resiNodes {
		if r == nil || (!r.Online && r.Diagnostic != nil && r.Diagnostic.FaultType == model.FaultMachineDown) || r.ChinaExit {
			continue
		}
		var pairedRelay *model.NodeProbeResult

		resiCountry := model.ResolveCountry(r.ExitCountry, r.Name)
		if candidates := regionalTransits[resiCountry]; len(candidates) > 0 {
			pairedRelay = candidates[0]
		}

		if pairedRelay == nil {
			pairedRelay = defaultRelay
		}

		// 构建链式节点链接: socks5://user:pass@host:port?dialerProxy=RelayName#ChainName
		origURL := r.OriginalURL
		hashIdx := strings.Index(origURL, "#")
		base := origURL
		resiLabel := r.Name
		if hashIdx != -1 {
			base = origURL[:hashIdx]
			resiLabel = origURL[hashIdx+1:]
		}
		base = cleanDialerProxyParam(base)

		separator := "?"
		if strings.Contains(base, "?") {
			separator = "&"
		}

		chainName := fmt.Sprintf("链式|%s ➔ %s", pairedRelay.Name, resiLabel)
		chainURL := fmt.Sprintf("%s%sdialerProxy=%s#%s", base, separator, url.QueryEscape(pairedRelay.Name), chainName)

		chains = append(chains, chainURL)
	}

	return chains
}

// BuildFullMeshRegionalChains 方案B：全量全网状池化中继 (所有自建中转服务器全量挂载全部家宽机)
// 验收标准：4837 ➔ 家宽，IIJ ➔ 家宽，软银 ➔ 家宽... 咱们所有自有中转服务器全部把 10 个家宽链式代理
func (h *Healer) BuildFullMeshRegionalChains(transitNodes []*model.NodeProbeResult, resiNodes []*model.NodeProbeResult) []string {
	var chains []string
	if len(resiNodes) == 0 || len(transitNodes) == 0 {
		return chains
	}

	// 1. 提取所有健康的线路机 (忽略静默待命的救活节点，排除送中节点)
	var activeTransits []*model.NodeProbeResult
	for _, t := range transitNodes {
		if t == nil || !t.Online || t.ChinaExit {
			continue
		}
		// 忽略处于静默待命的救活节点
		if strings.Contains(t.Name, "CF-WS") || strings.Contains(t.Name, "救活") || strings.Contains(t.Name, "Standby") {
			continue
		}
		activeTransits = append(activeTransits, t)
	}

	if len(activeTransits) == 0 {
		return chains
	}

	// 2. 遍历家宽，每一个自有中转机全量把所有家宽链式代理
	seen := make(map[string]bool)
	for _, r := range resiNodes {
		if r == nil || (!r.Online && r.Diagnostic != nil && r.Diagnostic.FaultType == model.FaultMachineDown) || r.ChinaExit {
			continue
		}

		origURL := r.OriginalURL
		hashIdx := strings.Index(origURL, "#")
		base := origURL
		resiLabel := r.Name
		if hashIdx != -1 {
			base = origURL[:hashIdx]
			resiLabel = origURL[hashIdx+1:]
		}
		base = cleanDialerProxyParam(base)

		separator := "?"
		if strings.Contains(base, "?") {
			separator = "&"
		}

		// 咱们所有自有中转服务器，全部把所有家宽链式代理
		for _, transit := range activeTransits {
			chainName := fmt.Sprintf("链式|%s ➔ %s", transit.Name, resiLabel)
			chainURL := fmt.Sprintf("%s%sdialerProxy=%s#%s", base, separator, url.QueryEscape(transit.Name), chainName)
			if !seen[chainURL] {
				seen[chainURL] = true
				chains = append(chains, chainURL)
			}
		}
	}

	return chains
}
