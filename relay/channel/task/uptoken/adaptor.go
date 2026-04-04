package uptoken

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

type MediaURL struct {
	URL string `json:"url"`
}

type ContentItem struct {
	Type     string    `json:"type"`                // "text", "image_url", "video_url", "audio_url"
	Text     string    `json:"text,omitempty"`      // for text type
	ImageURL *MediaURL `json:"image_url,omitempty"` // for image_url type
	VideoURL *MediaURL `json:"video_url,omitempty"` // for video_url type
	AudioURL *MediaURL `json:"audio_url,omitempty"` // for audio_url type
	Role     string    `json:"role,omitempty"`      // reference_image / first_frame / last_frame / reference_video / reference_audio
}

type requestPayload struct {
	Model         string         `json:"model"`
	Content       []ContentItem  `json:"content,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	Duration      dto.IntValue   `json:"duration,omitempty"`
	Resolution    string         `json:"resolution,omitempty"`
	Ratio         string         `json:"ratio,omitempty"`
	GenerateAudio *dto.BoolValue `json:"generate_audio,omitempty"`
	Seed          dto.IntValue   `json:"seed,omitempty"`
}

type responsePayload struct {
	ID string `json:"id"` // task_id, e.g. "ut-123"
}

type responseTask struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // queued, running, succeeded, failed
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
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

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/video/generations", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	// 渠道 model_mapping：允许将客户端 model 重定向到上游模型（例如 seedance-2-0-pro → uptoken-2.0-pro）
	if info != nil && info.UpstreamModelName != "" {
		req.Model = info.UpstreamModelName
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
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
	logger.LogInfo(c, fmt.Sprintf("[uptoken] upstream request body: %s", common.TruncateJsonValues(string(data))))
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var dResp responsePayload
	if err := json.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = dResp.ID
	ov.TaskID = dResp.ID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	// 延迟写入响应：等 task.Insert() 成功后再写，避免重试时响应体被写两次
	if delay, _ := c.Get(relaycommon.TaskSubmitDelayResponse); delay == true {
		if body, err := common.Marshal(ov); err == nil {
			c.Set(relaycommon.TaskSubmitResponseBody, body)
		}
		return dResp.ID, responseBody, nil
	}
	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v1/video/generations/%s", baseUrl, taskID)

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

	// 1) 优先使用客户端直传的 content 数组（包含 text/image_url/video_url/audio_url + role）
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
			default:
				// unknown type: skip
			}
		}
	}

	// 2) 兼容旧客户端：若没有 content，则按旧逻辑由 prompt/images 构建
	if len(r.Content) == 0 {
		if req.Prompt != "" {
			r.Content = append(r.Content, ContentItem{
				Type: "text",
				Text: req.Prompt,
			})
		}
		if req.HasImage() {
			for _, imgURL := range req.Images {
				r.Content = append(r.Content, ContentItem{
					Type:     "image_url",
					ImageURL: &MediaURL{URL: imgURL},
				})
			}
		}
	}

	// 3) 映射顶层字段到请求体
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
	if req.Seed != nil {
		r.Seed = dto.IntValue(*req.Seed)
	}

	// 4) 透传 metadata 中的额外字段（如 prompt 的替代方式等）
	metadata := req.Metadata
	if len(metadata) > 0 {
		metaBytes, err := json.Marshal(metadata)
		if err != nil {
			return nil, errors.Wrap(err, "marshal metadata failed")
		}
		err = json.Unmarshal(metaBytes, &r)
		if err != nil {
			return nil, errors.Wrap(err, "unmarshal metadata failed")
		}
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

	switch resTask.Status {
	case "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = resTask.Content.VideoURL
		taskResult.TotalTokens = resTask.Usage.TotalTokens
	case "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		if resTask.Error != nil {
			taskResult.Reason = resTask.Error.Message
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := json.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal uptoken task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	finalURL := dResp.Content.VideoURL
	openAIVideo.VideoURL = finalURL
	openAIVideo.URL = finalURL
	openAIVideo.SetMetadata("url", finalURL)
	openAIVideo.SetMetadata("video_url", finalURL)
	openAIVideo.SetMetadata("id", dResp.ID)
	openAIVideo.SetMetadata("status", dResp.Status)
	if dResp.Usage.TotalTokens > 0 {
		openAIVideo.SetMetadata("usage", map[string]any{
			"total_tokens": dResp.Usage.TotalTokens,
		})
	}
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Status == "failed" {
		errMsg := "task failed"
		if dResp.Error != nil {
			errMsg = dResp.Error.Message
		}
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: errMsg,
			Code:    "failed",
		}
	}

	jsonData, _ := common.Marshal(openAIVideo)
	return jsonData, nil
}
