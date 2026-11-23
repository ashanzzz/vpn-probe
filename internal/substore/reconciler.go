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

	"vpn-probe/internal/logger"
)

// CollectionItem SubStore 集合定义
type CollectionItem struct {
	Name                  string           `json:"name"`
	DisplayName           string           `json:"displayName"`
	Subscriptions         []string         `json:"subscriptions"`
	Process               []map[string]any `json:"process,omitempty"`
	IgnoreFailedRemoteSub string           `json:"ignoreFailedRemoteSub,omitempty"`
}

// FileItem SubStore 输出文件定义
type FileItem struct {
	Name                    string           `json:"name"`
	DisplayName             string           `json:"displayName"`
	Source                  string           `json:"source"`
	SourceType              string           `json:"sourceType"`
	SourceName              string           `json:"sourceName"`
	Mode                    string           `json:"mode"`
	Type                    string           `json:"type"`
	IncludeUnsupportedProxy bool             `json:"includeUnsupportedProxy"`
	Process                 []map[string]any `json:"process,omitempty"`
}

// TopologyAudit 拓扑审计与自愈报告
type TopologyAudit struct {
	Healthy          bool                `json:"healthy"`
	Subscriptions    []string            `json:"subscriptions"`
	Collections      map[string][]string `json:"collections"`
	Files            map[string]string   `json:"files"`
	Discrepancies    []string            `json:"discrepancies"`
	RecommendedFixes []string            `json:"recommended_fixes"`
	ActionsTaken     []string            `json:"actions_taken,omitempty"`
}

