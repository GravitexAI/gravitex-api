package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel"
	alitask "github.com/QuantumNous/new-api/relay/channel/task/ali"
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
	keySource := "channel_default"

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
		keySource = "private_data"
	} else if channel.ChannelInfo.IsMultiKey {
		// 多 key 渠道（如 Vertex AI 配了多个 service account JSON），
		// channel.Key 是拼接的完整字符串，需根据任务的 project 匹配正确的凭证
		// 优先使用 Properties.ProjectID（提交时写入），兜底从 taskID 提取
		projectID := task.Properties.ProjectID
		if projectID == "" {
			projectID = extractProjectFromTaskID(taskId)
		}
		key = channel.FindKeyByProjectID(projectID)
		keySource = fmt.Sprintf("multi_key(project=%s)", projectID)
	}
	{
		multiKeyIdx := -1
		if task.Properties.MultiKeyIndex != nil {
			multiKeyIdx = *task.Properties.MultiKeyIndex
		}
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] key_selected: task=%s channel=%d keySource=%s projectID=%s multiKeyIndex=%d hasPrivateKey=%v",
			taskId, channel.Id, keySource, task.Properties.ProjectID, multiKeyIdx, privateData.Key != ""))
	}
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

	logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] task=%s channel=%d status=%d 上游返回数据 body=%s",
		taskId, channel.Id, resp.StatusCode, common.TruncateJsonValues(string(responseBody))))

	// 上游返回非 JSON（如 502 HTML 页面）时的处理
	if resp.StatusCode >= 400 && len(responseBody) > 0 && responseBody[0] == '<' {
		errMsg := fmt.Sprintf("upstream returned HTTP %d with non-JSON response", resp.StatusCode)

		// 对于 502/503/504 等临时性错误，允许重试最多 3 次再标记失败
		const maxPollRetries = 3
		if resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
			var taskData map[string]interface{}
			if err := common.Unmarshal(task.Data, &taskData); err != nil || taskData == nil {
				taskData = make(map[string]interface{})
			}
			pollErrCount := 0
			if v, ok := taskData["poll_error_count"].(float64); ok {
				pollErrCount = int(v)
			}
			pollErrCount++
			taskData["poll_error_count"] = pollErrCount
			if merged, mergeErr := common.Marshal(taskData); mergeErr == nil {
				task.Data = merged
				task.Update()
			}
			if pollErrCount < maxPollRetries {
				logger.LogWarn(ctx, fmt.Sprintf("[TaskPoll] task=%s %s (retry %d/%d, will retry next round)",
					taskId, errMsg, pollErrCount, maxPollRetries))
				// 写 LogTypeRetryFail 日志，让用户/管理员可在日志页看到中间重试记录
				retryModelName := task.Properties.OriginModelName
				if retryModelName == "" {
					retryModelName = task.Properties.UpstreamModelName
				}
				retryLog := &model.Log{
					UserId:    task.UserId,
					CreatedAt: common.GetTimestamp(),
					Type:      model.LogTypeRetryFail,
					Content: fmt.Sprintf("视频任务轮询重试 %d/%d，模型 %s，任务 %s，原因：%s",
						pollErrCount, maxPollRetries, retryModelName, task.TaskID, errMsg),
					ChannelId: task.ChannelId,
					ModelName: retryModelName,
					TokenName: task.TokenName,
					TokenId:   task.TokenId,
					Group:     task.Group,
					RequestId: task.TaskID,
					Other: common.MapToJsonStr(map[string]interface{}{
						"poll_error_count": pollErrCount,
						"max_poll_retries": maxPollRetries,
						"status_code":      resp.StatusCode,
					}),
				}
				model.CreateLog(retryLog)
				return nil
			}
			logger.LogError(ctx, fmt.Sprintf("[TaskPoll] task=%s %s (retry exhausted %d/%d, marking as failure)",
				taskId, errMsg, pollErrCount, maxPollRetries))
		} else {
			logger.LogError(ctx, fmt.Sprintf("[TaskPoll] task=%s %s", taskId, errMsg))
		}

		taskResult := relaycommon.FailTaskInfo(errMsg)
		// 直接跳到状态更新逻辑
		task.Status = model.TaskStatus(taskResult.Status)
		task.Progress = "100%"
		task.FailReason = errMsg
		if task.FinishTime == 0 {
			task.FinishTime = time.Now().Unix()
		}
		preStatus := task.Status
		task.Status = model.TaskStatusFailure
		shouldRefund := false
		if task.Quota != 0 && preStatus != model.TaskStatusFailure {
			shouldRefund = true
		}
		if err := task.Update(); err != nil {
			common.SysLog("UpdateVideoTask task error: " + err.Error())
			shouldRefund = false
		}
		if shouldRefund {
			logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] refund: task=%s user=%d quota=%s",
				task.TaskID, task.UserId, logger.LogQuota(task.Quota)))
			if err := model.IncreaseUserQuota(task.UserId, task.Quota, false); err != nil {
				logger.LogWarn(ctx, "Failed to increase user quota: "+err.Error())
			}
			logContent := fmt.Sprintf("Video async task failed %s, refund %s", task.TaskID, logger.LogQuota(task.Quota))
			model.RecordLog(task.UserId, model.LogTypeSystem, logContent)
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
			Content:   fmt.Sprintf("视频任务失败，模型 %s，原因：%s", modelName, errMsg),
			ChannelId: task.ChannelId,
			ModelName: modelName,
			TokenName: task.TokenName,
			TokenId:   task.TokenId,
			Group:     task.Group,
			RequestId: task.TaskID,
		}
		if err := model.CreateLog(failLog); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to insert error log for task %s: %v", task.TaskID, err))
		}
		return nil
	}

	taskResult := &relaycommon.TaskInfo{}
	// 仅当确认为本系统 New API 格式（code=success 且 data 含 task_id）时才走 New API 分支，避免 Vertex/Gemini 原始响应 {"name":"..."} 被误判导致 task.Data 计费字段丢失（与 nebula-new-api 对齐）
	var responseItems dto.TaskResponse[model.Task]
	_isNewAPIFormat := false
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		if responseItems.Data.TaskID != "" {
			_isNewAPIFormat = true
		}
	}
	if _isNewAPIFormat {
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] task=%s branch=newAPI", taskId))
		logger.LogDebug(ctx, fmt.Sprintf("UpdateVideoSingleTask parsed as new api response format: %+v", responseItems))
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.FailReason
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = mergeBillingFieldsIntoTaskData(task.Data, t.Data)
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	} else {
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] task=%s branch=parseResult", taskId))
		// 与 nebula-new-api 对齐：合并新的响应数据，但保留计费所需的关键字段
		var existingData map[string]interface{}
		var newData map[string]interface{}

		if len(task.Data) > 0 {
			if err := common.Unmarshal(task.Data, &existingData); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("UpdateVideoSingleTask: failed to unmarshal existing task.Data: %v", err))
				existingData = make(map[string]interface{})
			}
		} else {
			existingData = make(map[string]interface{})
		}

		// 调试日志：打印旧 task.Data 中的计费关键字段
		logger.LogDebug(ctx, fmt.Sprintf("[TaskPoll] task=%s existingData: requested_seconds=%v billing_processed=%v generate_audio=%v",
			taskId, existingData["requested_seconds"], existingData["billing_processed"], existingData["generate_audio"]))

		// 计费所需字段，合并时从 existingData 保留到 newData，避免被上游响应覆盖
		preservedFields := []string{
			"requested_seconds", "billing_requested_seconds",
			"billing_model_name", "billing_group",
			"billing_effective_group_ratio",
			"billing_token_name", "billing_token_id", "billing_processed",
			"generate_audio", "generateAudio",
			"has_video_input",
			"billing_cost_discount",
		}

		// 合并后若仍缺 generate_audio，从 upstream_request_body 补全（与计费逻辑一致，保证 task.Data 完整）
		ensureGenerateAudioInMap := func(data map[string]interface{}) {
			if _, hasAudio := data["generate_audio"]; hasAudio {
				return
			}
			if _, hasAudio := data["generateAudio"]; hasAudio {
				return
			}
			if parseGenerateAudioFromUpstreamBody(task.UpstreamRequestBody) {
				data["generate_audio"] = true
				data["generateAudio"] = true
			}
		}

		redactedBody := redactVideoResponseBody(responseBody)
		if err := common.Unmarshal(redactedBody, &newData); err != nil {
			// 解析失败时仍保留计费字段，将上游响应放入 _upstream_response（与 nebula-new-api 对齐，不直接用 redactedBody 覆盖）
			logger.LogWarn(ctx, fmt.Sprintf("UpdateVideoSingleTask: failed to unmarshal video response body: %v", err))
			newData = make(map[string]interface{})
			for _, field := range preservedFields {
				if value, exists := existingData[field]; exists {
					newData[field] = value
				}
			}
			ensureGenerateAudioInMap(newData)
			newData["_upstream_response"] = string(redactedBody)
			if merged, err := common.Marshal(newData); err == nil {
				task.Data = merged
			} else {
				// Marshal 失败时也不覆盖为纯 redactedBody，保留计费字段
				fallback := make(map[string]interface{})
				for _, f := range preservedFields {
					if v, ok := existingData[f]; ok {
						fallback[f] = v
					}
				}
				ensureGenerateAudioInMap(fallback)
				fallback["_upstream_response"] = string(redactedBody)
				if fb, _ := common.Marshal(fallback); len(fb) > 0 {
					task.Data = fb
				} else {
					task.Data = redactedBody
				}
			}
		} else {
			for _, field := range preservedFields {
				if value, exists := existingData[field]; exists {
					newData[field] = value
				}
			}
			ensureGenerateAudioInMap(newData)

			if merged, err := common.Marshal(newData); err == nil {
				task.Data = merged
			} else {
				logger.LogError(ctx, fmt.Sprintf("UpdateVideoSingleTask: failed to marshal merged task.Data: %v", err))
				// 不直接覆盖为 redactedBody，避免丢失计费字段；退化为仅保留计费字段 + 上游响应
				fallback := make(map[string]interface{})
				for _, field := range preservedFields {
					if value, exists := existingData[field]; exists {
						fallback[field] = value
					}
				}
				ensureGenerateAudioInMap(fallback)
				fallback["_upstream_response"] = string(redactedBody)
				if fb, _ := common.Marshal(fallback); len(fb) > 0 {
					task.Data = fb
				} else {
					task.Data = redactedBody
				}
			}
		}
	}

	safeTaskResult := *taskResult
	safeTaskResult.Url = truncateBase64(safeTaskResult.Url)
	safeTaskResult.RemoteUrl = truncateBase64(safeTaskResult.RemoteUrl)
	safeTaskResult.Reason = truncateBase64(safeTaskResult.Reason)
	logger.LogDebug(ctx, fmt.Sprintf("UpdateVideoSingleTask taskResult: %+v", safeTaskResult))

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
		// 计费路由：按秒计费模型（Sora-2 等）使用独立计费函数，其他沿用预扣费差额结算
		taskModelName := task.Properties.OriginModelName
		if taskModelName == "" {
			taskModelName = task.Properties.UpstreamModelName
		}
		billingRoute := "pre_deduction_settle"
		if isVideoPerSecondModel(taskModelName) {
			billingRoute = "per_second"
		} else if isVideoTokenRatioModel(taskModelName) {
			billingRoute = "token_ratio"
		} else if taskResult.TotalTokens > 0 {
			billingRoute = "token_settle"
		}
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] task_success: task=%s model=%s channel=%d billingRoute=%s preQuota=%d",
			taskId, taskModelName, channel.Id, billingRoute, task.Quota))

		// 对于 Veo 模型，跳过 OSS 上传逻辑（使用前缀匹配避免误匹配其他含 "veo" 的模型名）
		if strings.HasPrefix(strings.ToLower(taskModelName), "veo-") {
			if taskResult.RemoteUrl != "" {
				task.FailReason = taskResult.RemoteUrl
			} else if taskResult.Url != "" {
				task.FailReason = taskResult.Url
			}
		} else {
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
		}

		if isVideoPerSecondModel(taskModelName) {
			// 按秒计费：轮询成功后根据 task.Data 中保存的信息计费并写消费日志
			if err := handleSora2TaskBilling(ctx, task); err != nil {
				logger.LogError(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s failed: %v", taskModelName, task.TaskID, err))
				// 计费失败时将任务标记为 FAILURE，防止用户免费获得视频
				task.Status = model.TaskStatusFailure
				task.FailReason = fmt.Sprintf("billing_failed: %v", err)
			}
		} else if isVideoTokenRatioModel(taskModelName) {
			// VideoRatio/VideoCompletionRatio：轮询成功后根据 task.Data + usage.tokens 计费并写消费日志
			if err := handleVideoTokenRatioBilling(ctx, task, taskResult); err != nil {
				logger.LogError(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s failed: %v", taskModelName, task.TaskID, err))
				task.Status = model.TaskStatusFailure
				task.FailReason = fmt.Sprintf("billing_failed: %v", err)
			}
		} else if taskResult.TotalTokens > 0 {
			// 按 token 计费模型（如 Doubao）：根据实际 token 数结算差额
			var taskData map[string]interface{}
			if err := common.Unmarshal(task.Data, &taskData); err == nil {
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
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] task_failure: task=%s channel=%d preQuota=%d shouldRefund=%v reason=%s",
			taskId, channel.Id, quota, preStatus != model.TaskStatusFailure, common.TruncateJsonValues(taskResult.Reason)))
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
		failOtherMap := map[string]interface{}{}
		if projectID := getVideoTaskProjectID(task); projectID != "" {
			failOtherMap["admin_info"] = map[string]interface{}{
				"project_id": projectID,
			}
		}
		failOtherStr := ""
		if len(failOtherMap) > 0 {
			if b, err := common.Marshal(failOtherMap); err == nil {
				failOtherStr = string(b)
			}
		}
		failLog := &model.Log{
			UserId:    task.UserId,
			CreatedAt: common.GetTimestamp(),
			Type:      model.LogTypeError,
			Content:   fmt.Sprintf("视频任务失败，模型 %s，原因：%s", modelName, task.FailReason),
			ChannelId: task.ChannelId,
			ModelName: modelName,
			TokenName: task.TokenName,
			TokenId:   task.TokenId,
			Group:     task.Group,
			RequestId: task.TaskID,
			Other:     failOtherStr,
		}
		if err := model.CreateLog(failLog); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to insert error log for task %s: %v", task.TaskID, err))
		}
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, taskId)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}
	logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] before task.Update(): task=%s status=%s",
		taskId, task.Status))
	if err := task.Update(); err != nil {
		common.SysLog("UpdateVideoTask task error: " + err.Error())
		shouldRefund = false
	}

	if shouldRefund {
		// 任务失败且之前状态不是失败才退还额度，防止重复退还
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] refund: task=%s user=%d quota=%s",
			task.TaskID, task.UserId, logger.LogQuota(quota)))
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

