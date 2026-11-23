package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// MakeProxyHTTPClient 创建一个通过指定本地端口 socks5 代理的 http.Client
func MakeProxyHTTPClient(port int, timeout time.Duration) (*http.Client, error) {
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", port)
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("create socks5 dialer: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if cd, ok := dialer.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		},
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

// CheckOnline 测试节点是否在线并测量 RTT 延迟
func CheckOnline(ctx context.Context, port int, testURL string, timeout time.Duration) (bool, int, error) {
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}

	client, err := MakeProxyHTTPClient(port, timeout)
	if err != nil {
		return false, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	latency := int(time.Since(start).Milliseconds())

	// 204 No Content 或 200 OK 均视为成功
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return true, latency, nil
	}

	return false, latency, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}
