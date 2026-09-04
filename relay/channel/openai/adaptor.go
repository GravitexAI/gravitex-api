package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/ai360"
	"github.com/QuantumNous/new-api/relay/channel/lingyiwanwu"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	"github.com/QuantumNous/new-api/relaykit/dto"

	//"github.com/QuantumNous/new-api/relay/channel/minimax"
	"github.com/QuantumNous/new-api/relay/channel/xinference"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/common_handler"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	ChannelType    int
	ResponseFormat string
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, request)
	if err != nil {
		return nil, err
	}
	openaiRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	return a.ConvertOpenAIRequest(c, info, openaiRequest)
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	//if !strings.Contains(request.Model, "claude") {
	//	return nil, fmt.Errorf("you are using openai channel type with path /v1/messages, only claude model supported convert, but got %s", request.Model)
	//}
	//if common.DebugEnabled {
	//	bodyBytes := []byte(common.GetJsonString(request))
	//	err := os.WriteFile(fmt.Sprintf("claude_request_%s.txt", c.GetString(common.RequestIdKey)), bodyBytes, 0644)
	//	if err != nil {
	//		println(fmt.Sprintf("failed to save request body to file: %v", err))
	//	}
	//}
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, request)
	if err != nil {
		return nil, err
	}
	aiRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	//if common.DebugEnabled {
	//	println(fmt.Sprintf("convert claude to openai request result: %s", common.GetJsonString(aiRequest)))
	//	// Save request body to file for debugging
	//	bodyBytes := []byte(common.GetJsonString(aiRequest))
	//	err = os.WriteFile(fmt.Sprintf("claude_to_openai_request_%s.txt", c.GetString(common.RequestIdKey)), bodyBytes, 0644)
	//	if err != nil {
	//		println(fmt.Sprintf("failed to save request body to file: %v", err))
	//	}
	//}
	if info.SupportStreamOptions && info.IsStream {
		aiRequest.StreamOptions = &dto.StreamOptions{
			IncludeUsage: true,
		}
	}
	return a.ConvertOpenAIRequest(c, info, aiRequest)
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType

	// initialize ThinkingContentInfo when thinking_to_content is enabled
	if info.ChannelSetting.ThinkingToContent {
		info.ThinkingContentInfo = relaycommon.ThinkingContentInfo{
			IsFirstThinkingContent:  true,
			SendLastThinkingContent: false,
			HasSentThinkingContent:  false,
		}
	}
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.RelayMode == relayconstant.RelayModeRealtime {
		if strings.HasPrefix(info.ChannelBaseUrl, "https://") {
			baseUrl := strings.TrimPrefix(info.ChannelBaseUrl, "https://")
			baseUrl = "wss://" + baseUrl
			info.ChannelBaseUrl = baseUrl
		} else if strings.HasPrefix(info.ChannelBaseUrl, "http://") {
			baseUrl := strings.TrimPrefix(info.ChannelBaseUrl, "http://")
			baseUrl = "ws://" + baseUrl
			info.ChannelBaseUrl = baseUrl
		}
	}
	switch info.ChannelType {
	case constant.ChannelTypeAzure:
		apiVersion := info.ApiVersion
		if apiVersion == "" {
			apiVersion = constant.AzureDefaultAPIVersion
		}
		// 如果配置了模型特定的 API 版本，优先使用模型特定的版本（适用于普通 API 和 Responses API）
		if len(info.ChannelOtherSettings.AzureModelApiVersions) > 0 {
			if modelApiVersion, exists := info.ChannelOtherSettings.AzureModelApiVersions[info.UpstreamModelName]; exists && modelApiVersion != "" {
				apiVersion = modelApiVersion
			}
		}
		// https://learn.microsoft.com/en-us/azure/cognitive-services/openai/chatgpt-quickstart?pivots=rest-api&tabs=command-line#rest-api
		requestURL := strings.Split(info.RequestURLPath, "?")[0]
		requestURL = fmt.Sprintf("%s?api-version=%s", requestURL, apiVersion)
		task := strings.TrimPrefix(requestURL, "/v1/")

		if info.RelayFormat == types.RelayFormatClaude {
			task = strings.TrimPrefix(task, "messages")
			task = "chat/completions" + task
		}

		// 特殊处理 responses API（包含 compact）
		if info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact {
			responsesApiVersion := apiVersion

			subUrl := "/openai/v1/responses"
			if strings.Contains(info.ChannelBaseUrl, "cognitiveservices.azure.com") {
				subUrl = "/openai/responses"
			}

			// 优先级：模型特定 responses 版本 > 渠道默认 responses 版本（仅对未配置 per-model 普通版本的模型） > apiVersion
			if len(info.ChannelOtherSettings.AzureModelResponsesVersions) > 0 {
				if v, ok := info.ChannelOtherSettings.AzureModelResponsesVersions[info.UpstreamModelName]; ok && v != "" {
					responsesApiVersion = v
				} else if info.ChannelOtherSettings.AzureResponsesVersion != "" {
					if len(info.ChannelOtherSettings.AzureModelApiVersions) == 0 {
						responsesApiVersion = info.ChannelOtherSettings.AzureResponsesVersion
					} else if _, exists := info.ChannelOtherSettings.AzureModelApiVersions[info.UpstreamModelName]; !exists {
						responsesApiVersion = info.ChannelOtherSettings.AzureResponsesVersion
					}
				}
			} else if info.ChannelOtherSettings.AzureResponsesVersion != "" {
				if len(info.ChannelOtherSettings.AzureModelApiVersions) == 0 {
					responsesApiVersion = info.ChannelOtherSettings.AzureResponsesVersion
				} else if _, exists := info.ChannelOtherSettings.AzureModelApiVersions[info.UpstreamModelName]; !exists {
					responsesApiVersion = info.ChannelOtherSettings.AzureResponsesVersion
				}
			}

			// compact 模式追加 /compact
			if info.RelayMode == relayconstant.RelayModeResponsesCompact {
				subUrl = subUrl + "/compact"
			}

			requestURL = fmt.Sprintf("%s?api-version=%s", subUrl, responsesApiVersion)
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, requestURL, info.ChannelType), nil
		}

		model_ := info.UpstreamModelName
		// 2025年5月10日后创建的渠道不移除.
		if info.ChannelCreateTime < constant.AzureNoRemoveDotTime {
			model_ = strings.Replace(model_, ".", "", -1)
		}
		// https://github.com/songquanpeng/one-api/issues/67
		requestURL = fmt.Sprintf("/openai/deployments/%s/%s", model_, task)
		if info.RelayMode == relayconstant.RelayModeRealtime {
			// GA API（api-version 不含 preview）使用 /openai/v1/realtime?model=
			// Preview API 使用 /openai/realtime?deployment=&api-version=
			if strings.Contains(apiVersion, "preview") {
				requestURL = fmt.Sprintf("/openai/realtime?deployment=%s&api-version=%s", model_, apiVersion)
			} else {
				requestURL = fmt.Sprintf("/openai/v1/realtime?model=%s", model_)
			}
		}
		return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, requestURL, info.ChannelType), nil
	//case constant.ChannelTypeMiniMax:
	//	return minimax.GetRequestURL(info)
	case constant.ChannelTypeCustom:
		url := info.ChannelBaseUrl
		url = strings.Replace(url, "{model}", info.UpstreamModelName, -1)
		return url, nil
	default:
		if (info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini) &&
			info.RelayMode != relayconstant.RelayModeResponses &&
			info.RelayMode != relayconstant.RelayModeResponsesCompact {
			return fmt.Sprintf("%s/v1/chat/completions", info.ChannelBaseUrl), nil
		}
		return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	if info.ChannelType == constant.ChannelTypeAzure {
		header.Set("api-key", info.ApiKey)
		return nil
	}
	if info.ChannelType == constant.ChannelTypeOpenAI && "" != info.Organization {
		header.Set("OpenAI-Organization", info.Organization)
	}
	// 检查 Header Override 是否已设置 Authorization，如果已设置则跳过默认设置
	// 这样可以避免在 Header Override 应用时被覆盖（虽然 Header Override 会在之后应用，但这里作为额外保护）
	hasAuthOverride := false
	if len(info.HeadersOverride) > 0 {
		for k := range info.HeadersOverride {
			if strings.EqualFold(k, "Authorization") {
				hasAuthOverride = true
				break
			}
		}
	}
	if info.RelayMode == relayconstant.RelayModeRealtime {
		// OpenAI 已下线 Realtime Beta API,GA 模型收到 beta 标识会以 beta_api_shape_disabled 拒绝;
		// 仅对遗留 preview 模型保留 beta 标识
		legacyRealtimeBeta := strings.Contains(info.UpstreamModelName, "-realtime-preview")
		swp := c.Request.Header.Get("Sec-WebSocket-Protocol")
		if swp != "" {
			items := []string{
				"realtime",
				"openai-insecure-api-key." + info.ApiKey,
			}
			if legacyRealtimeBeta {
				items = append(items, "openai-beta.realtime-v1")
			}
			header.Set("Sec-WebSocket-Protocol", strings.Join(items, ","))
			//req.Header.Set("Sec-WebSocket-Key", c.Request.Header.Get("Sec-WebSocket-Key"))
			//req.Header.Set("Sec-Websocket-Extensions", c.Request.Header.Get("Sec-Websocket-Extensions"))
			//req.Header.Set("Sec-Websocket-Version", c.Request.Header.Get("Sec-Websocket-Version"))
		} else {
			if legacyRealtimeBeta {
				header.Set("openai-beta", "realtime=v1")
			}
			if !hasAuthOverride {
				header.Set("Authorization", "Bearer "+info.ApiKey)
			}
		}
	} else {
		if !hasAuthOverride {
			header.Set("Authorization", "Bearer "+info.ApiKey)
		}
	}
	if info.ChannelType == constant.ChannelTypeOpenRouter {
		if header.Get("HTTP-Referer") == "" {
			header.Set("HTTP-Referer", "https://www.newapi.ai")
		}
		if header.Get("X-OpenRouter-Title") == "" {
			header.Set("X-OpenRouter-Title", "New API")
		}
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if info.ChannelType != constant.ChannelTypeOpenAI && info.ChannelType != constant.ChannelTypeAzure {
		request.StreamOptions = nil
	}
	if info.ChannelType == constant.ChannelTypeOpenRouter {
		initialIntent, err := kitreasoning.FromOpenAIChat(request)
		if err != nil {
			return nil, kitreasoning.AsClientError(err)
		}
		if request.THINKING != nil && strings.HasPrefix(info.UpstreamModelName, "anthropic") {
			var thinking dto.Thinking
			if err := common.Unmarshal(request.THINKING, &thinking); err != nil {
				return nil, fmt.Errorf("error Unmarshal thinking: %w", err)
			}
			legacyIntent, err := kitreasoning.FromClaude(&dto.ClaudeRequest{Thinking: &thinking})
			if err != nil {
				return nil, kitreasoning.AsClientError(err)
			}
			initialIntent, err = kitreasoning.MergeExplicit(initialIntent, legacyIntent, request.Model)
			if err != nil {
				return nil, kitreasoning.AsClientError(err)
			}
			request.THINKING = nil
		}
		if len(request.Usage) == 0 {
			request.Usage = json.RawMessage(`{"include":true}`)
		}
		// 适配 OpenRouter 的 thinking 后缀
		preserveSuffix := model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) || model_setting.ShouldPreserveThinkingSuffix(info.UpstreamModelName)
		mergeEffortSuffix := func(modelName string) error {
			rawEffort, _ := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(modelName)
			if rawEffort == "" {
				return nil
			}
			effort, err := kitreasoning.ParseEffort(rawEffort)
			if err != nil {
				return err
			}
			mode := kitreasoning.ModeEnabled
			if effort == kitreasoning.EffortNone {
				mode = kitreasoning.ModeDisabled
			}
			initialIntent, err = kitreasoning.MergeExplicitAndSuffix(initialIntent, kitreasoning.Intent{Mode: mode, Effort: effort, Source: kitreasoning.SourceSuffix}, modelName)
			return err
		}
		if !preserveSuffix {
			if err := mergeEffortSuffix(info.UpstreamModelName); err != nil {
				return nil, kitreasoning.AsClientError(err)
			}
			if _, baseModel := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(info.UpstreamModelName); baseModel != info.UpstreamModelName {
				info.UpstreamModelName = baseModel
				request.Model = baseModel
			}
			if info.OriginModelName != info.UpstreamModelName {
				if err := mergeEffortSuffix(info.OriginModelName); err != nil {
					return nil, kitreasoning.AsClientError(err)
				}
			}
		}
		if !preserveSuffix && strings.HasSuffix(info.UpstreamModelName, "-thinking") {
			initialIntent, err = kitreasoning.MergeExplicitAndSuffix(
				initialIntent,
				kitreasoning.Intent{Mode: kitreasoning.ModeEnabled},
				info.UpstreamModelName,
			)
			if err != nil {
				return nil, kitreasoning.AsClientError(err)
			}
			info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-thinking")
			request.Model = info.UpstreamModelName
		}
		if !preserveSuffix && info.OriginModelName != info.UpstreamModelName && strings.HasSuffix(info.OriginModelName, "-thinking") {
			initialIntent, err = kitreasoning.MergeExplicitAndSuffix(
				initialIntent,
				kitreasoning.Intent{Mode: kitreasoning.ModeEnabled},
				info.OriginModelName,
			)
			if err != nil {
				return nil, kitreasoning.AsClientError(err)
			}
		}
		if !initialIntent.IsEmpty() {
			reasoningConfig := make(map[string]any)
			if len(request.Reasoning) > 0 {
				if err := common.Unmarshal(request.Reasoning, &reasoningConfig); err != nil {
					return nil, fmt.Errorf("error unmarshalling reasoning: %w", err)
				}
				if reasoningConfig == nil {
					reasoningConfig = make(map[string]any)
				}
			}
			disabled := initialIntent.Mode == kitreasoning.ModeDisabled || initialIntent.Effort == kitreasoning.EffortNone
			if initialIntent.HasStrength() {
				reasoningConfig["enabled"] = !disabled
				if disabled {
					delete(reasoningConfig, "effort")
					delete(reasoningConfig, "max_tokens")
				}
			}
			if !disabled && initialIntent.BudgetTokens != nil {
				reasoningConfig["max_tokens"] = *initialIntent.BudgetTokens
				delete(reasoningConfig, "effort")
			} else if !disabled && initialIntent.Effort != "" && initialIntent.Effort != kitreasoning.EffortNone {
				reasoningConfig["effort"] = string(initialIntent.Effort)
				delete(reasoningConfig, "max_tokens")
			}
			if initialIntent.IncludeThoughts != nil {
				reasoningConfig["exclude"] = !*initialIntent.IncludeThoughts
			}
			marshal, err := common.Marshal(reasoningConfig)
			if err != nil {
				return nil, fmt.Errorf("error marshalling reasoning: %w", err)
			}
			request.Reasoning = marshal
		}
		request.ReasoningEffort = ""
		effectiveEffort := kitreasoning.EffectiveEffort(initialIntent)
		if initialIntent.BudgetTokens != nil {
			effectiveEffort = kitreasoning.EffortFromBudget(*initialIntent.BudgetTokens)
		}
		info.SetReasoningEffort(string(effectiveEffort))

	}
	isOModel := dto.IsOpenAIReasoningOModel(info.UpstreamModelName)
	isGPT5Model := dto.IsOpenAIGPT5Model(info.UpstreamModelName)
	if isOModel || isGPT5Model {
		if lo.FromPtrOr(request.MaxCompletionTokens, uint(0)) == 0 && lo.FromPtrOr(request.MaxTokens, uint(0)) != 0 {
			request.MaxCompletionTokens = request.MaxTokens
			request.MaxTokens = nil
		}

		if isOModel {
			request.Temperature = nil
		}

		// gpt-5系列模型适配 归零不再支持的参数
		if isGPT5Model {
			request.Temperature = nil
			request.TopP = nil
			request.LogProbs = nil
		}

		// o系列模型developer适配（o1-mini除外）
		if !strings.HasPrefix(info.UpstreamModelName, "o1-mini") && !strings.HasPrefix(info.UpstreamModelName, "o1-preview") {
			//修改第一个Message的内容，将system改为developer
			if len(request.Messages) > 0 && request.Messages[0].Role == "system" {
				request.Messages[0].Role = "developer"
			}
		}
	}

	if info.ChannelType != constant.ChannelTypeOpenRouter {
		preserveSuffix := model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) || model_setting.ShouldPreserveThinkingSuffix(info.UpstreamModelName)
		effort, baseModel := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(info.UpstreamModelName)
		if preserveSuffix {
			effort = ""
		}
		currentIntent, err := kitreasoning.FromOpenAIChat(request)
		if err != nil {
			return nil, kitreasoning.AsClientError(err)
		}
		mergeSuffix := func(modelName, rawEffort string) error {
			if rawEffort == "" {
				return nil
			}
			suffixEffort, err := kitreasoning.ParseEffort(rawEffort)
			if err != nil {
				return err
			}
			mode := kitreasoning.ModeEnabled
			if suffixEffort == kitreasoning.EffortNone {
				mode = kitreasoning.ModeDisabled
			}
			currentIntent, err = kitreasoning.MergeExplicitAndSuffix(currentIntent, kitreasoning.Intent{Mode: mode, Effort: suffixEffort, Source: kitreasoning.SourceSuffix}, modelName)
			return err
		}
		if err := mergeSuffix(info.UpstreamModelName, effort); err != nil {
			return nil, kitreasoning.AsClientError(err)
		}
		if !preserveSuffix && info.OriginModelName != info.UpstreamModelName {
			originEffort, _ := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(info.OriginModelName)
			if err := mergeSuffix(info.OriginModelName, originEffort); err != nil {
				return nil, kitreasoning.AsClientError(err)
			}
		}
		if effort != "" {
			info.UpstreamModelName = baseModel
			request.Model = baseModel
		}
		if canonicalEffort := kitreasoning.OpenAIEffort(kitreasoning.EffectiveEffort(currentIntent)); canonicalEffort != "" {
			request.ReasoningEffort = string(canonicalEffort)
			info.SetReasoningEffort(string(canonicalEffort))
		}
		if info.ChannelType == constant.ChannelTypeOpenAI || info.ChannelType == constant.ChannelTypeAzure {
			request.Reasoning = nil
		}
	}

	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	a.ResponseFormat = request.ResponseFormat
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		jsonData, err := common.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("error marshalling object: %w", err)
		}
		return bytes.NewReader(jsonData), nil
	} else {
		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		writer.WriteField("model", request.Model)

		formData, err2 := common.ParseMultipartFormReusable(c)
		if err2 != nil {
			return nil, fmt.Errorf("error parsing multipart form: %w", err2)
		}

		// 打印类似 curl 命令格式的信息
		logger.LogDebug(c.Request.Context(), "--form 'model=\"%s\"'", request.Model)

		// 遍历表单字段并打印输出
		for key, values := range formData.Value {
			if key == "model" {
				continue
			}
			for _, value := range values {
				writer.WriteField(key, value)
				logger.LogDebug(c.Request.Context(), "--form '%s=\"%s\"'", key, value)
			}
		}

		// 从 formData 中获取文件
		fileHeaders := formData.File["file"]
		if len(fileHeaders) == 0 {
			return nil, errors.New("file is required")
		}

		// 使用 formData 中的第一个文件
		fileHeader := fileHeaders[0]
		logger.LogDebug(c.Request.Context(), "--form 'file=@\"%s\"' (size: %d bytes, content-type: %s)",
			fileHeader.Filename, fileHeader.Size, fileHeader.Header.Get("Content-Type"))

		file, err := fileHeader.Open()
		if err != nil {
			return nil, fmt.Errorf("error opening audio file: %v", err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("file", fileHeader.Filename)
		if err != nil {
			return nil, errors.New("create form file failed")
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, errors.New("copy file failed")
		}

		// 关闭 multipart 编写器以设置分界线
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		logger.LogDebug(c.Request.Context(), "--header 'Content-Type: %s'", writer.FormDataContentType())
		return &requestBody, nil
	}
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	// gpt-image 系列：白名单过滤，只保留支持的参数
	if strings.HasPrefix(request.Model, "gpt-image") {
		request.ResponseFormat = ""
		request.Style = nil
		request.ExtraFields = nil
		request.Background = nil
		request.Moderation = nil
		request.OutputFormat = nil
		request.OutputCompression = nil
		request.PartialImages = nil
		request.Watermark = nil
		request.User = nil
		request.WatermarkEnabled = nil
		request.UserId = nil

		// Extra 中只保留白名单参数
		if request.Extra != nil {
			allowedParams := map[string]bool{
				"model": true, "prompt": true, "n": true,
				"size": true, "quality": true, "input_fidelity": true,
				"image": true, "images": true,
			}
			for key := range request.Extra {
				if !allowedParams[key] {
					delete(request.Extra, key)
				}
			}
		}
	}

	switch info.RelayMode {
	case relayconstant.RelayModeImagesEdits:
		// 图生图：统一转为 multipart/form-data 格式（Azure OpenAI 要求）
		// 对于纯 JSON 请求（非 gpt-image 系列），直接透传无需转换。
		// gpt-image 系列不能走 JSON 透传：Azure /v1/images/edits 的 JSON 模式既不接受
		// 单数 `image` 字符串（报 Unknown parameter: 'image'），也不接受 `images` 字符串数组
		// （报 Invalid type for 'images[0]': expected an object, but got a string）；
		// 唯一稳定可用的是 multipart/form-data。强制走下方 multipart 转换路径。
		if isJSONRequest(c) && !strings.HasPrefix(request.Model, "gpt-image") {
			return request, nil
		}

		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		writer.WriteField("model", request.Model)
		// gpt-image 系列：在 multipart 转换路径里，prompt/size/quality/n/input_fidelity 必须
		// 在这里手动写入，否则 Azure /v1/images/edits 会报 "Missing required parameter: 'prompt'"。
		// 下面 multipart 输入分支的 skipFields 也依赖这里已经写过这些字段（避免重复写）。
		if request.Prompt != "" {
			writer.WriteField("prompt", request.Prompt)
		}
		if request.Size != "" {
			writer.WriteField("size", request.Size)
		}
		if request.Quality != "" {
			writer.WriteField("quality", request.Quality)
		}
		if request.N != nil && *request.N > 0 {
			writer.WriteField("n", fmt.Sprintf("%d", *request.N))
		}
		if request.InputFidelity != nil && *request.InputFidelity != "" {
			writer.WriteField("input_fidelity", *request.InputFidelity)
		}

		// 检测请求格式：JSON 还是 multipart
		// 注意：不能用 c.GetHeader("Content-Type") 判断，因为重试时 Header 可能已被上一轮修改为 multipart
		// 改用 request.Image 是否有数据来判断：JSON 请求解析后 Image/Extra 中有图片数据，
		// 而原生 multipart 请求中图片在 form files 里，Image 字段为空
		hasImageInDTO := (request.Image != nil && len(request.Image) > 0) ||
			(request.Extra != nil && (request.Extra["image"] != nil || request.Extra["images"] != nil))

		if hasImageInDTO {
			// JSON 格式：从 Image 或 Extra 中提取图片，转为 multipart 文件
			var imageStrings []string

			// 优先从 Image 字段获取（dto 中的 json.RawMessage，支持 string 或 []string）
			if request.Image != nil && len(request.Image) > 0 {
				// 先尝试解析为单图字符串
				var imageStr string
				if err := common.Unmarshal(request.Image, &imageStr); err == nil && imageStr != "" {
					imageStrings = append(imageStrings, imageStr)
				} else {
					// 再尝试解析为图片数组
					var imageArr []string
					if err := common.Unmarshal(request.Image, &imageArr); err == nil && len(imageArr) > 0 {
						imageStrings = append(imageStrings, imageArr...)
					}
				}
			}
			// 其次从 Extra["images"] 获取多图
			if len(imageStrings) == 0 && request.Extra != nil {
				if imagesData, ok := request.Extra["images"]; ok {
					var images []string
					if err := common.Unmarshal(imagesData, &images); err == nil {
						imageStrings = images
					}
				}
				// 也尝试 Extra["image"]（单图字符串或数组）
				if len(imageStrings) == 0 {
					if imageData, ok := request.Extra["image"]; ok {
						var imageStr string
						if err := common.Unmarshal(imageData, &imageStr); err == nil && imageStr != "" {
							imageStrings = append(imageStrings, imageStr)
						} else {
							var imageArr []string
							if err := common.Unmarshal(imageData, &imageArr); err == nil && len(imageArr) > 0 {
								imageStrings = append(imageStrings, imageArr...)
							}
						}
					}
				}
			}

			if len(imageStrings) == 0 {
				return nil, errors.New("image or images field is required for edits endpoint")
			}

			logger.LogInfo(c.Request.Context(), fmt.Sprintf("gpt-image edits JSON→multipart: image_count=%d", len(imageStrings)))

			// 处理所有图片：下载 URL 或解码 base64
			for i, imageStr := range imageStrings {
				// 日志：图片来源摘要（截断 base64）
				srcPreview := imageStr
				if len(srcPreview) > 100 {
					srcPreview = srcPreview[:100] + "...(truncated)"
				}
				logger.LogInfo(c.Request.Context(), fmt.Sprintf("gpt-image edits processing image[%d]: %s", i, srcPreview))

				imageBytes, mimeType, err := downloadOrDecodeImage(imageStr)
				if err != nil {
					return nil, fmt.Errorf("failed to process image %d: %w", i, err)
				}

				// 根据 MIME 类型确定文件扩展名
				filename := fmt.Sprintf("image%d.png", i)
				if mimeType == "image/jpeg" {
					filename = fmt.Sprintf("image%d.jpg", i)
				} else if mimeType == "image/webp" {
					filename = fmt.Sprintf("image%d.webp", i)
				}

				// 统一使用 image[] 字段名
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="%s"`, filename))
				h.Set("Content-Type", mimeType)

				part, err := writer.CreatePart(h)
				if err != nil {
					return nil, fmt.Errorf("failed to create form file %d: %w", i, err)
				}
				if _, err := part.Write(imageBytes); err != nil {
					return nil, fmt.Errorf("failed to write image data %d: %w", i, err)
				}
				logger.LogInfo(c.Request.Context(), fmt.Sprintf("gpt-image edits image[%d] written: filename=%s, mime=%s, size=%d bytes", i, filename, mimeType, len(imageBytes)))
			}
		} else {
			// multipart/form-data 格式：使用已解析的 multipart 表单
			mf := c.Request.MultipartForm
			if mf == nil {
				if _, err := c.MultipartForm(); err != nil {
					return nil, errors.New("failed to parse multipart form")
				}
				mf = c.Request.MultipartForm
			}

			// 写入非文件字段（跳过已手动写入的字段）
			if mf != nil {
				skipFields := map[string]bool{
					"model": true, "prompt": true, "size": true,
					"quality": true, "n": true, "input_fidelity": true,
				}
				for key, values := range mf.Value {
					if skipFields[key] {
						continue
					}
					for _, value := range values {
						writer.WriteField(key, value)
					}
				}
			}

			if mf != nil && mf.File != nil {
				// 查找 image 文件（支持 image / image[] / image[N] 命名）
				var imageFiles []*multipart.FileHeader
				var exists bool

				if imageFiles, exists = mf.File["image"]; !exists || len(imageFiles) == 0 {
					if imageFiles, exists = mf.File["image[]"]; !exists || len(imageFiles) == 0 {
						foundArrayImages := false
						for fieldName, files := range mf.File {
							if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
								foundArrayImages = true
								imageFiles = append(imageFiles, files...)
							}
						}
						if !foundArrayImages && len(imageFiles) == 0 {
							return nil, errors.New("image is required")
						}
					}
				}

				// 写入所有图片文件
				for i, fileHeader := range imageFiles {
					file, err := fileHeader.Open()
					if err != nil {
						return nil, fmt.Errorf("failed to open image file %d: %w", i, err)
					}

					fieldName := "image"
					if len(imageFiles) > 1 {
						fieldName = "image[]"
					}

					mimeType := detectImageMimeType(fileHeader.Filename)
					h := make(textproto.MIMEHeader)
					h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileHeader.Filename))
					h.Set("Content-Type", mimeType)

					part, err := writer.CreatePart(h)
					if err != nil {
						return nil, fmt.Errorf("create form part failed for image %d: %w", i, err)
					}
					if _, err := io.Copy(part, file); err != nil {
						return nil, fmt.Errorf("copy file failed for image %d: %w", i, err)
					}
					_ = file.Close()
				}

				// 处理 mask 文件
				if maskFiles, exists := mf.File["mask"]; exists && len(maskFiles) > 0 {
					maskFile, err := maskFiles[0].Open()
					if err != nil {
						return nil, errors.New("failed to open mask file")
					}

					mimeType := detectImageMimeType(maskFiles[0].Filename)
					h := make(textproto.MIMEHeader)
					h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="mask"; filename="%s"`, maskFiles[0].Filename))
					h.Set("Content-Type", mimeType)

					maskPart, err := writer.CreatePart(h)
					if err != nil {
						return nil, errors.New("create form file failed for mask")
					}
					if _, err := io.Copy(maskPart, maskFile); err != nil {
						return nil, errors.New("copy mask file failed")
					}
					_ = maskFile.Close()
				}
			} else {
				return nil, errors.New("no multipart form data found")
			}
		}

		// 日志：记录本次构造 multipart 请求时使用的完整参数，长字段（如 base64）按字段截断。
		if requestBytes, err := common.Marshal(request); err == nil {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("gpt-image edits upstream full params: %s", common.TruncateJsonValues(string(requestBytes))))
		}

		// 日志：记录发送到上游的 multipart 表单参数摘要
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("gpt-image edits upstream request: model=%s, prompt=%q, size=%s, quality=%s, n=%v, input_fidelity=%q, content-type=%s, body_size=%d",
			request.Model, request.Prompt, request.Size, request.Quality,
			func() string {
				if request.N != nil {
					return fmt.Sprintf("%d", *request.N)
				}
				return "nil"
			}(),
			lo.FromPtr(request.InputFidelity), writer.FormDataContentType(), requestBody.Len()))

		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return &requestBody, nil

	default:
		return request, nil
	}
}

func isJSONRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json")
}

// detectImageMimeType determines the MIME type based on the file extension
func detectImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		if strings.HasPrefix(ext, ".jp") {
			return "image/jpeg"
		}
		return "image/png"
	}
}

// maxImageSize 单张图片最大 50MB
const maxImageSize = 50 * 1024 * 1024

// downloadOrDecodeImage 下载图片 URL 或解码 base64 图片
// 返回：图片字节、MIME 类型、错误
func downloadOrDecodeImage(imageData string) ([]byte, string, error) {
	if strings.HasPrefix(imageData, "data:image") {
		// base64 data URI 解码
		parts := strings.SplitN(imageData, ",", 2)
		if len(parts) != 2 {
			return nil, "", errors.New("invalid base64 image format")
		}

		// 提取 MIME 类型
		mimeType := "image/png"
		if strings.Contains(parts[0], "image/jpeg") || strings.Contains(parts[0], "image/jpg") {
			mimeType = "image/jpeg"
		} else if strings.Contains(parts[0], "image/png") {
			mimeType = "image/png"
		} else if strings.Contains(parts[0], "image/webp") {
			mimeType = "image/webp"
		}

		imageBytes, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			// 兼容缺少 padding 的 base64
			imageBytes, err = base64.RawStdEncoding.DecodeString(parts[1])
			if err != nil {
				return nil, "", fmt.Errorf("failed to decode base64: %w", err)
			}
		}
		if len(imageBytes) > maxImageSize {
			return nil, "", fmt.Errorf("image too large: %d bytes, max %d bytes", len(imageBytes), maxImageSize)
		}
		return imageBytes, mimeType, nil
	} else if strings.HasPrefix(imageData, "http://") || strings.HasPrefix(imageData, "https://") {
		// URL 下载（30s 超时，限制读取 50MB）
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(imageData)
		if err != nil {
			return nil, "", fmt.Errorf("failed to download image: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("failed to download image: status %d", resp.StatusCode)
		}

		mimeType := resp.Header.Get("Content-Type")
		if mimeType == "" || mimeType == "application/octet-stream" {
			mimeType = detectMimeTypeFromURL(imageData)
		}

		imageBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
		if err != nil {
			return nil, "", fmt.Errorf("failed to read image: %w", err)
		}
		if len(imageBytes) > maxImageSize {
			return nil, "", fmt.Errorf("image too large: exceeds %d bytes limit", maxImageSize)
		}
		return imageBytes, mimeType, nil
	}

	return nil, "", errors.New("image must be a URL or base64 data URI")
}

