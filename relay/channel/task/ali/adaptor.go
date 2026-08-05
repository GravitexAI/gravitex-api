package ali

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// AliVideoRequest 阿里通义万相视频生成请求
type AliVideoRequest struct {
	Model      string              `json:"model"`
	Input      AliVideoInput       `json:"input"`
	Parameters *AliVideoParameters `json:"parameters,omitempty"`
}

// AliVideoInput 视频输入参数
type AliVideoInput struct {
	Prompt         string          `json:"prompt,omitempty"`          // 文本提示词
	ImgURL         string          `json:"img_url,omitempty"`         // 首帧图像URL或Base64（图生视频）
	FirstFrameURL  string          `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string          `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	AudioURL       string          `json:"audio_url,omitempty"`       // 音频URL（wan2.5支持）
	NegativePrompt string          `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string          `json:"template,omitempty"`        // 视频特效模板
	ReferenceUrls  []string        `json:"reference_urls,omitempty"`  // 参考文件URL数组（wan2.6-r2v）
	Media          []AliVideoMedia `json:"media,omitempty"`           // 多媒体输入（wan2.7）
}

type AliVideoMedia struct {
	Type           string `json:"type,omitempty"`
	URL            string `json:"url,omitempty"`
	ReferenceVoice string `json:"reference_voice,omitempty"`
}

// AliVideoParameters 视频参数
type AliVideoParameters struct {
	Resolution   string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P（图生视频、首尾帧生视频）
	Size         string `json:"size,omitempty"`          // 尺寸: 如 "832*480"（文生视频、参考生视频）
	Ratio        string `json:"ratio,omitempty"`         // 比例: 16:9（wan2.7-t2v/r2v）
	Duration     int    `json:"duration,omitempty"`      // 时长: 2-15秒
	PromptExtend bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool  `json:"audio,omitempty"`         // 是否添加音频（wan2.5/wan2.6-flash）
	Seed         int    `json:"seed,omitempty"`          // 随机数种子
	ShotType     string `json:"shot_type,omitempty"`     // 镜头类型: single/multi（wan2.6，需prompt_extend=true）
}

// AliVideoResponse 阿里通义万相响应
type AliVideoResponse struct {
	Output    AliVideoOutput `json:"output"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Usage     *AliUsage      `json:"usage,omitempty"`
}

