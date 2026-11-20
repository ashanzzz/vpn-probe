package classifier

import (
	"testing"
	"vpn-probe/internal/model"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		isSelf    bool
		online    bool
		chinaExit bool
		expected  model.Category
	}{
		{
			name:      "自有服务 - 离线",
			isSelf:    true,
			online:    false,
			chinaExit: false,
			expected:  model.CategorySelfOffline,
		},
		{
			name:      "自有服务 - 在线送中",
			isSelf:    true,
			online:    true,
			chinaExit: true,
			expected:  model.CategorySelfOnlineChina,
		},
		{
			name:      "自有服务 - 在线不送中",
			isSelf:    true,
			online:    true,
			chinaExit: false,
			expected:  model.CategorySelfOnlineClean,
		},
		{
			name:      "外部服务 - 离线",
			isSelf:    false,
			online:    false,
			chinaExit: false,
			expected:  model.CategoryExtOffline,
		},
		{
			name:      "外部服务 - 送中",
			isSelf:    false,
			online:    true,
			chinaExit: true,
			expected:  model.CategoryExtOnlineChina,
		},
		{
			name:      "外部服务 - 不送中",
			isSelf:    false,
			online:    true,
			chinaExit: false,
			expected:  model.CategoryExtOnlineClean,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.isSelf, tt.online, tt.chinaExit)
			if got != tt.expected {
				t.Errorf("Classify(%v, %v, %v) = %v, want %v", tt.isSelf, tt.online, tt.chinaExit, got, tt.expected)
			}
		})
	}
}
