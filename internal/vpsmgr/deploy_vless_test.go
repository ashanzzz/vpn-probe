package vpsmgr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
)

func TestValidateAndBuildSafeTransitNodeName(t *testing.T) {
	if err := ValidateTransitNodeName("HK-HKG3-NAT"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	for _, bad := range []string{"", "LZ-HK-NAT", "香港家宽-NAT", "HK-Global-NAT", "US-Residential"} {
		if err := ValidateTransitNodeName(bad); err == nil {
			t.Errorf("expected name %q to be rejected", bad)
		}
	}

	got := BuildSafeTransitNodeName(&VPSInstance{
		ID:         "10674",
		Name:       "HKG 0.1折",
		Region:     "香港",
		ProviderID: "lazycat",
		IsNAT:      true,
	})
	if got != "HK-HKG-NAT" {
		t.Errorf("BuildSafeTransitNodeName() = %q, want HK-HKG-NAT", got)
	}
}

func TestDeployVLESS_HubNative_ReusesExistingPortAndOnboards(t *testing.T) {
	var sawInstall bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/lazycat/ports":
			_ = json.NewEncoder(w).Encode(HubPortsResponse{
				PortLimit: 10,
				Total:     1,
				Ports:     []HubPortMapping{{ID: 50285, Protocol: "tcp", PrivatePort: 30021, PublicPort: 30021}},
			})
		case "/api/lazycat/terminal/exec":
			var body struct {
				Command string `json:"command"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			out := "ok"
			switch {
			case strings.Contains(body.Command, "ss -tlnp"):
				out = ""
			case strings.Contains(body.Command, "uname -m"):
				out = "x86_64\n"
			case strings.Contains(body.Command, "rc-service"):
				sawInstall = true
				decodedMatch := false
				if idx := strings.Index(body.Command, "printf '%s' '"); idx != -1 {
					rest := body.Command[idx+len("printf '%s' '"):]
					if endIdx := strings.Index(rest, "'"); endIdx != -1 {
						if raw, decErr := base64.StdEncoding.DecodeString(rest[:endIdx]); decErr == nil {
							s := string(raw)
							decodedMatch = strings.Contains(s, `"listen_port": 30021`) && strings.Contains(s, `"aws.amazon.com"`)
						}
					}
				}
				if !decodedMatch {
					t.Errorf("expected reused port 30021 and SNI aws.amazon.com in install config, got cmd: %s", body.Command)
				}
				out = "UUID=uuid-1\nPUBLIC_KEY=PUB1\n"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "output": out, "exit_code": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(cfgFile, []byte("monitor:\n  nodes: []\n"), 0644)

	det := NewDetector(nil)
	det.SetHubClient(NewHubClient(server.URL, "admin", "pass"))
	det.SetHubExecutor(NewHubExecutor(server.URL, "admin", "pass"))

	m := NewManager(&config.Config{}, nil, cfgFile)
	m.SetVPSHubConfig(server.URL, "admin", "pass")
	m.SetDetectorForTest(det)
	m.SetInstancesForTest([]*VPSInstance{{
		ID: "10674", Name: "HKG 0.1折", IP: "216.236.60.76",
		ProviderID: "lazycat", Category: "固定出口", Region: "香港", IsNAT: true, ExecMode: "hub",
	}})

	var onboarded config.MonitoredNode
	m.SetOnNodeAdded(func(n config.MonitoredNode) {
		onboarded = n
	})
	m.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		return &model.NodeProbeResult{Online: true, LatencyMs: 42, ExitIP: "216.236.60.76"}
	})

	res, err := m.DeployVLESS(context.Background(), &DeployVLESSRequest{
		VPSID:    "10674",
		NodeName: "HK-HKG3-NAT",
	})
	if err != nil {
		t.Fatalf("DeployVLESS failed: %v", err)
	}
	if !sawInstall || res.ListenPort != 30021 || res.PublicPort != 30021 {
		t.Fatalf("unexpected deploy port/install result: sawInstall=%v res=%+v", sawInstall, res)
	}
	if !res.Success || res.RetriedNAT || !res.ConfigSynced || !res.MonitorSynced {
		t.Fatalf("unexpected deploy response: %+v", res)
	}
	if !strings.HasSuffix(res.VLESSURL, "#HK-HKG3-NAT") || !strings.Contains(res.VLESSURL, "pbk=PUB1") {
		t.Errorf("unexpected VLESS URL: %s", res.VLESSURL)
	}
	if onboarded.Name != "HK-HKG3-NAT" || onboarded.URL != res.VLESSURL {
		t.Errorf("onboarded node mismatch: %+v", onboarded)
	}
	rawCfg, _ := os.ReadFile(cfgFile)
	if !strings.Contains(string(rawCfg), "HK-HKG3-NAT") || !strings.Contains(string(rawCfg), "216.236.60.76:30021") {
		t.Errorf("config.yaml was not updated: %s", string(rawCfg))
	}
}

func TestDeployVLESS_HubNative_SmokeFailRetriesNATPort(t *testing.T) {
	var repubCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/lazycat/ports":
			if r.Method == http.MethodPost {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "public_port": 30021})
				return
			}
			_ = json.NewEncoder(w).Encode(HubPortsResponse{
				PortLimit: 10,
				Total:     1,
				Ports:     []HubPortMapping{{ID: 50285, Protocol: "tcp", PrivatePort: 30021, PublicPort: 30021}},
			})
		case "/api/lazycat/terminal/exec":
			var body struct {
				Command string `json:"command"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			out := "ok"
			switch {
			case strings.Contains(body.Command, "uname -m"):
				out = "x86_64\n"
			case strings.Contains(body.Command, "rc-service"):
				out = "UUID=uuid-1\nPUBLIC_KEY=PUB1\n"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "output": out, "exit_code": 0})
		case "/api/lazycat/ports/republish":
			repubCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"new_public_port": 41888, "new_mapping_id": 60123})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	det := NewDetector(nil)
	det.SetHubClient(NewHubClient(server.URL, "admin", "pass"))
	det.SetHubExecutor(NewHubExecutor(server.URL, "admin", "pass"))
	m := NewManager(&config.Config{}, nil, "")
	m.SetDetectorForTest(det)
	m.SetInstancesForTest([]*VPSInstance{{
		ID: "10674", Name: "HKG 0.1折", IP: "216.236.60.76",
		ProviderID: "lazycat", Category: "固定出口", Region: "香港", IsNAT: true, ExecMode: "hub",
	}})

	m.SetProbeFunc(func(ctx context.Context, link string) *model.NodeProbeResult {
		if strings.Contains(link, ":30021") {
			return &model.NodeProbeResult{Online: false}
		}
		return &model.NodeProbeResult{Online: true, LatencyMs: 55, ExitIP: "216.236.60.76"}
	})

	res, err := m.DeployVLESS(context.Background(), &DeployVLESSRequest{VPSID: "10674", NodeName: "HK-HKG3-NAT"})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if repubCalls.Load() != 1 || !res.RetriedNAT || res.PublicPort != 41888 || !strings.Contains(res.VLESSURL, ":41888") {
		t.Fatalf("unexpected retry result: %+v", res)
	}
}

