package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// BatchTaskResult is the normalized result returned by plugin batch polling.
type BatchTaskResult struct {
	TaskInfo   relaycommon.TaskInfo
	Action     string
	SubmitTime int64
	StartTime  int64
	FinishTime int64
	Data       any
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// UpdateVideoTasksFn 由 main 包注入，用于替换默认的视频任务轮询处理。
// 当设置后，DispatchPlatformUpdate 将使用此函数代替内置的 UpdateVideoTasks，
// 以支持 controller/task_video.go 中更完整的按秒/按量计费引擎。
var UpdateVideoTasksFn func(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		if isLocallyDispatchedLyriaTask(task) {
			continue
		}
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollSummary is the result recorded on an async_task_poll system task row,
// summarizing one polling pass.
type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

// RunTaskPollingOnce performs one async-task (Suno/video) polling pass
// synchronously. It honors ctx cancellation (the system-task runner cancels it
// when the lease is lost) and, when report is non-nil, reports progress as
// (processedPlatforms, totalPlatforms). It returns immediately if the task
// adaptor factory has not been wired yet, to avoid a nil call during startup.
func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if ctx == nil {
		ctx = context.Background()
	}
	recoverPendingInteractionsTaskSettlements(ctx, 100)
	deliverPendingTaskBillingLogs(ctx, 100)
	if GetTaskAdaptorFunc == nil {
		return summary
	}

	common.SysLog("任务进度轮询开始")
	sweepTimedOutTasks(ctx)
	allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	// Native Lyria background tasks are completed by their dedicated worker.
	// They use a local public task id, which must never be sent to the generic
	// provider polling endpoint as if it were an upstream Interaction id.
	filteredTasks := allTasks[:0]
	for _, task := range allTasks {
		if !isLocallyDispatchedLyriaTask(task) {
			filteredTasks = append(filteredTasks, task)
		}
	}
	allTasks = filteredTasks
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, t := range allTasks {
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	totalPlatforms := len(platformTask)
	processedPlatforms := 0
	for platform, tasks := range platformTask {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Task)
		nullTaskIds := make([]int64, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				// 统计失败的未完成任务
				nullTaskIds = append(nullTaskIds, task.ID)
				continue
			}
			taskM[upstreamID] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		if len(nullTaskIds) > 0 {
			summary.NullTasksFailed += len(nullTaskIds)
			err := model.TaskBulkUpdateByID(nullTaskIds, map[string]any{
				"status":   "FAILURE",
				"progress": "100%",
			})
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null task_id task error: %v", err))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("Fix null task_id task success: %v", nullTaskIds))
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	common.SysLog("任务进度轮询完成")
	return summary
}

type taskBillingOutboxPayload struct {
	SkipLog          bool                   `json:"skip_log"`
	UserID           int                    `json:"user_id"`
	LogType          int                    `json:"log_type"`
	Content          string                 `json:"content"`
	ChannelID        int                    `json:"channel_id"`
	ModelName        string                 `json:"model_name"`
	Quota            int                    `json:"quota"`
	TokenID          int                    `json:"token_id"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
	NodeName         string                 `json:"node_name"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	UseTime          int                    `json:"use_time"`
}

func deliverPendingTaskBillingLogs(ctx context.Context, limit int) {
	rows, err := model.GetPendingTaskBillingLogOutbox(limit)
	if err != nil {
		logger.LogError(ctx, "failed to query task billing log outbox: "+err.Error())
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		claimed, err := model.ClaimTaskBillingLogOutbox(row.ID)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("failed to claim task billing log outbox %s: %v", row.OperationID, err))
			continue
		}
		if !claimed {
			continue
		}
		var payload taskBillingOutboxPayload
		if err := common.Unmarshal([]byte(row.Payload), &payload); err != nil {
			_ = model.MarkTaskBillingLogOutboxFailed(row.ID, err)
			logger.LogError(ctx, fmt.Sprintf("invalid task billing log outbox payload %s: %v", row.OperationID, err))
			continue
		}
		if payload.SkipLog {
			if err := model.MarkTaskBillingLogOutboxDeliveredByID(row.ID); err != nil {
				logger.LogError(ctx, fmt.Sprintf("task billing outbox %s skip marker failed: %v", row.OperationID, err))
			}
			continue
		}
		err = model.RecordTaskBillingLogWithError(model.RecordTaskBillingLogParams{
			RequestId: row.OperationID, UserId: payload.UserID, LogType: payload.LogType,
			Content: payload.Content, ChannelId: payload.ChannelID, ModelName: payload.ModelName,
			Quota: payload.Quota, TokenId: payload.TokenID, Group: payload.Group, Other: payload.Other,
			NodeName: payload.NodeName, PromptTokens: payload.PromptTokens,
			CompletionTokens: payload.CompletionTokens, UseTime: payload.UseTime,
		})
		if err != nil {
			_ = model.MarkTaskBillingLogOutboxFailed(row.ID, err)
			logger.LogError(ctx, fmt.Sprintf("failed to deliver task billing log outbox %s: %v", row.OperationID, err))
			continue
		}
		if err := model.MarkTaskBillingLogOutboxDeliveredByID(row.ID); err != nil {
			logger.LogError(ctx, fmt.Sprintf("task billing log outbox %s was written but delivery marker failed: %v", row.OperationID, err))
		}
	}
}

func recoverPendingInteractionsTaskSettlements(ctx context.Context, limit int) {
	cacheRows, cacheErr := model.GetPendingTaskBillingCacheSync(limit)
	if cacheErr != nil {
		logger.LogError(ctx, "failed to query pending task billing cache sync: "+cacheErr.Error())
	} else {
		for _, row := range cacheRows {
			if err := model.SyncTaskBillingSettlementCache(row.TaskID); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("failed to recover task settlement cache sync %s: %v", row.TaskID, err))
			}
		}
	}
	settlements, err := model.GetPendingTaskBillingSettlements(limit)
	if err != nil {
		logger.LogError(ctx, "failed to query pending Interactions task settlements: "+err.Error())
		return
	}
	for _, settlement := range settlements {
		if ctx.Err() != nil {
			return
		}
		task, err := model.GetTaskByTaskID(settlement.TaskID)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("failed to load task %s for settlement recovery: %v", settlement.TaskID, err))
			continue
		}
		if task.Status != model.TaskStatusSuccess || task.PrivateData.BillingContext == nil ||
			!task.PrivateData.BillingContext.NativeInteractions || task.PrivateData.BillingContext.TieredSnapshot == nil {
			continue
		}
		if GetTaskAdaptorFunc == nil || len(task.Data) == 0 {
			logger.LogWarn(ctx, fmt.Sprintf("pending Interactions settlement %s cannot recover without stored result data", settlement.TaskID))
			continue
		}
		adaptor := GetTaskAdaptorFunc(task.Platform)
		if adaptor == nil {
			logger.LogWarn(ctx, fmt.Sprintf("pending Interactions settlement %s has no task adaptor", settlement.TaskID))
			continue
		}
		taskResult, err := adaptor.ParseTaskResult(task.Data)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("failed to parse stored result for Interactions settlement %s: %v", settlement.TaskID, err))
			continue
		}
		if !settleNativeInteractionsTieredTask(ctx, task, taskResult) {
			logger.LogWarn(ctx, fmt.Sprintf("pending Interactions settlement %s was not committed; it will be retried", settlement.TaskID))
		}
	}
}

