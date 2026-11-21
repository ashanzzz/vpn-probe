package substore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// SnapshotMeta 订阅快照元数据
type SnapshotMeta struct {
	FileName     string    `json:"file_name"`
	Size         int       `json:"size"`
	ProxyCount   int       `json:"proxy_count"`
	DurationMs   int64     `json:"duration_ms"`
	UpdatedAt    time.Time `json:"updated_at"`
	UpdatedAtStr string    `json:"updated_at_str"`
	SHA256       string    `json:"sha256"`
	Status       string    `json:"status"` // "ready", "warming", "stale", "failed"
	LastError    string    `json:"last_error,omitempty"`
}

// SnapshotManager 快照烘焙与极速分发管理器 (彻底解决 Sub-Store 即时计算 300% CPU 风暴)
type SnapshotManager struct {
	mu         sync.RWMutex
	client     *Client
	httpClient *http.Client
	storageDir string
	cache      map[string][]byte
	metas      map[string]*SnapshotMeta
	isWarming  map[string]bool
	asyncMu    sync.Mutex
	asyncQueue map[string]bool
	stopChan   chan struct{}
	stopOnce   sync.Once
}

// NewSnapshotManager 创建快照管理器
func NewSnapshotManager(client *Client, storageDir string) *SnapshotManager {
	if storageDir == "" {
		storageDir = filepath.Join("data", "snapshots")
	}
	_ = os.MkdirAll(storageDir, 0755)

	sm := &SnapshotManager{
		client: client,
		httpClient: &http.Client{
			Timeout: 75 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		storageDir: storageDir,
		cache:      make(map[string][]byte),
		metas:      make(map[string]*SnapshotMeta),
		isWarming:  make(map[string]bool),
		asyncQueue: make(map[string]bool),
		stopChan:   make(chan struct{}),
	}

	// 启动时自动从磁盘加载已有快照，实现秒级冷启动保底
	sm.loadFromDisk()
	return sm
}

// loadFromDisk 从磁盘读取历史快照
func (sm *SnapshotManager) loadFromDisk() {
	files, err := os.ReadDir(sm.storageDir)
	if err != nil {
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(f.Name(), ".yaml")
		filePath := filepath.Join(sm.storageDir, f.Name())
		data, err := os.ReadFile(filePath)
		if err != nil || len(data) == 0 {
			continue
		}

		info, _ := f.Info()
		modTime := time.Now()
		if info != nil {
			modTime = info.ModTime()
		}

		hash := sha256.Sum256(data)
		proxyCount := countProxiesInYAML(data)

		meta := &SnapshotMeta{
			FileName:     name,
			Size:         len(data),
			ProxyCount:   proxyCount,
			DurationMs:   0,
			UpdatedAt:    modTime,
			UpdatedAtStr: modTime.Format("2006-01-02 15:04:05"),
			SHA256:       hex.EncodeToString(hash[:]),
			Status:       "ready",
		}

		sm.cache[name] = data
		sm.metas[name] = meta
		logger.Infof("[SNAPSHOT] Loaded snapshot for '%s' from disk (%d bytes, %d proxies, updated %s)",
			name, len(data), proxyCount, meta.UpdatedAtStr)
	}
}

// GetSnapshot 获取指定文件的最新快照内容与元数据 (毫秒级响应，零 Node.js 负载)
func (sm *SnapshotManager) GetSnapshot(name string) ([]byte, *SnapshotMeta, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	data, exists := sm.cache[name]
	if !exists || len(data) == 0 {
		return nil, nil, false
	}
	meta := sm.metas[name]
	return data, meta, true
}

// GetMetas 获取所有订阅快照的当前元数据状态
func (sm *SnapshotManager) GetMetas() map[string]*SnapshotMeta {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*SnapshotMeta, len(sm.metas))
	for k, v := range sm.metas {
		copied := *v
		result[k] = &copied
	}
	return result
}

// WarmupOne 针对单个文件执行后台烘焙与快照固化
func (sm *SnapshotManager) WarmupOne(ctx context.Context, fileName string) (*SnapshotMeta, error) {
	sm.mu.Lock()
	if sm.isWarming[fileName] {
		sm.mu.Unlock()
		return nil, fmt.Errorf("snapshot for '%s' is already warming in background", fileName)
	}
	sm.isWarming[fileName] = true
	sm.mu.Unlock()

	defer func() {
		sm.mu.Lock()
		sm.isWarming[fileName] = false
		sm.mu.Unlock()
	}()

	logger.Infof("[SNAPSHOT-WARMUP] Starting background pre-render for '%s'...", fileName)
	start := time.Now()

	data, err := sm.fetchRenderedYAML(ctx, fileName)
	duration := time.Since(start)

	if err != nil {
		logger.Warnf("[SNAPSHOT-WARMUP] Pre-rendering '%s' failed after %v: %v", fileName, duration, err)
		sm.mu.Lock()
		if meta, exists := sm.metas[fileName]; exists {
			meta.Status = "stale"
			meta.LastError = err.Error()
		}
		sm.mu.Unlock()
		return nil, err
	}

	proxyCount := countProxiesInYAML(data)
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	meta := &SnapshotMeta{
		FileName:     fileName,
		Size:         len(data),
		ProxyCount:   proxyCount,
		DurationMs:   duration.Milliseconds(),
		UpdatedAt:    time.Now(),
		UpdatedAtStr: time.Now().Format("2006-01-02 15:04:05"),
		SHA256:       hashStr,
		Status:       "ready",
	}

	// 1. 落盘保存
	targetPath := filepath.Join(sm.storageDir, fmt.Sprintf("%s.yaml", fileName))
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		logger.Warnf("[SNAPSHOT-WARMUP] Failed to write snapshot file '%s': %v", targetPath, err)
	}

	// 2. 更新内存缓存
	sm.mu.Lock()
	sm.cache[fileName] = data
	sm.metas[fileName] = meta
	sm.mu.Unlock()

	logger.Sync("[SNAPSHOT-WARMUP] Successfully baked snapshot for '%s' in %v (size: %d bytes, proxies: %d, sha: %s)",
		fileName, duration, len(data), proxyCount, hashStr[:12])

	return meta, nil
}

