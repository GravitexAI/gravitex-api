package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// https://cloud.google.com/vertex-ai/generative-ai/docs/model-reference/inference?hl=zh-cn#blob
var geminiSupportedMimeTypes = map[string]bool{
	"application/pdf": true,
	"audio/mpeg":      true,
	"audio/mp3":       true,
	"audio/wav":       true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/jpg":       true, // support old image/jpeg
	"image/webp":      true,
	"image/heic":      true,
	"image/heif":      true,
	"text/plain":      true,
	"video/mov":       true,
	"video/mpeg":      true,
	"video/mp4":       true,
	"video/mpg":       true,
	"video/avi":       true,
	"video/wmv":       true,
	"video/mpegps":    true,
	"video/flv":       true,
}

const thoughtSignatureBypassValue = "context_engineering_is_the_way_to_go"

// Gemini 允许的思考预算范围
const (
	pro25MinBudget       = 128
	pro25MaxBudget       = 32768
	flash25MaxBudget     = 24576
	flash25LiteMinBudget = 512
	flash25LiteMaxBudget = 24576
)

func isNew25ProModel(modelName string) bool {
	return strings.HasPrefix(modelName, "gemini-2.5-pro") &&
		!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-05-06") &&
		!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-03-25")
}

func is25FlashLiteModel(modelName string) bool {
	return strings.HasPrefix(modelName, "gemini-2.5-flash-lite")
}

// clampThinkingBudget 根据模型名称将预算限制在允许的范围内
func clampThinkingBudget(modelName string, budget int) int {
	isNew25Pro := isNew25ProModel(modelName)
	is25FlashLite := is25FlashLiteModel(modelName)

	if is25FlashLite {
		if budget < flash25LiteMinBudget {
			return flash25LiteMinBudget
		}
		if budget > flash25LiteMaxBudget {
			return flash25LiteMaxBudget
		}
	} else if isNew25Pro {
		if budget < pro25MinBudget {
			return pro25MinBudget
		}
		if budget > pro25MaxBudget {
			return pro25MaxBudget
		}
	} else { // 其他模型
		if budget < 0 {
			return 0
		}
		if budget > flash25MaxBudget {
			return flash25MaxBudget
		}
	}
	return budget
}

// "effort": "high" - Allocates a large portion of tokens for reasoning (approximately 80% of max_tokens)
// "effort": "medium" - Allocates a moderate portion of tokens (approximately 50% of max_tokens)
// "effort": "low" - Allocates a smaller portion of tokens (approximately 20% of max_tokens)
// "effort": "minimal" - Allocates a minimal portion of tokens (approximately 5% of max_tokens)
func clampThinkingBudgetByEffort(modelName string, effort string) int {
	isNew25Pro := isNew25ProModel(modelName)
	is25FlashLite := is25FlashLiteModel(modelName)

	maxBudget := 0
	if is25FlashLite {
		maxBudget = flash25LiteMaxBudget
	}
	if isNew25Pro {
		maxBudget = pro25MaxBudget
	} else {
		maxBudget = flash25MaxBudget
	}
	switch effort {
	case "high":
		maxBudget = maxBudget * 80 / 100
	case "medium":
		maxBudget = maxBudget * 50 / 100
	case "low":
		maxBudget = maxBudget * 20 / 100
	case "minimal":
		maxBudget = maxBudget * 5 / 100
	}
	return clampThinkingBudget(modelName, maxBudget)
}

func ThinkingAdaptor(geminiRequest *dto.GeminiChatRequest, info *relaycommon.RelayInfo, oaiRequest ...dto.GeneralOpenAIRequest) {
	if model_setting.GetGeminiSettings().ThinkingAdapterEnabled {
		modelName := info.UpstreamModelName
		isNew25Pro := strings.HasPrefix(modelName, "gemini-2.5-pro") &&
			!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-05-06") &&
			!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-03-25")

		if strings.Contains(modelName, "-thinking-") {
			parts := strings.SplitN(modelName, "-thinking-", 2)
			if len(parts) == 2 && parts[1] != "" {
				if budgetTokens, err := strconv.Atoi(parts[1]); err == nil {
					clampedBudget := clampThinkingBudget(modelName, budgetTokens)
					geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
						ThinkingBudget:  common.GetPointer(clampedBudget),
						IncludeThoughts: true,
					}
				}
			}
		} else if strings.HasSuffix(modelName, "-thinking") {
			unsupportedModels := []string{
				"gemini-2.5-pro-preview-05-06",
				"gemini-2.5-pro-preview-03-25",
			}
			isUnsupported := false
			for _, unsupportedModel := range unsupportedModels {
				if strings.HasPrefix(modelName, unsupportedModel) {
					isUnsupported = true
					break
				}
			}

			if isUnsupported {
				geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
					IncludeThoughts: true,
				}
			} else {
				geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
					IncludeThoughts: true,
				}
				if geminiRequest.GenerationConfig.MaxOutputTokens != nil && *geminiRequest.GenerationConfig.MaxOutputTokens > 0 {
					budgetTokens := model_setting.GetGeminiSettings().ThinkingAdapterBudgetTokensPercentage * float64(*geminiRequest.GenerationConfig.MaxOutputTokens)
					clampedBudget := clampThinkingBudget(modelName, int(budgetTokens))
					geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = common.GetPointer(clampedBudget)
				} else {
					if len(oaiRequest) > 0 {
						// 如果有reasoningEffort参数，则根据其值设置思考预算
						geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = common.GetPointer(clampThinkingBudgetByEffort(modelName, oaiRequest[0].ReasoningEffort))
					}
				}
			}
		} else if strings.HasSuffix(modelName, "-nothinking") {
			if !isNew25Pro {
				geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
					ThinkingBudget: common.GetPointer(0),
				}
			}
		} else if _, level, ok := reasoning.TrimEffortSuffix(info.UpstreamModelName); ok && level != "" {
			geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
				IncludeThoughts: true,
				ThinkingLevel:   level,
			}
			info.ReasoningEffort = level
		} else if len(oaiRequest) > 0 && oaiRequest[0].ReasoningEffort != "" {
			// 客户端通过 OpenAI 的 reasoning_effort 参数直接请求思考（无模型后缀场景）。
			// 必须显式设置 IncludeThoughts=true，否则上游（Gemini/Vertex）只在 usage 中计费
			// thoughtsTokenCount，却不会回传思考摘要，导致 reasoning_tokens 有值但 reasoning_content 为空。
			effort := oaiRequest[0].ReasoningEffort
			thinkingConfig := &dto.GeminiThinkingConfig{IncludeThoughts: true}
			if strings.HasPrefix(modelName, "gemini-3") {
				// Gemini 3.x 使用 thinkingLevel（low/high），不再使用 thinkingBudget
				thinkingConfig.ThinkingLevel = mapEffortToGeminiThinkingLevel(effort)
			} else {
				thinkingConfig.ThinkingBudget = common.GetPointer(clampThinkingBudgetByEffort(modelName, effort))
			}
			geminiRequest.GenerationConfig.ThinkingConfig = thinkingConfig
			info.ReasoningEffort = effort
		} else if geminiThinkingOnByDefault(modelName) {
			// 默认开启思考摘要输出：这些模型本身默认就会思考（usage 中会产生 thoughtsTokenCount），
			// 但只有 includeThoughts=true 时上游才会回传思考内容。此处兜底设为 true，避免出现
			// reasoning_tokens 有值而 reasoning_content 为空的情况。
			// 若客户端想关闭，可通过 extra_body.google.thinking_config.include_thoughts=false 显式禁用
			// （该路径会跳过 ThinkingAdaptor，不会进入此分支）。
			geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
				IncludeThoughts: true,
			}
		}
	}
}

// geminiThinkingOnByDefault 判断该模型是否「默认开启思考」，用于在客户端未显式指定思考参数时
// 兜底打开 includeThoughts。仅覆盖默认会思考的文本模型（gemini-2.5-pro / 2.5-flash / 3.x），
// 排除不支持 thinkingConfig 的模型（2.0 / gemma / 图像 / TTS / 音频 / 向量 / 机器人 / computer-use /
// flash-lite 等），避免向不支持思考的上游发送 thinkingConfig 导致 400。
func geminiThinkingOnByDefault(modelName string) bool {
	if model_setting.IsGeminiModelSupportImagine(modelName) {
		return false
	}
	for _, kw := range []string{"lite", "image", "tts", "audio", "embedding", "robotics", "computer-use", "nothinking"} {
		if strings.Contains(modelName, kw) {
			return false
		}
	}
	return strings.HasPrefix(modelName, "gemini-3") ||
		strings.HasPrefix(modelName, "gemini-2.5-pro") ||
		strings.HasPrefix(modelName, "gemini-2.5-flash")
}

// mapEffortToGeminiThinkingLevel 将 OpenAI 的 reasoning_effort 映射为 Gemini 3.x 的 thinkingLevel。
// Gemini 3.x 当前仅支持 low / high 两档。
func mapEffortToGeminiThinkingLevel(effort string) string {
	switch effort {
	case "minimal", "low":
		return "low"
	case "medium", "high", "xhigh", "max":
		return "high"
	default:
		return ""
	}
}

