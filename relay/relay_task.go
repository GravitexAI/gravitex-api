package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

/*
Task 任务通过平台、Action 区分任务
*/
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	info.InitChannelMeta(c)
	// ensure TaskRelayInfo is initialized to avoid nil dereference when accessing embedded fields
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
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

	platform := constant.TaskPlatform(c.GetString("platform"))

	// 获取原始任务信息
	if info.OriginTaskID != "" {
		originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
		if err != nil {
			taskErr = service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
			return
		}
		if !exist {
			taskErr = service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
			return
		}
		if info.OriginModelName == "" {
			if originTask.Properties.OriginModelName != "" {
				info.OriginModelName = originTask.Properties.OriginModelName
			} else if originTask.Properties.UpstreamModelName != "" {
				info.OriginModelName = originTask.Properties.UpstreamModelName
			} else {
				var taskData map[string]interface{}
				_ = json.Unmarshal(originTask.Data, &taskData)
				if m, ok := taskData["model"].(string); ok && m != "" {
					info.OriginModelName = m
					platform = originTask.Platform
				}
			}
		}
		if originTask.ChannelId != info.ChannelId {
			channel, err := model.GetChannelById(originTask.ChannelId, true)
			if err != nil {
				taskErr = service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
				return
			}
			if channel.Status != common.ChannelStatusEnabled {
				taskErr = service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
				return
			}
			key, _, newAPIError := channel.GetNextEnabledKey()
			if newAPIError != nil {
				taskErr = service.TaskErrorWrapper(newAPIError, "channel_no_available_key", newAPIError.StatusCode)
				return
			}
			common.SetContextKey(c, constant.ContextKeyChannelKey, key)
			common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
			common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

			info.ChannelBaseUrl = channel.GetBaseURL()
			info.ChannelId = originTask.ChannelId
			info.ChannelType = channel.Type
			info.ApiKey = key
			platform = originTask.Platform
		}

		// 使用原始任务的参数
		if info.Action == constant.TaskActionRemix {
			var taskData map[string]interface{}
			_ = json.Unmarshal(originTask.Data, &taskData)
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
	if platform == "" {
		platform = GetTaskPlatform(c)
	}

	info.InitChannelMeta(c)
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	adaptor.Init(info)
	// get & validate taskRequest 获取并验证文本请求
	taskErr = adaptor.ValidateRequestAndSetAction(c, info)
	if taskErr != nil {
		return
	}

	// 打印用户请求体（供调试）
	if bodyStorage, bodyErr := common.GetBodyStorage(c); bodyErr == nil {
		if rawBody, readErr := io.ReadAll(common.ReaderOnly(bodyStorage)); readErr == nil {
			common.SysLog(fmt.Sprintf("[TaskSubmit] client request body: %s", common.TruncateJsonValues(string(rawBody))))
		}
	}

	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	// 处理 auto 分组：从 context 获取实际选中的分组
	// 当使用 auto 分组时，Distribute 中间件会将实际选中的分组存储在 ContextKeyAutoGroup 中
	if autoGroup, exists := common.GetContextKey(c, constant.ContextKeyAutoGroup); exists {
		if groupStr, ok := autoGroup.(string); ok && groupStr != "" {
			info.UsingGroup = groupStr
		}
	}

	groupRatio := ratio_setting.GetGroupRatio(info.UsingGroup)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(info.UserGroup, info.UsingGroup)
	effectiveGroupRatio := groupRatio
	if hasUserGroupRatio {
		effectiveGroupRatio = userGroupRatio
	}

	// isSora2VideoModel 判断是否为按秒计费的视频模型（提交时不预扣费，轮询成功后计费）
	isSora2VideoModel := isVideoPerSecondModel(modelName)

	// 计算余额检查用的估算quota（仅用于校验，不实际扣费）
	var quota int
	var modelPrice float64
	if isSora2VideoModel {
		// 按秒计费模型：估算 quota = videoPrice × 预期秒数 × groupRatio × oemDiscount
		videoPrice, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(modelName)
		oemUserDiscount := service.GetOemUserDiscountForQuota(c, modelName)
		if oemUserDiscount <= 0 {
			oemUserDiscount = 1.0
		}
		videoSeconds := parseVideoSeconds(c)
		if videoSeconds <= 0 {
			videoSeconds = 4 // 最短视频
		}
		if hasVideoPrice && videoPrice > 0 {
			quota = int(videoPrice * float64(videoSeconds) * common.QuotaPerUnit * effectiveGroupRatio * oemUserDiscount)
		} else {
			quota = int(0.4 * common.QuotaPerUnit) // 兜底：4秒 × $0.1/秒
		}
	} else {
		modelPrice, _ = ratio_setting.GetModelPrice(modelName, true)
		if modelPrice <= 0 {
			defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[modelName]
			if !ok {
				modelPrice = float64(common.PreConsumedQuota) / common.QuotaPerUnit
			} else {
				modelPrice = defaultPrice
			}
		}
		ratio := modelPrice * effectiveGroupRatio
		// FIXME: 临时修补，支持任务仅按次计费
		if !common.StringsContains(constant.TaskPricePatches, modelName) {
			for _, ra := range info.PriceData.OtherRatios {
				if ra != 1.0 {
					ratio *= ra
				}
			}
		}
		quota = int(ratio * common.QuotaPerUnit)
	}

	common.SysLog(fmt.Sprintf("[TaskSubmit] model=%s group=%s groupRatio=%.4f quota=%d isSora2=%v",
		modelName, info.UsingGroup, effectiveGroupRatio, quota, isSora2VideoModel))

	userQuota, err := model.GetUserQuota(info.UserId, false)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "get_user_quota_failed", http.StatusInternalServerError)
		return
	}
	if userQuota-quota < 0 {
		taskErr = service.TaskErrorWrapperLocal(errors.New("user quota is not enough"), "quota_not_enough", http.StatusForbidden)
		return
	}

	// build body
	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
		return
	}

	// 打印发送给上游的请求体（截断base64内容防止日志过大），并保存用于计费解析
	var upstreamBodyBytes []byte
	if bodyBytes, readErr := io.ReadAll(requestBody); readErr == nil {
		upstreamBodyBytes = bodyBytes
		common.SysLog(fmt.Sprintf("[TaskSubmit] upstream request body: %s", common.TruncateJsonValues(string(bodyBytes))))
		requestBody = bytes.NewReader(bodyBytes)
	}

	// do request
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("[TaskSubmit] do_request_failed: %s", err.Error()))
		taskErr = service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
		return
	}

	// handle response — only log on failure; success is recorded after task completes
	if resp != nil && resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		logger.LogError(c, fmt.Sprintf("[TaskSubmit] upstream error (status=%d): %s", resp.StatusCode, common.TruncateJsonValues(string(responseBody))))
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s", string(responseBody)), "fail_to_fetch_task", resp.StatusCode)
		return
	}

	// 非按秒计费模型：提交成功即预扣费并记录日志
	if !isSora2VideoModel {
		defer func() {
			if info.ConsumeQuota && taskErr == nil {
				err := service.PostConsumeQuota(info, quota, 0, true)
				if err != nil {
					common.SysLog("error consuming token remain quota: " + err.Error())
				}
				if quota != 0 {
					tokenName := c.GetString("token_name")
					logContent := fmt.Sprintf("操作 %s", info.Action)
					if common.StringsContains(constant.TaskPricePatches, modelName) {
						logContent = fmt.Sprintf("%s，按次计费", logContent)
					} else if len(info.PriceData.OtherRatios) > 0 {
						var contents []string
						for key, ra := range info.PriceData.OtherRatios {
							if ra != 1.0 {
								contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
							}
						}
						if len(contents) > 0 {
							logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
						}
					}
					other := make(map[string]interface{})
					if c != nil && c.Request != nil && c.Request.URL != nil {
						other["request_path"] = c.Request.URL.Path
					}
					other["model_price"] = modelPrice
					other["group_ratio"] = groupRatio
					if hasUserGroupRatio {
						other["user_group_ratio"] = userGroupRatio
					}
					priceChain := service.CalculatePriceChainForLog(c, modelName, 0, 0, quota)
					model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
						ChannelId:  info.ChannelId,
						ModelName:  modelName,
						TokenName:  tokenName,
						Quota:      quota,
						Content:    logContent,
						TokenId:    info.TokenId,
						Group:      info.UsingGroup,
						Other:      other,
						PriceChain: priceChain,
					})
					model.UpdateUserUsedQuotaAndRequestCount(info.UserId, quota)
					model.UpdateChannelUsedQuota(info.ChannelId, quota)
				}
			}
		}()
	}

	taskID, taskData, taskErr := adaptor.DoResponse(c, resp, info)
	if taskErr != nil {
		return
	}
	info.ConsumeQuota = true

	// 按秒计费模型：将扣费所需信息合并到 task.Data，轮询成功后再计费（含 token 信息供轮询完成后写日志）
	if isSora2VideoModel {
		taskData = mergeVideoTaskBillingData(c, info, taskData, modelName, info.UsingGroup, upstreamBodyBytes)
	}

	// insert task
	task := model.InitTask(platform, info)
	task.TaskID = taskID
	// 按秒计费模型：task.Quota=0（轮询后实际计费），其他模型保持估算的预扣quota
	if isSora2VideoModel {
		task.Quota = 0
	} else {
		task.Quota = quota
	}
	task.Data = taskData
	task.Action = info.Action
	err = task.Insert()
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "insert_task_failed", http.StatusInternalServerError)
		return
	}
	return nil
}

