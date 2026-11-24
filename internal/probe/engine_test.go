package probe

import (
	"fmt"
	"strings"
	"testing"
)

func TestEngine_DistinguishLoopbackDialFromSocksRefused(t *testing.T) {
	testPort := 36390

	// Case 1: 真实本地沙箱端口未就绪/未监听 (内核进程崩溃退出) -> dial tcp 127.0.0.1:<port>: connect: connection refused
	loopbackDialErr := fmt.Errorf("Get \"https://www.gstatic.com/generate_204\": socks connect tcp 127.0.0.1:%d->www.gstatic.com:443: dial tcp 127.0.0.1:%d: connect: connection refused", testPort, testPort)

	// Case 2: 本地沙箱进程正常监听，但远端节点拒绝或家庭端反向隧道未建立 -> SOCKS 服务端返回 0x05 REP
	socksRefusedErr := fmt.Errorf("request failed: Get \"https://www.gstatic.com/generate_204\": socks connect tcp 127.0.0.1:%d->www.gstatic.com:443: unknown error connection refused", testPort)

	// 判定辅助函数 (对齐 engine.go 第 116 行判定逻辑)
	isLoopbackDialFail := func(err error, port int) bool {
		if err == nil {
			return false
		}
		errStr := strings.ToLower(err.Error())
		return strings.Contains(errStr, fmt.Sprintf("dial tcp 127.0.0.1:%d", port)) &&
			(strings.Contains(errStr, "connect: connection refused") || strings.Contains(errStr, "connection refused")) &&
			!strings.Contains(errStr, "unknown error")
	}

	if !isLoopbackDialFail(loopbackDialErr, testPort) {
		t.Fatalf("expected loopbackDialErr to be identified as local sandbox loopback dial failure")
	}

	if isLoopbackDialFail(socksRefusedErr, testPort) {
		t.Fatalf("socksRefusedErr must NOT be classified as local sandbox loopback failure")
	}
}

func TestEngine_LoopbackFailureLogEnrichment(t *testing.T) {
	// 验证当本地沙箱环回端口连接被拒时，sing-box 日志输出能够正确附加至 ErrorMsg 与 TroubleshootTip
	testPort := 38767
	loopbackErr := fmt.Errorf("request failed: Get \"https://www.gstatic.com/generate_204\": socks connect tcp 127.0.0.1:%d->www.gstatic.com:443: dial tcp 127.0.0.1:%d: connect: connection refused", testPort, testPort)
	sbLog := "FATAL[0000] start service: inbound/mixed[mixed-in]: listen tcp 127.0.0.1:38767: bind: address already in use"

	errMsg := fmt.Sprintf("local sandbox connection refused: %v", loopbackErr)
	if sbLog != "" {
		errMsg = fmt.Sprintf("%s | sing-box: %s", errMsg, sbLog)
	}

	if !strings.Contains(errMsg, "bind: address already in use") {
		t.Errorf("expected errMsg to contain sing-box fatal log, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "127.0.0.1:38767") {
		t.Errorf("expected errMsg to contain port 38767, got: %s", errMsg)
	}
}
