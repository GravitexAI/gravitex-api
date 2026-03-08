package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func UpdateVideoTaskAll(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if err := updateVideoTaskAll(ctx, platform, channelId, taskIds, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateVideoTaskAll(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		errUpdate := model.TaskBulkUpdate(taskIds, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := relay.GetTaskAdaptor(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelType:          cacheGetChannel.Type,
		ChannelBaseUrl:       cacheGetChannel.GetBaseURL(),
		ApiVersion:           cacheGetChannel.Other,
		ChannelOtherSettings: cacheGetChannel.GetOtherSettings(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	for _, taskId := range taskIds {
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor channel.TaskAdaptor, channel *model.Channel, taskId string, taskM map[string]*model.Task) error {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}
	proxy := channel.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := channel.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] fetching task=%s baseURL=%s platform=%s", taskId, baseURL, task.Platform))
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": taskId,
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	//if resp.StatusCode != http.StatusOK {
	//return fmt.Errorf("get Video Task status code: %d", resp.StatusCode)
	//}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] task=%s channel=%d status=%d body=%s",
		taskId, channel.Id, resp.StatusCode, truncateForLog(string(responseBody))))

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, fmt.Sprintf("UpdateVideoSingleTask parsed as new api response format: %+v", responseItems))
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.FailReason
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	} else {
		task.Data = redactVideoResponseBody(responseBody)
	}

	logger.LogDebug(ctx, fmt.Sprintf("UpdateVideoSingleTask taskResult: %+v", taskResult))

	now := time.Now().Unix()
	if taskResult.Status == "" {
		//return fmt.Errorf("task %s status is empty", taskId)
		taskResult = relaycommon.FailTaskInfo("upstream returned empty status")
	}

	// 记录原本的状态，防止重复退款
	shouldRefund := false
	quota := task.Quota
	preStatus := task.Status

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = "10%"
	case model.TaskStatusQueued:
		task.Progress = "20%"
	case model.TaskStatusInProgress:
		task.Progress = "30%"
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		// Attempt to upload the generated video to OSS and store the permanent URL.
		// When OSS is not configured (OSS_BASE64_ENDPOINT unset), fall back to storing
		// the video directly in fail_reason (base64 data URI or CDN URL).
		if ossURL := uploadVideoToOSS(ctx, channel, task, taskResult); ossURL != "" {
			task.FailReason = ossURL
		} else {
			// OSS disabled or upload failed — store whatever URL/data we have.
			if taskResult.RemoteUrl != "" {
				task.FailReason = taskResult.RemoteUrl
			} else if taskResult.Url != "" {
				task.FailReason = taskResult.Url
			}
		}

		// 计费路由：按秒计费模型（Sora-2 等）使用独立计费函数，其他沿用预扣费差额结算
		taskModelName := task.Properties.OriginModelName
		if taskModelName == "" {
			taskModelName = task.Properties.UpstreamModelName
		}

		if isSora2VideoModel(taskModelName) {
			// 按秒计费：轮询成功后根据 task.Data 中保存的信息计费并写消费日志
			if err := handleSora2TaskBilling(ctx, task); err != nil {
				logger.LogError(ctx, fmt.Sprintf("[Sora2Billing] Failed for task %s: %v", task.TaskID, err))
			}
		} else if taskResult.TotalTokens > 0 {
			// 按 token 计费模型（如 Doubao）：根据实际 token 数结算差额
			var taskData map[string]interface{}
			if err := json.Unmarshal(task.Data, &taskData); err == nil {
				if modelName, ok := taskData["model"].(string); ok && modelName != "" {
					modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
					if hasRatioSetting && modelRatio > 0 {
						group := task.Group
						if group == "" {
							if user, err := model.GetUserById(task.UserId, false); err == nil {
								group = user.Group
							}
						}
						if group != "" {
							groupRatio := ratio_setting.GetGroupRatio(group)
							userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)
							finalGroupRatio := groupRatio
							if hasUserGroupRatio {
								finalGroupRatio = userGroupRatio
							}
							actualQuota := int(float64(taskResult.TotalTokens) * modelRatio * finalGroupRatio)
							preConsumedQuota := task.Quota
							quotaDelta := actualQuota - preConsumedQuota
							if quotaDelta > 0 {
								logger.LogInfo(ctx, fmt.Sprintf("视频任务 %s 补扣费：%s（实际：%s，预扣：%s，tokens：%d）",
									task.TaskID, logger.LogQuota(quotaDelta), logger.LogQuota(actualQuota),
									logger.LogQuota(preConsumedQuota), taskResult.TotalTokens))
								if err := model.DecreaseUserQuota(task.UserId, quotaDelta); err != nil {
									logger.LogError(ctx, fmt.Sprintf("补扣费失败: %s", err.Error()))
								} else {
									model.UpdateUserUsedQuotaAndRequestCount(task.UserId, quotaDelta)
									model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)
									task.Quota = actualQuota
									logContent := fmt.Sprintf("视频任务成功补扣费，模型倍率 %.2f，分组倍率 %.2f，tokens %d，预扣费 %s，实际扣费 %s，补扣费 %s",
										modelRatio, finalGroupRatio, taskResult.TotalTokens,
										logger.LogQuota(preConsumedQuota), logger.LogQuota(actualQuota), logger.LogQuota(quotaDelta))
									model.RecordLog(task.UserId, model.LogTypeSystem, logContent)
								}
							} else if quotaDelta < 0 {
								refundQuota := -quotaDelta
								logger.LogInfo(ctx, fmt.Sprintf("视频任务 %s 退还多扣：%s（实际：%s，预扣：%s，tokens：%d）",
									task.TaskID, logger.LogQuota(refundQuota), logger.LogQuota(actualQuota),
									logger.LogQuota(preConsumedQuota), taskResult.TotalTokens))
								if err := model.IncreaseUserQuota(task.UserId, refundQuota, false); err != nil {
									logger.LogError(ctx, fmt.Sprintf("退还预扣费失败: %s", err.Error()))
								} else {
									task.Quota = actualQuota
									logContent := fmt.Sprintf("视频任务成功退还多扣费用，模型倍率 %.2f，分组倍率 %.2f，tokens %d，预扣费 %s，实际扣费 %s，退还 %s",
										modelRatio, finalGroupRatio, taskResult.TotalTokens,
										logger.LogQuota(preConsumedQuota), logger.LogQuota(actualQuota), logger.LogQuota(refundQuota))
									model.RecordLog(task.UserId, model.LogTypeSystem, logContent)
								}
							} else {
								logger.LogInfo(ctx, fmt.Sprintf("视频任务 %s 预扣费准确（%s，tokens：%d）",
									task.TaskID, logger.LogQuota(actualQuota), taskResult.TotalTokens))
							}
						}
					}
				}
			}
		}
	case model.TaskStatusFailure:
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		taskResult.Progress = "100%"
		if quota != 0 {
			if preStatus != model.TaskStatusFailure {
				shouldRefund = true
			} else {
				logger.LogWarn(ctx, fmt.Sprintf("Task %s already in failure status, skip refund", task.TaskID))
			}
		}
		// 记录失败日志
		modelName := task.Properties.OriginModelName
		if modelName == "" {
			modelName = task.Properties.UpstreamModelName
		}
		failLog := &model.Log{
			UserId:    task.UserId,
			CreatedAt: common.GetTimestamp(),
			Type:      model.LogTypeError,
			Content:   fmt.Sprintf("视频任务失败，模型 %s，原因：%s", modelName, task.FailReason),
			ChannelId: task.ChannelId,
			ModelName: modelName,
			Group:     task.Group,
		}
		if err := model.LOG_DB.Create(failLog).Error; err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to insert error log for task %s: %v", task.TaskID, err))
		}
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, taskId)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}
	if err := task.Update(); err != nil {
		common.SysLog("UpdateVideoTask task error: " + err.Error())
		shouldRefund = false
	}

	if shouldRefund {
		// 任务失败且之前状态不是失败才退还额度，防止重复退还
		if err := model.IncreaseUserQuota(task.UserId, quota, false); err != nil {
			logger.LogWarn(ctx, "Failed to increase user quota: "+err.Error())
		}
		logContent := fmt.Sprintf("Video async task failed %s, refund %s", task.TaskID, logger.LogQuota(quota))
		model.RecordLog(task.UserId, model.LogTypeSystem, logContent)
	}

	return nil
}

