package doubao

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string          `json:"type"`                // "text", "image_url", "video_url", "audio_url" or "video"
	Text     string          `json:"text,omitempty"`      // for text type
	ImageURL *MediaURL       `json:"image_url,omitempty"` // for image_url type
	VideoURL *MediaURL       `json:"video_url,omitempty"` // for video_url type (Seedance 2.0)
	AudioURL *MediaURL       `json:"audio_url,omitempty"` // for audio_url type (Seedance 2.0)
	Video    *VideoReference `json:"video,omitempty"`     // for video (legacy draft) type
	Role     string          `json:"role,omitempty"`      // reference_image / first_frame / last_frame / reference_video / reference_audio
}

type MediaURL struct {
	URL string `json:"url"`
}

type VideoReference struct {
	URL string `json:"url"` // Draft video URL
}

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content"`
	CallbackURL           string         `json:"callback_url,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           string         `json:"service_tier,omitempty"`
	ExecutionExpiresAfter dto.IntValue   `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	Resolution            string         `json:"resolution,omitempty"`
	Ratio                 string         `json:"ratio,omitempty"`
	Duration              dto.IntValue   `json:"duration,omitempty"`
	Frames                dto.IntValue   `json:"frames,omitempty"`
	Seed                  dto.IntValue   `json:"seed,omitempty"`
	CameraFixed           *dto.BoolValue `json:"camera_fixed,omitempty"`
	Watermark             *dto.BoolValue `json:"watermark,omitempty"`
}

type responsePayload struct {
	ID string `json:"id"` // task_id
}

type responseTask struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Seed            int    `json:"seed"`
	Resolution      string `json:"resolution"`
	Duration        int    `json:"duration"`
	Ratio           string `json:"ratio"`
	FramesPerSecond int    `json:"framespersecond"`
	ServiceTier     string `json:"service_tier"`
	Usage           struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
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
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// Accept only POST /v1/video/generations as "generate" action.
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v3/contents/generations/tasks", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling 检测请求 metadata 中是否包含视频输入，返回视频折扣 OtherRatio。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	if hasVideoInMetadata(req.Metadata) {
		if ratio, ok := GetVideoInputRatio(info.OriginModelName); ok {
			return map[string]float64{"video_input": ratio}
		}
	}
	return nil
}

