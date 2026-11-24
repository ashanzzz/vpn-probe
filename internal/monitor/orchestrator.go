package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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
	Country       string // US, JP, SG...
	Kind          string // transit, residential, chain
	CleanOnly     bool   // 仅查询不送中的干净节点
	ChinaLeakOnly bool   // 仅查询送中的节点
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
		fullMesh := o.scheduler.healer.BuildFullMeshRegionalChains(transits, uniqueResis)
		chains := mergeCustomChains(fullMesh)

		for _, ch := range chains {
			chainName := ch
			if hashIdx := strings.Index(ch, "#"); hashIdx != -1 {
				chainName = ch[hashIdx+1:]
			}
			if uName, err := url.QueryUnescape(chainName); err == nil {
				chainName = uName
			}

			// 优先从标签名中解析中继与落地目标，避免 URL 参数/密码中的特殊符号干扰
			chainLabel := chainName
			if pipeIdx := strings.Index(chainLabel, "|"); pipeIdx != -1 {
				chainLabel = strings.TrimSpace(chainLabel[pipeIdx+1:])
			}
			transitPart := ""
			targetPart := chainLabel
			if arrowIdx := strings.Index(chainLabel, "➔"); arrowIdx != -1 {
				transitPart = strings.TrimSpace(chainLabel[:arrowIdx])
				targetPart = strings.TrimSpace(chainLabel[arrowIdx+len("➔"):])
			}
			country := model.ResolveCountry("", targetPart)

			chainOnline := true
			testedAt := time.Now()
			chainLatency := 0
			chainChinaExit := false

			// 匹配关联中转专线延迟与送中状态
			if transitPart != "" {
				for _, t := range transits {
					if t == nil || strings.TrimSpace(t.Name) == "" {
						continue
					}
					if MatchNodeName(transitPart, t.Name) {
						chainLatency += t.LatencyMs
						if t.ChinaExit {
							chainChinaExit = true
						}
						break
					}
				}
			}

			// 匹配关联目标家宽节点的真实反向隧道状态、延迟与送中状态
			if targetPart != "" {
				for _, r := range uniqueResis {
					if r == nil || strings.TrimSpace(r.Name) == "" {
						continue
					}
					if MatchNodeName(targetPart, r.Name) {
						testedAt = r.TestedAt
						if r.Diagnostic != nil && r.Diagnostic.AbroadRTTMs > 0 {
							chainLatency += r.Diagnostic.AbroadRTTMs
						} else if r.LatencyMs > 0 {
							chainLatency += r.LatencyMs
						}
						if r.ChinaExit {
							chainChinaExit = true
						}
						if !r.Online {
							// 仅当住宅节点海外反向链路畅通且处于纯国内直连受阻时，链式中继才有效；家庭端掉线或中转宕机均判定为离线
							if r.Diagnostic != nil && (r.Diagnostic.FaultType == model.FaultResidentialTunnelOffline || r.Diagnostic.FaultType == model.FaultTransitRelayDown || !r.Diagnostic.AbroadReachable) {
								chainOnline = false
							}
						}
						break
					}
				}
			}

			atoms = append(atoms, model.AtomNode{
				ID:        fmt.Sprintf("chain-%s", chainName),
				Name:      chainName,
				URL:       ch,
				Protocol:  "chain",
				Country:   country,
				Kind:      "chain",
				ChinaExit: chainChinaExit,
				LatencyMs: chainLatency,
				Online:    chainOnline,
				TestedAt:  testedAt,
			})
		}
	}

	meta, err := LoadNodeMetadata()
	if err != nil {
		logger.Warnf("[ORCHESTRATOR] metadata load failed: %v", err)
	}
	for i := range atoms {
		if m, ok := meta[atoms[i].ID]; ok {
			atoms[i].InfraType = m.InfraType
			atoms[i].Tags = append([]string(nil), m.Tags...)
		}
	}
	// A chain inherits its landing's manually configured NAT/VPS classification
	// and tags unless the chain itself has an explicit override.  A user-selected
	// 'NAT residential' group can therefore include chains without hand-tagging
	// every relay/landing combination.
	landings := make(map[string]model.AtomNode)
	transitMap := make(map[string]model.AtomNode)
	for _, a := range atoms {
		if a.Kind == "residential" {
			landings[a.Name] = a
		} else if a.Kind == "transit" {
			transitMap[a.Name] = a
		}
	}
	for i := range atoms {
		if atoms[i].Kind != "chain" {
			continue
		}
		parts := strings.SplitN(atoms[i].Name, "➔", 2)
		if len(parts) != 2 {
			continue
		}
		landingName := strings.TrimSpace(parts[1])
		landing, ok := landings[landingName]
		if !ok && landingName != "" {
			for k, v := range landings {
				if MatchNodeName(k, landingName) {
					landing = v
					ok = true
					break
				}
			}
		}
		if ok {
			if atoms[i].InfraType == "" {
				atoms[i].InfraType = landing.InfraType
			}
			if len(atoms[i].Tags) == 0 {
				atoms[i].Tags = append([]string(nil), landing.Tags...)
			}
			if landing.ChinaExit {
				atoms[i].ChinaExit = true
			}
		}
		transitName := strings.TrimSpace(parts[0])
		if pipeIdx := strings.Index(transitName, "|"); pipeIdx != -1 {
			transitName = strings.TrimSpace(transitName[pipeIdx+1:])
		}
		tr, trOk := transitMap[transitName]
		if !trOk && transitName != "" {
			for k, v := range transitMap {
				if MatchNodeName(k, transitName) {
					tr = v
					trOk = true
					break
				}
			}
		}
		if trOk {
			if tr.ChinaExit {
				atoms[i].ChinaExit = true
			}
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
		if filter.ChinaLeakOnly && !a.ChinaExit {
			continue
		}
		filtered = append(filtered, a)
	}

	return filtered
}

