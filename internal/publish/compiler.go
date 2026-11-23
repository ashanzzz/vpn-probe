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
const vpnCfg = ProxyUtils.yaml.safeLoad($content ?? $files[0]);
if (!vpnCfg || typeof vpnCfg !== 'object' || !Array.isArray(vpnCfg.proxies)) {
  throw new Error('vpn-probe: not a valid Mihomo/Clash YAML config');
}

const vpnExisting = Array.isArray(vpnCfg['proxy-groups']) ? vpnCfg['proxy-groups'] : [];
const vpnNames = new Set(vpnCfg.proxies.map(p => p && p.name).filter(Boolean));
const vpnDefined = new Set(vpnUserGroups.map(g => g.name));

function resolveProxyName(targetName, allNames) {
  if (allNames.has(targetName)) return targetName;
  if (targetName.includes('➔') || targetName.startsWith('链式|')) {
    const raw = targetName.replace(/^链式\|/, '');
    const parts = raw.split('➔');
    if (parts.length === 2) {
      const transit = parts[0].trim();
      const resi = parts[1].trim();
      let prefix = '';
      if (transit.includes('4837') || transit.includes('LAX')) prefix = 'LAX-';
      else if (transit.includes('IIJ')) prefix = 'IIJ-';
      else if (transit.includes('Softbank') || transit.includes('SoftBank') || transit.includes('SB')) prefix = 'SB-';

      let suffix = '';
      const keywords = ['Pavlov', 'Frontier', 'Verizon', 'BIGLOBE', 'KDDI', 'SoftBank', 'GLBB', 'StarHub', 'KT', 'HiNet'];
      for (const kw of keywords) {
        if (resi.includes(kw)) { suffix = kw; break; }
      }
      if (prefix && suffix) {
        for (const name of allNames) {
          if (name.startsWith(prefix) && name.includes(suffix)) {
            return name;
          }
        }
      }
    }
  }
  return null;
}

// Remove previously user-defined groups or old-style duplicate user groups, preserving baseline system groups unless overridden
const vpnGroups = vpnExisting.filter(g => g && !vpnDefined.has(g.name));
for (const g of vpnGroups) {
  if (Array.isArray(g.proxies)) {
    g.proxies = g.proxies.filter(n => !vpnDefined.has(n));
  }
}

for (const spec of vpnUserGroups) {
  const members = [];
  for (const rawName of spec.proxies) {
    const resolved = resolveProxyName(rawName, vpnNames);
    if (resolved && !members.includes(resolved)) {
      members.push(resolved);
    }
  }
  const group = {
    name: spec.name,
    type: spec.type,
    proxies: members.length ? members : ['DIRECT']
  };
  if (spec.type === 'url-test' || spec.type === 'fallback' || spec.type === 'load-balance') {
    group.url = 'http://www.gstatic.com/generate_204';
    group.interval = 300;
  }
  if (spec.type === 'load-balance') {
    group.strategy = 'consistent-hashing';
  }
  vpnGroups.push(group);
}

const vpnEntry = vpnGroups.find(g => g.name === '选择代理');
if (vpnEntry) {
  const current = Array.isArray(vpnEntry.proxies) ? vpnEntry.proxies : [];
  const additions = vpnUserGroups.filter(g => g.add_to_entry).map(g => g.name);
  const next = [...new Set(current.concat(additions))];
  vpnEntry.proxies = next.includes('自动选择') ? ['自动选择', ...next.filter(n => n !== '自动选择')] : next;
}

vpnCfg['proxy-groups'] = vpnGroups;
$content = ProxyUtils.yaml.dump(vpnCfg);
`, nil
}
