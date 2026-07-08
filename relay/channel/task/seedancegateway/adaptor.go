package seedancegateway

import (
	"bytes"
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

type mediaURL struct {
	URL string `json:"url"`
}

type contentItem struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *mediaURL `json:"image_url,omitempty"`
	VideoURL *mediaURL `json:"video_url,omitempty"`
	AudioURL *mediaURL `json:"audio_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}

type requestPayload struct {
	Model         string         `json:"model"`
	Content       []contentItem  `json:"content,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	Duration      dto.IntValue   `json:"duration,omitempty"`
	Resolution    string         `json:"resolution,omitempty"`
	Ratio         string         `json:"ratio,omitempty"`
	GenerateAudio *dto.BoolValue `json:"generate_audio,omitempty"`
	Seed          dto.IntValue   `json:"seed,omitempty"`
	Watermark     *dto.BoolValue `json:"watermark,omitempty"`
	CallbackURL   string         `json:"callback_url,omitempty"`
}

type responsePayload struct {
	ID string `json:"id"`
}

type responseTask struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Model   string `json:"model"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Duration   int    `json:"duration,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Usage      struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

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

	if info != nil && info.UpstreamModelName != "" {
		req.Model = info.UpstreamModelName
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}

	hasVideoInput := false
	for _, item := range body.Content {
		if item.Type == "video_url" && item.VideoURL != nil && item.VideoURL.URL != "" {
			hasVideoInput = true
			break
		}
	}
	c.Set("has_video_input", hasVideoInput)
	if body.Resolution != "" {
		c.Set("video_resolution", body.Resolution)
	}

	info.UpstreamModelName = body.Model
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	logger.LogInfo(c, fmt.Sprintf("[seedance-gateway] upstream request body: %s", common.TruncateJsonValues(string(data))))
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
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	info.PublicTaskID = dResp.ID

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

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
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()

	if normalized, changed, err := normalizeQueryResponse(responseBody); err == nil {
		if changed {
			responseBody = normalized
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(responseBody))
	resp.ContentLength = int64(len(responseBody))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(responseBody)))
	return resp, nil
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
		Content: []contentItem{},
	}

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
				r.Content = append(r.Content, contentItem{Type: "text", Text: text, Role: role})
			case "image_url":
				url := extractURL(item["image_url"])
				if url == "" {
					continue
				}
				r.Content = append(r.Content, contentItem{Type: "image_url", ImageURL: &mediaURL{URL: url}, Role: role})
			case "video_url":
				url := extractURL(item["video_url"])
				if url == "" {
					continue
				}
				r.Content = append(r.Content, contentItem{Type: "video_url", VideoURL: &mediaURL{URL: url}, Role: role})
			case "audio_url":
				url := extractURL(item["audio_url"])
				if url == "" {
					continue
				}
				r.Content = append(r.Content, contentItem{Type: "audio_url", AudioURL: &mediaURL{URL: url}, Role: role})
			}
		}
	}

	if len(r.Content) == 0 {
		if req.Prompt != "" {
			r.Content = append(r.Content, contentItem{Type: "text", Text: req.Prompt})
		}
		if req.HasImage() {
			for _, imgURL := range req.Images {
				r.Content = append(r.Content, contentItem{Type: "image_url", ImageURL: &mediaURL{URL: imgURL}})
			}
		}
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
	if req.Seed != nil {
		r.Seed = dto.IntValue(*req.Seed)
	}
	if req.Watermark != nil {
		b := dto.BoolValue(*req.Watermark)
		r.Watermark = &b
	}
	if req.CallbackURL != "" {
		r.CallbackURL = req.CallbackURL
	}

	if len(req.Metadata) > 0 {
		metaBytes, err := common.Marshal(req.Metadata)
		if err != nil {
			return nil, errors.Wrap(err, "marshal metadata failed")
		}
		if err := common.Unmarshal(metaBytes, &r); err != nil {
			return nil, errors.Wrap(err, "unmarshal metadata failed")
		}
	}

	return &r, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var resTask responseTask
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{Code: 0}
	if resTask.Error != nil && resTask.Error.Message != "" && resTask.Status == "" {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
		return &taskResult, nil
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
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
		taskResult.Resolution = resTask.Resolution
		taskResult.ActualDuration = resTask.Duration
	case "failed", "expired":
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
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal seedance gateway task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)

	finalURL := dResp.Content.VideoURL
	if finalURL == "" {
		finalURL = originTask.GetResultURL()
	}
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

	if dResp.Status == "failed" || dResp.Status == "expired" {
		errMsg := "task failed"
		if dResp.Error != nil && dResp.Error.Message != "" {
			errMsg = dResp.Error.Message
		}
		openAIVideo.Error = &dto.OpenAIVideoError{Message: errMsg, Code: dResp.Status}
	}

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

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

func extractURL(raw any) string {
	if m, ok := raw.(map[string]interface{}); ok && m != nil {
		if u, ok := m["url"].(string); ok {
			return u
		}
	}
	return ""
}

func normalizeQueryResponse(respBody []byte) ([]byte, bool, error) {
	var direct responseTask
	if err := common.Unmarshal(respBody, &direct); err == nil {
		if direct.Status != "" || direct.Content.VideoURL != "" || direct.Error != nil {
			return respBody, false, nil
		}
	}

	var outer struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := common.Unmarshal(respBody, &outer); err != nil {
		return nil, false, err
	}

	if len(outer.Data) == 0 {
		return respBody, false, nil
	}

	resultURL, _ := outer.Data["result_url"].(string)

	if normalized, ok := normalizeNestedTaskData(outer.Data["data"], resultURL); ok {
		data, err := common.Marshal(normalized)
		return data, true, err
	}

	status, _ := outer.Data["status"].(string)
	if status != "" {
		fallback := responseTask{Status: normalizeWrappedStatus(status)}
		fallback.Content.VideoURL = resultURL
		data, err := common.Marshal(fallback)
		return data, true, err
	}

	return respBody, false, nil
}

func normalizeNestedTaskData(raw any, resultURL string) (responseTask, bool) {
	if raw == nil {
		return responseTask{}, false
	}

	if rawMap, ok := raw.(map[string]any); ok {
		var direct responseTask
		rawBytes, err := common.Marshal(rawMap)
		if err == nil && common.Unmarshal(rawBytes, &direct) == nil {
			if direct.Status != "" || direct.Content.VideoURL != "" || direct.Error != nil {
				if direct.Content.VideoURL == "" && resultURL != "" {
					direct.Content.VideoURL = resultURL
				}
				return direct, true
			}
		}

		if nested, ok := rawMap["data"]; ok {
			return normalizeNestedTaskData(nested, resultURL)
		}
	}

	return responseTask{}, false
}

func normalizeWrappedStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "QUEUED":
		return "queued"
	case "IN_PROGRESS", "RUNNING", "PROCESSING":
		return "running"
	case "SUCCESS", "SUCCEEDED", "COMPLETED":
		return "succeeded"
	case "FAILED", "FAILURE", "EXPIRED":
		return "failed"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}
