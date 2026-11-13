package service

import (
	"encoding/base64"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/tidwall/gjson"

	"github.com/gin-gonic/gin"
)

// attachQuotaSaturationToOther nests a quota saturation marker under
// other.admin_info.quota_saturation. Nesting under admin_info makes it
// admin-only for free, since model.formatUserLogs strips the whole admin_info
// object for non-admin viewers. Creates admin_info if absent. No-op when the
// clamp is nil (the common case: no saturation happened).
func attachQuotaSaturationToOther(other any, clamp *common.QuotaClamp) {
	if legacy, ok := other.(map[string]interface{}); ok {
		if clamp != nil {
			legacy["quota_saturation"] = clamp.AuditMap()
		}
		return
	}
	otherTyped, ok := other.(*model.LogOther)
	if !ok {
		return
	}
	other = otherTyped
	if clamp == nil || otherTyped == nil {
		return
	}
	otherTyped.SetAdmin("quota_saturation", clamp.AuditMap())
}

// attachQuotaSaturation records the request's quota clamp (if any) onto the
// consume log's other.admin_info and emits a request-correlated backend audit
// line. Called right before RecordConsumeLog on the text/audio/wss paths.
func attachQuotaSaturation(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil {
		return
	}
	clamp := relayInfo.QuotaClamp
	if clamp == nil {
		return
	}
	attachQuotaSaturationToOther(other, clamp)
	logger.LogWarn(ctx, common.LogText("quota saturation on consume log: op=%s kind=%s original=%g clamped=%d user=%d model=%s",
		clamp.Op, clamp.Kind, clamp.Original, clamp.Clamped, relayInfo.UserId, relayInfo.GetBillingModelName()))
}

// attachUpstreamModelFallback records upstream model swaps onto the consume
// log's other.admin_info for reconciliation only; billing is not changed.
// model_fallback is a response model that differs from the one we sent
// (second-hand relays silently swapping models); claude_fallback_iterations
// is the per-attempt usage of an official Claude server-side fallback, whose
// rejected attempts are not billed on our side.
// Called right before RecordConsumeLog on the text paths.
func attachUpstreamModelFallback(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || other == nil {
		return
	}
	if fb := relayInfo.UpstreamModelFallback; fb != nil {
		other.SetAdmin("model_fallback", map[string]any{
			"requested_model": relayInfo.OriginModelName,
			"sent_model":      fb.SentModel,
			"actual_model":    fb.ActualModel,
		})
	}
	if len(relayInfo.ClaudeUsageIterations) >= 2 {
		other.SetAdmin("claude_fallback_iterations", map[string]any{
			"trigger_category": relayInfo.ClaudeFallbackTriggerCategory,
			"iterations":       relayInfo.ClaudeUsageIterations,
		})
	}
}

func appendRequestPath(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if other == nil {
		return
	}
	if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil {
		if path := ctx.Request.URL.Path; path != "" {
			other.SetPublic("request_path", path)
			return
		}
	}
	if relayInfo != nil && relayInfo.RequestURLPath != "" {
		path := relayInfo.RequestURLPath
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
		other.SetPublic("request_path", path)
	}
}

// AppendRelayLogAdminInfo records relay routing and conversion diagnostics in
// the admin-only scope shared by successful and failed request logs.
func AppendRelayLogAdminInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if ctx == nil || other == nil {
		return
	}
	other.SetAdmin("use_channel", ctx.GetStringSlice("use_channel"))
	if relayInfo != nil {
		if billingModel := relayInfo.GetBillingModelName(); billingModel != "" && billingModel != relayInfo.OriginModelName {
			other.SetAdmin("billing_model", billingModel)
		}
		if diagnostics := relayInfo.ConversionDiagnostics(); len(diagnostics) > 0 {
			other.SetAdmin("conversion_diagnostics", diagnostics)
		}
		if relayInfo.ConversionDiagnosticsTruncated() {
			other.SetAdmin("conversion_diagnostics_truncated", true)
		}
		if len(relayInfo.RequestPluginSteps) > 0 {
			other.SetAdmin("request_plugins", relayInfo.RequestPluginSteps)
		}
	}
	if common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey) {
		other.SetAdmin("is_multi_key", true)
		other.SetAdmin("multi_key_index", common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
	}
	if common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens) {
		other.SetAdmin("local_count_tokens", true)
	}

	AppendChannelAffinityAdminInfo(ctx, other)
	adminInfo := make(map[string]interface{})
	AppendAutoRouterAdminInfo(ctx, adminInfo)
	other.MergeAdmin(adminInfo)
	if discount, ok := common.GetConfiguredCostDiscount(ctx); ok {
		other.SetAdmin("cost_discount", discount)
	}
	if events := RequestPolicy(ctx).Events(); len(events) > 0 {
		other.SetAdmin("request_policy", events)
	}
}

func GenerateTextOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64, modelPrice float64, userGroupRatio float64) *model.LogOther {
	MarkRequestPolicySuccess(ctx, relayInfo.StreamStatus)
	other := model.NewLogOther()
	other.SetPublic("model_ratio", modelRatio)
	other.SetPublic("group_ratio", groupRatio)
	other.SetPublic("completion_ratio", completionRatio)
	other.SetPublic("cache_tokens", cacheTokens)
	other.SetPublic("cache_ratio", cacheRatio)
	other.SetPublic("model_price", modelPrice)
	other.SetPublic("user_group_ratio", userGroupRatio)
	other.SetPublic("frt", float64(relayInfo.FirstResponseTime.UnixMilli()-relayInfo.StartTime.UnixMilli()))
	if relayInfo.ReasoningEffort != "" {
		other.SetPublic("reasoning_effort", relayInfo.ReasoningEffort)
	}
	// 上游实际处理的服务档位及加收倍率，供日志展示与账单导出「服务档位」「档位倍率」列使用
	if relayInfo.UpstreamServiceTier != "" {
		other.SetPublic("service_tier", relayInfo.UpstreamServiceTier)
		if ratio := ServiceTierRatio(relayInfo); ratio != 1 {
			other.SetPublic("service_tier_ratio", ratio)
		}
	}
	if relayInfo.IsModelMapped {
		other.SetPublic("is_model_mapped", true)
		other.SetPublic("upstream_model_name", relayInfo.UpstreamModelName)
	}

	isSystemPromptOverwritten := common.GetContextKeyBool(ctx, constant.ContextKeySystemPromptOverride)
	if isSystemPromptOverwritten {
		other.SetPublic("is_system_prompt_overwritten", true)
	}

	AppendRelayLogAdminInfo(ctx, relayInfo, other)
	AppendResponseModelLogInfo(relayInfo, other)
	appendRequestPath(ctx, relayInfo, other)
	appendRequestConversionChain(relayInfo, other)
	appendFinalRequestFormat(relayInfo, other)
	usageAudit := make(map[string]interface{})
	appendUpstreamResponses(usageAudit, relayInfo)
	appendUsageConversion(usageAudit, relayInfo)
	other.MergePublic(usageAudit)
	appendBillingInfo(relayInfo, other)
	appendParamOverrideInfo(relayInfo, other)
	appendRequestPluginRecord(relayInfo, other)
	appendStreamStatus(relayInfo, other)
	return other
}

// AppendGeminiOmniTaskLogMetadata snapshots the standard request/response
// metadata into the async task data. It is intentionally scoped to Gemini
// Omni; regular relay/task logs keep their existing path unchanged.
func AppendGeminiOmniTaskLogMetadata(c *gin.Context, relayInfo *relaycommon.RelayInfo, taskData []byte) []byte {
	if relayInfo == nil || !relaycommon.IsGeminiOmniModel(relayInfo.OriginModelName) {
		return taskData
	}
	data := make(map[string]interface{})
	if len(taskData) > 0 {
		if err := common.Unmarshal(taskData, &data); err != nil || data == nil {
			data = make(map[string]interface{})
		}
	}
	if relayInfo.ReasoningEffort != "" {
		data["reasoning_effort"] = relayInfo.ReasoningEffort
	}
	model.AppendConfiguredClientRequestHeaders(c, relayInfo.UserId, data)
	appendUpstreamResponses(data, relayInfo)
	appendUsageConversion(data, relayInfo)
	other := model.NewLogOtherFromMap(data)
	AppendRelayLogAdminInfo(c, relayInfo, other)
	appendRequestPath(c, relayInfo, other)
	appendRequestConversionChain(relayInfo, other)
	appendFinalRequestFormat(relayInfo, other)
	appendBillingInfo(relayInfo, other)
	merged, err := common.Marshal(other)
	if err != nil {
		return taskData
	}
	return merged
}