// Setting safety to the lowest possible values since Gemini is already powerless enough
//
// CHZ-PATCH(gemini-extra-body-passthrough): 返回值统一为 any——
//   - 通常情况下返回 *dto.GeminiChatRequest 结构体（向后兼容）。
//   - 当 textRequest.ExtraBody.google 非空时，返回 map[string]any —— 把 extra_body.google.*
//     全量深度合并到上游 Gemini 请求（无 schema 校验，由调用方按 Gemini 原生字段名书写），
//     用于完全透传 generationConfig / safetySettings / tools / systemInstruction 等任意字段。
//
// 调用方（gemini/adaptor.go、vertex/adaptor.go）均通过 `:=` 接收且直接 `return geminiRequest, nil`，
// 不依赖具体类型，签名扩宽到 any 不破坏现有调用。
func CovertOpenAI2Gemini(c *gin.Context, textRequest dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo) (any, error) {

	// CHZ-PATCH(gemini-imagine-no-stream): Gemini imagine 模型（nano banana 等）官方不支持流式输出，
	// 客户端若传 stream:true 在此自动降级为非流式：上游走 :generateContent，下游走 GeminiChatHandler。
	// 放在 CovertOpenAI2Gemini 入口处，Gemini + Vertex 两个通道一次覆盖。
	if model_setting.IsGeminiModelSupportImagine(info.UpstreamModelName) && info.IsStream {
		info.IsStream = false
		textRequest.Stream = nil
		if textRequest.StreamOptions != nil {
			textRequest.StreamOptions = nil
		}
	}

	geminiRequest := dto.GeminiChatRequest{
		Contents: make([]dto.GeminiChatContent, 0, len(textRequest.Messages)),
		GenerationConfig: dto.GeminiChatGenerationConfig{
			Temperature: textRequest.Temperature,
		},
	}

	if textRequest.TopP != nil && *textRequest.TopP > 0 {
		geminiRequest.GenerationConfig.TopP = common.GetPointer(*textRequest.TopP)
	}

	if maxTokens := textRequest.GetMaxTokens(); maxTokens > 0 {
		geminiRequest.GenerationConfig.MaxOutputTokens = common.GetPointer(maxTokens)
	}

	if textRequest.Seed != nil && *textRequest.Seed != 0 {
		geminiSeed := int64(lo.FromPtr(textRequest.Seed))
		geminiRequest.GenerationConfig.Seed = common.GetPointer(geminiSeed)
	}

	// CHZ-PATCH(gemini-param-mapping): Map OpenAI standard parameters to Gemini
	if textRequest.FrequencyPenalty != nil {
		fp := float32(*textRequest.FrequencyPenalty)
		geminiRequest.GenerationConfig.FrequencyPenalty = &fp
	}
	if textRequest.PresencePenalty != nil {
		pp := float32(*textRequest.PresencePenalty)
		geminiRequest.GenerationConfig.PresencePenalty = &pp
	}
	if textRequest.TopK != nil {
		tk := float64(*textRequest.TopK)
		geminiRequest.GenerationConfig.TopK = &tk
	}
	if textRequest.N != nil && *textRequest.N > 1 {
		geminiRequest.GenerationConfig.CandidateCount = textRequest.N
	}
	if textRequest.LogProbs != nil {
		geminiRequest.GenerationConfig.ResponseLogprobs = textRequest.LogProbs
	}
	if textRequest.TopLogProbs != nil {
		tlp := int32(*textRequest.TopLogProbs)
		geminiRequest.GenerationConfig.Logprobs = &tlp
	}
	if len(textRequest.Modalities) > 0 && !model_setting.IsGeminiModelSupportImagine(info.UpstreamModelName) {
		var modalities []string
		if err := common.Unmarshal(textRequest.Modalities, &modalities); err == nil && len(modalities) > 0 {
			geminiRequest.GenerationConfig.ResponseModalities = modalities
		}
	}
	if len(textRequest.Audio) > 0 {
		geminiRequest.GenerationConfig.SpeechConfig = textRequest.Audio
	}

	attachThoughtSignature := (info.ChannelType == constant.ChannelTypeGemini ||
		info.ChannelType == constant.ChannelTypeVertexAi) &&
		model_setting.GetGeminiSettings().FunctionCallThoughtSignatureEnabled

	if model_setting.IsGeminiModelSupportImagine(info.UpstreamModelName) {
		geminiRequest.GenerationConfig.ResponseModalities = []string{
			"TEXT",
			"IMAGE",
		}
	}
	if stopSequences := parseStopSequences(textRequest.Stop); len(stopSequences) > 0 {
		// Gemini supports up to 5 stop sequences
		if len(stopSequences) > 5 {
			stopSequences = stopSequences[:5]
		}
		geminiRequest.GenerationConfig.StopSequences = stopSequences
	}

	adaptorWithExtraBody := false

	// patch extra_body
	if len(textRequest.ExtraBody) > 0 {
		var extraBody map[string]interface{}
		if err := common.Unmarshal(textRequest.ExtraBody, &extraBody); err != nil {
			return nil, fmt.Errorf("invalid extra body: %w", err)
		}

		// eg. {"google":{"thinking_config":{"thinking_budget":5324,"include_thoughts":true}}}
		if googleBody, ok := extraBody["google"].(map[string]interface{}); ok {
			// CHZ-PATCH(gemini-extra-body-passthrough): 只要传了 extra_body.google 就关闭自动
			// ThinkingAdaptor，把控制权完全交给调用方。snake_case 白名单路径保留兜底，camelCase
			// 字段通过末尾的 applyExtraBodyGooglePatch 全量透传给上游 Gemini。
			adaptorWithExtraBody = true
			if !strings.HasSuffix(info.UpstreamModelName, "-nothinking") {

				if thinkingConfig, ok := googleBody["thinking_config"].(map[string]interface{}); ok {
					var hasThinkingConfig bool
					var tempThinkingConfig dto.GeminiThinkingConfig

					if thinkingBudget, exists := thinkingConfig["thinking_budget"]; exists {
						// 非 float64（含 camelCase 透传分支）静默跳过，由末尾 patch 走原生路径
						if v, ok := thinkingBudget.(float64); ok {
							budgetInt := int(v)
							tempThinkingConfig.ThinkingBudget = common.GetPointer(budgetInt)
							tempThinkingConfig.IncludeThoughts = budgetInt > 0
							hasThinkingConfig = true
						}
					}

					if includeThoughts, exists := thinkingConfig["include_thoughts"]; exists {
						if v, ok := includeThoughts.(bool); ok {
							tempThinkingConfig.IncludeThoughts = v
							hasThinkingConfig = true
						}
					}
					if thinkingLevel, exists := thinkingConfig["thinking_level"]; exists {
						if v, ok := thinkingLevel.(string); ok {
							tempThinkingConfig.ThinkingLevel = v
							hasThinkingConfig = true
						}
					}

					if hasThinkingConfig {
						// 避免 panic: 仅在获得配置时分配，防止后续赋值时空指针
						if geminiRequest.GenerationConfig.ThinkingConfig == nil {
							geminiRequest.GenerationConfig.ThinkingConfig = &tempThinkingConfig
						} else {
							// 如果已分配，则合并内容
							if tempThinkingConfig.ThinkingBudget != nil {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = tempThinkingConfig.ThinkingBudget
							}
							geminiRequest.GenerationConfig.ThinkingConfig.IncludeThoughts = tempThinkingConfig.IncludeThoughts
							if tempThinkingConfig.ThinkingLevel != "" {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingLevel = tempThinkingConfig.ThinkingLevel
							}
						}
					}
				}
			}

			if imageConfig, ok := googleBody["image_config"].(map[string]interface{}); ok {
				// convert snake_case to camelCase for Gemini API
				geminiImageConfig := make(map[string]interface{})
				if aspectRatio, ok := imageConfig["aspect_ratio"]; ok {
					geminiImageConfig["aspectRatio"] = aspectRatio
				}
				if imageSize, ok := imageConfig["image_size"]; ok {
					geminiImageConfig["imageSize"] = imageSize
				}

				if len(geminiImageConfig) > 0 {
					imageConfigBytes, err := common.Marshal(geminiImageConfig)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal image_config: %w", err)
					}
					geminiRequest.GenerationConfig.ImageConfig = imageConfigBytes
				}
			}
		}
	}

	if !adaptorWithExtraBody {
		ThinkingAdaptor(&geminiRequest, info, textRequest)
	}

	safetySettings := make([]dto.GeminiChatSafetySettings, 0, len(SafetySettingList))
	for _, category := range SafetySettingList {
		safetySettings = append(safetySettings, dto.GeminiChatSafetySettings{
			Category:  category,
			Threshold: model_setting.GetGeminiSafetySetting(category),
		})
	}
	geminiRequest.SafetySettings = safetySettings

	// openaiContent.FuncToToolCalls()
	if textRequest.Tools != nil {
		functions := make([]dto.FunctionRequest, 0, len(textRequest.Tools))
		googleSearch := false
		codeExecution := false
		urlContext := false
		for _, tool := range textRequest.Tools {
			if tool.Function.Name == "googleSearch" {
				googleSearch = true
				continue
			}
			if tool.Function.Name == "codeExecution" {
				codeExecution = true
				continue
			}
			if tool.Function.Name == "urlContext" {
				urlContext = true
				continue
			}
			if tool.Function.Parameters != nil {

				params, ok := tool.Function.Parameters.(map[string]interface{})
				if ok {
					if props, hasProps := params["properties"].(map[string]interface{}); hasProps {
						if len(props) == 0 {
							tool.Function.Parameters = nil
						}
					}
				}
			}
			// Clean the parameters before appending
			cleanedParams := cleanFunctionParameters(tool.Function.Parameters)
			tool.Function.Parameters = cleanedParams
			functions = append(functions, tool.Function)
		}
		geminiTools := geminiRequest.GetTools()
		if codeExecution {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				CodeExecution: make(map[string]string),
			})
		}
		if googleSearch {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				GoogleSearch: make(map[string]string),
			})
		}
		if urlContext {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				URLContext: make(map[string]string),
			})
		}
		if len(functions) > 0 {
			geminiTools = append(geminiTools, dto.GeminiChatTool{
				FunctionDeclarations: functions,
			})
		}
		geminiRequest.SetTools(geminiTools)

		// [NEW] Convert OpenAI tool_choice to Gemini toolConfig.functionCallingConfig
		// Mapping: "auto" -> "AUTO", "none" -> "NONE", "required" -> "ANY"
		// Object format: {"type": "function", "function": {"name": "xxx"}} -> "ANY" + allowedFunctionNames
		if textRequest.ToolChoice != nil {
			geminiRequest.ToolConfig = convertToolChoiceToGeminiConfig(textRequest.ToolChoice)
		}
	}

	if textRequest.ResponseFormat != nil && (textRequest.ResponseFormat.Type == "json_schema" || textRequest.ResponseFormat.Type == "json_object") {
		geminiRequest.GenerationConfig.ResponseMimeType = "application/json"

		if len(textRequest.ResponseFormat.JsonSchema) > 0 {
			// 先将json.RawMessage解析
			var jsonSchema dto.FormatJsonSchema
			if err := common.Unmarshal(textRequest.ResponseFormat.JsonSchema, &jsonSchema); err == nil {
				cleanedSchema := removeAdditionalPropertiesWithDepth(jsonSchema.Schema, 0)
				geminiRequest.GenerationConfig.ResponseSchema = cleanedSchema
			}
		}
	}
	tool_call_ids := make(map[string]string)
	var system_content []string
	//shouldAddDummyModelMessage := false
	for _, message := range textRequest.Messages {
		if message.Role == "system" || message.Role == "developer" {
			system_content = append(system_content, message.StringContent())
			continue
		} else if message.Role == "tool" || message.Role == "function" {
			if len(geminiRequest.Contents) == 0 || geminiRequest.Contents[len(geminiRequest.Contents)-1].Role == "model" {
				geminiRequest.Contents = append(geminiRequest.Contents, dto.GeminiChatContent{
					Role: "user",
				})
			}
			var parts = &geminiRequest.Contents[len(geminiRequest.Contents)-1].Parts
			name := ""
			if message.Name != nil {
				name = *message.Name
			} else if val, exists := tool_call_ids[message.ToolCallId]; exists {
				name = val
			}
			var contentMap map[string]interface{}
			contentStr := message.StringContent()

			// 1. 尝试解析为 JSON 对象
			if err := json.Unmarshal([]byte(contentStr), &contentMap); err != nil {
				// 2. 如果失败，尝试解析为 JSON 数组
				var contentSlice []interface{}
				if err := json.Unmarshal([]byte(contentStr), &contentSlice); err == nil {
					// 如果是数组，包装成对象
					contentMap = map[string]interface{}{"result": contentSlice}
				} else {
					// 3. 如果再次失败，作为纯文本处理
					contentMap = map[string]interface{}{"content": contentStr}
				}
			}

			functionResp := &dto.GeminiFunctionResponse{
				Name:     name,
				Response: contentMap,
			}

			*parts = append(*parts, dto.GeminiPart{
				FunctionResponse: functionResp,
			})
			continue
		}
		var parts []dto.GeminiPart
		content := dto.GeminiChatContent{
			Role: message.Role,
		}
		shouldAttachThoughtSignature := attachThoughtSignature && (message.Role == "assistant" || message.Role == "model")
		signatureAttached := false
		// isToolCall := false
		if message.ToolCalls != nil {
			// message.Role = "model"
			// isToolCall = true
			for _, call := range message.ParseToolCalls() {
				args := map[string]interface{}{}
				if call.Function.Arguments != "" {
					if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
						return nil, fmt.Errorf("invalid arguments for function %s, args: %s", call.Function.Name, call.Function.Arguments)
					}
				}
				toolCall := dto.GeminiPart{
					FunctionCall: &dto.FunctionCall{
						FunctionName: call.Function.Name,
						Arguments:    args,
					},
				}
				if shouldAttachThoughtSignature && !signatureAttached && hasFunctionCallContent(toolCall.FunctionCall) && len(toolCall.ThoughtSignature) == 0 {
					toolCall.ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					signatureAttached = true
				}
				parts = append(parts, toolCall)
				tool_call_ids[call.ID] = call.Function.Name
			}
		}

		openaiContent := message.ParseContent()
		for _, part := range openaiContent {
			if part.Type == dto.ContentTypeText {
				if part.Text == "" {
					continue
				}
				// check markdown image ![image](data:image/jpeg;base64,xxxxxxxxxxxx)
				// 使用字符串查找而非正则，避免大文本性能问题
				text := part.Text
				hasMarkdownImage := false
				for {
					// 快速检查是否包含 markdown 图片标记
					startIdx := strings.Index(text, "![")
					if startIdx == -1 {
						break
					}
					// 找到 ](
					bracketIdx := strings.Index(text[startIdx:], "](data:")
					if bracketIdx == -1 {
						break
					}
					bracketIdx += startIdx
					// 找到闭合的 )
					closeIdx := strings.Index(text[bracketIdx+2:], ")")
					if closeIdx == -1 {
						break
					}
					closeIdx += bracketIdx + 2

					hasMarkdownImage = true
					// 添加图片前的文本
					if startIdx > 0 {
						textBefore := text[:startIdx]
						if textBefore != "" {
							parts = append(parts, dto.GeminiPart{
								Text: textBefore,
							})
						}
					}
					// 提取 data URL (从 "](" 后面开始，到 ")" 之前)
					dataUrl := text[bracketIdx+2 : closeIdx]
					format, base64String, err := service.DecodeBase64FileData(dataUrl)
					if err != nil {
						return nil, fmt.Errorf("decode markdown base64 image data failed: %s", err.Error())
					}
					imgPart := dto.GeminiPart{
						InlineData: &dto.GeminiInlineData{
							MimeType: format,
							Data:     base64String,
						},
					}
					if shouldAttachThoughtSignature {
						imgPart.ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					}
					parts = append(parts, imgPart)
					// 继续处理剩余文本
					text = text[closeIdx+1:]
				}
				// 添加剩余文本或原始文本（如果没有找到 markdown 图片）
				if !hasMarkdownImage {
					parts = append(parts, dto.GeminiPart{
						Text: part.Text,
					})
				}
			} else {
				source := part.ToFileSource()
				if source == nil {
					continue
				}
				base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Gemini")
				if err != nil {
					return nil, fmt.Errorf("get file data from '%s' failed: %w", source.GetIdentifier(), err)
				}

				// 校验 MimeType 是否在 Gemini 支持的白名单中
				if _, ok := geminiSupportedMimeTypes[strings.ToLower(mimeType)]; !ok {
					return nil, fmt.Errorf("mime type is not supported by Gemini: '%s', url: '%s', supported types are: %v", mimeType, source.GetIdentifier(), getSupportedMimeTypesList())
				}

				parts = append(parts, dto.GeminiPart{
					InlineData: &dto.GeminiInlineData{
						MimeType: mimeType,
						Data:     base64Data,
					},
				})
			}
		}

		// 如果需要附加签名但还没有附加（没有 tool_calls 或 tool_calls 为空），
		// 则在第一个文本 part 上附加 thoughtSignature
		if shouldAttachThoughtSignature && !signatureAttached && len(parts) > 0 {
			for i := range parts {
				if parts[i].Text != "" {
					parts[i].ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					break
				}
			}
		}

		content.Parts = parts

		// there's no assistant role in gemini and API shall vomit if Role is not user or model
		if content.Role == "assistant" {
			content.Role = "model"
		}
		if len(content.Parts) > 0 {
			geminiRequest.Contents = append(geminiRequest.Contents, content)
		}
	}

	if len(system_content) > 0 {
		geminiRequest.SystemInstructions = &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{
				{
					Text: strings.Join(system_content, "\n"),
				},
			},
		}
	}

	// CHZ-PATCH(gemini-extra-body-passthrough): 完全透传 extra_body.google.* 中的任意字段
	// （generationConfig / safetySettings / tools / systemInstruction 等）到上游 Gemini 请求：
	//   - 无 schema 校验，由调用方按 Gemini 原生字段名（camelCase）书写；
	//   - 已被前面 snake_case 白名单逻辑处理过的 thinking_config / image_config 不会再次叠加；
	//   - patch 中相同 key：map 递归合并，标量/数组直接覆盖（patch 优先）；
	//   - 命中 patch 时返回 map[string]any（绕过 *dto.GeminiChatRequest 结构体限制），
	//     调用方拿到后会被 common.Marshal 成 JSON 发给上游，效果等同于"原生 Gemini 调用"。
	patched, err := applyExtraBodyGooglePatch(&geminiRequest, textRequest.ExtraBody)
	if err != nil {
		return nil, err
	}
	if patched != nil {
		return patched, nil
	}
	return &geminiRequest, nil
}

