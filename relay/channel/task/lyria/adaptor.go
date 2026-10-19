package lyria

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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

// IsGenerateContentImageModel reports whether a native Interactions model
// bills its output as image tokens. Single source of truth for this check;
// service/task_billing.go keeps a local copy only because this package
// imports service (import cycle).
func IsGenerateContentImageModel(name string) bool {
	return isGenerateContentImageModel(name)
}

func isGenerateContentImageModel(name string) bool {
	modelName := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(modelName, "image") || strings.Contains(modelName, "nano-banana")
}

func nativeInteractionOutputModality(outputText string, outputAudio *lyriaOutputBlock, outputs []lyriaOutputBlock, steps []lyriaInteractionStep) (string, bool) {
	modalities := make([]string, 0, len(outputs)+len(steps))
	if strings.TrimSpace(outputText) != "" {
		modalities = append(modalities, "text")
	}
	if outputAudio != nil {
		modality := strings.ToLower(strings.TrimSpace(outputAudio.Type))
		if modality == "" {
			modality = "audio"
		}
		modalities = append(modalities, modality)
	}
	for _, output := range outputs {
		modalities = append(modalities, strings.ToLower(strings.TrimSpace(output.Type)))
	}
	for _, step := range steps {
		for _, content := range step.Content {
			modalities = append(modalities, strings.ToLower(strings.TrimSpace(content.Type)))
		}
	}
	if len(modalities) == 0 {
		return "", false
	}
	modality := modalities[0]
	switch modality {
	case "text", "image", "audio", "video":
	default:
		return "", false
	}
	for _, current := range modalities[1:] {
		if current != modality {
			return "", false
		}
	}
	return modality, true
}

func nativeInteractionHasImageOutput(outputs []lyriaOutputBlock, steps []lyriaInteractionStep) bool {
	for _, output := range outputs {
		if strings.EqualFold(strings.TrimSpace(output.Type), "image") {
			return true
		}
	}
	for _, step := range steps {
		for _, content := range step.Content {
			if strings.EqualFold(strings.TrimSpace(content.Type), "image") {
				return true
			}
		}
	}
	return false
}

func nativeInteractionHasAudioOutput(outputAudio *lyriaOutputBlock, outputs []lyriaOutputBlock, steps []lyriaInteractionStep) bool {
	if outputAudio != nil {
		return true
	}
	for _, output := range outputs {
		if strings.EqualFold(strings.TrimSpace(output.Type), "audio") {
			return true
		}
	}
	for _, step := range steps {
		for _, content := range step.Content {
			if strings.EqualFold(strings.TrimSpace(content.Type), "audio") {
				return true
			}
		}
	}
	return false
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
	if err := validatePreviousInteractionReference(c, info); err != nil {
		return err
	}
	applyNativeInteractionVideoBillingMetadata(c)
	info.Action = nativeInteractionTaskAction(info)
	return nil
}

// validatePreviousInteractionReference rejects an unresolvable
// previous_interaction_id at request validation time with a 400. Without this
// check the lookup fails inside BuildRequestBody and surfaces as a 500 after
// billing has been pre-consumed. Synchronous GenerateContent interactions are
// not persisted as tasks, so a chained reference can only resolve when it
// points at a stored (background) interaction.
func validatePreviousInteractionReference(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	if !IsNativeInteractionGenerateContentModel(info.OriginModelName) && !isVertexGenerateContentModel(info) {
		return nil
	}
	raw, ok := c.Get(common.KeyLyriaRawRequestBody)
	if !ok {
		return nil
	}
	body, _ := raw.([]byte)
	var request struct {
		PreviousInteractionID string `json:"previous_interaction_id"`
	}
	if common.Unmarshal(body, &request) != nil || strings.TrimSpace(request.PreviousInteractionID) == "" {
		return nil
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		return service.TaskErrorWrapperLocal(errors.New("previous_interaction_id requires an authenticated user"), "invalid_request", http.StatusBadRequest)
	}
	_, exists, err := model.GetByTaskId(userID, strings.TrimSpace(request.PreviousInteractionID))
	if err != nil || !exists {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("previous interaction not found: %s (synchronous interactions are not stored; only background interactions can be referenced)", request.PreviousInteractionID),
			"invalid_request", http.StatusBadRequest)
	}
	return nil
}

