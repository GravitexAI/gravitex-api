package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	// 429 rate limit: skip this poll cycle silently; the next scheduled tick will retry
	if resp.StatusCode == http.StatusTooManyRequests {
		logger.LogWarn(ctx, fmt.Sprintf("[TaskPoll] task=%s upstream returned 429, will retry next poll cycle", taskId))
		return nil
	}

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
			} else {
				// 用户额度退还成功后同步退还令牌额度
				service.TaskAdjustTokenQuota(ctx, task, -task.Quota)
			}
			logContent := fmt.Sprintf("Video async task failed %s, refund %s", task.TaskID, logger.LogQuota(task.Quota))
			model.RecordLog(task.UserId, model.LogTypeSystem, logContent)
		}
		// 记录失败日志
		modelName := task.Properties.OriginModelName
		if modelName == "" {
			modelName = task.Properties.UpstreamModelName
		}
		username, _ := model.GetUsernameById(task.UserId, false)
		failLog := &model.Log{
			UserId:    task.UserId,
			Username:  username,
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
	skipUpstreamParse := false

	// 上游返回 4xx JSON 错误响应（如 404 ResourceNotFound）时，直接标记失败
	// 4xx 是客户端错误，通常不可重试，直接判定任务失败
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := common.Unmarshal(responseBody, &errResp); err == nil && errResp.Error.Message != "" {
			errMsg := fmt.Sprintf("%s: %s", errResp.Error.Code, errResp.Error.Message)
			logger.LogError(ctx, fmt.Sprintf("[TaskPoll] task=%s upstream 4xx JSON error (HTTP %d): %s", taskId, resp.StatusCode, errMsg))
			taskResult = relaycommon.FailTaskInfo(errMsg)
			task.Data = mergeBillingFieldsIntoTaskData(task.Data, responseBody)
			skipUpstreamParse = true
		}
	}

	if !skipUpstreamParse {
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
			// New API 对外格式使用 OpenAI Video 状态（completed/failed/queued/in_progress），
			// 而内部使用 SUCCESS/FAILURE/QUEUED/IN_PROGRESS。当上游也是 new-api 实例时，
			// 返回的是对外格式，需要映射回内部状态，否则后续计费分支无法匹配。
			switch taskResult.Status {
			case "completed":
				taskResult.Status = model.TaskStatusSuccess
			case "failed":
				taskResult.Status = model.TaskStatusFailure
			case "queued":
				taskResult.Status = model.TaskStatusQueued
			case "in_progress", "processing":
				taskResult.Status = model.TaskStatusInProgress
			}
			// 从 t.Data（JSON RawMessage）中提取 usage tokens 用于计费
			// 上游 new-api 返回格式：data.metadata.usage.{completion_tokens, total_tokens}
			if len(t.Data) > 0 {
				var dataMap map[string]interface{}
				if err := common.Unmarshal(t.Data, &dataMap); err == nil {
					if metadata, ok := dataMap["metadata"].(map[string]interface{}); ok {
						if usage, ok := metadata["usage"].(map[string]interface{}); ok {
							if ct, ok := usage["completion_tokens"].(float64); ok && ct > 0 {
								taskResult.CompletionTokens = int(ct)
							}
							if tt, ok := usage["total_tokens"].(float64); ok && tt > 0 {
								taskResult.TotalTokens = int(tt)
							}
						}
						// 提取分辨率（用于按分辨率维度计费）
						if res, ok := metadata["resolution"].(string); ok && res != "" {
							taskResult.Resolution = res
						}
					}
					// 兜底：顶层 seconds 字段（用于按秒计费模型）
					if sec, ok := dataMap["seconds"].(string); ok && sec != "" {
						if n, err2 := strconv.Atoi(sec); err2 == nil && n > 0 && taskResult.ActualDuration == 0 {
							taskResult.ActualDuration = n
						}
					}
				}
			}
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
				"video_resolution",
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
	} // end if !skipUpstreamParse

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

		// Veo 和 Gemini Omni 的结果由上游直接提供，不上传 OSS。
		// Omni 的 inline data 保存在 task.Data，由 ConvertToOpenAIVideo 读取，避免把大 Base64 写入 fail_reason。
		if strings.HasPrefix(strings.ToLower(taskModelName), "veo-") || isGeminiOmniVideoModel(taskModelName) {
			if taskResult.RemoteUrl != "" {
				task.FailReason = taskResult.RemoteUrl
			} else if taskResult.Url != "" && !isGeminiOmniDataURL(taskResult.Url) {
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
			if err := handleVideoPerSecondBilling(ctx, task); err != nil {
				logger.LogError(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s failed: %v", taskModelName, task.TaskID, err))
				// 计费失败仅记录日志，不覆盖任务状态，视频已生成应正常返回给用户
			}
		} else if isVideoTokenRatioModel(taskModelName) {
			// VideoRatio/VideoCompletionRatio：轮询成功后根据 task.Data + usage.tokens 计费并写消费日志
			if err := handleVideoTokenRatioBilling(ctx, task, taskResult); err != nil {
				logger.LogError(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s failed: %v", taskModelName, task.TaskID, err))
				// 计费失败仅记录日志，不覆盖任务状态，视频已生成应正常返回给用户
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
								if err := model.DecreaseUserQuota(task.UserId, quotaDelta, true); err != nil {
									logger.LogError(ctx, fmt.Sprintf("补扣费失败: %s", err.Error()))
								} else {
									// 同步补扣令牌额度
									service.TaskAdjustTokenQuota(ctx, task, quotaDelta)
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
									// 同步退还令牌额度
									service.TaskAdjustTokenQuota(ctx, task, -refundQuota)
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

		// 保底计费：当正常计费路由执行后 quota 仍为 0 时，尝试从 upstream_request_body 解析参数并重新计费
		// 覆盖所有计费路由：per_second/token_ratio 计费函数可能因定价未配置等原因失败，也需要兜底
		if task.Quota == 0 && shouldRunVideoFallbackBilling(ctx, task) {
			logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] fallback triggered: task=%s model=%s billingRoute=%s quota=0, attempting fallback billing",
				taskId, taskModelName, billingRoute))
			handleFallbackBilling(ctx, task, taskResult, taskModelName)
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
		username, _ := model.GetUsernameById(task.UserId, false)
		failLog := &model.Log{
			UserId:    task.UserId,
			Username:  username,
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

	// 终态（SUCCESS/FAILURE）使用 CAS 保护，防止多节点或多路径竞态导致重复计费/退款
	// 非终态（IN_PROGRESS/QUEUED 等）仍用 Update() 直接覆盖
	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && preStatus != task.Status {
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] before UpdateWithStatus: task=%s from=%s to=%s",
			taskId, preStatus, task.Status))
		won, err := task.UpdateWithStatus(preStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("[TaskPoll] UpdateWithStatus failed for task %s: %s", taskId, err.Error()))
			shouldRefund = false
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("[TaskPoll] task=%s already transitioned by another process (from=%s), skip billing/refund", taskId, preStatus))
			shouldRefund = false
			// 如果 handleVideoPerSecondBilling 已经设置了 quota=-1 锁但 CAS 失败，需要回滚锁
			// 但由于 billing lock 是独立的原子操作（检查 quota=0），CAS 失败意味着另一个进程已处理
			// 此处无需额外操作
		}
	} else if !isDone {
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] before task.Update(): task=%s status=%s",
			taskId, task.Status))
		if err := task.Update(); err != nil {
			common.SysLog("UpdateVideoTask task error: " + err.Error())
		}
	} else {
		// isDone && preStatus == task.Status → 状态未变，跳过更新
		logger.LogDebug(ctx, fmt.Sprintf("[TaskPoll] task=%s status unchanged (%s), skip update", taskId, task.Status))
	}

	if shouldRefund {
		// 任务失败且之前状态不是失败才退还额度，防止重复退还
		logger.LogInfo(ctx, fmt.Sprintf("[TaskPoll] refund: task=%s user=%d quota=%s",
			task.TaskID, task.UserId, logger.LogQuota(quota)))
		if err := model.IncreaseUserQuota(task.UserId, quota, false); err != nil {
			logger.LogWarn(ctx, "Failed to increase user quota: "+err.Error())
		} else {
			// 用户额度退还成功后同步退还令牌额度
			service.TaskAdjustTokenQuota(ctx, task, -quota)
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

// parseResolutionFromVeoUpstreamBody 从 Veo 系列（Vertex/Gemini）上游请求体中提取分辨率参数
// Vertex 格式：{"instances": [...], "parameters": {"resolution": "720p", ...}}
// Gemini 格式：{"instances": [...], "parameters": {"resolution": "720p", ...}}
func parseResolutionFromVeoUpstreamBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err != nil {
		return ""
	}
	// parameters.resolution（Vertex / Gemini 通用）
	if params, ok := m["parameters"].(map[string]interface{}); ok {
		if res, ok := params["resolution"].(string); ok && res != "" {
			return res
		}
	}
	return ""
}

// parseHasVideoInputFromUpstreamBody 从 upstream_request_body 的 content 数组中检测是否包含 video_url 类型的输入，
// 用于在 task.Data 计费字段丢失时兜底判断 has_video_input 维度。
// 返回 (hasVideoInput, found)，found=true 表示成功从 upstream_request_body 解析到了判断依据。
func parseHasVideoInputFromUpstreamBody(body []byte) (bool, bool) {
	if len(body) == 0 {
		return false, false
	}
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err != nil {
		return false, false
	}
	content, ok := m["content"].([]interface{})
	if !ok {
		return false, false
	}
	for _, item := range content {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemType, _ := itemMap["type"].(string); itemType == "video_url" {
			if videoURL, _ := itemMap["video_url"].(map[string]interface{}); videoURL != nil {
				if url, _ := videoURL["url"].(string); url != "" {
					return true, true
				}
			}
		}
	}
	// content 数组存在但没有 video_url 类型
	return false, true
}

// parseResolutionFromUpstreamBody 从 upstream_request_body 解析 resolution 字段
func parseResolutionFromUpstreamBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err != nil {
		return ""
	}
	if res, ok := m["resolution"].(string); ok && res != "" {
		return res
	}
	return ""
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
		"video_resolution",
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
	// 分辨率维度（720p/1080p/4K 等）
	if ratio_setting.HasVideoCompletionRatioResolution(name) {
		return true
	}
	return false
}

