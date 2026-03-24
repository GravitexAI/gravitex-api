package kling

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// ============================
// Request / Response structures
// ============================

type TrajectoryPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type DynamicMask struct {
	Mask         string            `json:"mask,omitempty"`
	Trajectories []TrajectoryPoint `json:"trajectories,omitempty"`
}

type CameraConfig struct {
	Horizontal float64 `json:"horizontal,omitempty"`
	Vertical   float64 `json:"vertical,omitempty"`
	Pan        float64 `json:"pan,omitempty"`
	Tilt       float64 `json:"tilt,omitempty"`
	Roll       float64 `json:"roll,omitempty"`
	Zoom       float64 `json:"zoom,omitempty"`
}

type CameraControl struct {
	Type   string        `json:"type,omitempty"`
	Config *CameraConfig `json:"config,omitempty"`
}

type ImageListItem struct {
	ImageURL string `json:"image_url"`
	Type     string `json:"type,omitempty"` // "first_frame" / "end_frame"
}

type VideoListItem struct {
	VideoURL          string `json:"video_url"`
	ReferType         string `json:"refer_type,omitempty"`          // "feature" / "base"
	KeepOriginalSound string `json:"keep_original_sound,omitempty"` // "yes" / "no"
}

type MultiPromptItem struct {
	Index    int    `json:"index"`
	Prompt   string `json:"prompt"`
	Duration int    `json:"duration"`
}

type WatermarkInfo struct {
	Enabled bool `json:"enabled"`
}

type requestPayload struct {
	Prompt         string            `json:"prompt,omitempty"`
	Image          string            `json:"image,omitempty"`
	ImageTail      string            `json:"image_tail,omitempty"`
	NegativePrompt string            `json:"negative_prompt,omitempty"`
	Mode           string            `json:"mode,omitempty"`
	Duration       string            `json:"duration,omitempty"`
	AspectRatio    string            `json:"aspect_ratio,omitempty"`
	ModelName      string            `json:"model_name,omitempty"`
	Model          string            `json:"model,omitempty"` // Compatible with upstreams that only recognize "model"
	CfgScale       float64           `json:"cfg_scale,omitempty"`
	StaticMask     string            `json:"static_mask,omitempty"`
	DynamicMasks   []DynamicMask     `json:"dynamic_masks,omitempty"`
	CameraControl  *CameraControl    `json:"camera_control,omitempty"`
	CallbackUrl    string            `json:"callback_url,omitempty"`
	ExternalTaskId string            `json:"external_task_id,omitempty"`
	GenerateAudio  *bool             `json:"generate_audio,omitempty"`
	Sound          string            `json:"sound,omitempty"` // "on" / "off"
	MultiShot      *bool             `json:"multi_shot,omitempty"`
	ShotType       string            `json:"shot_type,omitempty"` // "customize" / "intelligence"
	MultiPrompt    []MultiPromptItem `json:"multi_prompt,omitempty"`
	ImageList      []ImageListItem   `json:"image_list,omitempty"` // omni-video
	VideoList      []VideoListItem   `json:"video_list,omitempty"` // omni-video
	WatermarkInfo  *WatermarkInfo    `json:"watermark_info,omitempty"`
}

