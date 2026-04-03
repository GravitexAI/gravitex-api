package custom

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Response structure (OpenAI-compatible)
// ============================

type responseTask struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id,omitempty"`
	Object      string `json:"object"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt int64  `json:"completed_at,omitempty"`
	VideoURL    string `json:"video_url,omitempty"`
	URL         string `json:"url,omitempty"`
	Content     *struct {
		VideoURL string `json:"video_url,omitempty"`
		URL      string `json:"url,omitempty"`
	} `json:"content,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ============================
// Adaptor implementation
// ============================

// TaskAdaptor is a passthrough task adaptor for Custom channel (type=8).
// It forwards requests as-is to upstream OpenAI-style video API:
//   - Submit: POST {baseURL}/v1/video/generations
//   - Poll:   GET  {baseURL}/v1/video/generations/{taskID}
type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateMultipartDirect(c, info)
}

// BuildRequestURL: forward to upstream /v1/video/generations
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/video/generations", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	ct := c.Request.Header.Get("Content-Type")
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	return nil
}

// BuildRequestBody: passthrough original request body, applying model name redirect if configured
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}

	rawBody, err := io.ReadAll(common.ReaderOnly(storage))
	if err != nil {
		return nil, errors.Wrap(err, "read_request_body_failed")
	}

	// Apply model name redirect (model_mapping) if configured
	if info != nil && info.UpstreamModelName != "" {
		var bodyMap map[string]any
		if err := common.Unmarshal(rawBody, &bodyMap); err == nil {
			bodyMap["model"] = info.UpstreamModelName
			if replaced, err := common.Marshal(bodyMap); err == nil {
				rawBody = replaced
			}
		}
	}

	return bytes.NewReader(rawBody), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse: parse upstream OpenAI-style response, extract task ID
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// Support both id and task_id fields
	if dResp.ID == "" {
		if dResp.TaskID == "" {
			taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
			return
		}
		dResp.ID = dResp.TaskID
	}

	// Return OpenAI Video object to frontend
	ov := dto.NewOpenAIVideo()
	ov.ID = dResp.ID
	ov.TaskID = dResp.ID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	ov.Status = dto.VideoStatusQueued

	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

// FetchTask: poll upstream GET /v1/video/generations/{taskID}
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
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return nil // Custom channel does not restrict model list
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ParseTaskResult: parse upstream poll response, map status
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var resTask responseTask
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	switch resTask.Status {
	case "queued", "pending":
		taskResult.Status = model.TaskStatusQueued
	case "processing", "in_progress", "running":
		taskResult.Status = model.TaskStatusInProgress
	case "completed", "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		// Try content.video_url first (uptoken-style), then metadata.url, video_url, url
		if resTask.Content != nil {
			if resTask.Content.VideoURL != "" {
				taskResult.Url = resTask.Content.VideoURL
			} else if resTask.Content.URL != "" {
				taskResult.Url = resTask.Content.URL
			}
		}
		if taskResult.Url == "" && resTask.Metadata != nil {
			if u, ok := resTask.Metadata["url"].(string); ok && u != "" {
				taskResult.Url = u
			}
		}
		if taskResult.Url == "" && resTask.VideoURL != "" {
			taskResult.Url = resTask.VideoURL
		}
		if taskResult.Url == "" && resTask.URL != "" {
			taskResult.Url = resTask.URL
		}
	case "failed", "cancelled":
		taskResult.Status = model.TaskStatusFailure
		if resTask.Error != nil {
			taskResult.Reason = resTask.Error.Message
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusInProgress
	}

	if resTask.Progress > 0 && resTask.Progress < 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return &taskResult, nil
}

// ConvertToOpenAIVideo converts stored task to OpenAI Video format (for GET /v1/videos/:task_id)
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	ov := dto.NewOpenAIVideo()
	ov.ID = originTask.TaskID
	ov.TaskID = originTask.TaskID
	ov.Status = originTask.Status.ToVideoStatus()
	ov.SetProgressStr(originTask.Progress)
	ov.CreatedAt = originTask.CreatedAt
	ov.CompletedAt = originTask.UpdatedAt
	ov.Model = originTask.Properties.OriginModelName

	// Restore video URL from task.Data
	var stored responseTask
	if err := common.Unmarshal(originTask.Data, &stored); err == nil {
		// Try content.video_url first (uptoken-style)
		if stored.Content != nil {
			if stored.Content.VideoURL != "" {
				ov.SetMetadata("url", stored.Content.VideoURL)
				ov.VideoURL = stored.Content.VideoURL
				ov.URL = stored.Content.VideoURL
			} else if stored.Content.URL != "" {
				ov.SetMetadata("url", stored.Content.URL)
				ov.VideoURL = stored.Content.URL
				ov.URL = stored.Content.URL
			}
		}
		if ov.VideoURL == "" && stored.Metadata != nil {
			if u, ok := stored.Metadata["url"].(string); ok && u != "" {
				ov.SetMetadata("url", u)
				ov.VideoURL = u
				ov.URL = u
			}
		}
		if ov.VideoURL == "" && stored.VideoURL != "" {
			ov.SetMetadata("url", stored.VideoURL)
			ov.VideoURL = stored.VideoURL
			ov.URL = stored.VideoURL
		}
		if ov.VideoURL == "" && stored.URL != "" {
			ov.SetMetadata("url", stored.URL)
			ov.VideoURL = stored.URL
			ov.URL = stored.URL
		}
		if stored.Error != nil && originTask.Status == model.TaskStatusFailure {
			ov.Error = &dto.OpenAIVideoError{
				Message: stored.Error.Message,
				Code:    stored.Error.Code,
			}
		}
	}

	return common.Marshal(ov)
}
