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

// Clone returns a deep copy of CustomRuleConfig with isolated slice fields
func (c CustomRuleConfig) Clone() CustomRuleConfig {
	out := c
	if c.LANDNSDomains != nil {
		out.LANDNSDomains = make([]string, len(c.LANDNSDomains))
		copy(out.LANDNSDomains, c.LANDNSDomains)
	}
	if c.DirectDomains != nil {
		out.DirectDomains = make([]string, len(c.DirectDomains))
		copy(out.DirectDomains, c.DirectDomains)
	}
	if c.CustomRules != nil {
		out.CustomRules = make([]string, len(c.CustomRules))
		copy(out.CustomRules, c.CustomRules)
	}
	return out
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".custom_rules-*.tmp")
	if err != nil {
		return err
	}
	tmpName := f.Name()
	defer os.Remove(tmpName)
	defer f.Close()

	if err := f.Chmod(0644); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, m.filePath)
}

func (m *Manager) GetConfig() CustomRuleConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.Clone()
}

func (m *Manager) UpdateConfig(newCfg CustomRuleConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = newCfg.Clone()
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

// GenerateJS 动态渲染最新的 Sub-Store JavaScript 算子脚本 (纯通用分流与防送中，不篡改用户本地网关/端口配置)
func (m *Manager) GenerateJS() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	marshalStr := func(s string) string {
		b, err := json.Marshal(s)
		if err != nil {
			return `""`
		}
		return string(b)
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
		dTrim := strings.TrimSpace(d)
		if dTrim == "" {
			continue
		}
		directRuleEntries = append(directRuleEntries, "      "+marshalStr(fmt.Sprintf("DOMAIN-SUFFIX,%s,DIRECT", dTrim))+",")
		fakeIpFilters = append(fakeIpFilters, "      "+marshalStr(fmt.Sprintf("+.%s", dTrim))+",")
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
		rTrim := strings.TrimSpace(r)
		if rTrim == "" {
			continue
		}
		userCustomRules = append(userCustomRules, "      "+marshalStr(rTrim)+",")
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

  // 1. 仅按需补全 Fake-IP 直连过滤规则，不强制改写客户端网关/监听端口/DNS
  if (config.dns && typeof config.dns === 'object') {
    const requiredFakeIpFilters = [
%s
    ];
    config.dns['fake-ip-filter'] = Array.isArray(config.dns['fake-ip-filter']) ? config.dns['fake-ip-filter'] : [];
    for (const item of requiredFakeIpFilters) {
      if (!config.dns['fake-ip-filter'].includes(item)) {
        config.dns['fake-ip-filter'].push(item);
      }
    }
  }

  // 2. 动态节点过滤与 Reality MLKEM 支持
  if (Array.isArray(config.proxies)) {
    const isPaused = (name) => typeof name === 'string' && (
      name.includes('[限速]') ||
      name.includes('[THROTTLED]') ||
      name.includes('[PAUSED]') ||
      name.includes('[DEGRADED]')
    );

    const purgedNames = new Set();
    const activeProxies = [];
    for (const p of config.proxies) {
      if (!p || typeof p !== 'object') continue;
      if (isPaused(p.name)) {
        purgedNames.add(p.name);
        continue;
      }
%s
      activeProxies.push(p);
    }
    if (activeProxies.length > 0) {
      config.proxies = activeProxies;
    } else {
      purgedNames.clear();
    }

    if (Array.isArray(config['proxy-groups'])) {
      config['proxy-groups'].forEach(group => {
        if (group && Array.isArray(group.proxies)) {
          const filtered = group.proxies.filter(p => !purgedNames.has(p) && !isPaused(p));
          group.proxies = filtered.length > 0 ? filtered : ['DIRECT'];
        }
      });
    }
  }

  // 3. 前置注入个性规则
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
        const matchIdx = mergedRules.findIndex(r => typeof r === 'string' && r.startsWith('MATCH,'));
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
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
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
