package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type TokenDetails struct {
	TextTokens        int
	AudioTokens       int
	ImageTokens       int
	CachedTokens      int
	CachedAudioTokens int // subset of CachedTokens that are audio; rest assumed text
}

type QuotaInfo struct {
	InputDetails  TokenDetails
	OutputDetails TokenDetails
	ModelName     string
	UsePrice      bool
	ModelPrice    float64
	ModelRatio    float64
	GroupRatio    float64
}

// realtimeCacheRatioMap 硬编码 realtime 各模型的缓存折扣比例（相对 textRatio）。
// 上游 cached_tokens 不区分文本/音频，统一用此比例折扣。
// gpt-realtime-2/1.5：文本缓存 $0.40 = 音频缓存 $0.40，两者均等于 0.1 × textRatio（精确）。
// gpt-realtime-mini：文本缓存 $0.06 = 0.1 × $0.60（精确）；音频缓存 $0.30 用相同比例（近似）。
var realtimeCacheRatioMap = map[string]float64{
	"gpt-realtime":                 0.1,
	"gpt-realtime-2025-08-28":      0.1,
	"gpt-realtime-2":               0.1,
	"gpt-realtime-2.1":             0.1,
	"gpt-realtime-1.5":             0.1,
	"gpt-realtime-mini":            0.1,
	"gpt-realtime-mini-2025-10-06": 0.1,
	"gpt-realtime-mini-2025-12-15": 0.1,
	"gpt-realtime-2.1-mini":        0.1,
}

func hasCustomModelRatio(modelName string, currentRatio float64) bool {
	defaultRatio, exists := ratio_setting.GetDefaultModelRatioMap()[modelName]
	if !exists {
		return true
	}
	return currentRatio != defaultRatio
}

// cachedAudioTokensEstimate returns the number of cached tokens that are audio.
// Uses cached_tokens_details when available (OpenAI native); otherwise estimates
// proportionally from the audio share of total input tokens (Azure GA fallback).
func cachedAudioTokensEstimate(usage *dto.RealtimeUsage) int {
	if usage == nil || usage.InputTokenDetails.CachedTokens == 0 {
		return 0
	}
	if d := usage.InputTokenDetails.CachedTokensDetails; d != nil {
		return d.AudioTokens
	}
	total := usage.InputTokenDetails.TextTokens + usage.InputTokenDetails.AudioTokens
	if total == 0 || usage.InputTokenDetails.AudioTokens == 0 {
		return 0
	}
	return common.QuotaFromFloat(float64(usage.InputTokenDetails.CachedTokens) * float64(usage.InputTokenDetails.AudioTokens) / float64(total))
}

