package service

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type TokenDetails struct {
	TextTokens  int
	AudioTokens int
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

func hasCustomModelRatio(modelName string, currentRatio float64) bool {
	defaultRatio, exists := ratio_setting.GetDefaultModelRatioMap()[modelName]
	if !exists {
		return true
	}
	return currentRatio != defaultRatio
}

func calculateAudioQuota(info QuotaInfo) int {
	if info.UsePrice {
		modelPrice := decimal.NewFromFloat(info.ModelPrice)
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		groupRatio := decimal.NewFromFloat(info.GroupRatio)

		quota := modelPrice.Mul(quotaPerUnit).Mul(groupRatio)
		return int(quota.IntPart())
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

	// If quota is less than or equal to zero, set quota to 1
	if quota.LessThanOrEqual(decimal.Zero) {
		quota = decimal.NewFromInt(1)
	}

	return int(quota.Round(0).IntPart())
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
		log.Printf("final group ratio: %f", groupRatio)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	actualGroupRatio := groupRatio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		actualGroupRatio = userGroupRatio
	}

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
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

	quota := calculateAudioQuota(quotaInfo)

	if userQuota < quota {
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
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
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

	quota := calculateAudioQuota(quotaInfo)
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
		other["system_request_id"] = systemRequestId
	}

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
		Other:            other,
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

	return int(math.Round((cost -
		totalPromptTokens*quotaPrice +
		promptCacheReadTokens*(quotaPrice-promptCacheReadPrice) -
		completionTokens*completionPrice) /
		(promptCacheCreatePrice - quotaPrice)))
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
	completionRatio := decimal.NewFromFloat(ratio_setting.GetCompletionRatio(relayInfo.OriginModelName))
	audioRatioF := ratio_setting.GetAudioRatio(relayInfo.OriginModelName)
	audioCompletionRatioF := ratio_setting.GetAudioCompletionRatio(relayInfo.OriginModelName)
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
		ModelName:  relayInfo.OriginModelName,
		UsePrice:   usePrice,
		ModelRatio: modelRatio,
		GroupRatio: groupRatio,
	}

	quota := calculateAudioQuota(quotaInfo)
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
			"tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, relayInfo.OriginModelName, relayInfo.FinalPreConsumedQuota))
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

	logModel := relayInfo.OriginModelName
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
		other["system_request_id"] = systemRequestId
	}

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
		Other:            other,
		PriceChain:       priceChain,
	})
}

func PreConsumeTokenQuota(relayInfo *relaycommon.RelayInfo, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if relayInfo.IsPlayground {
		return nil
	}
	//if relayInfo.TokenUnlimited {
	//	return nil
	//}
	token, err := model.GetTokenByKey(relayInfo.TokenKey, false)
	if err != nil {
		return err
	}
	if !relayInfo.TokenUnlimited && token.RemainQuota < quota {
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(token.RemainQuota), logger.FormatQuota(quota))
	}
	err = model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota)
	if err != nil {
		return err
	}
	return nil
}

func PostConsumeQuota(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int, sendEmail bool) (err error) {

	// 1) Consume from wallet quota OR subscription item
	if relayInfo != nil && relayInfo.BillingSource == BillingSourceSubscription {
		if relayInfo.SubscriptionId == 0 {
			return errors.New("subscription id is missing")
		}
		delta := int64(quota)
		if delta != 0 {
			if err := model.PostConsumeUserSubscriptionDelta(relayInfo.SubscriptionId, delta); err != nil {
				return err
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
			return err
		}
	}

	if !relayInfo.IsPlayground {
		if quota > 0 {
			err = model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota)
		} else {
			err = model.IncreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, -quota)
		}
		if err != nil {
			return err
		}
	}

	if sendEmail {
		if (quota + preConsumedQuota) != 0 {
			checkAndSendQuotaNotify(relayInfo, quota, preConsumedQuota)
		}
	}

	return nil
}

func checkAndSendQuotaNotify(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int) {
	gopool.Go(func() {

		if relayInfo == nil {
			return
		}
		userSetting := relayInfo.UserSetting
		threshold := common.QuotaRemindThreshold
		if userSetting.QuotaWarningThreshold != 0 {
			threshold = int(userSetting.QuotaWarningThreshold)
		}

		consumeQuota := quota + preConsumedQuota
		remainingQuota := relayInfo.UserQuota - consumeQuota
		if remainingQuota < threshold {
			err := SendQuotaWarningNotifyToAPIEnd(relayInfo.UserId, userSetting.NotifyType, int64(remainingQuota), int64(threshold))
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

		if err := SendQuotaWarningNotifyToAPIEnd(relayInfo.UserId, userSetting.NotifyType, remaining, int64(threshold)); err != nil {
			common.SysError(fmt.Sprintf("failed to send subscription quota notify to user %d: %s", relayInfo.UserId, err.Error()))
		}
	})
}