type responsePayload struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	TaskId    string `json:"task_id"`
	RequestId string `json:"request_id"`
	Data      struct {
		TaskId        string `json:"task_id"`
		TaskStatus    string `json:"task_status"`
		TaskStatusMsg string `json:"task_status_msg"`
		TaskResult    struct {
			Videos []struct {
				Id       string `json:"id"`
				Url      string `json:"url"`
				Duration string `json:"duration"`
			} `json:"videos"`
		} `json:"task_result"`
		CreatedAt int64 `json:"created_at"`
		UpdatedAt int64 `json:"updated_at"`
	} `json:"data"`
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

	// apiKey format: "access_key|secret_key"
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	taskErr = relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
	if taskErr != nil {
		return taskErr
	}
	// 前端对 kling 直接传顶层字段（如 aspect_ratio、image_tail），
	// 而 TaskSubmitReq 没有这些字段，需要从原始 body 提取并注入 metadata，
	// 使 BuildRequestBody 里的 metadata 覆盖逻辑能正常工作。
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil
	}
	rawBody, err := storage.Bytes()
	if err != nil || len(rawBody) == 0 {
		return nil
	}
	var rawMap map[string]interface{}
	if err := common.Unmarshal(rawBody, &rawMap); err != nil {
		return nil
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
		req.Metadata = make(map[string]interface{})
	}
	// 将顶层 kling 专有字段注入 metadata（不覆盖 metadata 里已有的值）
	for _, key := range []string{"aspect_ratio", "image_tail", "negative_prompt", "cfg_scale", "static_mask", "dynamic_masks", "camera_control", "callback_url", "external_task_id", "sound", "multi_shot", "shot_type", "multi_prompt", "image_list", "video_list", "watermark_info"} {
		if val, exists := rawMap[key]; exists {
			if _, alreadySet := req.Metadata[key]; !alreadySet {
				req.Metadata[key] = val
			}
		}
	}
	c.Set("task_request", req)
	// 将 duration 写入 context 供计费使用（resolveRequestedSeconds / parseVideoSeconds 优先读此值）
	if req.Duration > 0 {
		c.Set("video_seconds", req.Duration)
	}
	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	var path string
	switch info.Action {
	case constant.TaskActionOmniGenerate:
		path = "/v1/videos/omni-video"
	case constant.TaskActionGenerate:
		path = "/v1/videos/image2video"
	default:
		path = "/v1/videos/text2video"
	}

	if isNewAPIRelay(info.ApiKey) {
		return fmt.Sprintf("%s/kling%s", a.baseURL, path), nil
	}

	return fmt.Sprintf("%s%s", a.baseURL, path), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	token, err := a.createJWTToken()
	if err != nil {
		return fmt.Errorf("failed to create JWT token: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "kling-sdk/1.0")
	return nil
}

// BuildRequestBody converts request into Kling specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, err
	}
	// 三路 action 判定: omni-video / image2video / text2video
	isOmni := strings.Contains(body.ModelName, "omni")
	if isOmni {
		c.Set("action", constant.TaskActionOmniGenerate)
	} else if body.Image == "" && body.ImageTail == "" {
		c.Set("action", constant.TaskActionTextGenerate)
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	logger.LogInfo(c, fmt.Sprintf("[kling] upstream request body: %s", common.TruncateJsonValues(string(data))))
	if len(req.Metadata) > 0 {
		if metaBytes, err := common.Marshal(req.Metadata); err == nil {
			logger.LogInfo(c, fmt.Sprintf("[kling] client metadata: %s", common.TruncateJsonValues(string(metaBytes))))
		}
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	if action := c.GetString("action"); action != "" {
		info.Action = action
	}
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	var kResp responsePayload
	err = common.Unmarshal(responseBody, &kResp)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}
	if kResp.Code != 0 {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", kResp.Message), "task_failed", http.StatusBadRequest)
		return
	}
	ov := dto.NewOpenAIVideo()
	ov.ID = kResp.Data.TaskId
	ov.TaskID = kResp.Data.TaskId
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return kResp.Data.TaskId, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}
	action, ok := body["action"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid action")
	}
	var path string
	switch action {
	case constant.TaskActionOmniGenerate:
		path = "/v1/videos/omni-video"
	case constant.TaskActionGenerate:
		path = "/v1/videos/image2video"
	default:
		path = "/v1/videos/text2video"
	}
	url := fmt.Sprintf("%s%s/%s", baseUrl, path, taskID)
	if isNewAPIRelay(key) {
		url = fmt.Sprintf("%s/kling%s/%s", baseUrl, path, taskID)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	token, err := a.createJWTTokenWithKey(key)
	if err != nil {
		token = key
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "kling-sdk/1.0")

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{
		"kling-v1",
		"kling-v1-6",
		"kling-v2-master",
		"kling-v3",
		"kling-v3-pro",
		"kling-v3-omni",
		"kling-v3-omni-pro",
	}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "kling"
}

