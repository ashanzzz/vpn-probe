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
// targetFileType: "ashan" (all nodes), "q" (strictly backbone only, excludes residential), or "business" (pure self-built, no commercial airports)
func CompileScript(groups []model.CustomGroupDef, atoms []model.AtomNode, targetFileType ...string) (string, error) {
	track := "ashan"
	if len(targetFileType) > 0 && strings.TrimSpace(targetFileType[0]) != "" {
		track = strings.ToLower(strings.TrimSpace(targetFileType[0]))
	}
	isQ := track == "q"
	isBusiness := track == "business"

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

	isQJS := "false"
	if isQ {
		isQJS = "true"
	}
	isBusinessJS := "false"
	if isBusiness {
		isBusinessJS = "true"
	}

	// Embedded JS with Alias Resolver (resolving R2: 链式|Transit ➔ Resi <-> IIJ-美宽-Pavlov)
	return `// vpn-probe-managed user groups with dynamic alias resolution.
const vpnUserGroups = ` + string(encoded) + `;
const vpnIsQ = ` + isQJS + `;
const vpnIsBusiness = ` + isBusinessJS + `;

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
  if (n.includes('不送中') || n.includes('Non-Leak') || n.includes('Clean')) return false;
  return n === '自建-送中' || n === '自建-送中分组' || n === 'ashan-china-leak' || n === 'business-china-leak' || n.includes('送中') || n.includes('China-Leak');
}

function stripLeadingSymbols(s) {
  return String(s || '').trim().replace(/^[^a-zA-Z0-9\u4e00-\u9fa5]+/, '').trim();
}

function normalizeChainName(name) {
  const s = String(name || '').trim();
  if (!s.includes('➔')) return s;
  const idx = s.indexOf('➔');
  let left = stripLeadingSymbols(s.slice(0, idx));
  const pipeIdx = left.indexOf('|');
  if (pipeIdx !== -1) left = left.slice(pipeIdx + 1).trim();
  const right = s.slice(idx + 1).trim();
  return left + ' ➔ ' + right;
}

function transitMatchesCandidate(candidate, query) {
  const cClean = stripLeadingSymbols(candidate).toLowerCase();
  const qClean = stripLeadingSymbols(query).toLowerCase();
  if (!cClean || !qClean) return false;
  if (cClean === qClean) return true;
  const cUp = cClean.toUpperCase();
  const qUp = qClean.toUpperCase();
  if (cUp.includes('IIJ') && qUp.includes('IIJ')) return true;
  if ((cUp.includes('4837') || cUp.includes('LAX') || cClean.includes('美西') || cClean.includes('洛杉矶')) &&
      (qUp.includes('4837') || qUp.includes('LAX') || qClean.includes('美西') || qClean.includes('洛杉矶'))) return true;
  if ((/(^|[-_\s])SB($|[-_\s])/.test(cUp) || cUp.includes('SOFTBANK') || cClean.includes('软银')) &&
      (/(^|[-_\s])SB($|[-_\s])/.test(qUp) || qUp.includes('SOFTBANK') || qClean.includes('软银'))) return true;
  return false;
}

function resolveTransitProxyName(rawTransit, nonChainNames) {
  const t = String(rawTransit || '').trim();
  if (!t) return null;
  for (const name of nonChainNames) {
    if (name === t) return name;
  }
  const tClean = stripLeadingSymbols(t).toLowerCase();
  for (const name of nonChainNames) {
    if (stripLeadingSymbols(name).toLowerCase() === tClean) return name;
  }
  for (const name of nonChainNames) {
    if (transitMatchesCandidate(name, t)) return name;
  }
  return null;
}

function resolveProxyName(targetName, allNames) {
  const trimmed = (targetName || '').trim();
  if (trimmed === 'DIRECT' || trimmed === 'REJECT') return trimmed;
  if (allNames.has(trimmed)) return trimmed;
  if (allNames.has(targetName)) return targetName;
  const normalizedTarget = normalizeChainName(trimmed);
  if (allNames.has(normalizedTarget)) return normalizedTarget;
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
    for (const name of allNames) {
      if (name && !String(name).includes('➔') && transitMatchesCandidate(name, trimmed)) {
        return name;
      }
    }
  }
  if (trimmed.includes('➔')) {
    let raw = trimmed;
    const pipeIdx = raw.indexOf('|');
    if (pipeIdx !== -1) raw = raw.slice(pipeIdx + 1);
    const parts = raw.split('➔');
    if (parts.length === 2) {
      const transit = stripLeadingSymbols(parts[0]);
      const resi = parts[1].trim();
      const transitUpper = transit.toUpperCase();
      const resiLower = resi.toLowerCase();
      // 1. Direct match against another '➔' chain node (handles emoji prefix / 链式| stripping / legacy transit name)
      for (const name of allNames) {
        if (!name || !name.includes('➔')) continue;
        const nNorm = normalizeChainName(name);
        const nParts = nNorm.split('➔');
        if (nParts.length === 2) {
          const nTransit = nParts[0].trim();
          const nResi = nParts[1].trim().toLowerCase();
          if ((nTransit.toLowerCase() === transit.toLowerCase() || transitMatchesCandidate(nTransit, transit)) &&
              (nResi === resiLower || nResi.includes(resiLower) || resiLower.includes(nResi))) {
            return name;
          }
        }
      }
      // 2. Legacy compact 3-part alias fallback
      let prefix = '';
      if (transitUpper.includes('4837') || transitUpper.includes('LAX') || transit.includes('洛杉矶') || transit.includes('美西')) prefix = 'LAX-';
      else if (transitUpper.includes('IIJ')) prefix = 'IIJ-';
      else if (transitUpper.includes('SOFTBANK') || transitUpper.includes('SB') || transit.includes('软银')) prefix = 'SB-';

      let suffix = '';
      const resiParts = resi.replace(/[-_](家宽|住宅|Native|Residential)\d*$/i, '').split('-').filter(Boolean);
      const resiKeywords = ['Pavlov', 'Frontier', 'Verizon', 'BIGLOBE', 'KDDI', 'SoftBank', 'GLBB', 'StarHub', 'KT', 'HiNet'];
      const resiUpper = resi.toUpperCase();
      for (const kw of resiKeywords) {
        if (resiUpper.includes(kw.toUpperCase())) { suffix = kw; break; }
      }
      if (!suffix && resiParts.length > 0) {
        const cand = resiParts[resiParts.length - 1];
        if (cand && cand.length >= 2) suffix = cand;
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

function splitRuleTopLevel(rule) {
  const parts = [];
  let cur = '';
  let depth = 0;
  for (let i = 0; i < rule.length; i++) {
    const ch = rule[i];
    if (ch === '(') depth++;
    else if (ch === ')' && depth > 0) depth--;
    if (ch === ',' && depth === 0) {
      parts.push(cur);
      cur = '';
    } else {
      cur += ch;
    }
  }
  parts.push(cur);
  return parts;
}

function processConfig(vpnCfg) {
  if (!vpnCfg || typeof vpnCfg !== 'object' || !Array.isArray(vpnCfg.proxies)) {
    return vpnCfg;
  }

  const purgedNodeNames = new Set();
  const seenNodeNames = new Set();
  const renamedMap = new Map();
  const dedupedProxies = [];
  for (const p of vpnCfg.proxies) {
    if (!p) continue;
    if (isInformationalOrUnsupportedNode(p)) {
      if (p.name) purgedNodeNames.add(p.name);
      continue;
    }
    const origName = String(p.name);
    if (origName.includes('➔')) {
      const normName = normalizeChainName(origName);
      if (normName && normName !== origName) {
        renamedMap.set(origName, normName);
        p.name = normName;
      }
    }
    if (String(p.type || '').toLowerCase() === 'vless' && p['reality-opts'] && !p['client-fingerprint']) {
      p['client-fingerprint'] = 'chrome';
    }
    if (seenNodeNames.has(p.name)) {
      continue;
    }
    seenNodeNames.add(p.name);
    dedupedProxies.push(p);
  }
  vpnCfg.proxies = dedupedProxies;

  // Enforce dialer-proxy binding & alignment for all chain proxies in Clash YAML
  const nonChainNames = vpnCfg.proxies
    .filter(p => p && p.name && !String(p.name).includes('➔'))
    .map(p => String(p.name));
  for (const p of vpnCfg.proxies) {
    if (!p || !p.name) continue;
    const pName = String(p.name);
    const existingDialer = p['dialer-proxy'] ? String(p['dialer-proxy']).trim() : '';
    if (pName.includes('➔')) {
      let resolvedDialer = existingDialer ? resolveTransitProxyName(existingDialer, nonChainNames) : null;
      if (!resolvedDialer) {
        const transitPart = pName.split('➔')[0].trim();
        resolvedDialer = resolveTransitProxyName(transitPart, nonChainNames);
      }
      if (resolvedDialer && resolvedDialer !== pName) {
        p['dialer-proxy'] = resolvedDialer;
      }
    } else if (existingDialer) {
      const resolvedDialer = resolveTransitProxyName(existingDialer, nonChainNames);
      if (resolvedDialer && resolvedDialer !== pName) {
        p['dialer-proxy'] = resolvedDialer;
      }
    }
  }

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
      g.proxies = g.proxies
        .map(p => renamedMap.get(p) || p)
        .filter(p => !purgedNodeNames.has(p));
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
    '前置代理', '落地节点', '低倍率节点',
    '全球直连', '国内网站', '国内媒体', '国内直连', '漏网之鱼',
    '苹果服务', '微软服务', '谷歌FCM', 'Direct'
  ]);

  const builtinControls = new Set(['DIRECT', 'REJECT', 'REJECT-DROP', '选择代理', '自动选择', '手动选择', '故障转移', '负载均衡']);

  // Track-level business isolation: drop any non-self commercial proxies & commercial pool groups
  if (vpnIsBusiness) {
    const allowedSelfProxies = new Set();
    for (const g of vpnGroups) {
      if (isSelfName(g.name) || vpnDefined.has(g.name)) {
        for (const pName of (g.proxies || [])) {
          if (vpnNames.has(pName)) allowedSelfProxies.add(pName);
        }
      }
    }
    if (allowedSelfProxies.size > 0) {
      // Preserve any dialer-proxy transit targets referenced by retained chain proxies
      for (const p of vpnCfg.proxies) {
        if (p && p.name && allowedSelfProxies.has(p.name) && p['dialer-proxy'] && vpnNames.has(p['dialer-proxy'])) {
          allowedSelfProxies.add(p['dialer-proxy']);
        }
      }
      vpnCfg.proxies = vpnCfg.proxies.filter(p => p && allowedSelfProxies.has(p.name));
      vpnNames.clear();
      for (const p of vpnCfg.proxies) {
        if (p && p.name) vpnNames.add(p.name);
      }
    }
    const excludedCommercialPools = new Set(['外部分组', '外部节点', '低倍率节点']);
    vpnGroups = vpnGroups.filter(g => g && (isSelfName(g.name) || vpnDefined.has(g.name) || (nonAppGroupNames.has(g.name) && !excludedCommercialPools.has(g.name))));
    const validGroupSet = new Set(vpnGroups.map(g => g.name));
    for (const g of vpnGroups) {
      if (Array.isArray(g.proxies) && !isSelfName(g.name) && !vpnDefined.has(g.name)) {
        g.proxies = g.proxies.filter(p => builtinControls.has(p) || validGroupSet.has(p) || vpnNames.has(p));
        if (g.proxies.length === 0) g.proxies = ['DIRECT'];
      }
    }
    if (Array.isArray(vpnCfg.rules)) {
      const validRuleTargets = new Set([...validGroupSet, 'DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'COMPATIBLE', 'GLOBAL']);
      const fallbackProxyTarget = validGroupSet.has('选择代理') ? '选择代理' : (validGroupSet.has('Final') ? 'Final' : 'DIRECT');
      const fallbackDirectTarget = validGroupSet.has('全球直连') ? '全球直连' : 'DIRECT';
      const fallbackRejectTarget = validGroupSet.has('广告拦截') ? '广告拦截' : 'REJECT';
      vpnCfg.rules = vpnCfg.rules.map(rule => {
        if (typeof rule !== 'string') return rule;
        const parts = splitRuleTopLevel(rule);
        const targetIdx = parts.length === 2 ? 1 : (parts.length >= 3 ? 2 : -1);
        if (targetIdx === -1) return rule;
        const target = String(parts[targetIdx] || '').trim();
        if (!target || validRuleTargets.has(target)) return rule;
        if (/国内|直连|静态资源|哔哩哔哩|新浪微博/i.test(target)) {
          parts[targetIdx] = fallbackDirectTarget;
        } else if (/广告|拦截|reject/i.test(target)) {
          parts[targetIdx] = fallbackRejectTarget;
        } else {
          parts[targetIdx] = fallbackProxyTarget;
        }
        return parts.join(',');
      });
    }
  }

  // Extract active self-hosted groups in current subscription (ordered by spec.order)
  const activeSelfGroups = vpnGroups.filter(g => isSelfName(g.name) || vpnDefined.has(g.name));
  activeSelfGroups.sort((a, b) => (a._order || 999) - (b._order || 999));
  const activeSelfNames = activeSelfGroups.map(g => g.name);

  // Clean proxy groups:
  // 1. Region node pools (ends with 节点 / 池) MUST ONLY contain genuine external/pure nodes (strictly purge self groups & other policy groups).
  // 2. External app groups (TikTok, YouTube, AI服务, Spotify, Netflix, etc.):
  //    Unified layout: ['选择代理', '自动选择', '故障转移', ...external node pools & DIRECT, ...activeSelfNames (trailing additions only if present)]
  // 3. Self / user-authored groups MUST NOT be overwritten by external-app-group layout.
  for (const g of vpnGroups) {
    if (!g || !Array.isArray(g.proxies)) continue;

    // Anti-nesting: no group should ever contain itself
    g.proxies = g.proxies.filter(p => p !== g.name && !purgedNodeNames.has(p));

    if (isSelfName(g.name) || vpnDefined.has(g.name)) {
      // Self or user-defined custom groups: preserve compiled proxies directly
      continue;
    }

    const isDirectFirst = g.proxies.length > 0 && (g.proxies[0] === 'DIRECT' || g.proxies[0] === 'REJECT' || g.proxies[0] === 'REJECT-DROP');
    const isNodePool = g.name.endsWith('节点') || g.name.endsWith('池');
    if (isNodePool) {
      // Region node pool: strictly purge any self group or nested group names
      g.proxies = g.proxies.filter(p => !isSelfName(p) && (!existingGroupNames.has(p) || p === 'DIRECT' || p === 'REJECT') && !isDirtyLeakName(p));
    } else if (g.name !== '选择代理' && g.name !== '手动选择') {
      const isSpecialPool = isDirectFirst || g.name.endsWith('分组') || g.type === 'url-test' || g.type === 'fallback' || g.type === 'load-balance' || nonAppGroupNames.has(g.name);
      if (!isSpecialPool) {
        // External application policy group (TikTok, Spotify, AI服务, etc.)
        const externalMembers = g.proxies.filter(p => !isSelfName(p) && !vpnDefined.has(p) && p !== '自动选择' && p !== '选择代理' && p !== '故障转移' && !isDirtyLeakName(p));
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

  const selfGroups = vpnGroups.filter(g => isSelfName(g.name) || vpnDefined.has(g.name));
  selfGroups.sort((a, b) => (a._order || 999) - (b._order || 999));
  const selfSet = new Set(selfGroups.map(g => g.name));

  const middleGroups = vpnGroups.filter(g => !topSet.has(g.name) && !selfSet.has(g.name));

  vpnGroups.length = 0;
  vpnGroups.push(...topGroups, ...middleGroups, ...selfGroups);

  // Sync GLOBAL.proxies with deterministic group ordering (top -> middle -> self)
  // so Clash/Mihomo WebUIs (e.g. MetaCubeXD sorting by GLOBAL.all) also place selfGroups at the bottom.
  const globalGroup = vpnGroups.find(g => g.name === 'GLOBAL');
  if (globalGroup) {
    const orderedGroupNames = vpnGroups
      .filter(g => g.name !== 'GLOBAL')
      .map(g => g.name);
    const orderedGroupSet = new Set(orderedGroupNames);
    const extraEntries = Array.isArray(globalGroup.proxies)
      ? globalGroup.proxies.filter(p => p && p !== 'GLOBAL' && !orderedGroupSet.has(p) && !purgedNodeNames.has(p))
      : [];
    globalGroup.proxies = [...orderedGroupNames, ...extraEntries];
  }

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
