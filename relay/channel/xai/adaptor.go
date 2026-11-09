package xai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	rootconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type Adaptor struct {
}

// xAI's Responses OpenAPI schema is strict. This list contains only request
// fields documented for /v1/responses that are relevant to a Codex request.
// It is intentionally used only after isXaiGrok46 has matched the exact route.
var xaiGrok46ResponsesFields = map[string]struct{}{
	"background":           {},
	"input":                {},
	"instructions":         {},
	"max_output_tokens":    {},
	"max_tool_calls":       {},
	"metadata":             {},
	"model":                {},
	"parallel_tool_calls":  {},
	"previous_response_id": {},
	"prompt_cache_key":     {},
	"reasoning":            {},
	"safety_identifier":    {},
	"service_tier":         {},
	"store":                {},
	"stream":               {},
	"temperature":          {},
	"text":                 {},
	"tool_choice":          {},
	"tools":                {},
	"top_logprobs":         {},
	"top_p":                {},
	"truncation":           {},
	"user":                 {},
}

// OpenAI 图像接口专有、xAI 图像接口未定义的字段，转发前剔除。
var xaiImageUnsupportedFields = []string{
	"size", "style", "background", "moderation", "output_format", "output_compression",
	"partial_images", "stream", "mask", "input_fidelity", "watermark",
}

var xaiGrok46ToolTypes = map[string]struct{}{
	"function":           {},
	"web_search":         {},
	"x_search":           {},
	"image_generation":   {},
	"collections_search": {},
	"file_search":        {},
	"code_execution":     {},
	"code_interpreter":   {},
	"mcp":                {},
	"shell":              {},
	"tool_search":        {},
}