// EvaluateGroup returns URLs for compatibility with the existing preview API.
func (o *Orchestrator) EvaluateGroup(filter model.GroupFilter) []string {
	matched := MatchAtoms(o.CollectAtoms(), filter)
	links := make([]string, 0, len(matched))
	seen := map[string]bool{}
	for _, atom := range matched {
		if !seen[atom.URL] {
			seen[atom.URL] = true
			links = append(links, atom.URL)
		}
	}
	return links
}

// EvaluateGroupAtoms preserves stable IDs/names for the policy compiler.
func (o *Orchestrator) EvaluateGroupAtoms(filter model.GroupFilter) []model.AtomNode {
	return MatchAtoms(o.CollectAtoms(), filter)
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

	transitStat := &CategoryStat{DisplayName: "自建-线路机"}
	allResiStat := &CategoryStat{DisplayName: "自建-家宽分组"}
	usStat := &CategoryStat{Country: "US", DisplayName: "自建-美国和美国家宽"}
	jpStat := &CategoryStat{Country: "JP", DisplayName: "自建-日本和日本家宽"}
	twStat := &CategoryStat{Country: "TW", DisplayName: "自建-台湾家宽分组"}
	otherStat := &CategoryStat{Country: "OTHER", DisplayName: "自建-其他"}
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
		case "TW":
			target = twStat
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
		matchedAtoms := MatchAtoms(atoms, cg.Filter)
		seenLinks := make(map[string]bool, len(matchedAtoms))
		for _, a := range matchedAtoms {
			seenLinks[a.URL] = true
		}
		customSummary = append(customSummary, map[string]any{
			"name":         cg.Name,
			"display_name": cg.DisplayName,
			"enabled":      cg.Enabled,
			"filter":       cg.Filter,
			"count":        len(seenLinks),
			"created_at":   cg.CreatedAt,
			"updated_at":   cg.UpdatedAt,
		})
	}

	return map[string]any{
		"status":      "ok",
		"total_atoms": len(atoms),
		"categories": map[string]any{
			"external":       externalStat, // 外部分组
			"residential":    allResiStat,  // 自建-家宽分组 (全量自建家宽池)
			"transit":        transitStat,  // 自建-线路机
			"us_resi":        usStat,       // 自建-美国和美国家宽
			"jp_resi":        jpStat,       // 自建-日本和日本家宽
			"tw_resi":        twStat,       // 自建-台湾家宽分组
			"other_resi":     otherStat,    // 自建-其他
			"china_leaks":    chinaLeaks,   // 自建-送中
			// 规范干净组命名与兼容旧字段
			"transit_clean":  transitStat,
			"us_clean":       usStat,
			"jp_clean":       jpStat,
			"tw_clean":       twStat,
			"other_clean":    otherStat,
			"songzhong":      chinaLeaks,
			"us":             usStat,
			"jp":             jpStat,
			"tw":             twStat,
			"other":          otherStat,
		},
		"custom_groups": customSummary,
	}
}

// LoadCustomGroups fails visibly on corrupt JSON: never silently publish an empty policy.
func LoadCustomGroups() ([]model.CustomGroupDef, error) {
	customGroupsMu.RLock()
	defer customGroupsMu.RUnlock()
	b, err := os.ReadFile(customGroupsFile)
	if os.IsNotExist(err) {
		return []model.CustomGroupDef{}, nil
	}
	if err != nil {
		return nil, err
	}
	groups := []model.CustomGroupDef{}
	if err = json.Unmarshal(b, &groups); err != nil {
		return nil, fmt.Errorf("decode custom groups: %w", err)
	}
	return groups, nil
}

