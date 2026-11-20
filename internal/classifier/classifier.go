package classifier

import (
	"vpn-probe/internal/model"
)

// Classify 根据在线状态、送中状态及是否为自有服务，返回精准的分类标签
func Classify(isSelf, online, chinaExit bool) model.Category {
	if isSelf {
		if !online {
			return model.CategorySelfOffline // "离线自有服务"
		}
		if chinaExit {
			return model.CategorySelfOnlineChina // "在线自有服务送中"
		}
		return model.CategorySelfOnlineClean // "在线自有服务不送中"
	}

	// 外部公共/购买节点分类
	if !online {
		return model.CategoryExtOffline // "离线"
	}
	if chinaExit {
		return model.CategoryExtOnlineChina // "送中"
	}
	return model.CategoryExtOnlineClean // "不送中"
}
