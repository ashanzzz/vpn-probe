package speedtest

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"time"

	"golang.org/x/net/proxy"
	"vpn-probe/internal/logger"
	"vpn-probe/internal/model"
)

const (
	DefaultVPS30MURL    = "http://45.192.242.72:28745/s/default/speedtest-30m.bin"
	DefaultFallback30M  = "https://speed.cloudflare.com/__down?bytes=31457280"
	DefaultStreamURL    = "https://speed.cloudflare.com/__down?bytes=104857600"
)

// MakeProxyHTTPClient 为测速创建 SOCKS5 HTTP Client
func MakeProxyHTTPClient(port int, timeout time.Duration) (*http.Client, error) {
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", port)
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("create socks5 dialer: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		DisableKeepAlives:   false,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

// TestVPS30M 针对 VPS 自建 30MB 真实测试文件进行无风控吞吐量测速
func TestVPS30M(ctx context.Context, port int, targetURL string, latencyMs int) (*model.SpeedResult, error) {
	if targetURL == "" {
		targetURL = DefaultVPS30MURL
	}

	client, err := MakeProxyHTTPClient(port, 45*time.Second)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		// 尝试降级使用 Cloudflare 30MB 备选
		if targetURL == DefaultVPS30MURL {
			logger.Warnf("[SPEED] VPS endpoint failed (%v), falling back to Cloudflare 30M stream...", err)
			return TestVPS30M(ctx, port, DefaultFallback30M, latencyMs)
		}
		return nil, fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	ttfb := int(time.Since(start).Milliseconds())

	buf := make([]byte, 64*1024) // 64KB 缓冲区
	var totalBytes int64
	firstByteTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			totalBytes += int64(n)
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return nil, fmt.Errorf("read body: %w", rerr)
		}
	}

	downloadDur := time.Since(firstByteTime).Seconds()
	if downloadDur < 0.05 {
		downloadDur = 0.05
	}

	speedMBps := (float64(totalBytes) / (1024 * 1024)) / downloadDur
	speedMbps := speedMBps * 8.0

	res := &model.SpeedResult{
		Method:      "vps_30m",
		TestedAt:    time.Now(),
		DurationSec: math.Round(downloadDur*100) / 100,
		TotalBytes:  totalBytes,
		SpeedMBps:   math.Round(speedMBps*100) / 100,
		SpeedMbps:   math.Round(speedMbps*100) / 100,
		AvgMbps:     math.Round(speedMbps*100) / 100,
		PeakMbps:    math.Round(speedMbps*100) / 100,
		TTFBMs:      ttfb,
	}

	res.Score = CalculateScore(res, latencyMs, true)
	logger.Infof("[SPEED] VPS 30MB Test finished: %.2f MB in %.2fs -> %.2f MB/s (%.2f Mbps), TTFB=%dms, Score=%.1f",
		float64(totalBytes)/(1024*1024), downloadDur, res.SpeedMBps, res.SpeedMbps, ttfb, res.Score)

	return res, nil
}

