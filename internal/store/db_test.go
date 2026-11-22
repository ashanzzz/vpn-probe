package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"vpn-probe/internal/model"
)

func TestStore_SaveAndGetRecentResults(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-probe.db")

	st, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// 1. 测试空数据库返回非 nil 空切片
	emptyList, err := st.GetRecentResults(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecentResults on empty db failed: %v", err)
	}
	if emptyList == nil {
		t.Fatalf("expected non-nil slice for empty results, got nil")
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected 0 results, got %d", len(emptyList))
	}

	// 2. 写入测试记录
	now := time.Now().Truncate(time.Second)
	res1 := &model.NodeProbeResult{
		ID:          "probe-1",
		OriginalURL: "vless://u1@1.2.3.4:443#Node-1",
		Name:        "Node-1",
		Protocol:    "vless",
		IsSelf:      true,
		Online:      true,
		LatencyMs:   120,
		ChinaExit:   false,
		ExitIP:      "1.2.3.4",
		ExitCountry: "JP",
		Category:    model.CategorySelfOnlineClean,
		StatusText:  "在线洁净",
		TestedAt:    now,
		Split: &model.SplitInfo{
			ChatGPT: "ok (200)",
			YouTube: "ok (204)",
		},
	}

	res2 := &model.NodeProbeResult{
		ID:          "probe-2",
		OriginalURL: "vless://u2@5.6.7.8:443#Node-2",
		Name:        "Node-2",
		Protocol:    "vless",
		IsSelf:      true,
		Online:      false,
		LatencyMs:   0,
		ChinaExit:   false,
		Category:    model.CategorySelfOffline,
		StatusText:  "端口被墙",
		ErrorMsg:    "dial tcp timeout",
		TestedAt:    now.Add(time.Second),
		Diagnostic: &model.DiagnosticInfo{
			TargetIP:          "5.6.7.8",
			TargetPort:        443,
			FaultType:         model.FaultPortBlocked,
			StatusText:        "端口被墙需换端口",
			ActionType:        "CHANGE_PORT",
			TroubleshootTip:   "请更换端口",
			DomesticReachable: true,
			AbroadReachable:   true,
			AbroadSource:      "Tokyo",
			AbroadRTTMs:       45,
		},
	}

	if err := st.SaveResult(ctx, res1); err != nil {
		t.Fatalf("SaveResult res1 failed: %v", err)
	}
	if err := st.SaveResult(ctx, res2); err != nil {
		t.Fatalf("SaveResult res2 failed: %v", err)
	}

	// 3. 读取记录（按测试时间倒序排列）
	list, err := st.GetRecentResults(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecentResults failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 results, got %d", len(list))
	}

	// 验证最新的 res2
	latest := list[0]
	if latest.ID != "probe-2" || latest.Online != false || latest.Diagnostic == nil {
		t.Errorf("unexpected latest result: %+v", latest)
	}
	if latest.Diagnostic.FaultType != model.FaultPortBlocked {
		t.Errorf("expected FaultPortBlocked, got %v", latest.Diagnostic.FaultType)
	}

	// 验证第二条 res1
	second := list[1]
	if second.ID != "probe-1" || second.Online != true || second.Split == nil {
		t.Errorf("unexpected second result: %+v", second)
	}
	if second.Split.ChatGPT != "ok (200)" || second.Split.YouTube != "ok (204)" {
		t.Errorf("expected split ChatGPT & YouTube ok, got %+v", second.Split)
	}

	// 4. 测试 limit 截断
	limitedList, err := st.GetRecentResults(ctx, 1)
	if err != nil {
		t.Fatalf("GetRecentResults with limit 1 failed: %v", err)
	}
	if len(limitedList) != 1 {
		t.Fatalf("expected 1 result with limit 1, got %d", len(limitedList))
	}
}
