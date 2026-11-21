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
	IsChina     bool   `json:"is_china"`
	Certain     bool   `json:"certain"`
	Reason      string `json:"reason"`
	ExitIP      string `json:"exit_ip"`
	ExitCountry string `json:"exit_country"`
	Detail      string `json:"detail"`
}

// ArbitrateGFW 双路研判纯函数，用于根据双路探测输出进行综合裁决
func ArbitrateGFW(googleSuccess, googleChina bool, googleDetail string, cfSuccess, cfChina bool, cfLoc, cfIP, cfDetail string) *GFWResult {
	result := &GFWResult{
		ExitIP:      cfIP,
		ExitCountry: cfLoc,
	}

	// 1. 双路均失败：无法判定送中，标记为不可达且置信度为 false
	if !googleSuccess && !cfSuccess {
		result.IsChina = false
		result.Certain = false
		result.Reason = "unreachable"
		result.Detail = fmt.Sprintf("双路均不可达 [Google: %s; CF: %s]", googleDetail, cfDetail)
		return result
	}

	// 2. CF 探针成功
	if cfSuccess {
		if cfChina {
			result.IsChina = true
			result.Certain = true
			result.Reason = "cf_loc_cn"
			result.Detail = fmt.Sprintf("送中 (CF loc=CN) [Google: %s; CF: %s]", googleDetail, cfDetail)
			return result
		}

		// CF 确认非 CN 出口 (如 JP/US/HK 等)
		if googleSuccess && googleChina {
			// 两路冲突：Google 跳转国内/香港，但 CF 显示海外原生出口
			// 依据规则说明：以 Cloudflare trace 的 loc= 为准 (直接读出口归属更可靠)
			result.IsChina = false
			result.Certain = true
			result.Reason = "clean"
			result.Detail = fmt.Sprintf("不送中 (CF海外出口仲裁为准) [Google: %s; CF: %s]", googleDetail, cfDetail)
			return result
		}

		// CF 正常且 Google 正常未跳转 (或 Google 失败)
		result.IsChina = false
		result.Certain = true
		result.Reason = "clean"
		result.Detail = fmt.Sprintf("不送中 [Google: %s; CF: %s]", googleDetail, cfDetail)
		return result
	}

	// 3. CF 失败但 Google 成功
	if googleChina {
		result.IsChina = true
		result.Certain = true
		result.Reason = "redirected_cn"
		result.Detail = fmt.Sprintf("送中 (Google跳转国内/香港) [Google: %s; CF: %s]", googleDetail, cfDetail)
	} else {
		result.IsChina = false
		result.Certain = true
		result.Reason = "clean"
		result.Detail = fmt.Sprintf("不送中 (Google未跳转) [Google: %s; CF: %s]", googleDetail, cfDetail)
	}
	return result
}

// CheckGFW 执行送中检测（Google NCR + Cloudflare Trace 双路验证，带 1 次重试）
func CheckGFW(ctx context.Context, port int, googleURL, cfTraceURL string, timeout time.Duration) *GFWResult {
	if googleURL == "" {
		googleURL = "https://www.google.com/ncr"
	}
	if cfTraceURL == "" {
		cfTraceURL = "https://cloudflare.com/cdn-cgi/trace"
	}

	var wg sync.WaitGroup
	var googleSuccess, googleChina bool
	var cfSuccess, cfChina bool
	var cfLoc, cfIP string
	var googleDetail, cfDetail string

	// 探针 1: Google NCR 跳转
	wg.Add(1)
	go func() {
		defer wg.Done()
		var resp *http.Response
		var lastErr error

		for attempt := 0; attempt < 2; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					googleDetail = "ctx canceled"
					return
				case <-time.After(1 * time.Second):
				}
			}

			client, err := MakeProxyHTTPClient(port, timeout)
			if err != nil {
				lastErr = err
				continue
			}
			client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}

			req, err := http.NewRequestWithContext(ctx, "GET", googleURL, nil)
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

			resp, err = client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			lastErr = nil
			break
		}

		if lastErr != nil || resp == nil {
			googleSuccess = false
			googleChina = false
			googleDetail = fmt.Sprintf("unreachable: %v", lastErr)
			return
		}
		defer resp.Body.Close()

		googleSuccess = true
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
		var resp *http.Response
		var lastErr error

		for attempt := 0; attempt < 2; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					cfDetail = "ctx canceled"
					return
				case <-time.After(1 * time.Second):
				}
			}

			client, err := MakeProxyHTTPClient(port, timeout)
			if err != nil {
				lastErr = err
				continue
			}

			req, err := http.NewRequestWithContext(ctx, "GET", cfTraceURL, nil)
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("User-Agent", "curl/8.4.0")

			resp, err = client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			lastErr = nil
			break
		}

		if lastErr != nil || resp == nil {
			cfSuccess = false
			cfDetail = fmt.Sprintf("trace err: %v", lastErr)
			return
		}
		defer resp.Body.Close()

		cfSuccess = true
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

	return ArbitrateGFW(googleSuccess, googleChina, googleDetail, cfSuccess, cfChina, cfLoc, cfIP, cfDetail)
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
