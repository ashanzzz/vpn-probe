package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigEnvOverride(t *testing.T) {
	os.Setenv("VPN_PROBE_AUTH_TOKEN", "env-auth-token")
	os.Setenv("VPN_PROBE_READ_TOKEN", "env-read-token")
	os.Setenv("VPN_PROBE_ADMIN_TOKEN", "env-admin-token")
	defer func() {
		os.Unsetenv("VPN_PROBE_AUTH_TOKEN")
		os.Unsetenv("VPN_PROBE_READ_TOKEN")
		os.Unsetenv("VPN_PROBE_ADMIN_TOKEN")
	}()

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Server.AuthToken != "env-auth-token" {
		t.Errorf("expected AuthToken 'env-auth-token', got '%s'", cfg.Server.AuthToken)
	}
	if cfg.Server.ReadToken != "env-read-token" {
		t.Errorf("expected ReadToken 'env-read-token', got '%s'", cfg.Server.ReadToken)
	}
	if cfg.Server.AdminToken != "env-admin-token" {
		t.Errorf("expected AdminToken 'env-admin-token', got '%s'", cfg.Server.AdminToken)
	}
}

func TestLoadConfigResidentialSourceEnvAndYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
monitor:
  enabled: true
  residential_source:
    enabled: true
    url: "http://192.168.8.11:3338/api/lazycat/egress-subscriptions?instance_id=11433&format=json&only_mapped=true"
    token: "yaml-token"
    refresh_interval: "3m"
    cache_path: "/tmp/resi.json"
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Monitor.ResidentialSource.Enabled {
		t.Fatalf("expected residential_source.enabled=true")
	}
	if cfg.Monitor.ResidentialSource.Token != "yaml-token" {
		t.Fatalf("expected token 'yaml-token', got %q", cfg.Monitor.ResidentialSource.Token)
	}
	if cfg.Monitor.ResidentialSource.RefreshInterval != "3m" {
		t.Fatalf("expected refresh_interval '3m', got %q", cfg.Monitor.ResidentialSource.RefreshInterval)
	}

	os.Setenv("VPN_PROBE_RESIDENTIAL_SOURCE_TOKEN", "env-resi-token")
	os.Setenv("VPN_PROBE_RESIDENTIAL_SOURCE_INTERVAL", "10m")
	defer func() {
		os.Unsetenv("VPN_PROBE_RESIDENTIAL_SOURCE_TOKEN")
		os.Unsetenv("VPN_PROBE_RESIDENTIAL_SOURCE_INTERVAL")
	}()

	cfg2, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config with env: %v", err)
	}
	if cfg2.Monitor.ResidentialSource.Token != "env-resi-token" {
		t.Fatalf("expected env override token 'env-resi-token', got %q", cfg2.Monitor.ResidentialSource.Token)
	}
	if cfg2.Monitor.ResidentialSource.RefreshInterval != "10m" {
		t.Fatalf("expected env override interval '10m', got %q", cfg2.Monitor.ResidentialSource.RefreshInterval)
	}
}

func TestSaveConfigAtomicAndBackup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "test-config.yaml")
	cfg := DefaultConfig()
	cfg.Server.Port = 9999

	// 首次保存
	if err := SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// 验证文件存在且内容正确
	loaded, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if loaded.Server.Port != 9999 {
		t.Errorf("expected port 9999, got %d", loaded.Server.Port)
	}

	// 二次修改保存 (应生成 .bak)
	cfg.Server.Port = 8888
	if err := SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	bakPath := cfgPath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("expected backup file %s to exist", bakPath)
	}
}