// uploadVideoToOSS uploads the generated video to OSS via the Java backend and returns the permanent URL.
// Returns an empty string if OSS is disabled or if the upload fails.
//
// The Go backend is always responsible for downloading (it can inject channel auth headers),
// then uploads to OSS via the Java backend's base64 endpoint (OSS_BASE64_ENDPOINT).
//
// Channel-specific logic:
//   - AzureVideo / Azure / Sora: taskResult.RemoteUrl is a pre-signed Azure CDN URL (no extra auth needed).
//   - Gemini Veo:                taskResult.RemoteUrl is a GCS URL requiring x-goog-api-key.
//   - Vertex AI Veo:             taskResult.Url is an inline base64 data URI — upload directly.
func uploadVideoToOSS(ctx context.Context, ch *model.Channel, task *model.Task, taskResult *relaycommon.TaskInfo) string {
	if !service.IsVideoOSSEnabled() {
		return ""
	}

	switch ch.Type {
	case constant.ChannelTypeAzureVideo, constant.ChannelTypeAzure, constant.ChannelTypeSora:
		// When /content returns 302: RemoteUrl is a pre-signed CDN URL (download without auth).
		// When /content returns 200: Url is a base64 data URI (bytes already in memory).
		if taskResult.RemoteUrl != "" {
			ossURL, err := service.UploadVideoFromURL(ctx, taskResult.RemoteUrl, nil, "mp4")
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("OSS upload failed for task %s (azure cdn url): %s", task.TaskID, err.Error()))
				return ""
			}
			logger.LogInfo(ctx, fmt.Sprintf("Task %s video uploaded to OSS: %s", task.TaskID, ossURL))
			return ossURL
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			ossURL, err := service.UploadBase64ToOSS(ctx, taskResult.Url, "", "mp4")
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("OSS upload failed for task %s (azure direct download): %s", task.TaskID, err.Error()))
				return ""
			}
			logger.LogInfo(ctx, fmt.Sprintf("Task %s video uploaded to OSS: %s", task.TaskID, ossURL))
			return ossURL
		}
		return ""

	case constant.ChannelTypeGemini:
		remoteURL := taskResult.RemoteUrl
		if remoteURL == "" {
			return ""
		}
		// Gemini GCS URL requires api-key header; Go backend injects it during download
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			apiKey = ch.Key
		}
		headers := map[string]string{}
		if apiKey != "" {
			headers["x-goog-api-key"] = apiKey
		}
		ossURL, err := service.UploadVideoFromURL(ctx, remoteURL, headers, "mp4")
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("OSS upload failed for task %s (gemini gcs url): %s", task.TaskID, err.Error()))
			return ""
		}
		logger.LogInfo(ctx, fmt.Sprintf("Task %s video uploaded to OSS: %s", task.TaskID, ossURL))
		return ossURL

	case constant.ChannelTypeVertexAi:
		// Vertex AI Veo: inline base64 data URI
		dataURI := taskResult.Url
		if !strings.HasPrefix(dataURI, "data:") {
			return ""
		}
		ossURL, err := service.UploadBase64ToOSS(ctx, dataURI, "", "mp4")
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("OSS upload failed for task %s (vertex base64): %s", task.TaskID, err.Error()))
			return ""
		}
		logger.LogInfo(ctx, fmt.Sprintf("Task %s video uploaded to OSS: %s", task.TaskID, ossURL))
		return ossURL
	}

	return ""
}

