package substore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
)

// Client SubStore REST API 客户端
type Client struct {
	mu         sync.RWMutex
	cfg        *config.SubStoreConfig
	httpClient *http.Client
}

func NewClient(cfg *config.SubStoreConfig) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

// SetHTTPClient 注入自定义 HTTP 客户端 (便于单测与超时控制)
func (c *Client) SetHTTPClient(client *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpClient = client
}

// getHTTPClient 获取当前生效的 HTTP 客户端 (并发读安全)
func (c *Client) getHTTPClient() *http.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: 90 * time.Second}
}

// isLocalOrTestEndpoint 检查 URL 是否为本地环回或测试服务，避免在单测或本地模拟环境下向外部兜底 IP 发起长耗时无效连接
func isLocalOrTestEndpoint(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "127.0.0.1") ||
		strings.Contains(lower, "localhost") ||
		strings.Contains(lower, "::1") ||
		strings.Contains(lower, "example.com")
}

// ResolveFallbackURL 动态计算东京 IIJ (156.231.113.91) 直连兜底端点：
// 优先从 cfg.Token 提取路径，若 Token 为空则尝试从 cfg.URL 的 Path 中提取 (例如 /Woo@@@204317)，
// 确保即使生产环境 Token 置空仅在 URL Path 中携带凭据时，直连兜底端点亦能精准对齐 Sub-Store 后端路由，杜绝 404 故障。
func ResolveFallbackURL(cfg *config.SubStoreConfig) string {
	fallbackBase := "http://156.231.113.91:3001"
	if cfg != nil && cfg.FallbackURL != "" {
		fallbackBase = strings.TrimRight(cfg.FallbackURL, "/")
	}
	if cfg == nil {
		return fallbackBase
	}
	token := strings.Trim(cfg.Token, "/")
	if token == "" && cfg.URL != "" {
		if u, err := url.Parse(cfg.URL); err == nil && u.Path != "" && u.Path != "/" {
			token = strings.Trim(u.Path, "/")
		}
	}
	if token != "" {
		return fmt.Sprintf("%s/%s", fallbackBase, token)
	}
	return fallbackBase
}

// UpdateConfig 动态热更新 SubStore 连接参数 (并发写安全，支持无需重启进程切换地址或账密)
func (c *Client) UpdateConfig(newCfg *config.SubStoreConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = newCfg
}

// GetConfig 获取当前生效的 SubStore 配置 (并发读安全)
func (c *Client) GetConfig() *config.SubStoreConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// GroupUpdateMap 按类别分组的节点链接集合
type GroupUpdateMap map[model.Category][]string

