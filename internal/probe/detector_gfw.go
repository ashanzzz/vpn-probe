package probe

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type GFWResult struct {
	IsChina     bool
	ExitIP      string
	ExitCountry string
	Detail      string
}

// CheckGFW 执行送中检测（Google NCR + Cloudflare Trace 双路验证）
func CheckGFW(ctx context.Context, port int, googleURL, cfTraceURL string, timeout time.Duration) *GFWResult {
	if googleURL == "" {
		googleURL = "https://www.google.com/ncr"
	}
	if cfTraceURL == "" {
		cfTraceURL = "https://cloudflare.com/cdn-cgi/trace"
	}

	result := &GFWResult{
		IsChina: false,
	}

	var wg sync.WaitGroup
	var googleChina bool
	var cfChina bool
	var cfLoc, cfIP string
	var googleDetail, cfDetail string

	// 探针 1: Google NCR 跳转
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, err := MakeProxyHTTPClient(port, timeout)
		if err != nil {
			googleChina = true
			googleDetail = fmt.Sprintf("client err: %v", err)
			return
		}
		// 不跟随重定向，以直接检查 302 Location
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}

		req, err := http.NewRequestWithContext(ctx, "GET", googleURL, nil)
		if err != nil {
			googleChina = true
			googleDetail = fmt.Sprintf("req err: %v", err)
			return
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

		resp, err := client.Do(req)
		if err != nil {
			// 如果访问 google 超时或连不上，说明大概率被拦截送中/出不去
			googleChina = true
			googleDetail = "google unreachable"
			return
		}
		defer resp.Body.Close()

		location := resp.Header.Get("Location")
		if strings.Contains(location, "google.com.hk") || strings.Contains(location, "google.cn") {
			googleChina = true
			googleDetail = fmt.Sprintf("redirected to %s", location)
		} else {
			googleChina = false
			googleDetail = fmt.Sprintf("code %d, loc: %s", resp.StatusCode, location)
		}
	}()

	// 探针 2: Cloudflare Trace 出口与 IP 识别
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, err := MakeProxyHTTPClient(port, timeout)
		if err != nil {
			cfDetail = fmt.Sprintf("client err: %v", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, "GET", cfTraceURL, nil)
		if err != nil {
			cfDetail = fmt.Sprintf("req err: %v", err)
			return
		}
		req.Header.Set("User-Agent", "curl/8.4.0")

		resp, err := client.Do(req)
		if err != nil {
			cfDetail = fmt.Sprintf("cf trace err: %v", err)
			return
		}
		defer resp.Body.Close()

		ip, loc := parseCloudflareTrace(resp.Body)
		cfIP = ip
		cfLoc = loc
		if loc == "CN" {
			cfChina = true
			cfDetail = fmt.Sprintf("cf loc=CN (ip: %s)", ip)
		} else {
			cfChina = false
			cfDetail = fmt.Sprintf("cf loc=%s (ip: %s)", loc, ip)
		}
	}()

	wg.Wait()

	result.ExitIP = cfIP
	result.ExitCountry = cfLoc

	// 综合研判：如果 Google NCR 跳转香港/国内，或 Cloudflare 标记为 CN，则视为送中
	if googleChina || cfChina {
		result.IsChina = true
		result.Detail = fmt.Sprintf("送中! [Google: %s; CF: %s]", googleDetail, cfDetail)
	} else {
		result.IsChina = false
		result.Detail = fmt.Sprintf("不送中 [Google: %s; CF: %s]", googleDetail, cfDetail)
	}

	return result
}

func parseCloudflareTrace(r io.Reader) (ip, loc string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ip=") {
			ip = strings.TrimPrefix(line, "ip=")
		} else if strings.HasPrefix(line, "loc=") {
			loc = strings.TrimPrefix(line, "loc=")
		}
	}
	return
}
