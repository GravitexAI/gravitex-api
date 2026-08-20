package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if otherRatios := info.PriceData.OtherRatios(); len(otherRatios) > 0 {
			var contents []string
			for key, ra := range otherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}

	// 将系统生成的 request_id 存入 other，request_id 字段改存上游返回的 ID
	systemRequestId := c.GetString(common.RequestIdKey)
	if systemRequestId != "" {
		other["system_request_id"] = systemRequestId
	}

	attachQuotaSaturation(c, info, other)
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		RequestId: info.UpstreamResponseId,
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// TaskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 需要通过 resolveTokenKey 运行时获取 key（不从 PrivateData 中读取）。
func TaskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) {
	if delta == 0 {
		return
	}
	// 优先使用 PrivateData.TokenId，若为 0（JSON 反序列化失败等）则 fallback 到 task.TokenId（INT 列）
	tokenId := task.PrivateData.TokenId
	if tokenId <= 0 {
		tokenId = task.TokenId
	}
	if tokenId <= 0 {
		return
	}
	tokenKey := resolveTokenKey(ctx, tokenId, task.TaskID)
	if tokenKey == "" {
		return
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(tokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(tokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if task == nil {
		return other
	}
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if priceData := taskBillingContextPriceData(bc); priceData != nil {
			for k, v := range priceData.OtherRatios() {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	appendTaskVideoBillingOther(task, other)
	// Async settlement has no request context, so restore the effective channel
	// cost from the immutable task-creation snapshot. The snapshot already
	// reflects settings.model_cost_discount[original model] > cost_discount.
	// Keep task.Data authoritative for compatibility, then use the independent
	// billing_context copy because an upstream task result may replace task.Data.
	// Missing, malformed, or out-of-range values keep the legacy log unchanged.
	var taskData map[string]interface{}
	costDiscount := 0.0
	if len(task.Data) > 0 && common.Unmarshal(task.Data, &taskData) == nil {
		if discount, ok := taskData["billing_cost_discount"].(float64); ok && discount > 0 && discount <= 1 {
			costDiscount = discount
		}
	}
	if costDiscount <= 0 {
		if bc := task.PrivateData.BillingContext; bc != nil && bc.CostDiscount != nil &&
			*bc.CostDiscount > 0 && *bc.CostDiscount <= 1 {
			costDiscount = *bc.CostDiscount
		}
	}
	if costDiscount <= 0 {
		// Historical tasks have no immutable model-level snapshot. Preserve the
		// legacy fallback by reading only the channel's generic cost_discount;
		// do not apply today's model_cost_discount to old requests.
		if channel, err := model.CacheGetChannel(task.ChannelId); err == nil && channel != nil &&
			channel.CostDiscount != nil && *channel.CostDiscount > 0 {
			costDiscount = *channel.CostDiscount
		}
	}
	if costDiscount > 0 {
		adminInfo, _ := other["admin_info"].(map[string]interface{})
		if adminInfo == nil {
			adminInfo = make(map[string]interface{})
			other["admin_info"] = adminInfo
		}
		adminInfo["cost_discount"] = costDiscount
	}
	if strings.Contains(strings.ToLower(taskModelName(task)), "gemini-omni") {
		inputTokens := taskDataNumber(other, "input_tokens")
		textOutputTokens := taskDataNumber(other, "text_output_tokens")
		videoOutputTokens := taskDataNumber(other, "video_output_tokens")
		if inputTokens > 0 || textOutputTokens > 0 || videoOutputTokens > 0 {
			rawCost := inputTokens*1.5 + textOutputTokens*9.0 + videoOutputTokens*17.5
			other["official_quota"] = common.QuotaFromFloat(rawCost / 1_000_000.0 * common.QuotaPerUnit)
		}
	}
	return other
}

// enrichTaskBillingDataForTokenSettlement preserves the billing discriminator
// and token count for the generic token-settlement path. Task-specific billing
// writes these fields directly to the consume log, but the generic path builds
// its log from task.Data; without them the admin detail can only show a total.
func enrichTaskBillingDataForTokenSettlement(task *model.Task, taskResult *relaycommon.TaskInfo) {
	if task == nil || taskResult == nil || taskResult.TotalTokens <= 0 {
		return
	}
	modelName := strings.ToLower(taskModelName(task))
	if !strings.Contains(modelName, "gemini-omni") {
		return
	}

	data := make(map[string]interface{})
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &data); err != nil || data == nil {
			data = make(map[string]interface{})
		}
	}
	if _, exists := data["billing_type"]; !exists {
		data["billing_type"] = "video_token_ratio"
	}
	if _, exists := data["tokens"]; !exists {
		data["tokens"] = taskResult.TotalTokens
	}
	if taskResult.InputTokens > 0 || taskResult.TextOutputTokens > 0 || taskResult.VideoOutputTokens > 0 {
		inputTextTokens := taskResult.TextInputTokens
		if inputTextTokens <= 0 {
			inputTextTokens = taskResult.InputTokens - taskResult.ImageInputTokens - taskResult.VideoInputTokens
			if inputTextTokens < 0 {
				inputTextTokens = 0
			}
			if inputTextTokens == 0 && taskResult.ImageInputTokens == 0 && taskResult.VideoInputTokens == 0 {
				inputTextTokens = taskResult.InputTokens
			}
		}
		data["input_tokens"] = taskResult.InputTokens
		data["input_text_tokens"] = inputTextTokens
		data["input_image_tokens"] = taskResult.ImageInputTokens
		data["input_video_tokens"] = taskResult.VideoInputTokens
		data["text_output_tokens"] = taskResult.TextOutputTokens
		data["video_output_tokens"] = taskResult.VideoOutputTokens
		data["input_price_per_million_tokens"] = 1.5
		data["text_output_price_per_million_tokens"] = 9.0
		data["video_output_price_per_million_tokens"] = 17.5
	}
	if merged, err := common.Marshal(data); err == nil {
		task.Data = merged
	}
}

// calculateGeminiOmniQuota applies the same modality prices used by the
// dedicated Omni billing path when the generic task settlement path is used.
func calculateGeminiOmniQuota(task *model.Task, taskResult *relaycommon.TaskInfo) (int, bool, *common.QuotaClamp) {
	if task == nil || taskResult == nil || !strings.Contains(strings.ToLower(taskModelName(task)), "gemini-omni") {
		return 0, false, nil
	}
	if taskResult.InputTokens <= 0 && taskResult.TextOutputTokens <= 0 && taskResult.VideoOutputTokens <= 0 {
		return 0, false, nil
	}

	groupRatio := 0.0
	if task.PrivateData.BillingContext != nil {
		groupRatio = task.PrivateData.BillingContext.GroupRatio
	}
	if groupRatio <= 0 {
		group := task.Group
		if group == "" {
			if user, err := model.GetUserById(task.UserId, false); err == nil && user != nil {
				group = user.Group
			}
		}
		if group != "" {
			groupRatio = ratio_setting.GetGroupRatio(group)
			if specialRatio, ok := ratio_setting.GetGroupGroupRatio(group, group); ok {
				groupRatio = specialRatio
			}
		}
	}
	if groupRatio <= 0 {
		groupRatio = 1
	}

	inputCost := float64(taskResult.InputTokens) * 1.5
	textOutputCost := float64(taskResult.TextOutputTokens) * 9.0
	videoOutputCost := float64(taskResult.VideoOutputTokens) * 17.5
	otherMultiplier := 1.0
	if priceData := taskBillingContextPriceData(task.PrivateData.BillingContext); priceData != nil {
		otherMultiplier = priceData.OtherRatioMultiplier()
	}
	quota, clamp := common.QuotaFromFloatChecked(
		(inputCost + textOutputCost + videoOutputCost) / 1_000_000.0 * common.QuotaPerUnit * groupRatio * otherMultiplier,
	)
	return quota, true, clamp
}

func appendTaskVideoBillingOther(task *model.Task, other map[string]interface{}) {
	if task == nil || other == nil || len(task.Data) == 0 {
		return
	}

	var taskData map[string]interface{}
	if err := common.Unmarshal(task.Data, &taskData); err != nil || taskData == nil {
		return
	}

	for _, key := range []string{
		"requested_seconds",
		"billing_requested_seconds",
		"tokens",
		"input_tokens",
		"input_text_tokens",
		"input_image_tokens",
		"input_video_tokens",
		"text_output_tokens",
		"video_output_tokens",
		"input_price_per_million_tokens",
		"text_output_price_per_million_tokens",
		"video_output_price_per_million_tokens",
		"official_quota",
		"admin_info",
		"client_request_headers",
		"upstream_responses",
		"usage_conversion",
		"reasoning_effort",
		"request_conversion",
		"generate_audio",
		"has_video_input",
		"video_resolution",
		"video_price_per_second",
		"official_video_price_per_second",
		"video_price_per_million_tokens",
		"video_ratio",
		"video_completion_ratio_val",
		"effective_video_ratio",
		"ratio_mode",
	} {
		if value, ok := taskData[key]; ok {
			other[key] = value
		}
	}

	billingType, _ := taskData["billing_type"].(string)
	if billingType == "" {
		if taskDataNumber(taskData, "requested_seconds") > 0 || taskDataNumber(taskData, "billing_requested_seconds") > 0 {
			billingType = "per_second"
		} else if taskDataNumber(taskData, "tokens") > 0 ||
			taskDataNumber(taskData, "video_price_per_million_tokens") > 0 ||
			taskDataNumber(taskData, "effective_video_ratio") > 0 {
			billingType = "video_token_ratio"
		}
	}
	if billingType != "" {
		other["billing_type"] = billingType
	}

	if billingType == "per_second" {
		if _, ok := other["requested_seconds"]; !ok {
			if seconds, ok := other["billing_requested_seconds"]; ok {
				other["requested_seconds"] = seconds
			}
		}
		if _, ok := other["video_price_per_second"]; !ok {
			if price, ok := other["model_price"]; ok {
				other["video_price_per_second"] = price
			}
		}
		if _, ok := other["official_video_price_per_second"]; !ok {
			if price, ok := other["video_price_per_second"]; ok {
				other["official_video_price_per_second"] = price
			}
		}
	}
}

func taskDataNumber(taskData map[string]interface{}, key string) float64 {
	if taskData == nil {
		return 0
	}
	switch value := taskData[key].(type) {
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case float64:
		return value
	case float32:
		return float64(value)
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(value, "%f", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func taskBillingContextPriceData(bc *model.TaskBillingContext) *types.PriceData {
	if bc == nil || len(bc.OtherRatios) == 0 {
		return nil
	}
	priceData := &types.PriceData{}
	if !priceData.ReplaceOtherRatios(bc.OtherRatios) {
		return nil
	}
	return priceData
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
// 返回资金来源是否已成功退还；失败时保留 quota，供显式重试或人工对账。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	quota := task.Quota
	if quota == 0 {
		return true
	}

	// 1. 退还资金来源（钱包或订阅）
	if err := taskAdjustFunding(task, -quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
		return false
	}

	// 2. 退还令牌额度
	TaskAdjustTokenQuota(ctx, task, -quota)

	// 3. 记录日志
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})

	// 4. 资金退款完成后再清除持久化标记。
	// 回写失败必须显式告警，避免漏掉潜在的重复退款风险。
	task.Quota = 0
	if err := task.UpdateQuota(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("退款成功但清除 task quota 失败 task %s: %s", task.TaskID, err.Error()))
	}
	return true
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
// clamps 可选：若计算 actualQuota 时发生额度饱和，将其记入日志 admin_info（仅管理员可见）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) {
	if actualQuota <= 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	// 调整资金来源
	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 调整令牌额度
	TaskAdjustTokenQuota(ctx, task, quotaDelta)

	task.Quota = actualQuota
	if err := task.UpdateQuota(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算回写 quota 失败 task %s: %s", task.TaskID, err.Error()))
	}

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
		model.UpdateUserUsedQuotaAndRequestCount(task.UserId, quotaDelta)
		model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:           task.UserId,
		LogType:          logType,
		Content:          reason,
		ChannelId:        task.ChannelId,
		ModelName:        taskModelName(task),
		Quota:            logQuota,
		TokenId:          task.PrivateData.TokenId,
		Group:            task.Group,
		Other:            other,
		NodeName:         task.PrivateData.NodeName,
		PromptTokens:     int(taskDataNumber(other, "input_text_tokens")),
		CompletionTokens: int(taskDataNumber(other, "text_output_tokens") + taskDataNumber(other, "video_output_tokens")),
		UseTime:          geminiOmniTaskUseTimeSeconds(task),
	})
}

func geminiOmniTaskUseTimeSeconds(task *model.Task) int {
	if task == nil || !strings.EqualFold(taskModelName(task), "gemini-omni-flash-preview") {
		return 0
	}
	start, finish := task.StartTime, task.FinishTime
	if task.ID > 0 && (start == 0 || finish == 0) {
		var persisted struct {
			StartTime  int64
			FinishTime int64
		}
		if err := model.DB.Model(&model.Task{}).
			Select("start_time, finish_time").
			Where("id = ?", task.ID).First(&persisted).Error; err == nil {
			if start == 0 {
				start = persisted.StartTime
			}
			if finish == 0 {
				finish = persisted.FinishTime
			}
		}
	}
	if finish <= start || start <= 0 {
		return 0
	}
	return int(finish - start)
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	if totalTokens <= 0 {
		return
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if priceData := taskBillingContextPriceData(task.PrivateData.BillingContext); priceData != nil {
		otherMultiplier = priceData.OtherRatioMultiplier()
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier（饱和转换，防止溢出成负数）
	actualQuota, clamp := common.QuotaFromFloatChecked(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	RecalculateTaskQuota(ctx, task, actualQuota, reason, clamp)
}