// parseGenerateAudioFromUpstreamBody 从 upstream_request_body 解析 parameters.generateAudio / 顶层 generateAudio，用于计费与合并补全
func parseGenerateAudioFromUpstreamBody(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err != nil {
		return false
	}
	parseBool := func(v interface{}) bool {
		if b, ok := v.(bool); ok {
			return b
		}
		return false
	}
	for _, key := range []string{"generateAudio", "generate_audio"} {
		if parseBool(m[key]) {
			return true
		}
	}
	// Kling V3 使用 sound: "on"/"off"
	if s, ok := m["sound"].(string); ok {
		return strings.EqualFold(s, "on")
	}
	if params, _ := m["parameters"].(map[string]interface{}); params != nil {
		for _, key := range []string{"generateAudio", "generate_audio"} {
			if parseBool(params[key]) {
				return true
			}
		}
	}
	return false
}

// mergeBillingFieldsIntoTaskData 将 existingData 中的计费字段合并进 newData，返回合并后的 JSON；用于 New API 格式分支避免覆盖导致计费失败。
// 与 nebula-new-api 对齐：当上游响应非 New API（如 Vertex 仅返回 {"name":"..."}）时不要用空数据覆盖，保留 existingData。
func mergeBillingFieldsIntoTaskData(existingData, newData []byte) []byte {
	preserved := []string{
		"requested_seconds", "billing_requested_seconds",
		"billing_model_name", "billing_group",
		"billing_effective_group_ratio",
		"billing_token_name", "billing_token_id", "billing_processed",
		"generate_audio", "generateAudio", "sound",
		"has_video_input",
		"billing_cost_discount",
	}
	var existMap, newMap map[string]interface{}
	if len(existingData) > 0 {
		_ = common.Unmarshal(existingData, &existMap)
	}
	if len(newData) > 0 {
		_ = common.Unmarshal(newData, &newMap)
	}
	// 两者都空时不要用 nil 覆盖，保留原 existingData
	if existMap == nil && newMap == nil {
		return existingData
	}
	if newMap == nil {
		newMap = make(map[string]interface{})
	}
	for _, field := range preserved {
		if value, exists := existMap[field]; exists {
			newMap[field] = value
		}
	}
	merged, _ := common.Marshal(newMap)
	return merged
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
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
	b, err := common.Marshal(m)
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

// isVideoPerSecondModel 判断是否为按秒计费的视频模型（如 Veo、Sora 等，配置了每秒单价）
func isVideoPerSecondModel(name string) bool {
	_, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(name)
	return hasVideoPrice
}

func isVideoTokenRatioModel(name string) bool {
	if _, ok := ratio_setting.GetVideoRatio(name); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioPricing(name, true); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioPricing(name, false); ok {
		return true
	}
	// 视频输入维度（noVideo/video）
	if _, ok := ratio_setting.GetVideoCompletionRatioVideoPricing(name, true); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioVideoPricing(name, false); ok {
		return true
	}
	return false
}

// mergeVideoTaskDataWithUpstreamResponse 将上游响应合并进 task.Data，保留计费字段；供轮询与 GET 终态分支共用
func mergeVideoTaskDataWithUpstreamResponse(task *model.Task, responseBody []byte) {
	preservedFields := []string{
		"requested_seconds", "billing_requested_seconds",
		"billing_model_name", "billing_group",
		"billing_effective_group_ratio",
		"billing_token_name", "billing_token_id", "billing_processed",
		"generate_audio", "generateAudio",
		"has_video_input",
		"billing_cost_discount",
	}
	var existingData, newData map[string]interface{}
	if len(task.Data) > 0 {
		_ = common.Unmarshal(task.Data, &existingData)
	}
	if existingData == nil {
		existingData = make(map[string]interface{})
	}
	ensureGenerateAudioInMap := func(data map[string]interface{}) {
		if _, has := data["generate_audio"]; has {
			return
		}
		if _, has := data["generateAudio"]; has {
			return
		}
		if parseGenerateAudioFromUpstreamBody(task.UpstreamRequestBody) {
			data["generate_audio"] = true
			data["generateAudio"] = true
		}
	}
	redacted := redactVideoResponseBody(responseBody)
	if err := common.Unmarshal(redacted, &newData); err != nil {
		newData = make(map[string]interface{})
		for _, f := range preservedFields {
			if v, ok := existingData[f]; ok {
				newData[f] = v
			}
		}
		ensureGenerateAudioInMap(newData)
		newData["_upstream_response"] = string(redacted)
	} else {
		for _, f := range preservedFields {
			if v, ok := existingData[f]; ok {
				newData[f] = v
			}
		}
		ensureGenerateAudioInMap(newData)
	}
	if merged, err := common.Marshal(newData); err == nil {
		task.Data = merged
	}
}

// CompleteVideoTaskOnUpstreamSuccess 在 GET /v1/videos 收到上游终态（SUCCESS/FAILURE）时落库并计费，与轮询路径一致，避免仅轮询返回 {"name":"..."} 时任务永不完成
func CompleteVideoTaskOnUpstreamSuccess(ctx context.Context, task *model.Task, channel *model.Channel, taskResult *relaycommon.TaskInfo, responseBody []byte) error {
	mergeVideoTaskDataWithUpstreamResponse(task, responseBody)
	now := time.Now().Unix()
	task.Status = model.TaskStatus(taskResult.Status)
	task.Progress = taskResult.Progress
	if taskResult.Progress == "" {
		if taskResult.Status == model.TaskStatusSuccess || taskResult.Status == model.TaskStatusFailure {
			task.Progress = "100%"
		}
	}
	if taskResult.Status == model.TaskStatusSuccess {
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		taskModelName := task.Properties.OriginModelName
		if taskModelName == "" {
			taskModelName = task.Properties.UpstreamModelName
		}
		if strings.HasPrefix(strings.ToLower(taskModelName), "veo-") {
			if taskResult.RemoteUrl != "" {
				task.FailReason = taskResult.RemoteUrl
			} else if taskResult.Url != "" {
				task.FailReason = taskResult.Url
			}
		} else {
			if taskResult.RemoteUrl != "" {
				task.FailReason = taskResult.RemoteUrl
			} else if taskResult.Url != "" {
				task.FailReason = taskResult.Url
			}
		}
	}
	if taskResult.Status == model.TaskStatusFailure {
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.TrimSpace(taskResult.Reason) != "" {
			task.FailReason = strings.TrimSpace(taskResult.Reason)
		}
	}
	// 只更新状态相关字段，绝不碰 quota —— quota 由 handleSora2TaskBilling / handleVideoTokenRatioBilling
	// 的 DB 原子 guard（WHERE quota=0）独占更新。使用 DB.Save 全量写回会把后台轮询已更新的 quota 覆盖回 0，
	// 导致 DB guard 被绕过、重复计费。
	updateFields := map[string]interface{}{
		"status":      task.Status,
		"progress":    task.Progress,
		"fail_reason": task.FailReason,
		"finish_time": task.FinishTime,
		"data":        task.Data,
	}
	if err := model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(updateFields).Error; err != nil {
		return err
	}
	taskModelName := task.Properties.OriginModelName
	if taskModelName == "" {
		taskModelName = task.Properties.UpstreamModelName
	}
	if taskResult.Status == model.TaskStatusSuccess && isVideoPerSecondModel(taskModelName) {
		if err := handleSora2TaskBilling(ctx, task); err != nil {
			logger.LogError(ctx, fmt.Sprintf("[VideoBilling] GET path model=%s task=%s failed: %v", taskModelName, task.TaskID, err))
			_ = model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
				"status":      model.TaskStatusFailure,
				"fail_reason": fmt.Sprintf("billing_failed: %v", err),
			}).Error
			return err
		}
	}
	if taskResult.Status == model.TaskStatusSuccess && isVideoTokenRatioModel(taskModelName) {
		if err := handleVideoTokenRatioBilling(ctx, task, taskResult); err != nil {
			logger.LogError(ctx, fmt.Sprintf("[VideoBilling] GET path token_ratio model=%s task=%s failed: %v", taskModelName, task.TaskID, err))
			_ = model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
				"status":      model.TaskStatusFailure,
				"fail_reason": fmt.Sprintf("billing_failed: %v", err),
			}).Error
			return err
		}
	}
	return nil
}

