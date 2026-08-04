package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type textQuotaSummary struct {
	PromptTokens             int
	CompletionTokens         int
	TotalTokens              int
	CacheTokens              int
	CacheCreationTokens      int
	CacheCreationTokens5m    int
	CacheCreationTokens1h    int
	ImageTokens              int
	AudioTokens              int
	VideoTokens              int
	ModelName                string
	TokenName                string
	UseTimeSeconds           int64
	CompletionRatio          float64
	CacheRatio               float64
	ImageRatio               float64
	ImageCompletionRatio     float64
	VideoRatio               float64
	ModelRatio               float64
	GroupRatio               float64
	ModelPrice               float64
	CacheCreationRatio       float64
	CacheCreationRatio5m     float64
	CacheCreationRatio1h     float64
	Quota                    int
	IsClaudeUsageSemantic    bool
	UsageSemantic            string
	WebSearchPrice           float64
	WebSearchCallCount       int
	ClaudeWebSearchPrice     float64
	ClaudeWebSearchCallCount int
	FileSearchPrice          float64
	FileSearchCallCount      int
	AudioInputPrice          float64
	ImageGenerationCallPrice float64
	// Gemini image/text output split
	GeminiImageOutputTokens   int
	GeminiTextOutputTokens    int
	ReasoningTokens           int
	EffectiveImageOutputRatio float64
	ToolCallSurchargeQuota    decimal.Decimal
}

func cacheWriteTokensTotal(summary textQuotaSummary) int {
	if summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0 {
		splitCacheWriteTokens := summary.CacheCreationTokens5m + summary.CacheCreationTokens1h
		if summary.CacheCreationTokens > splitCacheWriteTokens {
			return summary.CacheCreationTokens
		}
		return splitCacheWriteTokens
	}
	return summary.CacheCreationTokens
}

func isLegacyClaudeDerivedOpenAIUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if relayInfo == nil || usage == nil {
		return false
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return false
	}
	if usage.UsageSource != "" || usage.UsageSemantic != "" {
		return false
	}
	return usage.ClaudeCacheCreation5mTokens > 0 || usage.ClaudeCacheCreation1hTokens > 0
}

