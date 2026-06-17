package vertex

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

// ============================
// Request / Response structures
// ============================

type requestPayload struct {
	Instances  []map[string]any `json:"instances"`
	Parameters map[string]any   `json:"parameters,omitempty"`
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
// Extracts Veo params from raw body into Metadata and sets video_seconds for billing (same as Gemini).
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	taskErr = relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate)
	if taskErr != nil {
		return taskErr
	}
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
		if n, ok := vertexToInt(durationSec); ok && n > 0 {
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

// isAPIKey returns true when the key is a plain API key (not a JSON service account).
func isAPIKey(key string) bool {
	return len(key) > 0 && key[0] != '{'
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = "veo-3.0-generate-001"
	}

	region := vertexcore.GetModelRegion(info.ApiVersion, modelName)
	if strings.TrimSpace(region) == "" {
		region = "global"
	}

	// API Key 模式：predictLongRunning 在 aiplatform.googleapis.com 需要 project 路径，
	// 但 API Key 没有 JSON credentials 无法获取 projectID，所以切换到 Google AI 端点。
	if info.ChannelOtherSettings.VertexKeyType == dto.VertexKeyTypeAPIKey || isAPIKey(a.apiKey) {
		baseURL := strings.TrimRight(a.baseURL, "/")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com"
		}
		version := model_setting.GetGeminiVersionSetting(modelName)
		return fmt.Sprintf("%s/%s/models/%s:predictLongRunning", baseURL, version, modelName), nil
	}

	// Service Account JSON 模式
	adc := &vertexcore.Credentials{}
	if err := json.Unmarshal([]byte(a.apiKey), adc); err != nil {
		return "", fmt.Errorf("failed to decode credentials: %w", err)
	}
	if region == "global" {
		return fmt.Sprintf(
			"https://aiplatform.googleapis.com/v1/projects/%s/locations/global/publishers/google/models/%s:predictLongRunning",
			adc.ProjectID,
			modelName,
		), nil
	}
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:predictLongRunning",
		region,
		adc.ProjectID,
		region,
		modelName,
	), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// API Key 模式：通过 x-goog-api-key 头部认证（与 Gemini task adaptor 一致）
	if info.ChannelOtherSettings.VertexKeyType == dto.VertexKeyTypeAPIKey || isAPIKey(a.apiKey) {
		req.Header.Set("x-goog-api-key", a.apiKey)
		return nil
	}

	// Service Account JSON 模式：获取 access token
	adc := &vertexcore.Credentials{}
	if err := json.Unmarshal([]byte(a.apiKey), adc); err != nil {
		return fmt.Errorf("failed to decode credentials: %w", err)
	}

	proxy := ""
	if info != nil {
		proxy = info.ChannelSetting.Proxy
	}
	token, err := vertexcore.AcquireAccessToken(*adc, proxy)
	if err != nil {
		return fmt.Errorf("failed to acquire access token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-goog-user-project", adc.ProjectID)
	return nil
}