func isGeminiOmniVideoModel(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "gemini-omni-flash-preview")
}

func isGeminiOmniDataURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:video/")
}

// MergeVideoTaskDataWithUpstreamResponse 将上游响应合并进 task.Data，保留计费字段；供轮询与 GET 终态分支共用。
// 导出以便 relay 层在 in-progress 分支通过函数指针注入复用，与 SUCCESS 路径共享同一份 preservedFields 白名单，
// 避免 in-progress 合并时把 billing_*/generate_audio/has_video_input 等字段冲掉导致 SUCCESS 扣费链路读不到必备字段。
func MergeVideoTaskDataWithUpstreamResponse(task *model.Task, responseBody []byte) {
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
	MergeVideoTaskDataWithUpstreamResponse(task, responseBody)
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
		if strings.HasPrefix(strings.ToLower(taskModelName), "veo-") || isGeminiOmniVideoModel(taskModelName) {
			if taskResult.RemoteUrl != "" {
				task.FailReason = taskResult.RemoteUrl
			} else if taskResult.Url != "" && !isGeminiOmniDataURL(taskResult.Url) {
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
	// 只更新状态相关字段，绝不碰 quota —— quota 由 handleVideoPerSecondBilling / handleVideoTokenRatioBilling
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
		if err := handleVideoPerSecondBilling(ctx, task); err != nil {
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
	// GET 路径保底计费：正常路由都未识别且 quota 仍为 0
	if taskResult.Status == model.TaskStatusSuccess &&
		!isVideoPerSecondModel(taskModelName) && !isVideoTokenRatioModel(taskModelName) &&
		shouldRunVideoFallbackBilling(ctx, task) {
		logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] GET path fallback triggered: task=%s model=%s quota=0",
			task.TaskID, taskModelName))
		handleFallbackBilling(ctx, task, taskResult, taskModelName)
	}
	return nil
}