// hasVideoInMetadata 直接检查 metadata 的 content 数组是否包含 video_url 条目，
// 避免构建完整的上游 requestPayload。
func hasVideoInMetadata(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	contentRaw, ok := metadata["content"]
	if !ok {
		return false
	}
	contentSlice, ok := contentRaw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range contentSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemMap["type"] == "video_url" {
			return true
		}
		if _, has := itemMap["video_url"]; has {
			return true
		}
	}
	return false
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	// 渠道 model_mapping：允许将客户端 model 重定向到上游模型/endpoint（例如 ep-xxxx）
	// 统一以 RelayInfo.UpstreamModelName 为准（若未映射则保持原样）
	if info != nil && info.UpstreamModelName != "" {
		req.Model = info.UpstreamModelName
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}

	// 校验 asset:// 引用的素材是否属于当前用户
	if assetIds := extractAssetVirtualIds(body.Content); len(assetIds) > 0 {
		userId := c.GetInt("id")
		notOwned, checkErr := model.CheckUserOwnsAssets(userId, assetIds)
		if checkErr != nil {
			return nil, errors.Wrap(checkErr, "validate asset ownership failed")
		}
		if len(notOwned) > 0 {
			return nil, fmt.Errorf("asset not found or access denied: %s", strings.Join(notOwned, ", "))
		}
	}

	// 检测是否包含视频输入（video_url 类型的 content），用于计费维度区分（noVideo/video）
	hasVideoInput := false
	for _, item := range body.Content {
		if item.Type == "video_url" && item.VideoURL != nil && item.VideoURL.URL != "" {
			hasVideoInput = true
			break
		}
	}
	c.Set("has_video_input", hasVideoInput)

	info.UpstreamModelName = body.Model
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	logger.LogInfo(c, fmt.Sprintf("[doubao] upstream request body: %s", common.TruncateJsonValues(string(data))))
	if len(req.Metadata) > 0 {
		if metaBytes, err := json.Marshal(req.Metadata); err == nil {
			logger.LogInfo(c, fmt.Sprintf("[doubao] client metadata: %s", common.TruncateJsonValues(string(metaBytes))))
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
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// Parse Doubao response
	var dResp responsePayload
	if err := json.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 使用上游真实 ID 作为公开 ID（避免依赖 private_data.upstream_task_id）
	info.PublicTaskID = dResp.ID

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v3/contents/generations/tasks/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// 1) 优先使用客户端直传的 OpenAI 风格 content（保留 role: first_frame/last_frame/reference_image）
	if len(req.Content) > 0 {
		for _, item := range req.Content {
			if item == nil {
				continue
			}
			typ, _ := item["type"].(string)
			role, _ := item["role"].(string)

			switch typ {
			case "text":
				text, _ := item["text"].(string)
				if text == "" {
					continue
				}
				r.Content = append(r.Content, ContentItem{
					Type: "text",
					Text: text,
					Role: role,
				})
			case "image_url":
				var url string
				if m, ok := item["image_url"].(map[string]interface{}); ok && m != nil {
					if u, ok := m["url"].(string); ok {
						url = u
					}
				}
				if url == "" {
					continue
				}
				r.Content = append(r.Content, ContentItem{
					Type:     "image_url",
					ImageURL: &MediaURL{URL: url},
					Role:     role,
				})
			case "video_url":
				var url string
				if m, ok := item["video_url"].(map[string]interface{}); ok && m != nil {
					if u, ok := m["url"].(string); ok {
						url = u
					}
				}
				if url == "" {
					continue
				}
				r.Content = append(r.Content, ContentItem{
					Type:     "video_url",
					VideoURL: &MediaURL{URL: url},
					Role:     role,
				})
			case "audio_url":
				var url string
				if m, ok := item["audio_url"].(map[string]interface{}); ok && m != nil {
					if u, ok := m["url"].(string); ok {
						url = u
					}
				}
				if url == "" {
					continue
				}
				r.Content = append(r.Content, ContentItem{
					Type:     "audio_url",
					AudioURL: &MediaURL{URL: url},
					Role:     role,
				})
			case "video":
				// 预留：若未来支持 video reference，保持透传结构
				var url string
				if m, ok := item["video"].(map[string]interface{}); ok && m != nil {
					if u, ok := m["url"].(string); ok {
						url = u
					}
				}
				if url == "" {
					continue
				}
				r.Content = append(r.Content, ContentItem{
					Type:  "video",
					Video: &VideoReference{URL: url},
					Role:  role,
				})
			default:
				// unknown type: skip
			}
		}
	}

	// 2) 兼容旧客户端：若没有 content，则按旧逻辑由 prompt/images 构建
	if len(r.Content) == 0 {
		// Add text prompt
		if req.Prompt != "" {
			r.Content = append(r.Content, ContentItem{
				Type: "text",
				Text: req.Prompt,
			})
		}
		// Add images if present
		if req.HasImage() {
			for _, imgURL := range req.Images {
				r.Content = append(r.Content, ContentItem{
					Type: "image_url",
					ImageURL: &MediaURL{
						URL: imgURL,
					},
				})
			}
		}
	}

	// 3) 将客户端顶层字段映射到上游请求体（seedance/doubao 常用字段）
	if req.CallbackURL != "" {
		r.CallbackURL = req.CallbackURL
	}
	if req.Resolution != "" {
		r.Resolution = req.Resolution
	}
	if req.Ratio != "" {
		r.Ratio = req.Ratio
	}
	if req.Duration > 0 {
		r.Duration = dto.IntValue(req.Duration)
	}
	if req.GenerateAudio != nil {
		b := dto.BoolValue(*req.GenerateAudio)
		r.GenerateAudio = &b
	}
	if req.CameraFixed != nil {
		b := dto.BoolValue(*req.CameraFixed)
		r.CameraFixed = &b
	}
	if req.Watermark != nil {
		b := dto.BoolValue(*req.Watermark)
		r.Watermark = &b
	}
	if req.Seed != nil {
		r.Seed = dto.IntValue(*req.Seed)
	}
	if req.ReturnLastFrame != nil {
		b := dto.BoolValue(*req.ReturnLastFrame)
		r.ReturnLastFrame = &b
	}

	metadata := req.Metadata
	medaBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, errors.Wrap(err, "metadata marshal metadata failed")
	}
	err = json.Unmarshal(medaBytes, &r)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	return &r, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := json.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// 检测上游错误响应（如 ResourceNotFound 404），优先于状态映射
	if resTask.Error != nil && resTask.Error.Message != "" {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = fmt.Sprintf("%s: %s", resTask.Error.Code, resTask.Error.Message)
		return &taskResult, nil
	}

	// Map Doubao status to internal status
	switch resTask.Status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = resTask.Content.VideoURL
		// 解析 usage 信息用于按倍率计费
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
	case "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = "task failed"
	default:
		// Unknown status, treat as processing
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := json.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)

	// 视频 URL：优先从上游响应取，fallback 到 GetResultURL()（alpha 引擎存 PrivateData.ResultURL，兼容旧 FailReason）
	finalURL := dResp.Content.VideoURL
	if finalURL == "" {
		finalURL = originTask.GetResultURL()
	}
	// 兼容前端轮询：同时写顶层 video_url / url 和 metadata
	openAIVideo.VideoURL = finalURL
	openAIVideo.URL = finalURL
	openAIVideo.SetMetadata("url", finalURL)
	openAIVideo.SetMetadata("video_url", finalURL)
	if dResp.Duration > 0 {
		openAIVideo.Seconds = fmt.Sprintf("%d", dResp.Duration)
	}
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Status == "failed" || (dResp.Error != nil && dResp.Error.Message != "") {
		errMsg := "task failed"
		errCode := "failed"
		if dResp.Error != nil && dResp.Error.Message != "" {
			errMsg = dResp.Error.Message
			if dResp.Error.Code != "" {
				errCode = dResp.Error.Code
			}
		}
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: errMsg,
			Code:    errCode,
		}
	}

	// 将 task.Data 中的完整上游响应数据合并到 metadata，过滤掉计费内部字段
	openAIVideo.Metadata = relaycommon.MergeUpstreamDataToMetadata(originTask.Data, openAIVideo.Metadata)

	jsonData, _ := common.Marshal(openAIVideo)
	return jsonData, nil
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

// extractAssetVirtualIds scans content items for asset:// URLs and returns their virtual IDs.
func extractAssetVirtualIds(items []ContentItem) []string {
	var ids []string
	extract := func(url string) {
		if strings.HasPrefix(url, "asset://") {
			vid := strings.TrimPrefix(url, "asset://")
			if vid != "" {
				ids = append(ids, vid)
			}
		}
	}
	for _, item := range items {
		if item.ImageURL != nil {
			extract(item.ImageURL.URL)
		}
		if item.VideoURL != nil {
			extract(item.VideoURL.URL)
		}
		if item.AudioURL != nil {
			extract(item.AudioURL.URL)
		}
	}
	return ids
}
