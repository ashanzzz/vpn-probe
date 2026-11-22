package substore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vpn-probe/internal/config"
)

func TestSnapshotManager(t *testing.T) {
	// 1. Mock Sub-Store 服务器，返回虚拟的 Clash 配置
	mockYAML := `proxies:
  - name: "Node-1"
    type: ss
    server: 1.1.1.1
    port: 8388
  - name: "Node-2"
    type: vless
    server: 2.2.2.2
    port: 443
proxy-groups:
  - name: "AUTO"
    type: url-test
    proxies: ["Node-1", "Node-2"]
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/file/Q" || r.URL.Path == "/api/file/ashan" {
			w.Header().Set("Content-Type", "text/yaml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(mockYAML))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpDir := t.TempDir()

	cfg := &config.SubStoreConfig{
		Enabled: true,
		URL:     server.URL,
	}
	client := NewClient(cfg)

	sm := NewSnapshotManager(client, tmpDir)

	// 2. 初始状态：无快照
	_, _, exists := sm.GetSnapshot("Q")
	if exists {
		t.Fatalf("expected snapshot 'Q' to not exist initially")
	}

	// 3. 执行单文件烘焙
	meta, err := sm.WarmupOne(context.Background(), "Q")
	if err != nil {
		t.Fatalf("WarmupOne failed: %v", err)
	}

	if meta.ProxyCount != 2 {
		t.Fatalf("expected proxy count 2, got %d", meta.ProxyCount)
	}
	if meta.Status != "ready" {
		t.Fatalf("expected status 'ready', got %s", meta.Status)
	}

	// 4. 再次获取快照，验证内存高速命中
	data, cachedMeta, exists := sm.GetSnapshot("Q")
	if !exists {
		t.Fatalf("expected snapshot 'Q' to exist in memory cache")
	}
	if string(data) != mockYAML {
		t.Fatalf("snapshot data mismatch")
	}
	if cachedMeta.ProxyCount != 2 {
		t.Fatalf("cached meta proxy count mismatch: %d", cachedMeta.ProxyCount)
	}

	// 5. 验证磁盘文件落盘
	diskFile := filepath.Join(tmpDir, "Q.yaml")
	diskData, err := os.ReadFile(diskFile)
	if err != nil {
		t.Fatalf("failed to read disk snapshot file: %v", err)
	}
	if string(diskData) != mockYAML {
		t.Fatalf("disk snapshot data mismatch")
	}

	// 6. 验证冷启动：新建 SnapshotManager 应该直接从磁盘加载
	sm2 := NewSnapshotManager(client, tmpDir)
	data2, meta2, exists2 := sm2.GetSnapshot("Q")
	if !exists2 {
		t.Fatalf("expected cold-start snapshot 'Q' to be loaded from disk")
	}
	if meta2.ProxyCount != 2 {
		t.Fatalf("cold-start meta proxy count mismatch: %d", meta2.ProxyCount)
	}
	if string(data2) != mockYAML {
		t.Fatalf("cold-start data mismatch")
	}

	// 7. 批量预热测试
	results := sm.Warmup(context.Background(), []string{"ashan", "non_existing"})
	if results["ashan"] != nil {
		t.Fatalf("expected 'ashan' warmup to succeed, got %v", results["ashan"])
	}
	if results["non_existing"] == nil {
		t.Fatalf("expected 'non_existing' warmup to fail")
	}

	// 验证所有状态列表
	metas := sm.GetMetas()
	if len(metas) < 2 {
		t.Fatalf("expected at least 2 metas, got %d", len(metas))
	}
}

func TestSanitizeRenderedYAML(t *testing.T) {
	input := []byte(`
tun:
  enable: false
  stack: mips
  device: mihomo
proxies:
  - name: 链式|US-LAX-ACCK-4837-Reality+➔+LZ-US-Pavlov-家宽-01
    type: socks5
`)
	sanitized := SanitizeRenderedYAML(input)
	res := string(sanitized)

	if strings.Contains(res, "stack: mips") {
		t.Errorf("expected 'stack: mips' to be replaced, got: %s", res)
	}
	if !strings.Contains(res, "stack: gvisor") {
		t.Errorf("expected 'stack: gvisor' in output, got: %s", res)
	}
	if strings.Contains(res, "+➔+") {
		t.Errorf("expected '+➔+' to be replaced, got: %s", res)
	}
	if !strings.Contains(res, " ➔ ") {
		t.Errorf("expected ' ➔ ' in output, got: %s", res)
	}
}
