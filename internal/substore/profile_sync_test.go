package substore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/store"
)

func TestProfileSync_Q_And_Ashan(t *testing.T) {
	var mu sync.Mutex
	updatedCols := make(map[string]map[string]any)
	updatedFiles := make(map[string]map[string]any)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		if strings.HasPrefix(r.URL.Path, "/api/collection/") {
			name := strings.TrimPrefix(r.URL.Path, "/api/collection/")
			updatedCols[name] = payload
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/file/") {
			name := strings.TrimPrefix(r.URL.Path, "/api/file/")
			updatedFiles[name] = payload
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		if r.URL.Path == "/api/collections" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[]}`))
			return
		}
		if r.URL.Path == "/api/files" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.Config{
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     server.URL,
			Token:   "test-token",
		},
	}
	client := NewClient(&cfg.SubStore)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-profilesync.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// 1. 同步 Q 标准 Profile (外部 + 自建线路机)
	qProfile, err := st.GetProfile(ctx, "Q")
	if err != nil {
		t.Fatalf("GetProfile 'Q' failed: %v", err)
	}

	boundQ, err := client.SyncProfileToSubStore(ctx, qProfile, st)
	if err != nil {
		t.Fatalf("SyncProfileToSubStore for Q failed: %v", err)
	}
	if len(boundQ) != 2 || boundQ[0] != "Q" || boundQ[1] != "q" {
		t.Errorf("expected bound files ['Q', 'q'], got %v", boundQ)
	}

	mu.Lock()
	colQ, exists := updatedCols["col-q"]
	mu.Unlock()
	if !exists {
		t.Fatalf("expected collection 'col-q' to be updated")
	}
	qSubs, ok := colQ["subscriptions"].([]any)
	if !ok {
		// or []string
		qSubsStr, _ := colQ["subscriptions"].([]string)
		if len(qSubsStr) != 2 {
			t.Errorf("expected 2 subscriptions in col-q, got %v", qSubsStr)
		}
	} else {
		if len(qSubs) != 2 {
			t.Errorf("expected 2 subscriptions in col-q, got %v", qSubs)
		}
	}

	// 2. 同步 ashan 标准 Profile (外部 + 自建全部: 线路 + 家宽)
	ashanProfile, err := st.GetProfile(ctx, "ashan")
	if err != nil {
		t.Fatalf("GetProfile 'ashan' failed: %v", err)
	}

	boundAshan, err := client.SyncProfileToSubStore(ctx, ashanProfile, st)
	if err != nil {
		t.Fatalf("SyncProfileToSubStore for ashan failed: %v", err)
	}
	if len(boundAshan) != 2 || boundAshan[0] != "ashan" || boundAshan[1] != "Ashan" {
		t.Errorf("expected bound files ['ashan', 'Ashan'], got %v", boundAshan)
	}

	mu.Lock()
	colAshan, exists := updatedCols["col-ashan"]
	mu.Unlock()
	if !exists {
		t.Fatalf("expected collection 'col-ashan' to be updated")
	}
	ashanSubs, _ := colAshan["subscriptions"].([]any)
	if len(ashanSubs) < 5 {
		ashanSubsStr, _ := colAshan["subscriptions"].([]string)
		if len(ashanSubsStr) < 5 {
			t.Errorf("expected at least 5 subscriptions in col-ashan (transit + 3 resi + external), got %v", colAshan["subscriptions"])
		}
	}

	// 3. 验证文件绑定
	mu.Lock()
	defer mu.Unlock()
	if updatedFiles["Q"]["sourceName"] != "col-q" {
		t.Errorf("expected file 'Q' to bind to 'col-q', got %v", updatedFiles["Q"])
	}
	if updatedFiles["ashan"]["sourceName"] != "col-ashan" {
		t.Errorf("expected file 'ashan' to bind to 'col-ashan', got %v", updatedFiles["ashan"])
	}
}

func TestProfileSync_DynamicExternalSources(t *testing.T) {
	var mu sync.Mutex
	updatedCols := make(map[string]map[string]any)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		if strings.HasPrefix(r.URL.Path, "/api/collection/") {
			name := strings.TrimPrefix(r.URL.Path, "/api/collection/")
			updatedCols[name] = payload
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/file/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.Config{
		SubStore: config.SubStoreConfig{
			Enabled: true,
			URL:     server.URL,
			Token:   "test-token",
		},
	}
	client := NewClient(&cfg.SubStore)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-profilesync-dynamic.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer st.Close()

	// 配置自定义外部源
	extFile := filepath.Join(tmpDir, "external_sources.json")
	restore := SetExternalSourcesFileForTest(extFile)
	defer restore()

	if err := client.SetExternalSources([]string{"airport_dynamic_1", "airport_dynamic_2"}); err != nil {
		t.Fatalf("SetExternalSources failed: %v", err)
	}

	ctx := context.Background()
	qProfile, err := st.GetProfile(ctx, "Q")
	if err != nil {
		t.Fatalf("GetProfile 'Q' failed: %v", err)
	}

	// 1. IncludeExternal 为 true: 包含 airport_dynamic_1, airport_dynamic_2
	_, err = client.SyncProfileToSubStore(ctx, qProfile, st)
	if err != nil {
		t.Fatalf("SyncProfileToSubStore failed: %v", err)
	}

	mu.Lock()
	colQ := updatedCols["col-q"]
	mu.Unlock()

	var qSubs []string
	if subsAny, ok := colQ["subscriptions"].([]any); ok {
		for _, s := range subsAny {
			qSubs = append(qSubs, fmt.Sprint(s))
		}
	} else if subsStr, ok := colQ["subscriptions"].([]string); ok {
		qSubs = subsStr
	}

	hasDynamic1 := false
	hasDynamic2 := false
	for _, s := range qSubs {
		if s == "airport_dynamic_1" {
			hasDynamic1 = true
		}
		if s == "airport_dynamic_2" {
			hasDynamic2 = true
		}
	}
	if !hasDynamic1 || !hasDynamic2 {
		t.Errorf("expected dynamic airports in col-q, got %v", qSubs)
	}

	// 2. IncludeExternal 为 false: 必须剔除 airport_dynamic_1 与 airport_dynamic_2
	qProfile.IncludeExternal = false
	_, err = client.SyncProfileToSubStore(ctx, qProfile, st)
	if err != nil {
		t.Fatalf("SyncProfileToSubStore (no external) failed: %v", err)
	}

	mu.Lock()
	colQNoExt := updatedCols["col-q"]
	mu.Unlock()

	var qSubsNoExt []string
	if subsAny, ok := colQNoExt["subscriptions"].([]any); ok {
		for _, s := range subsAny {
			qSubsNoExt = append(qSubsNoExt, fmt.Sprint(s))
		}
	} else if subsStr, ok := colQNoExt["subscriptions"].([]string); ok {
		qSubsNoExt = subsStr
	}

	for _, s := range qSubsNoExt {
		if s == "airport_dynamic_1" || s == "airport_dynamic_2" || s == "蜂窝云" {
			t.Errorf("col-q without external must not contain %s, got: %v", s, qSubsNoExt)
		}
	}
}
