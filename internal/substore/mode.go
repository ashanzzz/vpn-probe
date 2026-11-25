package substore

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"vpn-probe/internal/logger"
)

type SubStoreMode string

const (
	ModeExternalAndSelf SubStoreMode = "external+self" // 模式 A (默认): 外部机场 + 纯自建
	ModeSelfOnly        SubStoreMode = "self"          // 模式 B: 仅自建节点与中继
	ModeExternalOnly    SubStoreMode = "external"      // 模式 C: 仅外部机场节点
)

var ModeSubscriptions = map[SubStoreMode][]string{
	ModeExternalAndSelf: {"蜂窝云", "ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other"},
	ModeSelfOnly:        {"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other"},
	ModeExternalOnly:    {"蜂窝云"},
}

type ExternalSourceItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	URL         string `json:"url,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type ExternalSourcesConfig struct {
	Sources []string             `json:"sources"`
	Items   []ExternalSourceItem `json:"items,omitempty"`
}

var (
	extSourcesFile = filepath.Join("data", "external_sources.json")
	extMutex       sync.RWMutex
)

// SetExternalSourcesFileForTest 设置外部订阅配置文件路径 (仅供跨包测试隔离)
func SetExternalSourcesFileForTest(p string) func() {
	extMutex.Lock()
	defer extMutex.Unlock()
	orig := extSourcesFile
	extSourcesFile = p
	return func() {
		extMutex.Lock()
		defer extMutex.Unlock()
		extSourcesFile = orig
	}
}

// GetExternalSources 获取当前配置的外部订阅列表 (Issue #18 全动态解耦)
func (c *Client) GetExternalSources() []string {
	extMutex.RLock()
	defer extMutex.RUnlock()

	data, err := os.ReadFile(extSourcesFile)
	if err == nil {
		var cfg ExternalSourcesConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			var active []string
			if len(cfg.Items) > 0 {
				for _, it := range cfg.Items {
					if it.Enabled && it.Name != "" {
						active = append(active, it.Name)
					}
				}
				// 明确配置了 Items 时，返回实际启用的列表 (哪怕全部禁用返回空列表)
				return active
			}
			if len(cfg.Sources) > 0 {
				return cfg.Sources
			}
		}
	}
	cfg := c.GetConfig()
	if cfg != nil && len(cfg.ExternalSources) > 0 {
		return cfg.ExternalSources
	}
	return []string{"蜂窝云"}
}

// GetExternalSourceItems 获取外部订阅详情列表 (Issue #19)
func (c *Client) GetExternalSourceItems() []ExternalSourceItem {
	extMutex.RLock()
	defer extMutex.RUnlock()

	data, err := os.ReadFile(extSourcesFile)
	if err == nil {
		var cfg ExternalSourcesConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			if cfg.Items != nil {
				return cfg.Items
			}
			// 若只有 Sources 数组，自动升级构造成 Items
			if len(cfg.Sources) > 0 {
				var items []ExternalSourceItem
				for _, s := range cfg.Sources {
					items = append(items, ExternalSourceItem{
						Name:        s,
						DisplayName: s,
						Enabled:     true,
					})
				}
				return items
			}
			return []ExternalSourceItem{}
		}
	}

	// 未配置且未持久化时返回空列表（禁止硬编码外部订阅 Token URL）
	return []ExternalSourceItem{}
}

// SetExternalSources 设置外部订阅列表并持久化
func (c *Client) SetExternalSources(sources []string) error {
	extMutex.Lock()
	defer extMutex.Unlock()

	_ = os.MkdirAll("data", 0755)

	// 保留现有 Items 中的 URL 元数据
	var existingItems []ExternalSourceItem
	if data, err := os.ReadFile(extSourcesFile); err == nil {
		var cfg ExternalSourcesConfig
		_ = json.Unmarshal(data, &cfg)
		existingItems = cfg.Items
	}

	var newItems []ExternalSourceItem
	for _, s := range sources {
		var item ExternalSourceItem
		found := false
		for _, ex := range existingItems {
			if ex.Name == s {
				item = ex
				item.Enabled = true
				found = true
				break
			}
		}
		if !found {
			item = ExternalSourceItem{
				Name:        s,
				DisplayName: s,
				Enabled:     true,
			}
		}
		newItems = append(newItems, item)
	}

	cfg := ExternalSourcesConfig{
		Sources: sources,
		Items:   newItems,
	}
	bytesData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(extSourcesFile, bytesData, 0644)
}

// UpsertExternalSource 动态添加或更新一个外部订阅，并自动联动 SubStore 创建/更新 (Issue #19)
func (c *Client) UpsertExternalSource(ctx context.Context, item ExternalSourceItem) error {
	if item.Name == "" {
		return fmt.Errorf("name is required")
	}

	// 1. 若提供了 remoteURL，联动 SubStore 自动创建或更新远端订阅
	subCfg := c.GetConfig()
	if item.URL != "" && subCfg != nil && subCfg.Enabled && subCfg.URL != "" {
		if err := c.CreateOrUpdateRemoteSub(ctx, item.Name, item.DisplayName, item.URL); err != nil {
			logger.Warnf("[SUBSTORE] Auto create/update remote sub in SubStore: %v", err)
			return fmt.Errorf("create/update in SubStore failed: %w", err)
		}
	}

	extMutex.Lock()
	defer extMutex.Unlock()

	_ = os.MkdirAll("data", 0755)
	var cfg ExternalSourcesConfig
	if data, err := os.ReadFile(extSourcesFile); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	// 2. 更新或追加到 Items
	itemFound := false
	for i, ex := range cfg.Items {
		if ex.Name == item.Name {
			cfg.Items[i].DisplayName = item.DisplayName
			if item.URL != "" {
				cfg.Items[i].URL = item.URL
			}
			cfg.Items[i].Enabled = item.Enabled
			itemFound = true
			break
		}
	}
	if !itemFound {
		if item.DisplayName == "" {
			item.DisplayName = item.Name
		}
		cfg.Items = append(cfg.Items, item)
	}

	// 3. 重算 Sources 活跃列表
	var activeSources []string
	for _, it := range cfg.Items {
		if it.Enabled {
			activeSources = append(activeSources, it.Name)
		}
	}
	cfg.Sources = activeSources

	bytesData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWriteFile(extSourcesFile, bytesData, 0644); err != nil {
		return err
	}

	return nil
}

// DeleteExternalSource 从外部源池中删除订阅，支持 purge 联动 SubStore 物理删除 (Issue #19)
func (c *Client) DeleteExternalSource(ctx context.Context, name string, purgeRemote bool) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}

	// 1. 若开启 purgeRemote，调用 SubStore 物理删除
	subCfg := c.GetConfig()
	if purgeRemote && subCfg != nil && subCfg.Enabled && subCfg.URL != "" {
		if err := c.DeleteItem(ctx, "subs", name); err != nil {
			logger.Warnf("[SUBSTORE] Purge remote sub '%s' from SubStore failed: %v", name, err)
		} else {
			logger.Sync("[SUBSTORE] ✅ Purged remote sub '%s' from SubStore successfully", name)
		}
	}

	extMutex.Lock()
	defer extMutex.Unlock()

	_ = os.MkdirAll("data", 0755)
	var cfg ExternalSourcesConfig
	if data, err := os.ReadFile(extSourcesFile); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	var newItems []ExternalSourceItem
	for _, it := range cfg.Items {
		if it.Name != name {
			newItems = append(newItems, it)
		}
	}
	cfg.Items = newItems

	var newSources []string
	for _, s := range cfg.Sources {
		if s != name {
			newSources = append(newSources, s)
		}
	}
	cfg.Sources = newSources

	bytesData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(extSourcesFile, bytesData, 0644)
}

// GetModeSubscriptions 动态根据外部机场列表计算当前模式下的订阅成员
func (c *Client) GetModeSubscriptions(mode SubStoreMode) []string {
	extSources := c.GetExternalSources()
	selfSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other"}

	switch mode {
	case ModeSelfOnly:
		return selfSubs
	case ModeExternalOnly:
		return extSources
	case ModeExternalAndSelf:
		fallthrough
	default:
		var all []string
		all = append(all, extSources...)
		all = append(all, selfSubs...)
		return all
	}
}

type ModeState struct {
	CurrentMode SubStoreMode `json:"current_mode"`
	UpdatedAt   string       `json:"updated_at"`
	Details     string       `json:"details,omitempty"`
}

var (
	modeMutex sync.RWMutex
	modeFile  = filepath.Join("data", "substore_mode.json")
)

// GetCurrentMode 获取当前生效的模式，若未配置则默认为 external+self
func (c *Client) GetCurrentMode() SubStoreMode {
	modeMutex.RLock()
	defer modeMutex.RUnlock()

	data, err := os.ReadFile(modeFile)
	if err == nil {
		var state ModeState
		if err := json.Unmarshal(data, &state); err == nil && state.CurrentMode != "" {
			return state.CurrentMode
		}
	}
	return ModeExternalAndSelf
}

// SetMode 动态切换组合订阅三模式并原子持久化 (Issue #17 & #18 解耦架构)
func (c *Client) SetMode(ctx context.Context, mode SubStoreMode) (*ModeState, error) {
	if mode != ModeExternalAndSelf && mode != ModeSelfOnly && mode != ModeExternalOnly {
		return nil, fmt.Errorf("invalid mode: %s (allowed: 'external+self', 'self', 'external')", mode)
	}

	subs := c.GetModeSubscriptions(mode)

	modeMutex.Lock()
	defer modeMutex.Unlock()

	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return nil, fmt.Errorf("substore is not configured or disabled")
	}

	base := strings.TrimRight(cfg.URL, "/")

	// 1. 确保集合 ashan-mode 存在并更新成员
	colPayload := map[string]any{
		"name":                  "ashan-mode",
		"displayName":           fmt.Sprintf("Ashan 动态模式订阅 (%s)", mode),
		"subscriptions":         subs,
		"ignoreFailedRemoteSub": "quiet",
	}

	// 检查现有集合
	cols, err := c.GetCollections(ctx)
	if err == nil {
		hasCol := false
		for _, col := range cols {
			if col.Name == "ashan-mode" {
				hasCol = true
				break
			}
		}

		if hasCol {
			_ = c.UpdateCollection(ctx, "ashan-mode", colPayload)
		} else {
			// POST /api/collections 新建
			bodyBytes, _ := json.Marshal(colPayload)
			req, _ := http.NewRequestWithContext(ctx, "POST", base+"/api/collections", strings.NewReader(string(bodyBytes)))
			req.Header.Set("Content-Type", "application/json")
			c.setAuth(req)
			resp, reqErr := c.getHTTPClient().Do(req)
			if reqErr == nil {
				resp.Body.Close()
			}
		}
	}

	// 2. 职责解耦 (Issue #17):
	// - Q 和 q (动态组合订阅): 绑定 ashan-mode 集合
	for _, fname := range []string{"Q", "q"} {
		patchPayload := map[string]any{
			"source":     "local",
			"sourceType": "collection",
			"sourceName": "ashan-mode",
		}
		_ = c.UpdateFile(ctx, fname, patchPayload)
	}

	// - ashan 和 Ashan (纯自建专属订阅): 始终绑定 ashan-纯净 自建干净集合，毫秒级极速响应，永不受外部机场拖累
	for _, fname := range []string{"ashan", "Ashan"} {
		patchPayload := map[string]any{
			"source":     "local",
			"sourceType": "collection",
			"sourceName": "ashan-纯净",
		}
		_ = c.UpdateFile(ctx, fname, patchPayload)
	}

	// 3. 持久化到本地数据文件
	_ = os.MkdirAll("data", 0755)
	state := &ModeState{
		CurrentMode: mode,
		UpdatedAt:   time.Now().Format(time.RFC3339),
		Details:     fmt.Sprintf("Active subscriptions: %s", strings.Join(subs, ", ")),
	}
	bytesData, _ := json.MarshalIndent(state, "", "  ")
	_ = atomicWriteFile(modeFile, bytesData, 0644)

	logger.Sync("[SUBSTORE] Mode successfully switched to '%s' (active subs: %v)", mode, subs)
	return state, nil
}

// CleanupResult 节点清理响应结构
type CleanupResult struct {
	SubscriptionName string   `json:"subscription_name"`
	OriginalCount    int      `json:"original_count"`
	RemovedCount     int      `json:"removed_count"`
	RemainingCount   int      `json:"remaining_count"`
	BackupPath       string   `json:"backup_path"`
	RemovedNodes     []string `json:"removed_nodes,omitempty"`
}

// CleanupDeadSocks 从指定订阅中清理失效的 socks5 节点（带前置快照落盘备份）
func (c *Client) CleanupDeadSocks(ctx context.Context, subName string) (*CleanupResult, error) {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return nil, fmt.Errorf("substore is not configured or disabled")
	}

	rawContent, err := c.GetSubscriptionContent(ctx, subName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch subscription content: %w", err)
	}

	lines := strings.Split(rawContent, "\n")
	var cleanedLines []string
	var removed []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "socks") {
			removed = append(removed, trimmed)
		} else {
			cleanedLines = append(cleanedLines, line)
		}
	}

	// 创建备份快照 (防路径穿越处理)
	snapDir := filepath.Join("data", "snapshots")
	_ = os.MkdirAll(snapDir, 0755)
	safeSubName := strings.ReplaceAll(filepath.Clean(subName), "/", "_")
	safeSubName = strings.ReplaceAll(safeSubName, "..", "_")
	backupPath := filepath.Join(snapDir, fmt.Sprintf("%s_backup_%s.txt", safeSubName, time.Now().Format("20060102_150405")))
	if err := atomicWriteFile(backupPath, []byte(rawContent), 0644); err != nil {
		return nil, fmt.Errorf("create backup snapshot failed: %w", err)
	}

	if len(removed) > 0 {
		newContent := strings.Join(cleanedLines, "\n")
		base := strings.TrimRight(cfg.URL, "/")
		bodyBytes, _ := json.Marshal(map[string]string{"content": newContent})
		req, _ := http.NewRequestWithContext(ctx, "PATCH", fmt.Sprintf("%s/api/sub/%s", base, url.PathEscape(subName)), strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		c.setAuth(req)

		c.mu.RLock()
		client := c.httpClient
		c.mu.RUnlock()
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to patch subscription: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("substore returned status %d: %s", resp.StatusCode, string(b))
		}
	}

	return &CleanupResult{
		SubscriptionName: subName,
		OriginalCount:    len(lines),
		RemovedCount:     len(removed),
		RemainingCount:   len(cleanedLines),
		BackupPath:       backupPath,
		RemovedNodes:     removed,
	}, nil
}

// ShareDomainCheck 双域名分享链接本地连通性与防墙检测结果
type ShareDomainCheck struct {
	Domain      string `json:"domain"`
	URL         string `json:"url"`
	Reachable   bool   `json:"reachable"`
	StatusCode  int    `json:"status_code"`
	LatencyMs   int64  `json:"latency_ms"`
	Error       string `json:"error,omitempty"`
	PossibleGFW bool   `json:"possible_gfw"` // 若握手/连接超时或被阻断，标记可能被墙
}

// GroupAuditResult 策略组空引用审计结果
type GroupAuditResult struct {
	FileName           string              `json:"file_name"`
	TotalProxies       int                 `json:"total_proxies"`
	TotalGroups        int                 `json:"total_groups"`
	Valid              bool                `json:"valid"`
	DanglingReferences map[string][]string `json:"dangling_references"`
	GroupCounts        map[string]int      `json:"group_counts"`
	DomainsCheck       []ShareDomainCheck  `json:"domains_check,omitempty"`
}

var (
	auditCacheMu sync.RWMutex
	auditCache   = make(map[string]*GroupAuditResult)
)

// AuditFileGroups 审计指定 File 的 Clash 配置是否存在悬空/不存在的节点引用
func (c *Client) AuditFileGroups(ctx context.Context, fileName string) (*GroupAuditResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return nil, fmt.Errorf("substore is not configured or disabled")
	}

	urlsToTry := []string{
		strings.TrimRight(cfg.URL, "/"),
	}
	if !isLocalOrTestEndpoint(cfg.URL) {
		fallbackURL := ResolveFallbackURL(cfg)
		if !strings.Contains(cfg.URL, "156.231.113.91") {
			urlsToTry = append(urlsToTry, fallbackURL)
		}
	}

	var lastErr error
	var bodyBytes []byte

	c.mu.RLock()
	client := c.httpClient
	c.mu.RUnlock()
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}

	for _, base := range urlsToTry {
		fn := func(baseURL string) ([]byte, error) {
			auditCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(auditCtx, "GET", fmt.Sprintf("%s/api/file/%s", baseURL, fileName), nil)
			if err != nil {
				return nil, err
			}
			c.setAuth(req)
			req.Header.Set("User-Agent", "ClashX/1.90.0")

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			b, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
			if err != nil {
				return nil, err
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("substore returned HTTP %d: %s", resp.StatusCode, string(b))
			}

			return b, nil
		}

		b, err := fn(base)
		if err == nil && len(b) > 0 {
			bodyBytes = b
			lastErr = nil
			break
		}
		lastErr = err
	}

	if lastErr != nil {
		// 降级：检查是否有历史内存缓存
		auditCacheMu.RLock()
		cached, exists := auditCache[fileName]
		auditCacheMu.RUnlock()
		if exists && cached != nil {
			logger.Warnf("[SUBSTORE-AUDIT] Fetching file %s failed (%v), returning cached audit (%d groups, %d proxies)",
				fileName, lastErr, cached.TotalGroups, cached.TotalProxies)
			return cached, nil
		}
		return nil, fmt.Errorf("failed to fetch file: %w", lastErr)
	}

	return AuditFileYAML(bodyBytes, fileName)
}

// AuditFileYAML parses Clash YAML bytes and audits policy groups for dangling references
func AuditFileYAML(bodyBytes []byte, fileName string) (*GroupAuditResult, error) {
	var parsed struct {
		Proxies []map[string]any `yaml:"proxies"`
		Groups  []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}

	if err := yaml.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse Clash YAML: %w", err)
	}

	if len(parsed.Proxies) == 0 {
		return &GroupAuditResult{
			FileName:           fileName,
			TotalProxies:       0,
			Valid:              false,
			DanglingReferences: make(map[string][]string),
			GroupCounts:        make(map[string]int),
		}, fmt.Errorf("subscription '%s' rendered 0 proxies", fileName)
	}

	proxySet := make(map[string]bool)
	for _, p := range parsed.Proxies {
		if name, ok := p["name"].(string); ok {
			proxySet[name] = true
		}
	}

	dangling := make(map[string][]string)
	groupCounts := make(map[string]int)
	allValid := true

	for _, g := range parsed.Groups {
		groupCounts[g.Name] = len(g.Proxies)
		for _, m := range g.Proxies {
			if m == "DIRECT" || m == "REJECT" || m == "REJECT-DROP" || m == "PASS" || m == "COMPATIBLE" {
				continue
			}
			// 也可能是策略组嵌套引用
			isOtherGroup := false
			for _, other := range parsed.Groups {
				if other.Name == m {
					isOtherGroup = true
					break
				}
			}
			if !proxySet[m] && !isOtherGroup {
				dangling[g.Name] = append(dangling[g.Name], m)
				allValid = false
			}
		}
	}

	res := &GroupAuditResult{
		FileName:           fileName,
		TotalProxies:       len(parsed.Proxies),
		TotalGroups:        len(parsed.Groups),
		Valid:              allValid,
		DanglingReferences: dangling,
		GroupCounts:        groupCounts,
	}

	auditCacheMu.Lock()
	auditCache[fileName] = res
	auditCacheMu.Unlock()

	return res, nil
}

var (
	shareDomains   = []string{"free.1992004.xyz", "sub.ashanzzz.com"}
	shareDomainsMu sync.RWMutex
)

// SetShareDomainsForTest 测试钩子：替换检测域名
func SetShareDomainsForTest(domains []string) func() {
	shareDomainsMu.Lock()
	old := shareDomains
	shareDomains = domains
	shareDomainsMu.Unlock()
	return func() {
		shareDomainsMu.Lock()
		shareDomains = old
		shareDomainsMu.Unlock()
	}
}

func shareDomainsSnapshot() []string {
	shareDomainsMu.RLock()
	defer shareDomainsMu.RUnlock()
	return append([]string(nil), shareDomains...)
}

// CheckShareDomains 并发对配置域名进行本地连通性与防墙检测
func CheckShareDomains(ctx context.Context, fileName string, token string) []ShareDomainCheck {
	shareDomainsMu.RLock()
	domains := append([]string{}, shareDomains...)
	shareDomainsMu.RUnlock()

	results := make([]ShareDomainCheck, len(domains))
	var wg sync.WaitGroup

	for i, domain := range domains {
		wg.Add(1)
		go func(idx int, d string) {
			defer wg.Done()
			baseURL := fmt.Sprintf("https://%s", d)
			skipTLS := false
			dialTarget := d + ":443"
			serverName := d
			if strings.HasPrefix(d, "http://") {
				baseURL = strings.TrimRight(d, "/")
				skipTLS = true
			} else if strings.HasPrefix(d, "https://") {
				baseURL = strings.TrimRight(d, "/")
				if parsed, pErr := url.Parse(baseURL); pErr == nil && parsed.Host != "" {
					serverName = parsed.Hostname()
					if parsed.Port() != "" {
						dialTarget = parsed.Host
					} else {
						dialTarget = serverName + ":443"
					}
				}
			}
			targetURL := fmt.Sprintf("%s/share/file/%s", baseURL, fileName)
			if token != "" {
				targetURL = fmt.Sprintf("%s?token=%s", targetURL, token)
			}
			check := ShareDomainCheck{
				Domain: d,
				URL:    targetURL,
			}

			t0 := time.Now()
			if !skipTLS {
				// 1. 本地 TCP / TLS 握手探活 (端口 443，4s 超时)
				dialer := &tls.Dialer{
					Config: &tls.Config{
						InsecureSkipVerify: true,
						ServerName:         serverName,
					},
				}
				dialCtx, dialCancel := context.WithTimeout(ctx, 4*time.Second)
				conn, err := dialer.DialContext(dialCtx, "tcp", dialTarget)
				dialCancel()

				if err != nil {
					check.Reachable = false
					check.PossibleGFW = true
					check.Error = fmt.Sprintf("TLS/TCP 握手失败 (链接可能被墙或CF节点受阻): %v", err)
					results[idx] = check
					return
				}
				_ = conn.Close()
				check.LatencyMs = time.Since(t0).Milliseconds()
			}
			check.Reachable = true

			// 2. 发起 HTTP HEAD 请求测试分享端点响应 (带 8s 超时)
			httpCtx, httpCancel := context.WithTimeout(ctx, 8*time.Second)
			defer httpCancel()
			req, err := http.NewRequestWithContext(httpCtx, "HEAD", targetURL, nil)
			if err != nil {
				check.Reachable = false
				check.Error = fmt.Sprintf("创建请求失败: %v", err)
				results[idx] = check
				return
			}
			req.Header.Set("User-Agent", "ClashX/1.90.0")

			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			defer tr.CloseIdleConnections()
			client := &http.Client{
				Transport: tr,
				Timeout:   8 * time.Second,
			}
			resp, httpErr := client.Do(req)
			if resp != nil && resp.Body != nil {
				defer resp.Body.Close()
			}

			if httpErr != nil {
				check.Reachable = false
				check.StatusCode = 0
				check.Error = fmt.Sprintf("分享链接 HTTP 请求失败: %v", httpErr)
			} else {
				check.StatusCode = resp.StatusCode
				check.LatencyMs = time.Since(t0).Milliseconds()
				if resp.StatusCode >= 400 {
					check.Reachable = false
					check.Error = fmt.Sprintf("分享链接返回异常状态码 HTTP %d", resp.StatusCode)
				}
			}

			results[idx] = check
		}(i, domain)
	}

	wg.Wait()
	return results
}
