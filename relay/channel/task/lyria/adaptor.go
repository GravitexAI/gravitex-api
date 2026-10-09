package lyria

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	ProModelName  = "lyria-3-pro-preview"
	ClipModelName = "lyria-3-clip-preview"
)

var nativeInteractionModels = map[string]struct{}{
	"gemini-2.5-flash-image":         {},
	"gemini-3-flash-preview":         {},
	"gemini-3.1-flash-image-preview": {},
	"gemini-3-pro-image-preview":     {},
	"gemini-3.5-flash":               {},
	"gemini-3.1-flash-image":         {},
	"gemini-3-pro-image":             {},
	"gemini-3.1-flash-lite-image":    {},
	"gemini-2.5-flash":               {},
	"gemini-2.5-pro":                 {},
	"gemini-2.5-flash-lite":          {},
	"gemini-nano-banana-2.1":         {},
}

func IsNativeInteractionModel(name string) bool {
	_, ok := nativeInteractionModels[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func IsNativeInteractionGenerateContentModel(name string) bool {
	modelName := strings.ToLower(strings.TrimSpace(name))
	return IsNativeInteractionModel(modelName) && !strings.HasPrefix(modelName, "veo-")
}

func isGenerateContentImageModel(name string) bool {
	modelName := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(modelName, "image") || strings.Contains(modelName, "nano-banana")
}

func IsLyriaModel(name string) bool {
	return name == ProModelName || name == ClipModelName
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL     string
	apiKey      string
	channelType int
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	a.apiKey = info.ApiKey
	a.channelType = info.ChannelType
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); err != nil {
		return err
	}
	applyNativeInteractionVideoBillingMetadata(c)
	info.Action = nativeInteractionTaskAction(info.OriginModelName)
	return nil
}

func nativeInteractionTaskAction(modelName string) string {
	if IsLyriaModel(modelName) {
		return "song"
	}
	if IsNativeInteractionGenerateContentModel(modelName) {
		// textGenerate is a legacy alias for text_to_video. Gemini
		// GenerateContent models must remain non-video tasks for persistence,
		// billing, and task-list classification.
		return constant.TaskActionGenerateContent
	}
	return constant.TaskActionTextGenerate
}

func applyNativeInteractionVideoBillingMetadata(c *gin.Context) {
	if c == nil {
		return
	}
	var raw []byte
	if value, ok := c.Get(common.KeyLyriaRawRequestBody); ok {
		raw, _ = value.([]byte)
	}
	if len(raw) == 0 {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return
		}
		raw, _ = storage.Bytes()
	}
	var request map[string]any
	if common.Unmarshal(raw, &request) != nil {
		return
	}
	config, _ := request["generation_config"].(map[string]any)
	videoConfig, _ := config["video_config"].(map[string]any)
	if videoConfig == nil {
		return
	}
	metadata := map[string]any{}
	if value, ok := videoConfig["durationSeconds"]; ok {
		if seconds, ok := nativeInteractionDurationSeconds(value); ok {
			c.Set("video_seconds", seconds)
			metadata["durationSeconds"] = seconds
		}
	}
	if resolution, ok := videoConfig["resolution"].(string); ok && strings.TrimSpace(resolution) != "" {
		c.Set("video_resolution", resolution)
		metadata["resolution"] = resolution
	}
	for _, key := range []string{"generateAudio", "includeAudio"} {
		if value, ok := videoConfig[key]; ok {
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return
	}
	v, ok := c.Get("task_request")
	if !ok {
		return
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	for key, value := range metadata {
		req.Metadata[key] = value
	}
	c.Set("task_request", req)
}

func nativeInteractionDurationSeconds(value any) (int, bool) {
	var seconds float64
	switch typed := value.(type) {
	case float64:
		seconds = typed
	case int:
		seconds = float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		seconds = parsed
	default:
		return 0, false
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return 0, false
	}
	if seconds > float64(relaycommon.MaxTaskDurationSeconds) {
		seconds = float64(relaycommon.MaxTaskDurationSeconds)
	}
	return int(seconds), true
}

func isVertexInteractionsEndpoint(baseURL string) bool {
	value := strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(value, "aiplatform.googleapis.com") || strings.Contains(value, "/v1beta1/projects/")
}

func isServiceAccountJSON(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), "{") && strings.Contains(key, `"project_id"`)
}

func isVertexLyriaInteraction(info *relaycommon.RelayInfo) bool {
	if info == nil || !info.NativeInteractions || info.ChannelType != constant.ChannelTypeVertexAi {
		return false
	}
	return IsLyriaModel(info.OriginModelName)
}

