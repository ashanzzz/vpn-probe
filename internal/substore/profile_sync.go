package substore

import (
	"context"
	"fmt"
	"strings"

	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/store"
)

// SyncProfileToSubStore 将指定的 Profile 按选项卡递归解析结果同步至 SubStore
func (c *Client) SyncProfileToSubStore(ctx context.Context, p *model.SubscriptionProfile, st *store.Store) ([]string, error) {
	if p == nil || st == nil {
		return nil, fmt.Errorf("profile and store cannot be nil")
	}

	// 1. 递归解析所有勾选的选项卡
	var allSubs []string
	for _, tabID := range p.SelectedTabs {
		_, subs, err := st.RecursivelyResolveTab(ctx, tabID, nil)
		if err != nil {
			logger.Warnf("[PROFILE-SYNC] Failed to resolve tab '%s': %v", tabID, err)
			continue
		}
		allSubs = append(allSubs, subs...)
	}

	// 2. 外部商业机场包含控制 (动态读取配置的外部源，对齐 Issue #18 & #19)
	extSources := c.GetExternalSources()
	if len(extSources) == 0 {
		extSources = []string{"蜂窝云"}
	}
	extSet := make(map[string]bool)
	for _, s := range extSources {
		extSet[s] = true
	}

	if p.IncludeExternal {
		allSubs = append(allSubs, extSources...)
	} else {
		// 显式剔除所有外部机场
		var filtered []string
		for _, s := range allSubs {
			if !extSet[s] && s != "蜂窝云" {
				filtered = append(filtered, s)
			}
		}
		allSubs = filtered
	}

	// 去重规范化
	resolvedSubs := deduplicate(allSubs)
	if len(resolvedSubs) == 0 {
		resolvedSubs = []string{}
	}

	// 3. 决定绑定的专属集合名称与关联文件
	colName := "col-" + strings.ToLower(p.Name)
	var boundFiles []string
	if strings.EqualFold(p.Name, "q") {
		colName = "col-q"
		boundFiles = []string{"Q", "q"}
	} else if strings.EqualFold(p.Name, "ashan") {
		colName = "col-ashan"
		boundFiles = []string{"ashan", "Ashan"}
	} else {
		boundFiles = []string{p.Name}
	}

	// 4. 更新或创建 SubStore 专属集合
	colPayload := map[string]any{
		"name":                  colName,
		"displayName":           fmt.Sprintf("%s (选项卡动态编排)", p.DisplayName),
		"subscriptions":         resolvedSubs,
		"ignoreFailedRemoteSub": "quiet",
	}
	if err := c.UpdateCollection(ctx, colName, colPayload); err != nil {
		return nil, fmt.Errorf("update collection '%s': %w", colName, err)
	}
	logger.Sync("[PROFILE-SYNC] ✅ Collection '%s' synced with subscriptions: %v", colName, resolvedSubs)

	// 5. 绑定目标文件到专属集合
	for _, fName := range boundFiles {
		filePayload := map[string]any{
			"source":     "local",
			"sourceType": "collection",
			"sourceName": colName,
		}
		if err := c.UpdateFile(ctx, fName, filePayload); err != nil {
			logger.Warnf("[PROFILE-SYNC] Failed to bind file '%s' to collection '%s': %v", fName, colName, err)
		} else {
			logger.Sync("[PROFILE-SYNC] ✅ File '%s' bound to collection '%s'", fName, colName)
		}
	}

	return boundFiles, nil
}

func deduplicate(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			out = append(out, trimmed)
		}
	}
	return out
}