// ============================
// helpers
// ============================

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	realModel, defaultMode := normalizeKlingModelAndMode(req.Model)
	r := requestPayload{
		Prompt:         req.Prompt,
		Image:          req.Image,
		Mode:           defaultString(req.Mode, defaultMode),
		Duration:       fmt.Sprintf("%d", defaultInt(req.Duration, 5)),
		AspectRatio:    a.getAspectRatio(req.Size),
		ModelName:      realModel,
		Model:          realModel, // Keep consistent with model_name, double writing improves compatibility
		CfgScale:       0.5,
		StaticMask:     "",
		DynamicMasks:   []DynamicMask{},
		CameraControl:  nil,
		CallbackUrl:    "",
		ExternalTaskId: "",
		GenerateAudio:  req.GenerateAudio,
	}
	if r.ModelName == "" {
		r.ModelName = "kling-v1"
		r.Model = "kling-v1"
	}
	metadata := req.Metadata
	medaBytes, err := common.Marshal(metadata)
	if err != nil {
		return nil, errors.Wrap(err, "metadata marshal metadata failed")
	}
	err = common.Unmarshal(medaBytes, &r)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	// Kling API 要求纯 base64，不能带 data:image/...;base64, 前缀
	r.Image = stripDataURIPrefix(r.Image)
	r.ImageTail = stripDataURIPrefix(r.ImageTail)
	for i := range r.ImageList {
		r.ImageList[i].ImageURL = stripDataURIPrefix(r.ImageList[i].ImageURL)
	}

	// sound ↔ generate_audio 同步
	if r.Sound == "" && r.GenerateAudio != nil && *r.GenerateAudio {
		r.Sound = "on"
	}
	if r.Sound == "on" {
		boolTrue := true
		r.GenerateAudio = &boolTrue
	} else if r.Sound == "off" || r.Sound == "" {
		boolFalse := false
		r.GenerateAudio = &boolFalse
	}

	// omni 模型: image/image_tail → image_list 转换
	isOmni := strings.Contains(realModel, "omni")
	if isOmni && len(r.ImageList) == 0 {
		if r.Image != "" {
			r.ImageList = append(r.ImageList, ImageListItem{ImageURL: r.Image, Type: "first_frame"})
		}
		if r.ImageTail != "" {
			r.ImageList = append(r.ImageList, ImageListItem{ImageURL: r.ImageTail, Type: "end_frame"})
		}
		r.Image = ""
		r.ImageTail = ""
	}
	// 非 omni 模型清除 omni 专属字段
	if !isOmni {
		r.ImageList = nil
		r.VideoList = nil
	}

	// sanitize: Kling 仅支持 16:9 / 9:16 / 1:1 / 4:3，其他值回退到 16:9
	r.AspectRatio = sanitizeKlingAspectRatio(r.AspectRatio)
	return &r, nil
}

func (a *TaskAdaptor) getAspectRatio(size string) string {
	switch size {
	case "1024x1024", "512x512":
		return "1:1"
	case "1280x720", "1920x1080":
		return "16:9"
	case "720x1280", "1080x1920":
		return "9:16"
	default:
		return "1:1"
	}
}

// sanitizeKlingAspectRatio 将不被 Kling API 支持的比例回退到 16:9。
// Kling 支持：16:9 / 9:16 / 1:1 / 4:3
func sanitizeKlingAspectRatio(ratio string) string {
	switch ratio {
	case "16:9", "9:16", "1:1", "4:3":
		return ratio
	default:
		return "16:9"
	}
}

// stripDataURIPrefix 去除 data:image/...;base64, 前缀，Kling API 只接受纯 base64 字符串。
// 对 HTTP URL 和空字符串不做处理。
func stripDataURIPrefix(s string) string {
	if strings.HasPrefix(s, "data:") {
		if idx := strings.Index(s, ","); idx != -1 {
			return s[idx+1:]
		}
	}
	return s
}

