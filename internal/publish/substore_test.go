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