func TestDeployVLESS_RejectsResidentialLazycat(t *testing.T) {
	m := NewManager(&config.Config{}, nil, "")
	m.SetInstancesForTest([]*VPSInstance{{
		ID: "11433", Name: "全球家宽大杯02", IP: "167.104.167.151",
		ProviderID: "lazycat", Category: "全球家宽", IsNAT: true, ExecMode: "hub",
	}})
	if _, err := m.DeployVLESS(context.Background(), &DeployVLESSRequest{VPSID: "11433"}); err == nil {
		t.Fatal("expected residential lazycat instance to be rejected")
	}
}

func TestDeployVLESS_SSHNativeFallback(t *testing.T) {
	det := NewDetector(nil)
	var sawNative bool
	det.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		if strings.Contains(cmd, "NO_DOCKER") {
			return "NO_DOCKER\nx86_64\n", nil
		}
		if strings.Contains(cmd, "rc-service") {
			sawNative = true
			return "UUID=u-native\nPUBLIC_KEY=PUB_NATIVE\n", nil
		}
		return "ok", nil
	})
	m := NewManager(&config.Config{}, nil, "")
	m.SetDetectorForTest(det)
	m.SetInstancesForTest([]*VPSInstance{{
		ID: "vps-99", Name: "JP-Tokyo", IP: "1.2.3.4", SSHPort: 22, SSHUser: "root", Region: "日本",
	}})

	res, err := m.DeployVLESS(context.Background(), &DeployVLESSRequest{
		VPSID: "vps-99", ListenPort: 24445, SkipSmoke: true,
	})
	if err != nil {
		t.Fatalf("SSH native deploy failed: %v", err)
	}
	if !sawNative || res.DeployMode != "ssh-native" || !strings.Contains(res.VLESSURL, "u-native@1.2.3.4:24445") {
		t.Fatalf("unexpected SSH native result: %+v", res)
	}
}

