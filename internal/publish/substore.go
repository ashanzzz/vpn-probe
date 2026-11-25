package publish

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/substore"
)

const managedOperator = "vpn-probe:user-policy-groups"

func isLegacyPollutedOperator(item map[string]any) bool {
	id, _ := item["id"].(string)
	name, _ := item["customName"].(string)
	if id == "ashan_patch_v150" || id == "classify_songzhong_v1" {
		return true
	}
	if strings.Contains(name, "送中与不送中双轨分类") || strings.Contains(name, "送中与不送中") {
		return true
	}
	return false
}

// Publisher patches a file-level Script Operator LAST, preserving existing
// conversion/MLKEM operators. It supports differentiated scripts for Q vs ashan.
type Publisher struct {
	Client *substore.Client
}

func (p Publisher) filesRaw(ctx context.Context) ([]map[string]any, error) {
	return p.Client.GetFileConfigurations(ctx)
}

func patchFile(original map[string]any, script string) map[string]any {
	copyMap := make(map[string]any, len(original))
	for k, v := range original {
		copyMap[k] = v
	}
	operations := []map[string]any{}
	if ops, ok := original["process"].([]any); ok {
		for _, raw := range ops {
			if item, ok := raw.(map[string]any); ok && item["customName"] != managedOperator && !isLegacyPollutedOperator(item) {
				operations = append(operations, item)
			}
		}
	} else if ops, ok := original["process"].([]map[string]any); ok {
		for _, item := range ops {
			if item["customName"] != managedOperator && !isLegacyPollutedOperator(item) {
				operations = append(operations, item)
			}
		}
	}
	operations = append(operations, map[string]any{
		"id":         "Script Operator",
		"type":       "Script Operator",
		"customName": managedOperator,
		"args": map[string]any{
			"mode":    "code",
			"content": script,
		},
	})
	copyMap["process"] = operations
	return copyMap
}