func isLocallyDispatchedLyriaTask(task *model.Task) bool {
	return task != nil && task.Platform == constant.TaskPlatformLyria && task.PrivateData.LocalAsync
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		updateFn := UpdateVideoTasksFn
		if updateFn == nil {
			updateFn = UpdateVideoTasks
		}
		if err := updateFn(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		err = model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("UpdateSunoTask error: %v", err))
		}
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	proxy := ch.GetSetting().Proxy
	resp, err := adaptor.FetchTask(*ch.BaseURL, ch.Key, map[string]any{
		"ids": taskIds,
	}, proxy)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems taskdto.TaskResponse[[]taskdto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		common.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 成功获取到任务数: %s", channelId, len(taskIds), string(responseBody)))
		return err
	}

	for _, responseItem := range responseItems.Data {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[responseItem.TaskID]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task response ignored: unknown task_id=%s", responseItem.TaskID))
			continue
		}
		if !taskNeedsUpdate(task, responseItem) {
			continue
		}

		task.Status = lo.If(model.TaskStatus(responseItem.Status) != "", model.TaskStatus(responseItem.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		if responseItem.FailReason != "" || task.Status == model.TaskStatusFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Progress = "100%"
			RefundTaskQuota(ctx, task, task.FailReason)
		}
		if responseItem.Status == model.TaskStatusSuccess {
			task.Progress = "100%"
		}
		task.Data = responseItem.Data

		err = task.Update()
		if err != nil {
			common.SysLog("UpdateSunoTask task error: " + err.Error())
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask taskdto.SunoDataResponse) bool {
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	oldData, _ := common.Marshal(oldTask.Data)
	newData, _ := common.Marshal(newTask.Data)

	sort.Slice(oldData, func(i, j int) bool {
		return oldData[i] < oldData[j]
	})
	sort.Slice(newData, func(i, j int) bool {
		return newData[i] < newData[j]
	})

	if string(oldData) != string(newData) {
		return true
	}
	return false
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	var wg sync.WaitGroup
	for _, channelId := range channelIDs {
		taskIds := taskChannelM[channelId]
		if len(taskIds) == 0 {
			continue
		}
		taskIds = append([]string(nil), taskIds...)

		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		errUpdate := model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	pollingChannelType := 0
	if constant.IsInteractionsTaskPlatform(platform) {
		pollingChannelType = cacheGetChannel.Type
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelType:    pollingChannelType,
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	disablePollingSleep := cacheGetChannel.GetOtherSettings().DisableTaskPollingSleep
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}

		// sleep 1 second between tasks for this channel only.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		logger.LogError(ctx, fmt.Sprintf("updateVideoSingleTask response: %s", responseBody))
	} else {
		logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)
	}

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems taskdto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}

	task.Data = redactVideoResponseBody(responseBody)

	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	now := time.Now().Unix()
	if taskResult.Status == "" {
		//taskResult = relaycommon.FailTaskInfo("upstream returned empty status")
		errorResult := &dto.GeneralErrorResponse{}
		if err = common.Unmarshal(responseBody, &errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil {
				// 返回规范的 OpenAI 错误格式，提取错误信息，判断错误是否为任务失败
				if openaiError.Code == "429" {
					// 429 错误通常表示请求过多或速率限制，暂时不认为是任务失败，保持原状态等待下一轮轮询
					return nil
				}

				// 其他错误认为是任务失败，记录错误信息并更新任务状态
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else {
				// unknown error format, log original response
				logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status with unrecognized error format, response: %s", taskId, string(responseBody)))
				taskResult = relaycommon.FailTaskInfo("upstream returned unrecognized message")
			}
		}
	}

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
		shouldSettle = true
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
		if quota != 0 {
			shouldRefund = true
		}
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	if shouldSettle {
		settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}

	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
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

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 返回 true 表示计费已在此定稿（结算或明确保留预扣额度），调用方不得再全额退款；
// 返回 false 表示未结算，失败任务由调用方 RefundTaskQuota 全额退回。
// 优先级：1. 用量表达式（TieredSnapshot）结算
//
//  2. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//  3. taskResult 携带 token → 按 token 重算
//  4. 都不满足 → 保持预扣额度不变，返回 false
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) bool {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.TieredSnapshot != nil {
		if bc.NativeInteractions && model.HasTaskBillingSettlement(task.TaskID) {
			return settleNativeInteractionsTieredTask(ctx, task, taskResult)
		}
		// 用量表达式结算只适用于成功任务；失败任务由调用方全额退款。
		if task.Status == model.TaskStatusFailure {
			return false
		}
		if taskResult == nil {
			if bc.NativeInteractions {
				bc.TieredUsageMissing = true
				bc.TieredUsageSource = "configured"
			}
			logger.LogWarn(ctx, fmt.Sprintf("任务 %s 阶梯计费缺少完成用量，保留预扣额度", task.TaskID))
			return false
		}
		usageFacts := make(map[string]any, len(bc.TieredSnapshot.UsageFacts)+len(taskResult.UsageFacts))
		for key, value := range bc.TieredSnapshot.UsageFacts {
			usageFacts[key] = value
		}
		for key, value := range taskResult.UsageFacts {
			usageFacts[key] = value
		}
		if bc.NativeInteractions && bc.TieredSnapshot.TaskUsageBilling && !HasTieredTaskUsageFacts(bc.TieredSnapshot.ExprString, usageFacts) {
			bc.TieredUsageMissing = true
			bc.TieredUsageSource = "configured"
			logger.LogWarn(ctx, fmt.Sprintf("任务 %s 阶梯计费缺少表达式所需 usage facts，保留预扣额度", task.TaskID))
			return false
		}
		requestInput := billingexpr.RequestInput{Usage: usageFacts}
		if bc.TieredRequestInput != nil {
			requestInput.Body = append([]byte(nil), bc.TieredRequestInput.Body...)
			requestInput.Headers = bc.TieredRequestInput.Headers
		}
		var result billingexpr.TieredResult
		var err error
		if !bc.NativeInteractions {
			// Preserve the historical task settlement contract for non-Interactions
			// task platforms. Their snapshots have always been usage-fact based.
			result, err = billingexpr.ComputeTieredQuotaWithRequest(bc.TieredSnapshot, billingexpr.TokenParams{}, requestInput)
		} else if bc.TieredSnapshot.TaskUsageBilling {
			result, err = billingexpr.ComputeTieredQuotaWithRequest(bc.TieredSnapshot, billingexpr.TokenParams{}, requestInput)
		} else if bc.NativeInteractions && !taskResult.UsageAvailable {
			bc.TieredUsageMissing = true
			bc.TieredUsageSource = "configured"
			logger.LogWarn(ctx, fmt.Sprintf("任务 %s token 阶梯计费缺少上游 usage，保留预扣额度", task.TaskID))
			return false
		} else {
			params := BuildTieredTokenParamsForTask(taskResult, bc.OriginModelName, bc.TieredSnapshot.ExprString)
			result, err = billingexpr.ComputeTieredQuotaWithRequest(bc.TieredSnapshot, params, requestInput)
		}
		if err != nil {
			if bc.NativeInteractions {
				bc.TieredUsageMissing = true
				bc.TieredUsageSource = "configured"
			}
			logger.LogWarn(ctx, fmt.Sprintf("任务 %s 表达式结算失败，保留预扣额度: %v", task.TaskID, err))
			return true
		}
		bc.TieredUsageMissing = false
		if bc.NativeInteractions {
			bc.TieredUsageSource = "upstream"
		}
		if result.Clamp != nil {
			logger.LogWarn(ctx, fmt.Sprintf("任务 %s 表达式结算额度发生饱和: %+v", task.TaskID, result.Clamp))
		}
		bc.TieredSnapshot.UsageFacts = usageFacts
		bc.TieredSnapshot.EstimatedTier = result.MatchedTier
		RecalculateTaskQuota(ctx, task, result.ActualQuotaAfterGroup, "任务用量表达式结算", result.Clamp)
		return true
	}
	// 按次计费的成功任务保持预扣；失败任务由调用方全额退款。
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return false
	}
	// 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
		return true
	}
	// 回退到 token 重算
	if taskResult.TotalTokens > 0 {
		enrichTaskBillingDataForTokenSettlement(task, taskResult)
		if actualQuota, ok, clamp := calculateGeminiOmniQuota(task, taskResult); ok {
			RecalculateTaskQuota(ctx, task, actualQuota, "Gemini Omni token重算", clamp)
			return true
		}
		RecalculateTaskQuotaByTokens(ctx, task, taskResult.TotalTokens)
		return true
	}
	// 无调整，保持预扣额度；失败任务交由调用方退款
	return false
}

