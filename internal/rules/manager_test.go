package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateJSSyntax(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rules-test-*")
	if err != nil {
		t.Fatalf("create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	rulePath := filepath.Join(tmpDir, "rules.json")
	mgr, err := NewManager(rulePath)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	js := mgr.GenerateJS()
	if js == "" {
		t.Fatalf("expected non-empty JS string")
	}

	// 1. 结构与关键字完整性校验
	expectedSnippets := []string{
		"function operator",
		"function transformConfig",
		"return transformConfig(config)",
		"config.dns",
		"ashanzzz.com",
		"1992004.xyz",
	}

	for _, s := range expectedSnippets {
		if !strings.Contains(js, s) {
			t.Errorf("generated JS missing expected snippet: %s", s)
		}
	}

	// 2. 括号与大括号配对校验
	openBraces := strings.Count(js, "{")
	closeBraces := strings.Count(js, "}")
	if openBraces != closeBraces {
		t.Errorf("mismatched braces: open=%d, close=%d", openBraces, closeBraces)
	}

	openParens := strings.Count(js, "(")
	closeParens := strings.Count(js, ")")
	if openParens != closeParens {
		t.Errorf("mismatched parentheses: open=%d, close=%d", openParens, closeParens)
	}

	// 3. 若系统安装了 node，执行真实语法校验 (node -c)
	if nodePath, err := exec.LookPath("node"); err == nil {
		jsFile := filepath.Join(tmpDir, "test.js")
		if err := os.WriteFile(jsFile, []byte(js), 0644); err == nil {
			cmd := exec.Command(nodePath, "-c", jsFile)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node syntax check failed: %v\nOutput: %s", err, string(out))
			}
		}
	}
}

func TestManagerRuleUpdates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rules-test-*")
	if err != nil {
		t.Fatalf("create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	rulePath := filepath.Join(tmpDir, "rules.json")
	mgr, err := NewManager(rulePath)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := mgr.AddDirectDomain("test.example.com"); err != nil {
		t.Fatalf("add direct domain: %v", err)
	}

	cfg := mgr.GetConfig()
	found := false
	for _, d := range cfg.DirectDomains {
		if d == "test.example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected direct domain 'test.example.com' to be present")
	}

	js := mgr.GenerateJS()
	if !strings.Contains(js, "test.example.com") {
		t.Fatalf("expected generated JS to contain 'test.example.com'")
	}
}
