package gemini

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	if err := relayconvert.ApplyGeminiThinkingConfigChecked(request, info); err != nil {
		return nil, err
	}
	if len(request.Contents) > 0 {
		for i, content := range request.Contents {
			if i == 0 {
				if request.Contents[0].Role == "" {
					request.Contents[0].Role = "user"
				}
			}
			for _, part := range content.Parts {
				if part.FileData != nil {
					if part.FileData.MimeType == "" && strings.Contains(part.FileData.FileUri, "www.youtube.com") {
						part.FileData.MimeType = "video/webm"
					}
				}
			}
		}
	}
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatGemini, req)
	if err != nil {
		return nil, err
	}
	geminiRequest, ok := result.Value.(*dto.GeminiChatRequest)
	if !ok {
		return nil, fmt.Errorf("expected Gemini generateContent request, got %T", result.Value)
	}
	return geminiRequest, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	// imagen 系列：走 :predict（原 Imagen 协议）
	if strings.HasPrefix(info.UpstreamModelName, "imagen") {
		return convertImagenRequest(request)
	}

	// CHZ-PATCH(gemini-imagine-images-generations): nano banana 等 imagine 模型支持
	// 通过 /v1/images/generations 调用，底层走 :generateContent，构造 GeminiChatRequest，
	// 响应在 DoResponse 阶段由 GeminiImagineImageHandler 转回 OpenAI ImageResponse。
	if model_setting.IsGeminiModelSupportImagine(info.UpstreamModelName) {
		return convertImagineImageRequest(c, info, request)
	}

	return nil, errors.New("not supported model for image generation, only imagen / imagine (nano banana) models are supported")
}

// convertImagenRequest 构造 Imagen 系列模型的请求体（走 :predict）
func convertImagenRequest(request dto.ImageRequest) (any, error) {
	aspectRatio := imagineAspectRatioFromSize(request.Size)
	if aspectRatio == "" {
		aspectRatio = "1:1"
	}

	geminiRequest := dto.GeminiImageRequest{
		Instances: []dto.GeminiImageInstance{
			{
				Prompt: request.Prompt,
			},
		},
		Parameters: dto.GeminiImageParameters{
			SampleCount:      int(lo.FromPtrOr(request.N, uint(1))),
			AspectRatio:      aspectRatio,
			PersonGeneration: "allow_adult",
		},
	}

	// Set imageSize when quality parameter is specified
	// Map quality parameter to imageSize (only supported by Standard and Ultra models)
	// quality values: auto, high, medium, low (for gpt-image-1), hd, standard (for dall-e-3)
	// imageSize values: 1K (default), 2K
	// https://ai.google.dev/gemini-api/docs/imagen
	// https://platform.openai.com/docs/api-reference/images/create
	if request.Quality != "" {
		geminiRequest.Parameters.ImageSize = imagineImageSizeFromQuality(request.Quality)
	}

	return geminiRequest, nil
}

// imagenDefaultSampleCount is the number of images Imagen generates when a
// request names none, as Google's GenAI SDK documents it ("currently 4").
const imagenDefaultSampleCount = 4

