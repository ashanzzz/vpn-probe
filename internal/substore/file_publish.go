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

// GetFileConfigurations preserves unknown Sub-Store file settings while patching a script.
func (c *Client) GetFileConfigurations(ctx context.Context) ([]map[string]any, error) {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("substore URL empty")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(cfg.URL, "/")+"/api/files", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("list files: http %d %s", resp.StatusCode, b)
	}
	var result struct {
		Data []map[string]any `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (c *Client) RenderFileFresh(ctx context.Context, name string) ([]byte, error) {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("substore URL empty")
	}
	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/file/" + url.PathEscape(name) + "?noCache=true"
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clash.meta")
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("render %s: http %d %s", name, resp.StatusCode, b)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}
