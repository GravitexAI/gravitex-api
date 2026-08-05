package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func truncateImageLogBody(body []byte) string {
	bodyStr := strings.TrimSpace(string(body))
	if bodyStr == "" {
		return "<empty>"
	}
	// JSON 请求体按字段截断，避免 base64 等长字符串把整条日志刷爆。
	if strings.HasPrefix(bodyStr, "{") || strings.HasPrefix(bodyStr, "[") {
		return common.TruncateJsonValues(bodyStr)
	}
	const maxLogLen = 2000
	if len(bodyStr) > maxLogLen {
		return bodyStr[:maxLogLen] + fmt.Sprintf("...(truncated,total=%d)", len(body))
	}
	return bodyStr
}

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	// 记录用户侧解析后的图片请求参数，便于对比后续实际发往上游的内容。
	if requestBytes, err := common.Marshal(imageReq); err == nil {
		var requestMap map[string]json.RawMessage
		if err := common.Unmarshal(requestBytes, &requestMap); err == nil {
			for key, value := range imageReq.Extra {
				if _, exists := requestMap[key]; !exists {
					requestMap[key] = value
				}
			}
			if mergedBytes, err := common.Marshal(requestMap); err == nil {
				requestBytes = mergedBytes
			}
		}
		logger.LogInfo(c, fmt.Sprintf("image user request params: %s", common.TruncateJsonValues(string(requestBytes))))
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	// gpt-image 系列：当 /v1/images/generations 请求包含 image 字段时，自动升级为 edits 模式
	if info.RelayMode == relayconstant.RelayModeImagesGenerations && strings.HasPrefix(request.Model, "gpt-image") {
		hasImage := request.Image != nil && len(request.Image) > 0
		if !hasImage && request.Extra != nil {
			_, hasImage = request.Extra["image"]
			if !hasImage {
				_, hasImage = request.Extra["images"]
			}
		}
		if hasImage {
			info.RelayMode = relayconstant.RelayModeImagesEdits
			info.RequestURLPath = strings.Replace(info.RequestURLPath, "/images/generations", "/images/edits", 1)
			logger.LogDebug(c, fmt.Sprintf("gpt-image auto-upgrade: generations → edits (model=%s)", request.Model))
		}
	}

	// gpt-image 图生图：打印用户入参摘要
	if strings.HasPrefix(request.Model, "gpt-image") && info.RelayMode == relayconstant.RelayModeImagesEdits {
		imageCount := 0
		imageSummary := "none"
		if request.Image != nil && len(request.Image) > 0 {
			// 判断 Image 是 string 还是 []string
			raw := string(request.Image)
			if len(raw) > 0 && raw[0] == '[' {
				var arr []interface{}
				if err := common.Unmarshal(request.Image, &arr); err == nil {
					imageCount = len(arr)
				}
			} else {
				imageCount = 1
			}
			// 截断 base64 数据，只显示前 80 字符
			if len(raw) > 80 {
				imageSummary = raw[:80] + "...(truncated)"
			} else {
				imageSummary = raw
			}
		}
		n := uint(1)
		if request.N != nil {
			n = *request.N
		}
		logger.LogInfo(c, fmt.Sprintf("gpt-image edits user request: model=%s, prompt=%q, size=%s, quality=%s, n=%d, input_fidelity=%q, image_count=%d, image_preview=%s",
			request.Model, request.Prompt, request.Size, request.Quality, n, lo.FromPtr(request.InputFidelity), imageCount, imageSummary))
	} else if strings.HasPrefix(request.Model, "gpt-image") && info.RelayMode == relayconstant.RelayModeImagesGenerations {
		n := uint(1)
		if request.N != nil {
			n = *request.N
		}
		logger.LogInfo(c, fmt.Sprintf("gpt-image generations user request: model=%s, prompt=%q, size=%s, quality=%s, n=%d",
			request.Model, request.Prompt, request.Size, request.Quality, n))
	}

	var requestBody io.Reader

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		// 透传模式下直接记录原始请求体，排查"用户传了什么"与"上游收到了什么"是否一致。
		if bodyBytes, err := storage.Bytes(); err == nil {
			logger.LogInfo(c, fmt.Sprintf("image upstream request body(size=%d): %s", len(bodyBytes), truncateImageLogBody(bodyBytes)))
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			bodyBytes := convertedRequest.(*bytes.Buffer).Bytes()
			// multipart / buffer 形式的上游请求在这里记录最终内容，方便定位转换后的差异。
			logger.LogInfo(c, fmt.Sprintf("image upstream request body(size=%d): %s", len(bodyBytes), truncateImageLogBody(bodyBytes)))
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err := common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}
			// JSON 形式的上游请求在应用参数覆盖后记录，确保日志反映最终发送结果。
			logger.LogInfo(c, fmt.Sprintf("image upstream request body(size=%d): %s", len(jsonData), truncateImageLogBody(jsonData)))

			if common.DebugEnabled {
				const maxLogLen = 2000
				bodyStr := string(jsonData)
				if len(bodyStr) > maxLogLen {
					bodyStr = bodyStr[:maxLogLen] + "...(truncated)"
				}
				logger.LogDebug(c, fmt.Sprintf("image request body: %s", bodyStr))
			}
			body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			defer closer.Close()
			jsonData = nil
			info.UpstreamRequestBodySize = size
			requestBody = body
		}
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	if info.PriceData.ImagePerImagePricing != nil {
		if actualPrice, billingUsage, err := helper.SettleImagePerImageUsage(*info.PriceData.ImagePerImagePricing, request, usage.(*dto.Usage)); err == nil {
			info.PriceData.ModelPrice = actualPrice
			info.PriceData.ImageBillingUsage = billingUsage
			if billingUsage != nil {
				info.PriceData.PerImageUnitPrice = info.PriceData.ImagePerImagePricing.OutputImage[billingUsage.OutputSizeTier]
			}
			info.PriceData.ImagePriceMultiplier = 1
		}
	}

	// 按张计费：优先使用上游返回的 usage.generated_images（实际生成数量），
	// 其次回退到请求入参的 N，兜底为 1。
	// 解决场景：用户请求 n=4，但上游（如 seedream-5）实际只返回 1 张图，
	// 不应该按 4 张计费。
	imageN := uint(1)
	if u, ok := usage.(*dto.Usage); ok && u != nil && u.GeneratedImages > 0 {
		imageN = uint(u.GeneratedImages)
	} else if request.N != nil && *request.N > 0 {
		imageN = *request.N
	}

	// n is handled via OtherRatio so it is applied exactly once in quota
	// calculation (both price-based and ratio-based paths).
	// Adaptors may have already set a more accurate count from the
	// upstream response; only set the default when they haven't.
	// 保留 main-alpha 的 ImagePerImagePricing 判断（按张计费已含实际张数与输入成本），
	// 同时用官方新的 HasOtherRatio 访问器（otherRatios 已私有化）。
	if info.PriceData.UsePrice && info.PriceData.ImagePerImagePricing == nil {
		if !info.PriceData.HasOtherRatio("n") {
			info.PriceData.AddOtherRatio("n", float64(imageN))
		}
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	// 按张计费：ImagePriceMultiplier 仅作日志/展示用途，反映「尺寸/质量倍率 × 实际张数」。
	// 注意：不要在此处用张数覆盖 ModelPrice —— ModelPrice 在 price.go 中已是
	// `unit_price × size_ratio × quality_ratio`，张数已通过 OtherRatios["n"] 一次性参与计费，
	// 否则会丢失 size/quality 倍率，并与 OtherRatios["n"] 双重计算。
	if info.PriceData.PerImageUnitPrice > 0 {
		sizeQualityMultiplier := info.PriceData.ImagePriceMultiplier
		if sizeQualityMultiplier <= 0 {
			sizeQualityMultiplier = 1
		}
		info.PriceData.ImagePriceMultiplier = sizeQualityMultiplier * float64(imageN)
	}

	quality := "standard"
	if request.Quality == "hd" {
		quality = "hd"
	}

	var logContent []string

	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)
	return nil
}
