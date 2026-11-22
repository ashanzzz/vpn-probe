package vpsmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"vpn-probe/internal/logger"
)

type GitHubChecker struct {
	baseURL  string
	client   *http.Client
	cache    map[string]*ReleaseInfo
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

func NewGitHubChecker() *GitHubChecker {
	return &GitHubChecker{
		baseURL: "https://api.github.com",
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache:    make(map[string]*ReleaseInfo),
		cacheTTL: 2 * time.Hour,
	}
}

// SetBaseURLForTest 设置自定义 API 基地址 (用于单测 Mock)
func (g *GitHubChecker) SetBaseURLForTest(u string) {
	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()
	g.baseURL = u
}

type ghReleaseResponse struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

// GetLatestRelease 获取指定上游仓库的最新 Release（带内存缓存与超时防抖）
func (g *GitHubChecker) GetLatestRelease(ctx context.Context, repo string) (*ReleaseInfo, error) {
	g.cacheMu.RLock()
	cached, ok := g.cache[repo]
	if ok && time.Since(cached.CachedAt) < g.cacheTTL {
		g.cacheMu.RUnlock()
		return cached, nil
	}
	base := g.baseURL
	g.cacheMu.RUnlock()

	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(base, "/"), repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create req: %w", err)
	}
	req.Header.Set("User-Agent", "vpn-probe-vpsmgr/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if ghToken := os.Getenv("GITHUB_PAT"); ghToken != "" {
		req.Header.Set("Authorization", "token "+ghToken)
	} else if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
		req.Header.Set("Authorization", "Bearer "+ghToken)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		// 若请求失败且有旧缓存，返回过期缓存作为降级
		g.cacheMu.RLock()
		if old, exists := g.cache[repo]; exists {
			g.cacheMu.RUnlock()
			logger.Warnf("[VPSMGR-GH] Failed to query %s latest release: %v, falling back to cached %s", repo, err, old.Version)
			return old, nil
		}
		g.cacheMu.RUnlock()
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.cacheMu.RLock()
		if old, exists := g.cache[repo]; exists {
			g.cacheMu.RUnlock()
			logger.Warnf("[VPSMGR-GH] GitHub API returned status %d for %s, falling back to cached %s", resp.StatusCode, repo, old.Version)
			return old, nil
		}
		g.cacheMu.RUnlock()
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var ghResp ghReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&ghResp); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}

	ver := strings.TrimPrefix(ghResp.TagName, "v")
	pubDate := ghResp.PublishedAt
	if len(pubDate) >= 10 {
		pubDate = pubDate[:10]
	}

	info := &ReleaseInfo{
		Repo:        repo,
		TagName:     ghResp.TagName,
		Version:     ver,
		Name:        ghResp.Name,
		PublishedAt: pubDate,
		HTMLURL:     ghResp.HTMLURL,
		CachedAt:    time.Now(),
	}

	g.cacheMu.Lock()
	g.cache[repo] = info
	g.cacheMu.Unlock()

	return info, nil
}
