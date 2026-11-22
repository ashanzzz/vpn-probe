package probe

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParsedNode 统一解析后的节点结构
type ParsedNode struct {
	Tag      string         `json:"tag"`
	Protocol string         `json:"protocol"`
	RawOutbound map[string]any `json:"raw_outbound"`
}

// ParseLink 将各种代理链接解析为 sing-box outbound 配置
func ParseLink(raw string) (*ParsedNode, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty link")
	}

	// 1. 如果本身就是 JSON
	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		var outbound map[string]any
		if err := json.Unmarshal([]byte(raw), &outbound); err == nil {
			tag, _ := outbound["tag"].(string)
			if tag == "" {
				tag = "custom-node"
				outbound["tag"] = tag
			}
			proto, _ := outbound["type"].(string)
			return &ParsedNode{
				Tag:         tag,
				Protocol:    proto,
				RawOutbound: outbound,
			}, nil
		}
	}

	// 2. URI 协议解析
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("url parse error: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	tag := u.Fragment
	if tag == "" {
		tag = u.Host
	} else {
		if unescaped, err := url.QueryUnescape(tag); err == nil {
			tag = unescaped
		}
	}

	switch scheme {
	case "ss":
		return parseShadowsocks(u, tag)
	case "trojan":
		return parseTrojan(u, tag)
	case "vless":
		return parseVLESS(u, tag)
	case "vmess":
		return parseVMess(raw, tag)
	case "hysteria2", "hy2":
		return parseHysteria2(u, tag)
	case "socks", "socks5":
		return parseSocks(u, tag)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", scheme)
	}
}

func parseShadowsocks(u *url.URL, tag string) (*ParsedNode, error) {
	var method, password, server string
	var port int

	if u.User != nil {
		userinfo := u.User.Username()
		pwd, hasPwd := u.User.Password()
		if !hasPwd {
			// SIP002 标准: base64(method:password)@host:port
			if decoded, err := decodeBase64Safe(userinfo); err == nil && strings.Contains(decoded, ":") {
				parts := strings.SplitN(decoded, ":", 2)
				method = parts[0]
				password = parts[1]
			} else {
				method = userinfo
			}
		} else {
			method = userinfo
			password = pwd
		}

		server = u.Hostname()
		if p := u.Port(); p != "" {
			if pInt, err := strconv.Atoi(p); err == nil && pInt > 0 {
				port = pInt
			}
		}
	} else {
		// 可能是整个 base64
		decoded, err := decodeBase64Safe(u.Host)
		if err != nil {
			return nil, fmt.Errorf("invalid ss base64: %w", err)
		}
		// method:password@host:port
		atIdx := strings.LastIndex(decoded, "@")
		if atIdx == -1 {
			return nil, fmt.Errorf("invalid ss content: %s", decoded)
		}
		userinfo := decoded[:atIdx]
		hostport := decoded[atIdx+1:]

		userParts := strings.SplitN(userinfo, ":", 2)
		if len(userParts) == 2 {
			method = userParts[0]
			password = userParts[1]
		}
		if h, p, err := net.SplitHostPort(hostport); err == nil {
			server = h
			if pInt, err := strconv.Atoi(p); err == nil && pInt > 0 {
				port = pInt
			}
		} else {
			server = hostport
		}
	}

	outbound := map[string]any{
		"type":        "shadowsocks",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"method":      method,
		"password":    password,
	}

	return &ParsedNode{
		Tag:         tag,
		Protocol:    "shadowsocks",
		RawOutbound: outbound,
	}, nil
}