// BuildRequestBody converts request into Vertex specific format.
// Supports text-to-video (prompt only), first-frame (image), and first+last-frame (image + lastFrame) per Veo API.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, ok := c.Get("task_request")
	if !ok {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	instance := map[string]any{"prompt": req.Prompt}
	metadata := req.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	// 首帧图片：文生视频不传；首帧/首尾帧生视频需传到上游
	if imageVal, ok := metadata["image"]; ok {
		if imageStr, ok := imageVal.(string); ok && strings.TrimSpace(imageStr) != "" {
			b64, err := vertexConvertToBase64(imageStr)
			if err != nil {
				return nil, fmt.Errorf("image conversion failed: %w", err)
			}
			instance["image"] = map[string]any{
				"bytesBase64Encoded": b64,
				"mimeType":           vertexDetectImageMimeType(imageStr),
			}
		}
	}
	// 尾帧图片：首尾帧生视频
	if lastFrameVal, ok := metadata["lastFrame"]; ok {
		if lastFrameStr, ok := lastFrameVal.(string); ok && strings.TrimSpace(lastFrameStr) != "" {
			b64, err := vertexConvertToBase64(lastFrameStr)
			if err != nil {
				return nil, fmt.Errorf("lastFrame conversion failed: %w", err)
			}
			instance["lastFrame"] = map[string]any{
				"bytesBase64Encoded": b64,
				"mimeType":           vertexDetectImageMimeType(lastFrameStr),
			}
		}
	}

	body := requestPayload{
		Instances:  []map[string]any{instance},
		Parameters: map[string]any{},
	}
	if v, ok := metadata["storageUri"]; ok {
		body.Parameters["storageUri"] = v
	}
	sampleCount := vertexSanitizeSampleCount(metadata)
	body.Parameters["sampleCount"] = sampleCount
	if sampleCount <= 0 {
		return nil, fmt.Errorf("sampleCount must be greater than 0")
	}
	durationSeconds := vertexSanitizeDurationSeconds(metadata)
	body.Parameters["durationSeconds"] = durationSeconds
	body.Parameters["aspectRatio"] = vertexSanitizeAspectRatio(metadata)
	body.Parameters["resolution"] = vertexSanitizeResolution(metadata)
	includeAudio := vertexSanitizeGenerateAudio(metadata, req.GenerateAudio)
	body.Parameters["generateAudio"] = includeAudio

	info.PriceData.OtherRatios = map[string]float64{
		"sampleCount":     float64(sampleCount),
		"durationSeconds": float64(durationSeconds),
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	logger.LogInfo(c, fmt.Sprintf("[vertex] upstream request body: %s", common.TruncateJsonValues(string(data))))
	if len(req.Metadata) > 0 {
		if metaBytes, err := common.Marshal(req.Metadata); err == nil {
			logger.LogInfo(c, fmt.Sprintf("[vertex] client metadata: %s", common.TruncateJsonValues(string(metaBytes))))
		}
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var s submitResponse
	if err := json.Unmarshal(responseBody, &s); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
	}
	if strings.TrimSpace(s.Name) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("missing operation name"), "invalid_response", http.StatusInternalServerError)
	}
	localID := encodeLocalTaskID(s.Name)
	info.PublicTaskID = localID

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	// 将上游 Vertex 返回的原始字段灌入 metadata，对齐 "上游返回统一进 metadata" 的设计约定，
	// 让 submit / in-progress 轮询 / completed 三种响应格式保持一致（与 doubao / gemini 等渠道对齐）。
	var meta map[string]any
	if err := common.Unmarshal(responseBody, &meta); err == nil {
		for k, v := range meta {
			ov.SetMetadata(k, v)
		}
	}

	if delay, _ := c.Get(relaycommon.TaskSubmitDelayResponse); delay == true {
		if body, err := common.Marshal(ov); err == nil {
			c.Set(relaycommon.TaskSubmitResponseBody, body)
		}
		return localID, responseBody, nil
	}
	c.JSON(http.StatusOK, ov)
	return localID, responseBody, nil
}

func (a *TaskAdaptor) GetModelList() []string { return []string{"veo-3.0-generate-001"} }
func (a *TaskAdaptor) GetChannelName() string { return "vertex" }