func calculateTextToolCallSurcharge(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary *textQuotaSummary) decimal.Decimal {
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	var surcharge decimal.Decimal

	if relayInfo.ResponsesUsageInfo != nil {
		if webSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool.CallCount > 0 {
			summary.WebSearchCallCount = webSearchTool.CallCount
			summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
				Mul(decimal.NewFromInt(int64(webSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	} else if strings.HasSuffix(summary.ModelName, "search-preview") {
		summary.WebSearchCallCount = 1
		summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	summary.ClaudeWebSearchCallCount = ctx.GetInt("claude_web_search_requests")
	if summary.ClaudeWebSearchCallCount > 0 {
		summary.ClaudeWebSearchPrice = operation_setting.GetToolPrice("web_search")
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ClaudeWebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit).
			Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))))
	}

	if relayInfo.ResponsesUsageInfo != nil {
		if fileSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch]; exists && fileSearchTool.CallCount > 0 {
			summary.FileSearchCallCount = fileSearchTool.CallCount
			summary.FileSearchPrice = operation_setting.GetToolPrice("file_search")
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.FileSearchPrice).
				Mul(decimal.NewFromInt(int64(fileSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	}

	if ctx.GetBool("image_generation_call") {
		summary.ImageGenerationCallPrice = operation_setting.GetGPTImage1PriceOnceCall(ctx.GetString("image_generation_call_quality"), ctx.GetString("image_generation_call_size"))
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ImageGenerationCallPrice).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	return surcharge
}

// noteQuotaClamp records the first quota saturation event onto relayInfo so it
// can later be attached to the consume/task log for admin auditing. First
// non-nil clamp wins (a single request may hit multiple conversions).
func noteQuotaClamp(relayInfo *relaycommon.RelayInfo, clamp *common.QuotaClamp) {
	if clamp == nil || relayInfo == nil {
		return
	}
	if relayInfo.QuotaClamp == nil {
		relayInfo.QuotaClamp = clamp
	}
}

func composeTieredTextQuota(relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, tieredQuota int, tieredResult *billingexpr.TieredResult) int {
	if summary.ToolCallSurchargeQuota.IsZero() {
		return tieredQuota
	}

	if tieredResult != nil {
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			quota, clamp := common.QuotaFromDecimalChecked(decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Mul(decimal.NewFromFloat(snap.GroupRatio)).
				Add(summary.ToolCallSurchargeQuota))
			noteQuotaClamp(relayInfo, clamp)
			return quota
		}
	}

	// Saturate the final sum, not just the surcharge: tieredQuota can be near
	// MaxQuota and adding the surcharge could push the total past the int32
	// quota policy bound (persisted quota columns are 32-bit).
	total, clamp := common.QuotaFromDecimalChecked(
		decimal.NewFromInt(int64(tieredQuota)).Add(summary.ToolCallSurchargeQuota),
	)
	noteQuotaClamp(relayInfo, clamp)
	return total
}

func calculateTextQuotaSummary(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) textQuotaSummary {
	summary := textQuotaSummary{
		ModelName:            relayInfo.OriginModelName,
		TokenName:            ctx.GetString("token_name"),
		UseTimeSeconds:       time.Now().Unix() - relayInfo.StartTime.Unix(),
		CompletionRatio:      relayInfo.PriceData.CompletionRatio,
		CacheRatio:           relayInfo.PriceData.CacheRatio,
		ImageRatio:           relayInfo.PriceData.ImageRatio,
		ImageCompletionRatio: relayInfo.PriceData.ImageCompletionRatio,
		ModelRatio:           relayInfo.PriceData.ModelRatio,
		GroupRatio:           relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		ModelPrice:           relayInfo.PriceData.ModelPrice,
		CacheCreationRatio:   relayInfo.PriceData.CacheCreationRatio,
		CacheCreationRatio5m: relayInfo.PriceData.CacheCreation5mRatio,
		CacheCreationRatio1h: relayInfo.PriceData.CacheCreation1hRatio,
		UsageSemantic:        usageSemanticFromUsage(relayInfo, usage),
	}
	summary.IsClaudeUsageSemantic = summary.UsageSemantic == "anthropic"

	if usage == nil {
		usage = &dto.Usage{
			PromptTokens:     relayInfo.GetEstimatePromptTokens(),
			CompletionTokens: 0,
			TotalTokens:      relayInfo.GetEstimatePromptTokens(),
		}
	}

	summary.PromptTokens = usage.PromptTokens
	summary.CompletionTokens = outputTokensForModel(usage, summary.ModelName)
	summary.TotalTokens = summary.PromptTokens + summary.CompletionTokens
	summary.CacheTokens = usage.PromptTokensDetails.CachedTokens
	// gpt-5.6+ 显式/隐式缓存写入（cache_write_tokens）与 CachedCreationTokens 共用同一档
	// CacheCreationRatio 计价（写入统一按未缓存输入价的 1.25x 计费）
	summary.CacheCreationTokens = usage.PromptTokensDetails.CachedCreationTokens + usage.PromptTokensDetails.CacheWriteTokens
	summary.CacheCreationTokens5m = usage.ClaudeCacheCreation5mTokens
	summary.CacheCreationTokens1h = usage.ClaudeCacheCreation1hTokens
	summary.ImageTokens = usage.PromptTokensDetails.ImageTokens
	summary.AudioTokens = usage.PromptTokensDetails.AudioTokens
	summary.VideoTokens = usage.PromptTokensDetails.VideoTokens
	summary.VideoRatio = relayInfo.PriceData.VideoRatio
	summary.ReasoningTokens = usage.CompletionTokenDetails.ReasoningTokens
	// Gemini image/text output split: prefer context values, fallback to usage details
	summary.GeminiImageOutputTokens = ctx.GetInt("gemini_image_output_tokens")
	summary.GeminiTextOutputTokens = ctx.GetInt("gemini_text_output_tokens")
	if summary.GeminiImageOutputTokens == 0 {
		summary.GeminiImageOutputTokens = usage.CompletionTokenDetails.ImageTokens
	}
	if summary.GeminiTextOutputTokens == 0 {
		summary.GeminiTextOutputTokens = usage.CompletionTokenDetails.TextTokens
	}
	// 图片输出倍率优先级：ImageCompletionRatio → ImageRatio → 1.0（等同文本输入倍率）
	// 此值后续会与 modelRatio × groupRatio 相乘，故回退不能用 ModelRatio，否则会导致 ModelRatio 被乘两次
	summary.EffectiveImageOutputRatio = summary.ImageCompletionRatio
	if summary.EffectiveImageOutputRatio <= 0 {
		summary.EffectiveImageOutputRatio = summary.ImageRatio
	}
	if summary.EffectiveImageOutputRatio <= 0 {
		summary.EffectiveImageOutputRatio = 1.0
	}
	legacyClaudeDerived := isLegacyClaudeDerivedOpenAIUsage(relayInfo, usage)
	isOpenRouterClaudeBilling := relayInfo.ChannelMeta != nil &&
		relayInfo.ChannelType == constant.ChannelTypeOpenRouter &&
		summary.IsClaudeUsageSemantic

	if isOpenRouterClaudeBilling {
		summary.PromptTokens -= summary.CacheTokens
		isUsingCustomSettings := relayInfo.PriceData.UsePrice || hasCustomModelRatio(summary.ModelName, relayInfo.PriceData.ModelRatio)
		if summary.CacheCreationTokens == 0 && relayInfo.PriceData.CacheCreationRatio != 1 && usage.Cost != 0 && !isUsingCustomSettings {
			maybeCacheCreationTokens := CalcOpenRouterCacheCreateTokens(*usage, relayInfo.PriceData)
			if maybeCacheCreationTokens >= 0 && summary.PromptTokens >= maybeCacheCreationTokens {
				summary.CacheCreationTokens = maybeCacheCreationTokens
			}
		}
		summary.PromptTokens -= summary.CacheCreationTokens
	}

	dPromptTokens := decimal.NewFromInt(int64(summary.PromptTokens))
	dCacheTokens := decimal.NewFromInt(int64(summary.CacheTokens))
	dImageTokens := decimal.NewFromInt(int64(summary.ImageTokens))
	dAudioTokens := decimal.NewFromInt(int64(summary.AudioTokens))
	dVideoTokens := decimal.NewFromInt(int64(summary.VideoTokens))
	dCompletionTokens := decimal.NewFromInt(int64(summary.CompletionTokens))
	dCachedCreationTokens := decimal.NewFromInt(int64(summary.CacheCreationTokens))
	dCompletionRatio := decimal.NewFromFloat(summary.CompletionRatio)
	dCacheRatio := decimal.NewFromFloat(summary.CacheRatio)
	dImageRatio := decimal.NewFromFloat(summary.ImageRatio)
	dModelRatio := decimal.NewFromFloat(summary.ModelRatio)
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dModelPrice := decimal.NewFromFloat(summary.ModelPrice)
	dCacheCreationRatio := decimal.NewFromFloat(summary.CacheCreationRatio)
	dCacheCreationRatio5m := decimal.NewFromFloat(summary.CacheCreationRatio5m)
	dCacheCreationRatio1h := decimal.NewFromFloat(summary.CacheCreationRatio1h)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	ratio := dModelRatio.Mul(dGroupRatio)
	summary.ToolCallSurchargeQuota = calculateTextToolCallSurcharge(ctx, relayInfo, &summary)

	var audioInputQuota decimal.Decimal
	if !relayInfo.PriceData.UsePrice {
		baseTokens := dPromptTokens

		var cachedTokensWithRatio decimal.Decimal
		if !dCacheTokens.IsZero() {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCacheTokens)
			}
			cachedTokensWithRatio = dCacheTokens.Mul(dCacheRatio)
		}

		var cachedCreationTokensWithRatio decimal.Decimal
		hasSplitCacheCreationTokens := summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0
		if !dCachedCreationTokens.IsZero() || hasSplitCacheCreationTokens {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCachedCreationTokens)
				cachedCreationTokensWithRatio = dCachedCreationTokens.Mul(dCacheCreationRatio)
			} else {
				remaining := summary.CacheCreationTokens - summary.CacheCreationTokens5m - summary.CacheCreationTokens1h
				if remaining < 0 {
					remaining = 0
				}
				cachedCreationTokensWithRatio = decimal.NewFromInt(int64(remaining)).Mul(dCacheCreationRatio)
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens5m)).Mul(dCacheCreationRatio5m))
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens1h)).Mul(dCacheCreationRatio1h))
			}
		}

		var imageTokensWithRatio decimal.Decimal
		if !dImageTokens.IsZero() {
			baseTokens = baseTokens.Sub(dImageTokens)
			imageTokensWithRatio = dImageTokens.Mul(dImageRatio)
		}

		var videoTokensWithRatio decimal.Decimal
		if !dVideoTokens.IsZero() && summary.VideoRatio > 0 {
			baseTokens = baseTokens.Sub(dVideoTokens)
			videoTokensWithRatio = dVideoTokens.Mul(decimal.NewFromFloat(summary.VideoRatio))
		}

		if !dAudioTokens.IsZero() {
			summary.AudioInputPrice = operation_setting.GetGeminiInputAudioPricePerMillionTokens(summary.ModelName)
			if summary.AudioInputPrice > 0 {
				baseTokens = baseTokens.Sub(dAudioTokens)
				audioInputQuota = decimal.NewFromFloat(summary.AudioInputPrice).
					Div(decimal.NewFromInt(1000000)).Mul(dAudioTokens).Mul(dGroupRatio).Mul(dQuotaPerUnit)
			}
		}

		promptQuota := baseTokens.Add(cachedTokensWithRatio).Add(imageTokensWithRatio).Add(videoTokensWithRatio).Add(cachedCreationTokensWithRatio)
		var completionQuota decimal.Decimal
		if summary.GeminiImageOutputTokens > 0 && summary.EffectiveImageOutputRatio > 0 {
			// Gemini image/text output split billing
			textOutputTokens := int64(summary.ReasoningTokens + summary.GeminiTextOutputTokens)
			if textOutputTokens == 0 && summary.CompletionTokens >= summary.GeminiImageOutputTokens {
				textOutputTokens = int64(summary.CompletionTokens - summary.GeminiImageOutputTokens)
			}
			if textOutputTokens < 0 {
				textOutputTokens = 0
			}
			textCompletionQuota := decimal.NewFromInt(textOutputTokens).Mul(dCompletionRatio)
			imageCompletionQuota := decimal.NewFromInt(int64(summary.GeminiImageOutputTokens)).Mul(decimal.NewFromFloat(summary.EffectiveImageOutputRatio))
			completionQuota = textCompletionQuota.Add(imageCompletionQuota)
		} else {
			completionQuota = dCompletionTokens.Mul(dCompletionRatio)
		}
		quotaCalculateDecimal := promptQuota.Add(completionQuota).Mul(ratio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		quotaCalculateDecimal = relayInfo.PriceData.ApplyOtherRatiosToDecimal(quotaCalculateDecimal)

		if !ratio.IsZero() && quotaCalculateDecimal.LessThanOrEqual(decimal.Zero) {
			quotaCalculateDecimal = decimal.NewFromInt(1)
		}
		quota, clamp := common.QuotaFromDecimalChecked(quotaCalculateDecimal)
		summary.Quota = quota
		noteQuotaClamp(relayInfo, clamp)
	} else {
		quotaCalculateDecimal := dModelPrice.Mul(dQuotaPerUnit).Mul(dGroupRatio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		quotaCalculateDecimal = relayInfo.PriceData.ApplyOtherRatiosToDecimal(quotaCalculateDecimal)
		quota, clamp := common.QuotaFromDecimalChecked(quotaCalculateDecimal)
		summary.Quota = quota
		noteQuotaClamp(relayInfo, clamp)
	}

	if summary.TotalTokens == 0 {
		summary.Quota = 0
	} else if !ratio.IsZero() && summary.Quota == 0 {
		summary.Quota = 1
	}

	return summary
}

func usageSemanticFromUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

func PostTextConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) {
	originUsage := usage
	if usage == nil {
		extraContent = append(extraContent, "上游无计费信息")
	}
	if originUsage != nil {
		ObserveChannelAffinityUsageCacheByRelayFormat(ctx, usage, relayInfo.GetFinalRequestRelayFormat())
	}

	adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	var tieredResult *billingexpr.TieredResult
	tieredBillingApplied := false
	if originUsage != nil {
		var tieredUsedVars map[string]bool
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			tieredUsedVars = billingexpr.UsedVars(snap.ExprString)
		}
		tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, BuildTieredTokenParamsForModel(usage, summary.IsClaudeUsageSemantic, tieredUsedVars, summary.ModelName))
		if tieredOk {
			tieredBillingApplied = true
			tieredResult = tieredRes
			summary.Quota = composeTieredTextQuota(relayInfo, summary, tieredQuota, tieredRes)
		}
	}

	if summary.WebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Web Search 调用 %d 次，调用花费 $%s", summary.WebSearchCallCount, decimal.NewFromFloat(summary.WebSearchPrice).Mul(decimal.NewFromInt(int64(summary.WebSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).String()))
	}
	if summary.ClaudeWebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Claude Web Search 调用 %d 次，调用花费 $%s", summary.ClaudeWebSearchCallCount, decimal.NewFromFloat(summary.ClaudeWebSearchPrice).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))).String()))
	}
	if summary.FileSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("File Search 调用 %d 次，调用花费 $%s", summary.FileSearchCallCount, decimal.NewFromFloat(summary.FileSearchPrice).Mul(decimal.NewFromInt(int64(summary.FileSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).String()))
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Audio Input 花费 %s", decimal.NewFromFloat(summary.AudioInputPrice).Div(decimal.NewFromInt(1000000)).Mul(decimal.NewFromInt(int64(summary.AudioTokens))).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ImageGenerationCallPrice > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Image Generation Call 花费 %s", decimal.NewFromFloat(summary.ImageGenerationCallPrice).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ImageTokens > 0 && !relayInfo.PriceData.UsePrice {
		imageInputQuota := decimal.NewFromInt(int64(summary.ImageTokens)).
			Mul(decimal.NewFromFloat(summary.ImageRatio)).
			Mul(decimal.NewFromFloat(summary.ModelRatio)).
			Mul(decimal.NewFromFloat(summary.GroupRatio))
		imageInputQuota = relayInfo.PriceData.ApplyOtherRatiosToDecimal(imageInputQuota)
		imageInputPrice := summary.ModelRatio * 2.0 * summary.ImageRatio
		extraContent = append(extraContent, fmt.Sprintf("图片输入 %d tokens，图片输入价格 %.6f / 1M tokens，图片输入倍率 %.2f，图片输入花费 %s", summary.ImageTokens, imageInputPrice, summary.ImageRatio, imageInputQuota.String()))
	}

	if summary.TotalTokens == 0 {
		extraContent = append(extraContent, "上游没有返回计费信息，无法扣费（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, summary.ModelName, relayInfo.FinalPreConsumedQuota))
		// 退还用户预扣额度（与 SettleBilling 退还令牌保持一致）
		if relayInfo.Billing != nil && relayInfo.BillingSource != BillingSourceSubscription {
			preConsumed := relayInfo.Billing.GetPreConsumedQuota()
			if preConsumed > 0 {
				if err := model.ConsumeUserQuotaSettle(relayInfo.UserId, 0, -preConsumed); err != nil {
					logger.LogError(ctx, "error refund user quota on zero tokens: "+err.Error())
				}
			}
		}
	} else {
		// 有 BillingSession 时：
		//   - 钱包用户：ConsumeUserQuotaSettle 一次 UPDATE 完成 quota 扣减 + used_quota + request_count
		//   - 订阅用户：quota 已由 SubscriptionFunding 处理，只需更新 used_quota + request_count
		if relayInfo.Billing != nil && relayInfo.BillingSource != BillingSourceSubscription {
			preConsumed := relayInfo.Billing.GetPreConsumedQuota()
			delta := summary.Quota - preConsumed
			if err := model.ConsumeUserQuotaSettle(relayInfo.UserId, summary.Quota, delta); err != nil {
				logger.LogError(ctx, "error consume user quota settle: "+err.Error())
			}
		} else {
			model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, summary.Quota)
		}
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, summary.Quota)
	}

	if err := SettleBilling(ctx, relayInfo, summary.Quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	logModel := summary.ModelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}

	logContent := strings.Join(extraContent, ", ")
	var other map[string]interface{}
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other["usage_semantic"] = "anthropic"
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	if adminRejectReason != "" {
		other["reject_reason"] = adminRejectReason
	}
	if summary.ImageTokens != 0 {
		other["image"] = true
		other["image_ratio"] = summary.ImageRatio
		other["image_output"] = summary.ImageTokens
		// 写入 input_text_tokens 和 input_image_tokens，供前端图像 Token 计价展示
		other["input_image_tokens"] = summary.ImageTokens
		inputTextTokens := summary.PromptTokens - summary.ImageTokens - summary.CacheTokens
		if inputTextTokens < 0 {
			inputTextTokens = 0
		}
		other["input_text_tokens"] = inputTextTokens
		// 写入绝对价格（$/1M tokens），前端直接使用，无需再次计算
		inputTextPriceUSD := summary.ModelRatio * 2.0
		other["input_text_price"] = inputTextPriceUSD
		other["input_image_price"] = inputTextPriceUSD * summary.ImageRatio
	}
	if summary.WebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.WebSearchCallCount
		other["web_search_price"] = summary.WebSearchPrice
	} else if summary.ClaudeWebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.ClaudeWebSearchCallCount
		other["web_search_price"] = summary.ClaudeWebSearchPrice
	}
	if summary.FileSearchCallCount > 0 {
		other["file_search"] = true
		other["file_search_call_count"] = summary.FileSearchCallCount
		other["file_search_price"] = summary.FileSearchPrice
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		other["audio_input_seperate_price"] = true
		other["audio_input_token_count"] = summary.AudioTokens
		other["audio_input_price"] = summary.AudioInputPrice
	}
	if summary.AudioTokens > 0 {
		other["audio_input"] = summary.AudioTokens
		other["audio_tokens"] = summary.AudioTokens
	}
	if summary.VideoTokens > 0 {
		other["video_input_tokens"] = summary.VideoTokens
		if summary.VideoRatio > 0 {
			other["video_input_ratio"] = summary.VideoRatio
			other["video_input_price"] = summary.ModelRatio * 2.0 * summary.VideoRatio
			other["video_input_price_source"] = "VideoRatio"
			videoInputQuota := decimal.NewFromInt(int64(summary.VideoTokens)).
				Mul(decimal.NewFromFloat(summary.VideoRatio)).
				Mul(decimal.NewFromFloat(summary.ModelRatio)).
				Mul(decimal.NewFromFloat(summary.GroupRatio))
			other["video_input_quota"] = videoInputQuota.IntPart()
		} else {
			other["video_input_price_source"] = "ModelRatio"
		}
	}
	if summary.ImageGenerationCallPrice > 0 {
		other["image_generation_call"] = true
		other["image_generation_call_price"] = summary.ImageGenerationCallPrice
	}
	// 按张计费（per_image）：写入 per_call_price / per_call_image_multiplier 供下游
	// （Java 后端 BillingDetailService / 前端 LogsExpense 页面）识别为按张计费并正确渲染。
	// - per_call_price：单张有效价格 = unit_price × size_ratio × quality_ratio（即 PriceData.ModelPrice）
	// - per_call_image_multiplier：实际生成张数（来自 OtherRatios["n"]，由 image_handler 优先用
	//   上游返回的 usage.generated_images 计算，回退到 request.N，再回退到 1）
	if relayInfo.PriceData.PerImageUnitPrice > 0 {
		imageCount := 1.0
		if n, ok := relayInfo.PriceData.OtherRatios()["n"]; ok && n > 0 {
			imageCount = n
		}
		if relayInfo.PriceData.ImagePerImagePricing != nil && relayInfo.PriceData.ImageBillingUsage != nil {
			billingUsage := relayInfo.PriceData.ImageBillingUsage
			other["image_input_count"] = billingUsage.InputImageCount
			other["image_output_count"] = billingUsage.SuccessfulImageCount
			other["image_output_size"] = fmt.Sprintf("%dx%d", billingUsage.OutputWidth, billingUsage.OutputHeight)
			other["image_output_pixels"] = billingUsage.OutputPixels
			other["image_output_tier"] = billingUsage.OutputSizeTier
			other["image_input_price"] = relayInfo.PriceData.ImagePerImagePricing.InputImageFirst
			other["image_input_first_price"] = relayInfo.PriceData.ImagePerImagePricing.InputImageFirst
			other["image_input_from_second_price"] = relayInfo.PriceData.ImagePerImagePricing.InputImageFromThe2nd
			if billingUsage.InputImageCount > 0 && relayInfo.PriceData.ImagePerImagePricing.InputImageFirst == 0 {
				other["image_input_free_count"] = 1
			} else {
				other["image_input_free_count"] = 0
			}
			if billingUsage.InputImageCount > 1 {
				other["image_input_price"] = relayInfo.PriceData.ImagePerImagePricing.InputImageFirst +
					float64(billingUsage.InputImageCount-1)*relayInfo.PriceData.ImagePerImagePricing.InputImageFromThe2nd
			}
			other["image_output_price"] = float64(billingUsage.SuccessfulImageCount) * relayInfo.PriceData.PerImageUnitPrice
			other["image_total_price"] = relayInfo.PriceData.ModelPrice
			other["per_call_price"] = relayInfo.PriceData.PerImageUnitPrice
		} else {
			other["per_call_price"] = relayInfo.PriceData.ModelPrice
		}
		other["per_call_image_multiplier"] = imageCount
	}
	if summary.CacheCreationTokens > 0 {
		other["cache_creation_tokens"] = summary.CacheCreationTokens
		other["cache_creation_ratio"] = summary.CacheCreationRatio
	}
	if summary.CacheCreationTokens5m > 0 {
		other["cache_creation_tokens_5m"] = summary.CacheCreationTokens5m
		other["cache_creation_ratio_5m"] = summary.CacheCreationRatio5m
	}
	if summary.CacheCreationTokens1h > 0 {
		other["cache_creation_tokens_1h"] = summary.CacheCreationTokens1h
		other["cache_creation_ratio_1h"] = summary.CacheCreationRatio1h
	}
	cacheWriteTokens := cacheWriteTokensTotal(summary)
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other["cache_write_tokens"] = cacheWriteTokens
	}
	// Gemini image/text output split: record for admin UI and billing audit
	if summary.GeminiImageOutputTokens > 0 || summary.GeminiTextOutputTokens > 0 || summary.ReasoningTokens > 0 {
		other["image_output_tokens"] = summary.GeminiImageOutputTokens
		other["text_output_tokens"] = summary.GeminiTextOutputTokens
		other["reasoning_tokens"] = summary.ReasoningTokens
		if summary.EffectiveImageOutputRatio > 0 {
			other["image_completion_ratio"] = summary.EffectiveImageOutputRatio
			other["effective_image_output_ratio"] = summary.EffectiveImageOutputRatio
			// 图片输出绝对价格（$/1M tokens），含 modelRatio，与 input_image_price 模式一致
			other["output_image_price"] = summary.ModelRatio * 2.0 * summary.EffectiveImageOutputRatio
			if summary.GeminiImageOutputTokens > 0 {
				if summary.ImageRatio > 0 {
					other["image_output_ratio_source"] = "image_input"
				} else {
					other["image_output_ratio_source"] = "text_input"
				}
			}
		}
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && usage != nil && usage.UsageSource != "" && usage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		// Only write this field when upstream/current conversion has already provided a
		// reliable total input value and tagged the usage source. Do not infer it from
		// prompt/cache fields here, otherwise old upstream payloads may be double-counted.
		other["input_tokens_total"] = usage.InputTokens
	}
	if tieredBillingApplied {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)
	}

	// 将系统生成的 request_id 存入 other，request_id 字段改存上游返回的 ID
	systemRequestId := ctx.GetString(common.RequestIdKey)
	if systemRequestId != "" {
		other["system_request_id"] = systemRequestId
	}

	attachQuotaSaturation(ctx, relayInfo, other)

	priceChain := CalculatePriceChainForLog(ctx, logModel, summary.PromptTokens, summary.CompletionTokens, summary.Quota)
	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		RequestId:        relayInfo.UpstreamResponseId,
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		ModelName:        logModel,
		TokenName:        summary.TokenName,
		Quota:            summary.Quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(summary.UseTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
		PriceChain:       priceChain,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(summary.CompletionTokens))
	})
}
