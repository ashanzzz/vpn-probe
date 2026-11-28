package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"vpn-probe/internal/config"
	customlog "vpn-probe/internal/logger"
	"vpn-probe/internal/speedtest"
)

type persistedSpeedtestConfig struct {
	SelfHostedURL10M string `json:"self_hosted_url_10m"`
	SelfHostedURL30M string `json:"self_hosted_url_30m"`
}

func speedtestPersistPath() string {
	if p := os.Getenv("VPN_PROBE_SPEEDTEST_CONFIG"); strings.TrimSpace(p) != "" {
		return p
	}
	return filepath.Join("data", "speedtest_config.json")
}

func loadPersistedSpeedtestConfig() (string, string) {
	raw, err := os.ReadFile(speedtestPersistPath())
	if err != nil {
		return "", ""
	}
	var cfg persistedSpeedtestConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", ""
	}
	return strings.TrimSpace(cfg.SelfHostedURL10M), strings.TrimSpace(cfg.SelfHostedURL30M)
}

func savePersistedSpeedtestConfig(cfg *config.Config, configPath, url10m, url30m string) (bool, bool, error) {
	jsonPath := speedtestPersistPath()
	_ = os.MkdirAll(filepath.Dir(jsonPath), 0755)
	payload, err := json.MarshalIndent(persistedSpeedtestConfig{
		SelfHostedURL10M: strings.TrimSpace(url10m),
		SelfHostedURL30M: strings.TrimSpace(url30m),
	}, "", "  ")
	if err != nil {
		return false, false, err
	}
	if err := os.WriteFile(jsonPath, payload, 0600); err != nil {
		return false, false, fmt.Errorf("write %s: %w", jsonPath, err)
	}

	yamlSaved := false
	if cfg != nil {
		cfg.SpeedTest.SelfHostedURL10M = strings.TrimSpace(url10m)
		cfg.SpeedTest.SelfHostedURL30M = strings.TrimSpace(url30m)
		targetPath := configPath
		if targetPath == "" {
			targetPath = "config.yaml"
		}
		if _, statErr := os.Stat(targetPath); statErr == nil {
			if saveErr := config.SaveConfig(targetPath, cfg); saveErr == nil {
				yamlSaved = true
			} else {
				customlog.Warnf("[SPEEDTEST] config.yaml save skipped (%v), persisted in %s", saveErr, jsonPath)
			}
		}
	}
	return true, yamlSaved, nil
}

func (s *APIServer) handleSaveSpeedtestConfig(c *fiber.Ctx) error {
	var req struct {
		SelfHostedURL10M string `json:"self_hosted_url_10m"`
		SelfHostedURL30M string `json:"self_hosted_url_30m"`
		Verify           *bool  `json:"verify,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  "error",
			"error":   err.Error(),
			"message": err.Error(),
		})
	}

	u10 := strings.TrimSpace(req.SelfHostedURL10M)
	u30 := strings.TrimSpace(req.SelfHostedURL30M)

	if err := speedtest.ValidateSelfHostedURL(u10); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  "error",
			"error":   err.Error(),
			"message": err.Error(),
		})
	}
	if err := speedtest.ValidateSelfHostedURL(u30); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  "error",
			"error":   err.Error(),
			"message": err.Error(),
		})
	}

	shouldVerify := req.Verify == nil || *req.Verify
	if shouldVerify {
		for _, item := range []struct {
			slot string
			url  string
		}{
			{"10m", u10},
			{"30m", u30},
		} {
			if item.url == "" {
				continue
			}
			code, _, probeErr := speedtest.ProbeSelfHostedEndpoint(c.Context(), item.url)
			if probeErr != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"status":      "error",
					"slot":        item.slot,
					"probe_url":   item.url,
					"status_code": code,
					"error":       probeErr.Error(),
					"message":     fmt.Sprintf("%s 测速端点预检失败: %v", strings.ToUpper(item.slot), probeErr),
				})
			}
		}
	}

	speedtest.SetSelfHostedSpeedURLsExact(u10, u30)
	jsonSaved, yamlSaved, saveErr := savePersistedSpeedtestConfig(s.cfg, s.configPath, u10, u30)
	if saveErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"status":  "error",
			"error":   saveErr.Error(),
			"message": saveErr.Error(),
		})
	}

	customlog.Audit("update_speedtest_config", c.IP(), "/api/speedtest/config", "success",
		fmt.Sprintf("10m=%s 30m=%s json=%v yaml=%v", customlog.MaskURL(u10), customlog.MaskURL(u30), jsonSaved, yamlSaved))

	saved10, saved30 := speedtest.GetSelfHostedSpeedURLs()
	return c.JSON(fiber.Map{
		"status":              "success",
		"self_hosted_url_10m": saved10,
		"self_hosted_url_30m": saved30,
		"persisted_json":      jsonSaved,
		"persisted_yaml":      yamlSaved,
		"message":             "自建测速端点已通过预检并持久化保存",
	})
}

func (s *APIServer) handleTestSpeedtestConfig(c *fiber.Ctx) error {
	var req struct {
		SelfHostedURL10M string `json:"self_hosted_url_10m"`
		SelfHostedURL30M string `json:"self_hosted_url_30m"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error(), "message": err.Error()})
	}

	type probeItem struct {
		Slot         string `json:"slot"`
		URL          string `json:"url"`
		OK           bool   `json:"ok"`
		StatusCode   int    `json:"status_code,omitempty"`
		SampledBytes int64  `json:"sampled_bytes,omitempty"`
		Error        string `json:"error,omitempty"`
	}

	var results []probeItem
	allOK := true
	for _, entry := range []struct {
		slot string
		raw  string
	}{
		{"10m", strings.TrimSpace(req.SelfHostedURL10M)},
		{"30m", strings.TrimSpace(req.SelfHostedURL30M)},
	} {
		if entry.raw == "" {
			continue
		}
		code, n, err := speedtest.ProbeSelfHostedEndpoint(c.Context(), entry.raw)
		item := probeItem{
			Slot:         entry.slot,
			URL:          entry.raw,
			OK:           err == nil,
			StatusCode:   code,
			SampledBytes: n,
		}
		if err != nil {
			item.Error = err.Error()
			allOK = false
		}
		results = append(results, item)
	}

	if len(results) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"ok":      false,
			"message": "请至少填写一个自建测速 URL 再执行预检",
		})
	}
	return c.JSON(fiber.Map{
		"ok":      allOK,
		"results": results,
	})
}

func (s *APIServer) handleGetRecentLogs(c *fiber.Ctx) error {
	limit := 100
	if raw := strings.TrimSpace(c.Query("lines")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	source := strings.ToLower(strings.TrimSpace(c.Query("source", "runtime")))
	logPath := filepath.Join("data", "vpn-probe.log")
	if source == "audit" {
		logPath = filepath.Join("data", "audit.log")
	} else if s.cfg != nil && strings.TrimSpace(s.cfg.Logging.FilePath) != "" {
		logPath = s.cfg.Logging.FilePath
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		return c.JSON(fiber.Map{
			"status": "ok",
			"source": source,
			"path":   logPath,
			"lines":  []string{},
		})
	}
	allLines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(allLines))
	for _, line := range allLines {
		if strings.TrimSpace(line) != "" {
			filtered = append(filtered, line)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return c.JSON(fiber.Map{
		"status": "ok",
		"source": source,
		"path":   logPath,
		"total":  len(filtered),
		"lines":  filtered,
	})
}