func parseTrojan(u *url.URL, tag string) (*ParsedNode, error) {
	password := u.User.Username()
	server := u.Hostname()
	port := 443
	if p := u.Port(); p != "" {
		if pInt, err := strconv.Atoi(p); err == nil && pInt > 0 {
			port = pInt
		}
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	if sni == "" {
		sni = server
	}

	tlsConfig := map[string]any{
		"enabled":     true,
		"server_name": sni,
	}
	if q.Get("allowInsecure") == "1" || q.Get("insecure") == "1" {
		tlsConfig["insecure"] = true
	}

	outbound := map[string]any{
		"type":        "trojan",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"password":    password,
		"tls":         tlsConfig,
	}

	return &ParsedNode{
		Tag:         tag,
		Protocol:    "trojan",
		RawOutbound: outbound,
	}, nil
}

func parseVLESS(u *url.URL, tag string) (*ParsedNode, error) {
	uuid := u.User.Username()
	server := u.Hostname()
	port := 443
	if p := u.Port(); p != "" {
		if pInt, err := strconv.Atoi(p); err == nil && pInt > 0 {
			port = pInt
		}
	}

	q := u.Query()
	security := q.Get("security")
	flow := q.Get("flow")
	sni := q.Get("sni")
	if sni == "" {
		sni = server
	}

	outbound := map[string]any{
		"type":            "vless",
		"tag":             tag,
		"server":          server,
		"server_port":     port,
		"uuid":            uuid,
		"packet_encoding": "xudp",
	}
	if flow != "" {
		outbound["flow"] = flow
	} else if security == "reality" && (q.Get("type") == "" || q.Get("type") == "tcp") {
		// 容错：绝大部分 Reality TCP 部署强制启用 xtls-rprx-vision，若 URL 缺少 flow 参数，提升沙箱兼容性
		outbound["flow"] = "xtls-rprx-vision"
	}

	if security == "tls" || security == "reality" {
		tlsConfig := map[string]any{
			"enabled":     true,
			"server_name": sni,
		}
		fp := q.Get("fp")
		if fp == "" && security == "reality" {
			fp = "chrome"
		}
		if fp != "" {
			tlsConfig["utls"] = map[string]any{
				"enabled":     true,
				"fingerprint": fp,
			}
		}
		if security == "reality" {
			realityConfig := map[string]any{
				"enabled":    true,
				"public_key": q.Get("pbk"),
				"short_id":   q.Get("sid"),
			}
			tlsConfig["reality"] = realityConfig
		}
		if q.Get("allowInsecure") == "1" || q.Get("insecure") == "1" {
			tlsConfig["insecure"] = true
		}
		outbound["tls"] = tlsConfig
	}

	// 传输层
	netType := q.Get("type")
	if netType == "ws" {
		transport := map[string]any{
			"type": "ws",
			"path": q.Get("path"),
		}
		if host := q.Get("host"); host != "" {
			transport["headers"] = map[string]string{"Host": host}
		}
		outbound["transport"] = transport
	} else if netType == "grpc" {
		outbound["transport"] = map[string]any{
			"type":         "grpc",
			"service_name": q.Get("serviceName"),
		}
	}

	return &ParsedNode{
		Tag:         tag,
		Protocol:    "vless",
		RawOutbound: outbound,
	}, nil
}

func parseVMess(raw string, tag string) (*ParsedNode, error) {
	b64 := strings.TrimPrefix(raw, "vmess://")
	decoded, err := decodeBase64Safe(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid vmess base64: %w", err)
	}

	var v map[string]any
	if err := json.Unmarshal([]byte(decoded), &v); err != nil {
		return nil, fmt.Errorf("invalid vmess json: %w", err)
	}

	server, _ := v["add"].(string)
	portStr := fmt.Sprintf("%v", v["port"])
	port, _ := strconv.Atoi(portStr)
	uuid, _ := v["id"].(string)
	alterIDStr := fmt.Sprintf("%v", v["aid"])
	alterID, _ := strconv.Atoi(alterIDStr)
	ps, _ := v["ps"].(string)
	if ps != "" {
		tag = ps
	}

	outbound := map[string]any{
		"type":        "vmess",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"uuid":        uuid,
		"alter_id":    alterID,
	}

	netType, _ := v["net"].(string)
	if netType == "ws" {
		wsPath, _ := v["path"].(string)
		wsHost, _ := v["host"].(string)
		transport := map[string]any{
			"type": "ws",
			"path": wsPath,
		}
		if wsHost != "" {
			transport["headers"] = map[string]string{"Host": wsHost}
		}
		outbound["transport"] = transport
	}

	tlsStr, _ := v["tls"].(string)
	if tlsStr == "tls" {
		sni, _ := v["sni"].(string)
		if sni == "" {
			sni = server
		}
		outbound["tls"] = map[string]any{
			"enabled":     true,
			"server_name": sni,
		}
	}

	return &ParsedNode{
		Tag:         tag,
		Protocol:    "vmess",
		RawOutbound: outbound,
	}, nil
}

func parseHysteria2(u *url.URL, tag string) (*ParsedNode, error) {
	password := u.User.Username()
	server := u.Hostname()
	port := 443
	if p := u.Port(); p != "" {
		if pInt, err := strconv.Atoi(p); err == nil && pInt > 0 {
			port = pInt
		}
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		sni = server
	}

	tlsConfig := map[string]any{
		"enabled":     true,
		"server_name": sni,
	}
	if q.Get("insecure") == "1" {
		tlsConfig["insecure"] = true
	}

	outbound := map[string]any{
		"type":        "hysteria2",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"password":    password,
		"tls":         tlsConfig,
	}

	return &ParsedNode{
		Tag:         tag,
		Protocol:    "hysteria2",
		RawOutbound: outbound,
	}, nil
}

func parseSocks(u *url.URL, tag string) (*ParsedNode, error) {
	server := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		portStr = "1080"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid socks port: %w", err)
	}

	outbound := map[string]any{
		"type":        "socks",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"version":     "5",
	}

	if u.User != nil {
		outbound["username"] = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			outbound["password"] = pass
		}
	}

	return &ParsedNode{
		Tag:         tag,
		Protocol:    "socks5",
		RawOutbound: outbound,
	}, nil
}

func decodeBase64Safe(s string) (string, error) {
	s = strings.TrimSpace(s)
	// 补全 padding
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}

	// 尝试 URL Safe 编码或标准 Base64
	data, err := base64.URLEncoding.DecodeString(s)
	if err == nil {
		return string(data), nil
	}
	data, err = base64.StdEncoding.DecodeString(s)
	if err == nil {
		return string(data), nil
	}
	return "", err
}
