package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
)

var (
	customGroupsFile = filepath.Join("data", "custom_groups.json")
	customGroupsMu   sync.RWMutex
)

// SetCustomGroupsFileForTest 测试文件重定向钩子
func SetCustomGroupsFileForTest(p string) func() {
	customGroupsMu.Lock()
	old := customGroupsFile
	customGroupsFile = p
	customGroupsMu.Unlock()
	return func() {
		customGroupsMu.Lock()
		customGroupsFile = old
		customGroupsMu.Unlock()
	}
}

// AtomQueryFilter 原子节点查询过滤条件
type AtomQueryFilter struct {
	Country   string // US, JP, SG...
	Kind      string // transit, residential, chain
	CleanOnly bool   // 仅查询不送中的干净节点
}

// Orchestrator 动态分组编排引擎与节点原子库
type Orchestrator struct {
	scheduler *Scheduler
}

// NewOrchestrator 创建编排器
func NewOrchestrator(scheduler *Scheduler) *Orchestrator {
	return &Orchestrator{
		scheduler: scheduler,
	}
}

// CollectAtoms 收集当前系统的所有原子节点连接与元数据
func (o *Orchestrator) CollectAtoms() []model.AtomNode {
	var atoms []model.AtomNode
	if o.scheduler == nil {
		return atoms
	}

	o.scheduler.mu.RLock()
	defer o.scheduler.mu.RUnlock()

	var transits []*model.NodeProbeResult
	var resis []*model.NodeProbeResult

	for _, state := range o.scheduler.nodeStates {
		if state.LastResult == nil || !state.LastResult.Online {
			continue
		}
		res := state.LastResult
		if res.NodeType == "residential" {
			resis = append(resis, res)
		} else {
			transits = append(transits, res)
		}
	}

	// 1. 线路机原子
	activeTransitLinks := filterActiveTransitNodes(transits)
	activeSet := make(map[string]bool)
	for _, l := range activeTransitLinks {
		activeSet[l] = true
	}

	for _, t := range transits {
		if !activeSet[t.OriginalURL] {
			continue
		}
		country := strings.ToUpper(t.ExitCountry)
		if country == "" {
			upperName := strings.ToUpper(t.Name)
			if strings.Contains(upperName, "4837") || strings.Contains(upperName, "LAX") || strings.Contains(upperName, "ACCK") {
				country = "US"
			} else if strings.Contains(upperName, "JPSB") || strings.Contains(upperName, "SOFTBANK") || strings.Contains(upperName, "IIJ") || strings.Contains(t.Name, "软银") {
				country = "JP"
			}
		}
		atoms = append(atoms, model.AtomNode{
			ID:        t.ID,
			Name:      t.Name,
			URL:       t.OriginalURL,
			Protocol:  t.Protocol,
			Country:   country,
			Kind:      "transit",
			ChinaExit: t.ChinaExit,
			LatencyMs: t.LatencyMs,
			Online:    t.Online,
			TestedAt:  t.TestedAt,
		})
	}

	// 2. 家宽机原子 (直连)
	uniqueResis := deduplicateNodeResults(resis)
	for _, r := range uniqueResis {
		country := strings.ToUpper(r.ExitCountry)
		if country == "" {
			upperName := strings.ToUpper(r.Name)
			if strings.Contains(upperName, "-US-") || strings.Contains(upperName, "美国") {
				country = "US"
			} else if strings.Contains(upperName, "-JP-") || strings.Contains(upperName, "日本") {
				country = "JP"
			}
		}
		atoms = append(atoms, model.AtomNode{
			ID:        r.ID,
			Name:      r.Name,
			URL:       r.OriginalURL,
			Protocol:  r.Protocol,
			Country:   country,
			Kind:      "residential",
			ChinaExit: r.ChinaExit,
			LatencyMs: r.LatencyMs,
			Online:    r.Online,
			TestedAt:  r.TestedAt,
		})
	}

	// 3. 线路拉家宽链式中继原子 (方案B)
	if o.scheduler.healer != nil {
		chains := o.scheduler.healer.BuildFullMeshRegionalChains(transits, uniqueResis)
		for _, ch := range chains {
			country := "OTHER"
			upper := strings.ToUpper(ch)
			if strings.Contains(upper, "US") || strings.Contains(ch, "美西") || strings.Contains(ch, "美国") {
				country = "US"
			} else if strings.Contains(upper, "JP") || strings.Contains(ch, "东京") || strings.Contains(ch, "软银") || strings.Contains(ch, "IIJ") || strings.Contains(ch, "日本") {
				country = "JP"
			}

			chainName := ch
			if hashIdx := strings.Index(ch, "#"); hashIdx != -1 {
				chainName = ch[hashIdx+1:]
			}
			if uName, err := url.QueryUnescape(chainName); err == nil {
				chainName = uName
			}

			atoms = append(atoms, model.AtomNode{
				ID:        fmt.Sprintf("chain-%s", chainName),
				Name:      chainName,
				URL:       ch,
				Protocol:  "chain",
				Country:   country,
				Kind:      "chain",
				ChinaExit: false,
				Online:    true,
				TestedAt:  time.Now(),
			})
		}
	}

	return atoms
}

