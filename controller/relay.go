package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			// Request parse/validation failures are client errors (e.g. wrong field
			// type, missing required field). Return 400 instead of the default 500.
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		addUsedChannel(c, channel.Id)
		if billingErr := service.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
			newAPIError = billingErr
			break
		}

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError

		willRetry := shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry())

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError, willRetry)

		if !willRetry {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		if newAPIError != nil {
			logger.LogError(c, retryLogStr+" (全部失败)")
		} else {
			logger.LogInfo(c, retryLogStr)
		}
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, willRetry bool) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		// willRetry=true 时记录为"重试"类型，willRetry=false 时记录为"错误"类型
		logType := model.LogTypeError
		if willRetry {
			logType = model.LogTypeRetryFail
		}
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
			if originalPath := c.GetString("native_interactions_original_path"); originalPath != "" {
				other["request_path"] = originalPath
			}
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		costDiscount := common.GetContextKeyFloat64(c, constant.ContextKeyChannelCostDiscount)
		if costDiscount > 0 {
			adminInfo["cost_discount"] = costDiscount
		}
		other["admin_info"] = adminInfo
		model.AppendConfiguredClientRequestHeaders(c, userId, other)
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		if logType == model.LogTypeRetryFail {
			// 重试类型：直接写库，使用 LogTypeRetryFail
			// 添加重试链路信息：当前是第几次重试、渠道链路字符串
			useChannels := c.GetStringSlice("use_channel")
			retryCount := len(useChannels) // 第 N 次尝试（含首次）
			other["retry_count"] = retryCount
			other["retry_chain"] = strings.Join(useChannels, "->")
			requestId := c.GetString(common.RequestIdKey)
			otherStr := common.MapToJsonStr(other)
			retryContent := fmt.Sprintf("[重试%d/%d %s] %s", retryCount, common.RetryTimes+1, strings.Join(useChannels, "->"), err.MaskSensitiveErrorWithStatusCode())
			retryLog := &model.Log{
				UserId:    userId,
				Username:  c.GetString("username"),
				CreatedAt: common.GetTimestamp(),
				Type:      model.LogTypeRetryFail,
				Content:   retryContent,
				TokenName: tokenName,
				ModelName: modelName,
				ChannelId: channelId,
				TokenId:   tokenId,
				UseTime:   useTimeSeconds,
				Group:     userGroup,
				RequestId: requestId,
				Other:     otherStr,
			}
			model.CreateLog(retryLog)
		} else {
			model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
		}
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *taskdto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}
	// Keep the client-requested model stable for task properties and async
	// billing. The upstream model is stored separately in UpstreamModelName.
	if originalModel := strings.TrimSpace(c.GetString("original_model")); originalModel != "" {
		relayInfo.OriginModelName = originalModel
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		c.Set("native_vertex_lyria_response", isVertexLyriaScope(relayInfo.NativeInteractions, channel.Type, relayInfo.OriginModelName))
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode),
				false)
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		if taskErr != nil {
			logger.LogError(c, retryLogStr+" (全部失败)")
		} else {
			logger.LogInfo(c, retryLogStr)
		}
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		// The async dispatcher already pre-charged, inserted IN_PROGRESS and
		// wrote the immediate Interaction response. The worker owns settlement
		// and final task persistence.
		if result != nil && result.AsyncDispatched {
			return
		}
		// 按秒/按量视频计费模型：提交时不预扣费、不记录日志，轮询成功后由 controller.UpdateVideoTaskAll 计费
		// Keep the legacy billing-route decision unchanged. Cost-discount
		// snapshots are persisted independently below and must not alter whether
		// a task uses pre-deduction, per-second, or token-ratio settlement.
		isPerSecondOrTokenRatio := result.IsPerSecondBilling || result.IsVideoTokenRatioBilling
		lyriaFailed := isFailedNativeLyriaSubmit(relayInfo.NativeInteractions, relayInfo.OriginModelName, result)

		if lyriaFailed {
			upstreamStatus := result.UpstreamStatusCode
			if upstreamStatus == 0 {
				upstreamStatus = http.StatusOK
			}
			recordVertexLyriaSubmitFailure(c, relayInfo, result.InitialTaskInfo.Reason, upstreamStatus)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
		} else if !isPerSecondOrTokenRatio {
			if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
				common.SysError("settle task billing error: " + settleErr.Error())
			}
			service.LogTaskConsumption(c, relayInfo)
		} else {
			// 按秒/按量计费不做 Settle（未预扣），仅清理 Billing 引用防止 defer Refund
			relayInfo.Billing = nil
		}

		if shouldPersistLyriaTask(relayInfo.NativeInteractions, relayInfo.OriginModelName, c.GetBool("native_interactions_background")) {
			task := buildSubmittedTask(c, relayInfo, result, isPerSecondOrTokenRatio)
			if insertErr := task.Insert(); insertErr != nil {
				common.SysError("insert task error: " + insertErr.Error())
			}
		}
	}
	// Legacy task-backed providers still need a terminal task row for audit and
	// task-list consistency. Synchronous native Lyria is intentionally excluded;
	// processChannelError records the error log and the deferred BillingSession
	// refund remains responsible for the precharge.
	if taskErr != nil && result != nil && isNativeLyriaScope(relayInfo.NativeInteractions, relayInfo.OriginModelName) &&
		shouldPersistLyriaTask(relayInfo.NativeInteractions, relayInfo.OriginModelName, c.GetBool("native_interactions_background")) {
		task := buildSubmittedTask(c, relayInfo, result, false)
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert failed lyria task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func shouldPersistSynchronousLyriaTask(nativeInteractions bool, modelName string) bool {
	return !(nativeInteractions && (modelName == "lyria-3-pro-preview" || modelName == "lyria-3-clip-preview"))
}

func shouldPersistLyriaTask(nativeInteractions bool, modelName string, background bool) bool {
	if nativeInteractions && (modelName == "lyria-3-pro-preview" || modelName == "lyria-3-clip-preview") {
		return background
	}
	return true
}

func isVertexLyriaScope(nativeInteractions bool, channelType int, modelName string) bool {
	if !isNativeLyriaScope(nativeInteractions, modelName) || channelType != constant.ChannelTypeVertexAi {
		return false
	}
	return true
}

func isNativeLyriaScope(nativeInteractions bool, modelName string) bool {
	return nativeInteractions && (modelName == "lyria-3-pro-preview" || modelName == "lyria-3-clip-preview")
}

func isFailedNativeLyriaSubmit(nativeInteractions bool, modelName string, result *relay.TaskSubmitResult) bool {
	if !isNativeLyriaScope(nativeInteractions, modelName) || result == nil ||
		result.Platform != constant.TaskPlatformLyria || result.InitialTaskInfo == nil {
		return false
	}
	status := model.TaskStatus(result.InitialTaskInfo.Status)
	return status == model.TaskStatusFailure || status == model.TaskStatusCancelled
}

func isFailedVertexLyriaSubmit(nativeInteractions bool, channelType int, modelName string, result *relay.TaskSubmitResult) bool {
	return isVertexLyriaScope(nativeInteractions, channelType, modelName) && isFailedNativeLyriaSubmit(nativeInteractions, modelName, result)
}

func recordVertexLyriaSubmitFailure(c *gin.Context, info *relaycommon.RelayInfo, reason string, upstreamStatus int) {
	if !constant.ErrorLogEnabled || info == nil {
		return
	}
	requestPath := c.Request.URL.Path
	if originalPath := c.GetString("native_interactions_original_path"); originalPath != "" {
		requestPath = originalPath
	}
	other := map[string]interface{}{
		"is_task":                 true,
		"request_path":            requestPath,
		"status_code":             upstreamStatus,
		"interaction_status":      "failed",
		"channel_id":              info.ChannelId,
		"channel_type":            info.ChannelType,
		"upstream_business_error": true,
	}
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	model.RecordErrorLog(c, info.UserId, info.ChannelId, info.OriginModelName, c.GetString("token_name"),
		reason, info.TokenId, int(time.Since(startTime).Seconds()), common.GetContextKeyBool(c, constant.ContextKeyIsStream), info.UsingGroup, other)
}

func buildSubmittedTask(c *gin.Context, relayInfo *relaycommon.RelayInfo, result *relay.TaskSubmitResult, isPerSecondOrTokenRatio bool) *model.Task {
	task := model.InitTask(result.Platform, relayInfo)
	task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
	task.PrivateData.BillingSource = relayInfo.BillingSource
	task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
	task.PrivateData.TokenId = relayInfo.TokenId
	task.PrivateData.NodeName = common.NodeName
	billingContext := &model.TaskBillingContext{
		ModelPrice:      relayInfo.PriceData.ModelPrice,
		GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		ModelRatio:      relayInfo.PriceData.ModelRatio,
		OtherRatios:     relayInfo.PriceData.OtherRatios(),
		OriginModelName: relayInfo.OriginModelName,
		PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
	}
	task.PrivateData.BillingContext = billingContext
	if isPerSecondOrTokenRatio {
		task.Quota = 0
	} else {
		task.Quota = result.Quota
	}
	if isNativeLyriaScope(relayInfo.NativeInteractions, relayInfo.OriginModelName) {
		// Lyria's task row is the audit copy of the provider response. Keep it
		// byte-for-byte intact; the discount snapshot still lives in private_data.
		task.Data = append([]byte(nil), result.TaskData...)
		if discount := common.GetContextKeyFloat64(c, constant.ContextKeyChannelCostDiscount); discount > 0 && discount <= 1 {
			billingContext.CostDiscount = common.GetPointer(discount)
		}
	} else {
		task.Data = ensureAsyncTaskCostDiscountSnapshot(c, billingContext, result.TaskData)
	}
	task.Action = relayInfo.Action
	applyInitialTaskSubmitResult(task, result, time.Now().Unix())
	if result.IsPerSecondBilling {
		task.Properties.RequestedSeconds = relay.ResolveRequestedSeconds(c, result.UpstreamBodyBytes)
		if task.Properties.RequestedSeconds <= 0 {
			task.Properties.RequestedSeconds = 4
		}
	}
	if len(result.UpstreamBodyBytes) > 0 {
		task.UpstreamRequestBody = []byte(common.TruncateBase64Content(string(result.UpstreamBodyBytes)))
	}
	task.TokenName = c.GetString("token_name")
	task.TokenId = relayInfo.TokenId
	return task
}

func applyInitialTaskSubmitResult(task *model.Task, result *relay.TaskSubmitResult, now int64) {
	if task == nil || result == nil || result.InitialTaskInfo == nil || task.Platform != constant.TaskPlatformLyria {
		return
	}
	info := result.InitialTaskInfo
	status := model.TaskStatus(info.Status)
	if status != model.TaskStatusSuccess && status != model.TaskStatusFailure && status != model.TaskStatusCancelled {
		return
	}
	task.Status = status
	task.Progress = info.Progress
	if task.Progress == "" {
		task.Progress = "100%"
	}
	task.StartTime = task.SubmitTime
	task.FinishTime = now
	task.FailReason = info.Reason
	if info.Url != "" && !strings.HasPrefix(info.Url, "data:") {
		task.PrivateData.ResultURL = info.Url
	}
}

// ensureAsyncTaskCostDiscountSnapshot writes the effective discount to both
// existing task.Data metadata and private_data.billing_context. The private
// copy is required because polling may replace task.Data with the upstream
// response. An existing task.Data value remains authoritative for backward
// compatibility, while an unconfigured task keeps both snapshots absent.
func ensureAsyncTaskCostDiscountSnapshot(c *gin.Context, billingContext *model.TaskBillingContext, taskData []byte) []byte {
	discount := common.GetContextKeyFloat64(c, constant.ContextKeyChannelCostDiscount)
	if discount <= 0 || discount > 1 {
		return taskData
	}
	if billingContext != nil {
		billingContext.CostDiscount = common.GetPointer(discount)
	}

	data := make(map[string]interface{})
	if len(taskData) > 0 {
		if err := common.Unmarshal(taskData, &data); err != nil {
			return taskData
		}
	}
	if _, exists := data["billing_cost_discount"]; exists {
		return taskData
	}
	data["billing_cost_discount"] = discount
	merged, err := common.Marshal(data)
	if err != nil {
		common.SysLog("[ensureAsyncTaskCostDiscountSnapshot] failed to marshal task data: " + err.Error())
		return taskData
	}
	return merged
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if len(taskErr.RawBody) > 0 && common.IsTaskRawMirror(c) {
		c.Data(taskErr.StatusCode, "application/json", taskErr.RawBody)
		return
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *taskdto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if c.GetBool(common.KeyLyriaRawMirror) {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		// 429 不重试，直接返回给客户端，避免无 backoff 地反复请求上游
		return false
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 504/524 等状态码是否重试由 AutomaticRetryStatusCodes 配置决定；
		// 其他 5xx 保持任务链路原有的默认重试行为。
		if taskErr.StatusCode == http.StatusGatewayTimeout || taskErr.StatusCode == 524 {
			return operation_setting.ShouldRetryByStatusCode(taskErr.StatusCode)
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
