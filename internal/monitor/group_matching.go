package monitor

import (
	"regexp"
	"strings"
	"vpn-probe/internal/model"
)

func matchesString(items []string, value string) bool {
	for _, x := range items {
		if strings.EqualFold(strings.TrimSpace(x), value) {
			return true
		}
	}
	return false
}

// MatchAtoms uses one canonical matcher for previews, publishing and manual selections.
// The old include_* flags are accepted only when kinds is absent for compatibility.
func MatchAtoms(atoms []model.AtomNode, f model.GroupFilter) []model.AtomNode {
	var rx *regexp.Regexp
	if f.CustomMatchRegex != "" {
		rx, _ = regexp.Compile(f.CustomMatchRegex)
	}
	out := make([]model.AtomNode, 0)
	seen := make(map[string]bool)
	for _, a := range atoms {
		if f.Mode == "manual" {
			if !matchesString(f.NodeIDs, a.ID) {
				continue
			}
		} else {
			if len(f.Countries) > 0 && !matchesString(f.Countries, a.Country) {
				continue
			}
			kinds := f.Kinds
			if len(kinds) == 0 {
				if f.IncludeTransit {
					kinds = append(kinds, "transit")
				}
				if f.IncludeResidential {
					kinds = append(kinds, "residential")
				}
				if f.IncludeChain {
					kinds = append(kinds, "chain")
				}
			}
			if len(kinds) == 0 || !matchesString(kinds, a.Kind) {
				continue
			} // fail closed
			if len(f.InfraTypes) > 0 {
				match := false
				for _, t := range f.InfraTypes {
					if t == a.InfraType {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}
			if len(f.TagsAny) > 0 {
				match := false
				for _, t := range f.TagsAny {
					if matchesString(a.Tags, t) {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}
			all := true
			for _, t := range f.TagsAll {
				if !matchesString(a.Tags, t) {
					all = false
					break
				}
			}
			if !all {
				continue
			}
			excluded := false
			for _, t := range f.ExcludeTags {
				if matchesString(a.Tags, t) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
			text := strings.ToLower(a.Name)
			for _, k := range f.Keywords {
				if !strings.Contains(text, strings.ToLower(strings.TrimSpace(k))) {
					all = false
					break
				}
			}
			if !all {
				continue
			}
			for _, k := range f.Exclude {
				if k != "" && strings.Contains(text, strings.ToLower(strings.TrimSpace(k))) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
			if rx != nil && !rx.MatchString(a.Name) {
				continue
			}
		}
		if (f.ExcludeChinaLeak || f.CleanOnly) && a.ChinaExit {
			continue
		}
		if (f.ChinaLeakOnly || f.LeakOnly) && !a.ChinaExit {
			continue
		}
		if f.OnlineOnly && !a.Online {
			continue
		}
		dedupKey := a.ID
		if dedupKey == "" {
			dedupKey = a.Name
		}
		if dedupKey == "" {
			dedupKey = a.URL
		}
		if !seen[dedupKey] {
			seen[dedupKey] = true
			out = append(out, a)
		}
	}
	return out
}