// handleSora2TaskBilling 处理按秒计费视频模型的轮询成功计费逻辑（Sora-2 等）
// 计费公式: actualQuota = videoPrice × requestedSeconds × QuotaPerUnit × groupRatio
func handleSora2TaskBilling(ctx context.Context, task *model.Task) error {
	modelName := task.Properties.OriginModelName
	if modelName == "" {
		modelName = task.Properties.UpstreamModelName
	}
	{
		multiKeyIdx := -1
		if task.Properties.MultiKeyIndex != nil {
			multiKeyIdx = *task.Properties.MultiKeyIndex
		}
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] per_second_start: task=%s model=%s channel=%d projectID=%s multiKeyIndex=%d user=%d group=%s",
			task.TaskID, modelName, task.ChannelId, task.Properties.ProjectID, multiKeyIdx, task.UserId, task.Group))
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

	// 防重复计费（内存级检查）
	if processed, ok := taskData["billing_processed"].(bool); ok && processed {
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s already billed, skip", modelName, task.TaskID))
		return nil
	}

	// 防重复计费（DB 级原子抢占）：利用 quota 字段，仅当 quota=0（未计费）时原子更新为 -1（计费中）
	// 后台轮询和 GET /v1/videos 路径可能并发调用此函数，内存级检查无法防止竞态
	claimResult := model.DB.Model(&model.Task{}).Where("id = ? AND quota = 0", task.ID).Update("quota", -1)
	if claimResult.Error != nil {
		return fmt.Errorf("handleSora2TaskBilling: failed to claim billing lock: %w", claimResult.Error)
	}
	if claimResult.RowsAffected == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s already billed (DB guard), skip", modelName, task.TaskID))
		return nil
	}

	// 读取关键字段：requested_seconds（最高优先级 upstream_request_body，再 task.Data，再 Properties）
	// Gemini/Veo 上游体结构：顶层无 durationSeconds 时在 parameters 或 instances[0] 中
	requestedSeconds := 0
	if len(task.UpstreamRequestBody) > 0 {
		var upstreamReq map[string]interface{}
		if err := common.Unmarshal(task.UpstreamRequestBody, &upstreamReq); err == nil {
			if v, ok := upstreamReq["durationSeconds"].(float64); ok && v > 0 {
				requestedSeconds = int(v)
			} else if v, ok := upstreamReq["durationSeconds"].(int); ok && v > 0 {
				requestedSeconds = v
			}
			if requestedSeconds <= 0 {
				if instances, ok := upstreamReq["instances"].([]interface{}); ok && len(instances) > 0 {
					if inst, ok := instances[0].(map[string]interface{}); ok {
						if v, ok := inst["durationSeconds"].(float64); ok && v > 0 {
							requestedSeconds = int(v)
						} else if v, ok := inst["durationSeconds"].(int); ok && v > 0 {
							requestedSeconds = v
						}
					}
				}
			}
			// Gemini Veo 请求体把 durationSeconds 放在 parameters 中；Ali wan2.6 把 duration 放在 parameters 中
			if requestedSeconds <= 0 {
				if params, ok := upstreamReq["parameters"].(map[string]interface{}); ok && params != nil {
					for _, key := range []string{"durationSeconds", "duration"} {
						if requestedSeconds > 0 {
							break
						}
						switch v := params[key].(type) {
						case float64:
							if v > 0 {
								requestedSeconds = int(v)
							}
						case int:
							if v > 0 {
								requestedSeconds = v
							}
						}
					}
				}
			}
			// Kling 使用顶层 "duration" 字段（字符串格式如 "5"）
			if requestedSeconds <= 0 {
				switch v := upstreamReq["duration"].(type) {
				case string:
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						requestedSeconds = n
					}
				case float64:
					if v > 0 {
						requestedSeconds = int(v)
					}
				case int:
					if v > 0 {
						requestedSeconds = v
					}
				}
			}
		}
	}
	if requestedSeconds <= 0 {
		if v, ok := taskData["requested_seconds"].(float64); ok {
			requestedSeconds = int(v)
		} else if v, ok := taskData["requested_seconds"].(int); ok {
			requestedSeconds = v
		}
	}
	if requestedSeconds <= 0 {
		if v, ok := taskData["billing_requested_seconds"].(float64); ok {
			requestedSeconds = int(v)
		} else if v, ok := taskData["billing_requested_seconds"].(int); ok {
			requestedSeconds = v
		}
	}
	if requestedSeconds <= 0 {
		if secondsStr, ok := taskData["seconds"].(string); ok && secondsStr != "" {
			if sec, err := strconv.Atoi(secondsStr); err == nil && sec > 0 {
				requestedSeconds = sec
			}
		}
	}
	// 从上游响应 usage 中读取实际时长（Ali wan2.6 等返回 usage.duration / usage.output_video_duration）
	if requestedSeconds <= 0 {
		if usage, ok := taskData["usage"].(map[string]interface{}); ok {
			for _, key := range []string{"output_video_duration", "duration"} {
				if requestedSeconds > 0 {
					break
				}
				switch v := usage[key].(type) {
				case float64:
					if v > 0 {
						requestedSeconds = int(v)
					}
				case int:
					if v > 0 {
						requestedSeconds = v
					}
				}
			}
		}
	}
	if requestedSeconds <= 0 && task.Properties.RequestedSeconds > 0 {
		requestedSeconds = task.Properties.RequestedSeconds
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s using Properties.RequestedSeconds=%d", task.TaskID, requestedSeconds))
	}
	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s requested_seconds=%d (properties=%d)", task.TaskID, requestedSeconds, task.Properties.RequestedSeconds))
	// 最后兜底：按秒计费模型若仍为 0（历史数据或入库异常），按 4 秒计费，避免计费失败导致任务标 FAILURE
	if requestedSeconds <= 0 {
		logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s requested_seconds=0, fallback to 4s for billing", modelName, task.TaskID))
		model.RecordLog(task.UserId, model.LogTypeSystem,
			fmt.Sprintf("[VideoBilling] model=%s task=%s requested_seconds 解析失败，兜底按 4s 计费（properties=%d）",
				modelName, task.TaskID, task.Properties.RequestedSeconds))
		requestedSeconds = 4
	}

	tokenName := ""
	if task.TokenName != "" {
		tokenName = task.TokenName
	} else if v, ok := taskData["billing_token_name"].(string); ok {
		tokenName = v
	}
	tokenId := 0
	if task.TokenId > 0 {
		tokenId = task.TokenId
	} else {
		switch v := taskData["billing_token_id"].(type) {
		case float64:
			tokenId = int(v)
		case int:
			tokenId = v
		}
	}
	billingGroup := task.Group
	if v, ok := taskData["billing_group"].(string); ok && v != "" {
		billingGroup = v
	}

	// 使用提交时存储的 effectiveGroupRatio（已包含用户组倍率），与提交时估算保持完全一致
	// 若不存在（旧数据兼容），则回退到全局 groupRatio
	groupRatio := 0.0
	if v, ok := taskData["billing_effective_group_ratio"].(float64); ok && v > 0 {
		groupRatio = v
	}
	if groupRatio <= 0 {
		groupRatio = ratio_setting.GetGroupRatio(billingGroup)
	}
	if groupRatio <= 0 {
		groupRatio = 1.0
	}

	// 是否生成音频（Veo 非 fast：generateAudio true 用 audio 0.4/秒，否则 noAudio 0.2/秒）
	// 优先从 upstream_request_body 读（与 requested_seconds 一致，来源可靠），再兜底 task.Data
	generateAudioFromUpstream := parseGenerateAudioFromUpstreamBody(task.UpstreamRequestBody)
	generateAudio := generateAudioFromUpstream
	if !generateAudio {
		if v, ok := taskData["generate_audio"].(bool); ok {
			generateAudio = v
		} else if v, ok := taskData["generateAudio"].(bool); ok {
			generateAudio = v
		} else if s, ok := taskData["generate_audio"].(string); ok {
			generateAudio = strings.EqualFold(strings.TrimSpace(s), "true") || s == "1"
		} else if s, ok := taskData["generateAudio"].(string); ok {
			generateAudio = strings.EqualFold(strings.TrimSpace(s), "true") || s == "1"
		}
	}
	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s generate_audio=%v (from_upstream=%v)", task.TaskID, generateAudio, generateAudioFromUpstream))
	// 获取官方单秒价格（wan2.6-flash 含分辨率分档；Veo 等为 noAudio/audio 或单一数字）
	// 优先使用上游 usage.size（实际分辨率），兜底解析上游请求体中的参数
	resKey := ""
	if usageMap, ok := taskData["usage"].(map[string]interface{}); ok && usageMap != nil {
		if usageSize, ok := usageMap["size"].(string); ok && usageSize != "" {
			resKey = ratio_setting.NormalizeVideoResolutionKey(alitask.ParseBillingResolutionFromSize(usageSize))
			logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s billing_resolution from usage.size=%s -> %s", task.TaskID, usageSize, resKey))
		}
	}
	if resKey == "" {
		resKey = ratio_setting.NormalizeVideoResolutionKey(alitask.ParseBillingResolutionKeyFromUpstreamJSON(task.UpstreamRequestBody))
	}
	officialVideoPrice, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecondForBillingWithResolution(modelName, generateAudio, resKey)
	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s billing_resolution=%s", task.TaskID, resKey))
	if !hasVideoPrice || officialVideoPrice <= 0 {
		return fmt.Errorf("handleSora2TaskBilling: video price per second not configured for model: %s", modelName)
	}

	// 对于 wan2.6 系列，优先使用 usage.output_video_duration（实际输出时长），其次 usage.duration，再回退到 requestedSeconds
	if usageMap, ok := taskData["usage"].(map[string]interface{}); ok && usageMap != nil {
		actualSec := 0
		if v, ok := usageMap["output_video_duration"].(float64); ok && v > 0 {
			actualSec = int(v)
		} else if v, ok := usageMap["output_video_duration"].(int); ok && v > 0 {
			actualSec = v
		}
		if actualSec <= 0 {
			if v, ok := usageMap["duration"].(float64); ok && v > 0 {
				actualSec = int(v)
			} else if v, ok := usageMap["duration"].(int); ok && v > 0 {
				actualSec = v
			}
		}
		if actualSec > 0 && actualSec != requestedSeconds {
			logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s using actual duration from usage: %d (was requested: %d)", task.TaskID, actualSec, requestedSeconds))
			requestedSeconds = actualSec
		}
	}

	// 计算实际扣费 quota
	// actualQuota = officialVideoPrice × requestedSeconds × QuotaPerUnit × groupRatio
	effectiveVideoPrice := officialVideoPrice

	actualQuotaFloat := effectiveVideoPrice * float64(requestedSeconds) * common.QuotaPerUnit * groupRatio

	actualQuota := int(actualQuotaFloat)
	if actualQuota < 0 {
		actualQuota = 0
	}

	// 计费过程日志：公式与各因子，便于排查
	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s seconds=%d generate_audio=%v officialPrice=%.4f groupRatio=%.4f effectiveUserPrice=%.4f actualQuota=%d",
		modelName, task.TaskID, requestedSeconds, generateAudio, officialVideoPrice, groupRatio, effectiveVideoPrice, actualQuota))
	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] formula: effectivePrice(%.4f) × seconds(%d) × QuotaPerUnit(%.0f) × groupRatio(%.4f) = %d",
		effectiveVideoPrice, requestedSeconds, common.QuotaPerUnit, groupRatio, actualQuota))

	// 执行扣费
	if actualQuota > 0 {
		if err := model.DecreaseUserQuota(task.UserId, actualQuota); err != nil {
			return fmt.Errorf("handleSora2TaskBilling: DecreaseUserQuota failed: %w", err)
		}
	}

	// 获取用户名（用于日志）
	username := ""
	if user, err := model.GetUserById(task.UserId, false); err == nil && user != nil {
		username = user.Username
	}

	useTime := 0
	if task.FinishTime > 0 && task.StartTime > 0 {
		useTime = int(task.FinishTime - task.StartTime)
	}

	logContent := fmt.Sprintf("视频任务成功，模型 %s，时长 %d 秒，耗时 %ds，扣费 %s",
		modelName, requestedSeconds, useTime, logger.LogQuota(actualQuota))

	otherMap := map[string]interface{}{
		"billing_type":                    "per_second",
		"requested_seconds":               requestedSeconds,
		"official_video_price_per_second": officialVideoPrice,
		"video_price_per_second":          effectiveVideoPrice,
		"group_ratio":                     groupRatio,
		"user_group_ratio":                groupRatio,
	}
	// official_quota: vendor cost without group ratio
	if groupRatio > 0 {
		otherMap["official_quota"] = float64(actualQuota) / groupRatio
	}
	// 写入渠道成本折扣：优先从 task.Data 取（提交时保存），兜底从渠道查
	adminInfo := make(map[string]interface{})
	costDiscount := 0.0
	if v, ok := taskData["billing_cost_discount"].(float64); ok && v > 0 {
		costDiscount = v
	}
	if costDiscount <= 0 {
		// 兜底：通过 channel_id 查询渠道信息
		if ch, err := model.CacheGetChannel(task.ChannelId); err == nil && ch != nil && ch.CostDiscount != nil && *ch.CostDiscount > 0 {
			costDiscount = *ch.CostDiscount
			logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s cost_discount fallback from channel: %.3f", task.TaskID, costDiscount))
		}
	}
	if costDiscount > 0 {
		adminInfo["cost_discount"] = costDiscount
	}
	if projectID := getVideoTaskProjectID(task); projectID != "" {
		adminInfo["project_id"] = projectID
	}
	if len(adminInfo) > 0 {
		otherMap["admin_info"] = adminInfo
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
		RequestId:        task.TaskID,
		Other:            string(otherBytes),
	}
	if err := model.CreateLog(consumeLog); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s failed to insert consume log: %v", modelName, task.TaskID, err))
	}

	// 更新用量统计
	model.UpdateUserUsedQuotaAndRequestCount(task.UserId, actualQuota)
	model.UpdateChannelUsedQuota(task.ChannelId, actualQuota)

	// 更新 task.Quota 为实际扣费额度，标记 billing_processed=true 并立即持久化到 DB
	// 必须在函数内持久化，因为 CompleteVideoTaskOnUpstreamSuccess（GET 路径）在调用本函数后不会再调 task.Update()
	task.Quota = actualQuota
	taskData["billing_processed"] = true
	if merged, err := common.Marshal(taskData); err == nil {
		task.Data = merged
	}
	if err := model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"quota": actualQuota,
		"data":  task.Data,
	}).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s failed to persist billing result: %v", modelName, task.TaskID, err))
	}

	return nil
}

