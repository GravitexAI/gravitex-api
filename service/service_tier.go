package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// serviceTierRatios OpenAI 官方服务档位倍率（相对 Standard 价格）。
// 按上游响应回传的 service_tier 匹配；default / auto / flex 等不在表中的档位按原价计费。
var serviceTierRatios = map[string]float64{
	"priority":  2, // Fast 档（旧称 Priority），上游响应回传 priority
	"fast":      2,
	"ultrafast": 6,
}

// ServiceTierRatio 返回本次请求按上游实际处理档位应加收的倍率，无需加收时返回 1。
func ServiceTierRatio(relayInfo *relaycommon.RelayInfo) float64 {
	if relayInfo == nil {
		return 1
	}
	if ratio, ok := serviceTierRatios[relayInfo.UpstreamServiceTier]; ok {
		return ratio
	}
	return 1
}
