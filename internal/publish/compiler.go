package publish

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
)

type compiledGroup struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Order      int      `json:"order"`
	AddToEntry bool     `json:"add_to_entry"`
	Proxies    []string `json:"proxies"`
}

// CompileScript compiles user-authored groups into a robust Sub-Store script operator.
// targetFileType: "ashan" (all nodes) or "q" (strictly backbone only, excludes residential)
func CompileScript(groups []model.CustomGroupDef, atoms []model.AtomNode, targetFileType ...string) (string, error) {
	isQ := len(targetFileType) > 0 && strings.EqualFold(targetFileType[0], "q")

	output := make([]compiledGroup, 0, len(groups))
	seenGroups := map[string]bool{}

	for _, g := range groups {
		if err := g.Validate(); err != nil {
			return "", fmt.Errorf("group %s: %w", g.Name, err)
		}
		if !g.Enabled {
			continue
		}
		if seenGroups[g.DisplayName] {
			return "", fmt.Errorf("duplicate display_name %s", g.DisplayName)
		}
		seenGroups[g.DisplayName] = true

		filter := g.Filter
		if isQ {
			// For Q: strictly exclude residential and chain kinds
			filter.IncludeResidential = false
			filter.IncludeChain = false
			if len(filter.Kinds) > 0 {
				cleanKinds := []string{}
				for _, k := range filter.Kinds {
					if k == "transit" {
						cleanKinds = append(cleanKinds, k)
					}
				}
				filter.Kinds = cleanKinds
			}
		}

		matches := monitor.MatchAtoms(atoms, filter)
		if isQ {
			// For Q: strictly enforce backbone transit only across all filter modes (including manual selection)
			cleanMatches := make([]model.AtomNode, 0, len(matches))
			for _, a := range matches {
				if a.Kind == "transit" {
					cleanMatches = append(cleanMatches, a)
				}
			}
			matches = cleanMatches
		}
		seenNames := map[string]bool{}
		names := make([]string, 0, len(matches))
		for _, a := range matches {
			if strings.TrimSpace(a.Name) == "" || seenNames[a.Name] {
				continue
			}
			seenNames[a.Name] = true
			names = append(names, a.Name)
		}

		kind := g.Type
		if kind == "" {
			kind = "select"
		}

		output = append(output, compiledGroup{
			Name:       g.DisplayName,
			Type:       kind,
			Order:      g.Order,
			AddToEntry: g.AddToEntry,
			Proxies:    names,
		})
	}

	sort.SliceStable(output, func(i, j int) bool {
		if output[i].Order == output[j].Order {
			return output[i].Name < output[j].Name
		}
		return output[i].Order < output[j].Order
	})

	encoded, err := json.Marshal(output)
	if err != nil {
		return "", err
	}

	// Embedded JS with Alias Resolver (resolving R2: 链式|Transit ➔ Resi <-> IIJ-美宽-Pavlov)
	return `// vpn-probe-managed user groups with dynamic alias resolution.
const vpnUserGroups = ` + string(encoded) + `;

function getYamlLib() {
  if (typeof yaml !== 'undefined' && yaml) return yaml;
  if (typeof ProxyUtils !== 'undefined' && ProxyUtils && ProxyUtils.yaml) return ProxyUtils.yaml;
  return null;
}

function safeLoad(raw) {
  if (!raw) return null;
  if (typeof raw === 'object') return raw;
  const y = getYamlLib();
  if (!y) return null;
  try {
    if (typeof y.load === 'function') return y.load(raw);
    if (typeof y.safeLoad === 'function') return y.safeLoad(raw);
  } catch (e) {}
  return null;
}

function safeDump(obj) {
  if (typeof obj === 'string') return obj;
  const y = getYamlLib();
  if (!y) return '';
  try {
    if (typeof y.dump === 'function') return y.dump(obj);
    if (typeof y.safeDump === 'function') return y.safeDump(obj);
  } catch (e) {}
  return '';
}

function isSelfName(n) {
  return typeof n === 'string' && (n.startsWith('自建-') || n.startsWith('自建·') || n === '不送中');
}

function isInformationalOrUnsupportedNode(p) {
  if (!p || !p.name) return true;
  const name = String(p.name);
  if (/剩余流量|到期时间|官网|群组|通知|公告|Traffic|Expire|Website|故障|飞行模式|恢复|客服|说明|提示|注意|规则|备用/i.test(name)) return true;
  const pType = String(p.type || '').toLowerCase();
  const unsupportedTypes = ['anytls', 'unknown'];
  if (unsupportedTypes.includes(pType)) return true;
  return false;
}

function isDirtyLeakName(n) {
  if (typeof n !== 'string') return false;
  return n === '自建-送中' || n === '自建-送中分组' || n === 'ashan-china-leak' || n.includes('送中') || n.includes('China-Leak');
}

function resolveProxyName(targetName, allNames) {
  const trimmed = (targetName || '').trim();
  if (trimmed === 'DIRECT' || trimmed === 'REJECT') return trimmed;
  if (allNames.has(trimmed)) return trimmed;
  if (allNames.has(targetName)) return targetName;
  if (!trimmed.includes('➔')) {
    const trimmedLower = trimmed.toLowerCase();
    for (const name of allNames) {
      const nameLower = (name || '').toLowerCase();
      if (nameLower === trimmedLower) return name;
      if (nameLower.endsWith(trimmedLower)) {
        const pfx = nameLower.slice(0, nameLower.length - trimmedLower.length).trim();
        if (!/[a-zA-Z0-9\u4e00-\u9fa5]/.test(pfx)) return name;
      } else if (trimmedLower.endsWith(nameLower)) {
        const pfx = trimmedLower.slice(0, trimmedLower.length - nameLower.length).trim();
        if (!/[a-zA-Z0-9\u4e00-\u9fa5]/.test(pfx)) return name;
      }
    }
  }
  if (trimmed.includes('➔')) {
    let raw = trimmed;
    const pipeIdx = raw.indexOf('|');
    if (pipeIdx !== -1) raw = raw.slice(pipeIdx + 1);
    const parts = raw.split('➔');
    if (parts.length === 2) {
      const transit = parts[0].trim();
      const resi = parts[1].trim();
      const transitUpper = transit.toUpperCase();
      let prefix = '';
      if (transitUpper.includes('4837') || transitUpper.includes('LAX') || transit.includes('洛杉矶') || transit.includes('美西')) prefix = 'LAX-';
      else if (transitUpper.includes('IIJ')) prefix = 'IIJ-';
      else if (transitUpper.includes('SOFTBANK') || transitUpper.includes('SB') || transit.includes('软银')) prefix = 'SB-';

      let suffix = '';
      const resiUpper = resi.toUpperCase();
      const keywords = ['Pavlov', 'Frontier', 'Verizon', 'BIGLOBE', 'KDDI', 'SoftBank', 'GLBB', 'StarHub', 'KT', 'HiNet'];
      for (const kw of keywords) {
        if (resiUpper.includes(kw.toUpperCase())) { suffix = kw; break; }
      }
      if (prefix && suffix) {
        const prefixLower = prefix.toLowerCase();
        const suffixLower = suffix.toLowerCase();
        for (const name of allNames) {
          const nameLower = (name || '').toLowerCase();
          if (nameLower.includes(prefixLower) && nameLower.includes(suffixLower)) {
            return name;
          }
        }
      }
    }
  }
  return null;
}

function processConfig(vpnCfg) {
  if (!vpnCfg || typeof vpnCfg !== 'object' || !Array.isArray(vpnCfg.proxies)) {
    return vpnCfg;
  }

  const purgedNodeNames = new Set();
  const seenNodeNames = new Set();
  const dedupedProxies = [];
  for (const p of vpnCfg.proxies) {
    if (!p) continue;
    if (isInformationalOrUnsupportedNode(p)) {
      if (p.name) purgedNodeNames.add(p.name);
      continue;
    }
    if (seenNodeNames.has(p.name)) {
      continue;
    }
    seenNodeNames.add(p.name);
    dedupedProxies.push(p);
  }
  vpnCfg.proxies = dedupedProxies;

  const vpnExisting = Array.isArray(vpnCfg['proxy-groups']) ? vpnCfg['proxy-groups'] : [];
  const vpnNames = new Set(vpnCfg.proxies.map(p => p && p.name).filter(Boolean));
  const vpnDefined = new Set(vpnUserGroups.map(g => g.name));
  if (vpnDefined.has('自建-不送中')) {
    vpnDefined.add('不送中');
  }

  let vpnGroups = vpnExisting.filter(g => {
    if (!g) return false;
    if (vpnDefined.has(g.name)) return false;
    if (isSelfName(g.name)) return false;
    return true;
  });
  for (const g of vpnGroups) {
    if (Array.isArray(g.proxies)) {
      g.proxies = g.proxies.filter(p => !purgedNodeNames.has(p));
      if (g.proxies.length === 0) {
        g.proxies = ['DIRECT'];
      }
    }
  }

  for (const spec of vpnUserGroups) {
    const members = [];
    for (const rawName of spec.proxies) {
      const resolved = resolveProxyName(rawName, vpnNames);
      if (resolved && resolved !== spec.name && !members.includes(resolved)) {
        members.push(resolved);
      }
    }
    const group = {
      name: spec.name,
      type: spec.type,
      proxies: members.length ? members : ['DIRECT'],
      _order: typeof spec.order === 'number' ? spec.order : 999
    };
    if (spec.type === 'url-test' || spec.type === 'fallback' || spec.type === 'load-balance') {
      group.url = 'http://www.gstatic.com/generate_204';
      group.interval = 300;
    }
    if (spec.type === 'url-test') {
      group.tolerance = 50;
    }
    if (spec.type === 'load-balance') {
      group.strategy = 'consistent-hashing';
    }
    vpnGroups.push(group);
  }

  // Remap legacy clean group references (e.g. "不送中" -> "自建-不送中") to prevent dangling references in 手动选择 / 选择代理
  const cleanTarget = vpnDefined.has('自建-不送中') ? '自建-不送中' : (vpnDefined.has('不送中') ? '不送中' : '');
  if (cleanTarget) {
    for (const g of vpnGroups) {
      if (Array.isArray(g.proxies)) {
        g.proxies = g.proxies.map(p => (p === '不送中' || p === '自建-不送中') ? cleanTarget : p);
      }
    }
  }

  const existingGroupNames = new Set(vpnGroups.map(g => g.name));

  // Purge dead self group references that do not exist in this subscription (e.g. residential groups in Q)
  for (const g of vpnGroups) {
    if (Array.isArray(g.proxies)) {
      g.proxies = g.proxies.filter(p => !isSelfName(p) || existingGroupNames.has(p));
    }
  }

  const nonAppGroupNames = new Set([
    '选择代理', '手动选择', '自动选择', '故障转移', '负载均衡',
    '广告拦截', '搜狗输入法', 'DIRECT', 'REJECT', 'REJECT-DROP',
    '外部分组', '外部节点', '自有节点', '自建节点', 'GLOBAL', 'Final',
    '前置代理', '落地节点', '低倍率节点'
  ]);

  // Extract active self-hosted groups in current subscription (ordered by spec.order)
  const activeSelfGroups = vpnGroups.filter(g => isSelfName(g.name));
  activeSelfGroups.sort((a, b) => (a._order || 999) - (b._order || 999));
  const activeSelfNames = activeSelfGroups.map(g => g.name);

  // Clean proxy groups:
  // 1. Region node pools (ends with 节点 / 池) MUST ONLY contain genuine external/pure nodes (strictly purge self groups & other policy groups).
  // 2. External app groups (TikTok, YouTube, AI服务, Spotify, Netflix, etc.):
  //    Unified layout: ['自动选择', '选择代理', '故障转移', ...external node pools & DIRECT, ...activeSelfNames (trailing additions only if present)]
  // 3. Self groups MUST NOT contain self reference (anti-nesting).
  for (const g of vpnGroups) {
    if (!g || !Array.isArray(g.proxies)) continue;

    // Anti-nesting: no group should ever contain itself
    g.proxies = g.proxies.filter(p => p !== g.name && !purgedNodeNames.has(p));

    if (isSelfName(g.name)) {
      // Self groups: only keep valid nodes or allowed group references
      continue;
    }

    const isNodePool = g.name.endsWith('节点') || g.name.endsWith('池');
    if (isNodePool) {
      // Region node pool: strictly purge any self group or nested group names
      g.proxies = g.proxies.filter(p => !isSelfName(p) && (!existingGroupNames.has(p) || p === 'DIRECT' || p === 'REJECT') && !isDirtyLeakName(p));
    } else if (g.name !== '选择代理' && g.name !== '手动选择') {
      const isSpecialPool = g.name.endsWith('分组') || g.type === 'url-test' || g.type === 'fallback' || g.type === 'load-balance' || nonAppGroupNames.has(g.name);
      if (!isSpecialPool) {
        // External application policy group (TikTok, Spotify, AI服务, etc.)
        const externalMembers = g.proxies.filter(p => !isSelfName(p) && p !== '自动选择' && p !== '选择代理' && p !== '故障转移' && !isDirtyLeakName(p));
        const prefix = [];
        if (existingGroupNames.has('选择代理')) prefix.push('选择代理');
        if (existingGroupNames.has('自动选择')) prefix.push('自动选择');
        if (existingGroupNames.has('故障转移')) prefix.push('故障转移');

        const finalProxies = [...prefix];
        for (const m of externalMembers) {
          if (!finalProxies.includes(m)) finalProxies.push(m);
        }
        // Self groups placed strictly at the tail as optional additions (only if present, never dirty leak groups)
        for (const sa of activeSelfNames) {
          if (sa !== g.name && !isDirtyLeakName(sa) && !finalProxies.includes(sa)) {
            finalProxies.push(sa);
          }
        }
        g.proxies = finalProxies;
      }
    }

    if (g.proxies.length === 0) {
      g.proxies = ['DIRECT'];
    }
  }

  // Configure 选择代理: external connections first with 自动选择 leading, self-built optional additions strictly trailing
  const vpnEntry = vpnGroups.find(g => g.name === '选择代理');
  if (vpnEntry) {
    const allowedAdditions = new Set(vpnUserGroups.filter(g => g.add_to_entry && !isDirtyLeakName(g.name)).map(g => g.name));
    const externalProxies = Array.isArray(vpnEntry.proxies) ? vpnEntry.proxies.filter(n => (!isSelfName(n) && !vpnDefined.has(n)) && n !== '自动选择' && !isDirtyLeakName(n) && !purgedNodeNames.has(n)) : [];
    const selfAdditions = vpnUserGroups.filter(g => g.add_to_entry && !isDirtyLeakName(g.name)).map(g => g.name);
    
    const finalEntryProxies = [];
    if (existingGroupNames.has('自动选择')) {
      finalEntryProxies.push('自动选择');
    }
    for (const ep of externalProxies) {
      if (!finalEntryProxies.includes(ep)) finalEntryProxies.push(ep);
    }
    for (const sa of selfAdditions) {
      if (!finalEntryProxies.includes(sa)) finalEntryProxies.push(sa);
    }
    vpnEntry.proxies = finalEntryProxies;
    if (!Array.isArray(vpnEntry.proxies) || vpnEntry.proxies.length === 0) {
      vpnEntry.proxies = ['DIRECT'];
    }
  }

  // Deterministic Global Group Ordering:
  // 1. Topmost headers: 选择代理, 自动选择 (strictly at the very beginning)
  // 2. Control groups: 手动选择, 故障转移, 负载均衡
  // 3. Middle groups: External application groups, region node pools, system groups
  // 4. Bottom trailing: ALL self groups (isSelfName) strictly at the very end sorted by order
  const topNames = ['选择代理', '自动选择', '手动选择', '故障转移', '负载均衡'];
  const topGroups = [];
  for (const name of topNames) {
    const found = vpnGroups.find(g => g.name === name);
    if (found) topGroups.push(found);
  }
  const topSet = new Set(topGroups.map(g => g.name));

  const selfGroups = vpnGroups.filter(g => isSelfName(g.name));
  selfGroups.sort((a, b) => (a._order || 999) - (b._order || 999));
  const selfSet = new Set(selfGroups.map(g => g.name));

  const middleGroups = vpnGroups.filter(g => !topSet.has(g.name) && !selfSet.has(g.name));

  vpnGroups.length = 0;
  vpnGroups.push(...topGroups, ...middleGroups, ...selfGroups);

  for (const g of vpnGroups) {
    delete g._order;
    if (!Array.isArray(g.proxies) || g.proxies.length === 0) {
      g.proxies = ['DIRECT'];
    }
  }

  for (const g of vpnGroups) {
    if (Array.isArray(g.proxies)) {
      g.proxies = Array.from(new Set(g.proxies.filter(p => !purgedNodeNames.has(p))));
    }
    if (!Array.isArray(g.proxies) || g.proxies.length === 0) {
      g.proxies = ['DIRECT'];
    }
  }

  vpnCfg['proxy-groups'] = vpnGroups;
  return vpnCfg;
}

function main(rawConfig) {
  let target = rawConfig;
  if (!target && typeof $content !== 'undefined') target = $content;
  if (!target && typeof $files !== 'undefined' && $files) target = $files[0];
  if (!target) return target;

  // Case A: Wrapped Sub-Store file object { $content: string }
  if (typeof target === 'object' && target !== null && typeof target.$content === 'string') {
    let parsed = safeLoad(target.$content);
    if (parsed && typeof parsed === 'object' && Array.isArray(parsed.proxies)) {
      parsed = processConfig(parsed);
      target.$content = safeDump(parsed);
    }
    return target;
  }

  // Case B: Direct parsed YAML object
  if (typeof target === 'object' && target !== null && Array.isArray(target.proxies)) {
    return processConfig(target);
  }

  // Case C: Raw YAML string
  if (typeof target === 'string') {
    let parsed = safeLoad(target);
    if (parsed && typeof parsed === 'object' && Array.isArray(parsed.proxies)) {
      parsed = processConfig(parsed);
      return safeDump(parsed);
    }
  }

  return target;
}

function operator(rawConfig) {
  return main(rawConfig);
}

if (typeof $content !== 'undefined') {
  $content = main($content);
} else if (typeof module !== 'undefined' && module.exports) {
  module.exports = { main, operator, processConfig };
} else {
  globalThis.main = main;
  globalThis.operator = operator;
}
`, nil
}
