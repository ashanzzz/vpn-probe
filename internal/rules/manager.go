package rules

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CustomRuleConfig struct {
	LANPolicyGateway      string   `json:"lan_policy_gateway"`
	LANDNSDomains         []string `json:"lan_dns_domains"`
	DirectDomains         []string `json:"direct_domains"`
	CustomRules           []string `json:"custom_rules"`
	AntiSongZhongEnabled  bool     `json:"anti_songzhong_enabled"`
	MLKEMEnabled          bool     `json:"mlkem_enabled"`
}

type Manager struct {
	mu                 sync.RWMutex
	filePath           string
	cfg                CustomRuleConfig
	convertJSCache     string
	convertJSCacheTime time.Time
}

func DefaultConfig() CustomRuleConfig {
	return CustomRuleConfig{
		LANPolicyGateway: "10.0.0.1",
		LANDNSDomains: []string{
			"+.lan",
			"+.local",
			"unraid.local",
			"+.home.arpa",
		},
		DirectDomains: []string{
			"ashanzzz.com",
			"1992004.xyz",
		},
		CustomRules: []string{
			"GEOSITE,category-ai-!cn,AI服务",
			"DOMAIN-SUFFIX,linkedin.com,选择代理",
			"DOMAIN-SUFFIX,licdn.com,选择代理",
			"DOMAIN-SUFFIX,lnkd.in,选择代理",
			"DOMAIN-KEYWORD,linkedin,选择代理",
			"GEOSITE,linkedin,选择代理",
		},
		AntiSongZhongEnabled: true,
		MLKEMEnabled:         true,
	}
}

func NewManager(filePath string) (*Manager, error) {
	if filePath == "" {
		filePath = "./data/custom_rules.json"
	}
	m := &Manager{
		filePath: filePath,
		cfg:      DefaultConfig(),
	}
	if err := m.load(); err != nil {
		// 如果文件不存在，写入默认配置
		_ = m.save()
	}
	return m, nil
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.cfg)
}

func (m *Manager) save() error {
	dir := filepath.Dir(m.filePath)
	_ = os.MkdirAll(dir, 0755)
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.filePath, data, 0644)
}

func (m *Manager) GetConfig() CustomRuleConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) UpdateConfig(newCfg CustomRuleConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = newCfg
	return m.save()
}

func (m *Manager) AddDirectDomain(domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.cfg.DirectDomains {
		if d == domain {
			return nil
		}
	}
	m.cfg.DirectDomains = append(m.cfg.DirectDomains, domain)
	return m.save()
}

func (m *Manager) AddCustomRule(rule string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.cfg.CustomRules {
		if r == rule {
			return nil
		}
	}
	m.cfg.CustomRules = append(m.cfg.CustomRules, rule)
	return m.save()
}