// CHZ-PATCH(gemini-extra-body-passthrough): 把 extra_body.google.* 深度合并到 Gemini 请求 JSON。
// 没有 extra_body 或无 google 字段时返回 nil, nil（外层用结构体原样返回）。
func applyExtraBodyGooglePatch(geminiReq *dto.GeminiChatRequest, extraBody json.RawMessage) (any, error) {
	if len(extraBody) == 0 {
		return nil, nil
	}
	var eb map[string]any
	if err := common.Unmarshal(extraBody, &eb); err != nil {
		return nil, fmt.Errorf("invalid extra_body json: %w", err)
	}
	google, ok := eb["google"].(map[string]any)
	if !ok || len(google) == 0 {
		return nil, nil
	}
	// 已被旧白名单逻辑处理过的 snake_case 键不再透传，避免与上游 Gemini 字段名混淆
	patch := make(map[string]any, len(google))
	for k, v := range google {
		if k == "thinking_config" || k == "image_config" {
			continue
		}
		patch[k] = v
	}
	if len(patch) == 0 {
		return nil, nil
	}
	rawJSON, err := common.Marshal(geminiReq)
	if err != nil {
		return nil, err
	}
	var base map[string]any
	if err := common.Unmarshal(rawJSON, &base); err != nil {
		return nil, err
	}
	deepMergeMap(base, patch)
	return base, nil
}