func TestHealNATPublicPort_SyncsConfigAndResolvesPrivatePort(t *testing.T) {
	var seenPrivate, seenOldPub int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/lazycat/ports":
			_ = json.NewEncoder(w).Encode(HubPortsResponse{
				PortLimit: 10,
				Total:     1,
				Ports:     []HubPortMapping{{ID: 50285, Protocol: "tcp", PrivatePort: 30021, PublicPort: 41000}},
			})
		case "/api/lazycat/ports/republish":
			var body struct {
				PrivatePort   int `json:"private_port"`
				OldPublicPort int `json:"old_public_port"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			seenPrivate, seenOldPub = body.PrivatePort, body.OldPublicPort
			_ = json.NewEncoder(w).Encode(map[string]any{"new_public_port": 42000, "new_mapping_id": 50999})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	oldURL := "vless://uuid-1@216.236.60.76:41000?security=reality#HK-HKG3-NAT"
	_ = os.WriteFile(cfgFile, []byte("monitor:\n  nodes:\n    - name: HK-HKG3-NAT\n      url: \""+oldURL+"\"\n      is_self: true\n"), 0644)

	det := NewDetector(nil)
	det.SetHubClient(NewHubClient(server.URL, "admin", "pass"))
	healer := NewAutoHealer(det, nil, cfgFile)

	var hotUpdatedTo string
	healer.SetOnURLUpdated(func(oldU, newU string) {
		hotUpdatedTo = newU
	})

	inst := &VPSInstance{ID: "10674", IP: "216.236.60.76", ProviderID: "lazycat", IsNAT: true, Category: "固定出口"}
	resp, err := healer.ExecutePortHeal(context.Background(), inst, "HK-HKG3-NAT", oldURL, 0)
	if err != nil {
		t.Fatalf("ExecutePortHeal failed: %v", err)
	}
	if seenPrivate != 30021 || seenOldPub != 41000 {
		t.Errorf("expected private=30021, oldPub=41000, got private=%d oldPub=%d", seenPrivate, seenOldPub)
	}
	if !resp.Success || !resp.ConfigSync || resp.NewPort != 42000 {
		t.Fatalf("unexpected heal response: %+v", resp)
	}
	if !strings.Contains(hotUpdatedTo, ":42000") {
		t.Errorf("scheduler callback was not invoked with new port: %q", hotUpdatedTo)
	}
	rawCfg, _ := os.ReadFile(cfgFile)
	if !strings.Contains(string(rawCfg), ":42000") {
		t.Errorf("config.yaml not updated to 42000: %s", string(rawCfg))
	}
}

func TestRefreshFromVPSHub_IncludesAllNonResidentialWithoutPassword(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/instances":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"instances": []map[string]any{
					{"id": "23712", "name": "JP IIJ BGP", "ip": "156.231.113.91", "ssh_port": 22, "provider_id": "akile", "status": "running", "region": "日本"},
					{"id": "10674", "name": "HKG 0.1折", "ip": "216.236.60.76", "ssh_port": 0, "provider_id": "lazycat", "status": "running", "region": "香港", "category": "固定出口"},
					{"id": "11433", "name": "全球家宽大杯02", "ip": "167.104.167.151", "ssh_port": 0, "provider_id": "lazycat", "status": "running", "region": "日本", "category": "全球家宽"},
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
	if len(m.instances) != 2 {
		t.Fatalf("expected 2 managed instances (akile + lazycat fixed), got %d", len(m.instances))
	}
	for _, inst := range m.instances {
		if inst.ExecMode != "hub" {
			t.Errorf("instance %s must use hub exec mode, got %q", inst.ID, inst.ExecMode)
		}
	}
}

func TestInspectVPS_MultiAppsAndSubStoreSafeUpgrade(t *testing.T) {
	gh := NewGitHubChecker()
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "sing-box"):
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.14.0", "published_at": "2026-11-20T00:00:00Z"})
		case strings.Contains(r.URL.Path, "3x-ui"):
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v2.5.0", "published_at": "2026-11-20T00:00:00Z"})
		case strings.Contains(r.URL.Path, "Sub-Store"):
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v2.15.0", "published_at": "2026-11-20T00:00:00Z"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ghServer.Close()
	gh.SetBaseURLForTest(ghServer.URL)

	var restored bool
	var createdSnap string
	det := NewDetector(gh)
	det.SetSSHRunner(func(inst *VPSInstance, cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "docker ps"):
			return "sub-store|xream/sub-store:latest|Up 1 day\nsb-iij|ghcr.io/sagernet/sing-box:latest|Up 6 days\n3x-ui|ghcr.io/mhsanaei/3x-ui:latest|Up 1 day\n", nil
		case strings.Contains(cmd, "APP_VER_"):
			return "APP_VER_0:2.14.0\nAPP_VER_2:2.4.9\n", nil
		case strings.Contains(cmd, "sing-box version"):
			return "sing-box version 1.13.0", nil
		case strings.Contains(cmd, "docker inspect -f '{{range .Mounts}}"):
			return "/opt/1panel/apps/sub-store/data/sub-store.json", nil
		case strings.Contains(cmd, "cp /opt/1panel/apps/sub-store/data/sub-store.json "):
			fields := strings.Fields(cmd)
			if len(fields) >= 3 {
				createdSnap = fields[2]
			}
			return "ok", nil
		case strings.Contains(cmd, "stat -c '%n|%s|%y'"):
			snap := createdSnap
			if snap == "" {
				snap = "/opt/1panel/apps/sub-store/data/backups/config_20261128_120000.json"
			}
			return snap + "|1024|2026-11-28 17:00:00", nil
		case strings.Contains(cmd, "cp ") && strings.Contains(cmd, "docker restart sub-store"):
			restored = true
			return "ok", nil
		default:
			return "1024", nil
		}
	})

	m := NewManager(&config.Config{}, nil, "")
	m.SetDetectorForTest(det)
	m.SetInstancesForTest([]*VPSInstance{{ID: "23712", Name: "JP IIJ", IP: "156.231.113.91"}})
	m.SetSubStoreSmokeFunc(func(ctx context.Context) error {
		return errors.New("substore down after upgrade")
	})

	infos := m.RefreshVPSInstances(context.Background())
	if len(infos) != 1 || len(infos[0].ManagedApps) != 3 {
		t.Fatalf("expected 3 coexisting apps on 23712, got: %+v", infos)
	}

	resp, err := m.PerformSafeUpgrade(context.Background(), &SafeUpgradeRequest{
		VPSID:         "23712",
		Software:      "sub-store",
		ContainerName: "sub-store",
	})
	if err != nil {
		t.Fatalf("PerformSafeUpgrade returned unexpected error: %v", err)
	}
	if resp.Success || !resp.RolledBack || !restored {
		t.Fatalf("expected Sub-Store smoke failure to trigger automatic rollback, got resp=%+v restored=%v", resp, restored)
	}
}
