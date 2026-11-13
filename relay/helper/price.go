package helper

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func modelPriceNotConfiguredError(c *gin.Context, modelName string, userId int) error {
	key := i18n.MsgRelayModelPriceNotConfigured
	if model.IsAdmin(userId) {
		key = i18n.MsgRelayModelPriceNotConfiguredAdmin
	}
	return errors.New(i18n.T(c, key, map[string]any{"Model": modelName}))
}

// https://docs.claude.com/en/docs/build-with-claude/prompt-caching#1-hour-cache-duration
const claudeCacheCreation1hMultiplier = 6 / 3.75

// DefaultTieredPreConsumeMaxTokens is the bounded completion-token estimate
// used by the Interactions task billing adapter (alpha). Ordinary text/tiered
// pre-consume follows quota_setting.pre_consume_multiplier (官方 #146).
const DefaultTieredPreConsumeMaxTokens = 8192

// HandleGroupRatio checks for "auto_group" in the context and updates the group ratio and relayInfo.UsingGroup if present
func HandleGroupRatio(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) hosttypes.GroupRatioInfo {
	groupRatioInfo := hosttypes.GroupRatioInfo{
		GroupRatio:        1.0, // default ratio
		GroupSpecialRatio: -1,
	}

	// check auto group
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, "final group: %s", autoGroup)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	// check user group special ratio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		// user group special ratio
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		// normal group ratio
		groupRatioInfo.GroupRatio = ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	}

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (hosttypes.PriceData, error) {
	if info != nil {
		if matched := resolveBillingModelName(info.GetOriginModelName()); matched != "" && matched != info.OriginModelName {
			info.BillingModelName = matched
		}
	}
	billingModelName := info.GetBillingModelName()
	if imagePricing, ok := ratio_setting.GetImageModelPriceConfig(billingModelName); ok {
		groupRatioInfo := HandleGroupRatio(c, info)
		request, _ := info.Request.(*dto.ImageRequest)
		estimatedPrice, estimatedUsage := EstimateImagePerImageCost(imagePricing, request)
		imageUnitPrice := 0.0
		if estimatedUsage != nil {
			imageUnitPrice = ImageOutputUnitPriceForUsage(imagePricing, estimatedUsage)
		}
		preConsumedQuota, err := common.QuotaFromFloatStrict(estimatedPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData := hosttypes.PriceData{
			UsePrice:             true,
			ModelPrice:           estimatedPrice,
			PerImageUnitPrice:    imageUnitPrice,
			ImagePriceMultiplier: 1,
			ImagePerImagePricing: &imagePricing,
			ImageBillingUsage:    estimatedUsage,
			CompletionRatio:      ratio_setting.GetCompletionRatio(billingModelName),
			ImageCompletionRatio: ratio_setting.GetImageCompletionRatio(billingModelName),
			GroupRatioInfo:       groupRatioInfo,
			QuotaToPreConsume:    preConsumedQuota,
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume && groupRatioInfo.GroupRatio == 0 {
			priceData.FreeModel = true
			priceData.QuotaToPreConsume = 0
		}
		info.PriceData = priceData
		return priceData, nil
	}

	// 与 nebula 一致：优先按张计费（ImageModelPricePerImage），在 GetModelPrice 之前检查
	imageModelPrice, hasImageModelPrice := ratio_setting.GetImageModelPricePerImage(billingModelName)
	if hasImageModelPrice && imageModelPrice > 0 {
		groupRatioInfo := HandleGroupRatio(c, info)
		modelPrice := imageModelPrice
		imagePriceMultiplier := 1.0
		if meta != nil && meta.ImagePriceRatio != 0 {
			imagePriceMultiplier = meta.ImagePriceRatio
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
		preConsumedQuota, err := common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		completionRatio := ratio_setting.GetCompletionRatio(billingModelName)
		imageCompletionRatio := ratio_setting.GetImageCompletionRatio(billingModelName)
		priceData := hosttypes.PriceData{
			UsePrice:             true,
			ModelPrice:           modelPrice,
			PerImageUnitPrice:    imageModelPrice,
			ImagePriceMultiplier: imagePriceMultiplier,
			CompletionRatio:      completionRatio,
			ImageCompletionRatio: imageCompletionRatio,
			GroupRatioInfo:       groupRatioInfo,
			QuotaToPreConsume:    preConsumedQuota,
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume && groupRatioInfo.GroupRatio == 0 {
			priceData.FreeModel = true
			priceData.QuotaToPreConsume = 0
		} else if operation_setting.GetQuotaSetting().EnableFreeModelPreConsume && modelPrice == 0 {
			priceData.FreeModel = true
			priceData.QuotaToPreConsume = 0
		}
		info.PriceData = priceData
		return priceData, nil
	}

	modelPrice, usePrice := ratio_setting.GetModelPrice(billingModelName, false)

	groupRatioInfo := HandleGroupRatio(c, info)

	// Check if this model uses tiered_expr billing
	if billing_setting.GetBillingMode(billingModelName) == billing_setting.BillingModeTieredExpr {
		return modelPriceHelperTiered(c, info, billingModelName, promptTokens, groupRatioInfo)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var imageCompletionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var cacheCreationRatio12h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var videoRatio float64
	var freeModel bool
	if !usePrice {
		preConsumeMultiplier, err := operation_setting.InputPreConsumeMultiplier()
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedTokens := float64(promptTokens) * preConsumeMultiplier
		var success bool
		var matchName string
		modelRatio, success, matchName = ratio_setting.GetModelRatio(billingModelName)
		if !success {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(c, matchName, info.UserId)
			}
		}
		completionRatio = ratio_setting.GetCompletionRatio(billingModelName)
		imageCompletionRatio = ratio_setting.GetImageCompletionRatio(billingModelName)
		cacheRatio, _ = ratio_setting.GetCacheRatio(billingModelName)
		cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(billingModelName)
		cacheCreationRatio5m = cacheCreationRatio
		// 优先使用后台配置的 1h 缓存创建倍率，未配置时按官方 1h/5m 缓存写入价格比例推算
		if ratio1h, ok := ratio_setting.GetCreateCache1hRatio(billingModelName); ok {
			cacheCreationRatio1h = ratio1h
		} else {
			cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier
		}
		cacheCreationRatio12h = cacheCreationRatio * hosttypes.OpenAICacheCreation12hMultiplier
		imageRatio, _ = ratio_setting.GetImageRatio(billingModelName)
		audioRatio = ratio_setting.GetAudioRatio(billingModelName)
		audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(billingModelName)
		videoRatio, _ = ratio_setting.GetVideoRatio(billingModelName)
		ratio := modelRatio * groupRatioInfo.GroupRatio
		quota, err := common.QuotaFromFloatStrict(preConsumedTokens * ratio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedQuota = quota
		if _, image := info.Request.(*dto.ImageRequest); image {
			info.ImageQuotaBeforeGroup = preConsumedTokens * modelRatio
		}
	} else {
		completionRatio = ratio_setting.GetCompletionRatio(info.OriginModelName)
		imageCompletionRatio = ratio_setting.GetImageCompletionRatio(info.OriginModelName)
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
		if _, image := info.Request.(*dto.ImageRequest); image {
			info.ImageQuotaBeforeGroup = modelPrice * common.QuotaPerUnit
		}
	}

	// check if free model pre-consume is disabled
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		// if model price or ratio is 0, do not pre-consume quota
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		} else if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else {
			if modelRatio == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		}
	}

	priceData := hosttypes.PriceData{
		FreeModel:             freeModel,
		ModelPrice:            modelPrice,
		ModelRatio:            modelRatio,
		CompletionRatio:       completionRatio,
		ImageCompletionRatio:  imageCompletionRatio,
		GroupRatioInfo:        groupRatioInfo,
		UsePrice:              usePrice,
		CacheRatio:            cacheRatio,
		ImageRatio:            imageRatio,
		AudioRatio:            audioRatio,
		AudioCompletionRatio:  audioCompletionRatio,
		VideoRatio:            videoRatio,
		CacheCreationRatio:    cacheCreationRatio,
		CacheCreation5mRatio:  cacheCreationRatio5m,
		CacheCreation1hRatio:  cacheCreationRatio1h,
		CacheCreation12hRatio: cacheCreationRatio12h,
		QuotaToPreConsume:     preConsumedQuota,
	}
	if usePrice {
		for name, ratio := range meta.BillingRatios {
			priceData.AddOtherRatio(name, ratio)
		}
	}
	if request, image := info.Request.(*dto.ImageRequest); image {
		channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
		count, err := request.ImageCount(channelType == constant.ChannelTypeAli)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if usePrice || channelType == constant.ChannelTypeAli {
			priceData.AddOtherRatio("n", float64(count))
		}
		if channelType == constant.ChannelTypeAli && request.BillingParameters != nil && request.BillingParameters.PromptExtend != nil && *request.BillingParameters.PromptExtend {
			// Resolve only routing identity; do not initialize ChannelMeta on the
			// real request, which also distinguishes the first channel attempt.
			mapped := &relaycommon.RelayInfo{OriginModelName: info.OriginModelName, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: info.OriginModelName}}
			if err := ModelMappedHelper(c, mapped, nil); err != nil {
				return hosttypes.PriceData{}, err
			}
			if strings.Contains(mapped.UpstreamModelName, "z-image") {
				priceData.AddOtherRatio("prompt_extend", common.ZImagePromptExtendMultiplier)
			}
		}
		if !usePrice {
			quota, err := common.QuotaFromFloatStrict(priceData.ApplyOtherRatiosToFloat(info.ImageQuotaBeforeGroup * groupRatioInfo.GroupRatio))
			if err != nil {
				return hosttypes.PriceData{}, err
			}
			priceData.QuotaToPreConsume = quota
		}
	}
	if usePrice {
		quotaToPreConsume := priceData.ApplyOtherRatiosToFloat(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		quota, err := common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
	}

	if common.DebugEnabled {
		logger.LogDebug(c, "model_price_helper result: %s", priceData.ToSetting())
	}
	info.PriceData = priceData
	return priceData, nil
}

// ModelPriceHelperPerCall 按次/按量计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)

	modelPrice, success := ratio_setting.GetModelPrice(info.OriginModelName, false)
	usePrice := success
	var modelRatio float64

	// 如果没有配置价格，检查其他定价来源（视频按秒、视频倍率、模型倍率）
	if !success {
		defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
		if ok {
			modelPrice = defaultPrice
			usePrice = true
		} else if videoPrice, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(info.OriginModelName); hasVideoPrice && videoPrice > 0 {
			// 视频按秒计费模型（如 kling-v3, sora-2, veo 等）：提交阶段使用每秒单价作为预扣估算，
			// 实际按秒结算在轮询成功后由 handleVideoPerSecondBilling 完成。
			modelPrice = videoPrice
			usePrice = true
		} else if _, hasVideoCompletionRatio := ratio_setting.GetVideoCompletionRatioPricing(info.OriginModelName, false); hasVideoCompletionRatio {
			// VideoCompletionRatio 体系（按量计费视频模型，noAudio/audio 维度）：视为已配置，使用默认预扣价格
			modelPrice = float64(common.PreConsumedQuota) / common.QuotaPerUnit
			usePrice = true
		} else if _, hasVideoCompletionRatio := ratio_setting.GetVideoCompletionRatioVideoPricing(info.OriginModelName, false); hasVideoCompletionRatio {
			// VideoCompletionRatio 体系（按量计费视频模型，noVideo/video 维度，如 seedance）：视为已配置，使用默认预扣价格
			modelPrice = float64(common.PreConsumedQuota) / common.QuotaPerUnit
			usePrice = true
		} else {
			var ratioSuccess bool
			var matchName string
			modelRatio, ratioSuccess, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !ratioSuccess && !acceptUnsetRatio {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(c, matchName, info.UserId)
			}
		}
	}

	var quota int
	freeModel := false

	if usePrice {
		var err error
		quota, err = common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelPrice == 0 {
				quota = 0
				freeModel = true
			}
		}
	} else {
		// 按量计费：以模型倍率的一半作为预扣额度
		var err error
		quota, err = common.QuotaFromFloatStrict(modelRatio / 2 * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		modelPrice = -1
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelRatio == 0 {
				quota = 0
				freeModel = true
			}
		}
	}

	cacheRatioPerCall, _ := ratio_setting.GetCacheRatio(info.OriginModelName)
	priceData := hosttypes.PriceData{
		FreeModel:            freeModel,
		ModelPrice:           modelPrice,
		ModelRatio:           modelRatio,
		CompletionRatio:      ratio_setting.GetCompletionRatio(info.OriginModelName),
		ImageCompletionRatio: ratio_setting.GetImageCompletionRatio(info.OriginModelName),
		CacheRatio:           cacheRatioPerCall,
		UsePrice:             usePrice,
		Quota:                quota,
		GroupRatioInfo:       groupRatioInfo,
	}
	return priceData, nil
}

func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	// 视频按秒计费模型
	if videoPrice, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(modelName); hasVideoPrice && videoPrice > 0 {
		return true
	}
	// 视频按量计费模型（noAudio/audio 维度）
	if _, hasVideoCompletionRatio := ratio_setting.GetVideoCompletionRatioPricing(modelName, false); hasVideoCompletionRatio {
		return true
	}
	// 视频按量计费模型（noVideo/video 维度，如 seedance）
	if _, hasVideoCompletionRatio := ratio_setting.GetVideoCompletionRatioVideoPricing(modelName, false); hasVideoCompletionRatio {
		return true
	}
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expr, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expr) != ""
}

