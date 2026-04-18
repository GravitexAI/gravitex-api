package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// CompleteVideoTaskOnUpstreamSuccessFn is set by main.go to break the import cycle
// between relay and controller packages.
var CompleteVideoTaskOnUpstreamSuccessFn func(ctx context.Context, task *model.Task, channel *model.Channel, taskResult *relaycommon.TaskInfo, responseBody []byte) error

type TaskSubmitResult struct {
	UpstreamTaskID string
	TaskData       []byte
	Platform       constant.TaskPlatform
	Quota          int

	// 按秒/按量计费标记（提交时不预扣费，轮询成功后由 controller 计费）
	IsPerSecondBilling       bool
	IsVideoTokenRatioBilling bool
	UpstreamBodyBytes        []byte // 上游请求体，供计费解析
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 锁定到原始任务的渠道（重试时复用同一渠道，轮换 key）
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
	}
	info.LockedChannel = ch

	if originTask.ChannelId != info.ChannelId {
		key, _, newAPIError := ch.GetNextEnabledKey()
		if newAPIError != nil {
			return service.TaskErrorWrapper(newAPIError, "channel_no_available_key", newAPIError.StatusCode)
		}
		common.SetContextKey(c, constant.ContextKeyChannelKey, key)
		common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
		common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

		info.ChannelBaseUrl = ch.GetBaseURL()
		info.ChannelId = originTask.ChannelId
		info.ChannelType = ch.Type
		info.ApiKey = key
	}

	// 提取 remix 参数（时长、分辨率 → OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			sizeStr, _ := taskData["size"].(string)
			if info.PriceData.OtherRatios == nil {
				info.PriceData.OtherRatios = map[string]float64{}
			}
			info.PriceData.OtherRatios["seconds"] = float64(seconds)
			info.PriceData.OtherRatios["size"] = 1
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.OtherRatios["size"] = 1.666667
			}
		}
	}

	return nil
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 估算计费(EstimateBilling) → 计算价格 → 预扣费（仅首次）→
// 构建/发送/解析上游请求 → 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)

	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	// 2.5 应用渠道的模型映射（与同步任务对齐）
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
	}

	// 2.6 检测按秒/按量计费模型（提交时不预扣费，轮询成功后由 controller 计费）
	isPerSecondBilling := isVideoPerSecondModel(modelName)
	isVideoTokenRatioBilling := isVideoTokenRatioModel(modelName)

	// 3. PublicTaskID 不再预生成：由各 adaptor 的 DoResponse 在拿到上游真实 ID 后
	//    回写 info.PublicTaskID = upstreamID，使 task_id 直接使用上游 ID，
	//    避免依赖 private_data.upstream_task_id（GORM MySQL JSON Scanner 有 bug）。
	//    如果 adaptor 未设置，InitTask 内部会 fallback 生成 task_xxxx。

	// 4. 价格计算：基础模型价格
	info.OriginModelName = modelName
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "model_price_error", http.StatusBadRequest)
	}
	info.PriceData = priceData

	// 5. 计费估算：让适配器根据用户请求提供 OtherRatios（时长、分辨率等）
	//    必须在 ModelPriceHelperPerCall 之后调用（它会重建 PriceData）。
	//    ResolveOriginTask 可能已在 remix 路径中预设了 OtherRatios，此处合并。
	if estimatedRatios := adaptor.EstimateBilling(c, info); len(estimatedRatios) > 0 {
		for k, v := range estimatedRatios {
			info.PriceData.AddOtherRatio(k, v)
		}
	}

	// 6. 将 OtherRatios 应用到基础额度
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		for _, ra := range info.PriceData.OtherRatios {
			if ra != 1.0 {
				info.PriceData.Quota = int(float64(info.PriceData.Quota) * ra)
			}
		}
	}

	// 7. 预扣费（仅首次 — 重试时 info.Billing 已存在，跳过）
	// 按秒/按量视频计费模型：不做预扣费，轮询成功后由 controller.UpdateVideoTaskAll 计费
	// 但仍需检查用户余额，防止零余额用户白嫖（与 chat/images 路径一致，TokenUnlimited 只免 token 额度检查，不免用户余额检查）
	if !isPerSecondBilling && !isVideoTokenRatioBilling {
		if info.Billing == nil && !info.PriceData.FreeModel {
			info.ForcePreConsume = true
			if apiErr := service.PreConsumeBilling(c, info.PriceData.Quota, info); apiErr != nil {
				return nil, service.TaskErrorFromAPIError(apiErr)
			}
		}
	} else if !info.PriceData.FreeModel {
		// 按秒/按量计费模型：不预扣费，但检查用户余额 > 0
		userQuota, err := model.GetUserQuota(info.UserId, false)
		if err != nil {
			return nil, service.TaskErrorWrapperLocal(
				fmt.Errorf("查询用户额度失败: %v", err),
				"query_quota_failed", http.StatusInternalServerError)
		}
		if userQuota <= 0 {
			return nil, service.TaskErrorWrapperLocal(
				fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
				"insufficient_user_quota", http.StatusForbidden)
		}
	}

	// 8. 构建请求体
	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}

	// 8.5 捕获上游请求体用于计费解析（按秒计费模型需从中读取 durationSeconds 等）
	var upstreamBodyBytes []byte
	if requestBody != nil {
		if bodyBytes, readErr := io.ReadAll(requestBody); readErr == nil {
			upstreamBodyBytes = bodyBytes
			requestBody = bytes.NewReader(bodyBytes)
		}
	}

	// 9. 发送请求
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		return nil, service.TaskErrorWrapper(fmt.Errorf("%s", string(responseBody)), "fail_to_fetch_task", resp.StatusCode)
	}

	// 10. 返回 OtherRatios 给下游（header 必须在 DoResponse 写 body 之前设置）
	otherRatios := info.PriceData.OtherRatios
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	ratiosJSON, _ := common.Marshal(otherRatios)
	c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))

	// 11. 解析响应
	upstreamTaskID, taskData, taskErr := adaptor.DoResponse(c, resp, info)
	if taskErr != nil {
		return nil, taskErr
	}

	// 12. 合并计费数据到 task.Data（供轮询完成后计费使用）
	if isPerSecondBilling {
		taskData = mergeVideoTaskBillingData(c, info, taskData, modelName, info.UsingGroup, upstreamBodyBytes)
	} else if isVideoTokenRatioBilling {
		taskData = mergeVideoTokenRatioBillingData(c, info, taskData, modelName, info.UsingGroup)
	} else {
		taskData = mergeTokenInfoToTaskData(c, info, taskData)
	}

	// 13. 提交后计费调整：让适配器根据上游实际返回调整 OtherRatios
	finalQuota := info.PriceData.Quota
	if !isPerSecondBilling && !isVideoTokenRatioBilling {
		if adjustedRatios := adaptor.AdjustBillingOnSubmit(info, taskData); len(adjustedRatios) > 0 {
			finalQuota = recalcQuotaFromRatios(info, adjustedRatios)
			info.PriceData.OtherRatios = adjustedRatios
			info.PriceData.Quota = finalQuota
		}
	}

	return &TaskSubmitResult{
		UpstreamTaskID:           upstreamTaskID,
		TaskData:                 taskData,
		Platform:                 platform,
		Quota:                    finalQuota,
		IsPerSecondBilling:       isPerSecondBilling,
		IsVideoTokenRatioBilling: isVideoTokenRatioBilling,
		UpstreamBodyBytes:        upstreamBodyBytes,
	}, nil
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota × ∏(ratio) — 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *relaycommon.RelayInfo, ratios map[string]float64) int {
	// 从 PriceData 获取不含 OtherRatios 的基础价格
	baseQuota := info.PriceData.Quota
	// 先除掉原有的 OtherRatios 恢复基础额度
	for _, ra := range info.PriceData.OtherRatios {
		if ra != 1.0 && ra > 0 {
			baseQuota = int(float64(baseQuota) / ra)
		}
	}
	// 应用新的 ratios
	result := float64(baseQuota)
	for _, ra := range ratios {
		if ra != 1.0 {
			result *= ra
		}
	}
	return int(result)
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态
	if realtimeResp := tryRealtimeFetch(originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}

	// 统一走 ConvertToOpenAIVideo 转换为标准 OpenAI Video 格式（含 metadata）
	adaptor := GetTaskAdaptor(originTask.Platform)
	if adaptor != nil {
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			if isOpenAIVideoAPI {
				// /v1/videos/:task_id — 直接返回 OpenAIVideo JSON
				respBody = openAIVideoData
			} else {
				// /v1/video/generations/:task_id — 包装为 TaskResponse 格式
				var openAIVideo any
				if err := common.Unmarshal(openAIVideoData, &openAIVideo); err != nil {
					taskResp = service.TaskErrorWrapper(err, "unmarshal_openai_video_failed", http.StatusInternalServerError)
					return
				}
				respBody, err = common.Marshal(dto.TaskResponse[any]{
					Code: "success",
					Data: openAIVideo,
				})
				if err != nil {
					taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
				}
			}
			return
		}
		if isOpenAIVideoAPI {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
			return
		}
	}

	// Fallback: 没有实现 ConvertToOpenAIVideo 的 adaptor，走通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte {
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := constant.ChannelBaseURLs[channelModel.Type]
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	proxy := channelModel.GetSetting().Proxy
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil
	}

	resp, err := adaptor.FetchTask(baseURL, channelModel.Key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil || resp == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	ti, err := adaptor.ParseTaskResult(body)
	if err != nil || ti == nil {
		return nil
	}

	snap := task.Snapshot()

	// 将上游最新状态更新到 task
	if ti.Status != "" {
		task.Status = model.TaskStatus(ti.Status)
	}
	if ti.Progress != "" {
		task.Progress = ti.Progress
	}
	if strings.HasPrefix(ti.Url, "data:") {
		// data: URI — kept in Data, not ResultURL
	} else if ti.Url != "" {
		task.PrivateData.ResultURL = ti.Url
	} else if task.Status == model.TaskStatusSuccess {
		// No URL from adaptor — construct proxy URL using public task ID
		task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}

	if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	return respBody
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}

