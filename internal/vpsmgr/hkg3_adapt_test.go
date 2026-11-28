package vpsmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vpn-probe/internal/config"
)

func TestNeedsHub(t *testing.T) {
	cases := []struct {
		name string
		inst *VPSInstance
		want bool
	}{
		{"nil", nil, false},
		{"exec mode hub", &VPSInstance{ExecMode: "hub", SSHPort: 22}, true},
		{"lazycat without ssh port", &VPSInstance{ProviderID: "lazycat", SSHPort: 0}, true},
		{"lazycat with ssh port", &VPSInstance{ProviderID: "lazycat", SSHPort: 22022}, false},
		{"normal vps", &VPSInstance{ProviderID: "acck", SSHPort: 22}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsHub(tc.inst); got != tc.want {
				t.Fatalf("NeedsHub() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExecModeFor(t *testing.T) {
	if got := execModeFor("lazycat", 0); got != "hub" {
		t.Errorf("lazycat without ssh port must use hub, got %q", got)
	}
	if got := execModeFor("lazycat", 22022); got != "ssh" {
		t.Errorf("lazycat with mapped ssh port should use ssh, got %q", got)
	}
	if got := execModeFor("acck", 22); got != "ssh" {
		t.Errorf("acck must use ssh, got %q", got)
	}
}

func TestServiceRunningFromStatus(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{" * status: started", true},
		{"started", true},
		{"active", true},
		{"inactive", false},
		{" * status: stopped", false},
		{"", false},
		{"failed", false},
	}
	for _, tc := range cases {
		if got := serviceRunningFromStatus(tc.out); got != tc.want {
			t.Errorf("serviceRunningFromStatus(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}

func TestParseHubExecResponse(t *testing.T) {
	out, err := ParseHubExecResponse([]byte(`{"status":"success","output":"ok\n","exit_code":0}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok\n" {
		t.Errorf("output = %q", out)
	}

	out, err = ParseHubExecResponse([]byte(`{"status":"failed","output":"boom","exit_code":3}`))
	if err == nil {
		t.Fatal("expected error on non-zero exit")
	}
	if out != "boom" {
		t.Errorf("output must be preserved for diagnosis, got %q", out)
	}

	if _, err := ParseHubExecResponse([]byte("not-json")); err == nil {
		t.Fatal("expected error on invalid json")
	}
}

func TestNewHubExecutor_LoginAndExec(t *testing.T) {
	var loginCalls atomic.Int32
	var sawAuth atomic.Value
	sawAuth.Store("")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok-1"})
		case "/api/lazycat/terminal/exec":
			sawAuth.Store(r.Header.Get("Authorization"))
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["instance_id"] != "10674" {
				t.Errorf("instance_id = %v, want 10674", body["instance_id"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "output": "sing-box version 1.11.15", "exit_code": 0,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	exec := NewHubExecutor(server.URL, "admin", "secret")
	out, err := exec(context.Background(), "10674", "sing-box version", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "sing-box version 1.11.15" {
		t.Errorf("unexpected output: %q", out)
	}
	if sawAuth.Load().(string) != "Bearer tok-1" {
		t.Errorf("exec request must carry bearer token, got %q", sawAuth.Load())
	}
	if loginCalls.Load() != 1 {
		t.Errorf("expected 1 login, got %d", loginCalls.Load())
	}
}

func TestNewHubExecutor_RetriesOnUnauthorized(t *testing.T) {
	var loginCalls atomic.Int32
	var execCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			n := loginCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok-" + string(rune('0'+n))})
		case "/api/lazycat/terminal/exec":
			if execCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "output": "ok", "exit_code": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	exec := NewHubExecutor(server.URL, "admin", "secret")
	if _, err := exec(context.Background(), "10674", "echo ok", 10*time.Second); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if loginCalls.Load() != 2 {
		t.Errorf("expected 2 logins (initial + after 401), got %d", loginCalls.Load())
	}
}

// 懒猫云固定出口实例必须被纳管，全球家宽必须继续跳过。
func TestRefreshFromVPSHub_IncludesFixedExitLazycat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/instances":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"instances": []map[string]any{
					{
						"id": "10674", "name": "HKG 0.1折", "ip": "216.236.60.76",
						"ssh_port": 0, "ssh_user": "root", "provider_id": "lazycat",
						"status": "running", "region": "香港", "category": "固定出口",
					},
					{
						"id": "11433", "name": "全球家宽大杯02", "ip": "167.104.167.151",
						"ssh_port": 0, "ssh_user": "root", "provider_id": "lazycat",
						"status": "running", "region": "日本", "category": "全球家宽",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	m := NewManager(&config.Config{}, nil, "")
	m.SetInstancesForTest(nil)
	m.SetVPSHubConfig(server.URL, "admin", "admin")
	m.refreshFromVPSHub()

	m.instancesMu.RLock()
	defer m.instancesMu.RUnlock()

	var found10674, found11433 bool
	for _, inst := range m.instances {
		switch inst.ID {
		case "10674":
			found10674 = true
			if inst.SSHPort != 0 {
				t.Errorf("fixed-exit lazycat must keep ssh_port=0, got %d", inst.SSHPort)
			}
			if inst.ExecMode != "hub" {
				t.Errorf("fixed-exit lazycat must use hub exec mode, got %q", inst.ExecMode)
			}
			if !inst.IsNAT {
				t.Error("lazycat instance must be marked IsNAT")
			}
		case "11433":
			found11433 = true
		}
	}
	if !found10674 {
		t.Fatal("fixed-exit lazycat instance 10674 must be managed")
	}
	if found11433 {
		t.Fatal("residential lazycat instance 11433 must still be skipped")
	}
}

func TestReplaceURLPort(t *testing.T) {
	raw := "vless://uuid-1@216.236.60.76:30021?security=reality&sni=www.apple.com&pbk=PUB&sid=ab&flow=xtls-rprx-vision#HK-HKG3-NAT"
	got := replaceURLPort(raw, 41088)
	if !strings.Contains(got, "@216.236.60.76:41088?") {
		t.Fatalf("port not replaced correctly: %s", got)
	}
	if !strings.Contains(got, "sid=ab") || !strings.HasSuffix(got, "#HK-HKG3-NAT") {
		t.Fatalf("query/fragment must be preserved: %s", got)
	}
	if strings.Contains(got, ":30021") {
		t.Fatalf("old port must be gone: %s", got)
	}
}

func TestReplaceURLPort_InvalidInputUnchanged(t *testing.T) {
	if got := replaceURLPort("not a url", 1234); got != "not a url" {
		t.Fatalf("invalid url must be returned unchanged, got %q", got)
	}
}
