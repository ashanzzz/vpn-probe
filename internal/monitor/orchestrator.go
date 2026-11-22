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
	customChainsFile = filepath.Join("data", "custom_chains.json")
	customChainsMu   sync.RWMutex
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

// SetCustomChainsFileForTest 测试自定义链式文件重定向钩子
func SetCustomChainsFileForTest(p string) func() {
	customChainsMu.Lock()
	old := customChainsFile
	customChainsFile = p
	customChainsMu.Unlock()
	return func() {
		customChainsMu.Lock()
		customChainsFile = old
		customChainsMu.Unlock()
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
		if state.LastResult == nil {
			continue
		}
		res := state.LastResult
		if res.NodeType == "residential" {
			// 家宽机：只要不是机器彻底关机(FaultMachineDown)，均纳入原子池与中继中转
			if res.Diagnostic != nil && res.Diagnostic.FaultType == model.FaultMachineDown {
				continue
			}
			resis = append(resis, res)
		} else {
			if !res.Online {
				continue
			}
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
		country := model.ResolveCountry(t.ExitCountry, t.Name)
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
		country := model.ResolveCountry(r.ExitCountry, r.Name)
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

	// 3. 线路拉家宽链式中继原子 (方案B通用区域池化 + 显式定制绑定)
	if o.scheduler.healer != nil {
		chains := o.scheduler.healer.BuildFullMeshRegionalChains(transits, uniqueResis)

		// 合并用户显式定制的动态链式绑定
		customChains, _ := LoadCustomChains()
		for _, cc := range customChains {
			chains = append(chains, cc.ChainURL)
		}

		for _, ch := range chains {
			decodedCh, err := url.QueryUnescape(ch)
			if err != nil {
				decodedCh = ch
			}
			targetPart := decodedCh
			if idx := strings.Index(decodedCh, "➔"); idx != -1 {
				targetPart = decodedCh[idx+len("➔"):]
			} else if hashIdx := strings.Index(decodedCh, "#"); hashIdx != -1 {
				targetPart = decodedCh[hashIdx+1:]
			}
			country := model.ResolveCountry("", targetPart)
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
		Country          string           `json:"country,omitempty"`
		DisplayName      string           `json:"display_name"`
		TransitCount     int              `json:"transit_count"`
		ResidentialCount int              `json:"residential_count"`
		ChainCount       int              `json:"chain_count"`
		TotalCleanCount  int              `json:"total_clean_count"`
		Nodes            []model.AtomNode `json:"nodes"`
	}

	transitStat := &CategoryStat{DisplayName: "自建-线路分组 (纯骨干)"}
	allResiStat := &CategoryStat{DisplayName: "自建-家宽分组 (全量自建家宽池)"}
	usStat := &CategoryStat{Country: "US", DisplayName: "自建-美国家宽分组"}
	jpStat := &CategoryStat{Country: "JP", DisplayName: "自建-日本家宽分组"}
	otherStat := &CategoryStat{Country: "OTHER", DisplayName: "自建-其他家宽分组"}
	var chinaLeaks []model.AtomNode

	// 获取外部分组信息 (外部商业机场订阅池)
	var extSources []string
	if o.scheduler != nil && o.scheduler.substoreClient != nil {
		extSources = o.scheduler.substoreClient.GetExternalSources()
	} else if o.scheduler != nil && o.scheduler.cfg != nil {
		extSources = o.scheduler.cfg.SubStore.ExternalSources
	}
	if len(extSources) == 0 {
		extSources = []string{"蜂窝云"}
	}

	externalStat := map[string]any{
		"display_name": "外部分组 (外部机场池)",
		"sources":      extSources,
		"source_count": len(extSources),
		"description":  "独立的外部机场订阅池，随时插拔，参与 Q-汇总 与外部组合分发",
	}

	for _, a := range atoms {
		if a.ChinaExit {
			chinaLeaks = append(chinaLeaks, a)
			continue
		}

		if a.Kind == "transit" {
			transitStat.TotalCleanCount++
			transitStat.TransitCount++
			transitStat.Nodes = append(transitStat.Nodes, a)
		} else if a.Kind == "residential" || a.Kind == "chain" {
			allResiStat.TotalCleanCount++
			if a.Kind == "residential" {
				allResiStat.ResidentialCount++
			} else {
				allResiStat.ChainCount++
			}
			allResiStat.Nodes = append(allResiStat.Nodes, a)
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

	// 动态自定义分组加载
	customGroups, _ := LoadCustomGroups()
	var customSummary []map[string]any
	for _, cg := range customGroups {
		matched := o.EvaluateGroup(cg.Filter)
		customSummary = append(customSummary, map[string]any{
			"name":         cg.Name,
			"display_name": cg.DisplayName,
			"enabled":      cg.Enabled,
			"filter":       cg.Filter,
			"count":        len(matched),
			"created_at":   cg.CreatedAt,
			"updated_at":   cg.UpdatedAt,
		})
	}

	return map[string]any{
		"status":      "ok",
		"total_atoms": len(atoms),
		"categories": map[string]any{
			"external":    externalStat, // 外部分组
			"residential": allResiStat,  // 自建-家宽分组 (全量自建家宽池)
			"us_resi":     usStat,       // 自建-美国家宽分组
			"jp_resi":     jpStat,       // 自建-日本家宽分组
			"other_resi":  otherStat,    // 自建-其他家宽分组
			"transit":     transitStat,  // 自建-线路分组
			"china_leaks": chinaLeaks,   // 自建-送中分组 (严禁中继分流)
			// 兼容旧字段
			"us":    usStat,
			"jp":    jpStat,
			"other": otherStat,
		},
		"custom_groups": customSummary,
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

	groups := make([]model.CustomGroupDef, 0)
	if err := json.Unmarshal(data, &groups); err != nil {
		return []model.CustomGroupDef{}, nil
	}
	if groups == nil {
		groups = make([]model.CustomGroupDef, 0)
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
		nodeCount := len(nodes)
		if nodeCount == 0 {
			nodes = []string{}
		}
		_ = o.scheduler.substoreClient.UpdateSubscriptionNodes(ctx, g.Name, nodes)
		logger.Sync("[CUSTOM-GROUP] ✅ Synced dynamic custom group '%s' (%s) with %d nodes", g.Name, g.DisplayName, nodeCount)
	}

	return nil
}

// CustomChainBind 显式自定义的中继绑定 (用户通过 API 指定某台线路机拉某台家宽)
type CustomChainBind struct {
	ID              string `json:"id"`
	TransitName     string `json:"transit_name"`
	ResidentialName string `json:"residential_name"`
	ChainName       string `json:"chain_name"`
	ChainURL        string `json:"chain_url"`
	CreatedAt       string `json:"created_at"`
}

// LoadCustomChains 读取所有自定义显式链式绑定
func LoadCustomChains() ([]CustomChainBind, error) {
	customChainsMu.RLock()
	defer customChainsMu.RUnlock()

	if _, err := os.Stat(customChainsFile); os.IsNotExist(err) {
		return []CustomChainBind{}, nil
	}

	data, err := os.ReadFile(customChainsFile)
	if err != nil {
		return nil, err
	}

	chains := make([]CustomChainBind, 0)
	if err := json.Unmarshal(data, &chains); err != nil {
		return []CustomChainBind{}, nil
	}
	if chains == nil {
		chains = make([]CustomChainBind, 0)
	}

	return chains, nil
}

// SaveCustomChains 保存自定义显式链式绑定列表
func SaveCustomChains(chains []CustomChainBind) error {
	customChainsMu.Lock()
	defer customChainsMu.Unlock()
	_ = os.MkdirAll(filepath.Dir(customChainsFile), 0755)
	data, err := json.MarshalIndent(chains, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(customChainsFile, data, 0644)
}

// BindExplicitChain 显式创建一条“指定线路机拉指定家宽机”的定制连接 (满足用户 API 灵活拉线路需求)
func (o *Orchestrator) BindExplicitChain(transitName, resiName string) (CustomChainBind, error) {
	if o.scheduler == nil {
		return CustomChainBind{}, fmt.Errorf("scheduler not available")
	}

	o.scheduler.mu.RLock()
	var transitNode *model.NodeProbeResult
	var resiNode *model.NodeProbeResult

	for _, s := range o.scheduler.nodeStates {
		if s.LastResult == nil {
			continue
		}
		res := s.LastResult
		if strings.EqualFold(res.Name, transitName) || strings.EqualFold(res.ID, transitName) || strings.Contains(strings.ToLower(res.Name), strings.ToLower(transitName)) {
			transitNode = res
		}
		if strings.EqualFold(res.Name, resiName) || strings.EqualFold(res.ID, resiName) || strings.Contains(strings.ToLower(res.Name), strings.ToLower(resiName)) {
			resiNode = res
		}
	}
	o.scheduler.mu.RUnlock()

	// 支持直接传入原始代理 URL 作为线路机
	if transitNode == nil && strings.Contains(transitName, "://") {
		tag := "transit-custom"
		if u, err := url.Parse(transitName); err == nil && u.Fragment != "" {
			if unescaped, err := url.QueryUnescape(u.Fragment); err == nil {
				tag = unescaped
			} else {
				tag = u.Fragment
			}
		}
		transitNode = &model.NodeProbeResult{
			ID:          tag,
			Name:        tag,
			OriginalURL: transitName,
			NodeType:    "transit",
			Online:      true,
		}
	}

	// 支持直接传入原始代理 URL 作为家宽机
	if resiNode == nil && strings.Contains(resiName, "://") {
		tag := "residential-custom"
		if u, err := url.Parse(resiName); err == nil && u.Fragment != "" {
			if unescaped, err := url.QueryUnescape(u.Fragment); err == nil {
				tag = unescaped
			} else {
				tag = u.Fragment
			}
		}
		resiNode = &model.NodeProbeResult{
			ID:          tag,
			Name:        tag,
			OriginalURL: resiName,
			NodeType:    "residential",
			Online:      true,
		}
	}

	if transitNode == nil {
		return CustomChainBind{}, fmt.Errorf("transit node '%s' not found or offline", transitName)
	}
	if resiNode == nil {
		return CustomChainBind{}, fmt.Errorf("residential node '%s' not found", resiName)
	}

	origURL := resiNode.OriginalURL
	hashIdx := strings.Index(origURL, "#")
	base := origURL
	resiLabel := resiNode.Name
	if hashIdx != -1 {
		base = origURL[:hashIdx]
		resiLabel = origURL[hashIdx+1:]
	}

	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}

	chainName := fmt.Sprintf("链式|%s ➔ %s", transitNode.Name, resiLabel)
	chainURL := fmt.Sprintf("%s%sdialerProxy=%s#%s", base, separator, url.QueryEscape(transitNode.Name), chainName)

	bind := CustomChainBind{
		ID:              fmt.Sprintf("bind-%d", time.Now().UnixNano()),
		TransitName:     transitNode.Name,
		ResidentialName: resiNode.Name,
		ChainName:       chainName,
		ChainURL:        chainURL,
		CreatedAt:       time.Now().Format("2006-01-02 15:04:05"),
	}

	customChainsMu.Lock()
	defer customChainsMu.Unlock()

	chains := []CustomChainBind{}
	if data, err := os.ReadFile(customChainsFile); err == nil {
		_ = json.Unmarshal(data, &chains)
	}

	// 存在相同绑定则替换，否则追加
	updated := false
	for i, c := range chains {
		if c.TransitName == bind.TransitName && c.ResidentialName == bind.ResidentialName {
			chains[i] = bind
			updated = true
			break
		}
	}
	if !updated {
		chains = append(chains, bind)
	}

	_ = os.MkdirAll(filepath.Dir(customChainsFile), 0755)
	bytes, err := json.MarshalIndent(chains, "", "  ")
	if err != nil {
		return bind, err
	}
	_ = os.WriteFile(customChainsFile, bytes, 0644)

	logger.Sync("[CHAIN-BIND] 🔗 Custom chain bound: %s ➔ %s (URL: %s)", transitNode.Name, resiNode.Name, chainName)
	return bind, nil
}

// UnbindExplicitChain 移除某条指定的显式中继或指定家宽的旧连接
func (o *Orchestrator) UnbindExplicitChain(target string) error {
	customChainsMu.Lock()
	defer customChainsMu.Unlock()

	chains := []CustomChainBind{}
	if data, err := os.ReadFile(customChainsFile); err == nil {
		_ = json.Unmarshal(data, &chains)
	}

	var newChains []CustomChainBind
	found := false
	for _, c := range chains {
		if c.ID == target || c.ChainName == target || c.ResidentialName == target || strings.Contains(c.ChainName, target) {
			found = true
			continue
		}
		newChains = append(newChains, c)
	}

	if !found {
		return fmt.Errorf("custom chain target '%s' not found", target)
	}

	_ = os.MkdirAll(filepath.Dir(customChainsFile), 0755)
	bytes, err := json.MarshalIndent(newChains, "", "  ")
	if err != nil {
		return err
	}
	_ = os.WriteFile(customChainsFile, bytes, 0644)

	logger.Sync("[CHAIN-UNBIND] ✂️ Custom chain removed for target: %s", target)
	return nil
}