func defaultString(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func defaultInt(v int, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// normalizeKlingModelAndMode strips the "-pro" suffix from the model name and
// returns the real upstream model name together with the default mode to use.
// Examples:
//
//	"kling-v3-pro"      → ("kling-v3",      "pro")
//	"kling-v3-omni-pro" → ("kling-v3-omni", "pro")
//	"kling-v3"          → ("kling-v3",      "std")
//	"kling-v1"          → ("kling-v1",      "std")
func normalizeKlingModelAndMode(model string) (realModel string, defaultMode string) {
	if strings.HasSuffix(model, "-pro") {
		return strings.TrimSuffix(model, "-pro"), "pro"
	}
	return model, "std"
}

// ============================
// JWT helpers
// ============================

func (a *TaskAdaptor) createJWTToken() (string, error) {
	return a.createJWTTokenWithKey(a.apiKey)
}

func (a *TaskAdaptor) createJWTTokenWithKey(apiKey string) (string, error) {
	if isNewAPIRelay(apiKey) {
		return apiKey, nil // new api relay
	}
	keyParts := strings.Split(apiKey, "|")
	if len(keyParts) != 2 {
		return "", errors.New("invalid api_key, required format is accessKey|secretKey")
	}
	accessKey := strings.TrimSpace(keyParts[0])
	if len(keyParts) == 1 {
		return accessKey, nil
	}
	secretKey := strings.TrimSpace(keyParts[1])
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iss": accessKey,
		"exp": now + 1800, // 30 minutes
		"nbf": now - 5,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = "JWT"
	return token.SignedString([]byte(secretKey))
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	taskInfo := &relaycommon.TaskInfo{}
	resPayload := responsePayload{}
	err := common.Unmarshal(respBody, &resPayload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}
	taskInfo.Code = resPayload.Code
	taskInfo.TaskID = resPayload.Data.TaskId
	taskInfo.Reason = resPayload.Data.TaskStatusMsg
	//任务状态，枚举值：submitted（已提交）、processing（处理中）、succeed（成功）、failed（失败）
	status := resPayload.Data.TaskStatus
	switch status {
	case "submitted":
		taskInfo.Status = model.TaskStatusSubmitted
	case "processing":
		taskInfo.Status = model.TaskStatusInProgress
	case "succeed":
		taskInfo.Status = model.TaskStatusSuccess
	case "failed":
		taskInfo.Status = model.TaskStatusFailure
	default:
		return nil, fmt.Errorf("unknown task status: %s", status)
	}
	if videos := resPayload.Data.TaskResult.Videos; len(videos) > 0 {
		video := videos[0]
		taskInfo.Url = video.Url
	}
	return taskInfo, nil
}

func isNewAPIRelay(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-")
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var klingResp responsePayload
	if err := common.Unmarshal(originTask.Data, &klingResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal kling task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Model = originTask.Properties.OriginModelName
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = klingResp.Data.CreatedAt
	openAIVideo.CompletedAt = klingResp.Data.UpdatedAt

	// 将上游 kling 响应的全部字段透传进 metadata，便于前端/控制台查看完整信息
	openAIVideo.SetMetadata("task_id", klingResp.Data.TaskId)
	openAIVideo.SetMetadata("task_status", klingResp.Data.TaskStatus)
	openAIVideo.SetMetadata("task_status_msg", klingResp.Data.TaskStatusMsg)
	openAIVideo.SetMetadata("created_at", klingResp.Data.CreatedAt)
	openAIVideo.SetMetadata("updated_at", klingResp.Data.UpdatedAt)

	if len(klingResp.Data.TaskResult.Videos) > 0 {
		video := klingResp.Data.TaskResult.Videos[0]
		if video.Url != "" {
			openAIVideo.URL = video.Url
			openAIVideo.VideoURL = video.Url
			openAIVideo.SetMetadata("url", video.Url)
			openAIVideo.SetMetadata("video_url", video.Url)
		}
		if video.Duration != "" {
			openAIVideo.Seconds = video.Duration
			openAIVideo.SetMetadata("duration", video.Duration)
		}
		openAIVideo.SetMetadata("video_id", video.Id)
	}

	if klingResp.Code != 0 && klingResp.Message != "" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: klingResp.Message,
			Code:    fmt.Sprintf("%d", klingResp.Code),
		}
	}
	jsonData, _ := common.Marshal(openAIVideo)
	return jsonData, nil
}
