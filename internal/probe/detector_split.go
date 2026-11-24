package probe

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"vpn-probe/internal/model"
)

// CheckDetailedSplit 对在线节点进行常用关键业务的分流探针测试（全量解析 5 大核心服务及其独立落地 IP、城市与家宽/机房属性）
func CheckDetailedSplit(ctx context.Context, port int, timeout time.Duration) *model.SplitInfo {
	info := &model.SplitInfo{
		ChatGPT: "unknown",
		Claude:  "unknown",
		Gemini:  "unknown",
		YouTube: "unknown",
		Baidu:   "unknown",
		Details: make(map[string]model.SplitTargetResult),
	}

	targets := []struct {
		key      string
		name     string
		url      string
		fallback string
		isCF     bool
		ptr      *string
	}{
		{"chatgpt", "ChatGPT", "https://chatgpt.com/cdn-cgi/trace", "https://chatgpt.com/favicon.ico", true, &info.ChatGPT},
		{"claude", "Claude", "https://claude.ai/cdn-cgi/trace", "https://claude.ai/favicon.ico", true, &info.Claude},
		{"gemini", "Gemini", "https://gemini.google.com/", "https://www.google.com/generate_204", false, &info.Gemini},
		{"youtube", "YouTube", "https://www.youtube.com/generate_204", "", false, &info.YouTube},
		{"baidu", "百度直连", "https://www.baidu.com/favicon.ico", "", false, &info.Baidu},
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, t := range targets {
		wg.Add(1)
		go func(key, name, urlStr, fallbackStr string, isCF bool, dest *string) {
			defer wg.Done()

			client, err := MakeProxyHTTPClient(port, timeout)
			if err != nil {
				*dest = fmt.Sprintf("client err: %v", err)
				mu.Lock()
				info.Details[key] = model.SplitTargetResult{
					Target: key,
					Name:   name,
					Status: "error",
					Detail: *dest,
				}
				mu.Unlock()
				return
			}

			req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
			if err != nil {
				*dest = fmt.Sprintf("req err: %v", err)
				mu.Lock()
				info.Details[key] = model.SplitTargetResult{
					Target: key,
					Name:   name,
					Status: "error",
					Detail: *dest,
				}
				mu.Unlock()
				return
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

			start := time.Now()
			resp, err := client.Do(req)

			// 如果主请求失败且存在 fallback，尝试 fallback
			if (err != nil || (resp != nil && resp.StatusCode >= 500)) && fallbackStr != "" {
				if resp != nil {
					resp.Body.Close()
				}
				reqFb, errFb := http.NewRequestWithContext(ctx, "GET", fallbackStr, nil)
				if errFb == nil {
					reqFb.Header.Set("User-Agent", "Mozilla/5.0")
					start = time.Now()
					resp, err = client.Do(reqFb)
				}
			}

			duration := int(time.Since(start).Milliseconds())

			if err != nil {
				*dest = "fail (unreachable)"
				mu.Lock()
				info.Details[key] = model.SplitTargetResult{
					Target:    key,
					Name:      name,
					Status:    "fail",
					LatencyMs: duration,
					Detail:    "fail (unreachable)",
				}
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			var exitIP, exitLoc, exitColo, exitCity, exitIPType string
			status := "ok"
			detailText := ""

			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				detailText = fmt.Sprintf("ok (%dms)", duration)
				status = "ok"
				*dest = detailText

				// 如果是 Cloudflare 域名的 cdn-cgi/trace，直接解析精准落地 IP、国家与机场三字码
				if isCF && strings.Contains(urlStr, "cdn-cgi/trace") {
					scanner := bufio.NewScanner(resp.Body)
					for scanner.Scan() {
						line := strings.TrimSpace(scanner.Text())
						if strings.HasPrefix(line, "ip=") {
							exitIP = strings.TrimPrefix(line, "ip=")
						} else if strings.HasPrefix(line, "loc=") {
							exitLoc = strings.TrimPrefix(line, "loc=")
						} else if strings.HasPrefix(line, "colo=") {
							exitColo = strings.TrimPrefix(line, "colo=")
						}
					}
					if exitIP != "" {
						geo := LookupGeo(ctx, client, exitIP, exitColo)
						exitCity = geo.City
						if exitLoc == "" {
							exitLoc = geo.CountryCode
						}
						exitIPType = geo.IPType
					}
				}
			} else if resp.StatusCode == 403 {
				detailText = "blocked (403)"
				status = "blocked"
				*dest = detailText
			} else {
				detailText = fmt.Sprintf("http %d (%dms)", resp.StatusCode, duration)
				status = "fail"
				*dest = detailText
			}

			// 对于非 CF 服务（如 Gemini, YouTube），若请求成功但未通过 trace 获取到 IP，可通过节点网络补充 Geo 解析
			if exitIP == "" && status == "ok" && key != "baidu" {
				geo := LookupGeo(ctx, client, "", "")
				if geo.City != "" {
					exitCity = geo.City
					exitLoc = geo.CountryCode
					exitIP = geo.IP
					exitIPType = geo.IPType
				}
			}

			if exitIP != "" && exitIPType == "" {
				exitIPType = DetermineIPType(exitIP, "", "", false)
			}

			mu.Lock()
			info.Details[key] = model.SplitTargetResult{
				Target:      key,
				Name:        name,
				Status:      status,
				LatencyMs:   duration,
				ExitIP:      exitIP,
				ExitCity:    exitCity,
				ExitCountry: exitLoc,
				IPType:      exitIPType,
				Detail:      detailText,
			}
			mu.Unlock()
		}(t.key, t.name, t.url, t.fallback, t.isCF, t.ptr)
	}

	wg.Wait()
	return info
}
