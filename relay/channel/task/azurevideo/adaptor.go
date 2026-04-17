// Package azurevideo implements the TaskAdaptor for Azure OpenAI Sora-2 video generation.
//
// Azure OpenAI Sora-2 uses a slightly different API from OpenAI Sora:
//   - Endpoint: {baseURL}/openai/v1/videos?api-version={version}
//   - Auth header: api-key (instead of Authorization: Bearer)
//   - On completion the response contains a direct download URL in the `url` field — no proxy needed.
//
// The default API version is "2025-04-01-preview". Override per-channel by setting the channel's
// ApiVersion field in the admin UI.
package azurevideo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const defaultAPIVersion = "2025-04-01-preview"

// ============================
// Request / Response structures
// ============================

// responseTask mirrors the Azure OpenAI video task response.
// Azure returns the same shape as OpenAI Sora with minor additions.
type responseTask struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt int64  `json:"completed_at,omitempty"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
	// On completion, Azure provides a direct download URL (no further auth needed to GET it).
	URL string `json:"url,omitempty"`
	// Nested generations list (alternative response shape)
	Generations []struct {
		ID  string `json:"id"`
		URL string `json:"url,omitempty"`
	} `json:"generations,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	apiKey                string
	baseURL               string
	apiVersion            string
	upstreamModelName     string
	azureModelApiVersions map[string]string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	a.upstreamModelName = info.UpstreamModelName
	a.azureModelApiVersions = info.ChannelOtherSettings.AzureModelApiVersions
	a.apiVersion = a.resolveAPIVersion(info.ApiVersion)
}

// resolveAPIVersion returns the effective API version to use, in priority order:
// 1. Model-specific version from AzureModelApiVersions matching the upstream model name
// 2. First value in AzureModelApiVersions (fallback when model name is unknown, e.g. during polling)
// 3. Channel-level api_version (channel.Other field)
// 4. Hard-coded default
func (a *TaskAdaptor) resolveAPIVersion(channelVersion string) string {
	if len(a.azureModelApiVersions) > 0 {
		// Exact model match first
		if a.upstreamModelName != "" {
			if v, ok := a.azureModelApiVersions[a.upstreamModelName]; ok && v != "" {
				common.SysLog(fmt.Sprintf("[AzureVideo] using model-specific API version: %s (model: %s)", v, a.upstreamModelName))
				return v
			}
		}
		// Fallback: use the first configured version when model name is unavailable (e.g. during background polling)
		for model, v := range a.azureModelApiVersions {
			if v != "" {
				common.SysLog(fmt.Sprintf("[AzureVideo] using first AzureModelApiVersions entry as fallback: %s (model: %s)", v, model))
				return v
			}
		}
	}
	if channelVersion != "" {
		return channelVersion
	}
	return defaultAPIVersion
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateMultipartDirect(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.Action == "remix" {
		return fmt.Sprintf("%s/openai/v1/videos/%s/remix?api-version=%s", a.baseURL, info.OriginTaskID, a.apiVersion), nil
	}
	return fmt.Sprintf("%s/openai/v1/videos?api-version=%s", a.baseURL, a.apiVersion), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("api-key", a.apiKey)
	// Content-Type is set after BuildRequestBody runs (stored in context by BuildRequestBody).
	// Fall back to the original header if nothing was stored.
	if ct, exists := c.Get("azure_video_content_type"); exists {
		req.Header.Set("Content-Type", ct.(string))
	} else {
		req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	}
	return nil
}

// BuildRequestBody converts the multipart form sent by the frontend into the JSON body
// that Azure OpenAI Sora-2 expects, performing the following transformations:
//   - width + height  →  size  (e.g. "1280x720")
//   - n_seconds       →  seconds
//   - Only whitelisted parameters are forwarded (model, prompt, seconds, size, input_reference, user)
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	// Parse the incoming multipart form.
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, errors.Wrap(err, "parse_multipart_form_failed")
	}

	// Flatten single-value fields into a plain map.
	params := make(map[string]interface{})
	for key, vals := range form.Value {
		if len(vals) > 0 {
			params[key] = vals[0]
		}
	}

	// --- 1. n_seconds → seconds ---
	if _, hasSec := params["seconds"]; !hasSec {
		if ns, ok := params["n_seconds"]; ok {
			params["seconds"] = ns
		}
	}
	delete(params, "n_seconds")

	// --- 2. width + height → size ---
	if _, hasSize := params["size"]; !hasSize {
		w, _ := params["width"].(string)
		h, _ := params["height"].(string)
		if w != "" && h != "" {
			params["size"] = fmt.Sprintf("%sx%s", w, h)
			common.SysLog(fmt.Sprintf("[AzureVideo] converted width=%s height=%s → size=%s", w, h, params["size"]))
		} else {
			params["size"] = "1280x720"
			common.SysLog("[AzureVideo] no size/width/height provided, defaulting to 1280x720")
		}
	}
	delete(params, "width")
	delete(params, "height")

	// --- 3. Validate seconds (Sora-2 supports 4 / 8 / 12) ---
	validSeconds := map[string]bool{"4": true, "8": true, "12": true}
	if sec, ok := params["seconds"].(string); ok {
		if !validSeconds[sec] {
			common.SysLog(fmt.Sprintf("[AzureVideo] invalid seconds=%s, defaulting to 4", sec))
			params["seconds"] = "4"
		}
	} else {
		params["seconds"] = "4"
	}

	// --- 4. Whitelist filter ---
	allowed := map[string]bool{
		"model": true, "prompt": true, "seconds": true,
		"size": true, "input_reference": true, "user": true,
	}
	filtered := make(map[string]interface{})
	for k, v := range params {
		if allowed[k] {
			filtered[k] = v
		}
	}

	// Store validated seconds in context for billing (before potentially returning multipart body).
	if sec, ok := filtered["seconds"].(string); ok {
		c.Set("azure_video_seconds", sec)
	}

	common.SysLog(fmt.Sprintf("[AzureVideo] final params: model=%v prompt=%v size=%v seconds=%v has_input_reference=%v",
		filtered["model"], filtered["prompt"], filtered["size"], filtered["seconds"], filtered["input_reference"] != nil))

	// --- 5. Handle input_reference: Azure requires it as a file upload (multipart), not a JSON string ---
	if inputRef, exists := filtered["input_reference"]; exists && inputRef != nil {
		inputRefStr, ok := inputRef.(string)
		if ok && inputRefStr != "" {
			imageBytes, mimeType, err := convertImageToBytes(inputRefStr)
			if err != nil {
				common.SysLog(fmt.Sprintf("[AzureVideo] input_reference conversion failed: %v, sending without image", err))
				delete(filtered, "input_reference")
			} else {
				common.SysLog(fmt.Sprintf("[AzureVideo] input_reference converted (%d bytes, %s), using multipart", len(imageBytes), mimeType))
				return buildMultipartRequest(c, filtered, imageBytes, mimeType)
			}
		} else {
			delete(filtered, "input_reference")
		}
	}

	// No image — use JSON body.
	c.Set("azure_video_content_type", "application/json")

	body, err := common.Marshal(filtered)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_request_body_failed")
	}
	return bytes.NewReader(body), nil
}

