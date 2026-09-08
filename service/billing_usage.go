package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	usageBillingPathLocal              = "local"
	usageBillingPathUpstream           = "upstream"
	usageBillingPathOpenAI             = "billing-usage-openai"
	usageBillingPathOpenAIEstimated    = "billing-usage-openai-estimated"
	usageBillingPathAnthropic          = "billing-usage-anthropic"
	usageBillingPathAnthropicEstimated = "billing-usage-anthropic-estimated"
	usageBillingPathGemini             = "billing-usage-gemini"
	usageBillingPathGeminiEstimated    = "billing-usage-gemini-estimated"
)

func effectiveBillingUsage(usage *dto.Usage) *dto.Usage {
	if billingUsage, ok := usageFromBillingUsage(usage); ok {
		return billingUsage
	}
	return usage
}

func usageBillingPathForLog(isLocalCountTokens bool, usage *dto.Usage) string {
	effectiveUsage, ok := usageFromBillingUsage(usage)
	if !ok {
		if isLocalCountTokens {
			return usageBillingPathLocal
		}
		return usageBillingPathUpstream
	}

	switch effectiveUsage.UsageSemantic {
	case dto.BillingUsageSemanticOpenAI:
		if usage.BillingUsage.Estimated {
			return usageBillingPathOpenAIEstimated
		}
		return usageBillingPathOpenAI
	case dto.BillingUsageSemanticAnthropic:
		if usage.BillingUsage.Estimated {
			return usageBillingPathAnthropicEstimated
		}
		return usageBillingPathAnthropic
	case dto.BillingUsageSemanticGemini:
		if usage.BillingUsage.Estimated {
			return usageBillingPathGeminiEstimated
		}
		return usageBillingPathGemini
	}

	return usageBillingPathUpstream
}

func appendUsageBillingPathForLog(other *model.LogOther, isLocalCountTokens bool, usage *dto.Usage) {
	if other == nil {
		return
	}
	other.SetAdmin("usage_billing_path", usageBillingPathForLog(isLocalCountTokens, usage))
}

func usageFromBillingUsage(usage *dto.Usage) (*dto.Usage, bool) {
	if usage == nil || usage.BillingUsage == nil {
		return nil, false
	}
	canonical, ok := usage.BillingUsage.CanonicalUsage()
	if !ok {
		return nil, false
	}
	// VIDEO is a fork billing dimension; the shared Gemini canonicalizer
	// normalizes text/image/audio only. Read video from the same upstream
	// snapshot so conversions cannot silently bill it as ordinary text.
	if metadata := usage.BillingUsage.GeminiUsageMetadata; metadata != nil && canonical.UsageSemantic == dto.BillingUsageSemanticGemini {
		var videoTokens float64
		for _, detail := range metadata.PromptTokensDetails {
			if strings.EqualFold(strings.TrimSpace(detail.Modality), "VIDEO") && detail.TokenCount > 0 {
				videoTokens += float64(detail.TokenCount)
			}
		}
		for _, detail := range metadata.ToolUsePromptTokensDetails {
			if strings.EqualFold(strings.TrimSpace(detail.Modality), "VIDEO") && detail.TokenCount > 0 {
				videoTokens += float64(detail.TokenCount)
			}
		}
		canonical.PromptTokensDetails.VideoTokens = common.QuotaFromFloat(videoTokens)
	}
	return canonical, true
}