func buildFetchOperationURL(baseURL, upstreamName string) (string, error) {
	region := extractRegionFromOperationName(upstreamName)
	if region == "" {
		region = "us-central1"
	}
	project := extractProjectFromOperationName(upstreamName)
	modelName := extractModelFromOperationName(upstreamName)
	if strings.TrimSpace(modelName) == "" {
		return "", fmt.Errorf("cannot extract model from operation name")
	}
	if strings.TrimSpace(project) == "" {
		return "", fmt.Errorf("cannot extract project from operation name")
	}
	return vertexcore.BuildGoogleModelURL(baseURL, vertexcore.DefaultAPIVersion, project, region, modelName, "fetchPredictOperation"), nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}
	upstreamName, err := decodeLocalTaskID(taskID)
	if err != nil {
		return nil, fmt.Errorf("decode task_id failed: %w", err)
	}
	region := extractRegionFromOperationName(upstreamName)
	if region == "" {
		region = "us-central1"
	}
	project := extractProjectFromOperationName(upstreamName)
	modelName := extractModelFromOperationName(upstreamName)
	if modelName == "" {
		return nil, fmt.Errorf("cannot extract model from operation name")
	}

	// API Key 模式：使用 Google AI 端点 + x-goog-api-key 头部（与 Gemini task adaptor 一致）
	if isAPIKey(key) {
		fetchBaseURL := strings.TrimRight(baseUrl, "/")
		if fetchBaseURL == "" {
			fetchBaseURL = "https://generativelanguage.googleapis.com"
		}
		version := model_setting.GetGeminiVersionSetting("default")
		url := fmt.Sprintf("%s/%s/%s", fetchBaseURL, version, upstreamName)

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

	// Service Account JSON 模式
	if project == "" {
		return nil, fmt.Errorf("cannot extract project from operation name")
	}
	var url string
	if region == "global" {
		url = fmt.Sprintf("https://aiplatform.googleapis.com/v1/projects/%s/locations/global/publishers/google/models/%s:fetchPredictOperation", project, modelName)
	} else {
		url = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:fetchPredictOperation", region, project, region, modelName)
	}
	payload := map[string]string{"operationName": upstreamName}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	adc := &vertexcore.Credentials{}
	if err := json.Unmarshal([]byte(key), adc); err != nil {
		return nil, fmt.Errorf("failed to decode credentials: %w", err)
	}
	token, err := vertexcore.AcquireAccessToken(*adc, proxy)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire access token: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-goog-user-project", adc.ProjectID)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var op operationResponse
	if err := json.Unmarshal(respBody, &op); err != nil {
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
	// done=true 但视频被 RAI 过滤（无成片），视为失败，fail_reason 填 raiMediaFilteredReasons
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
	if len(op.Response.Videos) > 0 {
		v0 := op.Response.Videos[0]
		if v0.BytesBase64Encoded != "" {
			mime := strings.TrimSpace(v0.MimeType)
			if mime == "" {
				enc := strings.TrimSpace(v0.Encoding)
				if enc == "" {
					enc = "mp4"
				}
				if strings.Contains(enc, "/") {
					mime = enc
				} else {
					mime = "video/" + enc
				}
			}
			ti.Url = "data:" + mime + ";base64," + v0.BytesBase64Encoded
			return ti, nil
		}
	}
	if op.Response.BytesBase64Encoded != "" {
		enc := strings.TrimSpace(op.Response.Encoding)
		if enc == "" {
			enc = "mp4"
		}
		mime := enc
		if !strings.Contains(enc, "/") {
			mime = "video/" + enc
		}
		ti.Url = "data:" + mime + ";base64," + op.Response.BytesBase64Encoded
		return ti, nil
	}
	if op.Response.Video != "" { // some variants use `video` as base64
		enc := strings.TrimSpace(op.Response.Encoding)
		if enc == "" {
			enc = "mp4"
		}
		mime := enc
		if !strings.Contains(enc, "/") {
			mime = "video/" + enc
		}
		ti.Url = "data:" + mime + ";base64," + op.Response.Video
		return ti, nil
	}
	return ti, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	upstreamName, err := decodeLocalTaskID(task.TaskID)
	if err != nil {
		upstreamName = ""
	}
	modelName := extractModelFromOperationName(upstreamName)
	if strings.TrimSpace(modelName) == "" {
		modelName = "veo-3.0-generate-001"
	}
	v := dto.NewOpenAIVideo()
	v.ID = task.TaskID
	v.Model = modelName
	v.Status = task.Status.ToVideoStatus()
	v.SetProgressStr(task.Progress)
	v.CreatedAt = task.CreatedAt
	v.CompletedAt = task.UpdatedAt

	// 失败时把上游错误（如 Google RAI）写入 error 和 fail_reason，便于前端轮询展示
	failReason := strings.TrimSpace(task.FailReason)
	if task.Status == model.TaskStatusFailure && failReason != "" {
		v.Error = &dto.OpenAIVideoError{Message: failReason, Code: "upstream_error"}
	}

	// 在顶层返回 url（成功时为视频地址）或 fail_reason（失败时为错误说明），与 TaskDto 对齐
	type openAIVideoExtra struct {
		*dto.OpenAIVideo
		Url        string `json:"url,omitempty"`
		FailReason string `json:"fail_reason,omitempty"`
	}
	extra := openAIVideoExtra{OpenAIVideo: v}
	if failReason != "" {
		if strings.HasPrefix(failReason, "http") || strings.HasPrefix(failReason, "data:") {
			extra.Url = failReason
		} else {
			extra.FailReason = failReason
		}
	}

	// 将 task.Data 中的完整上游响应数据合并到 metadata，过滤掉计费内部字段
	v.Metadata = relaycommon.MergeUpstreamDataToMetadata(task.Data, v.Metadata)

	return common.Marshal(extra)
}

// ============================
// helpers
// ============================

func vertexToInt(value interface{}) (int, bool) {
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

func extractVeoParamsFromRequest(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	if m == nil {
		return out
	}
	for _, key := range []string{"durationSeconds", "duration_seconds"} {
		if v, ok := m[key]; ok {
			if n, ok := vertexToInt(v); ok && n > 0 {
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
			if n, ok := vertexToInt(v); ok && n > 0 {
				out["sampleCount"] = n
				break
			}
		}
	}
	// 首帧图片（文生视频不传；首帧/首尾帧生视频需传到上游）
	if v, ok := m["image"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out["image"] = s
		}
	}
	// 尾帧图片（首尾帧生视频）
	for _, key := range []string{"lastFrame", "last_frame"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out["lastFrame"] = s
				break
			}
		}
	}
	return out
}

