package publish

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"vpn-probe/internal/substore"
)

const managedOperator = "vpn-probe:user-policy-groups"

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
			if item, ok := raw.(map[string]any); ok && item["customName"] != managedOperator {
				operations = append(operations, item)
			}
		}
	} else if ops, ok := original["process"].([]map[string]any); ok {
		for _, item := range ops {
			if item["customName"] != managedOperator {
				operations = append(operations, item)
			}
		}
	}
	operations = append(operations, map[string]any{
		"id":         "Script Operator",
		"type":       "Script Operator",
		"customName": managedOperator,
		"args":       map[string]any{"content": script},
	})
	copyMap["process"] = operations
	return copyMap
}

// ApplyDifferentiated patches each file with its specific script (e.g. scriptAshan for ashan, scriptQ for Q)
func (p Publisher) ApplyDifferentiated(
	ctx context.Context,
	scriptAshan string,
	scriptQ string,
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
	for _, f := range files {
		name, _ := f["name"].(string)
		if strings.EqualFold(name, "q") || strings.EqualFold(name, "ashan") {
			selected = append(selected, f)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("neither Q nor ashan file exists in Sub-Store")
	}

	sort.Slice(selected, func(i, j int) bool { return fmt.Sprint(selected[i]["name"]) < fmt.Sprint(selected[j]["name"]) })
	updated := []map[string]any{}

	rollback := func(cause error) error {
		var problems []string
		for i := len(updated) - 1; i >= 0; i-- {
			old := updated[i]
			if err := p.Client.UpdateFile(ctx, fmt.Sprint(old["name"]), old); err != nil {
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
		}
		if err := p.Client.UpdateFile(ctx, name, patchFile(f, script)); err != nil {
			return rollback(fmt.Errorf("patch %s: %w", name, err))
		}
		updated = append(updated, f)
	}

	progress("verifying")
	for _, f := range selected {
		name := fmt.Sprint(f["name"])
		body, err := p.Client.RenderFileFresh(ctx, name)
		if err != nil {
			return rollback(fmt.Errorf("render %s: %w", name, err))
		}
		if err := verify(name, body); err != nil {
			return rollback(fmt.Errorf("validate %s: %w", name, err))
		}
	}
	return nil
}

// Apply fallback for backward compatibility
func (p Publisher) Apply(ctx context.Context, script string, verify func([]byte) error, progress func(string)) error {
	return p.ApplyDifferentiated(ctx, script, script, func(_ string, body []byte) error {
		return verify(body)
	}, progress)
}

// MatchMemberName matches expected raw node name with actual proxy name symmetrically, handling R2 aliases
func MatchMemberName(expected string, actual string) bool {
	if expected == actual {
		return true
	}
	return matchChainAlias(expected, actual) || matchChainAlias(actual, expected)
}

func matchChainAlias(full string, alias string) bool {
	if strings.Contains(full, "➔") || strings.HasPrefix(full, "链式|") {
		raw := strings.TrimPrefix(full, "链式|")
		parts := strings.Split(raw, "➔")
		if len(parts) == 2 {
			transit := strings.TrimSpace(parts[0])
			resi := strings.TrimSpace(parts[1])
			prefix := ""
			if strings.Contains(transit, "4837") || strings.Contains(transit, "LAX") {
				prefix = "LAX-"
			} else if strings.Contains(transit, "IIJ") {
				prefix = "IIJ-"
			} else if strings.Contains(transit, "Softbank") || strings.Contains(transit, "SoftBank") || strings.Contains(transit, "SB") {
				prefix = "SB-"
			}
			suffix := ""
			for _, kw := range []string{"Pavlov", "Frontier", "Verizon", "BIGLOBE", "KDDI", "SoftBank", "GLBB", "StarHub", "KT", "HiNet"} {
				if strings.Contains(resi, kw) {
					suffix = kw
					break
				}
			}
			if prefix != "" && suffix != "" && strings.HasPrefix(alias, prefix) && strings.Contains(alias, suffix) {
				return true
			}
		}
	}
	return false
}

// ValidateYAML checks final output
func ValidateYAML(body []byte, wanted []string) error {
	return ValidateYAMLWithMembers(body, wanted, nil)
}

func ValidateYAMLWithMembers(body []byte, wanted []string, required map[string][]string, isQFile ...bool) error {
	var cfg struct {
		Proxies []struct {
			Name string `yaml:"name"`
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
		names[p.Name] = true
	}

	isQ := len(isQFile) > 0 && isQFile[0]

	// Q Strict verification: NO residential proxies allowed
	if isQ {
		for _, p := range cfg.Proxies {
			if strings.Contains(p.Name, "家宽") || strings.Contains(strings.ToLower(p.Name), "resi") {
				return fmt.Errorf("Q file violates dual-standard: contains residential proxy %q", p.Name)
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

	// For wanted groups
	for _, n := range wanted {
		if !groups[n] {
			return fmt.Errorf("missing user group %q", n)
		}
	}

	// Only enforce required members for non-Q files (or for groups that have transit members)
	if !isQ {
		for _, g := range cfg.Groups {
			if candidates, ok := required[g.Name]; ok && len(candidates) > 0 {
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