// CHZ-PATCH(gemini-extra-body-passthrough): 将 src 深度合并进 dst：
// 相同 key 的 map 递归合并，标量/数组等其它类型直接覆盖（src 优先）。
func deepMergeMap(dst, src map[string]any) {
	for k, v := range src {
		if existing, exists := dst[k]; exists {
			if dm, dOk := existing.(map[string]any); dOk {
				if sm, sOk := v.(map[string]any); sOk {
					deepMergeMap(dm, sm)
					continue
				}
			}
		}
		dst[k] = v
	}
}

// parseStopSequences 解析停止序列，支持字符串或字符串数组
func parseStopSequences(stop any) []string {
	if stop == nil {
		return nil
	}

	switch v := stop.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []string:
		return v
	case []interface{}:
		sequences := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				sequences = append(sequences, str)
			}
		}
		return sequences
	}
	return nil
}

func hasFunctionCallContent(call *dto.FunctionCall) bool {
	if call == nil {
		return false
	}
	if strings.TrimSpace(call.FunctionName) != "" {
		return true
	}

	switch v := call.Arguments.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]interface{}:
		return len(v) > 0
	case []interface{}:
		return len(v) > 0
	default:
		return true
	}
}

// Helper function to get a list of supported MIME types for error messages
func getSupportedMimeTypesList() []string {
	keys := make([]string, 0, len(geminiSupportedMimeTypes))
	for k := range geminiSupportedMimeTypes {
		keys = append(keys, k)
	}
	return keys
}

var geminiOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const geminiFunctionSchemaMaxDepth = 64

// cleanFunctionParameters recursively removes unsupported fields from Gemini function parameters.
func cleanFunctionParameters(params interface{}) interface{} {
	return cleanFunctionParametersWithDepth(params, 0)
}

func cleanFunctionParametersWithDepth(params interface{}, depth int) interface{} {
	if params == nil {
		return nil
	}

	if depth >= geminiFunctionSchemaMaxDepth {
		return cleanFunctionParametersShallow(params)
	}

	switch v := params.(type) {
	case map[string]interface{}:
		// Keep only Gemini-supported OpenAPI schema subset fields (per official SDK Schema).
		cleanedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[k]; ok {
				cleanedMap[k] = val
			}
		}

		normalizeGeminiSchemaTypeAndNullable(cleanedMap)

		// Clean properties
		if props, ok := cleanedMap["properties"].(map[string]interface{}); ok && props != nil {
			cleanedProps := make(map[string]interface{})
			for propName, propValue := range props {
				cleanedProps[propName] = cleanFunctionParametersWithDepth(propValue, depth+1)
			}
			cleanedMap["properties"] = cleanedProps
		}

		// Recursively clean items in arrays
		if items, ok := cleanedMap["items"].(map[string]interface{}); ok && items != nil {
			cleanedMap["items"] = cleanFunctionParametersWithDepth(items, depth+1)
		}
		// OpenAPI tuple-style items is not supported by Gemini SDK Schema; keep first to avoid API rejection.
		if itemsArray, ok := cleanedMap["items"].([]interface{}); ok && len(itemsArray) > 0 {
			cleanedMap["items"] = cleanFunctionParametersWithDepth(itemsArray[0], depth+1)
		}

		// Recursively clean anyOf
		if nested, ok := cleanedMap["anyOf"].([]interface{}); ok && nested != nil {
			cleanedNested := make([]interface{}, len(nested))
			for i, item := range nested {
				cleanedNested[i] = cleanFunctionParametersWithDepth(item, depth+1)
			}
			cleanedMap["anyOf"] = cleanedNested
		}

		return cleanedMap

	case []interface{}:
		// Handle arrays of schemas
		cleanedArray := make([]interface{}, len(v))
		for i, item := range v {
			cleanedArray[i] = cleanFunctionParametersWithDepth(item, depth+1)
		}
		return cleanedArray

	default:
		// Not a map or array, return as is (e.g., could be a primitive)
		return params
	}
}

func cleanFunctionParametersShallow(params interface{}) interface{} {
	switch v := params.(type) {
	case map[string]interface{}:
		cleanedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[k]; ok {
				cleanedMap[k] = val
			}
		}
		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		// Stop recursion and avoid retaining huge nested structures.
		delete(cleanedMap, "properties")
		delete(cleanedMap, "items")
		delete(cleanedMap, "anyOf")
		return cleanedMap
	case []interface{}:
		// Prefer an empty list over deep recursion on attacker-controlled inputs.
		return []interface{}{}
	default:
		return params
	}
}

func normalizeGeminiSchemaTypeAndNullable(schema map[string]interface{}) {
	rawType, ok := schema["type"]
	if !ok || rawType == nil {
		return
	}

	normalize := func(t string) (string, bool) {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "object":
			return "OBJECT", false
		case "array":
			return "ARRAY", false
		case "string":
			return "STRING", false
		case "integer":
			return "INTEGER", false
		case "number":
			return "NUMBER", false
		case "boolean":
			return "BOOLEAN", false
		case "null":
			return "", true
		default:
			return t, false
		}
	}

	switch t := rawType.(type) {
	case string:
		normalized, isNull := normalize(t)
		if isNull {
			schema["nullable"] = true
			delete(schema, "type")
			return
		}
		schema["type"] = normalized
	case []interface{}:
		nullable := false
		var chosen string
		for _, item := range t {
			if s, ok := item.(string); ok {
				normalized, isNull := normalize(s)
				if isNull {
					nullable = true
					continue
				}
				if chosen == "" {
					chosen = normalized
				}
			}
		}
		if nullable {
			schema["nullable"] = true
		}
		if chosen != "" {
			schema["type"] = chosen
		} else {
			delete(schema, "type")
		}
	}
}

func removeAdditionalPropertiesWithDepth(schema interface{}, depth int) interface{} {
	if depth >= 5 {
		return schema
	}

	v, ok := schema.(map[string]interface{})
	if !ok || len(v) == 0 {
		return schema
	}
	// 删除所有的title字段
	delete(v, "title")
	delete(v, "$schema")
	// 如果type不为object和array，则直接返回
	if typeVal, exists := v["type"]; !exists || (typeVal != "object" && typeVal != "array") {
		return schema
	}
	switch v["type"] {
	case "object":
		delete(v, "additionalProperties")
		// 处理 properties
		if properties, ok := v["properties"].(map[string]interface{}); ok {
			for key, value := range properties {
				properties[key] = removeAdditionalPropertiesWithDepth(value, depth+1)
			}
		}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			if nested, ok := v[field].([]interface{}); ok {
				for i, item := range nested {
					nested[i] = removeAdditionalPropertiesWithDepth(item, depth+1)
				}
			}
		}
	case "array":
		if items, ok := v["items"].(map[string]interface{}); ok {
			v["items"] = removeAdditionalPropertiesWithDepth(items, depth+1)
		}
	}

	return v
}

func unescapeString(s string) (string, error) {
	var result []rune
	escaped := false
	i := 0

	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:]) // 正确解码UTF-8字符
		if r == utf8.RuneError {
			return "", fmt.Errorf("invalid UTF-8 encoding")
		}

		if escaped {
			// 如果是转义符后的字符，检查其类型
			switch r {
			case '"':
				result = append(result, '"')
			case '\\':
				result = append(result, '\\')
			case '/':
				result = append(result, '/')
			case 'b':
				result = append(result, '\b')
			case 'f':
				result = append(result, '\f')
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case '\'':
				result = append(result, '\'')
			default:
				// 如果遇到一个非法的转义字符，直接按原样输出
				result = append(result, '\\', r)
			}
			escaped = false
		} else {
			if r == '\\' {
				escaped = true // 记录反斜杠作为转义符
			} else {
				result = append(result, r)
			}
		}
		i += size // 移动到下一个字符
	}

	return string(result), nil
}
func unescapeMapOrSlice(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		for k, val := range v {
			v[k] = unescapeMapOrSlice(val)
		}
	case []interface{}:
		for i, val := range v {
			v[i] = unescapeMapOrSlice(val)
		}
	case string:
		if unescaped, err := unescapeString(v); err != nil {
			return v
		} else {
			return unescaped
		}
	}
	return data
}

func getResponseToolCall(item *dto.GeminiPart) *dto.ToolCallResponse {
	var argsBytes []byte
	var err error
	// 移除 unescapeMapOrSlice 调用，直接使用 json.Marshal
	// JSON 序列化/反序列化已经正确处理了转义字符
	argsBytes, err = json.Marshal(item.FunctionCall.Arguments)

	if err != nil {
		return nil
	}
	return &dto.ToolCallResponse{
		ID:   fmt.Sprintf("call_%s", common.GetUUID()),
		Type: "function",
		Function: dto.FunctionResponse{
			Arguments: string(argsBytes),
			Name:      item.FunctionCall.FunctionName,
		},
	}
}