// AliVideoOutput 输出信息
type AliVideoOutput struct {
	TaskID        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	SubmitTime    string `json:"submit_time,omitempty"`
	ScheduledTime string `json:"scheduled_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
	OrigPrompt    string `json:"orig_prompt,omitempty"`
	ActualPrompt  string `json:"actual_prompt,omitempty"`
	VideoURL      string `json:"video_url,omitempty"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

// AliUsage 使用统计
type AliUsage struct {
	Duration            dto.IntValue `json:"duration,omitempty"`
	VideoCount          dto.IntValue `json:"video_count,omitempty"`
	SR                  dto.IntValue `json:"SR,omitempty"`
	Size                string       `json:"size,omitempty"`                  // wan2.6 返回实际分辨率，如 "1280*720"
	OutputVideoDuration int          `json:"output_video_duration,omitempty"` // 实际输出时长（秒）
}

type AliMetadata struct {
	// Input 相关
	AudioURL       string   `json:"audio_url,omitempty"`       // 音频URL
	ImgURL         string   `json:"img_url,omitempty"`         // 图片URL（图生视频）
	FirstFrameURL  string   `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string   `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	NegativePrompt string   `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string   `json:"template,omitempty"`        // 视频特效模板
	ReferenceUrls  []string `json:"reference_urls,omitempty"`  // 参考文件URL数组（wan2.6-r2v）

	// Parameters 相关
	Resolution   *string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P
	Size         *string `json:"size,omitempty"`          // 尺寸: 如 "832*480"
	Ratio        *string `json:"ratio,omitempty"`         // 比例: 16:9
	Duration     *int    `json:"duration,omitempty"`      // 时长
	PromptExtend *bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool   `json:"audio,omitempty"`         // 是否添加音频
	Seed         *int    `json:"seed,omitempty"`          // 随机数种子
	ShotType     *string `json:"shot_type,omitempty"`     // 镜头类型: single/multi
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	ChannelType int
	apiKey      string
	baseURL     string
	aliReq      *AliVideoRequest
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	// 阿里通义万相支持 JSON 格式，不使用 multipart
	var taskReq relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &taskReq); err != nil {
		return service.TaskErrorWrapper(err, "unmarshal_task_request_failed", http.StatusBadRequest)
	}
	// 前端 wan2.5 将 audio_url 放顶层字段，而 TaskSubmitReq 没有该字段；
	// 从原始 body 顶层读取并注入 metadata，使 convertToAliRequest 能正常取到。
	if storage, sErr := common.GetBodyStorage(c); sErr == nil {
		if rawBody, rErr := storage.Bytes(); rErr == nil && len(rawBody) > 0 {
			var rawMap map[string]interface{}
			if uErr := common.Unmarshal(rawBody, &rawMap); uErr == nil {
				if audioURL, ok := rawMap["audio_url"].(string); ok && audioURL != "" {
					if taskReq.Metadata == nil {
						taskReq.Metadata = make(map[string]interface{})
					}
					if _, alreadySet := taskReq.Metadata["audio_url"]; !alreadySet {
						taskReq.Metadata["audio_url"] = audioURL
					}
				}
			}
		}
	}
	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return service.TaskErrorWrapper(err, "convert_to_ali_request_failed", http.StatusInternalServerError)
	}
	a.aliReq = aliReq
	if aliReq.Parameters != nil {
		c.Set("video_billing_resolution", BillingResolutionKeyFromParams(aliReq.Parameters))
	}
	if bodyBytes, err := common.Marshal(aliReq); err == nil {
		logger.LogInfo(c, fmt.Sprintf("[ali] converted request body: %s", common.TruncateJsonValues(string(bodyBytes))))
	}
	if len(taskReq.Metadata) > 0 {
		if metaBytes, err := common.Marshal(taskReq.Metadata); err == nil {
			logger.LogInfo(c, fmt.Sprintf("[ali] client metadata: %s", common.TruncateJsonValues(string(metaBytes))))
		}
	}
	taskErr = relaycommon.ValidateMultipartDirect(c, info)
	if taskErr != nil {
		return taskErr
	}
	switch {
	case isWan27I2VModel(aliReq.Model), isWan27R2VModel(aliReq.Model):
		info.Action = constant.TaskActionGenerate
	case isWan27T2VModel(aliReq.Model):
		info.Action = constant.TaskActionTextGenerate
	}
	return nil
}

// BillingResolutionKeyFromParams 返回按秒计费用的分辨率键：480p / 720p / 1080p（与 VideoModelPricePerSecond 中 flash 分档一致）
func BillingResolutionKeyFromParams(p *AliVideoParameters) string {
	if p == nil {
		return "720p"
	}
	if p.Size != "" {
		if r, err := sizeToResolution(p.Size); err == nil {
			return strings.ToLower(r)
		}
	}
	res := strings.TrimSpace(p.Resolution)
	if res != "" {
		res = strings.ToLower(res)
		if res == "4k" || res == "2160p" || strings.Contains(res, "2160") {
			return "4k"
		}
		if !strings.HasSuffix(res, "p") {
			res = res + "p"
		}
		return res
	}
	return "720p"
}

// ParseBillingResolutionKeyFromUpstreamJSON 从已持久化的上游请求体解析分辨率键（轮询成功计费用）
func ParseBillingResolutionKeyFromUpstreamJSON(body []byte) string {
	var req struct {
		Parameters *AliVideoParameters `json:"parameters"`
	}
	if err := common.Unmarshal(body, &req); err != nil {
		return "720p"
	}
	if req.Parameters == nil {
		return "720p"
	}
	return BillingResolutionKeyFromParams(req.Parameters)
}

// ParseBillingResolutionFromSize 将 usage.size 字符串（如 "1280*720"）转换为分辨率键（如 "720p"）
func ParseBillingResolutionFromSize(size string) string {
	if r, err := sizeToResolution(size); err == nil {
		return strings.ToLower(r)
	}
	return "720p"
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

// BuildRequestHeader sets required headers for Ali API
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	bodyBytes, err := common.Marshal(a.aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_ali_request_failed")
	}

	return bytes.NewReader(bodyBytes), nil
}

var (
	size480p = []string{
		"832*480",
		"480*832",
		"624*624",
	}
	size720p = []string{
		"1280*720",
		"720*1280",
		"960*960",
		"1088*832",
		"832*1088",
	}
	size1080p = []string{
		"1920*1080",
		"1080*1920",
		"1440*1440",
		"1632*1248",
		"1248*1632",
	}
)

func sizeToResolution(size string) (string, error) {
	if lo.Contains(size480p, size) {
		return "480P", nil
	} else if lo.Contains(size720p, size) {
		return "720P", nil
	} else if lo.Contains(size1080p, size) {
		return "1080P", nil
	}
	return "", fmt.Errorf("invalid size: %s", size)
}

func ProcessAliOtherRatios(aliReq *AliVideoRequest) (map[string]float64, error) {
	otherRatios := make(map[string]float64)
	aliRatios := map[string]map[string]float64{
		"wan2.7-t2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.7-i2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.7-r2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.6-t2v": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.6-i2v-flash": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.6-i2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.6-r2v-flash": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.6-r2v": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.5-t2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-t2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.5-i2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-i2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.2-kf2v-flash": {
			"480P":  1,
			"720P":  2,
			"1080P": 4.8,
		},
		"wan2.2-i2v-flash": {
			"480P": 1,
			"720P": 2,
		},
		"wan2.2-s2v": {
			"480P": 1,
			"720P": 0.9 / 0.5,
		},
	}
	var resolution string

	// size match
	if aliReq.Parameters.Size != "" {
		toResolution, err := sizeToResolution(aliReq.Parameters.Size)
		if err != nil {
			return nil, err
		}
		resolution = toResolution
	} else {
		resolution = strings.ToUpper(aliReq.Parameters.Resolution)
		if !strings.HasSuffix(resolution, "P") {
			resolution = resolution + "P"
		}
	}
	if otherRatio, ok := aliRatios[aliReq.Model]; ok {
		if ratio, ok := otherRatio[resolution]; ok {
			otherRatios[fmt.Sprintf("resolution-%s", resolution)] = ratio
		}
	}
	return otherRatios, nil
}

func isWan27Model(model string) bool {
	return strings.HasPrefix(model, "wan2.7-")
}

func isWan27T2VModel(model string) bool {
	return strings.HasPrefix(model, "wan2.7-t2v")
}

func isWan27I2VModel(model string) bool {
	return strings.HasPrefix(model, "wan2.7-i2v")
}

func isWan27R2VModel(model string) bool {
	return strings.HasPrefix(model, "wan2.7-r2v")
}

func hasAliMediaType(media []AliVideoMedia, mediaType string) bool {
	for _, item := range media {
		if item.Type == mediaType && strings.TrimSpace(item.URL) != "" {
			return true
		}
	}
	return false
}

func appendAliMediaIfMissing(media []AliVideoMedia, mediaType, url string) []AliVideoMedia {
	if strings.TrimSpace(url) == "" || hasAliMediaType(media, mediaType) {
		return media
	}
	return append(media, AliVideoMedia{Type: mediaType, URL: url})
}

func looksLikeVideoURL(url string) bool {
	lower := strings.ToLower(url)
	for _, suffix := range []string{".mp4", ".mov", ".avi", ".mkv", ".webm", ".m4v"} {
		if strings.Contains(lower, suffix) {
			return true
		}
	}
	return false
}

func normalizeWan27Input(req relaycommon.TaskSubmitReq, aliReq *AliVideoRequest) {
	if aliReq == nil || !isWan27Model(aliReq.Model) {
		return
	}

	if isWan27I2VModel(aliReq.Model) {
		aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "first_frame", aliReq.Input.ImgURL)
		aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "first_frame", aliReq.Input.FirstFrameURL)
		aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "last_frame", aliReq.Input.LastFrameURL)
		aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "driving_audio", aliReq.Input.AudioURL)
		if len(req.Images) > 0 {
			aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "first_frame", req.Images[0])
		}
		if len(req.Images) > 1 {
			aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "last_frame", req.Images[1])
		}

		aliReq.Input.ImgURL = ""
		aliReq.Input.FirstFrameURL = ""
		aliReq.Input.LastFrameURL = ""
		aliReq.Input.AudioURL = ""
	}

	if isWan27R2VModel(aliReq.Model) {
		for _, url := range aliReq.Input.ReferenceUrls {
			mediaType := "reference_image"
			if looksLikeVideoURL(url) {
				mediaType = "reference_video"
			}
			aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, mediaType, url)
		}
		for _, url := range req.Images {
			aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "reference_image", url)
		}
		if req.InputReference != "" {
			aliReq.Input.Media = appendAliMediaIfMissing(aliReq.Input.Media, "reference_image", req.InputReference)
		}

		aliReq.Input.ReferenceUrls = nil
		aliReq.Input.ImgURL = ""
		aliReq.Input.FirstFrameURL = ""
		aliReq.Input.LastFrameURL = ""
	}
}

func (a *TaskAdaptor) convertToAliRequest(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*AliVideoRequest, error) {
	aliReq := &AliVideoRequest{
		Model: req.Model,
		Input: AliVideoInput{
			Prompt: req.Prompt,
		},
		Parameters: &AliVideoParameters{
			PromptExtend: true, // 默认开启智能改写
			Watermark:    false,
		},
	}

	// wan2.6-r2v: 参考生视频使用 reference_urls 而非 img_url
	if strings.Contains(req.Model, "r2v") && !isWan27R2VModel(req.Model) {
		if len(req.ReferenceUrls) > 0 {
			aliReq.Input.ReferenceUrls = req.ReferenceUrls
		}
	} else {
		// 图生视频/文生视频：传入参考图
		aliReq.Input.ImgURL = req.InputReference
	}

	// 处理分辨率映射
	if req.Size != "" {
		// wan2.7 统一使用 resolution，不将 720P/1080P 转换成 size
		if (isWan27T2VModel(req.Model) || isWan27R2VModel(req.Model)) && !strings.Contains(req.Size, "*") {
			resolution := strings.ToUpper(req.Size)
			if !strings.HasSuffix(resolution, "P") {
				resolution = resolution + "P"
			}
			aliReq.Parameters.Resolution = resolution
		} else if (strings.Contains(req.Model, "t2v") || strings.Contains(req.Model, "r2v")) && !strings.Contains(req.Size, "*") {
			return nil, fmt.Errorf("invalid size: %s, example: %s", req.Size, "1920*1080")
		} else if strings.Contains(req.Size, "*") {
			aliReq.Parameters.Size = req.Size
		} else {
			resolution := strings.ToUpper(req.Size)
			// 支持 480p, 720p, 1080p 或 480P, 720P, 1080P
			if !strings.HasSuffix(resolution, "P") {
				resolution = resolution + "P"
			}
			aliReq.Parameters.Resolution = resolution
		}
	} else if req.Resolution != "" {
		// 客户端明确传了 resolution（如 720p/1080p），直接使用，转大写并补 P
		resolution := strings.ToUpper(req.Resolution)
		if !strings.HasSuffix(resolution, "P") {
			resolution = resolution + "P"
		}
		if (strings.Contains(req.Model, "t2v") || strings.Contains(req.Model, "r2v")) && !isWan27T2VModel(req.Model) && !isWan27R2VModel(req.Model) {
			// t2v/r2v 用 size，将 resolution 反查为 size（默认 16:9）
			resolutionToSize := map[string]string{
				"480P":  "832*480",
				"720P":  "1280*720",
				"1080P": "1920*1080",
			}
			if sz, ok := resolutionToSize[resolution]; ok {
				aliReq.Parameters.Size = sz
			} else {
				aliReq.Parameters.Size = "1280*720"
			}
		} else {
			aliReq.Parameters.Resolution = resolution
		}
	} else {
		// 根据模型设置默认分辨率
		if strings.Contains(req.Model, "t2v") || strings.Contains(req.Model, "r2v") {
			if isWan27T2VModel(req.Model) || isWan27R2VModel(req.Model) {
				aliReq.Parameters.Resolution = "720P"
			} else {
				// 文生视频 / 参考生视频 使用 size
				if strings.HasPrefix(req.Model, "wan2.6") {
					aliReq.Parameters.Size = "1920*1080" // 官方文档默认值
				} else if strings.HasPrefix(req.Model, "wan2.5") {
					aliReq.Parameters.Size = "1920*1080"
				} else if strings.HasPrefix(req.Model, "wan2.2") {
					aliReq.Parameters.Size = "1920*1080"
				} else {
					aliReq.Parameters.Size = "1280*720"
				}
			}
		} else {
			// 图生视频 使用 resolution，按模型设置合理默认值
			if isWan27I2VModel(req.Model) || strings.HasPrefix(req.Model, "wan2.2-i2v-flash") {
				aliReq.Parameters.Resolution = "720P"
			} else {
				// wan2.6-i2v, wan2.6-i2v-flash, wan2.5-i2v-preview, wan2.2-i2v-plus, 其他
				aliReq.Parameters.Resolution = "1080P"
			}
		}
	}

	// 处理时长
	if req.Duration > 0 {
		aliReq.Parameters.Duration = req.Duration
	} else if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, errors.Wrap(err, "convert seconds to int failed")
		} else {
			aliReq.Parameters.Duration = seconds
		}
	} else {
		aliReq.Parameters.Duration = 5 // 默认5秒
	}

	if req.Ratio != "" {
		aliReq.Parameters.Ratio = req.Ratio
	}

	// wan2.6 专有字段
	if strings.HasPrefix(req.Model, "wan2.6") {
		// smart_rewrite / prompt_extend 开关（默认已设为 true，支持覆盖为 false）
		if req.SmartRewrite != nil {
			aliReq.Parameters.PromptExtend = *req.SmartRewrite
		}
		// shot_type: single/multi（需 prompt_extend=true）
		if req.ShotType != "" {
			aliReq.Parameters.ShotType = req.ShotType
		}
		// audio: flash 模型支持有声/无声切换
		if req.Audio != nil {
			aliReq.Parameters.Audio = req.Audio
		}
	}

	// 处理水印
	if req.Watermark != nil {
		aliReq.Parameters.Watermark = *req.Watermark
	}

	// 处理随机种子
	if req.Seed != nil {
		aliReq.Parameters.Seed = *req.Seed
	}

	// 处理音频URL（wan2.5/wan2.6 文生视频配音）
	if req.Metadata != nil {
		if audioURL, ok := req.Metadata["audio_url"].(string); ok && audioURL != "" {
			aliReq.Input.AudioURL = audioURL
		}
	}

	// 从 metadata 中提取额外参数
	if req.Metadata != nil {
		if metadataBytes, err := common.Marshal(req.Metadata); err == nil {
			err = common.Unmarshal(metadataBytes, aliReq)
			if err != nil {
				return nil, errors.Wrap(err, "unmarshal metadata failed")
			}
		} else {
			return nil, errors.Wrap(err, "marshal metadata failed")
		}
	}

	if aliReq.Model != req.Model {
		return nil, errors.New("can't change model with metadata")
	}

	normalizeWan27Input(req, aliReq)

	// otherRatios 已被官方私有化，改用 AddOtherRatio 访问器写入
	info.PriceData.ReplaceOtherRatios(map[string]float64{
		"seconds": float64(aliReq.Parameters.Duration),
	})

	ratios, err := ProcessAliOtherRatios(aliReq)
	if err != nil {
		return nil, err
	}
	for s, f := range ratios {
		info.PriceData.AddOtherRatio(s, f)
	}

	return aliReq, nil
}

// DoRequest delegates to common helper
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// 解析阿里响应
	var aliResp AliVideoResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// 检查错误
	if aliResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
		return
	}

	if aliResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 使用上游真实 ID 作为公开 ID（避免依赖 private_data.upstream_task_id）
	info.PublicTaskID = aliResp.Output.TaskID

	// 转换为 OpenAI 格式响应
	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.CreatedAt = common.GetTimestamp()

	// 将上游返回的原始响应体灌入 metadata，对齐 "上游返回统一进 metadata" 的设计约定，
	// 让 submit / in-progress 轮询 / completed 三种响应格式保持一致（与 doubao / vertex / gemini 等渠道对齐）。
	var meta map[string]any
	if err := common.Unmarshal(responseBody, &meta); err == nil {
		for k, v := range meta {
			openAIResp.SetMetadata(k, v)
		}
	}

	// 返回 OpenAI 格式
	c.JSON(http.StatusOK, openAIResp)

	return aliResp.Output.TaskID, responseBody, nil
}

// FetchTask 查询任务状态
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

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

// ParseTaskResult 解析任务结果
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// 状态映射
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		// 阿里直接返回视频URL，不需要额外的代理端点
		taskResult.Url = aliResp.Output.VideoURL
		// wan2.6 在 usage 中返回实际分辨率（size / SR）和实际输出时长，存入 taskResult 供计费使用
		if aliResp.Usage != nil {
			taskResult.ActualSize = aliResp.Usage.Size
			taskResult.ActualSR = int(aliResp.Usage.SR)
			if aliResp.Usage.OutputVideoDuration > 0 {
				taskResult.ActualDuration = aliResp.Usage.OutputVideoDuration
			} else if aliResp.Usage.Duration > 0 {
				taskResult.ActualDuration = int(aliResp.Usage.Duration)
			}
		}
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		if aliResp.Message != "" {
			taskResult.Reason = aliResp.Message
		} else if aliResp.Output.Message != "" {
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", aliResp.Output.Code, aliResp.Output.Message)
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	// task.Data 轮询合并后已是扁平 map，output.video_url 嵌套丢失；仅用于读取 TaskStatus 和错误信息
	_ = common.Unmarshal(task.Data, &aliResp)

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	// 状态：优先从 task.Status（Go 内部状态）映射，aliResp.Output.TaskStatus 在合并后可能为空
	switch task.Status {
	case model.TaskStatusSuccess:
		openAIResp.Status = dto.VideoStatusCompleted
	case model.TaskStatusFailure:
		openAIResp.Status = dto.VideoStatusFailed
	case model.TaskStatusInProgress:
		openAIResp.Status = dto.VideoStatusInProgress
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		openAIResp.Status = dto.VideoStatusQueued
	default:
		openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	}

	// 视频 URL：alpha 引擎存放在 task.PrivateData.ResultURL，兼容旧数据 fallback 到 FailReason
	// aliResp.Output.VideoURL 因 task.Data 扁平合并后通常为空，作为兜底
	videoURL := task.GetResultURL()
	if videoURL == "" {
		videoURL = aliResp.Output.VideoURL
	}
	if videoURL != "" {
		// 同时写顶层 url / video_url 和 metadata，兼容前端不同读取路径
		openAIResp.URL = videoURL
		openAIResp.VideoURL = videoURL
		openAIResp.SetMetadata("url", videoURL)
		openAIResp.SetMetadata("video_url", videoURL)
	}

	// 透传上游特有字段到 metadata（task.Data 完整数据会通过 MergeUpstreamDataToMetadata 自动合并，
	// 这里只设置需要特殊处理的字段，如 Seconds 同时写入顶层字段）
	if aliResp.Usage != nil {
		if aliResp.Usage.Duration > 0 {
			openAIResp.Seconds = fmt.Sprintf("%d", aliResp.Usage.Duration)
		}
	}

	// 错误处理
	if task.Status == model.TaskStatusFailure {
		msg := task.FailReason
		if msg == "" {
			msg = aliResp.Output.Message
		}
		if msg == "" {
			msg = aliResp.Message
		}
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    "task_failed",
			Message: msg,
		}
	} else if aliResp.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Code,
			Message: aliResp.Message,
		}
	} else if aliResp.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Output.Code,
			Message: aliResp.Output.Message,
		}
	}

	// 将 task.Data 中的完整上游响应数据合并到 metadata，过滤掉计费内部字段
	openAIResp.Metadata = relaycommon.MergeUpstreamDataToMetadata(task.Data, openAIResp.Metadata)

	return common.Marshal(openAIResp)
}

func convertAliStatus(aliStatus string) string {
	switch aliStatus {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
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
