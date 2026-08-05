package gemini

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// ============================
// Request / Response structures
// ============================

// GeminiVideoGenerationConfig represents the video generation configuration
// Based on: https://ai.google.dev/gemini-api/docs/video
type GeminiVideoGenerationConfig struct {
	AspectRatio      string  `json:"aspectRatio,omitempty"`      // "16:9" or "9:16"
	DurationSeconds  float64 `json:"durationSeconds,omitempty"`  // 4, 6, or 8 (as number)
	NegativePrompt   string  `json:"negativePrompt,omitempty"`   // unwanted elements
	PersonGeneration string  `json:"personGeneration,omitempty"` // "allow_all" for text-to-video, "allow_adult" for image-to-video
	Resolution       string  `json:"resolution,omitempty"`       // video resolution
	IncludeAudio     *bool   `json:"includeAudio,omitempty"`     // Google Gemini API audio flag (assuming 'includeAudio')
}

// GeminiVideoRequest represents a single video generation instance
type GeminiVideoRequest struct {
	Prompt    string         `json:"prompt"`
	Image     *VeoImageInput `json:"image,omitempty"`
	LastFrame *VeoImageInput `json:"lastFrame,omitempty"`
}

// GeminiVideoPayload represents the complete video generation request payload
type GeminiVideoPayload struct {
	Instances  []GeminiVideoRequest        `json:"instances"`
	Parameters GeminiVideoGenerationConfig `json:"parameters,omitempty"`
}

type submitResponse struct {
	Name string `json:"name"`
}

type operationVideo struct {
	MimeType           string `json:"mimeType"`
	BytesBase64Encoded string `json:"bytesBase64Encoded"`
	Encoding           string `json:"encoding"`
}