func nativeInteractionTaskAction(info *relaycommon.RelayInfo) string {
	modelName := info.OriginModelName
	if IsLyriaModel(modelName) {
		return "song"
	}
	if IsNativeInteractionGenerateContentModel(modelName) || isVertexGenerateContentModel(info) {
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

func vertexExpressModeBaseURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(base)
	if base == "" || strings.Contains(strings.ToLower(base), "generativelanguage.googleapis.com") ||
		(err == nil && strings.HasSuffix(strings.ToLower(parsed.Hostname()), "aiplatform.googleapis.com")) {
		return "https://aiplatform.googleapis.com"
	}
	return base
}

func isVertexLyriaInteraction(info *relaycommon.RelayInfo) bool {
	if info == nil || !info.NativeInteractions || info.ChannelType != constant.ChannelTypeVertexAi {
		return false
	}
	return IsLyriaModel(info.OriginModelName)
}

func isVertexNativeInteraction(info *relaycommon.RelayInfo) bool {
	if info == nil || !info.NativeInteractions || info.ChannelType != constant.ChannelTypeVertexAi {
		return false
	}
	modelName := strings.ToLower(strings.TrimSpace(info.OriginModelName))
	if IsNativeInteractionModel(modelName) {
		return true
	}
	return modelName != "" && !relaycommon.IsGeminiOmniModel(modelName) && !strings.HasPrefix(modelName, "veo-")
}

func isVertexGenerateContentModel(info *relaycommon.RelayInfo) bool {
	return info != nil && info.NativeInteractions && info.ChannelType == constant.ChannelTypeVertexAi &&
		IsVertexGenerateContentModelName(info.OriginModelName)
}

// IsVertexGenerateContentModelName reports whether a Vertex Interactions
// request should use the generic GenerateContent fallback. Models with an
// existing dedicated task/Interactions adapter retain that path.
func IsVertexGenerateContentModelName(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return modelName != "" && !IsLyriaModel(modelName) &&
		!relaycommon.IsGeminiOmniModel(modelName) && !strings.HasPrefix(modelName, "veo-")
}

func buildVertexInteractionsURL(baseURL, key string) (string, error) {
	if !isServiceAccountJSON(key) {
		base := vertexExpressModeBaseURL(baseURL)
		if versionIndex := strings.Index(strings.ToLower(base), "/v1beta1/"); versionIndex >= 0 {
			base = base[:versionIndex]
		}
		return base + "/v1beta1/locations/global/interactions", nil
	}
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
		// Vertex API keys are Express Mode credentials. They must be sent to
		// the Vertex host and use the publisher-model resource path; forwarding
		// them to generativelanguage.googleapis.com makes Vertex channel calls
		// fail with API_KEY_INVALID.
		base := vertexExpressModeBaseURL(baseURL)
		return fmt.Sprintf("%s/v1/publishers/google/models/%s:generateContent", base, url.PathEscape(modelName)), nil
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

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if isVertexGenerateContentModel(info) {
		if !isServiceAccountJSON(a.apiKey) {
			req.Header.Set("x-goog-api-key", a.apiKey)
			return nil
		}
	}
	if c.GetBool("native_interactions_stream") && info.NativeInteractions && shouldUseVertexInteractions(info, a.baseURL, a.apiKey) {
		req.Header.Set("Accept", "text/event-stream")
	}
	if shouldUseVertexInteractions(info, a.baseURL, a.apiKey) {
		if !isServiceAccountJSON(a.apiKey) {
			req.Header.Set("x-goog-api-key", a.apiKey)
			return nil
		}
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
				if isVertexLyriaInteraction(info) {
					converted, err = removeVertexLyriaResponseFormat(converted)
					if err != nil {
						return nil, err
					}
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

// Vertex Lyria returns audio in its Interaction output and does not accept the
// Gemini API's response_format field. Remove it only on the Vertex Lyria path.
func removeVertexLyriaResponseFormat(raw []byte) ([]byte, error) {
	var request map[string]any
	if err := common.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if _, exists := request["response_format"]; !exists {
		return raw, nil
	}
	delete(request, "response_format")
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
	generationConfigInput := request["generation_config"]
	if generationConfigInput == nil {
		// Official clients send camelCase; accept both spellings.
		generationConfigInput = request["generationConfig"]
	}
	modelName, _ := request["model"].(string)
	config := convertInteractionGenerationConfig(generationConfigInput, modelName)
	if config == nil {
		config = map[string]any{}
	}
	result["generationConfig"] = config
	if _, ok := config["responseModalities"]; !ok {
		modalities := []string{"TEXT"}
		modelName, _ := request["model"].(string)
		if isGenerateContentImageModel(modelName) {
			modalities = []string{"TEXT", "IMAGE"}
		} else if format, ok := request["response_format"].(map[string]any); ok && strings.EqualFold(fmt.Sprint(format["type"]), "audio") {
			modalities = []string{"AUDIO"}
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
	applyInteractionResponseFormat(config, request["response_format"], modelName)
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
		if content, ok := part["content"].([]any); ok {
			for _, child := range content {
				childPart, ok := child.(map[string]any)
				if !ok {
					continue
				}
				convertedPart, err := convertInteractionInputPart(childPart)
				if err != nil {
					return nil, err
				}
				if convertedPart != nil {
					parts = append(parts, convertedPart)
				}
			}
			continue
		}
		convertedPart, err := convertInteractionInputPart(part)
		if err != nil {
			return nil, err
		}
		if convertedPart != nil {
			parts = append(parts, convertedPart)
		}
	}
	if len(parts) == 0 {
		return nil, errors.New("field input is required")
	}
	return []any{map[string]any{"role": "user", "parts": parts}}, nil
}

func convertInteractionInputPart(part map[string]any) (map[string]any, error) {
	typ, _ := part["type"].(string)
	switch typ {
	case "text":
		if text, ok := part["text"].(string); ok && text != "" {
			converted := map[string]any{"text": text}
			if annotations, ok := part["annotations"].([]any); ok {
				for _, annotation := range annotations {
					metadata, ok := annotation.(map[string]any)
					if !ok || !strings.EqualFold(fmt.Sprint(metadata["type"]), "speech_metadata") {
						continue
					}
					speechMetadata := map[string]any{}
					for key, value := range metadata {
						if key != "type" {
							speechMetadata[key] = value
						}
					}
					if len(speechMetadata) > 0 {
						converted["speechMetadata"] = speechMetadata
					}
				}
			}
			return converted, nil
		}
	case "image", "video", "audio", "document":
		media, err := convertInteractionMediaPart(part)
		if err != nil {
			return nil, err
		}
		return media, nil
	}
	return nil, nil
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

func convertInteractionGenerationConfig(value any, modelName string) map[string]any {
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
			switch key {
			case "thinking_config":
				result[mapped] = convertThinkingConfig(item)
			case "image_config":
				result[mapped] = convertImageConfig(item)
			case "speech_config":
				result[mapped] = convertInteractionSpeechConfig(item, modelName)
			default:
				result[mapped] = item
			}
		} else if strings.Contains(key, "_") {
			continue
		} else if key == "imageConfig" {
			result[key] = convertImageConfig(item)
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

func convertInteractionSpeechConfig(value any, modelName string) map[string]any {
	input, ok := value.(map[string]any)
	if !ok {
		if speakers, ok := value.([]any); ok {
			input = map[string]any{"speakers": speakers}
		} else {
			return nil
		}
	}
	speakers, _ := input["speakers"].([]any)
	voiceConfig := func(voice string) map[string]any {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelName)), "gemini-3.1-flash-tts") {
			return map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": voice}}
		}
		return map[string]any{"voice": voice}
	}
	if len(speakers) > 0 {
		voiceConfigs := make([]any, 0, len(speakers))
		isMultiSpeaker := false
		for _, item := range speakers {
			speaker, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := speaker["speaker"].(string)
			voice, _ := speaker["voice"].(string)
			if voice == "" {
				continue
			}
			if name == "" {
				continue
			}
			isMultiSpeaker = true
			voiceConfigs = append(voiceConfigs, map[string]any{"speaker": name, "voiceConfig": voiceConfig(voice)})
		}
		if isMultiSpeaker && len(voiceConfigs) > 0 {
			return map[string]any{"multiSpeakerVoiceConfig": map[string]any{"speakerVoiceConfigs": voiceConfigs}}
		}
		for _, item := range speakers {
			first, ok := item.(map[string]any)
			if !ok {
				continue
			}
			voice, _ := first["voice"].(string)
			if voice != "" {
				return map[string]any{"voiceConfig": voiceConfig(voice)}
			}
		}
		return nil
	}
	if voice, _ := input["voice"].(string); voice != "" {
		return map[string]any{"voiceConfig": voiceConfig(voice)}
	}
	return nil
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
		case "thinking_budget":
			result["thinkingBudget"] = item
		default:
			result[key] = item
		}
	}
	return result
}

// convertImageConfig normalizes the snake_case image size keys clients send
// on the Interactions surface into the camelCase keys GenerateContent expects.
func convertImageConfig(value any) any {
	input, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := map[string]any{}
	for key, item := range input {
		switch key {
		case "aspect_ratio":
			result["aspectRatio"] = item
		case "image_size":
			result["imageSize"] = item
		case "person_generation":
			result["personGeneration"] = item
		default:
			result[key] = item
		}
	}
	return result
}

func applyInteractionResponseFormat(config map[string]any, value any, modelName string) {
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
	if formatType == "audio" {
		config["responseModalities"] = []string{"AUDIO"}
		return
	}
	if formatType == "image" {
		// Vertex GenerateContent requires text and image together for Gemini
		// image generation; IMAGE-only requests are rejected as INVALID_ARGUMENT.
		config["responseModalities"] = []string{"TEXT", "IMAGE"}
		imageConfig := map[string]any{}
		for from, to := range map[string]string{"aspect_ratio": "aspectRatio", "person_generation": "personGeneration"} {
			if item, ok := format[from]; ok && item != nil {
				imageConfig[to] = item
			}
		}
		if imageSize, ok := format["image_size"].(string); ok && isGenerateContentImageSizeSupported(modelName, imageSize) {
			imageConfig["imageSize"] = strings.ToUpper(strings.TrimSpace(imageSize))
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

func isGenerateContentImageSizeSupported(modelName, imageSize string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	imageSize = strings.ToUpper(strings.TrimSpace(imageSize))
	if imageSize != "512" && imageSize != "1K" && imageSize != "2K" && imageSize != "4K" {
		return false
	}
	if strings.Contains(modelName, "2.5-flash-image") || strings.Contains(modelName, "2.0-flash-preview-image-generation") {
		return false
	}
	if strings.Contains(modelName, "3.1-flash-lite-image") {
		return imageSize == "1K"
	}
	if imageSize == "512" {
		return strings.Contains(modelName, "3.1-flash-image")
	}
	return true
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

// requestImageEchoParams extracts the image size parameters from the original
// Interactions request so the converted response can echo them, matching the
// behavior of the legacy /v1/images/generations surface.
func requestImageEchoParams(c *gin.Context) map[string]any {
	if c == nil {
		return nil
	}
	value, ok := c.Get(common.KeyLyriaRawRequestBody)
	if !ok {
		return nil
	}
	raw, _ := value.([]byte)
	var request map[string]any
	if common.Unmarshal(raw, &request) != nil {
		return nil
	}
	params := map[string]any{}
	config, _ := request["generation_config"].(map[string]any)
	if config == nil {
		config, _ = request["generationConfig"].(map[string]any)
	}
	imageConfig, _ := config["image_config"].(map[string]any)
	if imageConfig == nil {
		imageConfig, _ = config["imageConfig"].(map[string]any)
	}
	copyImageEchoField := func(source map[string]any, from, to string) {
		if source == nil {
			return
		}
		if text, ok := source[from].(string); ok && strings.TrimSpace(text) != "" {
			params[to] = text
		}
	}
	copyImageEchoField(imageConfig, "aspect_ratio", "aspect_ratio")
	copyImageEchoField(imageConfig, "aspectRatio", "aspect_ratio")
	copyImageEchoField(imageConfig, "image_size", "image_size")
	copyImageEchoField(imageConfig, "imageSize", "image_size")
	if format, ok := request["response_format"].(map[string]any); ok {
		if formatType, _ := format["type"].(string); formatType == "image" {
			copyImageEchoField(format, "aspect_ratio", "aspect_ratio")
			copyImageEchoField(format, "image_size", "image_size")
		}
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

// decodedImagePixelSize reads only the image header from a base64 payload and
// returns an OpenAI-style "WIDTHxHEIGHT" size string, or "" if undecodable.
func decodedImagePixelSize(b64 string) string {
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", config.Width, config.Height)
}

func convertGenerateContentResponse(raw []byte, modelName string, imageEcho map[string]any) ([]byte, error) {
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
		Usage json.RawMessage `json:"usageMetadata"`
	}
	if err := common.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	var usageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		PromptTokensDetails     []struct {
			Modality   string `json:"modality"`
			TokenCount int    `json:"tokenCount"`
		} `json:"promptTokensDetails"`
		CandidatesTokensDetails []struct {
			Modality   string `json:"modality"`
			TokenCount int    `json:"tokenCount"`
		} `json:"candidatesTokensDetails"`
	}
	usageAvailable := false
	var usageFields map[string]json.RawMessage
	if len(response.Usage) > 0 && string(response.Usage) != "null" && common.Unmarshal(response.Usage, &usageMetadata) == nil {
		if common.Unmarshal(response.Usage, &usageFields) == nil {
			for _, name := range []string{"promptTokenCount", "candidatesTokenCount", "totalTokenCount", "cachedContentTokenCount", "thoughtsTokenCount", "promptTokensDetails", "candidatesTokensDetails"} {
				if value, exists := usageFields[name]; exists && string(value) != "null" {
					usageAvailable = true
					break
				}
			}
		}
	}
	outputs := []any{}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				outputs = append(outputs, map[string]any{"type": "text", "text": part.Text})
			}
			if part.InlineData != nil && part.InlineData.Data != "" {
				if strings.HasPrefix(strings.ToLower(part.InlineData.MimeType), "audio/") {
					outputs = append(outputs, map[string]any{
						"type":      "audio",
						"data":      part.InlineData.Data,
						"mime_type": part.InlineData.MimeType,
					})
					continue
				}
				block := map[string]any{
					"type":      "image",
					"data":      part.InlineData.Data,
					"mime_type": part.InlineData.MimeType,
				}
				// Echo the size parameters like the legacy image surface did:
				// requested aspect_ratio / image_size plus the actual decoded
				// pixel size of the returned image.
				for key, value := range imageEcho {
					block[key] = value
				}
				if size := decodedImagePixelSize(part.InlineData.Data); size != "" {
					block["size"] = size
				}
				outputs = append(outputs, block)
			}
		}
	}
	usage := map[string]any{}
	if usageAvailable {
		if value, exists := usageFields["promptTokenCount"]; exists && string(value) != "null" {
			usage["total_input_tokens"] = usageMetadata.PromptTokenCount
		}
		if value, exists := usageFields["candidatesTokenCount"]; exists && string(value) != "null" {
			usage["total_output_tokens"] = usageMetadata.CandidatesTokenCount
		}
		if value, exists := usageFields["totalTokenCount"]; exists && string(value) != "null" {
			usage["total_tokens"] = usageMetadata.TotalTokenCount
		}
	}
	if usageAvailable && usageMetadata.CachedContentTokenCount > 0 {
		usage["cached_tokens"] = usageMetadata.CachedContentTokenCount
	}
	if usageAvailable && usageMetadata.ThoughtsTokenCount > 0 {
		usage["thought_tokens"] = usageMetadata.ThoughtsTokenCount
	}
	for _, detail := range usageMetadata.PromptTokensDetails {
		if detail.TokenCount <= 0 {
			continue
		}
		switch strings.ToUpper(detail.Modality) {
		case "TEXT":
			usage["text_input_tokens"] = detail.TokenCount
		case "IMAGE":
			usage["image_input_tokens"] = detail.TokenCount
		case "VIDEO":
			usage["video_input_tokens"] = detail.TokenCount
		case "AUDIO":
			usage["audio_input_tokens"] = detail.TokenCount
		case "DOCUMENT":
			usage["document_input_tokens"] = detail.TokenCount
		}
	}
	for _, detail := range usageMetadata.CandidatesTokensDetails {
		if detail.TokenCount <= 0 {
			continue
		}
		switch strings.ToUpper(detail.Modality) {
		case "TEXT":
			usage["text_output_tokens"] = detail.TokenCount
		case "IMAGE":
			usage["image_output_tokens"] = detail.TokenCount
		case "VIDEO":
			usage["video_output_tokens"] = detail.TokenCount
		case "AUDIO":
			usage["audio_output_tokens"] = detail.TokenCount
		}
	}
	if usageAvailable {
		if _, hasTotalInput := usage["total_input_tokens"]; !hasTotalInput {
			var totalInput int
			for _, detail := range usageMetadata.PromptTokensDetails {
				if detail.TokenCount > 0 {
					totalInput += detail.TokenCount
				}
			}
			if totalInput > 0 {
				usage["total_input_tokens"] = totalInput
			}
		}
		if _, hasTotalOutput := usage["total_output_tokens"]; !hasTotalOutput {
			var totalOutput int
			for _, detail := range usageMetadata.CandidatesTokensDetails {
				if detail.TokenCount > 0 {
					totalOutput += detail.TokenCount
				}
			}
			if totalOutput > 0 {
				usage["total_output_tokens"] = totalOutput
			}
		}
		if total, hasTotal := usage["total_tokens"]; !hasTotal || total == 0 {
			input, _ := usage["total_input_tokens"].(int)
			output, _ := usage["total_output_tokens"].(int)
			thought := usageMetadata.ThoughtsTokenCount
			if input+output+thought > 0 {
				usage["total_tokens"] = input + output + thought
			}
		}
	}
	result := map[string]any{
		"id":      model.GenerateInteractionID(),
		"object":  "interaction",
		"model":   modelName,
		"status":  "completed",
		"outputs": outputs,
	}
	if usageAvailable {
		result["usage"] = usage
	}
	return common.Marshal(result)
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, body)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *taskdto.TaskError) {
	if resp == nil || resp.Body == nil {
		return "", nil, service.TaskErrorWrapper(errors.New("upstream response is empty"), "invalid_response", http.StatusBadGateway)
	}
	var body []byte
	var err error
	if c.GetBool("native_interactions_stream") && info.NativeInteractions &&
		shouldUseVertexInteractions(info, a.baseURL, a.apiKey) && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		body, err = readVertexInteractionStream(c, resp.Body)
	} else {
		body, err = io.ReadAll(resp.Body)
	}
	_ = resp.Body.Close()
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	var interaction struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
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
		body, err = convertGenerateContentResponse(body, info.OriginModelName, requestImageEchoParams(c))
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

func readVertexInteractionStream(c *gin.Context, body io.Reader) ([]byte, error) {
	var emit func([]byte)
	if value, ok := c.Get("native_interactions_sse_writer"); ok {
		emit, _ = value.(func([]byte))
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var event bytes.Buffer
	var latest []byte
	var idlessTerminal []byte
	var upstreamErr error
	var lyriaAudio []byte
	lyriaAudioMimeType := ""
	flushEvent := func() {
		if event.Len() == 0 {
			return
		}
		rawEvent := append([]byte(nil), event.Bytes()...)
		if emit != nil {
			emit(rawEvent)
		}
		var dataLines []string
		for _, line := range strings.Split(string(rawEvent), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if strings.HasPrefix(line, "event:") && strings.TrimSpace(strings.TrimPrefix(line, "event:")) == "error" {
				upstreamErr = errors.New("Vertex Interactions stream returned an error")
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if len(dataLines) > 0 {
			var payload map[string]any
			if common.Unmarshal([]byte(strings.Join(dataLines, "\n")), &payload) == nil {
				eventType, _ := payload["event_type"].(string)
				if IsLyriaModel(c.GetString("native_interactions_model")) &&
					(strings.EqualFold(eventType, "step.delta") || strings.EqualFold(eventType, "content.delta")) {
					if delta, ok := payload["delta"].(map[string]any); ok && strings.EqualFold(fmt.Sprint(delta["type"]), "audio") {
						if data, _ := delta["data"].(string); data != "" {
							decoded, decodeErr := base64.StdEncoding.DecodeString(data)
							if decodeErr != nil {
								upstreamErr = fmt.Errorf("Vertex Lyria audio stream contained invalid base64 data: %w", decodeErr)
							} else {
								lyriaAudio = append(lyriaAudio, decoded...)
								if mimeType, _ := delta["mime_type"].(string); mimeType != "" {
									lyriaAudioMimeType = mimeType
								}
							}
						}
					}
				}
				if strings.EqualFold(eventType, "error") || upstreamErr != nil {
					message := ""
					code := ""
					if providerError, ok := payload["error"].(map[string]any); ok {
						message, _ = providerError["message"].(string)
						code, _ = providerError["code"].(string)
					}
					if message == "" {
						message, _ = payload["message"].(string)
					}
					if message != "" {
						if code != "" {
							upstreamErr = fmt.Errorf("Vertex Interactions stream error (%s): %s", code, message)
						} else {
							upstreamErr = fmt.Errorf("Vertex Interactions stream error: %s", message)
						}
					}
				}
				interaction := payload
				if nested, ok := payload["interaction"].(map[string]any); ok {
					interaction = nested
				}
				if id, _ := interaction["id"].(string); strings.TrimSpace(id) != "" {
					if encoded, err := common.Marshal(interaction); err == nil {
						latest = encoded
					}
				} else if IsLyriaModel(c.GetString("native_interactions_model")) {
					status, _ := interaction["status"].(string)
					terminal := strings.EqualFold(status, "completed") || strings.EqualFold(status, "failed") ||
						strings.EqualFold(status, "cancelled") || strings.EqualFold(status, "canceled") ||
						strings.EqualFold(status, "incomplete") || strings.EqualFold(status, "budget_exceeded")
					if eventType == "interaction.completed" || eventType == "interaction.failed" {
						terminal = true
					}
					for _, line := range strings.Split(string(rawEvent), "\n") {
						line = strings.TrimSuffix(line, "\r")
						if strings.HasPrefix(line, "event:") {
							name := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
							if name == "interaction.completed" || name == "interaction.failed" {
								terminal = true
							}
						}
					}
					if terminal {
						if encoded, err := common.Marshal(interaction); err == nil {
							idlessTerminal = encoded
						}
					}
				}
			}
		}
		event.Reset()
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			flushEvent()
			continue
		}
		if event.Len() > 0 {
			event.WriteByte('\n')
		}
		_, _ = event.WriteString(line)
	}
	flushEvent()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if upstreamErr != nil {
		return nil, upstreamErr
	}
	if len(idlessTerminal) > 0 {
		latest = idlessTerminal
	}
	if len(latest) == 0 {
		return nil, errors.New("Vertex Interactions stream did not contain an interaction")
	}
	if len(lyriaAudio) > 0 {
		parsed, err := parseInteractionResult(latest, c.GetString("native_interactions_model"))
		if err == nil && parsed.Url == "" {
			var interaction map[string]any
			if err := common.Unmarshal(latest, &interaction); err != nil {
				return nil, err
			}
			outputs, _ := interaction["outputs"].([]any)
			if lyriaAudioMimeType == "" {
				lyriaAudioMimeType = "audio/mpeg"
			}
			outputs = append(outputs, map[string]any{
				"type": "audio", "mime_type": lyriaAudioMimeType,
				"data": base64.StdEncoding.EncodeToString(lyriaAudio),
			})
			interaction["outputs"] = outputs
			latest, err = common.Marshal(interaction)
			if err != nil {
				return nil, err
			}
		}
	}
	return latest, nil
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
		if !isServiceAccountJSON(key) {
			req.Header.Set("x-goog-api-key", key)
		} else {
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
		}
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

func (a *TaskAdaptor) ParseTaskResultWithModel(body []byte, modelName string) (*relaycommon.TaskInfo, error) {
	return parseInteractionResult(body, modelName)
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

type lyriaInteractionStep struct {
	Type    string             `json:"type"`
	Content []lyriaOutputBlock `json:"content"`
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

func parseInteractionResult(body []byte, modelNames ...string) (*relaycommon.TaskInfo, error) {
	type interactionUsage struct {
		TotalInputTokens   int `json:"total_input_tokens"`
		TotalOutputTokens  int `json:"total_output_tokens"`
		TotalTokens        int `json:"total_tokens"`
		CachedTokens       int `json:"cached_tokens"`
		ThoughtTokens      int `json:"thought_tokens"`
		TotalThoughtTokens int `json:"total_thought_tokens"`
		TextInputTokens    int `json:"text_input_tokens"`
		ImageInputTokens   int `json:"image_input_tokens"`
		VideoInputTokens   int `json:"video_input_tokens"`
		AudioInputTokens   int `json:"audio_input_tokens"`
		TextOutputTokens   int `json:"text_output_tokens"`
		ImageOutputTokens  int `json:"image_output_tokens"`
		VideoOutputTokens  int `json:"video_output_tokens"`
		AudioOutputTokens  int `json:"audio_output_tokens"`
		InputByModality    []struct {
			Modality string `json:"modality"`
			Tokens   int    `json:"tokens"`
		} `json:"input_tokens_by_modality"`
		OutputByModality []struct {
			Modality string `json:"modality"`
			Tokens   int    `json:"tokens"`
		} `json:"output_tokens_by_modality"`
	}
	var interaction struct {
		ID          string                  `json:"id"`
		Model       string                  `json:"model"`
		Status      string                  `json:"status"`
		Steps       []lyriaInteractionStep  `json:"steps"`
		Outputs     []lyriaOutputBlock      `json:"outputs"`
		OutputAudio *lyriaOutputBlock       `json:"output_audio"`
		OutputText  string                  `json:"output_text"`
		Usage       json.RawMessage         `json:"usage"`
		Error       lyriaInteractionError   `json:"error"`
		Errors      []lyriaInteractionError `json:"errors"`
	}
	if err := common.Unmarshal(body, &interaction); err != nil {
		return nil, err
	}
	var usage interactionUsage
	var usageFields map[string]json.RawMessage
	usageAvailable := false
	hasInputTokenUsage := false
	if len(interaction.Usage) > 0 && string(interaction.Usage) != "null" {
		if err := common.Unmarshal(interaction.Usage, &usage); err == nil {
			if err := common.Unmarshal(interaction.Usage, &usageFields); err == nil {
				// A completed interaction with input usage only is not a complete
				// billing record. In particular, Vertex can expose an early/partial
				// usage snapshot while the generated output is still unavailable.
				// Do not settle that snapshot as if output usage were zero; explicit
				// zero output remains valid because key presence is preserved here.
				hasInputUsage := false
				for _, name := range []string{
					"total_input_tokens", "cached_tokens", "text_input_tokens", "image_input_tokens",
					"video_input_tokens", "audio_input_tokens", "input_tokens_by_modality",
				} {
					if value, exists := usageFields[name]; exists && string(value) != "null" {
						hasInputUsage = true
						break
					}
				}
				for _, name := range []string{
					"total_input_tokens", "text_input_tokens", "image_input_tokens",
					"video_input_tokens", "audio_input_tokens", "input_tokens_by_modality",
				} {
					if value, exists := usageFields[name]; exists && string(value) != "null" {
						hasInputTokenUsage = true
						break
					}
				}
				outputModality, hasOnlyOutputModality := nativeInteractionOutputModality(interaction.OutputText, interaction.OutputAudio, interaction.Outputs, interaction.Steps)
				_, hasTotalTokens := usageFields["total_tokens"]
				canDeriveAggregateOutput := hasOnlyOutputModality && hasInputTokenUsage && hasTotalTokens &&
					(outputModality == "text" || outputModality == "image" || outputModality == "audio" || outputModality == "video")
				hasOutputUsage := false
				for _, name := range []string{
					"total_output_tokens", "thought_tokens", "total_thought_tokens",
					"text_output_tokens", "image_output_tokens", "video_output_tokens", "audio_output_tokens",
					"output_tokens_by_modality",
				} {
					if value, exists := usageFields[name]; exists && string(value) != "null" {
						hasOutputUsage = true
						break
					}
				}
				if !hasOutputUsage && canDeriveAggregateOutput {
					hasOutputUsage = true
				}
				usageAvailable = hasInputUsage && hasOutputUsage
			}
		}
	}
	var modalityTextInput, modalityImageInput, modalityVideoInput, modalityAudioInput int
	var modalityTextOutput, modalityImageOutput, modalityVideoOutput, modalityAudioOutput, modalityThoughtOutput int
	for _, item := range usage.InputByModality {
		switch strings.ToLower(strings.TrimSpace(item.Modality)) {
		case "text":
			modalityTextInput += item.Tokens
		case "image":
			modalityImageInput += item.Tokens
		case "video":
			modalityVideoInput += item.Tokens
		case "audio":
			modalityAudioInput += item.Tokens
		}
	}
	for _, item := range usage.OutputByModality {
		switch strings.ToLower(strings.TrimSpace(item.Modality)) {
		case "text":
			modalityTextOutput += item.Tokens
		case "image":
			modalityImageOutput += item.Tokens
		case "video":
			modalityVideoOutput += item.Tokens
		case "audio":
			modalityAudioOutput += item.Tokens
		case "thought":
			modalityThoughtOutput += item.Tokens
		}
	}
	modalityInputTotal := modalityTextInput + modalityImageInput + modalityVideoInput + modalityAudioInput
	if _, exists := usageFields["total_input_tokens"]; !exists || (usage.TotalInputTokens == 0 && modalityInputTotal > 0) {
		usage.TotalInputTokens = modalityTextInput + modalityImageInput + modalityVideoInput + modalityAudioInput
	}
	modalityOutputTotal := modalityTextOutput + modalityImageOutput + modalityVideoOutput + modalityAudioOutput
	hasScalarOutput := usage.TextOutputTokens > 0 || usage.ImageOutputTokens > 0 || usage.VideoOutputTokens > 0 || usage.AudioOutputTokens > 0
	outputModality, hasOnlyOutputModality := nativeInteractionOutputModality(interaction.OutputText, interaction.OutputAudio, interaction.Outputs, interaction.Steps)
	_, hasTotalAggregate := usageFields["total_tokens"]
	canDeriveAggregateOutput := hasInputTokenUsage && hasTotalAggregate && hasOnlyOutputModality
	if _, exists := usageFields["total_output_tokens"]; !exists || (usage.TotalOutputTokens == 0 && (modalityOutputTotal > 0 || (canDeriveAggregateOutput && len(usage.OutputByModality) == 0 && !hasScalarOutput))) {
		// Thought tokens are reported separately from generated candidate tokens
		// in the gateway billing model, so do not fold them into completion.
		usage.TotalOutputTokens = modalityOutputTotal
		if canDeriveAggregateOutput && len(usage.OutputByModality) == 0 && !hasScalarOutput {
			thoughtTokens := usage.ThoughtTokens
			if thoughtTokens == 0 {
				thoughtTokens = usage.TotalThoughtTokens
			}
			inputTokensForDerivation := usage.TotalInputTokens
			if inputTokensForDerivation == 0 {
				inputTokensForDerivation = usage.TextInputTokens + usage.ImageInputTokens + usage.VideoInputTokens + usage.AudioInputTokens
			}
			if inputTokensForDerivation == 0 {
				inputTokensForDerivation = modalityInputTotal
			}
			derived := usage.TotalTokens - inputTokensForDerivation - thoughtTokens
			if derived >= 0 {
				usage.TotalOutputTokens = derived
				switch outputModality {
				case "image":
					usage.ImageOutputTokens = derived
				case "audio":
					usage.AudioOutputTokens = derived
				case "video":
					usage.VideoOutputTokens = derived
				default:
					usage.TextOutputTokens = derived
				}
			} else {
				usageAvailable = false
			}
		}
	}
	if _, exists := usageFields["text_input_tokens"]; !exists {
		usage.TextInputTokens = modalityTextInput
	}
	if _, exists := usageFields["image_input_tokens"]; !exists {
		usage.ImageInputTokens = modalityImageInput
	}
	if _, exists := usageFields["video_input_tokens"]; !exists {
		usage.VideoInputTokens = modalityVideoInput
	}
	if _, exists := usageFields["audio_input_tokens"]; !exists {
		usage.AudioInputTokens = modalityAudioInput
	}
	if _, exists := usageFields["text_output_tokens"]; !exists && len(usage.OutputByModality) > 0 {
		usage.TextOutputTokens = modalityTextOutput
	}
	if _, exists := usageFields["image_output_tokens"]; !exists && len(usage.OutputByModality) > 0 {
		usage.ImageOutputTokens = modalityImageOutput
	}
	if _, exists := usageFields["video_output_tokens"]; !exists {
		usage.VideoOutputTokens = modalityVideoOutput
	}
	if _, exists := usageFields["audio_output_tokens"]; !exists {
		usage.AudioOutputTokens = modalityAudioOutput
	}
	if _, exists := usageFields["thought_tokens"]; !exists {
		if _, exists := usageFields["total_thought_tokens"]; exists {
			usage.ThoughtTokens = usage.TotalThoughtTokens
		} else {
			usage.ThoughtTokens = modalityThoughtOutput
		}
	}
	modalityInputTotal = usage.TextInputTokens + usage.ImageInputTokens + usage.VideoInputTokens + usage.AudioInputTokens
	if _, exists := usageFields["total_input_tokens"]; !exists || (usage.TotalInputTokens == 0 && modalityInputTotal > 0) {
		usage.TotalInputTokens = usage.TextInputTokens + usage.ImageInputTokens + usage.VideoInputTokens + usage.AudioInputTokens
	}
	modalityOutputTotal = usage.TextOutputTokens + usage.ImageOutputTokens + usage.VideoOutputTokens + usage.AudioOutputTokens
	if _, exists := usageFields["total_output_tokens"]; !exists || (usage.TotalOutputTokens == 0 && modalityOutputTotal > 0) {
		usage.TotalOutputTokens = usage.TextOutputTokens + usage.ImageOutputTokens + usage.VideoOutputTokens + usage.AudioOutputTokens
	}
	result := &relaycommon.TaskInfo{TaskID: interaction.ID, Metadata: map[string]any{}}
	result.UsageFacts = map[string]any{}
	result.UsageAvailable = usageAvailable
	result.InputTokens = usage.TotalInputTokens
	result.CompletionTokens = usage.TotalOutputTokens
	result.TotalTokens = usage.TotalTokens
	if result.TotalTokens == 0 || (result.TotalTokens < result.InputTokens+result.CompletionTokens+usage.ThoughtTokens) {
		result.TotalTokens = result.InputTokens + result.CompletionTokens
		if usage.ThoughtTokens > 0 {
			result.TotalTokens += usage.ThoughtTokens
		}
	}
	if usageAvailable {
		result.UsageFacts["total_input_tokens"] = result.InputTokens
		result.UsageFacts["total_output_tokens"] = result.CompletionTokens
		result.UsageFacts["total_tokens"] = result.TotalTokens
	}
	result.CachedTokens = usage.CachedTokens
	if result.CachedTokens > result.InputTokens {
		result.CachedTokens = result.InputTokens
	}
	result.TextInputTokens = usage.TextInputTokens
	result.ImageInputTokens = usage.ImageInputTokens
	result.VideoInputTokens = usage.VideoInputTokens
	result.AudioInputTokens = usage.AudioInputTokens
	result.TextOutputTokens = usage.TextOutputTokens
	if result.TextOutputTokens > result.CompletionTokens {
		result.TextOutputTokens = result.CompletionTokens
	}
	result.ImageOutputTokens = usage.ImageOutputTokens
	result.VideoOutputTokens = usage.VideoOutputTokens
	result.AudioOutputTokens = usage.AudioOutputTokens
	if hasOnlyOutputModality && len(usage.OutputByModality) == 0 && !hasScalarOutput && result.CompletionTokens > 0 {
		// Keep aggregate-only output visible in billing logs, matching Gemini
		// GenerateContent's response-driven modality split across model families.
		switch outputModality {
		case "text":
			result.TextOutputTokens = result.CompletionTokens
		case "image":
			result.ImageOutputTokens = result.CompletionTokens
		case "audio":
			result.AudioOutputTokens = result.CompletionTokens
		case "video":
			result.VideoOutputTokens = result.CompletionTokens
		}
	}
	result.ThoughtTokens = usage.ThoughtTokens
	if result.ThoughtTokens == 0 {
		// Gemini reports thinking tokens only inside total_tokens; derive them
		// when the explicit count is absent so billing and the consume log
		// still see them.
		if derived := result.TotalTokens - result.InputTokens - result.CompletionTokens; derived > 0 {
			result.ThoughtTokens = derived
		}
	}
	if usageAvailable && nativeInteractionHasImageOutput(interaction.Outputs, interaction.Steps) && result.ImageOutputTokens == 0 {
		usageAvailable = false
		result.UsageAvailable = false
		result.UsageFacts = map[string]any{}
	}
	if usageAvailable && nativeInteractionHasAudioOutput(interaction.OutputAudio, interaction.Outputs, interaction.Steps) && result.AudioOutputTokens == 0 {
		usageAvailable = false
		result.UsageAvailable = false
		result.UsageFacts = map[string]any{}
	}
	if result.ThoughtTokens > 0 {
		result.UsageFacts["thought_tokens"] = result.ThoughtTokens
	}
	if usageAvailable {
		result.UsageFacts["cached_tokens"] = result.CachedTokens
		result.UsageFacts["text_input_tokens"] = result.TextInputTokens
		result.UsageFacts["image_input_tokens"] = result.ImageInputTokens
		result.UsageFacts["video_input_tokens"] = result.VideoInputTokens
		result.UsageFacts["audio_input_tokens"] = result.AudioInputTokens
		result.UsageFacts["text_output_tokens"] = result.TextOutputTokens
		result.UsageFacts["image_output_tokens"] = result.ImageOutputTokens
		result.UsageFacts["video_output_tokens"] = result.VideoOutputTokens
		result.UsageFacts["audio_output_tokens"] = result.AudioOutputTokens
		result.UsageFacts["thought_tokens"] = result.ThoughtTokens
	}
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