func isVertexNativeInteraction(info *relaycommon.RelayInfo) bool {
	return info != nil && info.NativeInteractions && info.ChannelType == constant.ChannelTypeVertexAi &&
		IsNativeInteractionModel(info.OriginModelName)
}

func isVertexGenerateContentModel(info *relaycommon.RelayInfo) bool {
	return info != nil && info.NativeInteractions && info.ChannelType == constant.ChannelTypeVertexAi &&
		IsNativeInteractionGenerateContentModel(info.OriginModelName)
}

func buildVertexInteractionsURL(baseURL, key string) (string, error) {
	var credentials vertexcore.Credentials
	if err := common.Unmarshal([]byte(key), &credentials); err != nil {
		return "", fmt.Errorf("failed to decode Vertex credentials: %w", err)
	}
	if strings.TrimSpace(credentials.ProjectID) == "" {
		return "", errors.New("Vertex credentials missing project_id")
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	// A type-41 request may enter through the Gemini-compatible route, but its
	// Vertex service-account credential must never be sent to the Gemini
	// Developer API host. Empty base selects the canonical aiplatform host.
	if strings.Contains(strings.ToLower(base), "generativelanguage.googleapis.com") {
		base = ""
	}
	if strings.Contains(base, "/v1beta1/projects/") {
		if strings.HasSuffix(base, "/interactions") {
			return base, nil
		}
		return base + "/interactions", nil
	}
	return vertexcore.BuildAPIBaseURL(base, "v1beta1", credentials.ProjectID, "global") + "/interactions", nil
}

func shouldUseVertexInteractions(info *relaycommon.RelayInfo, baseURL, key string) bool {
	return isVertexLyriaInteraction(info) || (isVertexNativeInteraction(info) && !isVertexGenerateContentModel(info)) ||
		isVertexInteractionsEndpoint(baseURL) || isServiceAccountJSON(key)
}

func buildVertexGenerateContentURL(baseURL, key, modelName string) (string, error) {
	if !isServiceAccountJSON(key) {
		base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
		if base == "" || strings.Contains(strings.ToLower(base), "aiplatform.googleapis.com") {
			base = "https://generativelanguage.googleapis.com"
		}
		return fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, url.PathEscape(modelName)), nil
	}
	var credentials vertexcore.Credentials
	if err := common.Unmarshal([]byte(key), &credentials); err != nil {
		return "", fmt.Errorf("failed to decode Vertex credentials: %w", err)
	}
	if strings.TrimSpace(credentials.ProjectID) == "" {
		return "", errors.New("Vertex credentials missing project_id")
	}
	return fmt.Sprintf("https://aiplatform.googleapis.com/v1beta1/projects/%s/locations/global/publishers/google/models/%s:generateContent",
		credentials.ProjectID, url.PathEscape(modelName)), nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if isVertexGenerateContentModel(info) {
		return buildVertexGenerateContentURL(a.baseURL, a.apiKey, info.OriginModelName)
	}
	if shouldUseVertexInteractions(info, a.baseURL, a.apiKey) {
		return buildVertexInteractionsURL(a.baseURL, a.apiKey)
	}
	if a.baseURL == "" {
		a.baseURL = "https://generativelanguage.googleapis.com"
	}
	return a.baseURL + "/v1beta/interactions", nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if isVertexGenerateContentModel(info) {
		if !isServiceAccountJSON(a.apiKey) {
			req.Header.Set("x-goog-api-key", a.apiKey)
			return nil
		}
	}
	if shouldUseVertexInteractions(info, a.baseURL, a.apiKey) {
		var credentials vertexcore.Credentials
		if err := common.Unmarshal([]byte(a.apiKey), &credentials); err != nil {
			return fmt.Errorf("failed to decode Vertex credentials: %w", err)
		}
		if strings.TrimSpace(credentials.ProjectID) == "" {
			return errors.New("Vertex credentials missing project_id")
		}
		proxy := ""
		if info != nil {
			proxy = info.ChannelSetting.Proxy
		}
		token, err := vertexcore.AcquireAccessToken(credentials, proxy)
		if err != nil {
			return fmt.Errorf("failed to acquire Vertex access token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("x-goog-user-project", credentials.ProjectID)
		return nil
	}
	req.Header.Set("x-goog-api-key", a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if raw, ok := c.Get(common.KeyLyriaRawRequestBody); ok {
		if body, ok := raw.([]byte); ok {
			if isVertexGenerateContentModel(info) {
				converted, err := convertNativeInteractionToGenerateContent(c, body)
				if err != nil {
					return nil, err
				}
				return bytes.NewReader(converted), nil
			}
			if shouldUseVertexInteractions(info, a.baseURL, a.apiKey) {
				converted, err := convertGoogleTextInputForVertex(body)
				if err != nil {
					return nil, err
				}
				if isGoogleLyriaAsyncCompatibilityRequest(c, info, converted) {
					converted, err = normalizeVertexLyriaAsyncFlags(converted)
					if err != nil {
						return nil, err
					}
				}
				return bytes.NewReader(converted), nil
			}
			if isGoogleLyriaAsyncCompatibilityRequest(c, info, body) {
				converted, err := normalizeVertexLyriaAsyncFlags(body)
				if err != nil {
					return nil, err
				}
				return bytes.NewReader(converted), nil
			}
			return bytes.NewReader(append([]byte(nil), body...)), nil
		}
	}
	if isVertexGenerateContentModel(info) {
		v, ok := c.Get("task_request")
		if !ok {
			return nil, errors.New("task_request not found in context")
		}
		req, ok := v.(relaycommon.TaskSubmitReq)
		if !ok {
			return nil, errors.New("unexpected task_request type")
		}
		return bytes.NewReader(buildGenerateContentBodyFromTaskRequest(req)), nil
	}
	v, ok := c.Get("task_request")
	if !ok {
		return nil, errors.New("task_request not found in context")
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil, errors.New("unexpected task_request type")
	}
	metadata := req.Metadata
	if metadata == nil {
		return nil, errors.New("lyria input is required")
	}
	if shouldUseVertexInteractions(info, a.baseURL, a.apiKey) {
		// Vertex Interactions treats response_format as a structured-response
		// setting; Lyria already returns audio by default, so do not forward the
		// Gemini API's {"type":"audio"} field to Vertex.
		metadata = make(map[string]any, len(req.Metadata))
		for key, value := range req.Metadata {
			metadata[key] = value
		}
		delete(metadata, "response_format")
		if isVertexLyriaAsyncMetadata(info, metadata) {
			metadata["background"] = false
			metadata["store"] = false
		}
	}
	body, err := buildLyriaRequestBody(map[string]any{
		"model":    req.Model,
		"prompt":   req.Prompt,
		"metadata": metadata,
	})
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

func isGoogleLyriaAsyncCompatibilityRequest(c *gin.Context, info *relaycommon.RelayInfo, raw []byte) bool {
	if c == nil || info == nil || !info.NativeInteractions || !IsLyriaModel(info.OriginModelName) {
		return false
	}
	if strings.TrimRight(strings.TrimSpace(c.GetString("native_interactions_original_path")), "/") != "/v1beta/interactions" {
		return false
	}
	var request map[string]any
	if err := common.Unmarshal(raw, &request); err != nil {
		return false
	}
	background, backgroundOK := request["background"].(bool)
	store, storeOK := request["store"].(bool)
	return (backgroundOK && background) || (storeOK && store)
}

func isVertexLyriaAsyncMetadata(info *relaycommon.RelayInfo, metadata map[string]any) bool {
	if info == nil || !isVertexLyriaInteraction(info) || metadata == nil {
		return false
	}
	background, backgroundOK := metadata["background"].(bool)
	store, storeOK := metadata["store"].(bool)
	return (backgroundOK && background) || (storeOK && store)
}

func normalizeVertexLyriaAsyncFlags(raw []byte) ([]byte, error) {
	var request map[string]any
	if err := common.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	request["background"] = false
	request["store"] = false
	return common.Marshal(request)
}

// convertGoogleTextInputForVertex adapts the Google AI text-only Interactions
// shape to Vertex Lyria's documented content-part shape. Multimodal input and
// all other provider parameters remain untouched; provider validation stays
// with the upstream service.
func convertGoogleTextInputForVertex(raw []byte) ([]byte, error) {
	var request map[string]any
	if err := common.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if input, ok := request["input"].(string); ok {
		request["input"] = []any{map[string]any{
			"type": "text",
			"text": input,
		}}
	}
	return common.Marshal(request)
}

func convertNativeInteractionToGenerateContent(c *gin.Context, raw []byte) ([]byte, error) {
	var request map[string]any
	if err := common.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("invalid interaction request: %w", err)
	}
	contents, err := convertInteractionInput(request["input"])
	if err != nil {
		return nil, err
	}
	if previousID, ok := request["previous_interaction_id"].(string); ok && strings.TrimSpace(previousID) != "" {
		previous, err := loadPreviousInteractionContents(c, previousID)
		if err != nil {
			return nil, err
		}
		contents = append(previous, contents...)
	}
	if len(contents) == 0 {
		return nil, errors.New("field input is required")
	}
	result := map[string]any{"contents": contents}
	config := convertInteractionGenerationConfig(request["generation_config"])
	if config == nil {
		config = map[string]any{}
	}
	result["generationConfig"] = config
	if _, ok := config["responseModalities"]; !ok {
		modalities := []string{"TEXT"}
		if modelName, ok := request["model"].(string); ok && isGenerateContentImageModel(modelName) {
			modalities = []string{"TEXT", "IMAGE"}
		}
		config["responseModalities"] = modalities
	}
	if systemInstruction := convertInteractionSystemInstruction(request["system_instruction"]); systemInstruction != nil {
		result["systemInstruction"] = systemInstruction
	}
	if safetySettings := convertInteractionSafetySettings(request["safety_settings"]); len(safetySettings) > 0 {
		result["safetySettings"] = safetySettings
	}
	if toolConfig := convertInteractionToolConfig(request["tool_config"]); toolConfig != nil {
		result["toolConfig"] = toolConfig
	}
	copyInteractionField(request, result, "cached_content", "cachedContent")
	copyInteractionField(request, result, "labels", "labels")
	if tools := convertInteractionTools(request["tools"]); len(tools) > 0 {
		result["tools"] = tools
	}
	applyInteractionResponseFormat(config, request["response_format"])
	return common.Marshal(result)
}

func convertInteractionInput(input any) ([]any, error) {
	if text, ok := input.(string); ok {
		return []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": text}}}}, nil
	}
	items, ok := input.([]any)
	if !ok {
		return nil, errors.New("field input is required")
	}
	parts := make([]any, 0, len(items))
	for _, item := range items {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := part["type"].(string)
		switch typ {
		case "text":
			if text, ok := part["text"].(string); ok && text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
		case "image", "video", "audio", "document":
			media, err := convertInteractionMediaPart(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, media)
		}
	}
	if len(parts) == 0 {
		return nil, errors.New("field input is required")
	}
	return []any{map[string]any{"role": "user", "parts": parts}}, nil
}

func convertInteractionMediaPart(part map[string]any) (map[string]any, error) {
	data, _ := part["data"].(string)
	uri, _ := part["uri"].(string)
	if imageURL, ok := part["image_url"].(map[string]any); ok {
		if urlValue, ok := imageURL["url"].(string); ok {
			data = urlValue
		}
	}
	if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") || strings.HasPrefix(data, "gs://") {
		uri = data
		data = ""
	}
	mimeType, _ := part["mime_type"].(string)
	if mimeType == "" {
		mimeType, _ = part["mimeType"].(string)
	}
	if strings.HasPrefix(data, "data:") {
		header, payload, found := strings.Cut(data, ",")
		if !found {
			return nil, errors.New("invalid data URI")
		}
		data = payload
		if mimeType == "" {
			mimeType = strings.TrimPrefix(header, "data:")
			if semicolon := strings.IndexByte(mimeType, ';'); semicolon >= 0 {
				mimeType = mimeType[:semicolon]
			}
		}
	}
	if data != "" {
		if mimeType == "" {
			return nil, errors.New("media mime_type is required for inline data")
		}
		return map[string]any{"inlineData": map[string]any{"mimeType": mimeType, "data": data}}, nil
	}
	if uri != "" {
		return map[string]any{"fileData": map[string]any{"mimeType": mimeType, "fileUri": uri}}, nil
	}
	return nil, errors.New("media data or uri is required")
}

func copyInteractionField(source, target map[string]any, from, to string) {
	if value, ok := source[from]; ok && value != nil {
		target[to] = value
	}
}

func convertInteractionSystemInstruction(value any) map[string]any {
	switch instruction := value.(type) {
	case string:
		if instruction != "" {
			return map[string]any{"parts": []any{map[string]any{"text": instruction}}}
		}
	case []any:
		parts := []any{}
		for _, item := range instruction {
			if part, ok := item.(map[string]any); ok {
				if text, ok := part["text"].(string); ok && text != "" {
					parts = append(parts, map[string]any{"text": text})
				}
			}
		}
		if len(parts) > 0 {
			return map[string]any{"parts": parts}
		}
	}
	return nil
}

func convertInteractionSafetySettings(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		setting, ok := item.(map[string]any)
		if !ok {
			continue
		}
		converted := map[string]any{}
		for key, value := range setting {
			switch key {
			case "harm_category":
				converted["category"] = value
			case "harm_block_threshold":
				converted["threshold"] = value
			default:
				converted[key] = value
			}
		}
		result = append(result, converted)
	}
	return result
}

func convertInteractionToolConfig(value any) map[string]any {
	config, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := map[string]any{}
	for key, item := range config {
		switch key {
		case "function_calling_config":
			result["functionCallingConfig"] = item
		default:
			result[key] = item
		}
	}
	if functionConfig, ok := result["functionCallingConfig"].(map[string]any); ok {
		converted := map[string]any{}
		for key, item := range functionConfig {
			if key == "allowed_function_names" {
				converted["allowedFunctionNames"] = item
			} else {
				converted[key] = item
			}
		}
		result["functionCallingConfig"] = converted
	}
	return result
}

func convertInteractionGenerationConfig(value any) map[string]any {
	input, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	known := map[string]string{
		"stop_sequences": "stopSequences", "response_mime_type": "responseMimeType",
		"response_modalities": "responseModalities", "thinking_config": "thinkingConfig",
		"top_p": "topP", "top_k": "topK", "candidate_count": "candidateCount",
		"max_output_tokens": "maxOutputTokens", "response_logprobs": "responseLogprobs",
		"logprobs": "logprobs", "presence_penalty": "presencePenalty",
		"frequency_penalty": "frequencyPenalty", "response_schema": "responseSchema",
		"response_json_schema": "responseJsonSchema", "audio_timestamp": "audioTimestamp",
		"media_resolution": "mediaResolution", "speech_config": "speechConfig",
		"enable_affective_dialog": "enableAffectiveDialog", "image_config": "imageConfig",
		"seed": "seed", "temperature": "temperature",
	}
	for key, item := range input {
		if mapped, ok := known[key]; ok {
			if key == "thinking_config" {
				result[mapped] = convertThinkingConfig(item)
			} else {
				result[mapped] = item
			}
		} else if strings.Contains(key, "_") {
			continue
		} else {
			result[key] = item
		}
	}
	if modalities, ok := result["responseModalities"].([]any); ok {
		for i, modality := range modalities {
			if text, ok := modality.(string); ok {
				modalities[i] = strings.ToUpper(text)
			}
		}
	}
	return result
}

func convertThinkingConfig(value any) any {
	input, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := map[string]any{}
	for key, item := range input {
		switch key {
		case "thinking_level":
			result["thinkingLevel"] = item
		case "include_thoughts":
			result["includeThoughts"] = item
		default:
			result[key] = item
		}
	}
	return result
}

func applyInteractionResponseFormat(config map[string]any, value any) {
	format, ok := value.(map[string]any)
	if !ok {
		if formats, ok := value.([]any); ok && len(formats) > 0 {
			format, _ = formats[0].(map[string]any)
		}
	}
	if format == nil {
		return
	}
	formatType, _ := format["type"].(string)
	if formatType == "image" {
		config["responseModalities"] = []string{"IMAGE"}
		imageConfig := map[string]any{}
		for from, to := range map[string]string{
			"aspect_ratio": "aspectRatio",
			"image_size":   "imageSize",
		} {
			if item, ok := format[from]; ok && item != nil {
				imageConfig[to] = item
			}
		}
		if len(imageConfig) > 0 {
			config["imageConfig"] = imageConfig
		}
		return
	}
	if formatType != "json_schema" && formatType != "json_object" && formatType != "json" {
		return
	}
	config["responseMimeType"] = "application/json"
	for _, key := range []string{"schema", "json_schema", "jsonSchema"} {
		if schema, ok := format[key]; ok {
			if schemaMap, ok := schema.(map[string]any); ok {
				if nested, exists := schemaMap["schema"]; exists {
					schema = nested
				}
			}
			config["responseSchema"] = schema
			return
		}
	}
}

func convertInteractionTools(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := []any{}
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if function, ok := tool["function"].(map[string]any); ok {
			result = append(result, map[string]any{"functionDeclarations": []any{map[string]any{
				"name": toolString(function, "name"), "description": toolString(function, "description"),
				"parameters": firstMap(function, "parameters", "parameters_json_schema"),
			}}})
			continue
		}
		if toolString(tool, "name") != "" {
			result = append(result, map[string]any{"functionDeclarations": []any{map[string]any{
				"name": toolString(tool, "name"), "description": toolString(tool, "description"),
				"parameters": firstMap(tool, "parameters", "parameters_json_schema"),
			}}})
		}
	}
	return result
}

