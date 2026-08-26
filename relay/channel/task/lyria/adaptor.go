package lyria

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	info.Action = "song"
	return nil
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
	return isVertexLyriaInteraction(info) || isVertexInteractionsEndpoint(baseURL) || isServiceAccountJSON(key)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
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
		Outputs     []lyriaOutputBlock      `json:"outputs"`
		OutputAudio *lyriaOutputBlock       `json:"output_audio"`
		OutputText  string                  `json:"output_text"`
		Error       lyriaInteractionError   `json:"error"`
		Errors      []lyriaInteractionError `json:"errors"`
	}
	if err := common.Unmarshal(body, &interaction); err != nil {
		return nil, err
	}
	result := &relaycommon.TaskInfo{TaskID: interaction.ID, Metadata: map[string]any{}}
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
		if result.Url == "" {
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