// Warmup 批量烘焙指定的一组文件 (如 ["ashan", "Ashan", "Q", "q"])
func (sm *SnapshotManager) Warmup(ctx context.Context, fileNames []string) map[string]error {
	results := make(map[string]error)
	for _, fn := range fileNames {
		// 单文件超时控制 90s，防止外部机场严重拖垮整体流程
		subCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		_, err := sm.WarmupOne(subCtx, fn)
		cancel()
		results[fn] = err
	}
	return results
}

// TriggerAsyncWarmup 异步触发一次预烘焙（带去重防抖与防协程堆积控制）
func (sm *SnapshotManager) TriggerAsyncWarmup(fileNames []string) {
	sm.asyncMu.Lock()
	var newTargets []string
	for _, fn := range fileNames {
		if !sm.asyncQueue[fn] {
			sm.asyncQueue[fn] = true
			newTargets = append(newTargets, fn)
		}
	}
	sm.asyncMu.Unlock()

	if len(newTargets) == 0 {
		return
	}

	go func(targets []string) {
		defer func() {
			sm.asyncMu.Lock()
			for _, t := range targets {
				delete(sm.asyncQueue, t)
			}
			sm.asyncMu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		sm.Warmup(ctx, targets)
	}(newTargets)
}

// StartAutoWarmup 启动后台常驻定时烘焙 Worker
func (sm *SnapshotManager) StartAutoWarmup(interval time.Duration, fileNames []string) {
	if interval < 1*time.Minute {
		interval = 10 * time.Minute
	}
	go func() {
		logger.Infof("[SNAPSHOT-WORKER] Auto-warmup worker started (interval: %v, targets: %v)", interval, fileNames)
		// 启动后 5 秒立即执行初次预烘焙
		select {
		case <-time.After(5 * time.Second):
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			sm.Warmup(ctx, fileNames)
			cancel()
		case <-sm.stopChan:
			return
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				sm.Warmup(ctx, fileNames)
				cancel()
			case <-sm.stopChan:
				logger.Infof("[SNAPSHOT-WORKER] Auto-warmup worker stopped.")
				return
			}
		}
	}()
}

// Stop 停止后台任务 (并发幂等安全，可多次调用绝不 panic)
func (sm *SnapshotManager) Stop() {
	sm.stopOnce.Do(func() {
		close(sm.stopChan)
	})
}

// fetchRenderedYAML 向 Sub-Store 源站拉取渲染后的真实 YAML 文本 (带直连与多域名自动重试)
func (sm *SnapshotManager) fetchRenderedYAML(ctx context.Context, fileName string) ([]byte, error) {
	if sm.client == nil || sm.client.cfg == nil || !sm.client.cfg.Enabled {
		return nil, fmt.Errorf("substore client is not configured or disabled")
	}

	var urlsToTry []string
	if sm.client.cfg.URL != "" {
		urlsToTry = append(urlsToTry, strings.TrimRight(sm.client.cfg.URL, "/"))
	}
	// 直连兜底端点
	directEndpoint := "http://156.231.113.91:3001/Woo@@@204317"
	hasDirect := false
	for _, u := range urlsToTry {
		if strings.Contains(u, "156.231.113.91") {
			hasDirect = true
			break
		}
	}
	if !hasDirect {
		urlsToTry = append(urlsToTry, directEndpoint)
	}

	httpClient := sm.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 75 * time.Second}
	}

	var lastErr error
	for _, base := range urlsToTry {
		endpoint := fmt.Sprintf("%s/api/file/%s", base, fileName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		sm.client.setAuth(req)
		req.Header.Set("User-Agent", "ClashX/1.90.0")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("substore status %d: %s", resp.StatusCode, string(body))
			continue
		}

		if len(body) > 0 {
			return body, nil
		}
	}

	return nil, fmt.Errorf("fetch rendered file '%s' failed on all endpoints: %w", fileName, lastErr)
}

// countProxiesInYAML 从 YAML 中统计代理节点数量
func countProxiesInYAML(data []byte) int {
	var parsed struct {
		Proxies []any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &parsed); err == nil {
		return len(parsed.Proxies)
	}
	return 0
}
