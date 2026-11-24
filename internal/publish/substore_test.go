package publish

import (
	"strings"
	"testing"
)

func TestValidateYAMLWithMembers_CycleDetection(t *testing.T) {
	// 1. Two-node cycle: 选择代理 <-> 外部分组
	cyclicYAML := []byte(`
proxies:
  - name: "Node1"
  - name: "Node2"
proxy-groups:
  - name: "选择代理"
    proxies: ["外部分组", "Node1"]
  - name: "外部分组"
    proxies: ["选择代理", "Node2"]
`)

	err := ValidateYAMLWithMembers(cyclicYAML, nil, nil, false)
	if err == nil {
		t.Fatalf("expected cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "loop is detected in ProxyGroup") {
		t.Fatalf("expected 'loop is detected in ProxyGroup' in error message, got: %v", err)
	}

	// 2. Three-node cycle: A -> B -> C -> A
	threeNodeCyclicYAML := []byte(`
proxies:
  - name: "Node1"
proxy-groups:
  - name: "GroupA"
    proxies: ["GroupB"]
  - name: "GroupB"
    proxies: ["GroupC"]
  - name: "GroupC"
    proxies: ["GroupA", "Node1"]
`)
	err = ValidateYAMLWithMembers(threeNodeCyclicYAML, nil, nil, false)
	if err == nil {
		t.Fatalf("expected 3-node cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "loop is detected in ProxyGroup") {
		t.Fatalf("expected 'loop is detected in ProxyGroup', got: %v", err)
	}

	// 3. Valid acyclic DAG
	validYAML := []byte(`
proxies:
  - name: "Node1"
  - name: "Node2"
proxy-groups:
  - name: "自建-线路机"
    proxies: ["Node1"]
  - name: "外部分组"
    proxies: ["Node2"]
  - name: "选择代理"
    proxies: ["外部分组", "自建-线路机", "Node1"]
  - name: "AI服务"
    proxies: ["选择代理", "Node1"]
`)
	err = ValidateYAMLWithMembers(validYAML, []string{"选择代理", "AI服务"}, nil, false)
	if err != nil {
		t.Fatalf("expected valid acyclic DAG to pass, got error: %v", err)
	}
}

func TestValidateYAMLWithMembers_QFileResidentialGuard(t *testing.T) {
	resiYAML := []byte(`
proxies:
  - name: "LZ-US-Pavlov-家宽"
proxy-groups:
  - name: "选择代理"
    proxies: ["LZ-US-Pavlov-家宽"]
`)
	err := ValidateYAMLWithMembers(resiYAML, nil, nil, true)
	if err == nil {
		t.Fatalf("expected Q file to reject residential proxy, got nil")
	}
	if !strings.Contains(err.Error(), "contains residential proxy") {
		t.Fatalf("expected 'contains residential proxy' in error, got: %v", err)
	}
}

func TestValidateYAMLWithMembers_UnsupportedProtocolAndDirtyGroups(t *testing.T) {
	anytlsYAML := []byte(`
proxies:
  - name: "HK 香港01-test"
    type: "anytls"
proxy-groups:
  - name: "选择代理"
    proxies: ["HK 香港01-test"]
`)
	err := ValidateYAMLWithMembers(anytlsYAML, nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "unsupported proxy type") {
		t.Fatalf("expected unsupported proxy type error, got: %v", err)
	}

	dupYAML := []byte(`
proxies:
  - name: "Node1"
    type: "vless"
  - name: "Node1"
    type: "vless"
proxy-groups:
  - name: "选择代理"
    proxies: ["Node1"]
`)
	err = ValidateYAMLWithMembers(dupYAML, nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "duplicate proxy name") {
		t.Fatalf("expected duplicate proxy name error, got: %v", err)
	}

	dirtyInCleanYAML := []byte(`
proxies:
  - name: "Node1"
    type: "vless"
proxy-groups:
  - name: "自建-送中"
    proxies: ["Node1"]
  - name: "谷歌服务"
    proxies: ["自建-送中", "Node1"]
`)
	err = ValidateYAMLWithMembers(dirtyInCleanYAML, nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "illegally contains dirty leak group") {
		t.Fatalf("expected dirty leak group error, got: %v", err)
	}
}

func TestPatchFile_PurgesLegacyPollutedOperators(t *testing.T) {
	mockFile := map[string]any{
		"name": "Ashan",
		"process": []any{
			map[string]any{"id": "conv-1", "customName": "覆写规则与自动分组"},
			map[string]any{"id": "ashan_patch_v150", "customName": "送中与不送中双轨分类"},
			map[string]any{"id": "classify_songzhong_v1", "customName": "送中与不送中双轨分类"},
			map[string]any{"id": "Script Operator", "customName": managedOperator},
		},
	}

	patched := patchFile(mockFile, "// new script")
	ops, ok := patched["process"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", patched["process"])
	}

	if len(ops) != 2 {
		t.Fatalf("expected exactly 2 operators after purging legacy ops, got %d: %v", len(ops), ops)
	}

	if ops[0]["customName"] != "覆写规则与自动分组" {
		t.Fatalf("expected first operator to be 覆写规则与自动分组, got %v", ops[0])
	}
	if ops[1]["customName"] != managedOperator {
		t.Fatalf("expected second operator to be %s, got %v", managedOperator, ops[1])
	}
}

func TestValidateYAMLWithProfile_BusinessCommercialAirportGuard(t *testing.T) {
	// Case 1: Business profile with commercial airport node (should fail)
	airportYAML := []byte(`
proxies:
  - name: "TAG-香港01-IEPL"
    type: "vless"
  - name: "US-LAX-ACCK-4837-Reality"
    type: "vless"
proxy-groups:
  - name: "选择代理"
    proxies: ["US-LAX-ACCK-4837-Reality", "TAG-香港01-IEPL"]
`)
	err := ValidateYAMLWithProfile(airportYAML, nil, nil, "business")
	if err == nil {
		t.Fatalf("expected business profile to reject commercial airport node, got nil")
	}
	if !strings.Contains(err.Error(), "illegally contains commercial airport node") {
		t.Fatalf("expected 'illegally contains commercial airport node' in error, got: %v", err)
	}

	// Case 2: Business profile with pure self-built nodes (should pass)
	pureBizYAML := []byte(`
proxies:
  - name: "US-LAX-ACCK-4837-Reality"
    type: "vless"
  - name: "JP-Tokyo-IIJ-Reality"
    type: "vless"
  - name: "LZ-US-Pavlov-家宽"
    type: "vless"
proxy-groups:
  - name: "自建-线路机"
    proxies: ["US-LAX-ACCK-4837-Reality", "JP-Tokyo-IIJ-Reality"]
  - name: "自建-美国家宽"
    proxies: ["LZ-US-Pavlov-家宽"]
  - name: "选择代理"
    proxies: ["自建-线路机", "自建-美国家宽"]
`)
	err = ValidateYAMLWithProfile(pureBizYAML, []string{"自建-线路机", "自建-美国家宽", "选择代理"}, nil, "business")
	if err != nil {
		t.Fatalf("expected pure self-built nodes to pass in business profile, got: %v", err)
	}
}

func TestValidateYAMLWithProfile_AshanAndQProfile(t *testing.T) {
	// Case 1: Q profile rejects residential
	resiYAML := []byte(`
proxies:
  - name: "LZ-US-Pavlov-家宽"
    type: "vless"
proxy-groups:
  - name: "选择代理"
    proxies: ["LZ-US-Pavlov-家宽"]
`)
	if err := ValidateYAMLWithProfile(resiYAML, nil, nil, "q"); err == nil {
		t.Fatalf("expected Q profile to reject residential, got nil")
	}

	// Case 2: Ashan profile accepts residential and airport
	mixedYAML := []byte(`
proxies:
  - name: "TAG-香港01"
    type: "vless"
  - name: "LZ-US-Pavlov-家宽"
    type: "vless"
proxy-groups:
  - name: "选择代理"
    proxies: ["TAG-香港01", "LZ-US-Pavlov-家宽"]
`)
	if err := ValidateYAMLWithProfile(mixedYAML, []string{"选择代理"}, nil, "ashan"); err != nil {
		t.Fatalf("expected ashan profile to accept mixed nodes, got: %v", err)
	}
}
