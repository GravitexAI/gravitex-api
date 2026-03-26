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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// CompleteVideoTaskOnUpstreamSuccessFn 由 main/router 注入，在 GET /v1/videos 收到上游终态时落库并计费，避免 relay 依赖 controller 产生 import cycle
var CompleteVideoTaskOnUpstreamSuccessFn func(context.Context, *model.Task, *model.Channel, *relaycommon.TaskInfo, []byte) error

/*
Task 任务通过平台、Action 区分任务
*/
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	info.InitChannelMeta(c)
	// ensure TaskRelayInfo is initialized to avoid nil dereference when accessing embedded fields
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	// 任务链路中 info.OriginModelName 可能尚未填充，优先用 distributor 写入的 original_model
	// 否则 ModelMappedHelper 无法命中映射表（会看到上游请求仍使用原模型名）
	if info.OriginModelName == "" && info.ChannelMeta != nil && info.ChannelMeta.UpstreamModelName != "" {
		info.OriginModelName = info.ChannelMeta.UpstreamModelName
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
				_ = common.Unmarshal(originTask.Data, &taskData)
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
	if platform == "" {
		platform = GetTaskPlatform(c)
	}

	info.InitChannelMeta(c)
	// 注意：上面再次 InitChannelMeta 会重置 ChannelMeta.UpstreamModelName 为 original_model，
	// 因此需要在这里（最终一次 InitChannelMeta 之后）再做 model_mapping，确保发往上游的 model 是映射后的值。
	if info.OriginModelName == "" && info.ChannelMeta != nil && info.ChannelMeta.UpstreamModelName != "" {
		info.OriginModelName = info.ChannelMeta.UpstreamModelName
	}
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		// 不直接中断请求，但必须打日志便于排查：通常是 model_mapping JSON 格式错误
		logger.LogWarn(c, fmt.Sprintf("[TaskSubmit] model_mapping apply failed: %v (model_mapping=%q origin_model=%q)",
			err, c.GetString(string(constant.ContextKeyChannelModelMapping)), info.OriginModelName))
	}
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

	// 打印用户请求体（供调试，写入文件日志，截断大字段）
	if bodyStorage, bodyErr := common.GetBodyStorage(c); bodyErr == nil {
		if rawBody, readErr := io.ReadAll(common.ReaderOnly(bodyStorage)); readErr == nil {
			logger.LogInfo(c, fmt.Sprintf("[TaskSubmit] client request body: %s", common.TruncateJsonValues(string(rawBody))))
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

	// isVideoTokenRatioBilling 是否为按量计费的视频模型（VideoRatio/VideoCompletionRatio，提交时不预扣费，轮询成功后按 usage 扣费）
	isVideoTokenRatioBilling := isVideoTokenRatioModel(modelName)
	// isPerSecondBilling 是否为按秒计费的视频模型（提交时不预扣费，轮询成功后计费）
	isPerSecondBilling := isVideoPerSecondModel(modelName)

	// 计算余额检查用的估算quota（仅用于校验，不实际扣费）
	var quota int
	var modelPrice float64
	if isPerSecondBilling {
		// 按秒计费模型（Veo 等）：按是否生成音频取价，估算 quota = videoPrice × 预期秒数 × groupRatio
		generateAudio := parseGenerateAudioForQuota(c)
		resKey := "720p"
		if v, exists := c.Get("video_billing_resolution"); exists {
			if s, ok := v.(string); ok && s != "" {
				resKey = ratio_setting.NormalizeVideoResolutionKey(s)
			}
		}
		videoPrice, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecondForBillingWithResolution(modelName, generateAudio, resKey)
		videoSeconds := parseVideoSeconds(c)
		if videoSeconds <= 0 {
			videoSeconds = 4 // 最短视频
		}
		if hasVideoPrice && videoPrice > 0 {
			quota = int(videoPrice * float64(videoSeconds) * common.QuotaPerUnit * effectiveGroupRatio)
		} else {
			quota = int(0.4 * common.QuotaPerUnit) // 兜底：4秒 × $0.1/秒
		}
	} else if isVideoTokenRatioBilling {
		// VideoRatio/VideoCompletionRatio：按量计费，usage 仅在轮询成功后返回，提交阶段不预扣费
		quota = 0
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

	logger.LogInfo(c, fmt.Sprintf("[TaskSubmit] model=%s group=%s groupRatio=%.4f quota=%d perSecondBilling=%v videoTokenRatioBilling=%v",
		modelName, info.UsingGroup, effectiveGroupRatio, quota, isPerSecondBilling, isVideoTokenRatioBilling))

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
		logger.LogInfo(c, fmt.Sprintf("[TaskSubmit] upstream request body: %s", common.TruncateJsonValues(string(bodyBytes))))
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

	// 非按秒/按量视频计费模型：提交成功即预扣费并记录日志
	if !isPerSecondBilling && !isVideoTokenRatioBilling {
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

	// 提交任务：打印上游返回数据（截断 base64 等大字段）
	if resp != nil && resp.Body != nil {
		submitResponseBody, _ := io.ReadAll(resp.Body)
		if len(submitResponseBody) > 0 {
			logger.LogInfo(c, fmt.Sprintf("[TaskSubmit] upstream response body: %s", common.TruncateJsonValues(string(submitResponseBody))))
		}
		resp.Body = io.NopCloser(bytes.NewReader(submitResponseBody))
	}
	// 延迟写入响应：DoResponse 将响应体存入 context，等 task.Insert() 成功后再写，避免重试时多次写响应导致前端收到多段 JSON 解析报错
	c.Set(relaycommon.TaskSubmitDelayResponse, true)
	taskID, taskData, taskErr := adaptor.DoResponse(c, resp, info)
	if taskErr != nil {
		return
	}
	info.ConsumeQuota = true

	// 按秒计费模型：将扣费所需信息合并到 task.Data，轮询成功后再计费（含 token 信息供轮询完成后写日志）
	if isPerSecondBilling {
		taskData = mergeVideoTaskBillingData(c, info, taskData, modelName, info.UsingGroup, upstreamBodyBytes)
	}
	// VideoRatio/VideoCompletionRatio：将扣费所需信息合并到 task.Data，轮询成功后按 usage 扣费
	if isVideoTokenRatioBilling {
		taskData = mergeVideoTokenRatioBillingData(c, info, taskData, modelName, info.UsingGroup)
	}

	// insert task
	task := model.InitTask(platform, info)
	task.TaskID = taskID
	// 按秒计费 / VideoRatio 按量计费模型：task.Quota=0（轮询后实际计费），其他模型保持估算的预扣quota
	if isPerSecondBilling || isVideoTokenRatioBilling {
		task.Quota = 0
		if isPerSecondBilling {
			sec := resolveRequestedSeconds(c, upstreamBodyBytes)
			task.Properties.RequestedSeconds = sec
		}
	} else {
		task.Quota = quota
	}
	task.Data = taskData
	task.Action = info.Action
	// 插入前再次保证按秒计费模型的 RequestedSeconds 已写入（防止 context/body 解析异常导致入库为 0）
	if isPerSecondBilling && task.Properties.RequestedSeconds <= 0 {
		sec := resolveRequestedSeconds(c, upstreamBodyBytes)
		if sec <= 0 {
			sec = 4
		}
		task.Properties.RequestedSeconds = sec
	}
	// 持久化用户原始请求体与上游请求体，轮询线程不覆盖，计费时从 upstream_request_body 读取 durationSeconds
	// 仅截断 base64 字符串，其它属性不截断，避免大图/视频 base64 撑爆存储
	if userBodyBytes := getUserRequestBody(c); len(userBodyBytes) > 0 {
		task.UserRequestBody = []byte(common.TruncateBase64Content(string(userBodyBytes)))
	}
	if len(upstreamBodyBytes) > 0 {
		task.UpstreamRequestBody = []byte(common.TruncateBase64Content(string(upstreamBodyBytes)))
	}
	if isPerSecondBilling {
		logger.LogInfo(c, fmt.Sprintf("[VideoTaskInsert] task_id=%s requested_seconds=%d",
			taskID, task.Properties.RequestedSeconds))
	}
	err = task.Insert()
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "insert_task_failed", http.StatusInternalServerError)
		return
	}
	// 任务入库成功后再写响应，保证只写一次，避免与重试逻辑叠加导致响应体被写两段
	if v, exists := c.Get(relaycommon.TaskSubmitResponseBody); exists && v != nil {
		if body, ok := v.([]byte); ok && len(body) > 0 {
			c.Data(http.StatusOK, "application/json", body)
		}
	}
	return nil
}

// isVideoPerSecondModel 判断是否为按秒计费的视频模型（提交时不预扣费）
func isVideoPerSecondModel(modelName string) bool {
	_, hasVideoPrice := ratio_setting.GetVideoModelPricePerSecond(modelName)
	return hasVideoPrice
}

func isVideoTokenRatioModel(modelName string) bool {
	// VideoRatio 存在即认为启用（含 0 值的显式配置）
	if _, ok := ratio_setting.GetVideoRatio(modelName); ok {
		return true
	}
	// VideoCompletionRatio 能取到值也认为启用（音频/无音频任一配置即可）
	if _, ok := ratio_setting.GetVideoCompletionRatioPricing(modelName, true); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoCompletionRatioPricing(modelName, false); ok {
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

// resolveRequestedSeconds 解析视频请求秒数，用于入库 Properties.RequestedSeconds（不依赖单一来源）
func resolveRequestedSeconds(c *gin.Context, upstreamBodyBytes []byte) int {
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

// parseGenerateAudioForQuota 从 task_request.Metadata 解析是否生成音频，用于按秒计费模型的预扣价（Veo 含音频/不含音频价格不同）。未指定时默认 true（按含音频价预留）。
func parseGenerateAudioForQuota(c *gin.Context) bool {
	if v, exists := c.Get("task_request"); exists {
		if req, ok := v.(relaycommon.TaskSubmitReq); ok {
			// wan2.6-flash 模型：audio 字段直接在顶层结构体（true=有声，false=无声）
			if req.Audio != nil {
				return *req.Audio
			}
			if req.Metadata != nil {
				// Kling V3 使用 sound: "on"/"off"
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
// 优先读取 adaptor 在 ValidateRequestAndSetAction 中存入的 video_seconds（Veo/Gemini/Vertex），
// 再读 azure_video_seconds，最后回退到 multipart 或上游请求体。
func parseVideoSeconds(c *gin.Context) int {
	// 1. Veo/Gemini/Vertex adaptor 在 ValidateRequestAndSetAction 中存入的秒数（用于计费）
	if v, exists := c.Get("video_seconds"); exists {
		if n, ok := v.(int); ok && n > 0 {
			return n
		}
		if f, ok := v.(float64); ok && f > 0 {
			return int(f)
		}
	}
	// 2. Adaptor may store validated seconds in context (e.g. AzureVideo)
	if v, exists := c.Get("azure_video_seconds"); exists {
		if s, ok := v.(string); ok {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				return n
			}
		}
	}
	// 3. Fall back to reading multipart form fields
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
	// 1. 尝试 JSON 解析（含 parameters.durationSeconds 与顶层 seconds/n_seconds）
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err == nil {
		for _, key := range []string{"durationSeconds", "duration", "seconds", "n_seconds"} {
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
				case int:
					if val > 0 {
						return val
					}
				}
			}
		}
		if params, _ := m["parameters"].(map[string]interface{}); params != nil {
			// 兼容 durationSeconds（Gemini/Veo）和 duration（Ali wan2.6）
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

	// 与 nebula-new-api 对齐：在 context 中缓存最终用于计费的秒数
	c.Set("video_seconds", videoSeconds)

	dataMap["billing_model_name"] = modelName
	dataMap["billing_group"] = usingGroup
	dataMap["billing_effective_group_ratio"] = effectiveGroupRatio
	dataMap["billing_token_name"] = tokenName
	dataMap["billing_token_id"] = tokenId
	// 与 nebula-new-api 对齐：使用 requested_seconds 作为通用字段名
	dataMap["requested_seconds"] = videoSeconds

	// 从请求中提取并保存音频生成标志（提交时落库，轮询计费时用）。优先 req.GenerateAudio，否则从 Metadata 取（客户端可能放在 metadata 里）
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

// mergeVideoTokenRatioBillingData 将按量计费视频模型（VideoRatio/VideoCompletionRatio）所需字段合并到 task.Data（轮询成功后使用）
func mergeVideoTokenRatioBillingData(c *gin.Context, info *relaycommon.RelayInfo, taskData []byte, modelName, usingGroup string) []byte {
	var dataMap map[string]interface{}
	if len(taskData) > 0 {
		_ = common.Unmarshal(taskData, &dataMap)
	}
	if dataMap == nil {
		dataMap = make(map[string]interface{})
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

	dataMap["billing_model_name"] = modelName
	dataMap["billing_group"] = usingGroup
	dataMap["billing_effective_group_ratio"] = effectiveGroupRatio
	dataMap["billing_token_name"] = tokenName
	dataMap["billing_token_id"] = tokenId

	generateAudio := parseGenerateAudioForQuota(c)
	dataMap["generate_audio"] = generateAudio
	dataMap["generateAudio"] = generateAudio

	dataMap["billing_processed"] = false

	merged, err := common.Marshal(dataMap)
	if err != nil {
		common.SysLog("[mergeVideoTokenRatioBillingData] failed to marshal: " + err.Error())
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
			// 保存之前的状态，用于判断是否需要更新数据库
			prevStatus := originTask.Status
			if ti.Status != "" {
				originTask.Status = model.TaskStatus(ti.Status)
			}
			if ti.Progress != "" {
				originTask.Progress = ti.Progress
			}
			// 失败时用 Reason（上游 error.message，如 Veo 敏感词等）；成功时用 Url/RemoteUrl（视频地址），必须入库供前端 GET /v1/videos/{id} 使用
			if ti.Status == model.TaskStatusFailure && strings.TrimSpace(ti.Reason) != "" {
				originTask.FailReason = strings.TrimSpace(ti.Reason)
			} else if ti.Url != "" {
				originTask.FailReason = ti.Url
			} else if ti.RemoteUrl != "" {
				originTask.FailReason = ti.RemoteUrl
			}
			// 当上游返回终态且当前任务尚未终态时：落库（data/status/progress/fail_reason）并执行计费，与后台轮询逻辑一致，避免 Vertex 轮询只返回 {"name":"..."} 导致任务永不完成、不扣费
			if prevStatus != model.TaskStatusSuccess && prevStatus != model.TaskStatusFailure {
				if originTask.Status == model.TaskStatusSuccess || originTask.Status == model.TaskStatusFailure {
					if CompleteVideoTaskOnUpstreamSuccessFn != nil {
						if errComplete := CompleteVideoTaskOnUpstreamSuccessFn(c.Request.Context(), originTask, channelModel, ti, body); errComplete != nil {
							logger.LogError(c.Request.Context(), fmt.Sprintf("[GET /v1/videos] CompleteVideoTaskOnUpstreamSuccess task=%s: %v", originTask.TaskID, errComplete))
						}
					} else {
						_ = model.TaskUpdateFailReason(originTask.ID, originTask.FailReason)
					}
				} else {
					_ = originTask.Update()
				}
			} else {
				// 后台轮询已处理过终态，使用 DB 中已有的 fail_reason（可能是 OSS URL）
				if dbTask, _, dbErr := model.GetByOnlyTaskId(originTask.TaskID); dbErr == nil && dbTask != nil && dbTask.FailReason != "" {
					originTask.FailReason = dbTask.FailReason
				}
			}
			var raw map[string]any
			_ = common.Unmarshal(body, &raw)
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
				respBody, _ = common.Marshal(dto.TaskResponse[any]{
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
	respBody, err = common.Marshal(dto.TaskResponse[any]{
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