// sanitizeFileName replaces characters that are unsafe in file names with underscores.
func sanitizeFileName(s string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return replacer.Replace(s)
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	// Vertex AI: remove embedded base64 video bytes
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	// Azure Video: if we injected a base64 data URI into "url", strip the data to keep DB small.
	// The OSS URL will be stored in task.FailReason — ConvertToOpenAIVideo will prefer that.
	if urlVal, ok := m["url"].(string); ok && strings.HasPrefix(urlVal, "data:") {
		m["url"] = "" // clear large base64 blob; OSS URL in FailReason takes precedence
	}
	b, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// truncateForLog truncates base64 data URIs and long strings to keep logs readable.
func truncateForLog(s string) string {
	const maxLen = 1000
	if idx := strings.Index(s, ";base64,"); idx >= 0 {
		end := idx + len(";base64,") + 64
		if end > len(s) {
			end = len(s)
		}
		s = s[:end] + "...[base64 truncated]"
	}
	if len(s) > maxLen {
		return s[:maxLen] + "...[truncated]"
	}
	return s
}

// isSora2VideoModel 判断是否为按秒计费的视频模型
func isSora2VideoModel(name string) bool {
	_, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(name)
	return hasVideoPrice
}

// handleSora2TaskBilling 处理按秒计费视频模型的轮询成功计费逻辑（Sora-2 等）
// 计费公式: actualQuota = videoPrice × requestedSeconds × QuotaPerUnit × groupRatio × oemUserDiscount
func handleSora2TaskBilling(ctx context.Context, task *model.Task) error {
	modelName := task.Properties.OriginModelName
	if modelName == "" {
		modelName = task.Properties.UpstreamModelName
	}

	// 从 task.Data 读取提交时保存的计费信息
	var taskData map[string]interface{}
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &taskData); err != nil {
			return fmt.Errorf("handleSora2TaskBilling: unmarshal task.Data failed: %w", err)
		}
	}
	if taskData == nil {
		taskData = make(map[string]interface{})
	}

	// 防重复计费
	if processed, ok := taskData["billing_processed"].(bool); ok && processed {
		logger.LogInfo(ctx, fmt.Sprintf("[Sora2Billing] Task %s already billed, skip", task.TaskID))
		return nil
	}

	// 读取关键字段
	requestedSeconds := 0
	if v, ok := taskData["billing_requested_seconds"].(float64); ok {
		requestedSeconds = int(v)
	}
	if requestedSeconds <= 0 {
		requestedSeconds = 4
	}

	oemCode := "gravitex"
	if v, ok := taskData["billing_oem_code"].(string); ok && v != "" {
		oemCode = v
	}
	oemUserDiscount := 1.0
	if v, ok := taskData["billing_oem_user_discount"].(float64); ok && v > 0 {
		oemUserDiscount = v
	}
	tokenName := ""
	if v, ok := taskData["billing_token_name"].(string); ok {
		tokenName = v
	}
	tokenId := 0
	if v, ok := taskData["billing_token_id"].(float64); ok {
		tokenId = int(v)
	}
	billingGroup := task.Group
	if v, ok := taskData["billing_group"].(string); ok && v != "" {
		billingGroup = v
	}

	// 获取 groupRatio（OEM 专属 GroupRatio 或全局 GroupRatio）
	groupRatio := service.GetGroupRatioByOem(oemCode, billingGroup)
	if groupRatio <= 0 {
		groupRatio = ratio_setting.GetGroupRatio(billingGroup)
	}
	if groupRatio <= 0 {
		groupRatio = 1.0
	}

	// 获取官方单秒价格
	officialVideoPrice, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(modelName)
	if !hasVideoPrice || officialVideoPrice <= 0 {
		return fmt.Errorf("handleSora2TaskBilling: video price per second not configured for model: %s", modelName)
	}

	// 计算实际扣费 quota
	// actualQuota = officialVideoPrice × requestedSeconds × QuotaPerUnit × groupRatio × oemUserDiscount
	actualQuota := int(officialVideoPrice * float64(requestedSeconds) * common.QuotaPerUnit * groupRatio * oemUserDiscount)
	if actualQuota < 0 {
		actualQuota = 0
	}

	logger.LogInfo(ctx, fmt.Sprintf("[Sora2Billing] task=%s model=%s seconds=%d officialPrice=%.4f groupRatio=%.4f oemDiscount=%.4f actualQuota=%d",
		task.TaskID, modelName, requestedSeconds, officialVideoPrice, groupRatio, oemUserDiscount, actualQuota))

	// 执行扣费
	if actualQuota > 0 {
		if err := model.DecreaseUserQuota(task.UserId, actualQuota); err != nil {
			return fmt.Errorf("handleSora2TaskBilling: DecreaseUserQuota failed: %w", err)
		}
	}

	// 获取 OEM 销售折扣（用于日志价格链）
	oemDiscount := model.GetOemDiscountByCode(oemCode, modelName, "")
	if oemDiscount <= 0 {
		oemDiscount = 1.0
	}
	oemVideoPrice := officialVideoPrice * oemDiscount

	// 获取用户名（用于日志）
	username := ""
	if user, err := model.GetUserById(task.UserId, false); err == nil && user != nil {
		username = user.Username
	}

	useTime := 0
	if task.FinishTime > 0 && task.StartTime > 0 {
		useTime = int(task.FinishTime - task.StartTime)
	}

	logContent := fmt.Sprintf("Sora-2视频任务成功，模型 %s，时长 %d 秒，耗时 %ds，扣费 %s",
		modelName, requestedSeconds, useTime, logger.LogQuota(actualQuota))

	otherMap := map[string]interface{}{
		"billing_type":                    "per_second",
		"requested_seconds":               requestedSeconds,
		"official_video_price_per_second": officialVideoPrice,
		"oem_video_price_per_second":      oemVideoPrice,
		"video_price_per_second":          officialVideoPrice * oemUserDiscount,
		"group_ratio":                     groupRatio,
		"oem_user_discount":               oemUserDiscount,
		"oem_code":                        oemCode,
		"oem_discount":                    oemDiscount,
	}
	otherBytes, _ := common.Marshal(otherMap)

	consumeLog := &model.Log{
		UserId:           task.UserId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             model.LogTypeConsume,
		Content:          logContent,
		ChannelId:        task.ChannelId,
		ModelName:        modelName,
		Quota:            actualQuota,
		CompletionTokens: requestedSeconds, // 用秒数存储
		TokenName:        tokenName,
		TokenId:          tokenId,
		UseTime:          useTime,
		Group:            billingGroup,
		Other:            string(otherBytes),
	}
	if err := model.LOG_DB.Create(consumeLog).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("[Sora2Billing] Failed to insert consume log for task %s: %v", task.TaskID, err))
	}

	// 更新用量统计
	model.UpdateUserUsedQuotaAndRequestCount(task.UserId, actualQuota)
	model.UpdateChannelUsedQuota(task.ChannelId, actualQuota)

	// 更新 task.Quota 为实际扣费额度，标记 billing_processed=true
	task.Quota = actualQuota
	taskData["billing_processed"] = true
	if merged, err := common.Marshal(taskData); err == nil {
		task.Data = merged
	}

	return nil
}
