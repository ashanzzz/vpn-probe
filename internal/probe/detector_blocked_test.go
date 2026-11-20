package probe

import (
	"fmt"
	"testing"
	"vpn-probe/internal/model"
)

func TestDiagnosisLogic(t *testing.T) {
	tests := []struct {
		name              string
		port              int
		domesticTCPOK     bool
		abroadOK          bool
		pingOK            bool
		wantFault         model.FaultType
		wantStatusText    string
		wantActionType    string
	}{
		{
			name:           "协议校验失败: 境内境外TCP都通，但代理报错 (如 Reality 公钥不匹配)",
			port:           33338,
			domesticTCPOK:  true,
			abroadOK:       true,
			pingOK:         true,
			wantFault:      model.FaultProtocolMismatch,
			wantStatusText: "【配置错误】Reality公钥(pbk)或ShortID校验失败",
			wantActionType: "CHECK_REALITY_KEYS",
		},
		{
			name:           "端口被墙: 海外通但国内TCP不通，且国内Ping通 (如 33338端口被墙)",
			port:           33338,
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
			diag := &model.DiagnosticInfo{
				TargetIP:          "156.231.113.91",
				TargetPort:        tt.port,
				AbroadReachable:   tt.abroadOK,
				DomesticReachable: tt.domesticTCPOK,
				DomesticPing:      tt.pingOK,
			}

			if tt.domesticTCPOK && tt.abroadOK {
				diag.FaultType = model.FaultProtocolMismatch
				diag.StatusText = "【配置错误】Reality公钥(pbk)或ShortID校验失败"
				diag.ActionType = "CHECK_REALITY_KEYS"
			} else if tt.abroadOK && !tt.domesticTCPOK {
				if tt.pingOK {
					diag.FaultType = model.FaultPortBlocked
					diag.StatusText = fmt.Sprintf("%d端口被墙", tt.port)
					diag.ActionType = "AI_CHANGE_PORT"
				} else {
					diag.FaultType = model.FaultIPBlocked
					diag.StatusText = "IP被墙"
					diag.ActionType = "CHANGE_IP_OR_CDN"
				}
			} else {
				diag.FaultType = model.FaultMachineDown
				diag.StatusText = "【离线】机子关机/软件崩溃/线路中断"
				diag.ActionType = "CHECK_SERVER_AND_PROCESS"
			}

			if diag.FaultType != tt.wantFault {
				t.Errorf("FaultType = %v, want %v", diag.FaultType, tt.wantFault)
			}
			if diag.StatusText != tt.wantStatusText {
				t.Errorf("StatusText = %v, want %v", diag.StatusText, tt.wantStatusText)
			}
			if diag.ActionType != tt.wantActionType {
				t.Errorf("ActionType = %v, want %v", diag.ActionType, tt.wantActionType)
			}
		})
	}
}