func calculateAudioQuota(info QuotaInfo) (int, *common.QuotaClamp) {
	if info.UsePrice {
		modelPrice := decimal.NewFromFloat(info.ModelPrice)
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		groupRatio := decimal.NewFromFloat(info.GroupRatio)

		quota := modelPrice.Mul(quotaPerUnit).Mul(groupRatio)
		return common.QuotaFromDecimalChecked(quota)
	}

	completionRatio := decimal.NewFromFloat(ratio_setting.GetCompletionRatio(info.ModelName))
	audioRatioF := ratio_setting.GetAudioRatio(info.ModelName)
	audioRatio := decimal.NewFromFloat(audioRatioF)

	// 音频补全倍率：未配置时默认 1.0
	audioCompletionRatioF := ratio_setting.GetAudioCompletionRatio(info.ModelName)
	if audioCompletionRatioF <= 0 {
		audioCompletionRatioF = 1.0
	}

	// 音频输出倍率 = 音频输入倍率 × 音频补全倍率；无音频输入倍率时回退到 1.0（等同文本输入倍率）
	// 此值后续会与 modelRatio × groupRatio 相乘，故回退不能用 ModelRatio，否则会导致 ModelRatio 被乘两次
	effectiveAudioOutputRatio := audioRatioF * audioCompletionRatioF
	if audioRatioF <= 0 {
		effectiveAudioOutputRatio = 1.0
	}
	dEffectiveAudioOutputRatio := decimal.NewFromFloat(effectiveAudioOutputRatio)

	inputTextTokens := decimal.NewFromInt(int64(info.InputDetails.TextTokens))
	outputTextTokens := decimal.NewFromInt(int64(info.OutputDetails.TextTokens))
	inputAudioTokens := decimal.NewFromInt(int64(info.InputDetails.AudioTokens))
	outputAudioTokens := decimal.NewFromInt(int64(info.OutputDetails.AudioTokens))
	inputImageTokens := decimal.NewFromInt(int64(info.InputDetails.ImageTokens))

	quota := decimal.Zero
	// 文本部分：使用 modelRatio 作为基础倍率
	groupRatio := decimal.NewFromFloat(info.GroupRatio)
	modelRatio := decimal.NewFromFloat(info.ModelRatio)
	textRatio := groupRatio.Mul(modelRatio)
	quota = quota.Add(inputTextTokens.Mul(textRatio))
	quota = quota.Add(outputTextTokens.Mul(completionRatio).Mul(textRatio))

	// 音频部分：输入用 audioRatio × modelRatio；输出用 audioRatio × audioCompletionRatio × modelRatio
	quota = quota.Add(inputAudioTokens.Mul(audioRatio).Mul(textRatio))
	quota = quota.Add(outputAudioTokens.Mul(dEffectiveAudioOutputRatio).Mul(textRatio))

	// 图片部分：输入图片用 imageRatio × modelRatio
	if info.InputDetails.ImageTokens > 0 {
		if imageRatioF, ok := ratio_setting.GetImageRatio(info.ModelName); ok {
			imageRatio := decimal.NewFromFloat(imageRatioF)
			quota = quota.Add(inputImageTokens.Mul(imageRatio).Mul(textRatio))
		}
	}

	// 缓存折扣：cached_tokens 是输入 token 的子集，已按全价计入上方，此处补扣折扣差额。
	// 目标：所有缓存 token 最终均按 cacheRatio × textRatio 计费。
	// 文字缓存：原按 textRatio 收，退差 = cached_text × (cacheRatio-1) × textRatio
	// 音频缓存：原按 audioRatio × textRatio 收，退差 = cached_audio × (cacheRatio-audioRatio) × textRatio
	// 若上游未返回 cached_tokens_details（如 Azure GA），按文字/音频比例估算。
	if info.InputDetails.CachedTokens > 0 {
		if cacheRatio, ok := realtimeCacheRatioMap[info.ModelName]; ok {
			cachedAudio := info.InputDetails.CachedAudioTokens
			cachedText := info.InputDetails.CachedTokens - cachedAudio
			if cachedText > 0 {
				adj := decimal.NewFromInt(int64(cachedText)).Mul(decimal.NewFromFloat(cacheRatio - 1)).Mul(textRatio)
				quota = quota.Add(adj)
			}
			if cachedAudio > 0 {
				// 音频缓存目标价 = cacheRatio × textRatio；原已按 audioRatio × textRatio 计费
				adj := decimal.NewFromInt(int64(cachedAudio)).Mul(decimal.NewFromFloat(cacheRatio - audioRatioF)).Mul(textRatio)
				quota = quota.Add(adj)
			}
		}
	}

	// If quota is less than or equal to zero, set quota to 1
	if quota.LessThanOrEqual(decimal.Zero) {
		quota = decimal.NewFromInt(1)
	}

	return common.QuotaFromDecimalChecked(quota)
}

func PreWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage) error {
	if relayInfo.UsePrice {
		return nil
	}
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return err
	}

	token, err := model.GetTokenByKey(strings.TrimPrefix(relayInfo.TokenKey, "sk-"), false)
	if err != nil {
		return err
	}

	modelName := relayInfo.OriginModelName
	textInputTokens := usage.InputTokenDetails.TextTokens
	textOutTokens := usage.OutputTokenDetails.TextTokens
	audioInputTokens := usage.InputTokenDetails.AudioTokens
	audioOutTokens := usage.OutputTokenDetails.AudioTokens
	groupRatio := ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	modelRatio, _, _ := ratio_setting.GetModelRatio(modelName)

	autoGroup, exists := common.GetContextKey(ctx, constant.ContextKeyAutoGroup)
	if exists {
		groupRatio = ratio_setting.GetGroupRatio(autoGroup.(string))
		logger.LogDebug(ctx, "final group ratio: %f", groupRatio)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	actualGroupRatio := groupRatio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		actualGroupRatio = userGroupRatio
	}

	cachedAudioTokens := cachedAudioTokensEstimate(usage)
	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:        textInputTokens,
			AudioTokens:       audioInputTokens,
			ImageTokens:       usage.InputTokenDetails.ImageTokens,
			CachedTokens:      usage.InputTokenDetails.CachedTokens,
			CachedAudioTokens: cachedAudioTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:  modelName,
		UsePrice:   relayInfo.UsePrice,
		ModelRatio: modelRatio,
		GroupRatio: actualGroupRatio,
	}

	quota, clamp := calculateAudioQuota(quotaInfo)
	noteQuotaClamp(relayInfo, clamp)

	if userQuota < quota && !IsNegativeBalanceAllowed(ctx) {
		return fmt.Errorf("user quota is not enough, user quota: %s, need quota: %s", logger.FormatQuota(userQuota), logger.FormatQuota(quota))
	}

	if !token.UnlimitedQuota && token.RemainQuota < quota {
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(token.RemainQuota), logger.FormatQuota(quota))
	}

	err = PostConsumeQuota(relayInfo, quota, 0, false)
	if err != nil {
		return err
	}
	logger.LogInfo(ctx, "realtime streaming consume quota success, quota: "+fmt.Sprintf("%d", quota))
	return nil
}

func PostWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelName string,
	usage *dto.RealtimeUsage, extraContent string) {

	var tieredResult *billingexpr.TieredResult
	tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, billingexpr.TokenParams{
		P:   float64(usage.InputTokens),
		C:   float64(usage.OutputTokens),
		Len: float64(usage.InputTokens),
	})
	if tieredOk {
		tieredResult = tieredRes
	}

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	textInputTokens := usage.InputTokenDetails.TextTokens
	textOutTokens := usage.OutputTokenDetails.TextTokens

	audioInputTokens := usage.InputTokenDetails.AudioTokens
	audioOutTokens := usage.OutputTokenDetails.AudioTokens

	tokenName := ctx.GetString("token_name")
	completionRatio := decimal.NewFromFloat(ratio_setting.GetCompletionRatio(modelName))
	audioRatio := ratio_setting.GetAudioRatio(relayInfo.OriginModelName)
	audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(relayInfo.OriginModelName)
	if audioCompletionRatio <= 0 {
		audioCompletionRatio = 1.0
	}
	// 音频输出倍率 = 音频输入倍率 × 音频补全倍率；无音频输入倍率时回退到 1.0
	effectiveAudioOutputRatio := audioRatio * audioCompletionRatio
	if audioRatio <= 0 {
		effectiveAudioOutputRatio = 1.0
	}

	modelRatio := relayInfo.PriceData.ModelRatio
	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	modelPrice := relayInfo.PriceData.ModelPrice
	usePrice := relayInfo.PriceData.UsePrice

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:        textInputTokens,
			AudioTokens:       audioInputTokens,
			ImageTokens:       usage.InputTokenDetails.ImageTokens,
			CachedTokens:      usage.InputTokenDetails.CachedTokens,
			CachedAudioTokens: cachedAudioTokensEstimate(usage),
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:  modelName,
		UsePrice:   usePrice,
		ModelRatio: modelRatio,
		GroupRatio: groupRatio,
	}

	quota, clamp := calculateAudioQuota(quotaInfo)
	noteQuotaClamp(relayInfo, clamp)
	if tieredOk {
		quota = tieredQuota
	}

	totalTokens := usage.TotalTokens
	var logContent string
	if !usePrice {
		audioOutputRatioSource := "音频输入倍率"
		if audioRatio <= 0 {
			audioOutputRatioSource = "文本输入倍率"
		}
		logContent = fmt.Sprintf("模型倍率 %.2f，补全倍率 %.2f，音频输入倍率 %.2f，音频输出倍率 %.2f（%s），分组倍率 %.2f",
			modelRatio, completionRatio.InexactFloat64(), audioRatio, effectiveAudioOutputRatio, audioOutputRatioSource, groupRatio)
	} else {
		logContent = fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", modelPrice, groupRatio)
	}

	// record all the consume log even if quota is 0
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
		logContent += "（可能是上游超时）"
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, "+
			"tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, modelName, relayInfo.FinalPreConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	if err := SettleBilling(ctx, relayInfo, quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	logModel := modelName
	if extraContent != "" {
		logContent += ", " + extraContent
	}
	other := GenerateWssOtherInfo(ctx, relayInfo, usage, modelRatio, groupRatio,
		completionRatio.InexactFloat64(), audioRatio, audioCompletionRatio, modelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	if tieredResult != nil {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)
	}

	// 将系统生成的 request_id 存入 other，request_id 字段改存上游返回的 ID
	systemRequestId := ctx.GetString(common.RequestIdKey)
	if systemRequestId != "" {
		other.SetPublic("system_request_id", systemRequestId)
	}

	attachQuotaSaturation(ctx, relayInfo, other)

	priceChain := CalculatePriceChainForLog(ctx, logModel, usage.InputTokens, usage.OutputTokens, quota)
	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		RequestId:        relayInfo.UpstreamResponseId,
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		ModelName:        logModel,
		TokenName:        tokenName,
		Quota:            quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(useTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other.Snapshot(),
		PriceChain:       priceChain,
	})
}