// QueryAtoms 按条件过滤原子节点
func (o *Orchestrator) QueryAtoms(filter AtomQueryFilter) []model.AtomNode {
	all := o.CollectAtoms()
	var filtered []model.AtomNode

	reqCountry := strings.ToUpper(strings.TrimSpace(filter.Country))
	reqKind := strings.ToLower(strings.TrimSpace(filter.Kind))

	for _, a := range all {
		if reqCountry != "" && a.Country != reqCountry {
			continue
		}
		if reqKind != "" && a.Kind != reqKind {
			continue
		}
		if filter.CleanOnly && a.ChinaExit {
			continue
		}
		filtered = append(filtered, a)
	}

	return filtered
}

// EvaluateGroup 根据自定义分组规则求值计算，输出满足公式的 URL 切片
func (o *Orchestrator) EvaluateGroup(filter model.GroupFilter) []string {
	atoms := o.CollectAtoms()
	var matchedURLs []string
	seen := make(map[string]bool)

	countrySet := make(map[string]bool)
	for _, c := range filter.Countries {
		countrySet[strings.ToUpper(strings.TrimSpace(c))] = true
	}

	var matchRegex *regexp.Regexp
	if filter.CustomMatchRegex != "" {
		matchRegex, _ = regexp.Compile(filter.CustomMatchRegex)
	}

	for _, a := range atoms {
		// 1. 排除送中
		if filter.ExcludeChinaLeak && a.ChinaExit {
			continue
		}

		// 2. 国家匹配 (若设置了国家限制)
		if len(countrySet) > 0 && !countrySet[a.Country] {
			continue
		}

		// 3. 类型匹配 (线路机 / 家宽直连 / 链式中继)
		kindMatched := false
		if a.Kind == "transit" && filter.IncludeTransit {
			kindMatched = true
		} else if a.Kind == "residential" && filter.IncludeResidential {
			kindMatched = true
		} else if a.Kind == "chain" && filter.IncludeChain {
			kindMatched = true
		}

		if !kindMatched {
			continue
		}

		// 4. 正则匹配
		if matchRegex != nil && !matchRegex.MatchString(a.Name) && !matchRegex.MatchString(a.URL) {
			continue
		}

		if !seen[a.URL] {
			seen[a.URL] = true
			matchedURLs = append(matchedURLs, a.URL)
		}
	}

	return matchedURLs
}

// GetCategoriesSummary 结构化输出当前全局分类大盘
func (o *Orchestrator) GetCategoriesSummary() map[string]any {
	atoms := o.CollectAtoms()

	type CategoryStat struct {
		Country          string           `json:"country"`
		DisplayName      string           `json:"display_name"`
		TransitCount     int              `json:"transit_count"`
		ResidentialCount int              `json:"residential_count"`
		ChainCount       int              `json:"chain_count"`
		TotalCleanCount  int              `json:"total_clean_count"`
		Nodes            []model.AtomNode `json:"nodes"`
	}

	usStat := &CategoryStat{Country: "US", DisplayName: "美国"}
	jpStat := &CategoryStat{Country: "JP", DisplayName: "日本"}
	otherStat := &CategoryStat{Country: "OTHER", DisplayName: "其他地区"}
	var chinaLeaks []model.AtomNode

	for _, a := range atoms {
		if a.ChinaExit {
			chinaLeaks = append(chinaLeaks, a)
			continue
		}

		var target *CategoryStat
		switch a.Country {
		case "US":
			target = usStat
		case "JP":
			target = jpStat
		default:
			target = otherStat
		}

		target.TotalCleanCount++
		switch a.Kind {
		case "transit":
			target.TransitCount++
		case "residential":
			target.ResidentialCount++
		case "chain":
			target.ChainCount++
		}
		target.Nodes = append(target.Nodes, a)
	}

	return map[string]any{
		"status": "ok",
		"total_atoms": len(atoms),
		"categories": map[string]any{
			"us":          usStat,
			"jp":          jpStat,
			"other":       otherStat,
			"china_leaks": chinaLeaks,
		},
	}
}

