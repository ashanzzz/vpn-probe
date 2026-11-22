package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/healer"
	"vpn-probe/internal/model"
	"vpn-probe/internal/substore"
)

func TestOrchestratorEvaluationAndQuery(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "custom_groups.json")
	restore := SetCustomGroupsFileForTest(tmpFile)
	defer restore()

	h := healer.NewHealer(nil, nil)
	sch := &Scheduler{
		healer:     h,
		nodeStates: make(map[string]*NodeState),
	}

	// 注入节点状态：
	// 1. 美西线路机 (US, Transit, Clean) - 任何线路机，不限4837
	sch.nodeStates["美西线路机"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "1",
			Name:        "美西洛杉矶专线线路机 — VLESS Reality",
			OriginalURL: "vless://u1@38.175.222.44:36564#US-LAX-Relay",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
			TestedAt:    time.Now(),
		},
	}
	// 2. LZ-US-家宽 (US, Residential, Clean)
	sch.nodeStates["美国家宽01"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "2",
			Name:        "LZ-US-Pavlov-家宽01",
			OriginalURL: "socks5://u:***@1.2.3.4:1001#LZ-US-家宽01",
			ExitCountry: "US",
			Online:      true,
			NodeType:    "residential",
			ChinaExit:   false,
			TestedAt:    time.Now(),
		},
	}
	// 3. 东京软银 (JP, Transit, Clean)
	sch.nodeStates["东京软银"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "3",
			Name:        "东京软银 — VLESS Reality Vision",
			OriginalURL: "vless://u3@45.192.242.72:24444#JP-SoftBank",
			ExitCountry: "JP",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   false,
			TestedAt:    time.Now(),
		},
	}
	// 4. 送中节点 (US, Transit, Leaked)
	sch.nodeStates["送中测试"] = &NodeState{
		LastResult: &model.NodeProbeResult{
			ID:          "4",
			Name:        "某送中机房",
			OriginalURL: "vless://u4@9.9.9.9:443#Leaked-CN",
			ExitCountry: "CN",
			Online:      true,
			NodeType:    "transit",
			ChinaExit:   true,
			TestedAt:    time.Now(),
		},
	}

	orc := NewOrchestrator(sch)

	// 1. 测试原子查询：查询美区线路机
	usTransits := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "transit", CleanOnly: true})
	if len(usTransits) != 1 || !strings.Contains(usTransits[0].Name, "美西洛杉矶专线") {
		t.Fatalf("expected 1 clean US transit, got %v", usTransits)
	}

	// 2. 测试原子查询：查询美国家宽直连
	usResis := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "residential", CleanOnly: true})
	if len(usResis) != 1 || !strings.Contains(usResis[0].Name, "LZ-US") {
		t.Fatalf("expected 1 US residential, got %v", usResis)
	}

	// 3. 测试原子查询：查询线路拉家宽链式中继 (全量网状挂载: 美西专线 + 东京软银 均拉取美国家宽，送中节点物理隔离排除)
	usChains := orc.QueryAtoms(AtomQueryFilter{Country: "US", Kind: "chain"})
	if len(usChains) != 2 || !strings.Contains(usChains[0].Name, "链式") {
		t.Fatalf("expected 2 clean US chains, got %v", usChains)
	}

	// 4. 测试动态公式编排求值：连接 = 美国家宽 + 美国线路机 + 线路家宽机
	bundleFilter := model.GroupFilter{
		Countries:          []string{"US"},
		IncludeTransit:     true, // 美国线路机 (1)
		IncludeResidential: true, // 美国家宽 (1)
		IncludeChain:       true, // 线路家宽机 (2: 美西拉美国家宽 + 东京软银拉美国家宽)
		ExcludeChinaLeak:   true, // 排除送中 (排除送中机房)
	}

	bundleURLs := orc.EvaluateGroup(bundleFilter)
	// 应当恰好返回 4 条连接 (1 线路 + 1 家宽直连 + 2 链式中继：美西与软银分别挂载美国家宽)
	if len(bundleURLs) != 4 {
		t.Fatalf("expected 4 nodes for US bundle formula, got %d: %v", len(bundleURLs), bundleURLs)
	}
	for _, u := range bundleURLs {
		if strings.Contains(u, "Leaked-CN") {
			t.Errorf("leak node must not be in US bundle: %s", u)
		}
	}

	// 5. 测试持久化 Save / Load / Delete
	def := model.CustomGroupDef{
		Name:        "ashan-us-bundle",
		DisplayName: "自建-美国全家桶",
		Enabled:     true,
		Filter:      bundleFilter,
	}

	if err := SaveCustomGroup(def); err != nil {
		t.Fatalf("SaveCustomGroup failed: %v", err)
	}

	loaded, err := LoadCustomGroups()
	if err != nil || len(loaded) != 1 || loaded[0].Name != "ashan-us-bundle" {
		t.Fatalf("LoadCustomGroups failed, got: %v, err: %v", loaded, err)
	}

	if err := DeleteCustomGroup("ashan-us-bundle"); err != nil {
		t.Fatalf("DeleteCustomGroup failed: %v", err)
	}

	afterDel, _ := LoadCustomGroups()
	if len(afterDel) != 0 {
		t.Fatalf("expected 0 groups after delete, got %d", len(afterDel))
	}

	// 6. 测试显式动态拉线路：指定某台线路机拉某台家宽
	bind, err := orc.BindExplicitChain("美西洛杉矶专线线路机 — VLESS Reality", "LZ-US-Pavlov-家宽01")
	if err != nil {
		t.Fatalf("BindExplicitChain failed: %v", err)
	}
	if !strings.Contains(bind.ChainName, "美西洛杉矶专线") || !strings.Contains(bind.ChainName, "LZ-US") {
		t.Errorf("unexpected chain name: %s", bind.ChainName)
	}

	// 检查读取显式定制中继列表
	customChains, err := LoadCustomChains()
	if err != nil || len(customChains) == 0 {
		t.Fatalf("expected custom chains loaded, got: %v, err: %v", customChains, err)
	}

	// 测试解绑
	if err := orc.UnbindExplicitChain(bind.ID); err != nil {
		t.Fatalf("UnbindExplicitChain failed: %v", err)
	}
}