func CalcOpenRouterCacheCreateTokens(usage dto.Usage, priceData types.PriceData) int {
	if priceData.CacheCreationRatio == 1 {
		return 0
	}
	quotaPrice := priceData.ModelRatio / common.QuotaPerUnit
	promptCacheCreatePrice := quotaPrice * priceData.CacheCreationRatio
	promptCacheReadPrice := quotaPrice * priceData.CacheRatio
	completionPrice := quotaPrice * priceData.CompletionRatio

	cost, _ := usage.Cost.(float64)
	totalPromptTokens := float64(usage.PromptTokens)
	completionTokens := float64(usage.CompletionTokens)
	promptCacheReadTokens := float64(usage.PromptTokensDetails.CachedTokens)

	value := (cost -
		totalPromptTokens*quotaPrice +
		promptCacheReadTokens*(quotaPrice-promptCacheReadPrice) -
		completionTokens*completionPrice) /
		(promptCacheCreatePrice - quotaPrice)
	quota, clamp := common.QuotaRoundChecked(value)
	if clamp != nil {
		return -1
	}
	return quota
}

func PostAudioConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent string) {

	var tieredUsedVars map[string]bool
	if snap := relayInfo.TieredBillingSnapshot; snap != nil {
		tieredUsedVars = billingexpr.UsedVars(snap.ExprString)
	}
	var tieredResult *billingexpr.TieredResult
	tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, BuildTieredTokenParams(usage, false, tieredUsedVars))
	if tieredOk {
		tieredResult = tieredRes
	}

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	textInputTokens := usage.PromptTokensDetails.TextTokens
	textOutTokens := usage.CompletionTokenDetails.TextTokens + usage.CompletionTokenDetails.ReasoningTokens

	audioInputTokens := usage.PromptTokensDetails.AudioTokens
	audioOutTokens := usage.CompletionTokenDetails.AudioTokens

	tokenName := ctx.GetString("token_name")
	billingModelName := relayInfo.GetBillingModelName()
	completionRatio := decimal.NewFromFloat(ratio_setting.GetCompletionRatio(billingModelName))
	audioRatioF := ratio_setting.GetAudioRatio(billingModelName)
	audioCompletionRatioF := ratio_setting.GetAudioCompletionRatio(billingModelName)
	if audioCompletionRatioF <= 0 {
		audioCompletionRatioF = 1.0
	}
	// 音频输出倍率 = 音频输入倍率 × 音频补全倍率；无音频输入倍率时回退到 1.0
	effectiveAudioOutputRatio := audioRatioF * audioCompletionRatioF
	if audioRatioF <= 0 {
		effectiveAudioOutputRatio = 1.0
	}

	modelRatio := relayInfo.PriceData.ModelRatio
	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	modelPrice := relayInfo.PriceData.ModelPrice
	usePrice := relayInfo.PriceData.UsePrice

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:  billingModelName,
		UsePrice:   usePrice,
		ModelRatio: modelRatio,
		GroupRatio: groupRatio,
	}

	quota, clamp := calculateAudioQuota(quotaInfo)
	noteQuotaClamp(relayInfo, clamp)
	if tieredOk {
		quota = tieredQuota
	}

	totalTokens := usage.TotalTokens
	var logContent string
	if !usePrice {
		audioOutputRatioSource := "音频输入倍率"
		if audioRatioF <= 0 {
			audioOutputRatioSource = "文本输入倍率"
		}
		logContent = fmt.Sprintf("模型倍率 %.2f，补全倍率 %.2f，音频输入倍率 %.2f，音频输出倍率 %.2f（%s），分组倍率 %.2f",
			modelRatio, completionRatio.InexactFloat64(), audioRatioF, effectiveAudioOutputRatio, audioOutputRatioSource, groupRatio)
	} else {
		logContent = fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", modelPrice, groupRatio)
	}

	// record all the consume log even if quota is 0
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
		logContent += "（可能是上游超时）"
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, "+
			"tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, billingModelName, relayInfo.FinalPreConsumedQuota))
	} else {
		// 有 BillingSession 时：
		//   - 钱包用户：ConsumeUserQuotaSettle 一次 UPDATE 完成 quota 扣减 + used_quota + request_count
		//   - 订阅用户：quota 已由 SubscriptionFunding 处理，只需更新 used_quota + request_count
		if relayInfo.Billing != nil && relayInfo.BillingSource != BillingSourceSubscription {
			preConsumed := relayInfo.Billing.GetPreConsumedQuota()
			delta := quota - preConsumed
			if err := model.ConsumeUserQuotaSettle(relayInfo.UserId, quota, delta); err != nil {
				logger.LogError(ctx, "error consume user quota settle: "+err.Error())
			}
		} else {
			model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		}
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	if err := SettleBilling(ctx, relayInfo, quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	logModel := billingModelName
	if extraContent != "" {
		logContent += ", " + extraContent
	}
	other := GenerateAudioOtherInfo(ctx, relayInfo, usage, modelRatio, groupRatio,
		completionRatio.InexactFloat64(), audioRatioF, audioCompletionRatioF, modelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	if tieredResult != nil {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)
	}

	// 将系统生成的 request_id 存入 other，request_id 字段改存上游返回的 ID
	systemRequestId := ctx.GetString(common.RequestIdKey)
	if systemRequestId != "" {
		other.SetPublic("system_request_id", systemRequestId)
	}

	attachQuotaSaturation(ctx, relayInfo, other)

	priceChain := CalculatePriceChainForLog(ctx, logModel, usage.PromptTokens, usage.CompletionTokens, quota)
	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		RequestId:        relayInfo.UpstreamResponseId,
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        logModel,
		TokenName:        tokenName,
		Quota:            quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(useTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other.Snapshot(),
		PriceChain:       priceChain,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(usage.CompletionTokens))
	})
}