// HasPriceOrRatioEntry reports whether name has a configured price, ratio, or
// tiered billing-mode entry after a single wildcard normalization. Self-use
// fallback does not count as a configured ratio.
func HasPriceOrRatioEntry(name string) bool {
	formatted := ratio_setting.FormatMatchingModelName(name)
	if _, ok := ratio_setting.GetModelPrice(formatted, false); ok {
		return true
	}
	if ratio_setting.HasConfiguredModelRatio(formatted) {
		return true
	}
	return billing_setting.GetBillingMode(formatted) == billing_setting.BillingModeTieredExpr
}

func resolveBillingModelName(origin string) string {
	var candidates []string
	if !reasoning.ParseModelModifiers(origin).HasModifiers() {
		candidates = append(candidates, origin)
	}
	candidates = append(candidates, hostreasoning.CanonicalBillingModelNames(origin)...)
	base := hostreasoning.BaseModelName(origin)
	// DeepSeek thinking tails (-none/-low/-max) bill at the base model's price,
	// with or without @ modifiers after them.
	if deepSeekBase, _, _, ok := reasoning.ParseDeepSeekV4ThinkingSuffix(base); ok && !model_setting.ShouldPreserveThinkingSuffix(base) {
		candidates = append(candidates, deepSeekBase)
	}
	candidates = append(candidates, base)

	seen := make(map[string]struct{}, len(candidates))
	matched := ""
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if HasPriceOrRatioEntry(name) {
			matched = name
			break
		}
	}
	if matched == "" {
		matched = base
	}
	return matched
}

