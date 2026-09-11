package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func GetAndValidateRequest(c *gin.Context, format types.RelayFormat) (request dto.Request, err error) {
	relayMode := relayconstant.Path2RelayMode(c.Request.URL.Path)

	switch format {
	case types.RelayFormatOpenAI:
		request, err = GetAndValidateTextRequest(c, relayMode)
	case types.RelayFormatGemini:
		if strings.Contains(c.Request.URL.Path, ":embedContent") {
			request, err = GetAndValidateGeminiEmbeddingRequest(c)
		} else if strings.Contains(c.Request.URL.Path, ":batchEmbedContents") {
			request, err = GetAndValidateGeminiBatchEmbeddingRequest(c)
		} else {
			request, err = GetAndValidateGeminiRequest(c)
		}
	case types.RelayFormatClaude:
		request, err = GetAndValidateClaudeRequest(c)
	case types.RelayFormatOpenAIResponses:
		request, err = GetAndValidateResponsesRequest(c)
	case types.RelayFormatOpenAIResponsesCompaction:
		request, err = GetAndValidateResponsesCompactionRequest(c)
	case types.RelayFormatOpenAIAlphaSearch:
		request, err = GetAndValidateAlphaSearchRequest(c)

	case types.RelayFormatOpenAIImage:
		request, err = GetAndValidOpenAIImageRequest(c, relayMode)
	case types.RelayFormatEmbedding:
		request, err = GetAndValidateEmbeddingRequest(c, relayMode)
	case types.RelayFormatRerank:
		request, err = GetAndValidateRerankRequest(c)
	case types.RelayFormatOpenAIAudio:
		request, err = GetAndValidAudioRequest(c, relayMode)
	case types.RelayFormatOpenAIRealtime:
		request = &dto.BaseRequest{}
	default:
		return nil, fmt.Errorf("unsupported relay format: %s", format)
	}
	return request, err
}

func GetAndValidAudioRequest(c *gin.Context, relayMode int) (*dto.AudioRequest, error) {
	audioRequest := &dto.AudioRequest{}
	err := common.UnmarshalBodyReusable(c, audioRequest)
	if err != nil {
		return nil, err
	}
	switch relayMode {
	case relayconstant.RelayModeAudioSpeech:
		if audioRequest.Model == "" {
			return nil, errors.New("model is required")
		}
	default:
		if audioRequest.Model == "" {
			return nil, errors.New("model is required")
		}
		if audioRequest.ResponseFormat == "" {
			audioRequest.ResponseFormat = "json"
		}
	}
	return audioRequest, nil
}