// handleVideoTokenRatioBilling 处理 VideoRatio/VideoCompletionRatio 按量计费视频模型的轮询成功计费逻辑（如 seedance）
// 计费公式：
//   - 若 VideoRatio 存在且 !=0：actualQuota = tokens × (VideoRatio×VideoCompletionRatio) × groupRatio
//   - 否则（VideoRatio 不存在/为0）：actualQuota = tokens × ($/M tokens)/1e6 × QuotaPerUnit × groupRatio
func handleVideoTokenRatioBilling(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo) error {
	modelName := task.Properties.OriginModelName
	if modelName == "" {
		modelName = task.Properties.UpstreamModelName
	}
	{
		multiKeyIdx := -1
		if task.Properties.MultiKeyIndex != nil {
			multiKeyIdx = *task.Properties.MultiKeyIndex
		}
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio_start: task=%s model=%s channel=%d projectID=%s multiKeyIndex=%d user=%d group=%s",
			task.TaskID, modelName, task.ChannelId, task.Properties.ProjectID, multiKeyIdx, task.UserId, task.Group))
	}

	// 从 task.Data 读取提交时保存的计费信息
	var taskData map[string]interface{}
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &taskData); err != nil {
			return fmt.Errorf("handleVideoTokenRatioBilling: unmarshal task.Data failed: %w", err)
		}
	}
	if taskData == nil {
		taskData = make(map[string]interface{})
	}

	// 防重复计费（内存级检查）
	if processed, ok := taskData["billing_processed"].(bool); ok && processed {
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s already billed, skip", modelName, task.TaskID))
		return nil
	}

	// 防重复计费（DB 级原子抢占）：利用 quota 字段，仅当 quota=0（未计费）时原子更新为 -1（计费中）
	claimResult := model.DB.Model(&model.Task{}).Where("id = ? AND quota = 0", task.ID).Update("quota", -1)
	if claimResult.Error != nil {
		return fmt.Errorf("handleVideoTokenRatioBilling: failed to claim billing lock: %w", claimResult.Error)
	}
	if claimResult.RowsAffected == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s already billed (DB guard), skip", modelName, task.TaskID))
		return nil
	}

	if taskResult == nil {
		return fmt.Errorf("handleVideoTokenRatioBilling: taskResult is nil")
	}

	// tokens：优先 completion_tokens，其次 total_tokens
	tokens := 0
	if taskResult.CompletionTokens > 0 {
		tokens = taskResult.CompletionTokens
	} else if taskResult.TotalTokens > 0 {
		tokens = taskResult.TotalTokens
	}
	if tokens <= 0 {
		// mock 环境不会返回用量信息，此时跳过计费而不报错，避免任务被标记为失败
		logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s tokens=0 (completion=%d total=%d), skip billing (mock or no usage reported)",
			modelName, task.TaskID, taskResult.CompletionTokens, taskResult.TotalTokens))
		// 释放 DB 级计费锁（quota 从 -1 恢复为 0）并标记已处理
		taskData["billing_processed"] = true
		taskData["billing_skipped"] = "no_usage"
		if merged, err := common.Marshal(taskData); err == nil {
			task.Data = merged
		}
		task.Quota = 0
		model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
			"quota": 0,
			"data":  task.Data,
		})
		return nil
	}

	tokenName := ""
	if task.TokenName != "" {
		tokenName = task.TokenName
	} else if v, ok := taskData["billing_token_name"].(string); ok {
		tokenName = v
	}
	tokenId := 0
	if task.TokenId > 0 {
		tokenId = task.TokenId
	} else {
		switch v := taskData["billing_token_id"].(type) {
		case float64:
			tokenId = int(v)
		case int:
			tokenId = v
		}
	}
	billingGroup := task.Group
	if v, ok := taskData["billing_group"].(string); ok && v != "" {
		billingGroup = v
	}

	groupRatio := 0.0
	if v, ok := taskData["billing_effective_group_ratio"].(float64); ok && v > 0 {
		groupRatio = v
	}
	if groupRatio <= 0 {
		groupRatio = ratio_setting.GetGroupRatio(billingGroup)
	}
	if groupRatio <= 0 {
		groupRatio = 1.0
	}

	// 是否生成音频：优先从 upstream_request_body 读，再兜底 task.Data
	generateAudioFromUpstream := parseGenerateAudioFromUpstreamBody(task.UpstreamRequestBody)
	generateAudio := generateAudioFromUpstream
	if !generateAudio {
		if v, ok := taskData["generate_audio"].(bool); ok {
			generateAudio = v
		} else if v, ok := taskData["generateAudio"].(bool); ok {
			generateAudio = v
		} else if s, ok := taskData["generate_audio"].(string); ok {
			generateAudio = strings.EqualFold(strings.TrimSpace(s), "true") || s == "1"
		} else if s, ok := taskData["generateAudio"].(string); ok {
			generateAudio = strings.EqualFold(strings.TrimSpace(s), "true") || s == "1"
		}
	}

	// 读取配置：GetVideoCompletionRatioPricing 返回「有效值」：
	// - VideoRatio!=0 时：返回倍率（VideoRatio×VideoCompletionRatio）
	// - VideoRatio==0/无时：返回 $/M tokens
	// 当 task.Data 中存在 has_video_input 时，使用 noVideo/video 维度（UpToken seedance 等）
	var cfgVal float64
	var ok bool
	if hasVideoInput, exists := taskData["has_video_input"].(bool); exists {
		cfgVal, ok = ratio_setting.GetVideoCompletionRatioVideoPricing(modelName, hasVideoInput)
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio task=%s using video_input dimension: has_video_input=%v cfgVal=%.6f ok=%v",
			task.TaskID, hasVideoInput, cfgVal, ok))
	} else {
		cfgVal, ok = ratio_setting.GetVideoCompletionRatioPricing(modelName, generateAudio)
	}
	if !ok || cfgVal <= 0 {
		return fmt.Errorf("handleVideoTokenRatioBilling: video completion ratio not configured for model=%s", modelName)
	}

	vr, hasVR := ratio_setting.GetVideoRatio(modelName)
	ratioMode := hasVR && vr != 0

	actualQuota := 0
	otherMap := map[string]interface{}{
		"billing_type":               "video_token_ratio",
		"tokens":                     tokens,
		"generate_audio":             generateAudio,
		"group_ratio":                groupRatio,
		"video_ratio":                vr,
		"video_completion_ratio_val": cfgVal,
		"ratio_mode":                 ratioMode,
	}
	// 记录视频输入维度（如果存在）
	if hasVideoInput, exists := taskData["has_video_input"].(bool); exists {
		otherMap["has_video_input"] = hasVideoInput
	}

	if ratioMode {
		// 走倍率体系：与 ModelRatio 计费一致，quota = tokens * ratio * groupRatio
		actualQuotaFloat := float64(tokens) * cfgVal * groupRatio
		actualQuota = int(actualQuotaFloat)
		otherMap["effective_video_ratio"] = cfgVal
	} else {
		// 走价格体系：cfgVal 为 $/M tokens
		pricePerMillion := cfgVal
		pricePerToken := pricePerMillion / 1000000.0
		actualQuotaFloat := pricePerToken * float64(tokens) * common.QuotaPerUnit * groupRatio
		actualQuota = int(actualQuotaFloat)
		otherMap["video_price_per_million_tokens"] = pricePerMillion
		otherMap["video_price_per_token"] = pricePerToken
	}
	if actualQuota < 0 {
		actualQuota = 0
	}

	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s tokens=%d generate_audio=%v cfgVal=%.6f ratioMode=%v groupRatio=%.4f actualQuota=%d",
		modelName, task.TaskID, tokens, generateAudio, cfgVal, ratioMode, groupRatio, actualQuota))

	// 执行扣费
	if actualQuota > 0 {
		if err := model.DecreaseUserQuota(task.UserId, actualQuota); err != nil {
			return fmt.Errorf("handleVideoTokenRatioBilling: DecreaseUserQuota failed: %w", err)
		}
	}

	// 获取用户名（用于日志）
	username := ""
	if user, err := model.GetUserById(task.UserId, false); err == nil && user != nil {
		username = user.Username
	}

	useTime := 0
	if task.FinishTime > 0 && task.StartTime > 0 {
		useTime = int(task.FinishTime - task.StartTime)
	}

	logContent := fmt.Sprintf("视频任务成功，模型 %s，tokens %d，耗时 %ds，扣费 %s",
		modelName, tokens, useTime, logger.LogQuota(actualQuota))

	// 写入渠道成本折扣：优先从 task.Data 取（提交时保存），兜底从渠道查
	adminInfoTokenRatio := make(map[string]interface{})
	costDiscountTokenRatio := 0.0
	if v, ok := taskData["billing_cost_discount"].(float64); ok && v > 0 {
		costDiscountTokenRatio = v
	}
	if costDiscountTokenRatio <= 0 {
		if ch, err := model.CacheGetChannel(task.ChannelId); err == nil && ch != nil && ch.CostDiscount != nil && *ch.CostDiscount > 0 {
			costDiscountTokenRatio = *ch.CostDiscount
			logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio task=%s cost_discount fallback from channel: %.3f", task.TaskID, costDiscountTokenRatio))
		}
	}
	if costDiscountTokenRatio > 0 {
		adminInfoTokenRatio["cost_discount"] = costDiscountTokenRatio
	}
	if projectID := getVideoTaskProjectID(task); projectID != "" {
		adminInfoTokenRatio["project_id"] = projectID
	}
	if len(adminInfoTokenRatio) > 0 {
		otherMap["admin_info"] = adminInfoTokenRatio
	}
	// official_quota: vendor cost without group ratio
	if groupRatio > 0 && actualQuota > 0 {
		otherMap["official_quota"] = float64(actualQuota) / groupRatio
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
		CompletionTokens: tokens,
		TokenName:        tokenName,
		TokenId:          tokenId,
		UseTime:          useTime,
		Group:            billingGroup,
		RequestId:        task.TaskID,
		Other:            string(otherBytes),
	}
	if err := model.CreateLog(consumeLog); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s failed to insert consume log: %v", modelName, task.TaskID, err))
	}

	model.UpdateUserUsedQuotaAndRequestCount(task.UserId, actualQuota)
	model.UpdateChannelUsedQuota(task.ChannelId, actualQuota)

	task.Quota = actualQuota
	taskData["billing_processed"] = true
	if merged, err := common.Marshal(taskData); err == nil {
		task.Data = merged
	}
	if err := model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"quota": actualQuota,
		"data":  task.Data,
	}).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s failed to persist billing result: %v", modelName, task.TaskID, err))
	}
	return nil
}

