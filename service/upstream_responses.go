package service

import (
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const logUpstreamResponsesEnabledOption = "LogUpstreamResponsesEnabled"
const logUsageConversionEnabledOption = "LogUsageConversionEnabled"

func isLogUpstreamResponsesEnabled() bool {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[logUpstreamResponsesEnabledOption] == "true"
}

func isLogUsageConversionEnabled() bool {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[logUsageConversionEnabledOption] == "true"
}

func appendUpstreamResponses(other map[string]interface{}, relayInfo *relaycommon.RelayInfo) {
	if other == nil || relayInfo == nil || !isLogUpstreamResponsesEnabled() {
		return
	}
	if len(relayInfo.UpstreamResponses) == 0 {
		return
	}
	other["upstream_responses"] = relayInfo.UpstreamResponses
}

func appendUsageConversion(other map[string]interface{}, relayInfo *relaycommon.RelayInfo) {
	if other == nil || relayInfo == nil || !isLogUsageConversionEnabled() {
		return
	}
	if !relayInfo.HasRequestFormatConversion() || relayInfo.UsageConversion == nil {
		return
	}
	other["usage_conversion"] = relayInfo.UsageConversion
}