// GetSubscriptions 获取所有订阅列表
func (c *Client) GetSubscriptions(ctx context.Context) ([]SubscriptionItem, error) {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/subs", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string             `json:"status"`
		Data   []SubscriptionItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetCollections 获取所有集合定义
func (c *Client) GetCollections(ctx context.Context) ([]CollectionItem, error) {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/collections", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string           `json:"status"`
		Data   []CollectionItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetFiles 获取所有文件输出配置
func (c *Client) GetFiles(ctx context.Context) ([]FileItem, error) {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/files", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string     `json:"status"`
		Data   []FileItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// UpdateCollection 更新指定集合的配置
func (c *Client) UpdateCollection(ctx context.Context, name string, payload map[string]any) error {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/collection/%s", baseURL, url.PathEscape(name))

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
			logger.Warnf("[SUBSTORE] UpdateCollection '%s' attempt %d failed: %v, retrying in 2s...", name, attempt, lastErr)
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
		// 集合不存在，调用 POST /api/collections 创建
		createEndpoint := fmt.Sprintf("%s/api/collections", baseURL)
		postReq, postReqErr := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
		if postReqErr != nil {
			return postReqErr
		}
		postReq.Header.Set("Content-Type", "application/json")
		c.setAuth(postReq)
		postResp, postErr := c.httpClient.Do(postReq)
		if postErr == nil {
			defer postResp.Body.Close()
			if postResp.StatusCode >= 200 && postResp.StatusCode < 300 {
				return nil
			}
			postBody, _ := io.ReadAll(postResp.Body)
			return fmt.Errorf("collection patch %d, fallback create returned %d: %s", resp.StatusCode, postResp.StatusCode, string(postBody))
		}
		return fmt.Errorf("collection patch %d, fallback create failed: %w", resp.StatusCode, postErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// UpdateFile 更新指定输出文件的配置
func (c *Client) UpdateFile(ctx context.Context, name string, payload map[string]any) error {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/file/%s", baseURL, url.PathEscape(name))

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
			logger.Warnf("[SUBSTORE] UpdateFile '%s' attempt %d failed: %v, retrying in 2s...", name, attempt, lastErr)
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
		// 文件配置不存在，调用 POST /api/files 创建
		createEndpoint := fmt.Sprintf("%s/api/files", baseURL)
		postReq, postReqErr := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
		if postReqErr != nil {
			return postReqErr
		}
		postReq.Header.Set("Content-Type", "application/json")
		c.setAuth(postReq)
		postResp, postErr := c.httpClient.Do(postReq)
		if postErr == nil {
			defer postResp.Body.Close()
			if postResp.StatusCode >= 200 && postResp.StatusCode < 300 {
				return nil
			}
			postBody, _ := io.ReadAll(postResp.Body)
			return fmt.Errorf("file patch %d, fallback create returned %d: %s", resp.StatusCode, postResp.StatusCode, string(postBody))
		}
		return fmt.Errorf("file patch %d, fallback create failed: %w", resp.StatusCode, postErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// AuditTopology 审计当前 SubStore 的订阅、集合与文件引用拓扑，诊断错配与孤儿节点
func (c *Client) AuditTopology(ctx context.Context) (*TopologyAudit, error) {
	var (
		subs     []SubscriptionItem
		cols     []CollectionItem
		files    []FileItem
		subsErr  error
		colsErr  error
		filesErr error
		wg       sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		subs, subsErr = c.GetSubscriptions(ctx)
	}()
	go func() {
		defer wg.Done()
		cols, colsErr = c.GetCollections(ctx)
	}()
	go func() {
		defer wg.Done()
		files, filesErr = c.GetFiles(ctx)
	}()
	wg.Wait()

	if subsErr != nil {
		return nil, fmt.Errorf("fetch subscriptions: %w", subsErr)
	}
	if colsErr != nil {
		return nil, fmt.Errorf("fetch collections: %w", colsErr)
	}
	if filesErr != nil {
		return nil, fmt.Errorf("fetch files: %w", filesErr)
	}

	audit := &TopologyAudit{
		Healthy:          true,
		Subscriptions:    make([]string, 0),
		Collections:      make(map[string][]string),
		Files:            make(map[string]string),
		Discrepancies:    make([]string, 0),
		RecommendedFixes: make([]string, 0),
	}

	subSet := make(map[string]bool)
	referencedSubs := make(map[string]bool)

	for _, s := range subs {
		audit.Subscriptions = append(audit.Subscriptions, s.Name)
		subSet[s.Name] = true
	}

	for _, col := range cols {
		audit.Collections[col.Name] = col.Subscriptions
		for _, s := range col.Subscriptions {
			referencedSubs[s] = true
		}
	}

	for _, f := range files {
		audit.Files[f.Name] = f.SourceName
	}

	// 1. 检查 ashan-纯净 是否名实相符
	if ashanPure, exists := audit.Collections["ashan-纯净"]; exists {
		hasAirport := false
		for _, s := range ashanPure {
			if s == "蜂窝云" {
				hasAirport = true
			}
		}
		if hasAirport {
			audit.Healthy = false
			msg := "集合 'ashan-纯净' (标称纯自建VPS) 混入了外部机场订阅 '蜂窝云'，破坏了纯净性"
			audit.Discrepancies = append(audit.Discrepancies, msg)
			audit.RecommendedFixes = append(audit.RecommendedFixes, "将 'ashan-纯净' 净化为仅引用自建纯净分组 [\"ashan-transit\", \"ashan-us-resi\", \"ashan-jp-resi\", \"ashan-other\"]")
		}
	}

	// 2. 检查 Q-汇总 是否缺少自建 VPS / Q 订阅
	if qSummary, exists := audit.Collections["Q-汇总"]; exists {
		hasSelfHosted := false
		for _, s := range qSummary {
			if s == "Q" || s == "ashan-transit" || strings.HasPrefix(s, "ashan-") {
				hasSelfHosted = true
				break
			}
		}
		if !hasSelfHosted {
			audit.Healthy = false
			msg := "集合 'Q-汇总' (标称蜂窝云+自建VPS) 未包含自建节点订阅，导致自建节点在组合输出中完全缺席"
			audit.Discrepancies = append(audit.Discrepancies, msg)
			audit.RecommendedFixes = append(audit.RecommendedFixes, "为 'Q-汇总' 补充勾选自建分组，形成双轨融合")
		}
	}

	// 3. 检查孤儿订阅
	for _, s := range audit.Subscriptions {
		if !referencedSubs[s] {
			audit.Healthy = false
			msg := fmt.Sprintf("订阅 '%s' 处于孤儿状态，未被任何集合引用 (导致其内部节点无法被任何下游分享产物使用)", s)
			audit.Discrepancies = append(audit.Discrepancies, msg)
			audit.RecommendedFixes = append(audit.RecommendedFixes, fmt.Sprintf("将孤儿订阅 '%s' 纳入对应集合或在无需使用时清理", s))
		}
	}

	// 4. 检查输出文件绑定是否规范
	for _, f := range files {
		if f.Name == "ashan" || f.Name == "Ashan" {
			if f.SourceName != "ashan-纯净" {
				audit.Healthy = false
				audit.Discrepancies = append(audit.Discrepancies, fmt.Sprintf("文件 '%s' 未绑定至集合 'ashan-纯净'", f.Name))
			}
		} else if f.Name == "Q" || f.Name == "q" {
			if f.SourceName != "Q-汇总" {
				audit.Healthy = false
				audit.Discrepancies = append(audit.Discrepancies, fmt.Sprintf("文件 '%s' 未绑定至集合 'Q-汇总'", f.Name))
			}
		}
	}

	return audit, nil
}

// ReconcileTopology 执行自愈整理：按标准高可用拓扑自动重构 SubStore 集合与文件绑定关系
func (c *Client) ReconcileTopology(ctx context.Context) (*TopologyAudit, error) {
	logger.Sync("Starting autonomous SubStore topology reconciliation...")

	audit, err := c.AuditTopology(ctx)
	if err != nil {
		return nil, err
	}

	actions := make([]string, 0)

	// 1. 修复 ashan-纯净：纯净自建
	if ashanPure, exists := audit.Collections["ashan-纯净"]; exists {
		hasAirport := false
		for _, s := range ashanPure {
			if s == "蜂窝云" {
				hasAirport = true
			}
		}
		if hasAirport {
			payload := map[string]any{
				"name":          "ashan-纯净",
				"displayName":   "Ashan 纯自建专线 (全量干净组)",
				"subscriptions": []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other"},
			}
			if err := c.UpdateCollection(ctx, "ashan-纯净", payload); err != nil {
				return nil, fmt.Errorf("reconcile ashan-纯净: %w", err)
			}
			actions = append(actions, "已将集合 'ashan-纯净' 净化为自建纯净分组 [\"ashan-transit\", \"ashan-us-resi\", \"ashan-jp-resi\", \"ashan-other\"]，彻底剔除机场节点混入")
			logger.Sync("Reconciled collection 'ashan-纯净'")
		}
	}

	// 2. 修复 Q-汇总：融合自建与蜂窝云全量订阅
	if qSummary, exists := audit.Collections["Q-汇总"]; exists {
		hasCloud := false
		hasTransit := false
		hasLegacyQ := false
		for _, s := range qSummary {
			if s == "蜂窝云" {
				hasCloud = true
			}
			if s == "ashan-transit" {
				hasTransit = true
			}
			if s == "Q" {
				hasLegacyQ = true
			}
		}
		qSubExists := false
		for _, s := range audit.Subscriptions {
			if s == "Q" {
				qSubExists = true
				break
			}
		}
		needsReconcile := !hasTransit || !hasCloud || (qSubExists && !hasLegacyQ)
		if needsReconcile {
			qSubs := []string{"ashan-transit", "ashan-us-resi", "ashan-jp-resi", "ashan-other", "ashan-china-leak"}
			if qSubExists {
				qSubs = append(qSubs, "Q")
			}
			qSubs = append(qSubs, "蜂窝云")
			payload := map[string]any{
				"name":                  "Q-汇总",
				"displayName":           "Q 组合订阅 (蜂窝云 + 自建VPS)",
				"subscriptions":         qSubs,
				"ignoreFailedRemoteSub": "quiet",
			}
			if err := c.UpdateCollection(ctx, "Q-汇总", payload); err != nil {
				return nil, fmt.Errorf("reconcile Q-汇总: %w", err)
			}
			actions = append(actions, "已为集合 'Q-汇总' 补齐 [自建五维 + 蜂窝云]，激活全量自建家宽与中继节点")
			logger.Sync("Reconciled collection 'Q-汇总' -> 全量自建+蜂窝云")
		}
	}

	// 3. 修复输出文件绑定：确保 ashan/Ashan 绑定 ashan-纯净，Q/q 绑定 Q-汇总
	for fName, sourceName := range audit.Files {
		if fName == "ashan" || fName == "Ashan" {
			if sourceName != "ashan-纯净" {
				payload := map[string]any{
					"source":     "local",
					"sourceType": "collection",
					"sourceName": "ashan-纯净",
				}
				if err := c.UpdateFile(ctx, fName, payload); err != nil {
					return nil, fmt.Errorf("reconcile file %s: %w", fName, err)
				}
				actions = append(actions, fmt.Sprintf("已将纯自建文件 '%s' 重新绑定至集合 'ashan-纯净'，恢复纯自建纯净性", fName))
				logger.Sync("Reconciled file '%s' -> source 'ashan-纯净'", fName)
			}
		} else if fName == "Q" || fName == "q" {
			if sourceName != "Q-汇总" {
				payload := map[string]any{
					"source":     "local",
					"sourceType": "collection",
					"sourceName": "Q-汇总",
				}
				if err := c.UpdateFile(ctx, fName, payload); err != nil {
					return nil, fmt.Errorf("reconcile file %s: %w", fName, err)
				}
				actions = append(actions, fmt.Sprintf("已将组合订阅文件 '%s' 重新绑定至集合 'Q-汇总'", fName))
				logger.Sync("Reconciled file '%s' -> source 'Q-汇总'", fName)
			}
		}
	}

	// 4. 再次执行复查，生成最终报告
	finalAudit, err := c.AuditTopology(ctx)
	if err != nil {
		return nil, err
	}
	finalAudit.ActionsTaken = actions
	return finalAudit, nil
}

// DeleteItem 删除指定类型的 SubStore 项目 (subs, collections, files)
func (c *Client) DeleteItem(ctx context.Context, itemType, name string) error {
	if itemType != "subs" && itemType != "collections" && itemType != "files" {
		return fmt.Errorf("invalid item type '%s', must be subs, collections, or files", itemType)
	}
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	// SubStore 单个资源路径通常为 /api/sub/:name, /api/collection/:name, /api/file/:name
	singleType := itemType
	switch itemType {
	case "subs":
		singleType = "sub"
	case "collections":
		singleType = "collection"
	case "files":
		singleType = "file"
	}

	endpoint := fmt.Sprintf("%s/api/%s/%s", baseURL, singleType, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		if isSubStoreNotFound(resp.StatusCode, body) {
			logger.Sync("[SUBSTORE] 🗑️ %s '%s' already absent from SubStore (idempotent)", itemType, name)
			return nil
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ToggleFilePipelineOperator 启停指定输出文件内部的某个脚本算子
func (c *Client) ToggleFilePipelineOperator(ctx context.Context, fileName, operatorID, scriptFile string, disabled bool) error {
	files, err := c.GetFiles(ctx)
	if err != nil {
		return fmt.Errorf("fetch files: %w", err)
	}

	var target *FileItem
	for i := range files {
		if files[i].Name == fileName {
			target = &files[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("file '%s' not found in SubStore", fileName)
	}

	modified := false
	for _, p := range target.Process {
		id, _ := p["id"].(string)
		urlVal, _ := p["url"].(string)
		customName, _ := p["customName"].(string)

		match := false
		if operatorID != "" && id == operatorID {
			match = true
		} else if scriptFile != "" && (strings.Contains(urlVal, scriptFile) || strings.Contains(customName, scriptFile)) {
			match = true
		}

		if match {
			p["disabled"] = disabled
			modified = true
		}
	}

	if !modified {
		return fmt.Errorf("operator (id='%s', script='%s') not found in file '%s' pipeline", operatorID, scriptFile, fileName)
	}

	payload := map[string]any{
		"name":                    target.Name,
		"displayName":             target.DisplayName,
		"source":                  target.Source,
		"sourceType":              target.SourceType,
		"sourceName":              target.SourceName,
		"mode":                    target.Mode,
		"type":                    target.Type,
		"includeUnsupportedProxy": target.IncludeUnsupportedProxy,
		"process":                 target.Process,
	}

	return c.UpdateFile(ctx, fileName, payload)
}

func (c *Client) setAuth(req *http.Request) {
	cfg := c.GetConfig()
	if cfg == nil {
		return
	}
	if cfg.Username != "" && cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	} else if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		req.Header.Set("x-api-key", cfg.Token)
	} else if cfg.Username != "" {
		password := cfg.Password
		// 当显式密码为空但配置了 URL 路径 Token (例如 /Woo@@@204317) 时，自动回落为 Basic Auth 密码，
		// 同时设置 x-api-key，确保反向代理 HTTP Basic Auth 与 Sub-Store 后端鉴权双双通过
		if password == "" && cfg.URL != "" {
			if u, err := url.Parse(cfg.URL); err == nil && u.Path != "" && u.Path != "/" {
				password = strings.Trim(u.Path, "/")
				req.Header.Set("x-api-key", password)
			}
		}
		req.SetBasicAuth(cfg.Username, password)
	}
	if cfg.Token != "" && (cfg.Username != "" && cfg.Password != "") {
		req.Header.Set("x-api-key", cfg.Token)
	}
}