func PreConsumeTokenQuota(relayInfo *relaycommon.RelayInfo, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if relayInfo.IsPlayground {
		return nil
	}
	// 原子预扣：检查与扣减在同一操作中完成，并发请求不可能同时通过检查后超扣。
	reserved, err := model.TryReserveTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota, relayInfo.TokenUnlimited)
	if err != nil {
		return err
	}
	if !reserved {
		remainQuota := 0
		if token, tokenErr := model.GetTokenByKey(relayInfo.TokenKey, false); tokenErr == nil && token != nil {
			remainQuota = token.RemainQuota
		}
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(remainQuota), logger.FormatQuota(quota))
	}
	return nil
}

type postConsumeQuotaResult struct {
	FundingApplied bool
	TokenApplied   bool
}

func PostConsumeQuota(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int, sendEmail bool) error {
	_, err := postConsumeQuotaWithResult(relayInfo, quota, preConsumedQuota, sendEmail)
	return err
}

func postConsumeQuotaWithResult(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int, sendEmail bool) (result postConsumeQuotaResult, err error) {

	// 1) Consume from wallet quota OR subscription item
	if relayInfo != nil && relayInfo.BillingSource == BillingSourceSubscription {
		if relayInfo.SubscriptionId == 0 {
			return result, errors.New("subscription id is missing")
		}
		delta := int64(quota)
		if delta != 0 {
			if err := model.PostConsumeUserSubscriptionDelta(relayInfo.SubscriptionId, delta); err != nil {
				return result, err
			}
			relayInfo.SubscriptionPostDelta += delta
		}
	} else {
		// Wallet
		if quota > 0 {
			err = model.DecreaseUserQuota(relayInfo.UserId, quota, false)
		} else {
			err = model.IncreaseUserQuota(relayInfo.UserId, -quota, false)
		}
		if err != nil {
			return result, err
		}
	}
	result.FundingApplied = true

	if !relayInfo.IsPlayground {
		if quota > 0 {
			err = model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota)
		} else {
			err = model.IncreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, -quota)
		}
		if err != nil {
			return result, err
		}
		result.TokenApplied = true
	}

	if sendEmail {
		if (quota + preConsumedQuota) != 0 {
			checkAndSendQuotaNotify(relayInfo, quota, preConsumedQuota)
		}
	}

	return result, nil
}