// convertImageToBytes converts a URL or base64 data URI to raw image bytes + MIME type.
func convertImageToBytes(imageInput string) ([]byte, string, error) {
	// base64 data URI: data:image/xxx;base64,...
	if strings.HasPrefix(imageInput, "data:image/") {
		mimeType := "image/jpeg"
		if strings.Contains(imageInput, "image/png") {
			mimeType = "image/png"
		} else if strings.Contains(imageInput, "image/webp") {
			mimeType = "image/webp"
		}
		parts := strings.Split(imageInput, ",")
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("invalid base64 data URI")
		}
		imageBytes, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, "", fmt.Errorf("base64 decode failed: %w", err)
		}
		return imageBytes, mimeType, nil
	}

	// HTTP/HTTPS URL — download the image.
	if strings.HasPrefix(imageInput, "http://") || strings.HasPrefix(imageInput, "https://") {
		resp, err := http.Get(imageInput) //nolint:noctx
		if err != nil {
			return nil, "", fmt.Errorf("download image failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("download image non-200: %d", resp.StatusCode)
		}
		imageBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("read image body failed: %w", err)
		}
		mimeType := resp.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		// Strip charset/params from content-type if present
		if idx := strings.Index(mimeType, ";"); idx != -1 {
			mimeType = strings.TrimSpace(mimeType[:idx])
		}
		return imageBytes, mimeType, nil
	}

	// Bare base64 string (no data: prefix) — attempt to decode.
	imageBytes, err := base64.StdEncoding.DecodeString(imageInput)
	if err != nil {
		return nil, "", fmt.Errorf("unsupported image format: not a URL or valid base64")
	}
	return imageBytes, "image/jpeg", nil
}

