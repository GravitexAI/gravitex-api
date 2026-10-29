package service

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
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
	logEnabled := isLogUpstreamResponsesEnabled()
	if other == nil || relayInfo == nil || (!logEnabled && !ShouldPersistSeedreamImageUsage(relayInfo) && !hasUpstreamGeminiRefusalAudit(relayInfo)) {
		return
	}
	if len(relayInfo.UpstreamResponses) == 0 && len(relayInfo.RawUpstreamUsage) == 0 && len(relayInfo.ClaudeRefusalAudit) == 0 {
		return
	}
	upstreamResponses := relayInfo.UpstreamResponses
	if logEnabled && len(relayInfo.ClaudeRefusalAudit) > 0 {
		upstreamResponses = make(map[string]any, len(relayInfo.UpstreamResponses)+len(relayInfo.ClaudeRefusalAudit))
		for key, value := range relayInfo.UpstreamResponses {
			upstreamResponses[key] = value
		}
		for key, value := range relayInfo.ClaudeRefusalAudit {
			upstreamResponses[key] = value
		}
	}
	if ShouldPersistSeedreamImageUsage(relayInfo) && len(relayInfo.RawUpstreamUsage) > 0 {
		if upstreamResponses == nil {
			upstreamResponses = make(map[string]any)
		}
		upstreamResponses["usage"] = json.RawMessage(relayInfo.RawUpstreamUsage)
	}
	other["upstream_responses"] = upstreamResponses
}

func hasUpstreamGeminiRefusalAudit(relayInfo *relaycommon.RelayInfo) bool {
	if relayInfo == nil {
		return false
	}
	usageMetadata, ok := relayInfo.UpstreamResponses["usageMetadata"].(map[string]any)
	if !ok {
		return false
	}
	candidates, ok := usageMetadata["candidates"].([]any)
	if ok && len(candidates) > 0 {
		return true
	}
	promptFeedback, ok := usageMetadata["promptFeedback"].(map[string]any)
	return ok && len(promptFeedback) > 0
}

func AppendUpstreamResponsesToMap(other map[string]interface{}, relayInfo *relaycommon.RelayInfo) {
	appendUpstreamResponses(other, relayInfo)
}

func ShouldPersistSeedreamImageUsage(relayInfo *relaycommon.RelayInfo) bool {
	if relayInfo == nil {
		return false
	}
	if relayInfo.RelayMode != relayconstant.RelayModeImagesGenerations && relayInfo.RelayMode != relayconstant.RelayModeImagesEdits {
		return false
	}

	model := strings.TrimSpace(relayInfo.OriginModelName)
	return strings.EqualFold(model, "dola-seedream-5-0-pro-260628") ||
		strings.EqualFold(model, "seedream-5-0-pro-NSFW")
}

func appendUsageConversion(other map[string]interface{}, relayInfo *relaycommon.RelayInfo) {
	if other == nil || relayInfo == nil || !isLogUsageConversionEnabled() {
		return
	}
	if relayInfo.UsageConversion == nil {
		return
	}
	other["usage_conversion"] = relayInfo.UsageConversion
}