// ApplyDifferentiated patches each file with its specific script (e.g. scriptAshan for ashan, scriptQ for Q, scriptBusiness for business)
func (p Publisher) ApplyDifferentiated(
	ctx context.Context,
	scriptAshan string,
	scriptQ string,
	scriptBusiness string,
	verify func(fileName string, body []byte) error,
	progress func(string),
) error {
	if p.Client == nil || p.Client.GetConfig() == nil || !p.Client.GetConfig().Enabled {
		return fmt.Errorf("substore is not enabled")
	}
	files, err := p.filesRaw(ctx)
	if err != nil {
		return fmt.Errorf("read substore files: %w", err)
	}

	selected := []map[string]any{}
	hasBusiness := false
	for _, f := range files {
		name, _ := f["name"].(string)
		if strings.EqualFold(name, "q") || strings.EqualFold(name, "ashan") || strings.EqualFold(name, "business") {
			selected = append(selected, f)
			if strings.EqualFold(name, "business") {
				hasBusiness = true
			}
		}
	}

	// 确保 col-business 集合存在 (0 外部机场)
	if cols, err := p.Client.GetCollections(ctx); err == nil {
		hasColBiz := false
		for _, c := range cols {
			if strings.EqualFold(c.Name, "col-business") {
				hasColBiz = true
				break
			}
		}
		if !hasColBiz {
			bizSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other", "ashan-china-leak"}
			_ = p.Client.UpdateCollection(ctx, "col-business", map[string]any{
				"name":                  "col-business",
				"displayName":           "Business 纯自建商务合集 (0 外部机场)",
				"subscriptions":         bizSubs,
				"ignoreFailedRemoteSub": "quiet",
			})
		}
	}

	// 若远端缺少 business 订阅文件，自动以现有 ashan 或 Q 为基准模板创建
	if !hasBusiness {
		var templateProcess []any
		for _, f := range files {
			if strings.EqualFold(fmt.Sprint(f["name"]), "ashan") || strings.EqualFold(fmt.Sprint(f["name"]), "q") {
				if proc, ok := f["process"].([]any); ok {
					templateProcess = proc
					break
				}
			}
		}
		newBiz := map[string]any{
			"name":                    "business",
			"displayName":             "Business 商业自建订阅 (高速骨干+原生家宽)",
			"source":                  "local",
			"sourceType":              "collection",
			"sourceName":              "col-business",
			"type":                    "mihomoConfig",
			"mode":                    "config",
			"includeUnsupportedProxy": true,
		}
		if len(templateProcess) > 0 {
			newBiz["process"] = templateProcess
		}
		if err := p.Client.UpdateFile(ctx, "business", newBiz); err == nil {
			selected = append(selected, newBiz)
		}
	}

	if len(selected) == 0 {
		return fmt.Errorf("neither Q, ashan nor business file exists in Sub-Store")
	}

	sort.Slice(selected, func(i, j int) bool { return fmt.Sprint(selected[i]["name"]) < fmt.Sprint(selected[j]["name"]) })
	updated := []map[string]any{}

	rollback := func(cause error) error {
		var problems []string
		rbCtx, rbCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rbCancel()
		for i := len(updated) - 1; i >= 0; i-- {
			old := updated[i]
			if err := p.Client.UpdateFile(rbCtx, fmt.Sprint(old["name"]), old); err != nil {
				problems = append(problems, err.Error())
			}
		}
		if len(problems) > 0 {
			return fmt.Errorf("%w; ROLLBACK FAILED: %s", cause, strings.Join(problems, "; "))
		}
		return cause
	}

	for _, f := range selected {
		name := fmt.Sprint(f["name"])
		script := scriptAshan
		if strings.EqualFold(name, "q") {
			script = scriptQ
		} else if strings.EqualFold(name, "business") {
			script = scriptBusiness
		}
		if err := p.Client.UpdateFile(ctx, name, patchFile(f, script)); err != nil {
			return rollback(fmt.Errorf("patch %s: %w", name, err))
		}
		updated = append(updated, f)
	}

	progress("verifying")
	// 针对不同订阅拓扑去重验证（q/Q 为同一极速骨干拓扑，ashan/Ashan 为同一全栈全家桶拓扑，business/Business 为纯自建商务拓扑），避免重复编译超时
	verifiedCanonical := map[string]bool{}
	for _, f := range selected {
		rawName := fmt.Sprint(f["name"])
		canon := "ashan"
		if strings.EqualFold(rawName, "q") {
			canon = "q"
		} else if strings.EqualFold(rawName, "business") {
			canon = "business"
		}
		if verifiedCanonical[canon] {
			continue
		}
		verifiedCanonical[canon] = true

		body, err := p.Client.RenderFileFresh(ctx, rawName)
		if err != nil {
			return rollback(fmt.Errorf("render %s: %w", rawName, err))
		}
		if err := verify(rawName, body); err != nil {
			return rollback(fmt.Errorf("validate %s: %w", rawName, err))
		}
	}
	return nil
}

// Apply fallback for backward compatibility
func (p Publisher) Apply(ctx context.Context, script string, verify func([]byte) error, progress func(string)) error {
	return p.ApplyDifferentiated(ctx, script, script, script, func(_ string, body []byte) error {
		return verify(body)
	}, progress)
}

// MatchMemberName matches expected raw node name with actual proxy name symmetrically, handling R2 aliases and emoji prefixes
func MatchMemberName(expected string, actual string) bool {
	if monitor.MatchNodeName(expected, actual) {
		return true
	}
	return matchChainAlias(expected, actual) || matchChainAlias(actual, expected)
}

