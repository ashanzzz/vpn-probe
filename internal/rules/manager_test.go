package rules

import (
	"net/http"
	"net/http/httptest"
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

func TestGetBridgedConvertJS(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(filepath.Join(tmpDir, "rules.json"))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	callCount := 0
	statusCode := 200
	responseBody := "// mock convert.min.js\nconst x = 1;"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(responseBody))
	}))
	defer server.Close()

	revert := SetConvertUpstreamURLForTest(server.URL)
	defer revert()

	// 1. 测试成功拉取与桥接注入
	res := mgr.GetBridgedConvertJS()
	if !strings.Contains(res, "mock convert.min.js") || !strings.Contains(res, "function operator") {
		t.Fatalf("expected bridged JS, got: %s", res)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// 2. 测试缓存命中 (不重复发起网络请求)
	resCached := mgr.GetBridgedConvertJS()
	if resCached != res {
		t.Fatalf("expected cached result to match")
	}
	if callCount != 1 {
		t.Fatalf("expected still 1 call due to cache, got %d", callCount)
	}

	// 3. 测试上游返回 500 时的优雅降级 (新 manager 无缓存)
	statusCode = 500
	responseBody = "Internal Server Error"
	mgr2, _ := NewManager(filepath.Join(tmpDir, "rules2.json"))
	resFallback := mgr2.GetBridgedConvertJS()
	if !strings.Contains(resFallback, "convert.js fetch fallback") {
		t.Fatalf("expected fallback JS when server returns 500, got: %s", resFallback)
	}
}
