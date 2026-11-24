package substore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vpn-probe/internal/config"
)

func TestExtractScriptFileName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://free.1992004.xyz/convert_openclash_v2.js?v=121#args=1", "convert_openclash_v2.js"},
		{"https://sub.example.com/scripts/custom_patch.js", "custom_patch.js"},
		{"// inline javascript\nfunction operator() { return 1; }", "inline_script.js"},
		{"function test() { var a = 1; }", "inline_script.js"},
	}

	for _, tc := range tests {
		got := extractScriptFileName(tc.input)
		if got != tc.expected {
			t.Errorf("extractScriptFileName(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestGetManifest_InlineScriptHandling(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/files"):
			files := map[string]any{
				"data": []map[string]any{
					{
						"name":        "ashan",
						"displayName": "Ashan 纯自建订阅",
						"sourceName":  "col-ashan",
						"type":        "mihomoConfig",
						"process": []map[string]any{
							{
								"id":         "op-remote",
								"customName": "覆写规则与自动分组",
								"disabled":   false,
								"args": map[string]any{
									"mode":    "link",
									"content": ts.URL + "/convert_openclash_v2.js?v=121",
									"arguments": map[string]any{
										"full": "true",
									},
								},
							},
							{
								"id":         "Script Operator",
								"customName": "vpn-probe:user-policy-groups",
								"disabled":   false,
								"args": map[string]any{
									"mode":    "code",
									"content": "// vpn-probe-managed user groups with dynamic alias resolution.\nconst vpnUserGroups = [];\nfunction operator(cfg) { return cfg; }",
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(files)
		case strings.HasSuffix(r.URL.Path, "/convert_openclash_v2.js"):
			_, _ = w.Write([]byte("/* convert_openclash_v2 content */"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := &config.SubStoreConfig{
		Enabled:    true,
		URL:        ts.URL,
		ShareToken: "13602004317",
	}
	client := NewClient(cfg)
	client.httpClient = ts.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manifest, err := client.GetManifest(ctx)
	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}

	if manifest.TotalShares != 1 {
		t.Fatalf("expected 1 share, got %d", manifest.TotalShares)
	}

	share := manifest.ShareLinks[0]
	if len(share.PipelineScripts) != 2 {
		t.Fatalf("expected 2 pipeline scripts, got %d", len(share.PipelineScripts))
	}

	// 1. Check remote operator
	remoteOp := share.PipelineScripts[0]
	if remoteOp.ScriptFile != "convert_openclash_v2.js" {
		t.Errorf("expected remote script file 'convert_openclash_v2.js', got %q", remoteOp.ScriptFile)
	}
	if remoteOp.Version != "121" {
		t.Errorf("expected version '121', got %q", remoteOp.Version)
	}

	// 2. Check inline operator
	inlineOp := share.PipelineScripts[1]
	if inlineOp.ScriptFile != "vpn-probe:user-policy-groups" {
		t.Errorf("expected inline script file 'vpn-probe:user-policy-groups', got %q", inlineOp.ScriptFile)
	}

	// 3. Check manifest.Scripts
	if len(manifest.Scripts) != 2 {
		t.Errorf("expected exactly 2 scripts in manifest.Scripts, got %d: %v", len(manifest.Scripts), manifest.ScriptList)
	}

	inlineScript, ok := manifest.Scripts["vpn-probe:user-policy-groups"]
	if !ok {
		t.Fatalf("expected manifest.Scripts to contain 'vpn-probe:user-policy-groups'")
	}
	if !strings.Contains(inlineScript.Content, "vpn-probe-managed") {
		t.Errorf("expected inline script content to contain 'vpn-probe-managed', got %q", inlineScript.Content)
	}
	if inlineScript.URL != "inline" {
		t.Errorf("expected URL 'inline', got %q", inlineScript.URL)
	}

	// 4. Verify no junk keys with newlines or code fragments
	for k := range manifest.Scripts {
		if strings.Contains(k, "\n") || strings.Contains(k, "function") || strings.Contains(k, ";") {
			t.Errorf("found polluted script key with code fragment: %q", k)
		}
	}
}
