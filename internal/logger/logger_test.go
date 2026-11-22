package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "",
			expected: "",
		},
		{
			input:    "vless://12345678-1234-1234-1234-123456789abc@1.2.3.4:443?type=tcp&security=reality#MyNode",
			expected: "vless://******@1.2.3.4:443?security=reality&type=tcp#MyNode",
		},
		{
			input:    "https://substore.example.com/api/sub?token=SecretToken123&name=test",
			expected: "https://substore.example.com/api/sub?name=test&token=******",
		},
		{
			input:    "http://admin:myPassword999@192.168.1.1:8080/data",
			expected: "http://admin:******@192.168.1.1:8080/data",
		},
	}

	for _, tc := range tests {
		got := MaskURL(tc.input)
		if strings.Contains(got, "myPassword999") || strings.Contains(got, "SecretToken123") {
			t.Errorf("MaskURL(%q) leaked sensitive credentials: %q", tc.input, got)
		}
	}
}

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
	if got := MaskSecret("abc"); got != "******" {
		t.Errorf("expected '******', got %q", got)
	}
	if got := MaskSecret("Woo@@@204317"); got != "W******7" {
		t.Errorf("expected 'W******7', got %q", got)
	}
}

func TestLevelString(t *testing.T) {
	if LevelDebug.String() != "DEBUG" {
		t.Errorf("expected DEBUG, got %s", LevelDebug.String())
	}
	if LevelInfo.String() != "INFO" {
		t.Errorf("expected INFO, got %s", LevelInfo.String())
	}
	if LevelWarn.String() != "WARN" {
		t.Errorf("expected WARN, got %s", LevelWarn.String())
	}
	if LevelError.String() != "ERROR" {
		t.Errorf("expected ERROR, got %s", LevelError.String())
	}
	if Level(999).String() != "INFO" {
		t.Errorf("expected default INFO, got %s", Level(999).String())
	}
}

func TestLoggerInitAndAudit(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	l, err := Init("debug", logPath)
	if err != nil {
		t.Fatalf("Init logger failed: %v", err)
	}

	l.Debugf("debug message: %s", "hello")
	l.Infof("info message: %s", "world")
	l.Warnf("warn message")
	l.Errorf("error message")
	l.Probe("probe message")
	l.Sync("sync message")

	// 记录审计日志
	l.Audit("test_action", "test_actor", "test_target", "success", "token=SensitiveToken456")

	// 验证日志文件生成
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "info message: world") {
		t.Errorf("expected log file to contain info message, got:\n%s", content)
	}

	// 验证审计日志生成
	auditPath := filepath.Join(tmpDir, "audit.log")
	auditData, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file failed: %v", err)
	}
	auditContent := string(auditData)
	if !strings.Contains(auditContent, "test_action") || !strings.Contains(auditContent, "test_actor") {
		t.Errorf("expected audit file to contain action & actor, got:\n%s", auditContent)
	}
	if strings.Contains(auditContent, "SensitiveToken456") {
		t.Errorf("audit log leaked SensitiveToken456: %s", auditContent)
	}
}