// GenerateJS 动态渲染最新的 Sub-Store JavaScript 算子脚本
func (m *Manager) GenerateJS() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lanPolicyEntries := []string{}
	for _, d := range m.cfg.LANDNSDomains {
		lanPolicyEntries = append(lanPolicyEntries, fmt.Sprintf("    config.dns['nameserver-policy']['%s'] = '%s';", d, m.cfg.LANPolicyGateway))
	}

	directRuleEntries := []string{
		"      'GEOIP,private,DIRECT,no-resolve',",
		"      'GEOIP,LAN,DIRECT,no-resolve',",
		"      'DOMAIN-SUFFIX,lan,DIRECT',",
		"      'DOMAIN-SUFFIX,local,DIRECT',",
		"      'DOMAIN-SUFFIX,home.arpa,DIRECT',",
	}
	fakeIpFilters := []string{
		"      'geosite:connectivity-check',",
		"      'geosite:cn',",
		"      '+.taobao.com',",
		"      '+.alicdn.com',",
		"      '+.alipay.com',",
		"      '+.tmall.com',",
	}

	for _, d := range m.cfg.DirectDomains {
		directRuleEntries = append(directRuleEntries, fmt.Sprintf("      'DOMAIN-SUFFIX,%s,DIRECT',", d))
		fakeIpFilters = append(fakeIpFilters, fmt.Sprintf("      '+.%s',", d))
	}

	antiSongZhongRules := []string{}
	if m.cfg.AntiSongZhongEnabled {
		antiSongZhongRules = []string{
			"      // 防送中：拦截定位上传",
			"      'DOMAIN,geolocation.googleapis.com,REJECT',",
			"      'DOMAIN,userlocation.googleapis.com,REJECT',",
			"      'DOMAIN,locationhistory.googleapis.com,REJECT',",
			"      'DOMAIN,locationhistory-pa.googleapis.com,REJECT',",
			"      'DOMAIN,locationhistoryplacedetails-pa.googleapis.com,REJECT',",
			"      'DOMAIN,locationhistoryaggregates-pa.googleapis.com,REJECT',",
			"      'DOMAIN,semanticlocation-pa.googleapis.com,REJECT',",
			"      'DOMAIN,userlocation-pa.googleapis.com,REJECT',",
			"      'DOMAIN,gs-loc.apple.com,REJECT',",
			"      'DOMAIN,gs-loc-cn.apple.com,REJECT',",
			"      'DOMAIN,maps-api.apple.com,REJECT',",
			"      'DOMAIN,configuration.ls.apple.com,REJECT',",
			"      'DOMAIN,gsp-ssl.ls.apple.com,REJECT',",
			"      'DOMAIN,gspe1-ssl.ls.apple.com,REJECT',",
			"      'DOMAIN-SUFFIX,ls.apple.com,REJECT',",
			"      'DOMAIN,location.services.mozilla.com,REJECT',",
			"      'DOMAIN,location.microsoft.com,REJECT',",
			"      'DOMAIN,inference.location.live.net,REJECT',",
			"      'DOMAIN-SUFFIX,beacondb.net,REJECT',",
			"      'DOMAIN-SUFFIX,skyhookwireless.com,REJECT',",
		}
	}

	userCustomRules := []string{}
	for _, r := range m.cfg.CustomRules {
		userCustomRules = append(userCustomRules, fmt.Sprintf("      '%s',", r))
	}

	mlkemLogic := ""
	if m.cfg.MLKEMEnabled {
		mlkemLogic = `
      if (p['reality-opts'] && typeof p['reality-opts'] === 'object') {
        p['reality-opts']['support-x25519mlkem768'] = true;
        if (!p['client-fingerprint']) {
          p['client-fingerprint'] = 'chrome';
        }
      }
`
	}

	jsTemplate := `/**
 * vpn-probe 动态生成的 Sub-Store 个性网络规则算子
 * Auto-generated by vpn-probe Dynamic Rules Manager
 */

function getYamlLib() {
  if (typeof yaml !== 'undefined' && yaml) return yaml;
  if (typeof ProxyUtils !== 'undefined' && ProxyUtils && ProxyUtils.yaml) return ProxyUtils.yaml;
  return null;
}

function safeLoadYaml(lib, str) {
  try {
    if (typeof lib.load === 'function') return lib.load(str);
    if (typeof lib.safeLoad === 'function') return lib.safeLoad(str);
  } catch (e) {}
  return null;
}

function safeDumpYaml(lib, obj) {
  try {
    if (typeof lib.dump === 'function') return lib.dump(obj);
    if (typeof lib.safeDump === 'function') return lib.safeDump(obj);
  } catch (e) {}
  return '';
}

function transformConfig(config) {
  if (!config || typeof config !== 'object') return config;

  // 1. 全屋网关基础暴露配置
  config['allow-lan'] = true;
  config['bind-address'] = '*';
  config['external-controller'] = '0.0.0.0:9090';

  // 2. DNS 监听 7874、本地内网 Nameserver 策略及 Fake-IP 过滤
  if (config.dns) {
    config.dns.enable = true;
    config.dns.listen = '0.0.0.0:7874';

    config.dns['nameserver-policy'] = config.dns['nameserver-policy'] || {};
%s

    const requiredFakeIpFilters = [
%s
    ];
    config.dns['fake-ip-filter'] = config.dns['fake-ip-filter'] || [];
    for (const item of requiredFakeIpFilters) {
      if (!config.dns['fake-ip-filter'].includes(item)) {
        config.dns['fake-ip-filter'].push(item);
      }
    }
  }

  // 3. 动态节点过滤与 Reality MLKEM 支持
  if (Array.isArray(config.proxies)) {
    const isPaused = (name) => name && (
      name.includes('[限速]') || 
      name.includes('[THROTTLED]') || 
      name.includes('[PAUSED]') ||
      name.includes('[DEGRADED]')
    );
    
    const activeProxies = config.proxies.filter(p => !isPaused(p.name));
    if (activeProxies.length > 0) {
      config.proxies = activeProxies;
    }

    const proxyNames = new Set(config.proxies.map(p => p && p.name).filter(Boolean));
    const laxRelayName = '美国-洛杉矶4837-直连';

    config.proxies.forEach(p => {
      if (!p || typeof p !== 'object') return;
%s
      if (p.name && p.name.includes('美西中继') && p.name !== laxRelayName && proxyNames.has(laxRelayName)) {
        p['dialer-proxy'] = laxRelayName;
      }
    });

    if (Array.isArray(config['proxy-groups'])) {
      config['proxy-groups'].forEach(group => {
        if (Array.isArray(group.proxies)) {
          const filtered = group.proxies.filter(p => !isPaused(p));
          if (filtered.length > 0) group.proxies = filtered;
        }
      });
    }
  }

  // 4. 前置注入个性规则
  if (Array.isArray(config.rules)) {
    const customHeadRules = [
%s
%s
%s
    ];

    const filteredExisting = config.rules.filter(r => !customHeadRules.includes(r));
    let mergedRules = [...customHeadRules, ...filteredExisting];

    if (!mergedRules.includes('GEOSITE,cn,DIRECT')) {
      const geoipIdx = mergedRules.indexOf('GEOIP,cn,DIRECT');
      if (geoipIdx >= 0) {
        mergedRules.splice(geoipIdx, 0, 'GEOSITE,cn,DIRECT');
      } else {
        const matchIdx = mergedRules.findIndex(r => r.startsWith('MATCH,'));
        if (matchIdx >= 0) {
          mergedRules.splice(matchIdx, 0, 'GEOSITE,cn,DIRECT', 'GEOIP,cn,DIRECT');
        } else {
          mergedRules.push('GEOSITE,cn,DIRECT', 'GEOIP,cn,DIRECT');
        }
      }
    }
    config.rules = mergedRules;
  }

  return config;
}

function main(config) {
  if (!config || typeof config !== 'object') return config;

  const yLib = getYamlLib();
  const isWrapped = Boolean(yLib && config && config.$content && typeof config.$content === 'string');

  if (isWrapped) {
    let parsed = safeLoadYaml(yLib, config.$content);
    if (parsed && typeof parsed === 'object') {
      parsed = transformConfig(parsed);
      config.$content = safeDumpYaml(yLib, parsed);
      return config;
    }
  }

  return transformConfig(config);
}

function operator(input, targetPlatform, context) {
  return main(input);
}

if (typeof $content !== 'undefined') {
  $content = main($content);
} else if (typeof module !== 'undefined' && module.exports) {
  module.exports = { main, operator };
} else {
  globalThis.main = main;
  globalThis.operator = operator;
}
`

	return fmt.Sprintf(
		jsTemplate,
		strings.Join(lanPolicyEntries, "\n"),
		strings.Join(fakeIpFilters, "\n"),
		mlkemLogic,
		strings.Join(directRuleEntries, "\n"),
		strings.Join(antiSongZhongRules, "\n"),
		strings.Join(userCustomRules, "\n"),
	)
}