func toolString(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

func firstMap(value map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if result, ok := value[key].(map[string]any); ok {
			return result
		}
	}
	return nil
}

func loadPreviousInteractionContents(c *gin.Context, taskID string) ([]any, error) {
	if c == nil || c.GetInt("id") <= 0 {
		return nil, errors.New("previous_interaction_id requires a local interaction task")
	}
	task, exists, err := model.GetByTaskId(c.GetInt("id"), taskID)
	if err != nil || !exists || task == nil {
		return nil, fmt.Errorf("previous interaction not found: %s", taskID)
	}
	var request map[string]any
	if err := common.Unmarshal(task.UpstreamRequestBody, &request); err != nil {
		return nil, fmt.Errorf("invalid previous interaction request: %w", err)
	}
	contents, _ := request["contents"].([]any)
	if len(contents) == 0 {
		return nil, errors.New("previous interaction has no conversation contents")
	}
	var response map[string]any
	if err := common.Unmarshal(task.Data, &response); err != nil {
		return nil, fmt.Errorf("invalid previous interaction response: %w", err)
	}
	outputs, _ := response["outputs"].([]any)
	parts := make([]any, 0, len(outputs))
	for _, output := range outputs {
		part, ok := output.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := part["type"].(string)
		switch typ {
		case "text":
			if text, ok := part["text"].(string); ok && text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
		case "image", "video", "audio":
			data, _ := part["data"].(string)
			mimeType, _ := part["mime_type"].(string)
			if data != "" && mimeType != "" {
				parts = append(parts, map[string]any{"inlineData": map[string]any{
					"mimeType": mimeType,
					"data":     data,
				}})
			}
		}
	}
	if len(parts) > 0 {
		contents = append(contents, map[string]any{"role": "model", "parts": parts})
	}
	return contents, nil
}