func settleNativeInteractionsTieredTask(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo) bool {
	bc := task.PrivateData.BillingContext
	if task.Status != model.TaskStatusSuccess {
		return false
	}
	var result billingexpr.TieredResult
	var settleErr error
	usageFacts := make(map[string]any, len(bc.TieredSnapshot.UsageFacts))
	for key, value := range bc.TieredSnapshot.UsageFacts {
		usageFacts[key] = value
	}
	usageAvailable := taskResult != nil && taskResult.UsageAvailable
	if taskResult != nil {
		for key, value := range taskResult.UsageFacts {
			usageFacts[key] = value
		}
	}
	requestInput := billingexpr.RequestInput{Usage: usageFacts}
	if bc.TieredRequestInput != nil {
		requestInput.Body = append([]byte(nil), bc.TieredRequestInput.Body...)
		requestInput.Headers = bc.TieredRequestInput.Headers
	}
	missingReason := ""
	missingKeys := ""
	if taskResult == nil {
		missingReason = "completion task result was unavailable"
	} else if len(billingexpr.UsedUsageKeys(bc.TieredSnapshot.ExprString)) > 0 && !HasTieredTaskUsageFacts(bc.TieredSnapshot.ExprString, usageFacts) {
		missingReason = "required expression usage facts were unavailable"
		keys := make([]string, 0)
		for key := range billingexpr.UsedUsageKeys(bc.TieredSnapshot.ExprString) {
			if _, exists := usageFacts[key]; !exists {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		missingKeys = strings.Join(keys, ",")
	} else if !usageAvailable && (billingexpr.UsedVars(bc.TieredSnapshot.ExprString)["p"] || billingexpr.UsedVars(bc.TieredSnapshot.ExprString)["c"]) {
		missingReason = "upstream token usage was unavailable"
		missingKeys = "input_tokens,output_tokens"
	} else {
		params := BuildTieredTokenParamsForTask(taskResult, bc.OriginModelName, bc.TieredSnapshot.ExprString)
		result, settleErr = billingexpr.ComputeTieredQuotaWithRequest(bc.TieredSnapshot, params, requestInput)
		if settleErr != nil {
			missingReason = "tiered expression could not be evaluated: " + settleErr.Error()
		}
	}

	status := model.TaskBillingSettlementSettled
	actualQuota := task.Quota
	if missingReason == "" {
		actualQuota = result.ActualQuotaAfterGroup
		bc.TieredUsageMissing = false
		bc.TieredUsageSource = "upstream"
		bc.TieredSnapshot.UsageFacts = usageFacts
		bc.TieredSnapshot.EstimatedTier = result.MatchedTier
	} else {
		status = model.TaskBillingSettlementUsageUnavailable
		bc.TieredUsageMissing = true
		bc.TieredUsageSource = "configured"
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = task.Quota
	other["actual_quota"] = actualQuota
	promptTokens := 0
	completionTokens := 0
	if taskResult != nil {
		promptTokens = taskResult.InputTokens
		completionTokens = taskResult.CompletionTokens
		imageOutputTokens := taskResult.ImageOutputTokens
		textOutputTokens := min(max(taskResult.TextOutputTokens, 0), max(taskResult.CompletionTokens, 0))
		if !bc.NativeInteractions && isNativeInteractionImageModel(taskModelName(task)) && taskResult.CompletionTokens > 0 && imageOutputTokens == 0 {
			imageOutputTokens = max(taskResult.CompletionTokens-textOutputTokens-max(taskResult.ThoughtTokens, 0), 0)
		}
		other["input_tokens"] = taskResult.InputTokens
		other["input_text_tokens"] = taskResult.TextInputTokens
		other["input_image_tokens"] = taskResult.ImageInputTokens
		other["video_input_tokens"] = taskResult.VideoInputTokens
		other["cache_tokens"] = taskResult.CachedTokens
		other["audio_input"] = taskResult.AudioInputTokens
		other["audio_output"] = taskResult.AudioOutputTokens
		other["text_output_tokens"] = textOutputTokens
		other["image_output_tokens"] = imageOutputTokens
		other["video_output_tokens"] = taskResult.VideoOutputTokens
		other["reasoning_tokens"] = taskResult.ThoughtTokens
	}
	if missingReason != "" {
		adminInfo, _ := other["admin_info"].(map[string]interface{})
		if adminInfo == nil {
			adminInfo = make(map[string]interface{})
			other["admin_info"] = adminInfo
		}
		adminInfo["tiered_usage_unavailable"] = true
		adminInfo["usage_missing_reason"] = missingReason
		adminInfo["missing_usage_keys"] = missingKeys
	}
	delta := actualQuota - task.Quota
	logType, logQuota := model.LogTypeConsume, delta
	if delta < 0 {
		logType, logQuota = model.LogTypeRefund, -delta
	}
	if delta == 0 {
		logType, logQuota = 0, 0
	}
	outboxPayload, err := model.EncodeTaskBillingPayload(map[string]any{
		"skip_log": logType == 0,
		"user_id":  task.UserId, "log_type": logType, "content": "Interactions tiered usage settlement",
		"channel_id": task.ChannelId, "model_name": taskModelName(task), "quota": logQuota,
		"token_id": task.PrivateData.TokenId, "group": task.Group, "other": other.Snapshot(),
		"node_name":     task.PrivateData.NodeName,
		"prompt_tokens": promptTokens, "completion_tokens": completionTokens,
	})
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Interactions task %s billing outbox encode failed: %v", task.TaskID, err))
		return false
	}
	privateData, err := common.Marshal(task.PrivateData)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Interactions task %s billing context encode failed: %v", task.TaskID, err))
		return false
	}
	committed, err := model.CommitTaskBillingSettlement(model.TaskBillingSettlementCommit{
		TaskID: task.TaskID, ActualQuota: actualQuota, Status: status,
		Outcome: missingReason, UsageMissingReason: missingReason, MissingUsageKeys: missingKeys,
		OutboxOperationID: "interactions-settlement:" + task.TaskID + ":" + string(status),
		OutboxPayload:     outboxPayload, TaskPrivateData: string(privateData),
	})
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Interactions task %s atomic billing settlement failed: %v", task.TaskID, err))
		return false
	}
	if committed {
		task.Quota = actualQuota
		task.PrivateData.BillingContext = bc
		if missingReason != "" {
			logger.LogWarn(ctx, fmt.Sprintf("Interactions task %s usage unavailable; retained pre-consumed quota: %s", task.TaskID, missingReason))
		} else if result.Clamp != nil {
			logger.LogWarn(ctx, fmt.Sprintf("Interactions task %s expression settlement quota saturated: %+v", task.TaskID, result.Clamp))
		}
	}
	return true
}