func shouldRunVideoFallbackBilling(ctx context.Context, task *model.Task) bool {
	if task == nil || task.ID == 0 {
		return false
	}
	if task.Quota != 0 {
		return false
	}
	var currentTaskData map[string]interface{}
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &currentTaskData); err == nil {
			if processed, ok := currentTaskData["billing_processed"].(bool); ok && processed {
				return false
			}
		}
	}
	var latest model.Task
	if err := model.DB.Select("id", "quota", "data").Where("id = ?", task.ID).First(&latest).Error; err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] fallback guard query failed: task=%s err=%v", task.TaskID, err))
		return false
	}
	if latest.Quota != 0 {
		task.Quota = latest.Quota
		return false
	}
	var taskData map[string]interface{}
	if len(latest.Data) > 0 {
		if err := common.Unmarshal(latest.Data, &taskData); err == nil {
			if processed, ok := taskData["billing_processed"].(bool); ok && processed {
				task.Data = latest.Data
				return false
			}
		}
	}
	return true
}

// handleVideoPerSecondBilling 处理按秒计费视频模型的轮询成功计费逻辑（Veo / Sora-2 / kling-v3 / wan2.6 等所有按秒价模型）
// 计费公式: actualQuota = videoPrice × requestedSeconds × QuotaPerUnit × groupRatio
func handleVideoPerSecondBilling(ctx context.Context, task *model.Task) error {
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
			return fmt.Errorf("handleVideoPerSecondBilling: unmarshal task.Data failed: %w", err)
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
		return fmt.Errorf("handleVideoPerSecondBilling: failed to claim billing lock: %w", claimResult.Error)
	}
	if claimResult.RowsAffected == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s already billed (DB guard), skip", modelName, task.TaskID))
		return nil
	}
	// 已获得计费锁（quota=-1），若后续计费失败则需释放锁（回滚为 0），允许 fallback 重试
	billingLockClaimed := true
	defer func() {
		if billingLockClaimed {
			// 计费成功前发生错误，释放 DB 级锁
			model.DB.Model(&model.Task{}).Where("id = ? AND quota = -1", task.ID).Update("quota", 0)
			logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s billing lock released due to error", modelName, task.TaskID))
		}
	}()

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
	// 获取官方单秒价格（wan2.6-flash / Veo 3.1 含分辨率分档；其它为 noAudio/audio 或单一数字）
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
	// Veo 系列：Ali 解析器返回默认 720p 时，补充尝试从 parameters.resolution 和 task.Data 中提取分辨率
	if resKey == "" || resKey == "720p" {
		if veoRes := parseResolutionFromVeoUpstreamBody(task.UpstreamRequestBody); veoRes != "" {
			parsed := ratio_setting.NormalizeVideoResolutionKey(veoRes)
			if parsed != "720p" || resKey == "" {
				resKey = parsed
				logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s billing_resolution from veo upstream body -> %s", task.TaskID, resKey))
			}
		}
	}
	if resKey == "" || resKey == "720p" {
		if v, ok := taskData["video_resolution"].(string); ok && v != "" {
			parsed := ratio_setting.NormalizeVideoResolutionKey(v)
			if parsed != "720p" || resKey == "" {
				resKey = parsed
				logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s billing_resolution from task.Data -> %s", task.TaskID, resKey))
			}
		}
	}
	officialVideoPrice, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecondForBillingWithResolution(modelName, generateAudio, resKey)
	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] task=%s billing_resolution=%s", task.TaskID, resKey))
	if !hasVideoPrice || officialVideoPrice <= 0 {
		return fmt.Errorf("handleVideoPerSecondBilling: video price per second not configured for model: %s", modelName)
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
		if err := model.DecreaseUserQuota(task.UserId, actualQuota, true); err != nil {
			return fmt.Errorf("handleVideoPerSecondBilling: DecreaseUserQuota failed: %w", err)
		}
		// 同步扣除令牌额度
		service.TaskAdjustTokenQuota(ctx, task, actualQuota)
	}
	// 扣费成功（或 actualQuota==0 无需扣费），标记计费锁已消费，defer 不再回滚
	billingLockClaimed = false

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
		"generate_audio":                  generateAudio,
		"video_resolution":                resKey,
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
	} else if model.IsQuotaDataStreamEnabled() {
		model.QueueConsumeLogToQuotaStream(consumeLog, "video_billing_per_second")
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
	if !model.IsQuotaDataStreamEnabled() {
		// 旧的 request_id 回填逻辑暂时保留，作为 Redis Stream 关闭时的兼容回退路径。
		if err := model.SyncQuotaDataFromConsumeLogsByRequestId(task.TaskID); err != nil {
			logger.LogError(ctx, fmt.Sprintf("[VideoBilling] model=%s task=%s failed to sync quota data: %v", modelName, task.TaskID, err))
		}
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
	// 已获得计费锁（quota=-1），若后续计费失败则需释放锁（回滚为 0），允许 fallback 重试
	billingLockClaimed := true
	defer func() {
		if billingLockClaimed {
			model.DB.Model(&model.Task{}).Where("id = ? AND quota = -1", task.ID).Update("quota", 0)
			logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s billing lock released due to error", modelName, task.TaskID))
		}
	}()

	if taskResult == nil {
		return fmt.Errorf("handleVideoTokenRatioBilling: taskResult is nil")
	}

	// tokens：优先 completion_tokens，其次 total_tokens
	tokens := 0
	if isGeminiOmniVideoModel(modelName) {
		tokens = taskResult.VideoOutputTokens
	} else if taskResult.CompletionTokens > 0 {
		tokens = taskResult.CompletionTokens
	} else if taskResult.TotalTokens > 0 {
		tokens = taskResult.TotalTokens
	}
	if tokens <= 0 {
		if isGeminiOmniVideoModel(modelName) {
			// Preview responses may omit usage. Keep billing deterministic with
			// Google's documented 5,792 video tokens per second fallback.
			seconds := task.Properties.RequestedSeconds
			if seconds <= 0 {
				if value, ok := taskData["requested_seconds"].(float64); ok {
					seconds = int(value)
				}
			}
			if seconds <= 0 {
				seconds = 4
			}
			tokens = seconds * 5792
			taskResult.VideoOutputTokens = tokens
			logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] Omni usage missing, fallback to %d video tokens (%ds)", tokens, seconds))
		}
	}
	if tokens <= 0 {
		tokens = extractVideoUsageTokensFromTaskData(taskData)
		if tokens > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio task=%s recovered tokens=%d from task.Data usage", task.TaskID, tokens))
		}
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
	// 判断 has_video_input：优先从 task.Data 读，再从 upstream_request_body 解析兜底
	// （task.Data 中的计费字段可能在中间轮询更新时丢失，upstream_request_body 不会被覆盖）
	var cfgVal float64
	var ok bool
	hasVideoInputResolved := false
	hasVideoInputVal := false
	if v, exists := taskData["has_video_input"].(bool); exists {
		hasVideoInputResolved = true
		hasVideoInputVal = v
	} else if v, found := parseHasVideoInputFromUpstreamBody(task.UpstreamRequestBody); found {
		hasVideoInputResolved = true
		hasVideoInputVal = v
		logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio task=%s has_video_input resolved from upstream_request_body: %v",
			task.TaskID, v))
	}

	// 解析分辨率：优先上游返回 > task.Data > upstream_request_body > 默认 720p
	videoResolution := ""
	if taskResult != nil && taskResult.Resolution != "" {
		videoResolution = taskResult.Resolution
	}
	if videoResolution == "" {
		if v, vOk := taskData["video_resolution"].(string); vOk && v != "" {
			videoResolution = v
		}
	}
	if videoResolution == "" {
		videoResolution = parseResolutionFromUpstreamBody(task.UpstreamRequestBody)
	}
	if videoResolution == "" {
		videoResolution = "720p"
	}

	cfgVal, ok, billingDimension := ratio_setting.ResolveVideoCompletionRatioForBilling(
		modelName,
		hasVideoInputResolved,
		hasVideoInputVal,
		videoResolution,
		generateAudio,
	)
	logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] token_ratio task=%s using %s dimension: has_video_input_resolved=%v has_video_input=%v resolution=%s generate_audio=%v cfgVal=%.6f ok=%v",
		task.TaskID, billingDimension, hasVideoInputResolved, hasVideoInputVal, videoResolution, generateAudio, cfgVal, ok))
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
	// 记录视频输入维度（如果解析到了）
	if hasVideoInputResolved {
		otherMap["has_video_input"] = hasVideoInputVal
	}
	otherMap["video_resolution"] = videoResolution

	if ratioMode {
		// 走倍率体系：与 ModelRatio 计费一致，quota = tokens * ratio * groupRatio
		actualQuotaFloat := float64(tokens) * cfgVal * groupRatio
		actualQuota = int(actualQuotaFloat)
		otherMap["effective_video_ratio"] = cfgVal
	} else if isGeminiOmniVideoModel(modelName) {
		// Omni uses one input price and separate text/video output prices.
		// Do not collapse all modalities into the video price: the Interaction
		// usage object reports them independently.
		inputTokens := taskResult.InputTokens
		videoTokens := taskResult.VideoOutputTokens
		if videoTokens <= 0 {
			videoTokens = tokens
		}
		textTokens := taskResult.TextOutputTokens
		actualCost := float64(inputTokens)*1.5 + float64(textTokens)*9.0 + float64(videoTokens)*17.5
		actualQuota = int(actualCost / 1000000.0 * common.QuotaPerUnit * groupRatio)
		otherMap["input_tokens"] = inputTokens
		otherMap["text_output_tokens"] = textTokens
		otherMap["video_output_tokens"] = videoTokens
		otherMap["input_price_per_million_tokens"] = 1.5
		otherMap["text_output_price_per_million_tokens"] = 9.0
		otherMap["video_output_price_per_million_tokens"] = 17.5
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
		if err := model.DecreaseUserQuota(task.UserId, actualQuota, true); err != nil {
			return fmt.Errorf("handleVideoTokenRatioBilling: DecreaseUserQuota failed: %w", err)
		}
		// 同步扣除令牌额度
		service.TaskAdjustTokenQuota(ctx, task, actualQuota)
	}
	// 扣费成功（或 actualQuota==0 无需扣费），标记计费锁已消费，defer 不再回滚
	billingLockClaimed = false

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
	} else if model.IsQuotaDataStreamEnabled() {
		model.QueueConsumeLogToQuotaStream(consumeLog, "video_billing_token_ratio")
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
	if !model.IsQuotaDataStreamEnabled() {
		// 旧的 request_id 回填逻辑暂时保留，作为 Redis Stream 关闭时的兼容回退路径。
		if err := model.SyncQuotaDataFromConsumeLogsByRequestId(task.TaskID); err != nil {
			logger.LogError(ctx, fmt.Sprintf("[VideoBilling] token_ratio model=%s task=%s failed to sync quota data: %v", modelName, task.TaskID, err))
		}
	}
	return nil
}

