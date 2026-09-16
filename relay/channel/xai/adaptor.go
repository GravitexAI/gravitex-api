package xai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	rootconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

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
	xaiRequest := ImageRequest{
		Model:          request.Model,
		Prompt:         request.Prompt,
		N:              int(lo.FromPtrOr(request.N, uint(1))),
		ResponseFormat: request.ResponseFormat,
	}
	return xaiRequest, nil
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
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