// SaveCustomGroup is an atomic, validated upsert. Name is a stable immutable key.
func SaveCustomGroup(def model.CustomGroupDef) error {
	if def.Type == "" {
		def.Type = "select"
	}
	if err := def.Validate(); err != nil {
		return err
	}
	customGroupsMu.Lock()
	defer customGroupsMu.Unlock()
	groups := []model.CustomGroupDef{}
	if b, err := os.ReadFile(customGroupsFile); err == nil {
		if err = json.Unmarshal(b, &groups); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// 严格校验 DisplayName 唯一性：不能与任何其他分组（无论在当前分组前还是后）的 DisplayName 冲突
	for _, g := range groups {
		if g.Name != def.Name && g.DisplayName == def.DisplayName {
			return fmt.Errorf("duplicate display_name %q", def.DisplayName)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	def.UpdatedAt = now
	updated := false
	for i, g := range groups {
		if g.Name == def.Name {
			def.CreatedAt = g.CreatedAt
			groups[i] = def
			updated = true
			break
		}
	}
	if def.CreatedAt == "" {
		def.CreatedAt = now
	}
	if !updated {
		groups = append(groups, def)
	}
	return writeAtomicJSON(customGroupsFile, groups)
}

func DeleteCustomGroup(name string) error {
	customGroupsMu.Lock()
	defer customGroupsMu.Unlock()
	groups := []model.CustomGroupDef{}
	if b, err := os.ReadFile(customGroupsFile); err == nil {
		if err = json.Unmarshal(b, &groups); err != nil {
			return err
		}
	} else {
		return err
	}
	next := make([]model.CustomGroupDef, 0, len(groups))
	found := false
	for _, g := range groups {
		if g.Name == name {
			found = true
		} else {
			next = append(next, g)
		}
	}
	if !found {
		return fmt.Errorf("custom group %q not found", name)
	}
	return writeAtomicJSON(customGroupsFile, next)
}

// SaveCustomGroupsBatch atomically validates and overwrites all custom groups.
func SaveCustomGroupsBatch(defs []model.CustomGroupDef) error {
	if len(defs) == 0 {
		return fmt.Errorf("cannot save empty custom groups batch")
	}
	seenNames := make(map[string]bool)
	seenDisplays := make(map[string]bool)
	now := time.Now().UTC().Format(time.RFC3339)

	validated := make([]model.CustomGroupDef, len(defs))
	for i := range defs {
		def := defs[i]
		if def.Name == "" {
			return fmt.Errorf("group name is required")
		}
		if def.DisplayName == "" {
			def.DisplayName = def.Name
		}
		if def.Type == "" {
			def.Type = "url-test"
		}
		if err := def.Validate(); err != nil {
			return fmt.Errorf("group %q invalid: %w", def.Name, err)
		}
		if seenNames[def.Name] {
			return fmt.Errorf("duplicate group name %q", def.Name)
		}
		seenNames[def.Name] = true

		if seenDisplays[def.DisplayName] {
			return fmt.Errorf("duplicate display_name %q", def.DisplayName)
		}
		seenDisplays[def.DisplayName] = true

		if def.CreatedAt == "" {
			def.CreatedAt = now
		}
		def.UpdatedAt = now
		validated[i] = def
	}

	customGroupsMu.Lock()
	defer customGroupsMu.Unlock()
	return writeAtomicJSON(customGroupsFile, validated)
}

// EnsureDefaultCustomGroups initializes baseline custom groups if none exist.
func EnsureDefaultCustomGroups() error {
	customGroupsMu.Lock()
	defer customGroupsMu.Unlock()
	b, err := os.ReadFile(customGroupsFile)
	if err == nil {
		var existing []model.CustomGroupDef
		if json.Unmarshal(b, &existing) == nil && len(existing) > 0 {
			return nil
		}
	}
	defaults := model.DefaultCustomGroups()
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range defaults {
		defaults[i].CreatedAt = now
		defaults[i].UpdatedAt = now
	}
	return writeAtomicJSON(customGroupsFile, defaults)
}

// SyncAllCustomGroups is kept only for legacy callers and now propagates real errors.
func (o *Orchestrator) SyncAllCustomGroups(ctx context.Context) error {
	if o.scheduler == nil || o.scheduler.substoreClient == nil {
		return fmt.Errorf("substore client unavailable")
	}
	groups, err := LoadCustomGroups()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if !g.Enabled {
			continue
		}
		if err := o.scheduler.substoreClient.UpdateSubscriptionNodes(ctx, g.Name, o.EvaluateGroup(g.Filter)); err != nil {
			return fmt.Errorf("sync %s: %w", g.Name, err)
		}
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

// loadCustomChainsLocked 读取所有自定义显式链式绑定 (调用方须已持有读锁或写锁)
func loadCustomChainsLocked() ([]CustomChainBind, error) {
	if _, err := os.Stat(customChainsFile); os.IsNotExist(err) {
		return []CustomChainBind{}, nil
	}

	data, err := os.ReadFile(customChainsFile)
	if err != nil {
		return nil, err
	}

	chains := make([]CustomChainBind, 0)
	if err := json.Unmarshal(data, &chains); err != nil {
		return nil, fmt.Errorf("unmarshal custom chains from %s: %w", customChainsFile, err)
	}
	if chains == nil {
		chains = make([]CustomChainBind, 0)
	}

	return chains, nil
}

// LoadCustomChains 读取所有自定义显式链式绑定
func LoadCustomChains() ([]CustomChainBind, error) {
	customChainsMu.RLock()
	defer customChainsMu.RUnlock()
	return loadCustomChainsLocked()
}

// SaveCustomChains 保存自定义显式链式绑定列表 (原子写入保证持久化安全)
func SaveCustomChains(chains []CustomChainBind) error {
	customChainsMu.Lock()
	defer customChainsMu.Unlock()
	return writeAtomicJSON(customChainsFile, chains)
}

// BindExplicitChain 显式创建一条“指定线路机拉指定家宽机”的定制连接 (满足用户 API 灵活拉线路需求)
func (o *Orchestrator) BindExplicitChain(transitName, resiName string) (CustomChainBind, error) {
	transitName = strings.TrimSpace(transitName)
	resiName = strings.TrimSpace(resiName)
	if transitName == "" || resiName == "" {
		return CustomChainBind{}, fmt.Errorf("transit and residential node names cannot be empty")
	}

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
		if transitNode == nil || !strings.EqualFold(transitNode.Name, transitName) {
			if strings.EqualFold(res.Name, transitName) || strings.EqualFold(res.ID, transitName) {
				transitNode = res
			} else if transitNode == nil && strings.Contains(strings.ToLower(res.Name), strings.ToLower(transitName)) {
				transitNode = res
			}
		}
		if resiNode == nil || !strings.EqualFold(resiNode.Name, resiName) {
			if strings.EqualFold(res.Name, resiName) || strings.EqualFold(res.ID, resiName) {
				resiNode = res
			} else if resiNode == nil && strings.Contains(strings.ToLower(res.Name), strings.ToLower(resiName)) {
				resiNode = res
			}
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

	chains, err := loadCustomChainsLocked()
	if err != nil {
		return bind, err
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

	if err := writeAtomicJSON(customChainsFile, chains); err != nil {
		return bind, err
	}

	logger.Sync("[CHAIN-BIND] 🔗 Custom chain bound: %s ➔ %s (URL: %s)", transitNode.Name, resiNode.Name, chainName)
	return bind, nil
}

// matchesChainTarget 精确判断目标是否指向该链式绑定，杜绝宽松子串碰撞误删无关绑定
func matchesChainTarget(c CustomChainBind, target string) bool {
	if c.ID == target {
		return true
	}
	if c.ChainName == target || strings.EqualFold(c.ChainName, target) {
		return true
	}
	if c.ResidentialName == target || MatchNodeName(c.ResidentialName, target) {
		return true
	}
	// 支持按 "中继 ➔ 落地" 组合精确解绑
	if strings.Contains(target, "➔") {
		parts := strings.SplitN(target, "➔", 2)
		transit := strings.TrimSpace(parts[0])
		if pipeIdx := strings.Index(transit, "|"); pipeIdx != -1 {
			transit = strings.TrimSpace(transit[pipeIdx+1:])
		}
		resi := strings.TrimSpace(parts[1])
		if MatchNodeName(c.TransitName, transit) && MatchNodeName(c.ResidentialName, resi) {
			return true
		}
	}
	return false
}

// UnbindExplicitChain 移除某条指定的显式中继或指定家宽的旧连接
func (o *Orchestrator) UnbindExplicitChain(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("custom chain target cannot be empty")
	}

	customChainsMu.Lock()
	defer customChainsMu.Unlock()

	chains, err := loadCustomChainsLocked()
	if err != nil {
		return err
	}

	var newChains []CustomChainBind
	found := false
	for _, c := range chains {
		if matchesChainTarget(c, target) {
			found = true
			continue
		}
		newChains = append(newChains, c)
	}

	if !found {
		return fmt.Errorf("custom chain target '%s' not found", target)
	}

	if err := writeAtomicJSON(customChainsFile, newChains); err != nil {
		return err
	}

	logger.Sync("[CHAIN-UNBIND] ✂️ Custom chain removed for target: %s", target)
	return nil
}