func modelPriceHelperTiered(c *gin.Context, info *relaycommon.RelayInfo, billingModelName string, promptTokens int, groupRatioInfo hosttypes.GroupRatioInfo) (hosttypes.PriceData, error) {
	exprStr, ok := billing_setting.GetBillingExpr(billingModelName)
	if !ok {
		return hosttypes.PriceData{}, fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", billingModelName)
	}
	exprHash := billingexpr.ExprHashString(exprStr)
	usedVars := billingexpr.UsedVarsByHash(exprStr, exprHash)
	if usedVars["u"] {
		return hosttypes.PriceData{}, errors.New(i18n.T(c, i18n.MsgRelayTaskUsagePriceUnsupported, map[string]any{"Model": billingModelName}))
	}
	fixedPricing := billingexpr.UsesFixedPricingByHash(exprStr, exprHash)
	value, _ := c.Get(jsplugin.ContextKeyRelayEndpoint)
	if endpoint, ok := value.(jsplugin.PinnedEndpoint); ok && !fixedPricing {
		for _, candidate := range endpoint.Candidates {
			pluginKey := candidate.Plugin.Meta.Key
			if _, overridden := billing_setting.GetPluginBillingExpr(pluginKey, endpoint.Model); overridden {
				continue
			}
			if _, overridden := billing_setting.GetPluginBillingExpr(pluginKey, endpoint.MappedModel); endpoint.MappedModel != "" && overridden {
				continue
			}
			taskBillingModel := endpoint.Model
			if billing_setting.GetBillingMode(taskBillingModel) != billing_setting.BillingModeTieredExpr {
				taskBillingModel = endpoint.MappedModel
			}
			// Task expressions return USD; ordinary expressions return USD *
			// 1M. Even a constant without u() cannot safely serve both contracts.
			if taskBillingModel == billingModelName {
				return hosttypes.PriceData{}, errors.New(i18n.T(c, i18n.MsgRelaySharedTaskPriceNeedsOverride, map[string]any{"Model": billingModelName, "Plugin": pluginKey}))
			}
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAIRealtime && fixedPricing {
		return hosttypes.PriceData{}, fmt.Errorf("fixed pricing is not supported for Realtime requests")
	}

	preConsumeMultiplier, err := operation_setting.InputPreConsumeMultiplier()
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info, usedVars["param"])
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if usedVars["image_count"] {
		requestInput, err = ResolveImageBillingRequestInput(c, info, requestInput)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
	}

	rawCost, trace, err := billingexpr.RunExprByHashWithRequest(exprStr, exprHash, billingexpr.TokenParams{
		P:   float64(promptTokens),
		C:   0,
		Len: float64(promptTokens),
	}, requestInput)
	if err != nil {
		return hosttypes.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", billingModelName, err)
	}

	// Expression coefficients are $/1M tokens prices; convert to quota the same way per-call billing does.
	quotaBeforeGroup := rawCost / 1_000_000 * common.QuotaPerUnit
	// Scale the reservation, preserving the expression's context-length tier
	// and leaving actual settlement and fixed request prices unchanged.
	if trace.BillingUnit != billingexpr.BillingUnitRequest {
		quotaBeforeGroup *= preConsumeMultiplier
	}
	preConsumedQuota, err := billingexpr.QuotaRoundStrict(quotaBeforeGroup * groupRatioInfo.GroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		}
	}

	snapshot := &billingexpr.BillingSnapshot{
		EstimatedImageCount:       trace.ImageCount,
		BillingMode:               billing_setting.BillingModeTieredExpr,
		ModelName:                 billingModelName,
		ExprString:                exprStr,
		ExprHash:                  exprHash,
		GroupRatio:                groupRatioInfo.GroupRatio,
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: 0,
		PreConsumeMultiplier:      preConsumeMultiplier,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup,
		EstimatedQuotaAfterGroup:  preConsumedQuota,
		EstimatedTier:             trace.MatchedTier,
		EstimatedBillingUnit:      trace.BillingUnit,
		EstimatedFixedPrice:       trace.FixedPrice,
		QuotaPerUnit:              common.QuotaPerUnit,
		ExprVersion:               billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := hosttypes.PriceData{
		FreeModel:         freeModel,
		GroupRatioInfo:    groupRatioInfo,
		QuotaToPreConsume: preConsumedQuota,
	}

	logger.LogDebug(c, "model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeGroup=%.2f groupRatio=%.2f tier=%s", billingModelName, preConsumedQuota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier)

	info.PriceData = priceData
	return priceData, nil
}
