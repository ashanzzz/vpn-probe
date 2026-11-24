package substore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) candidateBaseURLs() []string {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return nil
	}
	urls := []string{strings.TrimRight(cfg.URL, "/")}
	fb := ResolveFallbackURL(cfg)
	if fb != "" && !strings.EqualFold(fb, cfg.URL) {
		urls = append(urls, strings.TrimRight(fb, "/"))
	}
	return urls
}

// GetFileConfigurations preserves unknown Sub-Store file settings while patching a script.
func (c *Client) GetFileConfigurations(ctx context.Context) ([]map[string]any, error) {
	bases := c.candidateBaseURLs()
	if len(bases) == 0 {
		return nil, fmt.Errorf("substore URL empty")
	}

	var lastErr error
	for _, baseURL := range bases {
		req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/files", nil)
		if err != nil {
			lastErr = err
			continue
		}
		c.setAuth(req)
		resp, err := c.getHTTPClient().Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			lastErr = fmt.Errorf("list files: http %d %s", resp.StatusCode, b)
			continue
		}
		var result struct {
			Data []map[string]any `json:"data"`
		}
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return result.Data, nil
	}
	return nil, lastErr
}

func (c *Client) RenderFileFresh(ctx context.Context, name string) ([]byte, error) {
	bases := c.candidateBaseURLs()
	if len(bases) == 0 {
		return nil, fmt.Errorf("substore URL empty")
	}

	var lastErr error
	for _, baseURL := range bases {
		endpoint := baseURL + "/api/file/" + url.PathEscape(name) + "?noCache=true"
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "clash.meta")
		c.setAuth(req)
		resp, err := c.getHTTPClient().Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			lastErr = fmt.Errorf("render %s: http %d %s", name, resp.StatusCode, b)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, lastErr
}
