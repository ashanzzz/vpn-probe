package api

import (
	"regexp"
	"strings"
)

var (
	// 正则匹配 socks5://user:pass@host:port 或 socks5://pass@host:port
	reSocksUserPass = regexp.MustCompile(`^(socks5?://(?:[^:@]+:)?)([^@]+)(@.+)`)
	// 正则匹配 hysteria://pass@host:port 或 hysteria2://pass@host:port
	reHysteriaPass = regexp.MustCompile(`^((?:hysteria2?|hy2)://)([^@]+)(@.+)`)
	// 正则匹配 tuic://uuid:password@host:port 或 tuic://password@host:port
	reTuicPass = regexp.MustCompile(`^(tuic://(?:[^:@]+:)?)([^@]+)(@.+)`)
	// 正则匹配 http://user:pass@host:port 或 https://user:pass@host:port
	reHTTPPass = regexp.MustCompile(`^(https?://(?:[^:@]+:)?)([^@]+)(@.+)`)
	// 正则匹配 wireguard://privatekey@host:port 或 wg://privatekey@host:port
	reWireguardKey = regexp.MustCompile(`^((?:wireguard|wg)://)([^@]+)(@.+)`)
	// 正则匹配 snell://user:psk@host:port
	reSnellUserPass = regexp.MustCompile(`^(snell://(?:[^:@]+:)?)([^@]+)(@.+)`)
	// 正则匹配 query params: psk=...
	reSnellPSK = regexp.MustCompile(`([?&]psk=)([^&#]+)`)
	// 正则匹配 vless://uuid@host:port
	reVlessUUID = regexp.MustCompile(`^(vless://)([^@]+)(@.+)`)
	// 正则匹配 trojan://pass@host:port
	reTrojanPass = regexp.MustCompile(`^(trojan://)([^@]+)(@.+)`)
	// 正则匹配 ss://base64@host:port
	reSSBase64 = regexp.MustCompile(`^(ss://)([^@]+)(@.+)`)
	// 正则匹配 query params: pbk=...
	rePBK = regexp.MustCompile(`([?&]pbk=)([^&#]+)`)
	// 正则匹配 query params: sid=...
	reSID = regexp.MustCompile(`([?&]sid=)([^&#]+)`)
)

// MaskNodeURL 对各种协议的节点链接进行脱敏处理，遮蔽密码、UUID、Reality密钥等机密信息
// 保证节点名称、协议类型、IP与端口对运维大盘可见，但绝不泄露认证机密。
func MaskNodeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	lower := strings.ToLower(raw)
	res := raw

	switch {
	case strings.HasPrefix(lower, "socks5://") || strings.HasPrefix(lower, "socks://"):
		res = reSocksUserPass.ReplaceAllString(res, "${1}******${3}")

	case strings.HasPrefix(lower, "hysteria://") || strings.HasPrefix(lower, "hysteria2://") || strings.HasPrefix(lower, "hy2://"):
		res = reHysteriaPass.ReplaceAllString(res, "${1}******${3}")

	case strings.HasPrefix(lower, "tuic://"):
		res = reTuicPass.ReplaceAllString(res, "${1}******${3}")

	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		res = reHTTPPass.ReplaceAllString(res, "${1}******${3}")

	case strings.HasPrefix(lower, "wireguard://") || strings.HasPrefix(lower, "wg://"):
		res = reWireguardKey.ReplaceAllString(res, "${1}******${3}")

	case strings.HasPrefix(lower, "snell://"):
		if strings.Contains(res, "@") {
			res = reSnellUserPass.ReplaceAllString(res, "${1}******${3}")
		}
		res = reSnellPSK.ReplaceAllString(res, "${1}******")

	case strings.HasPrefix(lower, "vless://"):
		res = reVlessUUID.ReplaceAllStringFunc(res, func(m string) string {
			parts := reVlessUUID.FindStringSubmatch(m)
			if len(parts) == 4 {
				uuid := parts[2]
				maskedUUID := "****"
				if len(uuid) > 8 {
					maskedUUID = uuid[:4] + "****" + uuid[len(uuid)-4:]
				}
				return parts[1] + maskedUUID + parts[3]
			}
			return m
		})
		res = rePBK.ReplaceAllStringFunc(res, func(m string) string {
			parts := rePBK.FindStringSubmatch(m)
			if len(parts) == 3 {
				pbk := parts[2]
				maskedPBK := "****"
				if len(pbk) > 8 {
					maskedPBK = pbk[:4] + "****"
				}
				return parts[1] + maskedPBK
			}
			return m
		})
		res = reSID.ReplaceAllString(res, "${1}****")

	case strings.HasPrefix(lower, "trojan://"):
		res = reTrojanPass.ReplaceAllString(res, "${1}******${3}")

	case strings.HasPrefix(lower, "ss://"):
		if strings.Contains(res, "@") {
			res = reSSBase64.ReplaceAllString(res, "${1}******${3}")
		} else {
			if idx := strings.Index(res, "#"); idx != -1 {
				res = "ss://******" + res[idx:]
			} else {
				res = "ss://******"
			}
		}

	case strings.HasPrefix(lower, "vmess://"):
		if idx := strings.Index(res, "#"); idx != -1 {
			res = "vmess://******" + res[idx:]
		} else {
			res = "vmess://******"
		}
	}

	return res
}