func AppendResponseModelLogInfo(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || relayInfo.ResponseModel == nil || other == nil {
		return
	}
	observation := relayInfo.ResponseModel
	if observation.ReturnedModel == observation.RequestedModel &&
		(observation.UpstreamModel == "" || observation.UpstreamModel == observation.RequestedModel) &&
		(relayInfo.ChannelMeta == nil || !relayInfo.IsModelMapped) {
		return
	}
	other.SetPublic("response_model", *observation)
}

func appendParamOverrideInfo(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || other == nil || len(relayInfo.ParamOverrideAudit) == 0 {
		return
	}
	other.SetPublic("po", relayInfo.ParamOverrideAudit)
}

// appendRequestPluginRecord shows users what the request plugins changed in
// their request. A retry rewrites the attempt's entries in place, so the
// log keeps a copy.
func appendRequestPluginRecord(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || other == nil || len(relayInfo.RequestPluginRecord) == 0 {
		return
	}
	other.SetPublic("request_plugins", relaycommon.CapRequestPluginRecord(slices.Clone(relayInfo.RequestPluginRecord)))
}

func appendStreamStatus(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || other == nil || !relayInfo.IsStream || relayInfo.StreamStatus == nil {
		return
	}
	ss := relayInfo.StreamStatus
	status := "ok"
	if !ss.IsNormalEnd() || ss.HasErrors() || ss.ResponseFailed() {
		status = "error"
	}
	streamInfo := map[string]any{
		"status":     status,
		"end_reason": string(ss.EndReason),
	}
	if outcome := ss.ResponseOutcome(); outcome != "" {
		streamInfo["response_status"] = outcome
	}
	if ss.EndError != nil {
		streamInfo["end_error"] = ss.EndError.Error()
	}
	if ss.ErrorCount > 0 {
		streamInfo["error_count"] = ss.ErrorCount
		messages := make([]string, 0, len(ss.Errors))
		for _, e := range ss.Errors {
			messages = append(messages, e.Message)
		}
		streamInfo["errors"] = messages
	}
	other.SetPublic("stream_status", streamInfo)
}

func appendBillingInfo(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || other == nil {
		return
	}
	// billing_source: "wallet" or "subscription"
	if relayInfo.BillingSource != "" {
		other.SetPublic("billing_source", relayInfo.BillingSource)
	}
	if relayInfo.UserSetting.BillingPreference != "" {
		other.SetPublic("billing_preference", relayInfo.UserSetting.BillingPreference)
	}
	if relayInfo.BillingSource == "subscription" {
		if relayInfo.SubscriptionId != 0 {
			other.SetPublic("subscription_id", relayInfo.SubscriptionId)
		}
		if relayInfo.SubscriptionPreConsumed > 0 {
			other.SetPublic("subscription_pre_consumed", relayInfo.SubscriptionPreConsumed)
		}
		// post_delta: settlement delta applied after actual usage is known (can be negative for refund)
		if relayInfo.SubscriptionPostDelta != 0 {
			other.SetPublic("subscription_post_delta", relayInfo.SubscriptionPostDelta)
		}
		if relayInfo.SubscriptionPlanId != 0 {
			other.SetPublic("subscription_plan_id", relayInfo.SubscriptionPlanId)
		}
		if relayInfo.SubscriptionPlanTitle != "" {
			other.SetPublic("subscription_plan_title", relayInfo.SubscriptionPlanTitle)
		}
		// Compute "this request" subscription consumed + remaining
		consumed := relayInfo.SubscriptionPreConsumed + relayInfo.SubscriptionPostDelta
		usedFinal := relayInfo.SubscriptionAmountUsedAfterPreConsume + relayInfo.SubscriptionPostDelta
		if consumed < 0 {
			consumed = 0
		}
		if usedFinal < 0 {
			usedFinal = 0
		}
		if relayInfo.SubscriptionAmountTotal > 0 {
			remain := max(relayInfo.SubscriptionAmountTotal-usedFinal, 0)
			other.SetPublic("subscription_total", relayInfo.SubscriptionAmountTotal)
			other.SetPublic("subscription_used", usedFinal)
			other.SetPublic("subscription_remain", remain)
		}
		if consumed > 0 {
			other.SetPublic("subscription_consumed", consumed)
		}
		// Wallet quota is not deducted when billed from subscription.
		other.SetPublic("wallet_quota_deducted", 0)
	}
}