// SyncGroups 将分类好的节点链接更新到远程 SubStore 的指定分组/订阅
func (c *Client) SyncGroups(ctx context.Context, groups GroupUpdateMap) error {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		logger.Debugf("SubStore integration disabled, skipping sync")
		return fmt.Errorf("substore integration is not enabled or URL is empty")
	}

	targetSubLinks := make(map[string][]string)
	for cat, links := range groups {
		if len(links) == 0 {
			continue
		}
		subName := c.getSubscriptionNameForCategory(cat)
		if subName == "" {
			logger.Warnf("[SUBSTORE] ⚠️ Subscription group for category '%s' is not configured; skipping to avoid polluting other groups", cat)
			continue
		}
		targetSubLinks[subName] = append(targetSubLinks[subName], links...)
	}

	if len(targetSubLinks) == 0 {
		return nil
	}

	var errs []string
	logger.Sync("Starting sync to SubStore (%s)...", cfg.URL)

	for subName, links := range targetSubLinks {
		logger.Sync("Syncing %d nodes -> SubStore subscription '%s'", len(links), subName)
		err := c.updateSubStoreSubscription(ctx, subName, links)
		if err != nil {
			errs = append(errs, fmt.Sprintf("[%s]: %v", subName, err))
			logger.Errorf("Failed to sync to SubStore subscription '%s': %v", subName, err)
		} else {
			logger.Sync("Subscription '%s' updated successfully (%d nodes)", subName, len(links))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("substore sync errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (c *Client) getSubscriptionNameForCategory(cat model.Category) string {
	cfg := c.GetConfig()
	if cfg == nil {
		return ""
	}
	switch cat {
	case model.CategorySelfOnline:
		return cfg.GroupSelfOnline
	case model.CategorySelfOffline:
		return cfg.GroupSelfOffline
	case model.CategorySelfOnlineChina:
		// 严禁送中节点静默回退污染正常在线组
		return cfg.GroupSelfOnlineChina
	case model.CategorySelfOnlineClean:
		if cfg.GroupSelfOnlineClean != "" {
			return cfg.GroupSelfOnlineClean
		}
		return cfg.GroupSelfOnline
	case model.CategoryExtOnlineClean:
		return cfg.GroupExtOnlineClean
	case model.CategoryExtOnlineChina:
		return cfg.GroupExtOnlineChina
	default:
		return ""
	}
}

// SubscriptionItem SubStore 单个订阅的概要信息
type SubscriptionItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Source      string `json:"source"`
	Content     string `json:"content"`
	URL         string `json:"url,omitempty"`
}

// GetSubscriptionContent 获取指定订阅的原始节点内容
func (c *Client) GetSubscriptionContent(ctx context.Context, subName string) (string, error) {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return "", fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/subs", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	c.setAuth(req)

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string             `json:"status"`
		Data   []SubscriptionItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	for _, item := range result.Data {
		if item.Name == subName {
			return item.Content, nil
		}
	}

	return "", fmt.Errorf("subscription '%s' not found", subName)
}

// UpdateSubscriptionNodes 更新指定订阅的节点链接
func (c *Client) UpdateSubscriptionNodes(ctx context.Context, subName string, links []string) error {
	return c.updateSubStoreSubscription(ctx, subName, links)
}

// updateSubStoreSubscription 向 SubStore 推送节点内容或更新订阅
func (c *Client) updateSubStoreSubscription(ctx context.Context, subName string, links []string) error {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/sub/%s", baseURL, url.PathEscape(subName))

	content := strings.Join(links, "\n")
	payload := map[string]any{
		"name":    subName,
		"content": content,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var resp *http.Response
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		c.setAuth(req)

		resp, lastErr = c.getHTTPClient().Do(req)
		if lastErr == nil && resp != nil {
			// 若上游返回 5xx 服务端瞬态错误，在 attempt < 2 时主动释放 body 并退避重试
			if resp.StatusCode >= 500 && attempt < 2 {
				status := resp.StatusCode
				resp.Body.Close()
				lastErr = fmt.Errorf("transient server status %d", status)
				logger.Warnf("[SUBSTORE] Update subscription '%s' attempt %d returned %d, retrying in 2s...", subName, attempt, status)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
				continue
			}
			break
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if attempt < 2 {
			logger.Warnf("[SUBSTORE] Update subscription '%s' attempt %d failed: %v, retrying in 2s...", subName, attempt, lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	if lastErr != nil {
		return fmt.Errorf("http request: %w", lastErr)
	}

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		resp.Body.Close()
		// 订阅尚不存在，调用 POST /api/subs 自动创建
		createEndpoint := fmt.Sprintf("%s/api/subs", baseURL)
		createPayload := map[string]any{
			"name":        subName,
			"displayName": subName,
			"source":      "local",
			"content":     content,
		}
		createBytes, _ := json.Marshal(createPayload)
		postReq, postReqErr := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(createBytes))
		if postReqErr != nil {
			return postReqErr
		}
		postReq.Header.Set("Content-Type", "application/json")
		c.setAuth(postReq)
		postResp, postErr := c.getHTTPClient().Do(postReq)
		if postErr == nil {
			defer postResp.Body.Close()
			if postResp.StatusCode >= 200 && postResp.StatusCode < 300 {
				return nil
			}
			postBody, _ := io.ReadAll(io.LimitReader(postResp.Body, 1024*1024))
			return fmt.Errorf("subscription patch %d, fallback create returned %d: %s", resp.StatusCode, postResp.StatusCode, string(postBody))
		}
		return fmt.Errorf("subscription patch %d, fallback create failed: %w", resp.StatusCode, postErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// UpsertRemoteSubscription 在 SubStore 中自动创建或更新外部远程订阅 (Issue #19)
func (c *Client) UpsertRemoteSubscription(ctx context.Context, name, displayName, subURL string) error {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return fmt.Errorf("substore is not configured or disabled")
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/sub/%s", baseURL, url.PathEscape(name))

	if displayName == "" {
		displayName = name
	}

	payload := map[string]any{
		"name":        name,
		"displayName": displayName,
		"source":      "remote",
		"url":         subURL,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// 1. 尝试 PATCH 更新现有订阅
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.getHTTPClient().Do(req)
	if err == nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			logger.Sync("[SUBSTORE] Remote subscription '%s' updated successfully", name)
			return nil
		}
		resp.Body.Close()
	} else if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// 2. 若不存在，调用 POST /api/subs 新建
	createEndpoint := fmt.Sprintf("%s/api/subs", baseURL)
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/json")
	c.setAuth(postReq)

	postResp, err := c.getHTTPClient().Do(postReq)
	if err != nil {
		return fmt.Errorf("create remote subscription http error: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode >= 200 && postResp.StatusCode < 300 {
		logger.Sync("[SUBSTORE] Remote subscription '%s' created successfully", name)
		return nil
	}

	body, _ := io.ReadAll(postResp.Body)
	return fmt.Errorf("create remote subscription returned %d: %s", postResp.StatusCode, string(body))
}

// TriggerDownload 触发 SubStore 重新拉取/刷新
func (c *Client) TriggerDownload(ctx context.Context, subName string) error {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	reqURL := fmt.Sprintf("%s/download/%s", baseURL, url.PathEscape(subName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// SyncFiveGroups 方案B：原子同步自建五维策略组（线路机、美区全家桶、日区全家桶、其他、送中）与总集合
func (c *Client) SyncFiveGroups(ctx context.Context, payload model.FiveGroupPayload) error {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return fmt.Errorf("substore is not enabled")
	}

	logger.Sync("[SUBSTORE] 🚀 Starting 5-group subscription sync (Plan B)...")

	groups := []struct {
		SubName     string
		DisplayName string
		Nodes       []string
	}{
		{"ashan-transit", "自建-线路机", payload.TransitNodes},
		{"ashan-us-resi", "自建-美国家宽", payload.USResiNodes},
		{"ashan-jp-resi", "自建-日本家宽", payload.JPResiNodes},
		{"ashan-other", "自建-其他", payload.OtherNodes},
		{"ashan-china-leak", "自建-送中", payload.ChinaLeakNodes},
	}

	var errs []string
	for _, g := range groups {
		content := g.Nodes
		if len(content) == 0 {
			content = []string{}
		}
		if err := c.updateSubStoreSubscription(ctx, g.SubName, content); err != nil {
			errs = append(errs, fmt.Sprintf("update %s: %v", g.SubName, err))
			logger.Warnf("[SUBSTORE] Update %s (%s): %v", g.SubName, g.DisplayName, err)
		} else {
			logger.Sync("[SUBSTORE] ✅ '%s' (%s) updated with %d nodes", g.SubName, g.DisplayName, len(g.Nodes))
		}
	}

	masterSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other", "ashan-china-leak"}
	extSources := c.GetExternalSources()
	if len(extSources) == 0 {
		extSources = []string{"蜂窝云"}
	}
	masterSubs = deduplicate(append(masterSubs, extSources...))

	// 1. 同步更新 ashan-master 总合集
	masterPayload := map[string]any{
		"name":                  "ashan-master",
		"displayName":           "Ashan 总合集 (自建五维策略全景+外部机场)",
		"subscriptions":         masterSubs,
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, "ashan-master", masterPayload); err != nil {
		errs = append(errs, fmt.Sprintf("reconcile ashan-master: %v", err))
		logger.Warnf("[SUBSTORE] Reconcile ashan-master collection: %v", err)
	}

	// 2. 同步更新 ashan-纯净 集合 (排除送中，仅包含前 4 个干净组)
	cleanFourSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other"}
	purePayload := map[string]any{
		"name":                  "ashan-纯净",
		"displayName":           "Ashan 纯自建专线 (全量干净组)",
		"subscriptions":         cleanFourSubs,
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, "ashan-纯净", purePayload); err != nil {
		errs = append(errs, fmt.Sprintf("reconcile ashan-纯净: %v", err))
		logger.Warnf("[SUBSTORE] Reconcile ashan-纯净 collection: %v", err)
	}

	// 3. 同步更新 col-business 集合 (纯自建全量组：线路+家宽+送中隔离，0 外部机场)
	bizSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other", "ashan-china-leak"}
	bizPayload := map[string]any{
		"name":                  "col-business",
		"displayName":           "Business 纯自建商务合集 (0 外部机场)",
		"subscriptions":         bizSubs,
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, "col-business", bizPayload); err != nil {
		errs = append(errs, fmt.Sprintf("reconcile col-business: %v", err))
		logger.Warnf("[SUBSTORE] Reconcile col-business collection: %v", err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("substore 5-group sync errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ThreeTierPayload 三层同步负载
type ThreeTierPayload struct {
	TransitDirectNodes []string // 线路机直连 (已按测速与评分置顶排序)
	ResidentialNodes   []string // 懒猫云家宽直连
	ChainNodes         []string // 线路➔家宽链式组合
}

// SyncThreeTier 一键原子同步线路直连、家宽直连、链式代理三层订阅与总集合
func (c *Client) SyncThreeTier(ctx context.Context, payload ThreeTierPayload) error {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return fmt.Errorf("substore is not enabled")
	}

	logger.Sync("[SUBSTORE] Starting 3-tier subscription sync...")

	var errs []string
	// 1. 同步 线路机-直连 (ashan-transit-direct)
	transitContent := payload.TransitDirectNodes
	if len(transitContent) == 0 {
		transitContent = []string{}
	}
	if err := c.updateSubStoreSubscription(ctx, "ashan-transit-direct", transitContent); err != nil {
		errs = append(errs, fmt.Sprintf("update ashan-transit-direct: %v", err))
		logger.Warnf("[SUBSTORE] Update ashan-transit-direct: %v", err)
	} else {
		logger.Sync("[SUBSTORE] ✅ 'ashan-transit-direct' updated with %d transit nodes", len(payload.TransitDirectNodes))
	}

	// 2. 同步 家宽机-直连 (ashan-resi-direct)
	resiContent := payload.ResidentialNodes
	if len(resiContent) == 0 {
		resiContent = []string{}
	}
	if err := c.updateSubStoreSubscription(ctx, "ashan-resi-direct", resiContent); err != nil {
		errs = append(errs, fmt.Sprintf("update ashan-resi-direct: %v", err))
		logger.Warnf("[SUBSTORE] Update ashan-resi-direct: %v", err)
	} else {
		logger.Sync("[SUBSTORE] ✅ 'ashan-resi-direct' updated with %d residential nodes", len(payload.ResidentialNodes))
	}

	// 3. 同步 线路➔家宽链式代理 (ashan-chain)
	chainContent := payload.ChainNodes
	if len(chainContent) == 0 {
		chainContent = []string{}
	}
	if err := c.updateSubStoreSubscription(ctx, "ashan-chain", chainContent); err != nil {
		errs = append(errs, fmt.Sprintf("update ashan-chain: %v", err))
		logger.Warnf("[SUBSTORE] Update ashan-chain: %v", err)
	} else {
		logger.Sync("[SUBSTORE] ✅ 'ashan-chain' updated with %d chain nodes", len(payload.ChainNodes))
	}

	// 4. 确保总集合 ashan-master 存在并包含自建分组与外部机场
	masterSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other", "ashan-china-leak"}
	extSources := c.GetExternalSources()
	if len(extSources) == 0 {
		extSources = []string{"蜂窝云"}
	}
	masterSubs = deduplicate(append(masterSubs, extSources...))
	masterPayload := map[string]any{
		"name":                  "ashan-master",
		"displayName":           "Ashan 总合集 (自建五维策略全景+外部机场)",
		"subscriptions":         masterSubs,
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, "ashan-master", masterPayload); err != nil {
		errs = append(errs, fmt.Sprintf("reconcile ashan-master: %v", err))
		logger.Warnf("[SUBSTORE] Ensure ashan-master collection: %v", err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("substore 3-tier sync errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// CreateOrUpdateRemoteSub 在远程 SubStore 中创建或更新一个外部远程机场订阅 (Issue #19)
func (c *Client) CreateOrUpdateRemoteSub(ctx context.Context, subName, displayName, remoteURL string) error {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return fmt.Errorf("substore integration is not enabled or URL is empty")
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/sub/%s", baseURL, url.PathEscape(subName))

	if displayName == "" {
		displayName = subName
	}

	payload := map[string]any{
		"name":        subName,
		"displayName": displayName,
		"source":      "remote",
		"url":         remoteURL,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// 1. 优先尝试 PATCH 更新现有订阅
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.getHTTPClient().Do(req)
	if err == nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			logger.Sync("[SUBSTORE] ✅ Remote subscription '%s' patched successfully", subName)
			return nil
		}
		resp.Body.Close()
	} else if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// 2. 若更新返回 404/405 或失败，调用 POST /api/subs 自动创建
	createEndpoint := fmt.Sprintf("%s/api/subs", baseURL)
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/json")
	c.setAuth(postReq)

	postResp, postErr := c.getHTTPClient().Do(postReq)
	if postErr != nil {
		return fmt.Errorf("create remote sub http request: %w", postErr)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode < 200 || postResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(postResp.Body)
		return fmt.Errorf("create remote sub failed with status %d: %s", postResp.StatusCode, string(respBody))
	}

	logger.Sync("[SUBSTORE] ✅ Remote subscription '%s' created successfully in SubStore", subName)
	return nil
}

// isSubStoreNotFound 检查 SubStore 的 HTTP 响应是否表示资源不存在 (实现幂等删除)
func isSubStoreNotFound(statusCode int, body []byte) bool {
	if statusCode == http.StatusNotFound {
		return true
	}
	// Sub-Store 后端对于不存在的资源，经常返回 HTTP 500 并在 JSON 中标注 ResourceNotFoundError
	// 例如: {"status":"failed","error":{"code":"RESOURCE_NOT_FOUND","type":"ResourceNotFoundError","message":"Subscription xxx does not exist!"}}
	if statusCode >= 400 && len(body) > 0 {
		bodyStr := strings.ToLower(string(body))
		if strings.Contains(bodyStr, "resource_not_found") ||
			strings.Contains(bodyStr, "resourcenotfounderror") ||
			strings.Contains(bodyStr, "does not exist") ||
			strings.Contains(bodyStr, "not found") {
			return true
		}
	}
	return false
}

// DeleteSubscription 从 SubStore 彻底删除一个订阅 (具备 404 与 500 ResourceNotFoundError 幂等性)
func (c *Client) DeleteSubscription(ctx context.Context, subName string) error {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return fmt.Errorf("substore integration is not enabled or URL is empty")
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/sub/%s", baseURL, url.PathEscape(subName))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("delete subscription http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		if isSubStoreNotFound(resp.StatusCode, respBody) {
			logger.Sync("[SUBSTORE] 🗑️ Subscription '%s' already absent from SubStore (idempotent)", subName)
			return nil
		}
		return fmt.Errorf("delete subscription failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	logger.Sync("[SUBSTORE] 🗑️ Subscription '%s' deleted from SubStore", subName)
	return nil
}

// LegacyThreeTierSubNames 历史残留的三层架构与多余独立订阅名
var LegacyThreeTierSubNames = []string{
	"ashan-transit-direct",
	"ashan-resi-direct",
	"ashan-chain",
	"ashan-tw-resi",
	"ashan-residential",
	"ashan-clean",
}

// PurgeLegacyThreeTierSubs 从 SubStore 彻底平退并删除历史残留的三层订阅 (Issue/Task 备忘闭环)
func (c *Client) PurgeLegacyThreeTierSubs(ctx context.Context) (int, error) {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return 0, fmt.Errorf("substore integration is not enabled or URL is empty")
	}

	purgedCount := 0
	var lastErr error
	for _, name := range LegacyThreeTierSubNames {
		if err := c.DeleteSubscription(ctx, name); err != nil {
			logger.Warnf("[SUBSTORE] Purge legacy subscription '%s': %v", name, err)
			lastErr = err
		} else {
			purgedCount++
			logger.Sync("[SUBSTORE] 🗑️ Legacy subscription '%s' purged or confirmed absent from SubStore", name)
		}
	}
	return purgedCount, lastErr
}