// LoadCustomGroups 从磁盘读取所有动态自定义分组配置
func LoadCustomGroups() ([]model.CustomGroupDef, error) {
	customGroupsMu.RLock()
	defer customGroupsMu.RUnlock()

	if _, err := os.Stat(customGroupsFile); os.IsNotExist(err) {
		return []model.CustomGroupDef{}, nil
	}

	data, err := os.ReadFile(customGroupsFile)
	if err != nil {
		return nil, err
	}

	var groups []model.CustomGroupDef
	if err := json.Unmarshal(data, &groups); err != nil {
		return []model.CustomGroupDef{}, nil
	}

	return groups, nil
}

// SaveCustomGroup 保存或更新一个动态自定义分组定义
func SaveCustomGroup(def model.CustomGroupDef) error {
	customGroupsMu.Lock()
	defer customGroupsMu.Unlock()

	groups := []model.CustomGroupDef{}
	if data, err := os.ReadFile(customGroupsFile); err == nil {
		_ = json.Unmarshal(data, &groups)
	}

	nowStr := time.Now().Format("2006-01-02 15:04:05")
	if def.CreatedAt == "" {
		def.CreatedAt = nowStr
	}
	def.UpdatedAt = nowStr

	updated := false
	for i, g := range groups {
		if g.Name == def.Name {
			groups[i] = def
			updated = true
			break
		}
	}
	if !updated {
		groups = append(groups, def)
	}

	_ = os.MkdirAll(filepath.Dir(customGroupsFile), 0755)
	bytes, err := json.MarshalIndent(groups, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(customGroupsFile, bytes, 0644)
}

// DeleteCustomGroup 删除一个动态自定义分组定义
func DeleteCustomGroup(name string) error {
	customGroupsMu.Lock()
	defer customGroupsMu.Unlock()

	groups := []model.CustomGroupDef{}
	if data, err := os.ReadFile(customGroupsFile); err == nil {
		_ = json.Unmarshal(data, &groups)
	}

	var newGroups []model.CustomGroupDef
	found := false
	for _, g := range groups {
		if g.Name == name {
			found = true
			continue
		}
		newGroups = append(newGroups, g)
	}

	if !found {
		return fmt.Errorf("custom group '%s' not found", name)
	}

	_ = os.MkdirAll(filepath.Dir(customGroupsFile), 0755)
	bytes, err := json.MarshalIndent(newGroups, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(customGroupsFile, bytes, 0644)
}

// SyncAllCustomGroups 评估并同步所有动态自定义分组至 Sub-Store
func (o *Orchestrator) SyncAllCustomGroups(ctx context.Context) error {
	if o.scheduler == nil || o.scheduler.substoreClient == nil {
		return nil
	}

	groups, err := LoadCustomGroups()
	if err != nil || len(groups) == 0 {
		return nil
	}

	for _, g := range groups {
		if !g.Enabled {
			continue
		}
		nodes := o.EvaluateGroup(g.Filter)
		if len(nodes) == 0 {
			nodes = []string{fmt.Sprintf("# No active nodes matched for %s", g.DisplayName)}
		}
		_ = o.scheduler.substoreClient.CreateOrUpdateRemoteSub(ctx, g.Name, g.DisplayName, "")
		logger.Sync("[CUSTOM-GROUP] ✅ Synced dynamic custom group '%s' (%s) with %d nodes", g.Name, g.DisplayName, len(nodes))
	}

	return nil
}
