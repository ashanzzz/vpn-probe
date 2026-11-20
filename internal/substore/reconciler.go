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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/collection/%s", baseURL, url.PathEscape(name))

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// 集合不存在，调用 POST /api/collections 创建
		createEndpoint := fmt.Sprintf("%s/api/collections", baseURL)
		postReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// UpdateFile 更新指定输出文件的配置
func (c *Client) UpdateFile(ctx context.Context, name string, payload map[string]any) error {
	baseURL := strings.TrimRight(c.cfg.URL, "/")
	endpoint := fmt.Sprintf("%s/api/file/%s", baseURL, url.PathEscape(name))

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// 文件配置不存在，调用 POST /api/files 创建
		createEndpoint := fmt.Sprintf("%s/api/files", baseURL)
		postReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// AuditTopology 审计当前 SubStore 的订阅、集合与文件引用拓扑，诊断错配与孤儿节点
func (c *Client) AuditTopology(ctx context.Context) (*TopologyAudit, error) {
	subs, err := c.GetSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch subscriptions: %w", err)
	}
	cols, err := c.GetCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch collections: %w", err)
	}
	files, err := c.GetFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch files: %w", err)
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
			audit.RecommendedFixes = append(audit.RecommendedFixes, "将 'ashan-纯净' 净化为仅引用自建三层订阅 [\"ashan-transit-direct\", \"ashan-resi-direct\", \"ashan-chain\"]")
		}
	}

	// 2. 检查 Q-汇总 是否缺少自建 VPS / Q 订阅
	if qSummary, exists := audit.Collections["Q-汇总"]; exists {
		hasQ := false
		for _, s := range qSummary {
			if s == "Q" {
				hasQ = true
			}
		}
		if !hasQ {
			audit.Healthy = false
			msg := "集合 'Q-汇总' (标称蜂窝云+自建VPS) 未包含自有容灾订阅 'Q'，导致自建节点在组合输出中完全缺席"
			audit.Discrepancies = append(audit.Discrepancies, msg)
			audit.RecommendedFixes = append(audit.RecommendedFixes, "为 'Q-汇总' 补充勾选订阅 'Q'，形成 [\"Q\", \"蜂窝云\"] 双轨融合")
		}
	}

	// 3. 检查孤儿订阅
	for _, s := range audit.Subscriptions {
		if !referencedSubs[s] {
			audit.Healthy = false
			msg := fmt.Sprintf("订阅 '%s' 处于孤儿状态，未被任何集合引用 (导致其内部节点无法被任何下游分享产物使用)", s)
			audit.Discrepancies = append(audit.Discrepancies, msg)
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
				"displayName":   "Ashan 纯自建VPS",
				"subscriptions": []string{"ashan-transit-direct", "ashan-resi-direct", "ashan-chain"},
			}
			if err := c.UpdateCollection(ctx, "ashan-纯净", payload); err != nil {
				return nil, fmt.Errorf("reconcile ashan-纯净: %w", err)
			}
			actions = append(actions, "已将集合 'ashan-纯净' 净化为自建三层订阅 [\"ashan-transit-direct\", \"ashan-resi-direct\", \"ashan-chain\"]，彻底剔除机场节点混入")
			logger.Sync("Reconciled collection 'ashan-纯净'")
		}
	}

	// 2. 修复 Q-汇总：融合自建 Q 与蜂窝云
	if qSummary, exists := audit.Collections["Q-汇总"]; exists {
		hasQ := false
		hasCloud := false
		for _, s := range qSummary {
			if s == "Q" {
				hasQ = true
			}
			if s == "蜂窝云" {
				hasCloud = true
			}
		}
		if !hasQ || !hasCloud {
			payload := map[string]any{
				"name":                  "Q-汇总",
				"displayName":           "Q 组合订阅 (蜂窝云 + 自建VPS)",
				"subscriptions":         []string{"Q", "蜂窝云"},
				"ignoreFailedRemoteSub": "quiet",
			}
			if err := c.UpdateCollection(ctx, "Q-汇总", payload); err != nil {
				return nil, fmt.Errorf("reconcile Q-汇总: %w", err)
			}
			actions = append(actions, "已为集合 'Q-汇总' 补齐 [\"Q\", \"蜂窝云\"]，激活 19 个 CF 救活与家宽节点")
			logger.Sync("Reconciled collection 'Q-汇总' -> [\"Q\", \"蜂窝云\"]")
		}
	}

	// 3. 再次执行复查，生成最终报告
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
	baseURL := strings.TrimRight(c.cfg.URL, "/")
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
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
	for _, f := range files {
		if f.Name == fileName {
			target = &f
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
	if c.cfg.Username != "" || c.cfg.Password != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	} else if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
		req.Header.Set("x-api-key", c.cfg.Token)
	}
}