func matchChainAlias(full string, alias string) bool {
	// 1. 若两端均能通过 ParseChainParts 解析为链式代理 (长格式 ➔ 或紧凑别名 [前置]-[落地区域]-[运营商])
	t1, r1, is1 := monitor.ParseChainParts(full)
	t2, r2, is2 := monitor.ParseChainParts(alias)
	if is1 && is2 {
		if monitor.MatchTransitCandidate(t1, t2) && monitor.MatchResidentialCandidate(r1, r2) {
			return true
		}
	}

	if strings.Contains(full, "➔") {
		raw := full
		if idx := strings.Index(raw, "|"); idx != -1 {
			raw = raw[idx+1:]
		}
		parts := strings.Split(raw, "➔")
		if len(parts) == 2 {
			transit := strings.TrimSpace(parts[0])
			resi := strings.TrimSpace(parts[1])
			transitUpper := strings.ToUpper(transit)
			prefix := ""
			if strings.Contains(transitUpper, "4837") || strings.Contains(transitUpper, "LAX") || strings.Contains(transit, "洛杉矶") || strings.Contains(transit, "美西") {
				prefix = "LAX-"
			} else if strings.Contains(transitUpper, "IIJ") {
				prefix = "IIJ-"
			} else if strings.Contains(transitUpper, "SOFTBANK") || strings.Contains(transitUpper, "SB") || strings.Contains(transit, "软银") {
				prefix = "SB-"
			}
			suffix := ""
			resiUpper := strings.ToUpper(resi)
			for _, kw := range []string{"Pavlov", "Frontier", "Verizon", "BIGLOBE", "KDDI", "SoftBank", "GLBB", "StarHub", "KT", "HiNet"} {
				if strings.Contains(resiUpper, strings.ToUpper(kw)) {
					suffix = kw
					break
				}
			}
			if prefix != "" && suffix != "" {
				aliasLower := strings.ToLower(alias)
				prefixLower := strings.ToLower(prefix)
				suffixLower := strings.ToLower(suffix)
				if (strings.HasPrefix(aliasLower, prefixLower) || strings.Contains(aliasLower, prefixLower) ||
					(prefixLower == "lax-" && (strings.Contains(aliasLower, "4837") || strings.Contains(aliasLower, "acck")))) &&
					strings.Contains(aliasLower, suffixLower) {
					return true
				}
			}
		}
	}
	return false
}

// ValidateYAML checks final output
func ValidateYAML(body []byte, wanted []string) error {
	return ValidateYAMLWithMembers(body, wanted, nil)
}

var commercialAirportKeywords = []string{
	"amy", "tag", "flower", "泡泡", "速鹰", "云速", "paopao",
	"flowercloud", "tagvpn", "nexitally", "dler", "rix", "ikuuu",
}

func ValidateYAMLWithMembers(body []byte, wanted []string, required map[string][]string, isQFile ...bool) error {
	profile := "ashan"
	if len(isQFile) > 0 && isQFile[0] {
		profile = "q"
	}
	return ValidateYAMLWithProfile(body, wanted, required, profile)
}