// ============================
// 按秒/按量计费辅助函数（从 alpha 迁移）
// ============================

// isVideoPerSecondModel 判断是否为按秒计费的视频模型（提交时不预扣费）
func isVideoPerSecondModel(modelName string) bool {
	_, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(modelName)
	return hasVideoPrice
}

func isVideoTokenRatioModel(modelName string) bool {
	if _, ok := ratio_setting.GetVideoRatio(modelName); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioPricing(modelName, true); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioPricing(modelName, false); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioVideoPricing(modelName, true); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioVideoPricing(modelName, false); ok {
		return true
	}
	return false
}

// getUserRequestBody 获取用户原始请求体（用于持久化到 task.UserRequestBody，轮询不覆盖）
func getUserRequestBody(c *gin.Context) []byte {
	if c == nil {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil
	}
	b, _ := storage.Bytes()
	return b
}

// ResolveRequestedSeconds 解析视频请求秒数，用于入库 Properties.RequestedSeconds
func ResolveRequestedSeconds(c *gin.Context, upstreamBodyBytes []byte) int {
	if c != nil {
		if v, exists := c.Get("video_seconds"); exists {
			if n, ok := v.(int); ok && n > 0 {
				return n
			}
			if f, ok := v.(float64); ok && f > 0 {
				return int(f)
			}
		}
		if n := parseVideoSeconds(c); n > 0 {
			return n
		}
	}
	if len(upstreamBodyBytes) > 0 {
		if n := parseVideoSecondsFromBody(upstreamBodyBytes); n > 0 {
			return n
		}
	}
	return 4
}

