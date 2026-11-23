package api

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	customlog "vpn-probe/internal/logger"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
)

// 5.5.1 获取所有选项卡列表 (原子选项卡与组合节点组)
func (s *APIServer) handleGetTabs(c *fiber.Ctx) error {
	if s.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "database store not initialized"})
	}

	tabs, err := s.store.GetAllTabs(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if tabs == nil {
		tabs = []*model.NodeTab{}
	}

	return c.JSON(fiber.Map{
		"status": "ok",
		"count":  len(tabs),
		"tabs":   tabs,
	})
}

// 5.5.2 保存或新建选项卡 (支持节点组多选嵌套选项卡)
func (s *APIServer) handleSaveTab(c *fiber.Ctx) error {
	if s.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "database store not initialized"})
	}

	var tab model.NodeTab
	if err := c.BodyParser(&tab); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid json body"})
	}
	if tab.ID == "" || tab.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id and name are required"})
	}

	if err := s.store.SaveTab(c.Context(), &tab); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	customlog.Audit("save_tab", c.IP(), tab.ID, "success", fmt.Sprintf("Saved tab '%s' (type=%s, includes=%v)", tab.Name, tab.Type, tab.IncludeTabs))
	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": fmt.Sprintf("Tab '%s' saved successfully", tab.Name),
		"tab":     tab,
	})
}

// 5.5.3 删除非系统选项卡
func (s *APIServer) handleDeleteTab(c *fiber.Ctx) error {
	if s.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "database store not initialized"})
	}

	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tab id is required"})
	}

	if err := s.store.DeleteTab(c.Context(), id); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	customlog.Audit("delete_tab", c.IP(), id, "success", fmt.Sprintf("Deleted tab '%s'", id))
	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": fmt.Sprintf("Tab '%s' deleted", id),
	})
}

// 5.5.4 实时预览指定选项卡匹配到的节点列表
func (s *APIServer) handlePreviewTab(c *fiber.Ctx) error {
	if s.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "database store not initialized"})
	}

	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tab id is required"})
	}

	tab, err := s.store.GetTab(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("tab '%s' not found", id)})
	}

	keywords, subNames, err := s.store.RecursivelyResolveTab(c.Context(), id, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// 收集当前系统活跃原子节点
	var allAtoms []model.AtomNode
	if s.orchestrator != nil {
		allAtoms = s.orchestrator.QueryAtoms(monitor.AtomQueryFilter{})
	}

	// 执行匹配
	var matched []model.AtomNode
	for _, atom := range allAtoms {
		nodeNameLower := strings.ToLower(atom.Name)
		countryLower := strings.ToLower(atom.Country)
		kindLower := strings.ToLower(atom.Kind)

		// 检查排除关键字
		excluded := false
		for _, exc := range tab.ExcludeTags {
			excLower := strings.ToLower(exc)
			if strings.Contains(nodeNameLower, excLower) || (excLower == "送中" && atom.ChinaExit) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// 检查包含关键字
		hit := false
		if len(keywords) == 0 {
			hit = true
		} else {
			for _, kw := range keywords {
				kwLower := strings.ToLower(kw)
				if strings.Contains(nodeNameLower, kwLower) ||
					strings.Contains(countryLower, kwLower) ||
					strings.Contains(kindLower, kwLower) {
					hit = true
					break
				}
			}
		}

		if hit {
			matched = append(matched, atom)
		}
	}

	return c.JSON(fiber.Map{
		"status":        "ok",
		"tab_id":        tab.ID,
		"tab_name":      tab.Name,
		"keywords":      keywords,
		"sub_names":     subNames,
		"matched_count": len(matched),
		"matched_nodes": matched,
	})
}

// 5.5.5 获取所有对外订阅 Profiles
func (s *APIServer) handleGetProfiles(c *fiber.Ctx) error {
	if s.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "database store not initialized"})
	}

	profiles, err := s.store.GetAllProfiles(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if profiles == nil {
		profiles = []*model.SubscriptionProfile{}
	}

	return c.JSON(fiber.Map{
		"status":   "ok",
		"count":    len(profiles),
		"profiles": profiles,
	})
}

// 5.5.6 动态调整指定订阅 Profile 的选项卡多选配置并热生效 SubStore
func (s *APIServer) handleUpdateProfile(c *fiber.Ctx) error {
	if s.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "database store not initialized"})
	}

	name := c.Params("name")
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "profile name is required"})
	}

	var req model.SubscriptionProfile
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid json body"})
	}
	req.Name = name

	// 如果没有传 DisplayName，保留旧值
	oldProfile, _ := s.store.GetProfile(c.Context(), name)
	if req.DisplayName == "" && oldProfile != nil {
		req.DisplayName = oldProfile.DisplayName
	}
	if req.DisplayName == "" {
		req.DisplayName = name
	}

	// 1. 持久化至 SQLite
	if err := s.store.SaveProfile(c.Context(), &req); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// 2. 同步至 SubStore 集合与文件
	var boundFiles []string
	if s.substoreClient != nil && s.cfg != nil && s.cfg.SubStore.Enabled {
		bound, syncErr := s.substoreClient.SyncProfileToSubStore(c.Context(), &req, s.store)
		if syncErr != nil {
			customlog.Errorf("[API] Sync profile '%s' to SubStore failed: %v", name, syncErr)
		} else {
			boundFiles = bound
		}
	}

	// 3. 异步触发快照烘焙预热
	if s.snapshotMgr != nil && len(boundFiles) > 0 {
		s.snapshotMgr.TriggerAsyncWarmup(boundFiles)
	}

	customlog.Audit("update_profile", c.IP(), name, "success", fmt.Sprintf("Updated profile '%s' with tabs=%v, include_external=%v", name, req.SelectedTabs, req.IncludeExternal))
	return c.JSON(fiber.Map{
		"status":      "ok",
		"message":     fmt.Sprintf("Profile '%s' updated and SubStore synced successfully", name),
		"profile":     req,
		"bound_files": boundFiles,
	})
}

// 5.5.7 一键恢复 Q 与 ashan 订阅至用户指定标准预设
func (s *APIServer) handleResetProfiles(c *fiber.Ctx) error {
	if s.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "database store not initialized"})
	}

	if err := s.store.ResetProfiles(c.Context()); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// 同步 Q 与 ashan 到 SubStore
	var allBoundFiles []string
	if s.substoreClient != nil && s.cfg != nil && s.cfg.SubStore.Enabled {
		for _, pName := range []string{"Q", "ashan"} {
			p, err := s.store.GetProfile(c.Context(), pName)
			if err == nil && p != nil {
				bound, syncErr := s.substoreClient.SyncProfileToSubStore(c.Context(), p, s.store)
				if syncErr == nil {
					allBoundFiles = append(allBoundFiles, bound...)
				}
			}
		}
	}

	if s.snapshotMgr != nil && len(allBoundFiles) > 0 {
		s.snapshotMgr.TriggerAsyncWarmup(allBoundFiles)
	}

	customlog.Audit("reset_profiles", c.IP(), "profiles", "success", "Reset profiles Q and ashan to user standard standards")
	return c.JSON(fiber.Map{
		"status":      "ok",
		"message":     "Subscription profiles successfully reset to user standard (Q=External+Transit, ashan=External+Transit+Residential)",
		"bound_files": allBoundFiles,
	})
}