// detectMimeTypeFromURL 从 URL 推断 MIME 类型
func detectMimeTypeFromURL(url string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, ".jpg") || strings.Contains(lower, ".jpeg") {
		return "image/jpeg"
	} else if strings.Contains(lower, ".png") {
		return "image/png"
	} else if strings.Contains(lower, ".webp") {
		return "image/webp"
	}
	return "image/png"
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	//  转换模型推理力度后缀
	effort, originModel := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(request.Model)
	preserveSuffix := model_setting.ShouldPreserveThinkingSuffix(request.Model) || (info != nil && model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName))
	if preserveSuffix {
		effort = ""
	}
	currentIntent, err := kitreasoning.FromOpenAIResponses(&request)
	if err != nil {
		return nil, kitreasoning.AsClientError(err)
	}
	mergeSuffix := func(modelName, rawEffort string) error {
		if rawEffort == "" {
			return nil
		}
		suffixEffort, err := kitreasoning.ParseEffort(rawEffort)
		if err != nil {
			return err
		}
		mode := kitreasoning.ModeEnabled
		if suffixEffort == kitreasoning.EffortNone {
			mode = kitreasoning.ModeDisabled
		}
		currentIntent, err = kitreasoning.MergeExplicitAndSuffix(currentIntent, kitreasoning.Intent{Mode: mode, Effort: suffixEffort, Source: kitreasoning.SourceSuffix}, modelName)
		return err
	}
	if err := mergeSuffix(request.Model, effort); err != nil {
		return nil, kitreasoning.AsClientError(err)
	}
	if !preserveSuffix && info != nil && info.OriginModelName != request.Model {
		originEffort, _ := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(info.OriginModelName)
		if err := mergeSuffix(info.OriginModelName, originEffort); err != nil {
			return nil, kitreasoning.AsClientError(err)
		}
	}
	if effort != "" {
		request.Model = originModel
		if info != nil {
			info.UpstreamModelName = originModel
		}
	}
	if canonicalEffort := kitreasoning.OpenAIEffort(kitreasoning.EffectiveEffort(currentIntent)); canonicalEffort != "" {
		if request.Reasoning == nil {
			request.Reasoning = &dto.Reasoning{}
		}
		request.Reasoning.Effort = string(canonicalEffort)
		if info != nil {
			info.SetReasoningEffort(string(canonicalEffort))
		}
	}
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode == relayconstant.RelayModeAudioTranscription ||
		info.RelayMode == relayconstant.RelayModeAudioTranslation ||
		(info.RelayMode == relayconstant.RelayModeImagesEdits && !isJSONRequest(c)) {
		return channel.DoFormRequest(a, c, info, requestBody)
	} else if info.RelayMode == relayconstant.RelayModeRealtime {
		return channel.DoWssRequest(a, c, info, requestBody)
	} else {
		return channel.DoApiRequest(a, c, info, requestBody)
	}
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeRealtime:
		err, usage = OpenaiRealtimeHandler(c, info)
	case relayconstant.RelayModeAudioSpeech:
		usage = OpenaiTTSHandler(c, resp, info)
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err, usage = OpenaiSTTHandler(c, resp, info, a.ResponseFormat)
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		if info.IsStream {
			usage, err = OpenaiImageStreamHandler(c, info, resp)
		} else {
			usage, err = OpenaiImageHandler(c, info, resp)
		}
	case relayconstant.RelayModeRerank:
		usage, err = common_handler.RerankHandler(c, info, resp)
	case relayconstant.RelayModeResponses:
		if info.IsStream {
			usage, err = OaiResponsesStreamHandler(c, info, resp)
		} else {
			usage, err = OaiResponsesHandler(c, info, resp)
		}
	case relayconstant.RelayModeResponsesCompact:
		usage, err = OaiResponsesCompactionHandler(c, resp)
	default:
		if info.IsStream {
			usage, err = OaiStreamHandler(c, info, resp)
		} else {
			usage, err = OpenaiHandler(c, info, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	switch a.ChannelType {
	case constant.ChannelType360:
		return ai360.ModelList
	case constant.ChannelTypeLingYiWanWu:
		return lingyiwanwu.ModelList
	//case constant.ChannelTypeMiniMax:
	//	return minimax.ModelList
	case constant.ChannelTypeXinference:
		return xinference.ModelList
	case constant.ChannelTypeOpenRouter:
		return openrouter.ModelList
	default:
		return ModelList
	}
}

func (a *Adaptor) GetChannelName() string {
	switch a.ChannelType {
	case constant.ChannelType360:
		return ai360.ChannelName
	case constant.ChannelTypeLingYiWanWu:
		return lingyiwanwu.ChannelName
	//case constant.ChannelTypeMiniMax:
	//	return minimax.ChannelName
	case constant.ChannelTypeXinference:
		return xinference.ChannelName
	case constant.ChannelTypeOpenRouter:
		return openrouter.ChannelName
	default:
		return ChannelName
	}
}