// parseGenerateAudioForQuota 从 task_request.Metadata 解析是否生成音频
func parseGenerateAudioForQuota(c *gin.Context) bool {
	if v, exists := c.Get("task_request"); exists {
		if req, ok := v.(relaycommon.TaskSubmitReq); ok {
			if req.Audio != nil {
				return *req.Audio
			}
			if req.Metadata != nil {
				if soundVal, ok := req.Metadata["sound"]; ok {
					if s, ok := soundVal.(string); ok {
						return strings.EqualFold(s, "on")
					}
				}
				for _, key := range []string{"generateAudio", "generate_audio", "audio"} {
					if val, ok := req.Metadata[key]; ok {
						switch b := val.(type) {
						case bool:
							return b
						case string:
							s := strings.TrimSpace(strings.ToLower(b))
							if s == "false" || s == "0" || s == "no" {
								return false
							}
							if s == "true" || s == "1" || s == "yes" {
								return true
							}
						}
					}
				}
			}
		}
	}
	return true
}

// parseVideoSeconds 从请求参数中解析视频秒数
func parseVideoSeconds(c *gin.Context) int {
	if v, exists := c.Get("video_seconds"); exists {
		if n, ok := v.(int); ok && n > 0 {
			return n
		}
		if f, ok := v.(float64); ok && f > 0 {
			return int(f)
		}
	}
	if v, exists := c.Get("azure_video_seconds"); exists {
		if s, ok := v.(string); ok {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				return n
			}
		}
	}
	for _, key := range []string{"n_seconds", "seconds"} {
		if v := c.PostForm(key); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// parseVideoSecondsFromBody 从上游请求体解析视频秒数
func parseVideoSecondsFromBody(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err == nil {
		for _, key := range []string{"durationSeconds", "duration", "seconds", "n_seconds"} {
			if v, ok := m[key]; ok {
				switch val := v.(type) {
				case string:
					if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 {
						return n
					}
				case float64:
					if n := int(val); n > 0 {
						return n
					}
				case int:
					if val > 0 {
						return val
					}
				}
			}
		}
		if params, _ := m["parameters"].(map[string]interface{}); params != nil {
			for _, key := range []string{"durationSeconds", "duration"} {
				if v, ok := params[key]; ok {
					switch val := v.(type) {
					case float64:
						if n := int(val); n > 0 {
							return n
						}
					case int:
						if val > 0 {
							return val
						}
					}
				}
			}
		}
	}
	s := string(body)
	for _, name := range []string{`name="n_seconds"`, `name="seconds"`} {
		if idx := strings.Index(s, name); idx >= 0 {
			rest := s[idx+len(name):]
			if idx2 := strings.Index(rest, "\r\n\r\n"); idx2 >= 0 {
				valPart := rest[idx2+4:]
				if idx3 := strings.Index(valPart, "\r\n"); idx3 >= 0 {
					valPart = valPart[:idx3]
				}
				if n, err := strconv.Atoi(strings.TrimSpace(valPart)); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

// mergeTokenInfoToTaskData 将 token 信息写入 task.Data，供轮询完成后日志使用
func mergeTokenInfoToTaskData(c *gin.Context, info *relaycommon.RelayInfo, taskData []byte) []byte {
	var dataMap map[string]interface{}
	if len(taskData) > 0 {
		_ = common.Unmarshal(taskData, &dataMap)
	}
	if dataMap == nil {
		dataMap = make(map[string]interface{})
	}
	tokenName := c.GetString("token_name")
	tokenId := 0
	if tid, ok := c.Get(string(constant.ContextKeyTokenId)); ok {
		switch v := tid.(type) {
		case int:
			tokenId = v
		case float64:
			tokenId = int(v)
		}
	}
	if tokenId == 0 && info != nil && info.TokenId > 0 {
		tokenId = info.TokenId
	}
	dataMap["billing_token_name"] = tokenName
	dataMap["billing_token_id"] = tokenId
	merged, err := common.Marshal(dataMap)
	if err != nil {
		return taskData
	}
	return merged
}

// mergeVideoTaskBillingData 将按秒计费所需字段合并到 task.Data（轮询成功后使用）
func mergeVideoTaskBillingData(c *gin.Context, info *relaycommon.RelayInfo, taskData []byte, modelName, usingGroup string, upstreamBody []byte) []byte {
	var dataMap map[string]interface{}
	if len(taskData) > 0 {
		_ = common.Unmarshal(taskData, &dataMap)
	}
	if dataMap == nil {
		dataMap = make(map[string]interface{})
	}
	effectiveGroupRatio := ratio_setting.GetGroupRatio(usingGroup)
	if info != nil && info.UserGroup != "" {
		if ugr, hasUGR := ratio_setting.GetGroupGroupRatio(info.UserGroup, usingGroup); hasUGR {
			effectiveGroupRatio = ugr
		}
	}
	if effectiveGroupRatio <= 0 {
		effectiveGroupRatio = 1.0
	}
	tokenName := c.GetString("token_name")
	tokenId := 0
	if tid, ok := c.Get(string(constant.ContextKeyTokenId)); ok {
		switch v := tid.(type) {
		case int:
			tokenId = v
		case float64:
			tokenId = int(v)
		}
	}
	if tokenId == 0 && info != nil && info.TokenId > 0 {
		tokenId = info.TokenId
	}
	videoSeconds := parseVideoSeconds(c)
	if videoSeconds <= 0 && len(upstreamBody) > 0 {
		videoSeconds = parseVideoSecondsFromBody(upstreamBody)
	}
	if videoSeconds <= 0 {
		videoSeconds = 4
	}
	c.Set("video_seconds", videoSeconds)
	dataMap["billing_model_name"] = modelName
	dataMap["billing_group"] = usingGroup
	dataMap["billing_effective_group_ratio"] = effectiveGroupRatio
	dataMap["billing_token_name"] = tokenName
	dataMap["billing_token_id"] = tokenId
	dataMap["requested_seconds"] = videoSeconds
	costDiscount := common.GetContextKeyFloat64(c, constant.ContextKeyChannelCostDiscount)
	if costDiscount > 0 {
		dataMap["billing_cost_discount"] = costDiscount
	}
	generateAudio := parseGenerateAudioForQuota(c)
	dataMap["generate_audio"] = generateAudio
	dataMap["generateAudio"] = generateAudio
	dataMap["billing_processed"] = false
	merged, err := common.Marshal(dataMap)
	if err != nil {
		common.SysLog("[mergeVideoTaskBillingData] failed to marshal: " + err.Error())
		return taskData
	}
	return merged
}

// mergeVideoTokenRatioBillingData 将按量计费视频模型所需字段合并到 task.Data
func mergeVideoTokenRatioBillingData(c *gin.Context, info *relaycommon.RelayInfo, taskData []byte, modelName, usingGroup string) []byte {
	var dataMap map[string]interface{}
	if len(taskData) > 0 {
		_ = common.Unmarshal(taskData, &dataMap)
	}
	if dataMap == nil {
		dataMap = make(map[string]interface{})
	}
	effectiveGroupRatio := ratio_setting.GetGroupRatio(usingGroup)
	if info != nil && info.UserGroup != "" {
		if ugr, hasUGR := ratio_setting.GetGroupGroupRatio(info.UserGroup, usingGroup); hasUGR {
			effectiveGroupRatio = ugr
		}
	}
	if effectiveGroupRatio <= 0 {
		effectiveGroupRatio = 1.0
	}
	tokenName := c.GetString("token_name")
	tokenId := 0
	if tid, ok := c.Get(string(constant.ContextKeyTokenId)); ok {
		switch v := tid.(type) {
		case int:
			tokenId = v
		case float64:
			tokenId = int(v)
		}
	}
	if tokenId == 0 && info != nil && info.TokenId > 0 {
		tokenId = info.TokenId
	}
	dataMap["billing_model_name"] = modelName
	dataMap["billing_group"] = usingGroup
	dataMap["billing_effective_group_ratio"] = effectiveGroupRatio
	dataMap["billing_token_name"] = tokenName
	dataMap["billing_token_id"] = tokenId
	costDiscount := common.GetContextKeyFloat64(c, constant.ContextKeyChannelCostDiscount)
	if costDiscount > 0 {
		dataMap["billing_cost_discount"] = costDiscount
	}
	generateAudio := parseGenerateAudioForQuota(c)
	dataMap["generate_audio"] = generateAudio
	dataMap["generateAudio"] = generateAudio
	if v, exists := c.Get("has_video_input"); exists {
		if b, ok := v.(bool); ok {
			dataMap["has_video_input"] = b
		}
	}
	dataMap["billing_processed"] = false
	merged, err := common.Marshal(dataMap)
	if err != nil {
		common.SysLog("[mergeVideoTokenRatioBillingData] failed to marshal: " + err.Error())
		return taskData
	}
	return merged
}