var xaiGrok46ReasoningEfforts = map[string]struct{}{
	"low":    {},
	"medium": {},
	"high":   {},
	"xhigh":  {},
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	//TODO implement me
	//panic("implement me")
	return nil, errors.New("not available")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//not available
	return nil, errors.New("not available")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	// grok-imagine 的 aspect_ratio / resolution / quality / image / images / deferred 等参数
	// 都要原样透传：已定义字段与 Extra 里的未知字段合并成一个 map 发给上游。
	base, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	body := make(map[string]json.RawMessage, len(request.Extra))
	if err := common.Unmarshal(base, &body); err != nil {
		return nil, err
	}
	for k, v := range request.Extra {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}
	// deferred 模式上游只回 request_id，要靠 GET /v1/images/{request_id} 轮询取图，
	// 网关没有这个轮询接口，放行会让用户被扣费却拿不到图，直接拒绝。
	if bytes.Equal(bytes.TrimSpace(body["deferred"]), []byte("true")) {
		return nil, types.NewErrorWithStatusCode(
			errors.New("deferred image generation is not supported, please remove the deferred parameter"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	// OpenAI SDK 只会传 size，xAI 用 aspect_ratio 控制画幅；客户端没显式传 aspect_ratio 时按 size 换算。
	if _, exists := body["aspect_ratio"]; !exists {
		if ratio := xaiAspectRatioFromSize(request.Size); ratio != "" {
			encoded, err := common.Marshal(ratio)
			if err != nil {
				return nil, err
			}
			body["aspect_ratio"] = encoded
		}
	}
	// OpenAI 专有、xAI 未定义的字段去掉，避免老客户端（如固定带 size 的 SDK）被上游拒绝。
	for _, k := range xaiImageUnsupportedFields {
		delete(body, k)
	}

	// xAI 的参考图只认引用对象（{"url": ...} 或 {"file_id": ...}），且只收 JSON 请求体。
	// OpenAI 习惯的字符串 / 字符串数组转成 {"url": ...}；对象原样保留，
	// 其余取值也原样转发，合法性交给上游判断。
	_, usedImages := body["images"]
	var refs []json.RawMessage
	for _, raw := range []json.RawMessage{body["image"], body["images"]} {
		items := []json.RawMessage{raw}
		if common.GetJsonType(raw) == "array" {
			if err := common.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
		}
		for _, item := range items {
			switch common.GetJsonType(item) {
			case "unknown", "null":
				continue
			case "string":
				var url string
				if err := common.Unmarshal(item, &url); err != nil {
					return nil, err
				}
				if strings.TrimSpace(url) == "" {
					continue
				}
				if item, err = common.Marshal(map[string]string{"url": url}); err != nil {
					return nil, err
				}
			}
			refs = append(refs, item)
		}
	}
	// OpenAI SDK 的 images.edit() 以 multipart 上传图片文件，xAI 不支持表单，
	// 这里把上传的文件转成 data URI，按提交顺序追加到参考图里。
	if c != nil && info.RelayMode == constant.RelayModeImagesEdits {
		dataURIs, err := channel.ImageDataURIsFromForm(c)
		if err != nil {
			return nil, err
		}
		for _, dataURI := range dataURIs {
			ref, err := common.Marshal(map[string]string{"url": dataURI})
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref)
		}
	}
	delete(body, "image")
	delete(body, "images")
	// 单图用 image，多图用 images（两者互斥）；客户端本来就传 images 时保持 images。
	if len(refs) == 1 && !usedImages {
		body["image"] = refs[0]
	} else if len(refs) > 0 {
		encoded, err := common.Marshal(refs)
		if err != nil {
			return nil, err
		}
		body["images"] = encoded
	}

	// xAI 图生图只走 /v1/images/edits；generations 请求带了参考图时改发 edits，
	// 否则上游会忽略参考图、按纯文生图出图。
	if info.RelayMode == constant.RelayModeImagesGenerations && len(refs) > 0 {
		info.RequestURLPath = strings.Replace(info.RequestURLPath, "/images/generations", "/images/edits", 1)
	}
	return body, nil
}

// xaiAspectRatios 是 xAI 图像接口支持的固定画幅（auto 单独处理）。
var xaiAspectRatios = []struct {
	name  string
	value float64
}{
	{"1:1", 1}, {"3:4", 3.0 / 4}, {"4:3", 4.0 / 3}, {"9:16", 9.0 / 16}, {"16:9", 16.0 / 9},
	{"2:3", 2.0 / 3}, {"3:2", 3.0 / 2}, {"9:19.5", 9 / 19.5}, {"19.5:9", 19.5 / 9},
	{"9:20", 9.0 / 20}, {"20:9", 20.0 / 9}, {"1:2", 1.0 / 2}, {"2:1", 2}, {"21:9", 21.0 / 9}, {"5:2", 5.0 / 2},
}

// xaiAspectRatioFromSize 把 OpenAI 的 size（如 1536x1024、auto）换算成 xAI 的 aspect_ratio：
// 取宽高比最接近的支持画幅；size 为空或无法解析时返回空串，不设置 aspect_ratio。
func xaiAspectRatioFromSize(size string) string {
	size = strings.ToLower(strings.TrimSpace(size))
	if size == "auto" {
		return "auto"
	}
	widthStr, heightStr, ok := strings.Cut(size, "x")
	if !ok {
		return ""
	}
	width, errW := strconv.ParseFloat(strings.TrimSpace(widthStr), 64)
	height, errH := strconv.ParseFloat(strings.TrimSpace(heightStr), 64)
	if errW != nil || errH != nil || width <= 0 || height <= 0 || math.IsInf(width, 0) || math.IsInf(height, 0) {
		return ""
	}
	// 按对数距离比较，横竖画幅的偏差同等对待。
	target := math.Log(width / height)
	best, bestDiff := "", math.Inf(1)
	for _, ratio := range xaiAspectRatios {
		if diff := math.Abs(math.Log(ratio.value) - target); diff < bestDiff {
			best, bestDiff = ratio.name, diff
		}
	}
	return best
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	// 非透传时图像请求体总是转换后的 JSON；客户端用 multipart 调 edits 时，不能把表单的 Content-Type 带给上游。
	passThrough := model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled
	if !passThrough && (info.RelayMode == constant.RelayModeImagesGenerations || info.RelayMode == constant.RelayModeImagesEdits) {
		req.Set("Content-Type", "application/json")
	}
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if strings.HasSuffix(info.UpstreamModelName, "-search") {
		info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-search")
		request.Model = info.UpstreamModelName
		toMap := request.ToMap()
		toMap["search_parameters"] = map[string]any{
			"mode": "on",
		}
		return toMap, nil
	}
	if strings.HasPrefix(request.Model, "grok-3-mini") {
		if lo.FromPtrOr(request.MaxCompletionTokens, uint(0)) == 0 && lo.FromPtrOr(request.MaxTokens, uint(0)) != 0 {
			request.MaxCompletionTokens = request.MaxTokens
			request.MaxTokens = nil
		}
		if strings.HasSuffix(request.Model, "-high") {
			request.ReasoningEffort = "high"
			request.Model = strings.TrimSuffix(request.Model, "-high")
		} else if strings.HasSuffix(request.Model, "-low") {
			request.ReasoningEffort = "low"
			request.Model = strings.TrimSuffix(request.Model, "-low")
		}
		info.SetReasoningEffort(request.ReasoningEffort)
		info.UpstreamModelName = request.Model
	}
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//not available
	return nil, errors.New("not available")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if request.Model == "" && info != nil {
		request.Model = info.UpstreamModelName
	}
	return prepareGrok46ResponsesRequest(info, request)
}

// prepareGrok46ResponsesRequest is deliberately scoped to the exact xAI
// channel and mapped Grok 4.6 upstream model. Every other request returns the
// original DTO without parsing or reserializing tools, so no other model's
// outbound body can be changed by this compatibility path.
func prepareGrok46ResponsesRequest(info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, error) {
	if !isXaiGrok46(info) || len(request.Tools) == 0 {
		return request, nil
	}

	var tools []map[string]any
	if err := json.Unmarshal(request.Tools, &tools); err != nil {
		return request, fmt.Errorf("invalid Responses tools for grok-4.6: %w", err)
	}

	changed := false
	filteredTools := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		toolType, _ := tool["type"].(string)
		if _, supported := xaiGrok46ToolTypes[toolType]; !supported {
			// custom, namespace and every other type outside xAI's published
			// union cannot be transformed without changing the call protocol.
			changed = true
			continue
		}
		if !isXaiGrok46ToolStructurallyValid(tool, toolType) {
			changed = true
			continue
		}
		sanitizeXaiGrok46Tool(tool, toolType, &changed)
		filteredTools = append(filteredTools, tool)
	}

	if !changed {
		return request, nil
	}
	encoded, err := json.Marshal(filteredTools)
	if err != nil {
		return request, fmt.Errorf("encode xAI grok-4.6 tools: %w", err)
	}
	request.Tools = encoded
	return request, nil
}

func isXaiGrok46(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil &&
		info.ChannelType == rootconstant.ChannelTypeXai &&
		strings.EqualFold(strings.TrimSpace(info.UpstreamModelName), "grok-4.6")
}

func sanitizeXaiGrok46Tool(tool map[string]any, toolType string, changed *bool) {
	remove := func(field string) {
		if _, ok := tool[field]; ok {
			delete(tool, field)
			*changed = true
		}
	}

	switch toolType {
	case "web_search":
		// The xAI schema explicitly states that all three OpenAI-compatibility
		// fields are rejected when set.
		remove("external_web_access")
		remove("search_context_size")
		remove("user_location")
	case "file_search":
		remove("filters")
		remove("ranking_options")
	case "code_interpreter":
		remove("container")
	case "mcp":
		remove("connector_id")
		remove("require_approval")
	}
}

func isXaiGrok46ToolStructurallyValid(tool map[string]any, toolType string) bool {
	hasNonEmptyString := func(field string) bool {
		value, ok := tool[field].(string)
		return ok && strings.TrimSpace(value) != ""
	}

	switch toolType {
	case "function":
		_, hasParameters := tool["parameters"]
		return hasNonEmptyString("name") && hasParameters
	case "file_search":
		_, hasVectorStoreIDs := tool["vector_store_ids"]
		return hasVectorStoreIDs
	case "mcp":
		return hasNonEmptyString("server_label") && hasNonEmptyString("server_url")
	case "shell":
		_, hasEnvironment := tool["environment"]
		return hasEnvironment
	default:
		return true
	}
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	preparedBody, err := prepareGrok46OutboundRequestBody(info, requestBody)
	if err != nil {
		return nil, err
	}
	return channel.DoApiRequest(a, c, info, preparedBody)
}

// prepareGrok46OutboundRequestBody applies the same narrow tool filtering to
// raw pass-through requests. ResponsesHelper skips ConvertOpenAIResponsesRequest
// when pass-through is enabled, so this adapter-level guard is required to keep
// unsupported Codex tools from reaching xAI in that mode.
func prepareGrok46OutboundRequestBody(info *relaycommon.RelayInfo, requestBody io.Reader) (io.Reader, error) {
	if !isXaiGrok46(info) || requestBody == nil ||
		(info.RelayMode != constant.RelayModeResponses && info.RelayMode != constant.RelayModeResponsesCompact) {
		return requestBody, nil
	}

	rawBody, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, fmt.Errorf("read grok-4.6 outbound request body: %w", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, fmt.Errorf("decode grok-4.6 outbound request body: %w", err)
	}
	changed := false
	for field := range payload {
		if _, supported := xaiGrok46ResponsesFields[field]; !supported {
			delete(payload, field)
			changed = true
		}
	}
	if reasoning, ok := payload["reasoning"]; ok {
		sanitized, reasoningChanged, err := sanitizeXaiGrok46Reasoning(reasoning)
		if err != nil {
			return nil, err
		}
		if reasoningChanged {
			payload["reasoning"] = sanitized
			changed = true
		}
	}
	if input, ok := payload["input"]; ok {
		sanitized, inputChanged, err := sanitizeXaiGrok46Input(input)
		if err != nil {
			return nil, err
		}
		if inputChanged {
			payload["input"] = sanitized
			changed = true
		}
	}

	if tools, ok := payload["tools"]; ok {
		prepared, err := prepareGrok46ResponsesRequest(info, dto.OpenAIResponsesRequest{Tools: tools})
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(tools, prepared.Tools) {
			payload["tools"] = prepared.Tools
			changed = true
		}
	}
	if toolChoice, ok := payload["tool_choice"]; ok {
		keep, err := isXaiGrok46ToolChoice(toolChoice)
		if err != nil {
			return nil, err
		}
		if !keep {
			delete(payload, "tool_choice")
			changed = true
		}
	}
	if !changed {
		return bytes.NewReader(rawBody), nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode grok-4.6 outbound request body: %w", err)
	}
	return bytes.NewReader(encoded), nil
}

func sanitizeXaiGrok46Reasoning(raw json.RawMessage) (json.RawMessage, bool, error) {
	var reasoning map[string]json.RawMessage
	if err := json.Unmarshal(raw, &reasoning); err != nil {
		return nil, false, fmt.Errorf("decode grok-4.6 reasoning: %w", err)
	}
	effortRaw, ok := reasoning["effort"]
	if !ok {
		return raw, false, nil
	}
	var effort string
	if err := json.Unmarshal(effortRaw, &effort); err != nil {
		return nil, false, fmt.Errorf("decode grok-4.6 reasoning effort: %w", err)
	}
	if _, supported := xaiGrok46ReasoningEfforts[effort]; supported {
		return raw, false, nil
	}
	delete(reasoning, "effort")
	encoded, err := json.Marshal(reasoning)
	if err != nil {
		return nil, false, fmt.Errorf("encode grok-4.6 reasoning: %w", err)
	}
	return encoded, true, nil
}

// sanitizeXaiGrok46Input fixes only the incompatible representation emitted by
// Codex when it replays an earlier reasoning item: xAI requires content to be
// an array, while Codex may emit content: null for an otherwise valid item.
func sanitizeXaiGrok46Input(raw json.RawMessage) (json.RawMessage, bool, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		// A string input is valid Responses input and must pass through unchanged.
		return raw, false, nil
	}

	changed := false
	for index, item := range items {
		var inputItem map[string]json.RawMessage
		if err := json.Unmarshal(item, &inputItem); err != nil {
			continue
		}
		var itemType string
		if err := json.Unmarshal(inputItem["type"], &itemType); err != nil || itemType != "reasoning" {
			continue
		}
		content, exists := inputItem["content"]
		if !exists || !bytes.Equal(bytes.TrimSpace(content), []byte("null")) {
			continue
		}
		inputItem["content"] = json.RawMessage(`[]`)
		encoded, err := json.Marshal(inputItem)
		if err != nil {
			return nil, false, fmt.Errorf("encode grok-4.6 reasoning input item: %w", err)
		}
		items[index] = encoded
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, false, fmt.Errorf("encode grok-4.6 input: %w", err)
	}
	return encoded, true, nil
}

func isXaiGrok46ToolChoice(raw json.RawMessage) (bool, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString == "none" || asString == "auto" || asString == "required", nil
	}
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return false, fmt.Errorf("decode grok-4.6 tool_choice: %w", err)
	}
	var toolType string
	if err := json.Unmarshal(asObject["type"], &toolType); err != nil {
		return false, nil
	}
	return toolType == "function", nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case constant.RelayModeImagesGenerations, constant.RelayModeImagesEdits:
		usage, err = openai.OpenaiImageHandler(c, info, resp)
	case constant.RelayModeResponsesCompact:
		if isXaiGrok46(info) {
			usage, err = openai.OaiResponsesCompactionHandler(c, resp)
		} else {
			usage, err = xAIHandler(c, info, resp)
		}
	case constant.RelayModeResponses:
		if info.IsStream {
			usage, err = openai.OaiResponsesStreamHandler(c, info, resp)
		} else {
			usage, err = openai.OaiResponsesHandler(c, info, resp)
		}
	default:
		if info.IsStream {
			usage, err = xAIStreamHandler(c, info, resp)
		} else {
			usage, err = xAIHandler(c, info, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