var convertUpstreamURL = "https://cdn.jsdelivr.net/gh/powerfullz/override-rules/convert.min.js"

// SetConvertUpstreamURLForTest 测试钩子
func SetConvertUpstreamURLForTest(u string) func() {
	old := convertUpstreamURL
	convertUpstreamURL = u
	return func() {
		convertUpstreamURL = old
	}
}

// GetBridgedConvertJS 获取附带 SubStore operator 桥接函数的开源规则转换脚本 (自动缓存并代理 jsDelivr)
func (m *Manager) GetBridgedConvertJS() string {
	m.mu.RLock()
	if m.convertJSCache != "" && time.Since(m.convertJSCacheTime) < 1*time.Hour {
		cache := m.convertJSCache
		m.mu.RUnlock()
		return cache
	}
	m.mu.RUnlock()

	upstreamURL := convertUpstreamURL
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Get(upstreamURL)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			data, readErr := io.ReadAll(resp.Body)
			if readErr == nil && len(data) > 0 {
				bridge := `

function operator(input, targetPlatform, context) {
  if (typeof globalThis.main === "function") return globalThis.main(input);
  if (typeof main === "function") return main(input);
  return input;
}
`
				result := string(data) + bridge
				m.mu.Lock()
				m.convertJSCache = result
				m.convertJSCacheTime = time.Now()
				m.mu.Unlock()
				return result
			}
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.convertJSCache != "" {
		return m.convertJSCache
	}

	return `// convert.js fetch fallback
function operator(input, targetPlatform, context) {
  if (typeof globalThis.main === "function") return globalThis.main(input);
  if (typeof main === "function") return main(input);
  return input;
}
`
}
