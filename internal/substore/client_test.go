package substore

import (
	"fmt"
	"sync"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

func TestClient_ConcurrentConfigUpdate(t *testing.T) {
	initialCfg := &config.SubStoreConfig{
		Enabled:         true,
		URL:             "http://initial.example.com",
		GroupSelfOnline: "InitialGroup",
		Token:           "init-token",
	}

	client := NewClient(initialCfg)

	var wg sync.WaitGroup
	workers := 20
	iterations := 200

	// 1. 并发读者
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cfg := client.GetConfig()
				if cfg == nil {
					t.Errorf("worker %d got nil config", id)
					return
				}
				_ = client.getSubscriptionNameForCategory(model.CategorySelfOnline)
				_ = cfg.URL
			}
		}(i)
	}

	// 2. 并发写者 (频繁动态热重载配置)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				newCfg := &config.SubStoreConfig{
					Enabled:         true,
					URL:             fmt.Sprintf("http://update-%d-%d.example.com", id, j),
					GroupSelfOnline: fmt.Sprintf("Group-%d-%d", id, j),
					Token:           fmt.Sprintf("token-%d-%d", id, j),
				}
				client.UpdateConfig(newCfg)
			}
		}(i)
	}

	wg.Wait()

	finalCfg := client.GetConfig()
	if finalCfg == nil {
		t.Fatal("expected non-nil final config")
	}
}