type operationResponse struct {
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Response struct {
		Type                    string           `json:"@type"`
		RaiMediaFilteredCount   int              `json:"raiMediaFilteredCount"`
		RaiMediaFilteredReasons []string         `json:"raiMediaFilteredReasons"`
		Videos                  []operationVideo `json:"videos"`
		BytesBase64Encoded      string           `json:"bytesBase64Encoded"`
		Encoding                string           `json:"encoding"`
		Video                   string           `json:"video"`
		GenerateVideoResponse   struct {
			GeneratedSamples []struct {
				Video struct {
					URI string `json:"uri"`
				} `json:"video"`
			} `json:"generatedSamples"`
		} `json:"generateVideoResponse"`
	} `json:"response"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
// Extracts Veo params (durationSeconds, aspectRatio, resolution, etc.) from raw body into Metadata
// so BuildRequestBody can send them upstream; also sets video_seconds for billing.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	taskErr = relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate)
	if taskErr != nil {
		return taskErr
	}
	// Extract Veo params from raw body (not in TaskSubmitReq fields) and inject into task_request.Metadata
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil
	}
	rawBody, err := storage.Bytes()
	if err != nil || len(rawBody) == 0 {
		return nil
	}
	var requestMap map[string]interface{}
	if err := common.Unmarshal(rawBody, &requestMap); err != nil {
		return nil
	}
	metaData := extractVeoParamsFromRequest(requestMap)
	if len(metaData) == 0 {
		return nil
	}
	if durationSec, ok := metaData["durationSeconds"]; ok {
		if n, ok := toInt(durationSec); ok && n > 0 {
			c.Set("video_seconds", n)
		}
	}
	// 保存分辨率到 gin context，供 mergeVideoTaskBillingData 写入 task.Data（计费用）
	if res, ok := metaData["resolution"]; ok {
		if s, ok := res.(string); ok && s != "" {
			c.Set("video_resolution", s)
		}
	}
	v, ok := c.Get("task_request")
	if !ok {
		return nil
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil
	}
	if req.Metadata == nil {
		req.Metadata = metaData
	} else {
		for k, v := range metaData {
			req.Metadata[k] = v
		}
	}
	c.Set("task_request", req)
	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if isOmniModel(info.OriginModelName) {
		return buildOmniRequestURL(a.baseURL), nil
	}
	modelName := info.OriginModelName
	version := model_setting.GetGeminiVersionSetting(modelName)

	return fmt.Sprintf(
		"%s/%s/models/%s:predictLongRunning",
		a.baseURL,
		version,
		modelName,
	), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.GetBool("native_interactions_stream") && isOmniModel(info.OriginModelName) {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("x-goog-api-key", a.apiKey)
	return nil
}

// BuildRequestBody converts request into Gemini specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, ok := c.Get("task_request")
	if !ok {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil, fmt.Errorf("unexpected task_request type")
	}
	if isOmniModel(info.OriginModelName) {
		data, err := buildOmniRequestBody(req)
		if err != nil {
			return nil, err
		}
		c.Set("video_seconds", sanitizeDurationSecondsFromMetadata(req.Metadata))
		return bytes.NewReader(data), nil
	}

	body := GeminiVideoPayload{
		Instances: []GeminiVideoRequest{
			{Prompt: req.Prompt},
		},
		Parameters: GeminiVideoGenerationConfig{},
	}

	metadata := req.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	// 首帧图片：URL / data URI / raw base64 → base64
	if imageVal, ok := metadata["image"]; ok {
		if imageStr, ok := imageVal.(string); ok {
			imgInput, err := ParseImageInput(imageStr)
			if err != nil {
				return nil, fmt.Errorf("image conversion failed: %w", err)
			}
			if imgInput != nil {
				body.Instances[0].Image = imgInput
			}
		}
	}
	// 尾帧图片：URL / data URI / raw base64 → base64
	if lastFrameVal, ok := metadata["lastFrame"]; ok {
		if lastFrameStr, ok := lastFrameVal.(string); ok {
			lfInput, err := ParseImageInput(lastFrameStr)
			if err != nil {
				return nil, fmt.Errorf("lastFrame conversion failed: %w", err)
			}
			if lfInput != nil {
				body.Instances[0].LastFrame = lfInput
			}
		}
	}

	body.Parameters.DurationSeconds = float64(sanitizeDurationSecondsFromMetadata(metadata))
	body.Parameters.AspectRatio = sanitizeAspectRatioFromMetadata(metadata)
	body.Parameters.Resolution = sanitizeResolutionFromMetadata(metadata)
	body.Parameters.NegativePrompt = extractStringFromMetadata(metadata, "negativePrompt", "negative_prompt")
	body.Parameters.PersonGeneration = sanitizePersonGenerationFromMetadata(metadata)

	includeAudio := sanitizeGenerateAudio(metadata, req.GenerateAudio)
	body.Parameters.IncludeAudio = &includeAudio

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	logger.LogInfo(c, fmt.Sprintf("[gemini] upstream request body: %s", common.TruncateJsonValues(string(data))))
	if len(req.Metadata) > 0 {
		if metaBytes, err := common.Marshal(req.Metadata); err == nil {
			logger.LogInfo(c, fmt.Sprintf("[gemini] client metadata: %s", common.TruncateJsonValues(string(metaBytes))))
		}
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	if resp == nil {
		return "", nil, service.TaskErrorWrapper(errors.New("upstream response is nil"), "invalid_response", http.StatusBadGateway)
	}
	if resp.Body == nil {
		return "", nil, service.TaskErrorWrapper(errors.New("upstream response body is nil"), "invalid_response", http.StatusBadGateway)
	}
	var responseBody []byte
	var err error
	if isOmniModel(info.OriginModelName) && c.GetBool("native_interactions_stream") {
		responseBody, err = a.readOmniStream(c, resp, info)
	} else {
		responseBody, err = io.ReadAll(resp.Body)
	}
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()
	if isOmniModel(info.OriginModelName) {
		var interaction omniInteraction
		if err := common.Unmarshal(responseBody, &interaction); err != nil || strings.TrimSpace(interaction.ID) == "" {
			if err == nil {
				err = fmt.Errorf("missing interaction id")
			}
			return "", nil, service.TaskErrorWrapper(err, "invalid_response", http.StatusInternalServerError)
		}
		info.PublicTaskID = interaction.ID
		video := dto.NewOpenAIVideo()
		video.ID, video.TaskID, video.Model = interaction.ID, interaction.ID, info.OriginModelName
		video.CreatedAt = time.Now().Unix()
		if delay, _ := c.Get(relaycommon.TaskSubmitDelayResponse); delay == true {
			if body, err := common.Marshal(video); err == nil {
				c.Set(relaycommon.TaskSubmitResponseBody, body)
			}
			return interaction.ID, responseBody, nil
		}
		c.JSON(http.StatusOK, video)
		taskID = markOmniTaskID(interaction.ID)
		return taskID, responseBody, nil
	}

	var s submitResponse
	if err := common.Unmarshal(responseBody, &s); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
	}
	if strings.TrimSpace(s.Name) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("missing operation name"), "invalid_response", http.StatusInternalServerError)
	}
	taskID = encodeLocalTaskID(s.Name)

	// 使用上游真实 ID 作为公开 ID（避免依赖 private_data.upstream_task_id）
	info.PublicTaskID = taskID

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	if delay, _ := c.Get(relaycommon.TaskSubmitDelayResponse); delay == true {
		if body, err := common.Marshal(ov); err == nil {
			c.Set(relaycommon.TaskSubmitResponseBody, body)
		}
		return taskID, responseBody, nil
	}
	c.JSON(http.StatusOK, ov)
	return taskID, responseBody, nil
}

// readOmniStream forwards the upstream SSE blocks unchanged, records the
// interaction ID, and retrieves the completed interaction once the stream
// terminates. The retrieved JSON is returned to the normal task/billing path.
func (a *TaskAdaptor) readOmniStream(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) ([]byte, error) {
	var emit func([]byte)
	if value, ok := c.Get("native_interactions_sse_writer"); ok {
		emit, _ = value.(func([]byte))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var event bytes.Buffer
	interactionID := ""
	completed := false
	flushEvent := func() {
		if event.Len() == 0 {
			return
		}
		rawEvent := append([]byte(nil), event.Bytes()...)
		if emit != nil {
			emit(rawEvent)
		}
		for _, line := range strings.Split(string(rawEvent), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var payload map[string]any
			if common.Unmarshal([]byte(data), &payload) != nil {
				continue
			}
			if id, ok := payload["interaction_id"].(string); ok && id != "" {
				interactionID = id
			}
			if interaction, ok := payload["interaction"].(map[string]any); ok {
				if id, ok := interaction["id"].(string); ok && id != "" {
					interactionID = id
				}
				if status, ok := interaction["status"].(string); ok && isOmniTerminalStatus(status) {
					completed = true
				}
			}
			if eventType, ok := payload["event_type"].(string); ok && eventType == "interaction.completed" {
				completed = true
			}
			if id, ok := payload["id"].(string); ok && id != "" {
				interactionID = id
			}
			if status, ok := payload["status"].(string); ok && isOmniTerminalStatus(status) {
				completed = true
			}
		}
		event.Reset()
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			flushEvent()
			continue
		}
		if event.Len() > 0 {
			event.WriteByte('\n')
		}
		_, _ = event.Write(line)
	}
	flushEvent()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if interactionID == "" {
		return nil, errors.New("upstream SSE did not contain interaction id")
	}
	if !completed {
		return nil, errors.New("upstream SSE ended before interaction.completed")
	}

	finalResp, err := a.FetchTask(a.baseURL, a.apiKey, map[string]any{
		"task_id": markOmniTaskID(interactionID),
	}, info.ChannelSetting.Proxy)
	if err != nil {
		return nil, fmt.Errorf("fetch completed interaction: %w", err)
	}
	if finalResp == nil || finalResp.Body == nil {
		return nil, errors.New("completed interaction response body is nil")
	}
	defer finalResp.Body.Close()
	if finalResp.StatusCode < 200 || finalResp.StatusCode >= 300 {
		body, _ := io.ReadAll(finalResp.Body)
		return nil, fmt.Errorf("fetch completed interaction returned HTTP %d: %s", finalResp.StatusCode, string(body))
	}
	return io.ReadAll(finalResp.Body)
}

func isOmniTerminalStatus(status string) bool {
	switch strings.ToLower(status) {
	case "completed", "failed", "cancelled", "incomplete":
		return true
	default:
		return false
	}
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"veo-3.0-generate-001", "veo-3.1-generate-preview", "veo-3.1-fast-generate-preview", omniModelName}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "gemini"
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}
	if strings.HasPrefix(taskID, omniTaskIDPrefix) || strings.HasPrefix(taskID, "v1_") {
		url := omniInteractionURL(baseUrl, unmarkOmniTaskID(taskID))
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("x-goog-api-key", key)
		client, err := service.GetHttpClientWithProxy(proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
		return client.Do(req)
	}

	upstreamName, err := decodeLocalTaskID(taskID)
	if err != nil {
		return nil, fmt.Errorf("decode task_id failed: %w", err)
	}

	// For Gemini API, we use GET request to the operations endpoint
	version := model_setting.GetGeminiVersionSetting("default")
	url := fmt.Sprintf("%s/%s/%s", baseUrl, version, upstreamName)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-goog-api-key", key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var probe struct {
		Status string `json:"status"`
	}
	if common.Unmarshal(respBody, &probe) == nil && probe.Status != "" {
		return ParseOmniTaskResult(respBody)
	}
	var op operationResponse
	if err := common.Unmarshal(respBody, &op); err != nil {
		return nil, fmt.Errorf("unmarshal operation response failed: %w", err)
	}

	ti := &relaycommon.TaskInfo{}

	if op.Error.Message != "" {
		ti.Status = model.TaskStatusFailure
		ti.Reason = op.Error.Message
		ti.Progress = "100%"
		return ti, nil
	}

	if !op.Done {
		ti.Status = model.TaskStatusInProgress
		ti.Progress = "50%"
		return ti, nil
	}

	// done=true 但视频被 RAI 过滤（无成片），视为官方报错，记入 fail_reason
	if op.Response.RaiMediaFilteredCount > 0 {
		ti.Status = model.TaskStatusFailure
		ti.Progress = "100%"
		reason := strings.Join(op.Response.RaiMediaFilteredReasons, "; ")
		if reason == "" {
			reason = fmt.Sprintf("Vertex AI filtered %d video(s) (usage guidelines)", op.Response.RaiMediaFilteredCount)
		}
		ti.Reason = reason
		return ti, nil
	}

	ti.Status = model.TaskStatusSuccess
	ti.Progress = "100%"

	taskID := encodeLocalTaskID(op.Name)
	ti.TaskID = taskID
	contentURL := fmt.Sprintf("%s/v1/videos/%s/content", system_setting.ServerAddress, taskID)

	// 优先用上游返回的视频地址或 base64，写入 fail_reason 供前端展示；否则用 content 代理 URL
	if len(op.Response.GenerateVideoResponse.GeneratedSamples) > 0 {
		if uri := op.Response.GenerateVideoResponse.GeneratedSamples[0].Video.URI; uri != "" {
			ti.RemoteUrl = uri
		}
	}
	if ti.RemoteUrl == "" && len(op.Response.Videos) > 0 {
		if op.Response.Videos[0].BytesBase64Encoded != "" {
			ti.Url = "data:video/mp4;base64," + op.Response.Videos[0].BytesBase64Encoded
		}
	}
	if ti.Url == "" && ti.RemoteUrl == "" && op.Response.BytesBase64Encoded != "" {
		ti.Url = "data:video/mp4;base64," + op.Response.BytesBase64Encoded
	}
	if ti.Url == "" && ti.RemoteUrl == "" && op.Response.Video != "" {
		ti.Url = op.Response.Video
	}
	if ti.Url == "" && ti.RemoteUrl == "" {
		ti.Url = contentURL
	}

	return ti, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	// 模型名优先从 task.Properties 获取（任务提交时已记录），fallback 到从 operation name 解析
	modelName := task.Properties.OriginModelName
	if modelName == "" {
		upstreamName, err := decodeLocalTaskID(task.TaskID)
		if err != nil {
			upstreamName = ""
		}
		modelName = extractModelFromOperationName(upstreamName)
		if strings.TrimSpace(modelName) == "" {
			modelName = "veo-3.0-generate-001"
		}
	}

	video := dto.NewOpenAIVideo()
	video.ID = task.TaskID
	video.Model = modelName
	video.Status = task.Status.ToVideoStatus()
	video.SetProgressStr(task.Progress)
	video.CreatedAt = task.CreatedAt
	if task.FinishTime > 0 {
		video.CompletedAt = task.FinishTime
	} else if task.UpdatedAt > 0 {
		video.CompletedAt = task.UpdatedAt
	}

	// 视频 URL：alpha 引擎存放在 PrivateData.ResultURL，兼容旧数据 fallback 到 FailReason
	resultURL := strings.TrimSpace(task.GetResultURL())
	if isOmniModel(modelName) && (resultURL == "" || isOmniDataURL(resultURL)) {
		if dataURL := OmniVideoURLFromTaskData(task.Data); dataURL != "" {
			resultURL = dataURL
		}
	}
	if resultURL != "" {
		if strings.HasPrefix(resultURL, "http") || strings.HasPrefix(resultURL, "data:") ||
			(isOmniModel(modelName) && strings.HasPrefix(resultURL, "gs://")) {
			video.URL = resultURL
			video.VideoURL = resultURL
			video.SetMetadata("url", resultURL)
			video.SetMetadata("video_url", resultURL)
		}
	}

	// 上游 operation name 透传进 metadata
	if upstreamName, err := decodeLocalTaskID(task.TaskID); err == nil && upstreamName != "" {
		video.SetMetadata("operation_name", upstreamName)
	}

	// 错误处理
	if task.Status == model.TaskStatusFailure && task.FailReason != "" {
		video.Error = &dto.OpenAIVideoError{Message: task.FailReason, Code: "upstream_error"}
	}

	// 将 task.Data 中的完整上游响应数据合并到 metadata，过滤掉计费内部字段
	video.Metadata = relaycommon.MergeUpstreamDataToMetadata(task.Data, video.Metadata)

	return common.Marshal(video)
}

// ============================
// helpers
// ============================

// extractVeoParamsFromRequest extracts Veo-related fields from raw request map into metadata.
func extractVeoParamsFromRequest(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	if m == nil {
		return out
	}
	for _, key := range []string{"durationSeconds", "duration_seconds"} {
		if v, ok := m[key]; ok {
			if n, ok := toInt(v); ok && n > 0 {
				out["durationSeconds"] = n
				break
			}
		}
	}
	for _, key := range []string{"aspectRatio", "aspect_ratio"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				out["aspectRatio"] = s
				break
			}
		}
	}
	for _, key := range []string{"resolution"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				out["resolution"] = s
				break
			}
		}
	}
	for _, key := range []string{"generate_audio", "generateAudio"} {
		if v, ok := m[key]; ok {
			out["generate_audio"] = v
			out["generateAudio"] = v
			break
		}
	}
	for _, key := range []string{"sampleCount", "sample_count"} {
		if v, ok := m[key]; ok {
			if n, ok := toInt(v); ok && n > 0 {
				out["sampleCount"] = n
				break
			}
		}
	}
	if v, ok := m["image"]; ok {
		if s, ok := v.(string); ok && s != "" {
			out["image"] = s
		}
	}
	for _, key := range []string{"lastFrame", "last_frame"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				out["lastFrame"] = s
				break
			}
		}
	}
	if v, ok := m["negativePrompt"]; ok {
		if s, ok := v.(string); ok && s != "" {
			out["negativePrompt"] = s
		}
	}
	if v, ok := m["personGeneration"]; ok {
		if s, ok := v.(string); ok && s != "" {
			out["personGeneration"] = s
		}
	}
	return out
}

func toInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, true
		}
	}
	return 0, false
}

func toBool(value interface{}) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		if s == "true" || s == "1" || s == "yes" {
			return true, true
		}
		if s == "false" || s == "0" || s == "no" {
			return false, true
		}
	case float64:
		return v != 0, true
	case int:
		return v != 0, true
	}
	return false, false
}

func extractStringFromMetadata(metadata map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := metadata[key]; ok {
			switch v := val.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return v
				}
			}
		}
	}
	return ""
}

func extractIntFromMetadata(metadata map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if val, ok := metadata[key]; ok {
			if n, ok := toInt(val); ok {
				return n
			}
		}
	}
	return 0
}

func sanitizeDurationSecondsFromMetadata(metadata map[string]interface{}) int {
	seconds := extractIntFromMetadata(metadata, "durationSeconds", "duration_seconds")
	switch seconds {
	case 4, 6, 8, 12:
		return seconds
	}
	if seconds > 0 {
		return seconds
	}
	return 4
}

func sanitizeAspectRatioFromMetadata(metadata map[string]interface{}) string {
	ratio := strings.ReplaceAll(extractStringFromMetadata(metadata, "aspectRatio", "aspect_ratio"), " ", "")
	ratio = strings.ToLower(ratio)
	switch ratio {
	case "9:16", "9/16", "9-16":
		return "9:16"
	case "16:9", "16/9", "16-9":
		return "16:9"
	default:
		return "16:9"
	}
}

func sanitizeResolutionFromMetadata(metadata map[string]interface{}) string {
	res := strings.ToLower(strings.TrimSpace(extractStringFromMetadata(metadata, "resolution")))
	switch {
	case strings.Contains(res, "720"):
		return "720p"
	case strings.Contains(res, "1080"):
		return "1080p"
	case strings.Contains(res, "4k") || strings.Contains(res, "2160"):
		return "4k"
	default:
		return "1080p"
	}
}

func sanitizeGenerateAudio(metadata map[string]interface{}, fallback *bool) bool {
	for _, key := range []string{"generateAudio", "generate_audio"} {
		if val, ok := metadata[key]; ok {
			if b, ok := toBool(val); ok {
				return b
			}
		}
	}
	if fallback != nil {
		return *fallback
	}
	return true
}

func sanitizePersonGenerationFromMetadata(metadata map[string]interface{}) string {
	value := strings.ToLower(strings.TrimSpace(extractStringFromMetadata(metadata, "personGeneration", "person_generation")))
	switch value {
	case "allow_adult", "dont_allow":
		return value
	default:
		return "allow_all"
	}
}

func encodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

func decodeLocalTaskID(local string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(local)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var modelRe = regexp.MustCompile(`models/([^/]+)/operations/`)

func extractModelFromOperationName(name string) string {
	if name == "" {
		return ""
	}
	if m := modelRe.FindStringSubmatch(name); len(m) == 2 {
		return m[1]
	}
	if idx := strings.Index(name, "models/"); idx >= 0 {
		s := name[idx+len("models/"):]
		if p := strings.Index(s, "/operations/"); p > 0 {
			return s[:p]
		}
	}
	return ""
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	return nil
}