func buildUsageFromGeminiMetadata(metadata dto.GeminiUsageMetadata, fallbackPromptTokens int) dto.Usage {
	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if promptTokens <= 0 && fallbackPromptTokens > 0 {
		promptTokens = fallbackPromptTokens
	}

	usage := dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	// CHZ-PATCH(gemini-usage-fix): prompt 端补 IMAGE 分支，避免多模态输入 token 落入空分类
	for _, detail := range metadata.PromptTokensDetails {
		mod := strings.TrimSpace(detail.Modality)
		switch {
		case strings.EqualFold(mod, "AUDIO"):
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		case strings.EqualFold(mod, "TEXT"):
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		case strings.EqualFold(mod, "IMAGE"):
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		mod := strings.TrimSpace(detail.Modality)
		switch {
		case strings.EqualFold(mod, "AUDIO"):
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		case strings.EqualFold(mod, "TEXT"):
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		case strings.EqualFold(mod, "IMAGE"):
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		}
	}
	// CHZ-PATCH(gemini-usage-fix): 原代码对 CandidatesTokensDetails 写了两个循环导致
	// completion_tokens_details.{Image,Audio,Text}Tokens 被双重累加。这里只保留一个，
	// 同时 modality 用 EqualFold + TrimSpace 做大小写不敏感匹配，覆盖上游可能的 "image" 小写写法。
	for _, detail := range metadata.CandidatesTokensDetails {
		mod := strings.TrimSpace(detail.Modality)
		switch {
		case strings.EqualFold(mod, "IMAGE"):
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case strings.EqualFold(mod, "AUDIO"):
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case strings.EqualFold(mod, "TEXT"):
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}

	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}

	return usage
}

func responseGeminiChat2OpenAI(c *gin.Context, response *dto.GeminiChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		// CHZ-PATCH(gemini-resp-id): 用上游 responseId 作为 response.id，与日志 request_id 对齐
		Id:      helper.GetResponseIDFromUpstream(c, response.ResponseId),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	isToolCall := false
	for _, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: int(candidate.Index),
			Message: dto.Message{
				Role:    "assistant",
				Content: "",
			},
			FinishReason: constant.FinishReasonStop,
		}
		if len(candidate.Content.Parts) > 0 {
			// CHZ-PATCH(gemini-image-content-v2): 含 inlineData(image/*) 时把 message.content
			// 改成 OpenAI v2 多模态数组（[{type:text}, {type:image_url, image_url:{url:"data:..."}}]）；
			// 不含图片时保持原有字符串格式，按 "\n" 拼接 text/executable code/非图片 media 文本回退，
			// 维持纯文本、tool_call、reasoning 场景下的向后兼容。
			var mediaParts []dto.MediaContent
			var toolCalls []dto.ToolCallResponse
			hasImage := false
			appendText := func(text string) {
				mediaParts = append(mediaParts, dto.MediaContent{Type: dto.ContentTypeText, Text: text})
			}
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					if strings.HasPrefix(part.InlineData.MimeType, "image") {
						hasImage = true
						mediaParts = append(mediaParts, dto.MediaContent{
							Type: dto.ContentTypeImageURL,
							ImageUrl: &dto.MessageImageUrl{
								Url:      "data:" + part.InlineData.MimeType + ";base64," + part.InlineData.Data,
								MimeType: part.InlineData.MimeType,
							},
						})
					} else {
						// 非图片媒体（音频等）：仍以 markdown 文本承载，避免引入额外 DTO 字段
						appendText(fmt.Sprintf("[media](data:%s;base64,%s)", part.InlineData.MimeType, part.InlineData.Data))
					}
				} else if part.FunctionCall != nil {
					choice.FinishReason = constant.FinishReasonToolCalls
					if call := getResponseToolCall(&part); call != nil {
						toolCalls = append(toolCalls, *call)
					}
				} else if part.Thought {
					choice.Message.ReasoningContent = &part.Text
				} else {
					if part.ExecutableCode != nil {
						appendText("```" + part.ExecutableCode.Language + "\n" + part.ExecutableCode.Code + "\n```")
					} else if part.CodeExecutionResult != nil {
						appendText("```output\n" + part.CodeExecutionResult.Output + "\n```")
					} else if part.Text != "\n" {
						// 过滤掉纯换行 part
						appendText(part.Text)
					}
				}
			}
			if len(toolCalls) > 0 {
				choice.Message.SetToolCalls(toolCalls)
				isToolCall = true
			}
			if hasImage {
				choice.Message.SetMediaContent(mediaParts)
			} else {
				texts := make([]string, 0, len(mediaParts))
				for _, mp := range mediaParts {
					if mp.Type == dto.ContentTypeText {
						texts = append(texts, mp.Text)
					}
				}
				choice.Message.SetStringContent(strings.Join(texts, "\n"))
			}
		}
		if candidate.FinishReason != nil {
			switch *candidate.FinishReason {
			case "STOP":
				choice.FinishReason = constant.FinishReasonStop
			case "MAX_TOKENS":
				choice.FinishReason = constant.FinishReasonLength
			case "SAFETY":
				// Safety filter triggered
				choice.FinishReason = constant.FinishReasonContentFilter
			case "RECITATION":
				// Recitation (citation) detected
				choice.FinishReason = constant.FinishReasonContentFilter
			case "BLOCKLIST":
				// Blocklist triggered
				choice.FinishReason = constant.FinishReasonContentFilter
			case "PROHIBITED_CONTENT":
				// Prohibited content detected
				choice.FinishReason = constant.FinishReasonContentFilter
			case "SPII":
				// Sensitive personally identifiable information
				choice.FinishReason = constant.FinishReasonContentFilter
			case "OTHER":
				// Other reasons
				choice.FinishReason = constant.FinishReasonContentFilter
			default:
				choice.FinishReason = constant.FinishReasonContentFilter
			}
		}
		if isToolCall {
			choice.FinishReason = constant.FinishReasonToolCalls
		}

		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func streamResponseGeminiChat2OpenAI(geminiResponse *dto.GeminiChatResponse) (*dto.ChatCompletionsStreamResponse, bool) {
	choices := make([]dto.ChatCompletionsStreamResponseChoice, 0, len(geminiResponse.Candidates))
	isStop := false
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason == "STOP" {
			isStop = true
			candidate.FinishReason = nil
		}
		choice := dto.ChatCompletionsStreamResponseChoice{
			Index: int(candidate.Index),
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				//Role: "assistant",
			},
		}
		// 使用 strings.Builder 直接累积 delta content，避免每张 image / 每个
		// 文本片段都先 `+` 拼出一份临时 string，再 strings.Join 再拷贝一遍。
		var content strings.Builder
		var inlineGrow int
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
			}
		}
		if inlineGrow > 0 {
			content.Grow(inlineGrow)
		}
		appended := 0
		writeSep := func() {
			if appended > 0 {
				content.WriteByte('\n')
			}
			appended++
		}
		isTools := false
		isThought := false
		if candidate.FinishReason != nil {
			// Map Gemini FinishReason to OpenAI finish_reason
			switch *candidate.FinishReason {
			case "STOP":
				// Normal completion
				choice.FinishReason = &constant.FinishReasonStop
			case "MAX_TOKENS":
				// Reached maximum token limit
				choice.FinishReason = &constant.FinishReasonLength
			case "SAFETY":
				// Safety filter triggered
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "RECITATION":
				// Recitation (citation) detected
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "BLOCKLIST":
				// Blocklist triggered
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "PROHIBITED_CONTENT":
				// Prohibited content detected
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "SPII":
				// Sensitive personally identifiable information
				choice.FinishReason = &constant.FinishReasonContentFilter
			case "OTHER":
				// Other reasons
				choice.FinishReason = &constant.FinishReasonContentFilter
			default:
				// Unknown reason, treat as content filter
				choice.FinishReason = &constant.FinishReasonContentFilter
			}
		}
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				if strings.HasPrefix(part.InlineData.MimeType, "image") {
					writeSep()
					content.WriteString("![image](data:")
					content.WriteString(part.InlineData.MimeType)
					content.WriteString(";base64,")
					content.WriteString(part.InlineData.Data)
					content.WriteByte(')')
				}
			} else if part.FunctionCall != nil {
				isTools = true
				if call := getResponseToolCall(&part); call != nil {
					call.SetIndex(len(choice.Delta.ToolCalls))
					choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, *call)
				}

			} else if part.Thought {
				isThought = true
				writeSep()
				content.WriteString(part.Text)
			} else {
				if part.ExecutableCode != nil {
					writeSep()
					content.WriteString("```")
					content.WriteString(part.ExecutableCode.Language)
					content.WriteByte('\n')
					content.WriteString(part.ExecutableCode.Code)
					content.WriteString("\n```\n")
				} else if part.CodeExecutionResult != nil {
					writeSep()
					content.WriteString("```output\n")
					content.WriteString(part.CodeExecutionResult.Output)
					content.WriteString("\n```\n")
				} else {
					if part.Text != "\n" {
						writeSep()
						content.WriteString(part.Text)
					}
				}
			}
		}
		if isThought {
			choice.Delta.SetReasoningContent(content.String())
		} else {
			choice.Delta.SetContentString(content.String())
		}
		if isTools {
			choice.FinishReason = &constant.FinishReasonToolCalls
		}
		choices = append(choices, choice)
	}

	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Choices = choices
	return &response, isStop
}

func handleStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	err = openai.HandleStreamFormat(c, info, string(streamData), info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
	if err != nil {
		return fmt.Errorf("failed to handle stream format: %w", err)
	}
	return nil
}

func handleFinalStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	openai.HandleFinalResponse(c, info, string(streamData), resp.Id, resp.Created, resp.Model, resp.GetSystemFingerprint(), resp.Usage, false)
	return nil
}

func geminiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, callback func(data string, geminiResponse *dto.GeminiChatResponse) bool) (*dto.Usage, *types.NewAPIError) {
	var usage = &dto.Usage{}
	var imageCount int
	// CHZ-PATCH(gemini-usage-fix): 记录流中是否真的产出过 image inlineData，
	// 供下方 candidatesTokenCount 兜底归类时判断是图片输出还是文本输出
	var hasImagePart bool
	responseText := strings.Builder{}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var geminiResponse dto.GeminiChatResponse
		if err := common.UnmarshalJsonStr(data, &geminiResponse); err != nil {
			sr.Stop(fmt.Errorf("unmarshal: %w", err))
			return
		}
		if geminiResponse.ResponseId != "" {
			info.UpstreamResponseId = geminiResponse.ResponseId
		}

		if len(geminiResponse.Candidates) == 0 && geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
		}

		// 统计图片数量；CHZ-PATCH(gemini-usage-fix): 同时标记是否真的产出过 image inlineData
		for _, candidate := range geminiResponse.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil && part.InlineData.MimeType != "" {
					imageCount++
					if strings.HasPrefix(part.InlineData.MimeType, "image") {
						hasImagePart = true
					}
				}
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}

		// 更新使用量统计
		if geminiResponse.UsageMetadata.TotalTokenCount != 0 {
			// CHZ-PATCH(gemini-usage-fix): 打印上游 usageMetadata 原文，便于对账核对
			// prompt/candidates/thoughts/modality 拆分是否准确
			if metaJSON, jerr := common.Marshal(geminiResponse.UsageMetadata); jerr == nil {
				logger.LogInfo(c, fmt.Sprintf("gemini upstream usageMetadata (responseId=%s, model=%s): %s",
					geminiResponse.ResponseId, info.UpstreamModelName, string(metaJSON)))
			}
			mappedUsage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())
			*usage = mappedUsage
			// 流式最后一包：从 CandidatesTokensDetails 拆出图片/音频/文本输出 token，供计费与日志使用（modality 大小写不敏感）
			var imageOutputTokens, audioOutputTokens, textOutputTokens int
			for _, detail := range geminiResponse.UsageMetadata.CandidatesTokensDetails {
				mod := strings.TrimSpace(detail.Modality)
				if strings.EqualFold(mod, "IMAGE") {
					imageOutputTokens += detail.TokenCount
				} else if strings.EqualFold(mod, "AUDIO") {
					audioOutputTokens += detail.TokenCount
				} else if strings.EqualFold(mod, "TEXT") {
					textOutputTokens += detail.TokenCount
				}
			}
			// CHZ-PATCH(gemini-usage-fix): 上游未提供 CandidatesTokensDetails 拆分时，
			// 根据流中累计是否产出过 image inlineData 归类：
			//   - 实际产生图片  → 算图片输出
			//   - 实际只产出文本 → 算文本输出（避免多模态模型纯文本响应被误按图片计费）
			if imageOutputTokens == 0 && audioOutputTokens == 0 && textOutputTokens == 0 &&
				geminiResponse.UsageMetadata.CandidatesTokenCount > 0 {
				if hasImagePart {
					imageOutputTokens = geminiResponse.UsageMetadata.CandidatesTokenCount
				} else {
					textOutputTokens = geminiResponse.UsageMetadata.CandidatesTokenCount
				}
			}
			usage.CompletionTokenDetails.ImageTokens = imageOutputTokens
			usage.CompletionTokenDetails.AudioTokens = audioOutputTokens
			usage.CompletionTokenDetails.TextTokens = textOutputTokens
			c.Set("gemini_image_output_tokens", imageOutputTokens)
			c.Set("gemini_text_output_tokens", textOutputTokens)
		}

		if !callback(data, &geminiResponse) {
			sr.Stop(fmt.Errorf("gemini callback stopped"))
		}
	})

	if imageCount != 0 {
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = imageCount * 1400
		}
	}

	if usage.CompletionTokens <= 0 {
		if info.ReceivedResponseCount > 0 {
			usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else {
			usage = &dto.Usage{}
		}
	}

	return usage, nil
}

func GeminiChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	id := helper.GetResponseID(c)
	createAt := common.GetTimestamp()
	finishReason := constant.FinishReasonStop
	toolCallIndexByChoice := make(map[int]map[string]int)
	nextToolCallIndexByChoice := make(map[int]int)

	usage, err := geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		response, isStop := streamResponseGeminiChat2OpenAI(geminiResponse)

		// CHZ-PATCH(gemini-resp-id): 二步策略——首包用本地 chatcmpl-*，
		// 拿到上游 responseId 后改写 id 并应用到后续所有 chunk + 末尾 usage 帧
		if geminiResponse != nil && geminiResponse.ResponseId != "" && id != geminiResponse.ResponseId {
			id = geminiResponse.ResponseId
		}
		response.Id = id
		response.Created = createAt
		response.Model = info.UpstreamModelName
		if response.IsToolCall() {
			finishReason = constant.FinishReasonToolCalls
			if info.RelayFormat == types.RelayFormatClaude {
				for choiceIdx := range response.Choices {
					response.Choices[choiceIdx].FinishReason = nil
				}
			}
		}
		for choiceIdx := range response.Choices {
			choiceKey := response.Choices[choiceIdx].Index
			for toolIdx := range response.Choices[choiceIdx].Delta.ToolCalls {
				tool := &response.Choices[choiceIdx].Delta.ToolCalls[toolIdx]
				if tool.ID == "" {
					continue
				}
				m := toolCallIndexByChoice[choiceKey]
				if m == nil {
					m = make(map[string]int)
					toolCallIndexByChoice[choiceKey] = m
				}
				if idx, ok := m[tool.ID]; ok {
					tool.SetIndex(idx)
					continue
				}
				idx := nextToolCallIndexByChoice[choiceKey]
				nextToolCallIndexByChoice[choiceKey] = idx + 1
				m[tool.ID] = idx
				tool.SetIndex(idx)
			}
		}

		logger.LogDebug(c, "info.SendResponseCount = %d", info.SendResponseCount)
		if info.SendResponseCount == 0 {
			// send first response
			emptyResponse := helper.GenerateStartEmptyResponse(id, createAt, info.UpstreamModelName, nil)
			if response.IsToolCall() {
				if len(emptyResponse.Choices) > 0 && len(response.Choices) > 0 {
					toolCalls := response.Choices[0].Delta.ToolCalls
					copiedToolCalls := make([]dto.ToolCallResponse, len(toolCalls))
					for idx := range toolCalls {
						copiedToolCalls[idx] = toolCalls[idx]
						copiedToolCalls[idx].Function.Arguments = ""
					}
					emptyResponse.Choices[0].Delta.ToolCalls = copiedToolCalls
				}
				finishReason = constant.FinishReasonToolCalls
				err := handleStream(c, info, emptyResponse)
				if err != nil {
					logger.LogError(c, err.Error())
				}

				response.ClearToolCalls()
				if response.IsFinished() {
					response.Choices[0].FinishReason = nil
				}
			} else {
				err := handleStream(c, info, emptyResponse)
				if err != nil {
					logger.LogError(c, err.Error())
				}
			}
		}

		err := handleStream(c, info, response)
		if err != nil {
			logger.LogError(c, err.Error())
		}
		if isStop {
			if info.RelayFormat != types.RelayFormatClaude {
				_ = handleStream(c, info, helper.GenerateStopResponse(id, createAt, info.UpstreamModelName, finishReason))
			}
		}
		return true
	})

	if err != nil {
		return usage, err
	}

	response := helper.GenerateFinalUsageResponse(id, createAt, info.UpstreamModelName, *usage)
	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil && !info.ClaudeConvertInfo.Done {
		response = helper.GenerateStopResponse(id, createAt, info.UpstreamModelName, finishReason)
		response.Usage = usage
	}
	handleErr := handleFinalStream(c, info, response)
	if handleErr != nil {
		common.SysLog("send final response failed: " + handleErr.Error())
	}
	return usage, nil
}

func GeminiChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	logger.LogDebug(c, "Gemini response body: %s", responseBody)
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	info.UpstreamResponseId = geminiResponse.ResponseId

	// CHZ-PATCH(gemini-usage-fix): 打印上游 usageMetadata 原文，便于对账核对
	// prompt/candidates/thoughts/modality 拆分是否准确
	if metaJSON, jerr := common.Marshal(geminiResponse.UsageMetadata); jerr == nil {
		logger.LogInfo(c, fmt.Sprintf("gemini upstream usageMetadata (responseId=%s, model=%s): %s",
			geminiResponse.ResponseId, info.UpstreamModelName, string(metaJSON)))
	}

	if len(geminiResponse.Candidates) == 0 {
		usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())

		var newAPIError *types.NewAPIError
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			newAPIError = types.NewOpenAIError(
				errors.New("request blocked by Gemini API: "+*geminiResponse.PromptFeedback.BlockReason),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
			)
		} else {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
			newAPIError = types.NewOpenAIError(
				errors.New("empty response from Gemini API"),
				types.ErrorCodeEmptyResponse,
				http.StatusInternalServerError,
			)
		}

		service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))

		switch info.RelayFormat {
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
		return &usage, nil
	}
	fullTextResponse := responseGeminiChat2OpenAI(c, &geminiResponse)
	fullTextResponse.Model = info.UpstreamModelName
	usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())

	// 从 CandidatesTokensDetails 拆出图片/音频/文本输出 token，供计费与日志使用（modality 大小写不敏感）
	var imageOutputTokens, audioOutputTokens, textOutputTokens int
	for _, detail := range geminiResponse.UsageMetadata.CandidatesTokensDetails {
		mod := strings.TrimSpace(detail.Modality)
		if strings.EqualFold(mod, "IMAGE") {
			imageOutputTokens += detail.TokenCount
		} else if strings.EqualFold(mod, "AUDIO") {
			audioOutputTokens += detail.TokenCount
		} else if strings.EqualFold(mod, "TEXT") {
			textOutputTokens += detail.TokenCount
		}
	}
	// CHZ-PATCH(gemini-usage-fix): 上游未提供 CandidatesTokensDetails 拆分时，
	// 根据 candidate 实际产出的内容归类 candidatesTokenCount：
	//   - 实际产生 image inlineData → 算图片输出（按图片单价计费）
	//   - 实际只产出文本          → 算文本输出（避免把 banana 等多模态模型的纯文本响应误按图片计费）
	// 不再使用 "OriginModelName 含 image" 这种基于模型名的兜底（会误伤纯文本响应）。
	if imageOutputTokens == 0 && audioOutputTokens == 0 && textOutputTokens == 0 &&
		geminiResponse.UsageMetadata.CandidatesTokenCount > 0 {
		hasImagePart := false
		for _, candidate := range geminiResponse.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image") {
					hasImagePart = true
					break
				}
			}
			if hasImagePart {
				break
			}
		}
		if hasImagePart {
			imageOutputTokens = geminiResponse.UsageMetadata.CandidatesTokenCount
		} else {
			textOutputTokens = geminiResponse.UsageMetadata.CandidatesTokenCount
		}
	}
	usage.CompletionTokenDetails.ImageTokens = imageOutputTokens
	usage.CompletionTokenDetails.AudioTokens = audioOutputTokens
	usage.CompletionTokenDetails.TextTokens = textOutputTokens
	c.Set("gemini_image_output_tokens", imageOutputTokens)
	c.Set("gemini_text_output_tokens", textOutputTokens)

	fullTextResponse.Usage = usage

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		responseBody, err = common.Marshal(fullTextResponse)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(fullTextResponse, info)
		claudeRespStr, err := common.Marshal(claudeResp)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = claudeRespStr
	case types.RelayFormatGemini:
		break
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}

func GeminiEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	var geminiResponse dto.GeminiBatchEmbeddingResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, 0, len(geminiResponse.Embeddings)),
		Model:  info.UpstreamModelName,
	}

	for i, embedding := range geminiResponse.Embeddings {
		openAIResponse.Data = append(openAIResponse.Data, dto.OpenAIEmbeddingResponseItem{
			Object:    "embedding",
			Embedding: embedding.Values,
			Index:     i,
		})
	}

	// calculate usage
	// https://ai.google.dev/gemini-api/docs/pricing?hl=zh-cn#text-embedding-004
	// Google has not yet clarified how embedding models will be billed
	// refer to openai billing method to use input tokens billing
	// https://platform.openai.com/docs/guides/embeddings#what-are-embeddings
	usage := service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	openAIResponse.Usage = *usage

	jsonResponse, jsonErr := common.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}

func GeminiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var geminiResponse dto.GeminiImageResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if len(geminiResponse.Predictions) == 0 {
		return nil, types.NewOpenAIError(errors.New("no images generated"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    make([]dto.ImageData, 0, len(geminiResponse.Predictions)),
	}

	for _, prediction := range geminiResponse.Predictions {
		if prediction.RaiFilteredReason != "" {
			continue // skip filtered image
		}
		openAIResponse.Data = append(openAIResponse.Data, dto.ImageData{
			B64Json: prediction.BytesBase64Encoded,
		})
	}

	jsonResponse, jsonErr := json.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewError(jsonErr, types.ErrorCodeBadResponseBody)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	// https://github.com/google-gemini/cookbook/blob/719a27d752aac33f39de18a8d3cb42a70874917e/quickstarts/Counting_Tokens.ipynb
	// each image has fixed 258 tokens
	const imageTokens = 258
	generatedImages := len(openAIResponse.Data)

	usage := &dto.Usage{
		PromptTokens:     imageTokens * generatedImages, // each generated image has fixed 258 tokens
		CompletionTokens: 0,                             // image generation does not calculate completion tokens
		TotalTokens:      imageTokens * generatedImages,
	}

	return usage, nil
}

// CHZ-PATCH(gemini-imagine-images-generations): nano banana 等 imagine 模型可以通过
// /v1/images/generations 调用——底层走 :generateContent，把 OpenAI ImageRequest 翻译为
// 一个最小的 GeminiChatRequest（user prompt + 可选输入图）；响应通过
// GeminiImagineImageHandler 把 GeminiChatResponse 中的 inlineData(image/*) 还原为
// OpenAI ImageResponse 的 b64_json 数组，文本输出（描述、修订 prompt 等）合并写入 metadata。
//
// 维护要点：
//   - imagine 模型清单在 setting/model_setting/gemini.go → SupportedImagineModels；
//   - URL 仍由 GetRequestURL 自动选择（imagen→:predict，imagine→默认 :generateContent）；
//   - 计费按上游实际生成的图片数 (usage.GeneratedImages) 由 image_handler.go 统一处理。
func convertImagineImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (*dto.GeminiChatRequest, error) {
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required for image generation")
	}

	parts := []dto.GeminiPart{{Text: prompt}}

	// 图生图：v1/images/generations 也允许通过 image 字段传入参考图（与 gpt-image 系列保持一致）
	imageParts, err := buildImagineInputImageParts(c, request.Image)
	if err != nil {
		return nil, err
	}
	parts = append(parts, imageParts...)

	geminiReq := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role:  "user",
				Parts: parts,
			},
		},
		GenerationConfig: dto.GeminiChatGenerationConfig{
			ResponseModalities: []string{"TEXT", "IMAGE"},
		},
	}

	// 平台默认 safety
	safetySettings := make([]dto.GeminiChatSafetySettings, 0, len(SafetySettingList))
	for _, category := range SafetySettingList {
		safetySettings = append(safetySettings, dto.GeminiChatSafetySettings{
			Category:  category,
			Threshold: model_setting.GetGeminiSafetySetting(category),
		})
	}
	geminiReq.SafetySettings = safetySettings

	// size → aspectRatio / quality → imageSize 复用 imagen 那边的映射规则
	imageConfig := map[string]string{}
	if ar := imagineAspectRatioFromSize(request.Size); ar != "" {
		imageConfig["aspectRatio"] = ar
	}
	if request.Quality != "" {
		imageConfig["imageSize"] = imagineImageSizeFromQuality(request.Quality)
	}
	if len(imageConfig) > 0 {
		raw, marshalErr := common.Marshal(imageConfig)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal image_config: %w", marshalErr)
		}
		geminiReq.GenerationConfig.ImageConfig = raw
	}

	return geminiReq, nil
}

// buildImagineInputImageParts 解析 OpenAI ImageRequest.Image（json.RawMessage），
// 支持 string / []string / 嵌套对象等多种形式，逐项转成 Gemini inlineData part；
// 不存在或为空时返回 nil（即纯文生图）。
func buildImagineInputImageParts(c *gin.Context, raw json.RawMessage) ([]dto.GeminiPart, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	refs := make([]string, 0, 4)
	switch trimmed[0] {
	case '"':
		var single string
		if err := common.Unmarshal(raw, &single); err != nil {
			return nil, fmt.Errorf("invalid image field: %w", err)
		}
		if single != "" {
			refs = append(refs, single)
		}
	case '[':
		var arr []string
		if err := common.Unmarshal(raw, &arr); err != nil {
			// 数组里不是字符串时直接拒绝，避免静默吞错
			return nil, fmt.Errorf("invalid image field (expected []string): %w", err)
		}
		for _, item := range arr {
			if item != "" {
				refs = append(refs, item)
			}
		}
	default:
		// 其它形式（比如对象 {"url": "..."}）暂不支持
		return nil, fmt.Errorf("invalid image field: unsupported shape, expected string or []string")
	}

	if len(refs) == 0 {
		return nil, nil
	}

	parts := make([]dto.GeminiPart, 0, len(refs))
	for _, ref := range refs {
		// NewFileSourceFromData 自动判定：以 http(s):// 开头 → URLSource；
		// 否则按 base64 处理（data URI 前缀会在 service.loadFromBase64 中解析出 mime type）
		source := types.NewFileSourceFromData(ref, "")
		if source == nil {
			continue
		}
		base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Gemini imagine")
		if err != nil {
			return nil, fmt.Errorf("get image data from '%s' failed: %w", source.GetIdentifier(), err)
		}
		if _, ok := geminiSupportedMimeTypes[strings.ToLower(mimeType)]; !ok {
			return nil, fmt.Errorf("mime type is not supported by Gemini: '%s', url: '%s'", mimeType, source.GetIdentifier())
		}
		parts = append(parts, dto.GeminiPart{
			InlineData: &dto.GeminiInlineData{
				MimeType: mimeType,
				Data:     base64Data,
			},
		})
	}
	return parts, nil
}