// ValidateYAMLWithProfile checks final output with explicit profile rules ("ashan", "q", "business")
func ValidateYAMLWithProfile(body []byte, wanted []string, required map[string][]string, profile string) error {
	var cfg struct {
		Proxies []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"proxies"`
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if len(cfg.Proxies) == 0 {
		return fmt.Errorf("no proxies in published output")
	}

	names := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true}
	for _, p := range cfg.Proxies {
		if strings.EqualFold(p.Type, "anytls") || strings.EqualFold(p.Type, "unknown") {
			return fmt.Errorf("unsupported proxy type %q for node %q", p.Type, p.Name)
		}
		if names[p.Name] {
			return fmt.Errorf("duplicate proxy name %q", p.Name)
		}
		names[p.Name] = true
	}

	// Sent-to-China leak check in core clean groups (Ironclad Rule #19)
	for _, g := range cfg.Groups {
		if g.Name == "选择代理" || g.Name == "谷歌服务" || g.Name == "Youtube" || g.Name == "AI服务" {
			for _, px := range g.Proxies {
				if px == "自建-送中" || px == "自建-送中分组" || px == "ashan-china-leak" || px == "business-china-leak" {
					return fmt.Errorf("core clean group %q illegally contains dirty leak group %q", g.Name, px)
				}
			}
		}
	}

	prof := strings.ToLower(strings.TrimSpace(profile))
	isQ := prof == "q"
	isBusiness := prof == "business"

	// Q Strict verification: NO residential proxies allowed
	if isQ {
		for _, p := range cfg.Proxies {
			if strings.Contains(p.Name, "家宽") || strings.Contains(strings.ToLower(p.Name), "resi") {
				return fmt.Errorf("Q file violates dual-standard: contains residential proxy %q", p.Name)
			}
		}
	}

	// Business Strict verification: NO external commercial airport proxies allowed (pure self-built only)
	if isBusiness {
		for _, p := range cfg.Proxies {
			lowerName := strings.ToLower(p.Name)
			for _, kw := range commercialAirportKeywords {
				if strings.Contains(lowerName, kw) {
					return fmt.Errorf("business profile illegally contains commercial airport node %q (keyword: %s)", p.Name, kw)
				}
			}
		}
	}

	groups := map[string]bool{}
	for _, g := range cfg.Groups {
		if groups[g.Name] {
			return fmt.Errorf("duplicate group %q", g.Name)
		}
		groups[g.Name] = true
	}

	for _, g := range cfg.Groups {
		for _, n := range g.Proxies {
			if !names[n] && !groups[n] {
				return fmt.Errorf("dangling group reference %q in %q", n, g.Name)
			}
		}
	}

	// Cycle detection in proxy groups
	groupAdj := make(map[string][]string, len(cfg.Groups))
	for _, g := range cfg.Groups {
		for _, n := range g.Proxies {
			if groups[n] {
				groupAdj[g.Name] = append(groupAdj[g.Name], n)
			}
		}
	}
	visited := make(map[string]int, len(cfg.Groups)) // 0: unvisited, 1: visiting, 2: visited
	var dfs func(u string, path []string) error
	dfs = func(u string, path []string) error {
		visited[u] = 1
		path = append(path, u)
		for _, v := range groupAdj[u] {
			if visited[v] == 1 {
				cycle := append(path, v)
				return fmt.Errorf("loop is detected in ProxyGroup, please check following ProxyGroups: [%s]", strings.Join(cycle, " -> "))
			}
			if visited[v] == 0 {
				if err := dfs(v, path); err != nil {
					return err
				}
			}
		}
		visited[u] = 2
		return nil
	}
	for _, g := range cfg.Groups {
		if visited[g.Name] == 0 {
			if err := dfs(g.Name, nil); err != nil {
				return err
			}
		}
	}

	// For wanted groups
	for _, n := range wanted {
		if isQ && (strings.Contains(n, "家宽") || strings.Contains(strings.ToLower(n), "resi")) {
			continue // Q 极速骨干拓扑严禁包含家宽策略组
		}
		if !groups[n] {
			if (n == "自建-不送中" || n == "自建不送中") && groups["不送中"] {
				continue
			}
			if n == "不送中" && groups["自建-不送中"] {
				continue
			}
			return fmt.Errorf("missing user group %q", n)
		}
	}

	// Only enforce required members for non-Q files (or for groups that have transit members)
	if !isQ {
		for _, g := range cfg.Groups {
			candidates, ok := required[g.Name]
			if !ok && g.Name == "不送中" {
				candidates, ok = required["自建-不送中"]
			}
			if !ok && g.Name == "自建-不送中" {
				candidates, ok = required["不送中"]
			}
			if ok && len(candidates) > 0 {
				found := false
				for _, n := range g.Proxies {
					for _, name := range candidates {
						if MatchMemberName(name, n) {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					return fmt.Errorf("group %q lost all expected selected nodes (name mismatch or output not refreshed)", g.Name)
				}
			}
		}
	}

	return nil
}