// extractProjectFromTaskID 从 Vertex AI 视频任务的 taskID 中提取 project ID。
// taskID 是 base64 编码的 operation name，格式：projects/PROJECT_ID/locations/.../models/.../operations/...
var taskProjectRe = regexp.MustCompile(`projects/([^/]+)/locations/`)

func extractProjectFromTaskID(taskID string) string {
	b, err := base64.RawURLEncoding.DecodeString(taskID)
	if err != nil {
		return ""
	}
	matches := taskProjectRe.FindStringSubmatch(string(b))
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// getVideoTaskProjectID 从视频任务中提取 Vertex AI 的 project_id。
// 优先级：Properties.ProjectID（最可靠，提交时写入，不被轮询覆盖）
//
//	> PrivateData.Key（完整 JSON 凭证解析）
//	> taskID（base64 operation name 提取）
func getVideoTaskProjectID(task *model.Task) string {
	if task.Properties.ProjectID != "" {
		return task.Properties.ProjectID
	}
	if task.PrivateData.Key != "" {
		var cred struct {
			ProjectID string `json:"project_id"`
		}
		if err := common.Unmarshal([]byte(task.PrivateData.Key), &cred); err == nil && cred.ProjectID != "" {
			return cred.ProjectID
		}
	}
	return extractProjectFromTaskID(task.TaskID)
}