func imagineAspectRatioFromSize(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return ""
	}
	if strings.Contains(size, ":") {
		return size
	}
	switch size {
	case "256x256", "512x512", "1024x1024":
		return "1:1"
	case "1536x1024":
		return "3:2"
	case "1024x1536":
		return "2:3"
	case "1024x1792":
		return "9:16"
	case "1792x1024":
		return "16:9"
	}
	return ""
}

func imagineImageSizeFromQuality(quality string) string {
	switch quality {
	case "hd", "high", "2K":
		return "2K"
	case "standard", "medium", "low", "auto", "1K", "":
		return "1K"
	}
	return "1K"
}

// GeminiImagineImageHandler 把 nano banana 等 imagine 模型 :generateContent 的响应
// 转成 OpenAI v1/images/generations 的 ImageResponse 格式。
//   - candidates[].content.parts 里 inlineData(image/*) → ImageData{B64Json: ...}
//   - 文本 part → 合并到第一张图片的 revised_prompt（仿 dall-e-3 行为），同时整体 prompt
//     也写入 ImageResponse.Metadata.text 字段，便于客户端拿到模型描述。
//   - usage 用 buildUsageFromGeminiMetadata 复用与 chat 一致的对账口径，并设置
//     GeneratedImages，让 image_handler.go 按实际生成张数计费（避免 n=4 但只出 1 张时多扣费）。
func GeminiImagineImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	var geminiResponse dto.GeminiChatResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	info.UpstreamResponseId = geminiResponse.ResponseId

	// 与 GeminiChatHandler 保持一致：打印上游 usageMetadata 原文便于对账
	if metaJSON, jerr := common.Marshal(geminiResponse.UsageMetadata); jerr == nil {
		logger.LogInfo(c, fmt.Sprintf("gemini imagine images generations upstream usageMetadata (responseId=%s, model=%s): %s",
			geminiResponse.ResponseId, info.UpstreamModelName, string(metaJSON)))
	}

	imageResponse := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    make([]dto.ImageData, 0, 1),
	}

	textBuilder := strings.Builder{}
	for _, candidate := range geminiResponse.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image") {
				imageResponse.Data = append(imageResponse.Data, dto.ImageData{
					B64Json: part.InlineData.Data,
				})
			} else if part.Text != "" && !part.Thought {
				if textBuilder.Len() > 0 {
					textBuilder.WriteByte('\n')
				}
				textBuilder.WriteString(part.Text)
			}
		}
	}

	if len(imageResponse.Data) == 0 {
		var newAPIError *types.NewAPIError
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			newAPIError = types.NewOpenAIError(
				fmt.Errorf("request blocked by Gemini API: %s", *geminiResponse.PromptFeedback.BlockReason),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
			)
		} else {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_imagine_no_image")
			newAPIError = types.NewOpenAIError(
				errors.New("no images generated by gemini imagine model"),
				types.ErrorCodeEmptyResponse,
				http.StatusBadGateway,
			)
		}
		return nil, newAPIError
	}

	// 把模型同时返回的文本内容（描述/修订 prompt 等）作为 revised_prompt 与 metadata.text 透出
	if textBuilder.Len() > 0 {
		text := textBuilder.String()
		imageResponse.Data[0].RevisedPrompt = text
		if metaRaw, mErr := common.Marshal(map[string]string{"text": text}); mErr == nil {
			imageResponse.Metadata = metaRaw
		}
	}

	// 计算 usage：先按 chat 口径解析 GeminiUsageMetadata，再映射成 OpenAI image API 的 usage 字段
	usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())
	usage.GeneratedImages = len(imageResponse.Data)
	if usage.PromptTokens == 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	// CHZ-PATCH(image-usage): 把 token 用量按 OpenAI gpt-image-1 风格写进响应体的 usage 字段，
	// 让客户端在 /v1/images/generations 返回里也能拿到 input/output/total tokens 与 modality 拆分。
	imageResponse.Usage = buildImageUsageFromGeminiUsage(&usage)

	jsonResponse, jsonErr := common.Marshal(imageResponse)
	if jsonErr != nil {
		return nil, types.NewError(jsonErr, types.ErrorCodeBadResponseBody)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	return &usage, nil
}

// buildImageUsageFromGeminiUsage 把 dto.Usage（chat 口径：prompt_tokens/completion_tokens
// + prompt_tokens_details/completion_tokens_details）映射成 OpenAI gpt-image-1 风格的
// dto.ImageUsage（input_tokens/output_tokens + input_tokens_details/output_tokens_details）。
//
// 字段对应关系：
//
//	prompt_tokens                              -> input_tokens
//	completion_tokens                          -> output_tokens
//	total_tokens                               -> total_tokens
//	prompt_tokens_details.{text,image}_tokens  -> input_tokens_details.{text,image}_tokens
//	prompt_tokens_details.cached_tokens        -> input_tokens_details.cached_tokens
//	completion_tokens_details.image_tokens     -> output_tokens_details.image_tokens
func buildImageUsageFromGeminiUsage(u *dto.Usage) *dto.ImageUsage {
	if u == nil {
		return nil
	}
	usage := &dto.ImageUsage{
		TotalTokens:  u.TotalTokens,
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
	inputDetails := u.PromptTokensDetails
	if inputDetails.TextTokens > 0 || inputDetails.ImageTokens > 0 ||
		inputDetails.AudioTokens > 0 || inputDetails.CachedTokens > 0 {
		usage.InputTokensDetails = &dto.InputTokenDetails{
			TextTokens:   inputDetails.TextTokens,
			ImageTokens:  inputDetails.ImageTokens,
			AudioTokens:  inputDetails.AudioTokens,
			CachedTokens: inputDetails.CachedTokens,
		}
	}
	outputDetails := u.CompletionTokenDetails
	if outputDetails.TextTokens > 0 || outputDetails.ImageTokens > 0 ||
		outputDetails.AudioTokens > 0 {
		usage.OutputTokensDetails = &dto.OutputTokenDetails{
			TextTokens:  outputDetails.TextTokens,
			ImageTokens: outputDetails.ImageTokens,
			AudioTokens: outputDetails.AudioTokens,
		}
	}
	return usage
}

type GeminiModelsResponse struct {
	Models        []dto.GeminiModel `json:"models"`
	NextPageToken string            `json:"nextPageToken"`
}

func FetchGeminiModels(baseURL, apiKey, proxyURL string) ([]string, error) {
	client, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("创建HTTP客户端失败: %v", err)
	}

	allModels := make([]string, 0)
	nextPageToken := ""
	maxPages := 100 // Safety limit to prevent infinite loops

	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf("%s/v1beta/models", baseURL)
		if nextPageToken != "" {
			url = fmt.Sprintf("%s?pageToken=%s", url, nextPageToken)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("创建请求失败: %v", err)
		}

		request.Header.Set("x-goog-api-key", apiKey)

		response, err := client.Do(request)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("请求失败: %v", err)
		}

		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			cancel()
			return nil, fmt.Errorf("服务器返回错误 %d: %s", response.StatusCode, string(body))
		}

		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("读取响应失败: %v", err)
		}

		var modelsResponse GeminiModelsResponse
		if err = common.Unmarshal(body, &modelsResponse); err != nil {
			return nil, fmt.Errorf("解析响应失败: %v", err)
		}

		for _, model := range modelsResponse.Models {
			modelNameValue, ok := model.Name.(string)
			if !ok {
				continue
			}
			modelName := strings.TrimPrefix(modelNameValue, "models/")
			allModels = append(allModels, modelName)
		}

		nextPageToken = modelsResponse.NextPageToken
		if nextPageToken == "" {
			break
		}
	}

	return allModels, nil
}

// convertToolChoiceToGeminiConfig converts OpenAI tool_choice to Gemini toolConfig
// OpenAI tool_choice values:
//   - "auto": Let the model decide (default)
//   - "none": Don't call any tools
//   - "required": Must call at least one tool
//   - {"type": "function", "function": {"name": "xxx"}}: Call specific function
//
// Gemini functionCallingConfig.mode values:
//   - "AUTO": Model decides whether to call functions
//   - "NONE": Model won't call functions
//   - "ANY": Model must call at least one function
func convertToolChoiceToGeminiConfig(toolChoice any) *dto.ToolConfig {
	if toolChoice == nil {
		return nil
	}

	// Handle string values: "auto", "none", "required"
	if toolChoiceStr, ok := toolChoice.(string); ok {
		config := &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{},
		}
		switch toolChoiceStr {
		case "auto":
			config.FunctionCallingConfig.Mode = "AUTO"
		case "none":
			config.FunctionCallingConfig.Mode = "NONE"
		case "required":
			config.FunctionCallingConfig.Mode = "ANY"
		default:
			// Unknown string value, default to AUTO
			config.FunctionCallingConfig.Mode = "AUTO"
		}
		return config
	}

	// Handle object value: {"type": "function", "function": {"name": "xxx"}}
	if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if toolChoiceMap["type"] == "function" {
			config := &dto.ToolConfig{
				FunctionCallingConfig: &dto.FunctionCallingConfig{
					Mode: "ANY",
				},
			}
			// Extract function name if specified
			if function, ok := toolChoiceMap["function"].(map[string]interface{}); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
				}
			}
			return config
		}
		// Unsupported map structure (type is not "function"), return nil
		return nil
	}

	// Unsupported type, return nil
	return nil
}