// OutboundImageCount reads the image count of an image request body:
// Imagen generates parameters.sampleCount images, and its default without
// one.
func (a *Adaptor) OutboundImageCount(body []byte) (*uint, error) {
	var request struct {
		Instances  any `json:"instances"`
		Parameters struct {
			SampleCount *uint `json:"sampleCount"`
		} `json:"parameters"`
	}
	if err := common.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	if request.Parameters.SampleCount == nil && request.Instances != nil {
		return common.GetPointer(uint(imagenDefaultSampleCount)), nil
	}
	return request.Parameters.SampleCount, nil
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {

}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {

	version := model_setting.GetGeminiVersionSetting(info.UpstreamModelName)

	if strings.HasPrefix(info.UpstreamModelName, "imagen") {
		return fmt.Sprintf("%s/%s/models/%s:predict", info.ChannelBaseUrl, version, info.UpstreamModelName), nil
	}

	if strings.HasPrefix(info.UpstreamModelName, "text-embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "gemini-embedding") {
		action := "embedContent"
		if info.IsGeminiBatchEmbedding {
			action = "batchEmbedContents"
		}
		return fmt.Sprintf("%s/%s/models/%s:%s", info.ChannelBaseUrl, version, info.UpstreamModelName, action), nil
	}

	action := "generateContent"
	if info.IsStream {
		action = "streamGenerateContent?alt=sse"
		if info.RelayMode == constant.RelayModeGemini {
			info.DisablePing = true
		}
	}
	return fmt.Sprintf("%s/%s/models/%s:%s", info.ChannelBaseUrl, version, info.UpstreamModelName, action), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	auth, err := service.ResolveGeminiAuth(info.ChannelId, info.ApiKey, info.ChannelOtherSettings.GeminiKeyType, info.ChannelSetting.Proxy)
	if err != nil {
		return err
	}
	auth.Apply(*req)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	result, err := service.ConvertRequest(c, info, types.RelayFormatGemini, request)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	if request.Input == nil {
		return nil, errors.New("input is required")
	}
	// Gemini embeds text only. The request estimate billed for this channel
	// counts image, video and text parts, so a part must fail the request
	// instead of being dropped and charged without reaching Gemini.
	if items, ok := request.Input.([]any); ok && slices.ContainsFunc(items, func(item any) bool {
		_, isPart := item.(map[string]any)
		return isPart
	}) {
		return nil, types.NewErrorWithStatusCode(
			errors.New("gemini embedding models accept only plain text input; images, videos and other content parts are not supported"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	inputs := request.ParseInput()
	if len(inputs) == 0 {
		return nil, errors.New("input is empty")
	}
	// We always build a batch-style payload with `requests`, so ensure we call the
	// batch endpoint upstream to avoid payload/endpoint mismatches.
	info.IsGeminiBatchEmbedding = true
	// process all inputs
	geminiRequests := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		geminiRequest := map[string]any{
			"model": fmt.Sprintf("models/%s", info.UpstreamModelName),
			"content": dto.GeminiChatContent{
				Parts: []dto.GeminiPart{
					{
						Text: input,
					},
				},
			},
		}

		// set specific parameters for different models
		// https://ai.google.dev/api/embeddings?hl=zh-cn#method:-models.embedcontent
		switch info.UpstreamModelName {
		case "text-embedding-004", "gemini-embedding-exp-03-07", "gemini-embedding-001":
			// Only newer models introduced after 2024 support OutputDimensionality
			dimensions := lo.FromPtrOr(request.Dimensions, 0)
			if dimensions > 0 {
				geminiRequest["outputDimensionality"] = dimensions
			}
		}
		geminiRequests = append(geminiRequests, geminiRequest)
	}

	return map[string]any{
		"requests": geminiRequests,
	}, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatGemini, &request)
	if err != nil {
		return nil, err
	}
	geminiRequest, ok := result.Value.(*dto.GeminiChatRequest)
	if !ok {
		return nil, fmt.Errorf("expected Gemini generateContent request, got %T", result.Value)
	}
	return geminiRequest, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.ChannelOtherSettings.GeminiKeyType != dto.GeminiKeyTypeOAuthBroker {
		return channel.DoApiRequest(a, c, info, requestBody)
	}
	return doGeminiOAuthBrokerRequest(a, c, info, requestBody)
}

// doGeminiOAuthBrokerRequest 在 token 过期竞态（余量内被提前吊销）返回 401 时，作废缓存、换新 token 重放一次；
// 第二次仍 401 说明是确定性鉴权错误（scope/项目），直接返回，交给常规错误处理与自动禁用。
func doGeminiOAuthBrokerRequest(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	seeker, seekable := requestBody.(io.Seeker)
	if !seekable && requestBody != nil {
		body, err := io.ReadAll(requestBody)
		if err != nil {
			return nil, fmt.Errorf("read request body failed: %w", err)
		}
		reader := bytes.NewReader(body)
		requestBody, seeker = reader, reader
	}

	resp, err := channel.DoApiRequest(a, c, info, requestBody)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	failedToken := strings.TrimPrefix(resp.Request.Header.Get("Authorization"), "Bearer ")
	_ = resp.Body.Close()
	service.InvalidateGeminiOAuthToken(info.ApiKey, failedToken)
	if seeker != nil {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("rewind request body failed: %w", err)
		}
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.RelayMode == constant.RelayModeResponses {
		if info.IsStream {
			return GeminiResponsesStreamHandler(c, info, resp)
		}
		return GeminiResponsesHandler(c, info, resp)
	}

	if info.RelayMode == constant.RelayModeGemini {
		if strings.Contains(info.RequestURLPath, ":embedContent") ||
			strings.Contains(info.RequestURLPath, ":batchEmbedContents") {
			return NativeGeminiEmbeddingHandler(c, resp, info)
		}
		if info.IsStream {
			return GeminiTextGenerationStreamHandler(c, info, resp)
		} else {
			return GeminiTextGenerationHandler(c, info, resp)
		}
	}

	if strings.HasPrefix(info.UpstreamModelName, "imagen") {
		return GeminiImageHandler(c, info, resp)
	}

	// CHZ-PATCH(gemini-imagine-images-generations): nano banana 等 imagine 模型在
	// /v1/images/generations 与 /v1/images/edits 入口下都走 :generateContent，响应需翻译为
	// OpenAI ImageResponse；对 imagine 来说 generations 与 edits 在协议层没有区别（均通过
	// contents[].parts 的 inlineData 承载输入参考图，输出走 responseModalities）。
	if (info.RelayMode == constant.RelayModeImagesGenerations ||
		info.RelayMode == constant.RelayModeImagesEdits) &&
		model_setting.IsGeminiModelSupportImagine(info.UpstreamModelName) {
		return GeminiImagineImageHandler(c, info, resp)
	}

	// check if the model is an embedding model
	if strings.HasPrefix(info.UpstreamModelName, "text-embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "gemini-embedding") {
		return GeminiEmbeddingHandler(c, info, resp)
	}

	if info.IsStream {
		return GeminiChatStreamHandler(c, info, resp)
	} else {
		return GeminiChatHandler(c, info, resp)
	}

}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