func buildGenerateContentBodyFromTaskRequest(req relaycommon.TaskSubmitReq) []byte {
	modalities := []string{"TEXT"}
	if isGenerateContentImageModel(req.Model) {
		modalities = []string{"TEXT", "IMAGE"}
	}
	config := map[string]any{"responseModalities": modalities}
	return mustMarshalGenerateContent(map[string]any{
		"contents": []any{map[string]any{
			"role":  "user",
			"parts": []any{map[string]any{"text": req.Prompt}},
		}},
		"generationConfig": config,
	})
}

func mustMarshalGenerateContent(value any) []byte {
	data, _ := common.Marshal(value)
	return data
}

func convertGenerateContentResponse(raw []byte, modelName string) ([]byte, error) {
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text       string `json:"text"`
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Usage struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := common.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	outputs := []any{}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				outputs = append(outputs, map[string]any{"type": "text", "text": part.Text})
			}
			if part.InlineData != nil && part.InlineData.Data != "" {
				outputs = append(outputs, map[string]any{
					"type":      "image",
					"data":      part.InlineData.Data,
					"mime_type": part.InlineData.MimeType,
				})
			}
		}
	}
	usage := map[string]any{
		"total_input_tokens":  response.Usage.PromptTokenCount,
		"total_output_tokens": response.Usage.CandidatesTokenCount,
		"total_tokens":        response.Usage.TotalTokenCount,
	}
	if response.Usage.TotalTokenCount == 0 {
		usage["total_tokens"] = response.Usage.PromptTokenCount + response.Usage.CandidatesTokenCount
	}
	return common.Marshal(map[string]any{
		"id":      model.GenerateInteractionID(),
		"object":  "interaction",
		"model":   modelName,
		"status":  "completed",
		"outputs": outputs,
		"usage":   usage,
	})
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, body)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *taskdto.TaskError) {
	if resp == nil || resp.Body == nil {
		return "", nil, service.TaskErrorWrapper(errors.New("upstream response is empty"), "invalid_response", http.StatusBadGateway)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	var interaction struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	preservedVertexUsage := false
	if isVertexGenerateContentModel(info) {
		var upstreamResponse map[string]any
		if common.Unmarshal(body, &upstreamResponse) == nil {
			if usage, ok := upstreamResponse["usageMetadata"].(map[string]any); ok && info != nil {
				// Preserve the exact Vertex usage object before converting the
				// response to the Interactions usage shape.
				info.SetUpstreamResponsesField("usageMetadata", usage)
				preservedVertexUsage = true
			}
		}
		body, err = convertGenerateContentResponse(body, info.OriginModelName)
		if err != nil {
			return "", nil, service.TaskErrorWrapper(err, "invalid_response", http.StatusBadGateway)
		}
	}
	if info != nil {
		var response map[string]any
		if common.Unmarshal(body, &response) == nil {
			if usage, ok := response["usage"].(map[string]any); ok && !preservedVertexUsage {
				info.SetUpstreamResponsesField("usage", usage)
			}
		}
	}
	_ = common.Unmarshal(body, &interaction)
	if strings.TrimSpace(interaction.ID) != "" && info != nil {
		info.PublicTaskID = interaction.ID
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	// A background worker has no client response writer. Keep the raw provider
	// body in the submit result and let the worker update tasks/logs instead.
	if !c.GetBool("native_interactions_worker") {
		c.Data(resp.StatusCode, contentType, body)
	}
	return interaction.ID, body, nil
}

func (a *TaskAdaptor) GetModelList() []string { return []string{ProModelName, ClipModelName} }
func (a *TaskAdaptor) GetChannelName() string { return "lyria" }

func buildLyriaPublicResponse(body []byte, modelName string) ([]byte, error) {
	var response map[string]any
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	response["object"] = "interaction"
	if _, ok := response["model"]; !ok && modelName != "" {
		response["model"] = modelName
	}
	return common.Marshal(response)
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid task_id")
	}
	pollURL, useVertex, err := buildLyriaPollURL(baseURL, key, taskID, a.shouldUseVertexPolling(baseURL, key))
	if err != nil {
		return nil, err
	}
	common.SysLog(fmt.Sprintf("[LyriaPoll] endpoint=%s url=%s", map[bool]string{true: "vertex", false: "gemini"}[useVertex], pollURL))
	req, err := http.NewRequest(http.MethodGet, pollURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if useVertex {
		var credentials vertexcore.Credentials
		if err := common.Unmarshal([]byte(key), &credentials); err != nil {
			return nil, fmt.Errorf("failed to decode Vertex credentials: %w", err)
		}
		token, err := vertexcore.AcquireAccessToken(credentials, proxy)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire Vertex access token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("x-goog-user-project", credentials.ProjectID)
	} else {
		req.Header.Set("x-goog-api-key", key)
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (a *TaskAdaptor) shouldUseVertexPolling(baseURL, key string) bool {
	return a.channelType == constant.ChannelTypeVertexAi || isVertexInteractionsEndpoint(baseURL) || isServiceAccountJSON(key)
}

func buildLyriaPollURL(baseURL, key, taskID string, useVertex bool) (string, bool, error) {
	escapedTaskID := url.PathEscape(strings.TrimSpace(taskID))
	if useVertex {
		vertexURL, err := buildVertexInteractionsURL(baseURL, key)
		if err != nil {
			return "", true, err
		}
		return vertexURL + "/" + escapedTaskID, true, nil
	}
	return strings.TrimRight(baseURL, "/") + "/v1beta/interactions/" + escapedTaskID, false, nil
}

func (a *TaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	return parseInteractionResult(body)
}

func buildLyriaRequestBody(request map[string]any) ([]byte, error) {
	metadata, _ := request["metadata"].(map[string]any)
	input, ok := metadata["input"]
	if !ok {
		input = request["prompt"]
	}
	modelName, _ := request["model"].(string)
	if !IsLyriaModel(modelName) {
		return nil, fmt.Errorf("unsupported lyria model: %s", modelName)
	}
	result := map[string]any{"model": modelName, "input": input}
	if modelName == ProModelName {
		if format, ok := metadata["response_format"]; ok {
			result["response_format"] = format
		}
	}
	if background, ok := metadata["background"]; ok {
		result["background"] = background
	}
	if store, ok := metadata["store"]; ok {
		result["store"] = store
	}
	if previous, ok := metadata["previous_interaction_id"]; ok {
		result["previous_interaction_id"] = previous
	}
	return json.Marshal(result)
}

type lyriaOutputBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

type lyriaInteractionError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func lyriaErrorReason(providerError lyriaInteractionError) string {
	reason := providerError.Message
	if providerError.Code == nil {
		return reason
	}
	code := strings.TrimSpace(fmt.Sprint(providerError.Code))
	if providerError.Status != "" {
		code = strings.ToLower(providerError.Status)
	}
	if code == "" {
		return reason
	}
	return code + ": " + reason
}

func parseInteractionResult(body []byte) (*relaycommon.TaskInfo, error) {
	var interaction struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Steps  []struct {
			Type    string             `json:"type"`
			Content []lyriaOutputBlock `json:"content"`
		} `json:"steps"`
		Outputs     []lyriaOutputBlock `json:"outputs"`
		OutputAudio *lyriaOutputBlock  `json:"output_audio"`
		OutputText  string             `json:"output_text"`
		Usage       struct {
			TotalInputTokens  int `json:"total_input_tokens"`
			TotalOutputTokens int `json:"total_output_tokens"`
			TotalTokens       int `json:"total_tokens"`
		} `json:"usage"`
		Error  lyriaInteractionError   `json:"error"`
		Errors []lyriaInteractionError `json:"errors"`
	}
	if err := common.Unmarshal(body, &interaction); err != nil {
		return nil, err
	}
	result := &relaycommon.TaskInfo{TaskID: interaction.ID, Metadata: map[string]any{}}
	result.UsageFacts = map[string]any{}
	result.InputTokens = interaction.Usage.TotalInputTokens
	result.CompletionTokens = interaction.Usage.TotalOutputTokens
	result.TotalTokens = interaction.Usage.TotalTokens
	if result.TotalTokens == 0 {
		result.TotalTokens = result.InputTokens + result.CompletionTokens
	}
	result.UsageFacts["total_input_tokens"] = result.InputTokens
	result.UsageFacts["total_output_tokens"] = result.CompletionTokens
	result.UsageFacts["total_tokens"] = result.TotalTokens
	providerError := interaction.Error
	if providerError.Message == "" && len(interaction.Errors) > 0 {
		providerError = interaction.Errors[0]
	}
	if providerError.Message != "" {
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = lyriaErrorReason(providerError)
		return result, nil
	}
	status := strings.ToUpper(strings.TrimSpace(interaction.Status))
	var lyrics []string
	outputBlocks := append([]lyriaOutputBlock(nil), interaction.Outputs...)
	if interaction.OutputAudio != nil {
		outputBlocks = append(outputBlocks, *interaction.OutputAudio)
	}
	if interaction.OutputText != "" {
		lyrics = append(lyrics, interaction.OutputText)
	}
	for _, step := range interaction.Steps {
		if step.Type != "model_output" {
			continue
		}
		outputBlocks = append(outputBlocks, step.Content...)
	}
	for _, block := range outputBlocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				lyrics = append(lyrics, block.Text)
			}
		case "audio":
			if block.Data != "" {
				mime := block.MimeType
				if mime == "" {
					mime = "audio/mpeg"
				}
				result.Url = "data:" + mime + ";base64," + block.Data
			}
		}
	}
	if len(lyrics) > 0 {
		result.Metadata["lyrics"] = strings.Join(lyrics, "\n")
	}
	switch status {
	case "COMPLETED", "SUCCEEDED":
		result.Status, result.Progress = model.TaskStatusSuccess, "100%"
		if result.Url == "" && len(outputBlocks) == 0 && len(lyrics) == 0 {
			result.Status = model.TaskStatusFailure
			result.Reason = "completed_without_audio: Vertex returned COMPLETED without audio output"
		}
	case "FAILED":
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = "failed: Vertex interaction failed without error details"
	case "CANCELLED", "CANCELED":
		result.Status, result.Progress = string(model.TaskStatusCancelled), "100%"
		result.Reason = "cancelled: Vertex interaction was cancelled"
	case "INCOMPLETE":
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = "incomplete: Vertex returned incomplete results"
	case "BUDGET_EXCEEDED":
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = "budget_exceeded: Vertex interaction budget was exceeded"
	case "REQUIRES_ACTION":
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = "requires_action: Vertex interaction requires unsupported user action"
	case "IN_PROGRESS", "QUEUED":
		// These two Lyria models are submitted with store=false. A non-terminal
		// response therefore has no retrievable Vertex resource to poll and must
		// not leave a permanently running local task.
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = fmt.Sprintf("non_terminal_response_not_retrievable: Vertex returned %s while store=false", status)
	case "UNSPECIFIED":
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = "invalid_response: Vertex returned UNSPECIFIED interaction status"
	case "":
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = "invalid_response: Vertex response is missing interaction status"
	default:
		result.Status, result.Progress = model.TaskStatusFailure, "100%"
		result.Reason = fmt.Sprintf("invalid_response: Vertex returned unknown interaction status %s", status)
	}
	return result, nil
}