// defaultQuotaWarningAPIEndFallback 是未配置 GRAVITEX_API_END 环境变量时的默认 Java 后端地址（生产环境）。
const defaultQuotaWarningAPIEndFallback = "https://maas.gravitex.ai/prod-api"

// DefaultQuotaWarningWebhookURL 返回额度预警默认回调地址（Java 后端统一通知入口）。
// 基址取自环境变量 GRAVITEX_API_END（去掉尾部斜杠），未配置时回落到生产环境地址，
// 再拼接固定路径 /api/user/quota-warning-notify/send。
// 用户未在个人设置中自定义 webhook 地址时，默认回调到此接口。
func DefaultQuotaWarningWebhookURL() string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GRAVITEX_API_END")), "/")
	if base == "" {
		base = defaultQuotaWarningAPIEndFallback
	}
	return base + "/api/user/quota-warning-notify/send"
}

func checkAndSendQuotaNotify(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int) {
	gopool.Go(func() {
		userSetting := relayInfo.UserSetting
		threshold := common.QuotaRemindThreshold
		if userSetting.QuotaWarningThreshold != 0 {
			threshold = int(userSetting.QuotaWarningThreshold)
		}

		//noMoreQuota := userCache.Quota-(quota+preConsumedQuota) <= 0
		quotaTooLow := false
		consumeQuota := quota + preConsumedQuota
		if relayInfo.UserQuota-consumeQuota < threshold {
			quotaTooLow = true
		}
		if quotaTooLow {
			prompt := "您的额度即将用尽"
			topUpLink := PaymentReturnURL("/wallet")

			// 根据通知方式生成不同的内容格式
			var content string
			var values []interface{}

			notifyType := userSetting.NotifyType
			if notifyType == "" {
				notifyType = dto.NotifyTypeEmail
			}

			if notifyType == dto.NotifyTypeBark {
				// Bark推送使用简短文本，不支持HTML
				content = "{{value}}，剩余额度：{{value}}，请及时充值"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota)}
			} else if notifyType == dto.NotifyTypeGotify {
				content = "{{value}}，当前剩余额度为 {{value}}，请及时充值。"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota)}
			} else {
				// 默认内容格式，适用于Email和Webhook（支持HTML）
				content = "{{value}}，当前剩余额度为 {{value}}，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='{{value}}'>{{value}}</a>"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota), topUpLink, topUpLink}
			}

			notifyData := dto.NewNotify(dto.NotifyTypeQuotaExceed, prompt, content, values)
			if notifyType == dto.NotifyTypeWebhook {
				err := SendQuotaWarningNotifyToAPIEnd(
					relayInfo.UserId,
					notifyType,
					int64(relayInfo.UserQuota-consumeQuota),
					int64(threshold),
				)
				if err != nil {
					common.SysError(fmt.Sprintf("failed to send webhook quota notify to Java for user %d: %s", relayInfo.UserId, err.Error()))
				}
				return
			}
			if notifyType == dto.NotifyTypeEmail {
				err := SendQuotaWarningNotifyToAPIEnd(
					relayInfo.UserId,
					notifyType,
					int64(relayInfo.UserQuota-consumeQuota),
					int64(threshold),
				)
				if err != nil {
					common.SysError(fmt.Sprintf("failed to send email quota notify to Java for user %d: %s", relayInfo.UserId, err.Error()))
				}
				return
			}

			err := NotifyUser(relayInfo.UserId, relayInfo.UserEmail, relayInfo.UserSetting, notifyData)
			if err != nil {
				common.SysError(fmt.Sprintf("failed to send quota notify to user %d: %s", relayInfo.UserId, err.Error()))
			}
		}
	})
}