func extractVideoUsageTokensFromTaskData(taskData map[string]interface{}) int {
	tokenFromValue := func(value interface{}) int {
		switch v := value.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case json.Number:
			n, _ := v.Int64()
			return int(n)
		}
		return 0
	}
	var extractFromUsage func(map[string]interface{}) int
	extractFromUsage = func(data map[string]interface{}) int {
		if data == nil {
			return 0
		}
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			if tokens := tokenFromValue(usage["completion_tokens"]); tokens > 0 {
				return tokens
			}
			if tokens := tokenFromValue(usage["total_tokens"]); tokens > 0 {
				return tokens
			}
		}
		if metadata, ok := data["metadata"].(map[string]interface{}); ok {
			if tokens := extractFromUsage(metadata); tokens > 0 {
				return tokens
			}
		}
		return 0
	}
	return extractFromUsage(taskData)
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

// handleFallbackBilling 保底计费：当正常计费路由未能识别模型（billingRoute=pre_deduction_settle 且 quota=0）时，
// 尝试从 upstream_request_body 解析参数并调用按秒/按量计费函数，同时记录系统日志。
// 即使保底计费也失败，也会记录系统日志到 logs 表以便管理员排查。
func handleFallbackBilling(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo, taskModelName string) {
	username, _ := model.GetUsernameById(task.UserId, false)

	// 尝试1：检查按秒计费配置是否存在，存在则调用按秒计费
	// 先检查配置再调用，避免 DB 级计费锁被错误占用
	if _, hasPerSecondPrice := ratio_setting.GetVideoModelPricePerSecond(taskModelName); hasPerSecondPrice {
		if err := handleVideoPerSecondBilling(ctx, task); err == nil {
			// err==nil 表示计费流程正常完成（即使 quota=0 也表示"零费用"而非"计费失败"），不再尝试第二条路径
			if task.Quota > 0 {
				logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] fallback per_second success: task=%s model=%s quota=%d",
					task.TaskID, taskModelName, task.Quota))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] fallback per_second completed with zero quota: task=%s model=%s",
					task.TaskID, taskModelName))
			}
			sysLog := &model.Log{
				UserId:    task.UserId,
				Username:  username,
				CreatedAt: common.GetTimestamp(),
				Type:      model.LogTypeSystem,
				Content: fmt.Sprintf("[保底计费] 视频任务 %s 正常计费路由未识别模型 %s，保底按秒计费成功，扣费 %s",
					task.TaskID, taskModelName, logger.LogQuota(task.Quota)),
				ChannelId: task.ChannelId,
				ModelName: taskModelName,
				TokenName: task.TokenName,
				TokenId:   task.TokenId,
				Group:     task.Group,
				RequestId: task.TaskID,
				Other: common.MapToJsonStr(map[string]interface{}{
					"fallback_type":   "per_second",
					"fallback_reason": "billing_route_miss",
					"fallback_quota":  task.Quota,
				}),
			}
			model.CreateLog(sysLog)
			return
		} else if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] fallback per_second failed: task=%s model=%s err=%v", task.TaskID, taskModelName, err))
		}
	}

	// 尝试2：检查按量计费配置是否存在（VideoCompletionRatio），存在则调用按量计费
	hasTokenRatioConfig := false
	if _, ok := ratio_setting.GetVideoRatio(taskModelName); ok {
		hasTokenRatioConfig = true
	} else if _, ok := ratio_setting.GetVideoCompletionRatioPricing(taskModelName, true); ok {
		hasTokenRatioConfig = true
	} else if _, ok := ratio_setting.GetVideoCompletionRatioPricing(taskModelName, false); ok {
		hasTokenRatioConfig = true
	} else if _, ok := ratio_setting.GetVideoCompletionRatioVideoPricing(taskModelName, true); ok {
		hasTokenRatioConfig = true
	} else if _, ok := ratio_setting.GetVideoCompletionRatioVideoPricing(taskModelName, false); ok {
		hasTokenRatioConfig = true
	} else if ratio_setting.HasVideoCompletionRatioResolution(taskModelName) {
		hasTokenRatioConfig = true
	}
	if hasTokenRatioConfig {
		if err := handleVideoTokenRatioBilling(ctx, task, taskResult); err == nil {
			if task.Quota > 0 {
				logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] fallback token_ratio success: task=%s model=%s quota=%d",
					task.TaskID, taskModelName, task.Quota))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("[VideoBilling] fallback token_ratio completed with zero quota: task=%s model=%s",
					task.TaskID, taskModelName))
			}
			sysLog := &model.Log{
				UserId:    task.UserId,
				Username:  username,
				CreatedAt: common.GetTimestamp(),
				Type:      model.LogTypeSystem,
				Content: fmt.Sprintf("[保底计费] 视频任务 %s 正常计费路由未识别模型 %s，保底按量计费成功，扣费 %s",
					task.TaskID, taskModelName, logger.LogQuota(task.Quota)),
				ChannelId: task.ChannelId,
				ModelName: taskModelName,
				TokenName: task.TokenName,
				TokenId:   task.TokenId,
				Group:     task.Group,
				RequestId: task.TaskID,
				Other: common.MapToJsonStr(map[string]interface{}{
					"fallback_type":   "token_ratio",
					"fallback_reason": "billing_route_miss",
					"fallback_quota":  task.Quota,
				}),
			}
			model.CreateLog(sysLog)
			return
		} else if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("[VideoBilling] fallback token_ratio failed: task=%s model=%s err=%v", task.TaskID, taskModelName, err))
		}
	}

	// 两种保底计费都失败，记录系统警告日志
	logger.LogError(ctx, fmt.Sprintf("[VideoBilling] fallback billing FAILED: task=%s model=%s, no pricing config found",
		task.TaskID, taskModelName))
	sysLog := &model.Log{
		UserId:    task.UserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      model.LogTypeSystem,
		Content: fmt.Sprintf("[保底计费失败] 视频任务 %s 模型 %s 无法计费：正常路由和保底路由均未找到定价配置，任务成功但未扣费",
			task.TaskID, taskModelName),
		ChannelId: task.ChannelId,
		ModelName: taskModelName,
		TokenName: task.TokenName,
		TokenId:   task.TokenId,
		Group:     task.Group,
		RequestId: task.TaskID,
		Other: common.MapToJsonStr(map[string]interface{}{
			"fallback_type":   "none",
			"fallback_reason": "no_pricing_config",
			"warning":         "task_completed_without_billing",
		}),
	}
	model.CreateLog(sysLog)
}