// ParseVertexHTTPFailure converts a non-2xx Vertex response into a terminal
// task result while leaving the original HTTP status and body untouched for
// the client-facing raw mirror response.
func ParseVertexHTTPFailure(statusCode int, body []byte) *relaycommon.TaskInfo {
	if parsed, err := parseInteractionResult(body); err == nil && parsed.Reason != "" {
		parsed.Status = model.TaskStatusFailure
		if statusCode == 499 {
			parsed.Status = string(model.TaskStatusCancelled)
		}
		parsed.Progress = "100%"
		return parsed
	}
	code := map[int]string{
		http.StatusBadRequest:          "invalid_argument",
		http.StatusUnauthorized:        "unauthenticated",
		http.StatusForbidden:           "permission_denied",
		http.StatusNotFound:            "not_found",
		http.StatusTooManyRequests:     "resource_exhausted",
		499:                            "cancelled",
		http.StatusInternalServerError: "internal",
		http.StatusServiceUnavailable:  "unavailable",
		http.StatusGatewayTimeout:      "deadline_exceeded",
	}[statusCode]
	if code == "" {
		code = "http_error"
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(statusCode)
	}
	status := string(model.TaskStatusFailure)
	if statusCode == 499 {
		status = string(model.TaskStatusCancelled)
	}
	return &relaycommon.TaskInfo{
		Status:   status,
		Progress: "100%",
		Reason:   fmt.Sprintf("%s: HTTP %d: %s", code, statusCode, message),
		Metadata: map[string]any{"http_status": statusCode},
	}
}