// buildMultipartRequest builds a multipart/form-data request body with the image as a file part.
func buildMultipartRequest(c *gin.Context, params map[string]interface{}, imageBytes []byte, mimeType string) (io.Reader, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Write all non-image fields as form fields.
	for key, value := range params {
		if key == "input_reference" {
			continue
		}
		var valueStr string
		switch v := value.(type) {
		case string:
			valueStr = v
		case float64:
			valueStr = fmt.Sprintf("%.0f", v)
		case int:
			valueStr = fmt.Sprintf("%d", v)
		default:
			jsonBytes, _ := common.Marshal(v)
			valueStr = string(jsonBytes)
		}
		if err := writer.WriteField(key, valueStr); err != nil {
			return nil, fmt.Errorf("write field %s failed: %w", key, err)
		}
	}

	// Write the image as a file part with correct MIME type.
	ext := mimeTypeToExt(mimeType)
	fileName := "reference_image" + ext
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="input_reference"; filename="%s"`, fileName))
	h.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(h)
	if err != nil {
		return nil, fmt.Errorf("create file part failed: %w", err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		return nil, fmt.Errorf("write image bytes failed: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer failed: %w", err)
	}

	// Store boundary in context so BuildRequestHeader sets the correct Content-Type.
	c.Set("azure_video_multipart_boundary", writer.Boundary())
	c.Set("azure_video_content_type", "multipart/form-data; boundary="+writer.Boundary())

	common.SysLog(fmt.Sprintf("[AzureVideo] multipart body built: %d bytes, image=%s (%d bytes)", body.Len(), fileName, len(imageBytes)))
	return body, nil
}

func mimeTypeToExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
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

	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(
			errors.Wrapf(err, "body: %s", string(responseBody)),
			"unmarshal_response_body_failed",
			http.StatusInternalServerError,
		)
		return
	}
	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task id is empty in response"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 使用上游真实 ID 作为公开 ID（避免依赖 private_data.upstream_task_id）
	upstreamTaskID := dResp.ID
	info.PublicTaskID = upstreamTaskID

	if delay, _ := c.Get(relaycommon.TaskSubmitDelayResponse); delay == true {
		if body, err := common.Marshal(dResp); err == nil {
			c.Set(relaycommon.TaskSubmitResponseBody, body)
		}
		return upstreamTaskID, responseBody, nil
	}
	c.JSON(http.StatusOK, dResp)
	return upstreamTaskID, responseBody, nil
}

// FetchTask polls Azure for the video task status.
// URL: GET {baseURL}/openai/v1/videos/{taskID}?api-version={version}
//
// When the task is completed, Azure does NOT embed a video URL in the status response.
// A second GET to /openai/v1/videos/{taskID}/content?variant=video&api-version={version}
// is needed — Azure responds with a 302 redirect to a pre-signed CDN URL.
// We follow the redirect, capture the final URL, and inject it into the returned body as
// a synthetic "url" field so that ParseTaskResult can treat it like a direct-URL provider.
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	base := strings.TrimRight(baseUrl, "/")
	statusURI := fmt.Sprintf("%s/openai/v1/videos/%s?api-version=%s", base, taskID, a.apiVersion)

	req, err := http.NewRequest(http.MethodGet, statusURI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api-key", key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("create proxy http client failed: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}

	// Read the status body.
	statusBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read status body failed: %w", err)
	}

	// Parse just the status field to decide whether to fetch content.
	var peek struct {
		Status string `json:"status"`
		URL    string `json:"url"`
	}
	_ = common.Unmarshal(statusBody, &peek)

	// If completed but no URL in status body, call the /content endpoint to get the CDN URL.
	if peek.Status == "completed" && peek.URL == "" {
		contentURI := fmt.Sprintf("%s/openai/v1/videos/%s/content?variant=video&api-version=%s", base, taskID, a.apiVersion)
		common.SysLog(fmt.Sprintf("[AzureVideo] task completed, fetching CDN URL from: %s", contentURI))

		contentReq, reqErr := http.NewRequest(http.MethodGet, contentURI, nil)
		if reqErr == nil {
			contentReq.Header.Set("api-key", key)
			contentClient, clientErr := service.GetHttpClientWithProxy(proxy)
			if clientErr != nil {
				contentClient = &http.Client{}
			}
			contentResp, contentErr := contentClient.Do(contentReq)
			if contentErr == nil {
				defer contentResp.Body.Close()
				switch contentResp.StatusCode {
				case http.StatusFound, http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
					// Azure returns 302 with Location = pre-signed CDN URL.
					if cdnURL := contentResp.Header.Get("Location"); cdnURL != "" {
						common.SysLog(fmt.Sprintf("[AzureVideo] got CDN redirect URL: %s", cdnURL))
						var taskMap map[string]interface{}
						if common.Unmarshal(statusBody, &taskMap) == nil {
							taskMap["url"] = cdnURL
							if enriched, marshalErr := common.Marshal(taskMap); marshalErr == nil {
								statusBody = enriched
							}
						}
					}
				case http.StatusOK:
					// Azure returns the video bytes directly — read and encode as base64 data URI
					// so downstream OSS upload (UploadBase64ToOSS) can handle it uniformly.
					videoBytes, readErr := io.ReadAll(contentResp.Body)
					if readErr == nil && len(videoBytes) > 0 {
						mimeType := contentResp.Header.Get("Content-Type")
						if mimeType == "" {
							mimeType = "video/mp4"
						}
						dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(videoBytes)
						common.SysLog(fmt.Sprintf("[AzureVideo] downloaded video directly (%d bytes), encoded as base64 data URI", len(videoBytes)))
						var taskMap map[string]interface{}
						if common.Unmarshal(statusBody, &taskMap) == nil {
							taskMap["url"] = dataURI
							if enriched, marshalErr := common.Marshal(taskMap); marshalErr == nil {
								statusBody = enriched
							}
						}
					} else if readErr != nil {
						common.SysLog(fmt.Sprintf("[AzureVideo] failed to read /content body: %v", readErr))
					}
				default:
					common.SysLog(fmt.Sprintf("[AzureVideo] /content returned unexpected status %d", contentResp.StatusCode))
				}
			} else {
				common.SysLog(fmt.Sprintf("[AzureVideo] failed to fetch /content: %v", contentErr))
			}
		}
	}

	// Re-wrap the (possibly enriched) body into a synthetic http.Response.
	syntheticResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(statusBody)),
		Header:     resp.Header,
	}
	return syntheticResp, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var resTask responseTask
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	ti := &relaycommon.TaskInfo{}

	switch resTask.Status {
	case "queued", "pending":
		ti.Status = model.TaskStatusQueued
	case "processing", "in_progress":
		ti.Status = model.TaskStatusInProgress
	case "completed":
		ti.Status = model.TaskStatusSuccess
		ti.Progress = "100%"
		// Extract the video URL injected by FetchTask (either a CDN redirect URL or a base64 data URI).
		videoURL := extractAzureVideoURL(&resTask)
		if strings.HasPrefix(videoURL, "data:") {
			// Video bytes were downloaded directly and encoded as a data URI — treat as inline content.
			ti.Url = videoURL
		} else {
			// Pre-signed CDN URL — download without auth headers.
			ti.RemoteUrl = videoURL
		}
	case "failed", "cancelled":
		ti.Status = model.TaskStatusFailure
		if resTask.Error != nil {
			ti.Reason = resTask.Error.Message
		} else {
			ti.Reason = "task failed"
		}
	default:
		ti.Status = model.TaskStatusInProgress
	}

	if resTask.Progress > 0 && resTask.Progress < 100 {
		ti.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return ti, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"sora-2", "sora-2-pro"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "azure-video"
}

// ConvertToOpenAIVideo returns the task data in OpenAI Sora format.
// If the task succeeded and a result URL is stored (PrivateData.ResultURL or FailReason),
// it is injected into the "url" field so the frontend always receives a stable HTTP URL (never a base64 blob).
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	if len(task.Data) == 0 {
		return task.Data, nil
	}
	var m map[string]any
	if err := common.Unmarshal(task.Data, &m); err != nil {
		return task.Data, nil
	}
	// Inject result URL (OSS URL, CDN URL, or base64 data URI) into the "url" field.
	ossURL := task.GetResultURL()
	if ossURL != "" && (strings.HasPrefix(ossURL, "http") || strings.HasPrefix(ossURL, "data:")) {
		m["url"] = ossURL
	}
	// 过滤掉计费内部字段，避免暴露到前端
	relaycommon.StripBillingInternalKeys(m)
	out, err := common.Marshal(m)
	if err != nil {
		return task.Data, nil
	}
	return out, nil
}

// extractAzureVideoURL tries to find the download URL in the Azure response.
func extractAzureVideoURL(t *responseTask) string {
	if t.URL != "" {
		return t.URL
	}
	for _, g := range t.Generations {
		if g.URL != "" {
			return g.URL
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