func GetAndValidateRerankRequest(c *gin.Context) (*dto.RerankRequest, error) {
	var rerankRequest *dto.RerankRequest
	err := common.UnmarshalBodyReusable(c, &rerankRequest)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	if rerankRequest.Query == "" {
		return nil, types.NewError(fmt.Errorf("query is empty"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if len(rerankRequest.Documents) == 0 {
		return nil, types.NewError(fmt.Errorf("documents is empty"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	return rerankRequest, nil
}

func GetAndValidateEmbeddingRequest(c *gin.Context, relayMode int) (*dto.EmbeddingRequest, error) {
	var embeddingRequest *dto.EmbeddingRequest
	err := common.UnmarshalBodyReusable(c, &embeddingRequest)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	if embeddingRequest.Input == nil {
		return nil, fmt.Errorf("input is empty")
	}
	if relayMode == relayconstant.RelayModeModerations && embeddingRequest.Model == "" {
		embeddingRequest.Model = "omni-moderation-latest"
	}
	if relayMode == relayconstant.RelayModeEmbeddings && embeddingRequest.Model == "" {
		embeddingRequest.Model = c.Param("model")
	}
	return embeddingRequest, nil
}

// maxTokensLimit bounds user-supplied max token fields. These values feed
// pre-consume quota math (preConsumedTokens * ratio); an unbounded value can
// overflow the conversion and corrupt billing.
const maxTokensLimit = math.MaxInt32 / 2

func exceedsMaxTokensLimit(values ...*uint) bool {
	for _, v := range values {
		if lo.FromPtrOr(v, uint(0)) > maxTokensLimit {
			return true
		}
	}
	return false
}

func GetAndValidateResponsesRequest(c *gin.Context) (*dto.OpenAIResponsesRequest, error) {
	request := &dto.OpenAIResponsesRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	if request.Input == nil {
		return nil, errors.New("input is required")
	}
	if exceedsMaxTokensLimit(request.MaxOutputTokens) {
		return nil, errors.New("max_output_tokens is invalid")
	}
	return request, nil
}

func GetAndValidateAlphaSearchRequest(c *gin.Context) (*dto.AlphaSearchRequest, error) {
	request := &dto.AlphaSearchRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	rawBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	request.RawBody = rawBody
	return request, nil
}

func GetAndValidateResponsesCompactionRequest(c *gin.Context) (*dto.OpenAIResponsesCompactionRequest, error) {
	request := &dto.OpenAIResponsesCompactionRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	return request, nil
}

// formImageValues 收集图生图表单里的图片文本字段（URL 或 base64），覆盖 OpenAI
// 允许的 image、image[]、image[N] 三种命名，按 image → image[] → image[N] 的顺序拼接。
func formImageValues(formData url.Values) []string {
	values := make([]string, 0, len(formData))
	values = append(values, formData["image"]...)
	values = append(values, formData["image[]"]...)

	indexedKeys := make([]string, 0, len(formData))
	for key := range formData {
		if key != "image[]" && strings.HasPrefix(key, "image[") {
			indexedKeys = append(indexedKeys, key)
		}
	}
	sort.Strings(indexedKeys)
	for _, key := range indexedKeys {
		values = append(values, formData[key]...)
	}

	return lo.Filter(values, func(value string, _ int) bool {
		return strings.TrimSpace(value) != ""
	})
}

// imageEditParsedFormFields 列出 multipart 图生图里已经显式解析进 ImageRequest 的表单
// 字段。不在其中的字段会原样进 Extra，避免 form-data 请求悄悄丢掉渠道专有参数。
var imageEditParsedFormFields = map[string]bool{
	"model": true, "prompt": true, "n": true, "size": true, "quality": true,
	"stream": true, "image": true, "input_fidelity": true, "watermark": true,
	"response_format": true,
}

// formExtraValues 收集表单里网关没有显式解析的字段。OpenAI SDK 走 multipart 时会把嵌套
// 结构展平成括号语法（sequential_image_generation_options[max_images]=4、tags[]=a），
// 这里按同样的规则还原回嵌套对象与数组，再透传给上游。
func formExtraValues(formData url.Values) map[string]json.RawMessage {
	root := make(map[string]any, len(formData))
	for key, values := range formData {
		if len(values) == 0 {
			continue
		}
		segments := parseBracketFormKey(key)
		// image 及其 image[] / image[N] 变体已经作为参考图解析过，不要再进 Extra。
		if imageEditParsedFormFields[segments[0]] || segments[0] == "image" {
			continue
		}
		if !setNestedFormValue(root, segments, values) {
			// 还原不了的括号形态按字面量字段名透传，宁可让上游拒绝也不静默丢弃。
			root[key] = formValuesToJSON(values, false)
		}
	}

	extra := make(map[string]json.RawMessage, len(root))
	for key, value := range root {
		if encoded, err := common.Marshal(value); err == nil {
			extra[key] = encoded
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// parseBracketFormKey 把 a[b][] 这样的表单字段名拆成 ["a", "b", ""]，
// 非法括号语法原样返回单段，交给调用方按字面量处理。
func parseBracketFormKey(key string) []string {
	start := strings.IndexByte(key, '[')
	if start <= 0 || !strings.HasSuffix(key, "]") {
		return []string{key}
	}
	segments := []string{key[:start]}
	for _, segment := range strings.Split(key[start:], "[") {
		if segment == "" {
			continue
		}
		if !strings.HasSuffix(segment, "]") {
			return []string{key}
		}
		segments = append(segments, strings.TrimSuffix(segment, "]"))
	}
	return segments
}

// setNestedFormValue 按解析出的路径把表单值写进嵌套结构，末段为空表示该字段是数组。
// 返回 false 表示这种路径形态无法还原（例如数组元素再嵌套）。
func setNestedFormValue(root map[string]any, segments []string, values []string) bool {
	leaf, path := segments[len(segments)-1], segments[:len(segments)-1]
	isArray := leaf == ""
	if isArray {
		if len(path) == 0 {
			return false
		}
		leaf, path = path[len(path)-1], path[:len(path)-1]
	}

	node := root
	for _, segment := range path {
		if segment == "" {
			return false
		}
		child, ok := node[segment].(map[string]any)
		if !ok {
			if _, exists := node[segment]; exists {
				return false
			}
			child = make(map[string]any)
			node[segment] = child
		}
		node = child
	}
	if _, exists := node[leaf]; exists {
		return false
	}
	node[leaf] = formValuesToJSON(values, isArray)
	return true
}

// formValuesToJSON 把同一字段的表单值编码成 JSON：数组语法或同名多值收敛成数组。
func formValuesToJSON(values []string, forceArray bool) json.RawMessage {
	if !forceArray && len(values) == 1 {
		return formValueToJSON(values[0])
	}
	items := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		if encoded := formValueToJSON(value); encoded != nil {
			items = append(items, encoded)
		}
	}
	encoded, err := common.Marshal(items)
	if err != nil {
		return nil
	}
	return encoded
}

// formValueToJSON 把表单文本值还原成 JSON 值。表单只能传字符串，但上游要的是原始类型，
// 所以合法 JSON 字面量（数字、布尔、对象、数组）原样保留，其余按字符串编码。
func formValueToJSON(value string) json.RawMessage {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		var probe any
		if common.Unmarshal([]byte(trimmed), &probe) == nil {
			return json.RawMessage(trimmed)
		}
	}
	encoded, err := common.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func GetAndValidOpenAIImageRequest(c *gin.Context, relayMode int) (*dto.ImageRequest, error) {
	imageRequest := &dto.ImageRequest{}

	switch relayMode {
	case relayconstant.RelayModeImagesEdits:
		if strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
			form, err := common.ParseMultipartFormReusable(c)
			if err != nil {
				return nil, fmt.Errorf("failed to parse image edit form request: %w", err)
			}
			formData := url.Values(form.Value)
			c.Request.MultipartForm = form
			c.Request.PostForm = formData
			imageRequest.Prompt = formData.Get("prompt")
			imageRequest.Model = formData.Get("model")
			if nValue := strings.TrimSpace(formData.Get("n")); nValue != "" {
				n, err := strconv.Atoi(nValue)
				if err != nil || n < 0 || n > dto.MaxImageN {
					return nil, fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN)
				}
				imageRequest.N = common.GetPointer(uint(n))
			}
			imageRequest.Quality = formData.Get("quality")
			imageRequest.Size = formData.Get("size")
			if streamValue := strings.TrimSpace(formData.Get("stream")); streamValue != "" {
				stream, err := strconv.ParseBool(streamValue)
				if err != nil {
					return nil, fmt.Errorf("invalid stream value: %w", err)
				}
				imageRequest.Stream = common.GetPointer(stream)
			}
			// 与 OpenAI 官方 /v1/images/edits 对齐：image 允许重复出现，也允许写成
			// image[] / image[N]。多张时序列化为数组，单张仍是字符串；此前只取
			// formData.Get("image") 会把多图请求静默截断成第一张。
			if imageValues := formImageValues(formData); len(imageValues) == 1 {
				imageRequest.Image, _ = common.Marshal(imageValues[0])
			} else if len(imageValues) > 1 {
				imageRequest.Image, _ = common.Marshal(imageValues)
			}
			imageRequest.ResponseFormat = formData.Get("response_format")
			// 解析 input_fidelity (图生图保真度)
			if fidelity := formData.Get("input_fidelity"); fidelity != "" {
				imageRequest.InputFidelity = &fidelity
			}
			// 表单里其余字段进 Extra 透传给上游，让 form-data 与 JSON 的参数能力一致
			// （如 seed、sequential_image_generation 等渠道专有参数）。
			imageRequest.Extra = formExtraValues(formData)

			hasWatermark := formData.Has("watermark")
			if hasWatermark {
				watermark := formData.Get("watermark") == "true"
				imageRequest.Watermark = &watermark
			}
			break
		}
		fallthrough
	default:
		err := common.UnmarshalBodyReusable(c, imageRequest)
		if err != nil {
			return nil, err
		}

		if imageRequest.Model == "" {
			//imageRequest.Model = "dall-e-3"
			return nil, errors.New("model is required")
		}

		// n 是计费乘数，必须有上限：超大值或回绕成的巨大无符号数会让配额计算溢出成负扣费。
		// 这是唯一保留的取值校验，其余生图参数一律原样透传，合法性交给上游判断。
		if imageRequest.N != nil && *imageRequest.N > dto.MaxImageN {
			return nil, fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN)
		}

		// size / quality / response_format / n 等参数不再补默认值也不再改写：
		// 网关补的默认值会覆盖上游自己的默认（例如 OpenAI 的 size=auto、quality=auto），
		// 用户没传的字段就该让上游按自己的规则决定。
	}

	return imageRequest, nil
}

func GetAndValidateClaudeRequest(c *gin.Context) (textRequest *dto.ClaudeRequest, err error) {
	textRequest = &dto.ClaudeRequest{}
	err = common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}
	if textRequest.Messages == nil || len(textRequest.Messages) == 0 {
		return nil, errors.New("field messages is required")
	}
	if strings.TrimSpace(textRequest.Model) == "" {
		return nil, errors.New("field model is required")
	}
	if exceedsMaxTokensLimit(textRequest.MaxTokens, textRequest.MaxTokensToSample) {
		return nil, errors.New("max_tokens is invalid")
	}

	//if textRequest.Stream {
	//	relayInfo.IsStream = true
	//}

	return textRequest, nil
}

func GetAndValidateTextRequest(c *gin.Context, relayMode int) (*dto.GeneralOpenAIRequest, error) {
	textRequest := &dto.GeneralOpenAIRequest{}
	err := common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}

	if relayMode == relayconstant.RelayModeModerations && textRequest.Model == "" {
		textRequest.Model = "text-moderation-latest"
	}
	if relayMode == relayconstant.RelayModeEmbeddings && textRequest.Model == "" {
		textRequest.Model = c.Param("model")
	}

	if exceedsMaxTokensLimit(textRequest.MaxTokens, textRequest.MaxCompletionTokens) {
		return nil, errors.New("max_tokens is invalid")
	}
	if strings.TrimSpace(textRequest.Model) == "" {
		return nil, errors.New("model is required")
	}
	if textRequest.WebSearchOptions != nil {
		if textRequest.WebSearchOptions.SearchContextSize != "" {
			validSizes := map[string]bool{
				"high":   true,
				"medium": true,
				"low":    true,
			}
			if !validSizes[textRequest.WebSearchOptions.SearchContextSize] {
				return nil, errors.New("invalid search_context_size, must be one of: high, medium, low")
			}
		} else {
			textRequest.WebSearchOptions.SearchContextSize = "medium"
		}
	}
	switch relayMode {
	case relayconstant.RelayModeCompletions:
		if textRequest.Prompt == "" {
			return nil, errors.New("field prompt is required")
		}
	case relayconstant.RelayModeChatCompletions:
		// For FIM (Fill-in-the-middle) requests with prefix/suffix, messages is optional
		// It will be filled by provider-specific adaptors if needed (e.g., SiliconFlow)。Or it is allowed by model vendor(s) (e.g., DeepSeek)
		if len(textRequest.Messages) == 0 && textRequest.Prefix == nil && textRequest.Suffix == nil {
			return nil, errors.New("field messages is required")
		}
		if err := validateOpenAIMessageContent(textRequest.Messages); err != nil {
			return nil, err
		}
	case relayconstant.RelayModeEmbeddings:
	case relayconstant.RelayModeModerations:
		if textRequest.Input == nil || textRequest.Input == "" {
			return nil, errors.New("field input is required")
		}
	case relayconstant.RelayModeEdits:
		if textRequest.Instruction == "" {
			return nil, errors.New("field instruction is required")
		}
	}
	return textRequest, nil
}

// validateOpenAIMessageContent rejects message content shapes the OpenAI Chat
// Completions schema does not accept (matching the upstream 400). Valid content
// is a string, an array of objects (content parts), or null. Numbers, booleans,
// bare objects, and arrays containing non-object elements are rejected.
func validateOpenAIMessageContent(messages []dto.Message) error {
	for i, msg := range messages {
		switch content := msg.Content.(type) {
		case nil, string:
			// null is allowed (e.g. assistant with tool_calls); string is valid.
		case []any:
			for j, part := range content {
				if _, ok := part.(map[string]any); !ok {
					return fmt.Errorf("invalid type for 'messages[%d].content[%d]': expected an object, but got %s instead", i, j, jsonValueTypeName(part))
				}
			}
		default:
			return fmt.Errorf("invalid type for 'messages[%d].content': expected a string or an array of objects, but got %s instead", i, jsonValueTypeName(content))
		}
	}
	return nil
}

// jsonValueTypeName names a value decoded from JSON into an `any` for error text.
func jsonValueTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case float64, json.Number:
		return "a number"
	case string:
		return "a string"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	default:
		return "an unexpected value"
	}
}

func GetAndValidateGeminiRequest(c *gin.Context) (*dto.GeminiChatRequest, error) {
	request := &dto.GeminiChatRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	if len(request.Contents) == 0 && len(request.Requests) == 0 {
		return nil, errors.New("contents is required")
	}
	if exceedsMaxTokensLimit(request.GenerationConfig.MaxOutputTokens) {
		return nil, errors.New("maxOutputTokens is invalid")
	}

	//if c.Query("alt") == "sse" {
	//	relayInfo.IsStream = true
	//}

	return request, nil
}

func GetAndValidateGeminiEmbeddingRequest(c *gin.Context) (*dto.GeminiEmbeddingRequest, error) {
	request := &dto.GeminiEmbeddingRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	return request, nil
}

func GetAndValidateGeminiBatchEmbeddingRequest(c *gin.Context) (*dto.GeminiBatchEmbeddingRequest, error) {
	request := &dto.GeminiBatchEmbeddingRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	return request, nil
}