// TestStream30S 执行持续流媒体测速 (采样峰值、平均值与抖动)
func TestStream30S(ctx context.Context, port int, streamURL string, durationSec int, latencyMs int) (*model.SpeedResult, error) {
	if streamURL == "" {
		streamURL = DefaultStreamURL
	}
	if durationSec <= 0 {
		durationSec = 30
	}

	client, err := MakeProxyHTTPClient(port, time.Duration(durationSec+15)*time.Second)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(durationSec)*time.Second+5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, streamURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	ttfb := int(time.Since(start).Milliseconds())

	buf := make([]byte, 64*1024)
	var totalBytes int64
	var peakBytesPerSec int64
	var currentWindowBytes int64

	streamStart := time.Now()
	windowStart := time.Now()
	testDeadline := streamStart.Add(time.Duration(durationSec) * time.Second)

	for {
		if time.Now().After(testDeadline) {
			break
		}

		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			totalBytes += int64(n)
			currentWindowBytes += int64(n)
		}

		now := time.Now()
		elapsedWindow := now.Sub(windowStart)
		if elapsedWindow >= time.Second {
			windowRate := int64(float64(currentWindowBytes) / elapsedWindow.Seconds())
			if windowRate > peakBytesPerSec {
				peakBytesPerSec = windowRate
			}
			currentWindowBytes = 0
			windowStart = now
		}

		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			break
		}
	}

	totalDur := time.Since(streamStart).Seconds()
	if totalDur < 0.1 {
		totalDur = 0.1
	}

	avgMBps := (float64(totalBytes) / (1024 * 1024)) / totalDur
	avgMbps := avgMBps * 8.0
	peakMBps := (float64(peakBytesPerSec) / (1024 * 1024))
	peakMbps := peakMBps * 8.0
	if peakMbps < avgMbps {
		peakMbps = avgMbps
	}

	res := &model.SpeedResult{
		Method:      "stream_30s",
		TestedAt:    time.Now(),
		DurationSec: math.Round(totalDur*100) / 100,
		TotalBytes:  totalBytes,
		SpeedMBps:   math.Round(avgMBps*100) / 100,
		SpeedMbps:   math.Round(avgMbps*100) / 100,
		AvgMbps:     math.Round(avgMbps*100) / 100,
		PeakMbps:    math.Round(peakMbps*100) / 100,
		TTFBMs:      ttfb,
	}

	res.Score = CalculateScore(res, latencyMs, true)
	logger.Infof("[SPEED] Stream Test finished: %.2f MB in %.1fs -> Avg: %.2f Mbps, Peak: %.2f Mbps, TTFB=%dms, Score=%.1f",
		float64(totalBytes)/(1024*1024), totalDur, res.AvgMbps, res.PeakMbps, ttfb, res.Score)

	return res, nil
}

// CalculateScore 计算综合评分 (0~100)
// Score = 0.5 * SpeedScore + 0.3 * LatencyScore + 0.2 * StabilityScore
func CalculateScore(res *model.SpeedResult, latencyMs int, online bool) float64 {
	if !online || res == nil {
		return 0.0
	}

	// 1. 速度得分: 以 100Mbps 为基准饱和满分
	rate := res.AvgMbps
	if rate <= 0 {
		rate = res.SpeedMbps
	}
	speedScore := (rate / 100.0) * 100.0
	if speedScore > 100.0 {
		speedScore = 100.0
	}
	if speedScore < 0 {
		speedScore = 0
	}

	// 2. 延迟得分: 0ms -> 100分, 150ms -> 50分, >=300ms -> 0分
	latencyScore := 100.0 - (float64(latencyMs) / 3.0)
	if latencyScore > 100.0 {
		latencyScore = 100.0
	}
	if latencyScore < 0.0 {
		latencyScore = 0.0
	}

	// 3. 稳定性得分: 首字节延迟评估, TTFB < 200ms 得满分 100, 之后每超 20ms 扣 1 分
	stabilityScore := 100.0
	if res.TTFBMs > 200 {
		deduct := float64(res.TTFBMs-200) / 20.0
		stabilityScore -= deduct
		if stabilityScore < 20.0 {
			stabilityScore = 20.0
		}
	}

	totalScore := 0.5*speedScore + 0.3*latencyScore + 0.2*stabilityScore
	return math.Round(totalScore*10) / 10
}

// RankNodes 根据测速评分与在线状态对节点进行降序排列
func RankNodes(results []*model.NodeProbeResult) []*model.NodeProbeResult {
	sorted := make([]*model.NodeProbeResult, len(results))
	copy(sorted, results)

	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Online != b.Online {
			return a.Online // 在线优先
		}
		if a.Score != b.Score {
			return a.Score > b.Score // 高分优先
		}
		// 分数相同则低延迟优先
		return a.LatencyMs < b.LatencyMs
	})

	return sorted
}
