package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

// ServiceTierRatio 返回本次请求按上游实际处理档位应加收的倍率，无需加收时返回 1。
// 倍率来自后台 billing_setting.service_tier_ratio，按计费模型名 + 上游回传档位查找。
func ServiceTierRatio(relayInfo *relaycommon.RelayInfo) float64 {
	if relayInfo == nil || relayInfo.UpstreamServiceTier == "" {
		return 1
	}
	return billing_setting.GetServiceTierRatio(relayInfo.OriginModelName, relayInfo.UpstreamServiceTier)
}
