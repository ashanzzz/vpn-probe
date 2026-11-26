package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/probe"
)

const defaultResidentialCachePath = "./data/residential_nodes.json"

// ResidentialSyncStatus records the latest dynamic residential node sync state.
type ResidentialSyncStatus struct {
	Enabled            bool      `json:"enabled"`
	SourceURL          string    `json:"source_url,omitempty"`
	CachePath          string    `json:"cache_path"`
	RefreshInterval    string    `json:"refresh_interval,omitempty"`
	NodeCount          int       `json:"node_count"`
	UsingCacheFallback bool      `json:"using_cache_fallback"`
	LastSyncAt         time.Time `json:"last_sync_at,omitempty"`
	LastSuccessAt      time.Time `json:"last_success_at,omitempty"`
	LastChangedAt      time.Time `json:"last_changed_at,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
}

type egressSubscriptionBundle struct {
	InstanceID    string       `json:"instance_id"`
	HostPublicIP  string       `json:"host_public_ip"`
	TotalNodes    int          `json:"total_nodes"`
	MappedNodes   int          `json:"mapped_nodes"`
	ExportedNodes int          `json:"exported_nodes"`
	RefreshedAt   string       `json:"refreshed_at"`
	Nodes         []egressNode `json:"nodes"`
	URILinks      []string     `json:"uri_links"`
}

type egressNode struct {
	ID          int    `json:"id"`
	Priority    int    `json:"priority"`
	Name        string `json:"name"`
	CountryCode string `json:"country_code"`
	FlagEmoji   string `json:"flag_emoji"`
	PublicPort  int    `json:"public_port"`
	Mapped      bool   `json:"mapped"`
	HostIP      string `json:"host_ip"`
	SNI         string `json:"sni"`
	UUID        string `json:"uuid"`
	PublicKey   string `json:"public_key"`
	ShortID     string `json:"short_id"`
	VLESSUrl    string `json:"vless_url"`
}

func newResidentialHTTPClient(rawURL string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	if u, err := url.Parse(rawURL); err == nil {
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			transport.Proxy = nil
		} else if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
			transport.Proxy = nil
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// FetchResidentialNodes pulls dynamic LazyCat residential egress nodes from personal-vps-monitor.
// Supports both structured JSON (format=json) and plain-text VLESS URI list (format=uri).
func FetchResidentialNodes(ctx context.Context, src config.ResidentialSourceConfig) ([]config.MonitoredNode, error) {
	endpoint := strings.TrimSpace(src.URL)
	if endpoint == "" {
		return nil, fmt.Errorf("residential_source.url is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain;q=0.9")

	if tok := strings.TrimSpace(src.Token); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-API-Key", tok)
	}

	client := newResidentialHTTPClient(endpoint, 20*time.Second)
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 256 {
			msg = msg[:256] + "..."
		}
		return nil, fmt.Errorf("upstream status %d: %s", resp.StatusCode, msg)
	}

	nodes, err := parseResidentialResponse(body)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("upstream returned 0 valid residential nodes")
	}
	return nodes, nil
}

func parseResidentialResponse(body []byte) ([]config.MonitoredNode, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, fmt.Errorf("empty response body")
	}

	if strings.HasPrefix(trimmed, "{") {
		var bundle egressSubscriptionBundle
		if err := json.Unmarshal([]byte(trimmed), &bundle); err != nil {
			return nil, fmt.Errorf("decode egress json: %w", err)
		}
		var out []config.MonitoredNode
		seen := make(map[string]bool)

		for _, n := range bundle.Nodes {
			rawURL := strings.TrimSpace(n.VLESSUrl)
			if rawURL == "" {
				continue
			}
			if seen[rawURL] {
				continue
			}
			seen[rawURL] = true

			name := strings.TrimSpace(fmt.Sprintf("%s %s", n.FlagEmoji, n.Name))
			if parsed, err := probe.ParseLink(rawURL); err == nil && strings.TrimSpace(parsed.Tag) != "" {
				name = strings.TrimSpace(parsed.Tag)
			}
			if name == "" {
				name = fmt.Sprintf("LZ-Node-%d", n.ID)
			}
			out = append(out, config.MonitoredNode{
				Name:   name,
				URL:    rawURL,
				IsSelf: true,
			})
		}

		if len(out) == 0 && len(bundle.URILinks) > 0 {
			return parseResidentialURILines(strings.Join(bundle.URILinks, "\n"))
		}
		return out, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var direct []config.MonitoredNode
		if err := json.Unmarshal([]byte(trimmed), &direct); err != nil {
			return nil, fmt.Errorf("decode residential array json: %w", err)
		}
		var out []config.MonitoredNode
		seen := make(map[string]bool)
		for _, n := range direct {
			rawURL := strings.TrimSpace(n.URL)
			if rawURL == "" || seen[rawURL] {
				continue
			}
			seen[rawURL] = true
			name := strings.TrimSpace(n.Name)
			if name == "" {
				if parsed, err := probe.ParseLink(rawURL); err == nil {
					name = strings.TrimSpace(parsed.Tag)
				}
			}
			out = append(out, config.MonitoredNode{
				Name:   name,
				URL:    rawURL,
				IsSelf: true,
			})
		}
		return out, nil
	}

	return parseResidentialURILines(trimmed)
}

func parseResidentialURILines(raw string) ([]config.MonitoredNode, error) {
	var out []config.MonitoredNode
	seen := make(map[string]bool)
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parsed, err := probe.ParseLink(trimmed)
		if err != nil {
			continue
		}
		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		name := strings.TrimSpace(parsed.Tag)
		if name == "" {
			name = trimmed
		}
		out = append(out, config.MonitoredNode{
			Name:   name,
			URL:    trimmed,
			IsSelf: true,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid proxy URIs found in plain-text response")
	}
	return out, nil
}

func loadResidentialCache(cachePath string) ([]config.MonitoredNode, error) {
	if strings.TrimSpace(cachePath) == "" {
		cachePath = defaultResidentialCachePath
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}
	var loaded []config.MonitoredNode
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil, err
	}
	for i := range loaded {
		loaded[i].IsSelf = true
	}
	return loaded, nil
}

func saveResidentialCacheAtomic(cachePath string, nodes []config.MonitoredNode) error {
	if strings.TrimSpace(cachePath) == "" {
		cachePath = defaultResidentialCachePath
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(cachePath), ".residential-*.tmp")
	if err != nil {
		return err
	}
	tmpName := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmpName)
	}
	if err := f.Chmod(0644); err != nil {
		cleanup()
		return err
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, cachePath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func monitoredNodesEqual(a, b []config.MonitoredNode) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].URL != b[i].URL || a[i].IsSelf != b[i].IsSelf {
			return false
		}
	}
	return true
}