func TestOrchestrator_SyncAllCustomGroups_EmptyNodes(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "custom_groups.json")
	restore := SetCustomGroupsFileForTest(tmpFile)
	defer restore()

	// 注入一个匹配不到任何节点的自定义分组
	def := model.CustomGroupDef{
		Name:        "ashan-fr-empty",
		DisplayName: "自建-法国空分组",
		Enabled:     true,
		Filter: model.GroupFilter{
			Countries: []string{"FR"}, // 无法国节点
		},
	}
	if err := SaveCustomGroup(def); err != nil {
		t.Fatalf("SaveCustomGroup failed: %v", err)
	}

	var syncedName string
	var syncedContent string
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/sub/") {
			mu.Lock()
			syncedName = strings.TrimPrefix(r.URL.Path, "/api/sub/")
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if c, ok := body["content"].(string); ok {
				syncedContent = c
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	subClient := substore.NewClient(&config.SubStoreConfig{
		Enabled: true,
		URL:     ts.URL,
	})
	subClient.SetHTTPClient(ts.Client())

	sch := &Scheduler{
		healer:         healer.NewHealer(nil, nil),
		nodeStates:     make(map[string]*NodeState),
		substoreClient: subClient,
	}

	orc := NewOrchestrator(sch)
	err := orc.SyncAllCustomGroups(context.Background())
	if err != nil {
		t.Fatalf("SyncAllCustomGroups failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if syncedName != "ashan-fr-empty" {
		t.Errorf("expected synced subscription 'ashan-fr-empty', got %q", syncedName)
	}
	if syncedContent != "" {
		t.Errorf("expected empty string content for empty custom group, got %q", syncedContent)
	}
}