// isVideoPerSecondModel 判断是否为按秒计费的视频模型（提交时不预扣费）
func isVideoPerSecondModel(modelName string) bool {
	_, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(modelName)
	return hasVideoPrice
}

// parseVideoSeconds 从请求参数中解析视频秒数
// 优先读取 adaptor 在 context 中存储的已校验秒数（如 AzureVideo），
// 回退到 multipart form 字段 n_seconds / seconds，
// 最后尝试从上游请求体（JSON 或 multipart）解析。
func parseVideoSeconds(c *gin.Context) int {
	// 1. Adaptor may store validated seconds in context after BuildRequestBody runs
	if v, exists := c.Get("azure_video_seconds"); exists {
		if s, ok := v.(string); ok {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				return n
			}
		}
	}
	// 2. Fall back to reading multipart form fields
	for _, key := range []string{"n_seconds", "seconds"} {
		if v := c.PostForm(key); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// parseVideoSecondsFromBody 从上游请求体解析视频秒数（JSON 或 multipart 格式）
func parseVideoSecondsFromBody(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	// 1. 尝试 JSON 解析
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err == nil {
		for _, key := range []string{"seconds", "n_seconds"} {
			if v, ok := m[key]; ok {
				switch val := v.(type) {
				case string:
					if n, err := strconv.Atoi(val); err == nil && n > 0 {
						return n
					}
				case float64:
					if n := int(val); n > 0 {
						return n
					}
				}
			}
		}
	}
	// 2. 尝试从 multipart 中提取：name="n_seconds"\r\n\r\n12 或 name="seconds"\r\n\r\n12
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

// mergeVideoTaskBillingData 将扣费所需字段合并到 task.Data（轮询成功后使用）
func mergeVideoTaskBillingData(c *gin.Context, info *relaycommon.RelayInfo, taskData []byte, modelName, usingGroup string, upstreamBody []byte) []byte {
	var dataMap map[string]interface{}
	if len(taskData) > 0 {
		_ = common.Unmarshal(taskData, &dataMap)
	}
	if dataMap == nil {
		dataMap = make(map[string]interface{})
	}

	// 获取 OEM 信息
	oemCode := "gravitex"
	if code, exists := c.Get(string(constant.ContextKeyOemCode)); exists {
		if codeStr, ok := code.(string); ok && codeStr != "" {
			oemCode = codeStr
		}
	}
	oemUserDiscount := service.GetOemUserDiscountForQuota(c, modelName)
	if oemUserDiscount <= 0 {
		oemUserDiscount = 1.0
	}

	// 计算实际 groupRatio（与提交时保持一致：优先用用户组倍率）
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

	dataMap["billing_model_name"] = modelName
	dataMap["billing_group"] = usingGroup
	dataMap["billing_oem_code"] = oemCode
	dataMap["billing_oem_user_discount"] = oemUserDiscount
	dataMap["billing_effective_group_ratio"] = effectiveGroupRatio
	dataMap["billing_token_name"] = tokenName
	dataMap["billing_token_id"] = tokenId
	dataMap["billing_requested_seconds"] = videoSeconds
	dataMap["billing_processed"] = false

	merged, err := common.Marshal(dataMap)
	if err != nil {
		common.SysLog("[mergeVideoTaskBillingData] failed to marshal: " + err.Error())
		return taskData
	}
	return merged
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
	respBody, err = json.Marshal(dto.TaskResponse[[]any]{
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

	respBody, err = json.Marshal(dto.TaskResponse[any]{
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

	func() {
		channelModel, err2 := model.GetChannelById(originTask.ChannelId, true)
		if err2 != nil {
			return
		}
		if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
			return
		}
		baseURL := constant.ChannelBaseURLs[channelModel.Type]
		if channelModel.GetBaseURL() != "" {
			baseURL = channelModel.GetBaseURL()
		}
		proxy := channelModel.GetSetting().Proxy
		adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
		if adaptor == nil {
			return
		}
		resp, err2 := adaptor.FetchTask(baseURL, channelModel.Key, map[string]any{
			"task_id": originTask.TaskID,
			"action":  originTask.Action,
		}, proxy)
		if err2 != nil || resp == nil {
			return
		}
		defer resp.Body.Close()
		body, err2 := io.ReadAll(resp.Body)
		if err2 != nil {
			return
		}
		ti, err2 := adaptor.ParseTaskResult(body)
		if err2 == nil && ti != nil {
			if ti.Status != "" {
				originTask.Status = model.TaskStatus(ti.Status)
			}
			if ti.Progress != "" {
				originTask.Progress = ti.Progress
			}
			if ti.Url != "" {
				if strings.HasPrefix(ti.Url, "data:") {
				} else {
					originTask.FailReason = ti.Url
				}
			}
			_ = originTask.Update()
			var raw map[string]any
			_ = json.Unmarshal(body, &raw)
			format := "mp4"
			if respObj, ok := raw["response"].(map[string]any); ok {
				if vids, ok := respObj["videos"].([]any); ok && len(vids) > 0 {
					if v0, ok := vids[0].(map[string]any); ok {
						if mt, ok := v0["mimeType"].(string); ok && mt != "" {
							if strings.Contains(mt, "mp4") {
								format = "mp4"
							} else {
								format = mt
							}
						}
					}
				}
			}
			status := "processing"
			switch originTask.Status {
			case model.TaskStatusSuccess:
				status = "succeeded"
			case model.TaskStatusFailure:
				status = "failed"
			case model.TaskStatusQueued, model.TaskStatusSubmitted:
				status = "queued"
			}
			if !strings.HasPrefix(c.Request.RequestURI, "/v1/videos/") {
				out := map[string]any{
					"error":    nil,
					"format":   format,
					"metadata": nil,
					"status":   status,
					"task_id":  originTask.TaskID,
					"url":      originTask.FailReason,
				}
				respBody, _ = json.Marshal(dto.TaskResponse[any]{
					Code: "success",
					Data: out,
				})
			}
		}
	}()

	if len(respBody) != 0 {
		return
	}

	if strings.HasPrefix(c.Request.RequestURI, "/v1/videos/") {
		adaptor := GetTaskAdaptor(originTask.Platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}
	respBody, err = json.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		TaskID:     task.TaskID,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Data:       task.Data,
	}
}
