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
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
)

// Client SubStore REST API 客户端
type Client struct {
	cfg        *config.SubStoreConfig
	httpClient *http.Client
}

func NewClient(cfg *config.SubStoreConfig) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// UpdateConfig 动态热更新 SubStore 连接参数 (支持无需重启进程切换地址或账密)
func (c *Client) UpdateConfig(newCfg *config.SubStoreConfig) {
	c.cfg = newCfg
}

// GetConfig 获取当前生效的 SubStore 配置
func (c *Client) GetConfig() *config.SubStoreConfig {
	return c.cfg
}

// GroupUpdateMap 按类别分组的节点链接集合
type GroupUpdateMap map[model.Category][]string

// SyncGroups 将分类好的节点链接更新到远程 SubStore 的指定分组/订阅
func (c *Client) SyncGroups(ctx context.Context, groups GroupUpdateMap) error {
	if !c.cfg.Enabled || c.cfg.URL == "" {
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
	logger.Sync("Starting sync to SubStore (%s)...", c.cfg.URL)

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
	switch cat {
	case model.CategorySelfOnline:
		return c.cfg.GroupSelfOnline
	case model.CategorySelfOffline:
		return c.cfg.GroupSelfOffline
	case model.CategorySelfOnlineChina:
		// 严禁送中节点静默回退污染正常在线组
		return c.cfg.GroupSelfOnlineChina
	case model.CategorySelfOnlineClean:
		if c.cfg.GroupSelfOnlineClean != "" {
			return c.cfg.GroupSelfOnlineClean
		}
		return c.cfg.GroupSelfOnline
	case model.CategoryExtOnlineClean:
		return c.cfg.GroupExtOnlineClean
	case model.CategoryExtOnlineChina:
		return c.cfg.GroupExtOnlineChina
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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/subs", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
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

		resp, lastErr = c.httpClient.Do(req)
		if lastErr == nil {
			break
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
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		// 订阅尚不存在，调用 POST /api/subs 自动创建
		createEndpoint := fmt.Sprintf("%s/api/subs", baseURL)
		createPayload := map[string]any{
			"name":        subName,
			"displayName": subName,
			"source":      "local",
			"content":     content,
		}
		createBytes, _ := json.Marshal(createPayload)
		postReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(createBytes))
		postReq.Header.Set("Content-Type", "application/json")
		c.setAuth(postReq)
		postResp, postErr := c.httpClient.Do(postReq)
		if postErr == nil {
			defer postResp.Body.Close()
			if postResp.StatusCode >= 200 && postResp.StatusCode < 300 {
				return nil
			}
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// UpsertRemoteSubscription 在 SubStore 中自动创建或更新外部远程订阅 (Issue #19)
func (c *Client) UpsertRemoteSubscription(ctx context.Context, name, displayName, subURL string) error {
	if c.cfg == nil || !c.cfg.Enabled || c.cfg.URL == "" {
		return fmt.Errorf("substore is not configured or disabled")
	}

	baseURL := strings.TrimRight(c.cfg.URL, "/")
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

	resp, err := c.httpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Sync("[SUBSTORE] Remote subscription '%s' updated successfully", name)
			return nil
		}
	}

	// 2. 若不存在，调用 POST /api/subs 新建
	createEndpoint := fmt.Sprintf("%s/api/subs", baseURL)
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/json")
	c.setAuth(postReq)

	postResp, err := c.httpClient.Do(postReq)
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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
	reqURL := fmt.Sprintf("%s/download/%s", baseURL, url.PathEscape(subName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// SyncFiveGroups 方案B：原子同步自建五维策略组（线路机、美区全家桶、日区全家桶、其他、送中）与总集合
func (c *Client) SyncFiveGroups(ctx context.Context, payload model.FiveGroupPayload) error {
	if !c.cfg.Enabled || c.cfg.URL == "" {
		return fmt.Errorf("substore is not enabled")
	}

	logger.Sync("[SUBSTORE] 🚀 Starting 5-group subscription sync (Plan B)...")

	groups := []struct {
		SubName     string
		DisplayName string
		Nodes       []string
	}{
		{"ashan-transit", "自建-线路机", payload.TransitNodes},
		{"ashan-us-resi", "自建-美国和美国家宽", payload.USResiNodes},
		{"ashan-jp-resi", "自建-日本和日本家宽", payload.JPResiNodes},
		{"ashan-other", "自建-其他", payload.OtherNodes},
		{"ashan-china-leak", "自建-送中", payload.ChinaLeakNodes},
	}

	for _, g := range groups {
		content := g.Nodes
		if len(content) == 0 {
			content = []string{fmt.Sprintf("# No active nodes for %s", g.DisplayName)}
		}
		if err := c.updateSubStoreSubscription(ctx, g.SubName, content); err != nil {
			logger.Warnf("[SUBSTORE] Update %s (%s): %v", g.SubName, g.DisplayName, err)
		} else {
			logger.Sync("[SUBSTORE] ✅ '%s' (%s) updated with %d nodes", g.SubName, g.DisplayName, len(g.Nodes))
		}
	}

	fiveSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other", "ashan-china-leak"}

	// 1. 同步更新 ashan-master 总合集
	masterPayload := map[string]any{
		"name":                  "ashan-master",
		"displayName":           "Ashan 总合集 (自建五维策略全景)",
		"subscriptions":         fiveSubs,
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, "ashan-master", masterPayload); err != nil {
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
		logger.Warnf("[SUBSTORE] Reconcile ashan-纯净 collection: %v", err)
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
	if !c.cfg.Enabled || c.cfg.URL == "" {
		return fmt.Errorf("substore is not enabled")
	}

	logger.Sync("[SUBSTORE] Starting 3-tier subscription sync...")

	// 1. 同步 线路机-直连 (ashan-transit-direct)
	transitContent := payload.TransitDirectNodes
	if len(transitContent) == 0 {
		transitContent = []string{"# No active transit nodes"}
	}
	if err := c.updateSubStoreSubscription(ctx, "ashan-transit-direct", transitContent); err != nil {
		logger.Warnf("[SUBSTORE] Update ashan-transit-direct: %v", err)
	} else {
		logger.Sync("[SUBSTORE] ✅ 'ashan-transit-direct' updated with %d transit nodes", len(payload.TransitDirectNodes))
	}

	// 2. 同步 家宽机-直连 (ashan-resi-direct)
	resiContent := payload.ResidentialNodes
	if len(resiContent) == 0 {
		resiContent = []string{""}
	}
	if err := c.updateSubStoreSubscription(ctx, "ashan-resi-direct", resiContent); err != nil {
		logger.Warnf("[SUBSTORE] Update ashan-resi-direct: %v", err)
	} else {
		logger.Sync("[SUBSTORE] ✅ 'ashan-resi-direct' updated with %d residential nodes", len(payload.ResidentialNodes))
	}

	// 3. 同步 线路➔家宽链式代理 (ashan-chain)
	chainContent := payload.ChainNodes
	if len(chainContent) == 0 {
		chainContent = []string{""}
	}
	if err := c.updateSubStoreSubscription(ctx, "ashan-chain", chainContent); err != nil {
		logger.Warnf("[SUBSTORE] Update ashan-chain: %v", err)
	} else {
		logger.Sync("[SUBSTORE] ✅ 'ashan-chain' updated with %d chain nodes", len(payload.ChainNodes))
	}

	// 4. 确保总集合 ashan-master 存在并包含 3 个订阅
	masterPayload := map[string]any{
		"name":                  "ashan-master",
		"displayName":           "Ashan 总合集 (线路直连+家宽直连+链式代理)",
		"subscriptions":         []string{"ashan-transit-direct", "ashan-resi-direct", "ashan-chain"},
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, "ashan-master", masterPayload); err != nil {
		logger.Warnf("[SUBSTORE] Ensure ashan-master collection: %v", err)
	}

	// 6. 同步更新 ashan-纯净 集合，使其亦涵盖 3 个自建三层订阅
	purePayload := map[string]any{
		"name":                  "ashan-纯净",
		"displayName":           "Ashan 纯自建VPS (全量三层融合)",
		"subscriptions":         []string{"ashan-transit-direct", "ashan-resi-direct", "ashan-chain"},
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, "ashan-纯净", purePayload); err != nil {
		logger.Warnf("[SUBSTORE] Reconcile ashan-纯净 collection: %v", err)
	}

	return nil
}

// CreateOrUpdateRemoteSub 在远程 SubStore 中创建或更新一个外部远程机场订阅 (Issue #19)
func (c *Client) CreateOrUpdateRemoteSub(ctx context.Context, subName, displayName, remoteURL string) error {
	if !c.cfg.Enabled || c.cfg.URL == "" {
		return fmt.Errorf("substore integration is not enabled or URL is empty")
	}

	baseURL := strings.TrimRight(c.cfg.URL, "/")
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

	resp, err := c.httpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Sync("[SUBSTORE] ✅ Remote subscription '%s' patched successfully", subName)
			return nil
		}
	}

	// 2. 若更新返回 404/405 或失败，调用 POST /api/subs 自动创建
	createEndpoint := fmt.Sprintf("%s/api/subs", baseURL)
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/json")
	c.setAuth(postReq)

	postResp, postErr := c.httpClient.Do(postReq)
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

// DeleteSubscription 从 SubStore 彻底删除一个订阅
func (c *Client) DeleteSubscription(ctx context.Context, subName string) error {
	if !c.cfg.Enabled || c.cfg.URL == "" {
		return fmt.Errorf("substore integration is not enabled or URL is empty")
	}

	baseURL := strings.TrimRight(c.cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/sub/%s", baseURL, url.PathEscape(subName))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete subscription http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != 404 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete subscription failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	logger.Sync("[SUBSTORE] 🗑️ Subscription '%s' deleted from SubStore", subName)
	return nil
}