func checkAndSendSubscriptionQuotaNotify(relayInfo *relaycommon.RelayInfo) {
	gopool.Go(func() {
		if relayInfo == nil {
			return
		}
		if relayInfo.SubscriptionId == 0 || relayInfo.SubscriptionAmountTotal <= 0 {
			return
		}

		userSetting := relayInfo.UserSetting
		threshold := common.QuotaRemindThreshold
		if userSetting.QuotaWarningThreshold != 0 {
			threshold = int(userSetting.QuotaWarningThreshold)
		}

		usedAfter := relayInfo.SubscriptionAmountUsedAfterPreConsume + relayInfo.SubscriptionPostDelta
		remaining := relayInfo.SubscriptionAmountTotal - usedAfter
		if remaining >= int64(threshold) {
			return
		}

		prompt := "您的订阅额度即将用尽"
		topUpLink := PaymentReturnURL("/wallet")

		var content string
		var values []interface{}
		notifyType := userSetting.NotifyType
		if notifyType == "" {
			notifyType = dto.NotifyTypeEmail
		}

		if notifyType == dto.NotifyTypeBark {
			content = "{{value}}，剩余额度：{{value}}，请及时充值"
			values = []interface{}{prompt, logger.FormatQuota(int(remaining))}
		} else if notifyType == dto.NotifyTypeGotify {
			content = "{{value}}，当前剩余额度为 {{value}}，请及时充值。"
			values = []interface{}{prompt, logger.FormatQuota(int(remaining))}
		} else {
			content = "{{value}}，当前剩余额度为 {{value}}，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='{{value}}'>{{value}}</a>"
			values = []interface{}{prompt, logger.FormatQuota(int(remaining)), topUpLink, topUpLink}
		}

		notifyData := dto.NewNotify(dto.NotifyTypeQuotaExceed, prompt, content, values)
		if notifyType == dto.NotifyTypeWebhook {
			err := SendQuotaWarningNotifyToAPIEnd(
				relayInfo.UserId,
				notifyType,
				remaining,
				int64(threshold),
			)
			if err != nil {
				common.SysError(fmt.Sprintf("failed to send webhook subscription quota notify to Java for user %d: %s", relayInfo.UserId, err.Error()))
			}
			return
		}
		if notifyType == dto.NotifyTypeEmail {
			err := SendQuotaWarningNotifyToAPIEnd(
				relayInfo.UserId,
				notifyType,
				remaining,
				int64(threshold),
			)
			if err != nil {
				common.SysError(fmt.Sprintf("failed to send subscription email quota notify to Java for user %d: %s", relayInfo.UserId, err.Error()))
			}
			return
		}

		if err := NotifyUser(relayInfo.UserId, relayInfo.UserEmail, relayInfo.UserSetting, notifyData); err != nil {
			common.SysError(fmt.Sprintf("failed to send subscription quota notify to user %d: %s", relayInfo.UserId, err.Error()))
		}
	})
}