func appendRequestConversionChain(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || other == nil {
		return
	}
	if len(relayInfo.RequestConversionChain) == 0 {
		return
	}
	chain := make([]string, 0, len(relayInfo.RequestConversionChain))
	for _, f := range relayInfo.RequestConversionChain {
		switch f {
		case types.RelayFormatOpenAI:
			chain = append(chain, "OpenAI Compatible")
		case types.RelayFormatClaude:
			chain = append(chain, "Claude Messages")
		case types.RelayFormatGemini:
			chain = append(chain, "Google Gemini")
		case types.RelayFormatOpenAIResponses:
			chain = append(chain, "OpenAI Responses")
		default:
			chain = append(chain, string(f))
		}
	}
	if len(chain) == 0 {
		return
	}
	other.SetPublic("request_conversion", chain)
}

func appendFinalRequestFormat(relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if relayInfo == nil || other == nil {
		return
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		// claude indicates the final upstream request format is Claude Messages.
		// Frontend log rendering uses this to keep the original Claude input display.
		other.SetPublic("claude", true)
	}
}

func GenerateWssOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice, userGroupRatio float64) *model.LogOther {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, 0, 0.0, modelPrice, userGroupRatio)
	info.SetPublic("ws", true)
	info.SetPublic("audio_input", usage.InputTokenDetails.AudioTokens)
	info.SetPublic("audio_output", usage.OutputTokenDetails.AudioTokens)
	info.SetPublic("text_input", usage.InputTokenDetails.TextTokens)
	info.SetPublic("text_output", usage.OutputTokenDetails.TextTokens)
	info.SetPublic("audio_ratio", audioRatio)
	info.SetPublic("audio_completion_ratio", audioCompletionRatio)
	if usage.InputTokenDetails.CachedTokens > 0 {
		cachedAudio := cachedAudioTokensEstimate(usage)
		cachedText := usage.InputTokenDetails.CachedTokens - cachedAudio
		// cache_tokens 沿用"缓存读取Token"语义（文字缓存），与文本/Claude 路径一致
		info.SetPublic("cache_tokens", cachedText)
		// cache_audio_tokens：realtime 独有的音频缓存，单独列出
		if cachedAudio > 0 {
			info.SetPublic("cache_audio_tokens", cachedAudio)
		}
		if usage.InputTokenDetails.CachedTokensDetails != nil {
			info.SetPublic("cache_tokens_source", "upstream")
		} else {
			info.SetPublic("cache_tokens_source", "estimated")
		}
	}
	return info
}

func GenerateAudioOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice, userGroupRatio float64) *model.LogOther {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, 0, 0.0, modelPrice, userGroupRatio)
	info.SetPublic("audio", true)
	info.SetPublic("audio_input", usage.PromptTokensDetails.AudioTokens)
	info.SetPublic("audio_output", usage.CompletionTokenDetails.AudioTokens)
	info.SetPublic("text_input", usage.PromptTokensDetails.TextTokens)
	info.SetPublic("text_output", usage.CompletionTokenDetails.TextTokens+usage.CompletionTokenDetails.ReasoningTokens)
	info.SetPublic("audio_ratio", audioRatio)
	info.SetPublic("audio_completion_ratio", audioCompletionRatio)
	if usage.CompletionTokenDetails.ReasoningTokens > 0 {
		info.SetPublic("reasoning_tokens", usage.CompletionTokenDetails.ReasoningTokens)
	}
	return info
}

func GenerateClaudeOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64,
	cacheCreationTokens int, cacheCreationRatio float64,
	cacheCreationTokens5m int, cacheCreationRatio5m float64,
	cacheCreationTokens1h int, cacheCreationRatio1h float64,
	modelPrice float64, userGroupRatio float64) *model.LogOther {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, cacheTokens, cacheRatio, modelPrice, userGroupRatio)
	info.SetPublic("claude", true)
	info.SetPublic("cache_creation_tokens", cacheCreationTokens)
	info.SetPublic("cache_creation_ratio", cacheCreationRatio)
	if cacheCreationTokens5m != 0 {
		info.SetPublic("cache_creation_tokens_5m", cacheCreationTokens5m)
		info.SetPublic("cache_creation_ratio_5m", cacheCreationRatio5m)
	}
	if cacheCreationTokens1h != 0 {
		info.SetPublic("cache_creation_tokens_1h", cacheCreationTokens1h)
		info.SetPublic("cache_creation_ratio_1h", cacheCreationRatio1h)
	}
	return info
}

func GenerateMjOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, priceData hosttypes.PriceData) *model.LogOther {
	other := model.NewLogOther()
	other.SetPublic("model_price", priceData.ModelPrice)
	other.SetPublic("group_ratio", priceData.GroupRatioInfo.GroupRatio)
	if priceData.GroupRatioInfo.HasSpecialRatio {
		other.SetPublic("user_group_ratio", priceData.GroupRatioInfo.GroupSpecialRatio)
	}
	appendRequestPath(ctx, relayInfo, other)
	// 写入渠道成本折扣
	if ctx != nil {
		adminInfo := make(map[string]interface{})
		if costDiscount, ok := common.GetConfiguredCostDiscount(ctx); ok {
			adminInfo["cost_discount"] = costDiscount
		}
		if len(adminInfo) > 0 {
			other.MergeAdmin(adminInfo)
		}
	}
	appendRequestPluginRecord(relayInfo, other)
	return other
}

// InjectTieredBillingInfo overlays tiered billing fields onto an existing
// module-specific other map. Call this after GenerateTextOtherInfo /
// GenerateClaudeOtherInfo / etc. when the request used tiered_expr billing.
func InjectTieredBillingInfo(other *model.LogOther, relayInfo *relaycommon.RelayInfo, result *billingexpr.TieredResult) {
	if relayInfo == nil || other == nil {
		return
	}
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil {
		return
	}
	other.SetPublic("billing_mode", "tiered_expr")
	other.SetPublic("expr_b64", base64.StdEncoding.EncodeToString([]byte(snap.ExprString)))
	if strings.Contains(snap.ExprString, `param("prompt_cache_options.ttl")`) {
		// Preserve the request value used by param("prompt_cache_options.ttl") so
		// Java's billing explanation can select the same cache-write price branch.
		// Use the API default when omitted so Java can render the default cache-write
		// price even when the endpoint response does not echo prompt_cache_options.
		promptCacheTTL := "30m"
		if relayInfo.BillingRequestInput != nil && len(relayInfo.BillingRequestInput.Body) > 0 {
			ttl := gjson.GetBytes(relayInfo.BillingRequestInput.Body, "prompt_cache_options.ttl")
			if ttl.Type == gjson.String {
				promptCacheTTL = ttl.String()
			}
		}
		other.SetPublic("billing_prompt_cache_ttl", promptCacheTTL)
	}
	if result != nil {
		if tokens := result.BillingTokens; tokens != nil && result.BillingUnit == billingexpr.BillingUnitToken {
			other.SetPublic("image_cache_tokens", tokens.ImgCR)
			other.SetPublic("billing_tokens", map[string]float64{
				"p": tokens.P, "c": tokens.C, "len": tokens.Len,
				"cr": tokens.CR, "cc": tokens.CC, "cc1h": tokens.CC1h, "cc12h": tokens.CC12h,
				"img": tokens.Img, "img_cr": tokens.ImgCR, "img_o": tokens.ImgO,
				"ai": tokens.AI, "ao": tokens.AO,
			})
		}
		if result.ImageCount != nil {
			other.SetPublic("image_count", *result.ImageCount)
		}
		other.SetPublic("matched_tier", result.MatchedTier)
		if result.BillingUnit != "" {
			other.SetPublic("billing_unit", result.BillingUnit)
		}
		if result.FixedPrice != nil {
			other.SetPublic("fixed_price", *result.FixedPrice)
		}
		if len(result.RequestRules) > 0 {
			other.SetPublic("request_rules", result.RequestRules)
		}
	} else if snap.EstimatedBillingUnit != "" {
		if snap.EstimatedImageCount != nil {
			other.SetPublic("image_count", *snap.EstimatedImageCount)
		}
		other.SetPublic("matched_tier", snap.EstimatedTier)
		other.SetPublic("billing_unit", snap.EstimatedBillingUnit)
		if snap.EstimatedFixedPrice != nil {
			other.SetPublic("fixed_price", *snap.EstimatedFixedPrice)
		}
	}
}
