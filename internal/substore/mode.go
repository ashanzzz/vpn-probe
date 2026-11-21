package substore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	ModeExternalAndSelf: {"Q", "蜂窝云", "ashan's vps", "ashan-transit-direct", "ashan-resi-direct", "ashan-chain"},
	ModeSelfOnly:        {"ashan's vps", "ashan-transit-direct", "ashan-resi-direct", "ashan-chain"},
	ModeExternalOnly:    {"Q", "蜂窝云"},
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

// SetMode 动态切换组合订阅三模式并原子持久化
func (c *Client) SetMode(ctx context.Context, mode SubStoreMode) (*ModeState, error) {
	subs, exists := ModeSubscriptions[mode]
	if !exists {
		return nil, fmt.Errorf("invalid mode: %s (allowed: 'external+self', 'self', 'external')", mode)
	}

	modeMutex.Lock()
	defer modeMutex.Unlock()

	if c.cfg == nil || !c.cfg.Enabled || c.cfg.URL == "" {
		return nil, fmt.Errorf("substore is not configured or disabled")
	}

	base := strings.TrimRight(c.cfg.URL, "/")

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
			resp, reqErr := c.httpClient.Do(req)
			if reqErr == nil {
				resp.Body.Close()
			}
		}
	}

	// 2. 将 4 个分享 File 全部指向 ashan-mode 集合
	filesToUpdate := []string{"Q", "q", "ashan", "Ashan"}
	for _, fname := range filesToUpdate {
		patchPayload := map[string]any{
			"source":     "local",
			"sourceType": "collection",
			"sourceName": "ashan-mode",
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
	_ = os.WriteFile(modeFile, bytesData, 0644)

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
	if c.cfg == nil || !c.cfg.Enabled || c.cfg.URL == "" {
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

	// 创建备份快照
	snapDir := filepath.Join("data", "snapshots")
	_ = os.MkdirAll(snapDir, 0755)
	backupPath := filepath.Join(snapDir, fmt.Sprintf("%s_backup_%s.txt", subName, time.Now().Format("20060102_150405")))
	_ = os.WriteFile(backupPath, []byte(rawContent), 0644)

	if len(removed) > 0 {
		newContent := strings.Join(cleanedLines, "\n")
		base := strings.TrimRight(c.cfg.URL, "/")
		bodyBytes, _ := json.Marshal(map[string]string{"content": newContent})
		req, _ := http.NewRequestWithContext(ctx, "PATCH", fmt.Sprintf("%s/api/sub/%s", base, subName), strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		c.setAuth(req)

		resp, err := c.httpClient.Do(req)
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

// GroupAuditResult 策略组空引用审计结果
type GroupAuditResult struct {
	FileName           string              `json:"file_name"`
	TotalProxies       int                 `json:"total_proxies"`
	Valid              bool                `json:"valid"`
	DanglingReferences map[string][]string `json:"dangling_references"`
	GroupCounts        map[string]int      `json:"group_counts"`
}

// AuditFileGroups 审计指定 File 的 Clash 配置是否存在悬空/不存在的节点引用
func (c *Client) AuditFileGroups(ctx context.Context, fileName string) (*GroupAuditResult, error) {
	if c.cfg == nil || !c.cfg.Enabled || c.cfg.URL == "" {
		return nil, fmt.Errorf("substore is not configured or disabled")
	}

	base := strings.TrimRight(c.cfg.URL, "/")
	auditCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(auditCtx, "GET", fmt.Sprintf("%s/api/file/%s", base, fileName), nil)
	c.setAuth(req)

	auditClient := &http.Client{
		Timeout: 120 * time.Second,
	}
	resp, err := auditClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

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
			if m == "DIRECT" || m == "REJECT" || m == "REJECT-DROP" {
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

	return &GroupAuditResult{
		FileName:           fileName,
		TotalProxies:       len(parsed.Proxies),
		Valid:              allValid,
		DanglingReferences: dangling,
		GroupCounts:        groupCounts,
	}, nil
}
