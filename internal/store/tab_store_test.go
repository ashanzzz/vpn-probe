package store

import (
	"context"
	"path/filepath"
	"testing"

	"vpn-probe/internal/model"
)

func TestTabStore_InitAndResolve(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-tabs.db")

	st, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// 1. 验证默认选项卡已自动初始化
	tabs, err := st.GetAllTabs(ctx)
	if err != nil {
		t.Fatalf("GetAllTabs failed: %v", err)
	}
	if len(tabs) < 9 {
		t.Errorf("expected at least 9 seeded tabs, got %d", len(tabs))
	}

	// 2. 验证递归解析嵌套组合节点组 (group_self_all 嵌套包含了 group_transit 与 group_residential)
	keywords, subNames, err := st.RecursivelyResolveTab(ctx, "group_self_all", nil)
	if err != nil {
		t.Fatalf("RecursivelyResolveTab failed: %v", err)
	}

	hasTransitKW := false
	hasResiKW := false
	for _, kw := range keywords {
		if kw == "线路机" || kw == "4837" {
			hasTransitKW = true
		}
		if kw == "家宽" || kw == "美宽" {
			hasResiKW = true
		}
	}
	if !hasTransitKW || !hasResiKW {
		t.Errorf("expected group_self_all to contain both transit and residential keywords, got %v", keywords)
	}

	hasTransitSub := false
	hasResiSub := false
	for _, s := range subNames {
		if s == "ashan-transit" {
			hasTransitSub = true
		}
		if s == "ashan-us-resi" {
			hasResiSub = true
		}
	}
	if !hasTransitSub || !hasResiSub {
		t.Errorf("expected group_self_all to resolve both ashan-transit and ashan-us-resi, got %v", subNames)
	}

	// 3. 验证动态创建自定义选项卡 (嵌套自建全部 + 自定义关键词)
	customTab := &model.NodeTab{
		ID:          "my_super_group",
		Name:        "我的超级组",
		Type:        model.TabTypeComposite,
		Keywords:    []string{"特选VIP"},
		IncludeTabs: []string{"group_self_all", "external"},
	}
	if err := st.SaveTab(ctx, customTab); err != nil {
		t.Fatalf("SaveTab failed: %v", err)
	}

	// 4. 解析超级组，验证多级嵌套
	superKeywords, superSubs, err := st.RecursivelyResolveTab(ctx, "my_super_group", nil)
	if err != nil {
		t.Fatalf("resolve my_super_group failed: %v", err)
	}
	hasExternalSub := false
	for _, s := range superSubs {
		if s == "蜂窝云" {
			hasExternalSub = true
		}
	}
	if !hasExternalSub {
		t.Errorf("expected my_super_group to include 蜂窝云, got %v", superSubs)
	}
	hasSuperKW := false
	for _, kw := range superKeywords {
		if kw == "特选VIP" {
			hasSuperKW = true
		}
	}
	if !hasSuperKW {
		t.Errorf("expected super keywords to contain '特选VIP', got %v", superKeywords)
	}

	// 5. 循环引用防御测试
	cycleTab := &model.NodeTab{
		ID:          "cycle_a",
		Name:        "循环A",
		Type:        model.TabTypeComposite,
		IncludeTabs: []string{"cycle_b"},
	}
	cycleTabB := &model.NodeTab{
		ID:          "cycle_b",
		Name:        "循环B",
		Type:        model.TabTypeComposite,
		IncludeTabs: []string{"cycle_a"},
	}
	_ = st.SaveTab(ctx, cycleTab)
	_ = st.SaveTab(ctx, cycleTabB)

	// 不能发生死循环或栈溢出
	_, _, err = st.RecursivelyResolveTab(ctx, "cycle_a", nil)
	if err != nil {
		t.Errorf("recursive cycle test failed: %v", err)
	}

	// 6. 验证默认订阅 Profile (Q: 外部+线路; ashan: 外部+全部)
	profiles, err := st.GetAllProfiles(ctx)
	if err != nil {
		t.Fatalf("GetAllProfiles failed: %v", err)
	}
	if len(profiles) < 4 {
		t.Errorf("expected at least 4 default profiles, got %d", len(profiles))
	}

	qProfile, err := st.GetProfile(ctx, "Q")
	if err != nil {
		t.Fatalf("GetProfile 'Q' failed: %v", err)
	}
	if len(qProfile.SelectedTabs) != 2 || qProfile.SelectedTabs[1] != "group_transit" {
		t.Errorf("expected Q to select ['external', 'group_transit'], got %v", qProfile.SelectedTabs)
	}

	ashanProfile, err := st.GetProfile(ctx, "ashan")
	if err != nil {
		t.Fatalf("GetProfile 'ashan' failed: %v", err)
	}
	if len(ashanProfile.SelectedTabs) != 2 || ashanProfile.SelectedTabs[1] != "group_self_all" {
		t.Errorf("expected ashan to select ['external', 'group_self_all'], got %v", ashanProfile.SelectedTabs)
	}

	// 7. 测试删除自定义选项卡
	if err := st.DeleteTab(ctx, "my_super_group"); err != nil {
		t.Fatalf("DeleteTab failed: %v", err)
	}
	// 系统选项卡禁止删除
	if err := st.DeleteTab(ctx, "transit"); err == nil {
		t.Errorf("expected deleting system tab to fail, but succeeded")
	}
}
