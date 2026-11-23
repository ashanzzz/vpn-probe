package probe

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
	"vpn-probe/internal/model"
)

func TestDiagnosisLogic(t *testing.T) {
	tests := []struct {
		name           string
		port           int
		protocol       string
		domesticTCPOK  bool
		abroadOK       bool
		pingOK         bool
		isResi         bool
		probeErr       error
		wantFault      model.FaultType
		wantStatusText string
		wantActionType string
	}{
		{
			name:           "VLESS Reality 协议校验失败: 境内境外TCP都通，但代理报错",
			port:           33338,
			protocol:       "vless-reality",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			wantFault:      model.FaultProtocolMismatch,
			wantStatusText: "【配置错误】Reality公钥(pbk)、ShortID校验失败或缺少flow参数",
			wantActionType: "CHECK_REALITY_KEYS",
		},
		{
			name:           "VLESS WebSocket 握手失败: 境内境外TCP都通，但WS协商报错",
			port:           443,
			protocol:       "vless-ws",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			wantFault:      model.FaultProtocolMismatch,
			wantStatusText: "【配置错误】WebSocket路径(path)、Host反代域名或UUID不匹配",
			wantActionType: "CHECK_WS_PATH_OR_UUID",
		},
		{
			name:           "Socks5 认证/白名单失败: 境内境外TCP都通，但SOCKS握手失败",
			port:           40981,
			protocol:       "socks5",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			wantFault:      model.FaultProtocolMismatch,
			wantStatusText: "【认证失败】SOCKS5账号密码错误或服务器设置了源IP白名单",
			wantActionType: "CHECK_SOCKS_AUTH_OR_WHITELIST",
		},
		{
			name:           "Shadowsocks 密码失败: 境内境外TCP都通，但SS协商失败",
			port:           24443,
			protocol:       "shadowsocks",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			wantFault:      model.FaultProtocolMismatch,
			wantStatusText: "【配置错误】Shadowsocks密码或加密算法不匹配",
			wantActionType: "CHECK_SS_PASSWORD",
		},
		{
			name:           "端口被墙: 海外通但国内TCP不通，且国内Ping通 (如 33338端口被墙)",
			port:           33338,
			protocol:       "vless",
			domesticTCPOK:  false,
			abroadOK:       true,
			pingOK:         true,
			wantFault:      model.FaultPortBlocked,
			wantStatusText: "33338端口被墙",
			wantActionType: "AI_CHANGE_PORT",
		},
		{
			name:           "IP被墙: 海外通但国内TCP与Ping全黑洞丢包",
			port:           33338,
			protocol:       "vless",
			domesticTCPOK:  false,
			abroadOK:       true,
			pingOK:         false,
			wantFault:      model.FaultIPBlocked,
			wantStatusText: "IP被墙",
			wantActionType: "CHANGE_IP_OR_CDN",
		},
		{
			name:           "国内通境外不通: 国内TCP通但境外探针不通 (不误判为机子关机或真正离线)",
			port:           33338,
			protocol:       "vless",
			domesticTCPOK:  true,
			abroadOK:       false,
			pingOK:         true,
			wantFault:      model.FaultNetworkCongestion,
			wantStatusText: "【境外阻断或单向异常】国内TCP连通但境外探测不可达",
			wantActionType: "CHECK_NETWORK_ROUTING",
		},
		{
			name:           "真正离线: 全球海外和国内都不通 (机子关机/软件崩溃/线路中断)",
			port:           33338,
			protocol:       "vless",
			domesticTCPOK:  false,
			abroadOK:       false,
			pingOK:         false,
			wantFault:      model.FaultMachineDown,
			wantStatusText: "【离线】机子关机/软件崩溃/线路中断",
			wantActionType: "CHECK_SERVER_AND_PROCESS",
		},
		{
			name:           "服务未运行: 全球海外和国内端口不通但国内Ping通 (主机在线/代理进程未启动或崩溃)",
			port:           33338,
			protocol:       "vless",
			domesticTCPOK:  false,
			abroadOK:       false,
			pingOK:         true,
			wantFault:      model.FaultMachineDown,
			wantStatusText: "【服务未运行】主机在线(Ping通)但端口无响应/进程异常",
			wantActionType: "CHECK_PROXY_PROCESS",
		},
		{
			name:           "家宽国内直连受阻: 境外隧道正常但国内直连受阻 (需专线中转，严禁换端口)",
			port:           40981,
			protocol:       "socks5",
			domesticTCPOK:  false,
			abroadOK:       true,
			pingOK:         true,
			isResi:         true,
			wantFault:      model.FaultResidentialDirectBlocked,
			wantStatusText: "【仅境外通】国内直连受阻/需经自建专线链式中继",
			wantActionType: "CHAIN_VIA_TRANSIT",
		},
		{
			name:           "家宽国内直连受阻(TCP握手成功但直连不可用): 境外隧道正常，无论国内TCP SYN是否握手成功，均判定为需链式中继",
			port:           40981,
			protocol:       "socks5",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			isResi:         true,
			wantFault:      model.FaultResidentialDirectBlocked,
			wantStatusText: "【仅境外通】国内直连受阻/需经自建专线链式中继",
			wantActionType: "CHAIN_VIA_TRANSIT",
		},
		{
			name:           "家宽穿透掉线: 宿主机Ping通但内外端口不通 (家庭设备关机或反向穿透中断)",
			port:           40985,
			protocol:       "socks5",
			domesticTCPOK:  false,
			abroadOK:       false,
			pingOK:         true,
			isResi:         true,
			wantFault:      model.FaultResidentialTunnelOffline,
			wantStatusText: "【家庭端掉线】家庭设备离线或连接中断",
			wantActionType: "CHECK_RESIDENTIAL_DEVICE",
		},
		{
			name:           "家宽中转宿主机宕机: 宿主机Ping与内外端口全灭",
			port:           40985,
			protocol:       "socks5",
			domesticTCPOK:  false,
			abroadOK:       false,
			pingOK:         false,
			isResi:         true,
			wantFault:      model.FaultTransitRelayDown,
			wantStatusText: "【中转离线】家宽中转服务器宕机或网络中断",
			wantActionType: "CHECK_TRANSIT_SERVER",
		},
		{
			name:           "家宽VLESS Reality直连配置错误: 境内外TCP均通，但Reality握手失败 (不误判为仅境外通)",
			port:           40981,
			protocol:       "vless-reality",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			isResi:         true,
			probeErr:       errors.New("reality verification failed"),
			wantFault:      model.FaultProtocolMismatch,
			wantStatusText: "【配置错误】Reality公钥(pbk)、ShortID校验失败或缺少flow参数",
			wantActionType: "CHECK_REALITY_KEYS",
		},
		{
			name:           "家宽应用层探测超时: 境内外TCP均通，但应用层超时 (不误判为Reality公钥错误)",
			port:           56190,
			protocol:       "vless-reality",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			isResi:         true,
			probeErr:       context.DeadlineExceeded,
			wantFault:      model.FaultResidentialTunnelOffline,
			wantStatusText: "【家庭端拥堵或未就绪】家宽隧道探测超时或丢包",
			wantActionType: "CHECK_RESIDENTIAL_DEVICE",
		},
		{
			name:           "家宽反向隧道连接被拒: 境内外TCP均通，但SOCKS应用层报 connection refused (家庭端隧道未建立或未就绪)",
			port:           48731,
			protocol:       "vless-reality",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			isResi:         true,
			probeErr:       errors.New("request failed: Get \"https://www.gstatic.com/generate_204\": socks connect tcp 127.0.0.1:34059->www.gstatic.com:443: unknown error connection refused"),
			wantFault:      model.FaultResidentialTunnelOffline,
			wantStatusText: "【家庭端未就绪或断开】家宽反向隧道未建立或连接被拒",
			wantActionType: "CHECK_RESIDENTIAL_DEVICE",
		},
		{
			name:           "家宽反向隧道重置与SOCKS服务失败: 境内外TCP均通，但报 general SOCKS server failure 与 connection reset by peer",
			port:           29475,
			protocol:       "vless-reality",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			isResi:         true,
			probeErr:       errors.New("request failed: Get \"https://www.gstatic.com/generate_204\": socks connect tcp 127.0.0.1:39187->www.gstatic.com:443: unknown error general SOCKS server failure | sing-box: connection reset by peer"),
			wantFault:      model.FaultResidentialTunnelOffline,
			wantStatusText: "【家庭端未就绪或断开】家宽反向隧道未建立或连接被拒",
			wantActionType: "CHECK_RESIDENTIAL_DEVICE",
		},
		{
			name:           "家宽反向隧道纯SOCKS服务失败: 境内外TCP均通，报 socks connect unknown error general SOCKS server failure (无sing-box日志附带)",
			port:           29475,
			protocol:       "vless-reality",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			isResi:         true,
			probeErr:       errors.New("request failed: Get \"https://www.gstatic.com/generate_204\": socks connect tcp 127.0.0.1:39187->www.gstatic.com:443: unknown error general SOCKS server failure"),
			wantFault:      model.FaultResidentialTunnelOffline,
			wantStatusText: "【家庭端未就绪或断开】家宽反向隧道未建立或连接被拒",
			wantActionType: "CHECK_RESIDENTIAL_DEVICE",
		},
		{
			name:           "自建VPS线路机探测超时: 境内外TCP均通，但应用层超时 (不误判为配置错误或端口被墙)",
			port:           36564,
			protocol:       "vless-reality",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			isResi:         false,
			probeErr:       errors.New("Client.Timeout exceeded while awaiting headers"),
			wantFault:      model.FaultNetworkCongestion,
			wantStatusText: "【网络拥塞超时】国内外TCP连通但应用层请求超时",
			wantActionType: "CHECK_NETWORK_ROUTING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			faultType, statusText, actionType, tip := EvaluateFault(
				"156.231.113.91", tt.port, tt.protocol, tt.domesticTCPOK, tt.abroadOK, tt.pingOK, "peer:JP-Tokyo-Softbank", tt.isResi, tt.probeErr,
			)

			if faultType != tt.wantFault {
				t.Errorf("FaultType = %v, want %v", faultType, tt.wantFault)
			}
			if statusText != tt.wantStatusText {
				t.Errorf("StatusText = %v, want %v", statusText, tt.wantStatusText)
			}
			if actionType != tt.wantActionType {
				t.Errorf("ActionType = %v, want %v", actionType, tt.wantActionType)
			}
			if tip == "" {
				t.Errorf("tip should not be empty")
			}
		})
	}
}

func TestCheckDomesticTCPContext_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	// Should return false immediately without blocking
	ok := CheckDomesticTCPContext(ctx, "127.0.0.1", 33333)
	if ok {
		t.Errorf("expected false for cancelled context, got true")
	}
}

func TestCheckDomesticPingContext_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	// Should return false immediately without running command
	ok := CheckDomesticPingContext(ctx, "127.0.0.1")
	if ok {
		t.Errorf("expected false for cancelled context, got true")
	}
}

func TestCheckAbroadViaProxy_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	// Should return error or false immediately without hanging or leaking
	ok, _, err := CheckAbroadViaProxy(ctx, "127.0.0.1:39999", "8.8.8.8", 53, 500*time.Millisecond)
	if ok {
		t.Errorf("expected false for pre-cancelled context, got true")
	}
	if err == nil {
		t.Errorf("expected error for pre-cancelled context, got nil")
	}
}

func TestMakeProxyHTTPClient_ContextPropagation(t *testing.T) {
	client, err := MakeProxyHTTPClient(39999, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create proxy client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://1.1.1.1", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	_, doErr := client.Do(req)
	if doErr == nil {
		t.Errorf("expected error with cancelled context, got nil")
	}
}
