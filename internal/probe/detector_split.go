package probe

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"vpn-probe/internal/model"
)

// CheckDetailedSplit 对在线节点进行常用关键业务的分流探针测试（纯展示用）
func CheckDetailedSplit(ctx context.Context, port int, timeout time.Duration) *model.SplitInfo {
	info := &model.SplitInfo{
		ChatGPT: "unknown",
		Claude:  "unknown",
		YouTube: "unknown",
		Baidu:   "unknown",
	}

	targets := []struct {
		name string
		url  string
		ptr  *string
	}{
		{"ChatGPT", "https://chatgpt.com/favicon.ico", &info.ChatGPT},
		{"Claude", "https://claude.ai/favicon.ico", &info.Claude},
		{"YouTube", "https://www.youtube.com/generate_204", &info.YouTube},
		{"Baidu", "https://www.baidu.com/favicon.ico", &info.Baidu},
	}

	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(name, urlStr string, dest *string) {
			defer wg.Done()
			client, err := MakeProxyHTTPClient(port, timeout)
			if err != nil {
				*dest = fmt.Sprintf("client err: %v", err)
				return
			}

			req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
			if err != nil {
				*dest = fmt.Sprintf("req err: %v", err)
				return
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				*dest = "fail (unreachable)"
				return
			}
			defer resp.Body.Close()

			duration := time.Since(start).Milliseconds()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				*dest = fmt.Sprintf("ok (%dms)", duration)
			} else if resp.StatusCode == 403 {
				*dest = "blocked (403)"
			} else {
				*dest = fmt.Sprintf("http %d (%dms)", resp.StatusCode, duration)
			}
		}(t.name, t.url, t.ptr)
	}

	wg.Wait()
	return info
}