func vertexExtractInt(metadata map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if val, ok := metadata[key]; ok {
			if n, ok := vertexToInt(val); ok {
				return n
			}
		}
	}
	return 0
}

func vertexExtractString(metadata map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := metadata[key]; ok {
			if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func vertexSanitizeDurationSeconds(metadata map[string]interface{}) int {
	seconds := vertexExtractInt(metadata, "durationSeconds", "duration_seconds")
	switch seconds {
	case 4, 6, 8, 12:
		return seconds
	}
	if seconds > 0 {
		return seconds
	}
	return 4
}

func vertexSanitizeSampleCount(metadata map[string]interface{}) int {
	count := vertexExtractInt(metadata, "sampleCount", "sample_count")
	if count <= 0 {
		return 1
	}
	if count > 4 {
		return 4
	}
	return count
}

func vertexSanitizeAspectRatio(metadata map[string]interface{}) string {
	ratio := strings.ReplaceAll(vertexExtractString(metadata, "aspectRatio", "aspect_ratio"), " ", "")
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

func vertexSanitizeResolution(metadata map[string]interface{}) string {
	res := strings.ToLower(strings.TrimSpace(vertexExtractString(metadata, "resolution")))
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

func vertexToBool(value interface{}) (bool, bool) {
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

func vertexSanitizeGenerateAudio(metadata map[string]interface{}, fallback *bool) bool {
	for _, key := range []string{"generateAudio", "generate_audio"} {
		if val, ok := metadata[key]; ok {
			if b, ok := vertexToBool(val); ok {
				return b
			}
		}
	}
	if fallback != nil {
		return *fallback
	}
	return true
}

// vertexConvertToBase64 将 data URI 或 URL 转为纯 base64，供 Vertex instances.image/lastFrame 使用
func vertexConvertToBase64(input string) (string, error) {
	if strings.HasPrefix(input, "data:") {
		parts := strings.SplitN(input, ",", 2)
		if len(parts) == 2 {
			return parts[1], nil
		}
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Get(input)
		if err != nil {
			return "", fmt.Errorf("failed to download image from URL: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to download image from URL: HTTP %d", resp.StatusCode)
		}
		imageData, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read image data: %w", err)
		}
		return base64.StdEncoding.EncodeToString(imageData), nil
	}
	return input, nil
}

// vertexDetectImageMimeType 从 data URI、URL 扩展名或 base64 魔数检测 MIME 类型
func vertexDetectImageMimeType(input string) string {
	if strings.HasPrefix(input, "data:") {
		parts := strings.Split(input, ";")
		if len(parts) >= 1 {
			mimeType := strings.TrimPrefix(parts[0], "data:")
			if mimeType != "" {
				return mimeType
			}
		}
	}
	// URL — 从扩展名推断
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		lower := strings.ToLower(input)
		switch {
		case strings.Contains(lower, ".png"):
			return "image/png"
		case strings.Contains(lower, ".webp"):
			return "image/webp"
		case strings.Contains(lower, ".gif"):
			return "image/gif"
		case strings.Contains(lower, ".bmp"):
			return "image/bmp"
		}
		return "image/jpeg"
	}
	if len(input) > 10 {
		if strings.HasPrefix(input, "/9j/") || strings.HasPrefix(input, "/9j4") {
			return "image/jpeg"
		}
		if strings.HasPrefix(input, "iVBORw0KGgo") {
			return "image/png"
		}
		if strings.HasPrefix(input, "UklGR") {
			return "image/webp"
		}
	}
	return "image/jpeg"
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

var regionRe = regexp.MustCompile(`locations/([a-z0-9-]+)/`)

func extractRegionFromOperationName(name string) string {
	m := regionRe.FindStringSubmatch(name)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

var modelRe = regexp.MustCompile(`models/([^/]+)/operations/`)

func extractModelFromOperationName(name string) string {
	m := modelRe.FindStringSubmatch(name)
	if len(m) == 2 {
		return m[1]
	}
	idx := strings.Index(name, "models/")
	if idx >= 0 {
		s := name[idx+len("models/"):]
		if p := strings.Index(s, "/operations/"); p > 0 {
			return s[:p]
		}
	}
	return ""
}

var projectRe = regexp.MustCompile(`projects/([^/]+)/locations/`)

func extractProjectFromOperationName(name string) string {
	m := projectRe.FindStringSubmatch(name)
	if len(m) == 2 {
		return m[1]
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
