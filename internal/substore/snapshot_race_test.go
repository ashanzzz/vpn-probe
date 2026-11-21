package substore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vpn-probe/internal/config"
)

// TestSnapshotHighConcurrencyAndRace 高并发读写与竞态安全测试 (go test -race 验证)
func TestSnapshotHighConcurrencyAndRace(t *testing.T) {
	var requestCount int64

	mockYAML := `proxies:
  - name: "Race-Node"
    type: ss
    server: 1.1.1.1
    port: 8388
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		time.Sleep(10 * time.Millisecond) // 模拟计算耗时
		w.Header().Set("Content-Type", "text/yaml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockYAML))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     server.URL,
	}
	client := NewClient(cfg)
	sm := NewSnapshotManager(client, tmpDir)

	// 先预热一次
	_, err := sm.WarmupOne(context.Background(), "Q")
	if err != nil {
		t.Fatalf("initial warmup failed: %v", err)
	}

	var wg sync.WaitGroup
	workers := 20
	iterations := 100

	// 1. 启动 20 个 Worker 高并发并发读取快照 (模拟 2000 次并发拉取)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				data, meta, exists := sm.GetSnapshot("Q")
				if !exists {
					t.Errorf("worker %d: expected snapshot to exist", workerID)
					return
				}
				if len(data) == 0 || meta.ProxyCount != 1 {
					t.Errorf("worker %d: invalid data or proxy count", workerID)
					return
				}
				// 并发读取 Metas
				metas := sm.GetMetas()
				if len(metas) == 0 {
					t.Errorf("worker %d: expected metas to be non-empty", workerID)
					return
				}
			}
		}(i)
	}

	// 2. 同时在后台并发触发预热和模式刷新 (读写混跑，验证 RWMutex 绝对安全)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = sm.Warmup(context.Background(), []string{"Q", "ashan"})
				time.Sleep(15 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// 3. 测试 Stop() 的幂等性 (多次调用绝不 panic)
	sm.Stop()
	sm.Stop() // 第二次调用不应 panic
}
