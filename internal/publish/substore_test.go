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
