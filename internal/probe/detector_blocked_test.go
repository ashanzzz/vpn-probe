package probe

import (
	"testing"
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
			name:           "家宽穿透掉线: 宿主机Ping通但内外端口不通 (家庭设备关机或反向穿透中断)",
			port:           40985,
			protocol:       "socks5",
			domesticTCPOK:  false,
			abroadOK:       false,
			pingOK:         true,
			isResi:         true,
			wantFault:      model.FaultResidentialTunnelOffline,
			wantStatusText: "【家宽穿透掉线】家庭设备离线或反向穿透隧道中断",
			wantActionType: "CHECK_RESIDENTIAL_DEVICE_OR_TUNNEL",
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
			wantStatusText: "【中转离线】家宽中转宿主机宕机或网络中断",
			wantActionType: "CHECK_TRANSIT_RELAY_HOST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			faultType, statusText, actionType, tip := EvaluateFault(
				"156.231.113.91", tt.port, tt.protocol, tt.domesticTCPOK, tt.abroadOK, tt.pingOK, "peer:JP-Tokyo-Softbank", tt.isResi,
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
