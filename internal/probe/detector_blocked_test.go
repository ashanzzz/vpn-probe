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
		wantFault      model.FaultType
		wantStatusText string
		wantActionType string
	}{
		{
			name:           "VLESS Reality 协议校验失败: 境内境外TCP都通，但代理报错",
			port:           33338,
			protocol:       "vless",
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			wantFault:      model.FaultProtocolMismatch,
			wantStatusText: "【配置错误】Reality公钥(pbk)、ShortID校验失败或缺少flow参数",
			wantActionType: "CHECK_REALITY_KEYS",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			faultType, statusText, actionType, tip := EvaluateFault(
				"156.231.113.91", tt.port, tt.protocol, tt.domesticTCPOK, tt.abroadOK, tt.pingOK, "peer:JP-Tokyo-Softbank",
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
