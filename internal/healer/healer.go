package healer

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
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

// HealPortBlocked 处理端口被墙换端口自愈逻辑 (生成候选端口，另存 healed_url，不单方面直接覆盖原 URL)
func (h *Healer) HealPortBlocked(ctx context.Context, res *model.NodeProbeResult) (string, error) {
	if res.Diagnostic == nil || res.Diagnostic.FaultType != model.FaultPortBlocked {
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

	hostParts := strings.Split(u.Host, ":")
	if len(hostParts) == 2 {
		u.Host = fmt.Sprintf("%s:%d", hostParts[0], newPort)
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

	// 优选与当前被墙节点不同的健康中继
	var chosenRelay *model.NodeProbeResult
	for _, r := range healthyRelays {
		if r.ExitIP != res.ExitIP && r.Online {
			chosenRelay = r
			break
		}
	}
	if chosenRelay == nil {
		chosenRelay = healthyRelays[0]
	}

	// 在原节点 URL 中注入 dialerProxy 参数
	origURL := res.OriginalURL
	separator := "?"
	if strings.Contains(origURL, "?") {
		separator = "&"
	}

	// 截取 # 后的备注名
	hashIdx := strings.Index(origURL, "#")
	base := origURL
	nameTag := res.Name
	if hashIdx != -1 {
		base = origURL[:hashIdx]
		nameTag = origURL[hashIdx+1:]
	}

	fallbackName := fmt.Sprintf("%s-借道%s扶持", nameTag, chosenRelay.Name)
	newURL := fmt.Sprintf("%s%sdialerProxy=%s#%s", base, separator, url.QueryEscape(chosenRelay.Name), url.QueryEscape(fallbackName))

	logger.Sync("[HEALER] 🛡️ Mutual Relay Activated: IP %s blocked -> Downstream injected with dialer-proxy '%s'",
		res.ExitIP, chosenRelay.Name)

	return newURL
}

// BuildSmartChains 根据地理亲和性将自建线路机与懒猫云家宽组装为智能链式代理
func (h *Healer) BuildSmartChains(transitNodes []*model.NodeProbeResult, resiNodes []*model.NodeProbeResult) []string {
	var chains []string
	if len(resiNodes) == 0 || len(transitNodes) == 0 {
		return chains
	}

	// 定位可用健康线路机
	var jpsbRelay, iijRelay, laxRelay *model.NodeProbeResult
	for _, t := range transitNodes {
		if !t.Online {
			continue
		}
		upper := strings.ToUpper(t.Name)
		if strings.Contains(upper, "JPSB") || strings.Contains(upper, "SOFTBANK") || strings.Contains(t.Name, "软银") {
			jpsbRelay = t
		} else if strings.Contains(upper, "IIJ") {
			iijRelay = t
		} else if strings.Contains(upper, "4837") || strings.Contains(upper, "LAX") || strings.Contains(upper, "ACCK") {
			laxRelay = t
		}
	}

	// 兜底中继
	var defaultRelay *model.NodeProbeResult
	if jpsbRelay != nil {
		defaultRelay = jpsbRelay
	} else if laxRelay != nil {
		defaultRelay = laxRelay
	} else if iijRelay != nil {
		defaultRelay = iijRelay
	} else if len(transitNodes) > 0 {
		defaultRelay = transitNodes[0]
	}

	if defaultRelay == nil {
		return chains
	}

	for _, r := range resiNodes {
		var pairedRelay *model.NodeProbeResult

		// 判断家宽地域
		upperName := strings.ToUpper(r.Name)
		if strings.Contains(upperName, "-JP-") || strings.Contains(upperName, "-KR-") ||
			strings.Contains(upperName, "-TW-") || strings.Contains(upperName, "-SG-") ||
			strings.Contains(upperName, "-HK-") {
			// 亚太/日本 -> 首选 JPSB4 (次选 IIJ, 兜底 LAX)
			if jpsbRelay != nil {
				pairedRelay = jpsbRelay
			} else if iijRelay != nil {
				pairedRelay = iijRelay
			} else {
				pairedRelay = laxRelay
			}
		} else {
			// 美洲/其他 -> 首选 4837 (次选 JPSB)
			if laxRelay != nil {
				pairedRelay = laxRelay
			} else if jpsbRelay != nil {
				pairedRelay = jpsbRelay
			} else {
				pairedRelay = iijRelay
			}
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

		separator := "?"
		if strings.Contains(base, "?") {
			separator = "&"
		}

		chainName := fmt.Sprintf("链式|%s ➔ %s", pairedRelay.Name, resiLabel)
		chainURL := fmt.Sprintf("%s%sdialerProxy=%s#%s", base, separator, url.QueryEscape(pairedRelay.Name), url.QueryEscape(chainName))

		chains = append(chains, chainURL)
	}

	return chains
}
